// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The downstream keyer reader of bmdwire v1.3.0
// atemwire/messages/downstream_keyer.py. See read_state.go for the
// conventions.

// DSKStateView is dsk_state's dict. Rate is frames; clip and gain are
// percent (wire ÷ 10, one decimal); the mask edges are wire ÷ 1000, three
// decimals.
type DSKStateView struct {
	OnAir               bool    `json:"on_air"`
	Tie                 bool    `json:"tie"`
	Rate                int     `json:"rate"`
	InTransition        bool    `json:"in_transition"`
	IsAutoTransitioning bool    `json:"is_auto_transitioning"`
	FramesRemaining     int     `json:"frames_remaining"`
	FillSource          int     `json:"fill_source"`
	KeySource           int     `json:"key_source"`
	MaskEnabled         bool    `json:"mask_enabled"`
	MaskTop             float64 `json:"mask_top"`
	MaskBottom          float64 `json:"mask_bottom"`
	MaskLeft            float64 `json:"mask_left"`
	MaskRight           float64 `json:"mask_right"`
	PreMultiplied       bool    `json:"pre_multiplied"`
	Clip                float64 `json:"clip"`
	Gain                float64 `json:"gain"`
	InvertKey           bool    `json:"invert_key"`
}

// DSKState is dsk_state (messages/downstream_keyer.py), bmdwire v1.3.0:
// downstream keyer dsk from its dkey-state, dkey-properties and
// dkey-properties-base entries, each field at its default (false, 0, rate
// 25) while its packet is missing. InTransition is true for an auto
// transition too.
func (s State) DSKState(dsk int) DSKStateView {
	v := DSKStateView{Rate: 25}
	d := s.dskAt(dsk)
	if d == nil {
		d = &DSK{}
	}
	if st := d.State; st != nil {
		v.OnAir = st.OnAir
		v.InTransition = st.IsTransitioning || st.IsAutoTransitioning
		v.IsAutoTransitioning = st.IsAutoTransitioning
		v.FramesRemaining = int(st.FramesRemaining)
	}
	if b := d.Base; b != nil {
		v.FillSource = int(b.FillSource)
		v.KeySource = int(b.KeySource)
	}
	var clip, gain, top, bottom, left, right int
	if p := d.Properties; p != nil {
		v.Rate = int(p.Rate)
		v.Tie = p.Tie
		v.PreMultiplied = p.Premultiplied
		v.InvertKey = p.InvertKey
		v.MaskEnabled = p.Masked
		clip, gain = int(p.Clip), int(p.Gain)
		top, bottom, left, right = int(p.Top), int(p.Bottom), int(p.Left), int(p.Right)
	}
	v.Clip = pyRound(float64(clip)/10.0, 1)
	v.Gain = pyRound(float64(gain)/10.0, 1)
	v.MaskTop = pyRound(float64(top)/1000.0, 3)
	v.MaskBottom = pyRound(float64(bottom)/1000.0, 3)
	v.MaskLeft = pyRound(float64(left)/1000.0, 3)
	v.MaskRight = pyRound(float64(right)/1000.0, 3)
	return v
}
