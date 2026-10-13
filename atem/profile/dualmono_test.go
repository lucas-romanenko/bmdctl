// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// Dual-mono Fairlight inputs in profile save and apply, ported from
// bmdwire#23's test_profile_dual_mono.py (d86ed31) and pinned to its
// fixture, profile/profile_fairlight_dual_mono_asc.xml: the
// <FairlightAudioMixer> of a Save Switcher State ATEM Software Control
// 9.3.2 wrote for a 1 M/E Constellation HD (firmware 9.2.1) on 2026-10-13,
// otherwise unedited. Input 1 is in Dual Mono with its halves set apart
// (left: fader -10 dB, pan -50, EQ band 0 +3 dB; right: -20 dB, +50,
// -3 dB), input 2 is stereo, 1301 is the XLR.

// capturedIDs is CAPTURED_IDS: (AudioSource id, setter channel).
var capturedIDs = []struct{ id, channel int }{{-65280, -1}, {-256, 0}, {-255, 1}}

func dualMonoFixture(t *testing.T) *Element {
	t.Helper()
	return mustProfile(t, string(fixtures.Bytes(t, "profile/profile_fairlight_dual_mono_asc.xml"))).Root()
}

// audioInput is root.find(".//AudioInput[@id='src']").
func audioInput(t *testing.T, root *Element, src int) *Element {
	t.Helper()
	for _, ai := range root.FindAll("FairlightAudioMixer/AudioInputs/AudioInput") {
		if id, _ := ai.Get("id"); id == strconv.Itoa(src) {
			return ai
		}
	}
	t.Fatalf("no AudioInput %d", src)
	return nil
}

// canon is _canon: the element as a string, whitespace between tags
// dropped.
func canon(e *Element) string {
	var b strings.Builder
	var walk func(*Element)
	walk = func(e *Element) {
		b.WriteString("<" + e.Tag)
		for _, a := range e.Attrs {
			fmt.Fprintf(&b, " %s=%q", a.Name, a.Value)
		}
		b.WriteString(">")
		for _, c := range e.Children {
			walk(c)
		}
		b.WriteString("</" + e.Tag + ">")
	}
	walk(e)
	return b.String()
}

func attrOf(e *Element, name string) string { v, _ := e.Get(name); return v }

// hundredths is _hundredths.
func hundredths(t *testing.T, s string) int {
	t.Helper()
	if s == "-inf" {
		return -10000
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return int(math.RoundToEven(v * 100))
}

// dualMonoState is _mixerstate_from: the FAIP, FASP and AEBP records a
// switcher in the fixture's state would report for the inputs in faipSplit
// (source to FAIP byte 10).
func dualMonoState(t *testing.T, root *Element, faipSplit map[int]uint8) atem.State {
	t.Helper()
	shape, freqRange, mix := inverse(eqShapeNames), inverse(eqFreqRangeNames), inverse(mixOptionNames)
	st := atem.State{Fairlight: atem.Fairlight{
		Strips: map[string]*atem.FairlightStrip{},
		Inputs: map[uint16]*atem.FairlightAudioInputField{},
	}}
	for src, split := range faipSplit {
		st.Fairlight.Inputs[uint16(src)] = &atem.FairlightAudioInputField{Index: uint16(src), Split: split}
		for sub, s := range audioInput(t, root, src).FindAll("AudioSource") {
			isSplit := audioSourceChannel(s.attr("id")) != -1
			id, splitByte := fmt.Sprintf("%d.0", src), uint8(0x01)
			if isSplit {
				id, splitByte = fmt.Sprintf("%d.%d", src, sub), 0xFF
			}
			eq := s.Find("Equalizer")
			delay, _ := strconv.Atoi(s.GetDefault("delayFrames", "0"))
			strip := &atem.FairlightStrip{
				Properties: &atem.FairlightStripPropertiesField{
					Index: uint16(src), IsSplit: splitByte, Subchannel: uint8(sub), Delay: uint8(delay),
					Volume: int16(hundredths(t, attrOf(s, "faderGain"))), Gain: int16(hundredths(t, attrOf(s, "inputGain"))),
					Pan: int16(hundredths(t, attrOf(s, "pan"))), State: uint8(mix[attrOf(s, "mixOption")]),
					EQEnable: attrOf(eq, "enabled") == "True", EQGain: int16(hundredths(t, attrOf(eq, "gain"))),
					DynamicsGain: uint16(hundredths(t, attrOf(s.Find("DynamicsProcessor"), "makeupGain"))), StripID: id,
				},
				EQBands: map[uint8]*atem.AtemEQBandPropertiesField{},
			}
			for _, b := range eq.FindAll("EqualizerBand") {
				i, _ := strconv.Atoi(attrOf(b, "index"))
				hz, _ := strconv.Atoi(attrOf(b, "frequency"))
				strip.EQBands[uint8(i)] = &atem.AtemEQBandPropertiesField{
					Index: uint16(src), IsSplit: splitByte, Subchannel: uint8(sub), BandIndex: uint8(i),
					BandEnabled: attrOf(b, "enabled") == "True", BandFilter: uint8(shape[attrOf(b, "shape")]),
					BandFreqRange: uint8(freqRange[attrOf(b, "frequencyRange")]), BandFrequency: uint16(hz),
					BandGain: int32(hundredths(t, attrOf(b, "gain"))), BandQ: uint16(hundredths(t, attrOf(b, "qFactor"))),
					StripID: id,
				}
			}
			st.Fairlight.Strips[id] = strip
		}
	}
	return st
}

// saveFairlight is _build_fairlight on a new <Profile>.
func saveFairlight(st atem.State) *Element {
	root := NewElement("Profile")
	newSaver(context.Background(), &fakeSource{st: st}, nil).fairlight(root)
	return root
}

// --- What ASC wrote ------------------------------------------------------------

func TestASCWritesADualMonoInputAsTwoSourcesLeftFirst(t *testing.T) {
	root := dualMonoFixture(t)
	ai := audioInput(t, root, 1)
	if c := attrOf(ai, "configuration"); c != "DualMono" {
		t.Fatalf("configuration %q", c)
	}
	srcs := ai.FindAll("AudioSource")
	if len(srcs) != 2 {
		t.Fatalf("%d sources", len(srcs))
	}
	for i, want := range [][4]string{{"-256", "-10", "-50", "3"}, {"-255", "-20", "50", "-3"}} {
		s := srcs[i]
		band := s.FindAll("Equalizer/EqualizerBand")[0]
		if got := [4]string{attrOf(s, "id"), attrOf(s, "faderGain"), attrOf(s, "pan"), attrOf(band, "gain")}; got != want {
			t.Errorf("source %d: %v, want %v", i, got, want)
		}
	}
	for src, want := range map[int][2]string{2: {"Stereo", "-65280"}, 1301: {"Mono", "-256"}} {
		ai := audioInput(t, root, src)
		srcs := ai.FindAll("AudioSource")
		if attrOf(ai, "configuration") != want[0] || len(srcs) != 1 || attrOf(srcs[0], "id") != want[1] {
			t.Errorf("%d: %s", src, canon(ai)[:80])
		}
	}
}

// --- The id <-> channel pair ---------------------------------------------------

func TestAudioSourceIDIsTheStripAddress(t *testing.T) {
	for _, c := range capturedIDs {
		if got := audioSourceID(c.channel); got != c.id {
			t.Errorf("audioSourceID(%d) = %d, want %d", c.channel, got, c.id)
		}
		// The signed form, and the unsigned one macro ops carry as sourceId.
		for _, s := range []string{strconv.Itoa(c.id), strconv.FormatUint(uint64(int64(c.id)), 10)} {
			if got := audioSourceChannel(&s); got != c.channel {
				t.Errorf("audioSourceChannel(%s) = %d, want %d", s, got, c.channel)
			}
		}
	}
	// int()'s spellings, and values past 64 bits, which it masks: what
	// bmdwire's audio_source_channel gives for each.
	for s, want := range map[string]int{" -256 ": 0, "-2_55": 1, "+18446744073709551360": 0, "-\u0662\u0665\u0665": 1,
		"36893488147419168513": 1, "36893488147419103487": -1, "-18446744073709551872": 0, "18446744073709551615": -1} {
		if got := audioSourceChannel(&s); got != want {
			t.Errorf("audioSourceChannel(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestAnUnknownIDIsTakenAsStereo(t *testing.T) {
	if got := audioSourceChannel(nil); got != -1 {
		t.Errorf("nil: %d", got)
	}
	for _, s := range []string{"", "x", "0", "-1", "-768", "-256.0", "-25_6_"} {
		if got := audioSourceChannel(&s); got != -1 {
			t.Errorf("%q: %d", s, got)
		}
	}
}

// --- Apply ---------------------------------------------------------------------

// stripAddresses is (source, channel) of every Fairlight strip command sent.
func stripAddresses(tg *recTarget) map[uint16][]int {
	out := map[uint16][]int{}
	for _, c := range tg.cmds {
		switch c := c.(type) {
		case atem.FairlightStripProperties:
			out[c.Source] = append(out[c.Source], c.Channel)
		case atem.FairlightEQBandProperties:
			out[c.Source] = append(out[c.Source], c.Channel)
		case atem.FairlightCompressorProperties:
			out[c.Source] = append(out[c.Source], c.Channel)
		case atem.FairlightLimiterProperties:
			out[c.Source] = append(out[c.Source], c.Channel)
		case atem.FairlightExpanderProperties:
			out[c.Source] = append(out[c.Source], c.Channel)
		}
	}
	return out
}

func applyFixture(t *testing.T, root *Element) (*recTarget, *ApplyResult) {
	t.Helper()
	tg := newTarget(atem.State{})
	a := (&Profile{root: root}).newApplier(context.Background(), tg, nil)
	if err := a.fairlight(); err != nil {
		t.Fatal(err)
	}
	return tg, a.res
}

func TestApplySendsEachStripToTheAddressASCWrote(t *testing.T) {
	root := dualMonoFixture(t)
	tg, res := applyFixture(t, root)
	if len(res.Errors) != 0 {
		t.Fatalf("errors %v", res.Errors)
	}
	// CFSP payload bytes 8-15 are the address.
	var got, want []string
	for _, c := range ofType[atem.FairlightStripProperties](tg.cmds) {
		if c.Volume != nil {
			got = append(got, fmt.Sprintf("%d %d", c.Source, int64(binary.BigEndian.Uint64(payload(c)[8:16]))))
		}
	}
	for _, ai := range root.FindAll("FairlightAudioMixer/AudioInputs/AudioInput") {
		for _, s := range ai.FindAll("AudioSource") {
			want = append(want, attrOf(ai, "id")+" "+attrOf(s, "id"))
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("sent\n%v\nwant\n%v", got, want)
	}
}

func TestApplyGivesEachHalfItsOwnValues(t *testing.T) {
	tg, _ := applyFixture(t, dualMonoFixture(t))
	strips := map[int][2]int{}
	for _, c := range ofType[atem.FairlightStripProperties](tg.cmds) {
		if c.Source == 1 && c.Volume != nil {
			p := payload(c)
			strips[c.Channel] = [2]int{i32(p, 40), i16(p, 36)} // volume, balance
		}
	}
	if want := map[int][2]int{0: {-1000, -5000}, 1: {-2000, 5000}}; !maps.Equal(strips, want) {
		t.Errorf("strips %v, want %v", strips, want)
	}
	bands := map[int][2]int{}
	for _, c := range ofType[atem.FairlightEQBandProperties](tg.cmds) {
		if c.Source == 1 && c.Band == 0 {
			p := payload(c)
			bands[c.Channel] = [2]int{int(binary.BigEndian.Uint32(p[20:])), i32(p, 24)} // frequency, gain
		}
	}
	if want := map[int][2]int{0: {35, 300}, 1: {46, -300}}; !maps.Equal(bands, want) {
		t.Errorf("band 0 %v, want %v", bands, want)
	}
	var comp, lim, exp []int
	for _, c := range ofType[atem.FairlightCompressorProperties](tg.cmds) {
		if c.Source == 1 {
			comp = append(comp, c.Channel)
		}
	}
	for _, c := range ofType[atem.FairlightLimiterProperties](tg.cmds) {
		if c.Source == 1 {
			lim = append(lim, c.Channel)
		}
	}
	for _, c := range ofType[atem.FairlightExpanderProperties](tg.cmds) {
		if c.Source == 1 {
			exp = append(exp, c.Channel)
		}
	}
	for name, chs := range map[string][]int{"CICP": comp, "CILP": lim, "CIXP": exp} {
		if !slices.Equal(chs, []int{0, 1}) {
			t.Errorf("%s channels %v", name, chs)
		}
	}
	by := stripAddresses(tg)
	for src, want := range map[uint16]int{2: -1, 1301: 0} { // stereo as before; the XLR at ASC's -256
		chs := slices.Compact(slices.Sorted(slices.Values(by[src])))
		if !slices.Equal(chs, []int{want}) {
			t.Errorf("%d: channels %v, want %d", src, chs, want)
		}
	}
}

// An error names the half it was for.
func TestApplyErrorNamesTheHalf(t *testing.T) {
	_, res := applyFixture(t, mustProfile(t, `<Profile><FairlightAudioMixer><AudioInputs><AudioInput id="1">`+
		`<AudioSource id="-255" mixOption="Bogus"/></AudioInput></AudioInputs></FairlightAudioMixer></Profile>`).Root())
	if len(res.Errors) != 1 || !strings.HasPrefix(res.Errors[0], "fairlight.strip[1.1]: ") {
		t.Errorf("errors %v", res.Errors)
	}
}

// --- Save ----------------------------------------------------------------------

// test_save_writes_what_asc_wrote: FAIP byte 10 for a dual-mono input
// wasn't captured; save tells Dual Mono from the two split strips, so any
// value but 1 (Mono) must do.
func TestSaveWritesWhatASCWrote(t *testing.T) {
	fx := dualMonoFixture(t)
	for _, faip := range []uint8{2, 4} {
		saved := saveFairlight(dualMonoState(t, fx, map[int]uint8{1: faip, 2: 2, 1301: 1}))
		for _, src := range []int{1, 2, 1301} {
			if got, want := canon(audioInput(t, saved, src)), canon(audioInput(t, fx, src)); got != want {
				t.Errorf("faip %d, input %d:\n%s\nwant\n%s", faip, src, got, want)
			}
		}
	}
}

// The XLR's FASP split byte wasn't captured; either way it saves as ASC
// wrote it.
func TestSaveKeepsTheXLRAtMinus256IfItsStripReadsStereo(t *testing.T) {
	fx := dualMonoFixture(t)
	st := dualMonoState(t, fx, map[int]uint8{1301: 1})
	st.Fairlight.Strips["1301.0"].Properties.IsSplit = 0x01
	if got, want := canon(audioInput(t, saveFairlight(st), 1301)), canon(audioInput(t, fx, 1301)); got != want {
		t.Errorf("%s\nwant\n%s", got, want)
	}
}

func TestSaveThenApplyGivesEachHalfItsValuesBack(t *testing.T) {
	fx := dualMonoFixture(t)
	for _, faip := range []uint8{2, 4} {
		tg, _ := applyFixture(t, saveFairlight(dualMonoState(t, fx, map[int]uint8{1: faip, 2: 2, 1301: 1})))
		got := map[[2]int][2]int{}
		for _, c := range ofType[atem.FairlightStripProperties](tg.cmds) {
			if c.Volume != nil {
				p := payload(c)
				got[[2]int{int(c.Source), c.Channel}] = [2]int{i32(p, 40), i16(p, 36)}
			}
		}
		want := map[[2]int][2]int{{1, 0}: {-1000, -5000}, {1, 1}: {-2000, 5000}, {2, -1}: {-10000, 0}, {1301, 0}: {-10000, 0}}
		if !maps.Equal(got, want) {
			t.Errorf("faip %d: %v, want %v", faip, got, want)
		}
	}
}
