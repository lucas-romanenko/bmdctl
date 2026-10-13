// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// Group c, end to end against the fake switcher: the upstream and
// downstream keyers, aux, input rename and inputs, colour generators.
func TestKeyerCommands(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		args      []string
		code, hex string
		confirmed bool
	}{
		{[]string{"usk", "2", "on-air", "on"}, "CKOn", "00010100", true},
		{[]string{"usk", "1", "fill", "Media Player 1"}, "CKeF", "00000bc2", true},
		{[]string{"usk", "1", "key", "MP1K"}, "CKeC", "00000bc3", true},
		{[]string{"usk", "3", "type", "--type", "dve", "--fly"}, "CKTp", "0300020301000000", true},
		{[]string{"usk", "1", "luma", "--clip", "50"}, "CKLm", "0200000001f4000000000000", true},
		{[]string{"usk", "1", "chroma", "--spill", "0.5"}, "CACK", "0008000000000000000001f400000000000000000000000000000000", true},
		{[]string{"usk", "1", "chroma-sample", "--cursor"}, "CACC", "0100000100000000000000000000000000000000", true},
		{[]string{"usk", "1", "pattern", "--pattern", "DiamondIris"}, "CKPt", "01000006000000000000000000000000", true},
		{[]string{"usk", "1", "mask", "--enabled"}, "CKMs", "010000010000000000000000", true},
		{[]string{"usk", "1", "dve", "--size-x", "0.5"}, "CKDV", "", true},
		{[]string{"usk", "1", "keyframe-set", "a"}, "SFKF", "00000100", true},
		{[]string{"usk", "1", "keyframe-set", "full"}, "SFKF", "00000300", true},
		{[]string{"usk", "1", "keyframe-run", "b"}, "RFlK", "0000000002000000", false},
		{[]string{"usk", "1", "keyframe-run", "runToInfinite", "--direction", "MiddleCentre"}, "RFlK", "0200000004050000", false},
		{[]string{"dsk", "on-air", "on", "--dsk", "2"}, "CDsL", "01010000", true},
		{[]string{"dsk", "tie", "on"}, "CDsT", "00010000", true},
		{[]string{"dsk", "rate", "0:20", "--dsk", "2"}, "CDsR", "01140000", true},
		{[]string{"dsk", "fill", "cam1"}, "CDsF", "00000001", true},
		{[]string{"dsk", "key", "2"}, "CDsC", "00000002", true},
		{[]string{"dsk", "gain", "--gain", "20"}, "CDsG", "", true},
		{[]string{"dsk", "mask", "--enabled", "--top", "2"}, "CDsM", "", true},
		{[]string{"dsk", "auto", "--dsk", "2"}, "DDsA", "01000000", false},
		{[]string{"aux", "2", "Color 1"}, "CAuS", "010107d1", true},
		{[]string{"input", "rename", "Camera 2", "--long-name", "Stage Left", "--short-name", "STGL"}, "CInL", "", true},
		{[]string{"color", "2", "--hue", "120"}, "CClV", "0101" + "04b0" + "0000" + "0000", true},
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
	// The rename shows in the names: the new one works, and inputs shows
	// it. "Camera 2" still reaches input 2 as an alias, whatever its name;
	// a name nothing has points at inputs.
	if out := b.must(with("--json", "program", "Camera 2")...); !strings.Contains(out, `"source":"2 (Stage Left)"`) {
		t.Errorf("alias: %s", out)
	}
	if code, _, errOut := b.run(with("program", "Stage Right")...); code != exitUsage ||
		!strings.Contains(errOut, `no input is named "Stage Right" (bmdctl inputs lists them)`) {
		t.Errorf("unknown name: %d %q", code, errOut)
	}
	b.must(with("preview", "stage left")...)
	if got := b.lastSent(t, "CPvI"); got != "00000002" {
		t.Errorf("new name: %s", got)
	}
	// One source per line: id, short name, long name, port type.
	out := b.must(with("inputs")...)
	for _, want := range []string{"1      CAM1  Camera 1            external\n", "2      STGL  Stage Left          external\n",
		"1000   Bars  Color Bars          bars\n", "10010  PGM   ME 1 Program        m/e output\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("inputs lacks %q:\n%s", want, out)
		}
	}
	var ins struct {
		Result []engine.InputEntry `json:"result"`
	}
	if json.Unmarshal([]byte(b.must(with("inputs", "--json")...)), &ins); len(ins.Result) != 8 ||
		ins.Result[1].Source != 2 || ins.Result[1].Short != "STGL" || ins.Result[1].Long != "Stage Left" || ins.Result[1].Type != "external" {
		t.Errorf("inputs --json: %+v", ins.Result)
	}
	// input list, its name before, still works.
	if old := b.must(with("input", "list")...); old != out {
		t.Errorf("input list:\n%s", old)
	}
	// Toggles read the state: keyer 0 and DSK 0 start on air.
	for _, want := range []string{"00000000", "00000100", "00000000"} {
		if _, got := b.mustSend(t, "CKOn", with("usk", "1", "toggle")...); got != want {
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
