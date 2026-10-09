// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"slices"
	"testing"
)

// Reference values from bmdwire v1.3.0's helpers.parse_rate, format_rate
// and state.display_fps, run under Python 3.12.
func TestParseRateMatchesAtemwire(t *testing.T) {
	in := []string{"1:12", "0:10", "30", " 7 ", "1_0", "_1", "1__0", "+5", "-3", "-1:00", "1:2:3", "abc", "", ":5", "5:",
		"1:-2", " 1 : 2 ", "0x10", "1.5", "１２"}
	for fps, want := range map[int][]int{
		25: {37, 10, 30, 7, 10, 25, 25, 5, -3, -25, 25, 25, 25, 25, 25, 23, 27, 25, 25, 12},
		30: {42, 10, 30, 7, 10, 30, 30, 5, -3, -30, 30, 30, 30, 30, 30, 28, 32, 30, 30, 12},
		0:  {37, 10, 30, 7, 10, 25, 25, 5, -3, -25, 25, 25, 25, 25, 25, 23, 27, 25, 25, 12},
		-1: {37, 10, 30, 7, 10, 25, 25, 5, -3, -25, 25, 25, 25, 25, 25, 23, 27, 25, 25, 12},
	} {
		var got []int
		for _, s := range in {
			got = append(got, ParseRate(s, fps))
		}
		if !slices.Equal(got, want) {
			t.Errorf("fps %d:\n got %v\nwant %v", fps, got, want)
		}
	}
}

func TestFormatRateMatchesAtemwire(t *testing.T) {
	want := []string{"0:00", "0:00", "0:00", "1:00", "0:25", "1:00", "1:12", "1:07", "1:12", "-1:24", "-1:29", "-1:24",
		"-2:24", "-1:04", "-2:24", "24:00", "20:00", "24:00"}
	var got []string
	for _, f := range []int{0, 25, 37, -1, -26, 600} {
		for _, fps := range []int{25, 30, 0} {
			got = append(got, FormatRate(f, fps))
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("\n got %v\nwant %v", got, want)
	}
}

func TestDisplayFPSMatchesAtemwire(t *testing.T) {
	if DisplayFPS(newState()) != 25 {
		t.Error("no video mode")
	}
	for rate, want := range map[float64]int{0: 25, 23.98: 24, 24: 24, 25: 25, 29.97: 30, 30: 30, 50: 25, 59.94: 30, 60: 30, 47.9: 48, 48: 24} {
		s := newState()
		s.VideoMode.Mode = &VideoModeField{Rate: rate}
		if got := DisplayFPS(s); got != want {
			t.Errorf("rate %v: %d, want %d", rate, got, want)
		}
	}
}

// The restored-upstream classes and CKMs are unverified (SURFACE.md §5.4
// item 14); VideoMode, the two startup-state commands and ClearStill are
// destructive. Every echo key names a packet the parsers store, keyed the
// way the parser keys it.
func TestCommandFlagsAndEchoKeys(t *testing.T) {
	unverified := []string{"AiVM", "CAMI", "CAMM", "CAMm", "CCmd", "CKMs", "CMvI", "CMvP", "CRMS", "CRSS", "CSBP", "CSSc",
		"CTPr", "CTPs", "RcTM", "SALN", "SRcl", "SRsv", "STAB", "SToD", "StrR"}
	destructive := []string{"CSTL", "CVdM", "SRcl", "SRsv"}
	var gotU, gotD []string
	for _, c := range allCommands {
		if c.Unverified() {
			gotU = append(gotU, c.Opcode())
		}
		if c.Destructive() {
			gotD = append(gotD, c.Opcode())
		}
		k := c.EchoKey()
		if k == "" {
			continue
		}
		p, ok := parsers[k[:4]]
		if !ok || p.store == nil {
			t.Errorf("%s: echo key %q names no stored packet", c.Opcode(), k)
		}
	}
	slices.Sort(gotU)
	slices.Sort(gotD)
	slices.Sort(unverified)
	if !slices.Equal(gotU, unverified) {
		t.Errorf("unverified:\n got %v\nwant %v", gotU, unverified)
	}
	if !slices.Equal(gotD, destructive) {
		t.Errorf("destructive: got %v, want %v", gotD, destructive)
	}
	// Spot-check that echo keys use the parsers' index format.
	for _, tc := range []struct {
		c    Command
		want string
	}{
		{ProgramInput{ME: 1, Source: 5}, "PrgI:1"},
		{KeyOnAir{ME: 1, Keyer: 2}, "KeOn:1:2"},
		{FairlightStripProperties{Source: 1301, Channel: -1}, "FASP:1301.0"},
		{FairlightStripProperties{Source: 1301, Channel: 1}, "FASP:1301.1"},
		{FairlightEQBandProperties{Source: 2, Channel: -1, Band: 3}, "AEBP:2.0:3"},
		{AudioInput{Source: 7}, "AMIP:7.0"},
		{KeyerKeyframeSet{Keyframe: KeyframeB}, "KKFP:0:0:2"},
		{KeyerKeyframeSet{Keyframe: KeyframeFull}, "KeFS:0:0"},
		{MacroAction{Slot: 4, Action: MacroDelete}, "MPrp:4"},
		{CameraControl{Destination: 1, Category: 2, Parameter: 3}, "CCdP:1:2:3"},
	} {
		c, want := tc.c, tc.want
		if got := c.EchoKey(); got != want {
			t.Errorf("%T: echo key %q, want %q", c, got, want)
		}
	}
}
