// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem/macro"
)

const testOps = `[{"id":"ProgramInput","mixEffectBlockIndex":0,"input":"Camera1"},{"id":"MacroSleep","frames":"25"},` +
	`{"id":"PreviewInput","mixEffectBlockIndex":"0","input":"Camera2"}]`

// Group f, end to end against the fake switcher: macros listed, run,
// stopped, recorded, deleted, moved and converted.
func TestMacroCommands(t *testing.T) {
	b := newBench(t)
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(data), 0o644)
		return p
	}
	opsFile := write("ops.json", testOps)

	// encode and decode need no switcher: they run in the CLI.
	code, out, errOut := runCLI("macro", "encode", "--file", opsFile, "--out", filepath.Join(dir, "m.bin"))
	if code != 0 || !strings.Contains(out, "encoded 3 ops") {
		t.Fatalf("encode: %d %q %q", code, out, errOut)
	}
	bin, _ := os.ReadFile(filepath.Join(dir, "m.bin"))
	p := func(n, v string) macro.Param { return macro.Param{Name: n, Value: v} }
	want, _ := macro.Encode([]macro.Op{
		{ID: "ProgramInput", Params: []macro.Param{p("mixEffectBlockIndex", "0"), p("input", "Camera1")}},
		{ID: "MacroSleep", Params: []macro.Param{p("frames", "25")}},
		{ID: "PreviewInput", Params: []macro.Param{p("mixEffectBlockIndex", "0"), p("input", "Camera2")}},
	})
	if !bytes.Equal(bin, want) {
		t.Fatalf("encoded %x, want %x", bin, want)
	}
	code, out, _ = runCLI("macro", "decode", "--file", filepath.Join(dir, "m.bin"))
	if code != 0 || !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`+"\n<Macro>\n    <Op id=\"ProgramInput\"") ||
		!strings.Contains(out, `<Op id="MacroSleep" frames="25"/>`) {
		t.Fatalf("decode: %d %q", code, out)
	}
	write("m.xml", out)
	code, out, _ = runCLI("macro", "decode", "--file", filepath.Join(dir, "m.bin"), "--json")
	if code != 0 || !strings.Contains(out, `"ops":[{"id":"ProgramInput","mixEffectBlockIndex":"0","input":"Camera1"},`) {
		t.Errorf("decode --json: %d %q", code, out)
	}
	// XML encodes back to the same bytes.
	if code, _, errOut := runCLI("macro", "encode", "--file", filepath.Join(dir, "m.xml"), "--out", filepath.Join(dir, "m2.bin")); code != 0 {
		t.Fatalf("encode xml: %q", errOut)
	}
	if bin2, _ := os.ReadFile(filepath.Join(dir, "m2.bin")); !bytes.Equal(bin2, want) {
		t.Errorf("xml round trip %x", bin2)
	}
	if code, _, errOut := runCLI("macro", "encode", "--file", write("bad.json", `[{"id":"NoSuchOp"}]`), "--out", "x.bin"); code != exitUsage || !strings.Contains(errOut, "ValueError") {
		t.Errorf("bad op: %d %q", code, errOut)
	}

	// Upload, list, download.
	out = b.must(with("macro", "upload", "--slot", "5", "--file", filepath.Join(dir, "m.xml"), "--name", "Intro", "--description", "opens")...)
	if !strings.Contains(out, `confirmed`) || !strings.Contains(out, `slot 5 "Intro" from 3 ops`) {
		t.Errorf("upload: %q", out)
	}
	sw := b.switchers()[0]
	if got, name, desc, ok := sw.Macro(5); !ok || !bytes.Equal(got, want) || name != "Intro" || desc != "opens" {
		t.Errorf("switcher holds %x %q %q %v", got, name, desc, ok)
	}
	out = b.must(with("macro", "list")...)
	if !strings.Contains(out, "macro 5  Intro") || !strings.Contains(out, "opens") {
		t.Errorf("list:\n%s", out)
	}
	if code, _, errOut := b.run(with("macro", "upload", "--slot", "5", "--file", opsFile)...); code != exitDestructive || !strings.Contains(errOut, `holds "Intro"`) {
		t.Errorf("replace: %d %q", code, errOut)
	}
	out = b.must(with("macro", "download", "--slot", "intro")...)
	data, err := os.ReadFile(filepath.Join(dir, "Intro.xml"))
	if err != nil || !strings.Contains(string(data), `<Macro index="5" name="Intro" description="opens">`) {
		t.Errorf("download: %q %v\n%s", out, err, data)
	}
	b.must(with("macro", "download", "--slot", "5", "--file", "d.bin")...)
	if got, _ := os.ReadFile(filepath.Join(dir, "d.bin")); !bytes.Equal(got, want) {
		t.Errorf("download bin %x", got)
	}
	b.must(with("macro", "download", "--slot", "5", "--file", "d.json")...)
	if got, _ := os.ReadFile(filepath.Join(dir, "d.json")); !strings.Contains(string(got), `"name": "Intro"`) {
		t.Errorf("download json %s", got)
	}

	// Run and stop.
	out = b.must(with("macro", "run", "--slot", "Intro")...)
	if !strings.Contains(out, "confirmed") || !strings.Contains(out, "slot 5 (Intro)") || b.lastSent(t, "MAct") != "00050000" {
		t.Errorf("run: %q", out)
	}
	if out := b.must(with("macro", "list")...); !strings.Contains(out, "running") {
		t.Errorf("list while running:\n%s", out)
	}
	if b.must(with("macro", "stop")...); b.lastSent(t, "MAct") != "ffff0100" {
		t.Errorf("stop sent %s", b.lastSent(t, "MAct"))
	}
	// Record, a wait, stop recording.
	b.must(with("macro", "record", "--slot", "7", "--name", "Rec")...)
	b.mustSend(t, "MSlp", with("macro", "sleep", "--frames", "25")...)
	b.must(with("macro", "action", "--action", "stop-record")...)
	if out := b.must(with("macro", "list")...); !strings.Contains(out, "macro 7  Rec") {
		t.Errorf("after recording:\n%s", out)
	}
	if code, _, _ := b.run(with("macro", "record", "--slot", "7", "--name", "Again")...); code != exitDestructive {
		t.Errorf("record over a macro: %d", code)
	}
	// Delete is destructive, by slot or by name.
	if code, _, _ := b.run(with("macro", "delete", "--slot", "7")...); code != exitDestructive {
		t.Errorf("delete without --yes: %d", code)
	}
	if code, _, _ := b.run(with("macro", "action", "--slot", "7", "--action", "delete")...); code != exitDestructive {
		t.Errorf("action delete without --yes: %d", code)
	}
	out = b.must(with("macro", "delete", "--slot", "rec", "--yes")...)
	if !strings.Contains(out, "confirmed") || b.lastSent(t, "MAct") != "00070500" {
		t.Errorf("delete: %q", out)
	}
	if code, _, errOut := b.run(with("macro", "run", "--slot", "nobody")...); code != exitUsage || !strings.Contains(errOut, "no macro has that name") {
		t.Errorf("unknown name: %d %q", code, errOut)
	}
	if code, _, errOut := b.run(with("macro", "run", "--slot", "7")...); code != exitUsage || !strings.Contains(errOut, "slot 7 is empty") {
		t.Errorf("empty slot: %d %q", code, errOut)
	}
}
