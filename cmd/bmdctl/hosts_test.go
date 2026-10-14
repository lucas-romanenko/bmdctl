// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/mdns"
)

// A command's first word is never a device's name: "bmdctl hyperdeck"
// is the HyperDeck device, not an ATEM command.
func TestNoCommandIsADeviceName(t *testing.T) {
	reg, _ := engine.NewRegistry()
	for _, e := range reg.Entries("atem") {
		if slices.Contains(hosts.Devices, e.Words()[0]) {
			t.Errorf("atem %s", e.Path)
		}
	}
}

// whoIDump is the fixture dump with a WhoI naming the switcher name, as
// ATEM Setup does, before its last packet (InCm).
func whoIDump(t *testing.T, name string) []byte {
	d := fixtures.Bytes(t, "dump/initial-dump.bin")
	payload := make([]byte, 176)
	copy(payload[112:], name)
	i := bytes.Index(d, []byte("InCm")) - 4
	return append(append(append([]byte{}, d[:i]...), fakeatem.Command("WhoI", payload)...), d[i:]...)
}

func (b *bench) hostsFile(t *testing.T) *hosts.File {
	t.Helper()
	f, err := hosts.Load(filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// hosts add asks the switcher its name and stores it; --as names one
// itself; a device bmdctl can't ask needs --as.
func TestHostsFile(t *testing.T) {
	b := newBench(t)
	if out := b.must("hosts"); out != "no hosts (bmdctl hosts add ADDRESS, or bmdctl hosts discover)\n" {
		t.Errorf("empty: %q", out)
	}
	code, out, errOut := b.run("hosts", "add", "192.0.2.10")
	if code != 0 || out != "added ATEM 1 M/E Constellation HD (192.0.2.10)\n" ||
		errOut != "connecting to 192.0.2.10 …\nATEM 1 M/E Constellation HD, 1080p50\n" {
		t.Errorf("add: %d %q %q", code, out, errOut)
	}
	// A second switcher of the same model calls itself the same.
	code, _, errOut = b.run("hosts", "add", "192.0.2.11")
	if code != exitUsage || !strings.Contains(errOut, `another host goes by "ATEM 1 M/E Constellation HD" (192.0.2.10): give this one its own name with --as NAME`) {
		t.Errorf("duplicate: %d %q", code, errOut)
	}
	if out := b.must("hosts", "add", "192.0.2.11", "--as", "Backup"); out != "added Backup (192.0.2.11), which calls itself \"ATEM 1 M/E Constellation HD\"\n" {
		t.Errorf("--as: %q", out)
	}
	b.must("hosts", "add", "192.0.2.20:9993", "--as", "deck", "--device", "hyperdeck")
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"hosts", "add", "192.0.2.21", "--device", "videohub"}, "192.0.2.21 didn't say its name: give it one with --as NAME"},
		{[]string{"hosts", "add", "192.0.2.12", "--as", "backup"}, `another host goes by "Backup"`},
		{[]string{"hosts", "add", "192.0.2.12", "--as", " padded"}, "no spaces around it"},
		{[]string{"hosts", "add", "not an address"}, "not an IP address"},
		{[]string{"hosts", "add", "192.0.2.12", "--device", "toaster"}, "not one of"},
		{[]string{"hosts", "add"}, "takes ADDRESS"},
		{[]string{"hosts", "rm", "nobody"}, "no host named nobody"},
		{[]string{"hosts", "rm", "192.0.2.99"}, "no host named 192.0.2.99"},
		{[]string{"hosts", "default", "nobody"}, "no host named nobody (bmdctl hosts lists them)"},
		{[]string{"hosts", "default", "no one!"}, "neither a host"},
		{[]string{"hosts", "frobnicate"}, "usage: bmdctl hosts"},
		{[]string{"hosts", "list", "--device", "atem"}, "unexpected"},
		{[]string{"hosts", "discover", "--for", "2h"}, "up to a minute"},
	} {
		if code, _, errOut := b.run(c.args...); code != exitUsage || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %q", c.args, code, errOut, c.msg)
		}
	}
	// Names in any case, or the start of one.
	b.must("hosts", "default", "atem")
	if out := b.must("hosts", "default"); out != "ATEM 1 M/E Constellation HD\n" {
		t.Errorf("default by its start: %q", out)
	}
	out = b.must("hosts", "list")
	want := "ATEM 1 M/E Constellation HD  192.0.2.10       atem       default\n" +
		"Backup                       192.0.2.11       atem                calls itself \"ATEM 1 M/E Constellation HD\"\n" +
		"deck                         192.0.2.20:9993  hyperdeck\n"
	if out != want {
		t.Errorf("list:\n%s\nwant:\n%s", out, want)
	}
	var r struct {
		OK     bool       `json:"ok"`
		Result hosts.File `json:"result"`
	}
	if err := json.Unmarshal([]byte(b.must("hosts", "--json")), &r); err != nil || !r.OK || r.Result.Default != "ATEM 1 M/E Constellation HD" ||
		len(r.Result.Hosts) != 3 || r.Result.Hosts[1].As != "Backup" {
		t.Errorf("--json: %+v %v", r, err)
	}
	// Adding an address again asks it again: --as renames it, and the
	// default follows; the name it went by still finds it.
	if out := b.must("hosts", "add", "192.0.2.10", "--as", "Main"); out != "ATEM 1 M/E Constellation HD (192.0.2.10) goes by Main now\n" {
		t.Errorf("re-add: %q", out)
	}
	if f := b.hostsFile(t); f.Default != "Main" || f.Find("atem 1 m/e constellation hd") == nil {
		t.Errorf("after re-add: %+v", f)
	}
	if out := b.must("hosts", "add", "192.0.2.10", "--as", "Main"); out != "Main (192.0.2.10) is a host already\n" {
		t.Errorf("re-add, same: %q", out)
	}
	// The form before switchers named their hosts, with a notice.
	code, out, errOut = b.run("hosts", "add", "old", "192.0.2.40")
	if code != 0 || out != "added old (192.0.2.40), which calls itself \"ATEM 1 M/E Constellation HD\"\n" ||
		!strings.Contains(errOut, "bmdctl: hosts add NAME ADDRESS is deprecated, use: bmdctl hosts add 192.0.2.40 --as old\n") {
		t.Errorf("old form: %d %q %q", code, out, errOut)
	}
	// A switcher that doesn't answer: an error pointing at doctor, unless
	// --as names it, which adds it anyway.
	b.mu.Lock()
	b.dialErr = errors.New("atem: dial 192.0.2.50:9910: context deadline exceeded")
	b.mu.Unlock()
	if code, _, errOut := b.run("hosts", "add", "192.0.2.50"); code != exitUnreachable || !strings.HasSuffix(errOut, "(bmdctl doctor says why)\n") {
		t.Errorf("unreachable: %d %q", code, errOut)
	}
	code, out, errOut = b.run("hosts", "add", "192.0.2.50", "--as", "later")
	if code != 0 || out != "added later (192.0.2.50)\n" || !strings.Contains(errOut, "bmdctl: 192.0.2.50 didn't answer, so its name is unknown") {
		t.Errorf("unreachable --as: %d %q %q", code, out, errOut)
	}
	b.mu.Lock()
	b.dialErr = nil
	b.mu.Unlock()
	b.must("hosts", "rm", "back") // by the start of its name
	b.must("hosts", "rm", "192.0.2.40")
	b.must("hosts", "rm", "main")
	if out := b.must("hosts", "default"); out != "no default host\n" {
		t.Errorf("default after rm: %q", out)
	}
	path := filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "version: 2\n") || !strings.Contains(string(data), "  - name: deck\n    address: 192.0.2.20:9993\n    device: hyperdeck\n") {
		t.Errorf("hosts.yml:\n%s", data)
	}
	os.WriteFile(path, []byte("hosts:\n  - name: a\n\taddress: x\n"), 0o644)
	if code, _, errOut := b.run("hosts"); code != exitUsage || !strings.Contains(errOut, "hosts.yml: line 3") {
		t.Errorf("broken file: %d %q", code, errOut)
	}
}

// wentTo runs a command line and returns the address of the switcher
// that received one more command with this opcode.
func (b *bench) wentTo(t *testing.T, code string, args ...string) string {
	t.Helper()
	count := func() map[string]int {
		b.mu.Lock()
		sws, addrs := append([]*fakeatem.Switcher(nil), b.sws...), append([]string(nil), b.addrs...)
		b.mu.Unlock()
		n := map[string]int{}
		for i, sw := range sws {
			for _, c := range sw.Commands() {
				if string(c[4:8]) == code {
					n[addrs[i]]++
				}
			}
		}
		return n
	}
	before := count()
	b.must(args...)
	got := ""
	eventually(t, code, func() bool {
		for a, n := range count() {
			if n > before[a] {
				got = a
				return true
			}
		}
		return false
	})
	return got
}

// --host takes a name in any case or the start of one, or an address;
// without it a command goes to BMDCTL_HOST, else the last --host, else
// the default.
func TestHostResolution(t *testing.T) {
	b := newBench(t)
	if code, _, errOut := b.run("cut"); code != exitUsage || !strings.Contains(errOut, "no switcher named") || !strings.Contains(errOut, "--dry-run") {
		t.Errorf("no host: %d %q", code, errOut)
	}
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.11", "--as", "other")
	b.must("hosts", "add", "192.0.2.13", "--as", "stage")
	b.must("hosts", "add", "192.0.2.20", "--as", "deck", "--device", "hyperdeck")
	if got := b.wentTo(t, "DCut", "--host", "Studio", "cut"); got != "192.0.2.10:9910" {
		t.Fatalf("by name: %s", got)
	}
	// The last --host is remembered.
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.10:9910" {
		t.Errorf("last used: %s", got)
	}
	// The start of a name, once it is only one name's.
	if got := b.wentTo(t, "DCut", "--host", "OTH", "cut"); got != "192.0.2.11:9910" {
		t.Errorf("by its start: %s", got)
	}
	if got := b.cliLastHost(); got != "other" {
		t.Errorf("last host %q: the name, not what was typed", got)
	}
	if code, _, errOut := b.run("--host", "st", "cut"); code != exitUsage || !strings.Contains(errOut, `--host "st" could be "studio" or "stage": give more of the name (bmdctl hosts lists them)`) {
		t.Errorf("ambiguous: %d %q", code, errOut)
	}
	// BMDCTL_HOST wins over the last host.
	b.env[HostEnv] = "stage"
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.13:9910" {
		t.Errorf("BMDCTL_HOST: %s", got)
	}
	delete(b.env, HostEnv)
	// A host's address finds the host: the line names it.
	if out := b.must("--host", "192.0.2.10", "program", "2"); !strings.HasPrefix(out, "studio (192.0.2.10)  program") {
		t.Errorf("by address: %q", out)
	}
	// A bare address works, named by the switcher, and becomes the last
	// used.
	if out := b.must("--host", "192.0.2.12", "program", "2"); !strings.HasPrefix(out, "ATEM 1 M/E Constellation HD (192.0.2.12)  program") {
		t.Errorf("bare address: %q", out)
	}
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.12:9910" {
		t.Errorf("address: %s", got)
	}
	// Setting the default makes it the host commands go to now.
	b.must("hosts", "default", "other")
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.11:9910" {
		t.Errorf("default: %s", got)
	}
	os.Remove(filepath.Join(b.env["XDG_STATE_HOME"], "bmdctl", "last-host"))
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.11:9910" {
		t.Errorf("default without last-host: %s", got)
	}
	// A host's device type decides the device; it must match one named.
	if code, _, errOut := b.run("--host", "deck", "cut"); code != exitUsage || !strings.Contains(errOut, "deck (192.0.2.20) is a HyperDeck; bmdctl hyperdeck --help") {
		t.Errorf("hyperdeck host: %d %q", code, errOut)
	}
	if code, _, errOut := b.run("atem", "--host", "deck", "cut"); code != exitUsage || !strings.Contains(errOut, "deck (192.0.2.20) is a HyperDeck, not an ATEM") {
		t.Errorf("mismatch: %d %q", code, errOut)
	}
	if code, _, errOut := b.run("--host", "no such host!", "cut"); code != exitUsage || !strings.Contains(errOut, "neither a host") {
		t.Errorf("bad --host: %d %q", code, errOut)
	}
	// An unreachable --host isn't remembered.
	b.must("--host", "other", "cut")
	b.mu.Lock()
	b.dialErr = errNoEngine
	b.mu.Unlock()
	b.run("--host", "192.0.2.99", "cut")
	if got := b.cliLastHost(); got != "other" {
		t.Errorf("last host after a failure: %q", got)
	}
}

func (b *bench) cliLastHost() string {
	c, _, _ := b.cli("")
	return c.lastHost()
}

// A hosts.yml from before switchers named their hosts reads as it is;
// the next command to each switcher takes the name it gives itself, says
// so, and keeps the old name for --host. A rename in ATEM Setup does the
// same.
func TestSwitcherNamesItsHost(t *testing.T) {
	b := newBenchDump(t, whoIDump(t, "Studio A"))
	path := filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# bmdctl's hosts: 'bmdctl hosts add|rm|default' edit this file.\ndefault: bench\nhosts:\n"+
		"  - name: bench\n    address: 192.0.2.10\n    device: atem\n  - name: spare\n    address: 192.0.2.11\n    device: atem\n"), 0o644)
	code, out, errOut := b.run("--host", "bench", "program", "1")
	if code != 0 || !strings.HasPrefix(out, "Studio A (192.0.2.10)  program  ME1") ||
		!strings.Contains(errOut, "bmdctl: bench (192.0.2.10) calls itself \"Studio A\" now: hosts.yml updated, and --host \"bench\" still works\n") {
		t.Fatalf("migrated: %d %q %q", code, out, errOut)
	}
	f := b.hostsFile(t)
	if h := f.Hosts[0]; h.Name != "Studio A" || h.Was != "bench" || f.Default != "Studio A" || b.cliLastHost() != "Studio A" {
		t.Errorf("hosts.yml: %+v", f)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data[strings.Index(string(data), "version"):]), "version: 2\ndefault: Studio A\nhosts:\n  - name: Studio A\n    was: bench\n") {
		t.Errorf("hosts.yml:\n%s", data)
	}
	// Said once: the name now matches.
	if _, _, errOut := b.run("--host", "bench", "cut"); errOut != "" {
		t.Errorf("again: %q", errOut)
	}
	if out := b.must("--host", "studio a", "cut"); !strings.HasPrefix(out, "Studio A (192.0.2.10)  cut") {
		t.Errorf("new name: %q", out)
	}
	// The other switcher calls itself Studio A too: its host keeps its
	// name, and says why.
	code, out, errOut = b.run("--host", "spare", "cut")
	if code != 0 || !strings.HasPrefix(out, "spare (192.0.2.11)") || !strings.Contains(errOut, `spare (192.0.2.11) now calls itself "Studio A", but another host goes by "Studio A" (192.0.2.10): give it a name of its own with bmdctl hosts add 192.0.2.11 --as NAME`) {
		t.Errorf("clash: %d %q %q", code, out, errOut)
	}
	b.must("hosts", "add", "192.0.2.11", "--as", "Spare")
	// Renamed in ATEM Setup: the next session sees it.
	b.mu.Lock()
	b.dump = whoIDump(t, "Studio C")
	b.mu.Unlock()
	b.must("--host", "studio a", "disconnect")
	b.must("--host", "spare", "disconnect")
	code, _, errOut = b.run("--host", "studio a", "cut")
	if code != 0 || !strings.Contains(errOut, `bmdctl: Studio A (192.0.2.10) calls itself "Studio C" now: hosts.yml updated, and --host "Studio A" still works`) {
		t.Errorf("rename: %d %q", code, errOut)
	}
	// A host named with --as keeps its name; the change is said.
	code, out, errOut = b.run("--host", "spare", "cut")
	if code != 0 || !strings.HasPrefix(out, "Spare (192.0.2.11)") ||
		!strings.Contains(errOut, `bmdctl: Spare (192.0.2.11): the switcher calls itself "Studio C" now, not "Studio A": hosts.yml updated`) {
		t.Errorf("--as rename: %d %q %q", code, out, errOut)
	}
	// --json and --quiet say nothing, and the name is taken all the same.
	b.mu.Lock()
	b.dump = whoIDump(t, "Studio D")
	b.mu.Unlock()
	b.must("--host", "studio c", "disconnect")
	if _, _, errOut := b.run("--host", "studio c", "cut", "--json"); errOut != "" {
		t.Errorf("--json: %q", errOut)
	}
	if f := b.hostsFile(t); f.Hosts[0].Name != "Studio D" {
		t.Errorf("--json didn't take the name: %+v", f.Hosts[0])
	}
}

// hosts discover adds what Bonjour finds by its own name: once, and not a
// name another host goes by.
func TestHostsDiscover(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.13", "--as", "spare") // its switcher calls itself the model's name
	b.must("hosts", "add", "192.0.2.14")
	ip := func(s string) []net.IP { return []net.IP{net.ParseIP(s)} }
	found := []mdns.Instance{
		{Name: "Studio A", Addrs: ip("192.0.2.10")},
		{Name: "ATEM Mini", Addrs: ip("192.0.2.11")},
		{Name: "Studio A", Addrs: ip("192.0.2.12")},
		{Name: "Studio B", Addrs: ip("192.0.2.14")},
		{Name: "no address"},
	}
	var waited time.Duration
	run := func(args ...string) (int, string, string) {
		c, o, e := b.cli("")
		c.discover = func(_ context.Context, wait time.Duration) ([]mdns.Instance, error) { waited = wait; return found, nil }
		return c.run(append([]string{"hosts", "discover"}, args...)), o.String(), e.String()
	}
	code, out, errOut := run("--for", "1")
	want := "added    Studio A (192.0.2.10)\n" +
		"added    ATEM Mini (192.0.2.11)\n" +
		"skipped  Studio A (192.0.2.12)   another host goes by \"Studio A\" (192.0.2.10): give this one its own name with --as NAME\n" +
		"renamed  Studio B (192.0.2.14)   was ATEM 1 M/E Constellation HD\n"
	if code != 0 || out != want || errOut != "looking for switchers for 1s …\n" || waited != time.Second {
		t.Errorf("discover: %d\n%s\nwant:\n%s%q %v", code, out, want, errOut, waited)
	}
	f := b.hostsFile(t)
	if len(f.Hosts) != 4 || f.Find("Studio A").Address != "192.0.2.10" || f.Find("studio b").Was != "ATEM 1 M/E Constellation HD" {
		t.Errorf("hosts: %+v", f.Hosts)
	}
	// Again: they are known.
	_, out, _ = run("--json")
	var r struct {
		Result struct {
			Found []struct{ Name, Status string } `json:"found"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(out), &r); len(r.Result.Found) != 4 || r.Result.Found[0].Status != "known" || r.Result.Found[2].Status != "skipped" {
		t.Errorf("--json: %s", out)
	}
	found = nil
	if _, out, _ := run(); !strings.HasPrefix(out, "no switcher announced itself in 2s;") {
		t.Errorf("none: %q", out)
	}
}

// hosts alias gives a host a short name it goes by as well as its own,
// wherever a name is taken. A command's word, another host's name or
// alias, or something a line can't carry as one word is refused; another
// alias replaces it, --rm takes it away. hosts and bmdctl alone show it.
func TestHostsAlias(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "Block C - TEST")
	b.must("hosts", "add", "192.0.2.11", "--as", "studio")
	if out := b.must("hosts", "alias", "Block C - TEST", "bc"); out != "alias bc: Block C - TEST (192.0.2.10)\n" {
		t.Errorf("alias: %q", out)
	}
	for _, spec := range []string{"bc", "BC", "block", "192.0.2.10"} {
		if got := b.wentTo(t, "DCut", "--host", spec, "cut"); got != "192.0.2.10:9910" {
			t.Errorf("--host %q: %s", spec, got)
		}
	}
	if got := b.cliLastHost(); got != "Block C - TEST" {
		t.Errorf("last host %q: the name, not the alias", got)
	}
	for _, c := range []struct{ alias, want string }{
		{"cut", `"cut" is a command, so it can't be an alias`},
		{"CUT", `"CUT" is a command, so it can't be an alias`},
		{"atem", `"atem" is a command, so it can't be an alias`},
		{"hosts", `"hosts" is a command, so it can't be an alias`},
		{"play", `"play" is a command, so it can't be an alias`}, // a HyperDeck's
		{"Bc", `"bc" is Block C - TEST's alias already`},
		{"bc 2", `alias "bc 2": one word, no spaces`},
		{"192.0.2.99", `alias "192.0.2.99": that is an address`},
	} {
		if code, _, errOut := b.run("hosts", "alias", "studio", c.alias); code != exitUsage || errOut != "bmdctl hosts alias: "+c.want+"\n" {
			t.Errorf("alias %q: %d %q", c.alias, code, errOut)
		}
	}
	if code, _, errOut := b.run("hosts", "alias", "studio"); code != exitUsage || !strings.Contains(errOut, "takes NAME SHORT") {
		t.Errorf("no alias: %d %q", code, errOut)
	}
	if code, _, errOut := b.run("hosts", "alias", "nobody", "x"); code != exitUsage || errOut != "bmdctl hosts alias: no host named nobody (bmdctl hosts lists them)\n" {
		t.Errorf("no host: %d %q", code, errOut)
	}
	// A host's own name by its alias; another alias replaces it.
	if out := b.must("hosts", "alias", "bc", "bt"); out != "alias bt: Block C - TEST (192.0.2.10), was bc\n" {
		t.Errorf("replace: %q", out)
	}
	if out := b.must("hosts", "alias", "studio", "s"); out != "alias s: studio (192.0.2.11)\n" {
		t.Errorf("studio: %q", out)
	}
	if out := b.must("hosts"); out != "Block C - TEST  192.0.2.10  atem    alias bt, calls itself \"ATEM 1 M/E Constellation HD\"\n"+
		"studio           192.0.2.11  atem    alias s, calls itself \"ATEM 1 M/E Constellation HD\"\n" {
		t.Errorf("list: %q", out)
	}
	if out := b.must("hosts", "--json"); !strings.Contains(out, `"as":"Block C - TEST","alias":"bt","address":"192.0.2.10","device":"atem"}`) {
		t.Errorf("list --json: %s", out)
	}
	// bmdctl alone: the alias, then the name.
	if out := b.must(); !regexp.MustCompile(`(?m)^bt +Block C - TEST \(192\.0\.2\.10\) +ATEM 1 M/E Constellation HD +1080p50 +ready`).MatchString(out) ||
		!regexp.MustCompile(`(?m)^s +studio \(192\.0\.2\.11\) +ATEM`).MatchString(out) {
		t.Errorf("overview:\n%s", out)
	}
	if out := b.must("--json"); !strings.Contains(out, `"name":"Block C - TEST","alias":"bt","address":"192.0.2.10"`) {
		t.Errorf("overview --json: %s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml"))
	if !strings.Contains(string(raw), "version: 3\n") || !strings.Contains(string(raw), "    alias: bt\n") {
		t.Errorf("hosts.yml:\n%s", raw)
	}
	// --rm takes it away; the file is version 2 again with none left.
	if out := b.must("hosts", "alias", "bt", "--rm"); out != "no alias: Block C - TEST (192.0.2.10), was bt\n" {
		t.Errorf("rm: %q", out)
	}
	b.must("hosts", "alias", "studio", "--rm")
	if out := b.must("hosts", "alias", "studio", "--rm", "--json"); out != `{"ok":true,"confirmed":false,"result":{"name":"ATEM 1 M/E Constellation HD","as":"studio","address":"192.0.2.11","device":"atem"},"took_ms":0}`+"\n" {
		t.Errorf("rm --json: %s", out)
	}
	raw, _ = os.ReadFile(filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml"))
	if !strings.Contains(string(raw), "version: 2\n") || strings.Contains(string(raw), "alias:") {
		t.Errorf("hosts.yml:\n%s", raw)
	}
	if out := b.must(); strings.HasPrefix(out, " ") || !regexp.MustCompile(`(?m)^studio \(192\.0\.2\.11\)`).MatchString(out) {
		t.Errorf("overview without aliases:\n%s", out)
	}
}

// bmdctl HOST COMMAND: a first word that is a command is the command;
// otherwise one that names a host (its name or alias, the start of
// either, its address) or is an IP address is the host, for that command
// alone; otherwise it is refused, listing the hosts. --host wins.
func TestHostWord(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "Block C - TEST")
	b.must("hosts", "add", "192.0.2.11", "--as", "studio")
	b.must("hosts", "add", "192.0.2.13", "--as", "stage")
	b.must("hosts", "add", "192.0.2.14", "--as", "info") // a command's name
	b.must("hosts", "add", "192.0.2.20", "--as", "deck", "--device", "hyperdeck")
	b.must("hosts", "alias", "Block C - TEST", "bc")
	b.must("hosts", "alias", "deck", "d1")
	b.must("hosts", "default", "studio")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"bc", "cut"}, "192.0.2.10:9910"},
		{[]string{"BC", "cut"}, "192.0.2.10:9910"},
		{[]string{"Block C - TEST", "cut"}, "192.0.2.10:9910"},
		{[]string{"block", "cut"}, "192.0.2.10:9910"},
		{[]string{"192.0.2.10", "cut"}, "192.0.2.10:9910"},
		{[]string{"192.0.2.12", "cut"}, "192.0.2.12:9910"}, // an address no host has
		{[]string{"STUD", "cut"}, "192.0.2.11:9910"},
		{[]string{"atem", "bc", "cut"}, "192.0.2.10:9910"},            // after a device's word
		{[]string{"--host", "stage", "bc", "cut"}, "192.0.2.13:9910"}, // --host wins
		{[]string{"cut"}, "192.0.2.13:9910"},                          // and is the last used, as ever
	} {
		if got := b.wentTo(t, "DCut", c.args...); got != c.want {
			t.Errorf("%q: went to %s, want %s", c.args, got, c.want)
		}
	}
	// The host word is for its command alone: commands go where they did.
	b.must("hosts", "default", "studio")
	b.must("bc", "cut")
	if got := b.cliLastHost(); got != "studio" {
		t.Errorf("last host after bmdctl bc cut: %q", got)
	}
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.11:9910" {
		t.Errorf("after the host word: %s", got)
	}
	// A command is a command, whatever a host is called.
	if out := b.must("info"); !strings.HasPrefix(out, "studio (192.0.2.11)") {
		t.Errorf("info: %q", out)
	}
	// The host's device decides the command's.
	if code, _, errOut := b.run("d1", "cut"); code != exitUsage || errOut != "bmdctl: deck (192.0.2.20) is a HyperDeck; bmdctl hyperdeck --help\n" {
		t.Errorf("deck: %d %q", code, errOut)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"progam", "2"}, `bmdctl: no command or host named "progam"; hosts: bc, studio, stage, info, d1 (bmdctl --help lists the commands)`},
		{[]string{"studio.local", "cut"}, `bmdctl: no command or host named "studio.local"; hosts: bc, studio, stage, info, d1 (bmdctl --help lists the commands)`},
		{[]string{"st", "cut"}, `bmdctl: "st" could be "studio" or "stage": give more of the name (bmdctl hosts lists them)`},
		{[]string{"bc", "progam"}, `bmdctl: unknown command "progam" (bmdctl --help lists them)`},
	} {
		if code, _, errOut := b.run(c.args...); code != exitUsage || errOut != c.want+"\n" {
			t.Errorf("%q: %d %q", c.args, code, errOut)
		}
	}
	if code, out, _ := b.run("progam", "--json"); code != exitUsage || !strings.HasPrefix(out, `{"ok":false,"code":"usage","error":"bmdctl: no command or host named \"progam\"; hosts: bc,`) {
		t.Errorf("--json: %d %s", code, out)
	}
	// The host alone is bmdctl alone.
	if out := b.must("bc"); !strings.HasPrefix(out, "engine  running") {
		t.Errorf("bmdctl bc: %q", out)
	}
	// An alias binary's line starts with the host as well.
	before := len(b.sent("DCut"))
	c, _, e := b.cli("")
	c.alias, c.aliasName = "atem", "atemctl"
	if code := c.run([]string{"stage", "cut"}); code != exitOK || len(b.sent("DCut")) != before+1 {
		t.Errorf("atemctl stage cut: %d %s", code, e)
	}
	// exec: a line's host word wins over exec's host, and its own --host
	// over both.
	b.must("hosts", "default", "studio")
	script := filepath.Join(b.dir, "cue.txt")
	os.WriteFile(script, []byte("bc cut\ncut\n--host stage bc cut\nbmdctl stage cut\n"), 0o644)
	count := func() map[string]int {
		n := map[string]int{}
		for i, sw := range b.switchers() {
			for _, c := range sw.Commands() {
				if string(c[4:8]) == "DCut" {
					n[b.dialled()[i]]++
				}
			}
		}
		return n
	}
	was := count()
	b.must("exec", script, "--host", "studio")
	eventually(t, "the cuts", func() bool {
		now := count()
		return now["192.0.2.10:9910"] == was["192.0.2.10:9910"]+1 && now["192.0.2.11:9910"] == was["192.0.2.11:9910"]+1 &&
			now["192.0.2.13:9910"] == was["192.0.2.13:9910"]+2
	})
	os.WriteFile(script, []byte("cut\nprogam 2\n"), 0o644)
	if code, _, errOut := b.run("exec", script); code != exitUsage || !strings.Contains(errOut, "line 2:") {
		t.Errorf("exec checks a line's first word: %d %q", code, errOut)
	}
}

// use HOST makes a host the default, where commands that name none go,
// opening no session; bmdctl marks it. BMDCTL_HOST still wins, and it
// says so.
func TestUse(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "Block C - TEST")
	b.must("hosts", "add", "192.0.2.11", "--as", "studio")
	b.must("hosts", "add", "192.0.2.13", "--as", "stage")
	b.must("hosts", "alias", "Block C - TEST", "bc")
	b.must("disconnect", "--host", "bc")
	b.must("disconnect", "--host", "studio")
	b.must("disconnect", "--host", "stage")
	if out := b.must("use", "bc"); out != "default host: Block C - TEST\n" {
		t.Errorf("use: %q", out)
	}
	if f := b.hostsFile(t); f.Default != "Block C - TEST" {
		t.Errorf("hosts.yml: %+v", f)
	}
	if got := b.cliLastHost(); got != "Block C - TEST" {
		t.Errorf("last host %q", got)
	}
	if n := len(b.dialled()); n != 3 {
		t.Errorf("use opened a session: %d dials", n)
	}
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.10:9910" {
		t.Errorf("cut went to %s", got)
	}
	// After a --host elsewhere, use moves it back.
	b.must("--host", "stage", "cut")
	if out := b.must(); !regexp.MustCompile(`(?m)^ +stage \(192\.0\.2\.13\) .*\(default\)$`).MatchString(out) {
		t.Errorf("the default is where commands go:\n%s", out)
	}
	if out := b.must("use", "STUD", "--json"); out != `{"ok":true,"confirmed":false,"result":{"default":"studio"},"took_ms":0}`+"\n" {
		t.Errorf("--json: %s", out)
	}
	if got := b.wentTo(t, "DCut", "cut"); got != "192.0.2.11:9910" {
		t.Errorf("cut went to %s", got)
	}
	if out := b.must("--json"); !strings.Contains(out, `"name":"studio","address":"192.0.2.11","device":"atem","default":true`) {
		t.Errorf("overview --json: %s", out)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"use"}, "bmdctl use: takes HOST"},
		{[]string{"use", "a", "b"}, "bmdctl use: takes HOST"},
		{[]string{"use", "st"}, `bmdctl use: "st" could be "studio" or "stage": give more of the name (bmdctl hosts lists them)`},
		{[]string{"use", "nobody"}, "bmdctl use: no host named nobody (bmdctl hosts lists them)"},
		{[]string{"use", "192.0.2.99"}, "bmdctl use: no host at 192.0.2.99 (bmdctl hosts add 192.0.2.99 saves it)"},
		{[]string{"use", "bc", "--pin"}, `bmdctl use: unexpected "--pin"`},
	} {
		if code, _, errOut := b.run(c.args...); code != exitUsage || !strings.HasPrefix(errOut, c.want) {
			t.Errorf("%q: %d %q", c.args, code, errOut)
		}
	}
	b.env[HostEnv] = "stage"
	if code, out, errOut := b.run("use", "bc"); code != 0 || out != "default host: Block C - TEST\n" ||
		errOut != "bmdctl: $BMDCTL_HOST=stage still wins over the default\n" {
		t.Errorf("BMDCTL_HOST: %d %q %q", code, out, errOut)
	}
	delete(b.env, HostEnv)
	if out := b.must("use", "--help"); !strings.HasPrefix(out, "usage: bmdctl use HOST\n") {
		t.Errorf("help: %q", out)
	}
}
