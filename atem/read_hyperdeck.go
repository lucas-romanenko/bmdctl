// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The HyperDeck readers of bmdwire v1.3.0 atemwire/messages/hyperdeck.py.
// See read_state.go for the conventions.

import "slices"

// HyperdeckSettingsView is hyperdeck_settings's dict: one HyperDeck slot's
// binding. Configured is false for an unbound slot (0.0.0.0).
type HyperdeckSettingsView struct {
	Slot           int    `json:"slot"`
	NetworkAddress string `json:"network_address"`
	Input          int    `json:"input"`
	Configured     bool   `json:"configured"`
}

// HyperdeckSettings is hyperdeck_settings (messages/hyperdeck.py), bmdwire
// v1.3.0: the binding of slot, unbound (0.0.0.0, input 0) before its RXMS.
// Slot echoes the argument.
func (s State) HyperdeckSettings(slot int) HyperdeckSettingsView {
	k, ok := u16Key(slot)
	h := s.HyperDecks[k]
	if !ok || h == nil {
		return HyperdeckSettingsView{Slot: slot, NetworkAddress: "0.0.0.0"}
	}
	return HyperdeckSettingsView{
		Slot:           slot,
		NetworkAddress: h.NetworkAddress,
		Input:          int(h.Input),
		Configured:     h.Configured,
	}
}

// PlanBindingPush is plan_binding_push (messages/hyperdeck.py), bmdwire
// v1.3.0: the HyperDeck settings write (CXMS) that binds deckIP to
// switcherInput. It reuses the configured slot that already holds deckIP
// (the lowest, if several do), else takes the lowest unconfigured slot not
// in excludeSlots; slots are those the switcher has reported, or 0-9
// before any RXMS. slot is nil (atemwire's None) when no slot is free;
// needsWrite is false when the binding is already right, and when slot is
// nil.
func (s State) PlanBindingPush(deckIP string, switcherInput int, excludeSlots ...int) (slot *int, needsWrite bool) {
	var ids []int
	for k := range s.HyperDecks {
		ids = append(ids, int(k))
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		ids = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	}
	binds := make([]HyperdeckSettingsView, len(ids))
	for i, id := range ids {
		binds[i] = s.HyperdeckSettings(id)
	}
	pick := -1
	for i, b := range binds {
		if b.Configured && b.NetworkAddress == deckIP {
			pick = i
			break
		}
	}
	if pick < 0 {
		for i, b := range binds {
			if !b.Configured && !slices.Contains(excludeSlots, ids[i]) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return nil, false
	}
	cur := binds[pick]
	id := ids[pick]
	return &id, cur.NetworkAddress != deckIP || cur.Input != switcherInput
}
