// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.6 fade-to-black, from bmdwire v1.3.0
// atemwire/messages/fade_to_black.py.

// FadeToBlack is FtbA (FadeToBlackCommand, op fade_to_black): toggle fade
// to black on an M/E. No echo of its own.
type FadeToBlack struct {
	ME uint8
}

func (c FadeToBlack) Opcode() string    { return "FtbA" }
func (c FadeToBlack) Encode() []byte    { return c.build().encode() }
func (c FadeToBlack) Validate() error   { return c.build().err }
func (c FadeToBlack) EchoKey() string   { return "" }
func (c FadeToBlack) Unverified() bool  { return false }
func (c FadeToBlack) Destructive() bool { return false }

func (c FadeToBlack) build() *wire {
	w := newWire("FtbA", 4)
	w.u8(0, "me", int64(c.ME))
	return w
}

// FadeToBlackConfig is FtbC (FadeToBlackConfigCommand, op set_ftb_rate).
type FadeToBlackConfig struct {
	ME     uint8
	Frames uint8
}

func (c FadeToBlackConfig) Opcode() string    { return "FtbC" }
func (c FadeToBlackConfig) Encode() []byte    { return c.build().encode() }
func (c FadeToBlackConfig) Validate() error   { return c.build().err }
func (c FadeToBlackConfig) EchoKey() string   { return key("FtbP", c.ME) }
func (c FadeToBlackConfig) Unverified() bool  { return false }
func (c FadeToBlackConfig) Destructive() bool { return false }

func (c FadeToBlackConfig) build() *wire {
	w := newWire("FtbC", 4)
	w.u8(0, "mask", 1)
	w.u8(1, "me", int64(c.ME))
	w.u8(2, "frames", int64(c.Frames))
	return w
}

// FadeToBlackEnable is FEna (FadeToBlackEnableCommand, op
// set_ftb_disabled): whether fade to black is available. It has no M/E: the
// packet that would disable M/E 2 is byte for byte the one that enables
// M/E 1, so atemwire's operation always targets M/E 1 (SURFACE.md §5.4
// item 5), and so does this.
type FadeToBlackEnable struct {
	Enabled bool
}

func (c FadeToBlackEnable) Opcode() string    { return "FEna" }
func (c FadeToBlackEnable) Encode() []byte    { return c.build().encode() }
func (c FadeToBlackEnable) Validate() error   { return c.build().err }
func (c FadeToBlackEnable) EchoKey() string   { return "FEna" }
func (c FadeToBlackEnable) Unverified() bool  { return false }
func (c FadeToBlackEnable) Destructive() bool { return false }

func (c FadeToBlackEnable) build() *wire {
	w := newWire("FEna", 4)
	if c.Enabled {
		w.b[1] = 0x01
	}
	// Disabled: bytes 0x00, M/E index (pinned to 0), 0x00, pad.
	return w
}
