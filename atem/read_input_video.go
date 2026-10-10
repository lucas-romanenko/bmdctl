// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The input readers of bmdwire v1.3.0 atemwire/messages/input_video.py.
// See read_state.go for the conventions.

import "slices"

// InputLabelView is input_label's dict: a source's long and short names.
type InputLabelView struct {
	Long  string `json:"long"`
	Short string `json:"short"`
}

// InputSourceView is one entry of all_input_sources: a source with its
// names, port type and where it can be routed.
type InputSourceView struct {
	Source             int    `json:"source"`
	Long               string `json:"long"`
	Short              string `json:"short"`
	PortType           int    `json:"port_type"`
	AvailableAux       bool   `json:"available_aux"`
	AvailableKeySource bool   `json:"available_key_source"`
	AvailableMultiview bool   `json:"available_multiview"`
	AvailableME1       bool   `json:"available_me1"`
	AvailableME2       bool   `json:"available_me2"`
	AvailableME3       bool   `json:"available_me3"`
	AvailableME4       bool   `json:"available_me4"`
}

// RenamableInputsView is renamable_inputs's dict: the sources ATEM
// Software Control's three Labels tabs list.
type RenamableInputsView struct {
	Inputs  []RenamableInputView `json:"inputs"`
	Outputs []RenamableInputView `json:"outputs"`
	Media   []RenamableInputView `json:"media"`
}

// RenamableInputView is one entry of renamable_inputs.
type RenamableInputView struct {
	Source int    `json:"source"`
	Long   string `json:"long"`
	Short  string `json:"short"`
}

// InputLabel is input_label (messages/input_video.py), bmdwire v1.3.0: the
// names of source; empty before its InPr.
func (s State) InputLabel(source int) InputLabelView {
	k, ok := u16Key(source)
	in := s.Inputs[k]
	if !ok || in == nil {
		return InputLabelView{}
	}
	return InputLabelView{Long: decodeName(in.Name), Short: decodeName(in.ShortName)}
}

// sortedSources is the sources with an input-properties entry, ascending.
func (s State) sortedSources() []uint16 {
	var out []uint16
	for src, in := range s.Inputs {
		if in != nil {
			out = append(out, src)
		}
	}
	slices.Sort(out)
	return out
}

// AllInputSources is all_input_sources (messages/input_video.py), bmdwire
// v1.3.0: every source the switcher has described, by source id; empty
// before any InPr.
func (s State) AllInputSources() []InputSourceView {
	out := []InputSourceView{}
	for _, src := range s.sortedSources() {
		in := s.Inputs[src]
		out = append(out, InputSourceView{
			Source:             int(src),
			Long:               decodeName(in.Name),
			Short:              decodeName(in.ShortName),
			PortType:           int(in.PortType),
			AvailableAux:       in.AvailableAux,
			AvailableKeySource: in.AvailableKeySource,
			AvailableMultiview: in.AvailableMultiview,
			AvailableME1:       in.AvailableME1,
			AvailableME2:       in.AvailableME2,
			AvailableME3:       in.AvailableME3,
			AvailableME4:       in.AvailableME4,
		})
	}
	return out
}

// RenamableInputs is renamable_inputs (messages/input_video.py), bmdwire
// v1.3.0: external inputs (port type 0) under Inputs; M/E, aux and
// multiview outputs (128, 129, 131) under Outputs; colour generators and
// media players with their keys (3, 4, 5) under Media; each by source id.
// The fixed internal sources are left out.
func (s State) RenamableInputs() RenamableInputsView {
	v := RenamableInputsView{Inputs: []RenamableInputView{}, Outputs: []RenamableInputView{}, Media: []RenamableInputView{}}
	for _, src := range s.sortedSources() {
		in := s.Inputs[src]
		e := RenamableInputView{Source: int(src), Long: decodeName(in.Name), Short: decodeName(in.ShortName)}
		switch in.PortType {
		case 0:
			v.Inputs = append(v.Inputs, e)
		case 128, 129, 131:
			v.Outputs = append(v.Outputs, e)
		case 3, 4, 5:
			v.Media = append(v.Media, e)
		}
	}
	return v
}
