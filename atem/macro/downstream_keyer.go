// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Downstream keyer ops (fill and cut input, rate, auto, on air, tie, clip,
// gain, mask enable, pre-multiply): bmdwire v1.3.0
// atemwire/macrotransfer/downstream_keyer.py.

func dDSKFillInput(p params) []Param {
	return []Param{{"keyIndex", p.str(0)}, {"input", sourceToSymbol(int(p.u16(2)))}}
}

func dDSKCutInput(p params) []Param { return dDSKFillInput(p) }

func dDSKRate(p params) []Param {
	return []Param{{"keyIndex", p.str(0)}, {"rate", utoa(p.u16(2))}}
}

func dDSKAuto(p params) []Param { return []Param{{"keyIndex", p.str(0)}} }

// dskFlag is [u8 dsk, u8 flag, u16 _]: on air, tie, mask enable,
// pre-multiply.
func dskFlag(p params, name string) []Param {
	return []Param{{"keyIndex", p.str(0)}, {name, boolStr(p.at(1) != 0)}}
}

func dDSKOnAir(p params) []Param       { return dskFlag(p, "onAir") }
func dDSKTie(p params) []Param         { return dskFlag(p, "tie") }
func dDSKMaskEnable(p params) []Param  { return dskFlag(p, "enable") }
func dDSKPreMultiply(p params) []Param { return dskFlag(p, "preMultiply") }

func dDSKClip(p params) []Param {
	return []Param{{"keyIndex", p.str(0)}, {"clip", q16(int64(p.u32(4)))}}
}

func dDSKGain(p params) []Param {
	return []Param{{"keyIndex", p.str(0)}, {"gain", q16(int64(p.u32(4)))}}
}

func eDSKFillInput(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex"), num(0)), u16le(symbolToSource(a.req("input"))))
}

func eDSKCutInput(a attrs) []byte { return eDSKFillInput(a) }

func eDSKRate(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex"), num(0)), u16le(a.int("rate")))
}

func eDSKAuto(a attrs) []byte {
	return pyBytes(a.int("keyIndex"), num(0), num(0), num(0))
}

func eDSKOnAir(a attrs) []byte {
	on := truthy2(a.req("onAir"))
	return pyBytes(a.int("keyIndex"), num(int64(b2i(on))), num(0), num(0))
}

func eDSKTie(a attrs) []byte {
	tie := truthy2(a.req("tie"))
	return pyBytes(a.int("keyIndex"), num(int64(b2i(tie))), num(0), num(0))
}

// dskMarker is _DSK_MARKER: recordings have 0 in bytes 2-3.
const dskMarker = 0x0000

func eDSKClip(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex"), num(0)), marker(dskMarker), packFixed(a.req("clip")))
}

func eDSKGain(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex"), num(0)), marker(dskMarker), packFixed(a.req("gain")))
}

// eDSKMaskEnable writes the DVE marker 0x000C in bytes 2-3, as atemwire
// does (its comment says 0).
func eDSKMaskEnable(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex")), boolByte(a.req("enable")), marker(dveMarker))
}

func eDSKPreMultiply(a attrs) []byte {
	return cat(pyBytes(a.int("keyIndex")), boolByte(a.req("preMultiply")), marker(dskMarker))
}
