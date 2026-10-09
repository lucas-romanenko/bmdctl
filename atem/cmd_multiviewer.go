// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.14 multiviewer, from bmdwire v1.3.0
// atemwire/messages/multiviewer.py. Both commands were restored from
// upstream pyatem and are unverified.

// MultiviewInput is CMvI (MultiviewInputCommand, op
// set_multiviewer_input): route a source to a multiviewer window.
type MultiviewInput struct {
	Multiviewer uint8
	Window      uint8
	Source      uint16
}

func (c MultiviewInput) Opcode() string    { return "CMvI" }
func (c MultiviewInput) Encode() []byte    { return c.build().encode() }
func (c MultiviewInput) Validate() error   { return c.build().err }
func (c MultiviewInput) EchoKey() string   { return key("MvIn", c.Multiviewer, c.Window) }
func (c MultiviewInput) Unverified() bool  { return true }
func (c MultiviewInput) Destructive() bool { return false }

func (c MultiviewInput) build() *wire {
	w := newWire("CMvI", 4)
	w.u8(0, "multiviewer", int64(c.Multiviewer))
	w.u8(1, "window", int64(c.Window))
	w.u16(2, "source", int64(c.Source))
	return w
}

// MultiviewProperties is CMvP (MultiviewPropertiesCommand; ops
// set_multiviewer_layout, set_multiviewer_swap). Layout is a 4-bit field:
// bit N splits quadrant N into four.
type MultiviewProperties struct {
	Multiviewer uint8
	Layout      *uint8
	Swap        *bool
}

func (c MultiviewProperties) Opcode() string    { return "CMvP" }
func (c MultiviewProperties) Encode() []byte    { return c.build().encode() }
func (c MultiviewProperties) Validate() error   { return c.build().err }
func (c MultiviewProperties) EchoKey() string   { return key("MvPr", c.Multiviewer) }
func (c MultiviewProperties) Unverified() bool  { return true }
func (c MultiviewProperties) Destructive() bool { return false }

func (c MultiviewProperties) build() *wire {
	w := newWire("CMvP", 4)
	var mask int64
	w.u8(1, "multiviewer", int64(c.Multiviewer))
	if c.Layout != nil {
		mask |= 1 << 0
		w.u8(2, "layout", int64(*c.Layout))
	}
	if c.Swap != nil {
		mask |= 1 << 1
		w.flag(3, *c.Swap)
	}
	w.u8(0, "mask", mask)
	return w
}
