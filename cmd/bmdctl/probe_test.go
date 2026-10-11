// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fakenet"
)

// devices puts one of each device on the bench's network: the ATEM at
// fakeatem's address, a deck, a Videohub, an Ultimatte, and nothing at
// 192.0.2.99.
func devices(b *bench) {
	b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	b.net.Videohub("192.0.2.30", fakenet.VideohubPreamble)
	b.net.Ultimatte("192.0.2.40", fakenet.UltimattePrelude)
	b.net.Absent("192.0.2.99")
}

// connect ADDRESS asks what answers there, and hosts.yml records it as
// what it is, by its own name, the default.
func TestConnectAsksTheAddress(t *testing.T) {
	b := newBench(t)
	devices(b)
	for _, c := range []struct{ addr, device, name, line string }{
		{"192.0.2.30", "videohub", "Test Hub", `Test Hub (192.0.2.30)  Videohub  saved as "Test Hub", the default; bmdctl has no Videohub commands yet`},
		{"192.0.2.40", "ultimatte", "Keyer A", `Keyer A (192.0.2.40)  Ultimatte  saved as "Keyer A", the default; bmdctl has no Ultimatte commands yet`},
		{"192.0.2.10", "atem", "ATEM 1 M/E Constellation HD", ""},
	} {
		out := b.must("connect", c.addr)
		if c.line != "" && strings.TrimSpace(out) != c.line {
			t.Errorf("connect %s: %q", c.addr, out)
		}
		f := b.hostsFile(t)
		h := f.FindAddress(c.addr)
		if h == nil || h.Device != c.device || h.Name != c.name || f.Default != c.name {
			t.Errorf("connect %s: host %+v, default %q", c.addr, h, f.Default)
		}
	}
	// What a host is decides where its commands go.
	code, _, errOut := b.run("--host", "test hub", "program", "2")
	if code != exitUsage || !strings.Contains(errOut, "Test Hub (192.0.2.30) is a Videohub; bmdctl has no Videohub commands yet") {
		t.Errorf("%d %q", code, errOut)
	}
	// --type says what it is, without asking: the host is retyped.
	b.must("connect", "192.0.2.40", "--type", "videohub")
	if h := b.hostsFile(t).FindAddress("192.0.2.40"); h.Device != "videohub" {
		t.Errorf("--type: %+v", h)
	}
	if code, _, errOut := b.run("connect", "192.0.2.40", "--type", "toaster"); code != exitUsage || !strings.Contains(errOut, `--type "toaster" is not one of`) {
		t.Errorf("%d %q", code, errOut)
	}
	if code, _, errOut := b.run("atem", "connect", "192.0.2.40", "--type", "videohub"); code != exitUsage || !strings.Contains(errOut, "the line names atem") {
		t.Errorf("%d %q", code, errOut)
	}
	// Nothing answering is the engine's to say, with what to do.
	code, _, errOut = b.run("connect", "192.0.2.99")
	if code != exitUnreachable || !strings.Contains(errOut, "nothing answered at 192.0.2.99 within 2s as an ATEM (UDP 9910)") ||
		!strings.Contains(errOut, "(bmdctl connect ADDRESS --type DEVICE connects without asking)") {
		t.Errorf("%d %q", code, errOut)
	}
	// --json has what was found and saved.
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			engine.ProbeResult
			Saved string `json:"saved"`
		} `json:"result"`
	}
	b.net.Videohub("192.0.2.31", strings.Replace(fakenet.VideohubPreamble, "Test Hub", "Hub 2", 1))
	json.Unmarshal([]byte(b.must("--json", "connect", "192.0.2.31")), &r)
	if !r.OK || r.Result.Device != "videohub" || r.Result.Name != "Hub 2" || r.Result.Saved != "Hub 2" ||
		r.Result.Model != "Blackmagic Smart Videohub 40 x 40" {
		t.Errorf("%+v", r)
	}
}

// hosts add asks what answers too, and takes the name it gives.
func TestHostsAddAsksTheAddress(t *testing.T) {
	b := newBench(t)
	devices(b)
	b.must("hosts", "add", "192.0.2.40")
	b.must("hosts", "add", fakehyperdeck.Address)
	f := b.hostsFile(t)
	if h := f.FindAddress("192.0.2.40"); h == nil || h.Device != "ultimatte" || h.Name != "Keyer A" {
		t.Errorf("%+v", h)
	}
	if h := f.FindAddress(fakehyperdeck.Address); h == nil || h.Device != "hyperdeck" || h.Name != "Deck 1" {
		t.Errorf("%+v", h)
	}
	if code, _, errOut := b.run("hosts", "add", "192.0.2.99"); code != exitUnreachable || !strings.Contains(errOut, "nothing answered") {
		t.Errorf("%d %q", code, errOut)
	}
	b.must("hosts", "add", "192.0.2.99", "--as", "spare")
	if h := b.hostsFile(t).Find("spare"); h == nil || h.Device != "atem" {
		t.Errorf("%+v", h)
	}
}

// help lists the device groups hosts.yml has a host of, and names the
// others in one line.
func TestHelpListsTheDevicesThere(t *testing.T) {
	b := newBench(t)
	devices(b)
	out := b.must("help")
	if strings.Contains(out, "  atem  ") || !strings.Contains(out, "also atem, hyperdeck, videohub, ultimatte (no host in hosts.yml yet)") {
		t.Errorf("no hosts:\n%s", out)
	}
	b.must("connect", "192.0.2.30")
	out = b.must("help")
	if !strings.Contains(out, "  videohub    a Videohub router (no commands yet)") ||
		!strings.Contains(out, "also atem, hyperdeck, ultimatte (none in hosts.yml)") {
		t.Errorf("a videohub:\n%s", out)
	}
}

// At a terminal: the header names what answered, then what was saved.
func TestTerminalProbe(t *testing.T) {
	b := newBench(t)
	devices(b)
	tr := &transcript{b: b}
	tr.run("connect", "192.0.2.30")
	tr.run("connect", "192.0.2.40")
	tr.run("connect", "192.0.2.30") // the default already
	tr.run("connect", "192.0.2.99")
	tr.run("--host", "test hub", "program", "2")
	golden(t, "probe.golden", tr.out.String())
}
