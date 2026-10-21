// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fakeultimatte"
)

// doctor checks against the bench: its reachability checks go to the
// fake switcher and a stubbed TCP dialler, never the network.
func (b *bench) doctor(args ...string) (int, string) {
	b.t.Helper()
	c, o, e := b.cli("")
	c.dialATEM = b.dial
	c.dialTCP = func(ctx context.Context, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "192.0.2.20:") {
			return fakehyperdeck.New().Dial(ctx, "tcp", addr)
		}
		if strings.HasPrefix(addr, fakeultimatte.Address+":") {
			return fakeultimatte.New().Dial(ctx, "tcp", addr)
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}
	code := c.run(append([]string{"doctor"}, args...))
	// Columns line up by content: compare with single spaces.
	return code, strings.Join(strings.FieldsFunc(o.String()+e.String(), func(r rune) bool { return r == ' ' }), " ")
}

func TestDoctor(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", "192.0.2.12", "--as", "spare")
	b.must("hosts", "add", "192.0.2.20", "--as", "deck", "--device", "hyperdeck")
	b.must("hosts", "add", "192.0.2.21", "--as", "hub", "--device", "videohub")
	b.must("hosts", "add", fakeultimatte.Address, "--as", "keyer", "--device", "ultimatte")
	b.must("hosts", "default", "studio")
	b.must("--host", "studio", "cut") // the engine holds studio's session

	code, out := b.doctor()
	for _, want := range []string{
		"ok engine",
		"warn engine version", // the bench's engine reports no version
		"ok socket " + b.env["BMDCTL_SOCKET"] + " (from $BMDCTL_SOCKET)",
		"ok log",
		"5 hosts, default studio",
		"ok last host studio",
		"host studio atem 192.0.2.10: the engine's session is ready: ATEM 1 M/E Constellation HD 1080p50",
		"host spare atem 192.0.2.12: the engine's session is ready: ATEM 1 M/E Constellation HD 1080p50",
		"ok host deck hyperdeck 192.0.2.20: HyperDeck Studio HD Mini, protocol 1.13, software 8.1.1",
		"unreachable host hub videohub 192.0.2.21: nothing answers on TCP 192.0.2.21:9990: connection refused",
		"ok host keyer ultimatte 192.0.2.41: Keyer A answers, 3 presets",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if code != exitUnreachable {
		t.Errorf("exit %d, want 2 (a host only didn't answer)", code)
	}
	// It opened no session of its own: hosts add left the engine one to
	// each switcher.
	if n := len(b.switchers()); n != 2 {
		t.Errorf("%d sessions", n)
	}

	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Checks []Check `json:"checks"`
		} `json:"result"`
	}
	b.doctorJSON(t, &r)
	if !r.OK || len(r.Result.Checks) < 8 {
		t.Errorf("--json: %+v", r)
	}

	// Misconfigurations fail it: a default that isn't a host, a bad
	// BMDCTL_HOST, a socket left by a dead engine.
	path := filepath.Join(b.env["XDG_CONFIG_HOME"], "bmdctl", "hosts.yml")
	os.WriteFile(path, []byte("default: gone\nhosts:\n  - name: studio\n    address: 192.0.2.10\n"), 0o644)
	b.env[HostEnv] = "no such host!"
	b.engine().Stop()
	l, err := net.Listen("unix", b.env["BMDCTL_SOCKET"])
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	code, out = b.doctor()
	for _, want := range []string{
		"warn engine not running at",
		"is left from an engine that died",
		"fail hosts " + path + ": the default, gone, is not a host",
		`fail BMDCTL_HOST "no such host!" is neither a host in hosts.yml nor an address`,
		"host studio atem 192.0.2.10: ATEM 1 M/E Constellation HD",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if code != exitUsage {
		t.Errorf("exit %d, want 1", code)
	}
	// An unreachable ATEM.
	delete(b.env, HostEnv)
	b.mu.Lock()
	b.dialErr = errors.New("atem: dial 192.0.2.10:9910: context deadline exceeded")
	b.mu.Unlock()
	if _, out = b.doctor(); !strings.Contains(out, "unreachable host studio") {
		t.Errorf("unreachable ATEM:\n%s", out)
	}
}

func (b *bench) doctorJSON(t *testing.T, v any) {
	t.Helper()
	c, o, _ := b.cli("")
	c.dialATEM = func(ctx context.Context, addr string) (*atem.Session, error) { return b.dial(ctx, addr) }
	c.dialTCP = func(ctx context.Context, addr string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	c.run([]string{"doctor", "--json"})
	if err := json.Unmarshal(o.Bytes(), v); err != nil {
		t.Fatalf("%v: %s", err, o)
	}
}
