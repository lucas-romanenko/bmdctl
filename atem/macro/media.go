// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Media player ops: bmdwire v1.3.0 atemwire/macrotransfer/media.py.

func dMediaPlayerStillIndex(p params) []Param {
	return []Param{{"mediaPlayer", p.str(0)}, {"index", utoa(p.u16(2))}}
}

func dMediaPlayerStillOnly(p params) []Param {
	return []Param{{"mediaPlayer", p.str(0)}}
}

func eMediaPlayerStillIndex(a attrs) []byte {
	return cat(pyBytes(a.int("mediaPlayer"), num(0)), u16le(a.int("index")))
}

func eMediaPlayerStillOnly(a attrs) []byte {
	return pyBytes(a.int("mediaPlayer"), num(0), num(0), num(0))
}
