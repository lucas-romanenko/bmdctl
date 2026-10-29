// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "math"

// SURFACE.md §1.4 keyer-usk, from bmdwire v1.3.0
// atemwire/messages/upstream_keyer.py. ME and Keyer are 0-based. Every
// optional field is in the unit of the atemwire operation that sets it; a
// nil field leaves that setting unchanged.

// Key types (KeyType.Type).
const (
	KeyLuma uint8 = iota
	KeyChroma
	KeyPattern
	KeyDVE
)

// Fly keyframes (KeyerKeyframeSet.Keyframe, KeyerKeyframeRun.RunTo):
// atemwire's KEYFRAME_BY_NAME.
const (
	KeyframeA             uint8 = 1
	KeyframeB             uint8 = 2
	KeyframeFull          uint8 = 3
	KeyframeRunToInfinite uint8 = 4
)

// FlyKeyLocationNames is helpers.FLYKEY_LOCATION_NAMES (bmdwire#22,
// 1bee7d4): where KeyframeRunToInfinite flies the key
// (KeyerKeyframeRun.Direction), by ATEM Software Control's name. 1-9 are
// the 3x3 grid row by row from the top left, so 5 is MiddleCentre; 0 is
// CentreOfKey, a tenth location. A capture of ASC settled it: its RFlK for
// each of the ten carried this byte at payload byte 5, KeFS echoed it, and
// the same runs recorded as a macro hold it as FlyKeyRunToInfinity's
// location. atem/macro keeps a copy.
var FlyKeyLocationNames = map[uint8]string{
	0: "CentreOfKey", 1: "TopLeft", 2: "TopCentre", 3: "TopRight", 4: "MiddleLeft",
	5: "MiddleCentre", 6: "MiddleRight", 7: "BottomLeft", 8: "BottomCentre", 9: "BottomRight",
}

// KeyOnAir is CKOn (KeyOnAirCommand, op set_usk_on_air).
type KeyOnAir struct {
	ME, Keyer uint8
	Enabled   bool
}

func (c KeyOnAir) Opcode() string    { return "CKOn" }
func (c KeyOnAir) Encode() []byte    { return c.build().encode() }
func (c KeyOnAir) Validate() error   { return c.build().err }
func (c KeyOnAir) EchoKey() string   { return key("KeOn", c.ME, c.Keyer) }
func (c KeyOnAir) Unverified() bool  { return false }
func (c KeyOnAir) Destructive() bool { return false }

func (c KeyOnAir) build() *wire {
	w := newWire("CKOn", 4)
	w.u8(0, "me", int64(c.ME))
	w.u8(1, "keyer", int64(c.Keyer))
	w.flag(2, c.Enabled)
	return w
}

// KeyFill is CKeF (KeyFillCommand, op set_usk_fill_source).
type KeyFill struct {
	ME, Keyer uint8
	Source    uint16
}

func (c KeyFill) Opcode() string    { return "CKeF" }
func (c KeyFill) Encode() []byte    { return c.build().encode() }
func (c KeyFill) Validate() error   { return c.build().err }
func (c KeyFill) EchoKey() string   { return key("KeBP", c.ME, c.Keyer) }
func (c KeyFill) Unverified() bool  { return false }
func (c KeyFill) Destructive() bool { return false }

func (c KeyFill) build() *wire {
	w := newWire("CKeF", 4)
	w.u8(0, "me", int64(c.ME))
	w.u8(1, "keyer", int64(c.Keyer))
	w.u16(2, "source", int64(c.Source))
	return w
}

// KeyCut is CKeC (KeyCutCommand, op set_usk_key_source): the key source.
type KeyCut struct {
	ME, Keyer uint8
	Source    uint16
}

func (c KeyCut) Opcode() string    { return "CKeC" }
func (c KeyCut) Encode() []byte    { return c.build().encode() }
func (c KeyCut) Validate() error   { return c.build().err }
func (c KeyCut) EchoKey() string   { return key("KeBP", c.ME, c.Keyer) }
func (c KeyCut) Unverified() bool  { return false }
func (c KeyCut) Destructive() bool { return false }

func (c KeyCut) build() *wire {
	w := newWire("CKeC", 4)
	w.u8(0, "me", int64(c.ME))
	w.u8(1, "keyer", int64(c.Keyer))
	w.u16(2, "source", int64(c.Source))
	return w
}

// KeyType is CKTp (KeyTypeCommand; ops set_usk_type, set_usk_fly_enabled).
type KeyType struct {
	ME, Keyer  uint8
	Type       *uint8 // KeyLuma ... KeyDVE
	FlyEnabled *bool
}

func (c KeyType) Opcode() string    { return "CKTp" }
func (c KeyType) Encode() []byte    { return c.build().encode() }
func (c KeyType) Validate() error   { return c.build().err }
func (c KeyType) EchoKey() string   { return key("KeBP", c.ME, c.Keyer) }
func (c KeyType) Unverified() bool  { return false }
func (c KeyType) Destructive() bool { return false }

func (c KeyType) build() *wire {
	w := newWire("CKTp", 8)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	if c.Type != nil {
		mask |= 1 << 0
		w.u8(3, "type", int64(*c.Type))
	}
	if c.FlyEnabled != nil {
		mask |= 1 << 1
		w.flag(4, *c.FlyEnabled)
	}
	w.u8(0, "mask", mask)
	return w
}

// KeyPropertiesLuma is CKLm (KeyPropertiesLumaCommand; ops
// set_usk_luma_*, configure_usk_luma). Clip and Gain are percent (×10 on
// the wire, clamped 0-1000).
type KeyPropertiesLuma struct {
	ME, Keyer     uint8
	Premultiplied *bool
	Clip          *float64
	Gain          *float64
	InvertKey     *bool
}

func (c KeyPropertiesLuma) Opcode() string    { return "CKLm" }
func (c KeyPropertiesLuma) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesLuma) Validate() error   { return c.build().err }
func (c KeyPropertiesLuma) EchoKey() string   { return key("KeLm", c.ME, c.Keyer) }
func (c KeyPropertiesLuma) Unverified() bool  { return false }
func (c KeyPropertiesLuma) Destructive() bool { return false }

func (c KeyPropertiesLuma) build() *wire {
	w := newWire("CKLm", 12)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	if c.Premultiplied != nil {
		mask |= 1 << 0
		w.flag(3, *c.Premultiplied)
	}
	if c.Clip != nil {
		mask |= 1 << 1
		w.u16(4, "clip", w.percentToTenths("clip", *c.Clip))
	}
	if c.Gain != nil {
		mask |= 1 << 2
		w.u16(6, "gain", w.percentToTenths("gain", *c.Gain))
	}
	if c.InvertKey != nil {
		mask |= 1 << 3
		w.flag(8, *c.InvertKey)
	}
	w.u8(0, "mask", mask)
	return w
}

// KeyPropertiesAdvancedChroma is CACK (KeyPropertiesAdvancedChromaCommand;
// ops set_usk_chroma_*). Every field is the display value ×1000 on the
// wire, unclamped: Foreground, Background, KeyEdge, Spill and Flare 0-1;
// Brightness, Contrast, Red, Green and Blue roughly -1 to 1; Saturation
// 0-2.
type KeyPropertiesAdvancedChroma struct {
	ME, Keyer  uint8
	Foreground *float64
	Background *float64
	KeyEdge    *float64
	Spill      *float64
	Flare      *float64
	Brightness *float64
	Contrast   *float64
	Saturation *float64
	Red        *float64
	Green      *float64
	Blue       *float64
}

func (c KeyPropertiesAdvancedChroma) Opcode() string    { return "CACK" }
func (c KeyPropertiesAdvancedChroma) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesAdvancedChroma) Validate() error   { return c.build().err }
func (c KeyPropertiesAdvancedChroma) EchoKey() string   { return key("KACk", c.ME, c.Keyer) }
func (c KeyPropertiesAdvancedChroma) Unverified() bool  { return false }
func (c KeyPropertiesAdvancedChroma) Destructive() bool { return false }

func (c KeyPropertiesAdvancedChroma) build() *wire {
	w := newWire("CACK", 28)
	var mask int64
	w.u8(2, "me", int64(c.ME))
	w.u8(3, "keyer", int64(c.Keyer))
	fields := []struct {
		v      *float64
		name   string
		at     int
		signed bool
	}{
		{c.Foreground, "foreground", 4, false}, {c.Background, "background", 6, false},
		{c.KeyEdge, "key_edge", 8, false}, {c.Spill, "spill", 10, false}, {c.Flare, "flare", 12, false},
		{c.Brightness, "brightness", 14, true}, {c.Contrast, "contrast", 16, true},
		{c.Saturation, "saturation", 18, false}, {c.Red, "red", 20, true}, {c.Green, "green", 22, true},
		{c.Blue, "blue", 24, true},
	}
	for bit, f := range fields {
		if f.v == nil {
			continue
		}
		mask |= 1 << bit
		v := w.round(f.name, *f.v*1000)
		if f.signed {
			w.i16(f.at, f.name, v)
		} else {
			w.u16(f.at, f.name, v)
		}
	}
	w.u16(0, "mask", mask)
	return w
}

// KeyPropertiesAdvancedChromaColorpicker is CACC
// (KeyPropertiesAdvancedChromaColorpickerCommand; ops set_usk_chroma_sample,
// set_usk_chroma_preview, set_usk_chroma_sample_position,
// set_usk_chroma_sample_size, set_usk_chroma_sampled_color). X and Y are
// the cursor position, 0-1 ((v − 0.5) × 32000 and × 18000 on the wire);
// Size is 0-1 (×10000, clamped 620-9925); SampleY, SampleCb and SampleCr
// are the sampled colour, 0-1 (×10000), for restoring a key: live video
// under an enabled cursor can overwrite them.
type KeyPropertiesAdvancedChromaColorpicker struct {
	ME, Keyer uint8
	Cursor    *bool
	Preview   *bool
	X         *float64
	Y         *float64
	Size      *float64
	SampleY   *float64
	SampleCb  *float64
	SampleCr  *float64
}

func (c KeyPropertiesAdvancedChromaColorpicker) Opcode() string    { return "CACC" }
func (c KeyPropertiesAdvancedChromaColorpicker) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesAdvancedChromaColorpicker) Validate() error   { return c.build().err }
func (c KeyPropertiesAdvancedChromaColorpicker) EchoKey() string   { return key("KACC", c.ME, c.Keyer) }
func (c KeyPropertiesAdvancedChromaColorpicker) Unverified() bool  { return false }
func (c KeyPropertiesAdvancedChromaColorpicker) Destructive() bool { return false }

func (c KeyPropertiesAdvancedChromaColorpicker) build() *wire {
	w := newWire("CACC", 20)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	if c.Cursor != nil {
		mask |= 1 << 0
		w.flag(3, *c.Cursor)
	}
	if c.Preview != nil {
		mask |= 1 << 1
		w.flag(4, *c.Preview)
	}
	if c.X != nil {
		mask |= 1 << 2
		w.i16(6, "x", w.round("x", (*c.X-0.5)*32000))
	}
	if c.Y != nil {
		mask |= 1 << 3
		w.i16(8, "y", w.round("y", (*c.Y-0.5)*18000))
	}
	if c.Size != nil {
		mask |= 1 << 4
		w.u16(10, "size", w.clampRound("size", *c.Size*10000, 620, 9925))
	}
	if c.SampleY != nil {
		mask |= 1 << 5
		w.u16(12, "Y", w.round("Y", *c.SampleY*10000))
	}
	if c.SampleCb != nil {
		mask |= 1 << 6
		w.i16(14, "Cb", w.round("Cb", *c.SampleCb*10000))
	}
	if c.SampleCr != nil {
		mask |= 1 << 7
		w.i16(16, "Cr", w.round("Cr", *c.SampleCr*10000))
	}
	w.u8(0, "mask", mask)
	return w
}

// KeyPropertiesPattern is CKPt (KeyPropertiesPatternCommand; ops
// set_usk_pattern_*). Size, Symmetry and Softness are percent (×100,
// clamped 0-10000); PositionX and PositionY are 0-1 (×10000, clamped).
type KeyPropertiesPattern struct {
	ME, Keyer     uint8
	Pattern       *uint8
	Size          *float64
	Symmetry      *float64
	Softness      *float64
	PositionX     *float64
	PositionY     *float64
	InvertPattern *bool
}

func (c KeyPropertiesPattern) Opcode() string    { return "CKPt" }
func (c KeyPropertiesPattern) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesPattern) Validate() error   { return c.build().err }
func (c KeyPropertiesPattern) EchoKey() string   { return key("KePt", c.ME, c.Keyer) }
func (c KeyPropertiesPattern) Unverified() bool  { return false }
func (c KeyPropertiesPattern) Destructive() bool { return false }

func (c KeyPropertiesPattern) build() *wire {
	w := newWire("CKPt", 16)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	if c.Pattern != nil {
		mask |= 1 << 0
		w.u8(3, "pattern", int64(*c.Pattern))
	}
	if c.Size != nil {
		mask |= 1 << 1
		w.u16(4, "size", w.percentToThousandths("size", *c.Size))
	}
	if c.Symmetry != nil {
		mask |= 1 << 2
		w.u16(6, "symmetry", w.percentToThousandths("symmetry", *c.Symmetry))
	}
	if c.Softness != nil {
		mask |= 1 << 3
		w.u16(8, "softness", w.percentToThousandths("softness", *c.Softness))
	}
	if c.PositionX != nil {
		mask |= 1 << 4
		w.u16(10, "position_x", w.unitToThousandths("position_x", *c.PositionX))
	}
	if c.PositionY != nil {
		mask |= 1 << 5
		w.u16(12, "position_y", w.unitToThousandths("position_y", *c.PositionY))
	}
	if c.InvertPattern != nil {
		mask |= 1 << 6
		w.flag(14, *c.InvertPattern)
	}
	w.u8(0, "mask", mask)
	return w
}

// KeyPropertiesMask is CKMs (KeyPropertiesMaskCommand; ops
// set_usk_mask_*). Edges are display units ×1000, unclamped (±9 top and
// bottom, ±16 left and right). atemwire's docstring says it is not
// Wireshark-validated: unverified.
type KeyPropertiesMask struct {
	ME, Keyer uint8
	Enabled   *bool
	Top       *float64
	Bottom    *float64
	Left      *float64
	Right     *float64
}

func (c KeyPropertiesMask) Opcode() string    { return "CKMs" }
func (c KeyPropertiesMask) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesMask) Validate() error   { return c.build().err }
func (c KeyPropertiesMask) EchoKey() string   { return key("KeBP", c.ME, c.Keyer) }
func (c KeyPropertiesMask) Unverified() bool  { return true }
func (c KeyPropertiesMask) Destructive() bool { return false }

func (c KeyPropertiesMask) build() *wire {
	w := newWire("CKMs", 12)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(3, *c.Enabled)
	}
	for i, e := range []struct {
		v    *float64
		name string
	}{{c.Top, "top"}, {c.Bottom, "bottom"}, {c.Left, "left"}, {c.Right, "right"}} {
		if e.v != nil {
			mask |= 1 << (i + 1)
			w.i16(4+2*i, e.name, w.round(e.name, *e.v*1000))
		}
	}
	w.u8(0, "mask", mask)
	return w
}

// KeyPropertiesDVE is CKDV (KeyPropertiesDveCommand; ops set_usk_dve_*),
// a 26-bit mask. Units, as the operations take them:
//
//   - SizeX, SizeY: 0-2 (×1000, clamped 0-2000)
//   - PosX, PosY: display units (×1000, clamped only to the i32 range so
//     off-screen fly positions survive)
//   - Rotation, Angle (light direction): degrees (×10)
//   - BorderHue, BorderSaturation, BorderLuma: ×10 on the wire, the write
//     side of atemwire's "SCALE CONTRACT" (the read side divides hue by 10
//     and saturation and luma by 1000)
//   - OuterWidth, InnerWidth: ×100
//   - the softnesses, BevelPosition, BorderOpacity, Altitude: rounded to
//     an integer
//   - MaskTop ... MaskRight: ×1000
//   - Rate: frames
//
// BorderBevelEnabled is the bevel type, written as is: no operation sets
// it.
type KeyPropertiesDVE struct {
	ME, Keyer          uint8
	SizeX              *float64
	SizeY              *float64
	PosX               *float64
	PosY               *float64
	Rotation           *float64
	BorderEnabled      *bool
	ShadowEnabled      *bool
	BorderBevelEnabled *uint8
	OuterWidth         *float64
	InnerWidth         *float64
	OuterSoftness      *float64
	InnerSoftness      *float64
	BevelSoftness      *float64
	BevelPosition      *float64
	BorderOpacity      *float64
	BorderHue          *float64
	BorderSaturation   *float64
	BorderLuma         *float64
	Angle              *float64
	Altitude           *float64
	MaskEnabled        *bool
	MaskTop            *float64
	MaskBottom         *float64
	MaskLeft           *float64
	MaskRight          *float64
	Rate               *uint8
}

func (c KeyPropertiesDVE) Opcode() string    { return "CKDV" }
func (c KeyPropertiesDVE) Encode() []byte    { return c.build().encode() }
func (c KeyPropertiesDVE) Validate() error   { return c.build().err }
func (c KeyPropertiesDVE) EchoKey() string   { return key("KeDV", c.ME, c.Keyer) }
func (c KeyPropertiesDVE) Unverified() bool  { return false }
func (c KeyPropertiesDVE) Destructive() bool { return false }

func (c KeyPropertiesDVE) build() *wire {
	w := newWire("CKDV", 64)
	var mask int64
	w.u8(4, "me", int64(c.ME))
	w.u8(5, "keyer", int64(c.Keyer))
	set := func(bit int) { mask |= 1 << bit }
	if c.SizeX != nil {
		set(0)
		w.i32(8, "size_x", w.sizeThousandths("size_x", *c.SizeX))
	}
	if c.SizeY != nil {
		set(1)
		w.i32(12, "size_y", w.sizeThousandths("size_y", *c.SizeY))
	}
	if c.PosX != nil {
		set(2)
		w.i32(16, "pos_x", w.clampRound("pos_x", *c.PosX*1000, math.MinInt32, math.MaxInt32))
	}
	if c.PosY != nil {
		set(3)
		w.i32(20, "pos_y", w.clampRound("pos_y", *c.PosY*1000, math.MinInt32, math.MaxInt32))
	}
	if c.Rotation != nil {
		set(4)
		w.i32(24, "rotation", w.round("rotation", *c.Rotation*10))
	}
	if c.BorderEnabled != nil {
		set(5)
		w.flag(28, *c.BorderEnabled)
	}
	if c.ShadowEnabled != nil {
		set(6)
		w.flag(29, *c.ShadowEnabled)
	}
	if c.BorderBevelEnabled != nil {
		set(7)
		w.u8(30, "border_bevel_enabled", int64(*c.BorderBevelEnabled))
	}
	if c.OuterWidth != nil {
		set(8)
		w.u16(32, "outer_width", w.round("outer_width", *c.OuterWidth*100))
	}
	if c.InnerWidth != nil {
		set(9)
		w.u16(34, "inner_width", w.round("inner_width", *c.InnerWidth*100))
	}
	for i, f := range []struct {
		v    *float64
		name string
	}{
		{c.OuterSoftness, "outer_softness"}, {c.InnerSoftness, "inner_softness"},
		{c.BevelSoftness, "bevel_softness"}, {c.BevelPosition, "bevel_position"},
		{c.BorderOpacity, "border_opacity"},
	} {
		if f.v != nil {
			set(10 + i)
			w.u8(36+i, f.name, w.round(f.name, *f.v))
		}
	}
	if c.BorderHue != nil {
		set(15)
		w.u16(42, "border_hue", w.round("border_hue", *c.BorderHue*10))
	}
	if c.BorderSaturation != nil {
		set(16)
		w.u16(44, "border_saturation", w.round("border_saturation", *c.BorderSaturation*10))
	}
	if c.BorderLuma != nil {
		set(17)
		w.u16(46, "border_luma", w.round("border_luma", *c.BorderLuma*10))
	}
	if c.Angle != nil {
		set(18)
		w.u16(48, "angle", w.round("angle", *c.Angle*10))
	}
	if c.Altitude != nil {
		set(19)
		w.u8(50, "altitude", w.round("altitude", *c.Altitude))
	}
	if c.MaskEnabled != nil {
		set(20)
		w.flag(51, *c.MaskEnabled)
	}
	for i, e := range []struct {
		v    *float64
		name string
	}{{c.MaskTop, "mask_top"}, {c.MaskBottom, "mask_bottom"}, {c.MaskLeft, "mask_left"}, {c.MaskRight, "mask_right"}} {
		if e.v != nil {
			set(21 + i)
			w.i16(52+2*i, e.name, w.round(e.name, *e.v*1000))
		}
	}
	if c.Rate != nil {
		set(25)
		w.u8(60, "rate", int64(*c.Rate))
	}
	w.u32(0, "mask", mask)
	return w
}

// KeyerKeyframeSet is SFKF (KeyerKeyframeSetCommand, op
// set_keyer_fly_keyframe): store the keyer's live DVE as a keyframe.
// atemwire's operation accepts KeyframeFull and KeyframeRunToInfinite too,
// though the wire format documents only A and B. The echo is the stored
// keyframe (KKFP) for A and B, the fly state (KeFS) otherwise.
type KeyerKeyframeSet struct {
	ME, Keyer uint8
	Keyframe  uint8 // KeyframeA ...
}

func (c KeyerKeyframeSet) Opcode() string  { return "SFKF" }
func (c KeyerKeyframeSet) Encode() []byte  { return c.build().encode() }
func (c KeyerKeyframeSet) Validate() error { return c.build().err }

func (c KeyerKeyframeSet) EchoKey() string {
	if c.Keyframe == KeyframeA || c.Keyframe == KeyframeB {
		return key("KKFP", c.ME, c.Keyer, c.Keyframe)
	}
	return key("KeFS", c.ME, c.Keyer)
}
func (c KeyerKeyframeSet) Unverified() bool  { return false }
func (c KeyerKeyframeSet) Destructive() bool { return false }

func (c KeyerKeyframeSet) build() *wire {
	w := newWire("SFKF", 4)
	w.u8(0, "me", int64(c.ME))
	w.u8(1, "keyer", int64(c.Keyer))
	w.u8(2, "keyframe", int64(c.Keyframe))
	return w
}

// KeyerKeyframeRun is RFlK (KeyerKeyframeRunCommand; ops
// run_flying_key_keyframe, run_flying_key_infinite_direction): fly the key
// to a keyframe. Direction (mask bit 1) only matters for
// KeyframeRunToInfinite: it is the location, a FlyKeyLocationNames byte,
// sent as given. ASC sends 0x6E in padding byte 3, atemwire 0. No echo of
// its own.
type KeyerKeyframeRun struct {
	ME, Keyer uint8
	RunTo     uint8 // KeyframeA ... KeyframeRunToInfinite
	Direction *uint8
}

func (c KeyerKeyframeRun) Opcode() string    { return "RFlK" }
func (c KeyerKeyframeRun) Encode() []byte    { return c.build().encode() }
func (c KeyerKeyframeRun) Validate() error   { return c.build().err }
func (c KeyerKeyframeRun) EchoKey() string   { return "" }
func (c KeyerKeyframeRun) Unverified() bool  { return false }
func (c KeyerKeyframeRun) Destructive() bool { return false }

func (c KeyerKeyframeRun) build() *wire {
	w := newWire("RFlK", 8)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "keyer", int64(c.Keyer))
	w.u8(4, "run_to", int64(c.RunTo))
	if c.Direction != nil {
		mask |= 1 << 1
		w.u8(5, "direction", int64(*c.Direction))
	}
	w.u8(0, "mask", mask)
	return w
}
