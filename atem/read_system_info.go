// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The system readers of bmdwire v1.3.0 atemwire/messages/system_info.py.
// display_fps, which atemwire keeps in state.py, is DisplayFPS in rate.go.
// See read_state.go for the conventions.

import "strings"

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
// (atemwire's default is (1920, 1080)) and for a resolution atemwire's
// table lacks.
func (s State) VideoResolution(def [2]int) [2]int {
	m := s.VideoMode.Mode
	if m == nil {
		return def
	}
	w, h := m.FrameSize()
	if w == 0 && h == 0 {
		return def // atemwire's get_resolution raises KeyError
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
