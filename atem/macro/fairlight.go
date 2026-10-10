// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Fairlight audio mixer ops (strip fader and mix type, master fader, EQ,
// compressor, limiter, expander, input and dynamics gain, headphones):
// bmdwire v1.3.0 atemwire/macrotransfer/fairlight.py.
//
// Strip ops start with a 12-byte prefix, [u16 source, u16 family marker,
// u64 sourceId], and the op's own fields follow.

// fairlightStripMarker is _FAIRLIGHT_STRIP_MARKER, bytes 2-3 of the fader
// and mix-type ops.
const fairlightStripMarker = 0x4346

// mixTypeNames is _MIX_TYPE_NAMES.
var mixTypeNames = map[int]string{1: "Off", 2: "On", 4: "AFV"}

var mixTypeToInt = invert(mixTypeNames)

// mixTypeTrailingMarker is _FAIRLIGHT_MIX_TYPE_TRAILING_MARKER.
const mixTypeTrailingMarker = 0x000C

// eqShapeNames is _FAIRLIGHT_EQ_SHAPE_NAMES.
var eqShapeNames = map[int]string{1: "LowShelf", 2: "LowPass", 4: "BandPass", 8: "Notch", 16: "HighPass", 32: "HighShelf"}

// eqRangeNames is _FAIRLIGHT_EQ_RANGE_NAMES.
var eqRangeNames = map[int]string{1: "Low", 2: "MidLow", 4: "MidHigh", 8: "High"}

var (
	eqShapeToInt = invert(eqShapeNames)
	eqRangeToInt = invert(eqRangeNames)
)

// enableTrailingMarker is _FAIRLIGHT_ENABLE_TRAILING_MARKER.
const enableTrailingMarker = 0x000C

func dFairlightStripFaderGain(p params) []Param {
	source := p.u16(0)
	sid := p.u64(4)
	gain := p.i32(12)
	return []Param{{"input", sourceToSymbol(int(source))}, {"sourceId", utoa(sid)}, {"gain", q16(int64(gain))}}
}

func dFairlightStripMixType(p params) []Param {
	source := p.u16(0)
	sid := p.u64(4)
	state := int(p.u16(12))
	name, ok := mixTypeNames[state]
	if !ok {
		name = utoa(uint16(state))
	}
	return []Param{{"input", sourceToSymbol(int(source))}, {"sourceId", utoa(sid)}, {"mixType", name}}
}

func dFairlightMasterFaderGain(p params) []Param {
	return []Param{{"gain", q16(int64(p.i32(0)))}}
}

// stripPrefix is _strip_prefix_decode: the input and sourceId attributes.
func stripPrefix(p params) []Param {
	source := p.u16(0)
	p.u16(2)
	sid := p.u64(4)
	return []Param{{"input", sourceToSymbol(int(source))}, {"sourceId", utoa(sid)}}
}

// stripPrefixEncode is _strip_prefix_encode.
func stripPrefixEncode(a attrs, fam uint16) []byte {
	src := symbolToSource(a.req("input"))
	sid := resolveSourceID(a)
	return cat(le(mask(src, 16), 2), marker(fam), le(sid, 8))
}

// resolveSourceID is _resolve_source_id: missing or empty is the stereo
// sentinel 0xFFFFFFFFFFFF0000 (-65280); anything else int(), masked to 64
// bits.
func resolveSourceID(a attrs) uint64 {
	v, ok := a["sourceId"]
	if !ok || v == "" {
		return 0xFFFFFFFFFFFF0000
	}
	n, exc := parseInt(v, 10)
	if exc != nil {
		raise("ValueError", "unparseable sourceId %s", pyRepr(v))
	}
	return mask(n, 64)
}

// [prefix, i32 Q16.16]. Every caller is signed.
func dStripValueFP(p params, name string) []Param {
	head := stripPrefix(p)
	return append(head, Param{name, q16(int64(p.i32(12)))})
}

func eStripValueFP(a attrs, fam uint16, name string) []byte {
	payload := i32le(a.fixed(name))
	return cat(stripPrefixEncode(a, fam), payload)
}

// [prefix, u8 enabled, u8 marker, u16 marker]
func dStripEnable(p params) []Param {
	head := stripPrefix(p)
	return append(head, Param{"enabled", boolStr(p.at(12) != 0)})
}

func eStripEnable(a attrs, fam uint16, byte13 byte, trailing uint16) []byte {
	val := truthy3(a.req("enabled"))
	return cat(stripPrefixEncode(a, fam), []byte{b2i(val), byte13}, marker(trailing))
}

// --- Equaliser ---

func dEQGain(p params) []Param { return dStripValueFP(p, "gain") }
func eEQGain(a attrs) []byte   { return eStripValueFP(a, fairlightFam4346, "gain") }

func dEQBandEnabled(p params) []Param {
	head := stripPrefix(p)
	return append(head, Param{"band", p.str(12)}, Param{"enabled", boolStr(p.at(13) != 0)})
}

func eEQBandEnabled(a attrs) []byte {
	band := byte(mask(a.int("band"), 8))
	val := truthy3(a.req("enabled"))
	return cat(stripPrefixEncode(a, fairlightFam03E1), []byte{band, b2i(val)}, marker(0x000C))
}

func dEQBandEnum(p params, name string, table map[int]string) []Param {
	head := stripPrefix(p)
	v := int(p.u16(14))
	n, ok := table[v]
	if !ok {
		n = utoa(uint16(v))
	}
	return append(head, Param{"band", p.str(12)}, Param{name, n})
}

func eEQBandEnum(a attrs, name string, table map[string]int, what string) []byte {
	band := byte(mask(a.int("band"), 8))
	v := enumOrInt(table, a.req(name), what)
	return cat(stripPrefixEncode(a, fairlightFam03E1), []byte{band, 0xAC}, le(mask(v, 16), 2))
}

func dEQBandShape(p params) []Param { return dEQBandEnum(p, "shape", eqShapeNames) }
func eEQBandShape(a attrs) []byte   { return eEQBandEnum(a, "shape", eqShapeToInt, "EQ shape") }
func dEQBandRange(p params) []Param { return dEQBandEnum(p, "range", eqRangeNames) }
func eEQBandRange(a attrs) []byte   { return eEQBandEnum(a, "range", eqRangeToInt, "EQ range") }

func dEQBandFrequency(p params) []Param {
	head := stripPrefix(p)
	return append(head, Param{"band", p.str(12)}, Param{"frequency", utoa(p.u32(16))})
}

func eEQBandFrequency(a attrs) []byte {
	band := byte(mask(a.int("band"), 8))
	freq := num(int64(mask(a.int("frequency"), 32)))
	return cat(stripPrefixEncode(a, fairlightFam03E1), []byte{band, 0xAC}, marker(0x000C), u32le(freq))
}

// [prefix, u8 band, u8 0xAC, u16 0x000C, i32 Q16.16]: band gain and Q.
func dEQBandFP(p params, name string) []Param {
	head := stripPrefix(p)
	v := p.i32(16)
	return append(head, Param{"band", p.str(12)}, Param{name, q16(int64(v))})
}

// eEQBandFP range-checks the value last, after the prefix, as atemwire
// does.
func eEQBandFP(a attrs, name string) []byte {
	band := byte(mask(a.int("band"), 8))
	v := a.fixed(name)
	prefix := stripPrefixEncode(a, fairlightFam03E1)
	return cat(prefix, []byte{band, 0xAC}, marker(0x000C), i32le(v))
}

func dEQBandGain(p params) []Param    { return dEQBandFP(p, "gain") }
func eEQBandGain(a attrs) []byte      { return eEQBandFP(a, "gain") }
func dEQBandQFactor(p params) []Param { return dEQBandFP(p, "qFactor") }
func eEQBandQFactor(a attrs) []byte   { return eEQBandFP(a, "qFactor") }

// --- Dynamics: compressor and input/dynamics gain (0x4346), limiter
// (0x03E1), expander (0x024A; its enables carry 0x60 and 0x01B4). ---

func stripFP(code uint16, id string, fam uint16, name string) opCodec {
	return op(code, id, func(p params) []Param { return dStripValueFP(p, name) },
		func(a attrs) []byte { return eStripValueFP(a, fam, name) })
}

func stripEnable(code uint16, id string, fam uint16, byte13 byte, trailing uint16) opCodec {
	return op(code, id, dStripEnable, func(a attrs) []byte { return eStripEnable(a, fam, byte13, trailing) })
}

// --- Headphones: four ops, one codec, attribute gain. ---

func dHPGain(p params) []Param { return []Param{{"gain", q16(int64(p.i32(0)))}} }
func eHPGain(a attrs) []byte   { return i32le(a.fixed("gain")) }

// --- Strip fader, mix type and master fader ---

// eFairlightStripFaderGain packs the gain with struct.pack('<i'): out of
// range raises struct.error, not the ValueError of the other gains
// (SURFACE §5.4 item 8).
func eFairlightStripFaderGain(a attrs) []byte {
	source := symbolToSource(a.req("input"))
	sid := resolveSourceID(a)
	gain := a.fixed("gain")
	return cat(le(mask(source, 16), 2), marker(fairlightStripMarker), le(sid, 8), packi(gain))
}

// eFairlightStripMixType: an unknown mix type string is On (SURFACE §5.4
// item 9).
func eFairlightStripMixType(a attrs) []byte {
	source := symbolToSource(a.req("input"))
	sid := resolveSourceID(a)
	state, ok := mixTypeToInt[a.req("mixType")]
	if !ok {
		state = 2
	}
	return cat(le(mask(source, 16), 2), marker(fairlightStripMarker), le(sid, 8),
		packH(num(int64(state))), marker(mixTypeTrailingMarker))
}

func eFairlightMasterFaderGain(a attrs) []byte { return packi(a.fixed("gain")) }
