// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite docs/COMMANDS.md")

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

// docs/COMMANDS.md is generated from the command table.
func TestCommandsDoc(t *testing.T) {
	got := registry().Markdown()
	path := "../../docs/COMMANDS.md"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatal("docs/COMMANDS.md is stale: run go test ./cmd/bmdctl -run TestCommandsDoc -update")
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI("version")
	if code != 0 || !strings.HasPrefix(out, "bmdctl ") {
		t.Fatalf("%d %q", code, out)
	}
	version, commit, date = "v1.2.3", "abc1234", "2026-10-09"
	defer func() { version, commit, date = "", "", "" }()
	if _, out, _ := runCLI("version"); out != "bmdctl v1.2.3 (abc1234, 2026-10-09)\n" {
		t.Fatalf("%q", out)
	}
}

// --help works for the device, every group and every command.
func TestHelpAtEveryLevel(t *testing.T) {
	r := registry()
	paths := map[string]bool{"": true}
	for _, e := range r.Entries("atem") {
		w := e.Words()
		for i := 1; i <= len(w); i++ {
			paths[strings.Join(w[:i], " ")] = true
		}
	}
	for p := range paths {
		args := append([]string{"atem"}, strings.Fields(p)...)
		code, out, errOut := runCLI(append(args, "--help")...)
		if code != 0 || errOut != "" || !strings.HasPrefix(out, "usage: bmdctl atem") {
			t.Errorf("%q --help: %d %q %q", p, code, out, errOut)
		}
	}
	if code, out, _ := runCLI("atem", "--help"); code != 0 || !strings.Contains(out, "--dry-run") || !strings.Contains(out, "transition ...") {
		t.Errorf("atem --help:\n%s", out)
	}
	if code, out, _ := runCLI("atem", "transition", "wipe", "-h"); code != 0 || !strings.Contains(out, "--width") ||
		!strings.Contains(out, "CircleIris=7") || !strings.Contains(out, "Echo: TWpP:<me>") {
		t.Errorf("wipe --help:\n%s", out)
	}
	if code, out, _ := runCLI("--help"); code != 0 || !strings.Contains(out, "atem") {
		t.Errorf("--help: %d %q", code, out)
	}
}

// --dry-run prints the bytes and sends nothing. Expected bytes are the
// ones bmdwire v1.3.0's tests pin, with the 8-byte header in front.
func TestDryRun(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"program", "--source", "5"}, "000c000043506749" + "00000005"},
		{[]string{"program", "--me", "1", "--source", "3010"}, "000c000043506749" + "01000bc2"},
		{[]string{"transition", "preview", "--me", "1", "--enabled", "true"}, "000c000043545072" + "01010000"},
		{[]string{"transition", "position", "--position", "20000"}, "000c000043545073" + "00002710"},
		{[]string{"multiviewer", "input", "--multiviewer", "1", "--window", "7", "--source", "3010"}, "000c0000434d7649" + "01070bc2"},
		{[]string{"auto-video-mode", "--enable", "true"}, "000c00004169564d" + "01000000"},
		{[]string{"streaming", "audio-bitrate", "--min", "128000", "--max", "128000"}, "0010000053544142" + "0001f4000001f400"},
		{[]string{"time-of-day", "--time", "2026-09-11T12:00:00Z"}, "0010000053546f44" + "6aa3ed4000000000"},
		{[]string{"camera", "--destination", "2", "--category", "8", "--parameter", "1", "--type", "fixed16", "--data", "0.5,-1.0"},
			"0020000043436d64" + "020801008000000000020000000000000400f80000000000"},
		{[]string{"transition", "mix", "--rate", "1:00"}, "000c00004354 4d78 0019 0000"},
		{[]string{"--fps", "30", "transition", "mix", "--rate", "1:00"}, "000c000043544d78001e0000"},
	} {
		code, out, errOut := runCLI(append([]string{"atem", "--dry-run"}, c.args...)...)
		want := strings.ReplaceAll(c.want, " ", "") + "\n"
		if code != 0 || out != want {
			t.Errorf("%v: %d %q %q, want %q", c.args, code, out, errOut, want)
		}
	}
	// --dry-run is accepted after the command too.
	if _, out, _ := runCLI("atem", "cut", "--me", "2", "--dry-run"); out != "000c00004443757402000000\n" {
		t.Errorf("trailing --dry-run: %q", out)
	}
}

func TestDryRunWarnsAndRefuses(t *testing.T) {
	code, _, errOut := runCLI("atem", "--dry-run", "video-mode", "--mode", "1080p5994")
	if code != 0 || !strings.Contains(errOut, "destructive") {
		t.Errorf("video-mode: %d %q", code, errOut)
	}
	code, _, errOut = runCLI("atem", "--dry-run", "audio", "levels", "--enable", "true")
	if code != 0 || !strings.Contains(errOut, "unverified") {
		t.Errorf("audio levels: %d %q", code, errOut)
	}
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"atem", "program", "--source", "5"}, 2, "add --dry-run"},
		{[]string{"atem", "--dry-run", "program"}, 2, "--source is required"},
		{[]string{"atem", "--dry-run", "program", "--source", "70000"}, 2, "out of range"},
		{[]string{"atem", "--dry-run", "program", "--source", "5", "--bogus", "1"}, 2, "has no --bogus"},
		{[]string{"atem", "--dry-run", "transition", "wipe"}, 2, "nothing to set"},
		{[]string{"atem", "--dry-run", "transition", "settings", "--next", "0"}, 1, "selects no layer"},
		{[]string{"atem", "--dry-run", "media", "player", "--player", "0"}, 2, "nothing to set"},
		{[]string{"atem", "--dry-run", "media", "player", "--player", "0", "--still", "1", "--clip", "2"}, 1, "not both"},
		{[]string{"atem", "--dry-run", "transition", "stinger", "--clip", "7000"}, 1, "does not fit"},
		{[]string{"atem", "--dry-run", "nonsense"}, 2, "unknown command"},
		{[]string{"atem", "--dry-run", "transition"}, 2, "missing a subcommand"},
		{[]string{"nonsense"}, 2, "unknown command"},
		{nil, 2, "usage"},
	} {
		code, _, errOut := runCLI(c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d and %q", c.args, code, errOut, c.code, c.msg)
		}
	}
}
