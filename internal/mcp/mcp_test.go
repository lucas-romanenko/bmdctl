// SPDX-License-Identifier: MIT

package mcp

import (
	"slices"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// The tools are list_hosts and one per command group, about fifteen;
// every entry is a subcommand of its group's tool, under a name no other
// in it has; a group is destructive where a subcommand is, and only then
// takes confirm; every entry the lists name exists.
func TestTools(t *testing.T) {
	reg, err := engine.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	ts := tools(reg)
	if len(ts) < 10 || len(ts) > 20 {
		t.Errorf("%d tools", len(ts))
	}
	seen := map[string]bool{}
	n := 0
	for _, tl := range ts {
		if seen[tl.Name] || len(tl.Name) > 64 {
			t.Errorf("tool %q", tl.Name)
		}
		seen[tl.Name] = true
		for _, k := range []string{"anyOf", "oneOf", "allOf"} {
			if _, ok := tl.InputSchema[k]; ok {
				t.Errorf("%s: %s at the top of its schema", tl.Name, k)
			}
		}
		if tl.special != "" {
			continue
		}
		n += len(tl.subs)
		props := tl.InputSchema["properties"].(table.Schema)
		enum := props["subcommand"].(table.Schema)["enum"].([]string)
		if len(enum) != len(tl.subs) || !slices.Equal(tl.InputSchema["required"].([]string), []string{"subcommand"}) {
			t.Errorf("%s: %d subcommands, %d in the enum", tl.Name, len(tl.subs), len(enum))
		}
		destructive := false
		for sub, e := range tl.subs {
			destructive = destructive || e.Destructive
			for _, p := range e.Params {
				if props[p.Name] == nil {
					t.Errorf("%s %s: no property for %s", tl.Name, sub, p.Name)
				}
			}
		}
		if tl.Annotations["destructiveHint"] != destructive {
			t.Errorf("%s: destructiveHint %v", tl.Name, tl.Annotations["destructiveHint"])
		}
		if _, ok := props["confirm"]; destructive && !ok {
			t.Errorf("%s: destructive without confirm", tl.Name)
		}
	}
	want := 0
	for _, d := range reg.Devices() {
		want += len(reg.Entries(d))
		for _, g := range reg.Groups(d) {
			if g.Summary == "" {
				t.Errorf("%s %s: a group its device doesn't declare", d, g.Name)
			}
		}
	}
	if n != want {
		t.Errorf("%d subcommands for %d entries", n, want)
	}
	for _, m := range []map[string]bool{readOnly, boolKeys(asksFirst)} {
		for k := range m {
			found := false
			for _, d := range reg.Devices() {
				for _, e := range reg.Entries(d) {
					if d+" "+e.Path == k {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("%q is no entry", k)
			}
		}
	}
}

func boolKeys(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func TestURIs(t *testing.T) {
	for uri, want := range map[string]string{"bmdctl://hosts/studio/state": "studio", "bmdctl://hosts/Deck 1/state": "Deck 1",
		"bmdctl://hosts//state": "", "bmdctl://nowhere": "", "bmdctl://hosts/x/other": ""} {
		if got, _ := hostOfURI(uri); got != want {
			t.Errorf("%s: %q", uri, got)
		}
	}
}
