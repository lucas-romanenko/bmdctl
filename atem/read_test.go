// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The assertions bmdwire v1.3.0's tests make about its readers, ported one
// for one: one Go test per Python test file, each block naming its Python
// test. The Python tests build mixerstate from stand-in objects; here the
// same values go into the typed State. TestReadersMatchAtemwire
// (read_oracle_test.go) covers every reader more broadly.

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net"
	"reflect"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// near is pytest.approx's default: a relative tolerance of 1e-6.
func near(got, want float64) bool {
	return math.Abs(got-want) <= max(1e-6*math.Abs(want), 1e-12)
}

func TestReadSwitching(t *testing.T) {
	// test_switching_migration.py::test_program_source_reads_from_mixerstate
	s := newState()
	at(&s.MEs, 0).Program = &ProgramBusInputField{Source: 3}
	at(&s.MEs, 1).Program = &ProgramBusInputField{Source: 7}
	if s.ProgramSource(0) != 3 || s.ProgramSource(1) != 7 {
		t.Error("program_source")
	}

	// test_switching_migration.py::test_preview_source_reads_from_mixerstate
	s = newState()
	at(&s.MEs, 0).Preview = &PreviewBusInputField{Source: 11}
	if s.PreviewSource(0) != 11 {
		t.Error("preview_source")
	}

	// test_switching_migration.py::test_aux_source_reads_from_mixerstate
	s = newState()
	for i, src := range []uint16{101, 102, 103} {
		*at(&s.Aux, i) = &AuxOutputSourceField{Index: uint8(i), Source: src}
	}
	if s.AuxSource(0) != 101 || s.AuxSource(2) != 103 {
		t.Error("aux_source")
	}

	// test_switching_migration.py::test_readers_default_zero_when_mixerstate_empty
	s = newState()
	if s.ProgramSource(0) != 0 || s.PreviewSource(0) != 0 || s.AuxSource(0) != 0 {
		t.Error("defaults")
	}

	// test_switching_migration.py::test_readers_default_zero_when_index_missing
	s = newState()
	at(&s.MEs, 0).Program = &ProgramBusInputField{Source: 5}
	if s.ProgramSource(2) != 0 {
		t.Error("missing M/E")
	}
}

func TestReadFadeToBlack(t *testing.T) {
	// test_fade_to_black_migration.py::test_ftb_active_reads_done
	s := newState()
	at(&s.MEs, 0).FTB.State = &FadeToBlackStateField{Done: true}
	if !s.FTBActive(0) || s.FTBActive(1) {
		t.Error("ftb_active")
	}

	// test_fade_to_black_migration.py::test_ftb_in_transition_reads_transitioning
	s = newState()
	at(&s.MEs, 0).FTB.State = &FadeToBlackStateField{Transitioning: true}
	if !s.FTBInTransition(0) {
		t.Error("ftb_in_transition")
	}

	// test_fade_to_black_migration.py::test_ftb_frames_remaining
	s = newState()
	at(&s.MEs, 0).FTB.State = &FadeToBlackStateField{FramesRemaining: 12}
	if s.FTBFramesRemaining(0) != 12 {
		t.Error("ftb_frames_remaining")
	}

	// test_fade_to_black_migration.py::test_ftb_rate_default_25
	if newState().FTBRate(0) != 25 {
		t.Error("ftb_rate default")
	}

	// test_fade_to_black_migration.py::test_ftb_rate_reads_from_mixerstate
	s = newState()
	at(&s.MEs, 0).FTB.Rate = &FadeToBlackField{Rate: 60}
	if s.FTBRate(0) != 60 {
		t.Error("ftb_rate")
	}

	// test_fade_to_black_migration.py::test_ftb_disabled_singleton_no_me_index
	s = newState()
	s.FTBEnabled = &FadeToBlackEnabledField{Disabled: true}
	if !s.FTBDisabled() || newState().FTBDisabled() {
		t.Error("ftb_disabled")
	}
}

func TestReadDownstreamKeyer(t *testing.T) {
	// test_downstream_keyer_migration.py::test_dsk_state_assembles_all_fields
	s := newState()
	d := at(&s.DSKs, 0)
	d.State = &DkeyStateField{OnAir: true, FramesRemaining: 8}
	d.Base = &DkeyPropertiesBaseField{FillSource: 12, KeySource: 14}
	d.Properties = &DkeyPropertiesField{
		Rate: 30, Premultiplied: true, Clip: 660, Gain: 500, Masked: true,
		Top: 9000, Bottom: -5000, Left: 0, Right: 16000,
	}
	r := s.DSKState(0)
	if !r.OnAir || r.FramesRemaining != 8 || r.FillSource != 12 || r.KeySource != 14 || r.Rate != 30 ||
		!r.PreMultiplied || !near(r.Clip, 66.0) || !near(r.Gain, 50.0) || !r.MaskEnabled ||
		!near(r.MaskTop, 9.0) || !near(r.MaskBottom, -5.0) || !near(r.MaskLeft, 0.0) || !near(r.MaskRight, 16.0) {
		t.Errorf("dsk_state: %+v", r)
	}

	// test_downstream_keyer_migration.py::test_dsk_state_default_when_no_mixerstate
	r = newState().DSKState(0)
	if r.OnAir || r.Rate != 25 || r.FillSource != 0 || r.Clip != 0.0 || r.MaskTop != 0.0 {
		t.Errorf("dsk_state defaults: %+v", r)
	}

	// test_downstream_keyer_migration.py::test_dsk_state_in_transition_combines_two_state_fields
	s = newState()
	at(&s.DSKs, 0).State = &DkeyStateField{IsTransitioning: false, IsAutoTransitioning: true}
	if r := s.DSKState(0); !r.InTransition || !r.IsAutoTransitioning {
		t.Errorf("dsk_state in_transition: %+v", r)
	}
}

func TestReadColorGenerator(t *testing.T) {
	// test_color_generator_migration.py::test_color_generator_reader_scales_back_to_percent
	s := newState()
	*at(&s.ColorGenerators, 0) = &ColorGeneratorField{Hue: 240.0, Saturation: 0.6, Luma: 0.4}
	if r := s.ColorGenerator(0); !near(r.Hue, 240.0) || !near(r.Saturation, 60.0) || !near(r.Luma, 40.0) {
		t.Errorf("color_generator: %+v", r)
	}

	// test_color_generator_migration.py::test_color_generator_reader_default_on_missing
	if r := newState().ColorGenerator(0); r != (ColorGeneratorView{}) {
		t.Errorf("color_generator default: %+v", r)
	}
}

func TestReadHyperdeck(t *testing.T) {
	rxms := func(slot uint16, ip string, input uint16) []byte {
		b := binary.BigEndian.AppendUint16(nil, slot)
		b = append(b, 0, 0)
		b = append(b, net.ParseIP(ip).To4()...)
		b = binary.BigEndian.AppendUint16(b, input)
		return append(b, make([]byte, 10)...)
	}

	// test_hyperdeck.py::test_rxms_reader_configured
	s, _ := storeOne(t, "RXMS", rxms(0, "192.0.2.10", 4))
	if r := s.HyperdeckSettings(0); r != (HyperdeckSettingsView{Slot: 0, NetworkAddress: "192.0.2.10", Input: 4, Configured: true}) {
		t.Errorf("hyperdeck_settings: %+v", r)
	}

	// test_hyperdeck.py::test_rxms_reader_unconfigured_and_missing
	s, _ = storeOne(t, "RXMS", rxms(1, "0.0.0.0", 0))
	if r := s.HyperdeckSettings(1); r.Configured || r.NetworkAddress != "0.0.0.0" {
		t.Errorf("hyperdeck_settings unconfigured: %+v", r)
	}
	if r := newState().HyperdeckSettings(7); r != (HyperdeckSettingsView{Slot: 7, NetworkAddress: "0.0.0.0"}) {
		t.Errorf("hyperdeck_settings missing: %+v", r)
	}

	// _mx: all ten slots, the bound ones with their address and input.
	decks := func(bound map[uint16][2]any) State {
		s := newState()
		for slot := range uint16(10) {
			ip, inp := "0.0.0.0", uint16(0)
			if b, ok := bound[slot]; ok {
				ip, inp = b[0].(string), uint16(b[1].(int))
			}
			if _, err := s.store("RXMS", rxms(slot, ip, inp)); err != nil {
				t.Fatal(err)
			}
		}
		return s
	}
	plan := func(s State, ip string, input int, exclude ...int) (any, bool) {
		slot, write := s.PlanBindingPush(ip, input, exclude...)
		if slot == nil {
			return nil, write
		}
		return *slot, write
	}
	check := func(name string, slot any, write bool, wantSlot any, wantWrite bool) {
		t.Helper()
		if slot != wantSlot || write != wantWrite {
			t.Errorf("%s: (%v, %v), want (%v, %v)", name, slot, write, wantSlot, wantWrite)
		}
	}

	// test_hyperdeck.py::test_plan_reuses_existing_slot_and_detects_drift
	s = decks(map[uint16][2]any{2: {"10.0.0.50", 3}})
	slot, write := plan(s, "10.0.0.50", 4)
	check("drift", slot, write, 2, true)
	slot, write = plan(s, "10.0.0.50", 3)
	check("already right", slot, write, 2, false)

	// test_hyperdeck.py::test_plan_picks_first_free_slot_for_new_deck
	s = decks(map[uint16][2]any{0: {"10.0.0.50", 3}})
	slot, write = plan(s, "10.0.0.60", 5)
	check("first free", slot, write, 1, true)

	// test_hyperdeck.py::test_plan_exclude_slots_skips_just_claimed
	slot, write = plan(s, "10.0.0.70", 6, 1)
	check("exclude", slot, write, 2, true)

	// test_hyperdeck.py::test_plan_none_when_pool_full
	full := map[uint16][2]any{}
	for i := range uint16(10) {
		full[i] = [2]any{net.IPv4(10, 0, 0, byte(i+1)).String(), int(i + 1)}
	}
	slot, write = plan(decks(full), "10.9.9.9", 1)
	check("full", slot, write, nil, false)

	// test_hyperdeck.py::test_plan_empty_mixerstate_defaults_to_ten_slots
	slot, write = plan(newState(), "10.0.0.50", 4)
	check("empty", slot, write, 0, true)
}

func TestReadInputVideo(t *testing.T) {
	// test_input_video_migration.py::test_input_label_decodes_bytes_to_str
	// (InPr names are str in atemwire and Go; the NULs the stand-in carries
	// are cut as decode_name cuts them from bytes.)
	s := newState()
	s.Inputs[1] = &InputPropertiesField{Index: 1, Name: "Camera One\x00\x00\x00", ShortName: "CM1\x00"}
	if r := s.InputLabel(1); r != (InputLabelView{Long: "Camera One", Short: "CM1"}) {
		t.Errorf("input_label: %+v", r)
	}

	// test_input_video_migration.py::test_input_label_default_when_missing
	if r := newState().InputLabel(1); r != (InputLabelView{}) {
		t.Errorf("input_label default: %+v", r)
	}

	// test_input_video_migration.py::test_all_input_sources_sorted_by_id
	s = newState()
	s.Inputs[3] = &InputPropertiesField{Index: 3, Name: "C3"}
	s.Inputs[1] = &InputPropertiesField{Index: 1, Name: "C1", AvailableAux: true}
	s.Inputs[2] = &InputPropertiesField{Index: 2, Name: "C2"}
	all := s.AllInputSources()
	if len(all) != 3 || all[0].Source != 1 || all[1].Source != 2 || all[2].Source != 3 ||
		!all[0].AvailableAux || all[1].Long != "C2" {
		t.Errorf("all_input_sources: %+v", all)
	}

	// test_input_video_migration.py::test_all_input_sources_empty_when_no_inputs
	if all := newState().AllInputSources(); all == nil || len(all) != 0 {
		t.Errorf("all_input_sources empty: %#v", all)
	}

	// test_input_video_migration.py::test_renamable_inputs_partitions_by_port_type
	s = newState()
	for src, in := range map[uint16]struct {
		name string
		port uint8
	}{
		1: {"Cam1", 0}, 2: {"Cam2", 0}, 2001: {"Color1", 3}, 3010: {"MP1", 4},
		10010: {"AUX1", 129}, 7001: {"Pgm", 128}, 0: {"Black", 2},
	} {
		s.Inputs[src] = &InputPropertiesField{Index: src, Name: in.name, PortType: in.port}
	}
	sources := func(es []RenamableInputView) []int {
		var out []int
		for _, e := range es {
			out = append(out, e.Source)
		}
		return out
	}
	r := s.RenamableInputs()
	if !reflect.DeepEqual(sources(r.Inputs), []int{1, 2}) || !reflect.DeepEqual(sources(r.Outputs), []int{7001, 10010}) ||
		!reflect.DeepEqual(sources(r.Media), []int{2001, 3010}) {
		t.Errorf("renamable_inputs: %+v", r)
	}

	// test_input_video_migration.py::test_renamable_inputs_excludes_unrecognized_port_types
	s = newState()
	s.Inputs[99] = &InputPropertiesField{Index: 99, Name: "Unknown", PortType: 255}
	r = s.RenamableInputs()
	if len(r.Inputs)+len(r.Outputs)+len(r.Media) != 0 || r.Inputs == nil || r.Outputs == nil || r.Media == nil {
		t.Errorf("renamable_inputs unknown port type: %#v", r)
	}
}

func TestReadMacros(t *testing.T) {
	// test_macros_migration.py::test_macro_entry_decodes_name
	s := newState()
	s.Macros.Properties[0] = &MacroPropertiesField{IsUsed: true, Name: "Wipe to BG\x00\x00"}
	if r := s.MacroEntry(0); r != (MacroEntryView{IsUsed: true, Name: "Wipe to BG"}) {
		t.Errorf("macro_entry: %+v", r)
	}

	// test_macros_migration.py::test_macro_entry_default_when_missing
	if r := newState().MacroEntry(0); r != (MacroEntryView{}) {
		t.Errorf("macro_entry default: %+v", r)
	}

	// test_macros_migration.py::test_macro_play_running_and_index
	s = newState()
	s.Macros.PlayStatus = &MacroPlayStatusField{Running: true, Index: 5}
	if !s.MacroPlayRunning() || s.MacroPlayIndex() != 5 {
		t.Error("macro_play_running / macro_play_index")
	}

	// test_macros_migration.py::test_macro_play_index_default_idle_sentinel
	if s := newState(); s.MacroPlayIndex() != 0xFFFF || s.MacroPlayRunning() {
		t.Error("macro play defaults")
	}
}

func TestReadMedia(t *testing.T) {
	// test_media_migration.py::test_mediaplayer_slots_reads_capacity
	s := newState()
	s.MediaPool.Slots = &MediaplayerSlotsField{Stills: 32}
	if s.MediaplayerSlots() != 32 || newState().MediaplayerSlots() != 0 {
		t.Error("mediaplayer_slots")
	}

	// test_media_migration.py::test_mediaplayer_slot_info_decodes_name_and_hash
	h := MD5{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	s = newState()
	s.MediaPool.Files[0] = &MediaplayerFileInfoField{IsUsed: true, Hash: h, Name: "shot01\x00\x00"}
	if r := s.MediaplayerSlotInfo(0); !r.IsUsed || r.Hash != "0123456789abcdef0123456789abcdef" || r.Name != "shot01" {
		t.Errorf("mediaplayer_slot_info: %+v", r)
	}

	// test_media_migration.py::test_mediaplayer_slot_info_default_when_unpopulated
	if r := newState().MediaplayerSlotInfo(0); r != (MediaplayerSlotInfoView{}) {
		t.Errorf("mediaplayer_slot_info default: %+v", r)
	}

	// test_media_migration.py::test_mediaplayer_slot_info_all_zero_hash_renders_empty
	s = newState()
	s.MediaPool.Files[0] = &MediaplayerFileInfoField{IsUsed: true, Name: "pending"}
	if r := s.MediaplayerSlotInfo(0); r.Hash != "" {
		t.Errorf("mediaplayer_slot_info zero hash: %+v", r)
	}

	// test_media_migration.py::test_mediaplayer_selected_reads_source_type_and_slot
	s = newState()
	*at(&s.MediaPlayers, 0) = &MediaplayerSelectedField{SourceType: 1, Slot: 4}
	if r := s.MediaplayerSelected(0); r != (MediaplayerSelectedView{SourceType: 1, Slot: 4}) {
		t.Errorf("mediaplayer_selected: %+v", r)
	}

	// test_media_migration.py::test_mediaplayer_selected_default_when_unpopulated
	if r := newState().MediaplayerSelected(0); r != (MediaplayerSelectedView{}) {
		t.Errorf("mediaplayer_selected default: %+v", r)
	}
}

func TestReadSystemInfo(t *testing.T) {
	vidm := func(mode uint8) *VideoModeField {
		f, err := parseVidM([]byte{mode, 0, 0, 0})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}

	// test_system_info_migration.py::test_video_mode_returns_format_and_id
	s := newState()
	s.VideoMode.Mode = vidm(27)
	if r := s.CurrentVideoMode(); r == nil || *r != (VideoModeView{Format: "1080p60", ID: 27}) {
		t.Errorf("video_mode: %+v", r)
	}

	// test_system_info_migration.py::test_video_mode_strips_leading_f has no
	// Go counterpart: its stand-in fakes a label no mode has ("f1080p60").

	// test_system_info_migration.py::test_video_mode_none_when_missing
	if r := newState().CurrentVideoMode(); r != nil {
		t.Errorf("video_mode missing: %+v", r)
	}

	// test_system_info_migration.py::test_video_mode_label_failure_returns_empty_string
	// has no Go counterpart: Label can't fail.

	// test_system_info_migration.py::test_available_video_modes_iterates_and_builds_list
	s = newState()
	s.VideoMode.Capability = &VideoModeCapabilityField{Modes: []VideoModeCapability{
		{ModeNum: 27, Mode: *vidm(27)}, {ModeNum: 12, Mode: *vidm(12)},
	}}
	modes := s.AvailableVideoModes()
	if !reflect.DeepEqual(modes, []AvailableVideoModeView{{27, "1080p60"}, {12, "1080p50"}}) {
		t.Errorf("available_video_modes: %+v", modes)
	}

	// test_system_info_migration.py::test_available_video_modes_empty_during_handshake
	if m := newState().AvailableVideoModes(); m == nil || len(m) != 0 {
		t.Errorf("available_video_modes empty: %#v", m)
	}

	// test_system_info_migration.py::test_available_video_modes_skips_malformed_entries
	// has no Go counterpart: ModeNum is a uint8, so there is no malformed
	// entry to skip.

	// test_system_info_migration.py::test_video_resolution_calls_helper
	s = newState()
	s.VideoMode.Mode = vidm(16) // 2160p25
	if r := s.VideoResolution([2]int{1920, 1080}); r != [2]int{3840, 2160} {
		t.Errorf("video_resolution: %v", r)
	}

	// test_system_info_migration.py::test_video_resolution_default_when_missing
	if r := newState().VideoResolution([2]int{1920, 1080}); r != [2]int{1920, 1080} {
		t.Errorf("video_resolution default: %v", r)
	}
	if r := newState().VideoResolution([2]int{640, 480}); r != [2]int{640, 480} {
		t.Errorf("video_resolution custom default: %v", r)
	}

	// test_system_info_migration.py::test_product_name_decodes_bytes_name
	s = newState()
	s.Identity.Product = &ProductNameField{Name: "ATEM 1 M/E Constellation HD\x00\x00"}
	if r := s.ProductName(""); r != "ATEM 1 M/E Constellation HD" {
		t.Errorf("product_name: %q", r)
	}

	// test_system_info_migration.py::test_product_name_falls_back_to_product_name_attr
	// has no Go counterpart: no packet has a product_name attribute; an
	// empty name falls through to the default, which the oracle covers.

	// test_system_info_migration.py::test_product_name_default_when_missing
	if newState().ProductName("") != "" || newState().ProductName("Unknown") != "Unknown" {
		t.Error("product_name default")
	}
}

func TestReadTransition(t *testing.T) {
	tr := func(s *State, me int) *Transition { return &at(&s.MEs, me).Transition }

	// test_transition_migration.py::test_transition_style_reads_me_indexed_field
	s := newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 3, NextTransitionBkgd: true}
	if s.TransitionStyle(0) != 3 || s.TransitionStyle(1) != 0 {
		t.Error("transition_style")
	}

	// test_transition_migration.py::test_transition_style_per_me_isolation
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 3}
	tr(&s, 1).Settings = &TransitionSettingsField{Style: 1}
	if s.TransitionStyle(0) != 3 || s.TransitionStyle(1) != 1 {
		t.Error("transition_style per M/E")
	}

	// test_transition_migration.py::test_transition_style_defaults_zero
	if newState().TransitionStyle(0) != 0 {
		t.Error("transition_style default")
	}

	// test_transition_migration.py::test_transition_in_transition_and_position.
	// The stand-in's position is 0.42; TrPs carries a u16, which the reader
	// returns as a float unchanged, so the port uses 4200.
	s = newState()
	tr(&s, 0).Position = &TransitionPositionField{InTransition: true, Position: 4200, FramesRemaining: 8}
	if !s.TransitionInTransition(0) || s.TransitionPosition(0) != 4200.0 || s.TransitionFramesRemaining(0) != 8 {
		t.Error("transition position")
	}

	// test_transition_migration.py::test_transition_selection_default_when_unpopulated
	if r := newState().TransitionSelection(0); r != (TransitionSelectionView{Background: true}) {
		t.Errorf("transition_selection default: %+v", r)
	}

	// test_transition_migration.py::test_transition_selection_reads_5_booleans
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{NextTransitionKey1: true, NextTransitionKey3: true}
	if r := s.TransitionSelection(0); r != (TransitionSelectionView{Key1: true, Key3: true}) {
		t.Errorf("transition_selection: %+v", r)
	}

	// test_transition_migration.py::test_mix_dip_rate_and_source_readers
	s = newState()
	tr(&s, 0).Mix = &TransitionMixField{Rate: 30}
	tr(&s, 0).Dip = &TransitionDipField{Rate: 15, Source: 5}
	if s.MixRate(0) != 30 || s.DipRate(0) != 15 || s.DipSource(0) != 5 {
		t.Error("mix/dip")
	}

	// test_transition_migration.py::test_rate_readers_default_25_when_missing
	if s := newState(); s.MixRate(0) != 25 || s.DipRate(0) != 25 || s.DipSource(0) != 0 {
		t.Error("mix/dip defaults")
	}

	// test_transition_migration.py::test_active_transition_rate_dispatches_by_style
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 1}
	tr(&s, 0).Dip = &TransitionDipField{Rate: 42}
	if s.ActiveTransitionRate(0) != 42 {
		t.Error("active_transition_rate dip")
	}

	// test_transition_migration.py::test_wipe_rate_pattern_source
	wipe := TransitionWipeField{Rate: 25, PositionX: 5000, PositionY: 5000, Symmetry: 5000} // _TrWp defaults
	s = newState()
	w := wipe
	w.Rate, w.Pattern, w.Source = 50, 4, 99
	tr(&s, 0).Wipe = &w
	if s.WipeRate(0) != 50 || s.WipePattern(0) != 4 || s.WipeFillSource(0) != 99 {
		t.Error("wipe rate/pattern/source")
	}

	// test_transition_migration.py::test_wipe_position_x_y_unit_scale
	s = newState()
	w = wipe
	w.PositionX, w.PositionY = 5000, 2500
	tr(&s, 0).Wipe = &w
	if !near(s.WipePositionX(0), 0.5) || !near(s.WipePositionY(0), 0.25) {
		t.Error("wipe positions")
	}

	// test_transition_migration.py::test_wipe_position_x_default_half
	if !near(newState().WipePositionX(0), 0.5) {
		t.Error("wipe position default")
	}

	// test_transition_migration.py::test_wipe_softness_symmetry_width_percent_scale
	s = newState()
	w = wipe
	w.Softness, w.Symmetry, w.Width = 2690, 5000, 7500
	tr(&s, 0).Wipe = &w
	if !near(s.WipeSoftness(0), 26.9) || !near(s.WipeSymmetry(0), 50.0) || !near(s.WipeWidth(0), 75.0) {
		t.Error("wipe percents")
	}

	// test_transition_migration.py::test_wipe_reverse_flipflop_bool
	s = newState()
	w = wipe
	w.Reverse, w.FlipFlop = true, false
	tr(&s, 0).Wipe = &w
	if !s.WipeReverse(0) || s.WipeFlipFlop(0) {
		t.Error("wipe bools")
	}

	dve := TransitionDVEField{Rate: 25, FillSource: 1, KeySource: 1, KeyGain: 1000} // _TrDv defaults

	// test_transition_migration.py::test_dve_rate_default_25
	if newState().DVERate(0) != 25 {
		t.Error("dve_rate default")
	}

	// test_transition_migration.py::test_dve_fill_key_sources_defaults
	if s := newState(); s.DVEFillSource(0) != 1 || s.DVEKeySource(0) != 1 {
		t.Error("dve sources default")
	}

	// test_transition_migration.py::test_dve_clip_gain_scale_tenths
	s = newState()
	d := dve
	d.KeyClip, d.KeyGain = 1234, 750
	tr(&s, 0).DVE = &d
	if !near(s.DVEClip(0), 123.4) || !near(s.DVEGain(0), 75.0) {
		t.Error("dve clip/gain")
	}

	// test_transition_migration.py::test_dve_gain_default_100
	if !near(newState().DVEGain(0), 100.0) {
		t.Error("dve_gain default")
	}

	// test_transition_migration.py::test_dve_style_returns_none_when_unset
	if newState().DVEStyle(0) != nil {
		t.Error("dve_style unset")
	}
	s = newState()
	d = dve
	tr(&s, 0).DVE = &d
	if st := s.DVEStyle(0); st == nil || *st != 0 {
		t.Error("dve_style 0")
	}

	// test_transition_migration.py::test_dve_bool_readers
	s = newState()
	d = dve
	d.KeyEnable, d.KeyPremultiplied, d.KeyInvert, d.Reverse, d.FlipFlop = true, true, false, true, false
	tr(&s, 0).DVE = &d
	if !s.DVEEnableKey(0) || !s.DVEPreMultiplied(0) || s.DVEInvertKey(0) || !s.DVEReverse(0) || s.DVEFlipFlop(0) {
		t.Error("dve bools")
	}

	// test_transition_migration.py::test_stinger_rate_default_25
	if newState().StingerRate(0) != 25 {
		t.Error("stinger_rate default")
	}

	// test_transition_migration.py::test_stinger_source_default_slot1
	if newState().StingerSource(0) != 1 {
		t.Error("stinger_source default")
	}

	// test_transition_migration.py::test_stinger_durations_defaults_match_atem_factory
	if s := newState(); s.StingerClipDuration(0) != 150 || s.StingerTriggerPoint(0) != 34 || s.StingerPreRoll(0) != 48 {
		t.Error("stinger duration defaults")
	}

	// test_transition_migration.py::test_stinger_clip_gain_scale_tenths_with_specific_defaults
	if s := newState(); !near(s.StingerClip(0), 50.0) || !near(s.StingerGain(0), 70.0) {
		t.Error("stinger clip/gain defaults")
	}

	// test_transition_migration.py::test_stinger_clip_gain_round_trip. The
	// stand-in's mediaplayer default (3010) doesn't fit the u8 field and no
	// assertion reads it.
	stinger := TransitionStingerField{Rate: 25, Duration: 150, TriggerPoint: 34, Preroll: 48, KeyClip: 500, KeyGain: 700, KeyPremultiplied: true}
	s = newState()
	st := stinger
	st.KeyClip, st.KeyGain = 850, 420
	tr(&s, 0).Stinger = &st
	if !near(s.StingerClip(0), 85.0) || !near(s.StingerGain(0), 42.0) {
		t.Error("stinger clip/gain")
	}

	// test_transition_migration.py::test_stinger_pre_multiplied_default_true
	if s := newState(); !s.StingerPreMultiplied(0) || s.StingerInvertKey(0) {
		t.Error("stinger key defaults")
	}

	// test_transition_migration.py::test_active_transition_rate_dispatches_to_stinger
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 4}
	st = stinger
	st.Rate = 120
	tr(&s, 0).Stinger = &st
	if s.ActiveTransitionRate(0) != 120 {
		t.Error("active_transition_rate stinger")
	}

	// test_transition_migration.py::test_active_transition_rate_dispatches_to_dve
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 3}
	d = dve
	d.Rate = 50
	tr(&s, 0).DVE = &d
	if s.ActiveTransitionRate(0) != 50 {
		t.Error("active_transition_rate dve")
	}

	// test_transition_migration.py::test_active_transition_rate_dispatches_to_wipe
	s = newState()
	tr(&s, 0).Settings = &TransitionSettingsField{Style: 2}
	w = wipe
	w.Rate = 88
	tr(&s, 0).Wipe = &w
	if s.ActiveTransitionRate(0) != 88 {
		t.Error("active_transition_rate wipe")
	}
}

func TestReadUpstreamKeyer(t *testing.T) {
	// test_upstream_keyer_migration.py::test_usk_on_air_reads_nested_dict
	s := newState()
	usk(&s, 0, 1).OnAir = &KeyOnAirField{Enabled: true}
	if !s.USKOnAir(0, 1) || s.USKOnAir(0, 2) || newState().USKOnAir(0, 0) {
		t.Error("usk_on_air")
	}

	// test_upstream_keyer_migration.py::test_usk_type_reads_base
	s = newState()
	usk(&s, 0, 2).Base = &KeyPropertiesBaseField{Type: 3}
	if s.USKType(0, 2) != 3 || s.USKType(0, 0) != 0 {
		t.Error("usk_type")
	}

	// test_upstream_keyer_migration.py::test_usk_fill_and_key_sources_share_base_key
	s = newState()
	usk(&s, 0, 0).Base = &KeyPropertiesBaseField{FillSource: 10, KeySource: 12}
	if s.USKFillSource(0, 0) != 10 || s.USKKeySource(0, 0) != 12 {
		t.Error("usk sources")
	}

	// test_upstream_keyer_migration.py::test_usk_luma_assembles_4_field_dict
	s = newState()
	usk(&s, 0, 0).Luma = &KeyPropertiesLumaField{Clip: 660, Gain: 500, KeyInverted: true}
	if r := s.USKLuma(0, 0); !near(r.LumaClip, 66.0) || !near(r.LumaGain, 50.0) || !r.LumaInvert || r.LumaPreMultiplied {
		t.Errorf("usk_luma: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_luma_default_when_missing
	if r := newState().USKLuma(0, 0); r != (USKLumaView{}) {
		t.Errorf("usk_luma default: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_chroma_default_when_no_kack_no_cacc
	r := newState().USKChroma(0, 0)
	if r.ChromaForeground != 0.0 || r.ChromaSaturation != 1.0 || r.ChromaSample ||
		r.ChromaSamplePosition != (ChromaSamplePositionView{X: 0.5, Y: 0.5}) {
		t.Errorf("usk_chroma default: %+v", r)
	}

	kack := KeyPropertiesAdvancedChromaField{Saturation: 1000} // _KACk defaults

	// test_upstream_keyer_migration.py::test_usk_chroma_reads_kack_5_fields_with_thousandths
	s = newState()
	k := kack
	k.Foreground, k.Background, k.KeyEdge, k.SpillSuppress, k.FlareSuppress = 91, 500, 850, 200, 100
	usk(&s, 0, 0).AdvancedChroma = &k
	if r := s.USKChroma(0, 0); !near(r.ChromaForeground, 0.091) || !near(r.ChromaBackground, 0.5) || !near(r.ChromaKeyEdge, 0.85) {
		t.Errorf("usk_chroma key: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_chroma_color_correction_6_fields
	s = newState()
	k = kack
	k.Brightness, k.Contrast, k.Saturation, k.Red, k.Green, k.Blue = 500, 500, 600, 100, 200, 300
	usk(&s, 0, 0).AdvancedChroma = &k
	if r := s.USKChroma(0, 0); !near(r.ChromaBrightness, 0.5) || !near(r.ChromaSaturation, 0.6) ||
		!near(r.ChromaRed, 0.1) || !near(r.ChromaGreen, 0.2) || !near(r.ChromaBlue, 0.3) {
		t.Errorf("usk_chroma correction: %+v", r)
	}

	kacc := KeyPropertiesAdvancedChromaColorpickerField{Size: 1000} // _KACC defaults

	// test_upstream_keyer_migration.py::test_usk_chroma_cacc_sample_position_round_trip
	s = newState()
	c := kacc
	c.Cursor, c.X, c.Y, c.Size = true, 0, 0, 5000
	usk(&s, 0, 0).ChromaPicker = &c
	if r := s.USKChroma(0, 0); !r.ChromaSample || !near(r.ChromaSamplePosition.X, 0.5) || !near(r.ChromaSampleSize, 0.5) {
		t.Errorf("usk_chroma sample: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_chroma_cacc_sample_position_clamps_above_one
	s = newState()
	c = kacc
	c.X = 20000
	usk(&s, 0, 0).ChromaPicker = &c
	if r := s.USKChroma(0, 0); !near(r.ChromaSamplePosition.X, 1.0) {
		t.Errorf("usk_chroma clamp: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_dve_default_when_missing
	if r := newState().USKDVE(0, 0); r.DVEPositionX != 0.0 || r.DVESizeX != 1.0 || r.DVEBorderOpacity != 100.0 || r.DVERate != 25.0 {
		t.Errorf("usk_dve default: %+v", r)
	}

	kedv := KeyPropertiesDVEField{SizeX: 1000, SizeY: 1000, BorderOpacity: 100, Rate: 25} // _KeDV defaults

	// test_upstream_keyer_migration.py::test_usk_dve_reads_position_size_rotation_with_scale
	s = newState()
	dv := kedv
	dv.PosX, dv.PosY, dv.SizeX, dv.SizeY, dv.Rotation = 8000, -4500, 1500, 750, 450
	usk(&s, 0, 0).DVE = &dv
	if r := s.USKDVE(0, 0); !near(r.DVEPositionX, 8.0) || !near(r.DVEPositionY, -4.5) || !near(r.DVESizeX, 1.5) ||
		!near(r.DVESizeY, 0.75) || !near(r.DVERotation, 45.0) {
		t.Errorf("usk_dve geometry: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_dve_border_scale_contract_round_trip
	s = newState()
	dv = kedv
	dv.BorderEnabled, dv.BorderHue, dv.BorderSaturation, dv.BorderLuma = true, 180.0, 0.6, 0.75
	dv.BorderOpacity, dv.BorderOuterWidth, dv.BorderInnerWidth = 80, 850, 200
	usk(&s, 0, 0).DVE = &dv
	if r := s.USKDVE(0, 0); !near(r.DVEBorderHue, 180.0) || !near(r.DVEBorderSaturation, 60.0) ||
		!near(r.DVEBorderLuma, 75.0) || !near(r.DVEBorderOuterWidth, 8.5) {
		t.Errorf("usk_dve border: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_dve_mask_fields_scale_thousandths
	s = newState()
	dv = kedv
	dv.MaskEnabled, dv.MaskTop, dv.MaskBottom, dv.MaskLeft, dv.MaskRight = true, 5000, -5000, 8000, -16000
	usk(&s, 0, 0).DVE = &dv
	if r := s.USKDVE(0, 0); !r.DVEMasked || !near(r.DVETop, 5.0) || !near(r.DVEBottom, -5.0) ||
		!near(r.DVELeft, 8.0) || !near(r.DVERight, -16.0) {
		t.Errorf("usk_dve mask: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_dve_light_direction_wire_divided_by_10
	s = newState()
	dv = kedv
	dv.LightAngle, dv.LightAltitude = 900, 45
	usk(&s, 0, 0).DVE = &dv
	if r := s.USKDVE(0, 0); !near(r.DVELightDirection, 90.0) || !near(r.DVELightAltitude, 45.0) {
		t.Errorf("usk_dve light: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_pattern_default_when_missing
	if r := newState().USKPattern(0, 0); r.PatternSize != 50.0 || r.PatternSymmetry != 50.0 || r.PatternSoftness != 0.0 ||
		r.PatternPositionX != 0.5 || r.PatternPositionY != 0.5 {
		t.Errorf("usk_pattern default: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_pattern_round_trip_with_thousandths_scale
	s = newState()
	usk(&s, 0, 0).Pattern = &KeyPropertiesPatternField{
		Pattern: 5, Size: 7500, Symmetry: 4000, Softness: 2500, Invert: true, PositionX: 8000, PositionY: 2000,
	}
	if r := s.USKPattern(0, 0); r.PatternStyle != 5 || !near(r.PatternSize, 75.0) || !near(r.PatternSymmetry, 40.0) ||
		!near(r.PatternSoftness, 25.0) || !r.PatternInvert || !near(r.PatternPositionX, 0.8) || !near(r.PatternPositionY, 0.2) {
		t.Errorf("usk_pattern: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_mask_default_when_missing
	if r := newState().USKMask(0, 0); r != (USKMaskView{}) {
		t.Errorf("usk_mask default: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_mask_reads_from_base_with_thousandths_scale
	s = newState()
	usk(&s, 0, 0).Base = &KeyPropertiesBaseField{MaskEnabled: true, MaskTop: 9000, MaskBottom: -9000, MaskLeft: 16000, MaskRight: -16000}
	if r := s.USKMask(0, 0); !r.MaskEnabled || !near(r.MaskTop, 9.0) || !near(r.MaskBottom, -9.0) ||
		!near(r.MaskLeft, 16.0) || !near(r.MaskRight, -16.0) {
		t.Errorf("usk_mask: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_fly_default_when_missing
	if r := newState().USKFly(0, 0); r.FlyEnabled || r.FlyKeyframeAStored || r.FlyIsAtKeyframeA {
		t.Errorf("usk_fly default: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_fly_reads_enable_from_base
	s = newState()
	usk(&s, 0, 0).Base = &KeyPropertiesBaseField{FlyEnabled: true}
	usk(&s, 0, 0).Fly = &KeyPropertiesFlyField{IsASet: true, AtKeyframeB: true}
	if r := s.USKFly(0, 0); !r.FlyEnabled || !r.FlyKeyframeAStored || !r.FlyIsAtKeyframeB || r.FlyIsAtKeyframeA {
		t.Errorf("usk_fly: %+v", r)
	}

	// test_upstream_keyer_migration.py::test_usk_fly_enabled_without_fly_state
	s = newState()
	usk(&s, 0, 0).Base = &KeyPropertiesBaseField{FlyEnabled: true}
	if r := s.USKFly(0, 0); !r.FlyEnabled || r.FlyKeyframeAStored {
		t.Errorf("usk_fly without KeFS: %+v", r)
	}
}

func TestReadFairlight(t *testing.T) {
	// test_fairlight_migration.py::test_fairlight_master_default_when_missing
	if r := newState().FairlightMaster(); r.Present || r.VolumeDB == nil || *r.VolumeDB != 0.0 || r.EQEnable ||
		r.EQGainDB != 0.0 || r.DynamicsMakeupDB != 0.0 || r.AFV {
		t.Errorf("fairlight_master default: %+v", r)
	}

	// test_fairlight_migration.py::test_fairlight_master_reads_wire_db
	s := newState()
	s.Fairlight.Master = &FairlightMasterPropertiesField{Volume: -1234, EQGain: 500, DynamicsGain: 600, EQEnable: true, AFV: true}
	if r := s.FairlightMaster(); !r.Present || r.VolumeDB == nil || *r.VolumeDB != -12.34 || r.EQGainDB != 5.0 ||
		r.DynamicsMakeupDB != 6.0 || !r.EQEnable || !r.AFV {
		t.Errorf("fairlight_master: %+v", r)
	}

	// test_fairlight_migration.py::test_fairlight_strips_empty_when_missing
	if r := newState().FairlightStrips(); r == nil || len(r) != 0 {
		t.Errorf("fairlight_strips empty: %#v", r)
	}

	// test_fairlight_migration.py::test_fairlight_strips_orders_cameras_before_mp_before_mic
	s = newState()
	for id, vol := range map[string]int16{"1.0": -1000, "2001.0": -500, "1301.0": 0} {
		s.Fairlight.Strips[id] = &FairlightStrip{Properties: &FairlightStripPropertiesField{IsSplit: 0x01, Volume: vol, State: 2}}
	}
	s.Fairlight.Inputs[1] = &FairlightAudioInputField{Type: 0, Number: 1, Split: 0x01}
	s.Fairlight.Inputs[2001] = &FairlightAudioInputField{Type: 1, Number: 0, Split: 0x01}
	s.Fairlight.Inputs[1301] = &FairlightAudioInputField{Type: 2, Number: 0, Level: 1, Split: 0x01}
	s.Inputs[1] = &InputPropertiesField{Name: "Camera 1\x00", ShortName: "CAM1\x00"}
	strips := s.FairlightStrips()
	if len(strips) != 3 || strips[0].Source != 1 || strips[1].Source != 2001 || strips[2].Source != 1301 ||
		strips[0].DisplayName != "CAM1" || strips[1].DisplayName != "MP1" || strips[2].DisplayName != "Mic" {
		t.Errorf("fairlight_strips: %+v", strips)
	}

	// test_fairlight_migration.py::test_fairlight_audio_inputs_decodes_metadata
	s = newState()
	s.Fairlight.Inputs[1] = &FairlightAudioInputField{Type: 0, Level: 4, Split: 0xff}
	if r := s.FairlightAudioInputs(); !reflect.DeepEqual(r, map[int]FairlightAudioInputView{1: {Type: 0, Split: 0xff, Level: 4}}) {
		t.Errorf("fairlight_audio_inputs: %+v", r)
	}

	// test_fairlight_migration.py::test_fairlight_eq_bands_sorts_by_index_and_handles_master_id_none
	band := AtemMasterEQBandPropertiesField{BandEnabled: true, BandFilter: 0x01, BandFreqRange: 2, BandFrequency: 200, BandGain: 500, BandQ: 70, BandPossibleFilters: 0x3f}
	s = newState()
	b0, b1 := band, band
	b0.BandIndex, b1.BandIndex = 2, 0
	s.Fairlight.MasterEQBands[0], s.Fairlight.MasterEQBands[1] = &b0, &b1
	if r := s.FairlightEQBands(nil); len(r) != 2 || r[0].Index != 0 || r[1].Index != 2 {
		t.Errorf("fairlight_eq_bands: %+v", r)
	}

	// _Dyn's defaults, keyed by 0 as in the Python mixerstate: the readers
	// key their result by the strip id the packet spells, not the key.
	dyn := func(index uint16, mode uint8, threshold int32) *FairlightStrip {
		return &FairlightStrip{
			Compressor: &FairlightCompressorPropertiesField{Index: index, IsSplit: 0x01, Enabled: true, Threshold: threshold, Attack: 100, Hold: 400, Release: 1500, Ratio: 200},
			Limiter:    &FairlightLimiterPropertiesField{Index: index, IsSplit: 0x01, Enabled: true, Threshold: threshold, Attack: 100, Hold: 400, Release: 1500},
			Expander:   &FairlightExpanderPropertiesField{Index: index, IsSplit: 0x01, Enabled: true, Threshold: threshold, Attack: 100, Hold: 400, Release: 1500, Ratio: 200, Range: 3000, Mode: mode},
		}
	}

	// test_fairlight_migration.py::test_fairlight_compressor_keys_by_strip_id
	s = newState()
	s.Fairlight.Strips["0"] = dyn(1, 0, -1500)
	if r, ok := s.FairlightCompressor()["1.0"]; !ok || r.ThresholdDB != -15.0 || r.Ratio != 2.0 {
		t.Errorf("fairlight_compressor: %+v", s.FairlightCompressor())
	}

	// test_fairlight_migration.py::test_fairlight_limiter_does_not_include_ratio
	s = newState()
	s.Fairlight.Strips["0"] = dyn(2, 0, -2000)
	if b, err := json.Marshal(s.FairlightLimiter()["2.0"]); err != nil || jsonHasKey(t, b, "ratio") {
		t.Errorf("fairlight_limiter: %s", b)
	}

	// test_fairlight_migration.py::test_fairlight_expander_mode_byte_maps_to_gate_or_expander
	s = newState()
	s.Fairlight.Strips["0"] = dyn(3, 0, -2000)
	g := newState()
	g.Fairlight.Strips["0"] = dyn(4, 1, -2000)
	if s.FairlightExpander()["3.0"].Mode != "expander" || g.FairlightExpander()["4.0"].Mode != "gate" {
		t.Error("fairlight_expander mode")
	}

	// test_fairlight_migration.py::test_fairlight_headphones_default_when_missing
	if r := newState().FairlightHeadphones(); r != (FairlightHeadphonesView{Unmuted: true}) {
		t.Errorf("fairlight_headphones default: %+v", r)
	}

	// test_fairlight_migration.py::test_fairlight_headphones_reads_wire_db
	s = newState()
	s.Fairlight.Headphones = &FairlightHeadphonesField{Volume: -1500, Unmuted: false}
	if r := s.FairlightHeadphones(); r != (FairlightHeadphonesView{Present: true, VolumeDB: -15.0}) {
		t.Errorf("fairlight_headphones: %+v", r)
	}

	solo := func(f *FairlightSoloField) (bool, any) {
		s := newState()
		s.Fairlight.Solo = f
		r := s.FairlightSolo()
		if r.StripID == nil {
			return r.AnySoloed, nil
		}
		return r.AnySoloed, *r.StripID
	}

	// test_fairlight_migration.py::test_fairlight_solo_default_when_missing
	if on, id := solo(nil); on || id != nil {
		t.Error("fairlight_solo default")
	}

	// test_fairlight_migration.py::test_fairlight_solo_reads_real_fams_shape
	if on, id := solo(&FairlightSoloField{Solo: true, Channel: 1, IsSplitLR: 0xff, Subchannel: 1}); !on || id != "1.1" {
		t.Errorf("fairlight_solo split: %v %v", on, id)
	}

	// test_fairlight_migration.py::test_fairlight_solo_non_split_strip
	if on, id := solo(&FairlightSoloField{Solo: true, Channel: 3, IsSplitLR: 0x01}); !on || id != "3.0" {
		t.Errorf("fairlight_solo: %v %v", on, id)
	}

	// test_fairlight_migration.py::test_fairlight_solo_not_soloed_returns_empty
	if on, id := solo(&FairlightSoloField{Solo: false, Channel: 5, IsSplitLR: 0xff, Subchannel: 1}); on || id != nil {
		t.Errorf("fairlight_solo off: %v %v", on, id)
	}

	// test_fairlight_migration.py::test_mocp_decodes_master_compressor, the
	// reader half (TestFixtureMOCP has the parser half).
	s, _ = storeOne(t, "MOCP", fixtures.Bytes(t, "audio/MOCP-master-compressor.bin"))
	if r := s.FairlightMasterCompressor(); r == nil ||
		*r != (CompressorView{Enabled: true, ThresholdDB: -15.0, Ratio: 3.97, AttackMS: 1.4, HoldMS: 0.0, ReleaseMS: 93.0}) {
		t.Errorf("fairlight_master_compressor: %+v", r)
	}
	if newState().FairlightMasterCompressor() != nil {
		t.Error("fairlight_master_compressor missing")
	}

	// test_fairlight_migration.py::test_amlp_decodes_master_limiter, the
	// reader half.
	s, _ = storeOne(t, "AMLP", fixtures.Bytes(t, "audio/AMLP-master-limiter.bin"))
	if r := s.FairlightMasterLimiter(); r == nil ||
		*r != (LimiterView{Enabled: true, ThresholdDB: -5.01, AttackMS: 0.71, HoldMS: 0.0, ReleaseMS: 93.0}) {
		t.Errorf("fairlight_master_limiter: %+v", r)
	}
	if newState().FairlightMasterLimiter() != nil {
		t.Error("fairlight_master_limiter missing")
	}
}

func jsonHasKey(t *testing.T, b []byte, key string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	_, ok := m[key]
	return ok
}

// The helpers that are part of the API: the counts build_full_state sizes
// its lists with. bmdwire's tests exercise them only through the profile
// and the snapshot builder (later phases); these pin atemwire's fallbacks.
func TestReadCounts(t *testing.T) {
	s := newState()
	if s.MECount() != 1 || s.MEKeyerCount(0) != 4 || s.DSKCount() != 1 {
		t.Error("handshake-window fallbacks")
	}
	at(&s.MEs, 2).Config = &MixerEffectConfigField{Index: 2, Keyers: 2}
	*at(&s.DSKs, 1) = DSK{State: &DkeyStateField{Index: 1}}
	if s.MECount() != 3 || s.MEKeyerCount(2) != 2 || s.MEKeyerCount(0) != 4 || s.DSKCount() != 2 {
		t.Error("counts from _MeC and dkey-state")
	}
	s.Topology = &TopologyField{MEUnits: 4, DownstreamKeyers: 4}
	if s.MECount() != 4 || s.DSKCount() != 4 {
		t.Error("counts from _top")
	}
}

// pyRound matches Python's round(x, n), including the decimal ties that
// scaling first gets wrong: 0.00025 is just above the tie in binary, so
// Python rounds it up; 2.675 is just below, so down.
func TestPyRound(t *testing.T) {
	for _, c := range []struct {
		x      float64
		places int
		want   float64
	}{
		{0.00025, 4, 0.0003}, {2.675, 2, 2.67}, {0.125, 2, 0.12}, {0.375, 2, 0.38},
		{-0.0001, 3, math.Copysign(0, -1)}, {1e300, 2, 1e300}, {12.3456, 0, 12},
	} {
		if got := pyRound(c.x, c.places); math.Float64bits(got) != math.Float64bits(c.want) {
			t.Errorf("pyRound(%v, %d) = %v, want %v", c.x, c.places, got, c.want)
		}
	}
	if !math.IsNaN(pyRound(math.NaN(), 2)) || !math.IsInf(pyRound(math.Inf(-1), 2), -1) {
		t.Error("pyRound non-finite")
	}
}
