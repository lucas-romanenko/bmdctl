// SPDX-License-Identifier: MIT

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func env(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestXDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG layout")
	}
	e := env(map[string]string{"HOME": "/home/u"})
	if d, _ := Config(e); d != "/home/u/.config/bmdctl" {
		t.Errorf("config %q", d)
	}
	if d, _ := State(e); d != "/home/u/.local/state/bmdctl" {
		t.Errorf("state %q", d)
	}
	if s := Socket(e); s != filepath.Join("/tmp", fmt.Sprintf("bmdctl-%d", os.Getuid()), "bmdctl.sock") {
		t.Errorf("socket %q", s)
	}
	e = env(map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "relative",
		"XDG_RUNTIME_DIR": "/run/user/1000"})
	if d, _ := Config(e); d != "/c/bmdctl" {
		t.Errorf("config %q", d)
	}
	if d, _ := State(e); d != "/home/u/.local/state/bmdctl" {
		t.Errorf("relative XDG_STATE_HOME not ignored: %q", d)
	}
	if s := Socket(e); s != "/run/user/1000/bmdctl.sock" {
		t.Errorf("socket %q", s)
	}
	if s := Socket(env(map[string]string{SocketEnv: "/x.sock", "XDG_RUNTIME_DIR": "/run"})); s != "/x.sock" {
		t.Errorf("override %q", s)
	}
	if _, err := Config(env(nil)); err == nil {
		t.Error("no HOME accepted")
	}
}
