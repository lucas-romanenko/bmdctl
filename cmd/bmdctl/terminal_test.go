// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
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
	in  *string           // stdin for the next line only, a terminal
	out strings.Builder
}

func (tr *transcript) run(args ...string) {
	tr.b.t.Helper()
	in := ""
	if tr.in != nil {
		in = *tr.in
	}
	c, o, e := tr.b.cli(in)
	c.tty, c.ttyErr, c.ttyIn = true, true, tr.in != nil
	c.now = func() time.Time { return time.Date(2026, 10, 11, 13, 43, 28, 0, time.Local) }
	tr.in = nil
	// A terminal echoes what is typed after the question: so does the
	// transcript.
	c.spinAfter = time.Hour // no frame: what a wait leaves is the point
	for k, v := range tr.env {
		tr.b.env[k] = v
	}
	code := c.run(with(args...))
	if in != "" {
		out := strings.Replace(o.String(), "[y/N] ", "[y/N] "+in, 1)
		o.Reset()
		o.WriteString(out)
	}
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
// clock, the engine's pid and uptime. A result's time is right-aligned,
// so it keeps the column it ends in.
func normalise(s string) string {
	s = regexp.MustCompile(`( *)(<dim>)?(\d+ ms|\d+\.\d s)`).ReplaceAllStringFunc(s, func(m string) string {
		lead := len(m) - len(strings.TrimLeft(m, " "))
		rest := strings.TrimLeft(m, " ")
		tag := ""
		if strings.HasPrefix(rest, "<dim>") {
			tag, rest = "<dim>", rest[len("<dim>"):]
		}
		return strings.Repeat(" ", max(1, lead+len([]rune(rest))-len("# ms"))) + tag + "# ms"
	})
	for _, r := range []struct{ re, with string }{
		{`connected +# ms`, "connected # ms"},
		{`\d+\.\d s`, "# s"},
		{`idle \d+s`, "idle #s"},
		{`up \d+s`, "up #s"},
		{`pid \d+`, "pid #"},
		{`drops at \d\d:\d\d`, "drops at HH:MM"},
	} {
		s = regexp.MustCompile(r.re).ReplaceAllString(s, r.with)
	}
	return s
}

// goldenDir is where the golden files are, whatever directory a test
// moves to.
var goldenDir, _ = filepath.Abs(filepath.Join("testdata", "terminal"))

// golden compares got with a golden file, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
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
	// The fake answers a wipe with the settings it holds: the switcher
	// kept its rate.
	tr.run("transition", "wipe", "--rate", "10")
	tr.run("program", "cam1", "--me", "1")
	tr.run("connect")
	tr.run("disconnect")
	// Without colour, at a terminal: the same lines, plain.
	tr.env = map[string]string{"NO_COLOR": "1"}
	tr.run("program", "cam2")
	tr.env = nil
	// exec's lines share one header.
	script := filepath.Join(tr.b.dir, "cue.txt")
	os.WriteFile(script, []byte("preview mp1\ncut\naux 1 cam1\n"), 0o644)
	tr.run("exec", script)
	golden(t, "writes.golden", strings.ReplaceAll(tr.out.String(), tr.b.dir, "DIR"))
}

// connect ADDRESS saves the switcher as the default, by its own name,
// and the first time ever says Tab completes its names; connect alone
// goes to the default; disconnect names the switcher. A name another
// host goes by isn't taken.
func TestTerminalConnect(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	tr.run("connect", "192.0.2.10")
	tr.run("program", "cam1") // to the default now
	tr.run("disconnect")
	tr.run("connect")
	tr.run("connect", "192.0.2.10") // saved already, the default already
	tr.run("connect", "192.0.2.11") // calls itself what the first does
	tr.run("disconnect")
	golden(t, "connect.golden", tr.out.String())
	f, _, _ := tr.b.cli("")
	hs, _, _ := f.loadHosts()
	if len(hs.Hosts) != 1 || hs.Default != "ATEM 1 M/E Constellation HD" || hs.Hosts[0].Address != "192.0.2.10" {
		t.Errorf("hosts.yml: %+v", hs)
	}
}

// Transfers and profiles: a transfer's line says what moved; a profile
// shows a line per step as it finishes (✓), then what it all came to.
// (Progress draws on stderr, after 200 ms: TestTerminalSpinner.)
func TestTerminalTransfers(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: small stills
	b := newBenchDump(t, dump)
	t.Chdir(t.TempDir())
	os.WriteFile("logo.png", testPNG(t), 0o644)
	os.WriteFile("ops.json", []byte(testOps), 0o644)
	tr := &transcript{b: b}
	tr.run("media", "upload", "3", "logo.png")
	tr.run("media", "upload", "3", "logo.png", "--yes", "--name", "Logo")
	tr.run("media", "download", "3", "out.png")
	tr.run("macro", "upload", "5", "ops.json", "--name", "Cams")
	tr.run("macro", "download", "5", "cams.xml")
	tr.run("profile", "save", "show.xml")
	tr.run("profile", "images", "--dir", "stills")
	tr.run("profile", "restore", "show.xml", "--yes", "--video-mode", "false")
	tr.run("media", "clear", "3", "--yes")
	golden(t, "transfers.golden", tr.out.String())
}

// A step that fails says so in place, with what to do next, and the
// steps it left undone are listed as skipped.
func TestTerminalStepFails(t *testing.T) {
	var o, e bytes.Buffer
	c := newCLI(&o, &e)
	c.env = func(string) string { return "" }
	c.tty, c.ttyErr = true, true
	reg := c.reg
	p := &printer{c: c, e: reg.Find("atem", "profile save"), params: map[string]string{"file": "show.xml"},
		t: target{Address: "192.0.2.10:9910"}, sw: &engine.Switcher{Name: "studio"}}
	p.note(engine.Note{OnAir: &engine.OnAir{Program: "CAM1", Preview: "CAM2"}})
	p.note(engine.Note{Steps: []string{"setup", "stills", "upload"}})
	p.note(engine.Note{Step: &engine.Step{Index: 0}})
	p.note(engine.Note{Step: &engine.Step{Index: 0, Done: true, Detail: "6 macros", TookMS: 1900}})
	p.note(engine.Note{Step: &engine.Step{Index: 1}})
	p.note(engine.Note{Step: &engine.Step{Index: 1, Error: "the session closed"}})
	p.response(nil, engine.Response{Code: engine.CodeUnreachable, Error: "the session closed"})
	golden(t, "steps.golden", visible(o.String()+e.String()))
}

// A destructive command at a terminal, a person at the keyboard: what it
// would affect, then the question; y or yes does it, anything else (and
// Enter) cancels with exit 5. stdin not a terminal: refused, as ever.
func TestTerminalConfirm(t *testing.T) {
	b := newBench(t)
	b.must(with("media", "upload", "2", writePNG(t, b), "--name", "Logo")...)
	b.must(with("program", "mp1")...)
	tr := &transcript{b: b}
	answer := func(s string) { tr.in = &s }
	answer("y\n")
	tr.run("media", "clear", "2")
	answer("\n")
	tr.run("media", "upload", "1", writePNG(t, b))
	answer("no\n")
	tr.run("macro", "delete", "3")
	answer("YES\n")
	tr.run("video-mode", "525i5994")
	answer("") // end of input
	tr.run("macro", "delete", "1")
	answer("y\n")
	tr.run("video-mode", "525i5994") // already: nothing to ask
	tr.run("startup", "save")        // stdin no terminal: refused
	golden(t, "confirm.golden", strings.ReplaceAll(tr.out.String(), b.dir, "DIR"))
	if n := len(b.sent("CVdM")); n != 1 {
		t.Errorf("%d video mode changes", n)
	}
}

// writePNG writes a small PNG into the bench's directory.
func writePNG(t *testing.T, b *bench) string {
	path := filepath.Join(b.dir, "card.png")
	os.WriteFile(path, testPNG(t), 0o644)
	return path
}

// undo puts back the last program, preview, aux, keyer or DSK change
// made from this machine, as a write; a cut or an auto, by cutting back,
// as a cut; with nothing to undo it says so.
func TestTerminalUndo(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	tr.run("undo")
	tr.run("program", "cam1")
	tr.run("preview", "mp1")
	tr.run("program", "cam1") // already: nothing to undo
	tr.run("aux", "3", "cam2")
	tr.run("usk", "2", "on-air", "on")
	tr.run("dsk", "toggle", "--dsk", "2")
	for range 6 {
		tr.run("undo")
	}
	tr.run("cut")
	tr.run("undo")
	tr.run("auto")
	tr.run("undo")
	golden(t, "undo.golden", tr.out.String())
	if out := b.must(with("undo")...); out != "ATEM 1 M/E Constellation HD (192.0.2.10)  nothing to undo\n" {
		t.Errorf("pipe: %q", out)
	}
	b.must(with("cut")...)
	if out := b.must(with("undo")...); !strings.Contains(out, "  undo  cut  ") || !strings.Contains(out, "CAM2 on program · CAM1 on preview  confirmed") {
		t.Errorf("pipe: %q", out)
	}
}

// show: one area of the switcher, formatted.
func TestTerminalShow(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	for _, area := range []string{"keys", "dsk", "transition", "audio", "media", "macros", "aux", "hyperdecks"} {
		tr.run("show", area)
	}
	tr.run("show", "macros", "--all")
	golden(t, "show.golden", tr.out.String())
	if out := b.must(with("show", "dsk")...); !strings.HasPrefix(out, "ATEM 1 M/E Constellation HD (192.0.2.10)  ATEM") ||
		!regexp.MustCompile(`\n  dsk 1       6 / 7  1:05 · masked · premultiplied +on air\n`).MatchString(out) {
		t.Errorf("pipe:\n%s", out)
	}
}

// -t starts each result with the time of day, dim; a read's rows and a
// pipe are as they were.
func TestTerminalTimestamps(t *testing.T) {
	b := newBench(t)
	at := time.Date(2026, 10, 11, 13, 43, 28, 0, time.Local)
	run := func(tty bool, args ...string) string {
		c, o, _ := b.cli("")
		c.tty, c.ttyErr, c.spinAfter = tty, tty, time.Hour
		c.now = func() time.Time { return at }
		c.run(with(args...))
		return visible(o.String())
	}
	var out strings.Builder
	for _, line := range [][]string{{"-t", "program", "cam1"}, {"preview", "mp1", "--timestamps"}, {"-t", "cut"}, {"-t", "undo"}, {"-t", "status"}} {
		fmt.Fprintf(&out, "$ bmdctl %s\n%s\n", strings.Join(line, " "), normalise(run(true, line...)))
	}
	golden(t, "timestamps.golden", out.String())
	if got := run(false, "-t", "program", "cam2"); strings.Contains(got, "13:43:28") {
		t.Errorf("pipe: %q", got)
	}
}

// exec at a terminal: one header saying the file, a numbered line per
// command (its line in the file), the failed one's error and next step in
// place, and how it went.
func TestTerminalExec(t *testing.T) {
	b := newBench(t)
	write := func(name, s string) string {
		path := filepath.Join(b.dir, name)
		os.WriteFile(path, []byte(s), 0o644)
		return path
	}
	tr := &transcript{b: b}
	tr.run("exec", write("open.cue", "# the open\npreview mp1\ntransition mix 1:00\n\nauto\nusk 1 fill cam1\nusk 1 on-air off\n"))
	stop := write("stop.cue", "program cam1\nprogram nowhere\ncut\n")
	tr.run("exec", stop)
	tr.run("exec", stop, "--continue")
	tr.run("exec", write("t.cue", "preview cam2\n"), "-t")
	golden(t, "exec.golden", strings.ReplaceAll(tr.out.String(), b.dir, "DIR"))
}

// fit cuts a line to its columns, colour codes and all.
func TestFit(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"program", 10, "program"},
		{"programme", 5, "prog…"},
		{"\x1b[31mCAM1\x1b[0m → CAM2", 6, "\x1b[31mCAM1\x1b[0m …"},
		{"\x1b[2mmedia pool · 20 stills\x1b[0m", 8, "\x1b[2mmedia p…\x1b[0m"},
		{"Cam → Ca2", 9, "Cam → Ca2"},
	} {
		if got := fit(c.s, c.n); got != c.want || width(got) > c.n {
			t.Errorf("fit(%q, %d) = %q", c.s, c.n, got)
		}
	}
	c := newCLI(io.Discard, io.Discard)
	c.env = func(string) string { return "" }
	if got := c.resultLine("program", "CAM2 → CAM1", "32 ms"); got != "  program     CAM2 → CAM1"+strings.Repeat(" ", lineWidth-25-5)+"32 ms" {
		t.Errorf("%q", got)
	}
	long := c.resultLine("transition wipe", strings.Repeat("rate 1:00 → 0:12, ", 6), "rejected")
	if width(long) != lineWidth || !strings.HasSuffix(long, "…  rejected") {
		t.Errorf("%q", long)
	}
	// Every status ends at lineWidth, and where a line is cut doesn't
	// move with what its status says: a golden's # ms keeps its place.
	var cut string
	for i, status := range []string{"5 ms", "123 ms", "12.5 s", "sent", "rejected"} {
		got := c.resultLine("profile", strings.Repeat("show.xml · ", 6), status)
		left := strings.TrimRight(strings.TrimSuffix(got, status), " ")
		if width(got) != lineWidth || !strings.HasSuffix(got, status) || i > 0 && left != cut {
			t.Errorf("%s: %q", status, got)
		}
		cut = left
	}
	// A table row's status too.
	if got := trailing("  keys        1  luma  4 / 5", "on air"); width(got) != lineWidth || !strings.HasSuffix(got, " on air") {
		t.Errorf("%q", got)
	}
}

// Reads: ids dim, labels and values plain, empty slots counted unless
// --all, repeats collapsed, program and on air red, preview green.
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
	// Aux outputs on one source collapse into one row.
	b.must(with("aux", "1", "pgm")...)
	b.must(with("aux", "3", "pgm")...)
	for _, line := range [][]string{{"info"}, {"status"}, {"inputs"}, {"inputs", "--all"}, {"media", "list"}, {"media", "list", "--all"},
		{"macro", "list"}, {"macro", "list", "--all"}, {"video-modes"}, {"time"}} {
		tr.run(line...)
	}
	golden(t, "reads.golden", tr.out.String())
}

// Sources as typed: an alias whatever the name (cam2, in2, mp1), a name,
// the start of one; the line shows the switcher's short name, with what
// was typed dim where it differs. An alias that shadows a name says so,
// once ever.
func TestTerminalSources(t *testing.T) {
	b := newBench(t)
	tr := &transcript{b: b}
	tr.run("preview", "in2")
	tr.run("preview", "Camera 1")
	tr.run("aux", "1", "auxil")
	tr.run("program", "MP1")
	tr.run("input", "rename", "1", "--short-name", "cam2")
	tr.run("program", "cam2")
	tr.run("program", "cam2")
	tr.run("program", "cam")
	golden(t, "sources.golden", tr.out.String())
}

// Errors: "error" red, the message, the next step dim under it; a source
// that names nothing lists what it could have been.
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

// The banner: one dim line on bmdctl alone, never otherwise, nor on a
// pipe; bmdctl version has the commit, the hosts file and the engine.
func TestTerminalBanner(t *testing.T) {
	b := newBench(t)
	// A HyperDeck's line is a switcher's: its model and video format and
	// its session (deck, which answers), or not connected (deck2).
	b.net.Deck("192.0.2.20", fakehyperdeck.New())
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.11", "--as", "spare")
	b.must("hosts", "add", "192.0.2.20", "--as", "deck", "--device", "hyperdeck")
	b.must("hosts", "add", "192.0.2.21", "--as", "deck2", "--device", "hyperdeck")
	b.must("hosts", "default", "studio")
	b.must("--host", "spare", "disconnect")
	b.must("--host", "deck", "status")
	tr := &transcript{b: b}
	tr.run()
	// A pipe's deck lines are a switcher's too.
	out := b.must()
	for _, want := range []string{`(?m)^deck \(192\.0\.2\.20\) +HyperDeck Studio HD Mini +1080p25 +ready, idle`, `(?m)^deck2 \(192\.0\.2\.21\) +no session$`} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("pipe: no line matching %s in\n%s", want, out)
		}
	}
	tr.run("program", "cam1") // an ordinary command: no banner
	tr.run("version")
	// A command that starts the engine: no banner either.
	b.engine().Stop()
	eventually(t, "the engine to go", func() bool { _, err := os.Stat(b.env["BMDCTL_SOCKET"]); return os.IsNotExist(err) })
	c, o, e := b.cli("")
	c.tty, c.ttyErr, c.spinAfter = true, true, time.Hour
	c.spawn = func(string, []string, []string) error { return b.startEngine(engine.Config{}) }
	args := []string{"--host", "studio", "program", "cam2"}
	tr.record(args, c.run(args), o.String(), e.String())
	golden(t, "banner.golden", strings.ReplaceAll(tr.out.String(), shortVersion(), "VERSION"))
	// Not on a pipe.
	if out := b.must(); !strings.HasPrefix(out, "engine  ") {
		t.Errorf("pipe: %q", out)
	}
	if out := b.must("version"); strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "bmdctl ") {
		t.Errorf("pipe version: %q", out)
	}
}

func TestTildePath(t *testing.T) {
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

// watch: the time of day dim, program red and preview green, keyers and
// DSKs on air red, the session's own lines bold.
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
		event("DskS", "DskS:0", "12:00:03.500", map[string]any{"index": 0, "on_air": true, "is_transitioning": true}),
		event("DskS", "DskS:0", "12:00:03.600", map[string]any{"index": 0, "on_air": true, "is_transitioning": true}),
		event("InPr", "InPr:3", "12:00:03.700", map[string]any{"index": 3, "name": "Camera 3", "short_name": "CAM3", "port_type": 0}),
		{Kind: "disconnected", Time: "12:00:04.000"},
		event("PrgI", "PrgI:0", "12:00:09.000", map[string]int{"index": 0, "source": 3}),
		{Kind: "ready", Time: "12:00:09.000"},
	} {
		raw, _ := json.Marshal(ev)
		for _, l := range screenWatch(p, engine.Response{}, raw).rows {
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
		spinWriter{s, w}.Write([]byte("studio  PGM CAM1 · PVW CAM2 · connected 312 ms\n"))
		s.set("uploading logo.png → slot 3")
		step(100 * time.Millisecond) // a new wait: not yet
		step(150 * time.Millisecond)
		step(61 * time.Second)
		s.set(strings.Repeat("x", 80)) // cut, so the line doesn't wrap
		step(time.Second)
		// A transfer's progress: a bar, the share, the bytes moved.
		s.set("uploading sponsor.png → slot 10")
		step(250 * time.Millisecond)
		s.progress(engine.Progress{Done: 816_000, Total: 1_200_000, Bytes: true})
		step(550 * time.Millisecond)
		s.progress(engine.Progress{Done: 3500, Total: 6000, Item: "still 4 of 6"})
		step(time.Second)
		close(s.done) // run never ran
		s.close()
		fmt.Fprintf(&out, "prefixed %v:\n%s\n", prefixed, strings.ReplaceAll(visible(buf.String()), "<clear><cr>", "\n<clear><cr>"))
	}
	golden(t, "spinner.golden", out.String())
}

// A wait that goes past 200 ms shows the spinner on stderr, and leaves
// nothing of it: what follows clears its line. The header says how long
// connecting took.
func TestSpinnerRuns(t *testing.T) {
	b := newBench(t)
	b.onDial = func(*fakeatem.Switcher) { time.Sleep(500 * time.Millisecond) }
	c, o, e := b.cli("")
	c.tty, c.ttyErr = true, true
	c.connectTook = time.Since // the real time, which the bench fixes at 8 ms
	if code := c.run(with("program", "cam1")); code != 0 {
		t.Fatalf("exit %d: %q", code, e)
	}
	errOut := e.String()
	frames := strings.Count(errOut, clearLine+"\x1b[2m  ⠋") + strings.Count(errOut, clearLine+"\x1b[2m  ⠙")
	if frames < 2 || !strings.Contains(errOut, " connecting to 192.0.2.10 · 0.") || !strings.HasSuffix(errOut, clearLine) {
		t.Errorf("stderr: %q", errOut)
	}
	if !regexp.MustCompile(`^\x1b\[1mATEM 1 M/E Constellation HD\x1b\[0m  \x1b\[2mPGM CAM2 · PVW CAM1 · (1080p50 · )?connected \d{3,} ms\x1b\[0m\n  program  `).MatchString(o.String()) {
		t.Errorf("stdout: %q", o)
	}
	// TERM=dumb can't clear a line: no spinner, the connecting line as
	// on a pipe, and the header says the rest.
	b.must(with("disconnect")...)
	b.env["TERM"] = "dumb"
	c, o, e = b.cli("")
	c.tty, c.ttyErr = true, true
	c.run(with("program", "cam2"))
	if e.String() != "connecting to 192.0.2.10 …\n" || !strings.Contains(o.String(), " · connected ") {
		t.Errorf("dumb: %q %q", e, o)
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
	if s := e.String(); !strings.Contains(s, "⠋") || !strings.Contains(s[strings.LastIndex(s, "⠋"):], clearLine) {
		t.Errorf("interrupted: %q", s)
	}
}

// An upload that goes past 200 ms draws its bar on stderr at least once,
// with the spinner's real 200 ms, and leaves nothing of it. The fake
// holds the upload part way until a bar has been drawn, so the test
// waits on the bar, not on timing: the upload can't end before one is
// drawn, and the test fails if none is in 10 s.
func TestUploadBar(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: 720×480 stills
	b := newBenchDump(t, dump)
	release := make(chan struct{})
	b.onDial = func(sw *fakeatem.Switcher) { sw.HoldUpload(release) }
	t.Chdir(t.TempDir())
	os.WriteFile("noise.png", noisePNG(t), 0o644)
	w := &barWatch{release: release}
	timeout := time.AfterFunc(10*time.Second, func() { w.once.Do(func() { close(release) }) })
	defer timeout.Stop()
	c, o, _ := b.cli("")
	c.tty, c.ttyErr = true, true
	c.stderr = w
	code := c.run(with("media", "upload", "3", "noise.png"))
	errOut := w.String()
	if code != 0 || !w.drawn {
		t.Fatalf("exit %d, bar drawn %v: %q", code, w.drawn, errOut)
	}
	if !regexp.MustCompile(`uploading noise\.png → slot 3 [█░]{8} \d+%  \d+ KB  \d+\.\d s`).MatchString(errOut) {
		t.Errorf("no bar: %q", errOut)
	}
	if !strings.HasSuffix(errOut, clearLine) || strings.ContainsAny(o.String(), "█░") || !strings.Contains(o.String(), "upload      slot 3") {
		t.Errorf("the bar was left behind: %q %q", errOut, o)
	}
}

// barWatch is a terminal's stderr that lets the held upload go once a
// progress bar has been drawn on it.
type barWatch struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	release chan struct{}
	once    sync.Once
	drawn   bool
}

var barFrame = regexp.MustCompile(`[█░]{8} \d+%`)

func (w *barWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if barFrame.Match(p) {
		w.once.Do(func() { w.drawn = true; close(w.release) })
	}
	return len(p), nil
}

func (w *barWatch) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buf.String() }

// noisePNG is a 720×480 still whose top rows are noise, which doesn't
// compress: its upload takes more than the fake's first budget.
func noisePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 720, 480))
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 720*32*4; i++ {
		img.Pix[i] = byte(r.Uint32())
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
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
