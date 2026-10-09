// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Parsers for SURFACE.md §2.2 (mixeffect, aux, transition), from bmdwire
// v1.3.0 atemwire/messages/switching.py, transition.py and fade_to_black.py.

var mixeffectParsers = []parser{
	entry("PrgI", "program-bus-input", parsePrgI, func(s *State, f *ProgramBusInputField) string {
		at(&s.MEs, int(f.Index)).Program = f
		return key("PrgI", f.Index)
	}),
	entry("PrvI", "preview-bus-input", parsePrvI, func(s *State, f *PreviewBusInputField) string {
		at(&s.MEs, int(f.Index)).Preview = f
		return key("PrvI", f.Index)
	}),
	entry("AuxS", "aux-output-source", parseAuxS, func(s *State, f *AuxOutputSourceField) string {
		*at(&s.Aux, int(f.Index)) = f
		return key("AuxS", f.Index)
	}),
	entry("TrSS", "transition-settings", parseTrSS, func(s *State, f *TransitionSettingsField) string {
		at(&s.MEs, int(f.Index)).Transition.Settings = f
		return key("TrSS", f.Index)
	}),
	entry("TrPr", "transition-preview", parseTrPr, func(s *State, f *TransitionPreviewField) string {
		at(&s.MEs, int(f.Index)).Transition.Preview = f
		return key("TrPr", f.Index)
	}),
	entry("TrPs", "transition-position", parseTrPs, func(s *State, f *TransitionPositionField) string {
		at(&s.MEs, int(f.Index)).Transition.Position = f
		return key("TrPs", f.Index)
	}),
	entry("TMxP", "transition-mix", parseTMxP, func(s *State, f *TransitionMixField) string {
		at(&s.MEs, int(f.Index)).Transition.Mix = f
		return key("TMxP", f.Index)
	}),
	entry("TDpP", "transition-dip", parseTDpP, func(s *State, f *TransitionDipField) string {
		at(&s.MEs, int(f.Index)).Transition.Dip = f
		return key("TDpP", f.Index)
	}),
	entry("TWpP", "transition-wipe", parseTWpP, func(s *State, f *TransitionWipeField) string {
		at(&s.MEs, int(f.Index)).Transition.Wipe = f
		return key("TWpP", f.Index)
	}),
	entry("TDvP", "transition-dve", parseTDvP, func(s *State, f *TransitionDVEField) string {
		at(&s.MEs, int(f.Index)).Transition.DVE = f
		return key("TDvP", f.Index)
	}),
	entry("TStP", "transition-stinger", parseTStP, func(s *State, f *TransitionStingerField) string {
		at(&s.MEs, int(f.Index)).Transition.Stinger = f
		return key("TStP", f.Index)
	}),
	entry("FtbS", "fade-to-black-state", parseFtbS, func(s *State, f *FadeToBlackStateField) string {
		at(&s.MEs, int(f.Index)).FTB.State = f
		return key("FtbS", f.Index)
	}),
	entry("FtbP", "fade-to-black", parseFtbP, func(s *State, f *FadeToBlackField) string {
		at(&s.MEs, int(f.Index)).FTB.Rate = f
		return key("FtbP", f.Index)
	}),
	entry("FEna", "fade-to-black-enabled", parseFEna, func(s *State, f *FadeToBlackEnabledField) string {
		s.FTBEnabled = f
		return "FEna"
	}),
}

func parsePrgI(b []byte) (*ProgramBusInputField, error) {
	if err := atLeast("PrgI", b, 4); err != nil {
		return nil, err
	}
	return &ProgramBusInputField{Index: b[0], Source: u16(b, 2)}, nil
}

func parsePrvI(b []byte) (*PreviewBusInputField, error) {
	if err := atLeast("PrvI", b, 5); err != nil {
		return nil, err
	}
	return &PreviewBusInputField{Index: b[0], Source: u16(b, 2), InProgram: flag(b, 4)}, nil
}

func parseAuxS(b []byte) (*AuxOutputSourceField, error) {
	if err := atLeast("AuxS", b, 4); err != nil {
		return nil, err
	}
	return &AuxOutputSourceField{Index: b[0], Source: u16(b, 2)}, nil
}

// parseTrSS: struct.unpack('>B 2B 2B 3x'), the next-transition bytes
// unpacked into bits 0 (background) to 4 (key 4).
func parseTrSS(b []byte) (*TransitionSettingsField, error) {
	if err := exactly("TrSS", b, 8); err != nil {
		return nil, err
	}
	nt, ntn := b[2], b[4]
	return &TransitionSettingsField{
		Index: b[0], Style: b[1], StyleNext: b[3],
		NextTransitionBkgd: nt&0x01 != 0, NextTransitionKey1: nt&0x02 != 0,
		NextTransitionKey2: nt&0x04 != 0, NextTransitionKey3: nt&0x08 != 0,
		NextTransitionKey4:     nt&0x10 != 0,
		NextTransitionBkgdNext: ntn&0x01 != 0, NextTransitionKey1Next: ntn&0x02 != 0,
		NextTransitionKey2Next: ntn&0x04 != 0, NextTransitionKey3Next: ntn&0x08 != 0,
		NextTransitionKey4Next: ntn&0x10 != 0,
	}, nil
}

func parseTrPr(b []byte) (*TransitionPreviewField, error) {
	if err := atLeast("TrPr", b, 2); err != nil {
		return nil, err
	}
	return &TransitionPreviewField{Index: b[0], Enabled: flag(b, 1)}, nil
}

func parseTrPs(b []byte) (*TransitionPositionField, error) {
	if err := atLeast("TrPs", b, 6); err != nil {
		return nil, err
	}
	return &TransitionPositionField{Index: b[0], InTransition: flag(b, 1), FramesRemaining: b[2], Position: u16(b, 4)}, nil
}

func parseTMxP(b []byte) (*TransitionMixField, error) {
	if err := atLeast("TMxP", b, 2); err != nil {
		return nil, err
	}
	return &TransitionMixField{Index: b[0], Rate: b[1]}, nil
}

func parseTDpP(b []byte) (*TransitionDipField, error) {
	if err := atLeast("TDpP", b, 4); err != nil {
		return nil, err
	}
	return &TransitionDipField{Index: b[0], Rate: b[1], Source: u16(b, 2)}, nil
}

func parseTWpP(b []byte) (*TransitionWipeField, error) {
	if err := atLeast("TWpP", b, 18); err != nil {
		return nil, err
	}
	return &TransitionWipeField{
		Index: b[0], Rate: b[1], Pattern: b[2], Width: u16(b, 4), Source: u16(b, 6),
		Symmetry: u16(b, 8), Softness: u16(b, 10), PositionX: u16(b, 12), PositionY: u16(b, 14),
		Reverse: flag(b, 16), FlipFlop: flag(b, 17),
	}, nil
}

// parseTDvP: byte 1 is the logo-wipe rate and byte 2 the DVE rate, the
// layout atemwire corrected against ATEM Software Control.
func parseTDvP(b []byte) (*TransitionDVEField, error) {
	if err := atLeast("TDvP", b, 17); err != nil {
		return nil, err
	}
	return &TransitionDVEField{
		Index: b[0], LogoRate: b[1], Rate: b[2], Style: b[3],
		FillSource: u16(b, 4), KeySource: u16(b, 6), KeyEnable: flag(b, 8), KeyPremultiplied: flag(b, 9),
		KeyClip: u16(b, 10), KeyGain: u16(b, 12), KeyInvert: flag(b, 14), Reverse: flag(b, 15), FlipFlop: flag(b, 16),
	}, nil
}

func parseTStP(b []byte) (*TransitionStingerField, error) {
	if err := atLeast("TStP", b, 18); err != nil {
		return nil, err
	}
	return &TransitionStingerField{
		Index: b[0], MediaPlayer: b[1], KeyPremultiplied: flag(b, 2), KeyClip: u16(b, 4), KeyGain: u16(b, 6),
		KeyInvert: flag(b, 8), Preroll: u16(b, 10), Duration: u16(b, 12), TriggerPoint: u16(b, 14), Rate: u16(b, 16),
	}, nil
}

func parseFtbS(b []byte) (*FadeToBlackStateField, error) {
	if err := atLeast("FtbS", b, 4); err != nil {
		return nil, err
	}
	return &FadeToBlackStateField{Index: b[0], Done: flag(b, 1), Transitioning: flag(b, 2), FramesRemaining: b[3]}, nil
}

func parseFtbP(b []byte) (*FadeToBlackField, error) {
	if err := atLeast("FtbP", b, 2); err != nil {
		return nil, err
	}
	return &FadeToBlackField{Index: b[0], Rate: b[1]}, nil
}

// parseFEna is atemwire's byte heuristic: enabled when byte 0 is 0 and
// byte 1 is 1. Missing bytes read as 0.
func parseFEna(b []byte) (*FadeToBlackEnabledField, error) {
	var b0, b1 byte
	if len(b) > 0 {
		b0 = b[0]
	}
	if len(b) > 1 {
		b1 = b[1]
	}
	enabled := b0 == 0 && b1 == 1
	return &FadeToBlackEnabledField{Enabled: enabled, Disabled: !enabled}, nil
}
