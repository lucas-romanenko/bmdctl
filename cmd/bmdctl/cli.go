// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
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
	case engine.CodeUnreachable, engine.CodeEngine:
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
	environ        func() []string // the environment a spawned engine gets
	tty            bool            // stdout is a terminal: colour allowed
	reg            *table.Registry
	// spawn starts a background engine (engine.Spawn); tests replace it.
	spawn func(exe string, args, env []string) error
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
}

func newCLI(stdout, stderr io.Writer) *cli {
	reg, err := engine.NewRegistry()
	if err != nil {
		panic(err) // a malformed table is a programming error
	}
	return &cli{stdin: os.Stdin, stdout: stdout, stderr: stderr, env: os.Getenv, environ: os.Environ,
		reg: reg, spawn: engine.Spawn, engineConfig: func(c engine.Config) engine.Config { return c },
		ctx: context.Background()}
}

// builtins are the commands that aren't a device's.
var builtins = []struct{ name, summary string }{
	{"version", "print bmdctl's version"},
	{"engine", "run the background engine in the foreground; engine stop|status"},
	{"hosts", "name devices: hosts list|add|rm|default"},
	{"exec", "run the commands in a file, one per line: exec FILE|-"},
	{"atem", "an ATEM switcher's commands (the default device)"},
}

func (c *cli) usage(w io.Writer) {
	fmt.Fprint(w, `usage: bmdctl [--host H] [--json|--quiet] [--yes] <command> [--parameter value ...]

Commands run through a background engine that keeps a session open to
each switcher; the first command starts it. A command without a device
word goes to the ATEM named by --host.

Commands:
`)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, b := range builtins {
		fmt.Fprintf(tw, "  %s\t%s\n", b.name, b.summary)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nRun 'bmdctl atem --help' for the switcher's commands.\n\n")
	globalHelp(w)
}

func globalHelp(w io.Writer) {
	fmt.Fprint(w, `Flags, accepted anywhere on the line:
  --host H       the switcher: a name from 'bmdctl hosts', or host[:port]
                 (UDP 9910 by default); else $BMDCTL_HOST, else the last
                 --host, else the default host
  --json         print the engine's JSON response as it is
  --quiet, -q    print nothing on stdout; the exit status tells
  --yes, -y      allow a destructive command
  --dry-run      print a command's wire bytes and send nothing
  --fps N        frame rate for rates given as seconds:frames
                 (default: the switcher's; 25 with --dry-run)
  --timeout D    how long to wait for the switcher's echo (default 5s)
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
	if err != nil {
		return c.fail(exitUsage, "bmdctl: %v", err)
	}
	if len(words) == 0 {
		if g.help {
			c.usage(c.stdout)
			return exitOK
		}
		if len(tokens) == 0 {
			c.usage(c.stderr)
			return exitUsage
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
		fmt.Fprintln(c.stdout, versionString())
		return exitOK
	case "engine":
		return c.engineCommand(g, words[1:], tokens)
	case "hosts":
		return c.hostsCommand(g, words[1:], tokens)
	case "exec":
		return c.execCommand(g, words[1:], tokens)
	}
	// The device: named on the line, else the target host's type, else
	// an ATEM.
	device, named := "", false
	if slices.Contains(hosts.Devices, first) {
		device, words, named = first, words[1:], true
	} else {
		device = "atem"
		if t, err := c.resolveTarget(g); err == nil && t.Device != "" {
			device = t.Device
		}
	}
	if !slices.Contains(c.reg.Devices(), device) {
		return c.fail(exitUsage, "bmdctl: no %s commands yet; this version controls ATEM switchers", device)
	}
	if !named && len(words) > 0 && c.reg.Find(device, words[0]) == nil && !c.isGroup(device, words[0]) {
		return c.fail(exitUsage, "bmdctl: unknown command %q (bmdctl --help lists them)", first)
	}
	return c.runDevice(g, device, words, tokens)
}

func (c *cli) isGroup(device, word string) bool {
	_, prefix, _ := c.reg.Resolve(device, []string{word})
	return len(prefix) > 0
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
	fmt.Fprintln(c.stderr, msg)
	return code
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
