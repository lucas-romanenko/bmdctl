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
		{[]string{"hosts", "add", "192.0.2.21", "--device", "videohub"}, "can't ask a videohub its name yet: give it one with --as NAME"},
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
	if got := b.wentTo(t, "CPgI", "--host", "Studio", "program", "1"); got != "192.0.2.10:9910" {
		t.Fatalf("by name: %s", got)
	}
	// The last --host is remembered.
	if got := b.wentTo(t, "CPgI", "program", "2"); got != "192.0.2.10:9910" {
		t.Errorf("last used: %s", got)
	}
	// The start of a name, once it is only one name's.
	if got := b.wentTo(t, "CPgI", "--host", "OTH", "program", "1"); got != "192.0.2.11:9910" {
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
	if got := b.wentTo(t, "CPgI", "program", "1"); got != "192.0.2.13:9910" {
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
	if got := b.wentTo(t, "CPgI", "program", "1"); got != "192.0.2.12:9910" {
		t.Errorf("address: %s", got)
	}
	// Setting the default makes it the host commands go to now.
	b.must("hosts", "default", "other")
	if got := b.wentTo(t, "CPgI", "program", "1"); got != "192.0.2.11:9910" {
		t.Errorf("default: %s", got)
	}
	os.Remove(filepath.Join(b.env["XDG_STATE_HOME"], "bmdctl", "last-host"))
	if got := b.wentTo(t, "CPgI", "program", "2"); got != "192.0.2.11:9910" {
		t.Errorf("default without last-host: %s", got)
	}
	// A host's device type decides the device; it must match one named.
	if code, _, errOut := b.run("--host", "deck", "cut"); code != exitUsage || !strings.Contains(errOut, "no hyperdeck commands yet") {
		t.Errorf("hyperdeck host: %d %q", code, errOut)
	}
	if code, _, errOut := b.run("atem", "--host", "deck", "cut"); code != exitUsage || !strings.Contains(errOut, "deck (192.0.2.20) is a hyperdeck, not an atem") {
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
