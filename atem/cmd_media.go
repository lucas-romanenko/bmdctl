// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.8 media, from bmdwire v1.3.0 atemwire/messages/media.py.

// CaptureStill is Capt (CaptureStillCommand, op capture_still): capture
// program into the next free still slot, which the switcher picks. No echo
// of its own; the new slot shows up as MPfe.
type CaptureStill struct{}

func (c CaptureStill) Opcode() string    { return "Capt" }
func (c CaptureStill) Encode() []byte    { return newWire("Capt", 0).encode() }
func (c CaptureStill) EchoKey() string   { return "" }
func (c CaptureStill) Unverified() bool  { return false }
func (c CaptureStill) Destructive() bool { return false }

// ClearStill is CSTL (ClearStillCommand, op clear_still): the bare clear
// of a still slot (0-based). Some models ignore it unless the session holds
// the still-store lock; atemwire's locked clear is a transfer, a later
// phase. Destructive.
type ClearStill struct {
	Slot uint8
}

func (c ClearStill) Opcode() string    { return "CSTL" }
func (c ClearStill) Encode() []byte    { return c.build().encode() }
func (c ClearStill) Validate() error   { return c.build().err }
func (c ClearStill) EchoKey() string   { return key("MPfe", c.Slot) }
func (c ClearStill) Unverified() bool  { return false }
func (c ClearStill) Destructive() bool { return true }

func (c ClearStill) build() *wire {
	w := newWire("CSTL", 4)
	w.u8(0, "slot", int64(c.Slot))
	return w
}

// MediaplayerSelect is MPSS (MediaplayerSelectCommand; ops
// set_media_player_still, set_media_player_clip): point a media player at
// a still or a clip slot. Exactly one of Still and Clip must be set.
type MediaplayerSelect struct {
	Player uint8
	Still  *uint8
	Clip   *uint8
}

func (c MediaplayerSelect) Opcode() string    { return "MPSS" }
func (c MediaplayerSelect) Encode() []byte    { return c.build().encode() }
func (c MediaplayerSelect) Validate() error   { return c.build().err }
func (c MediaplayerSelect) EchoKey() string   { return key("MPCE", c.Player) }
func (c MediaplayerSelect) Unverified() bool  { return false }
func (c MediaplayerSelect) Destructive() bool { return false }

func (c MediaplayerSelect) build() *wire {
	w := newWire("MPSS", 8)
	switch {
	case c.Still != nil && c.Clip != nil:
		w.fail("can only set still or clip, not both")
	case c.Still == nil && c.Clip == nil:
		w.fail("either still or clip is required")
	}
	mask, sourceType := int64(1), int64(2)
	var still, clip int64
	if c.Still != nil {
		mask, sourceType, still = mask|1<<1, 1, int64(*c.Still)
	} else if c.Clip != nil {
		mask, clip = mask|1<<2, int64(*c.Clip)
	}
	w.u8(0, "mask", mask)
	w.u8(1, "player", int64(c.Player))
	w.u8(2, "source_type", sourceType)
	w.u8(3, "still", still)
	w.u8(4, "clip", clip)
	return w
}
