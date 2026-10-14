// SPDX-License-Identifier: MIT

package mcp

// wait_for_change: a long poll over a host's changes, for an agent that
// would otherwise run bmdctl watch in a background shell and lose control
// of it (#2). The first call for a host starts the engine's watch for it
// in the background; each change goes into a numbered log, and a cursor
// is a place in that log, so a change between two calls is not lost. The
// watch stops waitIdle after the last call, so the session can idle out
// as any other.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

const (
	waitKept    = 1000                   // changes a host's log keeps
	waitMax     = 100                    // changes one call returns
	waitBurst   = 250 * time.Millisecond // gathered after the first: a cut's packets come together
	waitDefault = 30                     // seconds, when timeout_s is left out
	waitLimit   = 60                     // seconds, at most
)

// waitIdle is how long a host's watch outlives its last call.
var waitIdle = 10 * time.Minute

var waitTool = tool{Name: "wait_for_change", Title: "Wait for a change", special: "wait_for_change",
	Description: "Wait for the next change on a device and return it: a long poll, for watching a switcher or a deck without a subscription. " +
		"It returns as soon as a change comes (with any that come in the 250 ms after it, as a cut's do), or after timeout_s with no changes, " +
		"and always a cursor: pass it to the next call and that call starts where this one ended, so nothing in between is missed. " +
		"To act and then see the result, call it first with timeout_s 0 for a cursor, act, then call it with that cursor. " +
		"Each change is {kind, opcode, key, value, time}: kind state, or disconnected then ready when the session dropped and came back. " +
		"An ATEM's keys are the opcode and indexes from 0 (PrgI:0 is M/E 1's program, KeOn:0:1 M/E 1 keyer 2, TlSr the tally); " +
		"the clock's Time packets and the index and Fairlight tallies are left out. " +
		"missed: true means some changes are gone (the cursor is from before the server last watched this host, or over a thousand changes old): read the state to catch up.",
	InputSchema: table.Schema{"type": "object", "additionalProperties": false, "properties": table.Schema{
		"host":   hostProp,
		"cursor": table.Schema{"type": "string", "description": "the cursor an earlier call returned; left out, the wait starts now"},
		"timeout_s": table.Schema{"type": "integer", "minimum": 0, "maximum": waitLimit,
			"description": fmt.Sprintf("how long to wait for a change, in seconds (default %d); 0 returns at once, with a cursor and any changes since the one given", waitDefault)},
		"filter": table.Schema{"type": "array", "items": table.Schema{"type": "string"},
			"description": "only changes whose key starts with one of these (PrgI, KeOn:0); disconnected and ready always come. The cursor moves past the changes left out"},
	}},
	Annotations: map[string]any{"title": "Wait for a change", "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
}

// changeLog is a host's watch and the changes it has seen.
type changeLog struct {
	gen     string        // this log's id, in its cursors
	started chan struct{} // closed once the watch runs, or failed to start
	cancel  context.CancelFunc

	mu      sync.Mutex
	running bool          // the engine said it was watching
	events  []change      // the last waitKept
	next    uint64        // the next change's number, from 1
	wake    chan struct{} // closed, and replaced, at each change and at the end
	done    bool
	err     error // why the watch ended, or failed to start
	callers int
	idle    *time.Timer
}

type change struct {
	n   uint64
	at  time.Time
	key string
	raw json.RawMessage
	all bool // not a state change: always passes a filter
}

// changeLog is the log of t's changes, its watch started (and waited for)
// if none runs.
func (s *Server) changeLog(ctx context.Context, t Target) (*changeLog, error) {
	key := deviceOr(t.Device) + " " + t.Address
	s.mu.Lock()
	l := s.logs[key]
	if l != nil {
		l.mu.Lock()
		done := l.done
		l.mu.Unlock()
		if done {
			l = nil // ended, and going: start another
		}
	}
	if l == nil {
		s.gens++
		wctx, cancel := context.WithCancel(s.base)
		l = &changeLog{gen: strconv.FormatInt(time.Now().UnixMilli(), 36) + strconv.Itoa(s.gens),
			started: make(chan struct{}), cancel: cancel, next: 1, wake: make(chan struct{})}
		l.idle = time.AfterFunc(waitIdle, l.expire)
		s.logs[key] = l
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			l.run(wctx, s.cfg.Call, engine.Request{Device: deviceOr(t.Device), Host: t.Address, Path: "watch"})
			s.mu.Lock()
			if s.logs[key] == l {
				delete(s.logs, key)
			}
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-l.started:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return nil, l.err
	}
	return l, nil
}

// run is the watch: it logs each change until the engine ends it or ctx
// ends.
func (l *changeLog) run(ctx context.Context, call func(context.Context, engine.Request, func(engine.Frame) error) error, req engine.Request) {
	var once sync.Once
	start := func() { once.Do(func() { close(l.started) }) }
	var failed error
	err := call(ctx, req, func(f engine.Frame) error {
		if f.Response == nil {
			return nil
		}
		if !f.Response.OK {
			failed = &engine.Error{Code: f.Response.Code, Err: errors.New(f.Response.Error)}
			return nil
		}
		raw, _ := f.Response.Result.(json.RawMessage)
		var ev struct{ Kind, Key string }
		json.Unmarshal(raw, &ev)
		if ev.Kind == "watching" {
			l.mu.Lock()
			l.running = true
			l.mu.Unlock()
			start()
			return nil
		}
		l.mu.Lock()
		l.events = append(l.events, change{n: l.next, at: time.Now(), key: ev.Key, raw: raw, all: ev.Kind != "state"})
		if len(l.events) > waitKept {
			l.events = slices.Delete(l.events, 0, len(l.events)-waitKept)
		}
		l.next++
		close(l.wake)
		l.wake = make(chan struct{})
		l.mu.Unlock()
		return nil
	})
	l.mu.Lock()
	switch {
	case failed != nil:
		l.err = failed
	case err != nil:
		l.err = err
	default:
		l.err = errors.New("the engine ended the watch")
	}
	l.done = true
	l.idle.Stop()
	close(l.wake)
	l.mu.Unlock()
	start()
}

// use marks a call in progress; release ends it, and the last call's end
// starts the idle countdown again.
func (l *changeLog) use() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callers++
	l.idle.Stop()
}

func (l *changeLog) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.callers--; l.callers == 0 && !l.done {
		l.idle.Reset(waitIdle)
	}
}

// expire stops the watch, waitIdle after the last call, unless one has
// begun since.
func (l *changeLog) expire() {
	l.mu.Lock()
	idle := l.callers == 0
	l.mu.Unlock()
	if idle {
		l.cancel()
	}
}

// from is where a cursor says to start: the number of the last change it
// saw, and whether changes after it are gone. No cursor is now.
func (l *changeLog) from(cursor string) (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.next - 1
	if cursor == "" {
		return now, false
	}
	gen, num, ok := strings.Cut(cursor, "-")
	n, err := strconv.ParseUint(num, 10, 64)
	if !ok || err != nil || gen != l.gen || n > now {
		return now, true // another watch's, or no cursor of ours
	}
	if len(l.events) > 0 && n+1 < l.events[0].n {
		return l.events[0].n - 1, true
	}
	return n, false
}

func (l *changeLog) cursor(n uint64) string { return l.gen + "-" + strconv.FormatUint(n, 10) }

// waitForChange is wait_for_change: the changes after the cursor that
// pass the filter, waiting up to timeout_s for the first.
func (s *Server) waitForChange(ctx context.Context, args map[string]any) map[string]any {
	host, _ := args["host"].(string)
	cursor, _ := args["cursor"].(string)
	timeout := float64(waitDefault)
	var filters []string
	for name, v := range args {
		switch name {
		case "host", "cursor":
			if _, ok := v.(string); !ok && v != nil {
				return toolError(engine.CodeUsage, name+": a string", nil)
			}
		case "timeout_s":
			f, ok := v.(float64)
			if !ok || f < 0 || f > waitLimit {
				return toolError(engine.CodeUsage, fmt.Sprintf("timeout_s: seconds from 0 to %d", waitLimit), nil)
			}
			timeout = f
		case "filter":
			str, ok := table.StringValue(v)
			if !ok {
				return toolError(engine.CodeUsage, "filter: a list of key prefixes", nil)
			}
			for _, f := range strings.Split(str, ",") {
				if f = strings.TrimSpace(f); f != "" {
					filters = append(filters, f)
				}
			}
		default:
			return toolError(engine.CodeUsage, fmt.Sprintf("wait_for_change takes no %q; it takes host, cursor, timeout_s, filter", name), nil)
		}
	}
	t, err := s.cfg.Target(host)
	if err != nil {
		return toolError(engine.CodeUsage, err.Error(), nil)
	}
	l, err := s.changeLog(ctx, t)
	if err != nil {
		return toolError(engine.CodeOf(err), err.Error(), nil)
	}
	l.use()
	defer l.release()
	at, missed := l.from(cursor)
	deadline := time.Now().Add(time.Duration(timeout * float64(time.Second)))
	events := []json.RawMessage{}
	var first time.Time
	more := false
	for {
		l.mu.Lock()
		if len(l.events) > 0 && at+1 < l.events[0].n {
			missed = true // over waitKept came while this call waited
		}
		for _, c := range l.events {
			if c.n <= at {
				continue
			}
			if !c.all && len(filters) > 0 && !slices.ContainsFunc(filters, func(f string) bool { return strings.HasPrefix(c.key, f) }) {
				at = c.n
				continue
			}
			if len(events) == waitMax {
				more = true
				break
			}
			at = c.n
			if len(events) == 0 {
				first = c.at
			}
			events = append(events, c.raw)
		}
		wake, done, ended := l.wake, l.done, l.err
		l.mu.Unlock()
		if more {
			break
		}
		if done {
			if len(events) == 0 {
				return toolError(engine.CodeOf(ended), "the watch ended: "+ended.Error(), nil)
			}
			break
		}
		stop := deadline
		if len(events) > 0 && first.Add(waitBurst).Before(stop) {
			stop = first.Add(waitBurst)
		}
		wait := time.Until(stop)
		if wait <= 0 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return toolError(engine.CodeOf(ctx.Err()), ctx.Err().Error(), nil)
		}
		timer.Stop()
	}
	out := map[string]any{"ok": true, "status": "done", "cursor": l.cursor(at), "events": events}
	if len(events) == 0 {
		out["timed_out"] = true
	}
	if missed {
		out["missed"] = true
	}
	if more {
		out["more"] = true
	}
	return toolOK(out)
}
