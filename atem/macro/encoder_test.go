// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// One for one, the assertions of bmdwire v1.3.0
// atemwire/tests/test_macro_encoder.py, plus the macro-codec halves of
// test_wipe_pattern_names.py and test_input_video_migration.py. Hex
// literals the Python tests compare with are the internal/fixtures/macro
// files.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

func encodeOK(t *testing.T, ops ...Op) []byte {
	t.Helper()
	b, err := Encode(ops)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func u16b(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func u32b(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func u64b(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }
func i32b(v int32) []byte  { return u32b(uint32(v)) }

func assertBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func fixture(t *testing.T, name string) []byte { return fixtures.Bytes(t, "macro/"+name) }

// --- Single-op encoders ---

func TestEncodeProgramInputCamera1(t *testing.T) {
	out := encodeOK(t, wantOp("ProgramInput", "mixEffectBlockIndex", "0", "input", "Camera1"))
	assertBytes(t, out, packOp(0x0002, cat([]byte{0, 0}, u16b(1))))
}

func TestEncodePreviewInputNumeric(t *testing.T) {
	out := encodeOK(t, wantOp("PreviewInput", "mixEffectBlockIndex", "0", "input", "3010"))
	assertBytes(t, out, packOp(0x0003, cat([]byte{0, 0}, u16b(3010))))
}

func TestEncodeCutNoArgs(t *testing.T) {
	assertBytes(t, encodeOK(t, wantOp("Cut", "mixEffectBlockIndex", "0")), packOp(0x0004, make([]byte, 4)))
}

// u32 frames, no leading padding.
func TestEncodeMacroSleep60Frames(t *testing.T) {
	assertBytes(t, encodeOK(t, wantOp("MacroSleep", "frames", "60")), packOp(0x0007, u32b(60)))
}

func TestEncodeTransitionStyleDip(t *testing.T) {
	out := encodeOK(t, wantOp("TransitionStyle", "mixEffectBlockIndex", "0", "style", "Dip"))
	assertBytes(t, out, packOp(0x0083, []byte{0, 1, 0, 0}))
}

// Background (bit 0) + Key2 (bit 2).
func TestEncodeTransitionSourceCombinedLayers(t *testing.T) {
	out := encodeOK(t, wantOp("TransitionSource", "mixEffectBlockIndex", "0", "source", "Background,Key2"))
	assertBytes(t, out, packOp(0x0084, cat([]byte{0, 0}, u16b(0x0005))))
}

func TestEncodeKeyOnAirTrue(t *testing.T) {
	out := encodeOK(t, wantOp("KeyOnAir", "mixEffectBlockIndex", "0", "keyIndex", "1", "onAir", "True"))
	assertBytes(t, out, packOp(0x0027, []byte{0, 1, 1, 0}))
}

// The marker in bytes 2-3 is 0x000C, as fresh recordings have it.
func TestEncodeLumaClipMarker(t *testing.T) {
	out := encodeOK(t, wantOp("LumaKeyClip", "mixEffectBlockIndex", "0", "keyIndex", "0", "clip", "0.5"))
	assertBytes(t, out, packOp(0x0029, cat([]byte{0, 0}, u16b(0x000C), u32b(32768))))
}

func TestEncodeDSKPreMultiplyZeroMarker(t *testing.T) {
	out := encodeOK(t, wantOp("DownstreamKeyPreMultiply", "keyIndex", "0", "preMultiply", "False"))
	assertBytes(t, out, packOp(0x00A4, []byte{0, 0, 0, 0}))
}

func TestEncodeAuxInputCamera1(t *testing.T) {
	out := encodeOK(t, wantOp("AuxiliaryInput", "auxiliaryIndex", "0", "input", "Camera1"))
	assertBytes(t, out, packOp(0x001F, cat([]byte{0, 0}, u16b(1))))
}

// --- Fairlight ---

// 0x014b: source, marker, sourceId, gain x 65536; matches the live
// capture.
func TestEncodeFairlightStripFaderGainCamera1(t *testing.T) {
	out := encodeOK(t, wantOp("FairlightAudioMixerInputSourceFaderGain",
		"input", "Camera1", "sourceId", "18446744073709486336", "gain", "-0.05"))
	assertBytes(t, out, packOp(0x014B, cat(u16b(1), u16b(0x4346), u64b(0xFFFFFFFFFFFF0100), i32b(-3277))))
	assertBytes(t, out, fixture(t, "op014b-14004b01010046430001ffffffffffff33f3ffff.bin"))
}

// 0x014a: source, marker, sourceId, state (1 Off), trailing marker.
func TestEncodeFairlightStripMixTypeOff(t *testing.T) {
	out := encodeOK(t, wantOp("FairlightAudioMixerInputSourceMixType",
		"input", "Camera1", "sourceId", "18446744073709486336", "mixType", "Off"))
	assertBytes(t, out, packOp(0x014A, cat(u16b(1), u16b(0x4346), u64b(0xFFFFFFFFFFFF0100), u16b(1), u16b(0x000C))))
	assertBytes(t, out, fixture(t, "op014a-14004a01010046430001ffffffffffff01000c00.bin"))
}

// 0x016b: i32 gain x 65536 only.
func TestEncodeFairlightMasterFaderGain(t *testing.T) {
	out := encodeOK(t, wantOp("FairlightAudioMixerMasterOutFaderGain", "gain", "-1.08"))
	assertBytes(t, out, packOp(0x016B, i32b(-70779)))
	assertBytes(t, out, fixture(t, "op016b-08006b0185ebfeff.bin"))
}

func decodeFixture(t *testing.T, name string) []Op {
	t.Helper()
	ops, err := Decode(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func assertOps(t *testing.T, got []Op, want ...Op) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDecodeFairlightStripFaderGain(t *testing.T) {
	assertOps(t, decodeFixture(t, "op014b-14004b01010046430001ffffffffffff33f3ffff.bin"),
		wantOp("FairlightAudioMixerInputSourceFaderGain",
			"input", "Camera1", "sourceId", "18446744073709486336", "gain", "-0.0500031")) // -3277/65536, six significant digits
}

func TestDecodeFairlightStripMixType(t *testing.T) {
	assertOps(t, decodeFixture(t, "op014a-14004a01010046430001ffffffffffff01000c00.bin"),
		wantOp("FairlightAudioMixerInputSourceMixType",
			"input", "Camera1", "sourceId", "18446744073709486336", "mixType", "Off"))
}

// --- Fade to black enable ---

func TestEncodeFTBEnabledTrue(t *testing.T) {
	out := encodeOK(t, wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "0", "enabled", "True"))
	assertBytes(t, out, packOp(0x0202, []byte{0, 1, 0, 0}))
}

func TestEncodeFTBEnabledFalse(t *testing.T) {
	out := encodeOK(t, wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "0", "enabled", "False"))
	assertBytes(t, out, packOp(0x0202, []byte{0, 0, 0, 0}))
}

// The uninitialised M/E byte ASC writes round-trips unchanged.
func TestEncodeFTBEnabledPreservesUninitializedMEBIByte(t *testing.T) {
	for _, g := range []byte{224, 14, 67, 139, 177, 170, 175, 129} {
		out := encodeOK(t, wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", strconv.Itoa(int(g)), "enabled", "True"))
		assertBytes(t, out, packOp(0x0202, []byte{g, 1, 0, 0}))
	}
}

func TestDecodeFTBEnabledTrue(t *testing.T) {
	assertOps(t, decodeFixture(t, "op0202-0800020203010000.bin"),
		wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "3", "enabled", "True"))
}

func TestDecodeFTBEnabledFalse(t *testing.T) {
	assertOps(t, decodeFixture(t, "op0202-0800020206000000.bin"),
		wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "6", "enabled", "False"))
}

func TestDecodeFTBEnabledKeepsGarbageMEBIByte(t *testing.T) {
	ops := decodeFixture(t, "op0202-08000202b7000000.bin")
	assertParam(t, ops[0], "mixEffectBlockIndex", "183")
	assertParam(t, ops[0], "enabled", "False")
}

// --- Video mode ---

func TestEncodeVideoMode1080p60(t *testing.T) {
	assertBytes(t, encodeOK(t, wantOp("VideoMode", "videoMode", "1080p60")), packOp(0x000C, []byte{27, 0, 0, 0}))
}

// Matches the live capture of a switcher running 720p60.
func TestEncodeVideoMode720p60(t *testing.T) {
	out := encodeOK(t, wantOp("VideoMode", "videoMode", "720p60"))
	assertBytes(t, out, packOp(0x000C, []byte{28, 0, 0, 0}))
	assertBytes(t, out, fixture(t, "op000c-08000c001c000000.bin"))
}

func TestEncodeVideoModeAcceptsNumeric(t *testing.T) {
	assertBytes(t, encodeOK(t, wantOp("VideoMode", "videoMode", "27")), packOp(0x000C, []byte{27, 0, 0, 0}))
}

// Mode 0 decodes to 525i5994, not NTSC.
func TestDecodeVideoModeCanonicalName(t *testing.T) {
	assertOps(t, decodeFixture(t, "op000c-08000c0000000000.bin"), wantOp("VideoMode", "videoMode", "525i5994"))
}

func TestDecodeVideoModeUnknownIntFallsBackToString(t *testing.T) {
	assertOps(t, decodeFixture(t, "op000c-08000c00ff000000.bin"), wantOp("VideoMode", "videoMode", "255"))
}

func TestDecodeFairlightMasterFaderGain(t *testing.T) {
	assertOps(t, decodeFixture(t, "op016b-08006b0185ebfeff.bin"),
		wantOp("FairlightAudioMixerMasterOutFaderGain", "gain", "-1.08")) // -70779/65536 = -1.0800018...
}

// Bytecode, decoded and encoded again, is byte-identical for every capture.
func TestFairlightByteIdenticalRoundtripForLiveCaptures(t *testing.T) {
	for _, name := range []string{
		"op014b-14004b01010046430001ffffffffffff33f3ffff.bin", // CAM1 vol -0.05
		"op014b-14004b01020046430001ffffffffffff33f3ffff.bin", // CAM2 vol -0.05
		"op014a-14004a01010046430001ffffffffffff01000c00.bin", // CAM1 mix Off
		"op014a-14004a01010046430001ffffffffffff02000c00.bin", // CAM1 mix On (synthetic)
		"op014a-14004a01010046430001ffffffffffff04000c00.bin", // CAM1 mix AFV (synthetic)
		"op016b-08006b0185ebfeff.bin",                         // master -1.08
		"op016b-08006b01f5e8ffff.bin",                         // master -0.09
		"op000c-08000c001c000000.bin",                         // video mode 720p60
		"op000c-08000c001b000000.bin",                         // video mode 1080p60 (synthetic)
		"op000c-08000c0000000000.bin",                         // video mode 525i5994 (synthetic)
		"op0202-0800020203010000.bin",                         // FTB enabled mEBI=3
		"op0202-0800020206000000.bin",                         // FTB disabled mEBI=6
		"op0202-08000202b7000000.bin",                         // FTB disabled mEBI=183 (garbage)
		"op0202-0800020207010000.bin",                         // FTB enabled mEBI=7
		"op0202-08000202b8010000.bin",                         // FTB enabled mEBI=184 (garbage)
	} {
		raw := fixture(t, name)
		ops := decodeFixture(t, name)
		if re := encodeOK(t, ops...); !bytes.Equal(re, raw) {
			t.Errorf("%s: re-encoded as %x; ops %v", name, re, ops)
		}
	}
}

// --- Round trips against the decoder (test_encode_decode_roundtrip) ---

const stereo = "18446744073709486336"

var roundTrips = []Op{
	wantOp("ProgramInput", "mixEffectBlockIndex", "0", "input", "Camera1"),
	wantOp("PreviewInput", "mixEffectBlockIndex", "0", "input", "Camera2"),
	wantOp("Cut", "mixEffectBlockIndex", "0"),
	wantOp("Auto", "mixEffectBlockIndex", "0"),
	wantOp("FadeToBlack", "mixEffectBlockIndex", "0"),
	wantOp("TransitionStyle", "mixEffectBlockIndex", "0", "style", "Wipe"),
	wantOp("TransitionSource", "mixEffectBlockIndex", "0", "source", "Background,Key1,Key3"),
	wantOp("MacroSleep", "frames", "15"),
	wantOp("AuxiliaryInput", "auxiliaryIndex", "0", "input", "Camera5"),
	wantOp("KeyOnAir", "mixEffectBlockIndex", "0", "keyIndex", "0", "onAir", "True"),
	wantOp("KeyType", "mixEffectBlockIndex", "0", "keyIndex", "0", "type", "Chroma"),
	wantOp("KeyMaskEnable", "mixEffectBlockIndex", "0", "keyIndex", "0", "enable", "True"),
	wantOp("LumaKeyClip", "mixEffectBlockIndex", "0", "keyIndex", "0", "clip", "0.5"),
	wantOp("LumaKeyGain", "mixEffectBlockIndex", "0", "keyIndex", "0", "gain", "0.25"),
	wantOp("LumaKeyInvert", "mixEffectBlockIndex", "0", "keyIndex", "0", "invert", "False"),
	wantOp("LumaKeyPreMultiply", "mixEffectBlockIndex", "0", "keyIndex", "0", "preMultiply", "True"),
	wantOp("DownstreamKeyOnAir", "keyIndex", "0", "onAir", "True"),
	wantOp("DownstreamKeyTie", "keyIndex", "0", "tie", "True"),
	wantOp("DownstreamKeyTie", "keyIndex", "1", "tie", "False"),
	wantOp("HyperDeckNetworkAddress", "slot", "1", "networkAddress", "10.20.30.40"),
	wantOp("HyperDeckNetworkAddress", "slot", "0", "networkAddress", "192.0.2.10"),
	wantOp("HyperDeckInput", "slot", "3", "input", "7"),
	wantOp("DownstreamKeyAuto", "keyIndex", "0"),
	wantOp("DownstreamKeyRate", "keyIndex", "0", "rate", "25"),
	wantOp("DownstreamKeyMaskEnable", "keyIndex", "0", "enable", "True"),
	wantOp("DownstreamKeyPreMultiply", "keyIndex", "0", "preMultiply", "True"),
	wantOp("FairlightAudioMixerInputSourceFaderGain", "input", "Camera1", "sourceId", stereo, "gain", "0.5"),
	wantOp("FairlightAudioMixerInputSourceMixType", "input", "Camera2", "sourceId", stereo, "mixType", "On"),
	wantOp("FairlightAudioMixerMasterOutFaderGain", "gain", "0"),
	wantOp("VideoMode", "videoMode", "1080p60"),
	wantOp("VideoMode", "videoMode", "720p60"),
	wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "0", "enabled", "True"),
	wantOp("FadeToBlackEnabled", "mixEffectBlockIndex", "177", "enabled", "False"),
	wantOp("TransitionMixRate", "mixEffectBlockIndex", "0", "rate", "55"),
	wantOp("TransitionDipRate", "mixEffectBlockIndex", "0", "rate", "50"),
	wantOp("TransitionDipInput", "mixEffectBlockIndex", "0", "input", "Color1"),
	wantOp("TransitionWipeRate", "mixEffectBlockIndex", "0", "rate", "50"),
	wantOp("TransitionWipePattern", "mixEffectBlockIndex", "0", "pattern", "HorizontalBarnDoor"),
	wantOp("TransitionWipeBorderSoftness", "mixEffectBlockIndex", "0", "softness", "0.5"),
	wantOp("TransitionWipeBorderWidth", "mixEffectBlockIndex", "0", "width", "0.25"),
	wantOp("TransitionWipeBorderFillInput", "mixEffectBlockIndex", "0", "input", "Color2"),
	wantOp("TransitionWipeXPosition", "mixEffectBlockIndex", "0", "xPosition", "0.5"),
	wantOp("TransitionWipeYPosition", "mixEffectBlockIndex", "0", "yPosition", "0.25"),
	wantOp("TransitionWipeAndDVEFlipFlop", "mixEffectBlockIndex", "0", "flipFlop", "True"),
	wantOp("TransitionWipeAndDVEReverse", "mixEffectBlockIndex", "0", "reverse", "False"),
	wantOp("TransitionDVERate", "mixEffectBlockIndex", "0", "rate", "50"),
	wantOp("TransitionDVEPattern", "mixEffectBlockIndex", "0", "pattern", "GraphicLogoWipe"),
	wantOp("TransitionDVEFillInput", "mixEffectBlockIndex", "0", "input", "Color1"),
	wantOp("TransitionDVECutInput", "mixEffectBlockIndex", "0", "input", "MediaPlayer1"),
	wantOp("TransitionDVECutInputEnable", "mixEffectBlockIndex", "0", "enable", "True"),
	wantOp("TransitionStingerSourceMediaPlayer", "mixEffectBlockIndex", "0", "mediaPlayer", "1"),
	wantOp("TransitionStingerPreRoll", "mixEffectBlockIndex", "0", "preRoll", "3"),
	wantOp("TransitionStingerClipDuration", "mixEffectBlockIndex", "0", "clipDuration", "74"),
	wantOp("TransitionStingerTriggerPoint", "mixEffectBlockIndex", "0", "triggerPoint", "26"),
	wantOp("TransitionStingerMixRate", "mixEffectBlockIndex", "0", "mixRate", "6"),
	wantOp("TransitionStingerDVEClip", "mixEffectBlockIndex", "0", "clip", "0.5"),
	wantOp("TransitionStingerDVEGain", "mixEffectBlockIndex", "0", "gain", "0.25"),
	wantOp("TransitionStingerDVEInvert", "mixEffectBlockIndex", "0", "invert", "True"),
	wantOp("TransitionStingerDVEPreMultiply", "mixEffectBlockIndex", "0", "preMultiply", "False"),
	wantOp("ColorGeneratorHue", "colorGeneratorIndex", "0", "hue", "180"),
	wantOp("ColorGeneratorSaturation", "colorGeneratorIndex", "0", "saturation", "0.5"),
	wantOp("ColorGeneratorLuminescence", "colorGeneratorIndex", "0", "luminescence", "0.25"),
	wantOp("FadeToBlackRate", "mixEffectBlockIndex", "0", "rate", "50"),
	wantOp("KeyMaskTop", "mixEffectBlockIndex", "0", "keyIndex", "0", "top", "8"),
	wantOp("KeyMaskBottom", "mixEffectBlockIndex", "0", "keyIndex", "0", "bottom", "-8"),
	wantOp("KeyMaskLeft", "mixEffectBlockIndex", "0", "keyIndex", "0", "left", "-15"),
	wantOp("KeyMaskRight", "mixEffectBlockIndex", "0", "keyIndex", "0", "right", "15"),
	wantOp("DVEAndFlyKeyRate", "mixEffectBlockIndex", "0", "keyIndex", "0", "rate", "50"),
	wantOp("DVEKeyBorderEnable", "mixEffectBlockIndex", "0", "keyIndex", "0", "enable", "True"),
	wantOp("DVEKeyBorderHue", "mixEffectBlockIndex", "0", "keyIndex", "0", "hue", "300"),
	wantOp("DVEKeyBorderSaturation", "mixEffectBlockIndex", "0", "keyIndex", "0", "saturation", "0.5"),
	wantOp("DVEKeyBorderLuminescence", "mixEffectBlockIndex", "0", "keyIndex", "0", "luminescence", "0.5"),
	wantOp("DVEKeyBorderInnerWidth", "mixEffectBlockIndex", "0", "keyIndex", "0", "innerWidth", "1.5"),
	wantOp("DVEKeyBorderOuterWidth", "mixEffectBlockIndex", "0", "keyIndex", "0", "outerWidth", "1.5"),
	wantOp("DVEKeyBorderInnerSoftness", "mixEffectBlockIndex", "0", "keyIndex", "0", "innerSoftness", "20"),
	wantOp("DVEKeyBorderOuterSoftness", "mixEffectBlockIndex", "0", "keyIndex", "0", "outerSoftness", "13"),
	wantOp("DVEKeyBorderOpacity", "mixEffectBlockIndex", "0", "keyIndex", "0", "opacity", "82"),
	wantOp("DVEKeyShadowEnable", "mixEffectBlockIndex", "0", "keyIndex", "0", "enable", "True"),
	wantOp("DVEKeyShadowDirection", "mixEffectBlockIndex", "0", "keyIndex", "0", "direction", "44"),
	wantOp("DVEKeyShadowAltitude", "mixEffectBlockIndex", "0", "keyIndex", "0", "altitude", "17"),
	wantOp("AdvancedChromaKeyForegroundLevel", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundLevel", "0.5"),
	wantOp("AdvancedChromaKeyBackgroundLevel", "mixEffectBlockIndex", "0", "keyIndex", "0", "backgroundLevel", "0.5"),
	wantOp("AdvancedChromaKeyKeyEdge", "mixEffectBlockIndex", "0", "keyIndex", "0", "keyEdge", "0.25"),
	wantOp("AdvancedChromaKeySpillSuppress", "mixEffectBlockIndex", "0", "keyIndex", "0", "spillSuppress", "0.5"),
	wantOp("AdvancedChromaKeyFlareSuppress", "mixEffectBlockIndex", "0", "keyIndex", "0", "flareSuppress", "0.5"),
	wantOp("AdvancedChromaKeyForegroundBrightness", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundBrightness", "-0.25"),
	wantOp("AdvancedChromaKeyForegroundContrast", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundContrast", "0.25"),
	wantOp("AdvancedChromaKeyForegroundColour", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundColour", "0.5"),
	wantOp("AdvancedChromaKeyForegroundRed", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundRed", "0.25"),
	wantOp("AdvancedChromaKeyForegroundGreen", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundGreen", "-0.25"),
	wantOp("AdvancedChromaKeyForegroundBlue", "mixEffectBlockIndex", "0", "keyIndex", "0", "foregroundBlue", "0.25"),
	wantOp("AdvancedChromaKeySamplingModeEnabled", "mixEffectBlockIndex", "0", "keyIndex", "0", "enabled", "True"),
	wantOp("AdvancedChromaKeyPreviewEnabled", "mixEffectBlockIndex", "0", "keyIndex", "0", "enabled", "False"),
	wantOp("AdvancedChromaKeyCursorXPosition", "mixEffectBlockIndex", "0", "keyIndex", "0", "xPosition", "10"),
	wantOp("AdvancedChromaKeyCursorYPosition", "mixEffectBlockIndex", "0", "keyIndex", "0", "yPosition", "0.5"),
	wantOp("AdvancedChromaKeyCursorSize", "mixEffectBlockIndex", "0", "keyIndex", "0", "size", "0.5"),
	wantOp("FlyKeySetKeyFrame", "mixEffectBlockIndex", "0", "keyIndex", "0", "keyFrameIndex", "0"),
	wantOp("FlyKeyRunToKeyFrame", "mixEffectBlockIndex", "0", "keyIndex", "0", "keyFrameIndex", "1"),
	wantOp("FlyKeyRunToFull", "mixEffectBlockIndex", "0", "keyIndex", "0"),
	wantOp("FlyKeyRunToInfinity", "mixEffectBlockIndex", "0", "keyIndex", "0", "location", "TopLeft"),
	wantOp("FairlightAudioMixerInputSourceEqualiserGain", "input", "Camera1", "sourceId", stereo, "gain", "0.5"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandEnabled", "input", "Camera1", "sourceId", stereo, "band", "0", "enabled", "True"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandShape", "input", "Camera1", "sourceId", stereo, "band", "2", "shape", "BandPass"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandRange", "input", "Camera1", "sourceId", stereo, "band", "1", "range", "MidLow"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandFrequency", "input", "Camera1", "sourceId", stereo, "band", "2", "frequency", "168"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandGain", "input", "Camera1", "sourceId", stereo, "band", "0", "gain", "2.5"),
	wantOp("FairlightAudioMixerInputSourceEqualiserBandQFactor", "input", "Camera1", "sourceId", stereo, "band", "2", "qFactor", "2.5"),
	wantOp("FairlightAudioMixerInputSourceCompressorEnabled", "input", "Camera1", "sourceId", stereo, "enabled", "True"),
	wantOp("FairlightAudioMixerInputSourceCompressorThreshold", "input", "Camera1", "sourceId", stereo, "threshold", "-37.5"),
	wantOp("FairlightAudioMixerInputSourceCompressorRatio", "input", "Camera1", "sourceId", stereo, "ratio", "2.5"),
	wantOp("FairlightAudioMixerInputSourceCompressorAttack", "input", "Camera1", "sourceId", stereo, "attack", "5.5"),
	wantOp("FairlightAudioMixerInputSourceCompressorHold", "input", "Camera1", "sourceId", stereo, "hold", "4.5"),
	wantOp("FairlightAudioMixerInputSourceCompressorRelease", "input", "Camera1", "sourceId", stereo, "release", "84.25"),
	wantOp("FairlightAudioMixerInputSourceLimiterThreshold", "input", "Camera1", "sourceId", stereo, "threshold", "-11.25"),
	wantOp("FairlightAudioMixerInputSourceLimiterAttack", "input", "Camera1", "sourceId", stereo, "attack", "1.5"),
	wantOp("FairlightAudioMixerInputSourceLimiterHold", "input", "Camera1", "sourceId", stereo, "hold", "2.5"),
	wantOp("FairlightAudioMixerInputSourceLimiterRelease", "input", "Camera1", "sourceId", stereo, "release", "85.875"),
	wantOp("FairlightAudioMixerInputSourceExpanderThreshold", "input", "Camera1", "sourceId", stereo, "threshold", "-44.25"),
	wantOp("FairlightAudioMixerInputSourceExpanderRange", "input", "Camera1", "sourceId", stereo, "range", "20.25"),
	wantOp("FairlightAudioMixerInputSourceExpanderRatio", "input", "Camera1", "sourceId", stereo, "ratio", "1.25"),
	wantOp("FairlightAudioMixerInputSourceExpanderAttack", "input", "Camera1", "sourceId", stereo, "attack", "3.5"),
	wantOp("FairlightAudioMixerInputSourceExpanderHold", "input", "Camera1", "sourceId", stereo, "hold", "9.25"),
	wantOp("FairlightAudioMixerInputSourceExpanderRelease", "input", "Camera1", "sourceId", stereo, "release", "97.75"),
	wantOp("FairlightAudioMixerInputSourceInputGain", "input", "Camera1", "sourceId", stereo, "gain", "1.5"),
	wantOp("FairlightAudioMixerInputSourceDynamicsGain", "input", "Camera1", "sourceId", stereo, "gain", "9.5"),
	wantOp("FairlightAudioMixerHeadphoneOutGain", "gain", "-13.5"),
	wantOp("FairlightAudioMixerHeadphoneOutMasterGain", "gain", "-27.5"),
	wantOp("FairlightAudioMixerHeadphoneOutSidetoneGain", "gain", "-18.5"),
	wantOp("FairlightAudioMixerHeadphoneOutTalkbackGain", "gain", "-25.5"),
}

// Encoding and decoding again gives the same attributes, in the same
// order.
func TestEncodeDecodeRoundtrip(t *testing.T) {
	for _, op := range roundTrips {
		ops, err := Decode(encodeOK(t, op))
		if err != nil {
			t.Fatal(err)
		}
		if len(ops) != 1 || !reflect.DeepEqual(ops[0], op) {
			t.Errorf("op=%s: %v != %v", op.ID, ops, op)
		}
	}
}

// --- Unknown ops via rawParams ---

func TestEncodeUnknownOpViaRawParams(t *testing.T) {
	raw := fixture(t, "opabcd-0800cdabdeadbeef.bin")
	assertBytes(t, raw, packOp(0xABCD, []byte{0xde, 0xad, 0xbe, 0xef}))
	ops := decodeFixture(t, "opabcd-0800cdabdeadbeef.bin")
	assertOps(t, ops, wantOp("Unknown_0xabcd", "rawParams", "deadbeef"))
	assertBytes(t, encodeOK(t, ops...), raw)
}

// An id with no encoder fails with ValueError, so profile apply can drop
// the op.
func TestEncodeUnsupportedOpIDRaises(t *testing.T) {
	_, err := Encode([]Op{{ID: "NotAThing"}})
	var e *Error
	if !errors.As(err, &e) || e.Class != "ValueError" || !strings.Contains(e.Message, "no encoder") {
		t.Errorf("got %v", err)
	}
}

// --- Whole-list encoding ---

func TestEncodeTwoOpsConcatenates(t *testing.T) {
	out := encodeOK(t, wantOp("Cut", "mixEffectBlockIndex", "0"), wantOp("Auto", "mixEffectBlockIndex", "0"))
	assertBytes(t, out, cat(packOp(0x0004, make([]byte, 4)), packOp(0x0005, make([]byte, 4))))
}

func TestEncodeEmptyListYieldsEmptyBytes(t *testing.T) {
	if b := encodeOK(t); len(b) != 0 {
		t.Errorf("%x", b)
	}
}

// --- test_wipe_pattern_names.py, the macro table ---

// Verbatim from ASC Save Switcher State XMLs (captured 2026-08-31).
var ascCapturedWipes = map[int]string{
	0: "LeftToRightBar", 1: "TopToBottomBar", 2: "HorizontalBarnDoor", 3: "VerticalBarnDoor", 6: "DiamondIris",
}

func TestMacroWipeTableMatchesASCCapture(t *testing.T) {
	for wire, name := range ascCapturedWipes {
		if wipePatternNames[wire] != name {
			t.Errorf("macro wipe name at wire %d is %q, ASC XML says %q", wire, wipePatternNames[wire], name)
		}
	}
}

// The macro half of test_the_two_tables_agree_across_all_18: 0-17, no
// more. The profile table it is compared with there is not ported yet.
func TestMacroWipeTableHas18(t *testing.T) {
	if len(wipePatternNames) != 18 {
		t.Errorf("%d names", len(wipePatternNames))
	}
	for i := 0; i < 18; i++ {
		if _, ok := wipePatternNames[i]; !ok {
			t.Errorf("no name for %d", i)
		}
	}
}

func TestMacroWipeNameIntRoundtripForCaptured(t *testing.T) {
	for wire, name := range ascCapturedWipes {
		if wipePatternToInt[name] != wire {
			t.Errorf("%s -> %d, want %d", name, wipePatternToInt[name], wire)
		}
	}
}

func TestMacroWipeNoPhantomNames(t *testing.T) {
	for _, bogus := range []string{"HorizontalBars", "VerticalBars", "HorizontalBar", "VerticalBar", "LeftSlash"} {
		if _, ok := wipePatternToInt[bogus]; ok {
			t.Errorf("%s is in the table", bogus)
		}
	}
}

// test_profile_table_matches_asc_capture has no counterpart: it checks the
// profile's WIPE_PATTERN_NAMES, which arrives with the profile port.

// --- test_input_video_migration.py ---

// The VideoMode encoder reads VIDEO_MODE_NAMES
// (test_macrotransfer_reroutes_to_input_video_module).
func TestMacroVideoModeEncoderUsesVideoModeNames(t *testing.T) {
	var b []byte
	if exc := catch(func() { b = eVideoMode(attrs{"videoMode": "1080p60"}) }); exc != nil {
		t.Fatal(exc.msg)
	}
	assertBytes(t, b, []byte{27, 0, 0, 0})
}

// test_apply_macros.py exercises the encoder only through Profile.apply's
// per-op drop policy; it is ported with profiles.
