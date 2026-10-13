// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// Profile apply: bmdwire v1.3.0 Profile.apply (__init__.py) and the
// _apply_* functions of atemwire/profile/apply.py, section by section in
// atemwire's order, each sending what atemwire's operations send.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
)

const (
	// resyncWait is the pause after a video mode change (apply.py's
	// time.sleep(3.0)).
	resyncWait = 3 * time.Second
	// macroUploadTimeout is upload_macro_bytecode's timeout.
	macroUploadTimeout = 10 * time.Second
)

// Apply restores the profile to t: Profile.apply. opts nil is
// DefaultApplyOptions().
//
// Sections run in atemwire's order: video_mode, inputs, settings_flags,
// color_generators, aux, transition, upstream_keyers, downstream_keys,
// fade_to_black, media_players, audio, multiview (no setter), macros,
// camera_control (no setter), hyperdecks, talkback (no setter),
// program_preview. A failing operation is recorded in the result and the
// apply carries on; a command that only fails to encode is dropped, as
// atemwire's connection drops it. There is no model or version guard.
//
// Like atemwire, Apply blocks only for the pause after a video mode change
// (ResyncWait, 3 s) and for macro uploads (MacroTimeout, 10 s each).
//
// It returns an error, with the result so far, where atemwire's apply
// raises instead of returning: an _int of an infinite attribute
// (OverflowError, an *Error) and a macro op whose encoding fails with
// anything but ValueError (a *macro.Error; SURFACE.md §5.4 item 8). It
// also stops with ctx's error if ctx ends, which atemwire can't.
func (p *Profile) Apply(ctx context.Context, t Target, opts *ApplyOptions) (*ApplyResult, error) {
	a := p.newApplier(ctx, t, opts)
	err := a.run()
	return a.res, err
}

func (p *Profile) newApplier(ctx context.Context, t Target, opts *ApplyOptions) *applier {
	if opts == nil {
		opts = DefaultApplyOptions()
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &applier{ctx: ctx, t: t, root: p.root, opts: opts, log: log, res: &ApplyResult{log: log}, sleep: sleepCtx}
}

type applier struct {
	ctx  context.Context
	t    Target
	root *Element
	opts *ApplyOptions
	log  *slog.Logger
	res  *ApplyResult
	// sleep is the resync pause; tests record it instead.
	sleep func(ctx context.Context, d time.Duration) error
	// onDrop, if set, hears of each command dropped as unencodable.
	onDrop func(opcode string)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// safe is _safe: record what an operation raised and carry on.
func (a *applier) safe(section string, err error) {
	if err != nil {
		a.res.NoteError(section, err)
	}
}

func anyME(mes []MixEffectOptions, f func(MixEffectOptions) bool) bool {
	return slices.ContainsFunc(mes, f)
}

func (a *applier) run() error {
	o := a.opts
	type section struct {
		label  string
		on     bool
		action func() error
		reason string // "" is atemwire's None: quiet when gated off
	}
	sections := []section{
		{"video_mode", o.RestoreVideoMode, a.videoMode, "gated off (option default)"},
		{"inputs", o.RestoreInputs, a.inputs, "gated off (no setter op)"},
		{"settings_flags", o.RestoreSettingsFlags, a.settingsFlags, ""},
		{"color_generators", o.RestoreColorGenerators, a.colorGenerators, ""},
		{"aux", o.RestoreAux, a.auxiliaries, ""},
		{"transition", anyME(o.MEs, func(m MixEffectOptions) bool { return m.NextTransition || m.TransitionStyle }), a.transition, ""},
		{"upstream_keyers", anyME(o.MEs, func(m MixEffectOptions) bool { return slices.Contains(m.USK, true) }), a.upstreamKeyers, ""},
		{"downstream_keys", o.RestoreDownstreamKeys, a.downstreamKeys, ""},
		{"fade_to_black", anyME(o.MEs, func(m MixEffectOptions) bool { return m.FadeToBlack }), a.fadeToBlack, ""},
		{"media_players", o.RestoreMediaPlayers, a.mediaPlayers, ""},
		{"audio", o.RestoreAudio, a.fairlight, "gated off (option default)"},
		{"multiview", o.RestoreMultiview, nil, "multiview ops not implemented (gap)"},
		{"macros", o.RestoreMacros, a.macros, "gated off (option default)"},
		{"camera_control", o.RestoreCameraControl, nil, "camera control ops not implemented (gap)"},
		{"hyperdecks", o.RestoreHyperDecks, a.hyperDecks, ""},
		{"talkback", o.RestoreTalkback, nil, "talkback ops not implemented (gap)"},
		{"program_preview", anyME(o.MEs, func(m MixEffectOptions) bool { return m.Program || m.Preview }), a.programPreview, "gated off (default — would cut to air)"},
	}
	for _, s := range sections {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		switch {
		case s.action == nil:
			reason := "gated off (option default)"
			if s.on {
				reason = s.reason
			}
			a.res.NoteSkipped(s.label, reason)
		case s.on:
			if err := s.action(); err != nil {
				return err
			}
		case s.reason != "":
			a.res.NoteSkipped(s.label, s.reason)
		}
	}
	return nil
}

// eachMEBlock is _iter_me_blocks: every <MixEffectBlock> whose index the
// switcher has (its M/E count read once), skipping the rest silently.
func (a *applier) eachMEBlock(f func(me int, block *Element) error) error {
	live := a.t.State().MECount()
	for _, block := range a.root.FindAll("MixEffectBlocks/MixEffectBlock") {
		me, err := attrInt(block.attr("index"), 0)
		if err != nil {
			return err
		}
		if me < 0 || me >= float64(live) {
			a.log.Debug("skipping MixEffectBlock the switcher doesn't have", "index", block.GetDefault("index", ""), "mes", live)
			continue
		}
		if err := f(int(me), block); err != nil {
			return err
		}
	}
	return nil
}

// --- video mode, inputs, settings, colour, aux, HyperDecks ------------------

// videoMode is _apply_video_mode: the saved mode by number (canonical
// names and atemwire's old decimal labels), nothing if the switcher is
// already there, and the resync pause after a change.
func (a *applier) videoMode() error {
	e := a.root.Find("VideoMode")
	if e == nil {
		return nil
	}
	target := e.GetDefault("videoMode", "")
	if target == "" {
		a.res.NoteSkipped("video_mode", "empty videoMode in profile")
		return nil
	}
	switch target {
	case sdLabelWithoutAspect:
		a.log.Warn(fmt.Sprintf("Profile.apply: videoMode %s was written by atemwire before 1.3.0 for both NTSC 4:3 and 16:9; leaving the video mode unchanged.", pyRepr(target)))
		a.res.NoteSkipped("video_mode", pyRepr(target)+" does not record 4:3 or 16:9")
		return nil
	case sdLabelAmbiguousBefore13:
		a.log.Warn(fmt.Sprintf("Profile.apply: videoMode %s is ambiguous in files saved by atemwire before 1.3.0 (written for both PAL 4:3 and 16:9); applying it as PAL 4:3, as ATEM Software Control means it.", pyRepr(target)))
	}
	targetMode, ok := videoModeNumber(target)
	if !ok {
		a.res.NoteSkipped("video_mode", "unrecognized mode "+pyRepr(target))
		return nil
	}

	label, current, known := "", 0, false
	if cur := a.t.State().CurrentVideoMode(); cur != nil {
		label, _, _ = strings.Cut(cur.Format, " ")
		if _, ok := videoModeXMLNames[cur.ID]; ok {
			current, known = cur.ID, true
		}
	}
	if !known {
		current, known = videoModeNumber(label)
	}
	if known && current == targetMode {
		a.res.NoteSkipped("video_mode", "already at "+target)
		return nil
	}

	from := label
	if from == "" {
		from = "?"
	}
	a.log.Warn(fmt.Sprintf("Profile.apply changing video mode %s → %s. Outputs will drop briefly while the ATEM resyncs.", from, target))
	if err := a.setVideoMode(targetMode); err != nil {
		a.res.NoteError("video_mode", err)
		return nil
	}
	wait := a.opts.ResyncWait
	if wait == 0 {
		wait = resyncWait
	}
	if wait > 0 {
		if err := a.sleep(a.ctx, wait); err != nil {
			return err
		}
	}
	a.res.NoteApplied("video_mode (-> " + target + ")")
	return nil
}

// inputs is _apply_inputs: one CInL per <Input> with a positive id and at
// least one label.
func (a *applier) inputs() error {
	settings := a.root.Find("Settings")
	if settings == nil {
		return nil
	}
	inputsE := settings.Find("Inputs")
	if inputsE == nil {
		return nil
	}
	n := 0
	for _, in := range inputsE.FindAll("Input") {
		src, err := attrInt(in.attr("id"), 0)
		if err != nil {
			return err
		}
		if src <= 0 {
			continue
		}
		long, short := in.attr("longName"), in.attr("shortName")
		if long == nil && short == nil {
			continue
		}
		if err := a.setInputLabel(src, long, short); err != nil {
			a.res.NoteError("inputs["+intString(src)+"]", err)
			continue
		}
		n++
	}
	if n > 0 {
		a.res.NoteApplied(fmt.Sprintf("inputs (%d)", n))
	}
	return nil
}

// settingsFlags is _apply_settings_flags: ftbEnabled only.
func (a *applier) settingsFlags() error {
	settings := a.root.Find("Settings")
	if settings == nil {
		return nil
	}
	enabled := attrBool(settings.attr("ftbEnabled"), true)
	a.safe("settings.ftbEnabled", a.send(atem.FadeToBlackEnable{Enabled: enabled}))
	a.res.NoteApplied("settings.ftbEnabled")
	return nil
}

func (a *applier) colorGenerators() error {
	parent := a.root.Find("ColorGenerators")
	if parent == nil {
		return nil
	}
	for _, cg := range parent.FindAll("ColorGenerator") {
		idx, err := attrInt(cg.attr("index"), 0)
		if err != nil {
			return err
		}
		label := "color_gen[" + intString(idx) + "]"
		hue := attrFloat(cg.attr("hue"), 0)
		a.safe(label+".hue", a.setColorGenerator(idx, func(c *atem.ColorGenerator) { c.Hue = &hue }))
		sat := attrFloat(cg.attr("saturation"), 0)
		a.safe(label+".saturation", a.setColorGenerator(idx, func(c *atem.ColorGenerator) { c.Saturation = &sat }))
		luma := attrFloat(cg.attr("luma"), 0)
		a.safe(label+".luma", a.setColorGenerator(idx, func(c *atem.ColorGenerator) { c.Luma = &luma }))
	}
	a.res.NoteApplied("color_generators")
	return nil
}

// auxiliaries is _apply_auxiliaries: id 8001 + n is aux n.
func (a *applier) auxiliaries() error {
	parent := a.root.Find("Auxiliaries")
	if parent == nil {
		return nil
	}
	for _, aux := range parent.FindAll("Auxiliary") {
		id, err := attrInt(aux.attr("id"), 0)
		if err != nil {
			return err
		}
		idx := id - 8001
		if idx < 0 {
			continue
		}
		src, err := attrInt(aux.attr("input"), 0)
		if err != nil {
			return err
		}
		a.safe("aux["+intString(idx)+"]", a.setAuxOutput(idx, src))
	}
	a.res.NoteApplied("aux_outputs")
	return nil
}

// hyperDecks is _apply_hyperdecks: every slot, so slots saved as 0.0.0.0
// are cleared (SURFACE.md §5.4 item 8); auto-roll isn't on the wire.
func (a *applier) hyperDecks() error {
	parent := a.root.Find("HyperDecks")
	if parent == nil {
		return nil
	}
	n := 0
	for _, e := range parent.FindAll("HyperDeck") {
		slot, err := attrInt(e.attr("id"), -1)
		if err != nil {
			return err
		}
		if slot < 0 {
			continue
		}
		ip := e.GetDefault("networkAddress", "")
		if ip == "" {
			ip = "0.0.0.0"
		}
		ip = pyStrip(ip)
		input, err := attrInt(e.attr("input"), 0)
		if err != nil {
			return err
		}
		a.safe("hyperdeck["+intString(slot)+"]", a.setHyperdeckSettings(slot, ip, input))
		if ip != "" && ip != "0.0.0.0" {
			n++
		}
	}
	a.res.NoteApplied(fmt.Sprintf("hyperdecks (%d)", n))
	return nil
}

// --- transition ---------------------------------------------------------------

// transition is _apply_transition: per M/E, the style half and the
// next-transition half each gated by their own flag.
func (a *applier) transition() error {
	didStyle, didNext := false, false
	err := a.eachMEBlock(func(me int, block *Element) error {
		m := a.opts.MEOptions(me)
		if err := a.transitionBlock(me, block, m); err != nil {
			return err
		}
		didStyle = didStyle || m.TransitionStyle
		didNext = didNext || m.NextTransition
		return nil
	})
	if err != nil {
		return err
	}
	switch {
	case didStyle && didNext:
		a.res.NoteApplied("transition")
	case didStyle:
		a.res.NoteApplied("transition_style")
	case didNext:
		a.res.NoteApplied("next_transition")
	}
	return nil
}

// stingerSourceIndex reads <StingerParameters source>: "MediaPlayerN" or
// a bare number, int() rules; nil when neither parses.
func stingerSourceIndex(s string) (float64, bool) {
	if rest, ok := strings.CutPrefix(s, "MediaPlayer"); ok {
		return pyInt(rest)
	}
	return pyInt(s)
}

func (a *applier) transitionBlock(me int, block *Element, m MixEffectOptions) error {
	ctx := fmt.Sprintf("me[%d]", me)
	ts := block.Find("TransitionStyle")
	if m.TransitionStyle && ts != nil {
		style := transStyleToInt[ts.GetDefault("style", "Mix")]
		a.safe(ctx+".transition.style", a.setTransitionStyle(me, style))

		if mix := ts.Find("MixParameters"); mix != nil {
			rate, err := attrInt(mix.attr("rate"), 25)
			if err != nil {
				return err
			}
			a.safe(ctx+".mix_rate", a.setMixRate(me, rate))
		}

		if dip := ts.Find("DipParameters"); dip != nil {
			rate, err := attrInt(dip.attr("rate"), 25)
			if err != nil {
				return err
			}
			a.safe(ctx+".dip_rate", a.setDipRate(me, rate))
			src, err := attrInt(dip.attr("input"), 0)
			if err != nil {
				return err
			}
			a.safe(ctx+".dip_source", a.setDipSource(me, src))
		}

		if wipe := ts.Find("WipeParameters"); wipe != nil {
			rate, err := attrInt(wipe.attr("rate"), 25)
			if err != nil {
				return err
			}
			a.safe(ctx+".wipe_rate", a.setWipeRate(me, rate))
			pattern := uint8(wipePatternToInt[wipe.GetDefault("pattern", "LeftToRightBar")])
			a.safe(ctx+".wipe_pattern", a.wipe(me, func(c *atem.WipeSettings) { c.Pattern = &pattern }))
			a.safe(ctx+".wipe_symmetry", a.setWipeFloat(me, attrFloat(wipe.attr("symmetry"), 50.0),
				func(c *atem.WipeSettings, v *float64) { c.Symmetry = v }))
			a.safe(ctx+".wipe_softness", a.setWipeFloat(me, attrFloat(wipe.attr("borderSoftness"), 0.0),
				func(c *atem.WipeSettings, v *float64) { c.Softness = v }))
			a.safe(ctx+".wipe_width", a.setWipeFloat(me, attrFloat(wipe.attr("borderWidth"), 0.0),
				func(c *atem.WipeSettings, v *float64) { c.Width = v }))
			a.safe(ctx+".wipe_position_x", a.setWipeFloat(me, attrFloat(wipe.attr("xPosition"), 0.5),
				func(c *atem.WipeSettings, v *float64) { c.PositionX = v }))
			a.safe(ctx+".wipe_position_y", a.setWipeFloat(me, attrFloat(wipe.attr("yPosition"), 0.5),
				func(c *atem.WipeSettings, v *float64) { c.PositionY = v }))
			reverse := attrBool(wipe.attr("reverseDirection"), false)
			a.safe(ctx+".wipe_reverse", a.wipe(me, func(c *atem.WipeSettings) { c.Reverse = &reverse }))
			flip := attrBool(wipe.attr("flipFlip"), false)
			a.safe(ctx+".wipe_flip_flop", a.wipe(me, func(c *atem.WipeSettings) { c.FlipFlop = &flip }))
			src, err := attrInt(wipe.attr("borderInput"), 0)
			if err != nil {
				return err
			}
			a.safe(ctx+".wipe_fill_source", a.setWipeSource(me, src))
		}

		if sting := ts.Find("StingerParameters"); sting != nil {
			// The media player number, "MediaPlayerN" or bare; the wire
			// field is the same 1-based slot.
			if mp, ok := stingerSourceIndex(sting.GetDefault("source", "")); ok {
				a.safe(ctx+".stinger.source", a.setStingerSource(me, mp))
			}
			a.safe(ctx+".stinger.clip", a.setStingerFloat(me, attrFloat(sting.attr("clip"), 50.0),
				func(c *atem.StingerSettings, v *float64) { c.KeyClip = v }))
			a.safe(ctx+".stinger.gain", a.setStingerFloat(me, attrFloat(sting.attr("gain"), 70.0),
				func(c *atem.StingerSettings, v *float64) { c.KeyGain = v }))
			invert := attrBool(sting.attr("invert"), false)
			a.safe(ctx+".stinger.invert_key", a.stinger(me, func(c *atem.StingerSettings) { c.KeyInvert = &invert }))
			pre := attrBool(sting.attr("preMultipliedKey"), false)
			a.safe(ctx+".stinger.pre_multiplied", a.stinger(me, func(c *atem.StingerSettings) { c.KeyPremultiplied = &pre }))
			for _, f := range []struct {
				label, attr string
				def         float64
				set         func(*atem.StingerSettings, *uint16)
			}{
				{"clip_duration", "clipDuration", 73, func(c *atem.StingerSettings, v *uint16) { c.Duration = v }},
				{"trigger_point", "triggerPoint", 34, func(c *atem.StingerSettings, v *uint16) { c.TriggerPoint = v }},
				{"mix_rate", "mixRate", 5, func(c *atem.StingerSettings, v *uint16) { c.Rate = v }},
				{"pre_roll", "preroll", 0, func(c *atem.StingerSettings, v *uint16) { c.Preroll = v }},
			} {
				frames, err := attrInt(sting.attr(f.attr), f.def)
				if err != nil {
					return err
				}
				a.safe(ctx+".stinger."+f.label, a.setStingerFrames(me, frames, f.set))
			}
		}

		if dvet := ts.Find("DVEParameters"); dvet != nil {
			rate, err := attrInt(dvet.attr("rate"), 25)
			if err != nil {
				return err
			}
			a.safe(ctx+".dve_t.rate", a.setDVERate(me, rate))
			fill, err := attrInt(dvet.attr("fillSource"), 0)
			if err != nil {
				return err
			}
			a.safe(ctx+".dve_t.fill_source", a.setDVESource(me, fill, func(c *atem.DVESettings, v *uint16) { c.FillSource = v }))
			key, err := attrInt(dvet.attr("keySource"), 0)
			if err != nil {
				return err
			}
			a.safe(ctx+".dve_t.key_source", a.setDVESource(me, key, func(c *atem.DVESettings, v *uint16) { c.KeySource = v }))
			enable := attrBool(dvet.attr("enableKey"), false)
			a.safe(ctx+".dve_t.enable_key", a.dve(me, func(c *atem.DVESettings) { c.KeyEnable = &enable }))
			a.safe(ctx+".dve_t.clip", a.setDVEFloat(me, attrFloat(dvet.attr("clip"), 50.0),
				func(c *atem.DVESettings, v *float64) { c.KeyClip = v }))
			a.safe(ctx+".dve_t.gain", a.setDVEFloat(me, attrFloat(dvet.attr("gain"), 70.0),
				func(c *atem.DVESettings, v *float64) { c.KeyGain = v }))
			invert := attrBool(dvet.attr("invertKey"), false)
			a.safe(ctx+".dve_t.invert_key", a.dve(me, func(c *atem.DVESettings) { c.KeyInvert = &invert }))
			pre := attrBool(dvet.attr("preMultipliedKey"), false)
			a.safe(ctx+".dve_t.pre_multiplied", a.dve(me, func(c *atem.DVESettings) { c.KeyPremultiplied = &pre }))
			// An effect name the table doesn't know is left alone.
			if eff, ok := dveEffectToInt[dvet.GetDefault("effect", "")]; ok {
				style := uint8(eff)
				a.safe(ctx+".dve_t.style", a.dve(me, func(c *atem.DVESettings) { c.Style = &style }))
			}
			reverse := attrBool(dvet.attr("reverseDirection"), false)
			a.safe(ctx+".dve_t.reverse", a.dve(me, func(c *atem.DVESettings) { c.Reverse = &reverse }))
			flip := attrBool(dvet.attr("flipFlop"), false)
			a.safe(ctx+".dve_t.flip_flop", a.dve(me, func(c *atem.DVESettings) { c.FlipFlop = &flip }))
		}
	}

	// The selection mask goes out absolutely, in one command.
	if m.NextTransition {
		if nt := block.Find("NextTransition"); nt != nil {
			a.safe(ctx+".next_transition", a.setNextTransitionLayers(me, nextTransitionLayers(nt.GetDefault("selection", "Background"))))
		}
	}
	return nil
}

// nextTransitionLayers is _str_to_next_transition: Background, Key1-Key4
// from the comma-separated list, whitespace around each ignored.
func nextTransitionLayers(s string) [5]bool {
	parts := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = pyStrip(p); p != "" {
			parts[p] = true
		}
	}
	return [5]bool{parts["Background"], parts["Key1"], parts["Key2"], parts["Key3"], parts["Key4"]}
}

// --- upstream keyers ----------------------------------------------------------

// upstreamKeyers is _apply_upstream_keyers: per M/E, each <Key> its
// usk[i] selects.
func (a *applier) upstreamKeyers() error {
	kept := map[int]string{}
	err := a.eachMEBlock(func(me int, block *Element) error {
		m := a.opts.MEOptions(me)
		keys := block.Find("Keys")
		if keys == nil {
			return nil
		}
		n, err := a.uskBlock(me, keys, m)
		if err != nil {
			return err
		}
		if n > 0 {
			var on []string
			for i, u := range m.USK {
				if u {
					on = append(on, fmt.Sprint(i+1))
				}
			}
			kept[me] = strings.Join(on, ", ")
		}
		return nil
	})
	if err != nil {
		return err
	}
	switch {
	case len(kept) == 0:
	case len(kept) == 1 && hasKey(kept, 0):
		a.res.NoteApplied("upstream_keyers (" + kept[0] + ")")
	default:
		var parts []string
		for _, me := range sortedKeys(kept) {
			parts = append(parts, fmt.Sprintf("M/E%d: %s", me+1, kept[me]))
		}
		a.res.NoteApplied("upstream_keyers (" + strings.Join(parts, "; ") + ")")
	}
	return nil
}

func hasKey[V any](m map[int]V, k int) bool { _, ok := m[k]; return ok }

func sortedKeys[V any](m map[int]V) []int {
	var ks []int
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// The per-keyer field tables of apply.py: XML attribute, the label
// atemwire uses for it, and the command field it sets.

type dveFloat struct {
	attr, label string
	set         func(*atem.KeyPropertiesDVE, *float64)
}

var (
	// _USK_DVE_MASK_EDGES (labelled by attribute).
	uskDVEMaskEdges = []dveFloat{
		{"maskTop", "maskTop", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskTop = v }},
		{"maskBottom", "maskBottom", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskBottom = v }},
		{"maskLeft", "maskLeft", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskLeft = v }},
		{"maskRight", "maskRight", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskRight = v }},
	}
	// _USK_DVE_FLOATS (labelled by keyword).
	uskDVEFloats = []dveFloat{
		{"lightSourceDirection", "direction", func(c *atem.KeyPropertiesDVE, v *float64) { c.Angle = v }},
		{"lightSourceAltitude", "altitude", func(c *atem.KeyPropertiesDVE, v *float64) { c.Altitude = v }},
		{"borderBevelHue", "hue", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderHue = v }},
		{"borderBevelSaturation", "saturation", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderSaturation = v }},
		{"borderBevelLuma", "luma", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderLuma = v }},
		{"borderWidthOut", "outer_width", func(c *atem.KeyPropertiesDVE, v *float64) { c.OuterWidth = v }},
		{"borderWidthIn", "inner_width", func(c *atem.KeyPropertiesDVE, v *float64) { c.InnerWidth = v }},
		{"borderSoftnessOut", "outer_softness", func(c *atem.KeyPropertiesDVE, v *float64) { c.OuterSoftness = v }},
		{"borderSoftnessIn", "inner_softness", func(c *atem.KeyPropertiesDVE, v *float64) { c.InnerSoftness = v }},
		{"borderBevelOpacity", "opacity", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderOpacity = v }},
		{"borderBevelPosition", "bevel_position", func(c *atem.KeyPropertiesDVE, v *float64) { c.BevelPosition = v }},
		{"borderBevelSoftness", "bevel_softness", func(c *atem.KeyPropertiesDVE, v *float64) { c.BevelSoftness = v }},
	}
	// _USK_FLY_GEOMETRY (labelled by attribute).
	uskFlyGeometry = []dveFloat{
		{"xPosition", "xPosition", func(c *atem.KeyPropertiesDVE, v *float64) { c.PosX = v }},
		{"yPosition", "yPosition", func(c *atem.KeyPropertiesDVE, v *float64) { c.PosY = v }},
		{"xSize", "xSize", func(c *atem.KeyPropertiesDVE, v *float64) { c.SizeX = v }},
		{"ySize", "ySize", func(c *atem.KeyPropertiesDVE, v *float64) { c.SizeY = v }},
		{"rotation", "rotation", func(c *atem.KeyPropertiesDVE, v *float64) { c.Rotation = v }},
	}
	// _FLY_KEYFRAME_GEOMETRY (labelled by attribute): the keyframe
	// attributes, named differently from <DVEParameters>, to the live
	// DVE setters.
	flyKeyframeGeometry = []dveFloat{
		{"xPosition", "xPosition", func(c *atem.KeyPropertiesDVE, v *float64) { c.PosX = v }},
		{"yPosition", "yPosition", func(c *atem.KeyPropertiesDVE, v *float64) { c.PosY = v }},
		{"xSize", "xSize", func(c *atem.KeyPropertiesDVE, v *float64) { c.SizeX = v }},
		{"ySize", "ySize", func(c *atem.KeyPropertiesDVE, v *float64) { c.SizeY = v }},
		{"rotation", "rotation", func(c *atem.KeyPropertiesDVE, v *float64) { c.Rotation = v }},
		{"borderWidthOut", "borderWidthOut", func(c *atem.KeyPropertiesDVE, v *float64) { c.OuterWidth = v }},
		{"borderWidthIn", "borderWidthIn", func(c *atem.KeyPropertiesDVE, v *float64) { c.InnerWidth = v }},
		{"borderSoftnessOut", "borderSoftnessOut", func(c *atem.KeyPropertiesDVE, v *float64) { c.OuterSoftness = v }},
		{"borderSoftnessIn", "borderSoftnessIn", func(c *atem.KeyPropertiesDVE, v *float64) { c.InnerSoftness = v }},
		{"borderBevelSoftness", "borderBevelSoftness", func(c *atem.KeyPropertiesDVE, v *float64) { c.BevelSoftness = v }},
		{"borderBevelPosition", "borderBevelPosition", func(c *atem.KeyPropertiesDVE, v *float64) { c.BevelPosition = v }},
		{"borderOpacity", "borderOpacity", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderOpacity = v }},
		{"borderHue", "borderHue", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderHue = v }},
		{"borderSaturation", "borderSaturation", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderSaturation = v }},
		{"borderLuma", "borderLuma", func(c *atem.KeyPropertiesDVE, v *float64) { c.BorderLuma = v }},
		{"borderLightSourceDirection", "borderLightSourceDirection", func(c *atem.KeyPropertiesDVE, v *float64) { c.Angle = v }},
		{"borderLightSourceAltitude", "borderLightSourceAltitude", func(c *atem.KeyPropertiesDVE, v *float64) { c.Altitude = v }},
		{"maskTop", "maskTop", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskTop = v }},
		{"maskBottom", "maskBottom", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskBottom = v }},
		{"maskLeft", "maskLeft", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskLeft = v }},
		{"maskRight", "maskRight", func(c *atem.KeyPropertiesDVE, v *float64) { c.MaskRight = v }},
	}
	// _USK_MASK_EDGES.
	uskMaskEdges = []struct {
		attr string
		set  func(*atem.KeyPropertiesMask, *float64)
	}{
		{"maskTop", func(c *atem.KeyPropertiesMask, v *float64) { c.Top = v }},
		{"maskBottom", func(c *atem.KeyPropertiesMask, v *float64) { c.Bottom = v }},
		{"maskLeft", func(c *atem.KeyPropertiesMask, v *float64) { c.Left = v }},
		{"maskRight", func(c *atem.KeyPropertiesMask, v *float64) { c.Right = v }},
	}
	// _USK_PATTERN_FLOATS.
	uskPatternFloats = []struct {
		attr, label string
		def         float64
		set         func(*atem.KeyPropertiesPattern, *float64)
	}{
		{"size", "size", 50.0, func(c *atem.KeyPropertiesPattern, v *float64) { c.Size = v }},
		{"symmetry", "symmetry", 50.0, func(c *atem.KeyPropertiesPattern, v *float64) { c.Symmetry = v }},
		{"softness", "softness", 0.0, func(c *atem.KeyPropertiesPattern, v *float64) { c.Softness = v }},
		{"xPosition", "position_x", 0.5, func(c *atem.KeyPropertiesPattern, v *float64) { c.PositionX = v }},
		{"yPosition", "position_y", 0.5, func(c *atem.KeyPropertiesPattern, v *float64) { c.PositionY = v }},
	}
	// _USK_CHROMA_FLOATS then _USK_CHROMA_PERCENTS (the latter ÷ 100).
	uskChromaFloats = []struct {
		attr    string
		percent bool
		set     func(*atem.KeyPropertiesAdvancedChroma, *float64)
	}{
		{"brightness", false, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Brightness = v }},
		{"contrast", false, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Contrast = v }},
		{"red", false, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Red = v }},
		{"green", false, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Green = v }},
		{"blue", false, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Blue = v }},
		{"foregroundLevel", true, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Foreground = v }},
		{"backgroundLevel", true, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Background = v }},
		{"keyEdge", true, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.KeyEdge = v }},
		{"spillSuppress", true, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Spill = v }},
		{"flareSuppress", true, func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Flare = v }},
	}
)

// setKeyDVEFloat is one of the set_usk_dve_* setters of a float: each
// rounds when called.
func (a *applier) setKeyDVEFloat(me, k int, v float64, set func(*atem.KeyPropertiesDVE, *float64)) error {
	if err := toInt(v); err != nil {
		return err
	}
	return a.keyDVE(me, k, func(c *atem.KeyPropertiesDVE) { set(c, &v) })
}

// uskBlock is _apply_usk_block: it returns how many keys it applied.
func (a *applier) uskBlock(me int, keys *Element, m MixEffectOptions) (int, error) {
	applied := 0
	for _, key := range keys.FindAll("Key") {
		kf, err := attrInt(key.attr("index"), 0)
		if err != nil {
			return applied, err
		}
		if kf < 0 || kf >= float64(len(m.USK)) || !m.USK[int(kf)] {
			continue
		}
		k := int(kf)
		applied++
		ctx := fmt.Sprintf("me[%d].usk[%d]", me, k)

		// Type first, so what follows configures the right key.
		a.safe(ctx+".type", a.setUSKType(me, k, uskTypeToInt[key.GetDefault("type", "Luma")]))
		fill, err := attrInt(key.attr("inputFill"), 0)
		if err != nil {
			return applied, err
		}
		a.safe(ctx+".fill", a.setUSKSource(me, k, fill, true))
		cut, err := attrInt(key.attr("inputCut"), 0)
		if err != nil {
			return applied, err
		}
		a.safe(ctx+".cut", a.setUSKSource(me, k, cut, false))
		a.safe(ctx+".on_air", a.setUSKOnAir(me, k, attrBool(key.attr("onAir"), false)))

		masked := attrBool(key.attr("masked"), false)
		a.safe(ctx+".mask_enabled", a.mask(me, k, func(c *atem.KeyPropertiesMask) { c.Enabled = &masked }))
		for _, f := range uskMaskEdges {
			v := attrFloat(key.attr(f.attr), 0)
			var err error
			if err = toInt(v); err == nil {
				err = a.mask(me, k, func(c *atem.KeyPropertiesMask) { f.set(c, &v) })
			}
			a.safe(ctx+"."+f.attr, err)
		}

		if luma := key.Find("LumaParameters"); luma != nil {
			a.safe(ctx+".luma", a.configureUSKLuma(me, k,
				attrFloat(luma.attr("clip"), 0), attrFloat(luma.attr("gain"), 0),
				attrBool(luma.attr("preMultiplied"), false), attrBool(luma.attr("inverse"), false)))
		}

		if pat := key.Find("PatternParameters"); pat != nil {
			style := uint8(wipePatternToInt[pat.GetDefault("style", "LeftToRightBar")])
			a.safe(ctx+".pattern.style", a.pattern(me, k, func(c *atem.KeyPropertiesPattern) { c.Pattern = &style }))
			inv := attrBool(pat.attr("inverse"), false)
			a.safe(ctx+".pattern.invert", a.pattern(me, k, func(c *atem.KeyPropertiesPattern) { c.InvertPattern = &inv }))
			for _, f := range uskPatternFloats {
				v := attrFloat(pat.attr(f.attr), f.def)
				var err error
				if err = toInt(v); err == nil {
					err = a.pattern(me, k, func(c *atem.KeyPropertiesPattern) { f.set(c, &v) })
				}
				a.safe(ctx+".pattern."+f.label, err)
			}
		}

		// Fly keyframes, before the live DVE and fly geometry that follow
		// overwrite what storing them drove on air.
		if a.opts.RestoreFlyKeyframes {
			if fly := key.Find("FlyParameters"); fly != nil {
				for _, which := range []string{"A", "B"} {
					if kfE := fly.Find("KeyFrame" + which); kfE != nil {
						a.flyKeyframe(me, k, kfE, which, ctx)
					}
				}
			}
		}

		if dve := key.Find("DVEParameters"); dve != nil {
			masked := attrBool(dve.attr("maskEnabled"), false)
			a.safe(ctx+".dve.masked", a.keyDVE(me, k, func(c *atem.KeyPropertiesDVE) { c.MaskEnabled = &masked }))
			for _, f := range uskDVEMaskEdges {
				a.safe(ctx+".dve."+f.label, a.setKeyDVEFloat(me, k, attrFloat(dve.attr(f.attr), 0), f.set))
			}
			shadow := attrBool(dve.attr("shadowEnabled"), false)
			a.safe(ctx+".dve.shadow", a.keyDVE(me, k, func(c *atem.KeyPropertiesDVE) { c.ShadowEnabled = &shadow }))
			border := attrBool(dve.attr("borderEnabled"), false)
			a.safe(ctx+".dve.enabled", a.keyDVE(me, k, func(c *atem.KeyPropertiesDVE) { c.BorderEnabled = &border }))
			for _, f := range uskDVEFloats {
				a.safe(ctx+".dve."+f.label, a.setKeyDVEFloat(me, k, attrFloat(dve.attr(f.attr), 0), f.set))
			}
		}

		// Chroma: only the attributes present.
		if chroma := key.Find("AdvancedChromaParameters"); chroma != nil {
			for _, f := range uskChromaFloats {
				s := chroma.attr(f.attr)
				if s == nil {
					continue
				}
				v := attrFloat(s, 0)
				if f.percent {
					v /= 100.0
				}
				a.safe(ctx+".chroma."+f.attr, a.chroma(me, k, v, f.set))
			}
			if s := chroma.attr("saturation"); s != nil {
				a.safe(ctx+".chroma.saturation", a.chroma(me, k, attrFloat(s, 0)/100.0,
					func(c *atem.KeyPropertiesAdvancedChroma, v *float64) { c.Saturation = v }))
			}
			// The cursor: size, position, then the sampled colour last, so
			// a resample the position triggers can't overwrite it.
			if s := chroma.attr("cursorSize"); s != nil {
				size := attrFloat(s, 0) / 100.0
				a.safe(ctx+".chroma.cursor_size", a.picker(me, k, []float64{size},
					func(c *atem.KeyPropertiesAdvancedChromaColorpicker) { c.Size = &size }))
			}
			cx, cy := chroma.attr("cursorXPosition"), chroma.attr("cursorYPosition")
			if cx != nil && cy != nil {
				x, y := attrFloat(cx, 0)/32.0+0.5, attrFloat(cy, 0)/18.0+0.5
				a.safe(ctx+".chroma.cursor_position", a.picker(me, k, []float64{x, y},
					func(c *atem.KeyPropertiesAdvancedChromaColorpicker) { c.X, c.Y = &x, &y }))
			}
			sy, scb, scr := chroma.attr("sampledY"), chroma.attr("sampledCb"), chroma.attr("sampledCr")
			if sy != nil && scb != nil && scr != nil {
				y, cb, cr := attrFloat(sy, 0), attrFloat(scb, 0), attrFloat(scr, 0)
				a.safe(ctx+".chroma.sampled_color", a.picker(me, k, []float64{y, cb, cr},
					func(c *atem.KeyPropertiesAdvancedChromaColorpicker) { c.SampleY, c.SampleCb, c.SampleCr = &y, &cb, &cr }))
			}
		}

		if fly := key.Find("FlyParameters"); fly != nil {
			a.safe(ctx+".fly_enabled", a.setUSKFlyEnabled(me, k, attrBool(fly.attr("enabled"), false)))
			for _, f := range uskFlyGeometry {
				if s := fly.attr(f.attr); s != nil {
					a.safe(ctx+".fly."+f.label, a.setKeyDVEFloat(me, k, attrFloat(s, 0), f.set))
				}
			}
			if s := fly.attr("rate"); s != nil {
				rate, err := attrInt(s, 25)
				if err != nil {
					return applied, err
				}
				a.safe(ctx+".fly.rate", a.setUSKDVERate(me, k, rate))
			}
		}
	}
	return applied, nil
}

// flyKeyframe is _apply_fly_keyframe: drive the live DVE through the
// keyframe's geometry, then store it as keyframe A or B (the switcher has
// no direct keyframe write). On air.
func (a *applier) flyKeyframe(me, k int, kfE *Element, which, ctx string) {
	for _, f := range flyKeyframeGeometry {
		if s := kfE.attr(f.attr); s != nil {
			a.safe(ctx+".kf"+which+"."+f.label, a.setKeyDVEFloat(me, k, attrFloat(s, 0), f.set))
		}
	}
	kf := uint8(1)
	if which == "B" {
		kf = 2
	}
	a.safe(ctx+".kf"+which+".snapshot", a.setKeyerFlyKeyframe(me, k, kf))
}

// --- downstream keys, fade to black, media players, program/preview ----------

func (a *applier) downstreamKeys() error {
	parent := a.root.Find("DownstreamKeys")
	if parent == nil {
		return nil
	}
	for _, d := range parent.FindAll("DownstreamKey") {
		idx, err := attrInt(d.attr("index"), 0)
		if err != nil {
			return err
		}
		ctx := "dsk[" + intString(idx) + "]"
		fill, err := attrInt(d.attr("fillSource"), 0)
		if err != nil {
			return err
		}
		a.safe(ctx+".fill", a.setDSKSource(idx, fill, true))
		key, err := attrInt(d.attr("keySource"), 0)
		if err != nil {
			return err
		}
		a.safe(ctx+".key", a.setDSKSource(idx, key, false))
		rate, err := attrInt(d.attr("rate"), 25)
		if err != nil {
			return err
		}
		a.safe(ctx+".rate", a.setDSKRate(idx, rate))
		masked := attrBool(d.attr("maskEnabled"), false)
		a.safe(ctx+".mask_enabled", a.dskMask(idx, func(c *atem.DkeyMask) { c.Enabled = &masked }))
		for _, f := range []struct {
			attr string
			set  func(*atem.DkeyMask, *float64)
		}{
			{"maskTop", func(c *atem.DkeyMask, v *float64) { c.Top = v }},
			{"maskBottom", func(c *atem.DkeyMask, v *float64) { c.Bottom = v }},
			{"maskLeft", func(c *atem.DkeyMask, v *float64) { c.Left = v }},
			{"maskRight", func(c *atem.DkeyMask, v *float64) { c.Right = v }},
		} {
			v := attrFloat(d.attr(f.attr), 0)
			var err error
			if err = toInt(v); err == nil {
				err = a.dskMask(idx, func(c *atem.DkeyMask) { f.set(c, &v) })
			}
			a.safe(ctx+"."+f.attr, err)
		}
		a.safe(ctx+".gain_block", a.configureDSKGain(idx,
			attrFloat(d.attr("clip"), 0), attrFloat(d.attr("gain"), 0),
			attrBool(d.attr("preMultipliedKey"), false), attrBool(d.attr("invert"), false)))
		a.safe(ctx+".on_air", a.setDSKOnAir(idx, attrBool(d.attr("onAir"), false)))
		a.safe(ctx+".tie", a.setDSKTie(idx, attrBool(d.attr("tie"), false)))
	}
	a.res.NoteApplied("downstream_keys")
	return nil
}

func (a *applier) fadeToBlack() error {
	applied := false
	err := a.eachMEBlock(func(me int, block *Element) error {
		if !a.opts.MEOptions(me).FadeToBlack {
			return nil
		}
		ftb := block.Find("FadeToBlack")
		if ftb == nil {
			return nil
		}
		rate, err := attrInt(ftb.attr("rate"), 25)
		if err != nil {
			return err
		}
		a.safe(fmt.Sprintf("me[%d].ftb_rate", me), a.setFTBRate(me, rate))
		applied = true
		return nil
	})
	if err != nil {
		return err
	}
	if applied {
		a.res.NoteApplied("fade_to_black")
	}
	return nil
}

func (a *applier) mediaPlayers() error {
	parent := a.root.Find("MediaPlayers")
	if parent == nil {
		return nil
	}
	for _, mp := range parent.FindAll("MediaPlayer") {
		idx, err := attrInt(mp.attr("index"), 0)
		if err != nil {
			return err
		}
		kind := mp.GetDefault("sourceType", "Still")
		slot, err := attrInt(mp.attr("sourceIndex"), 0)
		if err != nil {
			return err
		}
		if kind == "Clip" {
			a.safe("mp["+intString(idx)+"].clip", a.setMediaPlayer(idx, slot, true))
		} else {
			a.safe("mp["+intString(idx)+"].still", a.setMediaPlayer(idx, slot, false))
		}
	}
	a.res.NoteApplied("media_players")
	return nil
}

// programPreview is _apply_program_preview, last because it cuts to air:
// preview first, then program, per M/E.
func (a *applier) programPreview() error {
	didProg, didPrev := false, false
	err := a.eachMEBlock(func(me int, block *Element) error {
		m := a.opts.MEOptions(me)
		if prev := block.Find("Preview"); m.Preview && prev != nil {
			src, err := attrInt(prev.attr("input"), 0)
			if err != nil {
				return err
			}
			a.safe(fmt.Sprintf("me[%d].preview", me), a.setPreview(me, src))
			didPrev = true
		}
		if prog := block.Find("Program"); m.Program && prog != nil {
			src, err := attrInt(prog.attr("input"), 0)
			if err != nil {
				return err
			}
			a.safe(fmt.Sprintf("me[%d].program", me), a.setProgram(me, src))
			didProg = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	switch {
	case didProg && didPrev:
		a.res.NoteApplied("program_preview")
	case didProg:
		a.res.NoteApplied("program")
	case didPrev:
		a.res.NoteApplied("preview")
	}
	return nil
}

// --- Fairlight -----------------------------------------------------------------

// faderDB is _fader_db: "-inf" (any case, or "-infinity") is silence, an
// absent or empty or unparseable value is nil, so the field stays as it
// is.
func faderDB(s *string) *float64 {
	if s == nil || *s == "" {
		return nil
	}
	if t := strings.ToLower(pyStrip(*s)); t == "-inf" || t == "-infinity" {
		v := math.Inf(-1)
		return &v
	}
	if v, ok := pyFloat(*s); ok {
		return &v
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// fairlight is _apply_fairlight: the master (one field per CFMP, as ASC
// sends them), master EQ bands (CMBP), master dynamics (CMCP, CMLP), then
// every strip, each to the channel its id names (audioSourceChannel): a
// dual-mono input's halves to 0 and 1, the XLR's -256 to 0, a stereo strip
// to -1. An input's configuration is never changed (atemwire has no
// command for it), so a dual-mono profile restores onto an input that is
// already in Dual Mono.
func (a *applier) fairlight() error {
	fa := a.root.Find("FairlightAudioMixer")
	if fa == nil {
		return nil
	}
	masterEQ := fa.Find("MasterOutEqualizer")
	masterDyn := fa.Find("MasterOutDynamicsProcessor")
	var fields []atem.FairlightMasterProperties
	if masterEQ != nil {
		fields = append(fields,
			atem.FairlightMasterProperties{EQEnable: ptr(attrBool(masterEQ.attr("enabled"), false))},
			atem.FairlightMasterProperties{EQGain: ptr(attrFloat(masterEQ.attr("gain"), 0))})
	}
	if masterDyn != nil {
		fields = append(fields, atem.FairlightMasterProperties{DynamicsGain: ptr(attrFloat(masterDyn.attr("makeupGain"), 0))})
	}
	if v := faderDB(fa.attr("masterOutFaderGain")); v != nil {
		fields = append(fields, atem.FairlightMasterProperties{Volume: v})
	}
	fields = append(fields, atem.FairlightMasterProperties{AFV: ptr(attrBool(fa.attr("followFadeToBlack"), false))})
	for _, f := range fields {
		a.safe("fairlight.master", a.setFairlightMaster(f))
	}

	if masterEQ != nil {
		for _, band := range masterEQ.FindAll("EqualizerBand") {
			if err := a.masterEQBand(band); err != nil {
				return err
			}
		}
	}

	if masterDyn != nil {
		comp, lim := masterDyn.Find("Compressor"), masterDyn.Find("Limiter")
		if comp != nil {
			a.safe("fairlight.master.compressor", a.send(atem.FairlightMasterCompressorProperties{
				Enabled:   ptr(attrBool(comp.attr("enabled"), false)),
				Threshold: ptr(attrFloat(comp.attr("threshold"), 0)),
				Ratio:     ptr(attrFloat(comp.attr("ratio"), 0)),
				Attack:    ptr(attrFloat(comp.attr("attack"), 0)),
				Hold:      ptr(attrFloat(comp.attr("hold"), 0)),
				Release:   ptr(attrFloat(comp.attr("release"), 0)),
			}))
		}
		if lim != nil {
			a.safe("fairlight.master.limiter", a.send(atem.FairlightMasterLimiterProperties{
				Enabled:   ptr(attrBool(lim.attr("enabled"), false)),
				Threshold: ptr(attrFloat(lim.attr("threshold"), 0)),
				Attack:    ptr(attrFloat(lim.attr("attack"), 0)),
				Hold:      ptr(attrFloat(lim.attr("hold"), 0)),
				Release:   ptr(attrFloat(lim.attr("release"), 0)),
			}))
		}
		if comp != nil || lim != nil {
			a.res.NoteApplied("fairlight.master.dynamics")
		}
	}

	inputs := fa.Find("AudioInputs")
	if inputs == nil {
		return nil
	}
	nStrips, nBands := 0, 0
	for _, ai := range inputs.FindAll("AudioInput") {
		src, err := attrInt(ai.attr("id"), 0)
		if err != nil {
			return err
		}
		if src <= 0 {
			continue
		}
		for _, srcE := range ai.FindAll("AudioSource") {
			channel := audioSourceChannel(srcE.attr("id"))
			label := "fairlight.strip[" + intString(src) + "]"
			if channel != -1 {
				label = fmt.Sprintf("fairlight.strip[%s.%d]", intString(src), channel)
			}
			f := stripFields{
				gain:      ptr(attrFloat(srcE.attr("inputGain"), 0)),
				pan:       ptr(attrFloat(srcE.attr("pan"), 0)),
				volume:    faderDB(srcE.attr("faderGain")),
				mixOption: srcE.attr("mixOption"),
			}
			if s := srcE.attr("delayFrames"); s != nil {
				// _int runs inside the try here: an infinite delay is
				// recorded, not raised.
				d, err := attrInt(s, 0)
				f.delay, f.delayErr = &d, err
			}
			if err := a.setFairlightStrip(src, channel, f); err != nil {
				a.res.NoteError(label, err)
			} else {
				nStrips++
			}

			if eq := srcE.Find("Equalizer"); eq != nil {
				if err := a.setFairlightStrip(src, channel, stripFields{
					eqEnable: ptr(attrBool(eq.attr("enabled"), false)),
					eqGain:   ptr(attrFloat(eq.attr("gain"), 0)),
				}); err != nil {
					a.res.NoteError(label+".eq", err)
				}
				for _, band := range eq.FindAll("EqualizerBand") {
					if err := a.eqBand(src, channel, band, label+".eq"); err != nil {
						return err
					}
					nBands++
				}
			}

			if dyn := srcE.Find("DynamicsProcessor"); dyn != nil {
				if err := a.setFairlightStrip(src, channel, stripFields{
					dynGain: ptr(attrFloat(dyn.attr("makeupGain"), 0)),
				}); err != nil {
					a.res.NoteError(label+".dynamics", err)
				}
				a.dynamicsBlock(src, channel, dyn, label)
			}
		}
	}
	if nStrips > 0 {
		a.res.NoteApplied(fmt.Sprintf("fairlight (%d strips, %d EQ bands)", nStrips, nBands))
	}
	return nil
}

// readBand reads an <EqualizerBand>'s fields; the frequency's _int runs
// inside atemwire's try, so its error travels with the fields.
func readBand(e *Element) bandFields {
	f := bandFields{
		enabled:   attrBool(e.attr("enabled"), false),
		shape:     e.attr("shape"),
		freqRange: e.attr("frequencyRange"),
		gain:      attrFloat(e.attr("gain"), 0),
		q:         attrFloat(e.attr("qFactor"), 0),
	}
	if s := e.attr("frequency"); s != nil {
		hz, err := attrInt(s, 0)
		f.frequency, f.freqErr = &hz, err
	}
	return f
}

// masterEQBand is _apply_master_eq_band: one CMBP.
func (a *applier) masterEQBand(e *Element) error {
	band, err := attrInt(e.attr("index"), 0)
	if err != nil {
		return err
	}
	if err := a.setFairlightMasterEQBand(band, readBand(e)); err != nil {
		a.res.NoteError("fairlight.master.eq.band["+intString(band)+"]", err)
	}
	return nil
}

// eqBand is _apply_eq_band: one CEBP.
func (a *applier) eqBand(src float64, channel int, e *Element, label string) error {
	band, err := attrInt(e.attr("index"), 0)
	if err != nil {
		return err
	}
	if err := a.setFairlightEQBand(src, channel, band, readBand(e)); err != nil {
		a.res.NoteError(label+".band["+intString(band)+"]", err)
	}
	return nil
}

// dynamicsBlock is _apply_dynamics_block: a strip's Compressor, Limiter
// and Expander (CICP, CILP, CIXP).
func (a *applier) dynamicsBlock(src float64, channel int, dyn *Element, label string) {
	if comp := dyn.Find("Compressor"); comp != nil {
		c := atem.FairlightCompressorProperties{
			Channel:   channel,
			Enabled:   ptr(attrBool(comp.attr("enabled"), false)),
			Threshold: ptr(attrFloat(comp.attr("threshold"), 0)),
			Ratio:     ptr(attrFloat(comp.attr("ratio"), 0)),
			Attack:    ptr(attrFloat(comp.attr("attack"), 0)),
			Hold:      ptr(attrFloat(comp.attr("hold"), 0)),
			Release:   ptr(attrFloat(comp.attr("release"), 0)),
		}
		var err error
		if s, ok := a.fairlightSource(src, "CICP"); ok {
			c.Source = s
			err = a.send(c)
		}
		a.safe(label+".compressor", err)
	}
	if lim := dyn.Find("Limiter"); lim != nil {
		c := atem.FairlightLimiterProperties{
			Channel:   channel,
			Enabled:   ptr(attrBool(lim.attr("enabled"), false)),
			Threshold: ptr(attrFloat(lim.attr("threshold"), 0)),
			Attack:    ptr(attrFloat(lim.attr("attack"), 0)),
			Hold:      ptr(attrFloat(lim.attr("hold"), 0)),
			Release:   ptr(attrFloat(lim.attr("release"), 0)),
		}
		var err error
		if s, ok := a.fairlightSource(src, "CILP"); ok {
			c.Source = s
			err = a.send(c)
		}
		a.safe(label+".limiter", err)
	}
	if exp := dyn.Find("Expander"); exp != nil {
		mode := uint8(0) // "expander"
		if attrBool(exp.attr("gateMode"), false) {
			mode = 1 // "gate"
		}
		c := atem.FairlightExpanderProperties{
			Channel:   channel,
			Enabled:   ptr(attrBool(exp.attr("enabled"), false)),
			Mode:      &mode,
			Threshold: ptr(attrFloat(exp.attr("threshold"), 0)),
			Range:     ptr(attrFloat(exp.attr("range"), 0)),
			Ratio:     ptr(attrFloat(exp.attr("ratio"), 0)),
			Attack:    ptr(attrFloat(exp.attr("attack"), 0)),
			Hold:      ptr(attrFloat(exp.attr("hold"), 0)),
			Release:   ptr(attrFloat(exp.attr("release"), 0)),
		}
		var err error
		if s, ok := a.fairlightSource(src, "CIXP"); ok {
			c.Source = s
			err = a.send(c)
		}
		a.safe(label+".expander", err)
	}
}

// --- macros --------------------------------------------------------------------

type macroDrop struct {
	slot         float64
	name         string
	ids          []string
	metadataOnly bool
}

type macroFailure struct {
	slot   float64
	name   string
	reason string
}

// macros is _apply_macros: each <Macro> encoded op by op and uploaded to
// its slot. An op the encoder refuses with ValueError is dropped and
// reported; any other encoder error ends the apply (SURFACE.md §5.4 item
// 8). A macro whose ops all drop, or that has none, uploads empty
// bytecode so its name and description still land.
func (a *applier) macros() error {
	pool := a.root.Find("MacroPool")
	if pool == nil {
		return nil
	}
	macros := pool.FindAll("Macro")
	if len(macros) == 0 {
		return nil
	}
	total, restored := len(macros), 0
	var drops []macroDrop
	var failures []macroFailure
	a.log.Info(fmt.Sprintf("Profile.apply restoring %d macro(s) via bytecode upload", total))

	if c, ok := a.t.(interface{ Connected() bool }); ok && !c.Connected() {
		a.log.Warn("Profile.apply macros: no connected protocol — skipping all macros")
		a.res.NoteSkipped("macros", "no connected protocol")
		return nil
	}
	timeout := a.opts.MacroTimeout
	if timeout <= 0 {
		timeout = macroUploadTimeout
	}

	for _, m := range macros {
		slot, err := attrInt(m.attr("index"), -1)
		if err != nil {
			return err
		}
		if slot < 0 {
			continue
		}
		name := m.GetDefault("name", "")
		desc := m.GetDefault("description", "")

		var bytecode []byte
		var dropped []string
		encoded := 0
		for _, opE := range m.FindAll("Op") {
			op := macro.Op{ID: opE.GetDefault("id", "")}
			for _, at := range opE.Attrs {
				if at.Name != "id" {
					op.Params = append(op.Params, macro.Param{Name: at.Name, Value: at.Value})
				}
			}
			op.Params = append(op.Params, macro.Param{Name: "id", Value: op.ID})
			b, err := macro.EncodeOp(op)
			if err != nil {
				var me *macro.Error
				if errors.As(err, &me) && me.Class == "ValueError" {
					id := op.ID
					if id == "" {
						id = "<no-id>"
					}
					dropped = append(dropped, id)
					a.log.Debug("macro op dropped", "slot", intString(slot), "op", op.ID, "err", me.Message)
					continue
				}
				return err
			}
			bytecode = append(bytecode, b...)
			encoded++
		}
		metadataOnly := encoded == 0

		var upErr error
		if !fitsInt(slot, 0, 1<<31-1) {
			upErr = fmt.Errorf("macro slot %s out of range", intString(slot))
		} else {
			if bytecode == nil {
				bytecode = []byte{}
			}
			ctx, cancel := context.WithTimeout(a.ctx, timeout)
			upErr = a.t.UploadMacro(ctx, int(slot), name, desc, bytecode)
			cancel()
		}
		if upErr != nil {
			failures = append(failures, macroFailure{slot, name, "upload error: " + upErr.Error()})
			a.log.Warn("macro upload failed", "slot", intString(slot), "name", name, "err", upErr)
			continue
		}
		restored++
		if len(dropped) > 0 {
			drops = append(drops, macroDrop{slot, name, dropped, metadataOnly})
		}
	}

	summary := macroSummary(restored, total, drops, failures)
	a.log.Info("Profile.apply macros: " + summary)
	if restored > 0 {
		a.res.NoteApplied("macros (" + summary + ")")
	} else {
		a.res.NoteSkipped("macros", summary)
	}
	for _, f := range failures {
		a.res.NoteError("macros", &Error{"RuntimeError", fmt.Sprintf("slot %s (%s): %s", intString(f.slot), pyRepr(f.name), f.reason)})
	}
	return nil
}

// macroSummary is _format_macro_summary.
func macroSummary(restored, total int, drops []macroDrop, failures []macroFailure) string {
	if len(drops) == 0 && len(failures) == 0 {
		return fmt.Sprintf("%d/%d restored, 0 failed", restored, total)
	}
	head := fmt.Sprintf("%d/%d restored", restored, total)
	var sections []string
	if len(drops) > 0 {
		var details []string
		for _, d := range drops {
			details = append(details, dropEntry(d))
		}
		sections = append(sections, fmt.Sprintf("%d with dropped ops: %s", len(drops), strings.Join(details, "; ")))
	}
	if len(failures) > 0 {
		var details []string
		for _, f := range failures {
			details = append(details, fmt.Sprintf("slot %s (%s) — %s", intString(f.slot), pyRepr(f.name), f.reason))
		}
		sections = append(sections, fmt.Sprintf("%d failed: %s", len(failures), strings.Join(details, "; ")))
	}
	return head + ", " + strings.Join(sections, ", ")
}

// dropEntry is _format_drop_entry: the count, and each id once, sorted.
func dropEntry(d macroDrop) string {
	word := "ops"
	if len(d.ids) == 1 {
		word = "op"
	}
	ids := slices.Clone(d.ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	suffix := ""
	if d.metadataOnly {
		suffix = " — metadata only"
	}
	return fmt.Sprintf("slot %s (%s) dropped %d %s (%s)%s", intString(d.slot), pyRepr(d.name), len(d.ids), word, strings.Join(ids, ", "), suffix)
}
