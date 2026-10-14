// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Every command line in the documentation site's pages reads as a
// command and passes the checks a line gets before it is sent, as the
// table's examples and the skill's lines do: a page can't show a line
// bmdctl would refuse. Left out: a sample's line whose output is an
// error (it shows the refusal), a line with a shell variable (only the
// shell fills it in), and docs/HARDWARE.md, a log of what ran in the
// syntax of its day that is never rewritten.
func TestDocsCommands(t *testing.T) {
	pages := []string{"../../README.md", "../../CONTRIBUTING.md"}
	for _, glob := range []string{"../../docs/*.md", "../../docs/guide/*.md", "../../docs/commands/*.md"} {
		m, _ := filepath.Glob(glob)
		pages = append(pages, m...)
	}
	c := newCLI(io.Discard, io.Discard)
	checked := 0
	for _, p := range pages {
		switch filepath.Base(p) {
		case "HARDWARE.md", "PORTING.md", "SURFACE-atem.md", "SURFACE-hyperdeck.md":
			continue // not the site's, or a log
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(p, "../../")
		for _, l := range commandLines(string(b)) {
			args, err := splitWords(l.text)
			if err == nil {
				err = checkWords(c, args)
			}
			if err != nil {
				t.Errorf("%s:%d: %q: %v", name, l.n, l.text, err)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Errorf("checked %d lines", checked)
	}
	// What the checks refuse.
	for _, bad := range []string{"bmdctl progam 2", "bmdctl usk 1 fill", "bmdctl hosts ad 192.168.1.20",
		"hdctl play --sped 50", "bmdctl completion tcsh", "bmdctl use", "bmdctl aux 0 pgm"} {
		args, _ := splitWords(bad)
		if checkWords(c, args) == nil {
			t.Errorf("%q passes", bad)
		}
	}
}

// docLine is a command line found in a page, and the line it is on.
type docLine struct {
	n    int
	text string
}

var (
	inlineCommand = regexp.MustCompile("`((?:bmdctl|hdctl) [^`]+)`")
	// Syntax rather than an example: a word in capitals (HOST, ADDRESS),
	// <one>, [optional], one|other, or "...".
	placeholder = regexp.MustCompile(`(^|\s)[A-Z]+(\s|$|\|)|[<\[|]|\.\.\.`)
)

// commandLines are the command lines in a Markdown page: in a terminal
// sample (a code block with a "$ " prompt) the prompted lines, in a
// shell block (sh, bash) each command a line holds (a pipeline's, a
// loop's), and in the text, headings aside, a `bmdctl ...` code span
// that isn't syntax. Any other block is a diagram, data or the table's
// examples, which TestExamples checks.
func commandLines(page string) []docLine {
	var out []docLine
	lines := strings.Split(page, "\n")
	for i := 0; i < len(lines); i++ {
		fence := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(fence, "```") {
			if strings.HasPrefix(fence, "#") {
				continue // a heading: a usage
			}
			for _, m := range inlineCommand.FindAllStringSubmatch(lines[i], -1) {
				if !placeholder.MatchString(m[1]) {
					out = append(out, docLine{i + 1, m[1]})
				}
			}
			continue
		}
		start := i + 1
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
		}
		block := lines[start:i]
		prompt := slices.ContainsFunc(block, func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "$ ") })
		switch strings.TrimPrefix(fence, "```") {
		case "sh", "bash", "shell", "console":
		case "":
			if !prompt {
				continue
			}
		default:
			continue
		}
		for j, l := range block {
			l = strings.TrimSpace(l)
			if prompt {
				if !strings.HasPrefix(l, "$ ") {
					continue
				}
				if j+1 < len(block) && strings.HasPrefix(strings.TrimSpace(block[j+1]), "error") {
					continue // a refusal, shown
				}
				l = l[2:]
			}
			for _, cmd := range shellCommands(l) {
				if !strings.Contains(cmd, "$") {
					out = append(out, docLine{start + j + 1, cmd})
				}
			}
		}
	}
	return out
}

// shellCommands are the bmdctl commands in a shell line: it is cut at
// |, ; and && outside quotes, and each part that runs bmdctl (or hdctl)
// is one, less a do or then before it.
func shellCommands(line string) []string {
	var parts []string
	var cur strings.Builder
	quote := rune(0)
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && (i == 0 || rs[i-1] == ' '):
			i = len(rs)
			continue
		case r == '|' || r == ';' || r == '&' && i+1 < len(rs) && rs[i+1] == '&':
			parts = append(parts, cur.String())
			cur.Reset()
			if r == '&' {
				i++
			}
			continue
		}
		cur.WriteRune(r)
	}
	parts = append(parts, cur.String())
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		for _, kw := range []string{"do ", "then "} {
			p = strings.TrimPrefix(p, kw)
		}
		if p == "bmdctl" || strings.HasPrefix(p, "bmdctl ") || strings.HasPrefix(p, "hdctl ") {
			out = append(out, p)
		}
	}
	return out
}

// builtinArgs is what a built-in takes after its name, as its summary in
// builtins says: "list|add|..." its sub-commands, "HOST" one argument,
// nothing said anything.
func builtinArgs(name string) (subs []string, one bool) {
	for _, b := range builtins {
		if b.name != name {
			continue
		}
		i := strings.LastIndex(b.summary, name+" ")
		if i < 0 {
			return nil, false
		}
		f := strings.Fields(b.summary[i+len(name)+1:])
		switch {
		case len(f) == 0 || strings.HasPrefix(f[0], "["):
		case strings.ToLower(f[0]) == f[0]:
			subs = strings.Split(f[0], "|")
		default:
			one = true
		}
	}
	return subs, one
}

// checkWords reads a command line as bmdctl would and runs the checks a
// line gets before it is sent: a built-in's sub-command and arguments
// as its summary gives them, else the device command it names, with its
// arguments, read without a switcher (precheck). A first word that is
// no command is taken for a host, as bmdctl takes it.
func checkWords(c *cli, args []string) error {
	device := ""
	if len(args) > 0 {
		switch args[0] {
		case "bmdctl":
			args = args[1:]
		case "hdctl":
			device, args = "hyperdeck", args[1:]
		}
	}
	g, words, tokens, err := splitLine(args)
	if err != nil {
		return err
	}
	builtin := func(w string) bool {
		return w == "__complete" || slices.ContainsFunc(builtins, func(b struct{ name, summary string }) bool { return b.name == w })
	}
	if device == "" && len(words) > 0 && slices.Contains(hosts.Devices, words[0]) {
		device, words = words[0], words[1:]
	}
	if len(words) > 0 && (device == "" && !c.isCommand(words[0]) || device != "" && !builtin(words[0]) && !c.isCommandWord(device, words[0])) {
		words = words[1:] // the host
	}
	if len(words) == 0 {
		if len(tokens) == 0 {
			return nil // bmdctl alone, or a device's word alone: its help
		}
		return errors.New("no command")
	}
	if builtin(words[0]) {
		subs, one := builtinArgs(words[0])
		switch {
		case subs != nil && len(words) > 1 && !slices.Contains(subs, words[1]):
			return fmt.Errorf("bmdctl %s has no %q (%s)", words[0], words[1], strings.Join(subs, ", "))
		case one && len(words) != 2:
			return fmt.Errorf("bmdctl %s takes one argument", words[0])
		}
		return nil
	}
	devices := []string{"atem", "hyperdeck"}
	if device != "" {
		devices = []string{device}
	}
	var e *table.Entry
	var raw map[string]string
	for _, d := range devices {
		var nc *table.NoCommand
		if e, raw, err = c.reg.ParseLine(d, words, tokens); !errors.As(err, &nc) {
			break
		}
	}
	if err != nil {
		return err
	}
	return precheck(e, raw, g)
}
