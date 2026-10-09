// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.5 keyer-dsk, from bmdwire v1.3.0
// atemwire/messages/downstream_keyer.py. DSK is the 0-based downstream
// keyer.

// DkeyOnair is CDsL (DkeyOnairCommand, op set_dsk_on_air).
type DkeyOnair struct {
	DSK   uint8
	OnAir bool
}

func (c DkeyOnair) Opcode() string    { return "CDsL" }
func (c DkeyOnair) Encode() []byte    { return c.build().encode() }
func (c DkeyOnair) Validate() error   { return c.build().err }
func (c DkeyOnair) EchoKey() string   { return key("DskS", c.DSK) }
func (c DkeyOnair) Unverified() bool  { return false }
func (c DkeyOnair) Destructive() bool { return false }

func (c DkeyOnair) build() *wire {
	w := newWire("CDsL", 4)
	w.u8(0, "dsk", int64(c.DSK))
	w.flag(1, c.OnAir)
	return w
}

// DkeyAuto is DDsA (DkeyAutoCommand, op dsk_auto): run the DSK's auto
// transition. No echo of its own.
type DkeyAuto struct {
	DSK uint8
}

func (c DkeyAuto) Opcode() string    { return "DDsA" }
func (c DkeyAuto) Encode() []byte    { return c.build().encode() }
func (c DkeyAuto) Validate() error   { return c.build().err }
func (c DkeyAuto) EchoKey() string   { return "" }
func (c DkeyAuto) Unverified() bool  { return false }
func (c DkeyAuto) Destructive() bool { return false }

func (c DkeyAuto) build() *wire {
	w := newWire("DDsA", 4)
	w.u8(0, "dsk", int64(c.DSK))
	return w
}

// DkeyTie is CDsT (DkeyTieCommand, op set_dsk_tie).
type DkeyTie struct {
	DSK uint8
	Tie bool
}

func (c DkeyTie) Opcode() string    { return "CDsT" }
func (c DkeyTie) Encode() []byte    { return c.build().encode() }
func (c DkeyTie) Validate() error   { return c.build().err }
func (c DkeyTie) EchoKey() string   { return key("DskP", c.DSK) }
func (c DkeyTie) Unverified() bool  { return false }
func (c DkeyTie) Destructive() bool { return false }

func (c DkeyTie) build() *wire {
	w := newWire("CDsT", 4)
	w.u8(0, "dsk", int64(c.DSK))
	w.flag(1, c.Tie)
	return w
}

// DkeyRate is CDsR (DkeyRateCommand, op set_dsk_rate).
type DkeyRate struct {
	DSK  uint8
	Rate uint8 // frames
}

func (c DkeyRate) Opcode() string    { return "CDsR" }
func (c DkeyRate) Encode() []byte    { return c.build().encode() }
func (c DkeyRate) Validate() error   { return c.build().err }
func (c DkeyRate) EchoKey() string   { return key("DskP", c.DSK) }
func (c DkeyRate) Unverified() bool  { return false }
func (c DkeyRate) Destructive() bool { return false }

func (c DkeyRate) build() *wire {
	w := newWire("CDsR", 4)
	w.u8(0, "dsk", int64(c.DSK))
	w.u8(1, "rate", int64(c.Rate))
	return w
}

// DkeySetFill is CDsF (DkeySetFillCommand, op set_dsk_fill_source).
type DkeySetFill struct {
	DSK    uint8
	Source uint16
}

func (c DkeySetFill) Opcode() string    { return "CDsF" }
func (c DkeySetFill) Encode() []byte    { return c.build().encode() }
func (c DkeySetFill) Validate() error   { return c.build().err }
func (c DkeySetFill) EchoKey() string   { return key("DskB", c.DSK) }
func (c DkeySetFill) Unverified() bool  { return false }
func (c DkeySetFill) Destructive() bool { return false }

func (c DkeySetFill) build() *wire {
	w := newWire("CDsF", 4)
	w.u8(0, "dsk", int64(c.DSK))
	w.u16(2, "source", int64(c.Source))
	return w
}

// DkeySetKey is CDsC (DkeySetKeyCommand, op set_dsk_key_source).
type DkeySetKey struct {
	DSK    uint8
	Source uint16
}

func (c DkeySetKey) Opcode() string    { return "CDsC" }
func (c DkeySetKey) Encode() []byte    { return c.build().encode() }
func (c DkeySetKey) Validate() error   { return c.build().err }
func (c DkeySetKey) EchoKey() string   { return key("DskB", c.DSK) }
func (c DkeySetKey) Unverified() bool  { return false }
func (c DkeySetKey) Destructive() bool { return false }

func (c DkeySetKey) build() *wire {
	w := newWire("CDsC", 4)
	w.u8(0, "dsk", int64(c.DSK))
	w.u16(2, "source", int64(c.Source))
	return w
}

// DkeyGain is CDsG (DkeyGainCommand; ops set_dsk_pre_multiplied,
// set_dsk_clip, set_dsk_gain, set_dsk_invert_key, configure_dsk_gain).
// Clip and Gain are percent (×10, clamped 0-1000).
type DkeyGain struct {
	DSK           uint8
	Premultiplied *bool
	Clip          *float64
	Gain          *float64
	Invert        *bool
}

func (c DkeyGain) Opcode() string    { return "CDsG" }
func (c DkeyGain) Encode() []byte    { return c.build().encode() }
func (c DkeyGain) Validate() error   { return c.build().err }
func (c DkeyGain) EchoKey() string   { return key("DskP", c.DSK) }
func (c DkeyGain) Unverified() bool  { return false }
func (c DkeyGain) Destructive() bool { return false }

func (c DkeyGain) build() *wire {
	w := newWire("CDsG", 12)
	var mask int64
	w.u8(1, "dsk", int64(c.DSK))
	if c.Premultiplied != nil {
		mask |= 1 << 0
		w.flag(2, *c.Premultiplied)
	}
	if c.Clip != nil {
		mask |= 1 << 1
		w.u16(4, "clip", w.percentToTenths("clip", *c.Clip))
	}
	if c.Gain != nil {
		mask |= 1 << 2
		w.u16(6, "gain", w.percentToTenths("gain", *c.Gain))
	}
	if c.Invert != nil {
		mask |= 1 << 3
		w.flag(8, *c.Invert)
	}
	w.u8(0, "mask", mask)
	return w
}

// DkeyMask is CDsM (DkeyMaskCommand; ops set_dsk_mask_*). Edges are
// display units ×1000.
type DkeyMask struct {
	DSK     uint8
	Enabled *bool
	Top     *float64
	Bottom  *float64
	Left    *float64
	Right   *float64
}

func (c DkeyMask) Opcode() string    { return "CDsM" }
func (c DkeyMask) Encode() []byte    { return c.build().encode() }
func (c DkeyMask) Validate() error   { return c.build().err }
func (c DkeyMask) EchoKey() string   { return key("DskP", c.DSK) }
func (c DkeyMask) Unverified() bool  { return false }
func (c DkeyMask) Destructive() bool { return false }

func (c DkeyMask) build() *wire {
	w := newWire("CDsM", 12)
	var mask int64
	w.u8(1, "dsk", int64(c.DSK))
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(2, *c.Enabled)
	}
	for i, e := range []struct {
		v    *float64
		name string
	}{{c.Top, "top"}, {c.Bottom, "bottom"}, {c.Left, "left"}, {c.Right, "right"}} {
		if e.v != nil {
			mask |= 1 << (i + 1)
			w.i16(4+2*i, e.name, w.round(e.name, *e.v*1000))
		}
	}
	w.u8(0, "mask", mask)
	return w
}
