// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
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
		{[]string{"audio", "master", "--volume", "-6"}, "CFMP", "", true},
		{[]string{"audio", "strip", "Camera 1", "--channel", "-1", "--volume", "-10", "--mix", "On"}, "CFSP", "", true},
		{[]string{"audio", "eq", "1", "1", "--channel", "-1", "--gain", "3"}, "CEBP", "", true},
		{[]string{"audio", "master-eq", "1", "--enabled"}, "CMBP", "", true},
		{[]string{"audio", "compressor", "1", "--channel", "-1", "--threshold", "-20"}, "CICP", "", true},
		{[]string{"audio", "limiter", "1", "--channel", "-1", "--enabled"}, "CILP", "", true},
		{[]string{"audio", "expander", "1", "--channel", "-1", "--mode", "gate"}, "CIXP", "", true},
		{[]string{"audio", "master-compressor", "--ratio", "4"}, "CMCP", "", true},
		{[]string{"audio", "master-limiter", "--enabled", "false"}, "CMLP", "", true},
		{[]string{"audio", "levels", "false"}, "SFLN", "00000000", false},
		{[]string{"classic-audio", "input", "cam1", "--afv"}, "CAMI", "", true},
		{[]string{"classic-audio", "master", "--volume", "32768"}, "CAMM", "", true},
		{[]string{"classic-audio", "monitor", "--mute"}, "CAMm", "", true},
		{[]string{"classic-audio", "levels", "false"}, "SALN", "00000000", false},
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
	// A strip by input name: the Fairlight strip's source is Camera 1's id.
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

// fairlightCodes are the fixture dump's Fairlight mixer packets.
var fairlightCodes = map[string]bool{"FAMP": true, "FAIP": true, "FASP": true, "AEBP": true, "AICP": true, "AILP": true,
	"AMBP": true, "MOCP": true, "AMLP": true, "FMHP": true, "FAMS": true}

// audioDump is the fixture dump without its Fairlight mixer's packets,
// with extra packets in before its last (InCm).
func audioDump(t *testing.T, extra ...[]byte) []byte {
	d := fixtures.Bytes(t, "dump/initial-dump.bin")
	var out []byte
	for i := 0; i < len(d); {
		n := int(binary.BigEndian.Uint16(d[i:]))
		code := string(d[i+4 : i+8])
		if code == "InCm" {
			for _, x := range extra {
				out = append(out, x...)
			}
		}
		if !fairlightCodes[code] {
			out = append(out, d[i:i+n]...)
		}
		i += n
	}
	return out
}

// audio master, levels and meters reach whichever mixer the switcher
// reports: its capabilities (_FAC, _AMC) first, then its state.
func TestAudioFollowsMixer(t *testing.T) {
	fac, amc := fakeatem.Command("_FAC", make([]byte, 4)), fakeatem.Command("_AMC", make([]byte, 4))
	ammo := fakeatem.Command("AMMO", make([]byte, 8))
	for _, c := range []struct {
		name, mixer string
		dump        []byte
	}{
		{"Fairlight state", "fairlight", fixtures.Bytes(t, "dump/initial-dump.bin")},
		{"_AMC", "classic", audioDump(t, amc)},
		{"classic state", "classic", audioDump(t, ammo)},
		{"_FAC over classic state", "fairlight", audioDump(t, fac, ammo)},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBenchDump(t, c.dump)
			var info struct {
				Result engine.InfoResult `json:"result"`
			}
			json.Unmarshal([]byte(b.must(with("info", "--json")...)), &info)
			if info.Result.Audio != c.mixer {
				t.Errorf("info: audio %q", info.Result.Audio)
			}
			var meters struct {
				Result engine.MetersResult `json:"result"`
			}
			json.Unmarshal([]byte(b.must(with("audio", "meters", "--for", "0.1", "--json")...)), &meters)
			if meters.Result.Mixer != c.mixer {
				t.Errorf("meters: mixer %q", meters.Result.Mixer)
			}
			if c.mixer == "fairlight" {
				b.mustSend(t, "CFMP", with("audio", "master", "--volume", "-6")...)
				b.mustSend(t, "SFLN", with("audio", "levels", "on")...)
				return
			}
			// The classic master, with -6 dB on its scale (32768 is 0 dB),
			// shown as what was sent, with the unverified command's notice.
			n := len(b.sent("CAMM"))
			code, out, errOut := b.run(with("audio", "master", "--volume", "-6", "--afv")...)
			if code != 0 || len(b.sent("CAMM")) != n+1 || b.lastSent(t, "CAMM") != "0500402700000100" {
				t.Fatalf("audio master: %d %q %q", code, out, errOut)
			}
			if !strings.HasPrefix(out, "classic-audio master") || !strings.Contains(out, "volume 16423, afv true") ||
				!strings.Contains(errOut, "classic-audio master is unverified") {
				t.Errorf("audio master: %q %q", out, errOut)
			}
			if out := b.must(with("audio", "master", "--volume", "-inf", "--json")...); !strings.Contains(out, `"sent":"classic-audio master"`) ||
				b.lastSent(t, "CAMM") != "0100000000000000" {
				t.Errorf("-inf: %q %s", out, b.lastSent(t, "CAMM"))
			}
			if code, _, errOut := b.run(with("audio", "master", "--eq", "on")...); code != exitUsage ||
				!strings.Contains(errOut, "--eq: the switcher's audio mixer is the classic one, and classic-audio master has no --eq") {
				t.Errorf("--eq: %d %q", code, errOut)
			}
			if _, got := b.mustSend(t, "SALN", with("audio", "levels", "on")...); got != "01000000" {
				t.Errorf("audio levels sent SALN %s", got)
			}
		})
	}
	// classic-audio forces the classic mixer's command on a Fairlight
	// switcher.
	b := newBench(t)
	var meters struct {
		Result engine.MetersResult `json:"result"`
	}
	json.Unmarshal([]byte(b.must(with("classic-audio", "meters", "--for", "0.1", "--json")...)), &meters)
	if meters.Result.Mixer != "classic" || len(b.sent("SALN")) != 2 {
		t.Errorf("classic-audio meters: %+v, SALN %x", meters.Result, b.sent("SALN"))
	}
}
