// SPDX-License-Identifier: MIT

package engine

import (
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// Sources are found in order: a number, an alias, a name, the start of a
// name. An alias wins over a name, and says so in a note.
func TestSources(t *testing.T) {
	st := atem.State{Inputs: map[uint16]*atem.InputPropertiesField{}}
	for id, n := range map[uint16][2]string{0: {"BLK", "Black"}, 1: {"lkkk", "Cam_1"}, 2: {"Ca2", "CaM 23"}, 3: {"cam1", "cam69"},
		4: {"Cam4", "Camera 4"}, 5: {"Cam5", "Camera 5"}, 1000: {"BARS", "Color Bars"}, 2001: {"COL1", "Color 1"},
		3010: {"MP1", "Media Player 1"}, 3011: {"MP1K", "Media Player 1 Key"}, 8001: {"AUX1", "Auxiliary 1"}, 10010: {"PGM", "ME 1 Program"}} {
		st.Inputs[id] = &atem.InputPropertiesField{Index: id, ShortName: n[0], Name: n[1]}
	}
	c := &call{state: st, req: Request{Notes: true}, out: discardSink{}}
	r := Sources(atem.Resolver{State: st}, c)
	for typed, want := range map[string]uint64{
		"2": 2, "1301": 1301, "cam2": 2, "CAM2": 2, "camera 2": 2, "Camera2": 2, "in4": 4, "input 4": 4,
		"mp1": 3010, "MP1": 3010, "bars": 1000, "black": 0, "blk": 0, "col1": 2001, "pgm": 10010, "aux1": 8001,
		"Ca2": 2, "cam 23": 2, "lkkk": 1, "LK": 1, "media player 1": 3010, "mp1k": 3011, "Auxil": 8001,
		"cam1": 1, // the alias, not input 3's short name
	} {
		got, err := r.Source(typed)
		if err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", typed, got, err, want)
		}
	}
	for typed, msg := range map[string]string{
		"Cam":    `starts more than one input's name: lkkk, Ca2, cam1, Cam4, Cam5`,
		"zz":     `no input is named "zz" (bmdctl inputs lists them)`,
		"cam9":   `no input is named "cam9"`, // an alias for an input the switcher hasn't
		"media ": "starts more than one",
	} {
		if _, err := r.Source(typed); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q: %v, want %s", typed, err, msg)
		}
	}
	if c.noInput == nil || c.noInput.Typed != "cam9" || len(c.noInput.Inputs) != 6 || c.noInput.Inputs[1].Short != "lkkk" {
		t.Errorf("no input: %+v", c.noInput)
	}
	// Without a switcher (--dry-run): numbers and aliases.
	off := Sources(atem.Resolver{}, nil)
	if id, err := off.Source("cam3"); err != nil || id != 3 {
		t.Errorf("offline alias: %d %v", id, err)
	}
	if _, err := off.Source("lkkk"); err == nil {
		t.Errorf("offline name resolved")
	}
}

type discardSink struct{}

func (discardSink) file(FileFrame) error    { return nil }
func (discardSink) note(Note) error         { return nil }
func (discardSink) response(Response) error { return nil }
