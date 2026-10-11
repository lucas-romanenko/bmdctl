// SPDX-License-Identifier: MIT

package main

// The terminal's layout (docs/PORTING.md, "Terminal"; docs/terminal-mock.html
// is the picture): the switcher named once, in a header, and each result
// indented under it as "<verb>  <before> → <after>  <timing>". Only a
// person at a terminal sees it; a pipe, --json and --quiet get what they
// always had (output.go).

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

const (
	lineWidth = 72 // nothing printed is wider; longer lines are cut with "…"
	timingEnd = 64 // a result's timing ends here, right-aligned
	verbWidth = 10 // the verb column
	indent    = "  "
	hintPad   = "       " // under an error's message: len("error  ")
)

// screenState is what an invocation has printed at a terminal, shared by
// exec's lines: whose header is up.
type screenState struct {
	header string
}

// view is what one response shows at a terminal: the header's own
// parts, and the rows under it.
type view struct {
	about  []string // the header's parts after what is live
	noLive bool     // the header leaves PGM · PVW out: the rows say it
	model  bool     // the header has the model and video mode, connected now or not
	bare   bool     // no header: nothing the switcher said
	gap    bool     // a blank line under the header
	whole  bool     // the rows are a document (XML), printed whole, never cut
	rows   []string // indented already
}

// fit cuts s to n columns as they show, ending it with "…" when it is
// cut. Colour codes are kept, and one left open is closed.
func fit(s string, n int) string {
	if width(s) <= n {
		return s
	}
	var b strings.Builder
	cols, open := 0, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if j := strings.IndexByte(s[i:], 'm'); j >= 0 {
				code := s[i : i+j+1]
				b.WriteString(code)
				open = code != "\x1b[0m"
				i += j + 1
				continue
			}
		}
		if cols == n-1 {
			break
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		i += size
		cols++
	}
	b.WriteString("…")
	if open {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// took is a time as a result shows it: 32 ms, 1.1 s.
func took(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}

// ago is a duration as a header shows it: 12s, 4m, 1h04m.
func ago(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// verbCell is a verb in its column, padded to verbWidth, then the gap.
func verbCell(verb string) string { return padRight(verb, verbWidth) + "  " }

// resultLine is a result under the header: the verb in its column, what
// it did, and its status (the time taken, or sent, already, rejected)
// dim and right-aligned. A line that would pass lineWidth loses the end
// of what it did, not its status.
func (c *cli) resultLine(verb, change, status string) string {
	left := indent + verbCell(verb) + change
	if change == "" {
		left = strings.TrimRight(left, " ")
	}
	if status == "" {
		return fit(left, lineWidth)
	}
	sw := width(status)
	if width(left)+2+sw > lineWidth {
		left = fit(left, lineWidth-2-sw)
	}
	end := max(timingEnd, width(left)+2+sw)
	return left + strings.Repeat(" ", end-sw-width(left)) + c.colour(dim, status)
}

// rows lays cells out in columns (align), each line indented.
func rows(cells [][]string) []string {
	out := align(cells)
	for i := range out {
		out[i] = indent + strings.TrimRight(out[i], " ")
	}
	return out
}

// name is the switcher as a header names it: its host's name, else the
// name it gives itself, else its address.
func (p *printer) name() string {
	name, addr := p.whoParts()
	if name == "" {
		return addr
	}
	return name
}

// headerLine is the switcher's name, bold, then dim: what is live on M/E
// 1, the model, video mode and how long connecting took when this
// invocation opened the session, and the view's own parts.
//
// What is live comes first. Where the line would pass lineWidth, the
// model goes, then the video mode, before anything is cut.
func (p *printer) headerLine(v view) string {
	var live, model, mode, connected []string
	if p.live != nil && !v.noLive {
		live = []string{"PGM " + p.live.Program + " · PVW " + p.live.Preview}
	}
	if p.sw != nil && (v.model || p.connectedIn > 0) {
		if p.sw.Product != p.name() {
			model = nonEmpty(p.sw.Product)
		}
		mode = nonEmpty(p.sw.VideoMode)
	}
	if p.connectedIn > 0 {
		connected = []string{fmt.Sprintf("connected %d ms", p.connectedIn.Milliseconds())}
	}
	line := func() string {
		parts := slices.Concat(live, model, mode, connected, v.about)
		l := p.c.colour(bold, p.name())
		if len(parts) > 0 {
			l += "  " + p.c.colour(dim, strings.Join(parts, " · "))
		}
		return l
	}
	l := line()
	if width(l) > lineWidth && model != nil {
		model = nil
		l = line()
	}
	if width(l) > lineWidth && mode != nil {
		mode = nil
		l = line()
	}
	return fit(l, lineWidth)
}

// show prints a view: the header, once per invocation for a switcher
// (exec's lines share it), then the rows.
func (p *printer) show(v view) {
	out := p.c.stdout
	if !v.bare && p.c.scr.header != p.name() {
		p.c.scr.header = p.name()
		fmt.Fprintln(out, p.headerLine(v))
		if v.gap && len(v.rows) > 0 {
			fmt.Fprintln(out)
		}
	}
	for _, r := range v.rows {
		if !v.whole {
			r = fit(r, lineWidth)
		}
		fmt.Fprintln(out, r)
	}
}

// screen is a successful response's view.
func (p *printer) screen(r engine.Response) view {
	raw, _ := r.Result.(json.RawMessage)
	if f := screenFormats[p.e.Path]; f != nil {
		return f(p, r, raw)
	}
	if !p.e.Operation || isCommandResult(raw) {
		return p.writeView(r, raw)
	}
	var cells [][]string
	for _, row := range flatten(raw) {
		cells = append(cells, row)
	}
	return view{gap: true, rows: rows(cells)}
}

// screenFormats are the terminal's views of operations, by path.
var screenFormats = map[string]func(p *printer, r engine.Response, raw json.RawMessage) view{}

// writeView is a write's line: what was done and where, what changed,
// and the time it took; sent when nothing confirms it, rejected when the
// switcher answered with the value it already held.
func (p *printer) writeView(r engine.Response, raw json.RawMessage) view {
	e, vals := p.sentEntry(raw)
	verb, change := p.describeTerm(e, vals)
	status := took(r.TookMS)
	switch {
	case !r.Confirmed:
		status = "sent"
	case kept(vals):
		status = "rejected"
	}
	return view{rows: []string{p.c.resultLine(verb, change, status)}}
}

func kept(vals []engine.Value) bool {
	for _, v := range vals {
		if v.Kept {
			return true
		}
	}
	return false
}

// describeTerm is describe for a terminal: the verb with the arguments
// that say where (and a flag that does only when typed), then what
// changed; a cut or an auto transition says what each bus has now.
func (p *printer) describeTerm(e *table.Entry, vals []engine.Value) (verb, change string) {
	what, where, change := p.describe(e, vals, true)
	verb = strings.TrimSpace(what + " " + where)
	if e.Path == "cut" || e.Path == "auto" {
		change = ""
		for _, v := range vals {
			if v.Param == "program" && v.Before != nil {
				change = p.c.colour(red, v.After) + " on program · " + p.c.colour(green, *v.Before) + " on preview"
			}
		}
	}
	return verb, change
}

// screenError prints a failed response at a terminal: the switcher by
// name where it didn't answer, else the engine's message.
func (p *printer) screenError(r engine.Response) {
	code := exitCode(r.Code)
	msg, hint := splitHint(r.Error)
	if r.Code == engine.CodeUnreachable && p.t.Address != "" {
		cause := msg
		msg = p.who() + " did not answer"
		if strings.HasPrefix(cause, "no switcher answered at ") {
			cause = ""
		}
		hint = strings.Join(nonEmpty(cause, "bmdctl doctor checks the network"), " · ")
	}
	if n := p.noInput; n != nil && code == exitUsage {
		msg, hint = fmt.Sprintf("no input %q", n.Typed), p.inputsHint(n.Inputs)
	}
	if hint == "" {
		hint = termStep(code)
	}
	p.c.screenError(msg, hint)
}

// inputsHint is the inputs a source could have been, on one line: "1
// CAM1 Camera 1 · 2 CAM2 Camera 2 ...", then where the rest are. The
// list is cut, never the end.
func (p *printer) inputsHint(ins []engine.Source) string {
	var list []string
	for _, in := range ins {
		list = append(list, strings.Join(nonEmpty(strconv.Itoa(in.ID), in.Short, in.Name), " "))
	}
	tail := "bmdctl inputs for all"
	if len(list) == 0 {
		return tail
	}
	tail = " · " + tail
	return fit(strings.Join(list, " · "), lineWidth-len(hintPad)-width(tail)) + tail
}

// screenError is an error at a terminal: "error" red, the message, and
// under it, dim, what to do next.
func (c *cli) screenError(msg, hint string) { c.screenErrorTo(c.stderr, msg, hint) }

func (c *cli) screenErrorTo(w io.Writer, msg, hint string) {
	lines := strings.Split(msg, "\n")
	fmt.Fprintln(w, fit(c.colourErr(red, "error")+"  "+lines[0], lineWidth))
	for _, l := range lines[1:] {
		fmt.Fprintln(w, fit(hintPad+strings.TrimLeft(l, " "), lineWidth))
	}
	if hint != "" {
		fmt.Fprintln(w, hintPad+c.colourErr(dim, fit(hint, lineWidth-len(hintPad))))
	}
}

// termStep is what to do next after a failure, by its exit status, as a
// terminal says it.
func termStep(code int) string {
	switch code {
	case exitUnreachable:
		return "bmdctl doctor checks the network"
	case exitNoEcho:
		return "bmdctl status shows what the switcher holds now"
	case exitDestructive:
		return "add --yes to do it"
	}
	return ""
}

// splitHint takes what to do next off a message: a trailing
// " (bmdctl ...)", or ": add --yes ...", as the engine ends one.
func splitHint(msg string) (rest, hint string) {
	if rest, hint = splitStep(msg); hint != "" {
		return rest, hint
	}
	if i := strings.Index(msg, ": add --yes"); i >= 0 && !strings.Contains(msg[i:], "\n") {
		return msg[:i], msg[i+2:]
	}
	if i := strings.Index(msg, " is destructive: add --yes"); i >= 0 {
		return msg[:i] + " is destructive", msg[i+len(" is destructive: "):]
	}
	return msg, ""
}

// stripLabel takes the command off the front of a message the CLI
// made: "bmdctl atem program: x" is "x". The person just typed it.
func stripLabel(msg string) string {
	if !strings.HasPrefix(msg, "bmdctl") {
		return msg
	}
	if i := strings.Index(msg, ": "); i >= 0 && !strings.Contains(msg[:i], "\n") {
		return msg[i+2:]
	}
	return msg
}

// dangers say what a destructive command does, as a terminal refuses it
// without --yes, by path.
var dangers = map[string]string{
	"video-mode":           "video mode resyncs every output",
	"media clear":          "media clear empties a still slot",
	"media clear-unlocked": "media clear empties a still slot",
	"macro delete":         "macro delete removes a macro for good",
	"profile restore":      "profile restore rewrites the switcher's setup",
	"startup save":         "startup save replaces what the switcher starts up with",
	"startup clear":        "startup clear forgets what the switcher starts up with",
}

// danger is what a destructive command does, for its refusal.
func danger(e *table.Entry) string {
	if d, ok := dangers[e.Path]; ok {
		return d
	}
	return e.Path + " can't be undone"
}
