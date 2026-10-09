// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "errors"

// SURFACE.md §1.3 transition, from bmdwire v1.3.0
// atemwire/messages/transition.py. Rates and times are frames; see
// ParseRate for atemwire's "seconds:frames" strings. A nil optional field
// leaves that setting unchanged on the switcher.

// Transition styles (TransitionSettings.Style).
const (
	StyleMix uint8 = iota
	StyleDip
	StyleWipe
	StyleDVE
	StyleStinger
)

// Next-transition layers (TransitionSettings.NextTransition), a bitfield.
const (
	LayerBackground uint8 = 1 << iota
	LayerKey1
	LayerKey2
	LayerKey3
	LayerKey4
)

// TransitionSettings is CTTp (TransitionSettingsCommand; ops
// set_transition_style and set_next_transition_layers): the style and the
// layers the next transition takes. A NextTransition of 0 is an error:
// the switcher rejects an empty selection, so atemwire sends nothing.
type TransitionSettings struct {
	ME             uint8
	Style          *uint8 // StyleMix ... StyleStinger
	NextTransition *uint8 // Layer* bits
}

func (c TransitionSettings) Opcode() string    { return "CTTp" }
func (c TransitionSettings) Encode() []byte    { return c.build().encode() }
func (c TransitionSettings) Validate() error   { return c.build().err }
func (c TransitionSettings) EchoKey() string   { return key("TrSS", c.ME) }
func (c TransitionSettings) Unverified() bool  { return false }
func (c TransitionSettings) Destructive() bool { return false }

func (c TransitionSettings) build() *wire {
	w := newWire("CTTp", 4)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	if c.Style != nil {
		mask |= 1 << 0
		w.u8(2, "style", int64(*c.Style))
	}
	if c.NextTransition != nil {
		if *c.NextTransition == 0 {
			w.fail("%w", errEmptyLayers)
		}
		mask |= 1 << 1
		w.u8(3, "next_transition", int64(*c.NextTransition))
	}
	w.u8(0, "mask", mask)
	return w
}

var errEmptyLayers = errors.New("next transition selects no layer; the switcher rejects that, so atemwire sends nothing")

// MixSettings is CTMx (MixSettingsCommand, op set_mix_rate).
type MixSettings struct {
	ME   uint8
	Rate uint8 // frames
}

func (c MixSettings) Opcode() string    { return "CTMx" }
func (c MixSettings) Encode() []byte    { return c.build().encode() }
func (c MixSettings) Validate() error   { return c.build().err }
func (c MixSettings) EchoKey() string   { return key("TMxP", c.ME) }
func (c MixSettings) Unverified() bool  { return false }
func (c MixSettings) Destructive() bool { return false }

func (c MixSettings) build() *wire {
	w := newWire("CTMx", 4)
	w.u8(0, "me", int64(c.ME))
	w.u8(1, "rate", int64(c.Rate))
	return w
}

// DipSettings is CTDp (DipSettingsCommand; ops set_dip_rate,
// set_dip_source).
type DipSettings struct {
	ME     uint8
	Rate   *uint8 // frames
	Source *uint16
}

func (c DipSettings) Opcode() string    { return "CTDp" }
func (c DipSettings) Encode() []byte    { return c.build().encode() }
func (c DipSettings) Validate() error   { return c.build().err }
func (c DipSettings) EchoKey() string   { return key("TDpP", c.ME) }
func (c DipSettings) Unverified() bool  { return false }
func (c DipSettings) Destructive() bool { return false }

func (c DipSettings) build() *wire {
	w := newWire("CTDp", 8)
	var mask int64
	w.u8(1, "me", int64(c.ME))
	if c.Rate != nil {
		mask |= 1 << 0
		w.u8(2, "rate", int64(*c.Rate))
	}
	if c.Source != nil {
		mask |= 1 << 1
		w.u16(4, "source", int64(*c.Source))
	}
	w.u8(0, "mask", mask)
	return w
}

// WipeSettings is CTWp (WipeSettingsCommand; ops set_wipe_*). Width,
// Symmetry and Softness are percent (0-100, ×100 on the wire, clamped);
// PositionX and PositionY are 0-1 (×10000, clamped). Source is the border
// fill. Pattern names are in atemwire's macro tables (0 LeftToRightBar ...
// 17 TopRightDiagonal).
type WipeSettings struct {
	ME        uint8
	Rate      *uint8 // frames
	Pattern   *uint8
	Width     *float64
	Source    *uint16
	Symmetry  *float64
	Softness  *float64
	PositionX *float64
	PositionY *float64
	Reverse   *bool
	FlipFlop  *bool
}

func (c WipeSettings) Opcode() string    { return "CTWp" }
func (c WipeSettings) Encode() []byte    { return c.build().encode() }
func (c WipeSettings) Validate() error   { return c.build().err }
func (c WipeSettings) EchoKey() string   { return key("TWpP", c.ME) }
func (c WipeSettings) Unverified() bool  { return false }
func (c WipeSettings) Destructive() bool { return false }

func (c WipeSettings) build() *wire {
	w := newWire("CTWp", 20)
	var mask int64
	w.u8(2, "me", int64(c.ME))
	if c.Rate != nil {
		mask |= 1 << 0
		w.u8(3, "rate", int64(*c.Rate))
	}
	if c.Pattern != nil {
		mask |= 1 << 1
		w.u8(4, "pattern", int64(*c.Pattern))
	}
	if c.Width != nil {
		mask |= 1 << 2
		w.u16(6, "width", w.percentToThousandths("width", *c.Width))
	}
	if c.Source != nil {
		mask |= 1 << 3
		w.u16(8, "source", int64(*c.Source))
	}
	if c.Symmetry != nil {
		mask |= 1 << 4
		w.u16(10, "symmetry", w.percentToThousandths("symmetry", *c.Symmetry))
	}
	if c.Softness != nil {
		mask |= 1 << 5
		w.u16(12, "softness", w.percentToThousandths("softness", *c.Softness))
	}
	if c.PositionX != nil {
		mask |= 1 << 6
		w.u16(14, "positionx", w.unitToThousandths("positionx", *c.PositionX))
	}
	if c.PositionY != nil {
		mask |= 1 << 7
		w.u16(16, "positiony", w.unitToThousandths("positiony", *c.PositionY))
	}
	if c.Reverse != nil {
		mask |= 1 << 8
		w.flag(18, *c.Reverse)
	}
	if c.FlipFlop != nil {
		mask |= 1 << 9
		w.flag(19, *c.FlipFlop)
	}
	w.u16(0, "mask", mask)
	return w
}

// DVESettings is CTDv (DveSettingsCommand; ops set_dve_*). Byte 3 (mask
// bit 0) is the logo-wipe rate and byte 4 (bit 1) the DVE rate, as atemwire
// corrected them against ATEM Software Control; no op sets LogoRate.
// KeyClip and KeyGain are percent (×10 on the wire, clamped 0-1000). Known
// styles: 16-23 Squeeze*, 24-31 Push*, 34 GraphicLogoWipe.
type DVESettings struct {
	ME               uint8
	LogoRate         *uint8 // frames
	Rate             *uint8 // frames
	Style            *uint8
	FillSource       *uint16
	KeySource        *uint16
	KeyEnable        *bool
	KeyPremultiplied *bool
	KeyClip          *float64
	KeyGain          *float64
	KeyInvert        *bool
	Reverse          *bool
	FlipFlop         *bool
}

func (c DVESettings) Opcode() string    { return "CTDv" }
func (c DVESettings) Encode() []byte    { return c.build().encode() }
func (c DVESettings) Validate() error   { return c.build().err }
func (c DVESettings) EchoKey() string   { return key("TDvP", c.ME) }
func (c DVESettings) Unverified() bool  { return false }
func (c DVESettings) Destructive() bool { return false }

func (c DVESettings) build() *wire {
	w := newWire("CTDv", 20)
	var mask int64
	w.u8(2, "me", int64(c.ME))
	if c.LogoRate != nil {
		mask |= 1 << 0
		w.u8(3, "logo_rate", int64(*c.LogoRate))
	}
	if c.Rate != nil {
		mask |= 1 << 1
		w.u8(4, "rate", int64(*c.Rate))
	}
	if c.Style != nil {
		mask |= 1 << 2
		w.u8(5, "style", int64(*c.Style))
	}
	if c.FillSource != nil {
		mask |= 1 << 3
		w.u16(6, "fill_source", int64(*c.FillSource))
	}
	if c.KeySource != nil {
		mask |= 1 << 4
		w.u16(8, "key_source", int64(*c.KeySource))
	}
	if c.KeyEnable != nil {
		mask |= 1 << 5
		w.flag(10, *c.KeyEnable)
	}
	if c.KeyPremultiplied != nil {
		mask |= 1 << 6
		w.flag(11, *c.KeyPremultiplied)
	}
	if c.KeyClip != nil {
		mask |= 1 << 7
		w.u16(12, "key_clip", w.percentToTenths("key_clip", *c.KeyClip))
	}
	if c.KeyGain != nil {
		mask |= 1 << 8
		w.u16(14, "key_gain", w.percentToTenths("key_gain", *c.KeyGain))
	}
	if c.KeyInvert != nil {
		mask |= 1 << 9
		w.flag(16, *c.KeyInvert)
	}
	if c.Reverse != nil {
		mask |= 1 << 10
		w.flag(17, *c.Reverse)
	}
	if c.FlipFlop != nil {
		mask |= 1 << 11
		w.flag(18, *c.FlipFlop)
	}
	w.u16(0, "mask", mask)
	return w
}

// StingerSettings is CTSt (StingerSettingsCommand; ops set_stinger_*), a
// 20-byte payload. MediaPlayer is the media player slot (1-4), not a source
// id. KeyClip and KeyGain are percent ×10 on the wire and, as in atemwire,
// not clamped. Preroll, Duration (clip duration), TriggerPoint and Rate
// (mix rate) are frames.
type StingerSettings struct {
	ME               uint8
	MediaPlayer      *uint8
	KeyPremultiplied *bool
	KeyClip          *float64
	KeyGain          *float64
	KeyInvert        *bool
	Preroll          *uint16
	Duration         *uint16
	TriggerPoint     *uint16
	Rate             *uint16
}

func (c StingerSettings) Opcode() string    { return "CTSt" }
func (c StingerSettings) Encode() []byte    { return c.build().encode() }
func (c StingerSettings) Validate() error   { return c.build().err }
func (c StingerSettings) EchoKey() string   { return key("TStP", c.ME) }
func (c StingerSettings) Unverified() bool  { return false }
func (c StingerSettings) Destructive() bool { return false }

func (c StingerSettings) build() *wire {
	w := newWire("CTSt", 20)
	var mask int64
	w.u8(2, "me", int64(c.ME))
	if c.MediaPlayer != nil {
		mask |= 1 << 0
		w.u8(3, "mediaplayer", int64(*c.MediaPlayer))
	}
	if c.KeyPremultiplied != nil {
		mask |= 1 << 1
		w.flag(4, *c.KeyPremultiplied)
	}
	if c.KeyClip != nil {
		mask |= 1 << 2
		w.u16(6, "key_clip", w.round("key_clip", *c.KeyClip*10))
	}
	if c.KeyGain != nil {
		mask |= 1 << 3
		w.u16(8, "key_gain", w.round("key_gain", *c.KeyGain*10))
	}
	if c.KeyInvert != nil {
		mask |= 1 << 4
		w.flag(10, *c.KeyInvert)
	}
	if c.Preroll != nil {
		mask |= 1 << 5
		w.u16(12, "preroll", int64(*c.Preroll))
	}
	if c.Duration != nil {
		mask |= 1 << 6
		w.u16(14, "duration", int64(*c.Duration))
	}
	if c.TriggerPoint != nil {
		mask |= 1 << 7
		w.u16(16, "triggerpoint", int64(*c.TriggerPoint))
	}
	if c.Rate != nil {
		mask |= 1 << 8
		w.u16(18, "rate", int64(*c.Rate))
	}
	w.u16(0, "mask", mask)
	return w
}

// TransitionPreview is CTPr (TransitionPreviewCommand, op
// set_transition_preview). Restored from upstream pyatem; unverified.
type TransitionPreview struct {
	ME      uint8
	Enabled bool
}

func (c TransitionPreview) Opcode() string    { return "CTPr" }
func (c TransitionPreview) Encode() []byte    { return c.build().encode() }
func (c TransitionPreview) Validate() error   { return c.build().err }
func (c TransitionPreview) EchoKey() string   { return key("TrPr", c.ME) }
func (c TransitionPreview) Unverified() bool  { return true }
func (c TransitionPreview) Destructive() bool { return false }

func (c TransitionPreview) build() *wire {
	w := newWire("CTPr", 4)
	w.u8(0, "me", int64(c.ME))
	w.flag(1, c.Enabled)
	return w
}

// TransitionPosition is CTPs (TransitionPositionCommand, op
// set_transition_position): the T-bar, 0-10000, clamped. Restored from
// upstream pyatem; unverified.
type TransitionPosition struct {
	ME       uint8
	Position int
}

func (c TransitionPosition) Opcode() string    { return "CTPs" }
func (c TransitionPosition) Encode() []byte    { return c.build().encode() }
func (c TransitionPosition) Validate() error   { return c.build().err }
func (c TransitionPosition) EchoKey() string   { return key("TrPs", c.ME) }
func (c TransitionPosition) Unverified() bool  { return true }
func (c TransitionPosition) Destructive() bool { return false }

func (c TransitionPosition) build() *wire {
	w := newWire("CTPs", 4)
	w.u8(0, "me", int64(c.ME))
	w.u16(2, "position", int64(max(0, min(10000, c.Position))))
	return w
}
