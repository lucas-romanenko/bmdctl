// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func TestComplete(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "studio", "192.0.2.10")
	b.must("hosts", "add", "spare", "192.0.2.11")
	complete := func(args ...string) []string {
		c, _, _ := b.cli("")
		c.spawn = func(string, []string, []string) error { t.Fatal("completion started an engine"); return nil }
		return c.complete(args)
	}
	has := func(got []string, want ...string) bool {
		for _, w := range want {
			if !slices.Contains(got, w) {
				return false
			}
		}
		return true
	}
	if got := complete(""); !has(got, "version", "engine", "hosts", "exec", "completion", "man", "program", "transition", "usk") {
		t.Errorf("first word: %v", got)
	}
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"tra"}, []string{"transition"}},
		{[]string{"atem", "tb"}, []string{"tbar"}},
		{[]string{"transition", ""}, []string{"dip", "dve", "mix", "next", "preview", "rate", "settings", "stinger", "style", "wipe"}},
		{[]string{"transition", "wipe", "--pattern", "Circ"}, []string{"CircleIris"}},
		{[]string{"transition", "settings", "--next", "background,k"}, []string{"background,key1", "background,key2", "background,key3", "background,key4"}},
		{[]string{"usk", "1", "on-air", "t"}, []string{"true"}},
		{[]string{"usk", "1", "on-air", "o"}, []string{"off", "on"}},
		{[]string{"usk", "1", "type", "--fly", "f"}, []string{"false"}},
		{[]string{"usk", "1", "o"}, []string{"on-air"}},
		{[]string{"pro"}, []string{"profile", "program"}},
		{[]string{"transition", "style", "w"}, []string{"wipe"}},
		{[]string{"media", "upload", "1", ""}, []string{completeFiles}},
		{[]string{"profile", "save", ""}, []string{completeFiles}},
		{[]string{"media", "upload", "--file", ""}, []string{completeFiles}},
		{[]string{"exec", ""}, []string{completeFiles}},
		{[]string{"--host", "s"}, []string{"studio", "spare"}},
		{[]string{"hosts", ""}, []string{"list", "add", "rm", "default"}},
		{[]string{"hosts", "rm", "st"}, []string{"studio"}},
		{[]string{"hosts", "add", "x", "1.2.3.4", "--device", "h"}, []string{"hyperdeck"}},
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish"}},
		{[]string{"engine", ""}, []string{"stop", "status"}},
		{[]string{"program", "1", "x"}, nil},
		{[]string{"hyperdeck", ""}, nil},
	} {
		if got := complete(c.args...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.args, got, c.want)
		}
	}
	// Flags are the optional arguments; the positional ones' --name form
	// is accepted but not offered.
	got := complete("program", "--")
	if !has(got, "--me", "--host", "--json", "--yes") || slices.Contains(got, "--keyer") || slices.Contains(got, "--source") {
		t.Errorf("program flags: %v", got)
	}
	if got := complete("program", "1", "--me", "1", "--"); slices.Contains(got, "--me") {
		t.Errorf("--me offered twice: %v", got)
	}
	// Input names, live from the running engine, for the host on the line.
	if got := complete("--host", "studio", "program", "ca"); !reflect.DeepEqual(got, []string{"Camera 1", "Camera 2"}) {
		t.Errorf("live sources: %q", got)
	}
	// Indexes, counted from 1, as many as the switcher has.
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"usk", "on-air", "--keyer", ""}, []string{"1", "2", "3", "4"}},
		{[]string{"usk", "on-air", "--me", "1", "--keyer", "3"}, []string{"3"}},
		{[]string{"dsk", "tie", "--dsk", ""}, []string{"1", "2"}},
		{[]string{"aux", "--aux", ""}, []string{"1", "2", "3", "4", "5", "6"}},
		{[]string{"media", "player", ""}, []string{"1", "2"}},
		{[]string{"media", "player", "1", "--still", "2"}, []string{"2", "20"}},
		{[]string{"media", "download", "1"}, []string{"1", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19"}},
		{[]string{"program", "--me", ""}, []string{"1"}},
		// A macro by number or name, the ones there are.
		{[]string{"macro", "run", ""}, []string{"1", "3", "Half Time", "Open Show"}},
		{[]string{"macro", "run", "h"}, []string{"Half Time"}},
		{[]string{"macro", "record", "--slot", ""}, []string{"1", "2", "3"}},
		{[]string{"multiviewer", "input", "1", "cam1", "--multiviewer", ""}, []string{"1"}},
		{[]string{"supersource", "properties", "--supersource", ""}, nil}, // it has none
		{[]string{"hyperdeck-slot", "1"}, []string{"1", "10"}},
		// The alias completes as the commands it stands for.
		{[]string{"audio", "strip", "ca"}, []string{"Camera 1", "Camera 2"}},
	} {
		if got := complete(append([]string{"--host", "studio"}, c.args...)...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.args, got, c.want)
		}
	}
	// No engine: no names, and no engine started.
	b.engine().Stop()
	if got := complete("--host", "studio", "program", ""); got != nil {
		t.Errorf("sources without an engine: %q", got)
	}
	if got := complete("--host", "studio", "usk", "on-air", "--keyer", ""); got != nil {
		t.Errorf("indexes without an engine: %q", got)
	}
	// Every ATEM has two colour generators, four SuperSource boxes and
	// six EQ bands: no engine needed.
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"color", ""}, []string{"1", "2"}},
		{[]string{"supersource", "box", ""}, []string{"1", "2", "3", "4"}},
		{[]string{"fairlight", "master-eq", ""}, []string{"1", "2", "3", "4", "5", "6"}},
		{[]string{"fairlight", "eq", "cam1", ""}, []string{"1", "2", "3", "4", "5", "6"}},
		// The alias is hidden: audio lists the classic mixer's commands.
		{[]string{"audio", ""}, []string{"input", "levels", "master", "meters", "monitor"}},
	} {
		if got := complete(c.args...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.args, got, c.want)
		}
	}
	// __complete prints them one per line.
	code, out, _ := b.run("__complete", "transition", "m")
	if code != 0 || out != "mix\n" {
		t.Errorf("__complete: %d %q", code, out)
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		code, out, _ := runCLI("completion", sh)
		if code != 0 || !strings.Contains(out, "__complete") {
			t.Errorf("%s: %d %q", sh, code, out)
			continue
		}
		// Check the syntax with the shell itself, where it is installed.
		if path, err := exec.LookPath(sh); err == nil {
			f := filepath.Join(t.TempDir(), "c."+sh)
			os.WriteFile(f, []byte(out), 0o644)
			if msg, err := exec.Command(path, "-n", f).CombinedOutput(); err != nil {
				t.Errorf("%s -n: %v\n%s", sh, err, msg)
			}
		}
	}
	if code, _, _ := runCLI("completion", "tcsh"); code != exitUsage {
		t.Errorf("tcsh: %d", code)
	}
}

func TestManPage(t *testing.T) {
	code, page, _ := runCLI("man")
	if code != 0 || !strings.HasPrefix(page, ".TH BMDCTL 1") {
		t.Fatalf("%d %.80q", code, page)
	}
	reg, _ := engine.NewRegistry()
	for _, e := range reg.Entries("atem") {
		if !strings.Contains(page, ".SS \""+roff(e.Usage())+"\"\n") {
			t.Errorf("no section for %s", e.Path)
		}
	}
	for _, want := range []string{".SH EXIT STATUS", ".SH ENVIRONMENT", `\fB\-\-host H\fR`, `exec FILE|\- [\-\-continue]`} {
		if !strings.Contains(page, want) {
			t.Errorf("lacks %q", want)
		}
	}
	for _, r := range page {
		if r >= 0x80 {
			t.Fatalf("non-ASCII %q", r)
		}
	}
	// groff, where it is installed, has nothing to warn about.
	if groff, err := exec.LookPath("groff"); err == nil {
		cmd := exec.Command(groff, "-man", "-Tutf8", "-ww", "-z")
		cmd.Stdin = strings.NewReader(page)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil || stderr.Len() > 0 {
			t.Errorf("groff: %v\n%s", err, stderr.String())
		}
	}
	dir := t.TempDir()
	if code, out, _ := runCLI("man", "--dir", dir); code != 0 || !strings.Contains(out, "bmdctl.1") {
		t.Errorf("--dir: %d %q", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bmdctl.1")); string(got) != page {
		t.Error("--dir wrote another page")
	}
}
