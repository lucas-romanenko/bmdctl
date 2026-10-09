// SPDX-License-Identifier: MIT

package table

import (
	"errors"
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

func TestParse(t *testing.T) {
	r := testRegistry(t)
	e, _, _ := r.Resolve("dev", []string{"wipe", "set"})
	rate := func(s string) int { return 25 * len(s) }
	a, err := e.Parse(map[string]string{"width": "12.5", "style": "dip", "layers": "bg,key1", "rate": "1:00",
		"on": "off", "when": "1789128000", "data": "1,2"}, rate)
	if err != nil {
		t.Fatal(err)
	}
	if a.Value("me") != uint64(0) || a.Value("width") != 12.5 || a.Value("style") != uint64(1) ||
		a.Value("layers") != uint64(3) || a.Value("rate") != int64(100) || a.Value("on") != false ||
		!a.Value("when").(time.Time).Equal(time.Unix(1789128000, 0)) || len(a.Value("data").([]string)) != 2 {
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
