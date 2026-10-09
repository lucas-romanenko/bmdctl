// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.15 supersource, from bmdwire v1.3.0
// atemwire/messages/supersource.py. Both commands were restored from
// upstream pyatem and are unverified; their operations take wire units.

// SupersourceBoxProperties is CSBP (SupersourceBoxPropertiesCommand, op
// set_supersource_box). X -4800-4800, Y -3400-3400, Size 70-1000, Top and
// Bottom 0-18000, Left and Right 0-32000.
type SupersourceBoxProperties struct {
	SuperSource uint8
	Box         uint8
	Enabled     *bool
	Source      *uint16
	X           *int16
	Y           *int16
	Size        *uint16
	Masked      *bool
	Top         *uint16
	Bottom      *uint16
	Left        *uint16
	Right       *uint16
}

func (c SupersourceBoxProperties) Opcode() string    { return "CSBP" }
func (c SupersourceBoxProperties) Encode() []byte    { return c.build().encode() }
func (c SupersourceBoxProperties) Validate() error   { return c.build().err }
func (c SupersourceBoxProperties) EchoKey() string   { return key("SSBP", c.SuperSource, c.Box) }
func (c SupersourceBoxProperties) Unverified() bool  { return true }
func (c SupersourceBoxProperties) Destructive() bool { return false }

func (c SupersourceBoxProperties) build() *wire {
	w := newWire("CSBP", 24)
	var mask int64
	w.u8(2, "supersource", int64(c.SuperSource))
	w.u8(3, "box", int64(c.Box))
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(4, *c.Enabled)
	}
	if c.Source != nil {
		mask |= 1 << 1
		w.u16(6, "source", int64(*c.Source))
	}
	if c.X != nil {
		mask |= 1 << 2
		w.i16(8, "x", int64(*c.X))
	}
	if c.Y != nil {
		mask |= 1 << 3
		w.i16(10, "y", int64(*c.Y))
	}
	if c.Size != nil {
		mask |= 1 << 4
		w.u16(12, "size", int64(*c.Size))
	}
	if c.Masked != nil {
		mask |= 1 << 5
		w.flag(14, *c.Masked)
	}
	for i, e := range []struct {
		v    *uint16
		name string
	}{{c.Top, "top"}, {c.Bottom, "bottom"}, {c.Left, "left"}, {c.Right, "right"}} {
		if e.v != nil {
			mask |= 1 << (6 + i)
			w.u16(16+2*i, e.name, int64(*e.v))
		}
	}
	w.u16(0, "mask", mask)
	return w
}

// SupersourceProperties is CSSc (SupersourcePropertiesCommand, op
// set_supersource_properties). Layer 0 background, 1 foreground; Clip and
// Gain 0-1000.
type SupersourceProperties struct {
	SuperSource   uint8
	FillSource    *uint16
	KeySource     *uint16
	Layer         *uint8
	Premultiplied *bool
	Clip          *uint16
	Gain          *uint16
	Invert        *bool
}

func (c SupersourceProperties) Opcode() string    { return "CSSc" }
func (c SupersourceProperties) Encode() []byte    { return c.build().encode() }
func (c SupersourceProperties) Validate() error   { return c.build().err }
func (c SupersourceProperties) EchoKey() string   { return key("SSrc", c.SuperSource) }
func (c SupersourceProperties) Unverified() bool  { return true }
func (c SupersourceProperties) Destructive() bool { return false }

func (c SupersourceProperties) build() *wire {
	w := newWire("CSSc", 16)
	var mask int64
	w.u8(1, "supersource", int64(c.SuperSource))
	if c.FillSource != nil {
		mask |= 1 << 0
		w.u16(2, "fill_source", int64(*c.FillSource))
	}
	if c.KeySource != nil {
		mask |= 1 << 1
		w.u16(4, "key_source", int64(*c.KeySource))
	}
	if c.Layer != nil {
		mask |= 1 << 2
		w.u8(6, "layer", int64(*c.Layer))
	}
	if c.Premultiplied != nil {
		mask |= 1 << 3
		w.flag(7, *c.Premultiplied)
	}
	if c.Clip != nil {
		mask |= 1 << 4
		w.u16(8, "clip", int64(*c.Clip))
	}
	if c.Gain != nil {
		mask |= 1 << 5
		w.u16(10, "gain", int64(*c.Gain))
	}
	if c.Invert != nil {
		mask |= 1 << 6
		w.flag(12, *c.Invert)
	}
	w.u8(0, "mask", mask)
	return w
}
