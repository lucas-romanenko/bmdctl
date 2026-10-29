// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Shared primitives, layout helpers and enum tables: bmdwire v1.3.0
// atemwire/macrotransfer/_helpers.py. Decoders (dXxx) read a parameter
// block and return the op's attributes in atemwire's dict order; encoders
// (eXxx) read the attributes and return the parameter block. Both raise
// (panic a pyExc) where atemwire raises, in the same order.

import (
	"math/big"
	"strconv"
	"strings"
)

const mEBI = "mixEffectBlockIndex"

// internalSourceNames is _INTERNAL_SOURCE_NAMES: source ids that decode
// to a symbolic name (besides 1-20, Camera1-Camera20).
var internalSourceNames = map[int]string{
	0:     "Black",
	1000:  "Bars",
	2001:  "Color1",
	2002:  "Color2",
	3010:  "MediaPlayer1",
	3011:  "MediaPlayer1Key",
	3020:  "MediaPlayer2",
	3021:  "MediaPlayer2Key",
	1301:  "XLRMic",
	1401:  "ExternalTRS",
	4000:  "ProgramOut",
	4010:  "PreviewOut",
	4030:  "CleanFeed1",
	7001:  "CleanFeed1",
	7002:  "CleanFeed2",
	5010:  "ME1Program",
	5011:  "ME1Preview",
	8001:  "Auxiliary1",
	8002:  "Auxiliary2",
	8003:  "Auxiliary3",
	8004:  "Auxiliary4",
	8005:  "Auxiliary5",
	8006:  "Auxiliary6",
	9000:  "MultiViewer1",
	10010: "ProgramOut",
	10011: "PreviewOut",
}

// symbolToSourceID is _SYMBOL_TO_SOURCE: where two ids share a name, the
// one ATEM Software Control writes in recorded macros.
var symbolToSourceID = map[string]int64{
	"Black": 0, "Bars": 1000, "Color1": 2001, "Color2": 2002,
	"MediaPlayer1": 3010, "MediaPlayer1Key": 3011,
	"MediaPlayer2": 3020, "MediaPlayer2Key": 3021,
	"XLRMic": 1301, "ExternalTRS": 1401,
	"ProgramOut": 10010, "PreviewOut": 10011,
	"CleanFeed1": 7001, "CleanFeed2": 7002,
	"ME1Program": 5010, "ME1Preview": 5011,
	"MultiViewer1": 9000,
	"Auxiliary1":   8001, "Auxiliary2": 8002, "Auxiliary3": 8003,
	"Auxiliary4": 8004, "Auxiliary5": 8005, "Auxiliary6": 8006,
}

// sourceToSymbol is _resolve_source_to_symbol.
func sourceToSymbol(sid int) string {
	if sid >= 1 && sid <= 20 {
		return "Camera" + strconv.Itoa(sid)
	}
	if n, ok := internalSourceNames[sid]; ok {
		return n
	}
	return strconv.Itoa(sid)
}

// symbolToSource is _resolve_symbol_to_source: a number (as int() reads
// it), "Camera" and a number, or a name from symbolToSourceID. The result
// is not range-checked; callers mask it to 16 bits.
func symbolToSource(name string) *big.Int {
	s := pyStrip(name)
	if v, exc := parseInt(s, 10); exc == nil {
		return v
	}
	if tail, ok := strings.CutPrefix(s, "Camera"); ok {
		if v, exc := parseInt(tail, 10); exc == nil {
			return v
		}
	}
	if v, ok := symbolToSourceID[s]; ok {
		return num(v)
	}
	raise("ValueError", "can't resolve macro source symbol %s", pyRepr(name))
	return nil
}

// boolStr is _bool_str.
func boolStr(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// fmtScalar is _fmt_scalar as bmdwire#24 (c0e211e) made it: six
// significant digits with trailing zeros dropped, the way ATEM Software
// Control's XML writes every float (Python's '%.6g', C++'s default stream
// precision), so -21474836/65536 is "-327.68" and -5243/65536
// "-0.0800018"; zero (and -0) is "0". Go's 'g' formatting rounds the exact
// binary value half to even, as Python's does (oracle/macro.json pins the
// ties). Above 10 that is fewer decimals than Q16.16 resolves, so a decode
// and re-encode can move a value by a few 1/65536 steps, as saving and
// restoring through ASC does.
func fmtScalar(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// q16 is the Q16.16 decode every fixed-point attribute uses.
func q16(v int64) string { return fmtScalar(float64(v) / 65536.0) }

func utoa[T uint16 | uint32 | uint64](v T) string { return strconv.FormatUint(uint64(v), 10) }

// The three spellings of "is this attribute true" in atemwire's encoders.

// truthy is str(v).strip().lower() in ('true', '1', 'yes', 'on'):
// _bool_byte and the me/keyer bool helpers.
func truthy(v string) bool { return pyLowerIn(pyStrip(v), "true", "1", "yes", "on") }

// truthy3 is the same without 'on': FadeToBlackEnabled and the Fairlight
// enables.
func truthy3(v string) bool { return pyLowerIn(pyStrip(v), "true", "1", "yes") }

// truthy2 is str(v).lower() in ('true', '1'), unstripped: KeyOnAir and the
// DSK on-air and tie ops.
func truthy2(v string) bool { return pyLowerIn(v, "true", "1") }

func b2i(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// boolByte is _bool_byte.
func boolByte(v string) []byte { return []byte{b2i(truthy(v))} }

// attrs is the op dict an encoder reads.
type attrs map[string]string

// req is attrs[name], raising KeyError when it is missing.
func (a attrs) req(name string) string {
	v, ok := a[name]
	if !ok {
		panic(&pyExc{"KeyError", name})
	}
	return v
}

// int is int(attrs[name]).
func (a attrs) int(name string) *big.Int { return pyInt(a.req(name)) }

// intOr0 is int(attrs.get(name, 0)).
func (a attrs) intOr0(name string) *big.Int {
	if v, ok := a[name]; ok {
		return pyInt(v)
	}
	return num(0)
}

// masked8 is int(attrs.get(name, 0)) & 0xFF.
func (a attrs) masked8(name string) byte { return byte(mask(a.intOr0(name), 8)) }

// fixed is int(round(float(attrs[name]) * 65536)), the Q16.16 encode.
func (a attrs) fixed(name string) *big.Int { return pyRound(pyFloat(a.req(name)) * 65536.0) }

// u16le is _u16le_bytes: masked to 16 bits.
func u16le(v *big.Int) []byte { return le(mask(v, 16), 2) }

// u32le is _u32le_bytes: masked to 32 bits.
func u32le(v *big.Int) []byte { return le(mask(v, 32), 4) }

// marker is _u16le_bytes of a constant.
func marker(v uint16) []byte { return le(uint64(v), 2) }

// i32le is _i32le_bytes: a ValueError outside the int32 range.
func i32le(v *big.Int) []byte {
	if !v.IsInt64() || v.Int64() < -0x80000000 || v.Int64() >= 0x80000000 {
		raise("ValueError", "value %s out of int32 range for i32 LE encode", v.String())
	}
	return le(uint64(v.Int64()), 4)
}

// packFixed is _pack_fixed_value: u32, masked.
func packFixed(v string) []byte { return u32le(pyRound(pyFloat(v) * 65536.0)) }

// packSignedFixed is _pack_signed_fixed_value: i32, range-checked.
func packSignedFixed(v string) []byte { return i32le(pyRound(pyFloat(v) * 65536.0)) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// --- Layout helpers --------------------------------------------------------

// [u8 me, u8 family marker, u16 value]
func dMEMarkerU16(p params, name string) []Param {
	return []Param{{mEBI, p.str(0)}, {name, utoa(p.u16(2))}}
}

func eMEMarkerU16(a attrs, name string, fam byte) []byte {
	me := a.masked8(mEBI)
	return cat([]byte{me, fam}, u16le(a.int(name)))
}

// [u8 me, u8 family marker, u16 source]
func dMEMarkerSource(p params, name string) []Param {
	return []Param{{mEBI, p.str(0)}, {name, sourceToSymbol(int(p.u16(2)))}}
}

func eMEMarkerSource(a attrs, fam byte, name string) []byte {
	me := a.masked8(mEBI)
	src := symbolToSource(a.req(name))
	return cat([]byte{me, fam}, u16le(src))
}

// [u8 me, u8 bool, u16 op marker]
func dMEValueMarkerBool(p params, name string) []Param {
	return []Param{{mEBI, p.str(0)}, {name, boolStr(p.at(1) != 0)}}
}

func eMEValueMarkerBool(a attrs, name string, opMarker uint16) []byte {
	me := a.masked8(mEBI)
	val := b2i(truthy(a.req(name)))
	return cat([]byte{me, val}, marker(opMarker))
}

// [u8 me, u8 keyer, u8 bool, u8 marker]
func dMEKeyerBool(p params, name string) []Param {
	return []Param{{mEBI, p.str(0)}, {"keyIndex", p.str(1)}, {name, boolStr(p.at(2) != 0)}}
}

func eMEKeyerBool(a attrs, name string, byte3 byte) []byte {
	me := a.masked8(mEBI)
	keyer := byte(mask(a.int("keyIndex"), 8))
	val := b2i(truthy(a.req(name)))
	return []byte{me, keyer, val, byte3}
}

// [u8 me, u8 keyer, u16 value]
func dMEKeyerU16(p params, name string) []Param {
	return []Param{{mEBI, p.str(0)}, {"keyIndex", p.str(1)}, {name, utoa(p.u16(2))}}
}

func eMEKeyerU16(a attrs, name string) []byte {
	me := a.masked8(mEBI)
	keyer := byte(mask(a.int("keyIndex"), 8))
	return cat([]byte{me, keyer}, u16le(a.int(name)))
}

// [u8 me, u8 keyer, u16 marker, i32 Q16.16]. Every caller is signed.
func dMEKeyerMarkerFP(p params, name string) []Param {
	v := p.i32(4)
	return []Param{{mEBI, p.str(0)}, {"keyIndex", p.str(1)}, {name, q16(int64(v))}}
}

func eMEKeyerMarkerFP(a attrs, name string, mark uint16) []byte {
	me := a.masked8(mEBI)
	keyer := byte(mask(a.int("keyIndex"), 8))
	v := a.fixed(name)
	return cat([]byte{me, keyer}, marker(mark), i32le(v))
}

// [u8 me, u8 family marker, u16 op marker, i32 Q16.16]: wipe border and
// stinger DVE values. Every caller is signed.
func dMEDoubleMarkerFP(p params, name string) []Param {
	v := p.i32(4)
	return []Param{{mEBI, p.str(0)}, {name, q16(int64(v))}}
}

func eMEDoubleMarkerFP(a attrs, name string, fam byte, opMarker uint16) []byte {
	me := a.masked8(mEBI)
	v := a.fixed(name)
	return cat([]byte{me, fam}, marker(opMarker), i32le(v))
}

// [u8 generator, u8 0, u16 marker, i32 Q16.16]: colour generators.
func dGenMarkerFP(p params, name string) []Param {
	v := p.i32(4)
	return []Param{{"colorGeneratorIndex", p.str(0)}, {name, q16(int64(v))}}
}

func eGenMarkerFP(a attrs, name string) []byte {
	gen := a.masked8("colorGeneratorIndex")
	v := a.fixed(name)
	return cat([]byte{gen, 0}, marker(opMarker026C), i32le(v))
}

// --- Enum tables ------------------------------------------------------------

// wipePatternNames is _WIPE_PATTERN_NAMES (TransitionWipePattern).
var wipePatternNames = map[int]string{
	0: "LeftToRightBar", 1: "TopToBottomBar", 2: "HorizontalBarnDoor", 3: "VerticalBarnDoor",
	4: "CornersInFourBox", 5: "RectangleIris", 6: "DiamondIris", 7: "CircleIris",
	8: "TopLeftBox", 9: "TopRightBox", 10: "BottomRightBox", 11: "BottomLeftBox",
	12: "TopCentreBox", 13: "RightCentreBox", 14: "BottomCentreBox", 15: "LeftCentreBox",
	16: "TopLeftDiagonal", 17: "TopRightDiagonal",
}

// dvePatternNames is _DVE_PATTERN_NAMES (TransitionDVEPattern). 0-15, 32,
// 33 and 35 up are unmapped on purpose and decode as numbers.
var dvePatternNames = map[int]string{
	16: "SqueezeTopLeft", 17: "SqueezeTop", 18: "SqueezeTopRight", 19: "SqueezeLeft",
	20: "SqueezeRight", 21: "SqueezeBottomLeft", 22: "SqueezeBottom", 23: "SqueezeBottomRight",
	24: "PushTopLeft", 25: "PushTop", 26: "PushTopRight", 27: "PushLeft",
	28: "PushRight", 29: "PushBottomLeft", 30: "PushBottom", 31: "PushBottomRight",
	34: "GraphicLogoWipe",
}

// flyKeyLocationNames is _FLYKEY_LOCATION_NAMES (FlyKeyRunToInfinity),
// which since bmdwire#22 (1bee7d4) is helpers.FLYKEY_LOCATION_NAMES, the
// table the live RFlK uses: a copy of atem.FlyKeyLocationNames. 0 is
// CentreOfKey and 1-9 the 3x3 grid, 5 MiddleCentre, as ATEM Software
// Control's recorded macro holds them.
var flyKeyLocationNames = map[int]string{
	0: "CentreOfKey", 1: "TopLeft", 2: "TopCentre", 3: "TopRight", 4: "MiddleLeft",
	5: "MiddleCentre", 6: "MiddleRight", 7: "BottomLeft", 8: "BottomCentre", 9: "BottomRight",
}

// uskTypeNames is _USK_TYPE_NAMES (KeyType).
var uskTypeNames = map[int]string{0: "Luma", 1: "Chroma", 2: "Pattern", 3: "DVE"}

// Family markers at parameter byte 1, and the op marker of the fixed-point
// wipe, stinger and colour-generator ops.
const (
	famMix       = 0x4D
	famDip       = 0x4E
	famWipe      = 0x88
	famDVE       = 0x88
	famStinger   = 0x88
	famFTBRate   = 0x00
	opMarker026C = 0x026C

	// Fairlight family markers at strip-prefix bytes 2-3.
	fairlightFam4346 = 0x4346 // fader, mix type, EQ gain, compressor, input and dynamics gain
	fairlightFam03E1 = 0x03E1 // EQ bands, limiter
	fairlightFam024A = 0x024A // expander

	// dveMarker is _DVE_MARKER, the trailing marker of the DVE and DSK
	// mask ops.
	dveMarker = 0x000C
)

// invert is Python's {v: k for k, v in d.items()}.
func invert(m map[int]string) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

var (
	wipePatternToInt    = invert(wipePatternNames)
	dvePatternToInt     = invert(dvePatternNames)
	flyKeyLocationToInt = invert(flyKeyLocationNames)
	uskTypeToInt        = invert(uskTypeNames)
)

// enumOrInt is the encoders' "name from the table, else int(), else
// ValueError(unknown <what> 'value')" for wipe and DVE patterns, fly-key
// locations and EQ shapes and ranges. The result is masked by the caller.
func enumOrInt(table map[string]int, v, what string) *big.Int {
	if n, ok := table[v]; ok {
		return num(int64(n))
	}
	n, exc := parseInt(v, 10)
	if exc != nil {
		raise("ValueError", "unknown %s %s", what, pyRepr(v))
	}
	return n
}
