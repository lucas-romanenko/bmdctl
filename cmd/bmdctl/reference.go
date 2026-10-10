// SPDX-License-Identifier: MIT

package main

// The CLI's reference beyond the command table: global flags, built-in
// commands, exit status, environment and files. docs/COMMANDS.md and the
// man page are both rendered from it and from the table.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

type refItem struct{ term, text string }

var refGlobalFlags = []refItem{
	{"--host H", "The device: a name from `bmdctl hosts`, or host[:port] (UDP 9910 by default). Without it: $BMDCTL_HOST, else the host the last --host named, else the default host."},
	{"--json", "Print the engine's JSON response as it is: {ok, confirmed, result, took_ms} or {ok:false, code, error}, one per line."},
	{"--quiet, -q", "Print nothing on stdout; the exit status tells. Errors still go to stderr."},
	{"--yes, -y", "Allow a destructive command."},
	{"--dry-run", "Print a wire command's bytes, header included, and send nothing."},
	{"--fps N", "The frame rate for rates given as seconds:frames (default: the switcher's display rate; 25 with --dry-run)."},
	{"--timeout D", "How long to wait for the switcher's echo (default 5s), or for a transfer to finish (default: the library's)."},
	{"--help, -h", "Help for the command line so far."},
}

var refBuiltins = []refItem{
	{"bmdctl version", "Print bmdctl's version."},
	{"bmdctl engine [--log-level L]", "Run the background engine in the foreground, for systemd or Docker, logging to stderr and its log file. Any command starts one in the background when none is running."},
	{"bmdctl engine stop", "Stop the running engine; its sessions say goodbye to their switchers."},
	{"bmdctl engine status", "Show the engine's version, pid, socket, log and sessions."},
	{"bmdctl hosts [list]", "List the named hosts."},
	{"bmdctl hosts add NAME ADDRESS [--device D]", "Name a device: atem (the default), hyperdeck, videohub or ultimatte."},
	{"bmdctl hosts rm NAME", "Forget a host."},
	{"bmdctl hosts default [NAME]", "Show the default host, or make NAME the default and the host commands go to now."},
	{"bmdctl exec FILE|- [--continue]", "Run one command per line, in order, to one host. Every line is checked before the first is sent; it stops at the first failure unless --continue."},
	{"bmdctl completion bash|zsh|fish", "Print a shell completion script: commands, flags, host names and, while the engine runs, the switcher's input names and the indexes it has, counted from 1."},
	{"bmdctl man [--dir DIR]", "Print the bmdctl(1) manual page, or write it to DIR/bmdctl.1."},
	{"bmdctl doctor", "Check the engine, its socket and log, the hosts file and that each host answers, and say what is misconfigured. Exit 1 for a misconfiguration, 2 when a host only didn't answer."},
}

var refNotes = []refItem{
	{"Devices", "A command without a device word goes to the device type of its host, an ATEM for a bare address. `bmdctl atem ...` names the device."},
	{"Indexes", "M/Es, upstream and downstream keyers, aux outputs, media and macro slots and media players count from 1, as ATEM Software Control and the panels count them: on the command line, in --help and in results, --json included. The atem library counts them from 0, and so do the switcher state `state` and `watch` print and the index attributes of ATEM Software Control's XML files. Colour generators, multiviewers and their windows, SuperSources and their boxes, HyperDeck slots and EQ bands keep their fields' numbering."},
	{"Sources", "A source is an id or one of the switcher's input names, long or short, in any case."},
	{"Rates", "A rate is a frame count or seconds:frames at the switcher's display rate (half the field rate from 48 fps up)."},
}

var refExit = []refItem{
	{"0", "Done."},
	{"1", "A usage error, or any other failure."},
	{"2", "The switcher or the engine is unreachable."},
	{"3", "Sent, but the switcher did not confirm it in time."},
	{"4", "The switcher refused it (a transfer, a lock), or a profile restore recorded errors."},
	{"5", "Destructive, and --yes was not given."},
}

var refEnv = []refItem{
	{"BMDCTL_HOST", "The host a command goes to when --host doesn't name one."},
	{"BMDCTL_SOCKET", "The engine's socket (a pipe name on Windows), instead of $XDG_RUNTIME_DIR/bmdctl.sock."},
	{"XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_RUNTIME_DIR", "Where hosts.yml, the engine's log and last host, and the socket live."},
	{"NO_COLOR", "Set to anything: no colour, even on a terminal."},
}

var refFiles = []refItem{
	{"~/.config/bmdctl/hosts.yml", "The named hosts and the default."},
	{"~/.local/state/bmdctl/engine.log", "The engine's log, rotated at 5 MiB, three old files kept."},
	{"~/.local/state/bmdctl/last-host", "The host the last --host named."},
	{"$XDG_RUNTIME_DIR/bmdctl.sock", "The engine's socket; a lock file beside it."},
}

// commandsDoc is docs/COMMANDS.md: the CLI's reference, then the table.
func commandsDoc(r *table.Registry) string {
	var b strings.Builder
	md := r.Markdown()
	head, body, _ := strings.Cut(md, "\n## ")
	b.WriteString(head)
	b.WriteString("\n## The command line\n\n```\nbmdctl [--host H] [--json|--quiet] [--yes] [atem] <command> [--parameter value ...]\n```\n")
	section := func(title string, items []refItem, code bool) {
		fmt.Fprintf(&b, "\n### %s\n\n", title)
		for _, it := range items {
			term := it.term
			if code {
				term = "`" + term + "`"
			}
			fmt.Fprintf(&b, "- %s: %s\n", term, it.text)
		}
	}
	section("Flags, accepted anywhere on the line", refGlobalFlags, true)
	section("Built-in commands", refBuiltins, true)
	section("Conventions", refNotes, false)
	section("Exit status", refExit, false)
	section("Environment", refEnv, true)
	section("Files", refFiles, true)
	b.WriteString("\n## " + body)
	return b.String()
}

func (c *cli) manCommand(g globals, tokens []string) int {
	if g.help {
		fmt.Fprintln(c.stdout, "usage: bmdctl man [--dir DIR]\n\nPrints the bmdctl(1) manual page, or writes it to DIR/bmdctl.1.")
		return exitOK
	}
	dir := ""
	for i := 0; i < len(tokens); i++ {
		name, value, has := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		if name != "dir" {
			return c.fail(exitUsage, "bmdctl man: unexpected %q", tokens[i])
		}
		if !has {
			if i+1 == len(tokens) {
				return c.fail(exitUsage, "bmdctl man: --dir needs a value")
			}
			i++
			value = tokens[i]
		}
		dir = value
	}
	page := manPage(c.reg)
	if dir == "" {
		fmt.Fprint(c.stdout, page)
		return exitOK
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.fail(exitUsage, "bmdctl man: %v", err)
	}
	path := filepath.Join(dir, "bmdctl.1")
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		return c.fail(exitUsage, "bmdctl man: %v", err)
	}
	if !g.quiet {
		fmt.Fprintln(c.stdout, "wrote "+path)
	}
	return exitOK
}

var codeSpan = regexp.MustCompile("`([^`]*)`")

// roff escapes text for a man page: backslashes, a leading dot or quote,
// hyphens as minus signs (so flags copy and paste), and `code` in bold.
func roff(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	s = strings.ReplaceAll(s, "-", `\-`)
	s = codeSpan.ReplaceAllString(s, `\fB$1\fR`)
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	// Non-ASCII as groff's Unicode escapes, which mandoc reads too.
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, `\[u%04X]`, r)
		}
	}
	return b.String()
}

// manPage is bmdctl(1).
func manPage(r *table.Registry) string {
	var b strings.Builder
	v := strings.TrimPrefix(versionString(), "bmdctl ")
	fmt.Fprintf(&b, ".TH BMDCTL 1 \"\" \"bmdctl %s\" \"User Commands\"\n", roff(v))
	b.WriteString(".SH NAME\nbmdctl \\- control Blackmagic Design ATEM switchers\n")
	b.WriteString(".SH SYNOPSIS\n.B bmdctl\n[\\fB\\-\\-host\\fR \\fIH\\fR] [\\fB\\-\\-json\\fR|\\fB\\-\\-quiet\\fR] [\\fB\\-\\-yes\\fR] [\\fBatem\\fR] \\fIcommand\\fR [\\fB\\-\\-parameter\\fR \\fIvalue\\fR ...]\n")
	b.WriteString(".SH DESCRIPTION\nbmdctl sends commands to ATEM switchers over their UDP protocol and reads back their state. " +
		"Commands run through a background engine that keeps a session open to each switcher; the first command starts it. " +
		"A command that the switcher reports back is confirmed; one without such an echo is sent and left.\n")
	items := func(title string, list []refItem, bold bool) {
		fmt.Fprintf(&b, ".SH %s\n", title)
		for _, it := range list {
			term := roff(it.term)
			if bold {
				term = `\fB` + term + `\fR`
			}
			fmt.Fprintf(&b, ".TP\n%s\n%s\n", term, roff(it.text))
		}
	}
	items("OPTIONS", refGlobalFlags, true)
	items("BUILT-IN COMMANDS", refBuiltins, true)
	items("CONVENTIONS", refNotes, false)
	b.WriteString(".SH ATEM COMMANDS\n")
	for _, e := range r.Entries("atem") {
		fmt.Fprintf(&b, ".SS \"%s\"\n%s\n", roff(e.Path), roff(e.Summary))
		var notes []string
		if e.Destructive {
			notes = append(notes, "Destructive: needs --yes.")
		}
		if e.Unverified {
			notes = append(notes, "Unverified on hardware.")
		}
		if e.Local {
			notes = append(notes, "Runs in the CLI; needs no switcher.")
		}
		if e.AtLeastOne {
			notes = append(notes, "Give at least one optional parameter.")
		}
		if e.Echo != "" {
			notes = append(notes, "Echo: "+e.Echo+".")
		}
		if len(notes) > 0 {
			fmt.Fprintf(&b, ".PP\n%s\n", roff(strings.Join(notes, " ")))
		}
		for _, p := range e.Params {
			need := ""
			switch {
			case p.Required:
				need = " (required)"
			case p.Default != "":
				need = " (default " + p.Default + ")"
			}
			fmt.Fprintf(&b, ".TP\n\\fB%s\\fR \\fI%s\\fR%s\n%s\n", roff("--"+p.Name), roff(paramValue(p)), roff(need), roff(p.Help))
			if len(p.Names) > 0 {
				var ns []string
				for _, n := range p.SortedNames() {
					ns = append(ns, fmt.Sprintf("%s=%d", n, p.Names[n]))
				}
				fmt.Fprintf(&b, "Values: %s.\n", roff(strings.Join(ns, ", ")))
			}
		}
	}
	items("EXIT STATUS", refExit, true)
	items("ENVIRONMENT", refEnv, true)
	items("FILES", refFiles, true)
	b.WriteString(".SH SEE ALSO\nThe docs directory of the bmdctl source: COMMANDS.md, PORTING.md, SURFACE\\-atem.md.\n")
	return b.String()
}
