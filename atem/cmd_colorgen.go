// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.7 color-gen, from bmdwire v1.3.0
// atemwire/messages/color_generator.py.

// ColorGenerator is CClV (ColorGeneratorCommand; ops
// set_color_generator_hue, set_color_generator_saturation,
// set_color_generator_luma). Generator is 0 or 1. Hue is degrees (×10 on
// the wire, not clamped); Saturation and Luma are percent, clamped to
// 0-100, then ×10.
type ColorGenerator struct {
	Generator  uint8
	Hue        *float64
	Saturation *float64
	Luma       *float64
}

func (c ColorGenerator) Opcode() string    { return "CClV" }
func (c ColorGenerator) Encode() []byte    { return c.build().encode() }
func (c ColorGenerator) Validate() error   { return c.build().err }
func (c ColorGenerator) EchoKey() string   { return key("ColV", c.Generator) }
func (c ColorGenerator) Unverified() bool  { return false }
func (c ColorGenerator) Destructive() bool { return false }

func (c ColorGenerator) build() *wire {
	w := newWire("CClV", 8)
	var mask int64
	w.u8(1, "generator", int64(c.Generator))
	if c.Hue != nil {
		mask |= 1 << 0
		w.u16(2, "hue", w.round("hue", *c.Hue*10))
	}
	// atemwire's _percent_to_unit (max(0, min(1, v / 100)), where a NaN
	// becomes 1), then the declarative field's ×1000.
	unit := func(v float64) float64 { return pyMax(0.0, pyMin(1.0, v/100.0)) }
	if c.Saturation != nil {
		mask |= 1 << 1
		w.u16(4, "saturation", w.round("saturation", unit(*c.Saturation)*1000))
	}
	if c.Luma != nil {
		mask |= 1 << 2
		w.u16(6, "luma", w.round("luma", unit(*c.Luma)*1000))
	}
	w.u8(0, "mask", mask)
	return w
}
