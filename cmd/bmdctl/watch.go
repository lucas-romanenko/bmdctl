// SPDX-License-Identifier: MIT

package main

// watch's human form. Each change prints as its time, atemwire's name for
// the state (the pretty key: "key-on-air:0:1", indices as the switcher's
// state counts them) and the value as JSON; the common ones get a line
// of their own: program and preview with the source they leave and the
// one they take, the tally sources that changed, a transition while it
// runs. --json prints the engine's frames as they come and none of this.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// watchEvent is one frame of watch's stream (engine.ChangeEvent, the
// value left raw).
type watchEvent struct {
	Kind   string          `json:"kind"`
	Opcode string          `json:"opcode"`
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Time   string          `json:"time"`
}

// watchView is what the human form remembers: names to show sources by,
// and the values a change is shown against.
type watchView struct {
	names      map[int]string // source: short name, else long name
	program    map[int]int    // M/E: source
	preview    map[int]int
	tally      map[int]atem.TallyLamps // source: lamps
	tallyKnown bool
	style      map[int]int       // M/E: transition style
	started    map[int]time.Time // M/E: when its transition began
}

var transitionStyles = []string{"mix", "dip", "wipe", "dve", "stinger"}

// newWatchView starts from a snapshot of the switcher's state; nil
// starts from nothing.
func newWatchView(st *atem.State) *watchView {
	v := &watchView{names: map[int]string{}, program: map[int]int{}, preview: map[int]int{},
		tally: map[int]atem.TallyLamps{}, style: map[int]int{}, started: map[int]time.Time{}}
	if st == nil {
		return v
	}
	for id, in := range st.Inputs {
		if in != nil {
			v.name(int(id), in.Name, in.ShortName)
		}
	}
	for me := range st.MEs {
		if st.MEs[me].Program != nil {
			v.program[me] = st.ProgramSource(me)
		}
		if st.MEs[me].Preview != nil {
			v.preview[me] = st.PreviewSource(me)
		}
		v.style[me] = st.TransitionStyle(me)
	}
	if t := st.Tally.Source; t != nil {
		v.tallyKnown = true
		for src, l := range t.Tally {
			v.tally[int(src)] = l
		}
	}
	return v
}

func (v *watchView) name(id int, long, short string) {
	if n := strings.TrimSpace(short); n != "" {
		v.names[id] = n
	} else if n := strings.TrimSpace(long); n != "" {
		v.names[id] = n
	}
}

// source is a source by its short name, else its id.
func (v *watchView) source(id int) string {
	if n, ok := v.names[id]; ok {
		return n
	}
	return fmt.Sprint(id)
}

// prettyKey is the change's key with atemwire's name for the state in
// place of the opcode.
func prettyKey(ev watchEvent) string {
	name := atem.PrettyName(ev.Opcode)
	if name == "" {
		return ev.Key
	}
	return name + strings.TrimPrefix(ev.Key, ev.Opcode)
}

// line renders one change, without its time.
func (v *watchView) line(ev watchEvent) string {
	if ev.Kind != "state" {
		return ev.Kind
	}
	at, _ := time.Parse("15:04:05.000", ev.Time)
	switch ev.Opcode {
	case "PrgI", "PrvI":
		var f struct {
			Index  int `json:"index"`
			Source int `json:"source"`
		}
		json.Unmarshal(ev.Value, &f)
		label, last := "program", v.program
		if ev.Opcode == "PrvI" {
			label, last = "preview", v.preview
		}
		was, known := last[f.Index]
		last[f.Index] = f.Source
		if known && was != f.Source {
			return fmt.Sprintf("%s  ME%v  %s → %s", label, table.Index(f.Index), v.source(was), v.source(f.Source))
		}
		return fmt.Sprintf("%s  ME%v  %s", label, table.Index(f.Index), v.source(f.Source))
	case "TlSr":
		var f atem.TallySourceField
		json.Unmarshal(ev.Value, &f)
		var srcs []int
		for s := range f.Tally {
			srcs = append(srcs, int(s))
		}
		for s := range v.tally {
			if _, ok := f.Tally[uint16(s)]; !ok {
				srcs = append(srcs, s)
			}
		}
		// What went on air first, then to preview, then off; by source
		// within each.
		rank := func(s int) int {
			l := f.Tally[uint16(s)]
			switch {
			case l.Program:
				return 0
			case l.Preview:
				return 1
			}
			return 2
		}
		slices.SortFunc(srcs, func(a, b int) int {
			if ra, rb := rank(a), rank(b); ra != rb {
				return ra - rb
			}
			return a - b
		})
		var changed []string
		for _, s := range srcs {
			now := f.Tally[uint16(s)]
			if was := v.tally[s]; was == now && (v.tallyKnown || now == atem.TallyLamps{}) {
				continue
			}
			changed = append(changed, v.source(s)+" "+lamps(now))
		}
		v.tally, v.tallyKnown = map[int]atem.TallyLamps{}, true
		for s, l := range f.Tally {
			v.tally[int(s)] = l
		}
		if len(changed) == 0 {
			return "tally  unchanged"
		}
		return "tally  " + strings.Join(changed, ", ")
	case "TrSS":
		var f struct {
			Index int `json:"index"`
			Style int `json:"style"`
		}
		json.Unmarshal(ev.Value, &f)
		v.style[f.Index] = f.Style
	case "TrPs":
		var f atem.TransitionPositionField
		json.Unmarshal(ev.Value, &f)
		me := int(f.Index)
		head := fmt.Sprintf("transition  ME%v  %s", table.Index(me), styleName(v.style[me]))
		start, running := v.started[me]
		switch {
		case f.InTransition && !running:
			v.started[me] = at
			return head + "  running 0.0s"
		case f.InTransition:
			return fmt.Sprintf("%s  running %.1fs", head, elapsed(start, at))
		case running:
			delete(v.started, me)
			return fmt.Sprintf("%s  done %.1fs", head, elapsed(start, at))
		}
		return head + "  idle"
	case "InPr":
		var f struct {
			Index     int    `json:"index"`
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
		}
		json.Unmarshal(ev.Value, &f)
		v.name(f.Index, f.Name, f.ShortName)
	}
	return prettyKey(ev) + "  " + compact(ev.Value)
}

func styleName(s int) string {
	if s >= 0 && s < len(transitionStyles) {
		return transitionStyles[s]
	}
	return fmt.Sprint(s)
}

// elapsed is the seconds from start to at, both times of day.
func elapsed(start, at time.Time) float64 {
	d := at.Sub(start)
	if d < 0 {
		d += 24 * time.Hour // past midnight
	}
	return d.Seconds()
}

func lamps(l atem.TallyLamps) string {
	switch {
	case l.Program && l.Preview:
		return "pgm+pvw"
	case l.Program:
		return "pgm"
	case l.Preview:
		return "pvw"
	}
	return "off"
}

func formatWatch(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		// Watching has begun: what the switcher holds now is what the
		// changes are shown against.
		var st atem.State
		if p.c.ask(p.g, "state", nil, &st) {
			p.watch = newWatchView(&st)
		}
		fmt.Fprintln(p.c.stderr, "watching; Ctrl-C to stop")
		return nil
	}
	if p.watch == nil {
		p.watch = newWatchView(nil)
	}
	return []string{ev.Time + "  " + p.watch.line(ev)}
}
