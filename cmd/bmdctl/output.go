// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// printer prints what one request brings back: files to disk, notes to
// stderr, responses to stdout (or, failed, to stderr) in the chosen form.
type printer struct {
	c      *cli
	g      globals
	e      *table.Entry
	params map[string]string
	t      target // where the request went

	last       *engine.Response
	written    []string // files written, in order
	stdoutUsed bool     // a file went to stdout: print nothing else there
	watch      *watchView
	sw         *engine.Switcher // the switcher, from the engine's note
	vals       []engine.Value   // what the command set, from its note
	connected  bool             // this request opened the session
}

func (p *printer) label() string { return strings.TrimSpace(p.e.Device + " " + p.e.Path) }

func (p *printer) file(f engine.FileFrame) error {
	path, err := outPath(p.e, p.params, f)
	if err != nil {
		return err
	}
	if path == "-" {
		p.stdoutUsed = true
		_, err := p.c.stdout.Write(f.Data)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, f.Data, 0o644); err != nil {
		return err
	}
	p.written = append(p.written, path)
	return nil
}

// human reports whether the request's output is for a person: not
// --json, not --quiet.
func (p *printer) human() bool { return !p.g.json && !p.g.quiet }

// note takes one of the engine's notes: a connect is said on stderr, the
// switcher kept (and its name adopted into hosts.yml), a command's
// values kept for its line.
func (p *printer) note(n engine.Note) {
	switch {
	case n.Connecting != "":
		p.connected = true
		p.c.sayConnecting(p.g, p.who())
	case n.Switcher != nil:
		p.sw = n.Switcher
		p.c.adopt(p.g, &p.t, *n.Switcher)
		if p.c.connecting && p.human() {
			p.c.connecting = false
			fmt.Fprintln(p.c.stderr, strings.Join(nonEmpty(n.Switcher.Product, n.Switcher.VideoMode), ", "))
		}
	case n.Values != nil:
		p.vals = n.Values
	}
}

func (p *printer) response(line []byte, r engine.Response) {
	p.last = &r
	switch {
	case p.g.json:
		if !p.g.quiet && !p.stdoutUsed {
			fmt.Fprintf(p.c.stdout, "%s\n", line)
		}
	case !r.OK:
		p.c.errorf(exitCode(r.Code), "bmdctl %s: %s", p.label(), r.Error)
	case !p.g.quiet && !p.stdoutUsed:
		lines := p.humanLines(r)
		if reports[p.e.Path] && len(lines) > 0 && !(p.e.Path == "state" && !isTable(r)) {
			fmt.Fprintln(p.c.stdout, p.header())
		}
		for _, l := range lines {
			fmt.Fprintln(p.c.stdout, l)
		}
	}
}

// reports are the commands that read the switcher and print rows: they
// start with a header naming the switcher. A path to one value of the
// state prints the value alone, for scripts.
var reports = map[string]bool{
	"status": true, "info": true, "state": true, "inputs": true, "media list": true, "macro list": true,
	"video-modes": true, "time": true, "audio meters": true, "classic-audio meters": true,
}

// isTable reports whether a state result is an object or a list, not one
// value.
func isTable(r engine.Response) bool {
	raw, _ := r.Result.(json.RawMessage)
	v := parseOrdered(raw)
	return v.obj != nil || v.arr != nil
}

// who names the switcher a line is about: "<name> (<address>)", the name
// being the host's in hosts.yml (the switcher's own, unless --as gave
// one), else the one the switcher gives itself; the address alone when
// neither is known.
func (p *printer) who() string {
	name := ""
	switch {
	case p.t.Host != nil:
		name = p.t.Host.Label()
	case p.sw != nil:
		name = p.sw.Name
	}
	addr := shortAddress(p.t.Address)
	if name == "" {
		return addr
	}
	return name + " (" + addr + ")"
}

// header is a report's first line: the switcher, its model and video
// mode.
func (p *printer) header() string {
	parts := []string{p.who()}
	if p.sw != nil {
		parts = append(parts, nonEmpty(p.sw.Product, p.sw.VideoMode)...)
	}
	return p.c.colour(bold, strings.Join(parts, "  "))
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// code is the exit status for the last response.
func (p *printer) code() int {
	switch {
	case p.last == nil:
		return exitUnreachable
	case p.last.OK:
		if f := resultCodes[p.e.Path]; f != nil {
			raw, _ := p.last.Result.(json.RawMessage)
			return f(raw)
		}
		return exitOK
	}
	return exitCode(p.last.Code)
}

// resultCodes give a successful response an exit status from its result,
// by path: a restore that recorded errors applied only part of the
// profile.
var resultCodes = map[string]func(json.RawMessage) int{
	"profile restore": func(raw json.RawMessage) int {
		var r engine.ProfileResult
		json.Unmarshal(raw, &r)
		if len(r.Errors) > 0 {
			return exitRejected
		}
		return exitOK
	},
}

// colours reports whether output may be coloured: it goes to a terminal
// and the user hasn't asked for none (--no-color, NO_COLOR, TERM=dumb).
func (c *cli) colours(terminal bool) bool {
	return terminal && !c.noColor && c.env("NO_COLOR") == "" && c.env("TERM") != "dumb"
}

// colour wraps s in an ANSI colour for stdout, when it takes one.
func (c *cli) colour(code, s string) string {
	if !c.colours(c.tty) || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// colourErr is colour for stderr.
func (c *cli) colourErr(code, s string) string {
	if !c.colours(c.ttyErr) || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Colours: program red and preview green, as tally lamps are; errors red;
// a command sent without an echo to confirm it dim.
const (
	green  = "32"
	yellow = "33"
	red    = "31"
	bold   = "1"
	dim    = "2"
)

// humanLines renders a response as lines: one per result.
func (p *printer) humanLines(r engine.Response) []string {
	raw, _ := r.Result.(json.RawMessage)
	if f := humanFormats[p.e.Path]; f != nil {
		return f(p, r, raw)
	}
	if !p.e.Operation || isCommandResult(raw) {
		return []string{p.commandLine(r, raw)}
	}
	return p.table(flatten(raw))
}

// commandLine is a write's result line (docs/PORTING.md, "Output
// conventions"): the switcher; the command as said, with the arguments
// that say where; what changed, before → after; confirmed or sent; the
// time taken. A command the engine sent in place of the one asked for
// (classic-audio master for audio master) is shown as itself, with the
// notice an unverified one gets.
func (p *printer) commandLine(r engine.Response, raw json.RawMessage) string {
	var res engine.CommandResult
	json.Unmarshal(raw, &res)
	e := p.e
	if sent := p.c.reg.Find(e.Device, res.Sent); sent != nil {
		e = sent
		if e.Unverified {
			p.c.notice("%s %s is unverified on hardware: atemwire restored it from pyatem without checking it on a switcher", e.Device, e.Path)
		}
	}
	vals := p.vals
	if vals == nil {
		// No note (an engine of another build): what was sent, as the
		// result says it.
		for _, prm := range e.Params {
			if v, ok := res.Args[prm.Name]; ok {
				vals = append(vals, engine.Value{Param: prm.Name, After: v})
			}
		}
	}
	what, where, change := p.describe(e, vals)
	return p.writeLine(what, where, change, r.Confirmed, r.TookMS)
}

// writeLine lays a write's result out in columns, so the lines of exec
// line up: the switcher, what was done and where, what changed, the
// status, the time. What changed is as wide as a change between the
// switcher's two longest source names ("CAM2 → CAM5" on a switcher whose
// names are its four-letter short names); one that is wider pushes the
// status along.
func (p *printer) writeLine(what, where, change string, confirmed bool, took int64) string {
	status := p.c.colour(dim, padRight("sent", 9))
	if confirmed {
		status = padRight("confirmed", 9)
	}
	head := what
	if where != "" {
		head += "  " + where
	}
	changeWidth := 24 // without the engine's note
	if p.sw != nil && p.sw.NameWidth > 0 {
		changeWidth = 2*p.sw.NameWidth + width(" → ")
	}
	return fmt.Sprintf("%s  %s  %s  %s %5d ms", p.who(), padRight(head, 22), padRight(change, changeWidth), status, took)
}

// describe splits a command's values into what was done (its words, with
// the positional arguments that say where it was done, as said: "usk 2
// on-air", "aux 2"), where (the M/E or keyer given by a flag: ME1, DSK2)
// and what changed: "CAM2 → CAM5" for a command's one value, else "rate
// 1:00 → 0:12, width 25 → 50", a value the switcher already held, or
// whose before the state doesn't say, alone.
func (p *printer) describe(e *table.Entry, vals []engine.Value) (what, where, change string) {
	byParam := map[string]engine.Value{}
	for _, v := range vals {
		byParam[v.Param] = v
	}
	param := func(name string) *table.Param {
		if i := slices.IndexFunc(e.Params, func(x table.Param) bool { return x.Name == name }); i >= 0 {
			return &e.Params[i]
		}
		return nil
	}
	// A parameter says where when the echo is keyed by it, or, for a
	// command without one, when it is an index.
	locates := func(prm *table.Param) bool {
		return prm != nil && (strings.Contains(e.Echo, "<"+prm.Name+">") || e.Echo == "" && prm.OneBased)
	}
	var words []string
	for _, x := range e.Elems() {
		switch {
		case x.Word != "":
			words = append(words, x.Word)
		case locates(param(x.Param)):
			if v, ok := byParam[x.Param]; ok {
				words = append(words, v.After)
			}
		}
	}
	var tags, changes []string
	var changed []engine.Value
	for _, v := range vals {
		prm := param(v.Param)
		switch {
		case locates(prm) && e.Positional(v.Param):
		case locates(prm):
			if tag := flagTag(v.Param, v.After); tag != "" {
				tags = append(tags, tag)
			} else if _, given := p.params[v.Param]; given {
				tags = append(tags, v.Param+" "+v.After)
			}
		default:
			changed = append(changed, v)
		}
	}
	for _, v := range changed {
		after := v.After
		switch {
		case e.Path == "program" && v.Param == "source", v.Param == "program":
			after = p.c.colour(red, after)
		case e.Path == "preview" && v.Param == "source":
			after = p.c.colour(green, after)
		}
		ch := after
		if v.Before != nil && *v.Before != v.After {
			ch = *v.Before + " → " + after
		}
		prm := param(v.Param)
		if len(changed) > 1 || prm != nil && !(prm.Required && e.Positional(v.Param)) {
			ch = v.Param + " " + ch
		}
		changes = append(changes, ch)
	}
	return strings.Join(words, " "), strings.Join(tags, " "), strings.Join(changes, ", ")
}

// flagTag is how an index given by a flag reads in a line: ME1, DSK2,
// MV1, SS1; "" for any other.
func flagTag(name, v string) string {
	switch name {
	case "me":
		return "ME" + v
	case "dsk":
		return "DSK" + v
	case "multiviewer":
		return "MV" + v
	case "supersource":
		return "SS" + v
	}
	return ""
}

// shortAddress is an address as a line shows it: without the ATEM's
// default port.
func shortAddress(addr string) string {
	return strings.TrimSuffix(addr, ":9910")
}

// since is a duration as a person reads it: 12s, 3m12s, 1h04m.
func since(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// isCommandResult reports whether an operation's result is one command
// sent (engine.CommandResult).
func isCommandResult(raw json.RawMessage) bool {
	var r struct {
		Command *string `json:"command"`
	}
	return json.Unmarshal(raw, &r) == nil && r.Command != nil
}

// padRight pads s with spaces to n columns, counting what shows: runes,
// not bytes, and no colour codes.
func padRight(s string, n int) string {
	w := width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// width is how many columns s takes on a terminal: its runes, less ANSI
// colour codes.
func width(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if j := strings.IndexByte(s[i:], 'm'); j >= 0 {
				i += j + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

// table aligns rows of cells into lines.
func (p *printer) table(rows [][]string) []string {
	var b bytes.Buffer
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
	out := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}

// flatten turns a JSON value into "dotted.key value" rows, in the
// document's own key order, map keys that are numbers in numeric order.
func flatten(raw json.RawMessage) [][]string {
	var rows [][]string
	var walk func(prefix string, v ordered)
	walk = func(prefix string, v ordered) {
		switch {
		case v.obj != nil:
			if len(v.obj) == 0 && prefix != "" {
				rows = append(rows, []string{prefix, "{}"})
			}
			for _, kv := range v.obj {
				walk(join(prefix, kv.k), kv.v)
			}
		case v.arr != nil:
			if len(v.arr) == 0 && prefix != "" {
				rows = append(rows, []string{prefix, "[]"})
			}
			for i, e := range v.arr {
				walk(join(prefix, strconv.Itoa(i)), e)
			}
		default:
			if prefix == "" {
				prefix = "value"
			}
			rows = append(rows, []string{prefix, v.scalar})
		}
	}
	walk("", parseOrdered(raw))
	return rows
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

// ordered is a JSON value that keeps its objects' key order.
type ordered struct {
	obj    []kv // nil unless an object
	arr    []ordered
	scalar string // the literal, strings unquoted
}

type kv struct {
	k string
	v ordered
}

func parseOrdered(raw json.RawMessage) ordered {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, _ := decodeOrdered(dec)
	return v
}

func decodeOrdered(dec *json.Decoder) (ordered, error) {
	t, err := dec.Token()
	if err != nil {
		return ordered{}, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			o := ordered{obj: []kv{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return o, err
				}
				v, err := decodeOrdered(dec)
				if err != nil {
					return o, err
				}
				o.obj = append(o.obj, kv{kt.(string), v})
			}
			dec.Token()
			if numericKeys(o.obj) {
				sort.SliceStable(o.obj, func(i, j int) bool {
					a, _ := strconv.Atoi(o.obj[i].k)
					b, _ := strconv.Atoi(o.obj[j].k)
					return a < b
				})
			}
			return o, nil
		case '[':
			o := ordered{arr: []ordered{}}
			for dec.More() {
				v, err := decodeOrdered(dec)
				if err != nil {
					return o, err
				}
				o.arr = append(o.arr, v)
			}
			dec.Token()
			return o, nil
		}
	case string:
		return ordered{scalar: x}, nil
	case nil:
		return ordered{scalar: "null"}, nil
	}
	return ordered{scalar: fmt.Sprint(t)}, nil
}

func numericKeys(kvs []kv) bool {
	if len(kvs) == 0 {
		return false
	}
	return !slices.ContainsFunc(kvs, func(x kv) bool {
		_, err := strconv.Atoi(x.k)
		return err != nil
	})
}

// formatState prints a leaf of the state (--path to a number, a string, a
// bool) as its bare value, for scripts; anything bigger as one
// dotted.key value line per leaf.
func formatState(p *printer, _ engine.Response, raw json.RawMessage) []string {
	if v := parseOrdered(raw); v.obj == nil && v.arr == nil {
		return []string{v.scalar}
	}
	return p.table(flatten(raw))
}

// humanFormats renders operations whose results read better than
// flattened: by entry path.
var humanFormats = map[string]func(p *printer, r engine.Response, raw json.RawMessage) []string{}
