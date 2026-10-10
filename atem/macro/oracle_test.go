// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// The oracle, internal/fixtures/oracle/macro.json, records what bmdwire
// v1.3.0's codec does; gen_macro.py beside it says how.

type oracleResult struct {
	Bytes   *string         `json:"bytes"`
	Len     int             `json:"len"`
	SHA256  string          `json:"sha256"`
	Error   string          `json:"error"`
	Message json.RawMessage `json:"message"`
}

type oracleOp struct {
	ID     string              `json:"id"`
	Params [][]json.RawMessage `json:"params"`
}

type decodeCase struct {
	Kind   string       `json:"kind"` // file, op, stream
	Name   string       `json:"name"`
	Raw    string       `json:"raw"`
	Ops    []oracleOp   `json:"ops"`
	Stop   string       `json:"stop"`
	Encode oracleResult `json:"encode"`
}

type encodeCase struct {
	ID     string              `json:"id"`
	Params [][]json.RawMessage `json:"params"`
	oracleResult
}

type oracle struct {
	Bmdwire string       `json:"bmdwire"`
	Python  string       `json:"python"`
	Decode  []decodeCase `json:"decode"`
	Encode  []encodeCase `json:"encode"`
}

func loadOracle(t *testing.T) oracle {
	t.Helper()
	var o oracle
	if err := json.Unmarshal(fixtures.Bytes(t, "oracle/macro.json"), &o); err != nil {
		t.Fatal(err)
	}
	if o.Bmdwire != "v1.3.0" {
		t.Fatalf("oracle generated from bmdwire %s", o.Bmdwire)
	}
	return o
}

// str reads a JSON string, or the compact {"head", "repeat", "times",
// "tail"} form the generator writes long strings in.
func str(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var r struct {
		Head, Repeat, Tail string
		Times              int
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Times == 0 {
		t.Fatalf("bad oracle string %s", raw)
	}
	return r.Head + strings.Repeat(r.Repeat, r.Times) + r.Tail
}

func (o oracleOp) op(t *testing.T) Op {
	t.Helper()
	op := Op{ID: o.ID, Params: []Param{}}
	for _, kv := range o.Params {
		op.Params = append(op.Params, Param{str(t, kv[0]), str(t, kv[1])})
	}
	return op
}

func sameOps(got, want []Op) string {
	if len(got) != len(want) {
		return fmt.Sprintf("%d ops, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		g, w := got[i], want[i]
		if g.ID != w.ID || len(g.Params) != len(w.Params) {
			return fmt.Sprintf("op %d: %v, want %v", i, g, w)
		}
		for j := range g.Params {
			if g.Params[j] != w.Params[j] {
				return fmt.Sprintf("op %d %s: param %d is %q=%q, want %q=%q", i, g.ID, j,
					g.Params[j].Name, g.Params[j].Value, w.Params[j].Name, w.Params[j].Value)
			}
		}
	}
	return ""
}

// checkResult compares Encode's or EncodeOp's outcome with atemwire's: the
// same bytes, or an *Error with the same class and message.
func checkResult(t *testing.T, where string, got []byte, err error, want oracleResult) {
	t.Helper()
	if want.Error != "" {
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("%s: atemwire raises %s, Go: %x, %v", where, want.Error, got, err)
			return
		}
		if msg := str(t, want.Message); e.Class != want.Error || e.Message != msg {
			t.Errorf("%s: %s: %q, want %s: %q", where, e.Class, e.Message, want.Error, msg)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: %v", where, err)
		return
	}
	if want.SHA256 != "" {
		if sum := sha256.Sum256(got); len(got) != want.Len || hex.EncodeToString(sum[:]) != want.SHA256 {
			t.Errorf("%s: %d bytes, sha256 %x; want %d bytes, %s", where, len(got), sum, want.Len, want.SHA256)
		}
		return
	}
	if want.Bytes == nil {
		t.Fatalf("%s: oracle case has no result", where)
	}
	if h := hex.EncodeToString(got); h != *want.Bytes {
		t.Errorf("%s: %s, want %s", where, h, *want.Bytes)
	}
}

// Every bytecode decodes to the ops atemwire's decode_macro_bytecode
// returns (ids, attribute names and order, values), stops where it stops
// with its warning, and re-encodes as encode_macro_bytecode does.
func TestDecodeMatchesAtemwire(t *testing.T) {
	o := loadOracle(t)
	decoded := map[string]bool{} // op ids decoded from an op case
	failed := map[uint16]bool{}  // op codes a decoder raised on
	counts := map[string]int{}
	for i, c := range o.Decode {
		counts[c.Kind]++
		raw, err := hex.DecodeString(c.Raw)
		if err != nil {
			t.Fatal(err)
		}
		where := fmt.Sprintf("decode case %d (%s %s) %s", i, c.Kind, c.Name, c.Raw)
		want := []Op{}
		for _, op := range c.Ops {
			want = append(want, op.op(t))
		}
		ops, err := Decode(raw)
		if d := sameOps(ops, want); d != "" {
			t.Errorf("%s: %s", where, d)
		}
		switch {
		case c.Stop == "" && err != nil:
			t.Errorf("%s: %v; atemwire decodes it all", where, err)
		case c.Stop != "" && (err == nil || err.Error() != c.Stop):
			t.Errorf("%s: error %v, want %q", where, err, c.Stop)
		}
		b, err := Encode(ops)
		checkResult(t, where+" re-encoded", b, err, c.Encode)
		if c.Kind == "op" {
			for _, op := range ops {
				decoded[op.ID] = true
				if _, ok := op.Get("decodeError"); ok {
					var code uint16
					fmt.Sscanf(op.ID, "Unknown_0x%04x", &code)
					failed[code] = true
				}
			}
		}
	}
	for _, c := range knownOps {
		if !decoded[c.id] {
			t.Errorf("%s: no oracle case decodes it", c.id)
		}
		if !failed[c.code] {
			t.Errorf("%s: no oracle case where its decoder fails", c.id)
		}
	}
	if !decoded["Unknown_0x9999"] {
		t.Error("no oracle case with an unknown op code")
	}
	samples, err := fs.ReadDir(fixtures.FS(), "macro")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range samples {
		if !slices.ContainsFunc(o.Decode, func(c decodeCase) bool { return c.Kind == "file" && c.Name == f.Name() }) {
			t.Errorf("macro/%s: no oracle case", f.Name())
		}
	}
	t.Logf("%d decode cases %v, recorded with Python %s", len(o.Decode), counts, o.Python)
}

// Every op dict encodes as atemwire's encode_single_op encodes it, or
// fails where it raises, with the same exception class and message.
func TestEncodeMatchesAtemwire(t *testing.T) {
	o := loadOracle(t)
	ok, raised := map[string]bool{}, map[string]bool{}
	nerr := 0
	for i, c := range o.Encode {
		op := oracleOp{c.ID, c.Params}.op(t)
		where := fmt.Sprintf("encode case %d %s %q", i, c.ID, op.Params)
		if len(where) > 300 {
			where = where[:300] + "..."
		}
		b, err := EncodeOp(op)
		checkResult(t, where, b, err, c.oracleResult)
		if c.Error != "" {
			raised[c.ID] = true
			nerr++
		} else {
			ok[c.ID] = true
		}
	}
	for _, c := range knownOps {
		if !ok[c.id] {
			t.Errorf("%s: no oracle case encodes it", c.id)
		}
		if !raised[c.id] {
			t.Errorf("%s: no oracle case where its encoder raises", c.id)
		}
	}
	t.Logf("%d encode cases, %d raising", len(o.Encode), nerr)
}

// The table has atemwire's 146 op codes, each id once.
func TestKnownOps(t *testing.T) {
	if len(knownOps) != 146 || len(byCode) != 146 || len(byID) != 146 {
		t.Fatalf("%d ops, %d codes, %d ids; want 146", len(knownOps), len(byCode), len(byID))
	}
}
