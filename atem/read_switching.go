// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The switching readers of bmdwire v1.3.0 atemwire/messages/switching.py.
// See read_state.go for the conventions.

// ProgramSource is program_source (messages/switching.py), bmdwire v1.3.0:
// M/E me's program source, or 0 before PrgI has arrived.
func (s State) ProgramSource(me int) int {
	if m := s.meAt(me); m != nil && m.Program != nil {
		return int(m.Program.Source)
	}
	return 0
}

// PreviewSource is preview_source (messages/switching.py), bmdwire v1.3.0:
// M/E me's preview source, or 0.
func (s State) PreviewSource(me int) int {
	if m := s.meAt(me); m != nil && m.Preview != nil {
		return int(m.Preview.Source)
	}
	return 0
}

// AuxSource is aux_source (messages/switching.py), bmdwire v1.3.0: the
// source of aux output aux (0-based), or 0.
func (s State) AuxSource(aux int) int {
	if aux >= 0 && aux < len(s.Aux) && s.Aux[aux] != nil {
		return int(s.Aux[aux].Source)
	}
	return 0
}
