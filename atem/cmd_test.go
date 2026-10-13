// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The assertions bmdwire v1.3.0's tests make about the bytes commands
// send, ported. Each test, or each group of table rows, names its Python
// counterpart; a Python operation call becomes the Go command it sends,
// in the operation's units. TestCommandsMatchAtemwire
// (cmd_oracle_test.go) covers every command more broadly.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"slices"
	"testing"
	"time"
)

// emit is what an atemwire operation hands its connection: the command's
// bytes, header included. The command must validate, because the
// operation sent it; the header must carry the length and the opcode, as
// the payload() helper of test_restored_upstream_commands.py checks.
func emit(t *testing.T, c Command) []byte {
	t.Helper()
	if v, ok := c.(Validator); ok {
		if err := v.Validate(); err != nil {
			t.Fatalf("%T: %v", c, err)
		}
	}
	b := c.Encode()
	if len(b) < 8 || string(b[4:8]) != c.Opcode() || int(binary.BigEndian.Uint16(b)) != len(b) {
		t.Fatalf("%T: header %x", c, b)
	}
	return b
}

// refuses reports whether atemwire would raise on, or decline to send, c.
func refuses(c Command) bool {
	v, ok := c.(Validator)
	return ok && v.Validate() != nil
}

func u16At(b []byte, at int) uint16 { return binary.BigEndian.Uint16(b[at:]) }
func i16At(b []byte, at int) int16  { return int16(binary.BigEndian.Uint16(b[at:])) }
func i32At(b []byte, at int) int32  { return int32(binary.BigEndian.Uint32(b[at:])) }

// fpsAt is display_fps for a mixerstate whose video mode runs at rate; 0
// is a mixerstate with no video mode yet.
func fpsAt(rate float64) int {
	var s State
	if rate > 0 {
		s.VideoMode.Mode = &VideoModeField{Rate: rate}
	}
	return DisplayFPS(s)
}

// packBE is Python's struct.pack('>...') for a struct mirroring the
// format string; blank fields are the pad bytes.
func packBE(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := binary.Write(&b, binary.BigEndian, v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// sends is one operation call whose Python test pins only the command code
// it sends, or with code "" only that it sends something.
type sends struct {
	cmd  Command
	code string
}

func checkSends(t *testing.T, rows []sends) {
	t.Helper()
	for i, r := range rows {
		if b := emit(t, r.cmd); r.code != "" && string(b[4:8]) != r.code {
			t.Errorf("row %d: %T sends %s, want %s", i, r.cmd, b[4:8], r.code)
		}
	}
}

// test_restored_upstream_commands.py::test_payload_matches_upstream (CASES).
func TestRestoredUpstreamPayloads(t *testing.T) {
	for i, c := range []struct {
		cmd  Command
		want string
	}{
		{AutoInputVideoMode{Enable: true}, "01000000"},
		{AudioInput{Source: 1001, Balance: Set[int16](-2500), Volume: Set[uint16](40000), On: Set(true)}, "070003e901009c40f63c0000"},
		{AudioInput{Source: 2, AFV: Set(true)}, "010000020200000000000000"},
		{AudioInput{Source: 3, Volume: Set[uint16](12345)}, "020000030000303900000000"},
		{AudioMasterProperties{Volume: Set[uint16](65381), AFV: Set(true)}, "0500ff6500000100"},
		{AudioMasterProperties{AFV: Set(false)}, "0400000000000000"},
		{AudioMonitorProperties{Enabled: Set(true), Volume: Set[uint16](30000), Mute: Set(false), Solo: Set(true),
			SoloSource: Set[uint16](1002), Dim: Set(true), DimVolume: Set[uint16](5000)}, "7f017530000103ea01001388"},
		{AudioMonitorProperties{Volume: Set[uint16](100)}, "020000640000000000000000"},
		{SendAudioLevels{Enable: true}, "01000000"},
		{MultiviewInput{Multiviewer: 1, Window: 7, Source: 3010}, "01070bc2"},
		{MultiviewProperties{Multiviewer: 0, Layout: Set[uint8](3), Swap: Set(true)}, "03000301"},
		{MultiviewProperties{Multiviewer: 1, Swap: Set(false)}, "02010000"},
		{SupersourceBoxProperties{SuperSource: 0, Box: 2, Enabled: Set(true), Source: Set[uint16](5), X: Set[int16](-1200),
			Y: Set[int16](3400), Size: Set[uint16](500), Masked: Set(true), Top: Set[uint16](100), Bottom: Set[uint16](200),
			Left: Set[uint16](300), Right: Set[uint16](400)}, "03ff000201000005fb500d4801f40100006400c8012c0190"},
		{SupersourceBoxProperties{SuperSource: 1, Box: 0, X: Set[int16](4800)}, "000401000000000012c00000000000000000000000000000"},
		{SupersourceProperties{SuperSource: 0, FillSource: Set[uint16](3020), KeySource: Set[uint16](3021), Layer: Set[uint8](1),
			Premultiplied: Set(true), Clip: Set[uint16](500), Gain: Set[uint16](250), Invert: Set(true)}, "7f000bcc0bcd010101f400fa01000000"},
		{SupersourceProperties{SuperSource: 0, Clip: Set[uint16](10)}, "1000000000000000000a000000000000"},
		{TransitionPreview{ME: 1, Enabled: true}, "01010000"},
		{TransitionPosition{ME: 0, Position: 10000}, "00002710"},
		{RecorderStatus{Recording: true}, "01000000"},
		{ClearStartupState{}, "00000000"},
		{SaveStartupState{}, "00000000"},
		{StreamingAudioBitrate{Min: 128000, Max: 128000}, "0001f4000001f400"},
		{StreamingStatusSet{Streaming: false}, "00000000"},
		{SetTimeOfDay{Time: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}, "6aa3ed4000000000"},
		{SetTimeOfDay{Time: time.Unix(1789128000, 0)}, "6aa3ed4000000000"},
	} {
		if got := hex.EncodeToString(emit(t, c.cmd)[8:]); got != c.want {
			t.Errorf("case %d %s: %s, want %s", i, c.cmd.Opcode(), got, c.want)
		}
	}
}

// test_restored_upstream_commands.py::test_crms_matches_upstream_layout,
// against upstream's struct.pack('>B 128s xxx II ?xxx', ...).
func TestCRMSLayout(t *testing.T) {
	type crms struct {
		Mask           uint8
		Filename       [128]byte
		_              [3]byte
		Disk1, Disk2   uint32
		RecordInCamera bool
		_              [3]byte
	}
	pack := func(mask uint8, filename string, disk1, disk2 uint32, inCamera bool) []byte {
		v := crms{Mask: mask, Disk1: disk1, Disk2: disk2, RecordInCamera: inCamera}
		copy(v.Filename[:], filename)
		return packBE(t, v)
	}
	got := emit(t, RecordingSettingsSet{Filename: Set("Show 2026-09-11"), Disk1: Set[uint32](0x12345678),
		Disk2: Set[uint32](0x9ABCDEF0), RecordInCamera: Set(true)})[8:]
	if want := pack(0x0f, "Show 2026-09-11", 0x12345678, 0x9ABCDEF0, true); !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
	got = emit(t, RecordingSettingsSet{Filename: Set("x")})[8:]
	if want := pack(0x01, "x", 0, 0, false); !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
}

// test_restored_upstream_commands.py::test_crss_matches_upstream_layout,
// against upstream's struct.pack('>B 64s 512s 512s 3x II', ...).
func TestCRSSLayout(t *testing.T) {
	type crss struct {
		Mask                   uint8
		Name                   [64]byte
		URL, Key               [512]byte
		_                      [3]byte
		BitrateMin, BitrateMax uint32
	}
	pack := func(mask uint8, name, url, key string, lo, hi uint32) []byte {
		v := crss{Mask: mask, BitrateMin: lo, BitrateMax: hi}
		copy(v.Name[:], name)
		copy(v.URL[:], url)
		copy(v.Key[:], key)
		return packBE(t, v)
	}
	got := emit(t, StreamingServiceSet{Name: Set("YouTube"), URL: Set("rtmp://a.rtmp.youtube.com/live2"), Key: Set("abcd-1234"),
		BitrateMin: Set[uint32](6000000), BitrateMax: Set[uint32](9000000)})[8:]
	if want := pack(0x0f, "YouTube", "rtmp://a.rtmp.youtube.com/live2", "abcd-1234", 6000000, 9000000); !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
	got = emit(t, StreamingServiceSet{Key: Set("k")})[8:]
	if want := pack(0x04, "", "", "k", 0, 0); !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
}

// test_restored_upstream_commands.py::test_crss_requires_both_bitrates.
func TestCRSSRequiresBothBitrates(t *testing.T) {
	if !refuses(StreamingServiceSet{BitrateMin: Set[uint32](1)}) {
		t.Error("bitrate_min alone accepted")
	}
}

// test_restored_upstream_commands.py::test_ccmd_matches_upstream (CCMD):
// the payload, and the caller's data is not mutated.
func TestCCmdPayloads(t *testing.T) {
	for _, c := range []struct {
		datatype uint8
		relative bool
		data     []any
		want     string
	}{
		{CameraBool, false, []any{true}, "020801000000000100000000000000000100000000000000"},
		{CameraInt8, false, []any{-3, 7}, "02080100010000020000000000000000fd07000000000000"},
		{CameraInt16, false, []any{-1000, 1000}, "02080100020000020000000000000000fc1803e800000000"},
		{CameraInt32, false, []any{100000}, "02080100030000000001000000000000000186a000000000"},
		{CameraInt64, false, []any{1}, "020801000400000100000000000000000000000000000001"},
		{CameraString, false, []any{"ab"}, "020801000500000100000000000000006162000000000000"},
		{CameraFixed16, false, []any{0.5, -1.0}, "020801008000000000020000000000000400f80000000000"},
		{0, false, nil, "02080100000000000000000000000000"}, // datatype=None, data=None
		{CameraFixed16, true, []any{0.25}, "020801018000000000010000000000000200000000000000"},
	} {
		snapshot := slices.Clone(c.data)
		cmd := CameraControl{Destination: 2, Category: 8, Parameter: 1, Relative: c.relative, Datatype: c.datatype, Data: c.data}
		if got := hex.EncodeToString(emit(t, cmd)[8:]); got != c.want {
			t.Errorf("datatype %d %v: %s, want %s", c.datatype, c.data, got, c.want)
		}
		if !slices.Equal(c.data, snapshot) {
			t.Errorf("datatype %d: data mutated to %v", c.datatype, c.data)
		}
	}
}

// test_restored_upstream_commands.py::test_wrappers_emit_the_matching_command.
func TestRestoredUpstreamWrappers(t *testing.T) {
	cmds := []Command{
		TransitionPreview{ME: 1, Enabled: true},
		TransitionPosition{Position: 20000}, // clamped to 10000
		MultiviewInput{Multiviewer: 1, Window: 7, Source: 3010},
		MultiviewProperties{Layout: Set[uint8](3)},
		MultiviewProperties{Multiviewer: 1, Swap: Set(true)},
		RecorderStatus{Recording: true}, RecorderStatus{Recording: false},
		RecordingSettingsSet{Filename: Set("x")},
		StreamingStatusSet{Streaming: true}, StreamingStatusSet{Streaming: false},
		StreamingAudioBitrate{Min: 128000, Max: 128000},
		StreamingServiceSet{Key: Set("k")},
		SupersourceBoxProperties{Box: 2, X: Set[int16](100)},
		SupersourceProperties{Clip: Set[uint16](10)},
		AudioInput{Source: 1, Volume: Set[uint16](5)},
		AudioMasterProperties{AFV: Set(true)},
		AudioMonitorProperties{Mute: Set(true)},
		SendAudioLevels{Enable: true},
		CameraControl{Destination: 255, Datatype: CameraInt8, Data: []any{1}},
		AutoInputVideoMode{Enable: true},
		SaveStartupState{}, ClearStartupState{},
		SetTimeOfDay{Time: time.Unix(1789128000, 0)},
	}
	var codes []string
	for _, c := range cmds {
		codes = append(codes, string(emit(t, c)[4:8]))
	}
	want := []string{"CTPr", "CTPs", "CMvI", "CMvP", "CMvP", "RcTM", "RcTM", "CRMS", "StrR", "StrR",
		"STAB", "CRSS", "CSBP", "CSSc", "CAMI", "CAMM", "CAMm", "SALN", "CCmd", "AiVM",
		"SRsv", "SRcl", "SToD"}
	if !slices.Equal(codes, want) {
		t.Errorf("codes %v\nwant  %v", codes, want)
	}
	if got := hex.EncodeToString(emit(t, cmds[1])[8:]); got != "00002710" {
		t.Errorf("CTPs %s", got)
	}
	if got := hex.EncodeToString(emit(t, cmds[3])[8:]); got != "01000300" {
		t.Errorf("CMvP %s", got)
	}
}

func TestSwitchingOps(t *testing.T) {
	// test_switching_migration.py::test_set_program_emits_program_input_command
	if p := emit(t, ProgramInput{Source: 5})[8:]; p[0] != 0 || u16At(p, 2) != 5 {
		t.Errorf("CPgI %x", p)
	}
	// test_switching_migration.py::test_set_preview_emits_preview_input_command
	if p := emit(t, PreviewInput{ME: 1, Source: 9})[8:]; p[0] != 1 || u16At(p, 2) != 9 {
		t.Errorf("CPvI %x", p)
	}
	// test_switching_migration.py::test_cut_and_auto_emit_correct_packets
	checkSends(t, []sends{{Cut{}, "DCut"}, {Auto{}, "DAut"}})
	// test_switching_migration.py::test_set_aux_output_includes_mask_byte
	if p := emit(t, AuxSource{Aux: 2, Source: 42})[8:]; p[0] != 1 || p[1] != 2 || u16At(p, 2) != 42 {
		t.Errorf("CAuS %x", p)
	}
}

func TestTransitionOps(t *testing.T) {
	frames := func(s string, rate float64) int { return ParseRate(s, fpsAt(rate)) }
	checkSends(t, []sends{
		// test_transition_migration.py::test_set_mix_rate_uses_display_fps
		{MixSettings{Rate: uint8(frames("1:00", 60))}, "CTMx"},
		// test_transition_migration.py::test_set_dip_rate_and_source_use_correct_send_class
		{DipSettings{Rate: Set(uint8(frames("0:25", 50)))}, "CTDp"},
		{DipSettings{Source: Set[uint16](15)}, "CTDp"},
		// test_transition_migration.py::test_set_next_transition_layers_packs_absolute_mask
		{TransitionSettings{NextTransition: Set(LayerBackground | LayerKey1 | LayerKey3)}, "CTTp"},
		// test_transition_migration.py::test_set_wipe_rate_uses_display_fps
		{WipeSettings{Rate: Set(uint8(frames("1:00", 25)))}, "CTWp"},
		// test_transition_migration.py::test_set_wipe_pattern_and_source_identity
		{WipeSettings{Pattern: Set[uint8](3)}, ""},
		{WipeSettings{Source: Set[uint16](42)}, ""},
		// test_transition_migration.py::test_set_wipe_position_x_scales_and_clamps
		{WipeSettings{PositionX: Set(0.42)}, "CTWp"},
		// test_transition_migration.py::test_set_wipe_position_x_clamps_above_one
		{WipeSettings{PositionX: Set(1.5)}, "CTWp"},
		// test_transition_migration.py::test_set_wipe_softness_percent_to_wire
		{WipeSettings{Softness: Set(75.0)}, "CTWp"},
		// test_transition_migration.py::test_set_wipe_symmetry_and_width
		{WipeSettings{Symmetry: Set(50.0)}, ""},
		{WipeSettings{Width: Set(33.3)}, ""},
		// test_transition_migration.py::test_set_wipe_reverse_flipflop_bool_identity
		{WipeSettings{Reverse: Set(true)}, ""},
		{WipeSettings{FlipFlop: Set(false)}, ""},
		// test_transition_migration.py::test_set_dve_rate_uses_display_fps
		{DVESettings{Rate: Set(uint8(frames("1:00", 50)))}, "CTDv"},
		// test_transition_migration.py::test_set_dve_fill_and_key_sources
		{DVESettings{FillSource: Set[uint16](5)}, ""},
		{DVESettings{KeySource: Set[uint16](7)}, ""},
		{DVESettings{KeyEnable: Set(true)}, ""},
		// test_transition_migration.py::test_set_dve_clip_uses_tenths_scale
		{DVESettings{KeyClip: Set(60.0)}, "CTDv"},
		// test_transition_migration.py::test_set_dve_clip_clamps_above_100
		{DVESettings{KeyClip: Set(150.0)}, "CTDv"},
		// test_transition_migration.py::test_set_dve_gain_clamps_below_zero
		{DVESettings{KeyGain: Set(-10.0)}, "CTDv"},
		// test_transition_migration.py::test_set_dve_pre_multiplied_invert_bool
		{DVESettings{KeyPremultiplied: Set(true)}, ""},
		{DVESettings{KeyInvert: Set(true)}, ""},
		// test_transition_migration.py::test_set_dve_style_reverse_flipflop
		{DVESettings{Style: Set[uint8](3)}, ""},
		{DVESettings{Reverse: Set(true)}, ""},
		{DVESettings{FlipFlop: Set(true)}, ""},
		// test_transition_migration.py::test_set_stinger_rate_and_mix_rate_both_send_ctst
		{StingerSettings{Rate: Set(uint16(frames("1:00", 50)))}, "CTSt"},
		{StingerSettings{Rate: Set(uint16(frames("1:00", 50)))}, "CTSt"},
		// test_transition_migration.py::test_set_stinger_source_uses_mediaplayer_field
		{StingerSettings{MediaPlayer: Set[uint8](2)}, "CTSt"},
		// test_transition_migration.py::test_set_stinger_clip_duration_trigger_pre_roll_parse_rate
		{StingerSettings{Duration: Set(uint16(frames("2:00", 25)))}, ""},
		{StingerSettings{TriggerPoint: Set(uint16(frames("0:10", 25)))}, ""},
		{StingerSettings{Preroll: Set(uint16(frames("0:05", 25)))}, ""},
		// test_transition_migration.py::test_set_stinger_clip_gain_tenths_scale_no_clamp
		{StingerSettings{KeyClip: Set(60.0)}, ""},
		{StingerSettings{KeyGain: Set(33.3)}, ""},
		// test_transition_migration.py::test_set_stinger_bool_setters
		{StingerSettings{KeyPremultiplied: Set(false)}, ""},
		{StingerSettings{KeyInvert: Set(true)}, ""},
	})

	// test_transition_migration.py::test_set_transition_style_packs_style
	if p := emit(t, TransitionSettings{Style: Set(StyleWipe)})[8:]; p[0]&0b01 == 0 || p[1] != 0 || p[2] != 2 {
		t.Errorf("CTTp %x", p)
	}
	// test_transition_migration.py::test_set_next_transition_layers_noops_on_zero_mask
	if !refuses(TransitionSettings{NextTransition: Set[uint8](0)}) {
		t.Error("empty layer selection sent")
	}
	// test_transition_migration.py::test_set_dve_rate_wire_offsets_correction
	if p := emit(t, DVESettings{Rate: Set(uint8(frames("0:10", 50)))})[8:]; u16At(p, 0) != 0b10 || p[4] != 10 || p[3] != 0 {
		t.Errorf("CTDv %x", p)
	}
}

func TestUpstreamKeyerOps(t *testing.T) {
	checkSends(t, []sends{
		// test_upstream_keyer_migration.py::test_set_usk_on_air_packs_ckon
		{KeyOnAir{Enabled: true}, "CKOn"},
		// test_upstream_keyer_migration.py::test_set_usk_type_packs_cktp
		{KeyType{Keyer: 1, Type: Set(KeyDVE)}, "CKTp"},
		// test_upstream_keyer_migration.py::test_set_usk_fill_and_key_sources
		{KeyFill{Source: 15}, "CKeF"},
		{KeyCut{Source: 18}, "CKeC"},
		// test_upstream_keyer_migration.py::test_set_usk_luma_clip_gain_use_tenths_scale
		{KeyPropertiesLuma{Clip: Set(66.0)}, "CKLm"},
		{KeyPropertiesLuma{Gain: Set(50.0)}, "CKLm"},
		// test_upstream_keyer_migration.py::test_set_usk_luma_clip_clamps_above_100
		{KeyPropertiesLuma{Clip: Set(150.0)}, "CKLm"},
		// test_upstream_keyer_migration.py::test_set_usk_luma_invert_and_pre_multiplied_bool
		{KeyPropertiesLuma{InvertKey: Set(true)}, "CKLm"},
		{KeyPropertiesLuma{Premultiplied: Set(true)}, "CKLm"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_foreground_scale_thousandths
		{KeyPropertiesAdvancedChroma{Foreground: Set(0.091)}, "CACK"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_5_field_setters
		{KeyPropertiesAdvancedChroma{Background: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{KeyEdge: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Spill: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Flare: Set(0.5)}, "CACK"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_color_correction_setters
		{KeyPropertiesAdvancedChroma{Brightness: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Contrast: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Saturation: Set(0.6)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Red: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Green: Set(0.5)}, "CACK"},
		{KeyPropertiesAdvancedChroma{Blue: Set(0.5)}, "CACK"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_sample_toggles_cursor
		{KeyPropertiesAdvancedChromaColorpicker{Cursor: Set(true)}, "CACC"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_sample_position_uses_signed_wire
		{KeyPropertiesAdvancedChromaColorpicker{X: Set(0.5), Y: Set(0.5)}, "CACC"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_sample_size_clamps_to_spec_range
		{KeyPropertiesAdvancedChromaColorpicker{Size: Set(0.05)}, "CACC"},
		{KeyPropertiesAdvancedChromaColorpicker{Size: Set(2.0)}, "CACC"},
		// test_upstream_keyer_migration.py::test_set_usk_chroma_preview_toggles_overlay
		{KeyPropertiesAdvancedChromaColorpicker{Preview: Set(true)}, "CACC"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_position_clamps_to_spec_range
		{KeyPropertiesDVE{PosX: Set(20.0)}, "CKDV"},
		{KeyPropertiesDVE{PosY: Set(-12.0)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_size_uses_size_thousandths_helper
		{KeyPropertiesDVE{SizeX: Set(1.5)}, "CKDV"},
		{KeyPropertiesDVE{SizeY: Set(0.5)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_rotation_scale_tenths
		{KeyPropertiesDVE{Rotation: Set(45.0)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_border_enabled_bool
		{KeyPropertiesDVE{BorderEnabled: Set(true)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_border_hue_sat_luma_use_scale_10
		{KeyPropertiesDVE{BorderHue: Set(180.0)}, "CKDV"},
		{KeyPropertiesDVE{BorderSaturation: Set(50.0)}, "CKDV"},
		{KeyPropertiesDVE{BorderLuma: Set(75.0)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_border_widths_scale_hundredths
		{KeyPropertiesDVE{OuterWidth: Set(8.5)}, "CKDV"},
		{KeyPropertiesDVE{InnerWidth: Set(2.0)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_border_softness_and_opacity
		{KeyPropertiesDVE{BorderOpacity: Set(90.0)}, ""},
		{KeyPropertiesDVE{OuterSoftness: Set(50.0)}, ""},
		{KeyPropertiesDVE{InnerSoftness: Set(25.0)}, ""},
		{KeyPropertiesDVE{BevelSoftness: Set(10.0)}, ""},
		{KeyPropertiesDVE{BevelPosition: Set(50.0)}, ""},
		// test_upstream_keyer_migration.py::test_set_usk_dve_mask_setters_scale_thousandths
		{KeyPropertiesDVE{MaskEnabled: Set(true)}, "CKDV"},
		{KeyPropertiesDVE{MaskTop: Set(5.0)}, "CKDV"},
		{KeyPropertiesDVE{MaskBottom: Set(-5.0)}, "CKDV"},
		{KeyPropertiesDVE{MaskLeft: Set(8.0)}, "CKDV"},
		{KeyPropertiesDVE{MaskRight: Set(-8.0)}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_dve_light_direction_scale_tenths
		{KeyPropertiesDVE{Angle: Set(90.0)}, ""},
		{KeyPropertiesDVE{Altitude: Set(45.0)}, ""},
		{KeyPropertiesDVE{ShadowEnabled: Set(true)}, ""},
		// test_upstream_keyer_migration.py::test_set_usk_dve_rate_parses_against_display_fps
		{KeyPropertiesDVE{Rate: Set(uint8(ParseRate("1:00", fpsAt(60))))}, "CKDV"},
		// test_upstream_keyer_migration.py::test_set_usk_pattern_style_identity
		{KeyPropertiesPattern{Pattern: Set[uint8](3)}, "CKPt"},
		// test_upstream_keyer_migration.py::test_set_usk_pattern_size_uses_thousandths
		{KeyPropertiesPattern{Size: Set(75.0)}, "CKPt"},
		// test_upstream_keyer_migration.py::test_set_usk_pattern_symmetry_and_softness
		{KeyPropertiesPattern{Symmetry: Set(50.0)}, "CKPt"},
		{KeyPropertiesPattern{Softness: Set(25.0)}, "CKPt"},
		// test_upstream_keyer_migration.py::test_set_usk_pattern_invert_bool
		{KeyPropertiesPattern{InvertPattern: Set(true)}, "CKPt"},
		// test_upstream_keyer_migration.py::test_set_usk_pattern_position_uses_unit_thousandths
		{KeyPropertiesPattern{PositionX: Set(0.5)}, "CKPt"},
		{KeyPropertiesPattern{PositionY: Set(0.25)}, "CKPt"},
		// test_upstream_keyer_migration.py::test_set_usk_mask_enabled_bool
		{KeyPropertiesMask{Enabled: Set(true)}, "CKMs"},
		// test_upstream_keyer_migration.py::test_set_usk_mask_fields_scale_thousandths
		{KeyPropertiesMask{Top: Set(5.0)}, "CKMs"},
		{KeyPropertiesMask{Bottom: Set(-5.0)}, "CKMs"},
		{KeyPropertiesMask{Left: Set(8.0)}, "CKMs"},
		{KeyPropertiesMask{Right: Set(-8.0)}, "CKMs"},
		// test_upstream_keyer_migration.py::test_set_usk_fly_enabled_packs_cktp
		{KeyType{FlyEnabled: Set(true)}, "CKTp"},
		// test_upstream_keyer_migration.py::test_run_flying_key_keyframe_packs_rflk
		{KeyerKeyframeRun{RunTo: KeyframeA}, "RFlK"},
		{KeyerKeyframeRun{RunTo: KeyframeFull}, "RFlK"},
		// test_upstream_keyer_migration.py::test_run_flying_key_infinite_direction_includes_direction
		{KeyerKeyframeRun{RunTo: KeyframeRunToInfinite, Direction: Set[uint8](5)}, "RFlK"},
	})

	// test_upstream_keyer_migration.py::test_run_flying_key_infinite_direction_includes_direction
	// (bmdwire#22): the location at byte 5 under mask bit 1. By name, and
	// all ten, in flykey_test.go.
	if b := emit(t, KeyerKeyframeRun{RunTo: KeyframeRunToInfinite, Direction: Set[uint8](5)}); hex.EncodeToString(b[8:]) != "0200000004050000" {
		t.Errorf("RFlK %x", b[8:])
	}

	// test_upstream_keyer_migration.py::test_configure_usk_luma_uses_tenths_scale
	if b := emit(t, KeyPropertiesLuma{Clip: Set(50.0), Gain: Set(50.0)}); string(b[4:8]) != "CKLm" ||
		u16At(b, 12) != 500 || u16At(b, 14) != 500 {
		t.Errorf("CKLm %x", b)
	}
	// test_upstream_keyer_migration.py::test_set_keyer_fly_keyframe_packs_sfkf
	for kf, want := range map[uint8]byte{KeyframeA: 1, KeyframeB: 2, KeyframeFull: 3} {
		if b := emit(t, KeyerKeyframeSet{Keyframe: kf}); string(b[4:8]) != "SFKF" || b[10] != want {
			t.Errorf("SFKF %x", b)
		}
	}
}

func TestDownstreamKeyerOps(t *testing.T) {
	checkSends(t, []sends{
		// test_downstream_keyer_migration.py::test_set_dsk_on_air_packs_cdsl
		{DkeyOnair{DSK: 1, OnAir: true}, "CDsL"},
		// test_downstream_keyer_migration.py::test_dsk_auto_packs_ddsa
		{DkeyAuto{}, "DDsA"},
		// test_downstream_keyer_migration.py::test_set_dsk_rate_parses_against_display_fps
		{DkeyRate{Rate: uint8(ParseRate("1:00", fpsAt(60)))}, "CDsR"},
		// test_downstream_keyer_migration.py::test_set_dsk_fill_and_key_sources
		{DkeySetFill{Source: 5}, "CDsF"},
		{DkeySetKey{Source: 7}, "CDsC"},
		// test_downstream_keyer_migration.py::test_set_dsk_mask_fields_scale_thousandths
		{DkeyMask{Top: Set(5.0)}, "CDsM"},
		{DkeyMask{Bottom: Set(-5.0)}, "CDsM"},
		// test_downstream_keyer_migration.py::test_set_dsk_mask_enabled_packs_cdsm
		{DkeyMask{Enabled: Set(true)}, "CDsM"},
		// test_downstream_keyer_migration.py::test_set_dsk_clip_gain_use_tenths_scale
		{DkeyGain{Clip: Set(66.0)}, "CDsG"},
		{DkeyGain{Gain: Set(50.0)}, "CDsG"},
		// test_downstream_keyer_migration.py::test_set_dsk_clip_clamps_above_100
		{DkeyGain{Clip: Set(150.0)}, "CDsG"},
		// test_downstream_keyer_migration.py::test_set_dsk_pre_multiplied_and_invert_bool
		{DkeyGain{Premultiplied: Set(true)}, "CDsG"},
		{DkeyGain{Invert: Set(true)}, "CDsG"},
	})

	// test_downstream_keyer_migration.py::test_configure_dsk_gain_uses_tenths_scale_for_clip_gain
	if b := emit(t, DkeyGain{Clip: Set(50.0), Gain: Set(50.0)}); string(b[4:8]) != "CDsG" ||
		u16At(b, 12) != 500 || u16At(b, 14) != 500 {
		t.Errorf("CDsG %x", b)
	}
}

func TestFadeToBlackOps(t *testing.T) {
	// test_fade_to_black_migration.py::test_fade_to_black_triggers_ftba
	if b := emit(t, FadeToBlack{ME: 1}); string(b[4:8]) != "FtbA" || b[8] != 1 {
		t.Errorf("FtbA %x", b)
	}
	// test_fade_to_black_migration.py::test_set_ftb_rate_uses_display_fps_for_parsing
	if p := emit(t, FadeToBlackConfig{Frames: uint8(ParseRate("1:00", fpsAt(60)))})[8:]; p[0] != 1 || p[1] != 0 || p[2] != 30 {
		t.Errorf("FtbC %x", p)
	}
	// test_fade_to_black_migration.py::test_set_ftb_rate_defaults_to_25_when_video_mode_missing
	if p := emit(t, FadeToBlackConfig{Frames: uint8(ParseRate("1:00", fpsAt(0)))})[8:]; p[2] != 25 {
		t.Errorf("FtbC %x", p)
	}
	// test_fade_to_black_migration.py::test_set_ftb_disabled_inverts_to_enable
	if p := emit(t, FadeToBlackEnable{Enabled: false})[8:]; p[1] != 0 {
		t.Errorf("FEna disable %x", p)
	}
	// test_fade_to_black_migration.py::test_set_ftb_disabled_inverts_to_enable_when_false
	if p := emit(t, FadeToBlackEnable{Enabled: true})[8:]; p[1] != 1 {
		t.Errorf("FEna enable %x", p)
	}
	// test_fade_to_black_migration.py::test_set_ftb_disabled_pins_me1_for_any_me:
	// FadeToBlackEnable has no M/E, so the loop over me is one case.
	if p := emit(t, FadeToBlackEnable{Enabled: false})[8:]; !bytes.Equal(p, []byte{0, 0, 0, 0}) {
		t.Errorf("FEna disable %x", p)
	}
	if p := emit(t, FadeToBlackEnable{Enabled: true})[8:]; !bytes.Equal(p[:2], []byte{0, 1}) {
		t.Errorf("FEna enable %x", p)
	}
}

func TestColorGeneratorOps(t *testing.T) {
	// test_color_generator_migration.py::test_set_color_generator_hue_packs_with_scale_10
	if p := emit(t, ColorGenerator{Generator: 0, Hue: Set(180.5)})[8:]; p[1] != 0 || u16At(p, 2) != 1805 || p[0]&0b001 == 0 {
		t.Errorf("CClV hue %x", p)
	}
	// test_color_generator_migration.py::test_set_color_generator_saturation_percent_to_wire
	if p := emit(t, ColorGenerator{Generator: 1, Saturation: Set(50.0)})[8:]; p[1] != 1 || u16At(p, 4) != 500 || p[0]&0b010 == 0 {
		t.Errorf("CClV saturation %x", p)
	}
	// test_color_generator_migration.py::test_set_color_generator_luma_clamps_high
	if p := emit(t, ColorGenerator{Luma: Set(150.0)})[8:]; u16At(p, 6) != 1000 {
		t.Errorf("CClV luma 150 %x", p)
	}
	// test_color_generator_migration.py::test_set_color_generator_luma_clamps_low
	if p := emit(t, ColorGenerator{Luma: Set(-5.0)})[8:]; u16At(p, 6) != 0 {
		t.Errorf("CClV luma -5 %x", p)
	}
}

// test_dsl_scale.py, the ColorGeneratorCommand cases: the Send class
// takes saturation and luma as 0-1, the Go command as percent.
func TestColorGeneratorScale(t *testing.T) {
	// test_color_generator_command_hue_scale_10
	if p := emit(t, ColorGenerator{Hue: Set(180.5)})[8:]; u16At(p, 2) != 1805 || p[0]&0b001 == 0 {
		t.Errorf("hue %x", p)
	}
	// test_color_generator_command_saturation_scale_1000
	if p := emit(t, ColorGenerator{Generator: 1, Saturation: Set(50.0)})[8:]; u16At(p, 4) != 500 || p[0]&0b010 == 0 {
		t.Errorf("saturation %x", p)
	}
	// test_color_generator_command_luma_scale_1000
	if p := emit(t, ColorGenerator{Luma: Set(75.0)})[8:]; u16At(p, 6) != 750 || p[0]&0b100 == 0 {
		t.Errorf("luma %x", p)
	}
	// test_color_generator_command_all_three
	p := emit(t, ColorGenerator{Generator: 1, Hue: Set(359.9), Saturation: Set(100.0), Luma: Set(50.0)})[8:]
	if p[0] != 0b111 || p[1] != 1 || u16At(p, 2) != 3599 || u16At(p, 4) != 1000 || u16At(p, 6) != 500 {
		t.Errorf("all three %x", p)
	}
	// test_color_generator_send_recv_consistency: the CClV wire values,
	// spliced into a ColV, parse back to what was sent.
	p = emit(t, ColorGenerator{Generator: 2, Hue: Set(240.0), Saturation: Set(60.0), Luma: Set(40.0)})[8:]
	f, err := parseColV(append([]byte{2, 0}, p[2:8]...))
	if err != nil {
		t.Fatal(err)
	}
	if f.Hue != 240 || f.Saturation != 0.6 || f.Luma != 0.4 {
		t.Errorf("%+v", f)
	}
}

func TestMediaOps(t *testing.T) {
	// test_media_migration.py::test_clear_still_packs_slot_index
	if b := emit(t, ClearStill{Slot: 7}); string(b[4:8]) != "CSTL" || b[8] != 7 {
		t.Errorf("CSTL %x", b)
	}
	// test_media_migration.py::test_capture_still_has_no_payload
	if b := emit(t, CaptureStill{}); string(b[4:8]) != "Capt" || len(b) != 8 {
		t.Errorf("Capt %x", b)
	}
	// test_media_migration.py::test_set_media_player_still_packs_mask_source_slot
	if p := emit(t, MediaplayerSelect{Player: 0, Still: Set[uint8](5)})[8:]; p[0] != 0b011 || p[1] != 0 || p[2] != 1 || p[3] != 5 {
		t.Errorf("MPSS still %x", p)
	}
	// test_media_migration.py::test_set_media_player_clip_packs_mask_source_slot
	if p := emit(t, MediaplayerSelect{Player: 1, Clip: Set[uint8](3)})[8:]; p[0] != 0b101 || p[1] != 1 || p[2] != 2 || p[4] != 3 {
		t.Errorf("MPSS clip %x", p)
	}
}

func TestMacroOps(t *testing.T) {
	// test_macros_migration.py::test_action_constants_values
	if MacroRun != 0 || MacroStop != 1 || MacroStopRecord != 2 || MacroInsertUserWait != 3 || MacroContinue != 4 {
		t.Error("macro action constants")
	}
	// test_macros_migration.py::test_execute_macro_converts_1_indexed_to_0_indexed:
	// execute_macro(1) is slot 0.
	if b := emit(t, MacroAction{Slot: 1 - 1, Action: MacroRun}); string(b[4:8]) != "MAct" || u16At(b, 8) != 0 || b[10] != MacroRun {
		t.Errorf("MAct %x", b)
	}
	// test_macros_migration.py::test_execute_macro_with_higher_number
	if p := emit(t, MacroAction{Slot: 20 - 1, Action: MacroRun})[8:]; u16At(p, 0) != 19 {
		t.Errorf("MAct %x", p)
	}
	// test_macros_migration.py::test_macro_run_uses_0_indexed_slot
	if p := emit(t, MacroAction{Slot: 5, Action: MacroRun})[8:]; u16At(p, 0) != 5 {
		t.Errorf("MAct %x", p)
	}
	// test_macros_migration.py::test_macro_stop_uses_sentinel_index
	if p := emit(t, MacroAction{Slot: MacroNoSlot, Action: MacroStop})[8:]; u16At(p, 0) != 0xFFFF || p[2] != MacroStop {
		t.Errorf("MAct %x", p)
	}
	// test_macros_migration.py::test_macro_stop_recording_uses_stop_record_action
	if p := emit(t, MacroAction{Slot: MacroNoSlot, Action: MacroStopRecord})[8:]; p[2] != MacroStopRecord {
		t.Errorf("MAct %x", p)
	}
	checkSends(t, []sends{
		// test_macros_migration.py::test_macro_start_recording_sends_msrc_with_name
		{MacroRecord{Slot: 2, Name: "Setup", Description: "Live shot"}, "MSRc"},
		// test_macros_migration.py::test_macro_sleep_sends_mslp_with_frame_count
		{MacroSleep{Frames: 30}, "MSlp"},
	})
}

func TestFairlightOps(t *testing.T) {
	// test_fairlight_migration.py::test_constants_values
	if FairlightMixOff != 0x01 || FairlightMixOn != 0x02 || FairlightMixAFV != 0x04 ||
		EQLowShelf != 0x01 || EQLowPass != 0x02 || EQBandPass != 0x04 || EQNotch != 0x08 || EQHighPass != 0x10 || EQHighShelf != 0x20 ||
		EQRangeLow != 1 || EQRangeMidLow != 2 || EQRangeMidHigh != 4 || EQRangeHigh != 8 {
		t.Error("fairlight constants")
	}

	checkSends(t, []sends{
		// test_fairlight_migration.py::test_set_fairlight_master_emits_cfmp
		{FairlightMasterProperties{EQGain: Set(3.0), Volume: Set(0.0)}, "CFMP"},
		// test_fairlight_migration.py::test_set_fairlight_strip_emits_cfsp_with_split_flag
		{FairlightStripProperties{Source: 1, Channel: -1, Volume: Set(0.0)}, "CFSP"},
		// test_fairlight_migration.py::test_set_fairlight_strip_mix_option_by_name
		{FairlightStripProperties{Source: 1, Channel: -1, State: Set(FairlightMixAFV)}, "CFSP"},
		// test_fairlight_migration.py::test_set_fairlight_strip_mix_option_int_passthrough
		{FairlightStripProperties{Source: 1, Channel: -1, State: Set[uint8](2)}, "CFSP"},
		// test_fairlight_migration.py::test_set_fairlight_eq_band_emits_cebp_with_shape_name
		{FairlightEQBandProperties{Source: 1, Channel: -1, Band: 2, Shape: Set(EQBandPass), FrequencyRange: Set(EQRangeMidLow),
			Gain: Set(2.5)}, "CEBP"},
		// test_fairlight_migration.py::test_set_fairlight_compressor_emits_cicp
		{FairlightCompressorProperties{Source: 1, Channel: -1, Threshold: Set(-20.0), Ratio: Set(4.0)}, "CICP"},
		// test_fairlight_migration.py::test_set_fairlight_limiter_emits_cilp
		{FairlightLimiterProperties{Source: 1, Channel: -1, Threshold: Set(-10.0)}, "CILP"},
		// test_fairlight_migration.py::test_set_fairlight_expander_emits_cixp_with_mode
		{FairlightExpanderProperties{Source: 1, Channel: -1, Mode: Set[uint8](1), Threshold: Set(-30.0), Range: Set(20.0)}, "CIXP"},
		// test_fairlight_migration.py::test_enable_fairlight_levels_emits_sfln
		{SendFairlightLevels{Enable: true}, "SFLN"},
		// test_fairlight_migration.py::test_enable_fairlight_levels_disable_emits_sfln
		{SendFairlightLevels{Enable: false}, "SFLN"},
	})

	// test_fairlight_migration.py::test_set_fairlight_master_volume_neg_inf_clamps
	if b := emit(t, FairlightMasterProperties{Volume: Set(math.Inf(-1))}); i32At(b, 20) != -10000 {
		t.Errorf("CFMP %x", b)
	}
}

// test_fairlight_migration.py, the _db_to_wire tests: _db_to_wire is
// wire.dB, whose scale is always 100.
func TestDBToWire(t *testing.T) {
	w := newWire("TEST", 0)
	for _, c := range []struct {
		v           float64
		lo, hi, out int64
	}{
		{0.0, -10000, 1000, 0}, // test_db_to_wire_scales_and_rounds
		{-12.5, -10000, 1000, -1250},
		{5.4321, -10000, 1000, 543},
		{math.Inf(-1), -10000, 1000, -10000}, // test_db_to_wire_negative_infinity_clamps_to_floor
		{math.Inf(-1), 0, 2000, 0},
		{999.0, -10000, 1000, 1000},  // test_db_to_wire_clamps_high
		{-999.0, -2000, 1000, -2000}, // test_db_to_wire_clamps_low
	} {
		if got := w.dB("db", c.v, c.lo, c.hi); got != c.out {
			t.Errorf("_db_to_wire(%v, lo=%d, hi=%d) = %d, want %d", c.v, c.lo, c.hi, got, c.out)
		}
	}
	if w.err != nil {
		t.Error(w.err)
	}
}

func TestFairlightMasterDynamics(t *testing.T) {
	// test_fairlight_migration.py::test_set_master_compressor_emits_cmcp_full_mask
	b := emit(t, FairlightMasterCompressorProperties{Enabled: Set(true), Threshold: Set(-23.55), Ratio: Set(6.01),
		Attack: Set(11.56), Hold: Set(7.04), Release: Set(249.76)})
	if p := b[8:]; string(b[4:8]) != "CMCP" || len(p) != 24 || p[0] != 0x3f || p[1] != 0x01 || i32At(p, 4) != -2355 ||
		i16At(p, 6) != -2355 || u16At(p, 8) != 601 || i32At(p, 12) != 1156 || i32At(p, 16) != 704 || i32At(p, 20) != 24976 {
		t.Errorf("CMCP full %x", b)
	}
	// test_fairlight_migration.py::test_set_master_compressor_single_field_matches_capture
	if p := emit(t, FairlightMasterCompressorProperties{Threshold: Set(-23.5)})[8:]; p[0] != 0x02 || i32At(p, 4) != -2350 ||
		!bytes.Equal(p[4:6], []byte{0xff, 0xff}) {
		t.Errorf("CMCP threshold %x", p)
	}
	// test_fairlight_migration.py::test_set_master_limiter_emits_cmlp_full_mask
	b = emit(t, FairlightMasterLimiterProperties{Enabled: Set(true), Threshold: Set(-0.99), Attack: Set(2.24),
		Hold: Set(5.0), Release: Set(99.59)})
	if p := b[8:]; string(b[4:8]) != "CMLP" || len(p) != 20 || p[0] != 0x1f || p[1] != 0x01 || i32At(p, 4) != -99 ||
		i32At(p, 8) != 224 || i32At(p, 12) != 500 || i32At(p, 16) != 9959 {
		t.Errorf("CMLP full %x", b)
	}
	// test_fairlight_migration.py::test_set_master_limiter_single_field_matches_capture
	if p := emit(t, FairlightMasterLimiterProperties{Release: Set(100.0)})[8:]; p[0] != 0x10 || i32At(p, 16) != 10000 {
		t.Errorf("CMLP release %x", p)
	}
}

func TestFairlightMasterEQ(t *testing.T) {
	// test_fairlight_migration.py::test_set_master_eq_enable_gain_offsets_match_capture
	b := emit(t, FairlightMasterProperties{EQEnable: Set(true), EQGain: Set(8.7)})
	if p := b[8:]; string(b[4:8]) != "CFMP" || p[0] != 0x03 || p[1] != 1 || i32At(p, 4) != 870 || i16At(p, 6) != 870 {
		t.Errorf("CFMP %x", b)
	}
	// test_fairlight_migration.py::test_set_master_eq_gain_negative_sign_extends
	if p := emit(t, FairlightMasterProperties{EQGain: Set(-11.45)})[8:]; p[0] != 0x02 || i32At(p, 4) != -1145 ||
		p[4] != 0xff || p[5] != 0xff || i16At(p, 6) != -1145 {
		t.Errorf("CFMP %x", p)
	}
	// test_fairlight_migration.py::test_set_master_eq_band_emits_cmbp_full
	b = emit(t, FairlightMasterEQBandProperties{Band: 1, Enabled: Set(true), Shape: Set(EQLowShelf), FrequencyRange: Set(EQRangeLow),
		Frequency: Set(153), Gain: Set(13.5), Q: Set(2.3)})
	if p := b[8:]; string(b[4:8]) != "CMBP" || len(p) != 20 || p[0] != 0x3f || p[1] != 1 || p[2] != 1 || p[3] != 0x01 ||
		p[4] != 0x01 || u16At(p, 10) != 153 || i32At(p, 12) != 1350 || u16At(p, 16) != 230 {
		t.Errorf("CMBP full %x", b)
	}
	// test_fairlight_migration.py::test_set_master_eq_band_single_fields_match_capture
	if p := emit(t, FairlightMasterEQBandProperties{Band: 1, Frequency: Set(51)})[8:]; p[0] != 0x08 || p[1] != 1 || u16At(p, 10) != 51 {
		t.Errorf("CMBP frequency %x", p)
	}
	if p := emit(t, FairlightMasterEQBandProperties{Band: 1, Gain: Set(-11.0)})[8:]; p[0] != 0x10 || p[1] != 1 || i32At(p, 12) != -1100 {
		t.Errorf("CMBP gain %x", p)
	}
	if p := emit(t, FairlightMasterEQBandProperties{Band: 3, Q: Set(0.8)})[8:]; p[0] != 0x20 || p[1] != 3 || u16At(p, 16) != 80 {
		t.Errorf("CMBP q %x", p)
	}
	if p := emit(t, FairlightMasterEQBandProperties{Band: 1, Shape: Set(EQNotch)})[8:]; p[0] != 0x02 || p[1] != 1 || p[3] != 0x08 {
		t.Errorf("CMBP shape %x", p)
	}
}

func TestInputVideoOps(t *testing.T) {
	checkSends(t, []sends{
		// test_input_video_migration.py::test_set_input_label_packs_cinl
		{InputProperties{Source: 1, LongName: Set("Cam One"), ShortName: Set("C1")}, "CInL"},
		// test_input_video_migration.py::test_set_input_label_with_only_short_name
		{InputProperties{Source: 2, ShortName: Set("Rt")}, "CInL"},
	})
	// test_input_video_migration.py::test_set_video_mode_with_string_name
	if b := emit(t, VideoMode{Mode: VideoModeNames["1080p60"]}); string(b[4:8]) != "CVdM" || b[8] != 27 {
		t.Errorf("CVdM %x", b)
	}
	// test_input_video_migration.py::test_set_video_mode_with_integer
	if p := emit(t, VideoMode{Mode: 12})[8:]; p[0] != 12 {
		t.Errorf("CVdM %x", p)
	}
	// test_input_video_migration.py::test_set_video_mode_unknown_string_raises:
	// set_video_mode's table is VideoModeNames.
	if m, ok := VideoModeNames["not_a_mode"]; ok {
		t.Errorf("not_a_mode is mode %d", m)
	}
	// test_input_video_migration.py::test_set_video_mode_aliases
	for _, name := range []string{"NTSC", "525i5994"} {
		if b := emit(t, VideoMode{Mode: VideoModeNames[name]}); b[8] != 0 {
			t.Errorf("%s: CVdM %x", name, b)
		}
	}
}

func TestHyperdeckCXMS(t *testing.T) {
	payload := func(c HyperdeckSettings) []byte {
		b := emit(t, c)
		if string(b[4:8]) != "CXMS" || len(b[8:]) != 16 {
			t.Fatalf("CXMS %x", b)
		}
		return b[8:]
	}
	// test_hyperdeck.py::test_cxms_network_address_only
	if p := payload(HyperdeckSettings{Slot: 1, NetworkAddress: Set("10.20.30.40")}); p[0] != 0x01 || u16At(p, 2) != 1 ||
		!bytes.Equal(p[4:8], []byte{10, 20, 30, 40}) {
		t.Errorf("%x", p)
	}
	// test_hyperdeck.py::test_cxms_input_only
	if p := payload(HyperdeckSettings{Slot: 2, SwitcherInput: Set[uint16](8)}); p[0] != 0x02 || u16At(p, 2) != 2 || u16At(p, 8) != 8 {
		t.Errorf("%x", p)
	}
	// test_hyperdeck.py::test_cxms_both_fields_set_both_mask_bits
	if p := payload(HyperdeckSettings{Slot: 0, NetworkAddress: Set("1.2.3.4"), SwitcherInput: Set[uint16](5)}); p[0] != 0x03 ||
		!bytes.Equal(p[4:8], []byte{1, 2, 3, 4}) || u16At(p, 8) != 5 {
		t.Errorf("%x", p)
	}
	// test_hyperdeck.py::test_cxms_clear_slot_with_zero_ip
	if p := payload(HyperdeckSettings{Slot: 4, NetworkAddress: Set("0.0.0.0")}); p[0] != 0x01 || !bytes.Equal(p[4:8], []byte{0, 0, 0, 0}) {
		t.Errorf("%x", p)
	}
}
