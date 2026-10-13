// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Upstream keyer ops (key type, on air, fill and cut, luma, mask, fly
// enable, DVE size, position, border and shadow, mask edges, DVE and fly
// key rate, advanced chroma key, fly keyframes): bmdwire v1.3.0
// atemwire/macrotransfer/upstream_keyer.py.

// meKeyer is the [u8 me, u8 keyer] head most decoders here start with.
func meKeyer(p params) []Param {
	return []Param{{mEBI, p.str(0)}, {"keyIndex", p.str(1)}}
}

func dKeyType(p params) []Param {
	head := meKeyer(p)
	t, ok := uskTypeNames[p.at(2)]
	if !ok {
		t = p.str(2)
	}
	return append(head, Param{"type", t})
}

func dKeyOnAir(p params) []Param {
	return append(meKeyer(p), Param{"onAir", boolStr(p.u16(2) != 0)})
}

func dKeyFillInput(p params) []Param {
	return append(meKeyer(p), Param{"input", sourceToSymbol(int(p.u16(2)))})
}

func dKeyCutInput(p params) []Param { return dKeyFillInput(p) }

func dKeyMaskEnable(p params) []Param {
	return append(meKeyer(p), Param{"enable", boolStr(p.at(2) != 0)})
}

func dKeyFlyEnable(p params) []Param { return dKeyMaskEnable(p) }

// dLumaKeyClip: [u8 me, u8 keyer, u16 marker, u32 Q16.16], unsigned.
func dLumaKeyClip(p params) []Param {
	return append(meKeyer(p), Param{"clip", q16(int64(p.u32(4)))})
}

func dLumaKeyGain(p params) []Param {
	return append(meKeyer(p), Param{"gain", q16(int64(p.u32(4)))})
}

func dLumaKeyInvert(p params) []Param {
	return append(meKeyer(p), Param{"invert", boolStr(p.at(2) != 0)})
}

func dLumaKeyPreMultiply(p params) []Param {
	return append(meKeyer(p), Param{"preMultiply", boolStr(p.at(2) != 0)})
}

// DVE sizes are unsigned, positions and mask edges signed; all Q16.16.

func dDVEXSize(p params) []Param {
	return append(meKeyer(p), Param{"xSize", q16(int64(p.u32(4)))})
}

func dDVEYSize(p params) []Param {
	return append(meKeyer(p), Param{"ySize", q16(int64(p.u32(4)))})
}

func dDVEXPos(p params) []Param {
	return append(meKeyer(p), Param{"xPosition", q16(int64(p.i32(4)))})
}

func dDVEYPos(p params) []Param {
	return append(meKeyer(p), Param{"yPosition", q16(int64(p.i32(4)))})
}

func dDVEMaskEnable(p params) []Param { return dKeyMaskEnable(p) }

func dDVEMaskTop(p params) []Param {
	return append(meKeyer(p), Param{"top", q16(int64(p.i32(4)))})
}

func dDVEMaskBottom(p params) []Param {
	return append(meKeyer(p), Param{"bottom", q16(int64(p.i32(4)))})
}

func dDVEMaskLeft(p params) []Param {
	return append(meKeyer(p), Param{"left", q16(int64(p.i32(4)))})
}

func dDVEMaskRight(p params) []Param {
	return append(meKeyer(p), Param{"right", q16(int64(p.i32(4)))})
}

// --- USK mask edges ---

func dUSKMaskTop(p params) []Param    { return dMEKeyerMarkerFP(p, "top") }
func eUSKMaskTop(a attrs) []byte      { return eMEKeyerMarkerFP(a, "top", 0x000C) }
func dUSKMaskBottom(p params) []Param { return dMEKeyerMarkerFP(p, "bottom") }
func eUSKMaskBottom(a attrs) []byte   { return eMEKeyerMarkerFP(a, "bottom", 0x000C) }
func dUSKMaskLeft(p params) []Param   { return dMEKeyerMarkerFP(p, "left") }
func eUSKMaskLeft(a attrs) []byte     { return eMEKeyerMarkerFP(a, "left", 0x000C) }
func dUSKMaskRight(p params) []Param  { return dMEKeyerMarkerFP(p, "right") }
func eUSKMaskRight(a attrs) []byte    { return eMEKeyerMarkerFP(a, "right", 0x000C) }

// --- DVE and fly key rate ---

func dDVEFlyKeyRate(p params) []Param { return dMEKeyerU16(p, "rate") }
func eDVEFlyKeyRate(a attrs) []byte   { return eMEKeyerU16(a, "rate") }

// --- USK DVE border ---

func dDVEBorderEnable(p params) []Param { return dMEKeyerBool(p, "enable") }
func eDVEBorderEnable(a attrs) []byte   { return eMEKeyerBool(a, "enable", 0) }

func dDVEBorderHue(p params) []Param { return dMEKeyerMarkerFP(p, "hue") }
func eDVEBorderHue(a attrs) []byte   { return eMEKeyerMarkerFP(a, "hue", 0x000C) }

func dDVEBorderSaturation(p params) []Param { return dMEKeyerMarkerFP(p, "saturation") }
func eDVEBorderSaturation(a attrs) []byte   { return eMEKeyerMarkerFP(a, "saturation", 0x000C) }

func dDVEBorderLuminescence(p params) []Param { return dMEKeyerMarkerFP(p, "luminescence") }
func eDVEBorderLuminescence(a attrs) []byte   { return eMEKeyerMarkerFP(a, "luminescence", 0x000C) }

func dDVEBorderInnerWidth(p params) []Param { return dMEKeyerMarkerFP(p, "innerWidth") }
func eDVEBorderInnerWidth(a attrs) []byte   { return eMEKeyerMarkerFP(a, "innerWidth", 0x000C) }

func dDVEBorderOuterWidth(p params) []Param { return dMEKeyerMarkerFP(p, "outerWidth") }
func eDVEBorderOuterWidth(a attrs) []byte   { return eMEKeyerMarkerFP(a, "outerWidth", 0x000C) }

func dDVEBorderInnerSoftness(p params) []Param { return dMEKeyerU16(p, "innerSoftness") }
func eDVEBorderInnerSoftness(a attrs) []byte   { return eMEKeyerU16(a, "innerSoftness") }

func dDVEBorderOuterSoftness(p params) []Param { return dMEKeyerU16(p, "outerSoftness") }
func eDVEBorderOuterSoftness(a attrs) []byte   { return eMEKeyerU16(a, "outerSoftness") }

func dDVEBorderOpacity(p params) []Param { return dMEKeyerU16(p, "opacity") }
func eDVEBorderOpacity(a attrs) []byte   { return eMEKeyerU16(a, "opacity") }

// --- USK DVE shadow ---

func dDVEShadowEnable(p params) []Param { return dMEKeyerBool(p, "enable") }
func eDVEShadowEnable(a attrs) []byte   { return eMEKeyerBool(a, "enable", 0) }

func dDVEShadowDirection(p params) []Param { return dMEKeyerMarkerFP(p, "direction") }
func eDVEShadowDirection(a attrs) []byte   { return eMEKeyerMarkerFP(a, "direction", 0x000C) }

func dDVEShadowAltitude(p params) []Param { return dMEKeyerU16(p, "altitude") }
func eDVEShadowAltitude(a attrs) []byte   { return eMEKeyerU16(a, "altitude") }

// --- Advanced chroma key: marker 0x000C, except the cursor ops (0x026C);
// the two enables carry an op-specific byte 3. ---

func ackFP(code uint16, id, name string) opCodec {
	return op(code, id, func(p params) []Param { return dMEKeyerMarkerFP(p, name) },
		func(a attrs) []byte { return eMEKeyerMarkerFP(a, name, 0x000C) })
}

func ackCursor(code uint16, id, name string) opCodec {
	return op(code, id, func(p params) []Param { return dMEKeyerMarkerFP(p, name) },
		func(a attrs) []byte { return eMEKeyerMarkerFP(a, name, 0x026C) })
}

func dACKSamplingModeEnabled(p params) []Param { return dMEKeyerBool(p, "enabled") }
func eACKSamplingModeEnabled(a attrs) []byte   { return eMEKeyerBool(a, "enabled", 0x02) }

func dACKPreviewEnabled(p params) []Param { return dMEKeyerBool(p, "enabled") }
func eACKPreviewEnabled(a attrs) []byte   { return eMEKeyerBool(a, "enabled", 0x04) }

// --- Fly key keyframes ---

func dFlyKeySetKeyFrame(p params) []Param { return dMEKeyerU16(p, "keyFrameIndex") }
func eFlyKeySetKeyFrame(a attrs) []byte   { return eMEKeyerU16(a, "keyFrameIndex") }

func dFlyKeyRunToKeyFrame(p params) []Param { return dMEKeyerU16(p, "keyFrameIndex") }
func eFlyKeyRunToKeyFrame(a attrs) []byte   { return eMEKeyerU16(a, "keyFrameIndex") }

func dFlyKeyRunToFull(p params) []Param { return meKeyer(p) }

func eFlyKeyRunToFull(a attrs) []byte {
	me := a.masked8(mEBI)
	keyer := byte(mask(a.int("keyIndex"), 8))
	return cat([]byte{me, keyer}, marker(0x024A))
}

func dFlyKeyRunToInfinity(p params) []Param {
	loc := p.at(2)
	head := meKeyer(p)
	name, ok := flyKeyLocationNames[loc]
	if !ok {
		name = p.str(2)
	}
	return append(head, Param{"location", name})
}

// eFlyKeyRunToInfinity takes a location's name or its number; another
// name raises.
func eFlyKeyRunToInfinity(a attrs) []byte {
	me := a.masked8(mEBI)
	keyer := byte(mask(a.int("keyIndex"), 8))
	loc := enumOrInt(flyKeyLocationToInt, a.req("location"), "FlyKey location")
	return []byte{me, keyer, byte(mask(loc, 8)), 0x02}
}

// eKeyType: a missing or unknown type is Luma, numbers included (SURFACE
// §5.4 item 9). Byte 3 is 0x03, as fresh recordings have it.
func eKeyType(a attrs) []byte {
	name, ok := a["type"]
	if !ok {
		name = "Luma"
	}
	t := uskTypeToInt[name]
	return pyBytes(a.intOr0(mEBI), a.int("keyIndex"), num(int64(t)), num(0x03))
}

func eKeyOnAir(a attrs) []byte {
	on := truthy2(a.req("onAir"))
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), u16le(num(int64(b2i(on)))))
}

func eKeyFillInput(a attrs) []byte {
	src := symbolToSource(a.req("input"))
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), u16le(src))
}

func eKeyCutInput(a attrs) []byte { return eKeyFillInput(a) }

func eKeyMaskEnable(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), boolByte(a.req("enable")), []byte{0})
}

// eKeyFlyEnable: byte 3 is 0x03, as fresh recordings have it.
func eKeyFlyEnable(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), boolByte(a.req("enable")), []byte{0x03})
}

// lumaClipGainMarker is _LUMA_CLIP_GAIN_MARKER: bytes 2-3 are
// uninitialised in Software Control's recordings; fresh ones show 0x000C.
const lumaClipGainMarker = 0x000C

func eLumaKeyClip(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), marker(lumaClipGainMarker), packFixed(a.req("clip")))
}

func eLumaKeyGain(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), marker(lumaClipGainMarker), packFixed(a.req("gain")))
}

func eLumaKeyInvert(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), boolByte(a.req("invert")), []byte{0})
}

func eLumaKeyPreMultiply(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), boolByte(a.req("preMultiply")), []byte{0})
}

func eDVEXSize(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), marker(dveMarker), packFixed(a.req("xSize")))
}

func eDVEYSize(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), marker(dveMarker), packFixed(a.req("ySize")))
}

func eDVESigned(a attrs, name string) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), marker(dveMarker), packSignedFixed(a.req(name)))
}

func eDVEXPos(a attrs) []byte       { return eDVESigned(a, "xPosition") }
func eDVEYPos(a attrs) []byte       { return eDVESigned(a, "yPosition") }
func eDVEMaskTop(a attrs) []byte    { return eDVESigned(a, "top") }
func eDVEMaskBottom(a attrs) []byte { return eDVESigned(a, "bottom") }
func eDVEMaskLeft(a attrs) []byte   { return eDVESigned(a, "left") }
func eDVEMaskRight(a attrs) []byte  { return eDVESigned(a, "right") }

func eDVEMaskEnable(a attrs) []byte {
	return cat(pyBytes(a.intOr0(mEBI), a.int("keyIndex")), boolByte(a.req("enable")), []byte{0})
}
