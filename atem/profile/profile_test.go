// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// bmdwire v1.3.0's profile tests, one for one: test_profile_granular.py,
// test_profile_apply_restore.py, test_profile_roundtrip.py,
// test_profile_video_mode.py, test_profile_xml_hardening.py,
// test_apply_macros.py, test_apply_options_macros_only.py and the profile
// half of test_wipe_pattern_names.py. A test with no counterpart says why
// where it would be. The stand-in states the Python tests build come from
// the oracle (internal/fixtures/oracle/profile.json), which recorded them.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// --- helpers ------------------------------------------------------------------

func mustProfile(t *testing.T, xml string) *Profile {
	t.Helper()
	p, err := FromXML([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// newTarget is a recording connection: _RecordingConn with a mixerstate.
func newTarget(st atem.State) *recTarget {
	return &recTarget{st: st, connected: true, fail: map[int]string{}}
}

func mustApply(t *testing.T, p *Profile, tg *recTarget, opts *ApplyOptions) *ApplyResult {
	t.Helper()
	res, err := runApply(p, tg, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ofType[T atem.Command](cmds []atem.Command) []T {
	var out []T
	for _, c := range cmds {
		if v, ok := c.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// allOffApplyOptions is _all_off_apply_options: every per-M/E flag and
// most sections off; HyperDecks keeps its default, as in the Python helper.
func allOffApplyOptions(meCount int) *ApplyOptions {
	o := DefaultApplyOptions()
	o.MEs = nil
	for range meCount {
		o.MEs = append(o.MEs, MixEffectOptionsNone())
	}
	o.RestoreDownstreamKeys, o.RestoreColorGenerators, o.RestoreAudio = false, false, false
	o.RestoreAux, o.RestoreVideoMode, o.RestoreInputs, o.RestoreMacros = false, false, false, false
	o.RestoreMediaPlayers, o.RestoreSettingsFlags, o.RestoreMediaPoolImages = false, false, false
	return o
}

// fullMEXML is _full_me_profile's document.
const fullMEXML = `<Profile majorVersion="2" minorVersion="1">
    <MixEffectBlocks>
        <MixEffectBlock index="0">
            <Program input="3010"/>
            <Preview input="1"/>
            <NextTransition selection="Background,Key1" nextSelection="Background,Key1"/>
            <TransitionStyle style="Mix" nextStyle="Mix" previewTransition="False" transitionPosition="0">
                <MixParameters rate="25"/>
                <DipParameters rate="25" input="2001"/>
                <WipeParameters rate="25" pattern="LeftToRightBar" symmetry="50" xPosition="0.5" yPosition="0.5" reverseDirection="False" flipFlip="False" borderInput="2001" borderWidth="0" borderSoftness="0"/>
                <StingerParameters source="MediaPlayer1" preMultipliedKey="True" clip="50" gain="70" invert="False" clipDuration="73" triggerPoint="34" mixRate="5" preroll="0"/>
                <DVEParameters rate="25" logoRate="25" reverseDirection="False" flipFlop="False" effect="PushRight" fillSource="1" keySource="1" enableKey="False" preMultipliedKey="False" clip="50" gain="70" invertKey="False"/>
            </TransitionStyle>
            <Keys>
                <Key index="0" type="Luma" inputCut="1" inputFill="1" onAir="False" masked="False" maskTop="0" maskBottom="0" maskLeft="0" maskRight="0">
                    <LumaParameters preMultiplied="False" clip="50" gain="70" inverse="False"/>
                </Key>
                <Key index="1" type="Luma" inputCut="2" inputFill="2" onAir="False" masked="False" maskTop="0" maskBottom="0" maskLeft="0" maskRight="0">
                    <LumaParameters preMultiplied="False" clip="50" gain="70" inverse="False"/>
                </Key>
                <Key index="2" type="Luma" inputCut="3" inputFill="3" onAir="False" masked="False" maskTop="0" maskBottom="0" maskLeft="0" maskRight="0">
                    <LumaParameters preMultiplied="False" clip="50" gain="70" inverse="False"/>
                </Key>
                <Key index="3" type="Luma" inputCut="4" inputFill="4" onAir="False" masked="False" maskTop="0" maskBottom="0" maskLeft="0" maskRight="0">
                    <LumaParameters preMultiplied="False" clip="50" gain="70" inverse="False"/>
                </Key>
            </Keys>
            <FadeToBlack rate="25" isFullyBlack="False"/>
        </MixEffectBlock>
    </MixEffectBlocks>
</Profile>
`

// granularState is _synthetic_mixerstate(meCount): the golden's state for
// one M/E, the recorded four-M/E version otherwise.
func granularState(t *testing.T, meCount int) atem.State {
	t.Helper()
	o := loadOracle(t)
	if meCount == 1 {
		st, _ := buildState(t, o.Golden.State, true)
		return st
	}
	for _, s := range o.Saves {
		if s.Name == "granular-4me" {
			st, _ := buildState(t, s.State, false)
			return st
		}
	}
	t.Fatal("no granular-4me state")
	return atem.State{}
}

// saveRoot is _build_synthetic_save_xml: save st with opts, return the root.
func saveRoot(t *testing.T, st atem.State, opts *SaveOptions) *Element {
	t.Helper()
	p, err := Save(context.Background(), &fakeSource{st: st}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return p.Root()
}

// --- test_profile_granular.py -----------------------------------------------------

func TestApplyProgramOnlyDoesNotSetPreview(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].Program = true
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if len(ofType[atem.ProgramInput](tg.cmds)) != 1 || len(ofType[atem.PreviewInput](tg.cmds)) != 0 {
		t.Fatalf("sent %v", tg.cmds)
	}
}

func TestApplyPreviewOnlyDoesNotSetProgram(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].Preview = true
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if len(ofType[atem.ProgramInput](tg.cmds)) != 0 || len(ofType[atem.PreviewInput](tg.cmds)) != 1 {
		t.Fatalf("sent %v", tg.cmds)
	}
}

func TestApplyNeitherProgramNorPreviewEmitsNothingOnBus(t *testing.T) {
	tg := newTarget(atem.State{})
	mustApply(t, mustProfile(t, fullMEXML), tg, allOffApplyOptions(1))
	if len(ofType[atem.ProgramInput](tg.cmds))+len(ofType[atem.PreviewInput](tg.cmds)) != 0 {
		t.Fatalf("sent %v", tg.cmds)
	}
}

func TestApplyNextTransitionOnlyNoStylePacket(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].NextTransition = true
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	trss := ofType[atem.TransitionSettings](tg.cmds)
	if len(trss) != 1 || trss[0].NextTransition == nil {
		t.Fatalf("TrSS %v", trss)
	}
	if len(ofType[atem.MixSettings](tg.cmds))+len(ofType[atem.DipSettings](tg.cmds))+len(ofType[atem.WipeSettings](tg.cmds)) != 0 {
		t.Fatal("per-style commands sent")
	}
}

func TestApplyTransitionStyleOnlyNoNextTransitionMask(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].TransitionStyle = true
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	for _, c := range ofType[atem.TransitionSettings](tg.cmds) {
		if c.NextTransition != nil {
			t.Fatal("next-transition mask sent")
		}
	}
	if len(ofType[atem.MixSettings](tg.cmds)) == 0 {
		t.Fatal("no MixSettings")
	}
}

func TestApplyFadeToBlackOnly(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].FadeToBlack = true
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if len(ofType[atem.FadeToBlackConfig](tg.cmds)) < 1 || len(ofType[atem.ProgramInput](tg.cmds)) != 0 {
		t.Fatalf("sent %v", tg.cmds)
	}
}

// keyTypesPerKeyer is _key_setters_per_keyer.
func keyTypesPerKeyer(cmds []atem.Command) map[uint8]int {
	out := map[uint8]int{0: 0, 1: 0, 2: 0, 3: 0}
	for _, c := range ofType[atem.KeyType](cmds) {
		out[c.Keyer]++
	}
	return out
}

func TestApplyUSK2Only(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].USK = []bool{false, false, true, false}
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if n := keyTypesPerKeyer(tg.cmds); n[0] != 0 || n[1] != 0 || n[2] < 1 || n[3] != 0 {
		t.Fatalf("KeyType per keyer %v", n)
	}
}

func TestApplyUSKSkip2KeepOthers(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].USK = []bool{true, false, true, true}
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if n := keyTypesPerKeyer(tg.cmds); n[0] < 1 || n[1] != 0 || n[2] < 1 || n[3] < 1 {
		t.Fatalf("KeyType per keyer %v", n)
	}
}

func TestApplyNoUSKEmitsNoKeySetters(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].USK = []bool{false, false, false, false}
	mustApply(t, mustProfile(t, fullMEXML), tg, o)
	if len(ofType[atem.KeyType](tg.cmds))+len(ofType[atem.KeyFill](tg.cmds))+len(ofType[atem.KeyCut](tg.cmds)) != 0 {
		t.Fatal("key setters sent")
	}
}

// saveWith is _save_with: default options with mes[0] changed by set.
func saveWith(t *testing.T, set func(o *SaveOptions)) *Element {
	o := DefaultSaveOptions()
	set(o)
	return saveRoot(t, granularState(t, 1), o)
}

func TestSaveProgramOffOmitsProgramElement(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].Program = false }).Find("MixEffectBlocks/MixEffectBlock")
	if block == nil || block.Find("Program") != nil || block.Find("Preview") == nil {
		t.Fatal("wrong elements")
	}
}

func TestSavePreviewOffOmitsPreviewElement(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].Preview = false }).Find("MixEffectBlocks/MixEffectBlock")
	if block.Find("Preview") != nil || block.Find("Program") == nil {
		t.Fatal("wrong elements")
	}
}

func TestSaveNextTransitionOffOmitsNextTransitionElement(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].NextTransition = false }).Find("MixEffectBlocks/MixEffectBlock")
	if block.Find("NextTransition") != nil || block.Find("TransitionStyle") == nil {
		t.Fatal("wrong elements")
	}
}

func TestSaveTransitionStyleOffOmitsStyleAndPerStyleChildren(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].TransitionStyle = false }).Find("MixEffectBlocks/MixEffectBlock")
	if block.Find("TransitionStyle") != nil || block.Find("NextTransition") == nil {
		t.Fatal("wrong elements")
	}
}

func TestSaveFadeToBlackOffOmitsFTBElement(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].FadeToBlack = false }).Find("MixEffectBlocks/MixEffectBlock")
	if block.Find("FadeToBlack") != nil {
		t.Fatal("FadeToBlack written")
	}
}

func keyIndices(e *Element) []int {
	var out []int
	for _, k := range e.FindAll("Key") {
		n, _ := strconv.Atoi(k.GetDefault("index", ""))
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func TestSaveUSK2OffOmitsOnlyKeyIndex1(t *testing.T) {
	keys := saveWith(t, func(o *SaveOptions) { o.MEs[0].USK = []bool{true, false, true, true} }).Find("MixEffectBlocks/MixEffectBlock/Keys")
	if keys == nil || !slices.Equal(keyIndices(keys), []int{0, 2, 3}) {
		t.Fatal("wrong keys")
	}
}

func TestSaveNoUSKOmitsKeysElementEntirely(t *testing.T) {
	block := saveWith(t, func(o *SaveOptions) { o.MEs[0].USK = []bool{false, false, false, false} }).Find("MixEffectBlocks/MixEffectBlock")
	if block.Find("Keys") != nil {
		t.Fatal("Keys written")
	}
}

func TestSaveAllMEOffOmitsMixEffectBlocksEntirely(t *testing.T) {
	root := saveWith(t, func(o *SaveOptions) { o.MEs[0] = MixEffectOptionsNone() })
	if root.Find("MixEffectBlocks") != nil {
		t.Fatal("MixEffectBlocks written")
	}
}

func TestSaveOptionsDefaultEmitsAllNineElements(t *testing.T) {
	block := saveRoot(t, granularState(t, 1), DefaultSaveOptions()).Find("MixEffectBlocks/MixEffectBlock")
	for _, tag := range []string{"Program", "Preview", "NextTransition", "TransitionStyle", "FadeToBlack"} {
		if block.Find(tag) == nil {
			t.Fatalf("no %s", tag)
		}
	}
	if !slices.Equal(keyIndices(block.Find("Keys")), []int{0, 1, 2, 3}) {
		t.Fatal("wrong keys")
	}
}

func TestSaveOptionsDefaultHasNineMEFlags(t *testing.T) {
	o := DefaultSaveOptions()
	if len(o.MEs) != 4 {
		t.Fatalf("%d M/Es", len(o.MEs))
	}
	for _, m := range o.MEs {
		if !m.Program || !m.Preview || !m.NextTransition || !m.TransitionStyle || !m.FadeToBlack ||
			!slices.Equal(m.USK, []bool{true, true, true, true}) {
			t.Fatalf("%+v", m)
		}
	}
}

func TestMEOptionsBeyondListIsFullyDeselected(t *testing.T) {
	o := &SaveOptions{MEs: []MixEffectOptions{NewMixEffectOptions()}}
	if !o.MEOptions(0).AnySelected() || o.MEOptions(1).AnySelected() || o.MEOptions(7).AnySelected() {
		t.Fatal("wrong selection")
	}
}

// test_apply_options_old_field_names_are_gone has no counterpart: Go
// structs have only the fields declared, so an alias can't creep in.

func TestSaveEmitsBothKeyframeAAndB(t *testing.T) {
	st := granularState(t, 1)
	st.MEs[0].USKs = []atem.USK{{Keyframes: map[uint8]*atem.KeyPropertiesFlyKeyframeField{
		1: {SizeX: 1500, SizeY: 500, PosY: 0},
		2: {SizeX: 1500, SizeY: 500, PosY: 13000},
	}}}
	fly := saveRoot(t, st, DefaultSaveOptions()).Find("MixEffectBlocks/MixEffectBlock/Keys/Key/FlyParameters")
	a, b := fly.Find("KeyFrameA"), fly.Find("KeyFrameB")
	if a == nil || b == nil {
		t.Fatal("both keyframes must be written")
	}
	if a.GetDefault("xSize", "") != "1.5" || a.GetDefault("yPosition", "") != "0" || b.GetDefault("yPosition", "") != "13" {
		t.Fatalf("%v %v", a.Attrs, b.Attrs)
	}
}

func TestSaveEmitsOnlyStoredKeyframe(t *testing.T) {
	st := granularState(t, 1)
	st.MEs[0].USKs = []atem.USK{{Keyframes: map[uint8]*atem.KeyPropertiesFlyKeyframeField{1: {SizeX: 1500}}}}
	fly := saveRoot(t, st, DefaultSaveOptions()).Find("MixEffectBlocks/MixEffectBlock/Keys/Key/FlyParameters")
	if fly.Find("KeyFrameA") == nil || fly.Find("KeyFrameB") != nil {
		t.Fatal("wrong keyframes")
	}
}

// test_single_me_save_byte_identical_to_pre_4c1_golden is TestSaveGolden
// (oracle_test.go).

func blocksByIndex(root *Element) map[int]*Element {
	out := map[int]*Element{}
	for _, b := range root.FindAll("MixEffectBlocks/MixEffectBlock") {
		n, _ := strconv.Atoi(b.GetDefault("index", ""))
		out[n] = b
	}
	return out
}

func TestMultiMESaveEmitsOneBlockPerMEWithPerMEValues(t *testing.T) {
	blocks := blocksByIndex(saveRoot(t, granularState(t, 4), DefaultSaveOptions()))
	if len(blocks) != 4 {
		t.Fatalf("%d blocks", len(blocks))
	}
	for me, b := range blocks {
		if b.Find("Program").GetDefault("input", "") != strconv.Itoa(1+10*me) ||
			b.Find("Preview").GetDefault("input", "") != strconv.Itoa(2+10*me) ||
			b.Find("TransitionStyle/MixParameters").GetDefault("rate", "") != strconv.Itoa(25+me) {
			t.Fatalf("M/E %d wrong", me)
		}
	}
}

func TestMultiMESaveSizesUSKToEachMEsKeyerCount(t *testing.T) {
	blocks := blocksByIndex(saveRoot(t, granularState(t, 4), DefaultSaveOptions()))
	for me, want := range map[int][]int{0: {0, 1, 2, 3}, 1: {0, 1}, 2: {0, 1, 2, 3}, 3: {0, 1, 2, 3}} {
		if got := keyIndices(blocks[me].Find("Keys")); !slices.Equal(got, want) {
			t.Fatalf("M/E %d: %v", me, got)
		}
	}
}

func TestMultiMESaveGatesOneMEsProgramOnly(t *testing.T) {
	o := DefaultSaveOptions()
	o.MEs[2].Program = false
	blocks := blocksByIndex(saveRoot(t, granularState(t, 4), o))
	for _, me := range []int{0, 1, 3} {
		if blocks[me].Find("Program") == nil {
			t.Fatalf("M/E %d lost Program", me)
		}
	}
	if blocks[2].Find("Program") != nil || blocks[2].Find("Preview") == nil {
		t.Fatal("M/E 2 wrong")
	}
}

func TestMultiMESaveFullyDeselectedMEEmitsNoBlock(t *testing.T) {
	o := DefaultSaveOptions()
	o.MEs[1] = MixEffectOptionsNone()
	blocks := blocksByIndex(saveRoot(t, granularState(t, 4), o))
	if _, ok := blocks[1]; ok || len(blocks) != 3 {
		t.Fatalf("blocks %v", blocks)
	}
}

func TestMultiMESaveShortMEsListEmitsOnlyListedMEs(t *testing.T) {
	o := DefaultSaveOptions()
	o.MEs = []MixEffectOptions{NewMixEffectOptions()}
	blocks := blocksByIndex(saveRoot(t, granularState(t, 4), o))
	if _, ok := blocks[0]; !ok || len(blocks) != 1 {
		t.Fatalf("blocks %v", blocks)
	}
}

// fourMEProfile is _four_me_profile and fourMEState _four_me_conn's state.
func fourMEProfile(t *testing.T) *Profile {
	return New(saveRoot(t, granularState(t, 4), DefaultSaveOptions()))
}

func fourMEState() atem.State {
	var st atem.State
	for me := range 4 {
		keyers := uint8(4)
		if me == 1 {
			keyers = 2
		}
		st.MEs = append(st.MEs, atem.ME{Config: &atem.MixerEffectConfigField{Index: uint8(me), Keyers: keyers}})
	}
	return st
}

func TestApplyFourBlocksTargetsEachME(t *testing.T) {
	tg := newTarget(fourMEState())
	o := allOffApplyOptions(4)
	for i := range o.MEs {
		o.MEs[i].Program = true
	}
	mustApply(t, fourMEProfile(t), tg, o)
	got := map[uint8]uint16{}
	for _, c := range ofType[atem.ProgramInput](tg.cmds) {
		got[c.ME] = c.Source
	}
	if !reflect.DeepEqual(got, map[uint8]uint16{0: 1, 1: 11, 2: 21, 3: 31}) {
		t.Fatalf("%v", got)
	}
}

func TestApplyMEs1USK0FalseSkipsOnlyThatKey(t *testing.T) {
	tg := newTarget(fourMEState())
	o := allOffApplyOptions(4)
	for i := range o.MEs {
		o.MEs[i].USK = []bool{true, true, true, true}
	}
	o.MEs[1].USK[0] = false
	mustApply(t, fourMEProfile(t), tg, o)
	got := map[[2]uint8]bool{}
	for _, c := range ofType[atem.KeyType](tg.cmds) {
		got[[2]uint8{c.ME, c.Keyer}] = true
	}
	want := map[[2]uint8]bool{{1, 1}: true}
	for _, me := range []uint8{0, 2, 3} {
		for k := range uint8(4) {
			want[[2]uint8{me, k}] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestApplyCrossModelFourBlocksOntoOneMESwitcher(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(4)
	for i := range o.MEs {
		o.MEs[i].Program = true
	}
	res := mustApply(t, fourMEProfile(t), tg, o)
	progs := ofType[atem.ProgramInput](tg.cmds)
	if len(progs) != 1 || progs[0].ME != 0 || progs[0].Source != 1 || len(res.Errors) != 0 {
		t.Fatalf("%v %v", progs, res.Errors)
	}
}

func TestApplyLegacySingleBlockXMLStillApplies(t *testing.T) {
	tg := newTarget(atem.State{})
	o := allOffApplyOptions(1)
	o.MEs[0].Program = true
	res := mustApply(t, mustProfile(t, fullMEXML), tg, o)
	progs := ofType[atem.ProgramInput](tg.cmds)
	if len(progs) != 1 || progs[0].ME != 0 || progs[0].Source != 3010 || len(res.Errors) != 0 {
		t.Fatalf("%v %v", progs, res.Errors)
	}
}

func dskBlocks(root *Element) (idx, fill []string) {
	for _, b := range root.FindAll("DownstreamKeys/DownstreamKey") {
		idx = append(idx, b.GetDefault("index", ""))
		fill = append(fill, b.GetDefault("fillSource", ""))
	}
	return
}

func TestSaveEmitsAllDSKsFromTopologyCount(t *testing.T) {
	st := granularState(t, 1)
	st.Topology = &atem.TopologyField{DownstreamKeyers: 4}
	for i := range 4 {
		st.DSKs = append(st.DSKs, atem.DSK{
			State: &atem.DkeyStateField{Index: uint8(i)},
			Base:  &atem.DkeyPropertiesBaseField{Index: uint8(i), FillSource: uint16(10 + i), KeySource: uint16(20 + i)},
		})
	}
	idx, fill := dskBlocks(saveRoot(t, st, DefaultSaveOptions()))
	if !slices.Equal(idx, []string{"0", "1", "2", "3"}) || !slices.Equal(fill, []string{"10", "11", "12", "13"}) {
		t.Fatalf("%v %v", idx, fill)
	}
}

func TestSaveDSKCountFallsBackToDkeyStateEntries(t *testing.T) {
	st := granularState(t, 1)
	for i := range 3 {
		st.DSKs = append(st.DSKs, atem.DSK{State: &atem.DkeyStateField{Index: uint8(i)}})
	}
	if idx, _ := dskBlocks(saveRoot(t, st, DefaultSaveOptions())); !slices.Equal(idx, []string{"0", "1", "2"}) {
		t.Fatalf("%v", idx)
	}
}

func TestSaveDSKCountDefaultsToOneBlockPreHandshake(t *testing.T) {
	if idx, _ := dskBlocks(saveRoot(t, granularState(t, 1), DefaultSaveOptions())); !slices.Equal(idx, []string{"0"}) {
		t.Fatalf("%v", idx)
	}
}

// --- test_profile_apply_restore.py -----------------------------------------------

// payload is a command's bytes after the 8-byte header.
func payload(c atem.Command) []byte { return c.Encode()[8:] }

func i16(b []byte, off int) int   { return int(int16(binary.BigEndian.Uint16(b[off:]))) }
func u16At(b []byte, off int) int { return int(binary.BigEndian.Uint16(b[off:])) }
func i32(b []byte, off int) int   { return int(int32(binary.BigEndian.Uint32(b[off:]))) }

// applySection runs one section of apply on a document, as the Python
// tests call _apply_* directly.
func applySection(t *testing.T, xml string, opts *ApplyOptions, section func(*applier) error) *recTarget {
	t.Helper()
	tg := newTarget(atem.State{})
	a := mustProfile(t, xml).newApplier(context.Background(), tg, opts)
	if err := section(a); err != nil {
		t.Fatal(err)
	}
	return tg
}

func uskXML(key string) string {
	return `<Profile><MixEffectBlocks><MixEffectBlock index="0"><Keys>` + key + `</Keys></MixEffectBlock></MixEffectBlocks></Profile>`
}

func uskOpts() *ApplyOptions {
	o := DefaultApplyOptions()
	o.MEs[0].USK = []bool{true, true, true, true}
	return o
}

func TestApplyChromaCursorSizePositionSampled(t *testing.T) {
	tg := applySection(t, uskXML(`<Key index="0" type="Chroma"><AdvancedChromaParameters cursorSize="6.25" cursorXPosition="8.0" cursorYPosition="-4.5" sampledY="0.6748" sampledCb="0.1621" sampledCr="0.1015"/></Key>`),
		uskOpts(), (*applier).upstreamKeyers)
	var size, pos, col *atem.KeyPropertiesAdvancedChromaColorpicker
	for _, c := range ofType[atem.KeyPropertiesAdvancedChromaColorpicker](tg.cmds) {
		switch {
		case c.Size != nil:
			size = &c
		case c.X != nil:
			pos = &c
		case c.SampleY != nil:
			col = &c
		}
	}
	if size == nil || pos == nil || col == nil {
		t.Fatal("three CACC commands expected")
	}
	if u16At(payload(*size), 10) != 625 {
		t.Fatalf("size %d", u16At(payload(*size), 10))
	}
	if p := payload(*pos); i16(p, 6) != 8000 || i16(p, 8) != -4500 {
		t.Fatalf("position %d %d", i16(p, 6), i16(p, 8))
	}
	if p := payload(*col); u16At(p, 12) != 6748 || i16(p, 14) != 1621 || i16(p, 16) != 1015 {
		t.Fatalf("colour %d %d %d", u16At(p, 12), i16(p, 14), i16(p, 16))
	}
}

func TestApplyFlyGeometry(t *testing.T) {
	tg := applySection(t, uskXML(`<Key index="0" type="DVE"><FlyParameters enabled="True" xPosition="4.0" yPosition="-2.0" xSize="1.5" ySize="0.5" rotation="30" rate="20"/></Key>`),
		uskOpts(), (*applier).upstreamKeyers)
	got := map[string]int{}
	rate := false
	for _, c := range ofType[atem.KeyPropertiesDVE](tg.cmds) {
		p := payload(c)
		switch {
		case c.PosX != nil:
			got["pos_x"] = i32(p, 16)
		case c.PosY != nil:
			got["pos_y"] = i32(p, 20)
		case c.SizeX != nil:
			got["size_x"] = i32(p, 8)
		case c.SizeY != nil:
			got["size_y"] = i32(p, 12)
		case c.Rotation != nil:
			got["rotation"] = i32(p, 24)
		case c.Rate != nil:
			rate = true
		}
	}
	want := map[string]int{"pos_x": 4000, "pos_y": -2000, "size_x": 1500, "size_y": 500, "rotation": 300}
	if !reflect.DeepEqual(got, want) || !rate {
		t.Fatalf("%v rate %v", got, rate)
	}
}

func TestApplyFlyEnableOnlyWhenNoGeometry(t *testing.T) {
	tg := applySection(t, uskXML(`<Key index="0" type="DVE"><FlyParameters enabled="True"/></Key>`), uskOpts(), (*applier).upstreamKeyers)
	for _, c := range ofType[atem.KeyPropertiesDVE](tg.cmds) {
		if c.PosX != nil || c.PosY != nil || c.SizeX != nil || c.SizeY != nil || c.Rotation != nil {
			t.Fatal("geometry sent")
		}
	}
}

const stripDynamicsXML = `<Profile><FairlightAudioMixer><AudioInputs><AudioInput id="1"><AudioSource id="-65280">` +
	`<DynamicsProcessor makeupGain="3"><Compressor enabled="True" threshold="-30" ratio="2.5" attack="2" hold="10" release="100"/>` +
	`<Limiter enabled="True" threshold="-6" attack="1" hold="5" release="80"/>` +
	`<Expander enabled="True" gateMode="True" threshold="-40" range="20" ratio="2" attack="3" hold="0" release="90"/>` +
	`</DynamicsProcessor></AudioSource></AudioInput></AudioInputs></FairlightAudioMixer></Profile>`

func TestApplyStripDynamicsCompressorLimiterExpander(t *testing.T) {
	tg := applySection(t, stripDynamicsXML, nil, (*applier).fairlight)
	comp := ofType[atem.FairlightCompressorProperties](tg.cmds)[0]
	if comp.Source != 1 || comp.Channel != -1 || !*comp.Enabled || *comp.Threshold != -30 || *comp.Ratio != 2.5 ||
		*comp.Attack != 2 || *comp.Hold != 10 || *comp.Release != 100 {
		t.Fatalf("compressor %+v", comp)
	}
	lim := ofType[atem.FairlightLimiterProperties](tg.cmds)[0]
	if lim.Source != 1 || lim.Channel != -1 || !*lim.Enabled || *lim.Threshold != -6 || *lim.Attack != 1 ||
		*lim.Hold != 5 || *lim.Release != 80 {
		t.Fatalf("limiter %+v", lim)
	}
	exp := ofType[atem.FairlightExpanderProperties](tg.cmds)[0]
	if exp.Source != 1 || exp.Channel != -1 || !*exp.Enabled || *exp.Mode != 1 || *exp.Threshold != -40 ||
		*exp.Range != 20 || *exp.Ratio != 2 || *exp.Attack != 3 || *exp.Hold != 0 || *exp.Release != 90 {
		t.Fatalf("expander %+v", exp)
	}
}

func TestApplyMasterCompressorAndLimiter(t *testing.T) {
	tg := applySection(t, `<Profile><FairlightAudioMixer><MasterOutDynamicsProcessor makeupGain="0">`+
		`<Compressor enabled="True" threshold="-23.5" ratio="6" attack="12" hold="7" release="250"/>`+
		`<Limiter enabled="True" threshold="-1" attack="2.2" hold="5" release="100"/>`+
		`</MasterOutDynamicsProcessor></FairlightAudioMixer></Profile>`, nil, (*applier).fairlight)
	comps := ofType[atem.FairlightMasterCompressorProperties](tg.cmds)
	if len(comps) != 1 || !*comps[0].Enabled || *comps[0].Threshold != -23.5 || *comps[0].Ratio != 6 ||
		*comps[0].Attack != 12 || *comps[0].Hold != 7 || *comps[0].Release != 250 {
		t.Fatalf("compressor %+v", comps)
	}
	lims := ofType[atem.FairlightMasterLimiterProperties](tg.cmds)
	if len(lims) != 1 || !*lims[0].Enabled || *lims[0].Threshold != -1 || *lims[0].Attack != 2.2 ||
		*lims[0].Hold != 5 || *lims[0].Release != 100 {
		t.Fatalf("limiter %+v", lims)
	}
	if len(ofType[atem.FairlightCompressorProperties](tg.cmds))+len(ofType[atem.FairlightLimiterProperties](tg.cmds)) != 0 {
		t.Fatal("per-strip dynamics sent for the master")
	}
}

func TestApplyMasterEQEnableGainAndBands(t *testing.T) {
	tg := applySection(t, `<Profile><FairlightAudioMixer masterOutFaderGain="0"><MasterOutEqualizer enabled="True" gain="-5.96">`+
		`<EqualizerBand index="0" enabled="False" shape="HighPass" frequencyRange="Low" frequency="46" gain="0" qFactor="0.71"/>`+
		`<EqualizerBand index="2" enabled="True" shape="BandPass" frequencyRange="MidLow" frequency="150" gain="20" qFactor="2.3"/>`+
		`</MasterOutEqualizer></FairlightAudioMixer></Profile>`, nil, (*applier).fairlight)
	cfmp := ofType[atem.FairlightMasterProperties](tg.cmds)
	en, gn := -1, -1
	for i, c := range cfmp {
		if c.EQEnable != nil {
			en = i
			if !*c.EQEnable {
				t.Fatal("EQ enable false")
			}
		}
		if c.EQGain != nil {
			gn = i
			// -5.96 dB is -596 hundredths, a sign-extended i32 at byte 4.
			if got := i32(payload(c), 4); got != -596 {
				t.Fatalf("EQ gain %d", got)
			}
		}
	}
	if en < 0 || gn < 0 || en > gn {
		t.Fatalf("enable at %d, gain at %d", en, gn)
	}
	bands := ofType[atem.FairlightMasterEQBandProperties](tg.cmds)
	if len(bands) != 2 || bands[1].Band != 2 || !*bands[1].Enabled || *bands[1].Frequency != 150 || *bands[1].Gain != 20 || *bands[1].Q != 2.3 {
		t.Fatalf("bands %+v", bands)
	}
	if len(ofType[atem.FairlightEQBandProperties](tg.cmds)) != 0 {
		t.Fatal("master bands sent as strip bands")
	}
}

func TestApplyStripExpanderModeExpanderWhenNotGate(t *testing.T) {
	tg := applySection(t, `<Profile><FairlightAudioMixer><AudioInputs><AudioInput id="2"><AudioSource id="-256">`+
		`<DynamicsProcessor makeupGain="0"><Expander enabled="False" gateMode="False" threshold="-45" range="18" ratio="1.1" attack="1.4" hold="0" release="93"/>`+
		`</DynamicsProcessor></AudioSource></AudioInput></AudioInputs></FairlightAudioMixer></Profile>`, nil, (*applier).fairlight)
	exp := ofType[atem.FairlightExpanderProperties](tg.cmds)[0]
	if exp.Source != 2 || *exp.Mode != 0 {
		t.Fatalf("%+v", exp)
	}
}

func TestApplyDSKTieTrue(t *testing.T) {
	tg := applySection(t, `<Profile><DownstreamKeys><DownstreamKey index="0" tie="True" fillSource="1" keySource="2" rate="25"/></DownstreamKeys></Profile>`,
		nil, (*applier).downstreamKeys)
	ties := ofType[atem.DkeyTie](tg.cmds)
	if len(ties) != 1 || ties[0].DSK != 0 || !ties[0].Tie {
		t.Fatalf("%v", ties)
	}
}

func TestApplyDSKTieFalse(t *testing.T) {
	tg := applySection(t, `<Profile><DownstreamKeys><DownstreamKey index="0" tie="False" rate="25"/></DownstreamKeys></Profile>`,
		nil, (*applier).downstreamKeys)
	if ofType[atem.DkeyTie](tg.cmds)[0].Tie {
		t.Fatal("tie true")
	}
}

func TestApplyDownstreamKeysNoDSKCap(t *testing.T) {
	var b strings.Builder
	for i := range 4 {
		fmt.Fprintf(&b, `<DownstreamKey index="%d" tie="True" rate="25"/>`, i)
	}
	tg := applySection(t, `<Profile><DownstreamKeys>`+b.String()+`</DownstreamKeys></Profile>`, nil, (*applier).downstreamKeys)
	var idx []uint8
	for _, c := range ofType[atem.DkeyTie](tg.cmds) {
		idx = append(idx, c.DSK)
	}
	if !slices.Equal(idx, []uint8{0, 1, 2, 3}) {
		t.Fatalf("%v", idx)
	}
}

// --- test_profile_roundtrip.py -------------------------------------------------------

// test_profile_parses_reference, test_profile_parses_macros_only and
// test_profile_round_trip_normalized read two real ATEM Software Control
// exports bmdwire's repository doesn't contain (pytest skips them); the
// two tests below run on the golden instead.

func TestProfileToXMLStartsWithDeclaration(t *testing.T) {
	p := mustProfile(t, string(fixtures.Bytes(t, "profile/profile_single_me_save_golden.xml")))
	out, _ := p.ToXML()
	if !strings.HasPrefix(out, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n") {
		t.Fatal(out[:60])
	}
}

func TestProfileToXMLUses4SpaceIndentAndCompactSelfClosing(t *testing.T) {
	p := mustProfile(t, string(fixtures.Bytes(t, "profile/profile_single_me_save_golden.xml")))
	out, _ := p.ToXML()
	if strings.Contains(out, " />") || !strings.Contains(out, "\n    <MixEffectBlocks>") {
		t.Fatal("wrong formatting")
	}
}

func TestApplyOptionsDefaultSkipsProgramPreview(t *testing.T) {
	o := DefaultApplyOptions()
	if len(o.MEs) != 4 {
		t.Fatal(len(o.MEs))
	}
	for _, m := range o.MEs {
		if m.Program || m.Preview || !m.NextTransition || !m.TransitionStyle || !m.FadeToBlack ||
			!slices.Equal(m.USK, []bool{true, true, true, true}) {
			t.Fatalf("%+v", m)
		}
	}
	if !o.RestoreDownstreamKeys || !o.RestoreInputs || !o.RestoreVideoMode || !o.RestoreAudio || !o.RestoreMacros || o.RestoreMultiview {
		t.Fatalf("%+v", o)
	}
}

func TestApplyResultSummary(t *testing.T) {
	var r ApplyResult
	if r.Summary() != "no changes" {
		t.Fatal(r.Summary())
	}
	r.NoteApplied("color_generators")
	r.NoteSkipped("audio", "gap")
	if s := r.Summary(); !strings.Contains(s, "applied: 1") || !strings.Contains(s, "skipped: 1") {
		t.Fatal(s)
	}
}

// selectionMask applies a NextTransition selection and returns the mask
// sent (_apply_with_only_transition, _trss_next_transition_from_sent).
func selectionMask(t *testing.T, sel string) []uint8 {
	xml := `<Profile majorVersion="2" minorVersion="1"><MixEffectBlocks><MixEffectBlock index="0">` +
		`<NextTransition selection="` + sel + `" nextSelection="` + sel + `"/></MixEffectBlock></MixEffectBlocks></Profile>`
	o := &ApplyOptions{MEs: []MixEffectOptions{{NextTransition: true, USK: []bool{false, false, false, false}}}, RestoreHyperDecks: true}
	tg := newTarget(atem.State{})
	mustApply(t, mustProfile(t, xml), tg, o)
	var masks []uint8
	for _, c := range ofType[atem.TransitionSettings](tg.cmds) {
		if c.NextTransition != nil {
			masks = append(masks, *c.NextTransition)
		}
	}
	return masks
}

func TestApplyNextTransitionAllLayersNoWhitespace(t *testing.T) {
	if m := selectionMask(t, "Background,Key1,Key2,Key3,Key4"); !slices.Equal(m, []uint8{0x1f}) {
		t.Fatal(m)
	}
}

func TestApplyNextTransitionAllLayersWithWhitespace(t *testing.T) {
	if m := selectionMask(t, "Background, Key1, Key2, Key3, Key4"); !slices.Equal(m, []uint8{0x1f}) {
		t.Fatal(m)
	}
}

func TestApplyNextTransitionPartialSelection(t *testing.T) {
	if m := selectionMask(t, "Background,Key2"); !slices.Equal(m, []uint8{0x05}) {
		t.Fatal(m)
	}
}

func TestApplyNextTransitionKeyerOnly(t *testing.T) {
	if m := selectionMask(t, "Key3"); !slices.Equal(m, []uint8{0x08}) {
		t.Fatal(m)
	}
}

func TestApplyNextTransitionEmitsOnePacketNotFive(t *testing.T) {
	if m := selectionMask(t, "Background,Key1,Key2,Key3,Key4"); len(m) != 1 {
		t.Fatal(m)
	}
}

func TestProfileFromAtemAgainstSyntheticState(t *testing.T) {
	var st atem.State
	for _, s := range loadOracle(t).Saves {
		if s.Name == "roundtrip-synthetic" {
			st, _ = buildState(t, s.State, false)
		}
	}
	p, err := Save(context.Background(), &fakeSource{st: st}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.ToXML()
	p2 := mustProfile(t, out)
	if p2.Product() != "ATEM 1 M/E Constellation HD" || p2.VideoMode() != "1080p60" {
		t.Fatalf("%q %q", p2.Product(), p2.VideoMode())
	}
	for _, section := range []string{"MixEffectBlocks", "DownstreamKeys", "ColorGenerators", "Auxiliaries", "Settings",
		"VideoMode", "HyperDecks", "FairlightAudioMixer", "MediaPlayers", "MediaPool", "CameraControl", "MacroPool",
		"MacroControl", "Counters"} {
		if p2.Root().Find(section) == nil {
			t.Fatalf("missing section %s", section)
		}
	}
}

// --- test_profile_video_mode.py ------------------------------------------------------

// videoModeStates is each mode's VidM, as the oracle recorded it.
func videoModeStates(t *testing.T) map[int]atem.State {
	out := map[int]atem.State{}
	for _, s := range loadOracle(t).Saves {
		if m, ok := strings.CutPrefix(s.Name, "videomode-"); ok && m != "none" {
			n, _ := strconv.Atoi(m)
			out[n], _ = buildState(t, s.State, false)
		}
	}
	if len(out) != 30 {
		t.Fatalf("%d video modes", len(out))
	}
	return out
}

func videoModeOnlySave() *SaveOptions {
	o := &SaveOptions{VideoMode: true}
	for range 4 {
		o.MEs = append(o.MEs, MixEffectOptionsNone())
	}
	return o
}

func videoModeOnlyApply() *ApplyOptions {
	o := MacrosOnly()
	o.RestoreMacros = false
	o.RestoreVideoMode = true
	return o
}

func sentModes(tg *recTarget) []uint8 {
	var out []uint8
	for _, c := range ofType[atem.VideoMode](tg.cmds) {
		out = append(out, c.Mode)
	}
	return out
}

func TestCanonicalTableCoversEveryModeOnce(t *testing.T) {
	if len(videoModeXMLNames) != 30 {
		t.Fatal(len(videoModeXMLNames))
	}
	seen := map[string]bool{}
	for mode := range 30 {
		name, ok := videoModeXMLNames[mode]
		if !ok || seen[name] || videoModeNames[name] != mode {
			t.Fatalf("mode %d: %q", mode, name)
		}
		seen[name] = true
	}
}

func TestSavedModeAppliesAsTheSameMode(t *testing.T) {
	states := videoModeStates(t)
	for mode, st := range states {
		saved, err := Save(context.Background(), &fakeSource{st: st}, videoModeOnlySave())
		if err != nil {
			t.Fatal(err)
		}
		out, _ := saved.ToXML()
		p := mustProfile(t, out)
		if p.VideoMode() != videoModeXMLNames[mode] {
			t.Fatalf("mode %d saved as %q", mode, p.VideoMode())
		}
		other := 27
		if mode == 27 {
			other = 12
		}
		for _, live := range []atem.State{{}, states[other]} {
			tg := newTarget(live)
			res := mustApply(t, p, tg, videoModeOnlyApply())
			if got := sentModes(tg); !slices.Equal(got, []uint8{uint8(mode)}) {
				t.Fatalf("mode %d: sent %v (%s)", mode, got, res.Summary())
			}
		}
	}
}

func TestAlreadyAtTheSavedModeSendsNothing(t *testing.T) {
	for mode, st := range videoModeStates(t) {
		saved, _ := Save(context.Background(), &fakeSource{st: st}, videoModeOnlySave())
		tg := newTarget(st)
		res := mustApply(t, saved, tg, videoModeOnlyApply())
		if len(sentModes(tg)) != 0 || !slices.ContainsFunc(res.Skipped, func(s string) bool { return strings.HasPrefix(s, "video_mode: already at") }) {
			t.Fatalf("mode %d: %v %v", mode, sentModes(tg), res.Skipped)
		}
	}
}

// applyLabel is _apply_label; it also returns what was logged.
func applyLabel(t *testing.T, label string) ([]uint8, *ApplyResult, string) {
	var logged bytes.Buffer
	o := videoModeOnlyApply()
	o.Logger = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
	tg := newTarget(atem.State{})
	res := mustApply(t, mustProfile(t, `<Profile majorVersion="2" minorVersion="1"><VideoMode videoMode="`+label+`"/></Profile>`), tg, o)
	return sentModes(tg), res, logged.String()
}

// oldLabel is what atemwire up to 1.2.0 saved: the label without its
// aspect.
func oldLabel(st atem.State) string {
	l, _, _ := strings.Cut(st.VideoMode.Mode.Label(), " ")
	return l
}

func TestOldLabelSpellingsStillApply(t *testing.T) {
	for mode, st := range videoModeStates(t) {
		if mode < 4 {
			continue
		}
		if sent, _, _ := applyLabel(t, oldLabel(st)); !slices.Equal(sent, []uint8{uint8(mode)}) {
			t.Fatalf("%s: %v", oldLabel(st), sent)
		}
	}
}

func TestOldSDLabelsAreWhatTheOldSaveWroteForBothAspects(t *testing.T) {
	s := videoModeStates(t)
	if oldLabel(s[0]) != "525i59.94" || oldLabel(s[2]) != "525i59.94" || oldLabel(s[1]) != "625i50" || oldLabel(s[3]) != "625i50" {
		t.Fatal(oldLabel(s[0]), oldLabel(s[1]), oldLabel(s[2]), oldLabel(s[3]))
	}
}

func TestOldNTSCLabelIsSkippedWithAWarning(t *testing.T) {
	sent, res, logged := applyLabel(t, "525i59.94")
	if len(sent) != 0 || !slices.ContainsFunc(res.Skipped, func(s string) bool {
		return strings.HasPrefix(s, "video_mode:") && strings.Contains(s, "525i59.94")
	}) || !strings.Contains(logged, "525i59.94") {
		t.Fatalf("%v %v %q", sent, res.Skipped, logged)
	}
}

func TestPAL625i50AppliesAsPAL4x3WithAWarning(t *testing.T) {
	sent, _, logged := applyLabel(t, "625i50")
	if !slices.Equal(sent, []uint8{1}) || !strings.Contains(logged, "625i50") || !strings.Contains(logged, "ambiguous") {
		t.Fatalf("%v %q", sent, logged)
	}
}

func TestExplicitSDNamesApplyWithoutAWarning(t *testing.T) {
	for label, mode := range map[string]uint8{"525i5994": 0, "NTSC": 0, "PAL": 1, "NTSC_widescreen": 2, "PAL_widescreen": 3} {
		sent, _, logged := applyLabel(t, label)
		if !slices.Equal(sent, []uint8{mode}) || strings.Contains(logged, "atemwire before") {
			t.Fatalf("%s: %v %q", label, sent, logged)
		}
	}
}

// --- test_profile_xml_hardening.py ------------------------------------------------

const minimal = `<Profile majorVersion="2" minorVersion="1"/>`

const billionLaughs = `<?xml version="1.0"?>` +
	`<!DOCTYPE lolz [` +
	`<!ENTITY lol "lol">` +
	`<!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">` +
	`<!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">` +
	`]>` +
	`<Profile majorVersion="2" minorVersion="1">&lol3;</Profile>`

func valueError(t *testing.T, data []byte, words ...string) {
	t.Helper()
	_, err := FromXML(data)
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != "ValueError" && pe.Class != "UnicodeDecodeError" {
		t.Fatalf("%q: %v", data, err)
	}
	msg := strings.ToLower(pe.Message)
	if len(words) > 0 && !slices.ContainsFunc(words, func(w string) bool { return strings.Contains(msg, w) }) {
		t.Fatalf("%q: %v", data, err)
	}
}

func utf16Bytes(s string, bigEndian, bom bool) []byte {
	var out []byte
	units := utf16.Encode([]rune(s))
	if bom {
		units = append([]uint16{0xfeff}, units...)
	}
	for _, u := range units {
		if bigEndian {
			out = binary.BigEndian.AppendUint16(out, u)
		} else {
			out = binary.LittleEndian.AppendUint16(out, u)
		}
	}
	return out
}

func TestParsesMinimalProfile(t *testing.T) { mustProfile(t, minimal) }

func TestAcceptsBytesInput(t *testing.T) { mustProfile(t, string([]byte(minimal))) }

func TestRejectsBillionLaughs(t *testing.T) {
	valueError(t, []byte(billionLaughs), "entity", "doctype")
}

func TestRejectsBareDoctype(t *testing.T) { valueError(t, []byte("<!DOCTYPE x>"+minimal)) }

func TestRejectsEntityDeclaration(t *testing.T) {
	valueError(t, []byte(`<!DOCTYPE x [<!ENTITY e "AAAA">]>`+minimal))
}

func TestRejectIsCaseInsensitive(t *testing.T) {
	valueError(t, []byte(`<!doctype x [<!entity e "z">]>`+minimal))
}

func TestPrologCommentIsNotAFalsePositive(t *testing.T) { mustProfile(t, "<!-- a note -->"+minimal) }

func TestRejectsBillionLaughsAsUTF8Bytes(t *testing.T) {
	valueError(t, []byte(billionLaughs), "entity", "doctype")
}

// test_parses_minimal_profile_as_bytes is TestAcceptsBytesInput: FromXML
// takes bytes only.

func TestRejectsUTF16LEEncodedDTDNoBOM(t *testing.T) {
	valueError(t, utf16Bytes(billionLaughs, false, false), "nul", "utf-8")
}

func TestRejectsUTF16BEEncodedDTD(t *testing.T) {
	valueError(t, utf16Bytes(billionLaughs, true, false))
}

func TestRejectsUTF16BytesWithBOM(t *testing.T) {
	valueError(t, utf16Bytes(billionLaughs, false, true))
}

func TestRejectsStrDeclaringUTF16EncodingWithDTD(t *testing.T) {
	valueError(t, []byte(`<?xml version="1.0" encoding="utf-16"?><!DOCTYPE d [<!ENTITY a "AAAA">]>`+minimal))
}

func TestRejectsBareNULInStr(t *testing.T) {
	valueError(t, []byte("<Profile\x00 majorVersion=\"2\" minorVersion=\"1\"/>"))
}

func TestParsesNonASCIIInputLabelStrAndBytes(t *testing.T) {
	mustProfile(t, `<Profile majorVersion="2" minorVersion="1"><Settings><Input long="Caméra Café" short="CAMé"/></Settings></Profile>`)
}

// --- test_profile_protocol_adapter.py, test_profile_media_pool_native.py -------------

// test_profile_protocol_adapter.py has no counterpart: atemwire's
// _ProtocolAdapter and _resolve_connection choose between an ATEM, an
// ATEMConnection and a bare AtemProtocol; Save and Apply take one
// interface each, which *atem.Session satisfies (the compile-time check
// in profile.go), so there is nothing to adapt.

// test_profile_media_pool_native.py has no counterpart:
// download_media_pool_images is not ported.

// --- test_apply_macros.py ------------------------------------------------------------

func macroProfile(t *testing.T, inner string) *Profile {
	return mustProfile(t, `<Profile majorVersion="2" minorVersion="1"><MacroPool>`+inner+`</MacroPool></Profile>`)
}

// macrosOnlyOpts is _macros_only_opts: every restore_* flag off but
// macros; the per-M/E blocks keep their defaults (the Python helper only
// turns off bools and lists of bools).
func macrosOnlyOpts() *ApplyOptions {
	o := &ApplyOptions{RestoreMacros: true}
	for range PlatformMaxMEs {
		o.MEs = append(o.MEs, MixEffectOptionsApplyDefault())
	}
	return o
}

func applyMacros(t *testing.T, inner string, tg *recTarget) *ApplyResult {
	t.Helper()
	return mustApply(t, macroProfile(t, inner), tg, macrosOnlyOpts())
}

func macroLines(lines []string, prefix string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func uploads(tg *recTarget) [][4]string {
	var out [][4]string
	for _, e := range tg.events {
		if e.kind == "upload" {
			out = append(out, e.up)
		}
	}
	return out
}

func TestApplyMacrosTwoCleanUploadsReports2Of2(t *testing.T) {
	tg := newTarget(atem.State{})
	res := applyMacros(t, `<Macro index="0" name="A" description="">  <Op id="ProgramInput" mixEffectBlockIndex="0" input="Camera1"/>  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`+
		`<Macro index="1" name="B" description="">  <Op id="PreviewInput" mixEffectBlockIndex="0" input="Camera2"/></Macro>`, tg)
	if l := macroLines(res.Applied, "macros ("); !slices.Equal(l, []string{"macros (2/2 restored, 0 failed)"}) {
		t.Fatal(l)
	}
	if len(macroLines(res.Skipped, "macros:")) != 0 {
		t.Fatal(res.Skipped)
	}
	if u := uploads(tg); len(u) != 2 || u[0][0] != "0" || u[1][0] != "1" {
		t.Fatal(u)
	}
}

func TestApplyMacrosSingleCleanUploadReports1Of1(t *testing.T) {
	res := applyMacros(t, `<Macro index="0" name="solo" description="">  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`, newTarget(atem.State{}))
	if l := macroLines(res.Applied, "macros ("); !slices.Equal(l, []string{"macros (1/1 restored, 0 failed)"}) {
		t.Fatal(l)
	}
}

func TestApplyMacrosPartialUploadReportsDroppedOps(t *testing.T) {
	tg := newTarget(atem.State{})
	res := applyMacros(t, `<Macro index="0" name="WIDE CAM" description="">  <Op id="ProgramInput" mixEffectBlockIndex="0" input="Camera1"/>`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera1" sourceId="0" mixType="On"/>  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`, tg)
	l := macroLines(res.Applied, "macros (")
	if len(l) != 1 {
		t.Fatal(res.Applied)
	}
	for _, want := range []string{"1/1 restored", "1 with dropped ops", "slot 0 ('WIDE CAM')", "_TestSyntheticUnencodableOp2", "dropped 1 op"} {
		if !strings.Contains(l[0], want) {
			t.Fatalf("%q lacks %q", l[0], want)
		}
	}
	// Two ops of a 4-byte header and 4 bytes of parameters survive.
	if u := uploads(tg); len(u) != 1 || len(u[0][3]) != 32 {
		t.Fatal(u)
	}
}

func TestApplyMacrosDropsWithTwoUnknownIDsReportedDistinctly(t *testing.T) {
	res := applyMacros(t, `<Macro index="0" name="X" description="">  <Op id="Cut" mixEffectBlockIndex="0"/>`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera1" sourceId="0" ratio="2"/>`+
		`  <Op id="_TestSyntheticUnencodableOp"      input="Camera2" sourceId="0" threshold="-20"/></Macro>`, newTarget(atem.State{}))
	l := macroLines(res.Applied, "macros (")[0]
	for _, want := range []string{"dropped 2 ops", "_TestSyntheticUnencodableOp2", "_TestSyntheticUnencodableOp"} {
		if !strings.Contains(l, want) {
			t.Fatalf("%q lacks %q", l, want)
		}
	}
}

func TestApplyMacrosDropsDedupesRepeatedOpIDs(t *testing.T) {
	res := applyMacros(t, `<Macro index="0" name="A" description="">  <Op id="Cut" mixEffectBlockIndex="0"/>`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera1" sourceId="0" mixType="On"/>`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera2" sourceId="0" mixType="Off"/>`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera3" sourceId="0" mixType="Off"/></Macro>`, newTarget(atem.State{}))
	l := macroLines(res.Applied, "macros (")[0]
	if !strings.Contains(l, "dropped 3 ops") || strings.Count(l, "_TestSyntheticUnencodableOp2") != 1 {
		t.Fatal(l)
	}
}

func TestApplyMacrosAllOpsDroppedUploadsMetadataOnly(t *testing.T) {
	tg := newTarget(atem.State{})
	res := applyMacros(t, `<Macro index="0" name="all-bad" description="all dropped">`+
		`  <Op id="_TestSyntheticUnencodableOp2"      input="Camera1" sourceId="0" ratio="2"/>`+
		`  <Op id="_TestSyntheticUnencodableOp"      input="Camera2" sourceId="0" threshold="-20"/></Macro>`, tg)
	l := macroLines(res.Applied, "macros (")[0]
	for _, want := range []string{"1/1 restored", "1 with dropped ops", "dropped 2 ops", "metadata only"} {
		if !strings.Contains(l, want) {
			t.Fatalf("%q lacks %q", l, want)
		}
	}
	if u := uploads(tg); len(u) != 1 || u[0] != [4]string{"0", "all-bad", "all dropped", ""} {
		t.Fatal(u)
	}
	if len(ofType[atem.MacroRecord](tg.cmds))+len(ofType[atem.MacroAction](tg.cmds)) != 0 {
		t.Fatal("MSRc or MAct sent")
	}
}

func TestApplyMacrosTrulyEmptyMacroNoDropMessage(t *testing.T) {
	tg := newTarget(atem.State{})
	res := applyMacros(t, `<Macro index="7" name="meta-only" description="just a name"/>`, tg)
	if l := macroLines(res.Applied, "macros ("); !slices.Equal(l, []string{"macros (1/1 restored, 0 failed)"}) {
		t.Fatal(l)
	}
	if u := uploads(tg); len(u) != 1 || u[0] != [4]string{"7", "meta-only", "just a name", ""} {
		t.Fatal(u)
	}
	if len(tg.cmds) != 0 {
		t.Fatal("commands sent")
	}
}

func TestApplyMacrosUploadErrorReportsFailedWithMessage(t *testing.T) {
	tg := newTarget(atem.State{})
	tg.fail[3] = "ATEM rejected macro upload (status=not-found)"
	res := applyMacros(t, `<Macro index="3" name="solo" description="">  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`, tg)
	l := macroLines(res.Skipped, "macros:")
	if len(l) != 1 {
		t.Fatal(res.Skipped)
	}
	for _, want := range []string{"0/1 restored, 1 failed", "slot 3 ('solo')", "upload error", "not-found"} {
		if !strings.Contains(l[0], want) {
			t.Fatalf("%q lacks %q", l[0], want)
		}
	}
}

func TestApplyMacrosUploadErrorDistinctFromDrop(t *testing.T) {
	tg := newTarget(atem.State{})
	tg.fail[0] = "FTDE code 5"
	res := applyMacros(t, `<Macro index="0" name="A" description="">  <Op id="Cut" mixEffectBlockIndex="0"/>`+
		`  <Op id="_TestSyntheticUnencodableOp"      input="Camera2" sourceId="0" threshold="-20"/></Macro>`, tg)
	l := macroLines(res.Skipped, "macros:")
	if len(l) != 1 || !strings.Contains(l[0], "0/1 restored") || !strings.Contains(l[0], "1 failed") || strings.Contains(l[0], "dropped") {
		t.Fatal(l)
	}
}

func TestApplyMacrosMixedSuccessDropAndFailure(t *testing.T) {
	tg := newTarget(atem.State{})
	tg.fail[2] = "FTDE code 5"
	res := applyMacros(t, `<Macro index="0" name="clean" description="">  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`+
		`<Macro index="1" name="dropped" description="">  <Op id="Cut" mixEffectBlockIndex="0"/>`+
		`  <Op id="_TestSyntheticUnencodableOp"      input="Camera2" sourceId="0" threshold="-20"/></Macro>`+
		`<Macro index="2" name="boom" description="">  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`, tg)
	l := macroLines(res.Applied, "macros (")[0]
	if !strings.Contains(l, "2/3 restored") || !strings.Contains(l, "1 with dropped ops") || !strings.Contains(l, "1 failed") ||
		strings.Index(l, "with dropped ops") > strings.Index(l, "failed") {
		t.Fatal(l)
	}
}

func TestApplyEmptyMacroPoolEmitsNoResultLine(t *testing.T) {
	res := applyMacros(t, ``, newTarget(atem.State{}))
	if len(macroLines(res.Applied, "macros"))+len(macroLines(res.Skipped, "macros")) != 0 {
		t.Fatal(res.Applied, res.Skipped)
	}
}

func TestApplyNoMacroPoolElementEmitsNoResultLine(t *testing.T) {
	res := mustApply(t, mustProfile(t, `<Profile majorVersion="2" minorVersion="1"></Profile>`), newTarget(atem.State{}), macrosOnlyOpts())
	if len(macroLines(res.Applied, "macros"))+len(macroLines(res.Skipped, "macros")) != 0 {
		t.Fatal(res.Applied, res.Skipped)
	}
}

func TestApplyMacrosNoProtocolRoutesToSkipped(t *testing.T) {
	tg := newTarget(atem.State{})
	tg.connected = false
	res := applyMacros(t, `<Macro index="0" name="A" description="">  <Op id="Cut" mixEffectBlockIndex="0"/></Macro>`, tg)
	if len(macroLines(res.Applied, "macros")) != 0 || !slices.ContainsFunc(macroLines(res.Skipped, "macros:"), func(s string) bool {
		return strings.Contains(s, "no connected protocol")
	}) {
		t.Fatal(res.Applied, res.Skipped)
	}
}

// --- test_apply_options_macros_only.py ---------------------------------------------

// restoreFlags is the ApplyOptions fields atemwire has: every bool field
// but the Go additions (ResyncWait, MacroTimeout, Logger are not bools).
func restoreFlags(o *ApplyOptions) map[string]bool {
	out := map[string]bool{}
	v := reflect.ValueOf(o).Elem()
	for i := range v.NumField() {
		if f := v.Field(i); f.Kind() == reflect.Bool {
			out[v.Type().Field(i).Name] = f.Bool()
		}
	}
	return out
}

func TestMacrosOnlyEnablesOnlyRestoreMacros(t *testing.T) {
	o := MacrosOnly()
	for name, v := range restoreFlags(o) {
		if v != (name == "RestoreMacros") {
			t.Fatalf("%s is %v", name, v)
		}
	}
	for _, m := range o.MEs {
		if m.AnySelected() || !slices.Equal(m.USK, []bool{false, false, false, false}) {
			t.Fatalf("%+v", m)
		}
	}
}

func TestMacrosOnlyReturnsFreshInstance(t *testing.T) {
	a, b := MacrosOnly(), MacrosOnly()
	if a == b || &a.MEs[0] == &b.MEs[0] || &a.MEs[0].USK[0] == &b.MEs[0].USK[0] {
		t.Fatal("shared")
	}
	a.MEs[0].USK[0] = true
	if !slices.Equal(b.MEs[0].USK, []bool{false, false, false, false}) {
		t.Fatal("aliased")
	}
}

func TestMacrosOnlyListsEveryApplyOptionsField(t *testing.T) {
	want := []string{"MEs", "RestoreMultiview", "RestoreCameraControl", "RestoreTalkback", "RestoreHyperDecks",
		"RestoreFlyKeyframes", "RestoreDownstreamKeys", "RestoreColorGenerators", "RestoreAudio", "RestoreVideoMode",
		"RestoreInputs", "RestoreMacros", "RestoreAux", "RestoreMediaPlayers", "RestoreSettingsFlags",
		"RestoreMediaPoolImages", "ResyncWait", "MacroTimeout", "Logger"}
	var got []string
	for f := range reflect.TypeOf(ApplyOptions{}).Fields() {
		got = append(got, f.Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ApplyOptions fields changed: update MacrosOnly and this test: %v", got)
	}
}

func TestMixEffectOptionsListsEveryField(t *testing.T) {
	var got []string
	for f := range reflect.TypeOf(MixEffectOptions{}).Fields() {
		got = append(got, f.Name)
	}
	if !slices.Equal(got, []string{"Program", "Preview", "NextTransition", "TransitionStyle", "FadeToBlack", "USK"}) {
		t.Fatalf("MixEffectOptions fields changed: update MixEffectOptionsNone, AnySelected and this test: %v", got)
	}
}

func TestDefaultApplyOptionsDoesNotDisableMacros(t *testing.T) {
	if !DefaultApplyOptions().RestoreMacros {
		t.Fatal("RestoreMacros off")
	}
}

// --- test_wipe_pattern_names.py (the profile half) ------------------------------------

var ascCaptured = map[int]string{0: "LeftToRightBar", 1: "TopToBottomBar", 2: "HorizontalBarnDoor", 3: "VerticalBarnDoor", 6: "DiamondIris"}

func TestProfileTableMatchesASCCapture(t *testing.T) {
	for wire, name := range ascCaptured {
		if wipePatternNames[wire] != name {
			t.Fatalf("wire %d is %q, ASC says %q", wire, wipePatternNames[wire], name)
		}
	}
}

// test_macro_table_matches_asc_capture is the macro codec's (atem/macro).
// The two tables agreeing is checked through the codec's public API:
// TransitionWipePattern (op 0x007D) decodes each wire value to its name.
func TestTheTwoTablesAgreeAcrossAll18(t *testing.T) {
	if len(wipePatternNames) != 18 {
		t.Fatal(len(wipePatternNames))
	}
	for i, name := range wipePatternNames {
		raw := []byte{8, 0, 0x7d, 0, 0, 0, byte(i), 0}
		ops, err := macro.Decode(raw)
		if err != nil || len(ops) != 1 {
			t.Fatal(ops, err)
		}
		if got, _ := ops[0].Get("pattern"); got != name {
			t.Fatalf("wire %d: macro %q, profile %q", i, got, name)
		}
	}
}

func TestNameIntRoundtripForCaptured(t *testing.T) {
	for wire, name := range ascCaptured {
		if wipePatternToInt[name] != wire {
			t.Fatalf("%s", name)
		}
	}
}

func TestNoPhantomNames(t *testing.T) {
	for _, bogus := range []string{"HorizontalBars", "VerticalBars", "HorizontalBar", "VerticalBar", "LeftSlash"} {
		if _, ok := wipePatternToInt[bogus]; ok {
			t.Fatal(bogus)
		}
	}
}
