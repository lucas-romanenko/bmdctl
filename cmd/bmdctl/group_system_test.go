// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

// Group h, end to end against the fake switcher: the video mode, startup
// state, the clock, and the commands atemwire restored unverified, each of
// those saying so on stderr unless --json. The video mode and the clock
// are verified in atemwire and say nothing.
func TestSystemCommands(t *testing.T) {
	b := newBench(t)
	out := b.must(with("video-modes")...)
	if !strings.Contains(out, "current") {
		t.Errorf("video-modes:\n%s", out)
	}
	if code, _, errOut := b.run(with("video-mode", "1080p50")...); code != exitDestructive || !strings.Contains(errOut, "--yes") {
		t.Errorf("video-mode without --yes: %d %q", code, errOut)
	}
	if code, _, errOut := b.run(with("video-mode", "4320p60", "--yes")...); code != exitUsage || !strings.Contains(errOut, "not one of") {
		t.Errorf("unknown mode: %d %q", code, errOut)
	}
	if code, _, errOut := b.run(with("video-mode", "1080p25", "--yes")...); code != exitUsage ||
		!strings.Contains(errOut, "mode 1080p25: the switcher supports 1080p50, 525i5994") {
		t.Errorf("unsupported mode: %d %q", code, errOut)
	}
	for _, c := range []struct {
		args       []string
		code, hex  string
		confirmed  bool
		unverified string // the stderr line's reason, "" for none
	}{
		{[]string{"video-mode", "NTSC", "--yes"}, "CVdM", "00000000", true, ""},
		{[]string{"startup", "save", "--yes"}, "SRsv", "", false, "atemwire restored it"},
		{[]string{"startup", "clear", "--yes"}, "SRcl", "", false, "atemwire restored it"},
		{[]string{"time-of-day", "--time", "2026-09-11T12:00:00Z"}, "SToD", "6aa3ed4000000000", false, "atemwire restored it"},
		{[]string{"time-request"}, "TiRq", "", false, ""},
		{[]string{"auto-video-mode", "on"}, "AiVM", "01000000", true, "atemwire restored it"},
		{[]string{"camera", "1", "0", "2", "--type", "fixed16", "--data", "0.5"}, "CCmd", "", true, "atemwire restored it"},
		{[]string{"multiviewer", "input", "3", "CAM2"}, "CMvI", "00030002", true, "atemwire restored it"},
		{[]string{"multiviewer", "properties", "--swap"}, "CMvP", "", true, "atemwire restored it"},
		{[]string{"supersource", "box", "1", "--source", "cam1"}, "CSBP", "", true, "atemwire restored it"},
		{[]string{"supersource", "properties", "--fill-source", "MP1"}, "CSSc", "", true, "atemwire restored it"},
		{[]string{"recording", "settings", "--filename", "show"}, "CRMS", "", true, "atemwire restored it"},
		{[]string{"recording", "state", "on"}, "RcTM", "", true, "atemwire restored it"},
		{[]string{"streaming", "service", "--name", "YouTube"}, "CRSS", "", true, "atemwire restored it"},
		{[]string{"streaming", "audio-bitrate", "128000", "128000"}, "STAB", "", true, "atemwire restored it"},
		{[]string{"streaming", "state", "on"}, "StrR", "", true, "atemwire restored it"},
		{[]string{"hyperdeck-slot", "1", "--address", "192.0.2.50", "--input", "Camera 2"}, "CXMS", "", true, ""},
	} {
		n := len(b.sent(c.code))
		code, out, errOut := b.run(with(c.args...)...)
		if code != 0 {
			t.Errorf("%v: %d %q", c.args, code, errOut)
			continue
		}
		eventually(t, c.code, func() bool { return len(b.sent(c.code)) > n })
		if got := b.lastSent(t, c.code); c.hex != "" && got != c.hex {
			t.Errorf("%v: sent %s %s, want %s", c.args, c.code, got, c.hex)
		}
		status := "sent"
		if c.confirmed {
			status = "confirmed"
		}
		if !strings.Contains(out, status) {
			t.Errorf("%v: %q, want %s", c.args, out, status)
		}
		if got := strings.Contains(errOut, "unverified on hardware"); got != (c.unverified != "") || !strings.Contains(errOut, c.unverified) {
			t.Errorf("%v: stderr %q, want %q", c.args, errOut, c.unverified)
		}
		// --json: the response alone, no stderr line.
		if _, _, errOut := b.run(with(append(c.args, "--json")...)...); errOut != "" {
			t.Errorf("%v --json: stderr %q", c.args, errOut)
		}
	}
	// The mode changed: info shows it.
	if out := b.must(with("info")...); !strings.Contains(out, "525i59.94 4:3 (mode 0)") {
		t.Errorf("info after the mode change:\n%s", out)
	}
	// The clock: TiRq, then the Time packet.
	code, out, errOut := b.run(with("time")...)
	if code != 0 || !strings.Contains(out, "time  10:20:30:05") || errOut != "" {
		t.Errorf("time: %d %q %q", code, out, errOut)
	}
}
