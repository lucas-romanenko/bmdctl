// SPDX-License-Identifier: MIT

package hyperdeck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultPort is the HyperDeck Ethernet Protocol's TCP port.
const DefaultPort = 9993

// Timeouts, as hyperdeckwire's DEFAULT_CONNECT_TIMEOUT and
// DEFAULT_READ_TIMEOUT: opening the connection and reading the greeting,
// and waiting for an answer.
const (
	ConnectTimeout = 5 * time.Second
	ReadTimeout    = 5 * time.Second
)

// Errors a session's methods return.
var (
	// ErrClosed: the session has ended, by Close or because the
	// connection failed; the error wraps the cause.
	ErrClosed = errors.New("hyperdeck: session closed")
	// ErrTimeout: the deck didn't answer in time. The session has ended
	// with it (docs/PORTING.md, "HyperDeck": a late answer would be read
	// as the next command's).
	ErrTimeout = errors.New("hyperdeck: no answer in time")
)

// ChangeKind says what a Change is.
type ChangeKind int

const (
	// ChangeState: the deck sent a notification, now in the state.
	ChangeState ChangeKind = iota
	// ChangeDisconnected: the connection has ended; the channel closes
	// after it.
	ChangeDisconnected
)

// Change is a notification the deck sent (a 5xx block) once the state
// holds it.
type Change struct {
	Kind ChangeKind
	Code int // 508
	// Key names the block: "transport", "slot:2", "remote",
	// "configuration", or another block's text.
	Key string
	// Params are the fields the notification carried; Value is the
	// block as the state holds it now.
	Params map[string]string
	Value  map[string]string
}

// Dialer opens sessions with settings Dial has no arguments for.
type Dialer struct {
	// DialContext opens the TCP connection; nil means a net.Dialer's.
	// Tests point it at internal/fakehyperdeck.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// Logger receives the session's log; nil discards it.
	Logger *slog.Logger
	// Bare reads the greeting and nothing more, as hyperdeckwire's
	// connect does: no notifications are turned on, and the state holds
	// only what requests read.
	Bare bool
}

// Dial opens a session to the deck at addr ("host" or "host:port", port
// 9993 by default) with the default Dialer.
func Dial(ctx context.Context, addr string) (*Session, error) { return (&Dialer{}).Dial(ctx, addr) }

// Session is one connection to a deck. One goroutine owns it (the state,
// the commands waiting for answers, the subscribers, every write), and
// one reads what the deck sends; methods hand the owner closures.
type Session struct {
	conn     net.Conn
	log      *slog.Logger
	addr     string
	greeting Response // the first block, as read by Dial

	reqs  chan func()
	done  chan struct{} // closed when the owner has stopped
	ready chan struct{} // closed once Dial has read the deck's state
	conns atomic.Bool   // Connected

	errMu sync.Mutex
	err   error // why the session ended

	// Owned by the run goroutine.
	state   State
	pending []*waiter
	subs    map[int]chan Change
	nextSub int
	closing bool
}

// waiter is a command sent and not yet answered. Answers come in the
// order commands went, one each.
type waiter struct {
	line     string
	deadline time.Time
	ch       chan answer // nil: nobody waits (Send), the answer is logged
}

type answer struct {
	resp Response
	err  error
}

// Dial opens a session: the connection, the greeting (500 connection
// info, hyperdeckwire's connect), then, unless Bare, notifications on
// (transport, slot, remote, configuration) and the deck's state read
// (device info, transport info, each slot's info, remote, configuration,
// clips get). It returns once that is done. A failure of one of those
// (an older deck refusing a notification) is logged and leaves that part
// of the state empty; the connection failing fails Dial. Connecting and
// reading the greeting take ConnectTimeout at most, or less if the
// context's deadline is sooner; each answer after it takes ReadTimeout
// without a context deadline, else what is left of it.
func (d *Dialer) Dial(ctx context.Context, addr string) (*Session, error) {
	addr = withPort(addr)
	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	dial := d.DialContext
	if dial == nil {
		var nd net.Dialer
		dial = nd.DialContext
	}
	// ConnectTimeout whatever the caller's deadline, as hyperdeckwire's
	// connect: a longer deadline (the engine's covers the state read
	// too) had a deck that never answered waited on for all of it.
	cctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	conn, err := dial(cctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Session{conn: conn, log: log.With("hyperdeck", addr), addr: addr, reqs: make(chan func()),
		done: make(chan struct{}), ready: make(chan struct{}), state: newState(), subs: map[int]chan Change{}}
	r := newReader(conn)
	greeting, err := readGreeting(cctx, conn, r)
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.greeting = greeting
	s.state.store(greeting)
	s.conns.Store(true)
	in := make(chan answer)
	go s.read(r, in)
	go s.run(in)
	if !d.Bare {
		if err := s.setup(ctx); err != nil {
			s.Close()
			return nil, err
		}
	}
	close(s.ready)
	s.log.Debug("connected", "model", s.Model(), "protocol", s.ProtocolVersion())
	return s, nil
}

func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(DefaultPort))
}

// readGreeting reads the first block, whatever its code, as
// hyperdeckwire's connect does with skip_async=False, except that an
// error code fails: a deck with all the clients it takes answers 120 and
// closes the connection (docs/PORTING.md, "HyperDeck"). ctx has a
// deadline (Dial sees to it), which bounds the read.
func readGreeting(ctx context.Context, conn net.Conn, r *reader) (Response, error) {
	if dl, ok := ctx.Deadline(); ok {
		conn.SetReadDeadline(dl)
		defer conn.SetReadDeadline(time.Time{})
	}
	g, err := r.response()
	var ne net.Error
	switch {
	case err == nil && g.IsError():
		return g, &Error{g.Code, g.Text}
	case errors.Is(err, errPeerClosed):
		return g, fmt.Errorf("%w: %w", ErrClosed, err)
	case errors.As(err, &ne) && ne.Timeout():
		return g, fmt.Errorf("hyperdeck: no greeting: %w", context.DeadlineExceeded)
	}
	return g, err
}

// notifications are what Dial turns on: the transport, the slots, remote
// control and the configuration. Display timecode would send every frame.
var notifications = []string{"transport", "slot", "remote", "configuration"}

// setup turns notifications on and reads the deck's state.
func (s *Session) setup(ctx context.Context) error {
	ask := func(c Command) (Response, error) {
		r, err := s.SendAndWait(ctx, c)
		var de *Error
		if errors.As(err, &de) {
			s.log.Warn("setup", "command", c.Line(), "err", err)
			return r, nil
		}
		return r, err
	}
	for _, n := range notifications {
		if _, err := ask(Notify{Settings: []Setting{{n, true}}}); err != nil {
			return err
		}
	}
	for _, c := range []Command{DeviceInfo{}, TransportInfo{}, RemoteInfo{}, Configuration{}, ClipsGet{}} {
		if _, err := ask(c); err != nil {
			return err
		}
	}
	n, _ := intField(s.State().Device, "slot count")
	for id := 1; id <= min(n, 16); id++ {
		if _, err := ask(SlotInfo{SlotID: &id}); err != nil {
			return err
		}
	}
	if n == 0 {
		if _, err := ask(SlotInfo{}); err != nil {
			return err
		}
	}
	return nil
}

// read passes each block the deck sends to the owner, and the error that
// ends the stream.
func (s *Session) read(r *reader, in chan<- answer) {
	for {
		resp, err := r.response()
		select {
		case in <- answer{resp, err}:
		case <-s.done:
			return
		}
		if err != nil && !errors.Is(err, ErrMalformed) {
			return
		}
	}
}

// run is the owner goroutine.
func (s *Session) run(in <-chan answer) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case f := <-s.reqs:
			f()
		case a := <-in:
			s.receive(a)
		case now := <-tick.C:
			if len(s.pending) > 0 && now.After(s.pending[0].deadline) {
				s.fail(fmt.Errorf("%w: %q within %v", ErrTimeout, s.pending[0].line, ReadTimeout))
			}
		}
		if s.closing {
			s.finish()
			return
		}
	}
}

// receive takes one block, or the error reading one.
func (s *Session) receive(a answer) {
	switch {
	case a.err != nil && errors.Is(a.err, ErrMalformed):
		// hyperdeckwire raises it to the request that read it.
		s.answer(a)
	case a.err != nil:
		s.fail(a.err)
	case a.resp.IsAsync():
		s.notify(a.resp)
	default:
		s.state.store(a.resp)
		s.answer(a)
	}
}

// answer gives an answer to the oldest command waiting. One that comes
// with no command waiting is logged and dropped: hyperdeckwire would read
// it as the next command's answer.
func (s *Session) answer(a answer) {
	if len(s.pending) == 0 {
		s.log.Warn("an answer to no command", "code", a.resp.Code, "text", a.resp.Text, "err", a.err)
		return
	}
	w := s.pending[0]
	s.pending = s.pending[1:]
	if w.ch != nil {
		w.ch <- a
		return
	}
	if a.err != nil || a.resp.IsError() {
		s.log.Warn("refused", "command", w.line, "code", a.resp.Code, "text", a.resp.Text, "err", a.err)
	}
}

// notify takes a notification into the state and publishes it.
func (s *Session) notify(r Response) {
	key := s.state.store(r)
	s.log.Debug("notification", "code", r.Code, "text", r.Text)
	if key == "" {
		return
	}
	var value map[string]string
	switch {
	case key == "transport":
		value = s.state.Transport
	case key == "remote":
		value = s.state.Remote
	case key == "configuration":
		value = s.state.Configuration
	case key == "connection":
		value = s.state.Connection
	case strings.HasPrefix(key, "slot:"):
		id, _ := strconv.Atoi(key[5:])
		value = s.state.Slots[id]
	default:
		value = s.state.Other[key]
	}
	s.publish(Change{Kind: ChangeState, Code: r.Code, Key: key, Params: r.Params(), Value: value})
}

// publish sends a change to every subscriber, each its own copy. A
// subscriber that has fallen 1024 behind is closed and dropped rather
// than waited for.
func (s *Session) publish(c Change) {
	for id, ch := range s.subs {
		cp := c
		cp.Params, cp.Value = maps.Clone(c.Params), maps.Clone(c.Value)
		select {
		case ch <- cp:
		default:
			close(ch)
			delete(s.subs, id)
			s.log.Warn("subscriber fell behind: dropped")
		}
	}
}

// fail ends the session for err.
func (s *Session) fail(err error) {
	if !errors.Is(err, ErrClosed) {
		err = fmt.Errorf("%w: %w", ErrClosed, err)
	}
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
	s.log.Warn("session ended", "err", err)
	s.closing = true
}

// finish closes everything once the session has ended: the connection,
// the commands waiting, the subscribers.
func (s *Session) finish() {
	s.conns.Store(false)
	s.conn.Close()
	err := s.cause()
	for _, w := range s.pending {
		if w.ch != nil {
			w.ch <- answer{err: err}
		}
	}
	s.pending = nil
	for id, ch := range s.subs {
		select {
		case ch <- Change{Kind: ChangeDisconnected}:
		default:
		}
		close(ch)
		delete(s.subs, id)
	}
	close(s.done)
}

func (s *Session) cause() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		return ErrClosed
	}
	return s.err
}

// do runs f on the owner goroutine and returns once it has run, or
// fails if the session has ended. Once the owner has taken f it runs it
// before anything else.
func (s *Session) do(f func()) error {
	ran := make(chan struct{})
	select {
	case s.reqs <- func() { f(); close(ran) }:
		<-ran
		return nil
	case <-s.done:
		return s.cause()
	}
}

// write sends a line, on the owner goroutine. A write that fails ends
// the session.
func (s *Session) write(line string) error {
	s.conn.SetWriteDeadline(time.Now().Add(ReadTimeout))
	_, err := io.WriteString(s.conn, line+"\n")
	if err != nil {
		s.fail(err)
		return s.cause()
	}
	return nil
}

// send validates c, writes it and registers what waits for its answer,
// in one owner step so the answer can't come first.
func (s *Session) send(c Command, ch chan answer, timeout time.Duration) error {
	if v, ok := c.(Validator); ok {
		if err := v.Validate(); err != nil {
			return err
		}
	} else if err := checkLine(c); err != nil {
		return err
	}
	var err error
	if derr := s.do(func() {
		line := c.Line()
		if err = s.write(line); err == nil {
			s.pending = append(s.pending, &waiter{line: line, deadline: time.Now().Add(timeout), ch: ch})
		}
	}); derr != nil {
		return derr
	}
	return err
}

// Send sends a command and returns without waiting for its answer, which
// the session reads and drops (a refusal is logged).
func (s *Session) Send(c Command) error { return s.send(c, nil, ReadTimeout) }

// Request sends a command and returns its answer, whatever its code:
// hyperdeckwire's request. Asynchronous notifications that come before
// it go to the state and the subscribers, as request skips them. Without
// a context deadline it waits ReadTimeout. The deck not answering in time
// ends the session (ErrTimeout); the context ending first abandons the
// wait, and the answer, when it comes, is dropped.
func (s *Session) Request(ctx context.Context, c Command) (Response, error) {
	timeout := ReadTimeout
	if dl, ok := ctx.Deadline(); ok {
		timeout = max(time.Until(dl), 0)
	}
	ch := make(chan answer, 1)
	if err := s.send(c, ch, timeout); err != nil {
		return Response{}, err
	}
	select {
	case a := <-ch:
		return a.resp, a.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// The deadline is the waiter's too: the owner ends the
			// session for it at its next tick.
			return Response{}, fmt.Errorf("%w: %q: %w", ErrTimeout, c.Line(), ctx.Err())
		}
		return Response{}, ctx.Err()
	}
}

// SendAndWait sends a command and waits for its answer, as Request does,
// with a refusal (1xx) as an *Error: hyperdeckwire's _check_ok.
func (s *Session) SendAndWait(ctx context.Context, c Command) (Response, error) {
	r, err := s.Request(ctx, c)
	if err == nil && r.IsError() {
		return r, &Error{r.Code, r.Text}
	}
	return r, err
}

// State returns a copy of what the session has read from the deck.
func (s *Session) State() State {
	var st State
	if s.do(func() { st = s.state.clone() }) != nil {
		return newState()
	}
	return st
}

// Subscribe returns a channel of the notifications the deck sends from
// now on, and a function to stop them. The channel closes when the
// session ends (after a ChangeDisconnected) or the subscriber falls 1024
// behind.
func (s *Session) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 1024)
	id := -1
	if s.do(func() { id = s.nextSub; s.nextSub++; s.subs[id] = ch }) != nil {
		close(ch)
		return ch, func() {}
	}
	return ch, func() {
		s.do(func() {
			if c, ok := s.subs[id]; ok {
				close(c)
				delete(s.subs, id)
			}
		})
	}
}

// Ready returns a channel closed once the session has read the deck's
// state, which it has when Dial returns. After the session ends it
// returns one that never closes.
func (s *Session) Ready() <-chan struct{} {
	if !s.Connected() {
		return make(chan struct{})
	}
	return s.ready
}

// Connected reports whether the session is open: from Dial until Close or
// the connection failing.
func (s *Session) Connected() bool { return s.conns.Load() }

// Addr is the deck's address, host:port.
func (s *Session) Addr() string { return s.addr }

// Greeting is the first block the deck sent, 500 connection info.
func (s *Session) Greeting() Response { return s.greeting }

// Model and ProtocolVersion are what the greeting said: hyperdeckwire's
// model and protocol_version.
func (s *Session) Model() string { return s.greeting.Params()["model"] }

func (s *Session) ProtocolVersion() string { return s.greeting.Params()["protocol version"] }

// Close sends quit, as close does, and ends the session. Closing an
// ended session does nothing.
func (s *Session) Close() error {
	s.do(func() {
		s.write(quit{}.Line())
		s.errMu.Lock()
		if s.err == nil {
			s.err = ErrClosed
		}
		s.errMu.Unlock()
		s.closing = true
	})
	<-s.done
	return nil
}
