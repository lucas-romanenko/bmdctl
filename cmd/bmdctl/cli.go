// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
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
  --host H       the switcher: host or host:port (UDP 9910 by default)
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
	}
	device := "atem"
	if slices.Contains(c.reg.Devices(), first) {
		words = words[1:]
	} else if len(words) > 0 && c.reg.Find(device, words[0]) == nil && !c.isGroup(device, words[0]) {
		return c.fail(exitUsage, "bmdctl: unknown command %q (bmdctl --help lists them)", first)
	}
	return c.runDevice(g, device, words, tokens)
}

func (c *cli) isGroup(device, word string) bool {
	_, prefix, _ := c.reg.Resolve(device, []string{word})
	return len(prefix) > 0
}

// fail prints a message on stderr and returns code.
func (c *cli) fail(code int, format string, args ...any) int {
	fmt.Fprintf(c.stderr, format+"\n", args...)
	return code
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
