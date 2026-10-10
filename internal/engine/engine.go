// SPDX-License-Identifier: MIT

// Package engine is bmdctl's background process: it holds one session per
// switcher and runs the CLI's commands through them, so a command doesn't
// pay for a connect and a full state dump each time, and the switcher's
// live state (input names, frame rate) is at hand to read a command line.
//
// The CLI talks to it over a Unix socket (a named pipe on Windows) with
// one JSON request per connection and JSON replies (protocol.go). A
// session idles out after IdleTimeout without use unless it is pinned.
// docs/PORTING.md, "Engine", records the decisions.
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

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// IdleTimeout is how long an unpinned session lives without a request.
const IdleTimeout = 10 * time.Minute

// Config sets up an Engine.
type Config struct {
	// Registry holds the commands the engine runs (NewRegistry).
	Registry *table.Registry
	// Dial opens a session; nil means atem.Dialer with Logger. Tests
	// point it at internal/fakeatem.
	Dial func(ctx context.Context, addr string) (*atem.Session, error)
	// Logger receives the engine's log; nil discards it.
	Logger *slog.Logger
	// IdleTimeout overrides IdleTimeout when positive.
	IdleTimeout time.Duration
	// Version, Socket and LogPath are reported by the status request.
	Version, Socket, LogPath string
}

// Engine runs requests against switcher sessions it keeps open.
type Engine struct {
	cfg     Config
	log     *slog.Logger
	idle    time.Duration
	started time.Time
	ops     map[string]operation

	ctx      context.Context // ends when the engine stops
	cancel   context.CancelFunc
	done     chan struct{} // closed by Stop
	wg       sync.WaitGroup
	stopOnce sync.Once

	mu       sync.Mutex
	hosts    map[string]*host
	listener Listener
	stopping bool
}

// host is one switcher address and its session.
type host struct {
	key string // canonical "host:port"

	// Guarded by Engine.mu.
	sess    *atem.Session
	dialing chan struct{} // closed when the dial in progress ends
	dialErr error
	pinned  bool
	busy    int // requests using the session now
	idle    *time.Timer
	opened  time.Time
	used    time.Time
}

// New returns an engine; Serve starts it answering.
func New(cfg Config) *Engine {
	e := &Engine{cfg: cfg, log: cfg.Logger, idle: cfg.IdleTimeout, started: time.Now(),
		hosts: map[string]*host{}, done: make(chan struct{}), ops: atemOperations()}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	if e.idle <= 0 {
		e.idle = IdleTimeout
	}
	if e.cfg.Dial == nil {
		d := atem.Dialer{Logger: e.log}
		e.cfg.Dial = d.Dial
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
	e.log.Info("engine listening", "socket", l.Addr(), "pid", os.Getpid(), "version", e.cfg.Version)
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
		e.wg.Add(1)
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
	var sessions []*atem.Session
	for k, h := range e.hosts {
		if h.idle != nil {
			h.idle.Stop()
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
}

// sink takes what a request produces besides its response: files, and
// a streaming command's results.
type sink interface {
	file(FileFrame) error
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
		return e.engineRequest(req)
	}
	if req.Device != "atem" {
		return nil, false, codeErr(CodeUsage, "no commands for device %q", req.Device)
	}
	entry := e.cfg.Registry.Find(req.Device, req.Path)
	if entry == nil {
		return nil, false, codeErr(CodeUsage, "%s has no command %q", req.Device, req.Path)
	}
	c := &call{ctx: ctx, e: e, req: req, entry: entry, out: out, start: start}
	if entry.Destructive && !req.Yes {
		return nil, false, codeErr(CodeDestructive, "%s %s is destructive: add --yes to run it", req.Device, req.Path)
	}
	if entry.Local {
		return c.runLocal()
	}
	if req.Host == "" {
		return nil, false, codeErr(CodeUsage, "no switcher named: give --host")
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
	h, sess, release, err := e.acquire(ctx, req.Host)
	if err != nil {
		return nil, false, err
	}
	defer release()
	c.host, c.sess = h, sess
	if err := waitReady(ctx, sess); err != nil {
		return nil, false, err
	}
	c.state = sess.State()
	c.res = atem.Resolver{State: c.state, FPS: req.FPS}
	c.args, err = entry.Parse(req.Params, c.res)
	if err != nil {
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
func hostKey(addr string) (string, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		h, p = strings.Trim(addr, "[]"), fmt.Sprint(atem.DefaultPort)
	}
	if h == "" {
		return "", codeErr(CodeUsage, "no host in %q", addr)
	}
	return net.JoinHostPort(strings.ToLower(h), p), nil
}

// acquire returns the session for addr, dialling it if there is none or
// the one there has failed. Concurrent requests share one dial. release
// must be called when the request is done with the session.
func (e *Engine) acquire(ctx context.Context, addr string) (*host, *atem.Session, func(), error) {
	key, err := hostKey(addr)
	if err != nil {
		return nil, nil, nil, err
	}
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return nil, nil, nil, codeErr(CodeUnreachable, "the engine is stopping")
	}
	h := e.hosts[key]
	if h == nil {
		h = &host{key: key}
		e.hosts[key] = h
	}
	h.busy++
	if h.idle != nil {
		h.idle.Stop()
		h.idle = nil
	}
	release := func() { e.release(h) }
	for {
		if h.sess != nil && h.sess.Connected() {
			s := h.sess
			e.mu.Unlock()
			return h, s, release, nil
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
		select {
		case <-ch:
		case <-ctx.Done():
			release()
			return nil, nil, nil, codeErr(CodeUnreachable, "%s: %v", key, ctx.Err())
		}
		e.mu.Lock()
		if h.sess == nil {
			err := h.dialErr
			e.mu.Unlock()
			release()
			return nil, nil, nil, codeErr(CodeUnreachable, "%s", dialMessage(key, err))
		}
	}
}

func dialMessage(key string, err error) string {
	switch {
	case err == nil:
		return key + ": no session"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("no switcher answered at %s within %v", key, atem.ConnectTimeout)
	}
	return err.Error()
}

// dial opens h's session. It runs on the engine's context, not a
// request's, since every request waiting for it shares it.
func (e *Engine) dial(h *host) {
	ctx, cancel := context.WithTimeout(e.ctx, atem.ConnectTimeout)
	s, err := e.cfg.Dial(ctx, h.key)
	cancel()
	e.mu.Lock()
	ch := h.dialing
	h.dialing, h.dialErr = nil, err
	if err == nil {
		if e.stopping {
			go s.Close()
		} else {
			h.sess, h.opened = s, time.Now()
		}
	}
	close(ch)
	e.mu.Unlock()
	if err != nil {
		e.log.Warn("connect failed", "host", h.key, "err", err)
		return
	}
	st := s.State()
	e.log.Info("connected", "host", h.key, "product", st.ProductName(""), "video_mode", videoModeLabel(st))
}

func videoModeLabel(st atem.State) string {
	if m := st.VideoMode.Mode; m != nil {
		return m.Label()
	}
	return ""
}

// release ends one request's use of h; the last one out starts the idle
// clock unless h is pinned.
func (e *Engine) release(h *host) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h.busy--
	h.used = time.Now()
	e.scheduleIdle(h)
}

func (e *Engine) scheduleIdle(h *host) {
	if h.busy > 0 || h.pinned || e.stopping {
		return
	}
	if h.idle != nil {
		h.idle.Stop()
	}
	h.idle = time.AfterFunc(e.idle, func() { e.expire(h) })
}

func (e *Engine) expire(h *host) {
	e.mu.Lock()
	if h.busy > 0 || h.pinned || e.hosts[h.key] != h || time.Since(h.used) < e.idle {
		e.mu.Unlock()
		return
	}
	delete(e.hosts, h.key)
	s := h.sess
	h.sess = nil
	e.mu.Unlock()
	if s != nil {
		e.log.Info("closing idle session", "host", h.key)
		s.Close()
	}
}

// disconnect closes addr's session, pinned or not. Requests using it end
// with ErrClosed.
func (e *Engine) disconnect(addr string) (bool, error) {
	key, err := hostKey(addr)
	if err != nil {
		return false, err
	}
	e.mu.Lock()
	h := e.hosts[key]
	if h == nil {
		e.mu.Unlock()
		return false, nil
	}
	delete(e.hosts, key)
	if h.idle != nil {
		h.idle.Stop()
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

// pin keeps h's session open until disconnect.
func (e *Engine) pin(h *host, on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h.pinned = on
	if !on {
		e.scheduleIdle(h)
	}
}

// waitReady waits for the session's state to be complete: at once,
// unless it is re-handshaking after a silence.
func waitReady(ctx context.Context, s *atem.Session) error {
	select {
	case <-s.Ready():
		return nil
	default:
	}
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
	Version  string          `json:"version"`
	Protocol int             `json:"protocol"`
	PID      int             `json:"pid"`
	Started  time.Time       `json:"started"`
	Socket   string          `json:"socket,omitempty"`
	Log      string          `json:"log,omitempty"`
	Sessions []SessionStatus `json:"sessions"`
}

// SessionStatus is one switcher session.
type SessionStatus struct {
	Host      string     `json:"host"`
	Connected bool       `json:"connected"`
	Ready     bool       `json:"ready"`
	Pinned    bool       `json:"pinned"`
	Busy      int        `json:"busy"`
	Since     *time.Time `json:"since,omitempty"`
	// IdleCloseS is the seconds until an idle, unpinned session closes.
	IdleCloseS int    `json:"idle_close_s,omitempty"`
	Product    string `json:"product,omitempty"`
	VideoMode  string `json:"video_mode,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (e *Engine) status() Status {
	st := Status{Version: e.cfg.Version, Protocol: Version, PID: os.Getpid(), Started: e.started,
		Socket: e.cfg.Socket, Log: e.cfg.LogPath, Sessions: []SessionStatus{}}
	e.mu.Lock()
	type snap struct {
		ss   SessionStatus
		sess *atem.Session
	}
	var snaps []snap
	for _, h := range e.hosts {
		ss := SessionStatus{Host: h.key, Pinned: h.pinned, Busy: h.busy}
		if h.dialErr != nil && h.sess == nil {
			ss.Error = h.dialErr.Error()
		}
		if h.sess != nil {
			t := h.opened
			ss.Since = &t
		}
		if h.busy == 0 && !h.pinned && !h.used.IsZero() {
			ss.IdleCloseS = max(0, int((e.idle - time.Since(h.used)).Seconds()))
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
			state := s.sess.State()
			s.ss.Product, s.ss.VideoMode = state.ProductName(""), videoModeLabel(state)
		}
		st.Sessions = append(st.Sessions, s.ss)
	}
	slices.SortFunc(st.Sessions, func(a, b SessionStatus) int { return strings.Compare(a.Host, b.Host) })
	return st
}

func (e *Engine) engineRequest(req Request) (any, bool, error) {
	switch req.Path {
	case "status", "ping":
		return e.status(), false, nil
	case "stop":
		return map[string]bool{"stopping": true}, false, nil
	}
	return nil, false, codeErr(CodeUsage, "the engine has no request %q", req.Path)
}
