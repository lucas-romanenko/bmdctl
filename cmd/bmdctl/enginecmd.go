// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
)

// spawnWait is how long a command waits for an engine it started.
const spawnWait = 5 * time.Second

// connectEngine connects to the engine, starting one in the background
// if none is listening.
func (c *cli) connectEngine() (io.ReadWriteCloser, error) {
	sock := paths.Socket(c.env)
	conn, err := engine.Dial(c.ctx, sock)
	if err == nil {
		return conn, nil
	}
	if !engine.NotRunning(err) {
		return nil, exitf(exitUnreachable, "engine at %s: %v", sock, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, exitf(exitUnreachable, "starting the engine: %v", err)
	}
	if err := c.spawn(exe, []string{"engine", "--background"}, c.environ()); err != nil {
		return nil, exitf(exitUnreachable, "starting the engine: %v", err)
	}
	deadline := time.Now().Add(spawnWait)
	for wait := 10 * time.Millisecond; ; wait = min(2*wait, 200*time.Millisecond) {
		select {
		case <-c.ctx.Done():
			return nil, exitf(exitUnreachable, "%v", c.ctx.Err())
		case <-time.After(wait):
		}
		conn, err := engine.Dial(c.ctx, sock)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, exitf(exitUnreachable, "started an engine, but nothing answers at %s after %v (its log: %s)",
				sock, spawnWait, c.logPath())
		}
	}
}

// dialRunning connects to an engine that is already running, or returns
// errNoEngine.
func (c *cli) dialRunning() (io.ReadWriteCloser, error) {
	sock := paths.Socket(c.env)
	conn, err := engine.Dial(c.ctx, sock)
	if err != nil {
		if engine.NotRunning(err) {
			return nil, errNoEngine
		}
		return nil, exitf(exitUnreachable, "engine at %s: %v", sock, err)
	}
	return conn, nil
}

func (c *cli) logPath() string {
	d, err := paths.State(c.env)
	if err != nil {
		return "(none: " + err.Error() + ")"
	}
	return filepath.Join(d, "engine.log")
}

const engineUsage = `usage: bmdctl engine [--log-level debug|info|warn|error]
       bmdctl engine stop
       bmdctl engine status [--json]

The engine keeps one session open to each switcher and runs every command
through it. Any command starts one in the background when none is
running, and restarts one it started from another build of bmdctl;
'bmdctl engine' runs one in the foreground instead, for systemd or
Docker, logging to stderr as well as its log file, which answers every
build. A session closes after 10 minutes without a command unless
'connect' pinned it.

The socket is $BMDCTL_SOCKET, else $XDG_RUNTIME_DIR/bmdctl.sock (a named
pipe on Windows); the log is $XDG_STATE_HOME/bmdctl/engine.log, rotated.
`

// engineFlags are "bmdctl engine"'s own flags.
type engineFlags struct {
	logLevel string
	bg       bool // started by a command: log to the file only
}

func (c *cli) engineCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, engineUsage)
		return exitOK
	}
	sub := ""
	if len(words) > 0 {
		sub = words[0]
	}
	if len(words) > 1 {
		return c.fail(exitUsage, "bmdctl engine %s: unexpected %q", sub, words[1])
	}
	var f engineFlags
	for i := 0; i < len(tokens); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		switch {
		case sub == "" && name == "background" && !hasValue:
			f.bg = true
		case sub == "" && name == "log-level":
			if !hasValue {
				if i+1 == len(tokens) {
					return c.fail(exitUsage, "bmdctl engine: --log-level needs a value")
				}
				i++
				value = tokens[i]
			}
			f.logLevel = value
		default:
			return c.fail(exitUsage, "bmdctl engine %s: unexpected %q", sub, tokens[i])
		}
	}
	switch sub {
	case "":
		return c.serveEngine(f)
	case "stop":
		return c.stopEngine(g)
	case "status":
		return c.engineStatus(g)
	}
	fmt.Fprint(c.stderr, engineUsage)
	return exitUsage
}

// serveEngine runs the engine in this process until it is stopped or
// interrupted.
func (c *cli) serveEngine(g engineFlags) int {
	level := slog.LevelInfo
	if g.logLevel != "" {
		if err := level.UnmarshalText([]byte(g.logLevel)); err != nil {
			return c.fail(exitUsage, "bmdctl engine: --log-level %q: not debug, info, warn or error", g.logLevel)
		}
	}
	sock := paths.Socket(c.env)
	l, err := engine.Listen(sock)
	// An engine stopping (for a client of another build) holds its lock
	// a moment after its socket has gone: a background start waits it
	// out, unless another engine answers.
	for deadline := time.Now().Add(2 * time.Second); g.bg && errors.Is(err, engine.ErrRunning) && time.Now().Before(deadline); {
		if conn, err := engine.Dial(c.ctx, sock); err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
		l, err = engine.Listen(sock)
	}
	if errors.Is(err, engine.ErrRunning) {
		if g.bg {
			return exitOK // another start won the race
		}
		return c.fail(exitUsage, "bmdctl engine: an engine is already running at %s (bmdctl engine stop)", sock)
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl engine: %v", err)
	}
	var sinks []io.Writer
	if !g.bg {
		sinks = append(sinks, c.stderr)
	}
	logPath := c.logPath()
	if lf, err := engine.OpenLog(logPath); err == nil {
		defer lf.Close()
		sinks = append(sinks, lf)
	} else {
		if !g.bg {
			fmt.Fprintf(c.stderr, "bmdctl engine: no log file: %v\n", err)
		}
		logPath = ""
	}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(sinks...), &slog.HandlerOptions{Level: level}))
	e := engine.New(c.engineConfig(engine.Config{Registry: c.reg, Logger: logger, Version: versionString(),
		BuildID: c.build(), Background: g.bg, Socket: sock, LogPath: logPath}))
	stop := context.AfterFunc(c.ctx, e.Stop)
	defer stop()
	err = e.Serve(l)
	e.Stop()
	logger.Info("engine stopped")
	if err != nil {
		return c.fail(exitUsage, "bmdctl engine: %v", err)
	}
	return exitOK
}

func (c *cli) stopEngine(g globals) int {
	conn, err := c.dialRunning()
	if errors.Is(err, errNoEngine) {
		if !g.quiet && !g.json {
			fmt.Fprintln(c.stdout, "no engine running")
		}
		return exitOK
	}
	if err != nil {
		return c.fail(codeFor(err), "bmdctl engine stop: %v", err)
	}
	resp, err := c.engineCall(conn, "stop", g)
	if err != nil {
		return c.fail(codeFor(err), "bmdctl engine stop: %v", err)
	}
	if !resp.OK {
		return c.fail(exitCode(resp.Code), "bmdctl engine stop: %s", resp.Error)
	}
	c.waitEngineGone()
	if !g.quiet && !g.json {
		fmt.Fprintln(c.stdout, "engine stopped")
	}
	return exitOK
}

// waitEngineGone waits, up to 6 s, for a stopping engine to let go of
// its socket, so a start right after works.
func (c *cli) waitEngineGone() {
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
		conn, err := engine.Dial(c.ctx, paths.Socket(c.env))
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *cli) engineStatus(g globals) int {
	conn, err := c.dialRunning()
	if errors.Is(err, errNoEngine) {
		if g.json {
			fmt.Fprintf(c.stdout, `{"ok":false,"code":%q,"error":"no engine running"}`+"\n", engine.CodeUnreachable)
		} else {
			fmt.Fprintf(c.stderr, "bmdctl engine status: no engine running at %s\n", paths.Socket(c.env))
		}
		return exitUnreachable
	}
	if err != nil {
		return c.fail(codeFor(err), "bmdctl engine status: %v", err)
	}
	resp, err := c.engineCall(conn, "status", g)
	if err != nil {
		return c.fail(codeFor(err), "bmdctl engine status: %v", err)
	}
	if !resp.OK {
		return c.fail(exitCode(resp.Code), "bmdctl engine status: %s", resp.Error)
	}
	if g.json || g.quiet {
		return exitOK
	}
	var st engine.Status
	json.Unmarshal(resp.Result.(json.RawMessage), &st)
	for _, l := range engineStatusLines(st, c.build()) {
		fmt.Fprintln(c.stdout, l)
	}
	return exitOK
}

// engineCall sends an engine request, printing the response as it is
// with --json.
func (c *cli) engineCall(conn io.ReadWriteCloser, path string, g globals) (engine.Response, error) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return engine.Call(ctx, conn, engine.Request{Device: "engine", Path: path}, func(line []byte, _ *engine.FileFrame, _ *engine.Response) error {
		if g.json && !g.quiet {
			fmt.Fprintf(c.stdout, "%s\n", line)
		}
		return nil
	})
}

// engineStatusLines are engine status's lines; mine is this bmdctl's
// build.
func engineStatusLines(st engine.Status, mine string) []string {
	p := &printer{}
	rows := [][]string{
		{"engine", fmt.Sprintf("running, pid %d, up %s", st.PID, time.Since(st.Started).Round(time.Second))},
		{"version", fmt.Sprintf("%s, protocol %d", st.Version, st.Protocol)},
	}
	if st.Build != "" {
		build := st.Build
		switch {
		case mine == "" || st.Build == mine:
		case st.Background:
			build += " (not this bmdctl's: the next command restarts the engine)"
		default:
			build += " (not this bmdctl's; started by hand, it answers every build)"
		}
		rows = append(rows, []string{"build", build})
	}
	rows = append(rows, []string{"socket", st.Socket})
	if st.Log != "" {
		rows = append(rows, []string{"log", st.Log})
	}
	if len(st.Sessions) == 0 {
		rows = append(rows, []string{"sessions", "none"})
	}
	for _, s := range st.Sessions {
		state := "connecting"
		switch {
		case s.Error != "":
			state = "failed: " + s.Error
		case s.Ready:
			state = "ready"
		case s.Connected:
			state = "reconnecting"
		}
		if s.Pinned {
			state += ", pinned"
		} else if s.IdleCloseS > 0 && s.Error == "" {
			state += fmt.Sprintf(", closes in %s if idle", (time.Duration(s.IdleCloseS) * time.Second).String())
		}
		desc := s.Product
		if s.VideoMode != "" {
			desc += " " + s.VideoMode
		}
		rows = append(rows, []string{"session", s.Host, state, desc})
	}
	return p.table(rows)
}
