// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

// Group c, end to end against the fake switcher: the upstream and
// downstream keyers, aux, input rename and list, colour generators.
func TestKeyerCommands(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		args      []string
		code, hex string
		confirmed bool
	}{
		{[]string{"usk", "on-air", "--keyer", "2", "--on-air"}, "CKOn", "00010100", true},
		{[]string{"usk", "fill", "--keyer", "1", "--source", "Media Player 1"}, "CKeF", "00000bc2", true},
		{[]string{"usk", "key", "--keyer", "1", "--source", "MP1K"}, "CKeC", "00000bc3", true},
		{[]string{"usk", "type", "--keyer", "3", "--type", "dve", "--fly"}, "CKTp", "0300020301000000", true},
		{[]string{"usk", "luma", "--keyer", "1", "--clip", "50"}, "CKLm", "0200000001f4000000000000", true},
		{[]string{"usk", "chroma", "--keyer", "1", "--spill", "0.5"}, "CACK", "0008000000000000000001f400000000000000000000000000000000", true},
		{[]string{"usk", "chroma-sample", "--keyer", "1", "--cursor"}, "CACC", "0100000100000000000000000000000000000000", true},
		{[]string{"usk", "pattern", "--keyer", "1", "--pattern", "DiamondIris"}, "CKPt", "01000006000000000000000000000000", true},
		{[]string{"usk", "mask", "--keyer", "1", "--enabled"}, "CKMs", "010000010000000000000000", true},
		{[]string{"usk", "dve", "--keyer", "1", "--size-x", "0.5"}, "CKDV", "", true},
		{[]string{"usk", "keyframe-set", "--keyer", "1", "--keyframe", "a"}, "SFKF", "00000100", true},
		{[]string{"usk", "keyframe-set", "--keyer", "1", "--keyframe", "full"}, "SFKF", "00000300", true},
		{[]string{"usk", "keyframe-run", "--keyer", "1", "--to", "b"}, "RFlK", "0000000002000000", false},
		{[]string{"dsk", "on-air", "--dsk", "2", "--on-air"}, "CDsL", "01010000", true},
		{[]string{"dsk", "tie", "--tie"}, "CDsT", "00010000", true},
		{[]string{"dsk", "rate", "--dsk", "2", "--rate", "1:00"}, "CDsR", "01190000", true},
		{[]string{"dsk", "fill", "--source", "cam1"}, "CDsF", "00000001", true},
		{[]string{"dsk", "key", "--source", "2"}, "CDsC", "00000002", true},
		{[]string{"dsk", "gain", "--gain", "20"}, "CDsG", "", true},
		{[]string{"dsk", "mask", "--enabled"}, "CDsM", "", true},
		{[]string{"dsk", "auto", "--dsk", "2"}, "DDsA", "01000000", false},
		{[]string{"aux", "--aux", "2", "--source", "Color 1"}, "CAuS", "010107d1", true},
		{[]string{"input", "rename", "--source", "Camera 2", "--long-name", "Stage Left", "--short-name", "STGL"}, "CInL", "", true},
		{[]string{"color", "--generator", "1", "--hue", "120"}, "CClV", "0101" + "04b0" + "0000" + "0000", true},
	} {
		out, got := b.mustSend(t, c.code, with(c.args...)...)
		if c.hex != "" && got != c.hex {
			t.Errorf("%v: sent %s %s, want %s", c.args, c.code, got, c.hex)
		}
		status := "sent"
		if c.confirmed {
			status = "confirmed"
		}
		if !strings.Contains(out, status) {
			t.Errorf("%v: %q, want %s", c.args, out, status)
		}
	}
	// The DSK's tie survives a mirrored gain change: the fake answers with
	// the state it modelled, not the dump's.
	if out := b.must(with("state", "--path", "dsks.0.properties.tie")...); out != "true\n" {
		t.Errorf("tie after gain: %q", out)
	}
	// The rename shows in the names: the old one is gone, the new one
	// works, and input list shows it.
	if code, _, _ := b.run(with("program", "--source", "Camera 2")...); code != exitUsage {
		t.Errorf("old name still resolves: %d", code)
	}
	b.must(with("program", "--source", "stage left")...)
	if got := b.lastSent(t, "CPgI"); got != "00000002" {
		t.Errorf("new name: %s", got)
	}
	out := b.must(with("input", "list")...)
	for _, want := range []string{"1      Camera 1            CAM1  external", "2      Stage Left          STGL  external",
		"1000   Color Bars          Bars  bars", "10010  ME 1 Program        PGM   m/e output"} {
		if !strings.Contains(out, want) {
			t.Errorf("input list lacks %q:\n%s", want, out)
		}
	}
	// Toggles read the state: keyer 0 and DSK 0 start on air.
	for _, want := range []string{"00000000", "00000100", "00000000"} {
		if _, got := b.mustSend(t, "CKOn", with("usk", "toggle", "--keyer", "1")...); got != want {
			t.Errorf("usk toggle sent %s, want %s", got, want)
		}
	}
	for _, want := range []string{"00000000", "00010000"} {
		if _, got := b.mustSend(t, "CDsL", with("dsk", "toggle")...); got != want {
			t.Errorf("dsk toggle sent %s, want %s", got, want)
		}
	}
	if out := b.must(with("status")...); !strings.Contains(out, "me 1 key 1  off air") || !strings.Contains(out, "dsk 1       on air") {
		t.Errorf("status after toggles:\n%s", out)
	}
}
