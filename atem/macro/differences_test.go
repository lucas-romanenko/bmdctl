// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Where the codec differs from bmdwire v1.3.0 on purpose (PORTING.md,
// "Deliberate differences"). Each test fails on bmdwire's behaviour.

import (
	"encoding/hex"
	"testing"
)

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A Fairlight strip op without a sourceId, or with an empty one, encodes
// the stereo sourceId the switcher records, as in the live captures.
// bmdwire writes 0xFFFFFFFFFFFF0000 (-65536).
func TestMissingSourceIDIsStereo(t *testing.T) {
	for _, sid := range []string{"", stereo, "-65280"} {
		kv := []string{"input", "Camera1", "gain", "-0.05"}
		mix := []string{"input", "Camera1", "mixType", "Off"}
		if sid != "" {
			kv = append(kv, "sourceId", sid)
			mix = append(mix, "sourceId", sid)
		}
		assertBytes(t, encodeOK(t, wantOp("FairlightAudioMixerInputSourceFaderGain", kv...)),
			fixture(t, "op014b-14004b01010046430001ffffffffffff33f3ffff.bin"))
		assertBytes(t, encodeOK(t, wantOp("FairlightAudioMixerInputSourceMixType", mix...)),
			fixture(t, "op014a-14004a01010046430001ffffffffffff01000c00.bin"))
	}
	// An empty sourceId, and a strip op with the family-wide prefix.
	out := encodeOK(t, wantOp("FairlightAudioMixerInputSourceEqualiserGain",
		"input", "Camera1", "sourceId", "", "gain", "0.5"))
	assertBytes(t, out, packOp(0x014D, cat(u16b(1), u16b(0x4346), u64b(0xFFFFFFFFFFFF0100), i32b(32768))))
}

// The stereo sourceId decodes unsigned, the form ATEM Software Control
// writes, and that and the signed form both encode back to the same bytes.
func TestStereoSourceIDRoundTrips(t *testing.T) {
	raw := fixture(t, "op014a-14004a01010046430001ffffffffffff01000c00.bin")
	ops, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	assertParam(t, ops[0], "sourceId", "18446744073709486336")
	assertBytes(t, encodeOK(t, ops...), raw)
}

// A silenced fader's gain, -327.68 dB in Q16.16 (ec 51 b8 fe), decodes as
// ATEM Software Control writes it, "-327.68", in every Fairlight op with
// a gain, and encodes back to the same bytes. bmdwire decodes
// "-327.679993". The first op is the fader of input 2 in the macro
// downloaded on the fourth hardware run (docs/HARDWARE.md).
func TestSilencedGainIsASCs(t *testing.T) {
	strip := "01004643" + "0001ffffffffffff"
	cases := []struct{ name, raw string }{
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
	}
	for _, c := range cases {
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

// Only the silenced value, and only a gain: its neighbours, and the same
// value as a compressor threshold, keep bmdwire's six decimals.
func TestSilencedGainOnly(t *testing.T) {
	for raw, want := range map[string]string{
		"08006b01eb51b8fe": "-327.680008", // -21474837
		"08006b01ed51b8fe": "-327.679977", // -21474835
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
	assertParam(t, ops[0], "threshold", "-327.679993")
}
