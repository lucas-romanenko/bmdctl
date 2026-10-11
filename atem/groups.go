// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Groups are the ATEM's command groups, in the order a reader meets
// them: what bmdctl mcp makes one tool of each (docs/PORTING.md, "mcp").
// Every command belongs to one, by its first word (groupOf).
var Groups = []table.Group{
	{Name: "switching", Summary: "What is on program, preview and the aux outputs: program, preview, cut, auto, the T-bar, fade to black, undo."},
	{Name: "transition", Summary: "The next transition: its style and rate, each style's settings (mix, dip, wipe, DVE, stinger), what it takes, preview."},
	{Name: "usk", Summary: "Upstream keyers: fill and key, type (luma, chroma, pattern, DVE) and its settings, mask, fly keyframes, on air."},
	{Name: "dsk", Summary: "Downstream keyers: fill and key, gain, mask, rate, tie, on air, auto."},
	{Name: "audio", Summary: "The audio mixer: Fairlight strips, EQ, dynamics, the master and meters; the classic mixer's (classic-audio ...)."},
	{Name: "media", Summary: "The media pool and players: stills uploaded to and downloaded from slots, cleared, what each player shows."},
	{Name: "macro", Summary: "Macros: run, stop, record, list, upload and download, decode and encode."},
	{Name: "profile", Summary: "ATEM Software Control profiles: save the switcher's setup, restore one, download its stills."},
	{Name: "sources", Summary: "Inputs and sources: the list, renames, colour generators, camera control, HyperDeck slots."},
	{Name: "outputs", Summary: "Multiviewers, SuperSource, recording and streaming."},
	{Name: "system", Summary: "The switcher itself: what it is, its video mode, startup state, time."},
	{Name: "session", Summary: "The session and reads: connect, disconnect, status (what is on air), show one area, the raw state, watch changes."},
}

// groupOf is each first word's group.
var groupOf = map[string]string{
	"program": "switching", "preview": "switching", "cut": "switching", "auto": "switching", "tbar": "switching",
	"undo": "switching", "aux": "switching", "ftb": "switching",
	"transition": "transition", "usk": "usk", "dsk": "dsk",
	"audio": "audio", "classic-audio": "audio",
	"media": "media", "macro": "macro", "profile": "profile",
	"inputs": "sources", "input": "sources", "color": "sources", "camera": "sources", "hyperdeck-slot": "sources",
	"multiviewer": "outputs", "supersource": "outputs", "recording": "outputs", "streaming": "outputs",
	"info": "system", "video-mode": "system", "video-modes": "system", "auto-video-mode": "system",
	"startup": "system", "time": "system", "time-of-day": "system", "time-request": "system",
	"connect": "session", "disconnect": "session", "status": "session", "state": "session", "show": "session",
	"watch": "session",
}

// GroupOf is the group an ATEM command belongs to, by its path's first
// word; "" for a word no group has.
func GroupOf(path string) string {
	first, _, _ := strings.Cut(path, " ")
	return groupOf[first]
}
