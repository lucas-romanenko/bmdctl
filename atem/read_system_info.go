// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The system readers of bmdwire v1.3.0 atemwire/messages/system_info.py.
// display_fps, which atemwire keeps in state.py, is DisplayFPS in rate.go.
// See read_state.go for the conventions.

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// VideoModeView is video_mode's dict: the current mode's label ("1080p50")
// and number.
type VideoModeView struct {
	Format string `json:"format"`
	ID     int    `json:"id"`
}

// AvailableVideoModeView is one entry of available_video_modes.
type AvailableVideoModeView struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// CurrentVideoMode is video_mode (messages/system_info.py), bmdwire v1.3.0:
// the current video mode, or nil (atemwire's None) before VidM. It isn't
// called VideoMode because State has a field of that name. As in atemwire,
// a leading "f" is cut from the label (no label the mode table makes has
// one).
func (s State) CurrentVideoMode() *VideoModeView {
	m := s.VideoMode.Mode
	if m == nil {
		return nil
	}
	return &VideoModeView{Format: strings.TrimPrefix(m.Label(), "f"), ID: int(m.Mode)}
}

// AvailableVideoModes is available_video_modes (messages/system_info.py),
// bmdwire v1.3.0: the modes the switcher supports, in the order _VMC lists
// them; empty before _VMC.
func (s State) AvailableVideoModes() []AvailableVideoModeView {
	out := []AvailableVideoModeView{}
	if c := s.VideoMode.Capability; c != nil {
		for _, m := range c.Modes {
			out = append(out, AvailableVideoModeView{ID: int(m.ModeNum), Label: m.Mode.Label()})
		}
	}
	return out
}

// VideoResolution is video_resolution (messages/system_info.py), bmdwire
// v1.3.0: the frame size in pixels, width then height, or def before VidM
// (atemwire's default is (1920, 1080)) and for a mode atemwire's table
// lacks, whose frame size is unknown (bmdwire#71).
func (s State) VideoResolution(def [2]int) [2]int {
	m := s.VideoMode.Mode
	if m == nil {
		return def
	}
	w, h := m.FrameSize()
	if w == 0 && h == 0 {
		return def // atemwire's get_resolution returns None
	}
	return [2]int{w, h}
}

// ProductName is product_name (messages/system_info.py), bmdwire v1.3.0:
// the model name from _pin, stripped, or def (atemwire's default is "")
// before _pin and while the name is empty.
func (s State) ProductName(def string) string {
	p := s.Identity.Product
	if p == nil || p.Name == "" {
		// atemwire falls back to a product_name attribute no packet has,
		// then to the default.
		return def
	}
	return decodeName(p.Name)
}

// DeviceName is device_name (messages/system_info.py), bmdwire v1.3.0: the
// name the switcher was given in ATEM Setup (WhoI), stripped, or def
// (atemwire's default is "") before WhoI. Unlike ProductName, an empty
// name is returned as "".
func (s State) DeviceName(def string) string {
	d := s.Identity.Device
	if d == nil {
		return def
	}
	return decodeName(d.Name)
}

// SDI3GLevel is sdi_3g_level (messages/system_info.py), bmdwire v1.3.0:
// "LevelA" for V3sl level 1, "LevelB" for any other, def (atemwire's
// default is "LevelB") before V3sl.
func (s State) SDI3GLevel(def string) string {
	l := s.VideoMode.SDI3GLevel
	if l == nil {
		return def
	}
	if l.Level == 1 {
		return "LevelA"
	}
	return "LevelB"
}

// TopologyBlockCountsView is topology_block_counts' dict: the DVE, stinger
// and SuperSource block counts, each nil (atemwire's None) when they can't
// be known.
type TopologyBlockCountsView struct {
	DVEs         *int `json:"dves"`
	Stingers     *int `json:"stingers"`
	SuperSources *int `json:"superSources"`
}

// topologyLayouts is TOPOLOGY_LAYOUTS (bmdwire#70, 8defba1): where _top
// keeps the DVE, stinger and SuperSource counts, by protocol version,
// newest first: the first version, the shortest payload, and the offset
// of the DVE byte (the other two follow it). Protocol 2.30 (firmware
// 8.1.1) put a multiviewer count at offset 6; atem-connection reads the
// DVE byte at 8 up to 2.29 and at 9 from 2.30. The 2.30 layout is a 1 M/E
// Constellation HD's own and upstream pyatem's table of seven models; the
// older one is atem-connection's and untested on a switcher.
var topologyLayouts = []struct {
	major, minor uint16
	length, dve  int
}{
	{2, 30, 28, 9},
	{2, 22, 18, 8},
}

// topologyLayout is _topology_layout: the DVE offset when the layout of
// this switcher's _top is known, else -1 and why.
func (s State) topologyLayout() (offset int, raw []byte, version string, why string) {
	top := s.Topology
	if top == nil {
		return -1, nil, "", "no _top"
	}
	raw = top.Raw
	v := s.Identity.Firmware
	if v == nil {
		return -1, raw, "", "no _ver"
	}
	version = fmt.Sprintf("%d.%d", v.Major, v.Minor)
	if v.Major != 2 {
		return -1, raw, version, fmt.Sprintf("protocol %d.%d is not 2.x", v.Major, v.Minor)
	}
	for _, l := range topologyLayouts {
		if v.Minor >= l.minor {
			if len(raw) < l.length {
				return -1, raw, version, fmt.Sprintf("%d bytes, the %d.%d layout has %d", len(raw), l.major, l.minor, l.length)
			}
			return l.dve, raw, version, ""
		}
	}
	return -1, raw, version, fmt.Sprintf("protocol 2.%d is older than any known _top layout", v.Minor)
}

// TopologyBlockCounts is topology_block_counts (messages/system_info.py),
// bmdwire v1.10.0 (bmdwire#70, 8defba1): the DVE, stinger and SuperSource
// block counts from _top, read at the offsets the switcher's _ver calls
// for. All three are nil until _top and _ver have both arrived, for a
// protocol older than 2.22 or not 2.x, and for a payload shorter than its
// layout: a 0 is a count the switcher reported, never a default.
func (s State) TopologyBlockCounts() TopologyBlockCountsView {
	off, raw, _, _ := s.topologyLayout()
	if off < 0 {
		return TopologyBlockCountsView{}
	}
	at := func(i int) *int { n := int(raw[off+i]); return &n }
	return TopologyBlockCountsView{DVEs: at(0), Stingers: at(1), SuperSources: at(2)}
}

// TopologyDiagnostic is topology_diagnostic (messages/system_info.py),
// bmdwire v1.10.0: one line for the log, the protocol version, the _top
// payload in hex and the block counts read from it, or why they are
// unknown. The session logs it at every connect.
func (s State) TopologyDiagnostic() string {
	off, raw, version, why := s.topologyLayout()
	if version == "" {
		version = "?"
	}
	payload := "none"
	if s.Topology != nil {
		payload = fmt.Sprintf("%d bytes: %s", len(raw), hex.EncodeToString(raw))
	}
	result := "block counts unknown (" + why + ")"
	if off >= 0 {
		c := s.TopologyBlockCounts()
		result = fmt.Sprintf("dves=%d stingers=%d superSources=%d (DVE at offset %d)", *c.DVEs, *c.Stingers, *c.SuperSources, off)
	}
	return "protocol " + version + ", _top " + payload + " -> " + result
}
