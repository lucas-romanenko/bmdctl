// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Command is one command a client sends to the switcher: the Go
// counterpart of an atemwire Send class together with the operation that
// wraps it (bmdwire v1.3.0, atemwire/messages).
type Command interface {
	// Opcode is the four-character command code, for example "CPgI".
	Opcode() string
	// Encode returns the bytes that go on the wire: the 8-byte command
	// header (length, two zero bytes, opcode) and the payload, as
	// atemwire's get_command() does.
	Encode() []byte
	// EchoKey is the Change.Key of the state packet the switcher sends
	// back once it has applied the command, for example "PrgI:0". It is
	// "" for commands with no echo of their own (SURFACE.md §2.7).
	EchoKey() string
	// Unverified reports a command whose bytes are pinned by atemwire's
	// tests but which nobody has confirmed against a switcher: the 20
	// restored-upstream classes and CKMs (SURFACE.md §5.4 item 14).
	Unverified() bool
	// Destructive reports a command that changes what the switcher does
	// in a way that is hard to undo or drops clients: VideoMode,
	// SaveStartupState, ClearStartupState and ClearStill.
	Destructive() bool
}

// Validator is implemented by commands whose fields can hold values the
// wire cannot carry. Session.Send calls it before encoding; where atemwire
// raises (struct.error from a value out of range, ValueError from a bad
// argument combination) Validate returns an error instead.
type Validator interface {
	Validate() error
}

// maxCommandBytes is the largest command a session sends, and the most
// command bytes it packs into one packet (atemwire's per-packet BUDGET in
// ATEMConnection._drain_cmd_queue).
const maxCommandBytes = 1200

// wire builds one command's bytes. Each command type's build method writes
// its payload through it; Encode returns the bytes and Validate the first
// value that didn't fit or couldn't be converted, the point where
// atemwire's struct.pack or round() raises.
type wire struct {
	op  string
	b   []byte
	err error
}

func newWire(op string, size int) *wire { return &wire{op: op, b: make([]byte, size)} }

func (w *wire) fail(format string, args ...any) {
	if w.err == nil {
		w.err = fmt.Errorf(format, args...)
	}
}

// encode returns the 8-byte command header and the payload.
func (w *wire) encode() []byte {
	out := make([]byte, 8, 8+len(w.b))
	binary.BigEndian.PutUint16(out, uint16(8+len(w.b)))
	copy(out[4:], w.op)
	return append(out, w.b...)
}

func (w *wire) put(at int, name string, v, lo, hi int64, size int) {
	if v < lo || v > hi {
		w.fail("%s %d does not fit (%d to %d)", name, v, lo, hi)
		return
	}
	switch size {
	case 1:
		w.b[at] = byte(v)
	case 2:
		binary.BigEndian.PutUint16(w.b[at:], uint16(v))
	case 4:
		binary.BigEndian.PutUint32(w.b[at:], uint32(v))
	case 8:
		binary.BigEndian.PutUint64(w.b[at:], uint64(v))
	}
}

func (w *wire) u8(at int, name string, v int64)  { w.put(at, name, v, 0, math.MaxUint8, 1) }
func (w *wire) i8(at int, name string, v int64)  { w.put(at, name, v, math.MinInt8, math.MaxInt8, 1) }
func (w *wire) u16(at int, name string, v int64) { w.put(at, name, v, 0, math.MaxUint16, 2) }
func (w *wire) i16(at int, name string, v int64) { w.put(at, name, v, math.MinInt16, math.MaxInt16, 2) }
func (w *wire) u32(at int, name string, v int64) { w.put(at, name, v, 0, math.MaxUint32, 4) }
func (w *wire) i32(at int, name string, v int64) { w.put(at, name, v, math.MinInt32, math.MaxInt32, 4) }

func (w *wire) flag(at int, v bool) {
	if v {
		w.b[at] = 1
	}
}

// str writes s's bytes at at, cut to size, zero-padded: struct's "Ns".
func (w *wire) str(at, size int, s string) { copy(w.b[at:at+size], s) }

// round is Python's int(round(v)): half to even. NaN and the infinities
// raise in Python; so do values no wire field can hold.
func (w *wire) round(name string, v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		w.fail("%s is %v", name, v)
		return 0
	}
	r := math.RoundToEven(v)
	if math.Abs(r) > 1<<53 {
		w.fail("%s %v does not fit", name, v)
		return 0
	}
	return int64(r)
}

// clampRound is max(lo, min(hi, int(round(v)))), the shape of atemwire's
// percent and unit helpers: NaN and the infinities still raise, because
// round() runs first.
func (w *wire) clampRound(name string, v float64, lo, hi int64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		w.fail("%s is %v", name, v)
		return 0
	}
	r := math.RoundToEven(v)
	switch {
	case r < float64(lo):
		return lo
	case r > float64(hi):
		return hi
	}
	return int64(r)
}

// truncate is Python's int(v) on a float: toward zero.
func (w *wire) truncate(name string, v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		w.fail("%s is %v", name, v)
		return 0
	}
	t := math.Trunc(v)
	if math.Abs(t) > 1<<53 {
		w.fail("%s %v does not fit", name, v)
		return 0
	}
	return int64(t)
}

// The display-to-wire conversions of atemwire/state.py, each
// max(lo, min(hi, int(round(v * scale)))).

// percentToTenths: percent 0-100 to wire 0-1000 (clip, gain).
func (w *wire) percentToTenths(name string, v float64) int64 {
	return w.clampRound(name, v*10, 0, 1000)
}

// percentToThousandths: percent 0-100 to wire 0-10000 (width, softness).
func (w *wire) percentToThousandths(name string, v float64) int64 {
	return w.clampRound(name, v*100, 0, 10000)
}

// unitToThousandths: 0-1 to wire 0-10000 (positions).
func (w *wire) unitToThousandths(name string, v float64) int64 {
	return w.clampRound(name, v*10000, 0, 10000)
}

// sizeThousandths: 0-2 to wire 0-2000 (DVE size).
func (w *wire) sizeThousandths(name string, v float64) int64 {
	return w.clampRound(name, v*1000, 0, 2000)
}

// pyMin and pyMax are Python's two-argument min and max on floats: the
// first argument wins unless the second compares strictly smaller
// (larger), so a NaN second argument loses and a NaN first one wins.
func pyMin(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}

func pyMax(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

// Set returns a pointer to v: a convenience for the optional fields of
// masked commands, atem.WipeSettings{Width: atem.Set(50.0)}.
func Set[T any](v T) *T { return &v }
