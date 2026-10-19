// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// oracleCase is one entry of internal/fixtures/oracle/recv.json: a payload
// fed to bmdwire v1.6.0's AtemProtocol.save_field_data and what it did.
type oracleCase struct {
	Code    string         `json:"code"`
	Payload string         `json:"payload"`
	Result  string         `json:"result"` // error, consumed, raw, stored
	Error   string         `json:"error"`  // the exception's class, for an error
	Name    string         `json:"name"`   // mixerstate key
	Key     string         `json:"key"`
	Value   map[string]any `json:"value"`
}

// Every state packet behaves as atemwire's save_field_data: the same
// errors, the same consumed and raw codes, the same keys and the same
// attribute values, stored where the key says.
func TestParsersMatchAtemwire(t *testing.T) {
	var oracle struct {
		Bmdwire string       `json:"bmdwire"`
		Cases   []oracleCase `json:"cases"`
	}
	if err := json.Unmarshal(fixtures.Bytes(t, "oracle/recv.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Bmdwire != "v1.6.0" {
		t.Fatalf("oracle generated from bmdwire %s", oracle.Bmdwire)
	}
	codes := map[string]bool{}
	for i, c := range oracle.Cases {
		codes[c.Code] = true
		payload, err := hex.DecodeString(c.Payload)
		if err != nil {
			t.Fatal(err)
		}
		where := fmt.Sprintf("case %d %s %s", i, c.Code, c.Payload)
		c = deliberate(c, payload)
		st := newState()
		ch, err := st.store(c.Code, payload)
		switch c.Result {
		case "error":
			if err == nil {
				t.Errorf("%s: atemwire raises, Go stored %+v", where, ch)
			}
			continue
		case "consumed":
			if err != nil || ch != nil {
				t.Errorf("%s: atemwire consumes it, Go: %+v, %v", where, ch, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", where, err)
			continue
		}
		if ch == nil {
			t.Errorf("%s: Go stored nothing", where)
			continue
		}
		if ch.Key != c.Key || ch.Opcode != c.Code {
			t.Errorf("%s: key %q, want %q", where, ch.Key, c.Key)
		}
		if c.Result == "raw" {
			if b, ok := ch.Value.([]byte); !ok || hex.EncodeToString(b) != c.Payload || hex.EncodeToString(st.Raw[c.Code]) != c.Payload {
				t.Errorf("%s: raw value %v", where, ch.Value)
			}
			continue
		}
		if p := parsers[c.Code]; p.name != c.Name {
			t.Errorf("%s: mixerstate key %q, want %q", where, p.name, c.Name)
		}
		got := normalize(reflect.ValueOf(ch.Value), "")
		if d := diff(got, any(c.Value), ""); d != "" {
			t.Errorf("%s: %s", where, d)
		}
		if k := findInTree(st, c.Code, ch.Value); k != c.Key {
			t.Errorf("%s: stored at %q in the tree, want %q", where, k, c.Key)
		}
	}
	for code := range parsers {
		if !codes[code] {
			t.Errorf("%s: no oracle cases", code)
		}
	}
	t.Logf("%d cases over %d codes", len(oracle.Cases), len(codes))
}

// deliberate rewrites an oracle case to what Go must do where bmdctl
// differs from bmdwire v1.6.0 on purpose (docs/PORTING.md, "Deliberate
// differences").
func deliberate(c oracleCase, payload []byte) oracleCase {
	switch c.Code {
	case "LKST":
		// bmdwire#15: atemwire stores a lock taken and a release under one
		// bare key, the latest whatever its store; here each store keeps
		// its own.
		if c.Result == "stored" {
			store := uint16(payload[0])<<8 | uint16(payload[1])
			c.Key = fmt.Sprintf("LKST:%d", store)
		}
	case "InCm":
		// bmdwire#13: atemwire's save_field_data raises
		// IncompleteStateDump at an InCm with no video mode stored, for its
		// loop() to re-handshake. Here the session makes that check
		// (Session.onData) and the state stores InCm as it always does.
		if c.Error == "IncompleteStateDump" {
			c.Result, c.Error, c.Key = "raw", "", "InCm"
		}
	}
	return c
}

// byteStrings are string fields holding bytes atemwire leaves undecoded;
// the oracle has them in hex.
var byteStrings = map[string]bool{
	"MediaplayerFileInfoField.Name":    true,
	"MacroPropertiesField.Name":        true,
	"MacroPropertiesField.Description": true,
}

// normalize turns a Go value into the oracle's JSON shape.
func normalize(v reflect.Value, field string) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return normalize(v.Elem(), field)
	case reflect.Struct:
		out := map[string]any{}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			out[f.Tag.Get("json")] = normalize(v.Field(i), v.Type().Name()+"."+f.Name)
		}
		return out
	case reflect.Array, reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, v.Len())
			reflect.Copy(reflect.ValueOf(b), v)
			return hex.EncodeToString(b)
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := []any{}
		for i := range v.Len() {
			out = append(out, normalize(v.Index(i), field))
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		for it := v.MapRange(); it.Next(); {
			out[fmt.Sprint(it.Key().Interface())] = normalize(it.Value(), field)
		}
		return out
	case reflect.String:
		if byteStrings[field] {
			return hex.EncodeToString([]byte(v.String()))
		}
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float64:
		return v.Float()
	}
	panic("normalize: " + v.Kind().String())
}

// diff compares a normalized Go value with the oracle's, reporting the
// first difference. Floats may differ by an ulp or two: Go's math.Log10
// is not CPython's libm log10 (the classic mixer's meters).
func diff(got, want any, path string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: got %v, want object", path, got)
		}
		var keys []string
		for k := range w {
			keys = append(keys, k)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range slices.Compact(keys) {
			if d := diff(g[k], w[k], path+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: got %v, want %v", path, got, want)
		}
		for i := range w {
			if d := diff(g[i], w[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	case float64:
		g, ok := got.(float64)
		if !ok || math.Abs(g-w) > 1e-12*math.Max(1, math.Abs(w)) {
			return fmt.Sprintf("%s: got %v, want %v", path, got, want)
		}
		return ""
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Sprintf("%s: got %#v, want %#v", path, got, want)
	}
	return ""
}

// findInTree finds the pointer v under the State field tagged with code and
// returns the key its position spells (code plus slice indices and map
// keys on the way down from that field), or "" if it isn't there.
func findInTree(s State, code string, v any) string {
	target := reflect.ValueOf(v).Pointer()
	var found string
	var walk func(rv reflect.Value, path []string, inTagged bool)
	walk = func(rv reflect.Value, path []string, inTagged bool) {
		if found != "" {
			return
		}
		switch rv.Kind() {
		case reflect.Pointer:
			if rv.IsNil() {
				return
			}
			if inTagged && rv.Pointer() == target {
				found = strings.Join(append([]string{code}, path...), ":")
				return
			}
			walk(rv.Elem(), path, inTagged)
		case reflect.Struct:
			for i := range rv.NumField() {
				f := rv.Type().Field(i)
				tag, tagged := f.Tag.Lookup("atem")
				if tagged && tag != code {
					continue
				}
				walk(rv.Field(i), path, inTagged || tagged)
			}
		case reflect.Slice:
			for i := range rv.Len() {
				walk(rv.Index(i), append(slices.Clone(path), fmt.Sprint(i)), inTagged)
			}
		case reflect.Map:
			for it := rv.MapRange(); it.Next(); {
				walk(it.Value(), append(slices.Clone(path), fmt.Sprint(it.Key().Interface())), inTagged)
			}
		}
	}
	walk(reflect.ValueOf(s), nil, false)
	return found
}
