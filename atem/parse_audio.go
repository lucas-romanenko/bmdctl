// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Parsers for SURFACE.md §2.4 (Fairlight, the classic audio mixer and
// meters), from bmdwire v1.3.0 atemwire/messages/fairlight.py,
// audio_legacy.py and meters.py.

import (
	"fmt"
	"math"
)

var audioParsers = []parser{
	entry("FAMP", "fairlight-master-properties", parseFAMP, func(s *State, f *FairlightMasterPropertiesField) string {
		s.Fairlight.Master = f
		return "FAMP"
	}),
	entry("FASP", "fairlight-strip-properties", parseFASP, func(s *State, f *FairlightStripPropertiesField) string {
		strip(s, f.StripID).Properties = f
		return key("FASP", f.StripID)
	}),
	entry("AEBP", "atem-eq-band-properties", parseAEBP, func(s *State, f *AtemEQBandPropertiesField) string {
		st := strip(s, f.StripID)
		if st.EQBands == nil {
			st.EQBands = make(map[uint8]*AtemEQBandPropertiesField)
		}
		st.EQBands[f.BandIndex] = f
		return key("AEBP", f.StripID, f.BandIndex)
	}),
	entry("AMBP", "atem-master-eq-band-properties", parseAMBP, func(s *State, f *AtemMasterEQBandPropertiesField) string {
		s.Fairlight.MasterEQBands[f.BandIndex] = f
		return key("AMBP", f.BandIndex)
	}),
	entry("AICP", "fairlight-compressor-properties", parseAICP, func(s *State, f *FairlightCompressorPropertiesField) string {
		strip(s, f.StripID).Compressor = f
		return key("AICP", f.StripID)
	}),
	entry("AILP", "fairlight-limiter-properties", parseAILP, func(s *State, f *FairlightLimiterPropertiesField) string {
		strip(s, f.StripID).Limiter = f
		return key("AILP", f.StripID)
	}),
	entry("AIXP", "fairlight-expander-properties", parseAIXP, func(s *State, f *FairlightExpanderPropertiesField) string {
		strip(s, f.StripID).Expander = f
		return key("AIXP", f.StripID)
	}),
	entry("MOCP", "fairlight-master-compressor-properties", parseMOCP, func(s *State, f *FairlightMasterCompressorPropertiesField) string {
		s.Fairlight.MasterCompressor = f
		return "MOCP"
	}),
	entry("AMLP", "fairlight-master-limiter-properties", parseAMLP, func(s *State, f *FairlightMasterLimiterPropertiesField) string {
		s.Fairlight.MasterLimiter = f
		return "AMLP"
	}),
	entry("FAIP", "fairlight-audio-input", parseFAIP, func(s *State, f *FairlightAudioInputField) string {
		s.Fairlight.Inputs[f.Index] = f
		return key("FAIP", f.Index)
	}),
	entry("FMTl", "fairlight-tally", parseFMTl, func(s *State, f *FairlightTallyField) string {
		s.Fairlight.Tally = f
		return "FMTl"
	}),
	entry("FMHP", "fairlight-headphones", parseFMHP, func(s *State, f *FairlightHeadphonesField) string {
		s.Fairlight.Headphones = f
		return "FMHP"
	}),
	entry("FAMS", "fairlight-solo", parseFAMS, func(s *State, f *FairlightSoloField) string {
		s.Fairlight.Solo = f
		return "FAMS"
	}),
	entry("FASD", "fairlight-strip-delete", parseFASD, func(s *State, f *FairlightStripDeleteField) string {
		s.Fairlight.StripDelete = f
		return "FASD"
	}),
	entry("FMLv", "fairlight-meter-levels", parseFMLv, func(s *State, f *FairlightMeterLevelsField) string {
		s.Fairlight.MeterLevels = f
		return "FMLv"
	}),
	entry("FDLv", "fairlight-master-levels", parseFDLv, func(s *State, f *FairlightMasterLevelsField) string {
		s.Fairlight.MasterLevels = f
		return "FDLv"
	}),
	entry("AMIP", "audio-input", parseAMIP, func(s *State, f *AudioInputField) string {
		s.Audio.Inputs[f.StripID] = f
		return key("AMIP", f.StripID)
	}),
	entry("AMMO", "audio-mixer-master-properties", parseAMMO, func(s *State, f *AudioMixerMasterPropertiesField) string {
		s.Audio.Master = f
		return "AMMO"
	}),
	entry("AMmO", "audio-mixer-monitor-properties", parseAMmO, func(s *State, f *AudioMixerMonitorPropertiesField) string {
		s.Audio.Monitor = f
		return "AMmO"
	}),
	entry("AMTl", "audio-mixer-tally", parseAMTl, func(s *State, f *AudioMixerTallyField) string {
		s.Audio.Tally = f
		return "AMTl"
	}),
	entry("AMLv", "audio-meter-levels", parseAMLv, func(s *State, f *AudioMeterLevelsField) string {
		s.Audio.Levels = f
		return "AMLv"
	}),
}

// strip returns the Fairlight strip with the given id, creating it.
func strip(s *State, id string) *FairlightStrip {
	st := s.Fairlight.Strips[id]
	if st == nil {
		st = &FairlightStrip{}
		s.Fairlight.Strips[id] = st
	}
	return st
}

// parseFAMP: struct.unpack('>x ? 4x h 2x H i ? 3x'), 20 bytes.
func parseFAMP(b []byte) (*FairlightMasterPropertiesField, error) {
	if err := exactly("FAMP", b, 20); err != nil {
		return nil, err
	}
	return &FairlightMasterPropertiesField{
		EQEnable: flag(b, 1), EQGain: i16(b, 6), DynamicsGain: u16(b, 10), Volume: i32(b, 12), AFV: flag(b, 16),
	}, nil
}

// parseFASP: struct.unpack('>H 12xBBxxB 3x h 5x ? 4x h 2x Hh 4x h x B 2x'),
// 52 bytes. The delay is at byte 18 (atemwire moved it from 17).
func parseFASP(b []byte) (*FairlightStripPropertiesField, error) {
	if err := exactly("FASP", b, 52); err != nil {
		return nil, err
	}
	f := &FairlightStripPropertiesField{
		Index: u16(b, 0), IsSplit: b[14], Subchannel: b[15], Delay: b[18], Gain: i16(b, 22),
		EQEnable: flag(b, 29), EQGain: i16(b, 34), DynamicsGain: u16(b, 38), Pan: i16(b, 40),
		Volume: i16(b, 46), State: b[49],
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseAEBP: struct.unpack('>H 2x 4x 6x BB B ? B B x B 4x H i H 2x'), 36
// bytes.
func parseAEBP(b []byte) (*AtemEQBandPropertiesField, error) {
	if err := exactly("AEBP", b, 36); err != nil {
		return nil, err
	}
	f := &AtemEQBandPropertiesField{
		Index: u16(b, 0), IsSplit: b[14], Subchannel: b[15], BandIndex: b[16], BandEnabled: flag(b, 17),
		BandPossibleFilters: b[18], BandFilter: b[19], BandFreqRange: b[21],
		BandFrequency: u16(b, 26), BandGain: i32(b, 28), BandQ: u16(b, 32),
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseAMBP: struct.unpack('>B ? B B x B 4x H i H 2x'), 20 bytes: AEBP's
// band fields without the strip prefix.
func parseAMBP(b []byte) (*AtemMasterEQBandPropertiesField, error) {
	if err := exactly("AMBP", b, 20); err != nil {
		return nil, err
	}
	return &AtemMasterEQBandPropertiesField{
		BandIndex: b[0], BandEnabled: flag(b, 1), BandPossibleFilters: b[2], BandFilter: b[3],
		BandFreqRange: b[5], BandFrequency: u16(b, 10), BandGain: i32(b, 12), BandQ: u16(b, 16),
	}, nil
}

// parseAICP: struct.unpack('>H 12xBB ?3x i H 2x i i i'), 40 bytes.
func parseAICP(b []byte) (*FairlightCompressorPropertiesField, error) {
	if err := exactly("AICP", b, 40); err != nil {
		return nil, err
	}
	f := &FairlightCompressorPropertiesField{
		Index: u16(b, 0), IsSplit: b[14], Subchannel: b[15], Enabled: flag(b, 16),
		Threshold: i32(b, 20), Ratio: u16(b, 24), Attack: i32(b, 28), Hold: i32(b, 32), Release: i32(b, 36),
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseAILP: struct.unpack('>H 12xBB ?3x i i i i'), 36 bytes.
func parseAILP(b []byte) (*FairlightLimiterPropertiesField, error) {
	if err := exactly("AILP", b, 36); err != nil {
		return nil, err
	}
	f := &FairlightLimiterPropertiesField{
		Index: u16(b, 0), IsSplit: b[14], Subchannel: b[15], Enabled: flag(b, 16),
		Threshold: i32(b, 20), Attack: i32(b, 24), Hold: i32(b, 28), Release: i32(b, 32),
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseAIXP: struct.unpack('>H 12xBB ?B2x i H H i i i'), 40 bytes.
func parseAIXP(b []byte) (*FairlightExpanderPropertiesField, error) {
	if err := exactly("AIXP", b, 40); err != nil {
		return nil, err
	}
	f := &FairlightExpanderPropertiesField{
		Index: u16(b, 0), IsSplit: b[14], Subchannel: b[15], Enabled: flag(b, 16), Mode: b[17],
		Threshold: i32(b, 20), Range: u16(b, 24), Ratio: u16(b, 26),
		Attack: i32(b, 28), Hold: i32(b, 32), Release: i32(b, 36),
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseMOCP: struct.unpack('>B 5x h H 2x i i i'), 24 bytes: the compact
// master layout, threshold s16 at 6.
func parseMOCP(b []byte) (*FairlightMasterCompressorPropertiesField, error) {
	if err := exactly("MOCP", b, 24); err != nil {
		return nil, err
	}
	return &FairlightMasterCompressorPropertiesField{
		Enabled: b[0] != 0, Threshold: i16(b, 6), Ratio: u16(b, 8), Attack: i32(b, 12), Hold: i32(b, 16), Release: i32(b, 20),
	}, nil
}

// parseAMLP: struct.unpack('>B 5x h i i i'), 20 bytes.
func parseAMLP(b []byte) (*FairlightMasterLimiterPropertiesField, error) {
	if err := exactly("AMLP", b, 20); err != nil {
		return nil, err
	}
	return &FairlightMasterLimiterPropertiesField{
		Enabled: b[0] != 0, Threshold: i16(b, 6), Attack: i32(b, 8), Hold: i32(b, 12), Release: i32(b, 16),
	}, nil
}

// parseFAIP: struct.unpack('>HB 2x B xxxx B x B 3x'), 16 bytes.
func parseFAIP(b []byte) (*FairlightAudioInputField, error) {
	if err := exactly("FAIP", b, 16); err != nil {
		return nil, err
	}
	return &FairlightAudioInputField{Index: u16(b, 0), Type: b[2], Number: b[5], Split: b[10], Level: b[12]}, nil
}

// parseFMTl: a count, then from byte 15 one 11-byte entry per strip whose
// first four bytes are subchannel, source and tally.
func parseFMTl(b []byte) (*FairlightTallyField, error) {
	if err := atLeast("FMTl", b, 2); err != nil {
		return nil, err
	}
	f := &FairlightTallyField{Num: u16(b, 0), Tally: make(map[string]bool)}
	off := 15
	for range int(f.Num) {
		if err := atLeast("FMTl", b, off+4); err != nil {
			return nil, err
		}
		f.Tally[fmt.Sprintf("%d.%d", u16(b, off+1), b[off])] = flag(b, off+3)
		off += 11
	}
	return f, nil
}

// parseFMHP: struct.unpack('> i 4x ? 23x'), 32 bytes.
func parseFMHP(b []byte) (*FairlightHeadphonesField, error) {
	if err := exactly("FMHP", b, 32); err != nil {
		return nil, err
	}
	return &FairlightHeadphonesField{Volume: i32(b, 0), Unmuted: flag(b, 8)}, nil
}

// parseFAMS: struct.unpack('> ? 8x B 12x BB'), 24 bytes.
func parseFAMS(b []byte) (*FairlightSoloField, error) {
	if err := exactly("FAMS", b, 24); err != nil {
		return nil, err
	}
	return &FairlightSoloField{Solo: flag(b, 0), Channel: b[9], IsSplitLR: b[22], Subchannel: b[23]}, nil
}

// parseFASD keeps the raw payload; atemwire parses nothing from it.
func parseFASD(b []byte) (*FairlightStripDeleteField, error) {
	return &FairlightStripDeleteField{Raw: append([]byte{}, b...)}, nil
}

// fairlightLevel is the Fairlight meters' conversion: i16 hundredths of a
// dB, divided linearly.
func fairlightLevel(b []byte, off int) float64 { return float64(i16(b, off)) / 100.0 }

func fairlightLevels(b []byte, off int) [4]float64 {
	return [4]float64{fairlightLevel(b, off), fairlightLevel(b, off+2), fairlightLevel(b, off+4), fairlightLevel(b, off+6)}
}

// parseFMLv: '>6xBBH 15h' from the start.
func parseFMLv(b []byte) (*FairlightMeterLevelsField, error) {
	if err := atLeast("FMLv", b, 40); err != nil {
		return nil, err
	}
	f := &FairlightMeterLevelsField{
		IsSplit: b[6], Subchannel: b[7], Index: u16(b, 8),
		Input:      fairlightLevels(b, 10),
		ExpanderGR: fairlightLevel(b, 18), CompressorGR: fairlightLevel(b, 20), LimiterGR: fairlightLevel(b, 22),
		Output: fairlightLevels(b, 24), Level: fairlightLevels(b, 32),
	}
	f.StripID = stripID(f.Index, f.IsSplit, f.Subchannel)
	return f, nil
}

// parseFDLv: '>14h' from the start.
func parseFDLv(b []byte) (*FairlightMasterLevelsField, error) {
	if err := atLeast("FDLv", b, 28); err != nil {
		return nil, err
	}
	return &FairlightMasterLevelsField{
		Input: fairlightLevels(b, 0), CompressorGR: fairlightLevel(b, 8), LimiterGR: fairlightLevel(b, 10),
		Output: fairlightLevels(b, 12), Level: fairlightLevels(b, 20),
	}, nil
}

// parseAMIP: struct.unpack_from('>HB 2x B x BB x Hh 2x'), the first 16
// bytes. AMIP is 16 bytes from ATEM 8.0 firmware on and 20 before it, the
// first 14 the same in both (LibAtem's AudioMixerInputGetCommand and its
// V8 twin), so either parses. Until bmdwire v1.6.0 atemwire wanted
// exactly 16, so an older switcher's AMIP ended its connection there, and
// lost the rest of its packet here (bmdwire#49, 626c785).
func parseAMIP(b []byte) (*AudioInputField, error) {
	if err := atLeast("AMIP", b, 16); err != nil {
		return nil, err
	}
	f := &AudioInputField{
		Index: u16(b, 0), Type: b[2], Number: b[5], Plug: b[7], State: b[8], Volume: u16(b, 10), Balance: i16(b, 12),
	}
	f.StripID = fmt.Sprintf("%d.0", f.Index)
	return f, nil
}

func parseAMMO(b []byte) (*AudioMixerMasterPropertiesField, error) {
	if err := atLeast("AMMO", b, 5); err != nil {
		return nil, err
	}
	return &AudioMixerMasterPropertiesField{Volume: u16(b, 0), AFV: flag(b, 4)}, nil
}

func parseAMmO(b []byte) (*AudioMixerMonitorPropertiesField, error) {
	if err := atLeast("AMmO", b, 12); err != nil {
		return nil, err
	}
	return &AudioMixerMonitorPropertiesField{
		Enabled: flag(b, 0), Volume: u16(b, 2), Mute: flag(b, 4), Solo: flag(b, 5),
		SoloSource: u16(b, 6), Dim: flag(b, 8), DimVolume: u16(b, 10),
	}, nil
}

// parseAMTl: a count, then (source u16, tally) per input, keyed "<source>.0".
func parseAMTl(b []byte) (*AudioMixerTallyField, error) {
	if err := atLeast("AMTl", b, 2); err != nil {
		return nil, err
	}
	f := &AudioMixerTallyField{Num: u16(b, 0), Tally: make(map[string]bool)}
	off := 2
	for range int(f.Num) {
		if err := atLeast("AMTl", b, off+3); err != nil {
			return nil, err
		}
		f.Tally[fmt.Sprintf("%d.0", u16(b, off))] = flag(b, off+2)
		off += 3
	}
	return f, nil
}

// classicLevel is the classic mixer's meter conversion: u32 linear,
// 128 × 65536 full scale, in dB, with 0 read as -60.
func classicLevel(v uint32) float64 {
	if v == 0 {
		return -60
	}
	return math.Log10(float64(v)/(128*65536)) * 20
}

func classicLevels(b []byte, off int) [4]float64 {
	return [4]float64{classicLevel(u32(b, off)), classicLevel(u32(b, off+4)), classicLevel(u32(b, off+8)), classicLevel(u32(b, off+12))}
}

// parseAMLv: '>H2x 4I 4I' (count, master, monitor), the count's sources as
// u16, padded to 4 bytes, then four u32 levels per source.
func parseAMLv(b []byte) (*AudioMeterLevelsField, error) {
	if err := atLeast("AMLv", b, 36); err != nil {
		return nil, err
	}
	f := &AudioMeterLevelsField{
		Count: u16(b, 0), Master: classicLevels(b, 4), Monitor: classicLevels(b, 20),
		Input: make(map[uint16][4]float64),
	}
	n := int(f.Count)
	if err := atLeast("AMLv", b, 36+2*n); err != nil {
		return nil, err
	}
	off := (36 + 2*n + 3) / 4 * 4
	if err := atLeast("AMLv", b, off+16*n); err != nil {
		return nil, err
	}
	for i := range n {
		f.Input[u16(b, 36+2*i)] = classicLevels(b, off+16*i)
	}
	return f, nil
}
