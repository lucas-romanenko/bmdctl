// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The fade-to-black readers of bmdwire v1.3.0
// atemwire/messages/fade_to_black.py. See read_state.go for the
// conventions.

// FTBActive is ftb_active (messages/fade_to_black.py), bmdwire v1.3.0:
// whether M/E me is faded to black (FtbS done); false before FtbS.
func (s State) FTBActive(me int) bool {
	if m := s.meAt(me); m != nil && m.FTB.State != nil {
		return m.FTB.State.Done
	}
	return false
}

// FTBInTransition is ftb_in_transition (messages/fade_to_black.py),
// bmdwire v1.3.0; false before FtbS.
func (s State) FTBInTransition(me int) bool {
	if m := s.meAt(me); m != nil && m.FTB.State != nil {
		return m.FTB.State.Transitioning
	}
	return false
}

// FTBFramesRemaining is ftb_frames_remaining
// (messages/fade_to_black.py), bmdwire v1.3.0; 0 before FtbS.
func (s State) FTBFramesRemaining(me int) int {
	if m := s.meAt(me); m != nil && m.FTB.State != nil {
		return int(m.FTB.State.FramesRemaining)
	}
	return 0
}

// FTBRate is ftb_rate (messages/fade_to_black.py), bmdwire v1.3.0: frames;
// 25 before FtbP.
func (s State) FTBRate(me int) int {
	if m := s.meAt(me); m != nil && m.FTB.Rate != nil {
		return int(m.FTB.Rate.Rate)
	}
	return 25
}

// FTBDisabled is ftb_disabled (messages/fade_to_black.py), bmdwire v1.3.0:
// FEna's disabled flag, which has no M/E (SURFACE.md §5.4 item 5); false
// before FEna.
func (s State) FTBDisabled() bool {
	if s.FTBEnabled != nil {
		return s.FTBEnabled.Disabled
	}
	return false
}
