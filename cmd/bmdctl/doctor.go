// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/hyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
	"github.com/lucas-romanenko/bmdctl/videohub"
)

const doctorUsage = `usage: bmdctl doctor [--json]

Checks what bmdctl depends on and says what is wrong: the engine (running,
its version), its socket and lock, the log and state directory, the hosts
file, BMDCTL_HOST and the last host used, and whether each host answers:
an ATEM through the engine's session if it has one, else by connecting
for a moment; another device by its TCP port. It never starts an engine.

Exit status: 0 nothing wrong (warnings allowed), 1 something
misconfigured, 2 only that a host didn't answer.
`

// doctorTimeout bounds each host's reachability check.
const doctorTimeout = 5 * time.Second

// devicePorts are the TCP ports of the devices bmdctl has no protocol for
// yet: a check that something listens.
var devicePorts = map[string]int{"hyperdeck": 9993, "videohub": 9990, "ultimatte": 9998}

// Check is one doctor finding.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail, unreachable
	Detail string `json:"detail"`
}

func (c *cli) doctorCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, doctorUsage)
		return exitOK
	}
	if len(words) > 0 || len(tokens) > 0 {
		return c.fail(exitUsage, "bmdctl doctor: takes nothing\n\n%s", doctorUsage)
	}
	var checks []Check
	add := func(name, status, format string, args ...any) {
		checks = append(checks, Check{name, status, fmt.Sprintf(format, args...)})
	}
	sock := paths.Socket(c.env)
	st := c.doctorEngine(sock, add)
	c.doctorSocket(sock, st != nil, add)
	c.doctorState(add)
	f := c.doctorHosts(add)
	c.doctorReach(f, st, add)

	code := exitOK
	for _, ch := range checks {
		switch {
		case ch.Status == "fail":
			code = exitUsage
		case ch.Status == "unreachable" && code == exitOK:
			code = exitUnreachable
		}
	}
	switch {
	case g.quiet:
	case g.json:
		b, _ := json.Marshal(engine.Response{OK: true, Result: map[string]any{"checks": checks}})
		fmt.Fprintf(c.stdout, "%s\n", b)
	default:
		var rows [][]string
		for _, ch := range checks {
			// Green means preview alone, so ok is plain; there is no
			// yellow in the palette, so a warning is bold.
			status := ch.Status
			switch status {
			case "ok":
				status = padRight(status, 11)
			case "warn":
				status = c.colour(bold, padRight(status, 11))
			default:
				status = c.colour(red, padRight(status, 11))
			}
			rows = append(rows, []string{status, ch.Name, ch.Detail})
		}
		for _, l := range (&printer{c: c}).table(rows) {
			fmt.Fprintln(c.stdout, l)
		}
	}
	return code
}

// doctorEngine asks a running engine for its status.
func (c *cli) doctorEngine(sock string, add func(name, status, format string, args ...any)) *engine.Status {
	conn, err := engine.Dial(c.ctx, sock)
	if err != nil {
		if engine.NotRunning(err) {
			add("engine", "warn", "not running at %s; any command starts one", sock)
		} else {
			add("engine", "fail", "%s: %v", sock, err)
		}
		return nil
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	defer cancel()
	resp, err := engine.Call(ctx, conn, engine.Request{Device: "engine", Path: "status"}, nil)
	if err != nil {
		add("engine", "fail", "%s accepts connections but doesn't answer: %v", sock, err)
		return nil
	}
	if !resp.OK {
		add("engine", "fail", "%s; stop it (bmdctl engine stop) and retry", resp.Error)
		return nil
	}
	var st engine.Status
	json.Unmarshal(resp.Result.(json.RawMessage), &st)
	add("engine", "ok", "running, pid %d, up %s", st.PID, time.Since(st.Started).Round(time.Second))
	switch mine := c.build(); {
	case st.Build != "" && mine != "" && st.Build != mine && st.Background:
		add("engine version", "warn", "the engine runs another build of bmdctl (%s); the next command restarts it", st.Version)
	case st.Build != "" && mine != "" && st.Build != mine:
		add("engine version", "warn", "the engine runs another build of bmdctl (%s), started by hand: restart it yourself", st.Version)
	case st.Build == "" && st.Version != versionString():
		add("engine version", "warn", "the engine is %q, this bmdctl %q: restart it (bmdctl engine stop)", st.Version, versionString())
	}
	return &st
}

// doctorSocket checks the socket file, its directory and the lock.
func (c *cli) doctorSocket(sock string, running bool, add func(name, status, format string, args ...any)) {
	if paths.IsPipe(sock) {
		add("socket", "ok", "named pipe %s", sock)
		return
	}
	from := "$" + paths.SocketEnv
	switch {
	case c.env(paths.SocketEnv) != "":
	case c.env("XDG_RUNTIME_DIR") != "":
		from = "$XDG_RUNTIME_DIR"
	default:
		from = "the temporary directory, as $XDG_RUNTIME_DIR is unset"
	}
	dir := filepath.Dir(sock)
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		add("socket", "warn", "%s doesn't exist yet (from %s); the engine creates it", dir, from)
		return
	case err != nil:
		add("socket", "fail", "%s: %v", dir, err)
		return
	case !fi.IsDir():
		add("socket", "fail", "%s is not a directory", dir)
		return
	}
	if uid, ok := owner(fi); ok && uid != os.Getuid() && os.Getuid() != 0 {
		add("socket", "fail", "%s belongs to user %d, not you: the engine won't use it", dir, uid)
		return
	}
	sfi, err := os.Lstat(sock)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if running {
			add("socket", "fail", "%s is gone, but an engine answered there", sock)
		} else {
			add("socket", "ok", "%s (from %s), free", sock, from)
		}
		return
	case err != nil:
		add("socket", "fail", "%s: %v", sock, err)
		return
	case sfi.Mode()&fs.ModeSocket == 0:
		add("socket", "fail", "%s is not a socket: remove it", sock)
		return
	}
	detail := fmt.Sprintf("%s (from %s)", sock, from)
	switch perm := sfi.Mode().Perm(); {
	case !running:
		add("socket", "warn", "%s is left from an engine that died; the next one replaces it", detail)
	case perm&0o077 != 0:
		add("socket", "warn", "%s is mode %v: other users can reach the engine", detail, perm)
	default:
		add("socket", "ok", "%s", detail)
	}
}

func owner(fi fs.FileInfo) (int, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	return statUID(fi)
}

// doctorState checks the state directory: the log, the last host.
func (c *cli) doctorState(add func(name, status, format string, args ...any)) {
	dir, err := paths.State(c.env)
	if err != nil {
		add("log", "fail", "no state directory: %v", err)
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		add("log", "fail", "%s: %v", dir, err)
		return
	}
	probe := filepath.Join(dir, ".doctor")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		add("log", "fail", "%s is not writable: %v", dir, err)
		return
	}
	os.Remove(probe)
	log := filepath.Join(dir, "engine.log")
	if fi, err := os.Stat(log); err == nil {
		add("log", "ok", "%s, %d KiB", log, (fi.Size()+1023)/1024)
	} else {
		add("log", "ok", "%s (not written yet)", log)
	}
}

// doctorHosts checks hosts.yml, BMDCTL_HOST and the last host.
func (c *cli) doctorHosts(add func(name, status, format string, args ...any)) *hosts.File {
	f, path, err := c.loadHosts()
	if err != nil {
		add("hosts", "fail", "%v", err)
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		add("hosts", "ok", "%s: none yet (bmdctl hosts add ADDRESS, or bmdctl hosts discover)", path)
	} else if errs := f.Check(); len(errs) > 0 {
		for _, e := range errs {
			add("hosts", "fail", "%s: %v", path, e)
		}
	} else {
		def := ""
		if f.Default != "" {
			def = ", default " + f.Default
		}
		add("hosts", "ok", "%s: %d host%s%s", path, len(f.Hosts), plural(len(f.Hosts)), def)
	}
	for _, v := range []struct{ name, spec string }{{HostEnv, c.env(HostEnv)}, {"last host", c.lastHost()}} {
		if v.spec == "" {
			continue
		}
		if _, _, err := f.Resolve(v.spec); err != nil {
			add(v.name, "fail", "%v", err)
		} else {
			add(v.name, "ok", "%s", v.spec)
		}
	}
	return f
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// doctorReach checks that each host answers, all at once.
func (c *cli) doctorReach(f *hosts.File, st *engine.Status, add func(name, status, format string, args ...any)) {
	if f == nil {
		return
	}
	type result struct {
		status, detail string
	}
	results := make([]result, len(f.Hosts))
	stop := c.spin(fmt.Sprintf("reaching %d host%s", len(f.Hosts), plural(len(f.Hosts))))
	defer stop()
	var wg sync.WaitGroup
	for i, h := range f.Hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, detail := c.reach(h, st)
			results[i] = result{status, detail}
		}()
	}
	wg.Wait()
	for i, h := range f.Hosts {
		add("host "+h.Label(), results[i].status, "%s %s: %s", h.Device, h.Address, results[i].detail)
	}
}

// reach checks one host: an ATEM through the engine's session if it has
// one, else by connecting; another device by its TCP port.
func (c *cli) reach(h hosts.Host, st *engine.Status) (status, detail string) {
	ctx, cancel := context.WithTimeout(c.ctx, doctorTimeout)
	defer cancel()
	switch h.Device {
	case "hyperdeck":
		return c.reachDeck(ctx, h, st)
	case "videohub":
		return c.reachHub(ctx, h, st)
	}
	if h.Device != "atem" {
		port, ok := devicePorts[h.Device]
		if !ok {
			return "warn", "no check for this device"
		}
		addr := h.Address
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, fmt.Sprint(port))
		}
		conn, err := c.dialTCP(ctx, addr)
		if err != nil {
			return "unreachable", fmt.Sprintf("nothing answers on TCP %s: %v", addr, unwrapNet(err))
		}
		conn.Close()
		return "ok", "TCP " + addr + " answers (bmdctl has no commands for it yet)"
	}
	if st != nil {
		if key, err := hostKeyOf(h.Address); err == nil {
			for _, s := range st.Sessions {
				if s.Host == key && s.Ready {
					return "ok", fmt.Sprintf("the engine's session is ready: %s %s", s.Product, s.VideoMode)
				}
			}
		}
	}
	s, err := c.dialATEM(ctx, h.Address)
	if err != nil {
		return "unreachable", unwrapNet(err).Error()
	}
	state := s.State()
	s.Close()
	mode := ""
	if m := state.VideoMode.Mode; m != nil {
		mode = m.Label()
	}
	return "ok", strings.TrimSpace(state.ProductName("an ATEM") + " " + mode)
}

// reachDeck checks a HyperDeck: through the engine's session if it has
// one, else by reading its greeting and device info.
func (c *cli) reachDeck(ctx context.Context, h hosts.Host, st *engine.Status) (status, detail string) {
	addr := h.Address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, fmt.Sprint(hyperdeck.DefaultPort))
	}
	if st != nil {
		for _, s := range st.Sessions {
			if hosts.SameAddress(s.Host, addr) && s.Device == "hyperdeck" && s.Ready {
				return "ok", fmt.Sprintf("the engine's session is ready: %s", strings.Join(nonEmpty(s.Product, s.VideoMode), " "))
			}
		}
	}
	d := hyperdeck.Dialer{Bare: true, DialContext: func(ctx context.Context, _, a string) (net.Conn, error) { return c.dialTCP(ctx, a) }}
	s, err := d.Dial(ctx, addr)
	if err != nil {
		return "unreachable", unwrapNet(err).Error()
	}
	defer s.Close()
	if g := s.Greeting(); g.Code != 500 {
		return "fail", fmt.Sprintf("TCP %s answers, but not as a HyperDeck: %d %s", addr, g.Code, g.Text)
	}
	detail = strings.TrimSpace(s.Model() + ", protocol " + s.ProtocolVersion())
	if info, err := s.DeviceInfo(ctx); err == nil && info["software version"] != "" {
		detail += ", software " + info["software version"]
	}
	return "ok", detail
}

// reachHub checks a Videohub: through the engine's session if it has
// one, else by reading its preamble.
func (c *cli) reachHub(ctx context.Context, h hosts.Host, st *engine.Status) (status, detail string) {
	addr := h.Address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, fmt.Sprint(videohub.DefaultPort))
	}
	if st != nil {
		for _, s := range st.Sessions {
			if hosts.SameAddress(s.Host, addr) && s.Device == "videohub" && s.Ready {
				return "ok", "the engine's session is ready: " + s.Product
			}
		}
	}
	d := videohub.Dialer{DialContext: func(ctx context.Context, _, a string) (net.Conn, error) { return c.dialTCP(ctx, a) }}
	s, err := d.Dial(ctx, addr)
	var ve *videohub.Error
	switch {
	case errors.As(err, &ve):
		return "fail", fmt.Sprintf("TCP %s answers, but not as a Videohub: %v", addr, err)
	case err != nil:
		return "unreachable", fmt.Sprintf("nothing answers on TCP %s: %v", addr, unwrapNet(err))
	}
	defer s.Close()
	snap, err := s.Snapshot()
	if err != nil {
		return "fail", fmt.Sprintf("TCP %s answers as a Videohub, but its device block has %v", addr, err)
	}
	return "ok", fmt.Sprintf("%s, %d x %d, protocol %s", snap.Device.ModelName, snap.Device.VideoInputs,
		snap.Device.VideoOutputs, snap.Device.ProtocolVersion)
}

// hostKeyOf is the engine's session key for an address.
func hostKeyOf(addr string) (string, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		h, p = strings.Trim(addr, "[]"), fmt.Sprint(atem.DefaultPort)
	}
	return net.JoinHostPort(strings.ToLower(h), p), nil
}

func unwrapNet(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("no answer within %v", doctorTimeout)
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	return err
}
