// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The transition readers of bmdwire v1.3.0 atemwire/messages/transition.py.
// Rates are frames, as atemwire's readers return them; FormatRate with
// DisplayFPS turns them into "seconds:frames". See read_state.go for the
// conventions.

// TransitionSelectionView is transition_selection's dict: the layers the
// next transition takes.
type TransitionSelectionView struct {
	Background bool `json:"background"`
	Key1       bool `json:"key1"`
	Key2       bool `json:"key2"`
	Key3       bool `json:"key3"`
	Key4       bool `json:"key4"`
}

// The transition-* entries of M/E me, nil where atemwire's _kv finds none.

func (s State) trSettings(me int) *TransitionSettingsField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Settings
	}
	return nil
}

func (s State) trPosition(me int) *TransitionPositionField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Position
	}
	return nil
}

func (s State) trMix(me int) *TransitionMixField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Mix
	}
	return nil
}

func (s State) trDip(me int) *TransitionDipField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Dip
	}
	return nil
}

func (s State) trWipe(me int) *TransitionWipeField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Wipe
	}
	return nil
}

func (s State) trDVE(me int) *TransitionDVEField {
	if m := s.meAt(me); m != nil {
		return m.Transition.DVE
	}
	return nil
}

func (s State) trStinger(me int) *TransitionStingerField {
	if m := s.meAt(me); m != nil {
		return m.Transition.Stinger
	}
	return nil
}

// TransitionStyle is transition_style (messages/transition.py), bmdwire
// v1.3.0: 0 mix, 1 dip, 2 wipe, 3 DVE, 4 stinger; 0 before TrSS.
func (s State) TransitionStyle(me int) int {
	if t := s.trSettings(me); t != nil {
		return int(t.Style)
	}
	return 0
}

// TransitionInTransition is transition_in_transition
// (messages/transition.py), bmdwire v1.3.0; false before TrPs.
func (s State) TransitionInTransition(me int) bool {
	if t := s.trPosition(me); t != nil {
		return t.InTransition
	}
	return false
}

// TransitionPosition is transition_position (messages/transition.py),
// bmdwire v1.3.0: the T-bar, raw 0-10000 as a float; 0.0 before TrPs.
func (s State) TransitionPosition(me int) float64 {
	if t := s.trPosition(me); t != nil {
		return float64(t.Position)
	}
	return 0.0
}

// TransitionFramesRemaining is transition_frames_remaining
// (messages/transition.py), bmdwire v1.3.0; 0 before TrPs.
func (s State) TransitionFramesRemaining(me int) int {
	if t := s.trPosition(me); t != nil {
		return int(t.FramesRemaining)
	}
	return 0
}

// TransitionSelection is transition_selection (messages/transition.py),
// bmdwire v1.3.0; background alone before TrSS.
func (s State) TransitionSelection(me int) TransitionSelectionView {
	t := s.trSettings(me)
	if t == nil {
		return TransitionSelectionView{Background: true}
	}
	return TransitionSelectionView{
		Background: t.NextTransitionBkgd,
		Key1:       t.NextTransitionKey1,
		Key2:       t.NextTransitionKey2,
		Key3:       t.NextTransitionKey3,
		Key4:       t.NextTransitionKey4,
	}
}

// MixRate is mix_rate (messages/transition.py), bmdwire v1.3.0: frames;
// 25 before TMxP.
func (s State) MixRate(me int) int {
	if t := s.trMix(me); t != nil {
		return int(t.Rate)
	}
	return 25
}

// DipRate is dip_rate (messages/transition.py), bmdwire v1.3.0: frames; 25
// before TDpP.
func (s State) DipRate(me int) int {
	if t := s.trDip(me); t != nil {
		return int(t.Rate)
	}
	return 25
}

// DipSource is dip_source (messages/transition.py), bmdwire v1.3.0; 0
// before TDpP.
func (s State) DipSource(me int) int {
	if t := s.trDip(me); t != nil {
		return int(t.Source)
	}
	return 0
}

// ActiveTransitionRate is active_transition_rate (messages/transition.py),
// bmdwire v1.3.0: the rate of the current style's transition, frames; 25
// for a style atemwire doesn't know.
func (s State) ActiveTransitionRate(me int) int {
	switch s.TransitionStyle(me) {
	case 0:
		return s.MixRate(me)
	case 1:
		return s.DipRate(me)
	case 2:
		return s.WipeRate(me)
	case 3:
		return s.DVERate(me)
	case 4:
		return s.StingerRate(me)
	}
	return 25
}

// WipeRate is wipe_rate (messages/transition.py), bmdwire v1.3.0: frames;
// 25 before TWpP.
func (s State) WipeRate(me int) int {
	if t := s.trWipe(me); t != nil {
		return int(t.Rate)
	}
	return 25
}

// WipePattern is wipe_pattern (messages/transition.py), bmdwire v1.3.0; 0
// before TWpP.
func (s State) WipePattern(me int) int {
	if t := s.trWipe(me); t != nil {
		return int(t.Pattern)
	}
	return 0
}

// WipeFillSource is wipe_fill_source (messages/transition.py), bmdwire
// v1.3.0: the border fill; 0 before TWpP.
func (s State) WipeFillSource(me int) int {
	if t := s.trWipe(me); t != nil {
		return int(t.Source)
	}
	return 0
}

// WipePositionX is wipe_position_x (messages/transition.py), bmdwire
// v1.3.0: 0-1, round(wire / 10000, 4); 0.5 before TWpP.
func (s State) WipePositionX(me int) float64 {
	if t := s.trWipe(me); t != nil {
		return unitFromThousandths(float64(t.PositionX))
	}
	return 0.5
}

// WipePositionY is wipe_position_y (messages/transition.py), bmdwire
// v1.3.0: 0-1; 0.5 before TWpP.
func (s State) WipePositionY(me int) float64 {
	if t := s.trWipe(me); t != nil {
		return unitFromThousandths(float64(t.PositionY))
	}
	return 0.5
}

// WipeSoftness is wipe_softness (messages/transition.py), bmdwire v1.3.0:
// percent, round(wire / 100, 2); 0.0 before TWpP.
func (s State) WipeSoftness(me int) float64 {
	if t := s.trWipe(me); t != nil {
		return percentFromThousandths(float64(t.Softness))
	}
	return 0.0
}

// WipeSymmetry is wipe_symmetry (messages/transition.py), bmdwire v1.3.0:
// percent; 50.0 before TWpP.
func (s State) WipeSymmetry(me int) float64 {
	if t := s.trWipe(me); t != nil {
		return percentFromThousandths(float64(t.Symmetry))
	}
	return 50.0
}

// WipeWidth is wipe_width (messages/transition.py), bmdwire v1.3.0:
// percent; 0.0 before TWpP.
func (s State) WipeWidth(me int) float64 {
	if t := s.trWipe(me); t != nil {
		return percentFromThousandths(float64(t.Width))
	}
	return 0.0
}

// WipeReverse is wipe_reverse (messages/transition.py), bmdwire v1.3.0;
// false before TWpP.
func (s State) WipeReverse(me int) bool {
	if t := s.trWipe(me); t != nil {
		return t.Reverse
	}
	return false
}

// WipeFlipFlop is wipe_flip_flop (messages/transition.py), bmdwire v1.3.0;
// false before TWpP.
func (s State) WipeFlipFlop(me int) bool {
	if t := s.trWipe(me); t != nil {
		return t.FlipFlop
	}
	return false
}

// DVERate is dve_rate (messages/transition.py), bmdwire v1.3.0: frames; 25
// before TDvP.
func (s State) DVERate(me int) int {
	if t := s.trDVE(me); t != nil {
		return int(t.Rate)
	}
	return 25
}

// DVEFillSource is dve_fill_source (messages/transition.py), bmdwire
// v1.3.0; 1 before TDvP.
func (s State) DVEFillSource(me int) int {
	if t := s.trDVE(me); t != nil {
		return int(t.FillSource)
	}
	return 1
}

// DVEKeySource is dve_key_source (messages/transition.py), bmdwire v1.3.0;
// 1 before TDvP.
func (s State) DVEKeySource(me int) int {
	if t := s.trDVE(me); t != nil {
		return int(t.KeySource)
	}
	return 1
}

// DVEEnableKey is dve_enable_key (messages/transition.py), bmdwire v1.3.0;
// false before TDvP.
func (s State) DVEEnableKey(me int) bool {
	if t := s.trDVE(me); t != nil {
		return t.KeyEnable
	}
	return false
}

// DVEClip is dve_clip (messages/transition.py), bmdwire v1.3.0: percent,
// round(wire / 10, 2); 0.0 before TDvP.
func (s State) DVEClip(me int) float64 {
	if t := s.trDVE(me); t != nil {
		return valueFromTenths(float64(t.KeyClip))
	}
	return 0.0
}

// DVEGain is dve_gain (messages/transition.py), bmdwire v1.3.0: percent;
// 100.0 before TDvP.
func (s State) DVEGain(me int) float64 {
	if t := s.trDVE(me); t != nil {
		return valueFromTenths(float64(t.KeyGain))
	}
	return 100.0
}

// DVEPreMultiplied is dve_pre_multiplied (messages/transition.py), bmdwire
// v1.3.0; false before TDvP.
func (s State) DVEPreMultiplied(me int) bool {
	if t := s.trDVE(me); t != nil {
		return t.KeyPremultiplied
	}
	return false
}

// DVEInvertKey is dve_invert_key (messages/transition.py), bmdwire v1.3.0;
// false before TDvP.
func (s State) DVEInvertKey(me int) bool {
	if t := s.trDVE(me); t != nil {
		return t.KeyInvert
	}
	return false
}

// DVEStyle is dve_style (messages/transition.py), bmdwire v1.3.0: the DVE
// transition style, or nil (atemwire's None) before TDvP, so "style 0" and
// "not seen yet" differ.
func (s State) DVEStyle(me int) *int {
	if t := s.trDVE(me); t != nil {
		v := int(t.Style)
		return &v
	}
	return nil
}

// DVEReverse is dve_reverse (messages/transition.py), bmdwire v1.3.0;
// false before TDvP.
func (s State) DVEReverse(me int) bool {
	if t := s.trDVE(me); t != nil {
		return t.Reverse
	}
	return false
}

// DVEFlipFlop is dve_flip_flop (messages/transition.py), bmdwire v1.3.0;
// false before TDvP.
func (s State) DVEFlipFlop(me int) bool {
	if t := s.trDVE(me); t != nil {
		return t.FlipFlop
	}
	return false
}

// StingerRate is stinger_rate (messages/transition.py), bmdwire v1.3.0:
// the mix rate, frames; 25 before TStP.
func (s State) StingerRate(me int) int {
	if t := s.trStinger(me); t != nil {
		return int(t.Rate)
	}
	return 25
}

// StingerSource is stinger_source (messages/transition.py), bmdwire
// v1.3.0: the media player slot (1-4), not a source id; 1 before TStP.
func (s State) StingerSource(me int) int {
	if t := s.trStinger(me); t != nil {
		return int(t.MediaPlayer)
	}
	return 1
}

// StingerClipDuration is stinger_clip_duration (messages/transition.py),
// bmdwire v1.3.0: frames; 150 before TStP.
func (s State) StingerClipDuration(me int) int {
	if t := s.trStinger(me); t != nil {
		return int(t.Duration)
	}
	return 150
}

// StingerTriggerPoint is stinger_trigger_point (messages/transition.py),
// bmdwire v1.3.0: frames; 34 before TStP.
func (s State) StingerTriggerPoint(me int) int {
	if t := s.trStinger(me); t != nil {
		return int(t.TriggerPoint)
	}
	return 34
}

// StingerPreRoll is stinger_pre_roll (messages/transition.py), bmdwire
// v1.3.0: frames; 48 before TStP.
func (s State) StingerPreRoll(me int) int {
	if t := s.trStinger(me); t != nil {
		return int(t.Preroll)
	}
	return 48
}

// StingerClip is stinger_clip (messages/transition.py), bmdwire v1.3.0:
// percent, round(wire / 10, 2); 50.0 before TStP.
func (s State) StingerClip(me int) float64 {
	if t := s.trStinger(me); t != nil {
		return valueFromTenths(float64(t.KeyClip))
	}
	return 50.0
}

// StingerGain is stinger_gain (messages/transition.py), bmdwire v1.3.0:
// percent; 70.0 before TStP.
func (s State) StingerGain(me int) float64 {
	if t := s.trStinger(me); t != nil {
		return valueFromTenths(float64(t.KeyGain))
	}
	return 70.0
}

// StingerPreMultiplied is stinger_pre_multiplied (messages/transition.py),
// bmdwire v1.3.0; true before TStP (the DVE transition's default is false).
func (s State) StingerPreMultiplied(me int) bool {
	if t := s.trStinger(me); t != nil {
		return t.KeyPremultiplied
	}
	return true
}

// StingerInvertKey is stinger_invert_key (messages/transition.py), bmdwire
// v1.3.0; false before TStP.
func (s State) StingerInvertKey(me int) bool {
	if t := s.trStinger(me); t != nil {
		return t.KeyInvert
	}
	return false
}
