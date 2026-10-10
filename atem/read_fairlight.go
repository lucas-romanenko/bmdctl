// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The Fairlight readers of bmdwire v1.3.0 atemwire/messages/fairlight.py,
// with the helpers they use from atemwire/state.py. Gains are dB (wire
// ÷ 100, two decimals), times ms, pan ±100. See read_state.go for the
// conventions.

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// FairlightMasterView is fairlight_master's dict. VolumeDB is nil (atemwire's
// None) for a fully silenced master (-100 dB or below).
type FairlightMasterView struct {
	Present          bool     `json:"present"`
	VolumeDB         *float64 `json:"volume_db"`
	EQEnable         bool     `json:"eq_enable"`
	EQGainDB         float64  `json:"eq_gain_db"`
	DynamicsMakeupDB float64  `json:"dynamics_makeup_db"`
	AFV              bool     `json:"afv"`
}

// FairlightStripView is one entry of fairlight_strips. Channel is -1 for a
// stereo strip and the subchannel for a split-mono one; VolumeDB is nil
// when silenced. MixType names MixState (Off, On, AudioFollowVideo, else
// the number). AIType, AINumber and AILevel are the strip's FAIP type,
// number and level (atemwire's _ai_* keys, which it sorts by); DisplayName
// and DisplayNameLong are the labels ATEM Software Control shows.
type FairlightStripView struct {
	Source           int      `json:"source"`
	Channel          int      `json:"channel"`
	Subchannel       int      `json:"subchannel"`
	Split            bool     `json:"split"`
	StripID          string   `json:"strip_id"`
	VolumeDB         *float64 `json:"volume_db"`
	InputGainDB      float64  `json:"input_gain_db"`
	Pan              float64  `json:"pan"`
	MixType          string   `json:"mix_type"`
	MixState         int      `json:"mix_state"`
	DelayFrames      int      `json:"delay_frames"`
	EQEnable         bool     `json:"eq_enable"`
	EQGainDB         float64  `json:"eq_gain_db"`
	DynamicsMakeupDB float64  `json:"dynamics_makeup_db"`
	AIType           int      `json:"_ai_type"`
	AINumber         int      `json:"_ai_number"`
	AILevel          int      `json:"_ai_level"`
	DisplayName      string   `json:"display_name"`
	DisplayNameLong  string   `json:"display_name_long"`
}

// FairlightAudioInputView is one value of fairlight_audio_inputs: a
// source's FAIP input type, split and analog level.
type FairlightAudioInputView struct {
	Type  int `json:"type"`
	Split int `json:"split"`
	Level int `json:"level"`
}

// EQBandView is one entry of fairlight_eq_bands. Filter and Range name
// FilterInt and RangeInt (else the number); Q is wire ÷ 100.
type EQBandView struct {
	Index           int     `json:"index"`
	Enabled         bool    `json:"enabled"`
	Filter          string  `json:"filter"`
	FilterInt       int     `json:"filter_int"`
	PossibleFilters int     `json:"possible_filters"`
	Frequency       int     `json:"frequency"`
	GainDB          float64 `json:"gain_db"`
	Q               float64 `json:"q"`
	Range           string  `json:"range"`
	RangeInt        int     `json:"range_int"`
}

// CompressorView is the compressor block of _dynamics_block_dict
// (with_ratio): fairlight_compressor's values and
// fairlight_master_compressor.
type CompressorView struct {
	Enabled     bool    `json:"enabled"`
	ThresholdDB float64 `json:"threshold_db"`
	AttackMS    float64 `json:"attack_ms"`
	HoldMS      float64 `json:"hold_ms"`
	ReleaseMS   float64 `json:"release_ms"`
	Ratio       float64 `json:"ratio"`
}

// LimiterView is the limiter block of _dynamics_block_dict:
// fairlight_limiter's values and fairlight_master_limiter.
type LimiterView struct {
	Enabled     bool    `json:"enabled"`
	ThresholdDB float64 `json:"threshold_db"`
	AttackMS    float64 `json:"attack_ms"`
	HoldMS      float64 `json:"hold_ms"`
	ReleaseMS   float64 `json:"release_ms"`
}

// ExpanderView is fairlight_expander's value: _dynamics_block_dict with
// ratio and range, and Mode "expander" or "gate".
type ExpanderView struct {
	Enabled     bool    `json:"enabled"`
	ThresholdDB float64 `json:"threshold_db"`
	AttackMS    float64 `json:"attack_ms"`
	HoldMS      float64 `json:"hold_ms"`
	ReleaseMS   float64 `json:"release_ms"`
	Ratio       float64 `json:"ratio"`
	RangeDB     float64 `json:"range_db"`
	Mode        string  `json:"mode"`
}

// FairlightHeadphonesView is fairlight_headphones's dict.
type FairlightHeadphonesView struct {
	Present  bool    `json:"present"`
	VolumeDB float64 `json:"volume_db"`
	Unmuted  bool    `json:"unmuted"`
}

// FairlightSoloView is fairlight_solo's dict. StripID is nil (atemwire's
// None) when nothing is soloed.
type FairlightSoloView struct {
	AnySoloed bool    `json:"any_soloed"`
	StripID   *string `json:"strip_id"`
}

// state.py's _MIX_STATE_NAMES, _EQ_BAND_FILTER_NAMES and
// _EQ_FREQ_RANGE_NAMES.
var (
	mixStateNames     = map[int]string{1: "Off", 2: "On", 4: "AudioFollowVideo"}
	eqBandFilterNames = map[int]string{
		0x01: "LowShelf", 0x02: "LowPass", 0x04: "BandPass",
		0x08: "Notch", 0x10: "HighPass", 0x20: "HighShelf",
	}
	eqFreqRangeNames = map[int]string{1: "Low", 2: "MidLow", 4: "MidHigh", 8: "High"}
)

// nameOr is atemwire's table.get(v, str(v)).
func nameOr(table map[int]string, v int) string {
	if n, ok := table[v]; ok {
		return n
	}
	return strconv.Itoa(v)
}

// hundredths is round(wire / 100.0, 2): dB, ms, pan, ratio and Q.
func hundredths(wire int) float64 { return wireDB(wire) }

// FairlightMaster is fairlight_master (messages/fairlight.py), bmdwire
// v1.3.0: the master bus (FAMP); before FAMP, Present is false and the
// rest is zero (VolumeDB 0.0, not nil).
func (s State) FairlightMaster() FairlightMasterView {
	m := s.Fairlight.Master
	if m == nil {
		zero := 0.0
		return FairlightMasterView{VolumeDB: &zero}
	}
	return FairlightMasterView{
		Present:          true,
		VolumeDB:         wireDBSilenced(int(m.Volume)),
		EQEnable:         m.EQEnable,
		EQGainDB:         wireDB(int(m.EQGain)),
		DynamicsMakeupDB: wireDB(int(m.DynamicsGain)),
		AFV:              m.AFV,
	}
}

// stripAddressing is _strip_id_to_addressing (state.py): the source,
// channel and subchannel a strip id spells, or source 0, channel -1 for an
// id that doesn't parse (the state never holds one).
func stripAddressing(id string, isSplit int) FairlightStripView {
	if base, sub, ok := strings.Cut(id, "."); ok {
		source, err1 := pyAtoi(base)
		subchannel, err2 := pyAtoi(sub)
		if err1 == nil && err2 == nil {
			split := isSplit == 0xff
			channel := -1
			if split {
				channel = subchannel
			}
			return FairlightStripView{Source: source, Channel: channel, Subchannel: subchannel, Split: split, StripID: id}
		}
	}
	return FairlightStripView{Channel: -1, StripID: id}
}

// fairlightStrip is _fairlight_strip_dict (state.py): one FASP record.
func fairlightStrip(id string, sp *FairlightStripPropertiesField) FairlightStripView {
	v := stripAddressing(id, int(sp.IsSplit))
	v.VolumeDB = wireDBSilenced(int(sp.Volume))
	v.InputGainDB = wireDB(int(sp.Gain))
	v.Pan = hundredths(int(sp.Pan))
	v.MixType = nameOr(mixStateNames, int(sp.State))
	v.MixState = int(sp.State)
	v.DelayFrames = int(sp.Delay)
	v.EQEnable = sp.EQEnable
	v.EQGainDB = wireDB(int(sp.EQGain))
	v.DynamicsMakeupDB = wireDB(int(sp.DynamicsGain))
	return v
}

// FairlightStrips is fairlight_strips (messages/fairlight.py), bmdwire
// v1.3.0: every strip with a FASP, in ATEM Software Control's order:
// cameras (FAIP type 0), then media players (1), then mic and line inputs
// (2), each by FAIP number, then subchannel. atemwire's sort is stable
// over the order the strips first arrived in, which the state doesn't
// keep; strips equal on all three are ordered by source here (and by strip
// id, for a hand-built state whose ids name the same source twice).
//
// Display names: a camera takes its InPr short name, else its long name,
// else "Src<source>"; a media player is "MP<number+1>"; type 2 is "Mic"
// for level 1 and "TRS" otherwise; any other type is labelled as a camera.
func (s State) FairlightStrips() []FairlightStripView {
	out := []FairlightStripView{}
	for id, st := range s.Fairlight.Strips {
		if st == nil || st.Properties == nil || !strings.Contains(id, ".") {
			continue
		}
		v := fairlightStrip(id, st.Properties)
		src, srcOK := u16Key(v.Source)
		if ai := s.Fairlight.Inputs[src]; srcOK && ai != nil {
			v.AIType, v.AINumber, v.AILevel = int(ai.Type), int(ai.Number), int(ai.Level)
		}
		var short, long string
		if ip := s.Inputs[src]; srcOK && ip != nil {
			short, long = decodeName(ip.ShortName), decodeName(ip.Name)
		}
		switch v.AIType {
		case 1:
			v.DisplayName = "MP" + strconv.Itoa(v.AINumber+1)
			v.DisplayNameLong = "Media Player " + strconv.Itoa(v.AINumber+1)
		case 2:
			if v.AILevel == 1 {
				v.DisplayName, v.DisplayNameLong = "Mic", "XLR Mic"
			} else {
				v.DisplayName, v.DisplayNameLong = "TRS", "TRS Line In"
			}
		default: // 0, a camera, and anything else
			v.DisplayName = cmp.Or(short, long, "Src"+strconv.Itoa(v.Source))
			v.DisplayNameLong = cmp.Or(long, v.DisplayName)
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b FairlightStripView) int {
		return cmp.Or(cmp.Compare(a.AIType, b.AIType), cmp.Compare(a.AINumber, b.AINumber),
			cmp.Compare(a.Subchannel, b.Subchannel), cmp.Compare(a.Source, b.Source),
			strings.Compare(a.StripID, b.StripID))
	})
	return out
}

// FairlightAudioInputs is fairlight_audio_inputs (messages/fairlight.py),
// bmdwire v1.3.0: every source's FAIP, keyed by source.
func (s State) FairlightAudioInputs() map[int]FairlightAudioInputView {
	out := map[int]FairlightAudioInputView{}
	for src, ai := range s.Fairlight.Inputs {
		var v FairlightAudioInputView
		if ai != nil {
			v = FairlightAudioInputView{Type: int(ai.Type), Split: int(ai.Split), Level: int(ai.Level)}
		} else {
			v.Split = 0x01 // getattr's defaults on a None entry
		}
		out[int(src)] = v
	}
	return out
}

// eqBand is _eq_band_dict (state.py).
func eqBand(index int, enabled bool, possible, filter, freqRange, frequency, gain, q int) EQBandView {
	return EQBandView{
		Index:           index,
		Enabled:         enabled,
		Filter:          nameOr(eqBandFilterNames, filter),
		FilterInt:       filter,
		PossibleFilters: possible,
		Frequency:       frequency,
		GainDB:          hundredths(gain),
		Q:               hundredths(q),
		Range:           nameOr(eqFreqRangeNames, freqRange),
		RangeInt:        freqRange,
	}
}

// FairlightEQBands is fairlight_eq_bands (messages/fairlight.py), bmdwire
// v1.3.0: the EQ bands of strip *stripID, or of the master bus when stripID
// is nil (atemwire's None), by band index; empty before any arrive.
// atemwire's sort is stable over arrival order; here bands are first taken
// in key order, which is the band index for everything the switcher sends.
func (s State) FairlightEQBands(stripID *string) []EQBandView {
	out := []EQBandView{}
	if stripID == nil {
		for _, k := range slices.Sorted(maps.Keys(s.Fairlight.MasterEQBands)) {
			b := s.Fairlight.MasterEQBands[k]
			if b == nil {
				out = append(out, eqBand(0, false, 0, 0, 0, 0, 0, 100))
				continue
			}
			out = append(out, eqBand(int(b.BandIndex), b.BandEnabled, int(b.BandPossibleFilters), int(b.BandFilter),
				int(b.BandFreqRange), int(b.BandFrequency), int(b.BandGain), int(b.BandQ)))
		}
	} else if st := s.Fairlight.Strips[*stripID]; st != nil {
		for _, k := range slices.Sorted(maps.Keys(st.EQBands)) {
			b := st.EQBands[k]
			if b == nil {
				out = append(out, eqBand(0, false, 0, 0, 0, 0, 0, 100))
				continue
			}
			out = append(out, eqBand(int(b.BandIndex), b.BandEnabled, int(b.BandPossibleFilters), int(b.BandFilter),
				int(b.BandFreqRange), int(b.BandFrequency), int(b.BandGain), int(b.BandQ)))
		}
	}
	slices.SortStableFunc(out, func(a, b EQBandView) int { return cmp.Compare(a.Index, b.Index) })
	return out
}

// sortedStripIDs is the Fairlight strip ids in ascending order, so that if
// two entries ever computed the same id the last in this order would win,
// as the last inserted does in atemwire's dict.
func (s State) sortedStripIDs() []string {
	return slices.Sorted(maps.Keys(s.Fairlight.Strips))
}

// FairlightCompressor is fairlight_compressor (messages/fairlight.py),
// bmdwire v1.3.0: every strip's compressor (AICP), keyed by the strip id
// its packet spells.
func (s State) FairlightCompressor() map[string]CompressorView {
	out := map[string]CompressorView{}
	for _, id := range s.sortedStripIDs() {
		if st := s.Fairlight.Strips[id]; st != nil && st.Compressor != nil {
			c := st.Compressor
			out[stripID(c.Index, c.IsSplit, c.Subchannel)] = CompressorView{
				Enabled: c.Enabled, ThresholdDB: hundredths(int(c.Threshold)), AttackMS: hundredths(int(c.Attack)),
				HoldMS: hundredths(int(c.Hold)), ReleaseMS: hundredths(int(c.Release)), Ratio: hundredths(int(c.Ratio)),
			}
		}
	}
	return out
}

// FairlightLimiter is fairlight_limiter (messages/fairlight.py), bmdwire
// v1.3.0: every strip's limiter (AILP), keyed by strip id.
func (s State) FairlightLimiter() map[string]LimiterView {
	out := map[string]LimiterView{}
	for _, id := range s.sortedStripIDs() {
		if st := s.Fairlight.Strips[id]; st != nil && st.Limiter != nil {
			l := st.Limiter
			out[stripID(l.Index, l.IsSplit, l.Subchannel)] = LimiterView{
				Enabled: l.Enabled, ThresholdDB: hundredths(int(l.Threshold)), AttackMS: hundredths(int(l.Attack)),
				HoldMS: hundredths(int(l.Hold)), ReleaseMS: hundredths(int(l.Release)),
			}
		}
	}
	return out
}

// FairlightExpander is fairlight_expander (messages/fairlight.py), bmdwire
// v1.3.0: every strip's expander or gate (AIXP), keyed by strip id. Mode
// is "gate" for any non-zero mode byte.
func (s State) FairlightExpander() map[string]ExpanderView {
	out := map[string]ExpanderView{}
	for _, id := range s.sortedStripIDs() {
		if st := s.Fairlight.Strips[id]; st != nil && st.Expander != nil {
			x := st.Expander
			mode := "expander"
			if x.Mode != 0 {
				mode = "gate"
			}
			out[stripID(x.Index, x.IsSplit, x.Subchannel)] = ExpanderView{
				Enabled: x.Enabled, ThresholdDB: hundredths(int(x.Threshold)), AttackMS: hundredths(int(x.Attack)),
				HoldMS: hundredths(int(x.Hold)), ReleaseMS: hundredths(int(x.Release)), Ratio: hundredths(int(x.Ratio)),
				RangeDB: hundredths(int(x.Range)), Mode: mode,
			}
		}
	}
	return out
}

// FairlightMasterCompressor is fairlight_master_compressor
// (messages/fairlight.py), bmdwire v1.3.0: the master bus compressor
// (MOCP), or nil (atemwire's None) before it arrives.
func (s State) FairlightMasterCompressor() *CompressorView {
	c := s.Fairlight.MasterCompressor
	if c == nil {
		return nil
	}
	return &CompressorView{
		Enabled: c.Enabled, ThresholdDB: hundredths(int(c.Threshold)), AttackMS: hundredths(int(c.Attack)),
		HoldMS: hundredths(int(c.Hold)), ReleaseMS: hundredths(int(c.Release)), Ratio: hundredths(int(c.Ratio)),
	}
}

// FairlightMasterLimiter is fairlight_master_limiter
// (messages/fairlight.py), bmdwire v1.3.0: the master bus limiter (AMLP),
// or nil (atemwire's None) before it arrives.
func (s State) FairlightMasterLimiter() *LimiterView {
	l := s.Fairlight.MasterLimiter
	if l == nil {
		return nil
	}
	return &LimiterView{
		Enabled: l.Enabled, ThresholdDB: hundredths(int(l.Threshold)), AttackMS: hundredths(int(l.Attack)),
		HoldMS: hundredths(int(l.Hold)), ReleaseMS: hundredths(int(l.Release)),
	}
}

// FairlightHeadphones is fairlight_headphones (messages/fairlight.py),
// bmdwire v1.3.0: the headphone output (FMHP); before it, not present,
// 0.0 dB and unmuted.
func (s State) FairlightHeadphones() FairlightHeadphonesView {
	h := s.Fairlight.Headphones
	if h == nil {
		return FairlightHeadphonesView{Unmuted: true}
	}
	return FairlightHeadphonesView{Present: true, VolumeDB: wireDB(int(h.Volume)), Unmuted: h.Unmuted}
}

// FairlightSolo is fairlight_solo (messages/fairlight.py), bmdwire v1.3.0:
// whether a strip is soloed (FAMS) and which: "<channel>.<subchannel>" when
// is_split_lr is 0xFF, else "<channel>.0". atemwire notes the split test is
// not yet checked against a capture.
func (s State) FairlightSolo() FairlightSoloView {
	f := s.Fairlight.Solo
	if f == nil || !f.Solo {
		return FairlightSoloView{}
	}
	id := strconv.Itoa(int(f.Channel)) + ".0"
	if f.IsSplitLR == 0xff {
		id = strconv.Itoa(int(f.Channel)) + "." + strconv.Itoa(int(f.Subchannel))
	}
	return FairlightSoloView{AnySoloed: true, StripID: &id}
}
