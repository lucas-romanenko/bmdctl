// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
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

func TestHostsFile(t *testing.T) {
	b := newBench(t)
	if out := b.must("hosts"); out != "no hosts (bmdctl hosts add NAME ADDRESS)\n" {
		t.Errorf("empty: %q", out)
	}
	b.must("hosts", "add", "studio", "192.0.2.10")
	b.must("hosts", "add", "deck", "192.0.2.20:9993", "--device", "hyperdeck")
	b.must("hosts", "add", "b", "192.0.2.30")
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"hosts", "add", "studio", "192.0.2.11"}, "already a host"},
		{[]string{"hosts", "add", "1st", "192.0.2.11"}, "use a letter"},
		{[]string{"hosts", "add", "x", "not an address"}, "not an IP address"},
		{[]string{"hosts", "add", "x", "192.0.2.11", "--device", "toaster"}, "not one of"},
		{[]string{"hosts", "add", "x"}, "takes NAME ADDRESS"},
		{[]string{"hosts", "rm", "nobody"}, "no host named nobody"},
		{[]string{"hosts", "default", "nobody"}, "no host named nobody"},
		{[]string{"hosts", "frobnicate"}, "usage: bmdctl hosts"},
		{[]string{"hosts", "list", "--device", "atem"}, "unexpected"},
	} {
		if code, _, errOut := b.run(c.args...); code != exitUsage || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %q", c.args, code, errOut, c.msg)
		}
	}
	b.must("hosts", "default", "STUDIO")
	out := b.must("hosts", "list")
	want := "studio  192.0.2.10       atem       default\ndeck    192.0.2.20:9993  hyperdeck\nb       192.0.2.30       atem\n"
	if out != want {
		t.Errorf("list:\n%s\nwant:\n%s", out, want)
	}
	var r struct {
		OK     bool       `json:"ok"`
		Result hosts.File `json:"result"`
	}
	if err := json.Unmarshal([]byte(b.must("hosts", "--json")), &r); err != nil || !r.OK || r.Result.Default != "studio" || len(r.Result.Hosts) != 3 {
		t.Errorf("--json: %+v %v", r, err)
	}
	b.must("hosts", "rm", "studio")
	if out := b.must("hosts", "default"); out != "no default host\n" {
		t.Errorf("default after rm: %q", out)
	}
	path := filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "  - name: deck\n    address: 192.0.2.20:9993\n    device: hyperdeck\n") {
		t.Errorf("hosts.yml:\n%s", data)
	}
	os.WriteFile(path, []byte("hosts:\n  - name: a\n\taddress: x\n"), 0o644)
	if code, _, errOut := b.run("hosts"); code != exitUsage || !strings.Contains(errOut, "hosts.yml: line 3") {
		t.Errorf("broken file: %d %q", code, errOut)
	}
}

// --host takes a name or an address; without it a command goes to
// BMDCTL_HOST, else the last --host, else the default.
func TestHostResolution(t *testing.T) {
	b := newBench(t)
	if code, _, errOut := b.run("cut"); code != exitUsage || !strings.Contains(errOut, "no switcher named") || !strings.Contains(errOut, "--dry-run") {
		t.Errorf("no host: %d %q", code, errOut)
	}
	b.must("hosts", "add", "studio", "192.0.2.10")
	b.must("hosts", "add", "other", "192.0.2.11")
	b.must("hosts", "add", "deck", "192.0.2.20", "--device", "hyperdeck")
	last := func() string {
		d := b.dialled()
		if len(d) == 0 {
			return ""
		}
		return d[len(d)-1]
	}
	b.must("--host", "Studio", "program", "1")
	if last() != "192.0.2.10:9910" {
		t.Fatalf("by name: %v", b.dialled())
	}
	// The last --host is remembered.
	b.must("program", "2")
	if n := len(b.dialled()); n != 1 {
		t.Errorf("last used: %v", b.dialled())
	}
	// BMDCTL_HOST wins over it.
	b.env[HostEnv] = "other"
	b.must("cut")
	if last() != "192.0.2.11:9910" {
		t.Errorf("BMDCTL_HOST: %v", b.dialled())
	}
	delete(b.env, HostEnv)
	// A bare address works and becomes the last used.
	b.must("--host", "192.0.2.12", "cut")
	b.must("cut")
	if last() != "192.0.2.12:9910" || len(b.dialled()) != 3 {
		t.Errorf("address: %v", b.dialled())
	}
	// Setting the default makes it the host commands go to now.
	b.must("hosts", "default", "other")
	b.must("cut")
	if got := b.dialled(); len(got) != 3 {
		t.Errorf("default: %v", got)
	}
	os.Remove(filepath.Join(b.env["XDG_STATE_HOME"], "bmdctl", "last-host"))
	b.must("cut")
	if got := b.dialled(); len(got) != 3 {
		t.Errorf("default without last-host: %v", got)
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
