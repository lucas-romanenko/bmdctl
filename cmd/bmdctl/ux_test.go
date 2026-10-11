// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// fields splits a line at its column gaps (two spaces or more), less the
// time at the end of a write's line.
func fields(line string) []string {
	f := regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(line), -1)
	if n := len(f); n > 0 && strings.HasSuffix(f[n-1], " ms") {
		f = f[:n-1]
	}
	return f
}

const fake = "ATEM 1 M/E Constellation HD (192.0.2.10)"

// A write's line: the switcher, the command and where, what changed
// before → after with source names, confirmed or sent.
func TestWriteLines(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		line []string
		want []string
	}{
		// The dump: program CAM2, preview CAM1.
		{[]string{"program", "cam1"}, []string{"program", "ME1", "CAM2 → CAM1", "confirmed"}},
		{[]string{"preview", "5"}, []string{"preview", "ME1", "CAM1 → 5", "confirmed"}},
		{[]string{"preview", "5"}, []string{"preview", "ME1", "5", "confirmed"}}, // nothing changed
		{[]string{"usk", "2", "on-air", "on"}, []string{"usk 2 on-air", "ME1", "off → on", "confirmed"}},
		{[]string{"usk", "1", "toggle"}, []string{"usk 1 toggle", "ME1", "on → off", "confirmed"}},
		{[]string{"dsk", "on-air", "off"}, []string{"dsk on-air", "DSK1", "on → off", "confirmed"}},
		{[]string{"aux", "2", "cam1"}, []string{"aux 2", "PGM → CAM1", "confirmed"}},
		{[]string{"transition", "mix", "0:12"}, []string{"transition mix", "ME1", "1:00 → 0:12", "confirmed"}},
		{[]string{"transition", "rate", "1:00"}, []string{"transition rate", "ME1", "1:05 → 1:00", "confirmed"}},
		{[]string{"transition", "next", "--toggle", "key2"},
			[]string{"transition next", "ME1", "next background+key1 → background+key1+key2", "confirmed"}},
		{[]string{"color", "2", "--hue", "120"}, []string{"color 2", "hue 300 → 120", "confirmed"}},
		{[]string{"input", "rename", "Camera 2", "--long-name", "Stage Left", "--short-name", "STGL"},
			[]string{"input rename CAM2", `long-name "Camera 2" → "Stage Left", short-name "CAM2" → "STGL"`, "confirmed"}},
		// No echo: what it asks for, sent.
		{[]string{"cut"}, []string{"cut", "ME1", "CAM1 → 5", "sent"}},
		{[]string{"ftb", "toggle"}, []string{"ftb toggle", "ME1", "off → black", "sent"}},
		{[]string{"macro", "sleep", "25"}, []string{"macro sleep", "25", "sent"}},
		// After is what the switcher reports: the fake answers a wipe with
		// the settings it holds, unchanged.
		{[]string{"transition", "wipe", "--rate", "10"}, []string{"transition wipe", "ME1", "rate 0:20", "confirmed"}},
	} {
		out := b.must(with(c.line...)...)
		if got := fields(out); !reflect.DeepEqual(got, append([]string{fake}, c.want...)) {
			t.Errorf("%v: %q", c.line, got)
		}
	}
}

// Colour on a terminal: program red, preview green, sent dim, errors red
// on stderr. Not with --no-color, NO_COLOR or TERM=dumb, nor --json.
func TestColour(t *testing.T) {
	b := newBench(t)
	run := func(tty bool, args ...string) (string, string) {
		cl, o, e := b.cli("")
		cl.tty, cl.ttyErr = tty, tty
		cl.run(with(args...))
		return o.String(), e.String()
	}
	if out, _ := run(true, "program", "cam1"); !strings.Contains(out, "CAM2 → \x1b[31mCAM1\x1b[0m") {
		t.Errorf("program: %q", out)
	}
	if out, _ := run(true, "preview", "cam2"); !strings.Contains(out, "CAM1 → \x1b[32mCAM2\x1b[0m") {
		t.Errorf("preview: %q", out)
	}
	if out, _ := run(true, "cut"); !strings.Contains(out, "\x1b[2msent     \x1b[0m") {
		t.Errorf("cut: %q", out)
	}
	if _, errOut := run(true, "program", "Nobody"); !strings.HasPrefix(errOut, "\x1b[31mbmdctl atem program: ") {
		t.Errorf("error: %q", errOut)
	}
	for _, c := range []struct {
		env  map[string]string
		args []string
	}{
		{nil, []string{"--no-color"}},
		{map[string]string{"NO_COLOR": "1"}, nil},
		{map[string]string{"TERM": "dumb"}, nil},
		{nil, []string{"--json"}},
	} {
		for k, v := range c.env {
			b.env[k] = v
		}
		out, _ := run(true, append([]string{"program", "1"}, c.args...)...)
		_, errOut := run(true, append([]string{"program", "Nobody"}, c.args...)...)
		if strings.Contains(out+errOut, "\x1b[") {
			t.Errorf("%v %v: coloured: %q %q", c.env, c.args, out, errOut)
		}
		for k := range c.env {
			delete(b.env, k)
		}
	}
	// Not on a pipe.
	if out, errOut := run(false, "program", "cam1"); strings.Contains(out+errOut, "\x1b[") {
		t.Errorf("pipe: %q", out)
	}
	// exec passes --no-color to its lines.
	script := b.dir + "/cue.txt"
	os.WriteFile(script, []byte("program cam1\ncut\n"), 0o644)
	if out, _ := run(true, "exec", script, "--no-color"); strings.Contains(out, "\x1b[") || strings.Count(out, "\n") != 2 {
		t.Errorf("exec --no-color: %q", out)
	}
}

// Opening a session says so on stderr, then the model and video mode;
// a warm one says nothing, nor does --json or --quiet.
func TestConnecting(t *testing.T) {
	b := newBench(t)
	if _, _, errOut := b.run(with("program", "1")...); errOut != "connecting to 192.0.2.10 …\nATEM 1 M/E Constellation HD, 1080p50\n" {
		t.Errorf("first: %q", errOut)
	}
	if _, _, errOut := b.run(with("program", "2")...); errOut != "" {
		t.Errorf("warm: %q", errOut)
	}
	for _, flag := range []string{"--json", "-q"} {
		b.must(with("disconnect")...)
		code, out, errOut := b.run(with("program", "1", flag)...)
		if code != 0 || errOut != "" || flag == "--json" && strings.Count(out, "\n") != 1 || flag == "-q" && out != "" {
			t.Errorf("%s: %d %q %q", flag, code, out, errOut)
		}
	}
	// A named host by its name; and reads open sessions too.
	b.must("hosts", "add", "192.0.2.11", "--as", "studio")
	b.must("--host", "studio", "disconnect")
	code, out, errOut := b.run("--host", "studio", "info")
	if code != 0 || errOut != "connecting to studio (192.0.2.11) …\nATEM 1 M/E Constellation HD, 1080p50\n" ||
		!strings.HasPrefix(out, "studio (192.0.2.11)  ATEM 1 M/E Constellation HD  1080p50\n") {
		t.Errorf("named: %d %q %q", code, out, errOut)
	}
}

// The before → after column is as wide as a change between the
// switcher's two longest source names, so the status lines up.
func TestChangeColumn(t *testing.T) {
	b := newBench(t)
	// The fake's sources go by their short names, four letters at most.
	if out := b.must(with("program", "cam1")...); !strings.Contains(out, "  CAM2 → CAM1  confirmed ") {
		t.Errorf("program: %q", out)
	}
	if out := b.must(with("preview", "mp1")...); !strings.Contains(out, "  CAM1 → MP1   confirmed ") {
		t.Errorf("preview: %q", out)
	}
	// A source with no short name goes by its long one; the next line's
	// column makes room for it on both sides of the arrow.
	b.must(with("input", "rename", "Camera 2", "--long-name", "Stage Left Wide", "--short-name", "")...)
	if out := b.must(with("program", "1")...); !strings.Contains(out, "  "+padRight("CAM1", 2*15+3)+"  confirmed ") {
		t.Errorf("long name: %q", out)
	}
}

// bmdctl alone: the engine, then a line per host with its model, video
// mode, how its session stands and which is the default.
func TestOverview(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.20", "--as", "deck", "--device", "hyperdeck")
	b.must("hosts", "add", "192.0.2.11", "--as", "spare")
	b.must("hosts", "default", "studio")
	b.must("--host", "spare", "disconnect")
	b.must("--host", "192.0.2.50", "cut") // a session to a switcher with no host
	code, out, errOut := b.run()
	collapse := func(s string) string { return regexp.MustCompile(` {2,}`).ReplaceAllString(s, "  ") }
	want := "engine  running, pid \\d+, up \\ds, 2 sessions\n" +
		"studio \\(192.0.2.10\\)  ATEM 1 M/E Constellation HD  1080p50  ready, idle \\ds  \\(default\\)\n" +
		"deck \\(192.0.2.20\\)  hyperdeck  no commands yet\n" +
		"spare \\(192.0.2.11\\)  no session\n" +
		"ATEM 1 M/E Constellation HD \\(192.0.2.50\\)  ATEM 1 M/E Constellation HD  1080p50  ready, idle \\ds\n"
	if code != 0 || errOut != "" || !regexp.MustCompile("^"+want+"$").MatchString(collapse(out)) {
		t.Errorf("overview: %d %q\n%s", code, errOut, out)
	}
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Engine *engine.Status `json:"engine"`
			Hosts  []overviewHost `json:"hosts"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(b.must("--json")), &r); err != nil || !r.OK || r.Result.Engine == nil || len(r.Result.Hosts) != 4 ||
		!r.Result.Hosts[0].Default || r.Result.Hosts[0].Session == nil || r.Result.Hosts[0].Session.VideoMode != "1080p50" ||
		r.Result.Hosts[2].Session != nil {
		t.Errorf("--json: %+v %v", r, err)
	}
	if out := b.must("-q"); out != "" {
		t.Errorf("-q: %q", out)
	}
	if out := b.must("help"); !strings.HasPrefix(out, "usage: bmdctl") {
		t.Errorf("help: %q", out)
	}
	// The engine's commit; an engine of another build says so, whatever
	// its commit (two builds of one dirty tree share one).
	b.engine().Stop()
	eventually(t, "the engine to go", func() bool { _, err := os.Stat(b.env["BMDCTL_SOCKET"]); return os.IsNotExist(err) })
	c, o, _ := b.cli("")
	c.run(nil)
	if !strings.HasPrefix(o.String(), "engine  not running; the next command starts it\n") || !strings.Contains(o.String(), "studio (192.0.2.10)") {
		t.Errorf("no engine:\n%s", o)
	}
	if err := b.startEngine(engine.Config{BuildID: "aaa/bbb/ccc/0123456789abcdef", Commit: "274d6a2"}); err != nil {
		t.Fatal(err)
	}
	c, o, _ = b.cli("")
	c.build = func() string { return "mine" }
	c.run(nil)
	if !strings.Contains(o.String(), ", 0 sessions, commit 274d6a2 (not this bmdctl's build)\n") || strings.Contains(o.String(), "aaa") {
		t.Errorf("another build:\n%s", o)
	}
	c, o, _ = b.cli("")
	c.build = func() string { return "aaa/bbb/ccc/0123456789abcdef" }
	c.run(nil)
	if !strings.HasPrefix(o.String(), "engine  running, pid ") || !strings.Contains(o.String(), ", commit 274d6a2\n") {
		t.Errorf("this build:\n%s", o)
	}
	// status says which commit the engine runs too.
	if out := b.must(with("status")...); !strings.Contains(out, "\nengine      commit 274d6a2\n") {
		t.Errorf("status:\n%s", out)
	}
	// With no hosts: how to add one, and where the commands are.
	os.Remove(b.dir + "/config/bmdctl/hosts.yml")
	if out := b.must(); !strings.HasSuffix(out, "\nno hosts: bmdctl hosts add ADDRESS, or bmdctl hosts discover\nbmdctl help lists the commands\n") {
		t.Errorf("no hosts:\n%s", out)
	}
}

// A session that drops: watch says so and says when it is back, and a
// command meanwhile waits, saying it connects.
func TestReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 10 s")
	}
	b := newBench(t)
	b.must(with("program", "1")...)
	sw := b.switchers()[0]
	c, _, _ := b.cli("")
	w, ew := newLineWriter(), newLineWriter()
	c.stdout, c.stderr = w, ew
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	done := make(chan int)
	go func() { done <- c.run(with("watch", "--filter", "PrgI")) }()
	ew.next(t)
	w.next(t) // the header
	sw.Silence(true)
	if l := w.next2(t, 10*time.Second); !strings.HasSuffix(l, "  session to "+fake+" dropped") {
		t.Errorf("dropped: %q", l)
	}
	cmd := make(chan [3]string)
	go func() {
		code, out, errOut := b.run(with("program", "2")...)
		cmd <- [3]string{string(rune('0' + code)), out, errOut}
	}()
	time.Sleep(200 * time.Millisecond)
	sw.Silence(false)
	// The dump coming back isn't printed; what changed while it was away
	// is: the fake's dump has program CAM2, where program 1 left CAM1.
	if l := w.next2(t, 10*time.Second); !strings.HasSuffix(l, "  reconnected") {
		t.Errorf("reconnected: %q", l)
	}
	if l := w.next(t); !strings.HasSuffix(l, "  program  ME1  CAM1 → CAM2") {
		t.Errorf("after: %q", l)
	}
	r := <-cmd
	if r[0] != "0" || r[2] != "connecting to 192.0.2.10 …\nATEM 1 M/E Constellation HD, 1080p50\n" {
		t.Errorf("command meanwhile: %q", r)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("watch exit %d", code)
	}
}

// next2 is next with a longer wait.
func (w *lineWriter) next2(t *testing.T, d time.Duration) string {
	t.Helper()
	select {
	case l := <-w.lines:
		return l
	case <-time.After(d):
		t.Fatal("no line")
	}
	return ""
}
