// SPDX-License-Identifier: MIT

package hyperdeck

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// State is what the session has read from the deck: every block it was
// sent, a reply or a notification, under the names the deck gives its
// fields ("display timecode", "slot id"). A reply replaces its block; a
// notification, which carries only what changed, is merged into it.
// hyperdeckwire keeps no state: this is bmdctl's (docs/PORTING.md,
// "HyperDeck").
type State struct {
	Connection    map[string]string         `json:"connection"`    // the greeting: model, protocol version
	Device        map[string]string         `json:"device"`        // device info
	Transport     map[string]string         `json:"transport"`     // transport info
	Slots         map[int]map[string]string `json:"slots"`         // slot info, by slot id
	Remote        map[string]string         `json:"remote"`        // remote
	Configuration map[string]string         `json:"configuration"` // configuration
	Notify        map[string]string         `json:"notify"`        // notify, as last asked
	Timeline      []Clip                    `json:"timeline"`      // clips get
	Disks         map[int][]Clip            `json:"disks"`         // disk list, by slot id
	// Other holds any other block, by its head's text ("cache info").
	Other map[string]map[string]string `json:"other"`
}

func newState() State {
	return State{Connection: map[string]string{}, Device: map[string]string{}, Transport: map[string]string{},
		Slots: map[int]map[string]string{}, Remote: map[string]string{}, Configuration: map[string]string{},
		Notify: map[string]string{}, Timeline: []Clip{}, Disks: map[int][]Clip{}, Other: map[string]map[string]string{}}
}

func (s State) clone() State {
	c := State{Connection: maps.Clone(s.Connection), Device: maps.Clone(s.Device), Transport: maps.Clone(s.Transport),
		Remote: maps.Clone(s.Remote), Configuration: maps.Clone(s.Configuration), Notify: maps.Clone(s.Notify),
		Timeline: slices.Clone(s.Timeline), Slots: map[int]map[string]string{}, Disks: map[int][]Clip{},
		Other: map[string]map[string]string{}}
	for k, v := range s.Slots {
		c.Slots[k] = maps.Clone(v)
	}
	for k, v := range s.Disks {
		c.Disks[k] = slices.Clone(v)
	}
	for k, v := range s.Other {
		c.Other[k] = maps.Clone(v)
	}
	return c
}

// Blocks by the head's text: a reply's code is 2xx, a notification's
// 5xx, with the same text (208 and 508 transport info).
const (
	blockConnection    = "connection info"
	blockDevice        = "device info"
	blockTransport     = "transport info"
	blockSlot          = "slot info"
	blockRemote        = "remote info"
	blockConfiguration = "configuration"
	blockNotify        = "notify"
	blockClips         = "clips info"
	blockDisk          = "disk list"
)

// store takes a response into the state and returns the key of what it
// changed ("transport", "slot:2"), or "" for a response that holds no
// state (ok, an error).
func (s *State) store(r Response) string {
	if !r.OK() && !r.IsAsync() {
		return ""
	}
	merge := r.IsAsync()
	p := r.Params()
	put := func(dst *map[string]string) {
		if !merge || *dst == nil {
			*dst = map[string]string{}
		}
		maps.Copy(*dst, p)
	}
	switch strings.ToLower(r.Text) {
	case blockConnection:
		put(&s.Connection)
		return "connection"
	case blockDevice:
		put(&s.Device)
		return "device"
	case blockTransport:
		put(&s.Transport)
		return "transport"
	case blockSlot:
		id, err := strconv.Atoi(p["slot id"])
		if err != nil {
			break
		}
		m := s.Slots[id]
		put(&m)
		s.Slots[id] = m
		return "slot:" + strconv.Itoa(id)
	case blockRemote:
		put(&s.Remote)
		return "remote"
	case blockConfiguration:
		put(&s.Configuration)
		return "configuration"
	case blockNotify:
		put(&s.Notify)
		return "notify"
	case blockClips:
		if r.Code == 205 {
			s.Timeline = parseClipLines(r.Lines, parseClipsLine)
			return "timeline"
		}
	case blockDisk:
		id, err := strconv.Atoi(p["slot id"])
		if err != nil {
			break
		}
		s.Disks[id] = parseClipLines(r.Lines, parseDiskLine)
		return "disk:" + strconv.Itoa(id)
	}
	if len(r.Lines) == 0 || r.Code == 200 {
		return ""
	}
	key := strings.ToLower(r.Text)
	m := s.Other[key]
	put(&m)
	s.Other[key] = m
	return key
}

// Name is what the deck calls itself: the name set in HyperDeck Setup
// (device info's "name"), else its model.
func (s State) Name() string {
	for _, k := range []string{"name", "device name"} {
		if n := s.Device[k]; n != "" {
			return n
		}
	}
	return s.Model()
}

// Model is the deck's model: device info's, else the greeting's.
func (s State) Model() string {
	if m := s.Device["model"]; m != "" {
		return m
	}
	return s.Connection["model"]
}

// Status is the transport's status: preview, stopped, play, forward,
// rewind, jog, shuttle or record; "" before the transport is known.
func (s State) Status() string { return s.Transport["status"] }

// ClipID is the transport's timeline clip, if it is on one.
func (s State) ClipID() (int, bool) { return intField(s.Transport, "clip id") }

// SlotID is the active slot, if one is.
func (s State) SlotID() (int, bool) { return intField(s.Transport, "slot id") }

// Speed is the playback speed in percent.
func (s State) Speed() (int, bool) { return intField(s.Transport, "speed") }

// Loop reports whether playback loops.
func (s State) Loop() bool { return s.Transport["loop"] == "true" }

// SingleClip reports whether playback stays on the current clip.
func (s State) SingleClip() bool { return s.Transport["single clip"] == "true" }

// Timecode is the transport's display timecode, else its timecode.
func (s State) Timecode() string {
	if t := s.Transport["display timecode"]; t != "" {
		return t
	}
	return s.Transport["timecode"]
}

// Clip returns the timeline clip with this id.
func (s State) Clip(id int) (Clip, bool) {
	for _, c := range s.Timeline {
		if c.ID == id {
			return c, true
		}
	}
	return Clip{}, false
}

// RecordingTime is how long the slot can still record, in seconds, as
// slot info's "recording time" says.
func (s State) RecordingTime(slot int) (int, bool) { return intField(s.Slots[slot], "recording time") }

func intField(m map[string]string, k string) (int, bool) {
	n, err := strconv.Atoi(m[k])
	return n, err == nil
}
