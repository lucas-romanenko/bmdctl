// SPDX-License-Identifier: MIT

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeultimatte"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

// keyerBench is a bench with an Ultimatte at fakeultimatte's address,
// saved as the default host, in a directory of its own for the files
// archive writes.
func keyerBench(t *testing.T) (*bench, *fakeultimatte.Unit) {
	t.Helper()
	t.Chdir(t.TempDir())
	b := newBench(t)
	u := b.net.Keyer(fakeultimatte.Address, fakeultimatte.New())
	b.must("connect", fakeultimatte.Address)
	return b, u
}

// connect ADDRESS finds the keyer and saves it by its label; there is no
// session, and an ATEM's command says what it is.
func TestConnectAKeyer(t *testing.T) {
	b, _ := keyerBench(t)
	f := b.hostsFile(t)
	h := f.FindAddress(fakeultimatte.Address)
	if h == nil || h.Device != "ultimatte" || h.Name != "Keyer A" || f.Default != "Keyer A" {
		t.Errorf("host %+v, default %q", h, f.Default)
	}
	if out := b.must("connect"); out != "Keyer A (192.0.2.41)  answers, 3 presets\n" {
		t.Errorf("connect: %q", out)
	}
	code, _, errOut := b.run("program", "2")
	if code != exitUsage || errOut != "bmdctl: Keyer A (192.0.2.41) is an Ultimatte; bmdctl ultimatte --help\n" {
		t.Errorf("%d %q", code, errOut)
	}
	if out := b.must("ultimatte", "--help"); !strings.Contains(out, "  restore <file>  ") || !strings.Contains(out, "  network ...  ") {
		t.Errorf("help:\n%s", out)
	}
}

// Reads down a pipe: status, presets, the controls in the panel's
// units, the network interface, the prelude as it came; and as JSON.
func TestKeyerReads(t *testing.T) {
	b, _ := keyerBench(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"status"}, "Keyer A (192.0.2.41)  Ultimatte 12 4K\npresets  Green Screen, Blue Screen, Studio B\n" +
			"address  192.0.2.41/255.255.252.0\ngateway  192.0.2.1\ndns      192.0.2.53\ndhcp     off: static\nmac      7c:2e:0d:00:00:01\n"},
		{[]string{"presets"}, "Keyer A (192.0.2.41)  Ultimatte 12 4K\nGreen Screen\nBlue Screen\nStudio B\n"},
		{[]string{"network", "show"}, "Keyer A (192.0.2.41)  Ultimatte 12 4K\naddress  192.0.2.41/255.255.252.0\ngateway  192.0.2.1\n" +
			"dns      192.0.2.53\ndhcp     off: static\nmac      7c:2e:0d:00:00:01\n"},
	} {
		if out := b.must(c.args...); out != c.want {
			t.Errorf("%v:\n%s\nwant:\n%s", c.args, out, c.want)
		}
	}
	if out := b.must("controls"); !strings.Contains(out, "\nTransition Rate       30    30 frames\n") ||
		!strings.Contains(out, "\nOffset Black Balance  2500  -50%\n") {
		t.Errorf("controls:\n%s", out)
	}
	if out := b.must("controls", "--defaults"); !strings.Contains(out, "\nBlack Gloss           1000  10%\n") {
		t.Errorf("defaults:\n%s", out)
	}
	if out := b.must("prelude"); !strings.HasPrefix(out, "PROTOCOL PREAMBLE:\nVersion: 2.1\n\nIDENTITY:\nModel: Ultimatte 12 4K\n\nLabel: Keyer A\n") ||
		!strings.HasSuffix(out, "\nStudio B\n\nEND PRELUDE:\n") {
		t.Errorf("prelude:\n%s", out)
	}
	var resp struct {
		OK     bool                   `json:"ok"`
		Result engine.UltStatusResult `json:"result"`
	}
	json.Unmarshal([]byte(b.must("--json", "status")), &resp)
	n := resp.Result.Network
	if !resp.OK || resp.Result.Label != "Keyer A" || len(resp.Result.Presets) != 3 || n == nil || n.Address != fakeultimatte.Address ||
		n.DHCP == nil || *n.DHCP || n.MAC != "7c:2e:0d:00:00:01" {
		t.Errorf("%+v", resp)
	}
	var ct struct {
		Result engine.UltControls `json:"result"`
	}
	json.Unmarshal([]byte(b.must("--json", "controls")), &ct)
	if c := ct.Result.Controls; len(c) != 7 || c[0].Name != "Matte Density" || c[0].Value != "5000" || c[0].Display == nil ||
		*c[0].Display != 100 || c[0].Unit != "%" {
		t.Errorf("%+v", ct)
	}
}

// archive writes the zip, named by the label unless a file is given;
// restore asks first, then pushes it back, presets before settings.
func TestKeyerArchiveAndRestore(t *testing.T) {
	b, u := keyerBench(t)
	out := b.must("archive")
	if !strings.HasPrefix(out, "Keyer A (192.0.2.41)  saved Keyer_A.zip: 3 presets, GPISettings, SavedSettings, ") ||
		!strings.HasSuffix(out, " bytes\n") {
		t.Errorf("archive: %q", out)
	}
	z, err := os.ReadFile("Keyer_A.zip")
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil || len(r.File) != 9 || r.File[5].Name != "presets/Green Screen" {
		t.Fatalf("%v %d", err, len(r.File))
	}
	b.must("archive", "studio.zip")
	if _, err := os.Stat("studio.zip"); err != nil {
		t.Error(err)
	}
	code, _, errOut := b.run("restore", "studio.zip")
	if code != exitDestructive || !strings.Contains(errOut, "destructive: add --yes to run it") || len(u.Writes()) != 0 {
		t.Errorf("no --yes: %d %q, %d writes", code, errOut, len(u.Writes()))
	}
	if out := b.must("restore", "studio.zip", "--yes"); out != "Keyer A (192.0.2.41)  restored studio.zip: 3 presets, GPISettings, SavedSettings\n" {
		t.Errorf("restore: %q", out)
	}
	w := u.Writes()
	if len(w) != 5 || w[0].Name != ultimatte.WireNamePrefix+"/presets/Blue Screen" || !strings.HasSuffix(w[4].Name, "/SavedSettings") {
		t.Errorf("%+v", w)
	}
	// What isn't an archive is said so before anything is sent.
	os.WriteFile("notes.zip", []byte("not a zip"), 0o644)
	code, _, errOut = b.run("restore", "notes.zip", "--yes")
	if code != exitUsage || !strings.Contains(errOut, "notes.zip") || len(u.Writes()) != 5 {
		t.Errorf("not a zip: %d %q", code, errOut)
	}
}

// network set sends one block, waits for the interface to settle and
// says what changed; what the unit or bmdctl won't take is refused.
func TestKeyerNetwork(t *testing.T) {
	b, u := keyerBench(t)
	code, _, errOut := b.run("network", "set", "--gateway", "192.0.2.254")
	if code != exitDestructive || len(u.Blocks()) != 0 {
		t.Errorf("no --yes: %d %q, sent %q", code, errOut, u.Blocks())
	}
	out := b.must("network", "set", "--address", "192.0.2.42", "--netmask", "255.255.255.0", "--yes")
	want := "Keyer A (192.0.2.41)  network set  address 192.0.2.41 → 192.0.2.42, netmask 255.255.252.0 → 255.255.255.0  confirmed"
	if got := strings.Join(strings.Fields(out), " "); !strings.HasPrefix(got, strings.Join(strings.Fields(want), " ")) {
		t.Errorf("set: %q", out)
	}
	if a, m := u.Static(); a != "192.0.2.42" || m != "255.255.255.0" {
		t.Errorf("static %s/%s", a, m)
	}
	// The read, the set, a read-back that saw 0.0.0.0, the one that
	// settled.
	if bl := u.Blocks(); len(bl) != 4 || bl[1] != "NETWORK INTERFACE 0:\nStatic Addresses: 192.0.2.42/255.255.255.0" {
		t.Errorf("sent %q", bl)
	}
	out = b.must("network", "set", "--dns", "192.0.2.53, 192.0.2.54", "--dhcp=false", "--yes")
	if !strings.Contains(out, "dns 192.0.2.53 → 192.0.2.53, 192.0.2.54, dhcp off  confirmed") {
		t.Errorf("dns: %q", out)
	}
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"network", "set", "--address", "192.0.2.43"}, exitUsage, "--address and --netmask are one field on the wire"},
		{[]string{"network", "set", "--interface", "1"}, exitUsage, "nothing to set: give at least one of --address"},
		{[]string{"network", "set", "--interface", "1", "--gateway", "192.0.2.1"}, exitRejected, "the keyer refused NETWORK INTERFACE 1 (NAK)"},
		{[]string{"network", "set", "--address", "192.0.2.43", "--netmask", "24"}, exitRejected, "the keyer refused NETWORK INTERFACE 0 (NAK)"},
	} {
		code, _, errOut := b.run(append(c.args, "--yes")...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d %q", c.args, code, errOut, c.code, c.msg)
		}
	}
	u.RefuseNetwork(true)
	if out := b.must("status"); !strings.Contains(out, "\nnetwork  unknown: the keyer refused NETWORK INTERFACE 0 (NAK)\n") {
		t.Errorf("status, refused: %q", out)
	}
	u.Down(true)
	code, _, errOut = b.run("network", "show")
	if code != exitUnreachable || !strings.Contains(errOut, "nothing answers on TCP 9998: connection refused") {
		t.Errorf("down: %d %q", code, errOut)
	}
}

// At a terminal: the header names the keyer; reads lay out rows; archive
// and restore are steps and a line; a destructive command asks first,
// saying what it would change.
func TestTerminalKeyer(t *testing.T) {
	t.Chdir(t.TempDir())
	b := newBench(t)
	u := b.net.Keyer(fakeultimatte.Address, fakeultimatte.New())
	tr := &transcript{b: b, noHost: true}
	yes, no := "y\n", "n\n"
	tr.run("connect", fakeultimatte.Address)
	tr.run("status")
	tr.run("presets")
	tr.run("controls")
	tr.run("archive")
	tr.in = &yes
	u.SetFile("Studio B", "Modified") // changed on the unit, not saved: the restore says it loses them
	tr.run("restore", "Keyer_A.zip")
	tr.in = &no
	tr.run("network", "set", "--address", "192.0.2.42", "--netmask", "255.255.255.0")
	tr.run("network", "set", "--gateway", "192.0.2.254", "--dns", "192.0.2.53,192.0.2.54", "--yes")
	tr.run("network", "show")
	tr.run("program", "2")
	u.Down(true)
	tr.run("status")
	golden(t, "ultimatte.golden", tr.out.String())
}

// Every Ultimatte command runs against the fake keyer, as every ATEM
// command does against the fake switcher (TestEveryCommandRuns).
func TestEveryUltCommandRuns(t *testing.T) {
	b, _ := keyerBench(t)
	special := map[string][]string{
		"archive":     {"archive", "all.zip"},
		"restore":     {"restore", "all.zip", "--yes"},
		"network set": {"network", "set", "--gateway", "192.0.2.1", "--yes"},
	}
	c, _, _ := b.cli("")
	entries := c.reg.Entries("ultimatte")
	ran := 0
	for _, e := range entries {
		if e.Path == "restore" {
			continue // after archive has written its file
		}
		args := special[e.Path]
		if args == nil {
			args = strings.Fields(e.Path)
		}
		if code, out, errOut := b.run(args...); code != 0 {
			t.Errorf("%v: exit %d\n%s%s", args, code, out, errOut)
		}
		ran++
	}
	if code, out, errOut := b.run(special["restore"]...); code != 0 {
		t.Errorf("restore: exit %d\n%s%s", code, out, errOut)
	}
	if ran++; ran != len(entries) || ran < 9 {
		t.Errorf("ran %d of %d", ran, len(entries))
	}
}
