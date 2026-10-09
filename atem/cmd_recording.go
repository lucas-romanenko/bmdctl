// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.16 recording, from bmdwire v1.3.0
// atemwire/messages/recording.py. Both commands were restored from upstream
// pyatem and are unverified.

// RecordingSettingsSet is CRMS (RecordingSettingsSetCommand, op
// set_recording_settings). Filename is cut to 128 bytes.
type RecordingSettingsSet struct {
	Filename       *string
	Disk1          *uint32
	Disk2          *uint32
	RecordInCamera *bool
}

func (c RecordingSettingsSet) Opcode() string    { return "CRMS" }
func (c RecordingSettingsSet) Encode() []byte    { return c.build().encode() }
func (c RecordingSettingsSet) Validate() error   { return c.build().err }
func (c RecordingSettingsSet) EchoKey() string   { return "RMSu" }
func (c RecordingSettingsSet) Unverified() bool  { return true }
func (c RecordingSettingsSet) Destructive() bool { return false }

func (c RecordingSettingsSet) build() *wire {
	w := newWire("CRMS", 144)
	var mask int64
	if c.Filename != nil {
		mask |= 1 << 0
		w.str(1, 128, *c.Filename)
	}
	if c.Disk1 != nil {
		mask |= 1 << 1
		w.u32(132, "disk1", int64(*c.Disk1))
	}
	if c.Disk2 != nil {
		mask |= 1 << 2
		w.u32(136, "disk2", int64(*c.Disk2))
	}
	if c.RecordInCamera != nil {
		mask |= 1 << 3
		w.flag(140, *c.RecordInCamera)
	}
	w.u8(0, "mask", mask)
	return w
}

// RecorderStatus is RcTM (RecorderStatusCommand; ops start_recording,
// stop_recording).
type RecorderStatus struct {
	Recording bool
}

func (c RecorderStatus) Opcode() string    { return "RcTM" }
func (c RecorderStatus) Encode() []byte    { return c.build().encode() }
func (c RecorderStatus) Validate() error   { return c.build().err }
func (c RecorderStatus) EchoKey() string   { return "RTMS" }
func (c RecorderStatus) Unverified() bool  { return true }
func (c RecorderStatus) Destructive() bool { return false }

func (c RecorderStatus) build() *wire {
	w := newWire("RcTM", 4)
	w.flag(0, c.Recording)
	return w
}
