// SPDX-License-Identifier: MIT

// Package engine is bmdctl's background process: it holds one session per
// switcher and runs the CLI's commands through them, so a command doesn't
// pay for a connect and a full state dump each time, and the switcher's
// live state (input names, frame rate) is at hand to read a command line.
//
// The CLI talks to it over a Unix socket (a named pipe on Windows) with
// one JSON request per connection and JSON replies (protocol.go). A
// session closes after IdleTimeout without use, or PinIdleTimeout if it
// is pinned. docs/PORTING.md, "Engine", records the decisions.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/hyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/stillcache"
	"github.com/lucas-romanenko/bmdctl/internal/table"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
	"github.com/lucas-romanenko/bmdctl/videohub"
)

// IdleTimeout is how long an unpinned session lives without a request.
const IdleTimeout = 10 * time.Minute

// PinIdleTimeout is how long a pinned session lives without a request
// (#7: one was held for two days, a second client nobody knew of).
const PinIdleTimeout = time.Hour

// Clock is the time the engine keeps its sessions by: when each was
// opened and last used, and when it closes. Tests give a fake one
// (internal/fakeclock).
type Clock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has passed, unless
	// stop is called first; stop reports whether it stopped the call.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// realClock is the machine's.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

// Config sets up an Engine.
type Config struct {
	// Registry holds the commands the engine runs (NewRegistry).
	Registry *table.Registry
	// Dial opens a session; nil means atem.Dialer with Logger. Tests
	// point it at internal/fakeatem.
	Dial func(ctx context.Context, addr string) (*atem.Session, error)
	// DialHyperDeck opens a HyperDeck session; nil means
	// hyperdeck.Dialer with Logger. Tests point it at
	// internal/fakehyperdeck.
	DialHyperDeck func(ctx context.Context, addr string) (*hyperdeck.Session, error)
	// DialVideohub opens a Videohub session; nil means videohub.Dialer
	// with Logger. Tests point it at internal/fakevideohub.
	DialVideohub func(ctx context.Context, addr string) (*videohub.Session, error)
	// Ultimatte reaches Ultimatte units, which keep no session: nil means
	// ultimatte's defaults. Tests point its DialContext at
	// internal/fakeultimatte.
	Ultimatte *ultimatte.Client
	// ProbeATEM and ProbeDial are the device probe's ways of asking an
	// address (probe.go): an ATEM's handshake, and a TCP connection for
	// the others. Nil means atem.Probe and a net.Dialer's; tests point
	// them at the fakes.
	ProbeATEM func(ctx context.Context, addr string) error
	ProbeDial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Logger receives the engine's log; nil discards it.
	Logger *slog.Logger
	// IdleTimeout overrides IdleTimeout when positive.
	IdleTimeout time.Duration
	// PinIdle overrides PinIdleTimeout when positive; negative, pinned
	// sessions never close (BMDCTL_PIN_IDLE=0).
	PinIdle time.Duration
	// Clock is the sessions' time; nil means the machine's.
	Clock Clock
	// Version, Socket and LogPath are reported by the status request.
	Version, Socket, LogPath string
	// Commit is the git commit the engine was built from, short, for
	// people; reported by status. BuildID is what tells builds apart.
	Commit string
	// BuildID is the build the engine runs, reported by status.
	BuildID string
	// Stills keeps the media pool's stills by hash, so a profile save
	// writes a still it has seen without downloading it again
	// (internal/stillcache); nil keeps none.
	Stills *stillcache.Cache
	// Background: a command started the engine (engine --background), so
	// a command can start it again. It stops for a device request from
	// another build than BuildID, answering CodeStale, for that command
	// to start an engine of its own build. An engine run by hand, under
	// systemd or in a container, answers every build.
	Background bool
}

// Engine runs requests against switcher sessions it keeps open.
type Engine struct {
	cfg     Config
	log     *slog.Logger
	idle    time.Duration // an unpinned session's idle timeout
	pinIdle time.Duration // a pinned one's; 0, never
	clock   Clock
	started time.Time
	ops     map[string]operation

	ctx      context.Context // ends when the engine stops
	cancel   context.CancelFunc
	done     chan struct{} // closed by Stop
	wg       sync.WaitGroup
	stopOnce sync.Once

	mu       sync.Mutex
	hosts    map[string]*host
	undo     map[string][]undoEntry // per session key: what undo puts back
	listener Listener
	stopping bool
}

// devSession is a session to a device: an *atem.Session, a
// *hyperdeck.Session or a *videohub.Session.
type devSession interface {
	Connected() bool
	Ready() <-chan struct{}
	Close() error
}

// host is one device address and its session.
type host struct {
	key    string // canonical "host:port"
	device string // atem, hyperdeck, videohub

	// Guarded by Engine.mu.
	sess    devSession
	dialing chan struct{} // closed when the dial in progress ends
	dialErr error
	pinned  bool
	busy    int         // requests using the session now
	idle    func() bool // stops the idle timeout's call, if one waits
	opened  time.Time
	used    time.Time
}

// New returns an engine; Serve starts it answering.
func New(cfg Config) *Engine {
	e := &Engine{cfg: cfg, log: cfg.Logger, idle: cfg.IdleTimeout, pinIdle: cfg.PinIdle, clock: cfg.Clock, started: time.Now(),
		hosts: map[string]*host{}, undo: map[string][]undoEntry{}, done: make(chan struct{}), ops: atemOperations()}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	if e.idle <= 0 {
		e.idle = IdleTimeout
	}
	switch {
	case e.pinIdle == 0:
		e.pinIdle = PinIdleTimeout
	case e.pinIdle < 0:
		e.pinIdle = 0
	}
	if e.clock == nil {
		e.clock = realClock{}
	}
	if e.cfg.Dial == nil {
		d := atem.Dialer{Logger: e.log}
		e.cfg.Dial = d.Dial
	}
	if e.cfg.DialHyperDeck == nil {
		d := hyperdeck.Dialer{Logger: e.log}
		e.cfg.DialHyperDeck = d.Dial
	}
	if e.cfg.DialVideohub == nil {
		d := videohub.Dialer{Logger: e.log}
		e.cfg.DialVideohub = d.Dial
	}
	if e.cfg.Ultimatte == nil {
		e.cfg.Ultimatte = &ultimatte.Client{}
	}
	if e.cfg.ProbeATEM == nil {
		e.cfg.ProbeATEM = atem.Probe
	}
	if e.cfg.ProbeDial == nil {
		var d net.Dialer
		e.cfg.ProbeDial = d.DialContext
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	return e
}

// NewRegistry returns the command table the engine and the CLI share:
// every device's commands and the engine's operations.
func NewRegistry() (*table.Registry, error) {
	r := table.New()
	if err := atem.RegisterCommands(r); err != nil {
		return nil, err
	}
	if err := hyperdeck.RegisterCommands(r); err != nil {
		return nil, err
	}
	if err := videohub.RegisterCommands(r); err != nil {
		return nil, err
	}
	if err := ultimatte.RegisterCommands(r); err != nil {
		return nil, err
	}
	if err := RegisterOperations(r); err != nil {
		return nil, err
	}
	return r, nil
}

// Done is closed once Stop has been called.
func (e *Engine) Done() <-chan struct{} { return e.done }

// Serve answers connections from l until Stop. It returns nil after Stop.
func (e *Engine) Serve(l Listener) error {
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		l.Close()
		return nil
	}
	e.listener = l
	e.mu.Unlock()
	pinIdle := "never"
	if e.pinIdle > 0 {
		pinIdle = e.pinIdle.String()
	}
	e.log.Info("engine listening", "socket", l.Addr(), "pid", os.Getpid(), "version", e.cfg.Version, "build", e.cfg.BuildID,
		"idle_timeout", e.idle, "pin_idle", pinIdle)
	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-e.done:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			e.log.Error("accept", "err", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// Add under the lock, and not once stopping: Stop sets stopping
		// under it before it waits, so Add can't race the Wait.
		e.mu.Lock()
		if e.stopping {
			e.mu.Unlock()
			c.Close()
			return nil
		}
		e.wg.Add(1)
		e.mu.Unlock()
		go func() {
			defer e.wg.Done()
			e.serveConn(c)
		}()
	}
}

// Stop stops listening, ends running requests and closes every session,
// saying goodbye to each switcher. It waits up to five seconds for
// requests to finish. Every call returns once the first has finished.
func (e *Engine) Stop() { e.stopOnce.Do(e.stop) }

func (e *Engine) stop() {
	e.mu.Lock()
	e.stopping = true
	close(e.done)
	l := e.listener
	var sessions []devSession
	for k, h := range e.hosts {
		if h.idle != nil {
			h.idle()
		}
		if h.sess != nil {
			sessions = append(sessions, h.sess)
			h.sess = nil
		}
		delete(e.hosts, k)
	}
	e.mu.Unlock()
	e.log.Info("engine stopping", "sessions", len(sessions))
	if l != nil {
		l.Close()
	}
	e.cancel()
	for _, s := range sessions {
		s.Close()
	}
	finished := make(chan struct{})
	go func() { e.wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
	}
}

// serveConn answers the one request a connection carries.
func (e *Engine) serveConn(c io.ReadWriteCloser) {
	defer c.Close()
	br := bufio.NewReaderSize(c, 64<<10)
	w := &connSink{w: c}
	line, err := readLine(br)
	if err != nil {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		w.response(Response{Code: CodeUsage, Error: fmt.Sprintf("engine: bad request: %v", err)})
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	go func() {
		// The caller sends nothing more: a read ending means it hung up.
		io.Copy(io.Discard, br)
		cancel()
	}()
	resp := e.Handle(ctx, req, w)
	if _, end := resp.Result.(streamEnd); !end {
		w.response(resp)
	}
	if req.Device == "engine" && req.Path == "stop" && resp.OK {
		go e.Stop()
	}
	if resp.Code == CodeStale {
		e.log.Info("a client of another build: stopping", "build", e.cfg.BuildID, "client", req.Build)
		go e.Stop()
	}
}

// sink takes what a request produces besides its response: files, notes,
// and a streaming command's results.
type sink interface {
	file(FileFrame) error
	note(Note) error
	response(Response) error
}

type connSink struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *connSink) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.w.Write(append(b, '\n'))
	return err
}

func (s *connSink) file(f FileFrame) error    { return s.write(frame{File: &f}) }
func (s *connSink) note(n Note) error         { return s.write(frame{Note: &n}) }
func (s *connSink) response(r Response) error { return s.write(r) }

// Handle runs one request and returns its response. Files and stream
// results go to out first. ctx ending (the caller hanging up) abandons
// the request.
func (e *Engine) Handle(ctx context.Context, req Request, out sink) Response {
	start := time.Now()
	result, confirmed, err := e.handle(ctx, req, out, start)
	took := time.Since(start).Milliseconds()
	lvl := slog.LevelInfo
	attrs := []any{"device", req.Device, "host", req.Host, "path", req.Path, "took_ms", took}
	if err != nil {
		lvl = slog.LevelWarn
		attrs = append(attrs, "code", CodeOf(err), "err", err)
	}
	e.log.Log(context.Background(), lvl, "request", attrs...)
	if err != nil {
		return Response{Code: CodeOf(err), Error: err.Error()}
	}
	return Response{OK: true, Confirmed: confirmed, Result: result, TookMS: took}
}

func (e *Engine) handle(ctx context.Context, req Request, out sink, start time.Time) (any, bool, error) {
	if req.V != Version {
		return nil, false, codeErr(CodeEngine, "the engine speaks protocol %d, this request %d: stop the engine (bmdctl engine stop) and retry", Version, req.V)
	}
	if req.Device == "engine" {
		return e.engineRequest(ctx, req)
	}
	if e.cfg.Background && req.Build != "" && e.cfg.BuildID != "" && req.Build != e.cfg.BuildID {
		return nil, false, codeErr(CodeStale, "the engine runs another build of bmdctl and stops, for this one to start its own")
	}
	if !slices.Contains([]string{"atem", "hyperdeck", "videohub", "ultimatte"}, req.Device) {
		return nil, false, codeErr(CodeUsage, "no commands for device %q", req.Device)
	}
	entry := e.cfg.Registry.Find(req.Device, req.Path)
	if entry == nil {
		return nil, false, codeErr(CodeUsage, "%s has no command %q", req.Device, req.Path)
	}
	c := &call{ctx: ctx, e: e, req: req, entry: entry, out: out, start: start}
	if entry.Destructive && !req.Yes && !req.Ask {
		return nil, false, codeErr(CodeDestructive, "%s %s is destructive: add --yes to run it", req.Device, req.Path)
	}
	if entry.Local {
		return c.runLocal()
	}
	if req.Host == "" {
		return nil, false, codeErr(CodeUsage, "no switcher named: give --host")
	}
	switch req.Device {
	case "hyperdeck":
		return c.runDeck() // hyperdeck.go
	case "videohub":
		return c.runHub() // videohub.go
	case "ultimatte":
		return c.runUlt() // ultimatte.go
	}
	o, isOp := e.ops[entry.Path]
	if entry.Operation && !isOp {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", req.Device, req.Path)
	}
	if o.noDial {
		var err error
		if c.args, err = entry.Parse(req.Params, atem.Resolver{FPS: req.FPS}); err != nil {
			return nil, false, usageErr(err)
		}
		return o.run(c)
	}
	connecting := func(key string) { c.note(Note{Connecting: key}) }
	h, sess, idle, release, err := e.acquire(ctx, req.Host, connecting)
	if err != nil {
		return nil, false, err
	}
	defer release()
	c.host, c.sess = h, sess
	if err := waitReady(ctx, sess, func() { connecting(h.key) }); err != nil {
		return nil, false, err
	}
	c.state = sess.State()
	c.note(Note{Switcher: e.switcherNote(h, c.state, idle)})
	c.noteOnAir() // once, before the command: the header shows what it found
	c.res = Sources(atem.Resolver{State: c.state, FPS: req.FPS}, c)
	c.args, err = entry.Parse(req.Params, c.res)
	if err != nil {
		if c.noInput != nil {
			c.note(Note{NoInput: c.noInput})
		}
		return nil, false, usageErr(err)
	}
	if err := c.forMixer(e.cfg.Registry); err != nil {
		return nil, false, err
	}
	if err := c.checkIndices(); err != nil {
		return nil, false, err
	}
	if check := wireChecks[entry.Path]; check != nil {
		if err := check(c); err != nil {
			return nil, false, err
		}
	}
	if entry.Destructive && !req.Yes && !c.alreadyHeld() {
		return nil, false, c.refuseEntry() // a person may be asked
	}
	if !entry.Operation {
		return c.sendEntry()
	}
	return o.run(c)
}

// CodeEngine: the engine can't serve this CLI (a protocol mismatch).
const CodeEngine = "engine"

func usageErr(err error) error {
	return &Error{CodeUsage, errors.New(strings.TrimPrefix(err.Error(), "usage: "))}
}

// hostKey is addr as "host:port", lower-cased, port 9910 by default.
func hostKey(addr string) (string, error) { return deviceKey("atem", addr) }

// deviceKey is addr as "host:port", lower-cased, the device's port by
// default: 9910 for an ATEM, 9993 for a HyperDeck, 9990 for a Videohub,
// 9998 for an Ultimatte (its text channel).
func deviceKey(device, addr string) (string, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		h, p = strings.Trim(addr, "[]"), fmt.Sprint(atem.DefaultPort)
		switch device {
		case "hyperdeck":
			p = fmt.Sprint(hyperdeck.DefaultPort)
		case "videohub":
			p = fmt.Sprint(videohub.DefaultPort)
		case "ultimatte":
			p = fmt.Sprint(ultimatte.ControlPort)
		}
	}
	if h == "" {
		return "", codeErr(CodeUsage, "no host in %q", addr)
	}
	return net.JoinHostPort(strings.ToLower(h), p), nil
}

// acquire returns the ATEM session for addr: acquireSession's for an
// ATEM.
func (e *Engine) acquire(ctx context.Context, addr string, connecting func(key string)) (*host, *atem.Session, time.Duration, func(), error) {
	h, s, idle, release, err := e.acquireSession(ctx, "atem", addr, connecting)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	return h, s.(*atem.Session), idle, release, nil
}

// acquireSession returns the device's session for addr, dialling it if
// there is none or the one there has failed, and how long it had gone
// unused. Concurrent requests share one dial; connecting is called with
// the session's key when this request waits for one. release must be
// called when the request is done with the session.
func (e *Engine) acquireSession(ctx context.Context, device, addr string, connecting func(key string)) (*host, devSession, time.Duration, func(), error) {
	key, err := deviceKey(device, addr)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return nil, nil, 0, nil, codeErr(CodeUnreachable, "the engine is stopping")
	}
	h := e.hosts[key]
	if h != nil && h.device != device && h.busy == 0 {
		// The address was another device's: start again.
		if h.sess != nil {
			go h.sess.Close()
		}
		if h.idle != nil {
			h.idle()
		}
		h = nil
	}
	if h == nil {
		h = &host{key: key, device: device}
		e.hosts[key] = h
	} else if h.device != device {
		e.mu.Unlock()
		return nil, nil, 0, nil, codeErr(CodeUsage, "%s is in use as %s %s", key, DeviceArticle(h.device), DeviceNames[h.device])
	}
	var idle time.Duration
	if h.busy == 0 && !h.used.IsZero() {
		idle = e.clock.Now().Sub(h.used)
	}
	h.busy++
	if h.idle != nil {
		h.idle()
		h.idle = nil
	}
	release := func() { e.release(h) }
	for said := false; ; said = true {
		if h.sess != nil && h.sess.Connected() {
			s := h.sess
			e.mu.Unlock()
			return h, s, idle, release, nil
		}
		if h.sess != nil { // failed since
			go h.sess.Close()
			h.sess = nil
		}
		if h.dialing == nil {
			h.dialing = make(chan struct{})
			go e.dial(h)
		}
		ch := h.dialing
		e.mu.Unlock()
		if !said {
			connecting(key)
		}
		select {
		case <-ch:
		case <-ctx.Done():
			release()
			return nil, nil, 0, nil, codeErr(CodeUnreachable, "%s: %v", key, ctx.Err())
		}
		e.mu.Lock()
		if h.sess == nil {
			err := h.dialErr
			e.mu.Unlock()
			release()
			return nil, nil, 0, nil, codeErr(CodeUnreachable, "%s", dialMessage(key, err))
		}
	}
}

func dialMessage(key string, err error) string {
	switch {
	case err == nil:
		return key + ": no session"
	case errors.Is(err, context.DeadlineExceeded) && strings.HasSuffix(key, fmt.Sprintf(":%d", hyperdeck.DefaultPort)):
		return fmt.Sprintf("no HyperDeck answered at %s within %v", key, hyperdeck.ConnectTimeout)
	case errors.Is(err, context.DeadlineExceeded) && strings.HasSuffix(key, fmt.Sprintf(":%d", videohub.DefaultPort)):
		return fmt.Sprintf("no Videohub answered at %s within %v", key, videohub.ConnectTimeout)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("no switcher answered at %s within %v", key, atem.ConnectTimeout)
	}
	return err.Error()
}

// dial opens h's session. It runs on the engine's context, not a
// request's, since every request waiting for it shares it.
func (e *Engine) dial(h *host) {
	var s devSession
	var err error
	switch h.device {
	case "hyperdeck":
		ctx, cancel := context.WithTimeout(e.ctx, atem.ConnectTimeout)
		var d *hyperdeck.Session
		d, err = e.cfg.DialHyperDeck(ctx, h.key)
		cancel()
		if err == nil {
			s = d
		}
	case "videohub":
		// The connect's retry and the preamble's wait both fit: 3 s, 0.3
		// s and 3 s.
		ctx, cancel := context.WithTimeout(e.ctx, 2*videohub.ConnectTimeout+videohub.ReadTimeout)
		var v *videohub.Session
		v, err = e.cfg.DialVideohub(ctx, h.key)
		cancel()
		if err == nil {
			s = v
		}
	default:
		ctx, cancel := context.WithTimeout(e.ctx, atem.ConnectTimeout)
		var a *atem.Session
		a, err = e.cfg.Dial(ctx, h.key)
		cancel()
		if err == nil {
			s = a
		}
	}
	e.mu.Lock()
	ch := h.dialing
	h.dialing, h.dialErr = nil, err
	if err == nil {
		if e.stopping {
			go s.Close()
		} else {
			h.sess, h.opened = s, e.clock.Now()
		}
	}
	close(ch)
	e.mu.Unlock()
	if err != nil {
		e.log.Warn("connect failed", "host", h.key, "device", h.device, "err", err)
		return
	}
	name, product, mode := describe(s)
	e.log.Info("connected", "host", h.key, "device", h.device, "name", name, "product", product, "video_mode", mode)
}

// describe is a session's device as people read it: the name it gives
// itself, its model, and its video mode (a deck's video format).
func describe(s devSession) (name, product, mode string) {
	switch x := s.(type) {
	case *atem.Session:
		st := x.State()
		return SwitcherName(st), st.ProductName(""), videoModeLabel(st)
	case *hyperdeck.Session:
		st := x.State()
		return st.Name(), st.Model(), st.Transport["video format"]
	case *videohub.Session:
		st := x.State()
		return st.Name(), st.Model(), ""
	}
	return "", "", ""
}

func videoModeLabel(st atem.State) string {
	if m := st.VideoMode.Mode; m != nil {
		return m.Label()
	}
	return ""
}

// release ends one request's use of h; the last one out starts the idle
// clock: IdleTimeout, or PinIdleTimeout if h is pinned.
func (e *Engine) release(h *host) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h.busy--
	h.used = e.clock.Now()
	e.scheduleIdle(h)
}

// closesAfter is how long a session goes unused before it closes:
// pinned or not. 0 is never.
func (e *Engine) closesAfter(pinned bool) time.Duration {
	if pinned {
		return e.pinIdle
	}
	return e.idle
}

// scheduleIdle starts h's idle timeout, for what is left of it since h
// was last used, unless a request is using h. With e.mu held.
func (e *Engine) scheduleIdle(h *host) {
	if h.busy > 0 || e.stopping {
		return
	}
	if h.idle != nil {
		h.idle()
		h.idle = nil
	}
	d := e.closesAfter(h.pinned)
	if d <= 0 {
		return
	}
	left := max(0, d-e.clock.Now().Sub(h.used))
	h.idle = e.clock.AfterFunc(left, func() { e.expire(h) })
}

// expire closes h's session once it has gone unused for its idle
// timeout, saying so in the log with how old it was; if it hasn't (it
// was pinned since), it waits the rest.
func (e *Engine) expire(h *host) {
	e.mu.Lock()
	d := e.closesAfter(h.pinned)
	idle := e.clock.Now().Sub(h.used)
	if h.busy > 0 || d <= 0 || e.hosts[h.key] != h || e.stopping {
		e.mu.Unlock()
		return
	}
	if idle < d {
		e.scheduleIdle(h)
		e.mu.Unlock()
		return
	}
	delete(e.hosts, h.key)
	s, pinned, age := h.sess, h.pinned, e.clock.Now().Sub(h.opened)
	h.sess = nil
	e.mu.Unlock()
	if s == nil {
		return
	}
	attrs := []any{"host", h.key, "device", h.device, "idle", idle.Round(time.Second), "age", age.Round(time.Second)}
	if pinned {
		e.log.Info("closing pinned session: idle past the pin timeout", append(attrs, "pin_idle", d)...)
	} else {
		e.log.Info("closing idle session", attrs...)
	}
	s.Close()
}

// disconnect closes addr's ATEM session, pinned or not. Requests using
// it end with ErrClosed.
func (e *Engine) disconnect(addr string) (bool, error) {
	key, err := hostKey(addr)
	if err != nil {
		return false, err
	}
	return e.disconnectKey(key)
}

// disconnectKey closes the session at key, pinned or not.
func (e *Engine) disconnectKey(key string) (bool, error) {
	e.mu.Lock()
	h := e.hosts[key]
	if h == nil {
		e.mu.Unlock()
		return false, nil
	}
	delete(e.hosts, key)
	if h.idle != nil {
		h.idle()
		h.idle = nil
	}
	s := h.sess
	h.sess, h.pinned = nil, false
	e.mu.Unlock()
	if s == nil {
		return false, nil
	}
	e.log.Info("disconnecting", "host", key)
	s.Close()
	return true, nil
}

// pin keeps h's session open until disconnect, or until it has gone
// PinIdleTimeout unused; unpinned, IdleTimeout. A change is logged.
func (e *Engine) pin(h *host, on bool) {
	e.mu.Lock()
	changed := h.pinned != on
	h.pinned = on
	e.scheduleIdle(h)
	after := e.closesAfter(on)
	e.mu.Unlock()
	if !changed {
		return
	}
	closes := "never"
	if after > 0 {
		closes = after.String()
	}
	if on {
		e.log.Info("pinned session", "host", h.key, "device", h.device, "closes_after_idle", closes)
	} else {
		e.log.Info("unpinned session", "host", h.key, "device", h.device, "closes_after_idle", closes)
	}
}

// waitReady waits for the session's state to be complete: at once,
// unless it is re-handshaking after a silence, which it reports through
// waiting first.
func waitReady(ctx context.Context, s *atem.Session, waiting func()) error {
	select {
	case <-s.Ready():
		return nil
	default:
	}
	waiting()
	deadline := time.NewTimer(atem.ConnectTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.Ready():
			return nil
		case <-ctx.Done():
			return codeErr(CodeUnreachable, "waiting for the switcher: %v", ctx.Err())
		case <-deadline.C:
			return codeErr(CodeUnreachable, "the switcher stopped answering and has not come back within %v", atem.ConnectTimeout)
		case <-tick.C:
			if !s.Connected() {
				return codeErr(CodeUnreachable, "the session failed")
			}
		}
	}
}

// Status is what the engine reports about itself.
type Status struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"` // the git commit, short, for people
	Build    string `json:"build,omitempty"`
	Protocol int    `json:"protocol"`
	// Background: a command started it, and one of another build
	// restarts it.
	Background bool `json:"background"`
	// PinIdleS is how long a pinned session may go unused before it
	// closes, in seconds; 0, never (as an engine from before #7).
	PinIdleS int             `json:"pin_idle_s,omitempty"`
	PID      int             `json:"pid"`
	Started  time.Time       `json:"started"`
	Socket   string          `json:"socket,omitempty"`
	Log      string          `json:"log,omitempty"`
	Sessions []SessionStatus `json:"sessions"`
}

// SessionStatus is one device's session.
type SessionStatus struct {
	Host      string     `json:"host"`
	Device    string     `json:"device,omitempty"` // atem, hyperdeck, videohub
	Connected bool       `json:"connected"`
	Ready     bool       `json:"ready"`
	Pinned    bool       `json:"pinned"`
	Busy      int        `json:"busy"`
	Since     *time.Time `json:"since,omitempty"`
	// AgeS is how long the session has been open, in seconds, by the
	// engine's clock (Since by another machine's may be off).
	AgeS int `json:"age_s,omitempty"`
	// IdleS is how long it has gone without a request, in seconds; 0
	// while one uses it (Busy), a watch or a subscription included.
	IdleS int `json:"idle_s"`
	// IdleCloseS is the seconds until an idle session closes, pinned
	// or not; 0 while in use, or when it never closes.
	IdleCloseS int `json:"idle_close_s,omitempty"`
	// Stale: it has gone unused past its idle timeout, and closes now.
	Stale     bool   `json:"stale,omitempty"`
	Product   string `json:"product,omitempty"`
	VideoMode string `json:"video_mode,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (e *Engine) status() Status {
	st := Status{Version: e.cfg.Version, Commit: e.cfg.Commit, Build: e.cfg.BuildID, Protocol: Version, Background: e.cfg.Background,
		PinIdleS: int(e.pinIdle.Seconds()), PID: os.Getpid(), Started: e.started, Socket: e.cfg.Socket, Log: e.cfg.LogPath, Sessions: []SessionStatus{}}
	e.mu.Lock()
	type snap struct {
		ss   SessionStatus
		sess devSession
	}
	var snaps []snap
	now := e.clock.Now()
	for _, h := range e.hosts {
		ss := SessionStatus{Host: h.key, Device: h.device, Pinned: h.pinned, Busy: h.busy}
		if h.dialErr != nil && h.sess == nil {
			ss.Error = h.dialErr.Error()
		}
		if h.sess != nil {
			t := h.opened
			ss.Since, ss.AgeS = &t, int(now.Sub(h.opened).Seconds())
		}
		if h.busy == 0 && !h.used.IsZero() {
			idle := now.Sub(h.used)
			ss.IdleS = int(idle.Seconds())
			if d := e.closesAfter(h.pinned); d > 0 {
				ss.IdleCloseS, ss.Stale = max(0, int((d-idle).Seconds())), idle >= d
			}
		}
		snaps = append(snaps, snap{ss, h.sess})
	}
	e.mu.Unlock()
	for _, s := range snaps {
		if s.sess != nil {
			s.ss.Connected = s.sess.Connected()
			select {
			case <-s.sess.Ready():
				s.ss.Ready = true
			default:
			}
			_, s.ss.Product, s.ss.VideoMode = describe(s.sess)
		}
		st.Sessions = append(st.Sessions, s.ss)
	}
	slices.SortFunc(st.Sessions, func(a, b SessionStatus) int { return strings.Compare(a.Host, b.Host) })
	return st
}

// SwitcherName is what a switcher calls itself: the name set in ATEM
// Setup (WhoI), else its product name (_pin); bmdwire v1.3.0's state.py
// reports device_name(mx) or product_name(mx) as atem_name.
func SwitcherName(st atem.State) string {
	if n := st.DeviceName(""); n != "" {
		return n
	}
	return st.ProductName("")
}

// switcherNote is the Switcher note for a request using h, whose session
// holds st and had gone unused for idle.
func (e *Engine) switcherNote(h *host, st atem.State, idle time.Duration) *Switcher {
	e.mu.Lock()
	pinned := h.pinned
	e.mu.Unlock()
	sw := &Switcher{Host: h.key, Name: SwitcherName(st), Product: st.ProductName(""), VideoMode: videoModeLabel(st),
		Ready: true, Pinned: pinned, IdleS: int(idle.Seconds()), DropS: int(e.closesAfter(pinned).Seconds())}
	for id := range st.Inputs {
		sw.NameWidth = max(sw.NameWidth, utf8.RuneCountInString(sourceName(st, int(id))))
	}
	return sw
}

// Overview is the engine and each switcher it has a session to: what
// bmdctl prints when run without arguments.
type Overview struct {
	Engine    Status     `json:"engine"`
	Switchers []Switcher `json:"switchers"`
}

// switchers describes every session, as the overview shows them.
func (e *Engine) switchers() []Switcher {
	type snap struct {
		sw   Switcher
		sess devSession
	}
	var snaps []snap
	e.mu.Lock()
	now := e.clock.Now()
	for _, h := range e.hosts {
		sw := Switcher{Host: h.key, Device: h.device, Pinned: h.pinned}
		if h.dialErr != nil && h.sess == nil {
			sw.Error = h.dialErr.Error()
		}
		if h.busy == 0 && !h.used.IsZero() {
			sw.IdleS = int(now.Sub(h.used).Seconds())
			if d := e.closesAfter(h.pinned); d > 0 {
				sw.DropS = max(0, int((d - now.Sub(h.used)).Seconds()))
			}
		}
		snaps = append(snaps, snap{sw, h.sess})
	}
	e.mu.Unlock()
	out := []Switcher{}
	for _, s := range snaps {
		if s.sess != nil {
			select {
			case <-s.sess.Ready():
				s.sw.Ready = true
			default:
			}
			s.sw.Name, s.sw.Product, s.sw.VideoMode = describe(s.sess)
		}
		out = append(out, s.sw)
	}
	slices.SortFunc(out, func(a, b Switcher) int { return strings.Compare(a.Host, b.Host) })
	return out
}

// session returns addr's host and session, if the engine has one, without
// dialling.
func (e *Engine) session(addr string) (*host, *atem.Session) {
	key, err := hostKey(addr)
	if err != nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	h := e.hosts[key]
	if h == nil || h.sess == nil {
		return nil, nil
	}
	s, ok := h.sess.(*atem.Session)
	if !ok {
		return nil, nil
	}
	return h, s
}

func (e *Engine) engineRequest(ctx context.Context, req Request) (any, bool, error) {
	switch req.Path {
	case "probe":
		r, err := e.probe(ctx, req.Host)
		return r, false, err
	case "overview":
		return Overview{Engine: e.status(), Switchers: e.switchers()}, false, nil
	case "status", "ping":
		return e.status(), false, nil
	case "stop":
		return map[string]bool{"stopping": true}, false, nil
	}
	return nil, false, codeErr(CodeUsage, "the engine has no request %q", req.Path)
}
