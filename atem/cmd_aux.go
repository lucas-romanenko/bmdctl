// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.2 aux, from bmdwire v1.3.0 atemwire/messages/switching.py.

// AuxSource is CAuS (AuxSourceCommand, op set_aux_output): route a source
// to an aux output. Aux is 0-based on the wire and in State.Aux; atemwire's
// snapshot names them aux1 to auxN.
type AuxSource struct {
	Aux    uint8
	Source uint16
}

func (c AuxSource) Opcode() string    { return "CAuS" }
func (c AuxSource) Encode() []byte    { return c.build().encode() }
func (c AuxSource) Validate() error   { return c.build().err }
func (c AuxSource) EchoKey() string   { return key("AuxS", c.Aux) }
func (c AuxSource) Unverified() bool  { return false }
func (c AuxSource) Destructive() bool { return false }

func (c AuxSource) build() *wire {
	w := newWire("CAuS", 4)
	w.u8(0, "mask", 1) // always 1: "set source"
	w.u8(1, "aux", int64(c.Aux))
	w.u16(2, "source", int64(c.Source))
	return w
}
