// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// fakeWatch is the engine's watch for wait_for_change's tests: each
// watch it runs says watching, then passes on what send sends, until
// end ends it (with an error, or nil: the engine ended it).
type fakeWatch struct {
	mu      sync.Mutex
	events  chan string
	ends    chan error
	started int
	running int
	sent    int // to the watch running
	refuse  error
}

func newFakeWatch() *fakeWatch {
	return &fakeWatch{events: make(chan string, 10000), ends: make(chan error, 1)}
}

func (f *fakeWatch) call(ctx context.Context, req engine.Request, onFrame func(engine.Frame) error) error {
	if req.Path != "watch" {
		return fmt.Errorf("asked for %s", req.Path)
	}
	f.mu.Lock()
	if f.refuse != nil {
		f.mu.Unlock()
		onFrame(engine.Frame{Response: &engine.Response{Code: engine.CodeUnreachable, Error: f.refuse.Error()}})
		return nil
	}
	f.started++
	f.running++
	f.sent = 0
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.running--; f.mu.Unlock() }()
	frame := func(raw string) {
		onFrame(engine.Frame{Response: &engine.Response{OK: true, Result: json.RawMessage(raw)}})
	}
	frame(`{"kind":"watching","time":"16:55:00.000"}`)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-f.ends:
			if err != nil {
				onFrame(engine.Frame{Response: &engine.Response{Code: engine.CodeUnreachable, Error: err.Error()}})
			}
			return nil
		case raw := <-f.events:
			frame(raw)
		}
	}
}

// push sends the watch a frame's result.
func (f *fakeWatch) push(raw string) {
	f.mu.Lock()
	f.sent++
	f.mu.Unlock()
	f.events <- raw
}

func stateChange(key string) string {
	return fmt.Sprintf(`{"kind":"state","opcode":%q,"key":%q,"value":{},"time":"16:55:00.000"}`, key[:4], key)
}

// send sends a state change with key, and waits until the log has it and
// everything sent before it.
func (f *fakeWatch) send(t *testing.T, s *Server, key string) {
	t.Helper()
	f.push(stateChange(key))
	f.mu.Lock()
	sent := uint64(f.sent)
	f.mu.Unlock()
	eventually(t, key+" logged", func() bool { return s.logged() >= sent })
}

func (f *fakeWatch) counts() (started, running int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.running
}

// logged is how many changes the one log has had; waiting, how many
// calls are in progress on it.
func (s *Server) logged() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.logs {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.next - 1
	}
	return 0
}

func (s *Server) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.logs {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.callers
	}
	return 0
}

func waitServer(t *testing.T, fw *fakeWatch) *Server {
	t.Helper()
	reg, err := engine.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Registry: reg, Call: fw.call,
		Target: func(spec string) (Target, error) {
			if spec == "nowhere" {
				return Target{}, errors.New(`no host "nowhere"`)
			}
			return Target{Address: "192.0.2.10", Device: "atem", Label: "studio"}, nil
		}})
	ctx, cancel := context.WithCancel(context.Background())
	s.base = ctx
	t.Cleanup(func() { cancel(); s.wg.Wait() })
	return s
}

// waitResult is a wait_for_change result's structured content.
type waitResult struct {
	OK       bool              `json:"ok"`
	Cursor   string            `json:"cursor"`
	Events   []json.RawMessage `json:"events"`
	TimedOut bool              `json:"timed_out"`
	Missed   bool              `json:"missed"`
	More     bool              `json:"more"`
	Code     string            `json:"code"`
	Error    string            `json:"error"`
}

// wait calls wait_for_change with args as a client sends them: JSON, its
// numbers float64.
func (s *Server) wait(t *testing.T, args map[string]any) waitResult {
	t.Helper()
	b, _ := json.Marshal(args)
	args = nil
	json.Unmarshal(b, &args)
	out := s.call(context.Background(), s.byKey["wait_for_change"], args)
	b, _ = json.Marshal(out["structuredContent"])
	var r waitResult
	json.Unmarshal(b, &r)
	return r
}

func keys(r waitResult) []string {
	var out []string
	for _, raw := range r.Events {
		var ev struct{ Key, Kind string }
		json.Unmarshal(raw, &ev)
		if ev.Key == "" {
			out = append(out, ev.Kind)
		} else {
			out = append(out, ev.Key)
		}
	}
	return out
}

// A cursor is a place among the host's changes: a change between two
// calls comes in the second, and a call with nothing to return yet waits
// for the next.
func TestWaitForChangeCursor(t *testing.T) {
	fw := newFakeWatch()
	s := waitServer(t, fw)
	r := s.wait(t, map[string]any{"timeout_s": 0})
	if !r.OK || r.Cursor == "" || !r.TimedOut || len(r.Events) != 0 {
		t.Fatalf("timeout 0: %+v", r)
	}
	fw.send(t, s, "PrgI:0")
	fw.send(t, s, "PrvI:0")
	r = s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 5})
	if got := keys(r); fmt.Sprint(got) != "[PrgI:0 PrvI:0]" || r.Missed || r.TimedOut {
		t.Fatalf("since the cursor: %v %+v", got, r)
	}
	// Nothing since: it waits for the next change, and the ones that come
	// right after it.
	done := make(chan waitResult)
	go func() { done <- s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 30}) }()
	eventually(t, "the call to wait", func() bool { return s.waiting() == 1 })
	fw.push(stateChange("KeOn:0:1"))
	fw.push(stateChange("TlSr"))
	select {
	case r2 := <-done:
		if got := keys(r2); fmt.Sprint(got) != "[KeOn:0:1 TlSr]" {
			t.Errorf("woken: %v", got)
		}
		r = r2
	case <-time.After(10 * time.Second):
		t.Fatal("a change didn't wake the wait")
	}
	// Timed out: no changes, and a cursor where it ended.
	start := time.Now()
	if r2 := s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 1}); !r2.TimedOut || r2.Cursor != r.Cursor || time.Since(start) < time.Second {
		t.Errorf("timed out: %+v after %v", r2, time.Since(start))
	}
	if started, _ := fw.counts(); started != 1 {
		t.Errorf("%d watches for one host", started)
	}
}

// A filter passes the changes whose key starts with one of its prefixes,
// and the session's own; the cursor moves past the rest, so they aren't
// looked at again.
func TestWaitForChangeFilter(t *testing.T) {
	fw := newFakeWatch()
	s := waitServer(t, fw)
	c := s.wait(t, map[string]any{"timeout_s": 0}).Cursor
	fw.send(t, s, "AuxS:1")
	fw.send(t, s, "PrgI:0")
	fw.push(`{"kind":"disconnected","time":"16:55:01.000"}`)
	fw.send(t, s, "KeOn:0:0")
	r := s.wait(t, map[string]any{"cursor": c, "timeout_s": 1, "filter": []any{"PrgI", "KeOn:0"}})
	if got := keys(r); fmt.Sprint(got) != "[PrgI:0 disconnected KeOn:0:0]" {
		t.Errorf("filtered: %v", got)
	}
	fw.send(t, s, "AuxS:2")
	r = s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 0, "filter": "PrgI"})
	if !r.TimedOut || r.Cursor == c {
		t.Errorf("only left out: %+v", r)
	}
	if r2 := s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 0}); len(r2.Events) != 0 {
		t.Errorf("a change left out came again: %v", keys(r2))
	}
}

// What a cursor can't say: one from another watch (the server restarted,
// or the watch idled out) or older than the log keeps is missed: true;
// over a call's worth is more: true, and the next call goes on.
func TestWaitForChangeMissed(t *testing.T) {
	fw := newFakeWatch()
	s := waitServer(t, fw)
	c := s.wait(t, map[string]any{"timeout_s": 0}).Cursor
	if r := s.wait(t, map[string]any{"cursor": "nonsense", "timeout_s": 0}); !r.Missed || r.Cursor != c {
		t.Errorf("a cursor not ours: %+v", r)
	}
	for i := range waitKept + 5 {
		fw.push(stateChange(fmt.Sprintf("AuxS:%d", i)))
	}
	fw.send(t, s, "PrgI:0")
	r := s.wait(t, map[string]any{"cursor": c, "timeout_s": 0})
	if !r.Missed || !r.More || len(r.Events) != waitMax || keys(r)[0] != "AuxS:6" {
		t.Errorf("too old: missed %v more %v, %d events from %v", r.Missed, r.More, len(r.Events), keys(r)[0])
	}
	n := len(r.Events)
	for r.More {
		r = s.wait(t, map[string]any{"cursor": r.Cursor, "timeout_s": 0})
		if r.Missed {
			t.Fatal("missed between pages")
		}
		n += len(r.Events)
	}
	if n != waitKept {
		t.Errorf("%d changes over the pages, want the %d kept", n, waitKept)
	}
	if k := keys(r); k[len(k)-1] != "PrgI:0" {
		t.Errorf("last page ends %v", k[len(k)-1])
	}
}

// The watch ends with the session: a call waiting says why, and the next
// call starts another watch, the old cursor missed.
func TestWaitForChangeEnds(t *testing.T) {
	fw := newFakeWatch()
	s := waitServer(t, fw)
	c := s.wait(t, map[string]any{"timeout_s": 0}).Cursor
	done := make(chan waitResult)
	go func() { done <- s.wait(t, map[string]any{"cursor": c, "timeout_s": 30}) }()
	eventually(t, "the call to wait", func() bool { return s.waiting() == 1 })
	fw.ends <- errors.New("the session closed")
	select {
	case r := <-done:
		if r.OK || r.Code != engine.CodeUnreachable || r.Error != "the watch ended: the session closed" {
			t.Errorf("ended: %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the end didn't wake the wait")
	}
	r := s.wait(t, map[string]any{"cursor": c, "timeout_s": 0})
	if !r.OK || !r.Missed {
		t.Errorf("after: %+v", r)
	}
	if started, _ := fw.counts(); started != 2 {
		t.Errorf("%d watches started", started)
	}
	// One that can't start is the call's error.
	fw.mu.Lock()
	fw.refuse = errors.New("STUDIO B (192.0.2.10) did not answer")
	fw.mu.Unlock()
	fw.ends <- nil
	eventually(t, "the watch to end", func() bool { _, running := fw.counts(); return running == 0 })
	if r := s.wait(t, map[string]any{"timeout_s": 0}); r.OK || r.Code != engine.CodeUnreachable || r.Error != "STUDIO B (192.0.2.10) did not answer" {
		t.Errorf("refused: %+v", r)
	}
}

// The watch stops waitIdle after the last call, so the session can idle
// out; a call waiting keeps it.
func TestWaitForChangeIdle(t *testing.T) {
	defer func(d time.Duration) { waitIdle = d }(waitIdle)
	waitIdle = 300 * time.Millisecond
	fw := newFakeWatch()
	s := waitServer(t, fw)
	s.wait(t, map[string]any{"timeout_s": 1})
	if _, running := fw.counts(); running != 1 {
		t.Fatalf("not watching after a 1 s wait longer than waitIdle")
	}
	eventually(t, "the idle watch to stop", func() bool { _, running := fw.counts(); return running == 0 })
}

// Arguments: the tool's own, in range; a host bmdctl can't name.
func TestWaitForChangeArguments(t *testing.T) {
	s := waitServer(t, newFakeWatch())
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"seconds": 5}, `wait_for_change takes no "seconds"; it takes host, cursor, timeout_s, filter`},
		{map[string]any{"timeout_s": 61}, "timeout_s: seconds from 0 to 60"},
		{map[string]any{"timeout_s": -1}, "timeout_s: seconds from 0 to 60"},
		{map[string]any{"cursor": 5}, "cursor: a string"},
		{map[string]any{"host": "nowhere"}, `no host "nowhere"`},
	} {
		if r := s.wait(t, c.args); r.OK || r.Code != engine.CodeUsage || r.Error != c.want {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("no %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
