// SPDX-License-Identifier: MIT

package main

// A Videohub's views (docs/PORTING.md, "Videohub commands"): status, the
// outputs and inputs, and watch, at a terminal and down a pipe. Routes
// and renames are writes (screen.go's writeView), from the engine's
// values notes. Ports count from 1, as Videohub Control numbers them.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	screenFormats["videohub status"] = screenHubStatus
	screenFormats["videohub outputs"] = screenHubOutputs
	screenFormats["videohub inputs"] = screenHubInputs
	screenFormats["videohub watch"] = screenHubWatch
	screenFormats["videohub connect"] = screenConnect
	screenFormats["videohub disconnect"] = screenDisconnect
	screenFormats["videohub state"] = screenState_
	humanFormats["videohub status"] = formatHubStatus
	humanFormats["videohub outputs"] = formatHubOutputs
	humanFormats["videohub inputs"] = formatHubInputs
	humanFormats["videohub watch"] = formatHubWatch
	humanFormats["videohub connect"] = formatConnect
	humanFormats["videohub disconnect"] = formatDisconnect
	humanFormats["videohub state"] = formatState
	for _, p := range []string{"status", "outputs", "inputs", "state"} {
		reports["videohub "+p] = true
	}
}

// hubRouteRows are a router's outputs at a terminal: the number dim, the
// label, the input routed to it; a lock at the line's end, dim.
func (p *printer) hubRouteRows(routes []engine.HubRoute) []string {
	c := p.c
	if len(routes) == 0 {
		return []string{indent + c.colour(dim, "no outputs")}
	}
	numWidth := len(strconv.Itoa(len(routes)))
	var cells [][]string
	for _, r := range routes {
		in := c.colour(dim, "none")
		if r.Input != nil {
			in = r.InputLabel
		}
		cells = append(cells, []string{c.colour(dim, padLeft(fmt.Sprint(r.Output), numWidth)), r.Label, in})
	}
	out := rows(cells)
	for i, r := range routes {
		if r.LockLetter != "U" && r.LockLetter != "" {
			out[i] = trailing(out[i], c.colour(dim, r.Lock))
		}
	}
	return out
}

func screenHubStatus(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.HubStatusResult
	json.Unmarshal(raw, &r)
	about := []string{fmt.Sprintf("%d × %d", r.Inputs, r.Outputs)}
	if r.Locked > 0 {
		about = append(about, count(r.Locked, "output")+" locked")
	}
	return view{about: about, model: true, gap: true, rows: p.hubRouteRows(r.Routes)}
}

func formatHubStatus(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.HubStatusResult
	json.Unmarshal(raw, &r)
	rows := [][]string{{"router", strings.Join(nonEmpty(r.Model, fmt.Sprintf("%d x %d", r.Inputs, r.Outputs),
		"protocol "+r.Protocol), ", ")}}
	rows = append(rows, hubRouteTable(r.Routes)...)
	rows = append(rows, []string{"session", sessionLine(r.Session)})
	return p.table(rows)
}

// hubRouteTable is the outputs down a pipe: number, label, the input's
// number and label (or none), the lock.
func hubRouteTable(routes []engine.HubRoute) [][]string {
	var rows [][]string
	for _, r := range routes {
		in := []string{"none", ""}
		if r.Input != nil {
			in = []string{fmt.Sprint(*r.Input), r.InputLabel}
		}
		rows = append(rows, append(append([]string{fmt.Sprint(r.Output), r.Label}, in...), r.Lock))
	}
	return rows
}

func screenHubOutputs(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.HubOutputs
	json.Unmarshal(raw, &r)
	return view{about: []string{count(len(r.Outputs), "output")}, gap: true, rows: p.hubRouteRows(r.Outputs)}
}

func formatHubOutputs(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.HubOutputs
	json.Unmarshal(raw, &r)
	return p.table(hubRouteTable(r.Outputs))
}

// screenHubInputs is a router's inputs: the number dim, the label, and
// dim the outputs it is routed to.
func screenHubInputs(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.HubInputs
	json.Unmarshal(raw, &r)
	c := p.c
	v := view{about: []string{count(len(r.Inputs), "input")}, gap: true}
	if len(r.Inputs) == 0 {
		v.rows = []string{indent + c.colour(dim, "no inputs")}
		return v
	}
	numWidth := len(strconv.Itoa(len(r.Inputs)))
	var cells [][]string
	for _, in := range r.Inputs {
		to := c.colour(dim, "routed nowhere")
		if len(in.Outputs) > 0 {
			to = c.colour(dim, "to "+outputList(in.Outputs))
		}
		cells = append(cells, []string{c.colour(dim, padLeft(fmt.Sprint(in.Number), numWidth)), in.Label, to})
	}
	v.rows = rows(cells)
	return v
}

func outputList[T fmt.Stringer](outs []T) string {
	var s []string
	for _, o := range outs {
		s = append(s, o.String())
	}
	return strings.Join(s, ", ")
}

func formatHubInputs(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.HubInputs
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, in := range r.Inputs {
		rows = append(rows, []string{fmt.Sprint(in.Number), in.Label, outputList(in.Outputs)})
	}
	return p.table(rows)
}

// hubWatch is what a router's watch remembers to show a change against:
// the labels, routing and locks as the last snapshot or change left
// them, keyed by the wire's index.
type hubWatch struct {
	inputs, outputs, locks map[string]string
	routing                map[string]string
}

// startHubWatch takes the router's state once watching has begun.
func (p *printer) startHubWatch() {
	w := &hubWatch{inputs: map[string]string{}, outputs: map[string]string{}, locks: map[string]string{},
		routing: map[string]string{}}
	var st struct {
		InputLabels  map[string]string `json:"input_labels"`
		OutputLabels map[string]string `json:"output_labels"`
		Routing      map[string]int    `json:"routing"`
		Locks        map[string]string `json:"locks"`
	}
	if p.askHub("state", &st) {
		w.inputs, w.outputs, w.locks = st.InputLabels, st.OutputLabels, st.Locks
		for k, v := range st.Routing {
			w.routing[k] = strconv.Itoa(v)
		}
	}
	p.hubWatch = w
}

// askHub asks the engine for one of the router's reads, quietly, within
// a second; false if it can't.
func (p *printer) askHub(path string, v any) bool {
	conn, err := p.c.dialRunning()
	if err != nil {
		return false
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(p.c.ctx, time.Second)
	defer cancel()
	resp, err := engine.Call(ctx, conn, engine.Request{Device: "videohub", Host: p.t.Address, Path: path, Build: p.c.build()}, nil)
	if err != nil || !resp.OK {
		return false
	}
	return json.Unmarshal(resp.Result.(json.RawMessage), v) == nil
}

// port is a port as a watch line names it: its number from 1 and its
// label, "in 3 Camera 3".
func (w *hubWatch) port(kind, idx string) string {
	n, err := strconv.Atoi(idx)
	if err != nil {
		return kind + " " + idx
	}
	label := w.outputs[idx]
	if kind == "in" {
		label = w.inputs[idx]
	}
	return strings.TrimSpace(fmt.Sprintf("%s %d %s", kind, n+1, label))
}

var lockWords = map[string]string{"U": "unlocked", "L": "locked", "O": "ours"}

// lines are one pushed block as watch lines: a line per index it
// carried, each before → after, by number and label.
func (w *hubWatch) lines(ev watchEvent) []watchLine {
	if ev.Kind == "disconnected" {
		return []watchLine{{verb: "session dropped", own: true}}
	}
	var vals map[string]string
	json.Unmarshal(ev.Value, &vals)
	keys := sortedKeys(vals)
	slices.SortStableFunc(keys, func(a, b string) int { x, _ := strconv.Atoi(a); y, _ := strconv.Atoi(b); return x - y })
	var out []watchLine
	for _, k := range keys {
		v := vals[k]
		switch ev.Key {
		case "routing":
			before := w.routing[k]
			l := watchLine{verb: "route", where: w.port("out", k), change: arrow(w.inName(before), w.inName(v))}
			w.routing[k] = v
			out = append(out, l)
		case "input labels":
			out = append(out, watchLine{verb: "label", where: w.port("in", k), change: arrow(w.inputs[k], v)})
			w.inputs[k] = v
		case "output labels":
			out = append(out, watchLine{verb: "label", where: w.port("out", k), change: arrow(w.outputs[k], v)})
			w.outputs[k] = v
		case "locks":
			out = append(out, watchLine{verb: "lock", where: w.port("out", k), change: arrow(lockWords[w.locks[k]], lockOr(v))})
			w.locks[k] = v
		default:
			out = append(out, watchLine{verb: ev.Key, change: k + " " + v})
		}
	}
	return out
}

func lockOr(v string) string {
	if w := lockWords[v]; w != "" {
		return w
	}
	return v
}

// inName is an input by the wire's index, as a watch line shows it: its
// label, else its number from 1.
func (w *hubWatch) inName(idx string) string {
	if idx == "" {
		return ""
	}
	if l := w.inputs[idx]; l != "" {
		return l
	}
	if n, err := strconv.Atoi(idx); err == nil {
		return "input " + strconv.Itoa(n+1)
	}
	return idx
}

func formatHubWatch(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		p.startHubWatch()
		fmt.Fprintln(p.c.stderr, p.c.colourErr(dim, "watching; Ctrl-C to stop"))
		return []string{p.header()}
	}
	if p.hubWatch == nil {
		p.startHubWatch()
	}
	var out []string
	for _, l := range p.hubWatch.lines(ev) {
		out = append(out, p.c.colour(dim, ev.Time)+"  "+l.text())
	}
	return out
}

func screenHubWatch(p *printer, _ engine.Response, raw json.RawMessage) view {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		p.startHubWatch()
		return view{about: []string{"watching · ctrl-c to stop"}}
	}
	if p.hubWatch == nil {
		p.startHubWatch()
	}
	clock, _, _ := strings.Cut(ev.Time, ".")
	var v view
	for _, l := range p.hubWatch.lines(ev) {
		v.rows = append(v.rows, fit(indent+p.c.colour(dim, clock)+"  "+l.screen(p.c.colour), lineWidth))
	}
	return v
}
