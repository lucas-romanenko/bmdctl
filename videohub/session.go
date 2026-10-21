// SPDX-License-Identifier: MIT

package videohub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultPort is the Videohub Ethernet Protocol's TCP port.
const DefaultPort = 9990

// Timeouts, as videohubwire's: opening the connection
// (DEFAULT_CONNECT_TIMEOUT), the preamble and each answer
// (DEFAULT_READ_TIMEOUT), the quiet that ends a preamble without END
// PRELUDE (PREAMBLE_QUIET_S), and the wait before the connect's one
// retry (CONNECT_RETRY_DELAY_S).
const (
	ConnectTimeout    = 3 * time.Second
	ReadTimeout       = 3 * time.Second
	PreambleQuiet     = 300 * time.Millisecond
	ConnectRetryDelay = 300 * time.Millisecond
)

// ChangeKind says what a Change is.
type ChangeKind int

const (
	// ChangeState: the hub pushed a block, now in the state.
	ChangeState ChangeKind = iota
	// ChangeDisconnected: the connection has ended; the channel closes
	// after it.
	ChangeDisconnected
)

// Change is a block the hub pushed after the preamble (another client
// routing, a label edited at the panel) once the state holds it.
type Change struct {
	Kind ChangeKind
	// Key names the block: "routing", "locks", "input labels", "output
	// labels", "device", "protocol".
	Key string
	// Values are what the block carried: by index ("3") for routing,
	// locks and labels (the route's value is the input), by key for the
	// device and protocol blocks.
	Values map[string]string
}

// Dialer opens sessions with settings Dial has no arguments for.
type Dialer struct {
	// DialContext opens the TCP connection; nil means a net.Dialer's.
	// Tests point it at internal/fakevideohub.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// Logger receives the session's log; nil discards it.
	Logger *slog.Logger

	// Tests' timings: zero means the constants above.
	quiet, readTimeout, retryDelay time.Duration
}

// Dial opens a session to the hub at addr ("host" or "host:port", port
// 9990 by default) with the default Dialer.
func Dial(ctx context.Context, addr string) (*Session, error) { return (&Dialer{}).Dial(ctx, addr) }

// Session is one connection to a hub. One goroutine owns it (the state,
// the commands waiting for answers, the subscribers, every write), and
// one reads what the hub sends; methods hand the owner closures.
type Session struct {
	conn        net.Conn
	log         *slog.Logger
	addr, host  string
	readTimeout time.Duration

	reqs  chan func()
	done  chan struct{} // closed when the owner has stopped
	ready chan struct{} // closed once Dial has read the preamble
	conns atomic.Bool   // Connected

	errMu sync.Mutex
	err   error // why the session ended
	final State // the state when it ended

	// Owned by the run goroutine.
	state   State
	pending []*waiter
	subs    map[int]chan Change
	nextSub int
	closing bool
}

// waiter is a command sent and not yet answered: the hub answers each
// with ACK or NAK, in order, other blocks between.
type waiter struct {
	cmd      Command
	deadline time.Time
	ch       chan error
}

type incoming struct {
	lines []string
	err   error
}

// Dial opens a session: the connection, with one retry after
// ConnectRetryDelay (a first packet lost to ARP), then the preamble, read
// as videohubwire's _read_preamble reads it: to END PRELUDE, or, once the
// device block and a routing block have arrived, to PreambleQuiet without
// a block. Before those it waits ReadTimeout from the start, then fails:
// no state preamble. It returns once the preamble is read.
func (d *Dialer) Dial(ctx context.Context, addr string) (*Session, error) {
	addr = withPort(addr)
	host, _, _ := net.SplitHostPort(addr)
	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	quiet, readTimeout, retry := or(d.quiet, PreambleQuiet), or(d.readTimeout, ReadTimeout), or(d.retryDelay, ConnectRetryDelay)
	conn, err := d.connect(ctx, addr, retry)
	if err != nil {
		return nil, err
	}
	s := &Session{conn: conn, log: log.With("videohub", addr), addr: addr, host: host, readTimeout: readTimeout,
		reqs: make(chan func()), done: make(chan struct{}), ready: make(chan struct{}), state: newState(),
		subs: map[int]chan Change{}}
	in := make(chan incoming)
	stop := make(chan struct{})
	go read(newReader(conn), in, stop, s.done)
	if err := s.preamble(ctx, in, quiet, readTimeout); err != nil {
		close(stop)
		conn.Close()
		return nil, err
	}
	s.conns.Store(true)
	// Before the owner starts: from then on only it touches the state.
	s.log.Debug("connected", "model", s.state.Model(), "protocol", s.state.ProtocolVersion)
	go s.run(in, stop)
	close(s.ready)
	return s, nil
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(DefaultPort))
}

// connect opens the connection with ConnectTimeout, and once more after
// retry if that fails.
func (d *Dialer) connect(ctx context.Context, addr string, retry time.Duration) (net.Conn, error) {
	dial := d.DialContext
	if dial == nil {
		var nd net.Dialer
		dial = nd.DialContext
	}
	once := func() (net.Conn, error) {
		cctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
		defer cancel()
		return dial(cctx, "tcp", addr)
	}
	conn, err := once()
	if err == nil {
		return conn, nil
	}
	select {
	case <-time.After(retry):
	case <-ctx.Done():
		return nil, err
	}
	return once()
}

// read passes each block the hub sends to whoever reads in, then the
// error that ends the stream, until stop or done.
func read(r *reader, in chan<- incoming, stop, done <-chan struct{}) {
	for {
		lines, err := r.block()
		select {
		case in <- incoming{lines, err}:
		case <-stop:
			return
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

// preamble is _read_preamble.
func (s *Session) preamble(ctx context.Context, in <-chan incoming, quiet, readTimeout time.Duration) error {
	deadline := time.Now().Add(readTimeout)
	for {
		essentials := len(s.state.Device) > 0 && len(s.state.Routing) > 0
		wait := quiet
		if !essentials {
			wait = time.Until(deadline)
		}
		b, ok, err := within(ctx, in, wait)
		switch {
		case err != nil:
			return err
		case !ok && essentials:
			return nil
		case !ok:
			return &Error{Msg: fmt.Sprintf("no state preamble from %s — is this a Videohub?", s.host), kind: ErrClosed}
		}
		if h, _ := apply(&s.state, b, s.log); h == hdrEnd {
			return nil
		}
	}
}

// within is _recv_block with a timeout: a block already read is taken
// even when the wait is over.
func within(ctx context.Context, in <-chan incoming, wait time.Duration) ([]string, bool, error) {
	select {
	case m := <-in:
		return m.lines, m.err == nil, m.err
	default:
	}
	if wait <= 0 {
		return nil, false, nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case m := <-in:
		return m.lines, m.err == nil, m.err
	case <-t.C:
		return nil, false, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// run is the owner goroutine.
func (s *Session) run(in <-chan incoming, stop chan struct{}) {
	defer close(stop)
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case f := <-s.reqs:
			f()
		case m := <-in:
			s.receive(m)
		case now := <-tick.C:
			if len(s.pending) > 0 && now.After(s.pending[0].deadline) {
				msg, _ := what(s.pending[0].cmd)
				s.fail(&Error{Msg: msg, kind: ErrTimeout})
			}
		}
		if s.closing {
			s.finish()
			return
		}
	}
}

// receive takes one block, or the error reading one: an ACK or NAK
// answers the oldest command waiting, anything else is applied and
// published.
func (s *Session) receive(m incoming) {
	if m.err != nil {
		s.fail(m.err)
		return
	}
	switch h := header(m.lines); h {
	case hdrACK, hdrNAK:
		if len(s.pending) == 0 {
			s.log.Debug("an answer to no command", "answer", h)
			return
		}
		w := s.pending[0]
		if _, isPing := w.cmd.(Ping); isPing && h == hdrNAK {
			// ping() applies a NAK as a block, which changes nothing,
			// and waits on.
			return
		}
		s.pending = s.pending[1:]
		if h == hdrACK {
			acked(w.cmd, &s.state)
			w.ch <- nil
			return
		}
		_, refused := what(w.cmd)
		w.ch <- &Error{Msg: refused, kind: ErrRefused}
	default:
		key, vals := apply(&s.state, m.lines, s.log)
		if vals == nil {
			return
		}
		s.publish(Change{Kind: ChangeState, Key: keys[key], Values: vals})
	}
}

// publish sends a change to every subscriber, each its own copy. A
// subscriber that has fallen 1024 behind is closed and dropped rather
// than waited for.
func (s *Session) publish(c Change) {
	for id, ch := range s.subs {
		cp := c
		cp.Values = cloneMap(c.Values)
		select {
		case ch <- cp:
		default:
			close(ch)
			delete(s.subs, id)
			s.log.Warn("subscriber fell behind: dropped")
		}
	}
}

func cloneMap(m map[string]string) map[string]string {
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// fail ends the session for err.
func (s *Session) fail(err error) {
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
	if !errors.Is(err, ErrTimeout) {
		s.log.Warn("session ended", "err", err)
	}
	s.closing = true
}

// finish closes everything once the session has ended: the connection,
// the commands waiting (the oldest with the error that ended it, if a
// timeout), the subscribers.
func (s *Session) finish() {
	s.conns.Store(false)
	s.conn.Close()
	s.errMu.Lock()
	s.final = s.state.clone()
	s.errMu.Unlock()
	err := s.cause()
	for _, w := range s.pending {
		w.ch <- err
		err = s.closedErr()
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
		return s.closedErr()
	}
	return s.err
}

// closedErr is what a call to an ended session gets: videohubwire's "not
// connected" after close, else why it ended.
func (s *Session) closedErr() error {
	if s.err != nil && !errors.Is(s.err, errClosedByUs) {
		if errors.Is(s.err, ErrClosed) {
			return s.err
		}
		return &Error{Msg: s.err.Error(), kind: ErrClosed}
	}
	return &Error{Msg: "not connected", kind: ErrClosed}
}

var errClosedByUs = &Error{Msg: "not connected", kind: ErrClosed}

// do runs f on the owner goroutine and returns once it has run, or fails
// if the session has ended.
func (s *Session) do(f func()) error {
	ran := make(chan struct{})
	select {
	case s.reqs <- func() { f(); close(ran) }:
		<-ran
		return nil
	case <-s.done:
		s.errMu.Lock()
		defer s.errMu.Unlock()
		return s.closedErr()
	}
}

// Send checks a command as videohubwire's method does, with the hub's
// counts as the state has them, sends it and waits for the hub's ACK:
// route(), set_input_label(), set_output_label(), ping(). A NAK is an
// *Error (ErrRefused). Without a context deadline it waits the read
// timeout; the hub not answering in time ends the session (ErrTimeout),
// and the context ending first abandons the wait, its answer, when it
// comes, dropped.
func (s *Session) Send(ctx context.Context, c Command) error {
	ch := make(chan error, 1)
	timeout := s.readTimeout
	if dl, ok := ctx.Deadline(); ok {
		timeout = max(time.Until(dl), 0)
	}
	var err error
	if derr := s.do(func() {
		if err = check(c, s.state); err != nil {
			return
		}
		s.conn.SetWriteDeadline(time.Now().Add(s.readTimeout))
		if _, err = io.WriteString(s.conn, c.Block()); err != nil {
			s.fail(err)
			return
		}
		s.pending = append(s.pending, &waiter{cmd: c, deadline: time.Now().Add(timeout), ch: ch})
	}); derr != nil {
		return derr
	}
	if err != nil {
		return err
	}
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Route is route(dest, src): input src to output dest, 0-based.
func (s *Session) Route(ctx context.Context, dest, src int) error {
	return s.Send(ctx, Route{Dest: dest, Src: src})
}

// SetInputLabel is set_input_label(index, label).
func (s *Session) SetInputLabel(ctx context.Context, index int, label string) error {
	return s.Send(ctx, InputLabel{Index: index, Label: label})
}

// SetOutputLabel is set_output_label(index, label).
func (s *Session) SetOutputLabel(ctx context.Context, index int, label string) error {
	return s.Send(ctx, OutputLabel{Index: index, Label: label})
}

// Ping is ping().
func (s *Session) Ping(ctx context.Context) error { return s.Send(ctx, Ping{}) }

// State returns a copy of what the session holds of the hub; once it
// has ended, what it held then.
func (s *Session) State() State {
	var st State
	if s.do(func() { st = s.state.clone() }) != nil {
		s.errMu.Lock()
		defer s.errMu.Unlock()
		return s.final.clone()
	}
	return st
}

// Snapshot is state(): the session's State's Snapshot.
func (s *Session) Snapshot() (Snapshot, error) { return s.State().Snapshot() }

// Subscribe returns a channel of the blocks the hub pushes from now on,
// and a function to stop them. The channel closes when the session ends
// (after a ChangeDisconnected) or the subscriber falls 1024 behind.
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

// Ready returns a channel closed once the session has read the preamble,
// which it has when Dial returns. After the session ends it returns one
// that never closes.
func (s *Session) Ready() <-chan struct{} {
	if !s.Connected() {
		return make(chan struct{})
	}
	return s.ready
}

// Connected reports whether the session is open: from Dial until Close
// or the connection failing.
func (s *Session) Connected() bool { return s.conns.Load() }

// Addr is the hub's address, host:port.
func (s *Session) Addr() string { return s.addr }

// Close ends the session, as close() does: nothing is sent. Closing an
// ended session does nothing.
func (s *Session) Close() error {
	s.do(func() {
		s.errMu.Lock()
		if s.err == nil {
			s.err = errClosedByUs
		}
		s.errMu.Unlock()
		s.closing = true
	})
	<-s.done
	return nil
}
