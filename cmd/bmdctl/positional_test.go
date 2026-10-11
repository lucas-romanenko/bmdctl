// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
		{"preview cam2", "CPvI", "00000002"},
		{"program 1 --me 1", "CPgI", "00000001"},
		{"usk 1 fill cam2", "CKeF", "00000002"},
		{"aux 2 mp1", "CAuS", "01010bc2"},
		{"transition rate 0:20", "CTDp", "0100140000000000"},
		{"color 1 --hue 180 --sat 50", "CClV", "0300070801f40000"},
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
	if !regexp.MustCompile(`  media upload 10 +"X" from 64x36 png +confirmed`).MatchString(out) {
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
// release, with a notice on stderr, and nothing shows it.
func TestOldFlagForm(t *testing.T) {
	for old, now := range map[string]string{
		"program --source 5":                                "program 5",
		"program --me 2 --source 3010":                      "program 3010 --me 2",
		"usk on-air --keyer 2 --on-air":                     "usk 2 on-air on",
		"usk fill --keyer 1 --source 7":                     "usk 1 fill 7",
		"aux --aux 2 --source 2001":                         "aux 2 2001",
		"transition mix --rate 1:00":                        "transition mix 1:00",
		"color --generator 1 --hue 180":                     "color 1 --hue 180",
		"streaming audio-bitrate --min 8 --max 9":           "streaming audio-bitrate 8 9",
		"camera --destination 2 --category 8 --parameter 1": "camera 2 8 1",
		"macro action --action stop":                        "macro action stop",
		"dsk on-air --on-air --dsk 2":                       "dsk on-air on --dsk 2",
		"audio eq --source 1 --channel -1 --band 1 --q 2":   "audio eq 1 1 --channel -1 --q 2",
	} {
		_, o1, e1 := runCLI(append([]string{"--dry-run"}, strings.Fields(old)...)...)
		_, o2, e2 := runCLI(append([]string{"--dry-run"}, strings.Fields(now)...)...)
		if o1 == "" || o1 != o2 {
			t.Errorf("%q gives %q %q, %q gives %q %q", old, o1, e1, now, o2, e2)
		}
		// One line before whatever else the command says, naming the line
		// as it is written now.
		notice, rest, _ := strings.Cut(e1, "\n")
		if !strings.HasPrefix(notice, "bmdctl: --") || !strings.HasSuffix(notice, " deprecated, use: "+now) || rest != e2 {
			t.Errorf("%q: stderr %q, want the notice then %q", old, e1, e2)
		}
	}
	if _, _, errOut := runCLI("--dry-run", "program", "--source", "5"); errOut != "bmdctl: --source is deprecated, use: program 5\n" {
		t.Errorf("notice %q", errOut)
	}
	if _, _, errOut := runCLI("--dry-run", "usk", "on-air", "--keyer", "2", "--on-air"); errOut != "bmdctl: --keyer and --on-air are deprecated, use: usk 2 on-air on\n" {
		t.Errorf("notice %q", errOut)
	}
	// Help, COMMANDS.md and completion show the arguments positionally.
	_, help, _ := runCLI("program", "--help")
	if !strings.Contains(help, "\nusage: bmdctl [atem] program <source> [--flag value ...]") || strings.Contains(help, "--source") ||
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

// The notice is printed once per invocation, however many of exec's lines
// use the old form.
func TestOldFlagFormOnce(t *testing.T) {
	b := newBench(t)
	script := filepath.Join(t.TempDir(), "cue.txt")
	os.WriteFile(script, []byte("program --source 1\npreview --source 2\ncut\n"), 0o644)
	code, out, errOut := b.run(with("exec", script)...)
	if code != 0 || strings.Count(out, "\n") != 3 || strings.Count(errOut, "deprecated") != 1 ||
		!strings.HasPrefix(errOut, "bmdctl: --source is deprecated, use: program 1\n") {
		t.Errorf("exec: %d %q %q", code, out, errOut)
	}
}

// fairlight, atemwire's name for the audio group, still works, and
// nothing lists it.
func TestFairlightAlias(t *testing.T) {
	_, o1, e1 := runCLI("--dry-run", "fairlight", "strip", "1", "--volume", "-10")
	_, o2, _ := runCLI("--dry-run", "audio", "strip", "1", "--volume", "-10")
	if o1 == "" || o1 != o2 || e1 != "" {
		t.Errorf("fairlight strip: %q %q, audio strip: %q", o1, e1, o2)
	}
	if _, help, _ := runCLI("fairlight", "eq", "--help"); !strings.Contains(help, "\nusage: bmdctl [atem] audio eq <source> <band>") {
		t.Errorf("fairlight eq --help:\n%s", help)
	}
	// The groups: audio for the Fairlight mixer and both mixers' shared
	// commands, classic-audio for the classic mixer's.
	_, audio, _ := runCLI("audio", "--help")
	_, classic, _ := runCLI("classic-audio", "--help")
	for _, want := range []string{"  strip <source>", "  master ", "  levels <enable>", "  meters "} {
		if !strings.Contains(audio, want) {
			t.Errorf("audio --help lacks %q:\n%s", want, audio)
		}
	}
	for _, want := range []string{"  input <source>", "  master ", "  monitor ", "  levels <enable>", "  meters "} {
		if !strings.Contains(classic, want) {
			t.Errorf("classic-audio --help lacks %q:\n%s", want, classic)
		}
	}
	// Nothing lists the alias.
	_, top, _ := runCLI("atem", "--help")
	reg, _ := engine.NewRegistry()
	for what, text := range map[string]string{"atem --help": top, "COMMANDS.md": commandsDoc(reg)} {
		if strings.Contains(text, "fairlight strip") || strings.Contains(text, "fairlight ...") || strings.Contains(text, "  fairlight") {
			t.Errorf("%s shows the alias", what)
		}
	}
	// Naming no command, it gets the audio group's help.
	if code, _, errOut := runCLI("--dry-run", "fairlight", "bogus"); code != exitUsage ||
		!strings.Contains(errOut, `unknown command "fairlight bogus"`) || !strings.Contains(errOut, "bmdctl [atem] audio <command>") {
		t.Errorf("fairlight bogus: %d %q", code, errOut)
	}
	if _, help, _ := runCLI("fairlight", "--help"); !strings.HasPrefix(help, "usage: bmdctl [atem] audio <command>") {
		t.Errorf("fairlight --help:\n%s", help)
	}
}
