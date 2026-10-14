// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The session: one switcher connection, what atemwire spreads over
// ATEMConnection (the worker thread, send queue and readiness), AtemProtocol
// (loop, mixerstate, events) and ready.wait_ready, in bmdwire v1.3.0. See
// doc.go for the threading contract and docs/PORTING.md for why it is
// shaped this way.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectTimeout bounds Dial when its context has no deadline: atemwire's
// CONNECT_TIMEOUT, the handshake plus the whole initial state dump.
const ConnectTimeout = 15 * time.Second

// burstSpacing is the least time between two command packets (atemwire's
// BURST_SEND_SPACING): a burst of commands trickles out instead of arriving
// as one microburst the switcher drops the tail of.
const burstSpacing = 10 * time.Millisecond

// subscriberBuffer is how many changes a subscriber may fall behind by
// before its channel is closed.
const subscriberBuffer = 1024

var (
	// ErrClosed is returned once the session has been closed or has
	// failed. errors.Is matches it; the error also wraps the cause.
	ErrClosed = errors.New("atem: session closed")
	// ErrNotReady is returned by SendAndWait between a disconnect and the
	// end of the next state dump.
	ErrNotReady = errors.New("atem: switcher state is not ready")
	// ErrNoEcho is returned by SendAndWait for a command with no echo.
	ErrNoEcho = errors.New("atem: command has no state echo to wait for")
	// ErrDisconnected is returned by SendAndWait when the session drops
	// before the echo arrives.
	ErrDisconnected = errors.New("atem: switcher disconnected")
	// ErrIncompleteDump is returned by Dial when the switcher's state dump
	// ends without its video mode, which a lost first dump packet causes
	// (bmdwire#13), and the fresh dumps of incompleteDumpRetries
	// re-handshakes did too. The error says which sequence number came
	// first.
	ErrIncompleteDump = errors.New("atem: the state dump ended without a video mode")

	// errIncomplete stops a packet's commands at an InCm that ended a dump
	// without a video mode: the session re-handshakes for a fresh one.
	errIncomplete = errors.New("atem: the state dump ended without a video mode")
)

// incompleteDumpRetries is how many times in a row a dump that ends
// without a video mode is answered by a re-handshake for a fresh one, as
// atemwire's INCOMPLETE_DUMP_RETRIES (bmdwire#13, bmdwire#27): a switcher
// whose VidM never parses costs a few handshakes, not a loop.
const incompleteDumpRetries = 3

// ChangeKind says what a Change reports.
type ChangeKind uint8

const (
	// ChangeState: a state packet was stored.
	ChangeState ChangeKind = iota
	// ChangeReady: the switcher's state dump is complete (atemwire's
	// "connected" event).
	ChangeReady
	// ChangeDisconnected: the session was lost and the state emptied; the
	// transport is handshaking again (atemwire's "disconnected" event).
	ChangeDisconnected
)

// Change is one event from the session.
type Change struct {
	Kind ChangeKind
	// Opcode is the four-character code of the state packet ("PrgI").
	Opcode string
	// Key is Opcode followed by the packet's indices, colon-separated
	// ("PrgI:0", "KeOn:0:1"), or Opcode alone for a packet stored bare.
	// It is what Command.EchoKey names.
	Key string
	// Value is a copy of what was stored. It belongs to the receiver.
	Value any
}

// Dialer opens sessions. The zero value is ready to use.
type Dialer struct {
	// Conn, if set, is used instead of a new UDP socket, for tests and the
	// fake switcher in internal/fakeatem. The session closes it.
	Conn net.PacketConn
	// Logger receives the session's logs. Nil discards them.
	Logger *slog.Logger

	silence time.Duration // tests shorten the 5 s silence timeout
}

// Dial connects to the switcher at addr ("host" or "host:port", UDP 9910 by
// default) and returns once the handshake is done, the whole initial state
// dump has arrived and the switcher has reported its video mode: atemwire's
// ATEMConnection.connect and wait_ready. If ctx has no deadline,
// ConnectTimeout applies.
func Dial(ctx context.Context, addr string) (*Session, error) {
	var d Dialer
	return d.Dial(ctx, addr)
}

// Dial is the package-level Dial with the Dialer's settings.
func (d *Dialer) Dial(ctx context.Context, addr string) (*Session, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ConnectTimeout)
		defer cancel()
	}
	raddr, err := resolve(ctx, addr)
	if err != nil {
		return nil, err
	}
	conn := d.Conn
	if conn == nil {
		uc, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return nil, fmt.Errorf("atem: dial %s: %w", addr, err)
		}
		_ = uc.SetReadBuffer(16 << 20) // atemwire asks for 16 MiB too
		conn = uc
	}
	s := newSession(conn, raddr, d.Logger, d.silence)
	go s.read()
	go s.run()
	select {
	case <-s.dialReady:
		return s, nil
	case <-s.done:
		return nil, fmt.Errorf("atem: dial %s: %w", addr, s.closedErr())
	case <-ctx.Done():
		s.Close()
		return nil, fmt.Errorf("atem: dial %s: %w", addr, ctx.Err())
	}
}

func resolve(ctx context.Context, addr string) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, strconv.Itoa(DefaultPort)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("atem: bad port in %q", addr)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, fmt.Errorf("atem: resolve %s: %w", host, err)
	}
	return &net.UDPAddr{IP: ips[0].AsSlice(), Port: int(p)}, nil
}

// Session is a connection to one switcher. Its methods are safe to call
// from any goroutine.
type Session struct {
	conn net.PacketConn
	log  *slog.Logger

	// Owned by the run goroutine: nothing else reads or writes these.
	t              *transport
	state          State
	protoConnected bool // AtemProtocol.connected: a data packet has arrived
	initialized    bool // AtemProtocol.initialized: the state dump is complete
	readyClosed    bool // the current ready channel has been closed
	dialed         bool // dialReady has been closed
	outbox         [][]byte
	lastFlush      time.Time
	pace           *time.Timer
	waiters        map[string][]*waiter
	subs           map[*subscription]struct{}
	fatal          error
	dumpErr        error // the first parse error since the dump began
	// incompleteDumps counts the dumps in a row that ended without a
	// video mode, each answered by a re-handshake (bmdwire#13).
	incompleteDumps int
	lane            lane // the transfer engine (transfer.go)

	// Shared.
	reqs      chan func()   // closures the run goroutine executes
	datagrams chan datagram // from the read goroutine
	quit      chan struct{} // closed by Close
	done      chan struct{} // closed when run returns
	closeOnce sync.Once
	err       error // why run returned; read only after done is closed
	dialReady chan struct{}
	connected atomic.Bool
	ready     atomic.Pointer[chan struct{}]
}

type datagram struct {
	b    []byte
	from net.Addr
	err  error
}

type waiter struct {
	ch chan waitResult // buffered 1: the run goroutine never blocks on it
}

type waitResult struct {
	change Change
	err    error
}

type subscription struct {
	ch chan Change
}

func newSession(conn net.PacketConn, raddr *net.UDPAddr, log *slog.Logger, silence time.Duration) *Session {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Session{
		conn:      conn,
		log:       log,
		t:         newTransport(conn, raddr, log),
		state:     newState(),
		waiters:   make(map[string][]*waiter),
		subs:      make(map[*subscription]struct{}),
		lane:      newLane(),
		reqs:      make(chan func()),
		datagrams: make(chan datagram, 256),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		dialReady: make(chan struct{}),
	}
	if silence > 0 {
		s.t.silence = silence
	}
	ready := make(chan struct{})
	s.ready.Store(&ready)
	return s
}

// Connected reports whether the session is open: from Dial's return until
// Close or a fatal socket error. It stays true while the transport
// re-handshakes after a silence, as atemwire's is_connected does.
func (s *Session) Connected() bool { return s.connected.Load() }

// Ready returns a channel that is closed while the switcher's state is
// complete: from the end of a state dump until the next disconnect, when a
// new, open channel replaces it (atemwire's is_ready). After Close it
// returns a channel that is never closed.
func (s *Session) Ready() <-chan struct{} { return *s.ready.Load() }

// State returns a copy of the switcher's state. It is empty after Close.
func (s *Session) State() State {
	ch := make(chan State, 1)
	if s.do(func() { ch <- s.state.clone() }) != nil {
		return newState()
	}
	return <-ch
}

// Send queues cmd for the switcher and returns. It does not wait for the
// command to be applied; see SendAndWait.
func (s *Session) Send(cmd Command) error {
	b, err := encode(cmd)
	if err != nil {
		return err
	}
	return s.do(func() { s.queue(b) })
}

// SendAndWait sends cmd and returns the change whose Key is cmd.EchoKey()
// once it arrives: the switcher's state echo, the only confirmation the
// protocol has (SURFACE.md §4.4). It fails at once with ErrNoEcho for a
// command without an echo and ErrNotReady while the state is not ready,
// and later with ErrDisconnected if the session drops first.
func (s *Session) SendAndWait(ctx context.Context, cmd Command) (Change, error) {
	key := cmd.EchoKey()
	if key == "" {
		return Change{}, ErrNoEcho
	}
	b, err := encode(cmd)
	if err != nil {
		return Change{}, err
	}
	w := &waiter{ch: make(chan waitResult, 1)}
	errc := make(chan error, 1)
	if err := s.do(func() {
		if !s.initialized {
			errc <- ErrNotReady
			return
		}
		// Registered before the command is queued, so the echo can't
		// slip past.
		s.waiters[key] = append(s.waiters[key], w)
		s.queue(b)
		errc <- nil
	}); err != nil {
		return Change{}, err
	}
	if err := <-errc; err != nil {
		return Change{}, err
	}
	select {
	case r := <-w.ch:
		return r.change, r.err
	case <-ctx.Done():
		_ = s.do(func() { s.dropWaiter(key, w) })
		return Change{}, ctx.Err()
	}
}

// Subscribe returns a channel of every Change from now on, and a function
// that ends the subscription and closes the channel. A subscriber that
// falls subscriberBuffer changes behind is dropped and its channel closed
// rather than silently missing changes; it can subscribe again and call
// State. The channel is also closed when the session ends.
func (s *Session) Subscribe() (<-chan Change, func()) {
	sub := &subscription{ch: make(chan Change, subscriberBuffer)}
	if s.do(func() { s.subs[sub] = struct{}{} }) != nil {
		close(sub.ch)
		return sub.ch, func() {}
	}
	return sub.ch, func() { _ = s.do(func() { s.unsubscribe(sub) }) }
}

// Close says goodbye to the switcher, closes the socket and waits for the
// session's goroutines to finish. It is safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { close(s.quit) })
	<-s.done
	return nil
}

func encode(cmd Command) ([]byte, error) {
	if v, ok := cmd.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("atem: %s: %w", cmd.Opcode(), err)
		}
	}
	b := cmd.Encode()
	if len(b) > maxCommandBytes {
		return nil, fmt.Errorf("atem: %s is %d bytes, over the %d-byte packet budget", cmd.Opcode(), len(b), maxCommandBytes)
	}
	return b, nil
}

// do runs f on the run goroutine. reqs is unbuffered, so once the send
// succeeds f runs; if the session has ended it returns the error instead.
func (s *Session) do(f func()) error {
	select {
	case s.reqs <- f:
		return nil
	case <-s.done:
		return s.closedErr()
	}
}

func (s *Session) closedErr() error {
	if s.err != nil {
		return fmt.Errorf("%w: %w", ErrClosed, s.err)
	}
	return ErrClosed
}

// read is the read goroutine. It only reads datagrams and passes them on;
// every decision about them is made by run.
func (s *Session) read() {
	for {
		buf := make([]byte, 2048) // atemwire's recvfrom(2048)
		n, from, err := s.conn.ReadFrom(buf)
		select {
		case s.datagrams <- datagram{b: buf[:n], from: from, err: err}:
		case <-s.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// run is the owner goroutine: it alone touches the transport, the state,
// the outbox, the waiters and the subscribers.
func (s *Session) run() {
	defer close(s.done)
	s.err = s.loop()
	s.shutdown()
}

func (s *Session) loop() error {
	s.pace = time.NewTimer(time.Hour)
	s.pace.Stop()
	if err := s.t.connect(); err != nil {
		return err
	}
	silence := time.NewTimer(s.t.silence)
	for {
		select {
		case <-s.quit:
			return nil
		case d := <-s.datagrams:
			if d.err != nil {
				return fmt.Errorf("atem: read: %w", d.err)
			}
			s.onDatagram(d)
		case f := <-s.reqs:
			f()
			// Run everything else already waiting before sending, so
			// commands sent together can share a packet.
			for more := true; more; {
				select {
				case f := <-s.reqs:
					f()
				default:
					more = false
				}
			}
			s.flush()
		case <-s.pace.C:
			s.flush()
		case <-s.bulkTimerC():
			s.onBulkTimer()
		case <-silence.C:
			if s.t.silent() {
				s.onSilence()
			}
		}
		if s.fatal != nil {
			return s.fatal
		}
		silence.Reset(time.Until(s.t.lastRX.Add(s.t.silence)))
	}
}

func (s *Session) shutdown() {
	s.connected.Store(false)
	never := make(chan struct{})
	s.ready.Store(&never)
	if s.t.state == stateEstablished {
		s.releaseLocksNow()
	}
	s.t.closeSession()
	_ = s.conn.Close()
	err := s.closedErr()
	s.resetLane(err)
	for _, ws := range s.waiters {
		for _, w := range ws {
			w.ch <- waitResult{err: err}
		}
	}
	for sub := range s.subs {
		close(sub.ch)
	}
	s.waiters, s.subs = nil, nil
}

func (s *Session) onDatagram(d datagram) {
	pkts, err := s.t.receive(d.b, d.from)
	if err != nil {
		s.fatal = err
		return
	}
	for _, p := range pkts {
		ackOn := s.t.enableACK
		h, err := s.t.handle(p)
		if err != nil {
			s.fatal = err
			return
		}
		if h.ready {
			s.onReady()
		}
		// receive_packet's two queue_trigger calls: for every packet once
		// ACKs flow, and again for a control frame. A drained queue is
		// handled before the packet's data, as atemwire's flush sentinel is.
		if ackOn {
			s.bulkTrigger(true)
		}
		if h.control {
			s.bulkTrigger(true)
		}
		if h.data != nil && s.fatal == nil {
			s.onData(h.data)
		}
		if s.fatal != nil {
			return
		}
	}
	if !s.dialed && s.initialized && s.state.hasVideoMode() {
		// wait_ready's condition: the dump is complete and the video
		// mode has arrived.
		s.dialed = true
		s.connected.Store(true)
		close(s.dialReady)
	}
}

// onReady is AtemProtocol.loop's ConnectionReady branch: the state dump is
// complete. atemwire asks for the time once, here.
func (s *Session) onReady() {
	s.protoConnected = true
	s.initialized = true
	if err := s.t.sendReliable(timeRequest); err != nil {
		s.fatal = err
		return
	}
	if !s.readyClosed {
		s.readyClosed = true
		close(*s.ready.Load())
	}
	s.publish(Change{Kind: ChangeReady})
}

// timeRequest is a TiRq command: header only, no payload.
var timeRequest = []byte{0, 8, 0, 0, 'T', 'i', 'R', 'q'}

// onData is the data branch of AtemProtocol.loop.
func (s *Session) onData(data []byte) {
	s.protoConnected = true
	err := decodeFields(data, func(code string, payload []byte) error {
		switch code {
		case "LKOB", "FTCD", "FTDa", "FTDE", "FTDC":
			return s.onTransferPacket(code, payload)
		case "LKST":
			// The engine reacts to a release first, then it is stored and
			// published, as in save_field_data.
			v, err := parsers[code].parse(payload)
			if err != nil {
				return err
			}
			if f := v.(*LockStateField); !f.State {
				s.onLockReleased(f.Store)
			}
		}
		c, err := s.state.store(code, payload)
		if err != nil && !s.initialized && s.dumpErr == nil {
			s.dumpErr = fmt.Errorf("%s: %w", code, err)
		}
		if err != nil || c == nil {
			return err
		}
		s.publish(*c)
		if code == "InCm" {
			if !s.state.hasVideoMode() && s.incompleteDumps < incompleteDumpRetries {
				// Its first packet was lost for good (the first reliable
				// packet set the receive baseline): ask for a fresh dump
				// at once, as atemwire raises IncompleteStateDump.
				s.incompleteDumps++
				s.log.Error("atem: the state dump ended without a video mode, so its first packet was probably lost; re-handshaking",
					"first_sequence", s.t.firstRX, "attempt", s.incompleteDumps, "of", incompleteDumpRetries)
				return errIncomplete
			}
			// The end of the state dump: the next packet reports ready.
			s.t.markNextConnected = true
			if s.state.hasVideoMode() {
				s.incompleteDumps = 0
			} else {
				s.incompleteDumps = 0
				s.incompleteDump()
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, errIncomplete):
		s.resync("atem: forcing a clean session reconnect for a fresh state dump")
	case errors.Is(err, errCorrupt):
		s.resync("atem: protocol corruption, forcing a clean session reconnect")
	case err != nil:
		// An exception escaping atemwire's loop(): the rest of this
		// packet is lost, the session carries on.
		s.log.Error("atem: packet dropped part way", "err", err)
	}
}

// incompleteDump: the state dump ended (InCm) without a video mode, and
// so did the fresh dumps of incompleteDumpRetries re-handshakes. atemwire
// then carries on as it did before bmdwire#13, which for Dial means
// waiting out wait_ready's timeout; here Dial fails at once, saying which
// sequence number arrived first, since a packet the switcher sent before
// that one is lost for good (docs/PORTING.md). After Dial the session
// carries on without a video mode, as atemwire's does, and logs why.
func (s *Session) incompleteDump() {
	err := fmt.Errorf("%w: the first dump packet to arrive was sequence %d, and any the switcher sent before it was lost", ErrIncompleteDump, s.t.firstRX)
	if s.dumpErr != nil {
		err = fmt.Errorf("%w; a parse error also dropped part of the dump (%w)", err, s.dumpErr)
	}
	if !s.dialed {
		s.fatal = err
		return
	}
	s.log.Error("atem: incomplete state dump", "err", err)
}

// resync is atemwire's answer to a zero-length command header and to a
// dump without a video mode: report a disconnect, say goodbye and
// handshake again for a fresh state dump.
func (s *Session) resync(why string) {
	s.log.Error(why)
	s.disconnected()
	s.protoConnected = false
	s.t.closeSession()
	if err := s.t.connect(); err != nil {
		if errors.Is(err, errAlreadyOpen) {
			s.log.Error("atem: post-corruption reconnect failed", "err", err)
			return
		}
		s.fatal = err
	}
}

// onSilence: 5 s without traffic. The transport has handshaken again;
// atemwire's loop() then reports a disconnect if it had data.
func (s *Session) onSilence() {
	if err := s.t.timeout(); err != nil {
		s.fatal = err
		return
	}
	if s.protoConnected {
		s.disconnected()
	}
	s.protoConnected = false
}

// disconnected is atemwire's "disconnected" event and what follows it:
// the state is emptied and nothing is ready until the next dump completes.
func (s *Session) disconnected() {
	// Not ready before anyone is told ErrDisconnected, as atemwire's
	// 'disconnected' handler clears _session_ready before the lane is
	// reset: a caller that hears it and waits on Ready must not find the
	// old, closed channel.
	if s.readyClosed {
		s.readyClosed = false
		ready := make(chan struct{})
		s.ready.Store(&ready)
	}
	// The last-run macro latch lives on ATEMConnection, not in mixerstate,
	// so it survives.
	last := s.state.Macros.LastRun
	s.state = newState()
	s.state.Macros.LastRun = last
	s.initialized = false
	s.dumpErr = nil
	// A transfer in flight died with the session; the new one must not
	// inherit its queue or locks (_reset_transfer_lane).
	s.resetLane(ErrDisconnected)
	// A waiter could otherwise be satisfied by the next dump, which would
	// say nothing about whether its command was applied.
	for key, ws := range s.waiters {
		for _, w := range ws {
			w.ch <- waitResult{err: ErrDisconnected}
		}
		delete(s.waiters, key)
	}
	s.publish(Change{Kind: ChangeDisconnected})
}

// publish hands a change to the waiters on its key and to every subscriber,
// each with its own copy of the value.
func (s *Session) publish(c Change) {
	if c.Kind == ChangeState {
		for _, w := range s.waiters[c.Key] {
			w.ch <- waitResult{change: Change{Kind: c.Kind, Opcode: c.Opcode, Key: c.Key, Value: cloneValue(c.Value)}}
		}
		delete(s.waiters, c.Key)
	}
	for sub := range s.subs {
		cc := c
		cc.Value = cloneValue(c.Value)
		select {
		case sub.ch <- cc:
		default:
			s.log.Warn("atem: subscriber fell behind; closing its channel")
			s.unsubscribe(sub)
		}
	}
}

func (s *Session) unsubscribe(sub *subscription) {
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.ch)
	}
}

func (s *Session) dropWaiter(key string, w *waiter) {
	ws := s.waiters[key]
	for i, x := range ws {
		if x == w {
			s.waiters[key] = append(ws[:i:i], ws[i+1:]...)
			break
		}
	}
	if len(s.waiters[key]) == 0 {
		delete(s.waiters, key)
	}
}

// queue adds an encoded command to the outbox.
func (s *Session) queue(b []byte) {
	s.outbox = append(s.outbox, b)
}

// flush sends the outbox as reliable packets of at most maxCommandBytes,
// at most one every burstSpacing. A packet goes at once if none has gone
// in the last burstSpacing; otherwise it waits for that mark and takes
// whatever has been queued meanwhile. While a transfer is running there is
// no spacing, as atemwire's worker skips it then.
func (s *Session) flush() {
	for len(s.outbox) > 0 {
		if wait := time.Until(s.lastFlush.Add(burstSpacing)); wait > 0 && !s.lane.active() {
			s.pace.Reset(wait)
			return
		}
		var pkt []byte
		n := 0
		for n < len(s.outbox) && len(pkt)+len(s.outbox[n]) <= maxCommandBytes {
			pkt = append(pkt, s.outbox[n]...)
			n++
		}
		s.outbox = s.outbox[n:]
		if err := s.t.sendReliable(pkt); err != nil {
			s.fatal = err
			return
		}
		s.lastFlush = time.Now()
	}
}
