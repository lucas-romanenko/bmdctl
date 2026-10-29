// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Macro floats decode as ATEM Software Control writes them: six
// significant digits. Ported from bmdwire#24's test_macro_float_format.py
// (c0e211e).
//
// profile/macro_floats_asc.xml holds, verbatim, every distinct macro op
// with a fractional value in twelve ASC Save Switcher State exports from
// one 1 M/E Constellation HD (2026-04-27 to 2026-10-13), leaving out
// HyperDeck ops (they carry addresses) and ops atemwire has no codec for.
// ASC writes those strings from the bytecode the switcher holds, so
// decoding the bytes each op encodes to must give ASC's strings back. Six
// decimals, what v1.3.0 wrote, missed 50 of the 399 distinct attribute
// strings in those files ("-327.679993" for "-327.68", "-0.080002" for
// "-0.0800018", "7.199997" for "7.2"); six significant digits misses none.

import (
	"bytes"
	"encoding/xml"
	"math"
	"regexp"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// fraction is _FRACTION: an attribute ASC wrote as a fractional number.
var fraction = regexp.MustCompile(`^-?\d+\.\d+(e-?\d+)?$`)

// ascOps is ASC_OPS: the fixture's ops, attributes in document order.
func ascOps(t *testing.T) []Op {
	t.Helper()
	var doc struct {
		Ops []struct {
			Attrs []xml.Attr `xml:",any,attr"`
		} `xml:"Op"`
	}
	if err := xml.Unmarshal(fixtures.Bytes(t, "profile/macro_floats_asc.xml"), &doc); err != nil {
		t.Fatal(err)
	}
	var ops []Op
	for _, o := range doc.Ops {
		op := Op{Params: []Param{}}
		for _, a := range o.Attrs {
			if a.Name.Local == "id" {
				op.ID = a.Value
			} else {
				op.Params = append(op.Params, Param{a.Name.Local, a.Value})
			}
		}
		ops = append(ops, op)
	}
	return ops
}

func TestEveryASCFloatDecodesAsASCWroteIt(t *testing.T) {
	ops := ascOps(t)
	if len(ops) != 98 {
		t.Fatalf("%d ops, want 98", len(ops))
	}
	n := 0
	for i, op := range ops {
		got, err := Decode(encodeOK(t, op))
		if err != nil || len(got) != 1 {
			t.Fatalf("op %d %s: %v %v", i, op.ID, got, err)
		}
		for _, p := range op.Params {
			if !fraction.MatchString(p.Value) {
				continue
			}
			n++
			if v, _ := got[0].Get(p.Name); v != p.Value {
				t.Errorf("op %d %s %s: %q, ASC wrote %q", i, op.ID, p.Name, v, p.Value)
			}
		}
	}
	t.Logf("%d fractional attributes", n)
}

// test_gain_fmt_macro: the macro recorded for bmdwire#17 in ASC 9.3.2 holds
// two of the five values it was meant to: Camera 1's fader at -inf and the
// master at -0.08. The silenced value's ec 51 b8 fe is also what the fourth
// hardware run downloaded for twelve silenced strips (docs/HARDWARE.md).
func TestGainFmtMacro(t *testing.T) {
	for _, c := range []struct {
		op  Op
		raw string
	}{
		{wantOp("FairlightAudioMixerInputSourceFaderGain", "input", "Camera1",
			"sourceId", "18446744073709486336", "gain", "-327.68"),
			"14004b01" + "0100" + "4643" + "0001ffffffffffff" + "ec51b8fe"},
		{wantOp("FairlightAudioMixerMasterOutFaderGain", "gain", "-0.0800018"), "08006b01" + "85ebffff"},
	} {
		raw := hexBytes(t, c.raw)
		ops, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		assertOps(t, ops, c.op)
		if b := encodeOK(t, c.op); !bytes.Equal(b, raw) {
			t.Errorf("%s: %x, want %x", c.op.ID, b, raw)
		}
	}
}

// test_silenced_master_fader, and the same value in every Fairlight op
// with a gain: -327.68, encoding back to the same bytes. The first op is
// the fader of input 2 in the macro the fourth hardware run downloaded.
func TestSilencedGain(t *testing.T) {
	strip := "01004643" + "0001ffffffffffff"
	for _, c := range []struct{ name, raw string }{
		{"FairlightAudioMixerInputSourceFaderGain", "14004b01020046430001ffffffffffffec51b8fe"},
		{"FairlightAudioMixerMasterOutFaderGain", "08006b01ec51b8fe"},
		{"FairlightAudioMixerInputSourceEqualiserGain", "14004d01" + strip + "ec51b8fe"},
		{"FairlightAudioMixerInputSourceEqualiserBandGain", "18005201" + "0100e103" + "0001ffffffffffff" + "00ac0c00ec51b8fe"},
		{"FairlightAudioMixerInputSourceInputGain", "14004701" + strip + "ec51b8fe"},
		{"FairlightAudioMixerInputSourceDynamicsGain", "14005601" + strip + "ec51b8fe"},
		{"FairlightAudioMixerHeadphoneOutGain", "08008401ec51b8fe"},
		{"FairlightAudioMixerHeadphoneOutMasterGain", "08008501ec51b8fe"},
		{"FairlightAudioMixerHeadphoneOutTalkbackGain", "08008601ec51b8fe"},
		{"FairlightAudioMixerHeadphoneOutSidetoneGain", "08008701ec51b8fe"},
	} {
		raw := hexBytes(t, c.raw)
		ops, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(ops) != 1 || ops[0].ID != c.name {
			t.Fatalf("%s: decoded %v", c.raw, ops)
		}
		assertParam(t, ops[0], "gain", "-327.68")
		assertBytes(t, encodeOK(t, ops...), raw)
	}
}

// One rule for every float: no value or attribute is special. The silenced
// gain's neighbours and the same value as a threshold read -327.68 too,
// where v1.3.0's six decimals gave -327.680008, -327.679977 and
// -327.679993 (and bmdctl special-cased the gain alone).
func TestOneRuleForEveryFloat(t *testing.T) {
	for raw, want := range map[string]string{
		"08006b01eb51b8fe": "-327.68", // -21474837
		"08006b01ed51b8fe": "-327.68", // -21474835
		"08006b0101000000": "1.52588e-05",
		"08006b0133333333": "13107.2",
	} {
		ops, err := Decode(hexBytes(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		assertParam(t, ops[0], "gain", want)
	}
	ops, err := Decode(hexBytes(t, "14006001010046430001ffffffffffffec51b8fe"))
	if err != nil {
		t.Fatal(err)
	}
	assertParam(t, ops[0], "threshold", "-327.68")
}

// test_zero_is_written_plainly.
func TestZeroIsWrittenPlainly(t *testing.T) {
	if z, nz := fmtScalar(0), fmtScalar(math.Copysign(0, -1)); z != "0" || nz != "0" {
		t.Errorf("%q %q", z, nz)
	}
}
