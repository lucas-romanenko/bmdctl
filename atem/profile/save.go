// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// Profile save: bmdwire v1.3.0 Profile.from_atem (__init__.py) and the
// _build_* functions of atemwire/profile/save.py, each reading the state
// through the display-unit readers (atem/read_*.go) exactly as atemwire
// reads mixerstate.

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
)

// macroDownloadTimeout is download_macro's default timeout.
const macroDownloadTimeout = 10 * time.Second

// Save builds a profile from src's state: Profile.from_atem. opts nil is
// DefaultSaveOptions().
//
// atemwire first waits for the state dump to settle (wait_state_settled);
// there is nothing to wait for here, since a Session's state is complete
// once Dial returns. Then it reads the state once and downloads each used
// macro slot in turn, each within MacroTimeout (10 s); a download that
// fails leaves that macro with its name and description only, as in
// atemwire. Save fails only if ctx ends, which atemwire has no counterpart
// for.
func Save(ctx context.Context, src Source, opts *SaveOptions) (*Profile, error) {
	return newSaver(ctx, src, opts).build()
}

func newSaver(ctx context.Context, src Source, opts *SaveOptions) *saver {
	if opts == nil {
		opts = DefaultSaveOptions()
	}
	st := src.State()
	s := &saver{ctx: ctx, src: src, st: st, opts: opts, log: opts.Logger, stingerSource: st.StingerSource}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s
}

type saver struct {
	ctx  context.Context
	src  Source
	st   atem.State
	opts *SaveOptions
	log  *slog.Logger
	// stingerSource is st.StingerSource. The golden test replaces it to
	// reproduce a test state whose stinger media player (3010) doesn't fit
	// TStP's u8, so no atem.State can hold it.
	stingerSource func(me int) int
}

func (s *saver) build() (*Profile, error) {
	o, st := s.opts, s.st
	root := NewElement("Profile")
	root.Set("majorVersion", strconv.Itoa(MajorVersion))
	root.Set("minorVersion", strconv.Itoa(MinorVersion))
	if product := st.ProductName(""); product != "" {
		root.Set("product", product)
	}
	s.mixEffectBlocks(root)
	if o.DownstreamKeys {
		s.downstreamKeys(root)
	}
	if o.ColorGenerators {
		s.colorGenerators(root)
	}
	if o.Auxiliaries {
		s.auxiliaries(root)
	}
	if o.Settings {
		s.settings(root)
	}
	if o.VideoMode {
		s.videoMode(root)
	}
	if o.HyperDecks {
		s.hyperDecks(root)
	}
	if o.Fairlight {
		s.fairlight(root)
	}
	if o.MediaPlayers {
		s.mediaPlayers(root)
	}
	if o.MediaPoolMetadata {
		s.mediaPoolStills(root)
	}
	if o.CameraControl {
		s.cameraControl(root)
	}
	if o.Macros {
		if err := s.macroPool(root); err != nil {
			return nil, err
		}
		root.SubElement("MacroControl").Set("loop", "False")
	}
	if o.Counters {
		s.counters(root)
	}
	return &Profile{root: root}, nil
}

// --- MixEffectBlocks --------------------------------------------------------

// mixEffectBlocks is _build_mix_effect_blocks: one block per M/E the
// switcher has with anything selected, and no wrapper when none is.
func (s *saver) mixEffectBlocks(root *Element) {
	var selected []int
	for me := range s.st.MECount() {
		if s.opts.MEOptions(me).AnySelected() {
			selected = append(selected, me)
		}
	}
	if len(selected) == 0 {
		return
	}
	blocks := root.SubElement("MixEffectBlocks")
	for _, me := range selected {
		s.mixEffectBlock(blocks, me, s.opts.MEOptions(me))
	}
}

// nextTransitionString is _next_transition_to_str: the selected layers
// comma-joined, "Background" when none is.
func nextTransitionString(sel atem.TransitionSelectionView) string {
	var parts []string
	if sel.Background {
		parts = append(parts, "Background")
	}
	for i, on := range []bool{sel.Key1, sel.Key2, sel.Key3, sel.Key4} {
		if on {
			parts = append(parts, "Key"+strconv.Itoa(i+1))
		}
	}
	if len(parts) == 0 {
		return "Background"
	}
	return strings.Join(parts, ",")
}

func nameOr(m map[int]string, v int, def string) string {
	if n, ok := m[v]; ok {
		return n
	}
	return def
}

// mixEffectBlock is _build_mix_effect_block.
func (s *saver) mixEffectBlock(blocks *Element, me int, m MixEffectOptions) {
	st := s.st
	block := blocks.SubElement("MixEffectBlock")
	block.Set("index", strconv.Itoa(me))
	if m.Program {
		setAttrs(block.SubElement("Program"), "input", st.ProgramSource(me))
	}
	if m.Preview {
		setAttrs(block.SubElement("Preview"), "input", st.PreviewSource(me))
	}
	if m.NextTransition {
		sel := nextTransitionString(st.TransitionSelection(me))
		setAttrs(block.SubElement("NextTransition"), "selection", sel, "nextSelection", sel)
	}
	if m.TransitionStyle {
		ts := block.SubElement("TransitionStyle")
		style := nameOr(transStyleNames, st.TransitionStyle(me), "Mix")
		// atemwire's comment calls the position 0..1; the reader returns
		// the raw 0-10000, which this scales again (int(round(pos*10000))).
		pos := st.TransitionPosition(me)
		setAttrs(ts, "style", style, "nextStyle", style, "previewTransition", false,
			"transitionPosition", int(math.RoundToEven(pos*10000)))

		setAttrs(ts.SubElement("MixParameters"), "rate", st.MixRate(me))
		setAttrs(ts.SubElement("DipParameters"), "rate", st.DipRate(me), "input", st.DipSource(me))
		setAttrs(ts.SubElement("WipeParameters"),
			"rate", st.WipeRate(me),
			"pattern", wipePatternNames[st.WipePattern(me)%len(wipePatternNames)],
			"symmetry", st.WipeSymmetry(me),
			"xPosition", st.WipePositionX(me),
			"yPosition", st.WipePositionY(me),
			"reverseDirection", st.WipeReverse(me),
			"flipFlip", st.WipeFlipFlop(me),
			"borderInput", st.WipeFillSource(me),
			"borderWidth", st.WipeWidth(me),
			"borderSoftness", st.WipeSoftness(me))

		// The stinger source is the media player slot (1-4), written
		// "MediaPlayerN"; anything else as the number.
		mp := s.stingerSource(me)
		source := strconv.Itoa(mp)
		if mp >= 1 && mp <= 4 {
			source = "MediaPlayer" + source
		}
		setAttrs(ts.SubElement("StingerParameters"),
			"source", source,
			"preMultipliedKey", st.StingerPreMultiplied(me),
			"clip", st.StingerClip(me),
			"gain", st.StingerGain(me),
			"invert", st.StingerInvertKey(me),
			"clipDuration", st.StingerClipDuration(me),
			"triggerPoint", st.StingerTriggerPoint(me),
			"mixRate", st.StingerRate(me),
			"preroll", st.StingerPreRoll(me))

		effect := "SqueezeTopLeft"
		if v := st.DVEStyle(me); v != nil {
			effect = nameOr(dveEffectNames, *v, "SqueezeTopLeft")
		}
		setAttrs(ts.SubElement("DVEParameters"),
			"rate", st.DVERate(me),
			"logoRate", st.DVERate(me),
			"reverseDirection", st.DVEReverse(me),
			"flipFlop", st.DVEFlipFlop(me),
			"effect", effect,
			"fillSource", st.DVEFillSource(me),
			"keySource", st.DVEKeySource(me),
			"enableKey", st.DVEEnableKey(me),
			"preMultipliedKey", st.DVEPreMultiplied(me),
			"clip", st.DVEClip(me),
			"gain", st.DVEGain(me),
			"invertKey", st.DVEInvertKey(me))
	}
	if slices.Contains(m.USK, true) {
		keys := block.SubElement("Keys")
		for k := range st.MEKeyerCount(me) {
			if k < len(m.USK) && m.USK[k] {
				s.key(keys, me, k)
			}
		}
	}
	if m.FadeToBlack {
		setAttrs(block.SubElement("FadeToBlack"), "rate", st.FTBRate(me), "isFullyBlack", st.FTBActive(me))
	}
}

// key is _build_key.
func (s *saver) key(parent *Element, me, k int) {
	st := s.st
	mask := st.USKMask(me, k)
	luma := st.USKLuma(me, k)
	chroma := st.USKChroma(me, k)
	pattern := st.USKPattern(me, k)
	dve := st.USKDVE(me, k)
	fly := st.USKFly(me, k)

	key := parent.SubElement("Key")
	setAttrs(key, "index", k,
		"type", nameOr(uskTypeNames, st.USKType(me, k), "Luma"),
		"inputCut", st.USKKeySource(me, k),
		"inputFill", st.USKFillSource(me, k),
		"onAir", st.USKOnAir(me, k),
		"masked", mask.MaskEnabled,
		"maskTop", mask.MaskTop,
		"maskBottom", mask.MaskBottom,
		"maskLeft", mask.MaskLeft,
		"maskRight", mask.MaskRight)

	setAttrs(key.SubElement("LumaParameters"),
		"preMultiplied", luma.LumaPreMultiplied,
		"clip", luma.LumaClip,
		"gain", luma.LumaGain,
		"inverse", luma.LumaInvert)

	cur, sc := chroma.ChromaSamplePosition, chroma.ChromaSampledColor
	setAttrs(key.SubElement("AdvancedChromaParameters"),
		"foregroundLevel", chroma.ChromaForeground*100.0,
		"backgroundLevel", chroma.ChromaBackground*100.0,
		"keyEdge", chroma.ChromaKeyEdge*100.0,
		"spillSuppress", chroma.ChromaSpill*100.0,
		"flareSuppress", chroma.ChromaFlareSuppression*100.0,
		"brightness", chroma.ChromaBrightness,
		"contrast", chroma.ChromaContrast,
		"saturation", chroma.ChromaSaturation*100.0,
		"red", chroma.ChromaRed,
		"green", chroma.ChromaGreen,
		"blue", chroma.ChromaBlue,
		"cursorXPosition", (cur.X-0.5)*32.0,
		"cursorYPosition", (cur.Y-0.5)*18.0,
		"cursorSize", chroma.ChromaSampleSize*100.0,
		"sampledY", float64(sc.YRaw)/10000.0,
		"sampledCb", float64(sc.CbRaw)/10000.0,
		"sampledCr", float64(sc.CrRaw)/10000.0)

	setAttrs(key.SubElement("PatternParameters"),
		"style", wipePatternNames[pattern.PatternStyle%len(wipePatternNames)],
		"inverse", pattern.PatternInvert,
		"size", pattern.PatternSize,
		"symmetry", pattern.PatternSymmetry,
		"softness", pattern.PatternSoftness,
		"xPosition", pattern.PatternPositionX,
		"yPosition", pattern.PatternPositionY)

	setAttrs(key.SubElement("DVEParameters"),
		"maskEnabled", dve.DVEMasked,
		"maskTop", dve.DVETop,
		"maskBottom", dve.DVEBottom,
		"maskLeft", dve.DVELeft,
		"maskRight", dve.DVERight,
		"shadowEnabled", dve.DVEShadow,
		"lightSourceDirection", dve.DVELightDirection,
		"lightSourceAltitude", dve.DVELightAltitude,
		"borderEnabled", dve.DVEBorderEnabled,
		"borderStyle", "None",
		"borderBevelHue", dve.DVEBorderHue,
		"borderBevelSaturation", dve.DVEBorderSaturation,
		"borderBevelLuma", dve.DVEBorderLuma,
		"borderWidthOut", dve.DVEBorderOuterWidth,
		"borderWidthIn", dve.DVEBorderInnerWidth,
		"borderSoftnessOut", dve.DVEBorderOuterSoftness,
		"borderSoftnessIn", dve.DVEBorderInnerSoftness,
		"borderBevelOpacity", dve.DVEBorderOpacity,
		"borderBevelPosition", dve.DVEBorderBevelPosition,
		"borderBevelSoftness", dve.DVEBorderBevelSoftness)

	flyE := key.SubElement("FlyParameters")
	setAttrs(flyE,
		"enabled", fly.FlyEnabled,
		"xPosition", dve.DVEPositionX,
		"yPosition", dve.DVEPositionY,
		"xSize", dve.DVESizeX,
		"ySize", dve.DVESizeY,
		"rotation", dve.DVERotation,
		"rate", dve.DVERate)
	// KeyFrameA and KeyFrameB, each only when that keyframe is stored.
	for _, which := range []string{"A", "B"} {
		kf := st.USKFlyKeyframe(me, k, which)
		if kf == nil {
			continue
		}
		setAttrs(flyE.SubElement("KeyFrame"+which),
			"xSize", kf.SizeX, "ySize", kf.SizeY,
			"xPosition", kf.PosX, "yPosition", kf.PosY,
			"rotation", kf.Rotation,
			"borderWidthOut", kf.BorderOuterWidth,
			"borderWidthIn", kf.BorderInnerWidth,
			"borderSoftnessOut", kf.BorderOuterSoftness,
			"borderSoftnessIn", kf.BorderInnerSoftness,
			"borderBevelSoftness", kf.BorderBevelSoftness,
			"borderBevelPosition", kf.BorderBevelPosition,
			"borderOpacity", kf.BorderOpacity,
			"borderHue", kf.BorderHue,
			"borderSaturation", kf.BorderSaturation,
			"borderLuma", kf.BorderLuma,
			"borderLightSourceDirection", kf.LightDirection,
			"borderLightSourceAltitude", kf.LightAltitude,
			"maskTop", kf.MaskTop, "maskBottom", kf.MaskBottom,
			"maskLeft", kf.MaskLeft, "maskRight", kf.MaskRight)
	}
}

// --- DownstreamKeys, ColorGenerators, Auxiliaries ---------------------------

// downstreamKeys is _build_downstream_keys: one block per DSK the
// switcher has.
func (s *saver) downstreamKeys(root *Element) {
	parent := root.SubElement("DownstreamKeys")
	for idx := range s.st.DSKCount() {
		d := s.st.DSKState(idx)
		setAttrs(parent.SubElement("DownstreamKey"), "index", idx,
			"fillSource", d.FillSource,
			"keySource", d.KeySource,
			"rate", d.Rate,
			"maskEnabled", d.MaskEnabled,
			"maskTop", d.MaskTop,
			"maskBottom", d.MaskBottom,
			"maskLeft", d.MaskLeft,
			"maskRight", d.MaskRight,
			"preMultipliedKey", d.PreMultiplied,
			"clip", d.Clip,
			"gain", d.Gain,
			"invert", d.InvertKey,
			"onAir", d.OnAir,
			"tie", d.Tie)
	}
}

// colorGenerators is _build_color_generators: always two.
func (s *saver) colorGenerators(root *Element) {
	parent := root.SubElement("ColorGenerators")
	for idx := range 2 {
		cg := s.st.ColorGenerator(idx)
		setAttrs(parent.SubElement("ColorGenerator"), "index", idx,
			"hue", cg.Hue, "saturation", cg.Saturation, "luma", cg.Luma)
	}
}

// auxiliaries is _build_auxiliaries: every aux the switcher reported, as
// id 8001 + index.
func (s *saver) auxiliaries(root *Element) {
	parent := root.SubElement("Auxiliaries")
	for idx, a := range s.st.Aux {
		if a != nil {
			setAttrs(parent.SubElement("Auxiliary"), "id", 8001+idx, "input", s.st.AuxSource(idx))
		}
	}
}

// --- Settings ----------------------------------------------------------------

// sortedInputs is sorted(mx['input-properties']).
func (s *saver) sortedInputs() []uint16 {
	return slices.Sorted(maps.Keys(s.st.Inputs))
}

// extPortTypeName is _ext_port_type_name: the lowest bit's name, SDI when
// none is known.
func extPortTypeName(bits int) string {
	for _, t := range extPortTypes {
		if bits&t.bit != 0 {
			return t.name
		}
	}
	return "SDI"
}

// settings is _build_settings: the live SDI level, ftbEnabled, inputs and
// multiviews, and the hardcoded defaults for what atemwire can't read.
func (s *saver) settings(root *Element) {
	st := s.st
	settings := root.SubElement("Settings")
	setAttrs(settings, "abDirect", false, "cameraAux", -1,
		"SDI3GOutputLevel", st.SDI3GLevel("LevelB"),
		"ftbEnabled", !st.FTBDisabled())

	mvm := settings.SubElement("MultiViewVideoModes")
	for _, m := range defaultMultiviewModes {
		setAttrs(mvm.SubElement("MultiViewVideoMode"), "coreVideoMode", m[0], "multiViewVideoMode", m[1])
	}

	for _, which := range []string{"Talkback", "EngineeringTalkback"} {
		tb := settings.SubElement(which)
		tb.Set("sdiMuted", "False")
		inputs := tb.SubElement("TalkbackInputs")
		for i := 1; i <= 10; i++ {
			setAttrs(inputs.SubElement("TalkbackInput"), "audioInputId", i, "sdiMuted", false)
		}
	}

	// External inputs (port type 0). externalPortType comes from
	// source_ports, not external_port_type (SURFACE.md §5.4 item 8).
	inputs := settings.SubElement("Inputs")
	for _, src := range s.sortedInputs() {
		ip := st.Inputs[src]
		if ip == nil || ip.PortType != 0 {
			continue
		}
		ports := int(ip.SourcePorts)
		if ports == 0 {
			ports = 1
		}
		setAttrs(inputs.SubElement("Input"),
			"id", int(src),
			"shortName", ip.ShortName,
			"longName", ip.Name,
			"externalPortType", extPortTypeName(ports))
	}

	clips := settings.SubElement("MediaPool").SubElement("Clips")
	for ci := range 2 {
		setAttrs(clips.SubElement("Clip"), "index", ci, "maxFrameCount", 100)
	}

	mmo := settings.SubElement("MixMinusOutputs")
	for i := range 6 {
		setAttrs(mmo.SubElement("MixMinusOutput"), "index", i, "audioMode", "ProgramOut")
	}

	mvs := settings.SubElement("MultiViews")
	for mvIdx, mvState := range st.Multiviewers {
		props := mvState.Properties
		if props == nil {
			continue
		}
		mv := mvs.SubElement("MultiView")
		setAttrs(mv, "index", mvIdx, "LayoutID", int(props.Layout), "vuMeterOpacity", 1)
		windows := mv.SubElement("Windows")
		for winIdx, w := range mvState.Windows {
			if w.Input == nil {
				continue
			}
			vu := w.VU != nil && w.VU.Enabled
			safe := w.SafeArea != nil && w.SafeArea.Enabled
			setAttrs(windows.SubElement("Window"), "index", winIdx,
				"input", int(w.Input.Source),
				"vuMeterEnabled", vu,
				"safeAreaEnabled", safe)
		}
	}

	bm := settings.SubElement("ButtonMapping")
	for i := range 10 {
		setAttrs(bm.SubElement("Button"), "index", i, "externalInputIndex", i,
			"mappedToCamera", true, "mappedCameraModelName", "Blackmagic Generic")
	}

	// sizeLink is ASC's own state; atemwire writes the element empty.
	settings.SubElement("UpstreamKeys")
}

// --- VideoMode, HyperDecks ---------------------------------------------------

// videoMode is _build_video_mode: the canonical XML name of the mode
// (VIDEO_MODE_XML_NAMES, so 1080p5994 and NTSC_widescreen), 1080p60 before
// the switcher reports one.
func (s *saver) videoMode(root *Element) {
	name := ""
	if vm := s.st.CurrentVideoMode(); vm != nil {
		if n, ok := videoModeXMLNames[vm.ID]; ok {
			name = n
		} else if vm.Format != "" {
			name, _, _ = strings.Cut(vm.Format, " ")
		}
	}
	if name == "" {
		name = "1080p60"
	}
	root.SubElement("VideoMode").Set("videoMode", name)
}

// hyperDecks is _build_hyperdecks: always ten slots; auto-roll isn't on the
// wire, so it is written at its defaults.
func (s *saver) hyperDecks(root *Element) {
	parent := root.SubElement("HyperDecks")
	for i := range 10 {
		hd := s.st.HyperdeckSettings(i)
		setAttrs(parent.SubElement("HyperDeck"), "id", i, "networkAddress", hd.NetworkAddress,
			"input", hd.Input, "autoRoll", false, "autoRollFrameDelay", 0)
	}
}

// --- FairlightAudioMixer -----------------------------------------------------

type stripRef struct {
	id    string
	props *atem.FairlightStripPropertiesField
}

// split is _is_split_strip: one half of a split input (is_split 0xFF).
func (r stripRef) split() bool { return r.props != nil && r.props.IsSplit == 0xFF }

// subchannel is _strip_subchannel: the subchannel in a
// "<source>.<subchannel>" strip id, 0 if it has none.
func (r stripRef) subchannel() int {
	_, sub, _ := strings.Cut(r.id, ".")
	n, err := strconv.Atoi(sub)
	if err != nil {
		return 0
	}
	return n
}

// fairlight is _build_fairlight.
func (s *saver) fairlight(root *Element) {
	st := s.st
	fa := root.SubElement("FairlightAudioMixer")
	masterVolume, afv, eqEnable, eqGain, dynMakeup := 0.0, false, true, 0.0, 0.0
	if m := st.Fairlight.Master; m != nil {
		masterVolume = float64(m.Volume) / 100.0
		afv = m.AFV
		eqEnable = m.EQEnable
		eqGain = float64(m.EQGain) / 100.0
		dynMakeup = float64(m.DynamicsGain) / 100.0
	}
	setAttrs(fa, "masterOutFaderGain", masterVolume, "followFadeToBlack", afv,
		"audioFollowVideoCrossfadeTransition", false)

	eq := fa.SubElement("MasterOutEqualizer")
	setAttrs(eq, "enabled", eqEnable, "gain", eqGain)
	s.eqBands(eq, nil)

	dyn := fa.SubElement("MasterOutDynamicsProcessor")
	setAttrs(dyn, "makeupGain", dynMakeup)
	compressor(dyn, st.FairlightMasterCompressor())
	limiter(dyn, st.FairlightMasterLimiter())

	inputs := fa.SubElement("AudioInputs")
	comp, lim, exp := st.FairlightCompressor(), st.FairlightLimiter(), st.FairlightExpander()

	// fairlight-strip-properties grouped by source, each source's in the
	// subchannel order of their ids (left half first), as ASC writes them.
	bySource := map[int][]stripRef{}
	for id, strip := range st.Fairlight.Strips {
		if strip == nil || strip.Properties == nil {
			continue
		}
		base, _, _ := strings.Cut(id, ".")
		src, err := strconv.Atoi(base)
		if err != nil {
			continue
		}
		bySource[src] = append(bySource[src], stripRef{id, strip.Properties})
	}
	for _, refs := range bySource {
		slices.SortFunc(refs, func(a, b stripRef) int {
			return cmp.Or(cmp.Compare(a.subchannel(), b.subchannel()), strings.Compare(a.id, b.id))
		})
	}

	for _, src := range slices.Sorted(maps.Keys(st.Fairlight.Inputs)) {
		ai := st.Fairlight.Inputs[src]
		if ai == nil {
			continue
		}
		mono := ai.Split == 1
		refs := bySource[int(src)]
		// A dual-mono input reports two split strips, its halves. FAIP's
		// split byte for one wasn't captured, so it isn't read for that.
		halves := 0
		for _, ref := range refs {
			if ref.split() {
				halves++
			}
		}
		config := "Stereo"
		switch {
		case halves > 1:
			config = "DualMono"
		case mono:
			config = "Mono"
		}
		aiE := inputs.SubElement("AudioInput")
		setAttrs(aiE, "configuration", config, "id", int(src))
		if ai.Level == 1 || ai.Level == 2 {
			level := "ProLine"
			if ai.Level == 1 {
				level = "Mic"
			}
			aiE.SubElement("Analog").Set("level", level)
		}

		if len(refs) == 0 {
			refs = []stripRef{{id: fmt.Sprintf("%d.0", src)}}
		}
		for _, ref := range refs {
			s.audioSource(aiE, int(src), mono, ref, comp, lim, exp)
		}
	}

	hpVolume := -15.0
	if hp := st.Fairlight.Headphones; hp != nil {
		hpVolume = float64(hp.Volume) / 100.0
	}
	setAttrs(fa.SubElement("AudioHeadphoneOutputs").SubElement("AudioHeadphoneOutput"),
		"index", 0, "gain", hpVolume, "masterOutGain", -6, "masterOutMute", true,
		"talkbackGain", -10, "talkbackMute", true, "sidetoneGain", -10)
}

// audioSource writes one strip of a source. Its id is the strip's wire
// address as ASC writes it (audioSourceID): -256 or -255 for a split half
// by subchannel, -256 for a mono input's strip (the XLR's, however its
// FASP reports it), -65280 for stereo.
func (s *saver) audioSource(aiE *Element, src int, mono bool, ref stripRef,
	comp map[string]atem.CompressorView, lim map[string]atem.LimiterView, exp map[string]atem.ExpanderView) {
	sp := ref.props
	srcE := aiE.SubElement("AudioSource")
	sid := audioSourceStereoID
	switch {
	case ref.split():
		sid = audioSourceID(ref.subchannel())
	case mono:
		sid = audioSourceSplitID
	}
	volume, gain, pan, mix := -10000, 0, 0, 0x01
	if sp != nil {
		volume, gain, pan, mix = int(sp.Volume), int(sp.Gain), int(sp.Pan), int(sp.State)
	}
	fader := math.Inf(-1)
	if volume > -10000 {
		fader = float64(volume) / 100.0
	}
	setAttrs(srcE, "id", sid,
		"inputGain", float64(gain)/100.0,
		"pan", float64(pan)/100.0,
		"faderGain", fader,
		"mixOption", nameOr(mixOptionNames, mix, "Off"))
	if sp != nil && src >= 1300 {
		srcE.Set("delayFrames", fmtValue(int(sp.Delay)))
	}

	eqE := srcE.SubElement("Equalizer")
	eqEnabled, eqGain := true, 0.0
	if sp != nil {
		eqEnabled, eqGain = sp.EQEnable, float64(sp.EQGain)/100.0
	}
	setAttrs(eqE, "enabled", eqEnabled, "gain", eqGain)
	id := ref.id
	s.eqBands(eqE, &id)

	dynE := srcE.SubElement("DynamicsProcessor")
	dynGain := 0.0
	if sp != nil {
		dynGain = float64(sp.DynamicsGain) / 100.0
	}
	setAttrs(dynE, "makeupGain", dynGain)
	expander(dynE, lookup(exp, id))
	compressor(dynE, lookup(comp, id))
	limiter(dynE, lookup(lim, id))
}

func lookup[V any](m map[string]V, k string) *V {
	if v, ok := m[k]; ok {
		return &v
	}
	return nil
}

// eqBand is the part of an AEBP or AMBP band _build_eq_bands reads.
type eqBand struct {
	enabled           bool
	filter, freqRange int
	frequency         int
	gain, q           int
}

// eqBands is _build_eq_bands: six bands, each the live band where the
// switcher reported one and the reference defaults otherwise. stripID nil
// is the master EQ.
func (s *saver) eqBands(parent *Element, stripID *string) {
	bands := map[int]eqBand{}
	if stripID != nil {
		if strip := s.st.Fairlight.Strips[*stripID]; strip != nil {
			for i, b := range strip.EQBands {
				if b != nil {
					bands[int(i)] = eqBand{b.BandEnabled, int(b.BandFilter), int(b.BandFreqRange), int(b.BandFrequency), int(b.BandGain), int(b.BandQ)}
				}
			}
		}
	} else {
		for i, b := range s.st.Fairlight.MasterEQBands {
			if b != nil {
				bands[int(i)] = eqBand{b.BandEnabled, int(b.BandFilter), int(b.BandFreqRange), int(b.BandFrequency), int(b.BandGain), int(b.BandQ)}
			}
		}
	}
	for _, d := range defaultEQBandsByIndex {
		en, shape, freqRange, freq := d.enabled, d.shape, d.freqRange, d.frequency
		var gain, q any = 0, d.q
		if b, ok := bands[d.index]; ok {
			en = b.enabled
			if n, ok := eqShapeNames[b.filter]; ok {
				shape = n
			}
			if n, ok := eqFreqRangeNames[b.freqRange]; ok {
				freqRange = n
			}
			freq = b.frequency
			gain = float64(b.gain) / 100.0
			q = float64(b.q) / 100.0
		}
		setAttrs(parent.SubElement("EqualizerBand"),
			"index", d.index, "enabled", en, "shape", shape,
			"frequencyRange", freqRange, "frequency", freq,
			"gain", gain, "qFactor", q)
	}
}

// compressor is _build_compressor: the live block, else the defaults.
func compressor(parent *Element, b *atem.CompressorView) {
	e := parent.SubElement("Compressor")
	if b == nil {
		setAttrs(e, "enabled", false, "threshold", -35, "ratio", 2, "attack", 1.4, "hold", 0, "release", 93)
		return
	}
	setAttrs(e, "enabled", b.Enabled, "threshold", b.ThresholdDB, "ratio", b.Ratio,
		"attack", b.AttackMS, "hold", b.HoldMS, "release", b.ReleaseMS)
}

// limiter is _build_limiter.
func limiter(parent *Element, b *atem.LimiterView) {
	e := parent.SubElement("Limiter")
	if b == nil {
		setAttrs(e, "enabled", false, "threshold", -12, "attack", 0.71, "hold", 0, "release", 93)
		return
	}
	setAttrs(e, "enabled", b.Enabled, "threshold", b.ThresholdDB,
		"attack", b.AttackMS, "hold", b.HoldMS, "release", b.ReleaseMS)
}

// expander is _build_expander.
func expander(parent *Element, b *atem.ExpanderView) {
	e := parent.SubElement("Expander")
	if b == nil {
		setAttrs(e, "enabled", false, "gateMode", false, "threshold", -45, "range", 18,
			"ratio", 1.1, "attack", 1.4, "hold", 0, "release", 93)
		return
	}
	setAttrs(e, "enabled", b.Enabled, "gateMode", b.Mode == "gate",
		"threshold", b.ThresholdDB, "range", b.RangeDB, "ratio", b.Ratio,
		"attack", b.AttackMS, "hold", b.HoldMS, "release", b.ReleaseMS)
}

// --- MediaPlayers, MediaPool, CameraControl ---------------------------------

// mediaPlayers is _build_media_players: each reported player, or two
// stills at slot 0 before any.
func (s *saver) mediaPlayers(root *Element) {
	parent := root.SubElement("MediaPlayers")
	var players []int
	for i, p := range s.st.MediaPlayers {
		if p != nil {
			players = append(players, i)
		}
	}
	if len(players) == 0 {
		for i := range 2 {
			setAttrs(parent.SubElement("MediaPlayer"), "index", i, "sourceType", "Still", "sourceIndex", 0)
		}
		return
	}
	for _, i := range players {
		sel := s.st.MediaplayerSelected(i)
		kind := "Clip"
		if sel.SourceType == 1 {
			kind = "Still"
		}
		setAttrs(parent.SubElement("MediaPlayer"), "index", i, "sourceType", kind, "sourceIndex", sel.Slot)
	}
}

// mediaPoolStills is _build_media_pool_stills: each used slot's name and
// ASC's path for it; no pixels.
func (s *saver) mediaPoolStills(root *Element) {
	stills := root.SubElement("MediaPool").SubElement("Stills")
	for _, idx := range slices.Sorted(maps.Keys(s.st.MediaPool.Files)) {
		info := s.st.MediaplayerSlotInfo(int(idx))
		if !info.IsUsed {
			continue
		}
		path := ""
		if info.Name != "" {
			path = "ATEM Media Pool/" + info.Name + ".png"
		}
		setAttrs(stills.SubElement("Still"), "index", int(idx), "name", info.Name, "path", path)
	}
}

// cameraControl is _build_camera_control: the same hardcoded defaults for
// each external input (cameras 1-10 when there are none); nothing is read
// from the switcher.
func (s *saver) cameraControl(root *Element) {
	parent := root.SubElement("CameraControl")
	var cameras []int
	for _, src := range s.sortedInputs() {
		if ip := s.st.Inputs[src]; ip != nil && ip.PortType == 0 {
			cameras = append(cameras, int(src))
		}
	}
	if len(cameras) == 0 {
		for i := 1; i <= 10; i++ {
			cameras = append(cameras, i)
		}
	}
	param := func(d int, cat, name string, kv ...any) {
		setAttrs(parent.SubElement("Parameter"), append([]any{"device", d, "category", cat, "parameter", name}, kv...)...)
	}
	for _, d := range cameras {
		param(d, "Lens", "ApertureFstop", "value", 5)
		param(d, "Video", "ManualWhiteBalance", "temperature", 5600, "tint", 0)
		param(d, "Video", "Exposure", "value", 20000)
		param(d, "Video", "DetailLevel", "value", 1)
		param(d, "Video", "SensorGainInDecibels", "value", 0)
		param(d, "Video", "SensorNDFilter", "value", 0)
		param(d, "ColorCorrection", "LiftAdjust", "red", 0, "green", 0, "blue", 0, "luma", 0)
		param(d, "ColorCorrection", "GammaAdjust", "red", 0, "green", 0, "blue", 0, "luma", 0)
		param(d, "ColorCorrection", "GainAdjust", "red", 1, "green", 1, "blue", 1, "luma", 1)
		param(d, "ColorCorrection", "OffsetAdjust", "red", 0, "green", 0, "blue", 0, "luma", 0)
		param(d, "ColorCorrection", "ContrastAdjust", "pivot", 0.5, "adjust", 1)
		param(d, "ColorCorrection", "LumaMix", "value", 1)
		param(d, "ColorCorrection", "ColorAdjust", "hue", 0, "saturation", 1)
	}
	for _, d := range cameras {
		setAttrs(parent.SubElement("Property"), "device", d, "property", "ApertureCoarse", "value", 1)
		setAttrs(parent.SubElement("Property"), "device", d, "property", "Locked", "value", false)
	}
}

// --- MacroPool, Counters -----------------------------------------------------

// macroPool is _build_macro_pool: each used slot's name and description,
// and its ops, downloaded and decoded. A slot whose download fails keeps
// its metadata only.
func (s *saver) macroPool(root *Element) error {
	pool := root.SubElement("MacroPool")
	props := s.st.Macros.Properties
	timeout := s.opts.MacroTimeout
	if timeout <= 0 {
		timeout = macroDownloadTimeout
	}
	for _, idx := range slices.Sorted(maps.Keys(props)) {
		m := s.st.MacroEntry(int(idx))
		if !m.IsUsed {
			continue
		}
		e := pool.SubElement("Macro")
		desc := ""
		if node := props[idx]; node != nil {
			desc = decodeName(node.Description)
		}
		setAttrs(e, "index", int(idx), "name", m.Name, "description", desc)

		if err := s.ctx.Err(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(s.ctx, timeout)
		raw, err := s.src.DownloadMacro(ctx, int(idx))
		cancel()
		if err != nil {
			s.log.Warn("macro download failed; saving metadata only", "slot", idx, "err", err)
			continue
		}
		if len(raw) == 0 {
			continue
		}
		ops, err := macro.Decode(raw)
		if err != nil {
			// atemwire's decoder logs this and returns the ops before it.
			s.log.Warn("macro bytecode decode stopped early", "slot", idx, "err", err)
		}
		for _, op := range ops {
			opE := e.SubElement("Op")
			opE.Set("id", op.ID)
			for _, p := range op.Params {
				if p.Name != "id" {
					opE.Set(p.Name, p.Value)
				}
			}
		}
	}
	return nil
}

// counters is _build_counters: one hardcoded counter.
func (s *saver) counters(root *Element) {
	setAttrs(root.SubElement("Counters").SubElement("Counter"), "index", 0, "enabled", false,
		"mode", "Countdown", "autoHide", false, "positionX", 0, "positionY", -6.3,
		"size", 25, "opacity", 100, "startFromHours", 0, "startFromMinutes", 0,
		"startFromSeconds", 0, "startFromFrames", 0)
}
