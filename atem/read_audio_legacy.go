// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The legacy (pre-Fairlight) audio mixer's readers, units and log lines,
// from bmdwire v1.6.0 atemwire/messages/audio_legacy.py (bmdwire#49,
// bd2f4bb and 45fafa4). The readers give the Fairlight readers' shapes,
// less what the legacy mixer doesn't have (input gain, EQ, dynamics,
// delay). Volumes are dB by LegacyVolumeToDB, nil for silence; pan is
// balance ÷ 100. None of it has met a legacy switcher: it is built from the
// packet layouts, which agree with LibAtem's and atem-connection's. See
// read_state.go for the conventions.

import (
	"cmp"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// The legacy mixer's volume (AMIP, AMMO, AMmO and the CAMI, CAMM and CAMm
// writes) is a u16 gain: LegacyVolumeUnity is 0 dB, LegacyVolumeMax the
// top of the fader (+6 dB), 0 silence. Balance is an i16, ±10000 at the
// ends. The scales are LibAtem's and atem-connection's.
const (
	LegacyVolumeUnity = 32768
	LegacyVolumeMax   = 65381
)

// LegacyVolumeToDB is legacy_volume_to_db: a wire volume in dB, two
// places, 20·log10(v / 32768); nil (atemwire's None) for 0, silence, and
// below.
func LegacyVolumeToDB(volume int) *float64 {
	if volume <= 0 {
		return nil
	}
	db := pyRound(20*math.Log10(float64(volume)/LegacyVolumeUnity), 2)
	return &db
}

// LegacyDBToVolume is legacy_db_to_volume: dB as a wire volume,
// floor(10^(dB/20) × 32768) clamped to 0-65381, dB over 20 taken as 20.
// Nil (atemwire's None), -inf and NaN are silence, 0. +6.0 dB gives 65380,
// one step under the top: 65381 is +6.00003 dB.
func LegacyDBToVolume(db *float64) uint16 {
	if db == nil || math.IsNaN(*db) || math.IsInf(*db, -1) {
		return 0
	}
	v := math.Floor(math.Pow(10, min(*db, 20.0)/20) * LegacyVolumeUnity)
	return uint16(max(0, min(LegacyVolumeMax, v)))
}

// LegacyPanToBalance is legacy_pan_to_balance: pan ±100, as the strips
// show it, as wire balance, round(pan × 100) half to even, clamped to
// ±10000. NaN and the infinities are an error, where Python's round()
// raises.
func LegacyPanToBalance(pan float64) (int16, error) {
	b := math.RoundToEven(pan * 100)
	if math.IsNaN(b) || math.IsInf(b, 0) {
		return 0, fmt.Errorf("atem: pan %v is not a finite number", pan)
	}
	return int16(max(-10000, min(10000, b))), nil
}

// LegacyAudioMasterView is legacy_audio_master's dict: fairlight_master's
// keys without the EQ and dynamics ones. VolumeDB is nil when silenced.
type LegacyAudioMasterView struct {
	Present  bool     `json:"present"`
	VolumeDB *float64 `json:"volume_db"`
	AFV      bool     `json:"afv"`
}

// LegacyAudioStripView is one entry of legacy_audio_strips, shaped like a
// fairlight_strips entry: every legacy strip is stereo (Channel -1,
// StripID "<source>.0"). MixType takes the Fairlight names (Off, On,
// AudioFollowVideo, else the number); MixState is the legacy wire value
// (0, 1, 2), not Fairlight's bitfield. InputType is AMIP's type (0 video
// input, 1 media player, 2 external audio); Plug is PlugName's.
type LegacyAudioStripView struct {
	Source     int      `json:"source"`
	Channel    int      `json:"channel"`
	Subchannel int      `json:"subchannel"`
	Split      bool     `json:"split"`
	StripID    string   `json:"strip_id"`
	Name       string   `json:"name"`
	ShortName  string   `json:"short_name"`
	MixType    string   `json:"mix_type"`
	MixState   int      `json:"mix_state"`
	VolumeDB   *float64 `json:"volume_db"`
	Pan        float64  `json:"pan"`
	InputType  int      `json:"input_type"`
	Plug       string   `json:"plug"`
}

// LegacyAudioHeadphonesView is legacy_audio_headphones's dict: the monitor
// bus (AMmO) where fairlight_headphones reads FMHP, then the monitor's own
// keys. VolumeDB is nil when silenced; DimLevel is the wire value ÷ 100, a
// level 0-100 (LibAtem's reading), not dB; SoloSource is nil before AMmO.
type LegacyAudioHeadphonesView struct {
	Present    bool     `json:"present"`
	VolumeDB   *float64 `json:"volume_db"`
	Unmuted    bool     `json:"unmuted"`
	Enabled    bool     `json:"enabled"`
	Dim        bool     `json:"dim"`
	DimLevel   float64  `json:"dim_level"`
	Solo       bool     `json:"solo"`
	SoloSource *int     `json:"solo_source"`
}

// LegacyAudioSoloView is legacy_audio_solo's dict, fairlight_solo's shape.
// StripID is nil when nothing is soloed.
type LegacyAudioSoloView struct {
	AnySoloed bool    `json:"any_soloed"`
	StripID   *string `json:"strip_id"`
}

// audio_legacy.py's _LEGACY_MIX_OPTION_NAMES and
// _LEGACY_EXTERNAL_AUDIO_NAMES: external audio inputs have no InPr to
// name them by (source ids from LibAtem's AudioSource).
var (
	legacyMixOptionNames      = map[int]string{0: "Off", 1: "On", 2: "AudioFollowVideo"}
	legacyExternalAudioNames  = map[int][2]string{1001: {"XLR", "XLR"}, 1101: {"AES", "AES/EBU"}, 1201: {"RCA", "RCA"}, 1301: {"Mic1", "Mic 1"}, 1302: {"Mic2", "Mic 2"}}
	legacyAudioDiagnosticKeys = map[string]bool{"AMIP": true, "AMMO": true, "AMmO": true, "AMLv": true}
)

// AudioMixerType is audio_mixer_type (messages/audio_legacy.py), bmdwire
// v1.6.0: which audio mixer the switcher has, from what it has sent:
// "fairlight" once FAMP has arrived, "legacy" once AMMO has, "unknown"
// before either (and on a switcher that sends neither).
func (s State) AudioMixerType() string {
	switch {
	case s.Fairlight.Master != nil:
		return "fairlight"
	case s.Audio.Master != nil:
		return "legacy"
	}
	return "unknown"
}

// LegacyAudioMaster is legacy_audio_master (messages/audio_legacy.py),
// bmdwire v1.6.0: the master bus (AMMO); before AMMO, Present is false and
// VolumeDB 0.0, not nil.
func (s State) LegacyAudioMaster() LegacyAudioMasterView {
	m := s.Audio.Master
	if m == nil {
		zero := 0.0
		return LegacyAudioMasterView{VolumeDB: &zero}
	}
	return LegacyAudioMasterView{Present: true, VolumeDB: LegacyVolumeToDB(int(m.Volume)), AFV: m.AFV}
}

// LegacyAudioStrips is legacy_audio_strips (messages/audio_legacy.py),
// bmdwire v1.6.0: one strip per AMIP source, video inputs (type 0) first,
// then media players (1), then external audio (2), each by source.
//
// Names as FairlightStrips gives them: a media player (type 1, source
// 2001-2099) is "MP<n>" and "Media Player <n>" for source 2000 + n; an
// external audio input (type 2) is named by its source (XLR, AES, RCA,
// Mic1, Mic2); anything else takes its InPr short name, else its long
// name, else "Src<source>", and its long name, else the short one.
func (s State) LegacyAudioStrips() []LegacyAudioStripView {
	out := []LegacyAudioStripView{}
	for _, in := range s.Audio.Inputs {
		if in == nil {
			continue
		}
		source, inputType, state := int(in.Index), int(in.Type), int(in.State)
		var short, long string
		ext, isExt := legacyExternalAudioNames[source]
		switch {
		case inputType == 1 && 2001 <= source && source <= 2099:
			n := strconv.Itoa(source - 2000)
			short, long = "MP"+n, "Media Player "+n
		case inputType == 2 && isExt:
			short, long = ext[0], ext[1]
		default:
			var ipShort, ipLong string
			if ip := s.Inputs[in.Index]; ip != nil {
				ipShort, ipLong = decodeName(ip.ShortName), decodeName(ip.Name)
			}
			short = cmp.Or(ipShort, ipLong, "Src"+strconv.Itoa(source))
			long = cmp.Or(ipLong, short)
		}
		out = append(out, LegacyAudioStripView{
			Source:    source,
			Channel:   -1,
			StripID:   strconv.Itoa(source) + ".0",
			Name:      long,
			ShortName: short,
			MixType:   nameOr(legacyMixOptionNames, state),
			MixState:  state,
			VolumeDB:  LegacyVolumeToDB(int(in.Volume)),
			Pan:       hundredths(int(in.Balance)),
			InputType: inputType,
			Plug:      in.PlugName(),
		})
	}
	// atemwire's sort is stable over arrival order, which the state doesn't
	// keep; the strip id breaks a tie only a hand-built state can have
	// (AMIP is keyed by its own source).
	slices.SortFunc(out, func(a, b LegacyAudioStripView) int {
		return cmp.Or(cmp.Compare(a.InputType, b.InputType), cmp.Compare(a.Source, b.Source), strings.Compare(a.StripID, b.StripID))
	})
	return out
}

// LegacyAudioHeadphones is legacy_audio_headphones
// (messages/audio_legacy.py), bmdwire v1.6.0: the monitor bus (AMmO);
// before it, not present, 0.0 dB, unmuted, and no solo source.
func (s State) LegacyAudioHeadphones() LegacyAudioHeadphonesView {
	m := s.Audio.Monitor
	if m == nil {
		zero := 0.0
		return LegacyAudioHeadphonesView{VolumeDB: &zero, Unmuted: true}
	}
	solo := int(m.SoloSource)
	return LegacyAudioHeadphonesView{
		Present:    true,
		VolumeDB:   LegacyVolumeToDB(int(m.Volume)),
		Unmuted:    !m.Mute,
		Enabled:    m.Enabled,
		Dim:        m.Dim,
		DimLevel:   hundredths(int(m.DimVolume)),
		Solo:       m.Solo,
		SoloSource: &solo,
	}
}

// LegacyAudioSolo is legacy_audio_solo (messages/audio_legacy.py), bmdwire
// v1.6.0: whether the monitor solos a strip (AMmO) and which,
// "<solo_source>.0".
func (s State) LegacyAudioSolo() LegacyAudioSoloView {
	m := s.Audio.Monitor
	if m == nil || !m.Solo {
		return LegacyAudioSoloView{}
	}
	id := strconv.Itoa(int(m.SoloSource)) + ".0"
	return LegacyAudioSoloView{AnySoloed: true, StripID: &id}
}

// legacyAudioDiagnostic is legacy_audio_diagnostic, bmdwire v1.6.0
// (bmdwire#49, 45fafa4): one log line for a legacy-audio packet, its code
// (and source, for AMIP), the payload in hex and every attribute atemwire
// decodes, in its order and as Python prints them, plus volume_db for a
// volume and plug_name for a plug. AMLv levels are rounded to 0.1 dB. v is
// what parsing payload stored.
func legacyAudioDiagnostic(code string, payload []byte, v any) string {
	var where, decoded string
	switch f := v.(type) {
	case *AudioInputField:
		where = fmt.Sprintf(" source %d", f.Index)
		decoded = fmt.Sprintf("index=%d type=%d number=%d plug=%d state=%d volume=%d balance=%d volume_db=%s plug_name=%s",
			f.Index, f.Type, f.Number, f.Plug, f.State, f.Volume, f.Balance, pyOptFloatRepr(LegacyVolumeToDB(int(f.Volume))), f.PlugName())
	case *AudioMixerMasterPropertiesField:
		decoded = fmt.Sprintf("volume=%d afv=%s volume_db=%s", f.Volume, pyBoolRepr(f.AFV), pyOptFloatRepr(LegacyVolumeToDB(int(f.Volume))))
	case *AudioMixerMonitorPropertiesField:
		decoded = fmt.Sprintf("enabled=%s volume=%d mute=%s solo=%s solo_source=%d dim=%s dim_volume=%d volume_db=%s",
			pyBoolRepr(f.Enabled), f.Volume, pyBoolRepr(f.Mute), pyBoolRepr(f.Solo), f.SoloSource, pyBoolRepr(f.Dim), f.DimVolume,
			pyOptFloatRepr(LegacyVolumeToDB(int(f.Volume))))
	case *AudioMeterLevelsField:
		// The input dict in the packet's order, which the map doesn't keep.
		inputs := make([]string, 0, f.Count)
		for i := range int(f.Count) {
			src := u16(payload, 36+2*i)
			inputs = append(inputs, fmt.Sprintf("%d: %s", src, meterTuple(f.Input[src])))
		}
		decoded = fmt.Sprintf("count=%d master=%s monitor=%s input={%s}",
			f.Count, meterTuple(f.Master), meterTuple(f.Monitor), strings.Join(inputs, ", "))
	}
	return fmt.Sprintf("%s%s, %d bytes: %s -> %s", code, where, len(payload), hex.EncodeToString(payload), decoded)
}

// meterTuple is an AMLv level tuple as legacy_audio_diagnostic prints it:
// each level rounded to 0.1 dB, except the -60 a silent level is, which
// meters.py gives as an int (no level above silence comes out at exactly
// -60.0).
func meterTuple(l [4]float64) string {
	parts := make([]string, 4)
	for i, x := range l {
		if x == -60 {
			parts[i] = "-60"
		} else {
			parts[i] = pyFloatRepr(pyRound(x, 1))
		}
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// pyFloatRepr is Python's repr of a float in the range a level or a gain
// takes: the shortest digits, and ".0" for a whole number.
func pyFloatRepr(x float64) string {
	s := strconv.FormatFloat(x, 'f', -1, 64)
	if !strings.ContainsAny(s, ".NI") {
		s += ".0"
	}
	return s
}

// pyOptFloatRepr is pyFloatRepr, and None for nil.
func pyOptFloatRepr(x *float64) string {
	if x == nil {
		return "None"
	}
	return pyFloatRepr(*x)
}

// pyBoolRepr is Python's repr of a bool.
func pyBoolRepr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
