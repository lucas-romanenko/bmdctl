// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The build ID read is the one go tool buildid prints; a file that isn't
// ELF is searched for the marked string Mach-O and PE files carry.
func TestBuildID(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if goTool, err := exec.LookPath("go"); err == nil {
		out, err := exec.Command(goTool, "tool", "buildid", exe).Output()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := goBuildID(exe), strings.TrimSpace(string(out)); got == "" || got != want {
			t.Errorf("build ID %q, go tool buildid %q", got, want)
		}
	}
	if buildID() != goBuildID(exe) {
		t.Errorf("buildID %q", buildID())
	}
	dir := t.TempDir()
	marked := filepath.Join(dir, "marked")
	os.WriteFile(marked, []byte("\x00\x01header\xff Go build ID: \"aaa/bbb/ccc/ddd\"\n \xff rest"), 0o644)
	if got := goBuildID(marked); got != "aaa/bbb/ccc/ddd" {
		t.Errorf("marked: %q", got)
	}
	plain := filepath.Join(dir, "plain")
	os.WriteFile(plain, []byte("no id here"), 0o644)
	if got := goBuildID(plain); got != "" {
		t.Errorf("plain: %q", got)
	}
}
