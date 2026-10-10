// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The display-unit readers: atemwire's reader(mx, ...) functions
// (bmdwire v1.3.0, atemwire/messages/*.py, SURFACE.md §2.1) as methods on
// State, one file per atemwire module (read_<module>.go). They turn the
// wire values the state holds into what ATEM Software Control shows
// (percent, dB, 0-1 positions, decoded names) and return atemwire's
// defaults for a packet that hasn't arrived, so callers never see wire
// units.
//
// This file holds what the readers share from atemwire/state.py: the
// counting helpers (me_count, me_keyer_count, _dsk_count), the coercion
// and scale primitives (decode_name, md5_hex, percent_from_thousandths,
// ...) and Python's round().
//
// Conventions, the same in every read_*.go file:
//   - A dict atemwire returns is a struct named for the reader with a View
//     suffix (USKDVEView); its fields are the dict keys Go-cased, tagged with
//     the key as its JSON name, in atemwire's order. A list is a slice, a
//     tuple an array, a dict keyed by id a map.
//   - Python's None is a nil pointer, in results (DVEStyle, a silenced
//     volume_db) and in arguments (FairlightEQBands(nil) is the master EQ).
//     Where atemwire returns [] or {}, the Go value is empty, never nil, so
//     it encodes to JSON the same way.
//   - An index atemwire would look up in a dict (an M/E, keyer, slot or
//     source) is an int; one the state has no entry for, negative or out of
//     range included, gives the default, as the missing dict key does.
//   - Floats are computed with atemwire's operations in atemwire's order,
//     so they match bit for bit; round(x, n) is pyRound.

import (
	"math"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// MECount is me_count (state.py), bmdwire v1.3.0: the number of M/Es,
// from the topology packet, else one past the highest M/E with a
// mixer-effect-config entry, else 1.
func (s State) MECount() int {
	count := 0
	if s.Topology != nil {
		count = int(s.Topology.MEUnits)
	}
	if count <= 0 {
		for i := len(s.MEs) - 1; i >= 0; i-- {
			if s.MEs[i].Config != nil {
				count = i + 1
				break
			}
		}
	}
	if count > 0 {
		return count
	}
	return 1
}

// MEKeyerCount is me_keyer_count (state.py), bmdwire v1.3.0: the number of
// upstream keyers on M/E me from its mixer-effect-config entry, else 4.
func (s State) MEKeyerCount(me int) int {
	if m := s.meAt(me); m != nil && m.Config != nil && m.Config.Keyers > 0 {
		return int(m.Config.Keyers)
	}
	return 4
}

// DSKCount is _dsk_count (state.py), bmdwire v1.3.0: the number of
// downstream keyers, from the topology packet, else one past the highest
// DSK with a dkey-state entry, else 1. atemwire keeps it private; it is
// what sizes build_full_state's dsks list, which a caller iterating the
// DSKs needs too.
func (s State) DSKCount() int {
	count := 0
	if s.Topology != nil {
		count = int(s.Topology.DownstreamKeyers)
	}
	if count <= 0 {
		for i := len(s.DSKs) - 1; i >= 0; i-- {
			if s.DSKs[i].State != nil {
				count = i + 1
				break
			}
		}
	}
	if count > 0 {
		return count
	}
	return 1
}

// meAt is M/E me, or nil where atemwire's _kv walk finds no entry.
func (s State) meAt(me int) *ME {
	if me < 0 || me >= len(s.MEs) {
		return nil
	}
	return &s.MEs[me]
}

// uskAt is keyer k of M/E me, or nil.
func (s State) uskAt(me, k int) *USK {
	m := s.meAt(me)
	if m == nil || k < 0 || k >= len(m.USKs) {
		return nil
	}
	return &m.USKs[k]
}

// dskAt is downstream keyer d, or nil.
func (s State) dskAt(d int) *DSK {
	if d < 0 || d >= len(s.DSKs) {
		return nil
	}
	return &s.DSKs[d]
}

// u16Key converts a slot or source number to the u16 key the state's maps
// use; ok is false for a number no packet can carry, which atemwire's dict
// lookup simply misses.
func u16Key(i int) (k uint16, ok bool) {
	if i < 0 || i > math.MaxUint16 {
		return 0, false
	}
	return uint16(i), true
}

// pyRound is Python's round(x, places) on a float: the exact binary value
// rounded to places decimals, ties to even, and read back (CPython's
// double_round through _Py_dg_dtoa). strconv formats the exact value with
// the same rounding, so this is bit for bit; scaling by 10^places first,
// as math.Round would need, is not.
func pyRound(x float64, places int) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	r, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', places, 64), 64)
	if err != nil {
		panic("atem: pyRound: " + err.Error())
	}
	return r
}

// percentFromThousandths is percent_from_thousandths (state.py): wire
// 0-10000 to percent, round(wire / 100.0, 2).
func percentFromThousandths(wire float64) float64 { return pyRound(wire/100.0, 2) }

// unitFromThousandths is unit_from_thousandths (state.py): wire 0-10000 to
// 0-1, round(wire / 10000.0, 4).
func unitFromThousandths(wire float64) float64 { return pyRound(wire/10000.0, 4) }

// valueFromThousandths is value_from_thousandths (state.py):
// round(wire / 1000.0, places).
func valueFromThousandths(wire float64, places int) float64 { return pyRound(wire/1000.0, places) }

// valueFromTenths is value_from_tenths (state.py): round(wire / 10.0, 2).
func valueFromTenths(wire float64) float64 { return pyRound(wire/10.0, 2) }

// percentFromUnit is percent_from_unit (state.py): 0-1 to percent,
// round(wire * 100.0, 2).
func percentFromUnit(wire float64) float64 { return pyRound(wire*100.0, 2) }

// wireDB is _wire_db_to_float (state.py) with lo_silenced=None: 0.01 dB on
// the wire to dB, round(wire / 100.0, 2).
func wireDB(wire int) float64 { return pyRound(float64(wire)/100.0, 2) }

// wireDBSilenced is _wire_db_to_float (state.py) with its default
// lo_silenced of -10000: nil (atemwire's None, "−∞") at or below it.
func wireDBSilenced(wire int) *float64 {
	if wire <= -10000 {
		return nil
	}
	v := wireDB(wire)
	return &v
}

// decodeName is decode_name (state.py) for a name the state holds: the
// bytes up to the first NUL, decoded as UTF-8 with errors="replace" and
// stripped of whitespace as Python's str.strip() sees it. Names atemwire
// parses into str (InPr, _pin, WhoI) are already cut and decoded, so for
// them only the strip does anything, which is atemwire's str branch;
// MPfe and MPrp names are its bytes branch. atemwire's default argument is
// only reachable with None, which the state never holds.
func decodeName(raw string) string {
	return pyStrip(cstr([]byte(raw)))
}

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string {
	for s != "" {
		r, n := utf8.DecodeRuneInString(s)
		if !pyIsSpace(r) {
			break
		}
		s = s[n:]
	}
	for s != "" {
		r, n := utf8.DecodeLastRuneInString(s)
		if !pyIsSpace(r) {
			break
		}
		s = s[:len(s)-n]
	}
	return s
}

// pyIsSpace is Python's str.isspace() for one character: Unicode's
// White_Space property (Go's unicode.IsSpace) plus the four information
// separators U+001C-U+001F, which Python counts (bidirectional class B or
// S) and Go doesn't.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f
}

// md5Hex is md5_hex (state.py): the hash as lower-case hex, or "" while it
// is all zeros (atemwire's "not computed yet").
func md5Hex(h MD5) string {
	if h == (MD5{}) {
		return ""
	}
	b, _ := h.MarshalText()
	return string(b)
}
