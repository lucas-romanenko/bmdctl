// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// Group d, end to end against the fake switcher: the Fairlight and
// classic audio commands, and the meter readout.
func TestAudioCommands(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct {
		args      []string
		code, hex string
		confirmed bool
	}{
		{[]string{"fairlight", "master", "--volume", "-6"}, "CFMP", "", true},
		{[]string{"fairlight", "strip", "Camera 1", "--channel", "-1", "--volume", "-10", "--mix", "On"}, "CFSP", "", true},
		{[]string{"fairlight", "eq", "1", "1", "--channel", "-1", "--gain", "3"}, "CEBP", "", true},
		{[]string{"fairlight", "master-eq", "1", "--enabled"}, "CMBP", "", true},
		{[]string{"fairlight", "compressor", "1", "--channel", "-1", "--threshold", "-20"}, "CICP", "", true},
		{[]string{"fairlight", "limiter", "1", "--channel", "-1", "--enabled"}, "CILP", "", true},
		{[]string{"fairlight", "expander", "1", "--channel", "-1", "--mode", "gate"}, "CIXP", "", true},
		{[]string{"fairlight", "master-compressor", "--ratio", "4"}, "CMCP", "", true},
		{[]string{"fairlight", "master-limiter", "--enabled", "false"}, "CMLP", "", true},
		{[]string{"fairlight", "levels", "false"}, "SFLN", "00000000", false},
		{[]string{"audio", "input", "cam1", "--afv"}, "CAMI", "", true},
		{[]string{"audio", "master", "--volume", "32768"}, "CAMM", "", true},
		{[]string{"audio", "monitor", "--mute"}, "CAMm", "", true},
		{[]string{"audio", "levels", "false"}, "SALN", "00000000", false},
	} {
		out, got := b.mustSend(t, c.code, with(c.args...)...)
		if c.hex != "" && got != c.hex {
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
	// A strip by input name: the fairlight strip's source is Camera 1's id.
	if got := b.lastSent(t, "CFSP"); got[4:8] != "0001" {
		t.Errorf("strip source: %s", got)
	}

	// The meter readout turns the stream on, collects, and turns it off.
	n := len(b.sent("SFLN"))
	out := b.must(with("audio", "meters", "--for", "0.2")...)
	for _, want := range []string{"Master", "-10.0  -11.0 dB", "1.0", "Camera 1", "-18.0  -19.0 dB", "peak  -12.0  -13.0", "gr comp -3.0",
		"2001.0", "Media Player 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("meters lack %q:\n%s", want, out)
		}
	}
	if s := b.sent("SFLN"); len(s) != n+2 || s[n][8] != 1 || s[n+1][8] != 0 {
		t.Errorf("meter stream: %x", s[n:])
	}
	var r struct {
		Result engine.MetersResult `json:"result"`
	}
	json.Unmarshal([]byte(b.must(with("audio", "meters", "--json")...)), &r)
	if r.Result.Mixer != "fairlight" || r.Result.Master == nil || len(r.Result.Strips) != 2 || r.Result.Strips[0].Strip != "1.0" ||
		r.Result.Strips[0].Level != [2]float64{-18.01, -19.01} {
		t.Errorf("meters --json: %+v", r.Result)
	}
	if code, _, errOut := b.run(with("audio", "meters", "--for", "0")...); code != exitUsage || !strings.Contains(errOut, "0 to 60 seconds") {
		t.Errorf("--for 0: %d %q", code, errOut)
	}
}
