// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The UDP transport, ported from bmdwire v1.3.0 atemwire/transport.py
// (Packet and UdpProtocol). Python splits it across a UDP thread
// (_receive_packet_low, _send_packet_low) and the protocol worker
// (receive_packet); here both halves are methods on one struct that only the
// session's owner goroutine calls, so nothing in it is locked. It never reads
// the socket itself: the session hands it each datagram.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"
)

// DefaultPort is the switcher's UDP port.
const DefaultPort = 9910

// Header flags, the top five bits of the first header word.
const (
	flagReliable              = 0x01
	flagSYN                   = 0x02
	flagRetransmission        = 0x04
	flagRequestRetransmission = 0x08
	flagACK                   = 0x10
)

const (
	headerLen = 12

	// helloSession is the session id a client uses until the switcher
	// assigns one. The first byte of a SYN's data is the hello opcode.
	helloSession  = 0x1337
	helloConnect  = 0x01
	helloAccepted = 0x02
	helloClosing  = 0x04

	// ackMagic goes in header bytes 8-9 of every ACK atemwire sends.
	ackMagic = 0x61

	// The switcher's sequence numbers are 15 bits: they wrap 0x7FFF -> 0.
	seqMask = 0x7FFF
	seqHalf = 0x4000

	// receivedHistory is the length of atemwire's received_packets deque,
	// which drives both the retransmission dedup and the contiguous ACK.
	receivedHistory = 1024

	// silenceTimeout is the select() timeout of atemwire's UDP thread:
	// this long without a datagram accepted from the switcher, and the
	// transport re-handshakes (see lastRX).
	silenceTimeout = 5 * time.Second
)

// The paced bulk send queue (BaseProtocol.send_queue and queue_trigger),
// which carries upload chunks.
const (
	bulkBatch    = 5                    // batch_size: packets per trigger
	bulkDelay    = 3 * time.Millisecond // batch_delay: atemwire sleeps this long after a batch
	bulkQueueMax = 1024                 // the deque's maxlen
)

// The defaults for the three tunables atemwire keeps as class attributes
// (and its tests monkeypatch).
const (
	defaultRetransmitWindow   = 4096 // RETRANSMIT_WINDOW
	defaultDeliveryParkWindow = 512  // DELIVERY_PARK_WINDOW
	defaultRetransmitMaxBurst = 128  // RETRANSMIT_MAX_BURST
)

type transportState uint8

const (
	stateClosed transportState = iota
	stateSYNSent
	stateSYNReceived // declared by atemwire, never entered
	stateEstablished
)

func (s transportState) String() string {
	switch s {
	case stateClosed:
		return "closed"
	case stateSYNSent:
		return "syn-sent"
	case stateSYNReceived:
		return "syn-received"
	case stateEstablished:
		return "established"
	}
	return fmt.Sprintf("state(%d)", uint8(s))
}

// packet is one datagram: a 12-byte header and the data after it.
//
//	bytes 0-1   flags (top 5 bits) | length (low 11 bits)
//	bytes 2-3   session id
//	bytes 4-5   acknowledgement number
//	bytes 6-7   retransmission request: the first sequence the sender is
//	            missing, on a FLAG_REQUEST_RETRANSMISSION packet
//	bytes 8-9   "remote sequence number": 0x61 on atemwire's ACKs
//	bytes 10-11 sequence number
type packet struct {
	flags          uint8
	length         uint16
	session        uint16
	ack            uint16
	retransmitFrom uint16
	remoteSeq      uint16
	seq            uint16
	data           []byte

	// handshake marks the ACK that completes the handshake (atemwire's
	// "_handshake" label): once it is sent, the session id is forgotten
	// and taken from the next packet the switcher sends.
	handshake bool
}

var (
	errMalformed   = errors.New("atem: malformed datagram")
	errAlreadyOpen = errors.New("atem: trying to open a connection that's already open")
)

// parsePacket mirrors Packet.from_bytes: the length in the header must
// equal the datagram's length.
func parsePacket(b []byte) (packet, error) {
	if len(b) < headerLen {
		return packet{}, fmt.Errorf("%w: %d bytes, shorter than the header", errMalformed, len(b))
	}
	word := binary.BigEndian.Uint16(b[0:])
	p := packet{
		flags:          uint8(word >> 11),
		length:         word & 0x07FF,
		session:        binary.BigEndian.Uint16(b[2:]),
		ack:            binary.BigEndian.Uint16(b[4:]),
		retransmitFrom: binary.BigEndian.Uint16(b[6:]),
		remoteSeq:      binary.BigEndian.Uint16(b[8:]),
		seq:            binary.BigEndian.Uint16(b[10:]),
	}
	if int(p.length) != len(b) {
		return packet{}, fmt.Errorf("%w: %d in header but data length is %d", errMalformed, p.length, len(b))
	}
	p.data = append([]byte(nil), b[headerLen:]...)
	return p, nil
}

// marshal mirrors Packet.to_bytes. Bytes 6-7 are always zero: atemwire
// never asks the switcher to retransmit.
func (p *packet) marshal() []byte {
	b := make([]byte, headerLen+len(p.data))
	binary.BigEndian.PutUint16(b[0:], uint16(len(b))+uint16(p.flags)<<11)
	binary.BigEndian.PutUint16(b[2:], p.session)
	binary.BigEndian.PutUint16(b[4:], p.ack)
	binary.BigEndian.PutUint16(b[8:], p.remoteSeq)
	binary.BigEndian.PutUint16(b[10:], p.seq)
	copy(b[headerLen:], p.data)
	return b
}

// seqHistory is atemwire's received_packets: a deque of the last 1024
// sequence numbers seen, with O(1) membership.
type seqHistory struct {
	ring  [receivedHistory]uint16
	head  int // index of the oldest entry
	size  int
	count map[uint16]int
}

func (h *seqHistory) add(seq uint16) {
	if h.count == nil {
		h.count = make(map[uint16]int)
	}
	if h.size == receivedHistory {
		old := h.ring[h.head]
		if h.count[old]--; h.count[old] == 0 {
			delete(h.count, old)
		}
		h.ring[h.head] = seq
		h.head = (h.head + 1) % receivedHistory
	} else {
		h.ring[(h.head+h.size)%receivedHistory] = seq
		h.size++
	}
	h.count[seq]++
}

func (h *seqHistory) contains(seq uint16) bool { return h.count[seq] > 0 }

func (h *seqHistory) clear() { *h = seqHistory{} }

// transport is atemwire's UdpProtocol for one switcher.
type transport struct {
	conn net.PacketConn
	addr *net.UDPAddr
	log  *slog.Logger
	now  func() time.Time

	state        transportState
	sessionID    uint16
	sessionKnown bool // false where atemwire's session_id is None

	// prevSession is the id of the session before the last connect, while
	// prevKnown. Its packets are dropped unseen (bmdwire#12): after a
	// silence the switcher keeps resending the old session's packets for
	// a while, and they must not reach the new one.
	prevSession uint16
	prevKnown   bool

	localSeq      int    // last sequence sent; -1 right after connect
	remoteSeq     uint16 // sequence of the latest packet received
	ackNumber     uint16 // contiguous high-water mark we ACK
	rxEstablished bool   // the first reliable packet has set the baseline

	deliverNext  uint16 // next sequence owed to the session
	deliverKnown bool
	parked       map[uint16]packet

	enableACK  bool
	hadTraffic bool

	// markNextConnected is set when InCm (end of the state dump) is stored;
	// the next packet through handle reports ready before it is handled.
	markNextConnected bool

	received      seqHistory
	retransmitBuf map[uint16]*packet

	// bulk is atemwire's send_queue: the data of reliable packets (upload
	// chunks) waiting for queueTrigger. bulkEnabled is queue_enabled: the
	// queue has had packets since it last reported draining.
	bulk        [][]byte
	bulkEnabled bool

	// lastRX is when a datagram was last accepted from the switcher (the
	// right peer, well formed, not stale), or connect last ran. The
	// silence timeout counts from it. atemwire's select() also wakes on
	// foreign datagrams and on its own sends, so a client sending to a
	// dead switcher never times out there (bmdwire#11); here neither
	// counts.
	lastRX  time.Time
	silence time.Duration // silenceTimeout; tests shorten it

	// firstRX is the sequence of the first reliable packet of the session,
	// the one that set the receive baseline (rxEstablished). A packet the
	// switcher sent before it can't be recovered; see bmdwire#13 in
	// docs/PORTING.md.
	firstRX uint16

	retransmitWindow int
	parkWindow       int
	maxBurst         int
}

func newTransport(conn net.PacketConn, addr *net.UDPAddr, log *slog.Logger) *transport {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &transport{
		conn:             conn,
		addr:             addr,
		log:              log,
		now:              time.Now,
		state:            stateClosed,
		sessionID:        helloSession,
		sessionKnown:     true,
		parked:           make(map[uint16]packet),
		retransmitBuf:    make(map[uint16]*packet),
		silence:          silenceTimeout,
		retransmitWindow: defaultRetransmitWindow,
		parkWindow:       defaultDeliveryParkWindow,
		maxBurst:         defaultRetransmitMaxBurst,
	}
}

// connect resets the per-session state and sends the SYN. It is
// UdpProtocol.connect, which refuses to run unless the state is closed.
func (t *transport) connect() error {
	if t.state != stateClosed {
		return errAlreadyOpen
	}
	if t.sessionKnown && t.sessionID != helloSession {
		t.prevSession, t.prevKnown = t.sessionID, true
	}
	t.lastRX = t.now()
	t.localSeq = -1
	t.remoteSeq = 0
	t.ackNumber = 0
	t.rxEstablished = false
	t.deliverKnown = false
	clear(t.parked)
	t.sessionID, t.sessionKnown = helloSession, true
	t.enableACK = false
	clear(t.retransmitBuf)
	t.received.clear()
	t.hadTraffic = false
	// markNextConnected is not reset, as in atemwire.

	syn := &packet{flags: flagSYN, data: []byte{helloConnect, 0, 0, 0, 0, 0, 0, 0}}
	if err := t.sendLow(syn); err != nil {
		return err
	}
	t.state = stateSYNSent
	return nil
}

// queuePacket is queue_packet: queue reliable packet data for queueTrigger.
// A full queue drops its oldest packet, loudly, as the bounded deque does.
func (t *transport) queuePacket(data []byte) {
	if len(t.bulk) == bulkQueueMax {
		t.log.Error("atem: bulk send queue overflow, dropping the oldest packet; the transfer is likely corrupt", "max", bulkQueueMax)
		t.bulk = t.bulk[1:]
	}
	t.bulk = append(t.bulk, data)
}

// queueTrigger is queue_trigger without its sleep: send up to bulkBatch
// queued packets. flushed is true once, on the first call that finds the
// queue empty after it had packets (atemwire's TransferQueueFlushed). The
// session paces the calls.
func (t *transport) queueTrigger() (sent int, flushed bool, err error) {
	if len(t.bulk) == 0 {
		if t.bulkEnabled {
			t.bulkEnabled = false
			return 0, true, nil
		}
		return 0, false, nil
	}
	t.bulkEnabled = true
	for sent < bulkBatch && len(t.bulk) > 0 {
		d := t.bulk[0]
		t.bulk = t.bulk[1:]
		if err := t.sendReliable(d); err != nil {
			return sent, false, err
		}
		sent++
	}
	return sent, false, nil
}

// silent reports whether nothing has been accepted from the switcher for
// the silence timeout.
func (t *transport) silent() bool {
	return t.now().Sub(t.lastRX) >= t.silence
}

// timeout is the UDP thread's select() timeout: drop the session and
// handshake again at once. The caller reports a disconnect.
func (t *transport) timeout() error {
	t.log.Error("atem: timeout reading from switcher", "addr", t.addr)
	t.state = stateClosed
	return t.connect()
}

// sendReliable sends data in a reliable, sequenced packet (send_raw).
func (t *transport) sendReliable(data []byte) error {
	return t.sendLow(&packet{flags: flagReliable, data: data})
}

// sendLow is _send_packet_low. A non-nil error is fatal to the session: it
// is what kills atemwire's UDP thread.
func (t *transport) sendLow(p *packet) error {
	if !t.sessionKnown {
		// atemwire's to_bytes fails on a None session id and the packet
		// is dropped; it consumes no sequence number.
		t.log.Error("atem: dropping unserializable packet: session id not known yet", "flags", p.flags)
		return nil
	}
	p.session = t.sessionID
	if p.flags&flagACK == 0 {
		if t.localSeq == -1 {
			t.localSeq = 0
		}
		p.seq = uint16((t.localSeq + 1) % 0x8000)
	}
	raw := p.marshal()
	if _, err := t.conn.WriteTo(raw, t.addr); err != nil {
		if transientSendError(err) {
			// Dropped before it consumes a sequence number, so the next
			// reliable packet reuses this one's.
			t.log.Error("atem: transient send error (packet dropped)", "err", err)
			return nil
		}
		return fmt.Errorf("atem: send: %w", err)
	}
	if p.flags&(flagSYN|flagACK) == 0 {
		t.localSeq = (t.localSeq + 1) % 0x8000
		seq := uint16(t.localSeq)
		t.retransmitBuf[seq] = p
		delete(t.retransmitBuf, uint16(mod(t.localSeq-t.retransmitWindow, 0x8000)))
	}
	if p.handshake {
		// Forget the temporary id: the switcher's next packet carries
		// the real one.
		t.sessionKnown = false
	}
	return nil
}

// transientSendError is atemwire's TRANSIENT_SEND_ERRNOS: momentary network
// conditions that drop one packet without ending the session.
func transientSendError(err error) bool {
	for _, errno := range []syscall.Errno{
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ECONNREFUSED,
		syscall.ENOBUFS, syscall.EAGAIN, syscall.EPERM, syscall.EINTR,
	} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// receive is _receive_packet_low for one datagram. It returns the packets
// now owed to the session, in sequence order: none (dropped, duplicate or
// parked), the packet itself, or the packet plus the parked ones it
// released. A non-nil error is a fatal send error from the ACK.
func (t *transport) receive(b []byte, from net.Addr) ([]packet, error) {
	if !t.fromSwitcher(from) {
		// The socket is unconnected: anyone on the network can reach it.
		t.log.Debug("atem: ignoring datagram from foreign peer", "from", from)
		return nil, nil
	}
	p, err := parsePacket(b)
	if err != nil {
		t.log.Warn("atem: ignoring malformed datagram", "from", from, "err", err)
		return nil, nil
	}
	if t.stale(p) {
		t.log.Debug("atem: ignoring packet from another session", "session", p.session, "seq", p.seq, "state", t.state)
		return nil, nil
	}
	t.lastRX = t.now()
	if p.flags&flagRequestRetransmission != 0 {
		// The switcher missed one of ours: go back N from the sequence in
		// header bytes 6-7.
		t.retransmitFrom(p.retransmitFrom)
	}
	t.remoteSeq = p.seq
	if !t.sessionKnown {
		t.sessionID, t.sessionKnown = p.session, true
	}
	if p.flags&flagRetransmission != 0 && t.received.contains(t.remoteSeq) {
		return nil, nil
	}
	t.received.add(t.remoteSeq)
	t.updateACK(p)

	// ACK every reliable packet once established (or once ACKing has
	// started), and the first no-data control frame before that.
	if (p.flags&flagReliable != 0 && (t.enableACK || t.state == stateEstablished)) ||
		(!t.enableACK && len(p.data) == 0) {
		t.enableACK = true
		ack := &packet{flags: flagACK, ack: t.ackNumber, remoteSeq: ackMagic}
		if err := t.sendLow(ack); err != nil {
			return nil, err
		}
	}

	// The in-order delivery gate: reliable packets after the baseline reach
	// the session strictly in sequence order. Handshake and other
	// unreliable frames pass straight through.
	if p.flags&flagReliable == 0 || !t.rxEstablished {
		return []packet{p}, nil
	}
	seq := p.seq
	if !t.deliverKnown {
		t.deliverNext, t.deliverKnown = (seq+1)&seqMask, true
		return []packet{p}, nil
	}
	dist := (seq - t.deliverNext) & seqMask
	switch {
	case dist == 0:
		t.deliverNext = (seq + 1) & seqMask
		out := []packet{p}
		for {
			q, ok := t.parked[t.deliverNext]
			if !ok {
				break
			}
			delete(t.parked, t.deliverNext)
			out = append(out, q)
			t.deliverNext = (t.deliverNext + 1) & seqMask
		}
		return out, nil
	case dist > seqHalf:
		// Behind the cursor: already delivered.
		return nil, nil
	case int(dist) <= t.parkWindow:
		// Ahead of a gap: hold it. The stalled ACK makes the switcher
		// resend the missing packet, which releases this one.
		t.parked[seq] = p
		return nil, nil
	}
	t.log.Error("atem: in-order delivery resync", "cursor", t.deliverNext, "seq", seq, "parked_dropped", len(t.parked))
	clear(t.parked)
	t.deliverNext = (seq + 1) & seqMask
	return []packet{p}, nil
}

// stale reports a packet that belongs to no session of ours, which the
// transport drops before any of its state sees it (bmdwire#12; atemwire
// checks the session id only in _handshake): one carrying the previous
// session's id, or, while the handshake is in flight, anything but the
// switcher's SYN reply under the hello id. After the handshake ACK the
// session id is taken from the next packet that passes, so a stale one
// can't be adopted.
func (t *transport) stale(p packet) bool {
	if t.prevKnown && p.session == t.prevSession {
		return true
	}
	return t.state == stateSYNSent && (p.flags&flagSYN == 0 || p.session != helloSession)
}

// fromSwitcher compares hosts only, as atemwire does (address[0] != ip).
func (t *transport) fromSwitcher(from net.Addr) bool {
	if u, ok := from.(*net.UDPAddr); ok {
		return u.IP.Equal(t.addr.IP)
	}
	host, _, err := net.SplitHostPort(from.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.Equal(t.addr.IP)
}

// updateACK is _update_ack_number: before the first reliable packet the
// ACK mirrors the latest sequence; after it, it advances only through
// sequences received without a gap.
func (t *transport) updateACK(p packet) {
	if !t.rxEstablished {
		t.ackNumber = t.remoteSeq
		if p.flags&flagReliable != 0 {
			t.rxEstablished = true
			t.firstRX = p.seq
		}
		return
	}
	for t.received.contains((t.ackNumber + 1) & seqMask) {
		t.ackNumber = (t.ackNumber + 1) & seqMask
	}
}

// retransmitFrom is _retransmit_from: resend every buffered packet from
// requestSeq through the latest sent, in order, flagged as
// retransmissions, with their original sequence numbers. It writes to the
// socket directly, so resends don't take new sequence numbers. It returns
// the number resent.
func (t *transport) retransmitFrom(requestSeq uint16) int {
	last := t.localSeq
	span := (last - int(requestSeq)) & seqMask
	if span >= t.retransmitWindow {
		t.log.Error("atem: retransmission request outside the window", "from", requestSeq, "latest", last, "window", t.retransmitWindow)
		return 0
	}
	resent := 0
	for offset := 0; offset < min(span+1, t.maxBurst); offset++ {
		seq := uint16((int(requestSeq) + offset) & seqMask)
		p, ok := t.retransmitBuf[seq]
		if !ok {
			continue
		}
		p.flags |= flagRetransmission
		if _, err := t.conn.WriteTo(p.marshal(), t.addr); err != nil {
			t.log.Error("atem: retransmit failed", "seq", seq, "err", err)
			break
		}
		resent++
	}
	t.log.Warn("atem: retransmission requested", "from", requestSeq, "resent", resent, "through", last)
	return resent
}

// handled is what handle makes of one delivered packet.
type handled struct {
	// ready: the state dump finished before this packet (atemwire's
	// ConnectionReady). The session reports it before using data.
	ready bool
	// data is the packet's command data, for a data packet on an
	// established session; nil otherwise.
	data []byte
	// control: a control frame (no data) on an established session, after
	// which atemwire triggers the bulk queue a second time.
	control bool
}

// handle is the per-packet part of receive_packet: the handshake while
// SYN_SENT, control frames and data once established.
func (t *transport) handle(p packet) (handled, error) {
	var h handled
	if t.markNextConnected {
		// atemwire returns the sentinel first and hands this same packet
		// over on the next call; doing both here keeps that order.
		t.markNextConnected = false
		h.ready = true
	}
	switch t.state {
	case stateSYNSent:
		t.hadTraffic = true
		return h, t.handshake(p)
	case stateEstablished:
		if p.length == headerLen {
			// A control frame. Start ACKing if nothing has yet.
			h.control = true
			if !t.enableACK {
				t.enableACK = true
				ack := &packet{flags: flagACK, ack: t.ackNumber, remoteSeq: ackMagic}
				if err := t.sendLow(ack); err != nil {
					return h, err
				}
			}
			return h, nil
		}
		h.data = p.data
	}
	return h, nil
}

// handshake is _handshake: answer the switcher's SYN with the ACK that
// completes the handshake. Only hello 0x02 (accepted) establishes the
// session; atemwire ACKs any other answer too and stays in SYN_SENT.
func (t *transport) handshake(p packet) error {
	if p.flags&flagSYN == 0 {
		return nil
	}
	if !t.sessionKnown || p.session != t.sessionID {
		return nil
	}
	if len(p.data) == 0 {
		// atemwire raises IndexError here; the packet is lost.
		t.log.Warn("atem: SYN without a hello opcode")
		return nil
	}
	if p.data[0] == helloAccepted {
		t.state = stateEstablished
	}
	return t.sendLow(&packet{flags: flagACK, handshake: true})
}

// closeSession is the goodbye: one SYN with hello 0x04, fire and forget,
// only on an established session. It releases every lock the session holds.
func (t *transport) closeSession() {
	if t.state != stateEstablished {
		return
	}
	bye := &packet{flags: flagSYN, data: []byte{helloClosing, 0, 0, 0, 0, 0, 0, 0}}
	if err := t.sendLow(bye); err != nil {
		t.log.Debug("atem: goodbye not sent", "err", err)
	}
	t.state = stateClosed
}

// mod is Python's %, which is never negative for a positive modulus.
func mod(a, m int) int {
	return ((a % m) + m) % m
}
