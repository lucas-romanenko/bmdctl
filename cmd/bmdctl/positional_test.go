// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// The examples that set the syntax, as typed, through the engine to the
// fake switcher.
func TestPositionalExamples(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: small stills
	b := newBenchDump(t, dump)
	t.Chdir(t.TempDir())
	os.WriteFile("test.png", testPNG(t), 0o644)
	for _, c := range []struct {
		line      string
		code, hex string
	}{
		{"preview cam1", "CPvI", "00000001"},
		{"program 2 --me 1", "CPgI", "00000002"},
		{"usk 1 fill cam2", "CKeF", "00000002"},
		{"aux 2 mp1", "CAuS", "01010bc2"},
		{"transition rate 1:00", "CTDp", "01001e0000000000"}, // NTSC: one second is 30 frames
		{"color 1 --hue 180 --sat 50", "CClV", "030107080 1f40000"},
		{"macro run 3", "MAct", "00020000"},
	} {
		if _, got := b.mustSend(t, c.code, with(strings.Fields(c.line)...)...); got != strings.ReplaceAll(c.hex, " ", "") {
			t.Errorf("%s: sent %s %s, want %s", c.line, c.code, got, c.hex)
		}
	}
	// --me 2 on a one-M/E switcher is refused, by the engine, as M/E 2.
	if code, _, errOut := b.run(with("program", "5", "--me", "2")...); code != exitUsage || !strings.Contains(errOut, "--me 2: the switcher has 1 M/E") {
		t.Errorf("program 5 --me 2: %d %q", code, errOut)
	}
	out := b.must(with("media", "upload", "10", "test.png", "--name", "X")...)
	if !strings.Contains(out, `slot 10 "X" from 64x36 png`) {
		t.Errorf("media upload: %q", out)
	}
	if _, name, _, ok := b.switchers()[0].Still(9); !ok || name != "X" {
		t.Errorf("still 10: %q %v", name, ok)
	}
	out = b.must(with("media", "download", "10", "out.png")...)
	if _, err := os.Stat("out.png"); err != nil || !strings.Contains(out, "to out.png") {
		t.Errorf("media download: %q %v", out, err)
	}
	b.must(with("profile", "save", "out.xml", "--media-pool-images=false")...)
	if xml, err := os.ReadFile("out.xml"); err != nil || !bytes.HasPrefix(xml, []byte("<?xml")) {
		t.Errorf("profile save: %v", err)
	}
}

// The --name form of a positional argument still works, for this
// release, and nothing shows it.
func TestOldFlagForm(t *testing.T) {
	for old, now := range map[string]string{
		"program --source 5":                                  "program 5",
		"program --me 2 --source 3010":                        "program 3010 --me 2",
		"usk on-air --keyer 2 --on-air":                       "usk 2 on-air on",
		"usk fill --keyer 1 --source 7":                       "usk 1 fill 7",
		"aux --aux 2 --source 2001":                           "aux 2 2001",
		"transition mix --rate 1:00":                          "transition mix 1:00",
		"color --generator 1 --hue 180":                       "color 1 --hue 180",
		"streaming audio-bitrate --min 8 --max 9":             "streaming audio-bitrate 8 9",
		"camera --destination 2 --category 8 --parameter 1":   "camera 2 8 1",
		"macro action --action stop":                          "macro action stop",
		"dsk on-air --on-air --dsk 2":                         "dsk on-air on --dsk 2",
		"fairlight eq --source 1 --channel -1 --band 0 --q 2": "fairlight eq 1 0 --q 2",
	} {
		_, o1, e1 := runCLI(append([]string{"--dry-run"}, strings.Fields(old)...)...)
		_, o2, e2 := runCLI(append([]string{"--dry-run"}, strings.Fields(now)...)...)
		if o1 == "" || o1 != o2 {
			t.Errorf("%q gives %q %q, %q gives %q %q", old, o1, e1, now, o2, e2)
		}
	}
	// Help, COMMANDS.md and completion show the arguments positionally.
	_, help, _ := runCLI("program", "--help")
	if !strings.HasPrefix(help, "usage: bmdctl [atem] program <source> [--flag value ...]") || strings.Contains(help, "--source") ||
		!strings.Contains(help, "  <source>  source, 0-65535") {
		t.Errorf("help:\n%s", help)
	}
	reg, _ := engine.NewRegistry()
	md := commandsDoc(reg)
	for _, want := range []string{"### `bmdctl atem program <source>`", "### `bmdctl atem usk <keyer> fill <source>`",
		"### `bmdctl atem media download <slot> [file]`", "| `<source>` | source, 0-65535 | required |", "| `[file]` |"} {
		if !strings.Contains(md, want) {
			t.Errorf("COMMANDS.md lacks %q", want)
		}
	}
	if strings.Contains(md, "| `--source` | source, 0-65535 | required |") {
		t.Error("COMMANDS.md shows --source")
	}
}
