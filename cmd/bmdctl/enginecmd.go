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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
	"github.com/lucas-romanenko/bmdctl/internal/stillcache"
)

// spawnWait is how long a command waits for an engine it started.
const spawnWait = 5 * time.Second

// PinIdleEnv names the variable that says how long a pinned session may
// go without a command before the engine closes it, read when the engine
// starts.
const PinIdleEnv = "BMDCTL_PIN_IDLE"

// pinIdle reads $BMDCTL_PIN_IDLE: a duration (90m, 2h) or seconds; 0,
// off or never for pins that never close (engine.Config.PinIdle < 0);
// unset, the engine's hour.
func pinIdle(v string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return 0, nil
	case "off", "never":
		return -1, nil
	}
	if d, err := time.ParseDuration(v); err == nil && d == 0 {
		return -1, nil
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f == 0 {
		return -1, nil
	}
	d, err := parseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("$%s %q: %v, or 0 for never", PinIdleEnv, v, err)
	}
	return d, nil
}

// StillCacheEnv names the variable that sizes the engine's still cache, in
// megabytes, read when the engine starts.
const StillCacheEnv = "BMDCTL_STILL_CACHE_MB"

// Still cache defaults: room for about 300 1080p stills, and a still no
// save has used for 30 days goes. Only a save uses the cache (nothing
// watches the switcher), so its age limit is longer than WebATEM's week:
// a monthly or weekly save still finds last time's stills.
const (
	stillCacheMB  = 1024
	stillCacheAge = 30 * 24 * time.Hour
)

// stillCache is the engine's cache of the media pool's stills, in the
// cache directory's stills folder: $BMDCTL_STILL_CACHE_MB megabytes
// (1024 unless set), or none at 0.
func (c *cli) stillCache() (*stillcache.Cache, error) {
	mb := int64(stillCacheMB)
	if v := strings.TrimSpace(c.env(StillCacheEnv)); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("$%s %q: megabytes, or 0 for none", StillCacheEnv, v)
		}
		mb = n
	}
	if mb == 0 {
		return nil, nil
	}
	dir, err := paths.Cache(c.env)
	if err != nil {
		return nil, err
	}
	return stillcache.New(filepath.Join(dir, "stills"), mb<<20, stillCacheAge), nil
}

// connectEngine connects to the engine, starting one in the background
// if none is listening, and then calling spawned (if not nil).
func (c *cli) connectEngine(spawned func()) (io.ReadWriteCloser, error) {
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
	if spawned != nil {
		spawned()
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

const engineUsage = `usage: bmdctl engine [--log-level debug|info|warn|error] [--serve [--listen ADDRESS]]
       bmdctl engine stop
       bmdctl engine status [--json]

The engine keeps one session open to each switcher and runs every command
through it. Any command starts one in the background when none is
running, and restarts one it started from another build of bmdctl;
'bmdctl engine' runs one in the foreground instead, for systemd or
Docker, logging to stderr as well as its log file, which answers every
build. A session closes after 10 minutes without a command, or one
'connect' pinned after an hour ($BMDCTL_PIN_IDLE, read when the engine
starts: 90m, 2h, or 0 for never); a watch or an MCP subscription is a
command for as long as it runs.

--serve also serves every command over HTTP/JSON from the engine's own
process (bmdctl serve --help says what), on --listen, 127.0.0.1:8880
unless given: the container's command. It has no authentication.

The socket is $BMDCTL_SOCKET, else $XDG_RUNTIME_DIR/bmdctl.sock (a named
pipe on Windows); the log is $XDG_STATE_HOME/bmdctl/engine.log, rotated.
`

// engineFlags are "bmdctl engine"'s own flags.
type engineFlags struct {
	logLevel string
	bg       bool   // started by a command: log to the file only
	serve    string // --serve: the HTTP API's address, "" for none
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
	listen, listenGiven, tokens, err := listenFlag("engine", tokens)
	if err != nil {
		return c.fail(exitUsage, "%v", err)
	}
	if listenGiven && (sub != "" || !slices.Contains(tokens, "--serve")) {
		return c.fail(exitUsage, "bmdctl engine: --listen goes with --serve")
	}
	for i := 0; i < len(tokens); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		switch {
		case sub == "" && name == "background" && !hasValue:
			f.bg = true
		case sub == "" && name == "serve" && !hasValue:
			f.serve = listen
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
	// A bad BMDCTL_PIN_IDLE stops an engine run by hand; one a command
	// starts logs it and keeps the hour, or no command could run.
	pin, pinErr := pinIdle(c.env(PinIdleEnv))
	if pinErr != nil && !g.bg {
		return c.fail(exitUsage, "bmdctl engine: %v", pinErr)
	}
	// So does a bad BMDCTL_STILL_CACHE_MB; one a command starts keeps
	// the default cache.
	stills, stillsErr := c.stillCache()
	if stillsErr != nil && !g.bg {
		return c.fail(exitUsage, "bmdctl engine: %v", stillsErr)
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
	if pinErr != nil {
		logger.Warn("ignoring "+PinIdleEnv, "err", pinErr, "pin_idle", engine.PinIdleTimeout)
	}
	if stillsErr != nil {
		logger.Warn("ignoring "+StillCacheEnv, "err", stillsErr, "megabytes", stillCacheMB)
		if dir, err := paths.Cache(c.env); err == nil {
			stills = stillcache.New(filepath.Join(dir, "stills"), stillCacheMB<<20, stillCacheAge)
		}
	}
	e := engine.New(c.engineConfig(engine.Config{Registry: c.reg, Logger: logger, Version: versionString(),
		Commit: commitString(), BuildID: c.build(), Background: g.bg, Socket: sock, LogPath: logPath, PinIdle: pin,
		Stills: stills}))
	stop := context.AfterFunc(c.ctx, e.Stop)
	defer stop()
	if g.serve != "" {
		// The HTTP API, in this process: its requests come to this
		// engine over its own socket.
		stopAPI, err := c.startAPI(globals{quiet: g.bg}, g.serve)
		if err != nil {
			e.Stop()
			return c.fail(exitUsage, "bmdctl engine --serve: %v", err)
		}
		defer stopAPI()
		logger.Info("serving the HTTP API", "listen", g.serve)
	}
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

// runningEngine is a running engine's status, within a second, or nil;
// it never starts one.
func (c *cli) runningEngine() *engine.Status {
	conn, err := c.dialRunning()
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, time.Second)
	defer cancel()
	resp, err := engine.Call(ctx, conn, engine.Request{Device: "engine", Path: "status"}, nil)
	if err != nil || !resp.OK {
		return nil
	}
	var st engine.Status
	if json.Unmarshal(resp.Result.(json.RawMessage), &st) != nil {
		return nil
	}
	return &st
}

// engineCall sends an engine request, printing the response as it is
// with --json.
func (c *cli) engineCall(conn io.ReadWriteCloser, path string, g globals) (engine.Response, error) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return engine.Call(ctx, conn, engine.Request{Device: "engine", Path: path}, func(f engine.Frame) error {
		if g.json && !g.quiet && f.Response != nil {
			fmt.Fprintf(c.stdout, "%s\n", f.Line)
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
	// An engine from before pins closed reports none: it never closes
	// them.
	pins := "never close (" + PinIdleEnv + "=0)"
	if st.PinIdleS > 0 {
		pins = "close after " + span(time.Duration(st.PinIdleS)*time.Second) + " idle"
	}
	rows = append(rows, []string{"pins", pins})
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
		if s.Error == "" && (s.Ready || s.Connected) {
			state += ", " + strings.Join(sessionState(&s, nil), ", ")
		} else if s.Pinned {
			state += ", pinned"
		}
		if s.IdleCloseS > 0 && s.Error == "" && !s.Stale {
			state += fmt.Sprintf(", closes in %s if idle", span(time.Duration(s.IdleCloseS)*time.Second))
		}
		desc := s.Product
		if s.VideoMode != "" {
			desc += " " + s.VideoMode
		}
		rows = append(rows, []string{"session", s.Host, state, desc})
	}
	return p.table(rows)
}
