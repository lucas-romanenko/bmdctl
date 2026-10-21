// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
)

func TestAliasFor(t *testing.T) {
	for argv0, want := range map[string]string{
		"/usr/local/bin/atemctl": "atem", "hdctl": "hyperdeck", "./vhctl": "videohub", "ultctl": "ultimatte",
		`C:\Users\x\scoop\shims\HDCTL.EXE`: "hyperdeck", "bmdctl": "", "/usr/bin/bmdctl": "", "hdctl2": "",
	} {
		if got, _ := aliasFor(argv0); got != want {
			t.Errorf("aliasFor(%q) = %q, want %q", argv0, got, want)
		}
	}
}

// runAs runs a command line as bmdctl would run invoked by an alias.
func (b *bench) runAs(name string, args ...string) (int, string, string) {
	b.t.Helper()
	c, o, e := b.cli("")
	c.alias, c.aliasName = aliasFor(name)
	code := c.run(args)
	return code, o.String(), e.String()
}

// An alias is bmdctl with its device's word in front; its help is that
// device's alone.
func TestAliases(t *testing.T) {
	b := newBench(t)
	b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	b.must("connect", fakehyperdeck.Address)

	code, out, _ := b.runAs("hdctl", "--help")
	if code != 0 || !strings.HasPrefix(out, "usage: hdctl <command> [arguments]") || !strings.Contains(out, "  play  ") ||
		strings.Contains(out, "program") || !strings.Contains(out, "Run 'hdctl <command> --help'") {
		t.Errorf("hdctl --help: %d\n%s", code, out)
	}
	for _, args := range [][]string{nil, {"help"}} {
		if code, out2, _ := b.runAs("hdctl", args...); code != 0 || out2 != out {
			t.Errorf("hdctl %v: %d\n%s", args, code, out2)
		}
	}
	code, out, _ = b.runAs("hdctl", "play", "--help")
	if code != 0 || !strings.Contains(out, "\n  hdctl play --speed 200\n") || !strings.Contains(out, "usage: hdctl play") {
		t.Errorf("hdctl play --help: %d\n%s", code, out)
	}
	if code, out, errOut := b.runAs("hdctl", "--json", "play"); code != 0 || !strings.HasPrefix(out, `{"ok":true,"confirmed":true`) {
		t.Errorf("hdctl play: %d %q %q", code, out, errOut)
	}
	if code, out, _ := b.runAs("hdctl", "hyperdeck", "stop"); code != 0 || !strings.Contains(out, "play → stopped") {
		t.Errorf("hdctl hyperdeck stop: %d %q", code, out)
	}
	// Built-ins are bmdctl's.
	if code, out, _ := b.runAs("hdctl", "hosts"); code != 0 || !strings.Contains(out, "Deck 1") {
		t.Errorf("hdctl hosts: %d %q", code, out)
	}
	if code, out, _ := b.runAs("atemctl", "version"); code != 0 || !strings.HasPrefix(out, "bmdctl ") {
		t.Errorf("atemctl version: %d %q", code, out)
	}
	// Another device's word, or a command its device doesn't have.
	if code, _, errOut := b.runAs("atemctl", "hyperdeck", "play"); code != exitUsage ||
		!strings.Contains(errOut, "atemctl runs ATEM commands; bmdctl hyperdeck ... runs a HyperDeck's") {
		t.Errorf("atemctl hyperdeck: %d %q", code, errOut)
	}
	if code, _, errOut := b.runAs("atemctl", "--host", "192.0.2.10", "play"); code != exitUsage || !strings.Contains(errOut, "play") {
		t.Errorf("atemctl play: %d %q", code, errOut)
	}
	if code, out, _ := b.runAs("atemctl", "--host", "192.0.2.10", "program", "2"); code != 0 || !strings.Contains(out, "program") {
		t.Errorf("atemctl program: %d %q", code, out)
	}
	if code, out, _ := b.runAs("vhctl"); code != 0 || !strings.Contains(out, "usage: vhctl <command>") || !strings.Contains(out, "route <output> <input>") {
		t.Errorf("vhctl: %d %q", code, out)
	}
	if code, out, _ := b.runAs("ultctl"); code != 0 || !strings.Contains(out, "bmdctl has no Ultimatte commands yet") {
		t.Errorf("ultctl: %d %q", code, out)
	}
	// Completion completes the device's line.
	if code, out, _ := b.runAs("hdctl", "__complete", "pl"); code != 0 || out != "play\n" {
		t.Errorf("hdctl __complete pl: %d %q", code, out)
	}
}

// Run through main as hdctl: argv[0] is what decides.
func TestAliasArgv0(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "hdctl")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link, "--help")
	cmd.Env = append(os.Environ(), "BMDCTL_TEST_MAIN=1", "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(out), "usage: hdctl <command>") {
		t.Errorf("hdctl --help: %v\n%s", err, out)
	}
}
