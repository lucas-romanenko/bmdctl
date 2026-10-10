// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The colour generator reader of bmdwire v1.3.0
// atemwire/messages/color_generator.py. See read_state.go for the
// conventions.

// ColorGeneratorView is color_generator's dict: hue in degrees (one
// decimal), saturation and luma in percent (two decimals).
type ColorGeneratorView struct {
	Hue        float64 `json:"hue"`
	Saturation float64 `json:"saturation"`
	Luma       float64 `json:"luma"`
}

// ColorGenerator is color_generator (messages/color_generator.py), bmdwire
// v1.3.0: colour generator generator (0-based), zeros before its ColV.
func (s State) ColorGenerator(generator int) ColorGeneratorView {
	if generator < 0 || generator >= len(s.ColorGenerators) || s.ColorGenerators[generator] == nil {
		return ColorGeneratorView{}
	}
	g := s.ColorGenerators[generator]
	return ColorGeneratorView{
		Hue:        pyRound(g.Hue, 1),
		Saturation: percentFromUnit(g.Saturation),
		Luma:       percentFromUnit(g.Luma),
	}
}
