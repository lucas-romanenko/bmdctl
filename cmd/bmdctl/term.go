// SPDX-License-Identifier: MIT

package main

// What a person at a terminal sees besides the words (docs/PORTING.md,
// "Terminal"): the palette, the prompt before a result, the banner, an
// error's layout and columns measured as they show. None of it reaches
// a pipe, --json or --quiet.

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// The palette: ANSI's red and green, the tally colours, and the
// terminal's own text, bold or dim. No other hue (docs/BRAND.md), and
// nothing beyond the 16 colours, so every terminal shows it alike.
const (
	red   = "31" // program and on air; errors
	green = "32" // preview, and nothing else
	bold  = "1"  // the prompt, the switcher's name, bmdctl's own lines
	dim   = "2"  // labels, addresses, times, sent, empty slots, next steps
)

// prompt starts a result on a terminal: the mark's chevron, bold in the
// terminal's own colour as the mark's cursor is white.
const prompt = ">"

// terminal reports whether a person reads stdout at a terminal, and not
// through --json or --quiet: only then does a result take the prompt,
// an error its next step on a line of its own, and the banner and the
// spinner show. Colour is checked per stream (colours).
func (c *cli) terminal() bool { return c.tty && !c.json && !c.quiet }

// spins reports whether a wait shows the spinner: on a terminal, with
// stderr one too (it is drawn there), that can clear a line.
func (c *cli) spins() bool { return c.terminal() && c.ttyErr && c.env("TERM") != "dumb" }

// result is a result line as stdout shows it: after the prompt on a
// terminal, else as it is.
func (c *cli) result(line string) string {
	if !c.terminal() {
		return line
	}
	return c.colour(bold, prompt) + " " + line
}

// paintWho is "<name> (<address>)" with the name bold and the address
// dim; an address alone is bold, being the name.
func paintWho(paint func(code, s string) string, name, addr string) string {
	if name == "" {
		return paint(bold, addr)
	}
	return paint(bold, name) + " " + paint(dim, "("+addr+")")
}

// banner prints bmdctl's two lines, once per invocation: the wordmark,
// "bmd" dim and "ctl" bold after the prompt, then the version, the commit
// and the hosts file, dim. On bmdctl alone and when a command starts
// the engine; on stdout, only at a terminal.
func (c *cli) banner() {
	if !c.terminal() || *c.bannered {
		return
	}
	*c.bannered = true
	hosts, _ := c.hostsPath()
	for _, l := range bannerLines(c.colour, shortVersion(), commitString(), tildePath(hosts, c.env("HOME"))) {
		fmt.Fprintln(c.stdout, l)
	}
}

func bannerLines(paint func(code, s string) string, version, commit, hosts string) []string {
	about := []string{version}
	if commit != "" {
		about = append(about, "commit "+commit)
	}
	if hosts != "" {
		about = append(about, "hosts "+hosts)
	}
	return []string{
		paint(bold, prompt) + " " + paint(dim, "bmd") + paint(bold, "ctl"),
		"  " + paint(dim, strings.Join(about, ", ")),
	}
}

// shortVersion is the version alone: GoReleaser's, else the module's
// unless it is a pseudo-version (the commit beside it says which), else
// "(devel)".
func shortVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && !pseudoVersion.MatchString(bi.Main.Version) {
		return bi.Main.Version
	}
	return "(devel)"
}

// pseudoVersion matches the version Go gives a build of a commit no tag
// names: v0.0.0-20261011010118-274d6a26df98, perhaps +dirty.
var pseudoVersion = regexp.MustCompile(`-(\d+\.)?\d{14}-[0-9a-f]{12}(\+dirty)?$`)

// tildePath writes a path under home as ~/...
func tildePath(path, home string) string {
	if home == "" || path == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

// printError is an error on stderr. At a terminal: "error:" in red, the
// message, then its next step dim on a line of its own, under the
// message. Otherwise the message alone, its first line red where stderr
// takes colour.
func (c *cli) printError(w io.Writer, msg string) {
	if !c.terminal() {
		first, rest, _ := strings.Cut(msg, "\n")
		fmt.Fprint(w, c.colourErr(red, first)+"\n")
		if rest != "" {
			fmt.Fprintln(w, rest)
		}
		return
	}
	msg, step := splitStep(msg)
	fmt.Fprintln(w, c.colourErr(red, "error:")+" "+msg)
	if step != "" {
		fmt.Fprintln(w, c.colourErr(dim, strings.Repeat(" ", len("error: "))+step))
	}
}

// splitStep takes an error's next step off its end: " (bmdctl ...)",
// as nextStep and the engine end a message.
func splitStep(msg string) (rest, step string) {
	i := strings.LastIndex(msg, " (bmdctl ")
	if i < 0 || !strings.HasSuffix(msg, ")") || strings.ContainsAny(msg[i:], "\n") {
		return msg, ""
	}
	return msg[:i], msg[i+2 : len(msg)-1]
}

// align lays rows of cells out in columns as text/tabwriter does with
// two spaces' padding (a column runs over consecutive rows that have a
// cell in it before their last; a row's last cell is never padded), but
// measures a cell as it shows (width), where tabwriter would count a
// colour code's bytes.
func align(rows [][]string) []string {
	out := make([]string, len(rows))
	var widths []int
	write := func(from, to int) {
		for i := from; i < to; i++ {
			var b strings.Builder
			for j, cell := range rows[i] {
				b.WriteString(cell)
				if j < len(widths) {
					b.WriteString(strings.Repeat(" ", max(0, widths[j]-width(cell))))
				}
			}
			out[i] = b.String()
		}
	}
	var format func(line0, line1 int)
	format = func(line0, line1 int) {
		column := len(widths)
		for this := line0; this < line1; this++ {
			if column >= len(rows[this])-1 {
				continue
			}
			write(line0, this)
			line0 = this
			w := 0
			for ; this < line1 && column < len(rows[this])-1; this++ {
				w = max(w, width(rows[this][column])+2)
			}
			widths = append(widths, w)
			format(line0, this)
			widths = widths[:len(widths)-1]
			line0 = this
		}
		write(line0, line1)
	}
	format(0, len(rows))
	return out
}

// dimLabels dims each row's first cell, its label.
func (c *cli) dimLabels(rows [][]string) [][]string {
	for _, r := range rows {
		if len(r) > 0 {
			r[0] = c.colour(dim, r[0])
		}
	}
	return rows
}

// tallyColour is a source's colour by its tally lamps: red on program
// (on air), else green on preview, else none.
func tallyColour(l atem.TallyLamps) string {
	switch {
	case l.Program:
		return red
	case l.Preview:
		return green
	}
	return ""
}

// paintIf is paint when code isn't "".
func paintIf(paint func(code, s string) string, code, s string) string {
	if code == "" {
		return s
	}
	return paint(code, s)
}
