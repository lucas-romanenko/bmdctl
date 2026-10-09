// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.9 macro, from bmdwire v1.3.0 atemwire/messages/macros.py.

// Macro actions (MacroAction.Action).
const (
	MacroRun            uint8 = 0
	MacroStop           uint8 = 1
	MacroStopRecord     uint8 = 2
	MacroInsertUserWait uint8 = 3
	MacroContinue       uint8 = 4
	MacroDelete         uint8 = 5
)

// MacroNoSlot is the slot MacroStop and MacroStopRecord address: whatever
// is running or recording.
const MacroNoSlot uint16 = 0xFFFF

// MacroAction is MAct (MacroActionCommand; ops macro_run, execute_macro,
// macro_stop, macro_stop_recording). Slot is 0-based (atemwire's
// execute_macro takes it 1-based and subtracts one). No operation sends
// MacroInsertUserWait, MacroContinue or MacroDelete; the command can.
type MacroAction struct {
	Slot   uint16
	Action uint8
}

func (c MacroAction) Opcode() string  { return "MAct" }
func (c MacroAction) Encode() []byte  { return c.build().encode() }
func (c MacroAction) Validate() error { return c.build().err }

// EchoKey: run, stop and continue show up in macro-play-status, stopping a
// recording in macro-record-status, a delete in the slot's properties.
func (c MacroAction) EchoKey() string {
	switch c.Action {
	case MacroRun, MacroStop, MacroContinue:
		return "MRPr"
	case MacroStopRecord:
		return "MRcS"
	case MacroDelete:
		return key("MPrp", c.Slot)
	}
	return ""
}
func (c MacroAction) Unverified() bool  { return false }
func (c MacroAction) Destructive() bool { return false }

func (c MacroAction) build() *wire {
	w := newWire("MAct", 4)
	w.u16(0, "slot", int64(c.Slot))
	w.u8(2, "action", int64(c.Action))
	return w
}

// MacroRecord is MSRc (MacroRecordCommand, op macro_start_recording):
// start recording a macro into Slot (0-based). Variable length, padded to
// four bytes.
type MacroRecord struct {
	Slot        uint16
	Name        string
	Description string
}

func (c MacroRecord) Opcode() string    { return "MSRc" }
func (c MacroRecord) Encode() []byte    { return c.build().encode() }
func (c MacroRecord) Validate() error   { return c.build().err }
func (c MacroRecord) EchoKey() string   { return "MRcS" }
func (c MacroRecord) Unverified() bool  { return false }
func (c MacroRecord) Destructive() bool { return false }

func (c MacroRecord) build() *wire {
	n := 6 + len(c.Name) + len(c.Description)
	w := newWire("MSRc", (n+3)/4*4)
	w.u16(0, "slot", int64(c.Slot))
	w.u16(2, "name length", int64(len(c.Name)))
	w.u16(4, "description length", int64(len(c.Description)))
	copy(w.b[6:], c.Name)
	copy(w.b[6+len(c.Name):], c.Description)
	return w
}

// MacroSleep is MSlp (MacroSleepCommand, op macro_sleep): record a wait of
// Frames into the macro being recorded. No state of its own.
type MacroSleep struct {
	Frames uint32
}

func (c MacroSleep) Opcode() string    { return "MSlp" }
func (c MacroSleep) Encode() []byte    { return c.build().encode() }
func (c MacroSleep) Validate() error   { return c.build().err }
func (c MacroSleep) EchoKey() string   { return "" }
func (c MacroSleep) Unverified() bool  { return false }
func (c MacroSleep) Destructive() bool { return false }

func (c MacroSleep) build() *wire {
	w := newWire("MSlp", 4)
	w.u32(0, "frames", int64(c.Frames))
	return w
}
