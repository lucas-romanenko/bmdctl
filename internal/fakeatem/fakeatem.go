// SPDX-License-Identifier: MIT

// Package fakeatem is a fake ATEM switcher for tests. It works at the
// net.PacketConn level: a Switcher is the socket a session talks through,
// so the session's whole stack (transport, handshake, ACKs, in-order
// delivery, state) runs for real against it.
//
//	sw := fakeatem.New(dump)
//	s, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
//
// It answers the handshake, replays an initial state dump (normally
// internal/fixtures/dump/initial-dump.bin), ACKs what the session sends,
// echoes the commands in echoes.go as state packets, pings every
// PingInterval, resends what the session hasn't ACKed and asks for what
// it missed. Tests can make it drop packets, delay its ACKs, go silent,
// send stale packets from an old session, send any command, let another
// client hold a store lock, and inject datagrams from anywhere.
//
// It is not a switcher model: commands without an echo handler are ACKed
// and otherwise ignored.
package fakeatem

import (
	"bytes"
	"encoding/binary"
	"net"
	"slices"
	"sync"
	"time"
)

// Address is the address to dial: TEST-NET-1, which is never a real
// switcher.
const Address = "192.0.2.10"

var (
	switcherAddr = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 9910}
	clientAddr   = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 100), Port: 50000}
)

// Header flags, as on the wire.
const (
	flagReliable              = 0x01
	flagSYN                   = 0x02
	flagRetransmission        = 0x04
	flagRequestRetransmission = 0x08
	flagACK                   = 0x10

	helloSession = 0x1337
	maxData      = 1400 // most command bytes the fake puts in one packet
)

// Timing.
var (
	PingInterval = 500 * time.Millisecond // an idle switcher still sends
	ResendAfter  = 60 * time.Millisecond  // a real switcher resends after about 80 ms
	tickInterval = 10 * time.Millisecond
)

// Switcher is a fake ATEM. Its methods are safe for concurrent use.
type Switcher struct {
	mu        sync.Mutex
	dump      []byte
	out       []datagram // waiting for the session's ReadFrom
	wake      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once

	handshakes  int
	session     uint16
	established bool
	seq         uint16 // last reliable sequence sent
	unacked     map[uint16]*sent
	lastPing    time.Time
	nextClient  uint16 // next reliable sequence expected from the session
	clientKnown bool
	state       map[string][]byte // current state payloads, for echoes
	commands    [][]byte

	dropToClient   int
	dropSeqs       map[uint16]bool
	dropFromClient int
	ackDelay       time.Duration
	silent         bool
	stale          int // packets of the old session to send around the next handshake

	locks map[uint16]holder // store locks that are held
}

// holder is who holds a store lock.
type holder uint8

const (
	heldByOther   holder = iota + 1 // another client
	heldBySession                   // the session under test
)

type datagram struct {
	b    []byte
	from net.Addr
}

type sent struct {
	b  []byte
	at time.Time
}

// New returns a switcher that sends dump (concatenated commands, each
// with its 8-byte header) after every handshake.
func New(dump []byte) *Switcher {
	sw := &Switcher{
		dump:     dump,
		wake:     make(chan struct{}, 1),
		closed:   make(chan struct{}),
		unacked:  map[uint16]*sent{},
		state:    map[string][]byte{},
		dropSeqs: map[uint16]bool{},
		locks:    map[uint16]holder{},
	}
	forEachCommand(dump, func(code string, payload []byte) { sw.remember(code, payload) })
	go sw.tick()
	return sw
}

// DropToClient drops the next n reliable packets the switcher sends (the
// first time each is sent): the state it would have delivered is resent
// like any the session didn't ACK.
func (sw *Switcher) DropToClient(n int) { sw.mu.Lock(); sw.dropToClient += n; sw.mu.Unlock() }

// DropFromClient ignores the next n datagrams the session sends.
func (sw *Switcher) DropFromClient(n int) { sw.mu.Lock(); sw.dropFromClient += n; sw.mu.Unlock() }

// DelayACKs makes the switcher ACK the session's packets d late.
func (sw *Switcher) DelayACKs(d time.Duration) { sw.mu.Lock(); sw.ackDelay = d; sw.mu.Unlock() }

// Silence cuts the switcher off (true) or reconnects it (false): while
// silent it neither sends nor hears anything, as if the cable were pulled.
// Going silent also ends the switcher's side of the session, as a real
// switcher's does when its packets go unacknowledged, so after reconnecting
// it waits for the session to handshake again.
func (sw *Switcher) Silence(on bool) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.silent = on
	if on {
		sw.endSession()
		sw.unacked = map[uint16]*sent{}
		sw.out = nil
	}
}

// DropSequences drops the first sending of the reliable packets with these
// sequence numbers in the next session (the dump starts at 1); their
// resends go through.
func (sw *Switcher) DropSequences(seqs ...uint16) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	for _, s := range seqs {
		sw.dropSeqs[s] = true
	}
}

// SendStale makes the switcher, around the session's next handshake, send
// n reliable packets carrying the previous session's id and its next
// sequence numbers, both before it answers the SYN and again before the new
// session's first packet: what a real switcher does while it keeps
// resending an old session it hasn't yet given up on.
func (sw *Switcher) SendStale(n int) { sw.mu.Lock(); sw.stale = n; sw.mu.Unlock() }

// Send sends cmds (each a whole command, header included; see Command) to
// the session in one reliable packet. Anything goes, including commands no
// switcher sends and malformed ones.
func (sw *Switcher) Send(cmds ...[]byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.established {
		sw.sendReliable([][]byte{bytes.Join(cmds, nil)})
	}
}

// Inject hands the session a datagram as if from addr, which needn't be
// the switcher. Silence doesn't stop it.
func (sw *Switcher) Inject(from net.Addr, b []byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.out = append(sw.out, datagram{append([]byte(nil), b...), from})
	sw.wakeUp()
}

// HoldLock makes another client take store's lock, if it is free: the
// switcher broadcasts it (LKST) to the session.
func (sw *Switcher) HoldLock(store uint16) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if _, held := sw.locks[store]; !held {
		sw.locks[store] = heldByOther
		sw.broadcast(lockState(store, true))
	}
}

// ReleaseLock makes the other client let go of store's lock, if it holds
// it: the switcher broadcasts the release.
func (sw *Switcher) ReleaseLock(store uint16) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.locks[store] == heldByOther {
		delete(sw.locks, store)
		sw.broadcast(lockState(store, false))
	}
}

// Handshakes returns how many handshakes the switcher has completed.
func (sw *Switcher) Handshakes() int { sw.mu.Lock(); defer sw.mu.Unlock(); return sw.handshakes }

// Commands returns every command the switcher has received, in order,
// each with its 8-byte header.
func (sw *Switcher) Commands() [][]byte {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return append([][]byte(nil), sw.commands...)
}

// --- net.PacketConn, the session's side ------------------------------------

// ReadFrom returns the next datagram from the switcher.
func (sw *Switcher) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		sw.mu.Lock()
		if len(sw.out) > 0 {
			d := sw.out[0]
			sw.out = sw.out[1:]
			sw.mu.Unlock()
			return copy(p, d.b), d.from, nil
		}
		sw.mu.Unlock()
		select {
		case <-sw.wake:
		case <-sw.closed:
			return 0, nil, net.ErrClosed
		}
	}
}

// WriteTo hands the switcher one datagram from the session.
func (sw *Switcher) WriteTo(p []byte, addr net.Addr) (int, error) {
	select {
	case <-sw.closed:
		return 0, net.ErrClosed
	default:
	}
	if u, ok := addr.(*net.UDPAddr); !ok || !u.IP.Equal(switcherAddr.IP) || u.Port != switcherAddr.Port {
		return len(p), nil // sent somewhere else
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.silent {
		return len(p), nil
	}
	if sw.dropFromClient > 0 {
		sw.dropFromClient--
		return len(p), nil
	}
	sw.receive(append([]byte(nil), p...))
	return len(p), nil
}

// Close stops the switcher.
func (sw *Switcher) Close() error {
	sw.closeOnce.Do(func() { close(sw.closed) })
	return nil
}

func (sw *Switcher) LocalAddr() net.Addr              { return clientAddr }
func (sw *Switcher) SetDeadline(time.Time) error      { return nil }
func (sw *Switcher) SetReadDeadline(time.Time) error  { return nil }
func (sw *Switcher) SetWriteDeadline(time.Time) error { return nil }

// --- the switcher -------------------------------------------------------------

type packet struct {
	flags          uint8
	session        uint16
	ack            uint16
	retransmitFrom uint16
	seq            uint16
	data           []byte
}

func parse(b []byte) (packet, bool) {
	if len(b) < 12 || int(binary.BigEndian.Uint16(b)&0x07FF) != len(b) {
		return packet{}, false
	}
	return packet{
		flags:   b[0] >> 3,
		session: binary.BigEndian.Uint16(b[2:]),
		ack:     binary.BigEndian.Uint16(b[4:]),
		seq:     binary.BigEndian.Uint16(b[10:]),
		data:    b[12:],
	}, true
}

func (p packet) marshal() []byte {
	b := make([]byte, 12+len(p.data))
	binary.BigEndian.PutUint16(b, uint16(p.flags)<<11|uint16(len(b)))
	binary.BigEndian.PutUint16(b[2:], p.session)
	binary.BigEndian.PutUint16(b[4:], p.ack)
	binary.BigEndian.PutUint16(b[6:], p.retransmitFrom)
	binary.BigEndian.PutUint16(b[10:], p.seq)
	copy(b[12:], p.data)
	return b
}

// receive handles one datagram from the session. sw.mu is held.
func (sw *Switcher) receive(b []byte) {
	p, ok := parse(b)
	if !ok {
		return
	}
	switch {
	case p.flags&flagSYN != 0 && len(p.data) > 0 && p.data[0] == 0x01:
		// A new session: accept it under the temporary id.
		sw.sendStale()
		sw.established = false
		sw.send(packet{flags: flagSYN, session: helloSession, data: []byte{0x02, 0, 0, 0, 0, 0, 0, 0}})
		return
	case p.flags&flagSYN != 0 && len(p.data) > 0 && p.data[0] == 0x04:
		sw.endSession() // goodbye
		return
	case !sw.established && p.flags&flagACK != 0 && p.session == helloSession:
		sw.sendStale()
		sw.stale = 0
		sw.startSession()
		return
	}
	if !sw.established || p.session != sw.session {
		return
	}
	if p.flags&flagACK != 0 {
		for s := range sw.unacked {
			if (p.ack-s)&0x7FFF < 0x4000 {
				delete(sw.unacked, s)
			}
		}
	}
	if p.flags&flagReliable == 0 {
		return
	}
	switch {
	case !sw.clientKnown || p.seq == sw.nextClient:
		sw.clientKnown = true
		sw.nextClient = (p.seq + 1) & 0x7FFF
	case (p.seq-sw.nextClient)&0x7FFF < 0x4000:
		// A gap: ask for everything from the first missing packet.
		sw.send(packet{flags: flagRequestRetransmission, session: sw.session, retransmitFrom: sw.nextClient})
		return
	default:
		sw.ackClient(p.seq) // a duplicate: ACK it again, don't redo it
		return
	}
	sw.ackClient(p.seq)
	var echoes [][]byte
	forEachCommand(p.data, func(code string, payload []byte) {
		sw.commands = append(sw.commands, Command(code, payload))
		echoes = append(echoes, sw.echo(code, payload)...)
	})
	if len(echoes) > 0 {
		sw.sendReliable(echoes)
	}
}

// sendStale sends the packets SendStale asked for, under the session id
// the switcher used last, continuing its sequence numbers.
func (sw *Switcher) sendStale() {
	if sw.handshakes == 0 {
		return
	}
	for range sw.stale {
		sw.seq = (sw.seq + 1) & 0x7FFF
		sw.send(packet{flags: flagReliable, session: sw.session, seq: sw.seq, data: Command("Stal", []byte{0, 0, 0, 0})})
	}
}

// endSession forgets the session: the switcher gives up its locks.
func (sw *Switcher) endSession() {
	sw.established = false
	for store, h := range sw.locks {
		if h == heldBySession {
			delete(sw.locks, store)
		}
	}
}

// startSession runs after the handshake: a fresh session id, the dump,
// and the packet after it that tells the session the dump is complete.
func (sw *Switcher) startSession() {
	sw.handshakes++
	sw.session = 0x8000 | uint16(sw.handshakes)
	sw.established = true
	sw.seq = 0
	sw.unacked = map[uint16]*sent{}
	sw.clientKnown = false
	var cmds [][]byte
	forEachCommand(sw.dump, func(code string, payload []byte) { cmds = append(cmds, Command(code, payload)) })
	sw.sendReliable(cmds)
	sw.sendReliable(nil) // the post-dump ping
}

// sendReliable sends cmds in as many reliable packets as they need (one
// empty packet for none).
func (sw *Switcher) sendReliable(cmds [][]byte) {
	var data []byte
	flush := func() {
		sw.seq = (sw.seq + 1) & 0x7FFF
		b := packet{flags: flagReliable, session: sw.session, seq: sw.seq, data: data}.marshal()
		sw.unacked[sw.seq] = &sent{b: b, at: time.Now()}
		switch {
		case sw.dropSeqs[sw.seq]:
			delete(sw.dropSeqs, sw.seq)
		case sw.dropToClient > 0:
			sw.dropToClient--
		default:
			sw.enqueue(b)
		}
		data = nil
	}
	for _, c := range cmds {
		if len(data) > 0 && len(data)+len(c) > maxData {
			flush()
		}
		data = append(data, c...)
	}
	if len(data) > 0 || len(cmds) == 0 {
		flush()
	}
	sw.lastPing = time.Now()
}

func (sw *Switcher) ackClient(seq uint16) {
	ack := packet{flags: flagACK, session: sw.session, ack: seq}
	if sw.ackDelay == 0 {
		sw.send(ack)
		return
	}
	session := sw.session
	time.AfterFunc(sw.ackDelay, func() {
		sw.mu.Lock()
		defer sw.mu.Unlock()
		if sw.established && sw.session == session && !sw.silent {
			sw.send(ack)
		}
	})
}

func (sw *Switcher) send(p packet) { sw.enqueue(p.marshal()) }

// enqueue queues a datagram for the session.
func (sw *Switcher) enqueue(b []byte) {
	if sw.silent {
		return
	}
	sw.out = append(sw.out, datagram{b, switcherAddr})
	sw.wakeUp()
}

func (sw *Switcher) wakeUp() {
	select {
	case sw.wake <- struct{}{}:
	default:
	}
}

// tick resends what the session hasn't ACKed and pings when idle.
func (sw *Switcher) tick() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-sw.closed:
			return
		case <-t.C:
		}
		sw.mu.Lock()
		if sw.established && !sw.silent {
			now := time.Now()
			var due []uint16
			for seq, u := range sw.unacked {
				if now.Sub(u.at) >= ResendAfter {
					due = append(due, seq)
				}
			}
			// Oldest first: furthest behind the latest sequence.
			slices.SortFunc(due, func(a, b uint16) int {
				return int((sw.seq-b)&0x7FFF) - int((sw.seq-a)&0x7FFF)
			})
			for _, seq := range due {
				u := sw.unacked[seq]
				b := append([]byte(nil), u.b...)
				b[0] |= flagRetransmission << 3
				u.at = now
				sw.enqueue(b)
			}
			if now.Sub(sw.lastPing) >= PingInterval {
				sw.sendReliable(nil)
			}
		}
		sw.mu.Unlock()
	}
}

// forEachCommand walks concatenated commands.
func forEachCommand(b []byte, f func(code string, payload []byte)) {
	for len(b) >= 8 {
		n := int(binary.BigEndian.Uint16(b))
		if n < 8 || n > len(b) {
			return
		}
		f(string(b[4:8]), b[8:n])
		b = b[n:]
	}
}

// Command builds one command: the 8-byte header and the payload.
func Command(code string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(b, uint16(len(b)))
	copy(b[4:], code)
	copy(b[8:], payload)
	return b
}

// broadcast sends cmds to the session, if there is one: what a switcher
// tells every connected client.
func (sw *Switcher) broadcast(cmds ...[]byte) {
	if sw.established {
		sw.sendReliable(cmds)
	}
}

// lockState is the LKST the switcher broadcasts when a store's lock is
// taken or released.
func lockState(store uint16, taken bool) []byte {
	p := []byte{byte(store >> 8), byte(store), 0, 0}
	if taken {
		p[2] = 1
	}
	return Command("LKST", p)
}
