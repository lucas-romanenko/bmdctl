// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Colour only on a terminal, and not with NO_COLOR or TERM=dumb.
func TestColour(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		tty    bool
		env    map[string]string
		colour bool
	}{
		{true, nil, true},
		{false, nil, false},
		{true, map[string]string{"NO_COLOR": "1"}, false},
		{true, map[string]string{"TERM": "dumb"}, false},
	} {
		for k, v := range c.env {
			b.env[k] = v
		}
		cl, o, _ := b.cli("")
		cl.tty = c.tty
		if code := cl.run(with("program", "--source", "1")); code != 0 {
			t.Fatal(code)
		}
		if got := strings.Contains(o.String(), "\x1b["); got != c.colour {
			t.Errorf("tty %v env %v: colour %v: %q", c.tty, c.env, got, o)
		}
		for k := range c.env {
			delete(b.env, k)
		}
	}
	// --json is never coloured.
	cl, o, _ := b.cli("")
	cl.tty = true
	cl.run(with("program", "--source", "1", "--json"))
	if strings.Contains(o.String(), "\x1b[") {
		t.Errorf("--json coloured: %q", o)
	}
}

// With --json, a failure the CLI finds itself is printed as the engine
// prints one: {ok:false, code, error} on stdout, with its exit status.
func TestJSONFailures(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"nonsense", "--json"}, exitUsage, "usage"},
		{[]string{"transition", "--json"}, exitUsage, "usage"},
		{[]string{"program", "--source", "70000", "--json"}, exitUsage, "usage"},
		{[]string{"cut", "--json"}, exitUsage, "usage"}, // no host
		{with("video-mode", "--mode", "1080p50", "--json"), exitDestructive, "destructive"},
		{[]string{"engine", "status", "--json"}, exitOK, ""},
	} {
		code, out, errOut := b.run(c.args...)
		if code != c.exit || errOut != "" {
			t.Errorf("%v: %d %q %q", c.args, code, out, errOut)
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(out), &r); err != nil || strings.Count(out, "\n") != 1 {
			t.Errorf("%v: not one JSON line: %q", c.args, out)
			continue
		}
		if c.code != "" && (r["ok"] != false || r["code"] != c.code || r["error"] == "") {
			t.Errorf("%v: %v", c.args, r)
		}
	}
	// --quiet keeps even that off stdout.
	if code, out, errOut := b.run("nonsense", "--json", "-q"); code != exitUsage || out != "" || errOut == "" {
		t.Errorf("--json --quiet: %d %q %q", code, out, errOut)
	}
}

// Every command in the table runs through the engine against the fake
// switcher, with sample arguments: the ones the fake confirms are
// confirmed, the rest sent. A few need a setup or end in a known refusal.
func TestEveryCommandRuns(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: small stills
	b := newBenchDump(t, dump)
	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("logo.png", testPNG(t), 0o644)
	os.WriteFile("ops.json", []byte(testOps), 0o644)
	b.must(with("media", "upload", "--slot", "2", "--file", "logo.png")...)
	b.must(with("macro", "upload", "--slot", "2", "--file", "ops.json")...)
	b.must(with("profile", "save", "--file", "p.xml", "--media-pool-images=false")...)
	b.must("macro", "encode", "--file", "ops.json", "--out", "m.bin")

	// Arguments a sample can't guess.
	special := map[string][]string{
		"media upload":    {"--slot", "3", "--file", "logo.png"},
		"media download":  {"--slot", "2", "--file", "got.png"},
		"media clear":     {"--slot", "2", "--yes"},
		"macro upload":    {"--slot", "4", "--file", "ops.json"},
		"macro download":  {"--slot", "2", "--file", "m.xml"},
		"macro run":       {"--slot", "2"},
		"macro delete":    {"--slot", "2", "--yes"},
		"macro decode":    {"--file", "m.bin"},
		"macro encode":    {"--file", "ops.json", "--out", "m2.bin"},
		"profile save":    {"--file", "p2.xml", "--media-pool-images=false"},
		"profile restore": {"--file", "p.xml", "--yes", "--video-mode=false"},
		"transition next": {"--next", "background"},
		"watch":           {"--count", "1", "--filter", "PrgI"},
		"audio meters":    {"--for", "0.1"},
		"camera":          {"--destination", "1", "--category", "0", "--parameter", "0"},
		"macro record":    {"--slot", "10", "--name", "x"},
	}
	// Transfers get the library's timeouts, not the sweep's short echo
	// wait.
	transfer := map[string]bool{"media upload": true, "media download": true, "macro upload": true,
		"macro download": true, "profile save": true, "profile restore": true, "profile images": true}
	// Commands the fake doesn't confirm in time, by design.
	expect := map[string]int{
		"media clear-unlocked": exitNoEcho, // the fake, like some models, ignores CSTL without the lock
	}
	reg, _ := engine.NewRegistry()
	entries := reg.Entries("atem")
	// Run the ones that change the video mode or delete things last.
	sort.SliceStable(entries, func(i, j int) bool { return lateCommand(entries[i].Path) < lateCommand(entries[j].Path) })
	for _, e := range entries {
		args, ok := special[e.Path]
		if !ok {
			args = sampleArgs(e)
		}
		if e.Destructive && !contains(args, "--yes") {
			args = append(args, "--yes")
		}
		line := append(strings.Fields(e.Path), args...)
		if !e.Local {
			line = with(line...)
		}
		if e.Path == "watch" {
			// Once it watches, a program change ends it.
			cl, o, _ := b.cli("")
			ew := newLineWriter()
			cl.stderr = ew
			done := make(chan int)
			go func() { done <- cl.run(line) }()
			ew.next(t)
			b.must(with("program", "--source", "2")...)
			if code := <-done; code != 0 || !strings.Contains(o.String(), "program  ME1") {
				t.Errorf("watch: %d %q", code, o)
			}
			continue
		}
		if !transfer[e.Path] {
			line = append(line, "--timeout", "500ms")
		}
		code, out, errOut := b.run(line...)
		if code != expect[e.Path] {
			t.Errorf("%s: exit %d, want %d\n%s%s", strings.Join(line, " "), code, expect[e.Path], out, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "got.png")); err != nil {
		t.Error(err)
	}
}

func lateCommand(path string) int {
	switch path {
	case "video-mode", "startup clear", "startup save", "media clear", "macro delete", "profile restore", "input rename":
		return 1
	case "disconnect": // the next command gets a new fake switcher
		return 2
	}
	return 0
}

func contains(s []string, x string) bool {
	for _, y := range s {
		if y == x {
			return true
		}
	}
	return false
}

// sampleArgs are valid arguments for an entry: every required parameter,
// and the first optional one where the entry needs one.
func sampleArgs(e table.Entry) []string {
	var args []string
	add := func(p table.Param) { args = append(args, "--"+p.Name, sampleValue(p)) }
	for _, p := range e.Params {
		if p.Required {
			add(p)
		}
	}
	if e.AtLeastOne {
		for _, p := range e.Params {
			if !p.Required && p.Default == "" && p.Name != "clip" {
				add(p)
				break
			}
		}
	}
	return args
}

func sampleValue(p table.Param) string {
	switch p.Kind {
	case table.Enum, table.Flags:
		return p.SortedNames()[0]
	case table.Bool:
		return "true"
	case table.Float:
		return "1.5"
	case table.String:
		if p.Name == "address" {
			return "192.0.2.1"
		}
		return "x"
	case table.Rate:
		return "0:10"
	case table.Time:
		return "1789128000"
	case table.Source:
		return "Camera 1"
	}
	if p.Name == "channel" {
		return "-1"
	}
	return "1"
}
