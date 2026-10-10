// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// One for one, the assertions of bmdwire v1.3.0
// atemwire/tests/test_macro_decoder.py. The bytecode comes from
// internal/fixtures/macro, where the Python tests have it as literals.

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// packOp frames one op as the switcher stores it (the tests' _pack_op).
func packOp(code uint16, params []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(4+len(params)))
	b = binary.LittleEndian.AppendUint16(b, code)
	return append(b, params...)
}

// decodeOne decodes a fixture holding exactly one op (_decode_one).
func decodeOne(t *testing.T, fixture string) Op {
	t.Helper()
	ops, err := Decode(fixtures.Bytes(t, "macro/"+fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %v", ops)
	}
	return ops[0]
}

// wantOp builds the expected op from alternating names and values.
func wantOp(id string, kv ...string) Op {
	o := Op{ID: id, Params: []Param{}}
	for i := 0; i < len(kv); i += 2 {
		o.Params = append(o.Params, Param{kv[i], kv[i+1]})
	}
	return o
}

func assertOp(t *testing.T, got, want Op) {
	t.Helper()
	if got.Params == nil {
		got.Params = []Param{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertParam(t *testing.T, o Op, name, want string) {
	t.Helper()
	if v, ok := o.Get(name); !ok || v != want {
		t.Errorf("%s %s = %q (%v), want %q", o.ID, name, v, ok, want)
	}
}

// size_x = 500 wire (XML 0.5): 32768.
func TestDecoderXSizeUnsigned(t *testing.T) {
	assertOp(t, decodeOne(t, "op0047-0c00470000000c0000800000.bin"),
		wantOp("DVEAndFlyKeyXSize", "mixEffectBlockIndex", "0", "keyIndex", "0", "xSize", "0.5"))
}

// size_y = 1500 wire (XML 1.5): 98304.
func TestDecoderYSizeUnsigned(t *testing.T) {
	assertOp(t, decodeOne(t, "op0048-0c00480000010c0000800100.bin"),
		wantOp("DVEAndFlyKeyYSize", "mixEffectBlockIndex", "0", "keyIndex", "1", "ySize", "1.5"))
}

// pos_x = -16000 wire (XML -16): -1048576.
func TestDecoderXPosSignedNegative(t *testing.T) {
	op := decodeOne(t, "op004a-0c004a0000000c000000f0ff.bin")
	if op.ID != "DVEAndFlyKeyXPosition" {
		t.Errorf("id %s", op.ID)
	}
	assertParam(t, op, "xPosition", "-16")
	assertParam(t, op, "mixEffectBlockIndex", "0")
	assertParam(t, op, "keyIndex", "0")
}

// The live -1048577 keeps its precision rather than rounding to -16.
func TestDecoderXPosOffByOneKeepsPrecision(t *testing.T) {
	assertParam(t, decodeOne(t, "op004a-0c004a0000000c00ffffefff.bin"), "xPosition", "-16.000015")
}

// pos_y = 9000 wire (XML 9): 589824.
func TestDecoderYPosSignedPositive(t *testing.T) {
	assertOp(t, decodeOne(t, "op004b-0c004b0000000c0000000900.bin"),
		wantOp("DVEAndFlyKeyYPosition", "mixEffectBlockIndex", "0", "keyIndex", "0", "yPosition", "9"))
}

func TestDecoderDVEMaskEnableTrue(t *testing.T) {
	assertOp(t, decodeOne(t, "op0035-0800350000020100.bin"),
		wantOp("DVEKeyMaskEnable", "mixEffectBlockIndex", "0", "keyIndex", "2", "enable", "True"))
}

func TestDecoderDVEMaskEnableFalse(t *testing.T) {
	assertParam(t, decodeOne(t, "op0035-0800350000000000.bin"), "enable", "False")
}

// mask_top = -4500 wire (XML -4.5): -294912.
func TestDecoderDVEMaskTopSigned(t *testing.T) {
	assertOp(t, decodeOne(t, "op0036-0c00360000000c000080fbff.bin"),
		wantOp("DVEKeyMaskTop", "mixEffectBlockIndex", "0", "keyIndex", "0", "top", "-4.5"))
}

// mask_bottom = 7200 wire: 471859, 7.19998... to six decimals.
func TestDecoderDVEMaskBottomSigned(t *testing.T) {
	op := decodeOne(t, "op0037-0c00370000000c0033330700.bin")
	if op.ID != "DVEKeyMaskBottom" {
		t.Errorf("id %s", op.ID)
	}
	if v, _ := op.Get("bottom"); !strings.HasPrefix(v, "7.199") {
		t.Errorf("bottom %q", v)
	}
}

func TestDecoderDVEMaskLeftSigned(t *testing.T) {
	op := decodeOne(t, "op0038-0c00380000000c000000f0ff.bin")
	if op.ID != "DVEKeyMaskLeft" {
		t.Errorf("id %s", op.ID)
	}
	assertParam(t, op, "left", "-16")
}

func TestDecoderDVEMaskRightSigned(t *testing.T) {
	assertOp(t, decodeOne(t, "op0039-0c00390000000c0000001000.bin"),
		wantOp("DVEKeyMaskRight", "mixEffectBlockIndex", "0", "keyIndex", "0", "right", "16"))
}

// gain = 50% = XML 0.5: 32768.
func TestDecoderDSKGainUnsigned(t *testing.T) {
	op := decodeOne(t, "op009d-0c009d0000886c0200800000.bin")
	if op.ID != "DownstreamKeyGain" {
		t.Errorf("id %s", op.ID)
	}
	assertParam(t, op, "keyIndex", "0")
	assertParam(t, op, "gain", "0.5")
}

func TestDecoderDSKMaskEnableTrue(t *testing.T) {
	assertOp(t, decodeOne(t, "op009e-08009e0000010c00.bin"),
		wantOp("DownstreamKeyMaskEnable", "keyIndex", "0", "enable", "True"))
}

func TestDecoderDSKPreMultiplyFalse(t *testing.T) {
	assertOp(t, decodeOne(t, "op00a4-0800a40000006c02.bin"),
		wantOp("DownstreamKeyPreMultiply", "keyIndex", "0", "preMultiply", "False"))
}

// Every entry has an id, a decoder and an encoder
// (test_known_ops_table_entries_are_3_tuples).
func TestKnownOpsTableEntriesAreComplete(t *testing.T) {
	for _, c := range knownOps {
		if c.id == "" || c.dec == nil || c.enc == nil {
			t.Errorf("0x%04x %q: missing id, decoder or encoder", c.code, c.id)
		}
	}
}

// test_known_ops_symmetry_module_load_guard has no counterpart: it tests
// _check_known_ops_symmetry, a guard over Python's untyped table that
// nothing calls; the Go table's element type holds both codecs, and the
// test above checks none is nil.

// An op code with no codec survives as Unknown_0x... with its raw
// parameters.
func TestUnknownOpcodeRoundTripsAsUnknown(t *testing.T) {
	ops, err := Decode(fixtures.Bytes(t, "macro/op9999-0800999901020304.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Op{{"Unknown_0x9999", []Param{{"rawParams", "01020304"}}}}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("got %v, want %v", ops, want)
	}
}

// packOp matches the fixture framing.
func TestPackOp(t *testing.T) {
	if got := packOp(0x9999, []byte{1, 2, 3, 4}); string(got) != string(fixtures.Bytes(t, "macro/op9999-0800999901020304.bin")) {
		t.Errorf("%x", got)
	}
}
