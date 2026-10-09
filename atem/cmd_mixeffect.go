// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.1 mixeffect, from bmdwire v1.3.0
// atemwire/messages/switching.py. ME is the 0-based M/E index.

// ProgramInput is CPgI (ProgramInputCommand, op set_program): put a
// source on an M/E's program bus.
type ProgramInput struct {
	ME     uint8
	Source uint16
}

func (c ProgramInput) Opcode() string    { return "CPgI" }
func (c ProgramInput) Encode() []byte    { return c.build().encode() }
func (c ProgramInput) Validate() error   { return c.build().err }
func (c ProgramInput) EchoKey() string   { return key("PrgI", c.ME) }
func (c ProgramInput) Unverified() bool  { return false }
func (c ProgramInput) Destructive() bool { return false }

func (c ProgramInput) build() *wire {
	w := newWire("CPgI", 4)
	w.u8(0, "me", int64(c.ME))
	w.u16(2, "source", int64(c.Source))
	return w
}

// PreviewInput is CPvI (PreviewInputCommand, op set_preview): put a source
// on an M/E's preview bus.
type PreviewInput struct {
	ME     uint8
	Source uint16
}

func (c PreviewInput) Opcode() string    { return "CPvI" }
func (c PreviewInput) Encode() []byte    { return c.build().encode() }
func (c PreviewInput) Validate() error   { return c.build().err }
func (c PreviewInput) EchoKey() string   { return key("PrvI", c.ME) }
func (c PreviewInput) Unverified() bool  { return false }
func (c PreviewInput) Destructive() bool { return false }

func (c PreviewInput) build() *wire {
	w := newWire("CPvI", 4)
	w.u8(0, "me", int64(c.ME))
	w.u16(2, "source", int64(c.Source))
	return w
}

// Cut is DCut (CutCommand, op cut). It has no echo of its own: the result
// shows up as PrgI and PrvI.
type Cut struct {
	ME uint8
}

func (c Cut) Opcode() string    { return "DCut" }
func (c Cut) Encode() []byte    { return c.build().encode() }
func (c Cut) Validate() error   { return c.build().err }
func (c Cut) EchoKey() string   { return "" }
func (c Cut) Unverified() bool  { return false }
func (c Cut) Destructive() bool { return false }

func (c Cut) build() *wire {
	w := newWire("DCut", 4)
	w.u8(0, "me", int64(c.ME))
	return w
}

// Auto is DAut (AutoCommand, op auto): run the M/E's transition with its
// current style and rate. It has no echo of its own.
type Auto struct {
	ME uint8
}

func (c Auto) Opcode() string    { return "DAut" }
func (c Auto) Encode() []byte    { return c.build().encode() }
func (c Auto) Validate() error   { return c.build().err }
func (c Auto) EchoKey() string   { return "" }
func (c Auto) Unverified() bool  { return false }
func (c Auto) Destructive() bool { return false }

func (c Auto) build() *wire {
	w := newWire("DAut", 4)
	w.u8(0, "me", int64(c.ME))
	return w
}
