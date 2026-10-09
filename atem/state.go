// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"maps"
)

// State is everything the switcher has told the session, the counterpart
// of atemwire's AtemProtocol.mixerstate.
type State struct {
	// Raw holds every packet stored as bytes, keyed by its four-character
	// code; the last packet wins.
	Raw map[string][]byte `json:"raw"`
}

func newState() State {
	return State{Raw: make(map[string][]byte)}
}

// clone returns a copy that shares nothing with s.
func (s State) clone() State {
	c := State{Raw: maps.Clone(s.Raw)}
	for k, v := range c.Raw {
		c.Raw[k] = bytes.Clone(v)
	}
	return c
}

// hasVideoMode reports whether the switcher has sent its video mode, the
// second half of atemwire's wait_ready condition.
func (s State) hasVideoMode() bool {
	_, ok := s.Raw["VidM"]
	return ok
}
