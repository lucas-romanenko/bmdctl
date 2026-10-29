// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/hex"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Fly-key Run to Infinity locations, pinned to the capture of ATEM Software
// Control in bmdwire#22's test_flykey_locations.py (1bee7d4): ASC 9.3.2 ran
// USK 1 of M/E 1 on a 1 M/E Constellation HD to each of its ten locations;
// each RFlK carried the location at payload byte 5 and KeFS echoed it.
// atem/macro's flykey_test.go pins the same ten runs recorded as a macro.

// flyKeyClicks is CLICKS: ASC's name for each run, in click order, and the
// byte its RFlK carried.
var flyKeyClicks = []struct {
	name string
	b    uint8
}{
	{"TopLeft", 0x01}, {"TopCentre", 0x02}, {"TopRight", 0x03}, {"MiddleLeft", 0x04},
	{"CentreOfKey", 0x00}, {"MiddleCentre", 0x05}, {"MiddleRight", 0x06},
	{"BottomLeft", 0x07}, {"BottomCentre", 0x08}, {"BottomRight", 0x09},
}

// test_one_table_for_the_command_and_the_macro_codec (the codec's copy is
// checked in atem/macro).
func TestFlyKeyLocationNamesAreTheCapture(t *testing.T) {
	want := map[uint8]string{}
	for _, c := range flyKeyClicks {
		want[c.b] = c.name
	}
	if !maps.Equal(FlyKeyLocationNames, want) {
		t.Errorf("%v, want %v", FlyKeyLocationNames, want)
	}
}

func keyframeRunEntry(t *testing.T) table.Entry {
	t.Helper()
	r := table.New()
	if err := RegisterCommands(r); err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Entries("atem") {
		if e.Path == "usk keyframe-run" {
			return e
		}
	}
	t.Fatal("no usk keyframe-run entry")
	return table.Entry{}
}

// test_rflk_payload_matches_the_capture: by name or by byte, the RFlK of
// each run is the capture's, but for padding byte 3 (ASC 0x6E, atemwire 0).
func TestRFlKPayloadMatchesTheCapture(t *testing.T) {
	e := keyframeRunEntry(t)
	for _, c := range flyKeyClicks {
		want := "0200000004" + hex.EncodeToString([]byte{c.b}) + "0000"
		if got := hex.EncodeToString(emit(t, KeyerKeyframeRun{RunTo: KeyframeRunToInfinite, Direction: Set(c.b)})[8:]); got != want {
			t.Errorf("%s: RFlK %s, want %s", c.name, got, want)
		}
		for _, arg := range []string{c.name, strconv.Itoa(int(c.b))} {
			a, err := e.Parse(map[string]string{"keyer": "1", "to": "runToInfinite", "direction": arg}, Resolver{})
			if err != nil {
				t.Errorf("--direction %s: %v", arg, err)
				continue
			}
			cmd, err := e.Build(a)
			if err != nil {
				t.Errorf("--direction %s: %v", arg, err)
				continue
			}
			if got := hex.EncodeToString(emit(t, cmd.(Command))[8:]); got != want {
				t.Errorf("--direction %s: RFlK %s, want %s", arg, got, want)
			}
		}
	}
}

// test_flykey_location_lookup, test_run_flying_key_infinite_direction_unknown_raises:
// a name that isn't one of ASC's is refused; a number goes through as is.
func TestFlyKeyLocationUnknownName(t *testing.T) {
	e := keyframeRunEntry(t)
	_, err := e.Parse(map[string]string{"keyer": "1", "to": "runToInfinite", "direction": "Centre"}, Resolver{})
	if err == nil || !strings.Contains(err.Error(), "MiddleCentre") {
		t.Errorf("Centre: %v", err)
	}
	a, err := e.Parse(map[string]string{"keyer": "1", "to": "runToInfinite", "direction": "200"}, Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := e.Build(a)
	if err != nil {
		t.Fatal(err)
	}
	if b := emit(t, cmd.(Command)); b[13] != 200 {
		t.Errorf("200: RFlK %x", b[8:])
	}
}

// test_kefs_reads_the_echoed_location: the two KeFS per run, the echo (run
// to 4, no flags) and the run's end (at infinite, flag 0x08), carry the
// location at byte 5.
func TestKeFSReadsTheEchoedLocation(t *testing.T) {
	for _, c := range flyKeyClicks {
		echo, err := parseKeFS([]byte{0, 0, 0, 0, 0x04, c.b, 0x00})
		if err != nil {
			t.Fatal(err)
		}
		end, err := parseKeFS([]byte{0, 0, 0, 0, 0x00, c.b, 0x08})
		if err != nil {
			t.Fatal(err)
		}
		if echo.RunToInfiniteIndex != c.b || end.RunToInfiniteIndex != c.b ||
			FlyKeyLocationNames[end.RunToInfiniteIndex] != c.name {
			t.Errorf("%s: echo %d, end %d", c.name, echo.RunToInfiniteIndex, end.RunToInfiniteIndex)
		}
		if echo.AtKeyframeInfinite || !end.AtKeyframeInfinite {
			t.Errorf("%s: at infinite %v then %v", c.name, echo.AtKeyframeInfinite, end.AtKeyframeInfinite)
		}
	}
}
