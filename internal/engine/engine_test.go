// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// rig is an engine on a temporary socket whose sessions go to fake
// switchers.
type rig struct {
	t     *testing.T
	e     *Engine
	sock  string
	dials atomic.Int32
	mu    sync.Mutex
	sws   []*fakeatem.Switcher
	gate  chan struct{} // if set, each dial waits for it
}

func newRig(t *testing.T, cfg Config) *rig {
	t.Helper()
	dir, err := os.MkdirTemp("", "engine")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r := &rig{t: t, sock: filepath.Join(dir, "s.sock")}
	dump := fixtures.Bytes(t, "dump/initial-dump.bin")
	reg, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Registry = reg
	cfg.Dial = func(ctx context.Context, addr string) (*atem.Session, error) {
		r.dials.Add(1)
		if r.gate != nil {
			<-r.gate
		}
		sw := fakeatem.New(dump)
		sw.Unanswered("CTWp") // a command that is never confirmed
		r.mu.Lock()
		r.sws = append(r.sws, sw)
		r.mu.Unlock()
		return (&atem.Dialer{Conn: sw}).Dial(ctx, addr)
	}
	r.e = New(cfg)
	l, err := Listen(r.sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.e.Serve(l); close(done) }()
	t.Cleanup(func() { r.e.Stop(); <-done })
	return r
}

// call sends one request over the socket.
func (r *rig) call(ctx context.Context, req Request) (Response, error) {
	conn, err := Dial(ctx, r.sock)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	return Call(ctx, conn, req, nil)
}

func (r *rig) must(req Request) Response {
	r.t.Helper()
	resp, err := r.call(context.Background(), req)
	if err != nil || !resp.OK {
		r.t.Fatalf("%+v: %+v %v", req, resp, err)
	}
	return resp
}

func program(source string) Request {
	return Request{Device: "atem", Host: fakeatem.Address, Path: "program", Params: map[string]string{"source": source}}
}

func (r *rig) status() Status {
	var st Status
	resp := r.must(Request{Device: "engine", Path: "status"})
	json.Unmarshal(resp.Result.(json.RawMessage), &st)
	return st
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !f(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Concurrent requests for a host share one dial and one session.
func TestRequestsShareADial(t *testing.T) {
	r := newRig(t, Config{})
	r.gate = make(chan struct{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if resp, err := r.call(context.Background(), program(string(rune('1'+i%2)))); err != nil || !resp.OK {
				t.Errorf("%+v %v", resp, err)
			}
		}()
	}
	eventually(t, "the dial", func() bool { return r.dials.Load() == 1 })
	time.Sleep(20 * time.Millisecond)
	close(r.gate)
	wg.Wait()
	if n := r.dials.Load(); n != 1 {
		t.Errorf("%d dials", n)
	}
	// The same switcher however its address is written.
	r.must(Request{Device: "atem", Host: fakeatem.Address + ":9910", Path: "cut"})
	if n := r.dials.Load(); n != 1 {
		t.Errorf("%d dials after host:port", n)
	}
	st := r.status()
	if len(st.Sessions) != 1 || st.Sessions[0].Host != fakeatem.Address+":9910" || !st.Sessions[0].Ready ||
		st.Sessions[0].Product != "ATEM 1 M/E Constellation HD" || st.Sessions[0].VideoMode != "1080p50" {
		t.Errorf("%+v", st.Sessions)
	}
}

// An unpinned session closes after the idle timeout; the next request
// dials again. A pinned one stays until disconnected.
func TestIdleTimeoutAndPinning(t *testing.T) {
	r := newRig(t, Config{IdleTimeout: 100 * time.Millisecond})
	r.must(program("1"))
	eventually(t, "the idle close", func() bool { return len(r.status().Sessions) == 0 })
	r.must(program("2"))
	if n := r.dials.Load(); n != 2 {
		t.Fatalf("%d dials", n)
	}
	r.e.mu.Lock()
	h := r.e.hosts[fakeatem.Address+":9910"]
	r.e.mu.Unlock()
	r.e.pin(h, true)
	time.Sleep(300 * time.Millisecond)
	if st := r.status(); len(st.Sessions) != 1 || !st.Sessions[0].Pinned {
		t.Fatalf("pinned session gone: %+v", st.Sessions)
	}
	if closed, err := r.e.disconnect(fakeatem.Address); !closed || err != nil {
		t.Fatalf("disconnect: %v %v", closed, err)
	}
	if st := r.status(); len(st.Sessions) != 0 {
		t.Fatalf("after disconnect: %+v", st.Sessions)
	}
	r.must(program("1"))
	if n := r.dials.Load(); n != 3 {
		t.Fatalf("%d dials", n)
	}
}

// A session that fails is replaced by the next request.
func TestFailedSessionIsRedialled(t *testing.T) {
	r := newRig(t, Config{})
	r.must(program("1"))
	r.e.mu.Lock()
	s := r.e.hosts[fakeatem.Address+":9910"].sess
	r.e.mu.Unlock()
	s.Close()
	r.must(program("2"))
	if n := r.dials.Load(); n != 2 {
		t.Errorf("%d dials", n)
	}
}

// A caller hanging up ends its request: the echo wait stops at once.
func TestHangUpCancels(t *testing.T) {
	r := newRig(t, Config{})
	r.must(program("1"))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	req := Request{Device: "atem", Host: fakeatem.Address, Path: "transition wipe", Params: map[string]string{"rate": "10"}, TimeoutMS: 60_000}
	if _, err := r.call(ctx, req); err == nil {
		t.Fatal("no error")
	}
	eventually(t, "the request to end", func() bool {
		st := r.status()
		return len(st.Sessions) == 1 && st.Sessions[0].Busy == 0
	})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

// The engine refuses a request of another protocol version, and unknown
// devices, commands and engine requests.
func TestRefusals(t *testing.T) {
	r := newRig(t, Config{})
	conn, err := Dial(context.Background(), r.sock)
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte(`{"v":99,"device":"engine","path":"status"}` + "\n"))
	b := make([]byte, 4096)
	n, _ := conn.Read(b)
	conn.Close()
	if !strings.Contains(string(b[:n]), `"code":"engine"`) {
		t.Errorf("v99: %s", b[:n])
	}
	for _, c := range []struct {
		req  Request
		code string
	}{
		{Request{Device: "hyperdeck", Host: "x", Path: "play"}, CodeUsage},
		{Request{Device: "atem", Host: fakeatem.Address, Path: "nonsense"}, CodeUsage},
		{Request{Device: "atem", Path: "cut"}, CodeUsage},
		{Request{Device: "engine", Path: "dance"}, CodeUsage},
		{Request{Device: "atem", Host: fakeatem.Address, Path: "startup clear"}, CodeDestructive},
		{Request{Device: "atem", Host: fakeatem.Address, Path: "program", Params: map[string]string{"source": "1", "me": "0"}}, CodeUsage},
	} {
		resp, err := r.call(context.Background(), c.req)
		if err != nil || resp.OK || resp.Code != c.code {
			t.Errorf("%+v: %+v %v", c.req, resp, err)
		}
	}
}

// A second engine can't take the socket of a running one; a socket left
// by a dead engine is taken over.
func TestListenTwice(t *testing.T) {
	r := newRig(t, Config{})
	if _, err := Listen(r.sock); err != ErrRunning {
		t.Fatalf("second listen: %v", err)
	}
	dir := t.TempDir()
	stale := filepath.Join(dir, "x.sock")
	os.WriteFile(stale, nil, 0o600)
	l, err := Listen(stale)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	l.Close()
}

// The socket is the owner's alone.
func TestSocketMode(t *testing.T) {
	r := newRig(t, Config{})
	fi, err := os.Stat(r.sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
}

// Notes go only to a request that asks: the connect, the switcher, what
// is live before and after, and a command's values before and after. The response is the same either way.
func TestNotes(t *testing.T) {
	r := newRig(t, Config{})
	frames := func(req Request) ([]Note, string) {
		conn, err := Dial(context.Background(), r.sock)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var notes []Note
		var resp []byte
		Call(context.Background(), conn, req, func(f Frame) error {
			if f.Note != nil {
				notes = append(notes, *f.Note)
			}
			if f.Response != nil {
				resp = f.Line
			}
			return nil
		})
		return notes, string(resp)
	}
	req := Request{Device: "atem", Host: fakeatem.Address, Path: "program", Params: map[string]string{"source": "1"}, Notes: true}
	notes, resp := frames(req)
	// What is live comes before the command and after it.
	if len(notes) != 5 || notes[0].Connecting != "192.0.2.10:9910" || notes[1].Switcher == nil || notes[2].OnAir == nil ||
		len(notes[3].Values) != 2 || notes[4].OnAir == nil {
		t.Fatalf("notes: %+v", notes)
	}
	if was, now := *notes[2].OnAir, *notes[4].OnAir; was != (OnAir{"CAM2", "CAM1", 2, 1}) || now != (OnAir{"CAM1", "CAM1", 1, 1}) {
		t.Errorf("on air: %+v, then %+v", was, now)
	}
	notes = append(notes[:2], notes[3])
	sw := notes[1].Switcher
	if sw.Name != "ATEM 1 M/E Constellation HD" || sw.Product != sw.Name || sw.VideoMode != "1080p50" || !sw.Ready || sw.DropS != 600 {
		t.Errorf("switcher: %+v", sw)
	}
	before, after := *notes[2].Values[1].Before, notes[2].Values[1].After
	if v := notes[2].Values; v[0].Param != "me" || v[0].After != "1" || v[1].Param != "source" || before != "CAM2" || after != "CAM1" {
		t.Errorf("values: %+v", v)
	}
	// A warm session: no connect; no notes at all unless asked. (Source
	// 1 is on program now, so these are answered "already".)
	notes, resp = frames(req)
	if len(notes) != 4 || notes[0].Switcher == nil {
		t.Errorf("warm: %+v", notes)
	}
	req.Notes = false
	notes, plain := frames(req)
	strip := func(s string) string { return s[:strings.Index(s, `"took_ms"`)] }
	if len(notes) != 0 || strip(plain) != strip(resp) {
		t.Errorf("unasked: %+v\n%s\n%s", notes, plain, resp)
	}
	// The overview: the engine and the session.
	o := r.must(Request{Device: "engine", Path: "overview"})
	var ov Overview
	json.Unmarshal(o.Result.(json.RawMessage), &ov)
	if len(ov.Switchers) != 1 || ov.Switchers[0].Name != "ATEM 1 M/E Constellation HD" || !ov.Switchers[0].Ready || ov.Engine.PID == 0 {
		t.Errorf("overview: %+v", ov)
	}
}
