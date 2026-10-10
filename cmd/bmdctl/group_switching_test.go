// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"testing"
)

// lastSent is the payload of the last command with this opcode the
// switchers received, as hex.
func (b *bench) lastSent(t *testing.T, code string) string {
	t.Helper()
	c := b.sent(code)
	if len(c) == 0 {
		t.Fatalf("no %s sent", code)
	}
	return fmt.Sprintf("%x", c[len(c)-1][8:])
}

// mustSend runs a command line and waits until the switcher has received
// one more command with this opcode: a command without an echo returns
// before it is on the wire. It returns stdout and the command's payload.
func (b *bench) mustSend(t *testing.T, code string, args ...string) (string, string) {
	t.Helper()
	n := len(b.sent(code))
	out := b.must(args...)
	eventually(t, code+" at the switcher", func() bool { return len(b.sent(code)) > n })
	return out, b.lastSent(t, code)
}

// Group b, end to end against the fake switcher: program, preview, cut,
// auto, ftb, tbar and the transitions.
func TestSwitchingCommands(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		args      []string
		code, hex string
		confirmed bool
	}{
		{[]string{"program", "--source", "cam2"}, "CPgI", "00000002", true},
		{[]string{"preview", "--source", "Color Bars"}, "CPvI", "000003e8", true},
		{[]string{"cut"}, "DCut", "00000000", false},
		{[]string{"auto", "--me", "1"}, "DAut", "00000000", false},
		{[]string{"ftb", "toggle"}, "FtbA", "00000000", false},
		{[]string{"ftb", "rate", "--rate", "1:00"}, "FtbC", "01001900", true},
		{[]string{"ftb", "enable", "--enabled", "false"}, "FEna", "00000000", true},
		{[]string{"tbar", "--position", "5000"}, "CTPs", "00001388", true},
		{[]string{"transition", "style", "--style", "wipe"}, "CTTp", "01000200", true},
		{[]string{"transition", "next", "--next", "background,key1"}, "CTTp", "02000003", true},
		// The dump's selection is background+key1.
		{[]string{"transition", "next", "--toggle", "key1"}, "CTTp", "02000001", true},
		{[]string{"transition", "next", "--toggle", "key2"}, "CTTp", "02000007", true},
		// The dump's style is dip: the rate goes to the dip settings.
		{[]string{"transition", "rate", "--rate", "1:00"}, "CTDp", "0100190000000000", true},
		{[]string{"transition", "settings", "--style", "dve", "--next", "key1"}, "CTTp", "03000302", true},
		{[]string{"transition", "mix", "--rate", "12"}, "CTMx", "000c0000", true},
		{[]string{"transition", "dip", "--source", "CAM1"}, "CTDp", "0200000000010000", true},
		{[]string{"transition", "wipe", "--pattern", "CircleIris"}, "CTWp", "0002000007000000000000000000000000000000", true},
		{[]string{"transition", "dve", "--style", "PushLeft", "--reverse"}, "CTDv", "04040000001b0000000000000000000000010000", true},
		{[]string{"transition", "stinger", "--duration", "2:00"}, "CTSt", "0040000000000000000000000000003200000000", true},
		{[]string{"transition", "preview", "--enabled"}, "CTPr", "00010000", true},
	} {
		out, got := b.mustSend(t, c.code, with(c.args...)...)
		if got != c.hex {
			t.Errorf("%v: sent %s %s, want %s", c.args, c.code, got, c.hex)
		}
		status := "sent"
		if c.confirmed {
			status = "confirmed"
		}
		if !strings.Contains(out, status) {
			t.Errorf("%v: %q, want %s", c.args, out, status)
		}
	}
	// Cut swaps program and preview: status shows it.
	b.must(with("program", "--source", "1")...)
	b.must(with("preview", "--source", "2")...)
	b.mustSend(t, "DCut", with("cut")...)
	out := b.must(with("status")...)
	if !strings.Contains(out, "program 2 (Camera 2)") || !strings.Contains(out, "preview 1 (Camera 1)") {
		t.Errorf("after cut:\n%s", out)
	}
	// The unverified line goes to stderr, not with --json.
	if _, _, errOut := b.run(with("tbar", "--position", "0")...); !strings.Contains(errOut, "tbar is unverified on hardware") {
		t.Errorf("tbar stderr %q", errOut)
	}
	if _, _, errOut := b.run(with("tbar", "--position", "0", "--json")...); errOut != "" {
		t.Errorf("tbar --json stderr %q", errOut)
	}
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"program", "--me", "2", "--source", "1"}, "--me 2: the switcher has 1 M/E"},
		{[]string{"usk", "on-air", "--keyer", "4", "--on-air"}, "--keyer 4: M/E 1 has 4 upstream keyers, 0-3"},
		{[]string{"dsk", "on-air", "--dsk", "2", "--on-air"}, "--dsk 2: the switcher has 2 downstream keyers, 0-1"},
		{[]string{"transition", "next"}, "give --next or --toggle"},
		{[]string{"transition", "next", "--next", "key1", "--toggle", "key2"}, "give --next or --toggle"},
		{[]string{"transition", "next", "--next", "0"}, "selects no layer"},
		{[]string{"transition", "rate", "--rate", "300"}, "does not fit a dip rate"},
		{[]string{"transition", "style", "--style", "spin"}, "not one of mix, dip, wipe, dve, stinger"},
	} {
		if code, _, errOut := b.run(with(c.args...)...); code != exitUsage || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %q", c.args, code, errOut, c.msg)
		}
	}
}
