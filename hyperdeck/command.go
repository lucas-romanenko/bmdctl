// SPDX-License-Identifier: MIT

package hyperdeck

// The commands: one type per line bmdwire v1.8.0's hyperdeckwire sends
// (client.py, the Hyperdeck methods), written as it writes them. Record,
// GotoClipBy, GotoTimecode, Notify and Play's loop and single clip turned
// off came to hyperdeckwire in 1.8.0 (bmdwire#60), from lines bmdctl had
// sent on its own.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// Command is one line to the deck.
type Command interface {
	// Line is the line as sent, without its newline.
	Line() string
}

// Validator is a command that can refuse its own fields: one whose line
// would be a different command, or none.
type Validator interface {
	Validate() error
}

// Setting is one "name: value" pair of a configuration or notify line.
// A name in snake_case is written with spaces, as set_configuration's
// keyword arguments are; true and false are written so, anything else
// as fmt prints it.
type Setting struct {
	Name  string
	Value any
}

func (s Setting) String() string {
	v := fmt.Sprint(s.Value)
	if b, ok := s.Value.(bool); ok {
		v = map[bool]string{true: "true", false: "false"}[b]
	}
	return strings.ReplaceAll(s.Name, "_", " ") + ": " + v
}

func settings(ss []Setting) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = s.String()
	}
	return strings.Join(parts, " ")
}

// checkLine refuses a line holding a line break, which the deck would
// read as a second command: hyperdeckwire sends it as given.
func checkLine(c Command) error {
	if strings.ContainsAny(c.Line(), "\r\n") {
		return fmt.Errorf("hyperdeck: %q: a value can't hold a line break", c.Line())
	}
	return nil
}

func optID(prefix string, id *int) string {
	if id == nil {
		return prefix
	}
	return fmt.Sprintf("%s: slot id: %d", prefix, *id)
}

// DeviceInfo asks for the model, protocol version, unique id, slot count
// and software version: device_info.
type DeviceInfo struct{}

func (DeviceInfo) Line() string { return "device info" }

// RemoteInfo asks whether remote control is enabled: remote_info.
type RemoteInfo struct{}

func (RemoteInfo) Line() string { return "remote" }

// SlotInfo asks for a slot's status, volume, recording time left and
// video format: the active slot's, or SlotID's. slot_info.
type SlotInfo struct{ SlotID *int }

func (c SlotInfo) Line() string { return optID("slot info", c.SlotID) }

// TransportInfo asks for the transport: status, speed, slot, clip,
// timecode, video format, loop. transport_info.
type TransportInfo struct{}

func (TransportInfo) Line() string { return "transport info" }

// Configuration asks for the configuration: configuration.
type Configuration struct{}

func (Configuration) Line() string { return "configuration" }

// SetConfiguration sets configuration parameters in one line, which the
// deck applies together: set_configuration. It refuses to be empty,
// where set_configuration sends nothing.
type SetConfiguration struct{ Settings []Setting }

func (c SetConfiguration) Line() string { return "configuration: " + settings(c.Settings) }

func (c SetConfiguration) Validate() error {
	if len(c.Settings) == 0 {
		return errors.New("hyperdeck: configuration: nothing to set")
	}
	return checkLine(c)
}

// DiskList asks for the clips on the active disk, or SlotID's: disk_list.
type DiskList struct{ SlotID *int }

func (c DiskList) Line() string { return optID("disk list", c.SlotID) }

// ClipsCount asks how many clips the timeline holds: clips_count.
type ClipsCount struct{}

func (ClipsCount) Line() string { return "clips count" }

// ClipsGet asks for the timeline's clips: clips_get.
type ClipsGet struct{}

func (ClipsGet) Line() string { return "clips get" }

// Stop stops playback or recording: stop.
type Stop struct{}

func (Stop) Line() string { return "stop" }

// Play starts playback, or changes the one under way: play. Speed is a
// percentage, -5000 to 5000 (100 is normal, negative plays backwards),
// ClipID a timeline clip to start at. Loop and SingleClip are written as
// given, true or false, and nil leaves them out, as hyperdeckwire 1.8.0
// writes them (bmdwire#60; before, a false was left out too). The order
// is hyperdeckwire's: clip id, loop, single clip, speed.
type Play struct {
	ClipID     *int
	Loop       *bool
	SingleClip *bool
	Speed      *int
}

func (c Play) Line() string {
	var parts []string
	if c.ClipID != nil {
		parts = append(parts, fmt.Sprintf("clip id: %d", *c.ClipID))
	}
	if c.Loop != nil {
		parts = append(parts, fmt.Sprintf("loop: %t", *c.Loop))
	}
	if c.SingleClip != nil {
		parts = append(parts, fmt.Sprintf("single clip: %t", *c.SingleClip))
	}
	if c.Speed != nil {
		parts = append(parts, fmt.Sprintf("speed: %d", *c.Speed))
	}
	if len(parts) == 0 {
		return "play"
	}
	return "play: " + strings.Join(parts, " ")
}

// Pause holds the current frame: the protocol has no pause, so it is
// play at speed 0, as pause does.
type Pause struct{}

func (Pause) Line() string { return "play: speed: 0" }

// ClipsClear empties the timeline, deleting nothing on disk: clips_clear.
type ClipsClear struct{}

func (ClipsClear) Line() string { return "clips clear" }

// ClipsAdd adds a clip to the timeline by its file name (with any
// folder), at the end or before BeforeClipID: clips_add. The name goes as
// it is: the deck reads everything after "name:" to the end of the line.
type ClipsAdd struct {
	Name         string
	BeforeClipID *int
}

func (c ClipsAdd) Line() string {
	if c.BeforeClipID != nil {
		return fmt.Sprintf("clips add: clip id: %d name: %s", *c.BeforeClipID, c.Name)
	}
	return "clips add: name: " + c.Name
}

func (c ClipsAdd) Validate() error { return checkLine(c) }

// ClipsRemove removes a timeline clip, renumbering those after it:
// clips_remove.
type ClipsRemove struct{ ClipID int }

func (c ClipsRemove) Line() string { return fmt.Sprintf("clips remove: clip id: %d", c.ClipID) }

// GotoClip moves the transport to the start of a timeline clip without
// playing it: goto_clip.
type GotoClip struct{ ClipID int }

func (c GotoClip) Line() string { return fmt.Sprintf("goto: clip id: %d", c.ClipID) }

// GotoClipBy moves the transport Offset clips along the timeline, back
// when negative: "goto: clip id: +1" is the next clip. goto_clip_by
// (bmdwire#60); past either end a deck refuses it, 109.
type GotoClipBy struct{ Offset int }

func (c GotoClipBy) Line() string { return fmt.Sprintf("goto: clip id: %+d", c.Offset) }

func (c GotoClipBy) Validate() error {
	if c.Offset == 0 {
		return errors.New("hyperdeck: goto_clip_by: an offset of 0 clips moves nowhere")
	}
	return nil
}

// GotoTimecode moves the transport to a timecode, HH:MM:SS:FF:
// goto_timecode (bmdwire#60), which refuses anything else, unsent.
type GotoTimecode struct{ Timecode string }

func (c GotoTimecode) Line() string { return "goto: timecode: " + c.Timecode }

func (c GotoTimecode) Validate() error {
	if !IsTimecode(c.Timecode) {
		return fmt.Errorf("hyperdeck: goto_timecode: %s is not a timecode, HH:MM:SS:FF", pytext.Repr(c.Timecode))
	}
	return nil
}

// IsTimecode reports whether s is HH:MM:SS:FF (or HH:MM:SS;FF, drop
// frame), two digits each, hours under 24 and minutes and seconds under
// 60: goto_timecode's _TIMECODE (bmdwire#60).
func IsTimecode(s string) bool {
	if len(s) != 11 {
		return false
	}
	for i := range 11 {
		switch {
		case i%3 == 2:
			if s[i] != ':' && !(i == 8 && s[i] == ';') {
				return false
			}
		case s[i] < '0' || s[i] > '9':
			return false
		}
	}
	two := func(i int) int { return int(s[i]-'0')*10 + int(s[i+1]-'0') }
	return two(0) < 24 && two(3) < 60 && two(6) < 60
}

// Record starts recording, into a clip named Name if given: record
// (bmdwire#60). With no input a deck refuses it, 110.
type Record struct{ Name string }

func (c Record) Line() string {
	if c.Name == "" {
		return "record"
	}
	return "record: name: " + c.Name
}

func (c Record) Validate() error { return checkLine(c) }

// RemoteEnable turns remote control on or off: remote_enable.
type RemoteEnable struct{ Enable bool }

func (c RemoteEnable) Line() string { return fmt.Sprintf("remote: enable: %t", c.Enable) }

// SlotSelect makes a slot the active one: slot_select.
type SlotSelect struct{ SlotID int }

func (c SlotSelect) Line() string { return fmt.Sprintf("slot select: slot id: %d", c.SlotID) }

// Ping asks the deck to answer: ping.
type Ping struct{}

func (Ping) Line() string { return "ping" }

// Notify turns asynchronous notifications on or off for this connection
// ("transport: true"), set_notify, or with no settings asks which are on,
// notify (bmdwire#60).
type Notify struct{ Settings []Setting }

func (c Notify) Line() string {
	if len(c.Settings) == 0 {
		return "notify"
	}
	return "notify: " + settings(c.Settings)
}

func (c Notify) Validate() error { return checkLine(c) }

// quit ends the connection: close sends it.
type quit struct{}

func (quit) Line() string { return "quit" }

// Raw is any line, for what this package has no type for.
type Raw string

func (c Raw) Line() string { return string(c) }

func (c Raw) Validate() error {
	if strings.TrimSpace(string(c)) == "" {
		return errors.New("hyperdeck: an empty line")
	}
	return checkLine(c)
}
