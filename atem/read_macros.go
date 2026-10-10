// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The macro readers of bmdwire v1.3.0 atemwire/messages/macros.py. See
// read_state.go for the conventions.

// MacroEntryView is macro_entry's dict: whether the slot holds a macro and
// its decoded name.
type MacroEntryView struct {
	IsUsed bool   `json:"is_used"`
	Name   string `json:"name"`
}

// MacroEntry is macro_entry (messages/macros.py), bmdwire v1.3.0: macro
// slot slot (0-based); unused and unnamed before its MPrp.
func (s State) MacroEntry(slot int) MacroEntryView {
	k, ok := u16Key(slot)
	p := s.Macros.Properties[k]
	if !ok || p == nil {
		return MacroEntryView{}
	}
	return MacroEntryView{IsUsed: p.IsUsed, Name: decodeName(p.Name)}
}

// MacroPlayRunning is macro_play_running (messages/macros.py), bmdwire
// v1.3.0; false before MRPr.
func (s State) MacroPlayRunning() bool {
	if s.Macros.PlayStatus != nil {
		return s.Macros.PlayStatus.Running
	}
	return false
}

// MacroPlayIndex is macro_play_index (messages/macros.py), bmdwire v1.3.0:
// the macro playing, 0xFFFF when idle and before MRPr.
func (s State) MacroPlayIndex() int {
	if s.Macros.PlayStatus != nil {
		return int(s.Macros.PlayStatus.Index)
	}
	return 0xFFFF
}
