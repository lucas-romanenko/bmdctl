// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// What a person sees at a terminal is snapshot-tested against the golden
// files in testdata/terminal: each command line runs with stdout and
// stderr forced to be terminals (cli.tty, cli.ttyErr), colour codes are
// written as tags (visible) and times as #. -update rewrites them:
//
//	go test ./cmd/bmdctl -run TestTerminal -update

// transcript runs command lines at a forced terminal and records what
// each printed: "$ bmdctl ..." then stdout's lines, then stderr's after
// "! ", then the exit status if it isn't 0.
type transcript struct {
	b   *bench
	env map[string]string // set for the next line only
	out strings.Builder
}

func (tr *transcript) run(args ...string) {
	tr.b.t.Helper()
	c, o, e := tr.b.cli("")
	c.tty, c.ttyErr = true, true
	c.spinAfter = time.Hour // no frame: what a wait leaves is the point
	for k, v := range tr.env {
		tr.b.env[k] = v
	}
	code := c.run(with(args...))
	for k := range tr.env {
		delete(tr.b.env, k)
	}
	tr.env = nil
	tr.record(args, code, o.String(), e.String())
}

func (tr *transcript) record(args []string, code int, stdout, stderr string) {
	fmt.Fprintf(&tr.out, "$ bmdctl %s\n", strings.Join(args, " "))
	for _, l := range lines(stdout) {
		fmt.Fprintf(&tr.out, "%s\n", l)
	}
	for _, l := range lines(stderr) {
		fmt.Fprintf(&tr.out, "! %s\n", l)
	}
	if code != 0 {
		fmt.Fprintf(&tr.out, "[exit %d]\n", code)
	}
	tr.out.WriteString("\n")
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(normalise(visible(s)), "\n"), "\n")
}

var codeTags = map[string]string{"0": "</>", "1": "<b>", "2": "<dim>", "31": "<red>", "32": "<green>"}

// visible writes the terminal's codes as tags: <red>, <green>, <b>,
// <dim> and </> for the colours, <clear> and <cr> for the spinner's.
// Any other code is a test failure waiting to happen: <?...>.
func visible(s string) string {
	s = strings.ReplaceAll(s, "\x1b[2K", "<clear>")
	s = strings.ReplaceAll(s, "\r", "<cr>")
	return regexp.MustCompile(`\x1b\[([0-9;]*)m`).ReplaceAllStringFunc(s, func(m string) string {
		if tag, ok := codeTags[m[2:len(m)-1]]; ok {
			return tag
		}
		return "<?" + m[2:] + ">"
	})
}

// normalise replaces what changes from run to run: times taken, the
// clock, the engine's pid and uptime.
func normalise(s string) string {
	for _, r := range []struct{ re, with string }{
		{` +\d+ ms`, "  # ms"}, // a write's time, padded to five
		{`\d+ ms`, "# ms"},
		{`idle \d+s`, "idle #s"},
		{`up \d+s`, "up #s"},
		{`pid \d+`, "pid #"},
		{`drops at \d\d:\d\d`, "drops at HH:MM"},
	} {
		s = regexp.MustCompile(r.re).ReplaceAllString(s, r.with)
	}
	return s
}

// golden compares got with a golden file, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "terminal", name)
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s is stale (go test ./cmd/bmdctl -run TestTerminal -update); got:\n%s", path, got)
	}
}

// Writes: the prompt, the switcher's name bold and its address dim, the
// change with program and on air red and preview green, confirmed plain,
// sent and the time dim. Opening the session says "connected to" in the
// spinner's place.
func TestTerminalWrites(t *testing.T) {
	tr := &transcript{b: newBench(t)}
	tr.run("program", "cam1")
	tr.run("preview", "cam2")
	tr.run("usk", "2", "on-air", "on")
	tr.run("usk", "2", "on-air", "off")
	tr.run("dsk", "auto")
	tr.run("cut")
	tr.run("ftb", "toggle")
	tr.run("aux", "2", "cam1")
	tr.run("transition", "mix", "0:12")
	tr.run("color", "2", "--hue", "120")
	tr.run("connect")
	tr.run("disconnect")
	// Without colour, at a terminal: the same lines, plain.
	tr.env = map[string]string{"NO_COLOR": "1"}
	tr.run("program", "cam2")
	golden(t, "writes.golden", tr.out.String())
}

// Reads: labels dim, values plain, empty slots a dim -, program and on
// air red, preview green.
func TestTerminalReads(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	tr.run("connect")
	tr.run("usk", "1", "on-air", "on")
	tr.out.Reset()
	// The dump has no tally by source; a switcher sends one: CAM2 on
	// program, CAM1 and media player 1 on preview.
	b.switchers()[0].Send(fakeatem.Command("TlSr", []byte{0, 3, 0, 2, 1, 0, 1, 2, 0x0b, 0xc2, 2, 0}))
	eventually(t, "the tally", func() bool { return b.must(with("state", "--path", "tally.source")...) != "null\n" })
	for _, line := range [][]string{{"info"}, {"status"}, {"inputs"}, {"media", "list"}, {"macro", "list", "--all"}, {"video-modes"}} {
		tr.run(line...)
	}
	golden(t, "reads.golden", tr.out.String())
}

// Errors: "error:" red, the message, the next step dim on its own line.
func TestTerminalErrors(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	tr.run("program", "Nobody")
	tr.run("program")
	tr.run("media", "clear", "1")
	tr.env = map[string]string{"NO_COLOR": "1"}
	tr.run("program", "Nobody")
	b.mu.Lock()
	b.dialErr = fmt.Errorf("no route to host")
	b.mu.Unlock()
	tr.run("--host", "192.0.2.99", "program", "1")
	golden(t, "errors.golden", tr.out.String())
}

// The banner: on bmdctl alone, and once when a command starts the
// engine; never otherwise, nor on a pipe.
func TestTerminalBanner(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.11", "--as", "spare")
	b.must("hosts", "default", "studio")
	tr := &transcript{b: b}
	run := func(c *cli, o, e *bytes.Buffer, args ...string) {
		c.tty, c.ttyErr, c.spinAfter = true, true, time.Hour
		tr.record(args, c.run(args), o.String(), e.String())
	}
	c, o, e := b.cli("")
	run(c, o, e)
	tr.run("program", "cam1") // an ordinary command: no banner
	// A command that starts the engine.
	b.engine().Stop()
	eventually(t, "the engine to go", func() bool { _, err := os.Stat(b.env["BMDCTL_SOCKET"]); return os.IsNotExist(err) })
	c, o, e = b.cli("")
	c.spawn = func(string, []string, []string) error { return b.startEngine(engine.Config{}) }
	run(c, o, e, "--host", "studio", "program", "cam2")
	golden(t, "banner.golden", tr.out.String())

	// Once per invocation: an exec whose first line starts the engine.
	b.engine().Stop()
	eventually(t, "the engine to go", func() bool { _, err := os.Stat(b.env["BMDCTL_SOCKET"]); return os.IsNotExist(err) })
	script := filepath.Join(b.dir, "cue.txt")
	os.WriteFile(script, []byte("program cam1\npreview cam2\n"), 0o644)
	c, o, _ = b.cli("")
	c.tty, c.ttyErr, c.spinAfter = true, true, time.Hour
	c.spawn = func(string, []string, []string) error { return b.startEngine(engine.Config{}) }
	if code := c.run([]string{"--host", "studio", "exec", script}); code != 0 || strings.Count(o.String(), "bmd\x1b[0m\x1b[1mctl") != 1 {
		t.Errorf("exec: %d %q", code, o)
	}
	// Not on a pipe.
	if out := b.must(); !strings.HasPrefix(out, "engine  ") {
		t.Errorf("pipe: %q", out)
	}
}

func TestBannerLines(t *testing.T) {
	plain := func(_, s string) string { return s }
	got := strings.Join(bannerLines(plain, "v1.4.0", "877d29a", "~/.config/bmdctl/hosts.yml"), "\n")
	if want := "> bmdctl\n  v1.4.0, commit 877d29a, hosts ~/.config/bmdctl/hosts.yml"; got != want {
		t.Errorf("%q", got)
	}
	if got := tildePath("/home/x/.config/bmdctl/hosts.yml", "/home/x"); got != filepath.FromSlash("~/.config/bmdctl/hosts.yml") {
		t.Errorf("tilde: %q", got)
	}
	if got := tildePath("/etc/bmdctl/hosts.yml", "/home/x"); got != "/etc/bmdctl/hosts.yml" {
		t.Errorf("not home: %q", got)
	}
	for v, pseudo := range map[string]bool{"v0.0.0-20261011010118-274d6a26df98+dirty": true, "v1.2.1-0.20261011010118-274d6a26df98": true,
		"v1.2.0": false, "v1.2.0+dirty": false} {
		if pseudoVersion.MatchString(v) != pseudo {
			t.Errorf("%s: pseudo %v", v, !pseudo)
		}
	}
}

// watch: the time dim, program red and preview green, the session's own
// lines bold.
func TestTerminalWatch(t *testing.T) {
	var st atem.State
	json.Unmarshal([]byte(`{"mes":[{"program":{"index":0,"source":3},"preview":{"index":0,"source":5},
		"transition":{"settings":{"index":0,"style":0}}}],
		"inputs":{"3":{"index":3,"name":"Camera 3","short_name":"CAM3"},"5":{"index":5,"name":"Camera 5","short_name":"CAM5"}},
		"tally":{"source":{"num":2,"tally":{"3":{"program":true,"preview":false},"5":{"program":false,"preview":true}}}}}`), &st)
	var o, e bytes.Buffer
	c := newCLI(&o, &e)
	c.env = func(string) string { return "" }
	c.tty = true
	p := &printer{c: c, t: target{Address: "192.0.2.10:9910"}, watch: newWatchView(&st)}
	var out strings.Builder
	for _, ev := range []watchEvent{
		event("PrgI", "PrgI:0", "12:00:00.000", map[string]int{"index": 0, "source": 5}),
		event("PrvI", "PrvI:0", "12:00:00.000", map[string]int{"index": 0, "source": 3}),
		event("TlSr", "TlSr", "12:00:00.000", atem.TallySourceField{Num: 2, Tally: map[uint16]atem.TallyLamps{
			3: {Preview: true}, 5: {Program: true}}}),
		event("TrPs", "TrPs:0", "12:00:01.000", atem.TransitionPositionField{InTransition: true}),
		event("TrPs", "TrPs:0", "12:00:02.000", atem.TransitionPositionField{}),
		event("KeOn", "KeOn:0:1", "12:00:03.000", map[string]any{"index": 0, "keyer": 1, "enabled": true}),
		{Kind: "disconnected", Time: "12:00:04.000"},
		event("PrgI", "PrgI:0", "12:00:09.000", map[string]int{"index": 0, "source": 3}),
		{Kind: "ready", Time: "12:00:09.000"},
	} {
		raw, _ := json.Marshal(ev)
		for _, l := range formatWatch(p, engine.Response{}, raw) {
			fmt.Fprintf(&out, "%s\n", visible(l))
		}
	}
	golden(t, "watch.golden", out.String())
}

// The spinner, frame by frame on a clock the test moves: nothing for
// the first 200 ms, then a frame each tick with the time; a line written
// meanwhile takes its place, and closing clears it. Through exec's
// prefix, each frame keeps the line's number.
func TestTerminalSpinner(t *testing.T) {
	var out strings.Builder
	for _, prefixed := range []bool{false, true} {
		var buf bytes.Buffer
		var w io.Writer = &buf
		if prefixed {
			w = &prefixWriter{w: &buf, prefix: "line 2: "}
		}
		paint := func(code, s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }
		at := time.Unix(0, 0)
		s := newSpinner(w, paint, spinAfter, "")
		s.now = func() time.Time { return at }
		s.set("connecting to studio (192.0.2.10)")
		step := func(d time.Duration) { at = at.Add(d); s.tick() }
		step(100 * time.Millisecond) // not yet
		step(100 * time.Millisecond)
		step(100 * time.Millisecond)
		spinWriter{s, w}.Write([]byte("connected to studio (192.0.2.10): ATEM 1 M/E Constellation HD, 1080p50  312 ms\n"))
		s.set("uploading still 3 to studio (192.0.2.10)")
		step(100 * time.Millisecond) // a new wait: not yet
		step(150 * time.Millisecond)
		step(61 * time.Second)
		s.set(strings.Repeat("x", 80)) // cut, so the line doesn't wrap
		step(time.Second)
		close(s.done) // run never ran
		s.close()
		fmt.Fprintf(&out, "prefixed %v:\n%s\n", prefixed, strings.ReplaceAll(visible(buf.String()), "<clear><cr>", "\n<clear><cr>"))
	}
	golden(t, "spinner.golden", out.String())
}

// A wait that goes past 200 ms shows the spinner on stderr, and leaves
// nothing of it: what follows clears its line.
func TestSpinnerRuns(t *testing.T) {
	b := newBench(t)
	b.onDial = func(*fakeatem.Switcher) { time.Sleep(500 * time.Millisecond) }
	c, o, e := b.cli("")
	c.tty, c.ttyErr = true, true
	if code := c.run(with("program", "cam1")); code != 0 {
		t.Fatalf("exit %d: %q", code, e)
	}
	errOut := e.String()
	frames := strings.Count(errOut, clearLine+"\x1b[1m")
	connected := strings.LastIndex(errOut, clearLine+"connected to ")
	if frames < 2 || !strings.Contains(errOut, " connecting to 192.0.2.10  \x1b[2m0.") || connected < 0 ||
		!strings.HasSuffix(errOut, "1080p50  \x1b[2m"+regexp.MustCompile(`\d+ ms`).FindString(errOut[connected:])+"\x1b[0m\n") {
		t.Errorf("stderr: %q", errOut)
	}
	if !strings.HasPrefix(o.String(), "\x1b[1m>\x1b[0m ") {
		t.Errorf("stdout: %q", o)
	}
	// TERM=dumb can't clear a line: no spinner, and the connecting line
	// as on a pipe.
	b.must(with("disconnect")...)
	b.env["TERM"] = "dumb"
	c, _, e = b.cli("")
	c.tty, c.ttyErr = true, true
	c.run(with("program", "cam2"))
	if e.String() != "connecting to 192.0.2.10 …\nATEM 1 M/E Constellation HD, 1080p50\n" {
		t.Errorf("dumb: %q", e)
	}
	// Interrupted while it waits: its line is cleared all the same.
	delete(b.env, "TERM")
	b.must(with("disconnect")...)
	c, _, e = b.cli("")
	c.tty, c.ttyErr = true, true
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	c.ctx = ctx
	c.run(with("program", "cam1"))
	if s := e.String(); !strings.Contains(s, "\x1b[1m") || !strings.Contains(s[strings.LastIndex(s, "\x1b[1m"):], clearLine) {
		t.Errorf("interrupted: %q", s)
	}
}

// align lays rows out as text/tabwriter does, for any table, and counts
// colour codes as nothing.
func TestAlign(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	words := []string{"", "a", "me 1", "program", "CAM1 → CAM2", "Stage Left Wide", "-"}
	for range 500 {
		rows := make([][]string, r.IntN(8))
		for i := range rows {
			rows[i] = make([]string, 1+r.IntN(5))
			for j := range rows[i] {
				rows[i][j] = words[r.IntN(len(words))]
			}
		}
		var b bytes.Buffer
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, row := range rows {
			fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
		tw.Flush()
		want := strings.TrimSuffix(b.String(), "\n")
		if got := strings.Join(align(rows), "\n"); got != want {
			t.Fatalf("%q:\ngot\n%s\nwant\n%s", rows, got, want)
		}
	}
	coloured := align([][]string{{"\x1b[2mme 1\x1b[0m", "\x1b[31mCAM1\x1b[0m", "x"}, {"aux 1", "CAM2", "y"}})
	if width(coloured[0]) != width(coloured[1]) || !strings.HasSuffix(coloured[1], "  CAM2  y") {
		t.Errorf("%q", coloured)
	}
}

func TestSplitStep(t *testing.T) {
	for _, c := range []struct{ msg, rest, step string }{
		{`no input is named "x" (bmdctl inputs lists them)`, `no input is named "x"`, "bmdctl inputs lists them"},
		{"no route (bmdctl doctor says why)", "no route", "bmdctl doctor says why"},
		{"no host named x (bmdctl hosts lists them); or add --dry-run", "no host named x (bmdctl hosts lists them); or add --dry-run", ""},
		{"engine: stop it (bmdctl engine stop) and retry", "engine: stop it (bmdctl engine stop) and retry", ""},
		{"nothing run:\n  line 2 (bmdctl x)\n  line 3", "nothing run:\n  line 2 (bmdctl x)\n  line 3", ""},
		{"destructive: add --yes to run it", "destructive: add --yes to run it", ""},
	} {
		if rest, step := splitStep(c.msg); rest != c.rest || step != c.step {
			t.Errorf("%q: %q, %q", c.msg, rest, step)
		}
	}
}
