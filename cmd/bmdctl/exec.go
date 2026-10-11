// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

const execUsage = `usage: bmdctl exec FILE|- [--continue] [--host H] [--json|--quiet] [--yes] [--timeout D]

Runs one bmdctl command per line, in order, through the engine: the
commands a command line takes, without "bmdctl" (a leading one is
allowed). Quote arguments with spaces as a shell would; # starts a
comment. Every line is checked before the first is sent, so a typo can't
leave a cue half run. The host is resolved once, from --host or as any
command resolves it, and every line goes there unless it gives its own
--host. The flags given to exec apply to every line.

It stops at the first command that fails, with that command's exit
status, unless --continue, which runs every line and exits with the
first failure's status. --json prints one line per command.
`

// line is one command of an exec file.
type line struct {
	n    int
	args []string
}

func (c *cli) execCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, execUsage)
		return exitOK
	}
	cont := false
	for _, t := range tokens {
		if t != "--continue" {
			return c.fail(exitUsage, "bmdctl exec: unexpected %q\n\n%s", t, execUsage)
		}
		cont = true
	}
	if len(words) != 1 {
		return c.fail(exitUsage, "bmdctl exec: takes one FILE, or - for stdin\n\n%s", execUsage)
	}
	var data []byte
	var err error
	if words[0] == "-" {
		data, err = io.ReadAll(c.stdin)
	} else {
		data, err = os.ReadFile(words[0])
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl exec: %v", err)
	}
	lines, err := parseScript(string(data))
	if err != nil {
		return c.fail(exitUsage, "bmdctl exec %s: %v", words[0], err)
	}
	// The flags exec was given go on every line, before the line's own,
	// which win.
	var common []string
	if t, err := c.resolveTarget(g); err == nil {
		common = append(common, "--host", t.Address)
		if t.Host != nil {
			common[1] = t.Host.Label()
		}
	}
	for _, f := range []struct {
		on   bool
		flag string
	}{{g.json, "--json"}, {g.quiet, "--quiet"}, {g.yes, "--yes"}, {g.noColor, "--no-color"}} {
		if f.on {
			common = append(common, f.flag)
		}
	}
	if g.fps > 0 {
		common = append(common, "--fps", fmt.Sprint(g.fps))
	}
	if g.timeout > 0 {
		common = append(common, "--timeout", g.timeout.String())
	}
	var bad []string
	for _, l := range lines {
		if err := c.checkLine(append(slices.Clone(common), l.args...)); err != nil {
			bad = append(bad, fmt.Sprintf("line %d: %v", l.n, err))
		}
	}
	if len(bad) > 0 {
		return c.fail(exitUsage, "bmdctl exec %s: nothing run:\n  %s", words[0], strings.Join(bad, "\n  "))
	}
	first := exitOK
	for _, l := range lines {
		if c.ctx.Err() != nil {
			return c.fail(exitUsage, "bmdctl exec: interrupted before line %d", l.n)
		}
		sub := *c
		sub.noRemember = true
		sub.ttyIn = false // a cue doesn't stop to ask: its lines were checked for --yes
		sub.stderr = &prefixWriter{w: c.stderr, prefix: fmt.Sprintf("line %d: ", l.n)}
		code := sub.run(append(slices.Clone(common), l.args...))
		if code != exitOK && first == exitOK {
			first = code
			if !cont {
				return code
			}
		}
	}
	return first
}

// checkLine runs the checks a command line gets before anything is sent,
// without sending: the command exists, its arguments parse and, for a
// wire command, build. Built-ins exec can't run are refused.
func (c *cli) checkLine(args []string) error {
	g, words, tokens, err := splitLine(args)
	if err != nil {
		return err
	}
	if len(words) == 0 {
		return errors.New("no command")
	}
	switch words[0] {
	case "exec", "engine", "completion", "man", "doctor", "help", "__complete":
		return fmt.Errorf("%s can't run inside exec", words[0])
	case "version", "hosts":
		return nil
	}
	device := "atem"
	if slices.Contains(hosts.Devices, words[0]) {
		device, words = words[0], words[1:]
	}
	if !slices.Contains(c.reg.Devices(), device) {
		return fmt.Errorf("no %s commands yet", device)
	}
	e, raw, err := c.reg.ParseLine(device, words, tokens)
	var nc *table.NoCommand
	if errors.As(err, &nc) {
		return nc
	}
	if g.help {
		return nil
	}
	if err == nil {
		c.noticeOldForm(e, raw, tokens)
		err = precheck(e, raw, g)
	}
	if err != nil {
		return fmt.Errorf("%s: %s", e.Path, strings.TrimPrefix(err.Error(), "usage: "))
	}
	if e.Destructive && !g.yes && !g.dryRun {
		return fmt.Errorf("%s is destructive: add --yes to the line or to exec", e.Path)
	}
	return nil
}

// parseScript splits an exec file into command lines.
func parseScript(s string) ([]line, error) {
	var out []line
	for i, text := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		args, err := splitWords(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		if len(args) > 0 && args[0] == "bmdctl" {
			args = args[1:]
		}
		if len(args) > 0 {
			out = append(out, line{i + 1, args})
		}
	}
	return out, nil
}

// splitWords splits a line as a POSIX shell would split a simple command:
// whitespace between words, '...' literal, "..." with \" \\ \$ \`
// escapes, a backslash escaping the next character elsewhere, and # at
// the start of a word starting a comment. Nothing is expanded.
func splitWords(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		case r == '#' && !inWord:
			return out, nil
		case r == '\\':
			if i+1 == len(rs) {
				return nil, errors.New("a backslash at the end of the line")
			}
			i++
			cur.WriteRune(rs[i])
			inWord = true
		case r == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j == len(rs) {
				return nil, errors.New("an unterminated '")
			}
			cur.WriteString(string(rs[i+1 : j]))
			i, inWord = j, true
		case r == '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' && j+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[j+1]) {
					j++
				}
				cur.WriteRune(rs[j])
			}
			if j == len(rs) {
				return nil, errors.New(`an unterminated "`)
			}
			i, inWord = j, true
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

// prefixWriter prefixes each line written with the exec line it came
// from. A carriage return starts the line again, as the spinner's frames
// do (clearLine), so each frame is prefixed too.
type prefixWriter struct {
	w      io.Writer
	prefix string
	mid    bool // inside a line
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	var out []byte
	for _, c := range b {
		if !p.mid {
			out = append(out, p.prefix...)
			p.mid = true
		}
		out = append(out, c)
		if c == '\n' || c == '\r' {
			p.mid = false
		}
	}
	if _, err := p.w.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}
