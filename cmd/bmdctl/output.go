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
	params map[string]string // as sent
	typed  map[string]string // as typed: a terminal may ask for more
	t      target            // where the request went

	last       *engine.Response
	written    []string // files written, in order
	stdoutUsed bool     // a file went to stdout: print nothing else there
	watch      *watchView
	deckWatch  *deckWatch       // a HyperDeck's watch (deck.go)
	hubWatch   *hubWatch        // a Videohub's watch (hub.go)
	sw         *engine.Switcher // the switcher, from the engine's note
	vals       []engine.Value   // what the command set, from its note
	connected  bool             // this request opened the session
	// live is what M/E 1 had on program and preview when the command
	// ran, from the engine's on_air note (the first: read once);
	// connectedIn how long opening the session took, when this request
	// opened it. A terminal's header says both.
	live        *engine.OnAir
	deckLive    *engine.DeckLive // a HyperDeck's transport, as live is a switcher's
	connectedIn time.Duration
	saved       remembered      // what connect ADDRESS did to hosts.yml
	noInput     *engine.NoInput // a source typed that names nothing
	// steps are an operation's steps (profile save, restore), done how
	// far (stepped), with what each did; failed is the one that failed.
	steps   []string
	details []string
	stepped int
	failed  *engine.Step
	// confirm is what a destructive command would affect, from the
	// engine, for a person to be asked.
	confirm *engine.Confirm
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
			name, addr := p.whoParts()
			p.connectedIn = p.c.sayConnected(name, addr, *n.Switcher, p.waiting())
		}
	case n.Values != nil:
		p.vals = n.Values
	case n.OnAir != nil:
		if p.live == nil {
			p.live = n.OnAir
		}
	case n.Deck != nil:
		if p.deckLive == nil {
			p.deckLive = n.Deck
		}
	case n.NoInput != nil:
		p.noInput = n.NoInput
	case n.Progress != nil:
		if p.c.spinner != nil {
			p.c.spinner.progress(*n.Progress)
		}
	case n.Steps != nil:
		p.steps = n.Steps
		if p.c.terminal() {
			p.show(view{}) // the header, before the steps under it
		}
	case n.Step != nil:
		p.step(*n.Step)
	case n.Confirm != nil:
		p.confirm = n.Confirm
	case n.Alias != nil && p.c.terminal():
		a := n.Alias
		if p.c.tellOnce(fmt.Sprintf("alias %s %s %d", p.t.Address, strings.ToLower(a.Typed), a.Shadowed.ID)) {
			p.c.notice("%s means input %d (%s); for the input named %q, type %d", a.Typed, a.Source.ID, short(a.Source), a.Typed, a.Shadowed.ID)
		}
	}
}

// typedAs is what was typed for a source, dim in brackets, where it
// isn't the name a line shows it by (nor a number, which says itself):
// " (cam1)" after "lkkk".
func (p *printer) typedAs(param, shown string) string {
	typed := strings.TrimSpace(p.typed[param])
	if typed == "" || isNumber(typed) || strings.EqualFold(typed, shown) {
		return ""
	}
	return " " + p.c.colour(dim, "("+typed+")")
}

func (p *printer) response(line []byte, r engine.Response) {
	p.last = &r
	if r.OK && p.e.Path == "connect" && p.typed["address"] != "" {
		name := ""
		if p.sw != nil {
			name = p.sw.Name
		}
		p.saved = p.c.remember(p.g, p.t, p.e.Device, name)
	}
	switch {
	case p.g.json:
		if !p.g.quiet && !p.stdoutUsed {
			fmt.Fprintf(p.c.stdout, "%s\n", line)
		}
	case !r.OK && r.Code == engine.CodeDestructive && p.confirm != nil && p.c.asks():
		// A person will be asked (cli.send).
	case !r.OK && p.c.terminal():
		p.screenError(r)
	case !r.OK:
		p.c.errorf(exitCode(r.Code), "bmdctl %s: %s", p.label(), r.Error)
	case !p.g.quiet && !p.stdoutUsed && p.c.terminal():
		p.show(p.screen(r))
	case !p.g.quiet && !p.stdoutUsed:
		lines := p.humanLines(r)
		if reports[formatKey(p.e)] && len(lines) > 0 && !(p.e.Path == "state" && !isTable(r)) {
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
	name, addr := p.whoParts()
	if name == "" {
		return addr
	}
	return name + " (" + addr + ")"
}

// whoParts are who's name ("" when none is known) and address.
func (p *printer) whoParts() (name, addr string) {
	switch {
	case p.t.Host != nil:
		name = p.t.Host.Label()
	case p.sw != nil:
		name = p.sw.Name
	}
	return name, shortAddress(p.t.Address)
}

// whoOut is who as stdout shows it: the name bold, the address dim.
func (p *printer) whoOut() string {
	name, addr := p.whoParts()
	return paintWho(p.c.colour, name, addr)
}

// header is a report's first line: the switcher, its model and video
// mode.
func (p *printer) header() string {
	parts := []string{p.whoOut()}
	if p.sw != nil {
		parts = append(parts, nonEmpty(p.sw.Product, p.sw.VideoMode)...)
	}
	return strings.Join(parts, "  ")
}

// waiting is what the spinner says while a request waits on the
// switcher: what it moves, or whom it waits for.
func (p *printer) waiting() string {
	slot, file := p.params["slot"], p.params["file"]
	switch p.e.Path {
	case "media upload":
		return fmt.Sprintf("uploading %s → slot %s", file, slot)
	case "media download":
		return fmt.Sprintf("downloading slot %s", slot)
	case "macro upload":
		return fmt.Sprintf("uploading %s → macro %s", file, slot)
	case "macro download":
		return fmt.Sprintf("downloading macro %s", slot)
	case "profile save":
		return "saving the profile to " + file
	case "profile restore":
		return "restoring " + file
	case "profile images":
		return "downloading the stills"
	}
	if p.e.Device == "ultimatte" {
		switch p.e.Path {
		case "archive":
			return "archiving " + p.name()
		case "restore":
			return "restoring " + file + " to " + p.name()
		case "network set":
			return "setting " + p.name() + "'s network, then reading it back"
		}
	}
	return "waiting for " + p.name()
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
		if f := resultCodes[formatKey(p.e)]; f != nil {
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

// humanLines renders a response as lines: one per result.
func (p *printer) humanLines(r engine.Response) []string {
	raw, _ := r.Result.(json.RawMessage)
	if f := humanFormats[formatKey(p.e)]; f != nil {
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
	e, vals := p.sentEntry(raw)
	what, where, change := p.describe(e, vals, false)
	return p.writeLine(what, where, change, r.Confirmed, r.TookMS)
}

// sentEntry is the entry of the command a result says was sent (the one
// asked for, or the one the engine sent in its place, with the notice an
// unverified one gets) and its values: from the engine's note, else as
// the result says it sent them.
func (p *printer) sentEntry(raw json.RawMessage) (*table.Entry, []engine.Value) {
	var res engine.CommandResult
	json.Unmarshal(raw, &res)
	e := p.e
	if sent := p.c.reg.Find(e.Device, res.Sent); sent != nil {
		e = sent
		if e.Unverified {
			p.c.unverified(e.Device + " " + e.Path)
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
	return e, vals
}

// writeLine lays a write's result out in columns, so the lines of exec
// line up: the switcher, what was done and where, what changed, the
// status, the time. What changed is as wide as a change between the
// switcher's two longest source names ("CAM2 → CAM5" on a switcher whose
// names are its four-letter short names); one that is wider pushes the
// status along.
// At a terminal the line starts with the prompt, the switcher's name
// bold and its address dim, sent and the time dim.
func (p *printer) writeLine(what, where, change string, confirmed bool, took int64) string {
	c := p.c
	status := c.colour(dim, padRight("sent", 9))
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
	return fmt.Sprintf("%s  %s  %s  %s %s", p.whoOut(), padRight(head, 22), padRight(change, changeWidth), status,
		c.colour(dim, fmt.Sprintf("%5d ms", took)))
}

// describe splits a command's values into what was done (its words, with
// the positional arguments that say where it was done, as said: "usk 2
// on-air", "aux 2"), where (the M/E or keyer given by a flag: ME1, DSK2)
// and what changed: "CAM2 → CAM5" for a command's one value, else "rate
// 1:00 → 0:12, width 25 → 50", a value the switcher already held, or
// whose before the state doesn't say, alone.
//
// At a terminal (term) a flag that says where is shown only when typed:
// the header has named the switcher, and ME1 on a one-M/E switcher is
// noise.
func (p *printer) describe(e *table.Entry, vals []engine.Value, term bool) (what, where, change string) {
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
	// command without one, when it is an index; or when it says so
	// (Where).
	locates := func(prm *table.Param) bool {
		return prm != nil && (prm.Where || strings.Contains(e.Echo, "<"+prm.Name+">") || e.Echo == "" && prm.OneBased)
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
			_, given := p.params[v.Param]
			if tag := flagTag(v.Param, v.After); tag != "" && (given || !term) {
				tags = append(tags, tag)
			} else if tag == "" && given {
				tags = append(tags, v.Param+" "+v.After)
			}
		default:
			changed = append(changed, v)
		}
	}
	for _, v := range changed {
		// What a tally lamp would show: the source put on program red, on
		// preview green; a keyer or DSK put on air, or fade to black, red.
		after := v.After
		switch {
		case e.Path == "program" && v.Param == "source", v.Param == "program",
			v.Param == "on-air" && v.After == "on", v.Param == "ftb" && v.After == "black",
			e.Device == "hyperdeck" && v.Param == "transport" && v.After == "record":
			after = p.c.colour(red, after)
		case e.Path == "preview" && v.Param == "source":
			after = p.c.colour(green, after)
		}
		if prm := param(v.Param); term && prm != nil && prm.Kind == table.Source {
			after += p.typedAs(v.Param, v.After)
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

// shortAddress is an address as a line shows it: without the ATEM's,
// the HyperDeck's, the Videohub's or the Ultimatte's default port.
func shortAddress(addr string) string {
	for _, p := range []string{":9910", ":9993", ":9990", ":9998"} {
		addr = strings.TrimSuffix(addr, p)
	}
	return addr
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

// table aligns rows of cells into lines (align), colour codes and all.
func (p *printer) table(rows [][]string) []string {
	out := align(rows)
	if len(out) == 0 {
		return nil
	}
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

// step takes a step's note: begun, the spinner says it; done, a line
// says so under the header, in place of the spinner; failed, it is kept
// for the response, which says why (stepFailed).
func (p *printer) step(s engine.Step) {
	if s.Index < 0 || s.Index >= len(p.steps) {
		return
	}
	label := p.steps[s.Index]
	switch {
	case s.Error != "":
		p.failed = &s
	case s.Done:
		p.stepped = s.Index + 1
		p.details = append(p.details, nonEmpty(s.Detail)...)
		if p.c.terminal() && !p.g.quiet {
			fmt.Fprintln(p.c.stdout, p.c.resultLine("✓ "+label, s.Detail, took(s.TookMS)))
		}
	default:
		if p.c.spinner != nil {
			p.c.spinner.set(label)
		}
	}
}

// stepFailed is a failed step at a terminal, in place: the step and why,
// red, what to do next under it, and the steps it left undone.
func (p *printer) stepFailed(msg, hint string) {
	c := p.c
	label := p.steps[p.failed.Index]
	fmt.Fprintln(c.stdout, fit(indent+c.colour(red, verbCell("✗ "+label)+msg), lineWidth))
	if hint != "" {
		fmt.Fprintln(c.stdout, indent+strings.Repeat(" ", verbWidth+2)+c.colour(dim, fit(hint, lineWidth-verbWidth-4)))
	}
	for _, l := range p.steps[p.failed.Index+1:] {
		fmt.Fprintln(c.stdout, indent+c.colour(dim, verbCell("· "+l)+"skipped"))
	}
}
