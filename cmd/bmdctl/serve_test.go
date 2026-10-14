// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/serve"
)

// api serves the HTTP API on loopback against the bench's engine.
func (b *bench) api(t *testing.T) *httptest.Server {
	t.Helper()
	c, _, _ := b.cli("")
	srv := httptest.NewServer(serve.Handler(c.apiConfig(nil)))
	t.Cleanup(srv.Close)
	return srv
}

type apiAnswer struct {
	OK        bool            `json:"ok"`
	Confirmed bool            `json:"confirmed"`
	Result    json.RawMessage `json:"result"`
	Code      string          `json:"code"`
	Error     string          `json:"error"`
	Files     []struct {
		Param string `json:"param"`
		Name  string `json:"name"`
		Data  []byte `json:"data"`
	} `json:"files"`
	Confirm *struct {
		Impact, Question string
	} `json:"confirm"`
}

func call(t *testing.T, method, u, ct string, body io.Reader) (int, apiAnswer, string) {
	t.Helper()
	req, _ := http.NewRequest(method, u, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var a apiAnswer
	json.Unmarshal(raw, &a)
	return resp.StatusCode, a, string(raw)
}

func post(t *testing.T, srv *httptest.Server, path, body string) (int, apiAnswer, string) {
	return call(t, "POST", srv.URL+path, "application/json", strings.NewReader(body))
}

func TestServeCommands(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	srv := b.api(t)

	// A write: the engine's answer as --json prints it.
	code, a, raw := post(t, srv, "/hosts/studio/program", `{"source": "cam1"}`)
	if code != 200 || !a.OK || !a.Confirmed || !strings.HasPrefix(raw, `{"ok":true,"confirmed":true,"result":{`) {
		t.Errorf("program: %d %s", code, raw)
	}
	if len(b.sent("CPgI")) != 1 {
		t.Errorf("CPgI sent %d times", len(b.sent("CPgI")))
	}
	// Numbers, booleans and lists as JSON; a path's words as the URL's.
	if code, a, raw := post(t, srv, "/hosts/stu/usk/on-air", `{"keyer": 1, "on-air": true}`); code != 200 || !a.OK {
		t.Errorf("usk on-air: %d %s", code, raw)
	}
	if code, _, raw := post(t, srv, "/hosts/studio/transition/mix", `{"rate": 12}`); code != 200 {
		t.Errorf("transition mix: %d %s", code, raw)
	}
	// What --json says goes wrong, with an HTTP status for it.
	for _, c := range []struct {
		path, body string
		status     int
		code, msg  string
	}{
		{"/hosts/studio/program", `{}`, 400, "usage", "missing <source>"},
		{"/hosts/studio/program", `{"source": "cam2", "nonsense": 1}`, 400, "usage", `program takes no "nonsense"`},
		{"/hosts/studio/program", `[1]`, 400, "usage", "not a JSON object"},
		{"/hosts/studio/frobnicate", `{}`, 404, "usage", `ATEM has no command "frobnicate"`},
		{"/hosts/studio/play", `{}`, 404, "usage", "play is a HyperDeck's command"},
		{"/hosts/no%20one%21/program", `{"source": 1}`, 404, "usage", "neither a host"},
		{"/hosts/studio/usk/1/on-air", `{"on": true}`, 404, "usage", `ATEM has no command "usk 1 on-air"`},
		{"/hosts/studio/watch", `{}`, 400, "usage", "streams: GET /hosts/{host}/events"},
		{"/hosts/studio/video-mode", `{"mode": "525i5994"}`, 409, "destructive", "destructive"},
		{"/hosts/studio/program?bogus=1", `{"source": 1}`, 400, "usage", "the query takes yes, timeout and fps"},
	} {
		code, a, raw := post(t, srv, c.path, c.body)
		if code != c.status || a.OK || a.Code != c.code || !strings.Contains(a.Error, c.msg) {
			t.Errorf("%s %s: %d %s", c.path, c.body, code, raw)
		}
	}
	// A destructive command refused says what it would affect; ?yes=true
	// runs it.
	code, a, _ = post(t, srv, "/hosts/studio/media/clear", `{"slot": 1}`)
	if code != 409 || a.Confirm == nil || a.Confirm.Question == "" {
		t.Errorf("media clear: %d %+v", code, a)
	}
	if code, a, raw := post(t, srv, "/hosts/studio/media/clear?yes=true", `{"slot": 1}`); code != 200 || !a.OK {
		t.Errorf("media clear ?yes: %d %s", code, raw)
	}
	// The hosts, the state.
	code, a, raw = call(t, "GET", srv.URL+"/hosts", "", nil)
	if code != 200 || !strings.Contains(string(a.Result), `"name":"ATEM 1 M/E Constellation HD"`) {
		t.Errorf("hosts: %d %s", code, raw)
	}
	code, a, raw = call(t, "GET", srv.URL+"/hosts/studio/state?path=mes.0.program.source", "", nil)
	if code != 200 || string(a.Result) != "1" {
		t.Errorf("state: %d %s", code, raw)
	}
	if code, _, raw := call(t, "GET", srv.URL+"/nowhere", "", nil); code != 404 || !strings.Contains(raw, "GET /openapi.json lists") {
		t.Errorf("404: %d %s", code, raw)
	}
}

// Files: an upload as multipart/form-data, a download's file in the
// answer.
func TestServeFiles(t *testing.T) {
	b := newBench(t)
	srv := b.api(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("slot", "2")
	mw.WriteField("name", "Logo")
	fw, _ := mw.CreateFormFile("file", "logo.png")
	fw.Write(testPNG(t))
	mw.Close()
	code, a, raw := call(t, "POST", srv.URL+"/hosts/192.0.2.10/media/upload", mw.FormDataContentType(), &body)
	if code != 200 || !a.OK {
		t.Fatalf("upload: %d %s", code, raw)
	}
	code, a, raw = post(t, srv, "/hosts/192.0.2.10/media/download", `{"slot": 2}`)
	if code != 200 || len(a.Files) != 1 || !bytes.HasPrefix(a.Files[0].Data, []byte("\x89PNG")) {
		t.Errorf("download: %d %.200s", code, raw)
	}
	if code, _, raw := post(t, srv, "/hosts/192.0.2.10/media/upload", `{"slot": 2, "file": "x.png"}`); code != 400 ||
		!strings.Contains(raw, "send it as multipart/form-data") {
		t.Errorf("upload as JSON: %d %s", code, raw)
	}
}

// Events: watch's frames, one data: line each, the first "watching".
func TestServeEvents(t *testing.T) {
	b := newBench(t)
	srv := b.api(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/hosts/192.0.2.10/events?count=1&filter=PrgI", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || ct != "text/event-stream" {
		t.Fatalf("%d %s", resp.StatusCode, ct)
	}
	sc := bufio.NewScanner(resp.Body)
	var events []string
	for sc.Scan() {
		if l, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			events = append(events, l)
			if len(events) == 1 {
				post(t, srv, "/hosts/192.0.2.10/program", `{"source": 5}`)
			}
		}
	}
	if len(events) != 2 || !strings.Contains(events[0], `"kind":"watching"`) || !strings.Contains(events[1], `"key":"PrgI:0"`) {
		t.Errorf("events: %q", events)
	}
	if code, _, raw := call(t, "GET", srv.URL+"/hosts/192.0.2.10/events?bogus=1", "", nil); code != 400 || !strings.Contains(raw, "watch takes no such argument") {
		t.Errorf("bad query: %d %s", code, raw)
	}
}

// A HyperDeck host's commands, by its type or with its word in the path.
func TestServeADeck(t *testing.T) {
	b := newBench(t)
	b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	b.must("connect", fakehyperdeck.Address)
	srv := b.api(t)
	if code, a, raw := post(t, srv, "/hosts/"+url.PathEscape("deck 1")+"/play", `{"speed": 50}`); code != 200 || !a.OK || !a.Confirmed {
		t.Errorf("play: %d %s", code, raw)
	}
	if code, _, raw := post(t, srv, "/hosts/deck/hyperdeck/stop", ``); code != 200 {
		t.Errorf("hyperdeck/stop: %d %s", code, raw)
	}
	if code, a, raw := post(t, srv, "/hosts/deck/program", `{"source": 1}`); code != 404 || !strings.Contains(a.Error, "Deck 1 (192.0.2.20) is a HyperDeck: program is an ATEM's command") {
		t.Errorf("program: %d %s", code, raw)
	}
	if code, _, raw := post(t, srv, "/hosts/deck/atem/cut", ``); code != 400 || !strings.Contains(raw, "is a HyperDeck, not an ATEM") {
		t.Errorf("atem/cut: %d %s", code, raw)
	}
	code, a, raw := call(t, "GET", srv.URL+"/hosts/deck/state?path=transport.status", "", nil)
	if code != 200 || string(a.Result) != `"stopped"` {
		t.Errorf("state: %d %s", code, raw)
	}
}

// lockedBuffer is a buffer two goroutines can share: serve writes its
// access lines while the test reads them.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// bmdctl serve itself: it says where it listens, logs each request, and
// stops when interrupted.
func TestServeCommand(t *testing.T) {
	b := newBench(t)
	c, _, _ := b.cli("")
	o := &lockedBuffer{}
	c.stdout = o
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	done := make(chan int)
	go func() { done <- c.run([]string{"serve", "--listen", "127.0.0.1:0"}) }()
	var u string
	eventually(t, "serve to listen", func() bool {
		line, _, _ := strings.Cut(o.String(), "\n")
		u = strings.TrimSuffix(strings.TrimPrefix(line, "serving "), " (no authentication)")
		return strings.HasPrefix(u, "http://127.0.0.1:")
	})
	resp, err := http.Get(u + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cancel()
	if code := <-done; code != 0 || !strings.Contains(o.String(), " GET /openapi.json 200 ") {
		t.Errorf("%d %q", code, o.String())
	}
	if code, _, e := b.run("engine", "--listen", "127.0.0.1:0"); code != exitUsage || !strings.Contains(e, "--listen goes with --serve") {
		t.Errorf("engine --listen: %d %q", code, e)
	}
}
