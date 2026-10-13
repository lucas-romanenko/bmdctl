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
