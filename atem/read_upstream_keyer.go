// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The upstream keyer readers of bmdwire v1.3.0
// atemwire/messages/upstream_keyer.py, each for keyer keyer of M/E me. See
// read_state.go for the conventions.

import "strings"

// USKLumaView is usk_luma's dict: clip and gain in percent (wire ÷ 10).
type USKLumaView struct {
	LumaClip          float64 `json:"luma_clip"`
	LumaGain          float64 `json:"luma_gain"`
	LumaInvert        bool    `json:"luma_invert"`
	LumaPreMultiplied bool    `json:"luma_pre_multiplied"`
}

// USKChromaView is usk_chroma's dict. The five key fields and the six
// colour corrections are wire ÷ 1000 (three decimals); ChromaHue, ChromaGain,
// ChromaLift, ChromaNarrow and ChromaYSuppress are the legacy chroma
// keyer's, which atemwire has no packet for, so they always hold their
// defaults.
type USKChromaView struct {
	ChromaHue              float64                  `json:"chroma_hue"`
	ChromaGain             float64                  `json:"chroma_gain"`
	ChromaLift             float64                  `json:"chroma_lift"`
	ChromaNarrow           bool                     `json:"chroma_narrow"`
	ChromaYSuppress        float64                  `json:"chroma_y_suppress"`
	ChromaForeground       float64                  `json:"chroma_foreground"`
	ChromaBackground       float64                  `json:"chroma_background"`
	ChromaKeyEdge          float64                  `json:"chroma_key_edge"`
	ChromaSpill            float64                  `json:"chroma_spill"`
	ChromaFlareSuppression float64                  `json:"chroma_flare_suppression"`
	ChromaSample           bool                     `json:"chroma_sample"`
	ChromaPreview          bool                     `json:"chroma_preview"`
	ChromaSampleSize       float64                  `json:"chroma_sample_size"`
	ChromaSamplePosition   ChromaSamplePositionView `json:"chroma_sample_position"`
	ChromaSampledColor     ChromaSampledColorView   `json:"chroma_sampled_color"`
	ChromaBrightness       float64                  `json:"chroma_brightness"`
	ChromaContrast         float64                  `json:"chroma_contrast"`
	ChromaSaturation       float64                  `json:"chroma_saturation"`
	ChromaRed              float64                  `json:"chroma_red"`
	ChromaGreen            float64                  `json:"chroma_green"`
	ChromaBlue             float64                  `json:"chroma_blue"`
}

// ChromaSamplePositionView is usk_chroma's chroma_sample_position: the
// sample cursor as 0-1 of the frame, clamped.
type ChromaSamplePositionView struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ChromaSampledColorView is usk_chroma's chroma_sampled_color: the sampled
// Y, Cb and Cr (four decimals) and the wire values atemwire computes back
// from them (int(Y × 8544 + 625), int(Cb × 5000 + 5000)), which can be one
// below the wire value the switcher sent.
type ChromaSampledColorView struct {
	YRaw  int     `json:"y_raw"`
	CbRaw int     `json:"cb_raw"`
	CrRaw int     `json:"cr_raw"`
	Y     float64 `json:"y"`
	Cb    float64 `json:"cb"`
	Cr    float64 `json:"cr"`
}

// USKDVEView is usk_dve's dict. Positions, sizes and mask edges are wire
// ÷ 1000 (three decimals), rotation and light direction ÷ 10, border widths
// ÷ 100; hue is degrees, saturation and luma percent. Softness, bevel,
// opacity, altitude and rate are the wire values as floats.
type USKDVEView struct {
	DVEPositionX           float64 `json:"dve_position_x"`
	DVEPositionY           float64 `json:"dve_position_y"`
	DVESizeX               float64 `json:"dve_size_x"`
	DVESizeY               float64 `json:"dve_size_y"`
	DVERotation            float64 `json:"dve_rotation"`
	DVEMasked              bool    `json:"dve_masked"`
	DVETop                 float64 `json:"dve_top"`
	DVEBottom              float64 `json:"dve_bottom"`
	DVELeft                float64 `json:"dve_left"`
	DVERight               float64 `json:"dve_right"`
	DVEBorderEnabled       bool    `json:"dve_border_enabled"`
	DVEBorderHue           float64 `json:"dve_border_hue"`
	DVEBorderSaturation    float64 `json:"dve_border_saturation"`
	DVEBorderLuma          float64 `json:"dve_border_luma"`
	DVEBorderOpacity       float64 `json:"dve_border_opacity"`
	DVEBorderOuterWidth    float64 `json:"dve_border_outer_width"`
	DVEBorderInnerWidth    float64 `json:"dve_border_inner_width"`
	DVEBorderOuterSoftness float64 `json:"dve_border_outer_softness"`
	DVEBorderInnerSoftness float64 `json:"dve_border_inner_softness"`
	DVEBorderBevelType     int     `json:"dve_border_bevel_type"`
	DVEBorderBevelPosition float64 `json:"dve_border_bevel_position"`
	DVEBorderBevelSoftness float64 `json:"dve_border_bevel_softness"`
	DVELightDirection      float64 `json:"dve_light_direction"`
	DVELightAltitude       float64 `json:"dve_light_altitude"`
	DVEShadow              bool    `json:"dve_shadow"`
	DVERate                float64 `json:"dve_rate"`
}

// USKPatternView is usk_pattern's dict: size, symmetry and softness in
// percent (wire ÷ 100), positions 0-1 (wire ÷ 10000).
type USKPatternView struct {
	PatternStyle     int     `json:"pattern_style"`
	PatternSize      float64 `json:"pattern_size"`
	PatternSymmetry  float64 `json:"pattern_symmetry"`
	PatternSoftness  float64 `json:"pattern_softness"`
	PatternInvert    bool    `json:"pattern_invert"`
	PatternPositionX float64 `json:"pattern_position_x"`
	PatternPositionY float64 `json:"pattern_position_y"`
}

// USKMaskView is usk_mask's dict: the mask edges wire ÷ 1000.
type USKMaskView struct {
	MaskEnabled bool    `json:"mask_enabled"`
	MaskTop     float64 `json:"mask_top"`
	MaskBottom  float64 `json:"mask_bottom"`
	MaskLeft    float64 `json:"mask_left"`
	MaskRight   float64 `json:"mask_right"`
}

// USKFlyView is usk_fly's dict.
type USKFlyView struct {
	FlyEnabled              bool `json:"fly_enabled"`
	FlyKeyframeAStored      bool `json:"fly_keyframe_a_stored"`
	FlyKeyframeBStored      bool `json:"fly_keyframe_b_stored"`
	FlyIsAtKeyframeA        bool `json:"fly_is_at_keyframe_a"`
	FlyIsAtKeyframeB        bool `json:"fly_is_at_keyframe_b"`
	FlyIsAtKeyframeFull     bool `json:"fly_is_at_keyframe_full"`
	FlyIsAtKeyframeInfinite bool `json:"fly_is_at_keyframe_infinite"`
}

// USKFlyKeyframeView is usk_fly_keyframe's dict, in usk_dve's units except
// that softness, bevel, opacity and altitude are ints.
type USKFlyKeyframeView struct {
	SizeX               float64 `json:"size_x"`
	SizeY               float64 `json:"size_y"`
	PosX                float64 `json:"pos_x"`
	PosY                float64 `json:"pos_y"`
	Rotation            float64 `json:"rotation"`
	BorderOuterWidth    float64 `json:"border_outer_width"`
	BorderInnerWidth    float64 `json:"border_inner_width"`
	BorderOuterSoftness int     `json:"border_outer_softness"`
	BorderInnerSoftness int     `json:"border_inner_softness"`
	BorderBevelSoftness int     `json:"border_bevel_softness"`
	BorderBevelPosition int     `json:"border_bevel_position"`
	BorderOpacity       int     `json:"border_opacity"`
	BorderHue           float64 `json:"border_hue"`
	BorderSaturation    float64 `json:"border_saturation"`
	BorderLuma          float64 `json:"border_luma"`
	LightDirection      float64 `json:"light_direction"`
	LightAltitude       int     `json:"light_altitude"`
	MaskTop             float64 `json:"mask_top"`
	MaskBottom          float64 `json:"mask_bottom"`
	MaskLeft            float64 `json:"mask_left"`
	MaskRight           float64 `json:"mask_right"`
}

// uskBase is the keyer's key-properties-base entry, or nil.
func (s State) uskBase(me, keyer int) *KeyPropertiesBaseField {
	if u := s.uskAt(me, keyer); u != nil {
		return u.Base
	}
	return nil
}

// thousandths is wire ÷ 1000 to three decimals, the usk_dve, usk_mask and
// fly keyframe position, size and mask scale.
func thousandths(wire int64) float64 { return pyRound(float64(wire)/1000.0, 3) }

// USKOnAir is usk_on_air (messages/upstream_keyer.py), bmdwire v1.3.0;
// false before KeOn.
func (s State) USKOnAir(me, keyer int) bool {
	if u := s.uskAt(me, keyer); u != nil && u.OnAir != nil {
		return u.OnAir.Enabled
	}
	return false
}

// USKType is usk_type (messages/upstream_keyer.py), bmdwire v1.3.0: 0 luma,
// 1 chroma, 2 pattern, 3 DVE; 0 before KeBP.
func (s State) USKType(me, keyer int) int {
	if b := s.uskBase(me, keyer); b != nil {
		return int(b.Type)
	}
	return 0
}

// USKFillSource is usk_fill_source (messages/upstream_keyer.py), bmdwire
// v1.3.0; 0 before KeBP.
func (s State) USKFillSource(me, keyer int) int {
	if b := s.uskBase(me, keyer); b != nil {
		return int(b.FillSource)
	}
	return 0
}

// USKKeySource is usk_key_source (messages/upstream_keyer.py), bmdwire
// v1.3.0; 0 before KeBP.
func (s State) USKKeySource(me, keyer int) int {
	if b := s.uskBase(me, keyer); b != nil {
		return int(b.KeySource)
	}
	return 0
}

// USKLuma is usk_luma (messages/upstream_keyer.py), bmdwire v1.3.0; zeros
// before KeLm.
func (s State) USKLuma(me, keyer int) USKLumaView {
	u := s.uskAt(me, keyer)
	if u == nil || u.Luma == nil {
		return USKLumaView{}
	}
	l := u.Luma
	return USKLumaView{
		LumaClip:          valueFromTenths(float64(l.Clip)),
		LumaGain:          valueFromTenths(float64(l.Gain)),
		LumaInvert:        l.KeyInverted,
		LumaPreMultiplied: l.Premultiplied,
	}
}

// USKChroma is usk_chroma (messages/upstream_keyer.py), bmdwire v1.3.0:
// the advanced chroma key (KACk) and its colour picker (KACC). Each half
// keeps its defaults until its packet arrives: saturation 1.0, sample size
// 0.1, the cursor centred, the sampled colour's wire values 0, 5000, 5000.
func (s State) USKChroma(me, keyer int) USKChromaView {
	v := USKChromaView{
		ChromaSampleSize:     0.1,
		ChromaSamplePosition: ChromaSamplePositionView{X: 0.5, Y: 0.5},
		ChromaSampledColor:   ChromaSampledColorView{CbRaw: 5000, CrRaw: 5000},
		ChromaSaturation:     1.0,
	}
	u := s.uskAt(me, keyer)
	if u == nil {
		return v
	}
	if a := u.AdvancedChroma; a != nil {
		v.ChromaForeground = valueFromThousandths(float64(a.Foreground), 3)
		v.ChromaBackground = valueFromThousandths(float64(a.Background), 3)
		v.ChromaKeyEdge = valueFromThousandths(float64(a.KeyEdge), 3)
		v.ChromaSpill = valueFromThousandths(float64(a.SpillSuppress), 3)
		v.ChromaFlareSuppression = valueFromThousandths(float64(a.FlareSuppress), 3)
		v.ChromaBrightness = valueFromThousandths(float64(a.Brightness), 3)
		v.ChromaContrast = valueFromThousandths(float64(a.Contrast), 3)
		v.ChromaSaturation = valueFromThousandths(float64(a.Saturation), 3)
		v.ChromaRed = valueFromThousandths(float64(a.Red), 3)
		v.ChromaGreen = valueFromThousandths(float64(a.Green), 3)
		v.ChromaBlue = valueFromThousandths(float64(a.Blue), 3)
	}
	if c := u.ChromaPicker; c != nil {
		v.ChromaSample = c.Cursor
		v.ChromaPreview = c.Preview
		v.ChromaSampleSize = pyRound(float64(c.Size)/10000.0, 4)
		// round(max(0.0, min(1.0, (wire + half) / range)), 4); no NaN can
		// reach Python's min and max here, so their argument order doesn't
		// matter.
		v.ChromaSamplePosition = ChromaSamplePositionView{
			X: pyRound(max(0.0, min(1.0, float64(int(c.X)+16000)/32000.0)), 4),
			Y: pyRound(max(0.0, min(1.0, float64(int(c.Y)+9000)/18000.0)), 4),
		}
		// int(Y * 8544 + 625): the explicit float64 conversions keep the
		// compiler from fusing the multiply and add, which Python doesn't.
		v.ChromaSampledColor = ChromaSampledColorView{
			YRaw:  int(float64(c.SampleY*8544) + 625),
			CbRaw: int(float64(c.SampleCb*5000) + 5000),
			CrRaw: int(float64(c.SampleCr*5000) + 5000),
			Y:     pyRound(c.SampleY, 4),
			Cb:    pyRound(c.SampleCb, 4),
			Cr:    pyRound(c.SampleCr, 4),
		}
	}
	return v
}

// USKDVE is usk_dve (messages/upstream_keyer.py), bmdwire v1.3.0. Before
// KeDV: sizes 1.0, opacity 100.0, rate 25.0, everything else zero.
func (s State) USKDVE(me, keyer int) USKDVEView {
	u := s.uskAt(me, keyer)
	if u == nil || u.DVE == nil {
		return USKDVEView{
			DVESizeX: 1.0, DVESizeY: 1.0, DVEBorderOpacity: 100.0, DVERate: 25.0,
		}
	}
	d := u.DVE
	return USKDVEView{
		DVEPositionX:           thousandths(int64(d.PosX)),
		DVEPositionY:           thousandths(int64(d.PosY)),
		DVESizeX:               thousandths(int64(d.SizeX)),
		DVESizeY:               thousandths(int64(d.SizeY)),
		DVERotation:            pyRound(float64(d.Rotation)/10.0, 2),
		DVEMasked:              d.MaskEnabled,
		DVETop:                 thousandths(int64(d.MaskTop)),
		DVEBottom:              thousandths(int64(d.MaskBottom)),
		DVELeft:                thousandths(int64(d.MaskLeft)),
		DVERight:               thousandths(int64(d.MaskRight)),
		DVEBorderEnabled:       d.BorderEnabled,
		DVEBorderHue:           pyRound(d.BorderHue, 2),
		DVEBorderSaturation:    percentFromUnit(d.BorderSaturation),
		DVEBorderLuma:          percentFromUnit(d.BorderLuma),
		DVEBorderOpacity:       float64(d.BorderOpacity),
		DVEBorderOuterWidth:    percentFromThousandths(float64(d.BorderOuterWidth)),
		DVEBorderInnerWidth:    percentFromThousandths(float64(d.BorderInnerWidth)),
		DVEBorderOuterSoftness: float64(d.BorderOuterSoftness),
		DVEBorderInnerSoftness: float64(d.BorderInnerSoftness),
		DVEBorderBevelType:     int(d.BorderBevel),
		DVEBorderBevelPosition: float64(d.BorderBevelPosition),
		DVEBorderBevelSoftness: float64(d.BorderBevelSoftness),
		DVELightDirection:      float64(d.LightAngle) / 10.0,
		DVELightAltitude:       float64(d.LightAltitude),
		DVEShadow:              d.ShadowEnabled,
		DVERate:                float64(d.Rate),
	}
}

// USKPattern is usk_pattern (messages/upstream_keyer.py), bmdwire v1.3.0.
// Before KePt: size and symmetry 50.0, positions 0.5.
func (s State) USKPattern(me, keyer int) USKPatternView {
	u := s.uskAt(me, keyer)
	if u == nil || u.Pattern == nil {
		return USKPatternView{
			PatternSize: 50.0, PatternSymmetry: 50.0, PatternPositionX: 0.5, PatternPositionY: 0.5,
		}
	}
	p := u.Pattern
	return USKPatternView{
		PatternStyle:     int(p.Pattern),
		PatternSize:      percentFromThousandths(float64(p.Size)),
		PatternSymmetry:  percentFromThousandths(float64(p.Symmetry)),
		PatternSoftness:  percentFromThousandths(float64(p.Softness)),
		PatternInvert:    p.Invert,
		PatternPositionX: unitFromThousandths(float64(p.PositionX)),
		PatternPositionY: unitFromThousandths(float64(p.PositionY)),
	}
}

// USKMask is usk_mask (messages/upstream_keyer.py), bmdwire v1.3.0: the
// mask on key-properties-base; zeros before KeBP.
func (s State) USKMask(me, keyer int) USKMaskView {
	b := s.uskBase(me, keyer)
	if b == nil {
		return USKMaskView{}
	}
	return USKMaskView{
		MaskEnabled: b.MaskEnabled,
		MaskTop:     thousandths(int64(b.MaskTop)),
		MaskBottom:  thousandths(int64(b.MaskBottom)),
		MaskLeft:    thousandths(int64(b.MaskLeft)),
		MaskRight:   thousandths(int64(b.MaskRight)),
	}
}

// USKFly is usk_fly (messages/upstream_keyer.py), bmdwire v1.3.0: the fly
// enable from key-properties-base and the keyframe state from
// key-properties-fly, each false before its packet.
func (s State) USKFly(me, keyer int) USKFlyView {
	var v USKFlyView
	if b := s.uskBase(me, keyer); b != nil {
		v.FlyEnabled = b.FlyEnabled
	}
	if u := s.uskAt(me, keyer); u != nil && u.Fly != nil {
		f := u.Fly
		v.FlyKeyframeAStored = f.IsASet
		v.FlyKeyframeBStored = f.IsBSet
		v.FlyIsAtKeyframeA = f.AtKeyframeA
		v.FlyIsAtKeyframeB = f.AtKeyframeB
		v.FlyIsAtKeyframeFull = f.AtKeyframeFull
		v.FlyIsAtKeyframeInfinite = f.AtKeyframeInfinite
	}
	return v
}

// USKFlyKeyframe is usk_fly_keyframe (messages/upstream_keyer.py), bmdwire
// v1.3.0: the geometry of fly keyframe which ("A", or anything else for B;
// atemwire compares str(which).upper() with "A"), or nil (atemwire's None)
// when that keyframe isn't stored.
func (s State) USKFlyKeyframe(me, keyer int, which string) *USKFlyKeyframeView {
	id := uint8(2)
	if strings.ToUpper(which) == "A" {
		id = 1
	}
	u := s.uskAt(me, keyer)
	if u == nil || u.Keyframes[id] == nil {
		return nil
	}
	k := u.Keyframes[id]
	return &USKFlyKeyframeView{
		SizeX:               thousandths(int64(k.SizeX)),
		SizeY:               thousandths(int64(k.SizeY)),
		PosX:                thousandths(int64(k.PosX)),
		PosY:                thousandths(int64(k.PosY)),
		Rotation:            pyRound(float64(k.Rotation)/10.0, 2),
		BorderOuterWidth:    percentFromThousandths(float64(k.BorderOuterWidth)),
		BorderInnerWidth:    percentFromThousandths(float64(k.BorderInnerWidth)),
		BorderOuterSoftness: int(k.BorderOuterSoftness),
		BorderInnerSoftness: int(k.BorderInnerSoftness),
		BorderBevelSoftness: int(k.BorderBevelSoftness),
		BorderBevelPosition: int(k.BorderBevelPosition),
		BorderOpacity:       int(k.BorderOpacity),
		BorderHue:           pyRound(k.BorderHue, 2),
		BorderSaturation:    percentFromUnit(k.BorderSaturation),
		BorderLuma:          percentFromUnit(k.BorderLuma),
		LightDirection:      pyRound(float64(k.LightAngle)/10.0, 2),
		LightAltitude:       int(k.LightAltitude),
		MaskTop:             thousandths(int64(k.MaskTop)),
		MaskBottom:          thousandths(int64(k.MaskBottom)),
		MaskLeft:            thousandths(int64(k.MaskLeft)),
		MaskRight:           thousandths(int64(k.MaskRight)),
	}
}
