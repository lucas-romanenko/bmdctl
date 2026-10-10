// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// TestMain lets a test run this binary as bmdctl itself: a spawned
// engine re-executes the test binary with BMDCTL_TEST_MAIN set.
func TestMain(m *testing.M) {
	if os.Getenv("BMDCTL_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// bench is a CLI wired to an engine running in the test, whose sessions
// go to fake switchers: one per dial, each replaying dump. Its files
// live in a temporary home.
type bench struct {
	t    *testing.T
	dir  string
	env  map[string]string
	eng  *engine.Engine
	dump []byte

	mu    sync.Mutex
	sws   []*fakeatem.Switcher
	addrs []string // the address of each dial
	// onDial, if set, sees each new fake switcher before the session.
	onDial func(*fakeatem.Switcher)
	// dialErr, if set, fails every dial.
	dialErr error
}

func newBench(t *testing.T) *bench {
	return newBenchDump(t, fixtures.Bytes(t, "dump/initial-dump.bin"))
}

func newBenchDump(t *testing.T, dump []byte) *bench {
	t.Helper()
	// A short directory: Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "bmdctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := &bench{t: t, dir: dir, dump: dump, env: map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
		"BMDCTL_SOCKET":   filepath.Join(dir, "s.sock"),
	}}
	if err := b.startEngine(engine.Config{}); err != nil {
		t.Fatal(err)
	}
	return b
}

// startEngine runs an engine for the bench on its socket. It may run on
// any goroutine.
func (b *bench) startEngine(cfg engine.Config) error {
	reg, err := engine.NewRegistry()
	if err != nil {
		return err
	}
	l, err := engine.Listen(b.env["BMDCTL_SOCKET"])
	if err != nil {
		return err
	}
	cfg.Registry, cfg.Dial, cfg.Socket = reg, b.dial, l.Addr()
	eng := engine.New(cfg)
	b.mu.Lock()
	b.eng = eng
	b.mu.Unlock()
	served := make(chan struct{})
	go func() { eng.Serve(l); close(served) }()
	b.t.Cleanup(func() { eng.Stop(); <-served })
	return nil
}

// engine is the bench's current engine.
func (b *bench) engine() *engine.Engine {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eng
}

func (b *bench) dial(ctx context.Context, addr string) (*atem.Session, error) {
	b.mu.Lock()
	if b.dialErr != nil {
		err := b.dialErr
		b.mu.Unlock()
		return nil, err
	}
	sw := fakeatem.New(b.dump)
	b.sws = append(b.sws, sw)
	b.addrs = append(b.addrs, addr)
	on := b.onDial
	b.mu.Unlock()
	if on != nil {
		on(sw)
	}
	// Whatever the address, the fake switcher answers as its own.
	return (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
}

// dialled returns the addresses dialled so far.
func (b *bench) dialled() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.addrs...)
}

// switchers returns the fake switchers dialled so far.
func (b *bench) switchers() []*fakeatem.Switcher {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*fakeatem.Switcher(nil), b.sws...)
}

// sent returns the commands with this opcode the switchers received.
func (b *bench) sent(code string) [][]byte {
	var out [][]byte
	for _, sw := range b.switchers() {
		for _, c := range sw.Commands() {
			if string(c[4:8]) == code {
				out = append(out, c)
			}
		}
	}
	return out
}

func (b *bench) getenv(k string) string { return b.env[k] }

func (b *bench) cli(stdin string) (*cli, *bytes.Buffer, *bytes.Buffer) {
	var o, e bytes.Buffer
	c := newCLI(&o, &e)
	c.env = b.getenv
	c.stdin = strings.NewReader(stdin)
	c.environ = func() []string {
		var env []string
		for k, v := range b.env {
			env = append(env, k+"="+v)
		}
		return env
	}
	c.spawn = func(string, []string, []string) error { return errors.New("tests start no engine") }
	return c, &o, &e
}

// run runs a command line and returns its exit code and output.
func (b *bench) run(args ...string) (code int, stdout, stderr string) {
	return b.runIn("", args...)
}

func (b *bench) runIn(stdin string, args ...string) (code int, stdout, stderr string) {
	b.t.Helper()
	c, o, e := b.cli(stdin)
	code = c.run(args)
	return code, o.String(), e.String()
}

// must runs a command line that must succeed and returns its stdout.
func (b *bench) must(args ...string) string {
	b.t.Helper()
	code, out, errOut := b.run(args...)
	if code != 0 {
		b.t.Fatalf("%q: exit %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	return out
}

// host is the fake switcher's address flag.
var host = []string{"--host", fakeatem.Address}

func with(args ...string) []string { return append(append([]string{}, host...), args...) }

// runCLI runs a command line with no engine at all (help, dry runs).
func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	c := newCLI(&o, &e)
	dir := os.TempDir()
	c.env = func(k string) string {
		switch k {
		case "BMDCTL_SOCKET":
			return filepath.Join(dir, fmt.Sprintf("bmdctl-none-%d.sock", os.Getpid()))
		case "HOME":
			return dir
		}
		return ""
	}
	c.spawn = func(string, []string, []string) error { return errors.New("tests start no engine") }
	code = c.run(args)
	return code, o.String(), e.String()
}

// eventually polls f until it is true or a few seconds pass.
func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !f(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
