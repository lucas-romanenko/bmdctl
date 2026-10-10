// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// Group g, end to end against the fake switcher: a profile saved with
// its images, the images alone, and the profile restored with the images
// it names.
func TestProfileCommands(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: small stills
	b := newBenchDump(t, dump)
	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("logo.png", testPNG(t), 0o644)
	os.WriteFile("ops.json", []byte(testOps), 0o644)
	// Still 3 and macro 5, counted from 1: slots 2 and 4 in the switcher
	// and in the profile's XML, which counts from 0 as ASC writes it.
	b.must(with("media", "upload", "3", "logo.png", "--name", "Logo")...)
	b.must(with("macro", "upload", "5", "ops.json", "--name", "Cams")...)

	out := b.must(with("profile", "save", "prof/studio.xml")...)
	if !strings.Contains(out, "saved prof/studio.xml: ATEM 1 M/E Constellation HD 525i59.94 4:3") ||
		!strings.Contains(out, "still 3  Logo   ATEM Media Pool/Logo.png") {
		t.Errorf("save:\n%s", out)
	}
	// Still 1 (slot 0) is used in the dump, but the fake has no frame for it: it is
	// skipped, as atemwire skips a slot that fails.
	if !strings.Contains(out, "still 1") {
		t.Errorf("still 1 not reported:\n%s", out)
	}
	xml, err := os.ReadFile("prof/studio.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<?xml version="1.0" encoding="UTF-8"?>`, `<Profile majorVersion="2"`,
		`<Still index="2" name="Logo" path="ATEM Media Pool/Logo.png"/>`, `<Macro index="4" name="Cams"`,
		`<Op id="ProgramInput" mixEffectBlockIndex="0" input="Camera1"/>`} {
		if !bytes.Contains(xml, []byte(want)) {
			t.Errorf("profile lacks %s", want)
		}
	}
	if _, err := os.Stat(filepath.Join("prof", "ATEM Media Pool", "Logo.png")); err != nil {
		t.Errorf("image: %v", err)
	}
	// Sections can be left out.
	b.must(with("profile", "save", "lean.xml", "--macros=false", "--media-pool-images", "false", "--keyers", "none")...)
	lean, _ := os.ReadFile("lean.xml")
	if bytes.Contains(lean, []byte("<MacroPool")) || bytes.Contains(lean, []byte("<Key ")) {
		t.Errorf("lean profile:\n%s", lean)
	}
	if _, err := os.Stat("ATEM Media Pool"); !os.IsNotExist(err) {
		t.Errorf("images saved for lean: %v", err)
	}

	// profile images alone.
	out = b.must(with("profile", "images", "--dir", "imgs")...)
	if _, err := os.Stat(filepath.Join("imgs", "Logo.png")); err != nil || !strings.Contains(out, "imgs") && !strings.Contains(out, "Logo.png") {
		t.Errorf("images: %v\n%s", err, out)
	}

	// Restore: destructive; it applies the profile, then uploads the
	// images it names into their slots.
	if code, _, _ := b.run(with("profile", "restore", "prof/studio.xml")...); code != exitDestructive {
		t.Errorf("restore without --yes: %d", code)
	}
	b.must(with("media", "clear", "3", "--yes")...)
	b.switchers()[0].PutMacro(4, nil, "", "")
	n := len(b.sent("CTTp"))
	out = b.must(with("profile", "restore", "prof/studio.xml", "--yes", "--video-mode", "false")...)
	if !strings.Contains(out, "restored prof/studio.xml (Test") && !strings.Contains(out, "restored prof/studio.xml") {
		t.Errorf("restore:\n%s", out)
	}
	if !strings.Contains(out, "still 3  Logo") {
		t.Errorf("restore images:\n%s", out)
	}
	if len(b.sent("CTTp")) == n {
		t.Error("no transition settings sent")
	}
	if _, name, _, ok := b.switchers()[0].Still(2); !ok || name != "Logo" {
		t.Errorf("still 2 not restored: %q %v", name, ok)
	}
	if code, ops, name, _ := b.switchers()[0].Macro(4); len(code) == 0 || ops != "Cams" && name == "" {
		t.Errorf("macro 4 not restored: %x %q", code, ops)
	}
	var r struct {
		Result engine.ProfileResult `json:"result"`
	}
	json.Unmarshal([]byte(b.must(with("profile", "restore", "prof/studio.xml", "--yes", "--video-mode", "false", "--macros-only", "--json")...)), &r)
	if len(r.Result.Applied) != 1 || !strings.HasPrefix(r.Result.Applied[0], "macros") {
		t.Errorf("--macros-only applied %v", r.Result.Applied)
	}
	// A profile whose apply records errors exits 4, listing them.
	os.WriteFile("bad.xml", []byte(`<Profile majorVersion="2" minorVersion="1" product="Test">`+
		`<FairlightAudioMixer masterOutFaderGain="inf" followFadeToBlack="False"/></Profile>`), 0o644)
	code, out, _ := b.run(with("profile", "restore", "bad.xml", "--yes")...)
	if code != exitRejected || !strings.Contains(out, "error") || !strings.Contains(out, "fairlight.master: ") {
		t.Errorf("bad profile: %d\n%s", code, out)
	}
	os.WriteFile("broken.xml", []byte(`<Profile><Unclosed></Profile>`), 0o644)
	if code, _, errOut := b.run(with("profile", "restore", "broken.xml", "--yes")...); code != exitUsage || !strings.Contains(errOut, "broken.xml") {
		t.Errorf("broken: %d %q", code, errOut)
	}
}
