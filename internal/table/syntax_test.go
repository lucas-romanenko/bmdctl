// SPDX-License-Identifier: MIT

package table

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// syntaxRegistry has a keyer said before its subcommand, an optional
// trailing argument, and plain commands.
func syntaxRegistry(t *testing.T) *Registry {
	t.Helper()
	lo, hi := Bounds(0, 3)
	keyer := Param{Name: "keyer", Kind: Uint, Min: lo, Max: hi, Required: true, OneBased: true}
	me := Param{Name: "me", Kind: Uint, Min: lo, Max: hi, Default: "1", OneBased: true}
	r := New()
	err := r.Add(
		Entry{Device: "dev", Path: "program", Summary: "Program.", Build: build,
			Params: []Param{me, {Name: "source", Kind: Source, Required: true}}},
		Entry{Device: "dev", Path: "usk fill", Summary: "Fill.", Build: build, Syntax: "usk <keyer> fill <source>",
			Params: []Param{me, keyer, {Name: "source", Kind: Source, Required: true}}},
		Entry{Device: "dev", Path: "usk on-air", Summary: "On air.", Build: build, Syntax: "usk <keyer> on-air <on>",
			Params: []Param{me, keyer, {Name: "on", Kind: Bool, Required: true}}},
		Entry{Device: "dev", Path: "usk luma", Summary: "Luma.", Build: build, Syntax: "usk <keyer> luma",
			Params: []Param{me, keyer, {Name: "clip", Kind: Float}, {Name: "gain", Kind: Float}, {Name: "glow", Kind: Float}}},
		Entry{Device: "dev", Path: "media download", Summary: "Download.", Operation: true, Syntax: "media download <slot> [file]",
			Params: []Param{{Name: "slot", Kind: Uint, Required: true}, {Name: "file", Kind: OutFile}}},
		Entry{Device: "dev", Path: "color", Summary: "Color.", Build: build, AtLeastOne: true,
			Params: []Param{{Name: "generator", Kind: Uint, Required: true}, {Name: "hue", Kind: Float},
				{Name: "saturation", Kind: Float}, {Name: "luma", Kind: Float}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSyntax(t *testing.T) {
	r := syntaxRegistry(t)
	var usages []string
	for _, e := range r.Entries("dev") {
		usages = append(usages, e.Usage())
	}
	want := []string{"color <generator>", "media download <slot> [file]", "program <source>",
		"usk <keyer> fill <source>", "usk <keyer> luma", "usk <keyer> on-air <on>"}
	if !reflect.DeepEqual(usages, want) {
		t.Errorf("%q", usages)
	}
	e := r.Find("dev", "usk fill")
	if e.Shown("keyer") != "<keyer>" || e.Shown("me") != "--me" || e.Label("source") != "source" || e.Label("me") != "--me" {
		t.Error("labels")
	}
	if d := r.Find("dev", "media download"); d.Shown("file") != "[file]" {
		t.Error("optional")
	}
	// The table holds every required argument positional, the syntax's
	// words to the path, and an optional positional to an optional one.
	for _, bad := range []Entry{
		{Device: "dev", Path: "a", Build: build, Params: []Param{{Name: "x", Kind: Uint, Required: true}}, Syntax: "a"},
		{Device: "dev", Path: "b", Build: build, Params: []Param{{Name: "x", Kind: Uint}}, Syntax: "b <x>"},
		{Device: "dev", Path: "c", Build: build, Params: []Param{{Name: "x", Kind: Uint, Required: true}}, Syntax: "c [x]"},
		{Device: "dev", Path: "d", Build: build, Syntax: "dd"},
		{Device: "dev", Path: "e", Build: build, Syntax: "e <nothing>"},
	} {
		if err := r.Add(bad); err == nil {
			t.Errorf("%q accepted", bad.Syntax)
		}
	}
}

func TestParseLine(t *testing.T) {
	r := syntaxRegistry(t)
	for _, c := range []struct {
		line string
		path string
		want map[string]string
	}{
		{"program cam1", "program", map[string]string{"source": "cam1"}},
		{"program 5 --me 2", "program", map[string]string{"source": "5", "me": "2"}},
		{"program --me 2 5", "program", map[string]string{"source": "5", "me": "2"}},
		{"usk 1 fill cam3", "usk fill", map[string]string{"keyer": "1", "source": "cam3"}},
		{"usk 2 on-air off", "usk on-air", map[string]string{"keyer": "2", "on": "off"}},
		{"usk 2 luma --clip 50", "usk luma", map[string]string{"keyer": "2", "clip": "50"}},
		{"media download 10 out.png", "media download", map[string]string{"slot": "10", "file": "out.png"}},
		{"media download 10", "media download", map[string]string{"slot": "10"}},
		{"color 1 --hue 180 --sat 50", "color", map[string]string{"generator": "1", "hue": "180", "saturation": "50"}},
		// The --name form of a positional argument, from before: accepted.
		{"program --source 5", "program", map[string]string{"source": "5"}},
		{"usk fill --keyer 1 --source cam3", "usk fill", map[string]string{"keyer": "1", "source": "cam3"}},
		{"usk on-air --keyer 2 --on", "usk on-air", map[string]string{"keyer": "2", "on": "true"}},
		{"usk 1 fill --source cam3", "usk fill", map[string]string{"keyer": "1", "source": "cam3"}},
		{"media download --slot 3 --file x.png", "media download", map[string]string{"slot": "3", "file": "x.png"}},
		// Missing arguments resolve to the command, which says what is
		// missing when it parses them.
		{"program", "program", map[string]string{}},
		{"usk fill", "usk fill", map[string]string{}},
	} {
		words, tokens := splitTest(c.line)
		e, raw, err := r.ParseLine("dev", words, tokens)
		if err != nil || e.Path != c.path || !reflect.DeepEqual(raw, c.want) {
			t.Errorf("%q: %v %v %v", c.line, e, raw, err)
		}
	}
	for line, msg := range map[string]string{
		"program 5 6":          `unexpected "6": program <source>`,
		"program 5 --source 6": "source given twice",
		"usk 1 luma --g 1":     "--g could be --gain or --glow",
		"color 1 --l 1 --lu 2": "luma given twice",
		"color 1 --bogus 1":    "color has no --bogus",
		"program --me":         "--me needs a value",
		"usk 1 fill a b":       `unexpected "b"`,
	} {
		words, tokens := splitTest(line)
		_, _, err := r.ParseLine("dev", words, tokens)
		if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q: %v, want %q", line, err, msg)
		}
	}
	for line, want := range map[string]NoCommand{
		"usk":         {Prefix: []string{"usk"}, Partial: true},
		"usk 1":       {Prefix: []string{"usk"}, Partial: true},
		"usk 1 bogus": {Prefix: []string{"usk"}},
		"nonsense":    {},
		"media":       {Prefix: []string{"media"}, Partial: true},
	} {
		words, tokens := splitTest(line)
		_, _, err := r.ParseLine("dev", words, tokens)
		var nc *NoCommand
		if !errors.As(err, &nc) || !reflect.DeepEqual(nc.Prefix, want.Prefix) || nc.Partial != want.Partial {
			t.Errorf("%q: %#v", line, err)
		}
	}
	// A missing argument is reported by its name when parsed.
	e, raw, _ := r.ParseLine("dev", []string{"program"}, nil)
	if _, err := e.Parse(raw, testResolver{}); err == nil || !strings.Contains(err.Error(), "missing <source>: program <source>") {
		t.Errorf("missing: %v", err)
	}
	if _, err := r.Find("dev", "program").Parse(map[string]string{"source": "nobody"}, testResolver{}); err == nil ||
		!strings.Contains(err.Error(), `source "nobody"`) {
		t.Errorf("bad value: %v", err)
	}
}

// splitTest splits a line the way the CLI does: words up to the first
// flag, then the rest.
func splitTest(line string) (words, tokens []string) {
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, "--") || len(tokens) > 0 {
			tokens = append(tokens, f)
		} else {
			words = append(words, f)
		}
	}
	return words, tokens
}

func TestChildrenAndNext(t *testing.T) {
	r := syntaxRegistry(t)
	var top []string
	for _, c := range r.Children("dev", nil) {
		top = append(top, c.Usage)
	}
	if !reflect.DeepEqual(top, []string{"color <generator>", "media", "program <source>", "usk"}) {
		t.Errorf("top %q", top)
	}
	var usk []string
	for _, c := range r.Children("dev", []string{"usk"}) {
		usk = append(usk, c.Usage)
	}
	if !reflect.DeepEqual(usk, []string{"<keyer> fill <source>", "<keyer> luma", "<keyer> on-air <on>"}) {
		t.Errorf("usk %q", usk)
	}
	names := func(next []NextElem) []string {
		var out []string
		for _, x := range next {
			if x.Word != "" {
				out = append(out, x.Word)
			} else {
				out = append(out, "<"+x.Param+">")
			}
		}
		return out
	}
	for words, want := range map[string][]string{
		"usk":              {"<keyer>"},
		"usk 1":            {"fill", "luma", "on-air"},
		"usk 1 fill":       {"<source>"},
		"media download 3": {"<file>"},
		"program 5":        nil,
	} {
		next, _ := r.Next("dev", strings.Fields(words))
		if got := names(next); !reflect.DeepEqual(got, want) {
			t.Errorf("after %q: %q, want %q", words, got, want)
		}
	}
	if _, whole := r.Next("dev", []string{"program", "5"}); whole == nil || whole.Path != "program" {
		t.Errorf("whole: %v", whole)
	}
}
