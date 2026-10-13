// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro_test

import (
	"bytes"
	"errors"
	"maps"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// Fly-key Run to Infinity locations in the codec, pinned to bmdwire#22's
// test_flykey_locations.py (b89fcb7). macro/flykey-10.bin is
// FLYKEY_10_MACRO: the ten runs of the ASC capture (atem's flykey_test.go)
// recorded into macro slot 20 and downloaded from the switcher, ten 0x0056
// ops of M/E, keyer, location and marker 0x02. ASC's Save Switcher State
// named its ops as flyKeyClicks does, in order.

var flyKeyClicks = []struct {
	name string
	b    byte
}{
	{"TopLeft", 0x01}, {"TopCentre", 0x02}, {"TopRight", 0x03}, {"MiddleLeft", 0x04},
	{"CentreOfKey", 0x00}, {"MiddleCentre", 0x05}, {"MiddleRight", 0x06},
	{"BottomLeft", 0x07}, {"BottomCentre", 0x08}, {"BottomRight", 0x09},
}

func flyKeyOp(location string) macro.Op {
	return macro.Op{ID: "FlyKeyRunToInfinity", Params: []macro.Param{
		{Name: "mixEffectBlockIndex", Value: "0"}, {Name: "keyIndex", Value: "0"}, {Name: "location", Value: location}}}
}

// test_one_table_for_the_command_and_the_macro_codec: the codec's copy is
// the live command's table.
func TestFlyKeyLocationNamesMatchAtem(t *testing.T) {
	want := map[int]string{}
	for b, name := range atem.FlyKeyLocationNames {
		want[int(b)] = name
	}
	if !maps.Equal(macro.FlyKeyLocationNames, want) {
		t.Errorf("macro %v\natem  %v", macro.FlyKeyLocationNames, want)
	}
}

// test_recorded_macro_decodes_to_ascs_names_in_click_order and
// test_recorded_macro_encodes_back_byte_for_byte.
func TestRecordedFlyKeyMacro(t *testing.T) {
	raw := fixtures.Bytes(t, "macro/flykey-10.bin")
	if len(raw) != 80 {
		t.Fatalf("%d bytes", len(raw))
	}
	ops, err := macro.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != len(flyKeyClicks) {
		t.Fatalf("%d ops: %v", len(ops), ops)
	}
	for i, c := range flyKeyClicks {
		want := flyKeyOp(c.name)
		if ops[i].ID != want.ID || len(ops[i].Params) != 3 || ops[i].Params[0] != want.Params[0] ||
			ops[i].Params[1] != want.Params[1] || ops[i].Params[2] != want.Params[2] {
			t.Errorf("op %d: %v, want %v", i, ops[i], want)
		}
	}
	b, err := macro.Encode(ops)
	if err != nil || !bytes.Equal(b, raw) {
		t.Errorf("re-encoded: %x %v\nwant %x", b, err, raw)
	}
}

// test_each_name_encodes_to_its_byte: MiddleCentre used to raise "unknown
// FlyKey location".
func TestEachFlyKeyLocationEncodesToItsByte(t *testing.T) {
	for _, c := range flyKeyClicks {
		b, err := macro.EncodeOp(flyKeyOp(c.name))
		if want := []byte{0x08, 0x00, 0x56, 0x00, 0x00, 0x00, c.b, 0x02}; err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s: %x %v, want %x", c.name, b, err, want)
		}
	}
}

// test_an_unknown_name_still_refuses_to_encode.
func TestUnknownFlyKeyLocationRefuses(t *testing.T) {
	_, err := macro.EncodeOp(flyKeyOp("Centre"))
	var e *macro.Error
	if !errors.As(err, &e) || e.Class != "ValueError" || e.Message != "unknown FlyKey location 'Centre'" {
		t.Errorf("%v", err)
	}
}
