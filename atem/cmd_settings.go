// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "time"

// SURFACE.md §1.18 settings (inputs, video mode, system), from bmdwire
// v1.3.0 atemwire/messages/input_video.py and system_info.py.

// InputProperties is CInL (InputPropertiesCommand, op set_input_label):
// rename an input. Any field left nil is unchanged; an empty string is a
// valid label. LongName is cut to 20 bytes and ShortName to 4, byte-wise.
// PortType writes the external port type (no operation sets it). Fixed
// internal sources (black, bars, key masks, program, preview) can't be
// renamed.
type InputProperties struct {
	Source    uint16
	LongName  *string
	ShortName *string
	PortType  *uint16
}

func (c InputProperties) Opcode() string    { return "CInL" }
func (c InputProperties) Encode() []byte    { return c.build().encode() }
func (c InputProperties) Validate() error   { return c.build().err }
func (c InputProperties) EchoKey() string   { return key("InPr", c.Source) }
func (c InputProperties) Unverified() bool  { return false }
func (c InputProperties) Destructive() bool { return false }

func (c InputProperties) build() *wire {
	w := newWire("CInL", 32)
	var mask int64
	w.u16(2, "source", int64(c.Source))
	if c.LongName != nil {
		mask |= 1 << 0
		w.str(4, 20, *c.LongName)
	}
	if c.ShortName != nil {
		mask |= 1 << 1
		w.str(24, 4, *c.ShortName)
	}
	if c.PortType != nil {
		mask |= 1 << 2
		w.u16(28, "port_type", int64(*c.PortType))
	}
	w.u8(0, "mask", mask)
	return w
}

// VideoModeNames is atemwire's VIDEO_MODE_NAMES: the names set_video_mode
// accepts, case-sensitive.
var VideoModeNames = map[string]uint8{
	"NTSC": 0, "525i5994": 0, "PAL": 1, "625i50": 1,
	"NTSC_widescreen": 2, "PAL_widescreen": 3,
	"720p50": 4, "720p5994": 5, "720p60": 28,
	"1080i50": 6, "1080i5994": 7, "1080i60": 29,
	"1080p2398": 8, "1080p24": 9, "1080p25": 10, "1080p2997": 11, "1080p30": 26,
	"1080p50": 12, "1080p5994": 13, "1080p60": 27,
	"2160p2398": 14, "2160p24": 15, "2160p25": 16, "2160p2997": 17, "2160p50": 18, "2160p5994": 19,
	"4320p2398": 20, "4320p24": 21, "4320p25": 22, "4320p2997": 23, "4320p50": 24, "4320p5994": 25,
}

// VideoMode is CVdM (VideoModeCommand, op set_video_mode): change the
// switcher's video mode (0-29; see VideoModeNames). Destructive: the
// outputs resync and clients may be dropped.
type VideoMode struct {
	Mode uint8
}

func (c VideoMode) Opcode() string    { return "CVdM" }
func (c VideoMode) Encode() []byte    { return c.build().encode() }
func (c VideoMode) Validate() error   { return c.build().err }
func (c VideoMode) EchoKey() string   { return "VidM" }
func (c VideoMode) Unverified() bool  { return false }
func (c VideoMode) Destructive() bool { return true }

func (c VideoMode) build() *wire {
	w := newWire("CVdM", 4)
	w.u8(0, "mode", int64(c.Mode))
	return w
}

// AutoInputVideoMode is AiVM (AutoInputVideoModeCommand, op
// set_auto_video_mode). Restored from upstream pyatem; unverified.
type AutoInputVideoMode struct {
	Enable bool
}

func (c AutoInputVideoMode) Opcode() string    { return "AiVM" }
func (c AutoInputVideoMode) Encode() []byte    { return c.build().encode() }
func (c AutoInputVideoMode) Validate() error   { return c.build().err }
func (c AutoInputVideoMode) EchoKey() string   { return "AiVM" }
func (c AutoInputVideoMode) Unverified() bool  { return true }
func (c AutoInputVideoMode) Destructive() bool { return false }

func (c AutoInputVideoMode) build() *wire {
	w := newWire("AiVM", 4)
	w.flag(0, c.Enable)
	return w
}

// SaveStartupState is SRsv (SaveStartupStateCommand, op
// save_startup_state): make the current state the power-on state.
// Destructive; restored from upstream pyatem, unverified. No echo.
type SaveStartupState struct{}

func (c SaveStartupState) Opcode() string    { return "SRsv" }
func (c SaveStartupState) Encode() []byte    { return newWire("SRsv", 4).encode() }
func (c SaveStartupState) EchoKey() string   { return "" }
func (c SaveStartupState) Unverified() bool  { return true }
func (c SaveStartupState) Destructive() bool { return true }

// ClearStartupState is SRcl (ClearStartupStateCommand, op
// clear_startup_state). Destructive; restored from upstream pyatem,
// unverified. No echo.
type ClearStartupState struct{}

func (c ClearStartupState) Opcode() string    { return "SRcl" }
func (c ClearStartupState) Encode() []byte    { return newWire("SRcl", 4).encode() }
func (c ClearStartupState) EchoKey() string   { return "" }
func (c ClearStartupState) Unverified() bool  { return true }
func (c ClearStartupState) Destructive() bool { return true }

// SetTimeOfDay is SToD (SetTimeOfDayCommand, op set_time_of_day): set the
// switcher's clock to Time, as whole Unix seconds. A zero Time means now,
// as the operation's default does. Restored from upstream pyatem;
// unverified. No echo of its own.
type SetTimeOfDay struct {
	Time time.Time
}

func (c SetTimeOfDay) Opcode() string    { return "SToD" }
func (c SetTimeOfDay) Encode() []byte    { return c.build().encode() }
func (c SetTimeOfDay) Validate() error   { return c.build().err }
func (c SetTimeOfDay) EchoKey() string   { return "" }
func (c SetTimeOfDay) Unverified() bool  { return true }
func (c SetTimeOfDay) Destructive() bool { return false }

func (c SetTimeOfDay) build() *wire {
	t := c.Time
	if t.IsZero() {
		t = time.Now()
	}
	w := newWire("SToD", 8)
	w.u32(0, "timestamp", t.Unix())
	return w
}

// TimeRequest is TiRq (TimeRequestCommand): ask for the time. The session
// sends one after every state dump. The reply is Time, not an echo.
type TimeRequest struct{}

func (c TimeRequest) Opcode() string    { return "TiRq" }
func (c TimeRequest) Encode() []byte    { return newWire("TiRq", 0).encode() }
func (c TimeRequest) EchoKey() string   { return "" }
func (c TimeRequest) Unverified() bool  { return false }
func (c TimeRequest) Destructive() bool { return false }
