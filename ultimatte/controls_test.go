// SPDX-License-Identifier: MIT

package ultimatte

// The control table against the protocol document, and the value checks:
// bmdwire v1.14.0's test_controls.py, one for one.

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func lookup(t *testing.T, name string) Control {
	t.Helper()
	c, ok := Lookup(name)
	if !ok {
		t.Fatalf("no %q", name)
	}
	return c
}

// test_the_table_has_every_control_in_the_document
func TestTheTableHasEveryControlInTheDocument(t *testing.T) {
	names := map[string]bool{}
	kinds := map[Kind]int{}
	for _, c := range Controls {
		names[strings.ToLower(c.Name)] = true
		kinds[c.Kind]++
	}
	if len(Controls) != 289 || len(names) != 289 {
		t.Fatalf("%d rows, %d names", len(Controls), len(names))
	}
	want := map[Kind]int{KindRange: 176, KindBool: 79, KindReadOnly: 1, KindChoice: 13, KindFunction: 20}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatal(kinds)
	}
}

// test_documented_ranges
func TestDocumentedRanges(t *testing.T) {
	for _, c := range []struct {
		name   string
		lo, hi int
	}{
		{"Matte Density", 0, 10000}, {"Matte Correct Horizontal Size", 0, 6},
		{"Matte Correct Vertical Size", 0, 3}, {"Veil Correct Vertical Size", 0, 6},
		{"Transition Rate", 1, 120}, {"BM Process Horizontal", 0, 3},
		{"FG Input Frame Delay", 0, 14}, {"Output Offset", -1500, 1500},
		{"GP 5 Input Delay", 1, 120},
	} {
		got := lookup(t, c.name)
		if got.Kind != KindRange || *got.Min != c.lo || *got.Max != c.hi {
			t.Errorf("%s: %s %d-%d", c.name, got.Kind, *got.Min, *got.Max)
		}
	}
}

// test_footnote_limits
func TestFootnoteLimits(t *testing.T) {
	if lookup(t, "GP Out Delay").Limit != NotMini || lookup(t, "Talent Mirror").Limit != NotHD ||
		lookup(t, "Talent Highlight Level").Limit != NotHD || lookup(t, "GM Freeze").Limit != NotMini {
		t.Error("limits")
	}
	if p := lookup(t, "Output Range").MinProtocol; p == nil || *p != [2]int{2, 0} {
		t.Error("Output Range", p)
	}
	if lookup(t, "Tally Active").Kind != KindReadOnly {
		t.Error("Tally Active")
	}
	if lookup(t, "GP Out").AvailableOn(ModelHDMini) || !lookup(t, "GP Out").AvailableOn(ModelHD) {
		t.Error("GP Out")
	}
	ly := lookup(t, "LY Freeze")
	if ly.AvailableOn(ModelHD) || !ly.AvailableOn(Model4K) || !ly.AvailableOn(ModelUnknown) {
		t.Error("LY Freeze")
	}
}

// test_lookup_ignores_case_and_spacing
func TestLookupIgnoresCaseAndSpacing(t *testing.T) {
	if lookup(t, "matte  density").Name != "Matte Density" || lookup(t, "3g sdi LEVEL").Name != "3G SDI level" {
		t.Error("lookup")
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("nope")
	}
}

// test_model_family
func TestModelFamily(t *testing.T) {
	for model, family := range map[string]string{
		"Ultimatte 12 8K": Model8K, "Ultimatte 12 4K": Model4K, "Ultimatte 12 HD": ModelHD,
		"Ultimatte 12 HD Mini": ModelHDMini, "ultimatte 12  hd mini": ModelHDMini,
		"Ultimatte 12": Model12, "Ultimatte 11": "", "": "", "Ultimatte 12 16K": "",
	} {
		if got := ModelFamily(model); got != family {
			t.Errorf("%q: %q", model, got)
		}
	}
}

// test_model_inputs_and_blocks
func TestModelInputsAndBlocks(t *testing.T) {
	if !reflect.DeepEqual(ModelInputs(ModelHDMini), []string{"FG In", "BG In"}) ||
		len(ModelInputs(ModelHD)) != 6 || len(ModelInputs(Model8K)) != 9 {
		t.Error("inputs")
	}
	if HasGPI(ModelHDMini) || !HasGPI(ModelHD) || !HasCameraControl(ModelHDMini) || HasCameraControl(Model8K) {
		t.Error("blocks")
	}
}

// test_parse_value
func TestParseValue(t *testing.T) {
	for _, c := range []struct {
		name, text string
		value      any
	}{
		{"Matte Density", "273", 273}, {"Output Offset", "-1500", -1500},
		{"Output Offset", "+1500", 1500}, {"Matte Enable", "On", true},
		{"Matte Enable", "off", false}, {"Tally Active", "on", true},
		{"Factory Defaults", "Yes", true}, {"Backing Color", "blue", "blue"},
		{"Matte Density", "lots", "lots"}, {"Matte Enable", "maybe", "maybe"},
		{"Active Unit", "on", "on"}, // a 12 4K control the document lacks
	} {
		if got := ParseValue(c.name, c.text); got != c.value {
			t.Errorf("%s %q: %#v", c.name, c.text, got)
		}
	}
}

// test_frame_size
func TestFrameSize(t *testing.T) {
	for f, size := range map[string][2]int{
		"1080p25": {1920, 1080}, "2160p59.94": {3840, 2160},
		"525i59.94 NTSC 4:3": {720, 486}, "625i50 PAL 4:3": {720, 576},
		"720p50": {1280, 720}, "4320p24": {7680, 4320},
		"auto detect": {}, "": {},
	} {
		w, h, ok := FrameSize(f)
		if [2]int{w, h} != size || ok != (size != [2]int{}) {
			t.Errorf("%q: %d %d %v", f, w, h, ok)
		}
	}
}

func format(t *testing.T, name string, value any, ctx Context) string {
	t.Helper()
	s, err := FormatValue(lookup(t, name), value, ctx)
	if err != nil {
		t.Fatalf("%s %v: %v", name, value, err)
	}
	return s
}

// test_format_value_kinds
func TestFormatValueKinds(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
		want  string
	}{
		{"Matte Density", 10000, "10000"}, {"Matte Density", "42", "42"},
		{"Matte Enable", true, "on"}, {"Matte Enable", "OFF", "off"},
		{"Backing Color", "Blue", "blue"}, {"Monitor Out", "combined matte", "combined matte"},
		{"Layer Order", "fg/layer/bg layer/bg", "fg/layer/bg layer/bg"},
		{"Factory Defaults", true, "yes"}, {"Filter Median", 3, "3"},
	} {
		if got := format(t, c.name, c.value, Context{}); got != c.want {
			t.Errorf("%s %v: %q", c.name, c.value, got)
		}
	}
}

func wantKind(t *testing.T, err, kind error) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Errorf("%v is not %v", err, kind)
	}
}

// test_format_value_refuses
func TestFormatValueRefuses(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
	}{
		{"Matte Density", 10001}, {"Matte Density", -1}, {"Matte Density", "x"},
		{"Matte Density", true}, {"Transition Rate", 0}, {"Output Offset", 1501},
		{"Matte Enable", "maybe"}, {"Backing Color", "purple"},
		{"Filter Median", 5}, {"Tally Active", true}, {"Factory Defaults", "no"},
	} {
		_, err := FormatValue(lookup(t, c.name), c.value, Context{})
		wantKind(t, err, ErrControlValue)
		wantKind(t, err, ErrValue)
	}
}

// test_model_and_protocol_refusals
func TestModelAndProtocolRefusals(t *testing.T) {
	_, err := FormatValue(lookup(t, "GP Out Delay"), 10, Context{Family: ModelHDMini})
	wantKind(t, err, ErrUnsupported)
	_, err = FormatValue(lookup(t, "Talent Mirror"), true, Context{Family: ModelHD})
	wantKind(t, err, ErrUnsupported)
	_, err = FormatValue(lookup(t, "Output Range"), "full", Context{Protocol: &[2]int{1, 2}})
	wantKind(t, err, ErrUnsupported)
	if got := format(t, "Output Range", "Full", Context{Protocol: &[2]int{2, 0}}); got != "full" {
		t.Error(got)
	}
}

// test_window_position_follows_the_running_format
func TestWindowPositionFollowsTheRunningFormat(t *testing.T) {
	ctx := Context{VideoFormat: "1080p25"}
	if format(t, "Window Position Top", 1080, ctx) != "1080" || format(t, "Window Position Left", 1920, ctx) != "1920" {
		t.Error("in range")
	}
	_, err := FormatValue(lookup(t, "Window Position Top"), 1081, ctx)
	wantKind(t, err, ErrControlValue)
	// format unknown: only the lower bound
	if format(t, "Window Position Top", 5000, Context{}) != "5000" {
		t.Error("unknown format")
	}
	_, err = FormatValue(lookup(t, "Window Position Top"), -1, Context{})
	wantKind(t, err, ErrControlValue)
}

// test_video_format_by_model_and_by_the_units_own_list
func TestVideoFormatByModelAndByTheUnitsOwnList(t *testing.T) {
	vf := lookup(t, "Video Format")
	if format(t, "Video Format", "4320p25", Context{Family: Model8K}) != "4320p25" {
		t.Error("8K")
	}
	for _, c := range []struct {
		v   string
		ctx Context
	}{{"4320p25", Context{Family: Model4K}}, {"1080PsF25", Context{Family: ModelHDMini}}} {
		_, err := FormatValue(vf, c.v, c.ctx)
		wantKind(t, err, ErrControlValue)
	}
	if format(t, "Video Format", "1080psf25", Context{Family: ModelHD}) != "1080psf25" {
		t.Error("HD")
	}
	own := Context{VideoFormats: []string{"auto detect", "1080p25"}}
	if format(t, "Video Format", "1080P25", own) != "1080p25" {
		t.Error("own")
	}
	_, err := FormatValue(vf, "1080p50", own)
	wantKind(t, err, ErrControlValue)
}

// test_offsets
func TestOffsets(t *testing.T) {
	md := lookup(t, "Matte Density")
	if s, _ := FormatOffset(md, -10, Context{}); s != "-10" {
		t.Error(s)
	}
	if s, _ := FormatOffset(md, "+5", Context{}); s != "5" {
		t.Error(s)
	}
	_, err := FormatOffset(lookup(t, "Matte Enable"), 1, Context{})
	wantKind(t, err, ErrControlValue)
	_, err = FormatOffset(md, "x", Context{})
	wantKind(t, err, ErrControlValue)
	_, err = FormatOffset(lookup(t, "GP Out Delay"), 1, Context{Family: ModelHDMini})
	wantKind(t, err, ErrUnsupported)
}
