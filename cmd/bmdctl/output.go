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

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// printer prints what one request brings back: files to disk, responses
// to stdout (or, failed, to stderr) in the chosen form.
type printer struct {
	c      *cli
	g      globals
	e      *table.Entry
	params map[string]string

	last       *engine.Response
	written    []string // files written, in order
	stdoutUsed bool     // a file went to stdout: print nothing else there
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

func (p *printer) response(line []byte, r engine.Response) {
	p.last = &r
	switch {
	case p.g.json:
		if !p.g.quiet && !p.stdoutUsed {
			fmt.Fprintf(p.c.stdout, "%s\n", line)
		}
	case !r.OK:
		fmt.Fprintf(p.c.stderr, "bmdctl %s: %s\n", p.label(), r.Error)
	case !p.g.quiet && !p.stdoutUsed:
		for _, l := range p.human(r) {
			fmt.Fprintln(p.c.stdout, l)
		}
	}
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

// colour wraps s in an ANSI colour when stdout is a terminal and the
// user hasn't asked for none (NO_COLOR, TERM=dumb).
func (c *cli) colour(code, s string) string {
	if !c.tty || c.env("NO_COLOR") != "" || c.env("TERM") == "dumb" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

const (
	green  = "32"
	yellow = "33"
	red    = "31"
	bold   = "1"
)

// human renders a response as lines: one per result.
func (p *printer) human(r engine.Response) []string {
	raw, _ := r.Result.(json.RawMessage)
	if f := humanFormats[p.e.Path]; f != nil {
		return f(p, r, raw)
	}
	if !p.e.Operation || isCommandResult(raw) {
		return []string{p.commandLine(r, raw)}
	}
	return p.table(flatten(raw))
}

// commandLine is a wire command's result: the command, whether the
// switcher confirmed it, how long it took and what was sent.
func (p *printer) commandLine(r engine.Response, raw json.RawMessage) string {
	var res engine.CommandResult
	json.Unmarshal(raw, &res)
	status := p.c.colour(yellow, padRight("sent", 9))
	if r.Confirmed {
		status = p.c.colour(green, padRight("confirmed", 9))
	}
	var args []string
	for _, prm := range p.e.Params {
		if v, ok := res.Args[prm.Name]; ok {
			args = append(args, prm.Name+" "+v)
		}
	}
	return strings.TrimRight(fmt.Sprintf("%-24s %s %5d ms  %s", p.e.Path, status, r.TookMS, strings.Join(args, ", ")), " ")
}

// isCommandResult reports whether an operation's result is one command
// sent (engine.CommandResult).
func isCommandResult(raw json.RawMessage) bool {
	var r struct {
		Command *string `json:"command"`
	}
	return json.Unmarshal(raw, &r) == nil && r.Command != nil
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
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

// humanFormats renders operations whose results read better than
// flattened: by entry path.
var humanFormats = map[string]func(p *printer, r engine.Response, raw json.RawMessage) []string{}
