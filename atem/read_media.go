// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The media readers of bmdwire v1.3.0 atemwire/messages/media.py. See
// read_state.go for the conventions.

// MediaplayerSlotInfoView is mediaplayer_slot_info's dict: whether the
// media pool slot is used, its MD5 as hex ("" until the switcher has
// computed it) and its decoded name.
type MediaplayerSlotInfoView struct {
	IsUsed bool   `json:"is_used"`
	Hash   string `json:"hash"`
	Name   string `json:"name"`
}

// MediaplayerSelectedView is mediaplayer_selected's dict: SourceType 1
// still, 2 clip, and the slot.
type MediaplayerSelectedView struct {
	SourceType int `json:"source_type"`
	Slot       int `json:"slot"`
}

// MediaplayerSlots is mediaplayer_slots (messages/media.py), bmdwire
// v1.3.0: the number of still slots in the media pool; 0 before _mpl.
func (s State) MediaplayerSlots() int {
	if s.MediaPool.Slots != nil {
		return int(s.MediaPool.Slots.Stills)
	}
	return 0
}

// MediaplayerSlotInfo is mediaplayer_slot_info (messages/media.py),
// bmdwire v1.3.0: media pool slot slot (keyed by index only, SURFACE.md
// §5.4 item 6); unused and unnamed before its MPfe.
func (s State) MediaplayerSlotInfo(slot int) MediaplayerSlotInfoView {
	k, ok := u16Key(slot)
	f := s.MediaPool.Files[k]
	if !ok || f == nil {
		return MediaplayerSlotInfoView{}
	}
	return MediaplayerSlotInfoView{IsUsed: f.IsUsed, Hash: md5Hex(f.Hash), Name: decodeName(f.Name)}
}

// MediaplayerSelected is mediaplayer_selected (messages/media.py), bmdwire
// v1.3.0: what media player player (0-based) plays; zeros before its MPCE.
func (s State) MediaplayerSelected(player int) MediaplayerSelectedView {
	if player < 0 || player >= len(s.MediaPlayers) || s.MediaPlayers[player] == nil {
		return MediaplayerSelectedView{}
	}
	p := s.MediaPlayers[player]
	return MediaplayerSelectedView{SourceType: int(p.SourceType), Slot: int(p.Slot)}
}
