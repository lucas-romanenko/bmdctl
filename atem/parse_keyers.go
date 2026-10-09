// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Parsers for SURFACE.md §2.3 (keyers, colour, inputs, media, macros), from
// bmdwire v1.3.0 atemwire/messages/upstream_keyer.py, downstream_keyer.py,
// color_generator.py, input_video.py, media.py, system_info.py (MPCE) and
// macros.py.

import "fmt"

var keyerParsers = []parser{
	entry("KeOn", "key-on-air", parseKeOn, func(s *State, f *KeyOnAirField) string {
		usk(s, f.Index, f.Keyer).OnAir = f
		return key("KeOn", f.Index, f.Keyer)
	}),
	entry("KeBP", "key-properties-base", parseKeBP, func(s *State, f *KeyPropertiesBaseField) string {
		usk(s, f.Index, f.Keyer).Base = f
		return key("KeBP", f.Index, f.Keyer)
	}),
	entry("KeLm", "key-properties-luma", parseKeLm, func(s *State, f *KeyPropertiesLumaField) string {
		usk(s, f.Index, f.Keyer).Luma = f
		return key("KeLm", f.Index, f.Keyer)
	}),
	entry("KACk", "key-properties-advanced-chroma", parseKACk, func(s *State, f *KeyPropertiesAdvancedChromaField) string {
		usk(s, f.Index, f.Keyer).AdvancedChroma = f
		return key("KACk", f.Index, f.Keyer)
	}),
	entry("KACC", "key-properties-advanced-chroma-colorpicker", parseKACC, func(s *State, f *KeyPropertiesAdvancedChromaColorpickerField) string {
		usk(s, f.Index, f.Keyer).ChromaPicker = f
		return key("KACC", f.Index, f.Keyer)
	}),
	entry("KePt", "key-properties-pattern", parseKePt, func(s *State, f *KeyPropertiesPatternField) string {
		usk(s, f.Index, f.Keyer).Pattern = f
		return key("KePt", f.Index, f.Keyer)
	}),
	entry("KeDV", "key-properties-dve", parseKeDV, func(s *State, f *KeyPropertiesDVEField) string {
		usk(s, f.Index, f.Keyer).DVE = f
		return key("KeDV", f.Index, f.Keyer)
	}),
	entry("KeFS", "key-properties-fly", parseKeFS, func(s *State, f *KeyPropertiesFlyField) string {
		usk(s, f.Index, f.Keyer).Fly = f
		return key("KeFS", f.Index, f.Keyer)
	}),
	entry("KKFP", "key-properties-fly-keyframe", parseKKFP, func(s *State, f *KeyPropertiesFlyKeyframeField) string {
		u := usk(s, f.Index, f.Keyer)
		if u.Keyframes == nil {
			u.Keyframes = make(map[uint8]*KeyPropertiesFlyKeyframeField)
		}
		u.Keyframes[f.Keyframe] = f
		return key("KKFP", f.Index, f.Keyer, f.Keyframe)
	}),
	entry("DskB", "dkey-properties-base", parseDskB, func(s *State, f *DkeyPropertiesBaseField) string {
		at(&s.DSKs, int(f.Index)).Base = f
		return key("DskB", f.Index)
	}),
	entry("DskP", "dkey-properties", parseDskP, func(s *State, f *DkeyPropertiesField) string {
		at(&s.DSKs, int(f.Index)).Properties = f
		return key("DskP", f.Index)
	}),
	entry("DskS", "dkey-state", parseDskS, func(s *State, f *DkeyStateField) string {
		at(&s.DSKs, int(f.Index)).State = f
		return key("DskS", f.Index)
	}),
	entry("ColV", "color-generator", parseColV, func(s *State, f *ColorGeneratorField) string {
		*at(&s.ColorGenerators, int(f.Index)) = f
		return key("ColV", f.Index)
	}),
	entry("InPr", "input-properties", parseInPr, func(s *State, f *InputPropertiesField) string {
		s.Inputs[f.Index] = f
		return key("InPr", f.Index)
	}),
	entry("MPfe", "mediaplayer-file-info", parseMPfe, func(s *State, f *MediaplayerFileInfoField) string {
		s.MediaPool.Files[f.Index] = f
		return key("MPfe", f.Index)
	}),
	entry("MPCE", "mediaplayer-selected", parseMPCE, func(s *State, f *MediaplayerSelectedField) string {
		*at(&s.MediaPlayers, int(f.Index)) = f
		return key("MPCE", f.Index)
	}),
	entry("MPrp", "macro-properties", parseMPrp, func(s *State, f *MacroPropertiesField) string {
		s.Macros.Properties[f.Index] = f
		return key("MPrp", f.Index)
	}),
	entry("MRcS", "macro-record-status", parseMRcS, func(s *State, f *MacroRecordStatusField) string {
		s.Macros.RecordStatus = f
		return "MRcS"
	}),
	entry("MRPr", "macro-play-status", parseMRPr, func(s *State, f *MacroPlayStatusField) string {
		s.Macros.PlayStatus = f
		// ATEMConnection's last-run latch (bmdwire v1.3.0, a9f3646): any
		// running slot but the idle 0xFFFF, slot 0 included.
		if f.Running && f.Index != 0xFFFF {
			s.Macros.LastRun = int(f.Index)
		}
		return "MRPr"
	}),
}

// usk returns keyer k of M/E me, growing the tree to reach it.
func usk(s *State, me, k uint8) *USK {
	return at(&at(&s.MEs, int(me)).USKs, int(k))
}

func parseKeOn(b []byte) (*KeyOnAirField, error) {
	if err := atLeast("KeOn", b, 3); err != nil {
		return nil, err
	}
	return &KeyOnAirField{Index: b[0], Keyer: b[1], Enabled: flag(b, 2)}, nil
}

func parseKeBP(b []byte) (*KeyPropertiesBaseField, error) {
	if err := atLeast("KeBP", b, 20); err != nil {
		return nil, err
	}
	return &KeyPropertiesBaseField{
		Index: b[0], Keyer: b[1], Type: b[2], Enabled: flag(b, 3), FlyEnabled: flag(b, 5),
		FillSource: u16(b, 6), KeySource: u16(b, 8), MaskEnabled: flag(b, 10),
		MaskTop: i16(b, 12), MaskBottom: i16(b, 14), MaskLeft: i16(b, 16), MaskRight: i16(b, 18),
	}, nil
}

func parseKeLm(b []byte) (*KeyPropertiesLumaField, error) {
	if err := atLeast("KeLm", b, 9); err != nil {
		return nil, err
	}
	return &KeyPropertiesLumaField{
		Index: b[0], Keyer: b[1], Premultiplied: flag(b, 2), Clip: u16(b, 4), Gain: u16(b, 6), KeyInverted: flag(b, 8),
	}, nil
}

func parseKACk(b []byte) (*KeyPropertiesAdvancedChromaField, error) {
	if err := atLeast("KACk", b, 24); err != nil {
		return nil, err
	}
	return &KeyPropertiesAdvancedChromaField{
		Index: b[0], Keyer: b[1], Foreground: u16(b, 2), Background: u16(b, 4), KeyEdge: u16(b, 6),
		SpillSuppress: u16(b, 8), FlareSuppress: u16(b, 10), Brightness: i16(b, 12), Contrast: i16(b, 14),
		Saturation: u16(b, 16), Red: i16(b, 18), Green: i16(b, 20), Blue: i16(b, 22),
	}, nil
}

// parseKACC: struct.unpack('>BB?? hhH HHH'); the sampled colour is
// Y = (wire − 625) / 8544 and Cb, Cr = (wire − 5000) / 5000.
func parseKACC(b []byte) (*KeyPropertiesAdvancedChromaColorpickerField, error) {
	if err := exactly("KACC", b, 16); err != nil {
		return nil, err
	}
	return &KeyPropertiesAdvancedChromaColorpickerField{
		Index: b[0], Keyer: b[1], Cursor: flag(b, 2), Preview: flag(b, 3),
		X: i16(b, 4), Y: i16(b, 6), Size: u16(b, 8),
		SampleY:  float64(int(u16(b, 10))-625) / 8544,
		SampleCb: float64(int(u16(b, 12))-5000) / 5000,
		SampleCr: float64(int(u16(b, 14))-5000) / 5000,
	}, nil
}

// parseKePt: invert is bit 0 of byte 14.
func parseKePt(b []byte) (*KeyPropertiesPatternField, error) {
	if err := atLeast("KePt", b, 15); err != nil {
		return nil, err
	}
	return &KeyPropertiesPatternField{
		Index: b[0], Keyer: b[1], Pattern: b[2], Size: u16(b, 4), Symmetry: u16(b, 6), Softness: u16(b, 8),
		PositionX: u16(b, 10), PositionY: u16(b, 12), Invert: b[14]&0x01 != 0,
	}, nil
}

// parseKeDV: struct.unpack('>BBxx 5i ??Bx HH BBBBBx 4HB? 4hB 3x'), 60 bytes.
func parseKeDV(b []byte) (*KeyPropertiesDVEField, error) {
	if err := exactly("KeDV", b, 60); err != nil {
		return nil, err
	}
	return &KeyPropertiesDVEField{
		Index: b[0], Keyer: b[1],
		SizeX: i32(b, 4), SizeY: i32(b, 8), PosX: i32(b, 12), PosY: i32(b, 16), Rotation: i32(b, 20),
		BorderEnabled: flag(b, 24), ShadowEnabled: flag(b, 25), BorderBevel: b[26],
		BorderOuterWidth: u16(b, 28), BorderInnerWidth: u16(b, 30),
		BorderOuterSoftness: b[32], BorderInnerSoftness: b[33], BorderBevelSoftness: b[34],
		BorderBevelPosition: b[35], BorderOpacity: b[36],
		BorderHue:        float64(u16(b, 38)) / 10.0,
		BorderSaturation: float64(u16(b, 40)) / 1000.0,
		BorderLuma:       float64(u16(b, 42)) / 1000.0,
		LightAngle:       u16(b, 44), LightAltitude: b[46], MaskEnabled: flag(b, 47),
		MaskTop: i16(b, 48), MaskBottom: i16(b, 50), MaskLeft: i16(b, 52), MaskRight: i16(b, 54),
		Rate: b[56],
	}, nil
}

// parseKeFS reads what is there: a short payload leaves the rest zero.
// atemwire's parser accepts any length, but save_field_data then reads the
// index with KEY_FORMAT '>BB', which raises under two bytes.
func parseKeFS(b []byte) (*KeyPropertiesFlyField, error) {
	if err := atLeast("KeFS", b, 2); err != nil {
		return nil, err
	}
	byteAt := func(i int) byte {
		if len(b) > i {
			return b[i]
		}
		return 0
	}
	f6 := byteAt(6)
	return &KeyPropertiesFlyField{
		Index: byteAt(0), Keyer: byteAt(1),
		IsASet: byteAt(2)&0x01 != 0, IsBSet: byteAt(3)&0x01 != 0,
		RunToInfiniteIndex: byteAt(5),
		AtKeyframeA:        f6&0x01 != 0, AtKeyframeB: f6&0x02 != 0,
		AtKeyframeFull: f6&0x04 != 0, AtKeyframeInfinite: f6&0x08 != 0,
	}, nil
}

// parseKKFP: struct.unpack('>BBBx IIii i HH BBBBB x HHHH B x hhhh'), 52
// bytes, scaled like KeDV.
func parseKKFP(b []byte) (*KeyPropertiesFlyKeyframeField, error) {
	if err := exactly("KKFP", b, 52); err != nil {
		return nil, err
	}
	return &KeyPropertiesFlyKeyframeField{
		Index: b[0], Keyer: b[1], Keyframe: b[2],
		SizeX: u32(b, 4), SizeY: u32(b, 8), PosX: i32(b, 12), PosY: i32(b, 16), Rotation: i32(b, 20),
		BorderOuterWidth: u16(b, 24), BorderInnerWidth: u16(b, 26),
		BorderOuterSoftness: b[28], BorderInnerSoftness: b[29], BorderBevelSoftness: b[30],
		BorderBevelPosition: b[31], BorderOpacity: b[32],
		BorderHue:        float64(u16(b, 34)) / 10.0,
		BorderSaturation: float64(u16(b, 36)) / 1000.0,
		BorderLuma:       float64(u16(b, 38)) / 1000.0,
		LightAngle:       u16(b, 40), LightAltitude: b[42],
		MaskTop: i16(b, 44), MaskBottom: i16(b, 46), MaskLeft: i16(b, 48), MaskRight: i16(b, 50),
	}, nil
}

func parseDskB(b []byte) (*DkeyPropertiesBaseField, error) {
	if err := atLeast("DskB", b, 6); err != nil {
		return nil, err
	}
	return &DkeyPropertiesBaseField{Index: b[0], FillSource: u16(b, 2), KeySource: u16(b, 4)}, nil
}

func parseDskP(b []byte) (*DkeyPropertiesField, error) {
	if err := atLeast("DskP", b, 18); err != nil {
		return nil, err
	}
	return &DkeyPropertiesField{
		Index: b[0], Tie: flag(b, 1), Rate: b[2], Premultiplied: flag(b, 3), Clip: u16(b, 4), Gain: u16(b, 6),
		InvertKey: flag(b, 8), Masked: flag(b, 9), Top: i16(b, 10), Bottom: i16(b, 12), Left: i16(b, 14), Right: i16(b, 16),
	}, nil
}

func parseDskS(b []byte) (*DkeyStateField, error) {
	if err := atLeast("DskS", b, 5); err != nil {
		return nil, err
	}
	return &DkeyStateField{
		Index: b[0], OnAir: flag(b, 1), IsTransitioning: flag(b, 2), IsAutoTransitioning: flag(b, 3), FramesRemaining: b[4],
	}, nil
}

// parseColV: the DSL's scale divides on receive (hue ÷ 10, saturation and
// luma ÷ 1000).
func parseColV(b []byte) (*ColorGeneratorField, error) {
	if err := atLeast("ColV", b, 8); err != nil {
		return nil, err
	}
	return &ColorGeneratorField{
		Index: b[0], Hue: float64(u16(b, 2)) / 10, Saturation: float64(u16(b, 4)) / 1000, Luma: float64(u16(b, 6)) / 1000,
	}, nil
}

// parseInPr: struct.unpack('>H 20s 4s ?x HH Bx BB'), 36 bytes. Byte 34
// holds the availability bits, byte 35 the per-M/E bits.
func parseInPr(b []byte) (*InputPropertiesField, error) {
	if err := exactly("InPr", b, 36); err != nil {
		return nil, err
	}
	av, me := b[34], b[35]
	return &InputPropertiesField{
		Index: u16(b, 0), Name: cstr(b[2:22]), ShortName: cstr(b[22:26]), DefaultName: flag(b, 26),
		SourcePorts: u16(b, 28), ExternalPortType: u16(b, 30), PortType: b[32],
		AvailableAux: av&0x01 != 0, AvailableMultiview: av&0x02 != 0,
		AvailableSupersourceArt: av&0x04 != 0, AvailableSupersourceBox: av&0x08 != 0,
		AvailableKeySource: av&0x10 != 0, AvailableAux1: av&0x20 != 0, AvailableAux2: av&0x40 != 0,
		AvailableUSB: av&0x80 != 0,
		AvailableME1: me&0x01 != 0, AvailableME2: me&0x02 != 0, AvailableME3: me&0x04 != 0, AvailableME4: me&0x08 != 0,
	}, nil
}

// parseMPfe: struct.unpack('>Bx H ? 16s 2x {n}p') with n = len − 23. The
// name is a Pascal string: a length byte, then at most n − 1 bytes. At
// exactly 23 bytes Python's struct raises (a zero-length "p"), so this
// errors too.
func parseMPfe(b []byte) (*MediaplayerFileInfoField, error) {
	if len(b) <= 23 {
		return nil, fmt.Errorf("atem: MPfe payload is %d bytes, needs more than 23", len(b))
	}
	f := &MediaplayerFileInfoField{Type: b[0], Index: u16(b, 2), IsUsed: flag(b, 4)}
	copy(f.Hash[:], b[5:21])
	n := min(int(b[23]), len(b)-24)
	f.Name = string(b[24 : 24+n])
	return f, nil
}

func parseMPCE(b []byte) (*MediaplayerSelectedField, error) {
	if err := atLeast("MPCE", b, 3); err != nil {
		return nil, err
	}
	return &MediaplayerSelectedField{Index: b[0], SourceType: b[1], Slot: b[2]}, nil
}

// parseMPrp: '>H ?? H H', then the name and description bytes.
func parseMPrp(b []byte) (*MacroPropertiesField, error) {
	if err := atLeast("MPrp", b, 8); err != nil {
		return nil, err
	}
	nl, dl := int(u16(b, 4)), int(u16(b, 6))
	if err := atLeast("MPrp", b, 8+nl+dl); err != nil {
		return nil, err
	}
	return &MacroPropertiesField{
		Index: u16(b, 0), IsUsed: flag(b, 2), IsInvalid: flag(b, 3),
		Name: string(b[8 : 8+nl]), Description: string(b[8+nl : 8+nl+dl]),
	}, nil
}

func parseMRcS(b []byte) (*MacroRecordStatusField, error) {
	if err := atLeast("MRcS", b, 4); err != nil {
		return nil, err
	}
	return &MacroRecordStatusField{IsRecording: flag(b, 0), Index: u16(b, 2)}, nil
}

// parseMRPr: running is bit 0 and waiting bit 1 of byte 0, looping bit 0
// of byte 1.
func parseMRPr(b []byte) (*MacroPlayStatusField, error) {
	if err := atLeast("MRPr", b, 4); err != nil {
		return nil, err
	}
	return &MacroPlayStatusField{
		Running: b[0]&0x01 != 0, Waiting: b[0]&0x02 != 0, IsLooping: b[1]&0x01 != 0, Index: u16(b, 2),
	}, nil
}
