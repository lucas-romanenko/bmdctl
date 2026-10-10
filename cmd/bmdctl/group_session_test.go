// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// Group a, end to end against the fake switcher: status, connect,
// disconnect, info, state, watch.
func TestSessionCommands(t *testing.T) {
	b := newBench(t)
	b.must(with("program", "--source", "CAM1")...)
	b.must(with("preview", "--source", "Camera 2")...)

	out := b.must(with("status")...)
	for _, want := range []string{"session     192.0.2.10:9910", "ATEM 1 M/E Constellation HD", "1080p50",
		"program 1 (Camera 1)", "preview 2 (Camera 2)", "me 1 key 0  on air", "dsk 0", "aux 0", "macro"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	var st struct {
		Result engine.StatusResult `json:"result"`
	}
	json.Unmarshal([]byte(b.must(with("status", "--json")...)), &st)
	if len(st.Result.MEs) != 1 || st.Result.MEs[0].ME != 1 || st.Result.MEs[0].Program.ID != 1 || st.Result.MEs[0].Program.Name != "Camera 1" ||
		st.Result.MEs[0].Transition.Style != "dip" || len(st.Result.MEs[0].Keyers) != 4 || len(st.Result.DSKs) != 2 {
		t.Errorf("status --json: %+v", st.Result)
	}

	out = b.must(with("info")...)
	for _, want := range []string{"product       ATEM 1 M/E Constellation HD (model 27)", "video mode    1080p50 (mode 12), 1920x1080",
		"m/es          1, upstream keyers 4", "audio         fairlight", "inputs        8"} {
		if !strings.Contains(out, want) {
			t.Errorf("info lacks %q:\n%s", want, out)
		}
	}

	if out := b.must(with("state", "--path", "mes.0.program.source")...); out != "value  1\n" {
		t.Errorf("state leaf: %q", out)
	}
	if out := b.must(with("state", "--path", "MES.0.Program", "--json")...); !strings.Contains(out, `"result":{"index":0,"source":1}`) {
		t.Errorf("state --json: %q", out)
	}
	if out := b.must(with("state", "--json")...); !strings.Contains(out, `"product":{"name":"ATEM 1 M/E Constellation HD"`) {
		t.Errorf("whole state: %.200s", out)
	}
	for path, msg := range map[string]string{"nope": "the state has no \"nope\"", "mes.7": "is a list of 1", "mes.0.program.source.x": "not an object or a list"} {
		if code, _, errOut := b.run(with("state", "--path", path)...); code != exitUsage || !strings.Contains(errOut, msg) {
			t.Errorf("--path %s: %d %q", path, code, errOut)
		}
	}

	// connect pins the session; disconnect closes it; the next command
	// opens a new one.
	if out := b.must(with("connect")...); !strings.Contains(out, "connected  192.0.2.10:9910  ready, pinned") {
		t.Errorf("connect: %q", out)
	}
	if out := b.must("engine", "status"); !strings.Contains(out, "ready, pinned") {
		t.Errorf("engine status after connect:\n%s", out)
	}
	if out := b.must(with("disconnect")...); out != "disconnected 192.0.2.10:9910\n" {
		t.Errorf("disconnect: %q", out)
	}
	if out := b.must(with("disconnect")...); out != "no session to 192.0.2.10:9910\n" {
		t.Errorf("disconnect again: %q", out)
	}
	if n := len(b.switchers()); n != 1 {
		t.Errorf("disconnect dialled: %d sessions", n)
	}
	b.must(with("cut")...)
	if n := len(b.switchers()); n != 2 {
		t.Errorf("%d sessions after disconnect", n)
	}
	if out := b.must(with("connect", "--pin", "false")...); !strings.Contains(out, "closes after") {
		t.Errorf("connect --pin false: %q", out)
	}
}

// lineWriter passes each line written to a channel.
type lineWriter struct {
	pw    *io.PipeWriter
	lines chan string
}

func newLineWriter() *lineWriter {
	pr, pw := io.Pipe()
	w := &lineWriter{pw: pw, lines: make(chan string, 100)}
	go func() {
		s := bufio.NewScanner(pr)
		for s.Scan() {
			w.lines <- s.Text()
		}
		close(w.lines)
	}()
	return w
}

func (w *lineWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }

func (w *lineWriter) next(t *testing.T) string {
	t.Helper()
	select {
	case l := <-w.lines:
		return l
	case <-time.After(5 * time.Second):
		t.Fatal("no line")
	}
	return ""
}

// watch streams each change; --filter narrows it, --count ends it, and
// --json prints each engine frame as it is.
func TestWatch(t *testing.T) {
	b := newBench(t)
	c, _, _ := b.cli("")
	w := newLineWriter()
	c.stdout = w
	done := make(chan int)
	go func() { done <- c.run(with("watch", "--json", "--filter", "PrgI,KeOn:0:1", "--count", "2")) }()
	first := w.next(t)
	if !strings.HasPrefix(first, `{"ok":true,"confirmed":false,"result":{"kind":"watching"`) {
		t.Fatalf("first frame: %s", first)
	}
	b.must(with("preview", "--source", "1")...) // filtered out
	b.must(with("program", "--source", "1")...)
	b.must(with("usk", "on-air", "--keyer", "1", "--on-air")...)
	for _, want := range []string{`"key":"PrgI:0","value":{"index":0,"source":1}`, `"key":"KeOn:0:1","value":{"index":0,"keyer":1,"enabled":true}`} {
		var frame struct {
			OK     bool `json:"ok"`
			Result struct {
				Kind string `json:"kind"`
			} `json:"result"`
		}
		line := w.next(t)
		if json.Unmarshal([]byte(line), &frame); !frame.OK || frame.Result.Kind != "state" || !strings.Contains(line, want) {
			t.Errorf("frame %s, want %s", line, want)
		}
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("watch exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop at --count")
	}
	// Human form: time, key, value.
	c, _, _ = b.cli("")
	w, ew := newLineWriter(), newLineWriter()
	c.stdout, c.stderr = w, ew
	go func() { done <- c.run(with("watch", "--filter", "PrgI", "--count", "1")) }()
	if l := ew.next(t); l != "watching; Ctrl-C to stop" {
		t.Errorf("stderr %q", l)
	}
	b.must(with("program", "--source", "2")...)
	if l := w.next(t); !strings.Contains(l, "  PrgI:0       {\"index\":0,\"source\":2}") {
		t.Errorf("human line %q", l)
	}
	<-done
}
