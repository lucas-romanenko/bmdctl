// SPDX-License-Identifier: MIT

package table

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeCmd []byte

func (c fakeCmd) Encode() []byte { return c }

func build(Args) (Command, error) { return fakeCmd{1}, nil }

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r := New()
	lo, hi := Bounds(0, 3)
	err := r.Add(
		Entry{Device: "dev", Path: "cut", Summary: "Cut.", Build: build,
			Params: []Param{{Name: "me", Kind: Uint, Min: lo, Max: hi, Default: "0", Help: "M/E"}}},
		Entry{Device: "dev", Path: "wipe set", Summary: "Wipe.", Build: build, AtLeastOne: true, Echo: "TWpP:<me>",
			Params: []Param{
				{Name: "me", Kind: Uint, Default: "0"},
				{Name: "width", Kind: Float, Unit: "%"},
				{Name: "style", Kind: Enum, Names: map[string]uint64{"mix": 0, "dip": 1}},
				{Name: "layers", Kind: Flags, Names: map[string]uint64{"bg": 1, "key1": 2}},
				{Name: "rate", Kind: Rate},
				{Name: "on", Kind: Bool},
				{Name: "when", Kind: Time},
				{Name: "data", Kind: List},
				{Name: "src", Kind: Source},
			}},
		Entry{Device: "dev", Path: "wipe get", Summary: "Get.", Build: build, Unverified: true, Destructive: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAddRefusesBadEntries(t *testing.T) {
	r := testRegistry(t)
	for _, e := range []Entry{
		{Device: "dev", Path: "cut", Build: build},
		{Device: "dev", Path: "wipe", Build: build},
		{Device: "dev", Path: "cut deeper", Build: build},
		{Device: "dev", Path: "x", Build: build, Params: []Param{{Name: "a"}, {Name: "a"}}},
		{Device: "dev", Path: "y", Build: build, Params: []Param{{Name: "e", Kind: Enum}}},
		{Device: "dev", Path: "z"},
	} {
		if err := r.Add(e); err == nil {
			t.Errorf("%q accepted", e.Path)
		}
	}
}

func TestResolveAndChildren(t *testing.T) {
	r := testRegistry(t)
	if e, _, rest := r.Resolve("dev", []string{"wipe", "set", "extra"}); e == nil || e.Path != "wipe set" || len(rest) != 1 {
		t.Fatalf("%v %v", e, rest)
	}
	if e, prefix, _ := r.Resolve("dev", []string{"wipe"}); e != nil || strings.Join(prefix, " ") != "wipe" {
		t.Fatalf("group: %v %v", e, prefix)
	}
	if e, prefix, _ := r.Resolve("dev", []string{"nope"}); e != nil || prefix != nil {
		t.Fatalf("unknown: %v %v", e, prefix)
	}
	if got := r.Children("dev", nil); len(got) != 2 || got[0][0] != "cut" || got[1][0] != "wipe ..." {
		t.Fatalf("%v", got)
	}
	if got := r.Children("dev", []string{"wipe"}); len(got) != 2 || got[0][0] != "get" {
		t.Fatalf("%v", got)
	}
}

// testResolver reads a rate as 25 frames per character and a source as
// its length, refusing "nobody".
type testResolver struct{}

func (testResolver) Rate(s string) (int64, error) { return int64(25 * len(s)), nil }
func (testResolver) Source(s string) (uint64, error) {
	if s == "nobody" {
		return 0, errors.New("no such input")
	}
	return uint64(len(s)), nil
}

func TestParse(t *testing.T) {
	r := testRegistry(t)
	e, _, _ := r.Resolve("dev", []string{"wipe", "set"})
	var rate testResolver
	a, err := e.Parse(map[string]string{"width": "12.5", "style": "dip", "layers": "bg,key1", "rate": "1:00",
		"on": "off", "when": "1789128000", "data": "1,2", "src": "Cam 1"}, rate)
	if err != nil {
		t.Fatal(err)
	}
	if a.Value("me") != uint64(0) || a.Value("width") != 12.5 || a.Value("style") != uint64(1) ||
		a.Value("layers") != uint64(3) || a.Value("rate") != int64(100) || a.Value("on") != false ||
		!a.Value("when").(time.Time).Equal(time.Unix(1789128000, 0)) || len(a.Value("data").([]string)) != 2 ||
		a.Value("src") != uint64(5) {
		t.Fatalf("%+v", a.values)
	}
	for _, raw := range []map[string]string{
		{},                      // nothing to set
		{"nope": "1"},           // unknown
		{"width": "wide"},       // not a number
		{"style": "wipe"},       // not a name
		{"layers": "bg,key9"},   // not a flag
		{"on": "maybe"},         // not a bool
		{"when": "yesterday"},   // not a time
		{"me": "-1", "on": "1"}, // not a uint
		{"src": "nobody"},       // the resolver refuses it
	} {
		if _, err := e.Parse(raw, rate); !errors.Is(err, ErrUsage) {
			t.Errorf("%v: %v", raw, err)
		}
	}
	cut, _, _ := r.Resolve("dev", []string{"cut"})
	if _, err := cut.Parse(map[string]string{"me": "4"}, rate); !errors.Is(err, ErrUsage) {
		t.Errorf("range not checked: %v", err)
	}
}

func TestMarkdown(t *testing.T) {
	md := testRegistry(t).Markdown()
	for _, want := range []string{
		"### `bmdctl dev wipe set`", "Echo: `TWpP:<me>`.", "**Unverified.** **Destructive.**",
		"| `--me` | uint, 0-3 | default 0 | M/E |", "enum: mix=0 dip=1", "flags: bg=1 key1=2",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
}

// A one-based parameter is typed from 1 and stored from 0; its range is
// shown and checked as typed.
func TestOneBased(t *testing.T) {
	r := New()
	lo, hi := Bounds(0, 3)
	if err := r.Add(Entry{Device: "dev", Path: "cut", Build: build,
		Params: []Param{{Name: "me", Kind: Uint, Min: lo, Max: hi, Default: "1", OneBased: true}}}); err != nil {
		t.Fatal(err)
	}
	e := r.Find("dev", "cut")
	if got := e.Params[0].Range(); got != "1-4" {
		t.Errorf("range %q", got)
	}
	for in, want := range map[string]any{"": uint64(0), "1": uint64(0), "4": uint64(3)} {
		raw := map[string]string{}
		if in != "" {
			raw["me"] = in
		}
		a, err := e.Parse(raw, testResolver{})
		if err != nil || a.Value("me") != want {
			t.Errorf("--me %q: %v %v", in, a.Value("me"), err)
		}
	}
	for _, in := range []string{"0", "5"} {
		if _, err := e.Parse(map[string]string{"me": in}, testResolver{}); !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "1-4") {
			t.Errorf("--me %s: %v", in, err)
		}
	}
	if !strings.Contains(r.Markdown(), "uint, 1-4, counted from 1") {
		t.Errorf("markdown:\n%s", r.Markdown())
	}
	if err := r.Add(Entry{Device: "dev", Path: "x", Build: build, Params: []Param{{Name: "f", Kind: Float, OneBased: true}}}); err == nil {
		t.Error("one-based float accepted")
	}
}

// Operations have no Build; only operations can be local.
func TestOperations(t *testing.T) {
	r := New()
	for _, e := range []Entry{
		{Device: "dev", Path: "a", Operation: true, Build: build},
		{Device: "dev", Path: "b", Local: true, Build: build},
	} {
		if err := r.Add(e); err == nil {
			t.Errorf("%q accepted", e.Path)
		}
	}
	if err := r.Add(Entry{Device: "dev", Path: "status", Summary: "Show it.", Operation: true},
		Entry{Device: "dev", Path: "decode", Summary: "Decode.", Operation: true, Local: true}); err != nil {
		t.Fatal(err)
	}
	md := r.Markdown()
	if strings.Contains(md, "No echo") || !strings.Contains(md, "Runs in the CLI") {
		t.Errorf("markdown:\n%s", md)
	}
}

// An Index counts from 1 wherever people see it, and from 0 as a value.
func TestIndex(t *testing.T) {
	i := Index(2)
	for got, want := range map[string]string{
		i.String(): "3", fmt.Sprintf("%d", i): "3", fmt.Sprintf("%v", i): "3", fmt.Sprintf("%3d|%-3v|", i, i): "  3|3  |",
		fmt.Sprint(i): "3", fmt.Sprintf("%s", i): "3", fmt.Sprintf("%d", *IndexPtr(0)): "1",
		fmt.Sprintf("%+v", struct{ I Index }{i}): "{I:3}",
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	b, _ := json.Marshal(struct {
		I Index  `json:"i"`
		P *Index `json:"p,omitempty"`
	}{I: 2, P: IndexPtr(0)})
	if string(b) != `{"i":3,"p":1}` {
		t.Errorf("json %s", b)
	}
	var back Index
	if err := json.Unmarshal([]byte("3"), &back); err != nil || back != 2 {
		t.Errorf("unmarshal %v %v", int(back), err)
	}
	for in, want := range map[string]Index{"1": 0, "20": 19} {
		if got, err := ParseIndex(in); err != nil || got != want {
			t.Errorf("parse %q: %v %v", in, int(got), err)
		}
	}
	for _, in := range []string{"0", "-1", "x", "", "1.5"} {
		if _, err := ParseIndex(in); err == nil {
			t.Errorf("parse %q accepted", in)
		}
	}
}
