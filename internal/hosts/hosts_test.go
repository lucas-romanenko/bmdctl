// SPDX-License-Identifier: MIT

package hosts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	f := &File{Default: "studio", Hosts: []Host{
		{"studio", "192.168.81.85", "atem"},
		{"deck_1", "deck-1.local:9993", "hyperdeck"},
		{"v6", "[fe80::1]:9910", "atem"},
	}}
	path := filepath.Join(t.TempDir(), "bmdctl", "hosts.yml")
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f, g) {
		t.Errorf("%+v\n%+v", f, g)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "  - name: studio\n    address: 192.168.81.85\n    device: atem\n") ||
		!strings.Contains(string(b), `address: "[fe80::1]:9910"`) {
		t.Errorf("%s", b)
	}
	empty, err := Load(filepath.Join(t.TempDir(), "none.yml"))
	if err != nil || len(empty.Hosts) != 0 || empty.Default != "" {
		t.Errorf("missing file: %+v %v", empty, err)
	}
	if err := empty.Save(path); err != nil {
		t.Fatal(err)
	}
	if g, err := Load(path); err != nil || len(g.Hosts) != 0 {
		t.Errorf("empty: %+v %v", g, err)
	}
}

// What a person might write by hand reads as they meant it.
func TestParseHandWritten(t *testing.T) {
	f, err := Parse([]byte(`---
# my switchers
default: 'studio'   # the main one
hosts:
- name: studio
  address: "192.168.81.85"
  # comment inside
  device: atem
-
  name: Deck
  address: 10.0.0.9:9993
  device: hyperdeck
- address: 10.0.0.5
  name: quiet  # device defaults to atem
`))
	if err != nil {
		t.Fatal(err)
	}
	want := &File{Default: "studio", Hosts: []Host{
		{"studio", "192.168.81.85", "atem"}, {"Deck", "10.0.0.9:9993", "hyperdeck"}, {"quiet", "10.0.0.5", "atem"},
	}}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("%+v", f)
	}
	if h := f.Find("deck"); h == nil || h.Name != "Deck" {
		t.Errorf("find ignores case: %v", h)
	}
}

func TestParseErrors(t *testing.T) {
	for in, msg := range map[string]string{
		"hosts:\n  - name: a\n\taddress: x\n":                "line 3: indent with spaces",
		"default: a\ndefault: b\n":                           "line 2: default given twice",
		"colour: red\n":                                      `unknown key "colour"`,
		"hosts:\n  - name: a\n    address: x\n    port: 1\n": `line 4: unknown key "port"`,
		"hosts:\n  - name: a\n      address: x\n":            "line 3: keys must line up",
		"hosts:\n  - name: a\n    address: x\n - name: b\n":  "line 4: list items must line up",
		"hosts:\n  - name: a\n":                              "needs a name and an address",
		"hosts:\n  - name: a\n    name: b\n":                 "name given twice",
		"hosts: [a]\n":                                       "hosts is a list",
		"hosts:\n  - name: \"a\n":                            "bad quoted value",
		"  - name: a\n":                                      "unexpected indented line",
		"hosts:\n    address: x\n":                           `expected "- name: ..."`,
		"default\n":                                          "expected",
	} {
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q: %v, want %q", in, err, msg)
		}
	}
}

func TestCheck(t *testing.T) {
	f := &File{Default: "gone", Hosts: []Host{
		{"a", "10.0.0.1", "atem"}, {"A", "10.0.0.2", "atem"}, {"1st", "x", "atem"}, {"b", "bad host", "atem"},
		{"c", "10.0.0.3:99999", "atem"}, {"d", "10.0.0.4", "toaster"},
	}}
	errs := f.Check()
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	got := strings.Join(all, "\n")
	for _, want := range []string{"A is named twice", `host name "1st"`, `b: address "bad host"`, "c: address", `d: device "toaster"`, "the default, gone, is not a host"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if err := f.Add(Host{"a", "10.0.0.9", "atem"}); err == nil {
		t.Error("duplicate added")
	}
	ok := &File{Default: "s", Hosts: []Host{{"s", "studio.local", "atem"}}}
	if errs := ok.Check(); len(errs) != 0 {
		t.Error(errs)
	}
	if err := ok.Remove("S"); err != nil || ok.Default != "" || len(ok.Hosts) != 0 {
		t.Errorf("remove: %v %+v", err, ok)
	}
	if err := ok.Remove("s"); err == nil {
		t.Error("removed twice")
	}
}
