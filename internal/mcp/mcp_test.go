// SPDX-License-Identifier: MIT

package mcp

import (
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// Every entry is a tool, by a name no other has; every entry the
// read-only list names exists.
func TestTools(t *testing.T) {
	reg, err := engine.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	ts := tools(reg)
	seen := map[string]bool{}
	n := 0
	for _, tl := range ts {
		if seen[tl.Name] || len(tl.Name) > 64 {
			t.Errorf("tool %q", tl.Name)
		}
		seen[tl.Name] = true
		if tl.entry != nil {
			n++
		}
	}
	want := 0
	for _, d := range reg.Devices() {
		want += len(reg.Entries(d))
	}
	if n != want {
		t.Errorf("%d tools for %d entries", n, want)
	}
	for k := range readOnly {
		found := false
		for _, d := range reg.Devices() {
			for _, e := range reg.Entries(d) {
				if d+" "+e.Path == k {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("readOnly names %q, which is no entry", k)
		}
	}
}

func TestURIs(t *testing.T) {
	for uri, want := range map[string]string{"bmdctl://hosts/studio/state": "studio", "bmdctl://hosts/Deck 1/state": "Deck 1",
		"bmdctl://hosts//state": "", "bmdctl://nowhere": "", "bmdctl://hosts/x/other": ""} {
		if got, _ := hostOfURI(uri); got != want {
			t.Errorf("%s: %q", uri, got)
		}
	}
}
