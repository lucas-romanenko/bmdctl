// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/mdns"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Exit codes.
const (
	exitOK          = 0
	exitUsage       = 1 // a usage error, or anything else that failed
	exitUnreachable = 2 // no switcher, or no engine
	exitNoEcho      = 3 // sent, but not confirmed in time
	exitRejected    = 4 // the switcher refused it
	exitDestructive = 5 // destructive, and --yes wasn't given
)

// exitCode maps an engine error code to the process's exit status.
func exitCode(code string) int {
	switch code {
	case engine.CodeUnreachable, engine.CodeEngine, engine.CodeStale:
		return exitUnreachable
	case engine.CodeNoEcho:
		return exitNoEcho
	case engine.CodeRejected:
		return exitRejected
	case engine.CodeDestructive:
		return exitDestructive
	}
	return exitUsage
}

// cli is the program with its surroundings, so tests can replace them.
type cli struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	env            paths.Env
	environ        func() []string  // the environment a spawned engine gets
	tty, ttyErr    bool             // stdout, stderr is a terminal: colour allowed
	ttyIn          bool             // stdin is a terminal: a person can confirm a destructive command
	noColor        bool             // --no-color
	stamp          bool             // -t
	lineNo         string           // exec's line number, as a result starts with it at a terminal
	now            func() time.Time // the clock -t reads; tests stop it
	reg            *table.Registry
	// spawn starts a background engine (engine.Spawn); tests replace it.
	spawn func(exe string, args, env []string) error
	// build is this binary's build (buildID), sent with every request
	// and run by the engine it starts; tests replace it.
	build func() string
	// serve runs the engine in this process, for "bmdctl engine";
	// tests replace its dialler.
	engineConfig func(engine.Config) engine.Config
	// ctx is cancelled by an interrupt.
	ctx context.Context
	// json: the line asked for --json, so a failure the CLI finds itself
	// is printed as the engine would print it; quiet keeps it off stdout.
	json, quiet bool
	// noRemember: --host doesn't become the last host used (exec's
	// lines).
	noRemember bool
	// noticed: the old --name form's notice has been printed; it is
	// printed once per invocation, however many lines use the form.
	noticed bool
	// said: "connecting to ..." has been printed for these switchers;
	// connecting: the last one awaits its model and video mode line.
	said       map[string]bool
	connecting bool
	// bannered: the banner has been printed; shared with exec's lines.
	bannered *bool
	// scr is what has been laid out at a terminal (screen.go); shared
	// with exec's lines, so they share one header.
	scr *screenState
	// connectAt is when "connecting to ..." was said.
	connectAt time.Time
	// spinner is the one a request shows while it waits (spin), if any;
	// spinAfter is how long a wait goes before it shows (tests lengthen
	// it, so a slow machine can't draw one).
	spinner   *spinner
	spinAfter time.Duration
	// discover browses for switchers (mdns); tests replace it.
	discover func(ctx context.Context, wait time.Duration) ([]mdns.Instance, error)
	// probed is what answered at connect ADDRESS's address, when the
	// engine was asked (connect.go).
	probed *engine.ProbeResult
	// dialATEM and dialTCP are doctor's reachability checks; tests
	// replace them.
	dialATEM func(ctx context.Context, addr string) (*atem.Session, error)
	dialTCP  func(ctx context.Context, addr string) (net.Conn, error)
}

func newCLI(stdout, stderr io.Writer) *cli {
	reg, err := engine.NewRegistry()
	if err != nil {
		panic(err) // a malformed table is a programming error
	}
	var d net.Dialer
	return &cli{stdin: os.Stdin, stdout: stdout, stderr: stderr, env: os.Getenv, environ: os.Environ,
		reg: reg, spawn: engine.Spawn, build: sync.OnceValue(buildID), engineConfig: func(c engine.Config) engine.Config { return c },
		ctx: context.Background(), dialATEM: atem.Dial, said: map[string]bool{}, discover: browse,
		bannered: new(bool), scr: &screenState{}, spinAfter: spinAfter, now: time.Now,
		dialTCP: func(ctx context.Context, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }}
}

// builtins are the commands that aren't a device's.
var builtins = []struct{ name, summary string }{
	{"help", "this help; 'bmdctl' alone shows the engine and the switchers"},
	{"version", "print bmdctl's version"},
	{"engine", "run the background engine in the foreground; engine stop|status"},
	{"hosts", "name switchers by their own names: hosts list|add|rm|default|discover"},
	{"exec", "run the commands in a file, one per line: exec FILE|-"},
	{"completion", "print a shell completion script: completion bash|zsh|fish"},
	{"man", "print the manual page, or write it with --dir"},
	{"doctor", "check the engine, its socket, the hosts and that each answers"},
}

// deviceGroups say what each device's commands are, for help.
var deviceGroups = map[string]string{
	"atem":      "an ATEM switcher's commands: program, cut, auto, usk, media, macro, ...",
	"hyperdeck": "a HyperDeck's commands: play, stop, record, goto, clips, slot, ...",
	"videohub":  "a Videohub router (no commands yet)",
	"ultimatte": "an Ultimatte keyer (no commands yet)",
}

// presentDevices are the device types hosts.yml has a host of, in
// hosts.Devices' order.
func (c *cli) presentDevices() []string {
	f, _, err := c.loadHosts()
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range hosts.Devices {
		if slices.ContainsFunc(f.Hosts, func(h hosts.Host) bool { return h.Device == d }) {
			out = append(out, d)
		}
	}
	return out
}

func (c *cli) usage(w io.Writer) {
	fmt.Fprint(w, `usage: bmdctl [--host H] [--json|--quiet] [--yes] [device] <command> [arguments] [--flag value ...]

Commands run through a background engine that keeps a session open to
each device; the first command starts it. A command without a device
word goes to the device --host names, as its type in hosts.yml says (an
ATEM for an address no host has). 'bmdctl' alone shows the engine and
each device.

Commands:
`)
	present := c.presentDevices()
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, b := range builtins {
		fmt.Fprintf(tw, "  %s\t%s\n", b.name, b.summary)
	}
	for _, d := range present {
		fmt.Fprintf(tw, "  %s\t%s\n", d, deviceGroups[d])
	}
	tw.Flush()
	var others []string
	for _, d := range hosts.Devices {
		if !slices.Contains(present, d) {
			others = append(others, d)
		}
	}
	if len(others) > 0 {
		what := "none in hosts.yml"
		if len(present) == 0 {
			what = "no host in hosts.yml yet"
		}
		fmt.Fprintf(w, "  %s\n", c.colour(dim, fmt.Sprintf("also %s (%s)", strings.Join(others, ", "), what)))
	}
	fmt.Fprintf(w, "\nRun 'bmdctl <device> --help' for a device's commands.\n\n")
	globalHelp(w)
}

func globalHelp(w io.Writer) {
	fmt.Fprint(w, `Flags, accepted anywhere on the line:
  --host H       the switcher: a name from 'bmdctl hosts' in any case, or
                 any unambiguous start of one, or host[:port] (UDP 9910 by
                 default); else $BMDCTL_HOST, else the last --host, else
                 the default host
  --json         print the engine's JSON response as it is
  --quiet, -q    print nothing on stdout; the exit status tells
  --yes, -y      allow a destructive command
  --dry-run      print a command's wire bytes and send nothing
  --fps N        frame rate for rates given as seconds:frames
                 (default: the switcher's; 25 with --dry-run)
  --timeout D    how long to wait for the switcher's echo (default 5s)
  --no-color     no colour, even on a terminal (so does $NO_COLOR)
  --timestamps, -t
                 at a terminal, start each result with the time of day
  --help, -h     help for the command line so far

Exit status: 0 done, 1 usage, 2 switcher or engine unreachable, 3 sent but
not confirmed in time, 4 refused by the switcher, 5 destructive without --yes.
`)
}

// run is main without the process: it takes the arguments after the
// program name and returns the exit code.
func (c *cli) run(args []string) int {
	g, words, tokens, err := splitLine(args)
	c.json, c.quiet = g.json, g.quiet
	c.noColor = c.noColor || g.noColor
	c.stamp = c.stamp || g.stamp
	if err != nil {
		return c.fail(exitUsage, "bmdctl: %v", err)
	}
	if len(words) == 0 {
		if g.help {
			c.usage(c.stdout)
			return exitOK
		}
		if len(tokens) == 0 {
			return c.overview(g)
		}
	}
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	switch first {
	case "help":
		c.usage(c.stdout)
		return exitOK
	case "version":
		if c.terminal() {
			c.versionScreen()
			return exitOK
		}
		fmt.Fprintln(c.stdout, versionString())
		return exitOK
	case "engine":
		return c.engineCommand(g, words[1:], tokens)
	case "hosts":
		return c.hostsCommand(g, words[1:], tokens)
	case "exec":
		return c.execCommand(g, words[1:], tokens)
	case "completion":
		return c.completionCommand(g, words[1:])
	case "__complete":
		return c.completeCommand(args[1:])
	case "man":
		return c.manCommand(g, tokens)
	case "doctor":
		return c.doctorCommand(g, words[1:], tokens)
	}
	// The device: named on the line, else the target host's type, else
	// an ATEM. connect ADDRESS asks the address (connect.go).
	device, named := "", false
	if slices.Contains(hosts.Devices, first) {
		device, words, named = first, words[1:], true
	}
	if len(words) > 0 && words[0] == "connect" {
		return c.connectCommand(g, device, words, tokens)
	}
	t, terr := c.resolveTarget(g)
	byHost := !named && terr == nil && t.Device != ""
	if !named {
		device = "atem"
		if byHost {
			device = t.Device
		}
	}
	if !slices.Contains(c.reg.Devices(), device) {
		if byHost {
			return c.fail(exitUsage, "bmdctl: %s is %s %s; bmdctl has no %s commands yet",
				t.label(), engine.DeviceArticle(device), engine.DeviceNames[device], engine.DeviceNames[device])
		}
		return c.fail(exitUsage, "bmdctl: no %s commands yet", device)
	}
	if !named && len(words) > 0 && !c.isCommandWord(device, words[0]) {
		if c.deviceWith(words[0]) != "" && byHost {
			// Another device's command: say what this host is.
			return c.fail(exitUsage, "bmdctl: %s is %s %s; bmdctl %s --help",
				t.label(), engine.DeviceArticle(device), engine.DeviceNames[device], device)
		}
		return c.fail(exitUsage, "bmdctl: unknown command %q (bmdctl --help lists them)", first)
	}
	return c.runDevice(g, device, words, tokens)
}

// deviceWith returns the first device one of whose commands starts with
// word, or "".
func (c *cli) deviceWith(word string) string {
	for _, d := range c.reg.Devices() {
		if c.isCommandWord(d, word) {
			return d
		}
	}
	return ""
}

// isCommandWord reports whether word starts one of device's commands.
func (c *cli) isCommandWord(device, word string) bool {
	alt, _ := c.reg.Unalias(device, []string{word})
	return slices.ContainsFunc(c.reg.Entries(device), func(e table.Entry) bool { return e.Words()[0] == word || e.Words()[0] == alt[0] })
}

// fail reports a failure and returns code: on stderr, or with --json
// as the engine's failure response on stdout.
func (c *cli) fail(code int, format string, args ...any) int {
	msg := fmt.Sprintf(format, args...)
	if c.json && !c.quiet {
		b, _ := json.Marshal(engine.Response{Code: codeName(code), Error: msg})
		fmt.Fprintf(c.stdout, "%s\n", b)
		return code
	}
	c.errorf(code, "%s", msg)
	return code
}

// errorf prints an error on stderr, ending with the next step for its
// exit status (nextStep); at a terminal, the next step under it
// (termStep), unless the message names its own.
func (c *cli) errorf(code int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if c.terminal() {
		msg, hint := splitHint(stripLabel(msg))
		if hint == "" {
			hint = termStep(code)
		}
		c.screenError(msg, hint)
		return
	}
	c.printError(c.stderr, nextStep(code, msg))
}

// nextStep ends an error with what to do next, by its exit status:
// doctor for a switcher or engine that can't be reached, status for a
// command the switcher didn't confirm, --yes for a destructive one. (An
// unknown source already points at inputs, where the engine says so.)
func nextStep(code int, msg string) string {
	step := ""
	switch code {
	case exitUnreachable:
		step = "bmdctl doctor says why"
	case exitNoEcho:
		step = "bmdctl status shows what the switcher holds now"
	case exitDestructive:
		step = "add --yes to run it"
	}
	if step == "" || strings.Contains(msg, step) || code == exitDestructive && strings.Contains(msg, "--yes") {
		return msg
	}
	return msg + " (" + step + ")"
}

// notice prints a line for a person on stderr, prefixed "bmdctl: "; at
// a terminal dim and indented, as a hint. Never with --json or --quiet.
func (c *cli) notice(format string, args ...any) {
	if c.json || c.quiet {
		return
	}
	if c.terminal() {
		fmt.Fprintln(c.stderr, fit(indent+c.colourErr(dim, fmt.Sprintf(format, args...)), lineWidth))
		return
	}
	fmt.Fprintf(c.stderr, "bmdctl: "+format+"\n", args...)
}

// unverified says on stderr that a command is unverified on hardware:
// at a terminal in a few words, dim.
func (c *cli) unverified(label string) {
	if c.terminal() {
		c.notice("%s is unverified on hardware", label)
		return
	}
	why := "atemwire restored it from pyatem without checking it on a switcher"
	if strings.HasPrefix(label, "hyperdeck ") {
		why = "bmdctl sends it as BMD's protocol document describes; hyperdeckwire never sent it to a deck"
	}
	fmt.Fprintf(c.stderr, "bmdctl: %s is unverified on hardware: %s\n", label, why)
}

// sayConnecting prints "connecting to <who> ..." on stderr, once per
// switcher in an invocation: when a command starts the engine, opens a
// session, or waits for one to come back. The model and video mode
// follow when the session is ready (printer.note). While the spinner
// shows, it says so instead, and the line that follows replaces it.
func (c *cli) sayConnecting(g globals, who string) {
	if g.json || g.quiet || c.said[who] {
		return
	}
	c.said[who], c.connecting, c.connectAt = true, true, time.Now()
	if c.spinner != nil {
		c.spinner.set("connecting to " + who)
		return
	}
	fmt.Fprintf(c.stderr, "connecting to %s …\n", who)
}

// sayConnected follows sayConnecting once the session is ready, and
// returns how long connecting took. Off a terminal it prints the model
// and video mode on stderr; at one the header says them (screen.go), and
// the spinner goes on, saying next.
func (c *cli) sayConnected(name, addr string, sw engine.Switcher, next string) time.Duration {
	c.connecting = false
	took := max(time.Since(c.connectAt), time.Millisecond)
	if c.terminal() {
		if c.spinner != nil {
			c.spinner.set(next)
		}
		return took
	}
	fmt.Fprintln(c.stderr, strings.Join(nonEmpty(sw.Product, sw.VideoMode), ", "))
	return took
}

// codeName is the engine's error code for an exit status.
func codeName(code int) string {
	switch code {
	case exitUnreachable:
		return engine.CodeUnreachable
	case exitNoEcho:
		return engine.CodeNoEcho
	case exitRejected:
		return engine.CodeRejected
	case exitDestructive:
		return engine.CodeDestructive
	}
	return engine.CodeUsage
}

func versionString() string {
	v, cm, d := version, commit, date
	if v == "" {
		v = "(devel)"
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" {
				v = bi.Main.Version
			}
			for _, s := range bi.Settings {
				switch {
				case s.Key == "vcs.revision" && cm == "":
					cm = s.Value[:min(12, len(s.Value))]
				case s.Key == "vcs.time" && d == "":
					d = s.Value
				}
			}
		}
	}
	var extra []string
	for _, s := range []string{cm, d} {
		if s != "" {
			extra = append(extra, s)
		}
	}
	if len(extra) == 0 {
		return "bmdctl " + v
	}
	return fmt.Sprintf("bmdctl %s (%s)", v, strings.Join(extra, ", "))
}

// versionScreen is bmdctl version at a terminal: the version, then the
// commit it was built from, the hosts file and the engine, which bmdctl
// alone no longer shows.
func (c *cli) versionScreen() {
	fmt.Fprintln(c.stdout, "bmdctl "+shortVersion())
	row := func(label, value string) { fmt.Fprintln(c.stdout, fit(labelled(label, value), lineWidth)) }
	if cm := commitString(); cm != "" {
		row("commit", c.colour(dim, cm))
	}
	if date != "" {
		row("built", date)
	}
	if path, err := c.hostsPath(); err == nil {
		row("hosts", tildePath(path, c.env("HOME")))
	}
	st := c.runningEngine()
	if st == nil {
		row("engine", c.colour(dim, "not running · the next command starts it"))
		return
	}
	parts := []string{"running", fmt.Sprintf("pid %d", st.PID), "up " + ago(time.Since(st.Started)), count(len(st.Sessions), "session")}
	if st.Commit != "" {
		parts = append(parts, "commit "+c.colour(dim, st.Commit))
	}
	if mine := c.build(); st.Build != "" && mine != "" && mine != st.Build {
		parts = append(parts, "another build")
	}
	row("engine", strings.Join(parts, " · "))
}

// commitString is the git commit this binary was built from, short as
// git prints it: GoReleaser's, else the toolchain's build info
// (vcs.revision, "+dirty" when vcs.modified says the tree had changes,
// untracked files included); "" when neither says (go run, go test). It
// is for people; the stale-engine check compares build IDs.
func commitString() string {
	var settings []debug.BuildSetting
	if bi, ok := debug.ReadBuildInfo(); ok {
		settings = bi.Settings
	}
	return shortCommit(commit, settings)
}

func shortCommit(ldflag string, settings []debug.BuildSetting) string {
	if ldflag != "" {
		return ldflag[:min(7, len(ldflag))]
	}
	rev, dirty := "", ""
	for _, s := range settings {
		switch {
		case s.Key == "vcs.revision":
			rev = s.Value[:min(7, len(s.Value))]
		case s.Key == "vcs.modified" && s.Value == "true":
			dirty = "+dirty"
		}
	}
	if rev == "" {
		return ""
	}
	return rev + dirty
}

// errExit carries an exit code through a function that returns an error.
type errExit struct {
	code int
	err  error
}

func (e *errExit) Error() string { return e.err.Error() }

func exitf(code int, format string, args ...any) error {
	return &errExit{code, fmt.Errorf(format, args...)}
}

func codeFor(err error) int {
	var e *errExit
	if errors.As(err, &e) {
		return e.code
	}
	return exitUsage
}
