// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitWords(t *testing.T) {
	for in, want := range map[string][]string{
		``:                                 nil,
		`   # just a comment`:              nil,
		`program --source 5`:               {"program", "--source", "5"},
		`program --source "Camera 2"`:      {"program", "--source", "Camera 2"},
		`input rename --long-name 'It''s'`: {"input", "rename", "--long-name", "Its"},
		`a "b \"c\" \\ \x" d\ e`:           {"a", `b "c" \ \x`, "d e"},
		`cut # cut to camera 2`:            {"cut"},
		`name a#b`:                         {"name", "a#b"},
		"tab\tseparated\t\twords":          {"tab", "separated", "words"},
		`""`:                               {""},
		`x"y"z`:                            {"xyz"},
	} {
		got, err := splitWords(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`"open`, `'open`, `trailing\`} {
		if _, err := splitWords(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// exec runs a file's commands in order, through the engine, to one host.
func TestExec(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.11", "--as", "other")
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	cue := write("cue.txt", `# opening
program "Camera 2"
bmdctl preview CAM1

--host other cut
cut   # back on the show's switcher
`)
	code, out, errOut := b.run("exec", cue, "--host", "studio")
	if code != 0 || errOut != "" { // hosts add opened both sessions
		t.Fatalf("exec: %d %q %q", code, out, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "studio (192.0.2.10)  program") || !strings.HasPrefix(lines[1], "studio (192.0.2.10)  preview") ||
		!strings.HasPrefix(lines[2], "other (192.0.2.11)  cut") || !strings.HasPrefix(lines[3], "studio (192.0.2.10)  cut") {
		t.Errorf("output:\n%s", out)
	}
	// The status column lines up across the commands to one switcher.
	column := func(l, word string) int { return utf8.RuneCountInString(l[:strings.Index(l, word)]) }
	if column(lines[0], "confirmed") != column(lines[3], "confirmed") {
		t.Errorf("not aligned:\n%s", out)
	}
	// Every line went to studio but the one that named other.
	if n := len(b.sent("DCut")); n != 2 {
		t.Errorf("%d cuts", n)
	}
	if got := b.wentTo(t, "DCut", "exec", write("cut.txt", "cut\n"), "--host", "studio"); got != "192.0.2.10:9910" {
		t.Errorf("cut went to %s", got)
	}
	// exec doesn't change the last host used.
	if got := b.cliLastHost(); got != "" {
		t.Errorf("last host %q", got)
	}

	// Every line is checked before the first is sent.
	n := len(b.sent("CPgI"))
	bad := write("bad.txt", "program 1\npreview\ntransition wipe --pattern Spiral\nvideo-mode 1080p50\nexec x\n")
	code, _, errOut = b.run("exec", bad, "--host", "studio")
	if code != exitUsage || len(b.sent("CPgI")) != n {
		t.Errorf("bad file ran: %d", code)
	}
	for _, want := range []string{"nothing run", "line 2: preview: missing <source>: preview <source>", "line 3: transition wipe: --pattern",
		"line 4: video-mode is destructive", "line 5: exec can't run inside exec"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("errors lack %q:\n%s", want, errOut)
		}
	}

	// It stops at the first failure, with its status, unless --continue.
	stop := write("stop.txt", "program 1\nprogram Nobody\nprogram 2\n")
	n = len(b.sent("CPgI"))
	code, _, errOut = b.run("exec", stop, "--host", "studio")
	if code != exitUsage || len(b.sent("CPgI")) != n+1 || !strings.Contains(errOut, `line 2: bmdctl atem program: source "Nobody"`) {
		t.Errorf("stop: %d %d %q", code, len(b.sent("CPgI"))-n, errOut)
	}
	// Line 1's program 1 is on program already, so only line 3 sends.
	code, _, _ = b.run("exec", stop, "--host", "studio", "--continue")
	if code != exitUsage || len(b.sent("CPgI")) != n+2 {
		t.Errorf("--continue: %d %d", code, len(b.sent("CPgI"))-n)
	}

	// --json: one line per command, each the engine's response.
	code, out, _ = b.run("exec", stop, "--host", "studio", "--continue", "--json")
	jl := strings.Split(strings.TrimSpace(out), "\n")
	if code != exitUsage || len(jl) != 3 {
		t.Fatalf("--json: %d\n%s", code, out)
	}
	for i, want := range []bool{true, false, true} {
		var r struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal([]byte(jl[i]), &r); err != nil || r.OK != want {
			t.Errorf("--json line %d: %s", i, jl[i])
		}
	}

	// From stdin; a destructive line needs --yes, on the line or on exec.
	if code, _, errOut := b.runIn("startup save\n", "exec", "-", "--host", "studio"); code != exitUsage || !strings.Contains(errOut, "destructive") {
		t.Errorf("destructive: %d %q", code, errOut)
	}
	if code, _, errOut := b.runIn("startup save\n", "exec", "-", "--host", "studio", "--yes"); code != 0 {
		t.Errorf("destructive with --yes: %d %q", code, errOut)
	}
	if code, _, _ := b.run("exec"); code != exitUsage {
		t.Errorf("no file: %d", code)
	}
	if code, _, errOut := b.run("exec", write("q.txt", `program --source "open`)); code != exitUsage || !strings.Contains(errOut, "line 1: an unterminated") {
		t.Errorf("unterminated: %d %q", code, errOut)
	}
}
