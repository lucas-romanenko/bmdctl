// SPDX-License-Identifier: MIT

package hyperdeck

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// testdata/oracle/client.json records what bmdwire v1.7.1's hyperdeckwire
// client did in 747 sessions over scripted streams (gen_client.py). Each
// is replayed here through a Session on a connection that gives it, call
// by call, exactly the bytes hyperdeckwire had read: the Session reads
// eagerly, hyperdeckwire only when asked.

type oracle struct {
	Sessions []oracleSession `json:"sessions"`
	Parsers  []struct {
		Fn     string          `json:"fn"`
		Line   string          `json:"line"`
		Result json.RawMessage `json:"result"`
		Error  *oracleError    `json:"error"`
	} `json:"parsers"`
	Params []struct {
		Lines  []string          `json:"lines"`
		Result map[string]string `json:"result"`
	} `json:"params"`
}

type oracleSession struct {
	Name    string `json:"name"`
	Stream  string `json:"stream"`
	Connect struct {
		oracleStep
		Model           string `json:"model"`
		ProtocolVersion string `json:"protocol_version"`
		Greeting        *struct {
			Code  int      `json:"code"`
			Text  string   `json:"text"`
			Lines []string `json:"lines"`
		} `json:"greeting"`
	} `json:"connect"`
	Calls []oracleCall `json:"calls"`
}

type oracleStep struct {
	Sent     string          `json:"sent"`
	Consumed int             `json:"consumed"`
	Before   int             `json:"before"` // read when it sent its request
	Result   json.RawMessage `json:"result"`
	Error    *oracleError    `json:"error"`
}

type oracleCall struct {
	oracleStep
	Method string                     `json:"method"`
	Args   map[string]json.RawMessage `json:"args"`
}

type oracleError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
	Code    int    `json:"code"`
	Text    string `json:"text"`
}

func loadOracle(t *testing.T) oracle {
	t.Helper()
	b, err := os.ReadFile("testdata/oracle/client.json")
	if err != nil {
		t.Fatal(err)
	}
	var o oracle
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// gatedConn is the deck's end of a session: it lets the session read the
// stream only up to the window armed for the next write, and ends it
// where hyperdeckwire found it ended.
type gatedConn struct {
	stream []byte

	mu       sync.Mutex
	cond     *sync.Cond
	pos      int // read so far
	released int // readable
	waiting  int // reads blocked for more
	eof      bool
	closed   bool
	armed    *window
	written  bytes.Buffer
}

type window struct {
	upTo int
	eof  bool
}

func newGated(stream []byte, first window) *gatedConn {
	g := &gatedConn{stream: stream}
	g.cond = sync.NewCond(&g.mu)
	g.release(first)
	return g
}

func (g *gatedConn) release(w window) {
	g.released = max(g.released, min(w.upTo, len(g.stream)))
	if w.eof {
		g.released, g.eof = len(g.stream), true
	}
	g.cond.Broadcast()
}

// preload releases the stream up to upTo before a call, as what had
// arrived before hyperdeckwire sent its request, and waits until the
// Session has read it all and waits for more: the reader hands each
// answer to the owner on an unbuffered channel, so the owner has taken
// it, as an answer to no command, before the call's request.
func (g *gatedConn) preload(upTo int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.release(window{upTo: upTo})
	for target := min(upTo, len(g.stream)); !g.closed && !(g.pos >= target && g.waiting > 0); {
		if g.pos >= target && g.eof {
			return
		}
		g.cond.Wait()
	}
}

// arm sets what the next write releases.
func (g *gatedConn) arm(w window) {
	g.mu.Lock()
	g.armed = &w
	g.mu.Unlock()
}

func (g *gatedConn) Read(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.pos >= g.released && !(g.eof && g.pos >= len(g.stream)) && !g.closed {
		g.waiting++
		g.cond.Broadcast() // preload watches for a read waiting
		g.cond.Wait()
		g.waiting--
	}
	if g.closed {
		return 0, net.ErrClosed
	}
	if g.pos >= g.released {
		return 0, io.EOF
	}
	n := copy(p, g.stream[g.pos:g.released])
	g.pos += n
	return n, nil
}

func (g *gatedConn) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return 0, net.ErrClosed
	}
	g.written.Write(p)
	if g.armed != nil {
		g.release(*g.armed)
		g.armed = nil
	}
	return len(p), nil
}

// sent returns what was written since the last call and forgets it.
func (g *gatedConn) sent() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	b := bytes.Clone(g.written.Bytes())
	g.written.Reset()
	return b
}

func (g *gatedConn) Close() error {
	g.mu.Lock()
	g.closed = true
	g.cond.Broadcast()
	g.mu.Unlock()
	return nil
}

func (g *gatedConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (g *gatedConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (g *gatedConn) SetDeadline(t time.Time) error      { return nil }
func (g *gatedConn) SetReadDeadline(t time.Time) error  { return nil }
func (g *gatedConn) SetWriteDeadline(t time.Time) error { return nil }

func (s oracleStep) window(stream []byte) window {
	// The stream ended under hyperdeckwire only if a call found it so.
	return window{upTo: s.Consumed, eof: s.Error != nil && s.Error.Class == "ConnectionError"}
}

func TestClientMatchesHyperdeckwire(t *testing.T) {
	o := loadOracle(t)
	if len(o.Sessions) != 747 {
		t.Fatalf("%d sessions, want 747: regenerate with gen_client.py?", len(o.Sessions))
	}
	methods := map[string]int{}
	for _, sc := range o.Sessions {
		t.Run(sc.Name, func(t *testing.T) {
			stream, _ := hex.DecodeString(sc.Stream)
			conn := newGated(stream, sc.Connect.window(stream))
			d := Dialer{Bare: true, DialContext: func(context.Context, string, string) (net.Conn, error) { return conn, nil }}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s, err := d.Dial(ctx, "192.0.2.11")
			if g := sc.Connect.Greeting; g != nil && g.Code >= 100 && g.Code < 200 {
				// Deliberate: a refusal for a greeting fails Dial.
				var de *Error
				if !errors.As(err, &de) || de.Code != g.Code || de.Text != g.Text {
					t.Fatalf("greeting %d %q: Dial = %v, want the refusal", g.Code, g.Text, err)
				}
				return
			}
			if !sameError(t, "connect", err, sc.Connect.Error) || err != nil {
				return
			}
			defer s.Close()
			if got := s.Model(); got != sc.Connect.Model {
				t.Errorf("model %q, want %q", got, sc.Connect.Model)
			}
			if got := s.ProtocolVersion(); got != sc.Connect.ProtocolVersion {
				t.Errorf("protocol version %q, want %q", got, sc.Connect.ProtocolVersion)
			}
			if g := s.Greeting(); g.Code != sc.Connect.Greeting.Code || g.Text != sc.Connect.Greeting.Text ||
				!reflect.DeepEqual(append([]string{}, g.Lines...), append([]string{}, sc.Connect.Greeting.Lines...)) {
				t.Errorf("greeting %+v, want %+v", g, *sc.Connect.Greeting)
			}
			ended := false // the stream has ended: the session has too
			for i, c := range sc.Calls {
				methods[c.Method]++
				if !ended && c.Before > 0 {
					conn.preload(c.Before)
				}
				conn.arm(c.window(stream))
				res, err := invoke(ctx, s, c)
				sent := conn.sent()
				what := c.Method
				if ended {
					// hyperdeckwire writes into the socket it found
					// closed; the Session has ended and sends nothing.
					// Closing again does nothing, in both.
					if c.Method == "close" && err == nil && len(sent) == 0 {
						continue
					}
					if !errors.Is(err, ErrClosed) || len(sent) > 0 {
						t.Errorf("call %d %s after the stream ended: %v, sent %q", i, what, err, sent)
					}
					continue
				}
				if want, _ := hex.DecodeString(c.Sent); !bytes.Equal(sent, want) {
					t.Errorf("call %d %s sent %q, want %q", i, what, sent, want)
				}
				if !sameError(t, what, err, c.Error) {
					continue
				}
				if c.Error != nil {
					ended = ended || c.Error.Class == "ConnectionError" || c.Method == "close"
					continue
				}
				if c.Method == "close" {
					ended = true
				}
				sameResult(t, what, res, c.Result)
			}
		})
	}
	for _, m := range []string{"device_info", "remote_info", "slot_info", "transport_info", "configuration",
		"set_configuration", "disk_list", "clips_count", "clips_get", "stop", "play", "pause", "clips_clear",
		"clips_add", "clips_remove", "goto_clip", "remote_enable", "slot_select", "ping", "close"} {
		if methods[m] == 0 {
			t.Errorf("no case calls %s", m)
		}
	}
}

// invoke calls the Session method hyperdeckwire's method maps to.
func invoke(ctx context.Context, s *Session, c oracleCall) (any, error) {
	intArg := func(name string) *int {
		raw, ok := c.Args[name]
		if !ok {
			return nil
		}
		var n int
		json.Unmarshal(raw, &n)
		return &n
	}
	boolArg := func(name string) bool {
		var b bool
		json.Unmarshal(c.Args[name], &b)
		return b
	}
	switch c.Method {
	case "device_info":
		return s.DeviceInfo(ctx)
	case "remote_info":
		return s.RemoteInfo(ctx)
	case "slot_info":
		return s.SlotInfo(ctx, intArg("slot_id"))
	case "transport_info":
		return s.TransportInfo(ctx)
	case "configuration":
		return s.Configuration(ctx)
	case "set_configuration":
		var pairs [][2]any
		json.Unmarshal(c.Args["params"], &pairs)
		var ss []Setting
		for _, p := range pairs {
			v := p[1]
			if f, ok := v.(float64); ok {
				v = int(f)
			}
			ss = append(ss, Setting{p[0].(string), v})
		}
		return nil, s.SetConfiguration(ctx, ss...)
	case "disk_list":
		return s.DiskList(ctx, intArg("slot_id"))
	case "clips_count":
		return s.ClipsCount(ctx)
	case "clips_get":
		return s.ClipsGet(ctx)
	case "stop":
		return nil, s.Stop(ctx)
	case "play":
		return nil, s.Play(ctx, PlayOptions{Loop: boolArg("loop"), SingleClip: boolArg("single_clip"),
			Speed: intArg("speed"), ClipID: intArg("clip_id")})
	case "pause":
		return nil, s.Pause(ctx)
	case "clips_clear":
		return nil, s.ClipsClear(ctx)
	case "clips_add":
		var name string
		json.Unmarshal(c.Args["name"], &name)
		return nil, s.ClipsAdd(ctx, name, intArg("before_clip_id"))
	case "clips_remove":
		return nil, s.ClipsRemove(ctx, *intArg("clip_id"))
	case "goto_clip":
		return nil, s.GotoClip(ctx, *intArg("clip_id"))
	case "remote_enable":
		return nil, s.RemoteEnable(ctx, boolArg("enable"))
	case "slot_select":
		return nil, s.SlotSelect(ctx, *intArg("slot_id"))
	case "ping":
		return nil, s.Ping(ctx)
	case "close":
		return nil, s.Close()
	}
	panic("no method " + c.Method)
}

// sameError checks err against what hyperdeckwire raised (nil: nothing)
// and reports whether they agree.
func sameError(t *testing.T, what string, err error, want *oracleError) bool {
	t.Helper()
	ok := true
	switch {
	case want == nil:
		ok = err == nil
	case want.Class == "HyperdeckError":
		var de *Error
		ok = errors.As(err, &de) && de.Code == want.Code && de.Text == want.Text && de.Error() == want.Message
	case want.Class == "ConnectionError":
		ok = errors.Is(err, ErrClosed) && strings.Contains(err.Error(), want.Message)
	case want.Class == "OSError" && want.Message == "not connected":
		ok = errors.Is(err, ErrClosed)
	case want.Class == "OSError" && strings.HasPrefix(want.Message, "unexpected greeting: "):
		ok = errors.Is(err, ErrGreeting) && strings.TrimPrefix(err.Error(), "hyperdeck: ") == want.Message
	case want.Class == "OSError":
		ok = errors.Is(err, ErrMalformed) && strings.TrimPrefix(err.Error(), "hyperdeck: ") == want.Message
	case want.Class == "ValueError":
		ok = err != nil && strings.HasSuffix(err.Error(), ": "+want.Message)
	default:
		ok = false
	}
	if !ok {
		t.Errorf("%s: error %v, want %+v", what, err, want)
	}
	return ok
}

func sameResult(t *testing.T, what string, got any, want json.RawMessage) {
	t.Helper()
	var w any
	switch got.(type) {
	case map[string]string:
		var m map[string]string
		json.Unmarshal(want, &m)
		w = m
	case []Clip:
		var cs []Clip
		json.Unmarshal(want, &cs)
		if cs == nil {
			cs = []Clip{}
		}
		w = cs
	case int:
		var n int
		json.Unmarshal(want, &n)
		w = n
	case nil:
		if string(want) != "null" && len(want) > 0 {
			t.Errorf("%s returned nothing, want %s", what, want)
		}
		return
	}
	if !reflect.DeepEqual(got, w) {
		t.Errorf("%s = %#v, want %s", what, got, want)
	}
}

func TestParsersMatchHyperdeckwire(t *testing.T) {
	o := loadOracle(t)
	diskRaised := map[string]bool{}
	for _, p := range o.Parsers {
		if p.Fn == "_parse_disk_line" {
			diskRaised[p.Line] = p.Error != nil
		}
	}
	for _, p := range o.Parsers {
		var got Clip
		var err error
		switch p.Fn {
		case "_looks_like_clip_line":
			var want bool
			json.Unmarshal(p.Result, &want)
			if g := looksLikeClipLine(p.Line); g != want {
				// str.isdigit() counts digits int() refuses: such a line
				// is skipped by the parser instead of the filter.
				if !(want && diskRaised[p.Line]) {
					t.Errorf("looksLikeClipLine(%q) = %v, want %v", p.Line, g, want)
				}
			}
			continue
		case "_parse_disk_line":
			got, err = parseDiskLine(p.Line)
		case "_parse_clips_v1_line":
			got, err = parseClipsLine(p.Line)
		}
		if p.Error != nil {
			if err == nil || err.Error() != p.Error.Message {
				t.Errorf("%s(%q): %v, want %s", p.Fn, p.Line, err, p.Error.Message)
			}
			continue
		}
		var want Clip
		json.Unmarshal(p.Result, &want)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s(%q) = %+v, %v; want %s", p.Fn, p.Line, got, err, p.Result)
		}
	}
	for _, p := range o.Params {
		if got := (Response{Lines: p.Lines}).Params(); !reflect.DeepEqual(got, p.Result) {
			t.Errorf("params(%q) = %q, want %q", p.Lines, got, p.Result)
		}
	}
}
