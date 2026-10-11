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
	f := &File{Default: "Studio A", Hosts: []Host{
		{Name: "Studio A", Address: "192.168.81.85", Device: "atem"},
		{Name: "ATEM 1 M/E Constellation HD", As: "backup", Was: "spare", Address: "192.168.81.86", Device: "atem"},
		{Name: "deck_1", Address: "deck-1.local:9993", Device: "hyperdeck"},
		{Name: "v6", Address: "[fe80::1]:9910", Device: "atem"},
		{Name: "#1: the main one", Address: "10.0.0.1", Device: "atem"},
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
	for _, want := range []string{"version: 2\ndefault: Studio A\n", "  - name: Studio A\n    address: 192.168.81.85\n    device: atem\n",
		"  - name: ATEM 1 M/E Constellation HD\n    as: backup\n    was: spare\n    address: 192.168.81.86\n",
		`address: "[fe80::1]:9910"`, `name: "#1: the main one"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lacks %q:\n%s", want, b)
		}
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
		{Name: "studio", Address: "192.168.81.85", Device: "atem"}, {Name: "Deck", Address: "10.0.0.9:9993", Device: "hyperdeck"},
		{Name: "quiet", Address: "10.0.0.5", Device: "atem"},
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
		"version: 3\n":                                       "reads versions 1 to 2",
		"hosts:\n  - name: a\n    as: b\n    as: c\n":        "as given twice",
	} {
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q: %v, want %q", in, err, msg)
		}
	}
}

func TestCheck(t *testing.T) {
	f := &File{Default: "gone", Hosts: []Host{
		{Name: "a", Address: "10.0.0.1", Device: "atem"}, {Name: "x", As: "A", Address: "10.0.0.2", Device: "atem"},
		{Name: " padded", Address: "x", Device: "atem"}, {Name: "b", Address: "bad host", Device: "atem"},
		{Name: "c", Address: "10.0.0.3:99999", Device: "atem"}, {Name: "d", Address: "10.0.0.4", Device: "toaster"},
	}}
	errs := f.Check()
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	got := strings.Join(all, "\n")
	for _, want := range []string{"A is named twice", `host name " padded": no spaces around it`, `b: address "bad host"`, "c: address",
		`d: device "toaster"`, "the default, gone, is not a host"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if err := f.Add(Host{Name: "a", Address: "10.0.0.9", Device: "atem"}); err == nil || !strings.Contains(err.Error(), "--as NAME") {
		t.Errorf("duplicate added: %v", err)
	}
	if err := f.Add(Host{Name: "new", Address: "10.0.0.1:9910", Device: "atem"}); err == nil || !strings.Contains(err.Error(), "already a host, a") {
		t.Errorf("address added twice: %v", err)
	}
	ok := &File{Default: "s", Hosts: []Host{{Name: "s", Address: "studio.local", Device: "atem"}}}
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

// --host takes a name in any case, any unambiguous start of one, a name a
// host went by, or an address, finding the host at it.
func TestResolve(t *testing.T) {
	f := &File{Hosts: []Host{
		{Name: "Studio A", Address: "192.168.81.85", Device: "atem"},
		{Name: "Studio B", Was: "bench", Address: "192.168.81.86", Device: "atem"},
		{Name: "ATEM Mini", As: "mini2", Address: "10.0.0.7:9911", Device: "atem"},
		{Name: "deck", Address: "deck.local", Device: "hyperdeck"},
	}}
	for spec, want := range map[string]string{
		"studio a": "192.168.81.85", "STUDIO B": "192.168.81.86", "bench": "192.168.81.86", "mini": "10.0.0.7:9911",
		"Mini2": "10.0.0.7:9911", "de": "deck.local", "192.168.81.85": "192.168.81.85", "192.168.81.86:9910": "192.168.81.86",
		"10.0.0.7": "10.0.0.7:9911",
	} {
		h, addr, err := f.Resolve(spec)
		if err != nil || h == nil || addr != want {
			t.Errorf("%q: %v %q %v, want %s", spec, h, addr, err, want)
		}
	}
	// The switcher's own name of a host that goes by --as isn't its name.
	if h, _, err := f.Resolve("atem mini"); h != nil || err == nil {
		t.Errorf("atem mini: %v %v", h, err)
	}
	for spec, want := range map[string]string{
		"stu":           `"stu" could be "Studio A" or "Studio B": give more of the name`,
		"no such host!": `"no such host!" is neither a host in hosts.yml nor an address`,
	} {
		if _, _, err := f.Resolve(spec); err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %s", spec, err, want)
		}
	}
	for spec, want := range map[string]string{"10.0.0.99": "10.0.0.99", "other.local:9910": "other.local:9910"} {
		if h, addr, err := f.Resolve(spec); h != nil || addr != want || err != nil {
			t.Errorf("%q: %v %q %v", spec, h, addr, err)
		}
	}
}

// A switcher renamed in ATEM Setup renames its host; the old name is kept
// as was, the default follows, and a name another host goes by is refused.
func TestRename(t *testing.T) {
	f := &File{Default: "bench", Hosts: []Host{
		{Name: "bench", Address: "192.168.81.85", Device: "atem"},
		{Name: "Studio B", Address: "192.168.81.86", Device: "atem"},
		{Name: "x", As: "mini2", Address: "10.0.0.7", Device: "atem"},
	}}
	h := &f.Hosts[0]
	if err := f.Rename(h, "Studio A"); err != nil || h.Name != "Studio A" || h.Was != "bench" || f.Default != "Studio A" {
		t.Errorf("rename: %v %+v %q", err, h, f.Default)
	}
	if f.Find("bench") != h {
		t.Error("the old name doesn't find it")
	}
	if err := f.Rename(h, "studio b"); err == nil || h.Name != "Studio A" {
		t.Errorf("took another host's name: %v %+v", err, h)
	}
	// A host going by --as keeps its name; the switcher's is recorded.
	if err := f.Rename(&f.Hosts[2], "Studio B"); err != nil || f.Hosts[2].Label() != "mini2" || f.Hosts[2].Name != "Studio B" {
		t.Errorf("--as host: %v %+v", err, f.Hosts[2])
	}
	if errs := f.Check(); len(errs) != 0 {
		t.Error(errs)
	}
}

// A file written before switchers named their hosts reads as it is.
func TestVersion1(t *testing.T) {
	f, err := Parse([]byte("# bmdctl's hosts: 'bmdctl hosts add|rm|default' edit this file.\nhosts:\n  - name: bench\n    address: 192.168.81.85\n    device: atem\n"))
	if err != nil || len(f.Hosts) != 1 || f.Hosts[0] != (Host{Name: "bench", Address: "192.168.81.85", Device: "atem"}) {
		t.Fatalf("%+v %v", f, err)
	}
	if !strings.HasPrefix(string(f.Marshal())[strings.Index(string(f.Marshal()), "version"):], "version: 2\nhosts:\n  - name: bench\n") {
		t.Errorf("%s", f.Marshal())
	}
}
