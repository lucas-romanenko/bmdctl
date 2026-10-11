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
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

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
		{[]string{"program", "70000", "--json"}, exitUsage, "usage"},
		{[]string{"cut", "--json"}, exitUsage, "usage"}, // no host
		{with("video-mode", "1080p50", "--json"), exitDestructive, "destructive"},
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
	b.must(with("media", "upload", "2", "logo.png")...)
	b.must(with("macro", "upload", "2", "ops.json")...)
	b.must(with("profile", "save", "p.xml", "--media-pool-images=false")...)
	b.must("macro", "encode", "ops.json", "m.bin")

	// Lines a sample can't guess, as typed.
	special := map[string][]string{
		"media upload":    {"media", "upload", "3", "logo.png"},
		"media download":  {"media", "download", "2", "got.png"},
		"media clear":     {"media", "clear", "2", "--yes"},
		"macro upload":    {"macro", "upload", "4", "ops.json"},
		"macro download":  {"macro", "download", "2", "m.xml"},
		"macro run":       {"macro", "run", "2"},
		"macro delete":    {"macro", "delete", "2", "--yes"},
		"macro decode":    {"macro", "decode", "m.bin"},
		"macro encode":    {"macro", "encode", "ops.json", "m2.bin"},
		"profile save":    {"profile", "save", "p2.xml", "--media-pool-images=false"},
		"profile restore": {"profile", "restore", "p.xml", "--yes", "--video-mode=false"},
		"transition next": {"transition", "next", "--next", "background"},
		"watch":           {"watch", "--count", "1", "--filter", "PrgI"},
		"audio meters":    {"audio", "meters", "--for", "0.1"},
		"camera":          {"camera", "1", "0", "0"},
		"macro record":    {"macro", "record", "10", "x"},
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
		line, ok := special[e.Path]
		if !ok {
			line = sampleLine(e)
		}
		if e.Destructive && !contains(line, "--yes") {
			line = append(line, "--yes")
		}
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
			// One of the two changes program, whatever it holds: one it
			// holds already sends nothing.
			b.must(with("program", "1")...)
			b.must(with("program", "2")...)
			select {
			case code := <-done:
				if code != 0 || !strings.Contains(o.String(), "program  ME1") {
					t.Errorf("watch: %d %q", code, o)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("watch saw no program change")
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

// sampleLine is a valid command line for an entry, as typed: its words
// and positional arguments, then the first flag where it needs one.
func sampleLine(e table.Entry) []string {
	var line []string
	for _, x := range e.Elems() {
		switch {
		case x.Word != "":
			line = append(line, x.Word)
		case !x.Optional:
			for _, p := range e.Params {
				if p.Name == x.Param {
					line = append(line, sampleValue(p))
				}
			}
		}
	}
	if e.AtLeastOne {
		for _, p := range e.Params {
			if !p.Required && p.Default == "" && !e.Positional(p.Name) && p.Name != "clip" {
				line = append(line, "--"+p.Name, sampleValue(p))
				break
			}
		}
	}
	return line
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
