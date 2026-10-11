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
		{[]string{"program", "cam1"}, "CPgI", "00000001", true},
		{[]string{"preview", "Color Bars"}, "CPvI", "000003e8", true},
		// No echo, but the switcher reports the swap, or the transition
		// starting: confirmed by that.
		{[]string{"cut"}, "DCut", "00000000", true},
		{[]string{"auto", "--me", "1"}, "DAut", "00000000", true},
		{[]string{"ftb", "toggle"}, "FtbA", "00000000", false},
		{[]string{"ftb", "rate", "1:00"}, "FtbC", "01001900", true},
		{[]string{"ftb", "enable", "false"}, "FEna", "00000000", true},
		{[]string{"tbar", "5000"}, "CTPs", "00001388", true},
		{[]string{"transition", "style", "wipe"}, "CTTp", "01000200", true},
		{[]string{"transition", "next", "--next", "background,key2"}, "CTTp", "02000005", true},
		// The dump's selection is background+key1, which the fake keeps.
		{[]string{"transition", "next", "--toggle", "key1"}, "CTTp", "02000001", true},
		{[]string{"transition", "next", "--toggle", "key2"}, "CTTp", "02000007", true},
		// The dump's style is dip: the rate goes to the dip settings.
		{[]string{"transition", "rate", "1:00"}, "CTDp", "0100190000000000", true},
		{[]string{"transition", "settings", "--style", "dve", "--next", "key1"}, "CTTp", "03000302", true},
		{[]string{"transition", "mix", "12"}, "CTMx", "000c0000", true},
		{[]string{"transition", "dip", "--source", "CAM1"}, "CTDp", "0200000000010000", true},
		{[]string{"transition", "wipe", "--pattern", "CircleIris"}, "CTWp", "0002000007000000000000000000000000000000", true},
		{[]string{"transition", "dve", "--style", "PushLeft", "--reverse"}, "CTDv", "04040000001b0000000000000000000000010000", true},
		{[]string{"transition", "stinger", "--duration", "2:00"}, "CTSt", "0040000000000000000000000000003200000000", true},
		{[]string{"transition", "preview", "on"}, "CTPr", "00010000", true},
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
	b.must(with("program", "1")...)
	b.must(with("preview", "2")...)
	b.mustSend(t, "DCut", with("cut")...)
	out := b.must(with("status")...)
	if !strings.Contains(out, "program 2 (Camera 2)") || !strings.Contains(out, "preview 1 (Camera 1)") {
		t.Errorf("after cut:\n%s", out)
	}
	// The unverified line goes to stderr, not with --json.
	if _, _, errOut := b.run(with("tbar", "0")...); !strings.Contains(errOut, "tbar is unverified on hardware") {
		t.Errorf("tbar stderr %q", errOut)
	}
	if _, _, errOut := b.run(with("tbar", "0", "--json")...); errOut != "" {
		t.Errorf("tbar --json stderr %q", errOut)
	}
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"program", "1", "--me", "2"}, "--me 2: the switcher has 1 M/E"},
		{[]string{"usk", "5", "on-air", "on"}, "keyer 5: M/E 1 has 4 upstream keyers, 1-4"},
		{[]string{"dsk", "on-air", "on", "--dsk", "3"}, "--dsk 3: the switcher has 2 downstream keyers, 1-2"},
		{[]string{"transition", "next"}, "give --next or --toggle"},
		{[]string{"transition", "next", "--next", "key1", "--toggle", "key2"}, "give --next or --toggle"},
		{[]string{"transition", "next", "--next", "0"}, "selects no layer"},
		{[]string{"transition", "rate", "300"}, "does not fit a dip rate"},
		{[]string{"transition", "style", "spin"}, "not one of mix, dip, wipe, dve, stinger"},
	} {
		if code, _, errOut := b.run(with(c.args...)...); code != exitUsage || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %q", c.args, code, errOut, c.msg)
		}
	}
}

// A write the switcher holds already sends nothing, and says so: the
// fake answers nothing here, so anything sent would go unconfirmed.
// --json has "already": true; a terminal says already where the time
// goes (terminal_test.go).
func TestAlready(t *testing.T) {
	b := newBench(t)
	b.must(with("program", "1")...)
	sw := b.switchers()[0]
	sw.Unanswered("CPgI", "CPvI", "CTTp", "CAuS")
	n := len(sw.Commands())
	for _, line := range [][]string{
		{"program", "cam1"},
		{"preview", "1"},               // the dump's preview
		{"transition", "style", "dip"}, // the dump's style: an operation
		{"aux", "2", "pgm"},
	} {
		code, out, errOut := b.run(with(append(line, "--json")...)...)
		if code != 0 || !strings.Contains(out, `"confirmed":true`) || !strings.Contains(out, `"already":true`) {
			t.Errorf("%v: %d %s %s", line, code, out, errOut)
		}
	}
	if sent := sw.Commands()[n:]; len(sent) != 0 {
		t.Errorf("sent %d commands", len(sent))
	}
	// A value it doesn't hold is sent, and the silent switcher leaves it
	// unconfirmed.
	if code, _, _ := b.run(with("program", "2", "--timeout", "200ms")...); code != exitNoEcho {
		t.Errorf("a change: exit %d", code)
	}
}
