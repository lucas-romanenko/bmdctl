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
	"maps"
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
	who        string                      // the switcher, for the line saying its session dropped
	paint      func(code, s string) string // program red, preview green, the session's own lines bold
	names      map[int]string              // source: short name, else long name
	program    map[int]int                 // M/E: source
	preview    map[int]int
	tally      map[int]atem.TallyLamps // source: lamps
	tallyKnown bool
	style      map[int]int       // M/E: transition style
	started    map[int]time.Time // M/E: when its transition began
	// away: the session dropped. The switcher's state dump when it comes
	// back isn't news; it updates the view quietly, and program and
	// preview as they were when it dropped show what changed meanwhile.
	away               bool
	awayProg, awayPrev map[int]int
	// term: lines for a terminal (screen.go), where keyers and DSKs
	// going on and off air get lines of their own.
	term bool
	dsk  map[int]bool // DSK: on air, as the last DskS said
}

// watchLine is one change as a line: what changed (verb), on which M/E
// (where), and how; own marks bmdctl's own lines (the session dropping,
// coming back), bold.
type watchLine struct {
	verb, where, change string
	own                 bool
}

// text is the line as a pipe has it: its parts two spaces apart.
func (l watchLine) text() string { return strings.Join(nonEmpty(l.verb, l.where, l.change), "  ") }

// screen is the line as a terminal has it: the verb in its column (with
// the M/E where it isn't the first), then the change.
func (l watchLine) screen(paint func(code, s string) string) string {
	if l.own {
		return strings.TrimSpace(paint(bold, l.verb) + "  " + paint(dim, l.change))
	}
	verb := l.verb
	if l.where != "" && l.where != "ME1" {
		verb += " " + l.where
	}
	return strings.TrimRight(verbCell(verb)+l.change, " ")
}

var transitionStyles = []string{"mix", "dip", "wipe", "dve", "stinger"}

// newWatchView starts from a snapshot of the switcher's state; nil
// starts from nothing.
func newWatchView(st *atem.State) *watchView {
	v := &watchView{names: map[int]string{}, program: map[int]int{}, preview: map[int]int{},
		tally: map[int]atem.TallyLamps{}, style: map[int]int{}, started: map[int]time.Time{}, dsk: map[int]bool{},
		paint: func(_, s string) string { return s }}
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
	for d, k := range st.DSKs {
		if k.State != nil {
			v.dsk[d] = k.State.OnAir
		}
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
func (v *watchView) line(ev watchEvent) watchLine {
	switch ev.Kind {
	case "disconnected":
		who := v.who
		if who == "" {
			who = "the switcher"
		}
		if v.term {
			return watchLine{verb: "session dropped", own: true}
		}
		return watchLine{verb: v.paint(bold, "session to "+who+" dropped")}
	case "ready":
		if v.term {
			return watchLine{verb: "reconnected", own: true}
		}
		return watchLine{verb: v.paint(bold, "reconnected")}
	case "state":
	default:
		return watchLine{verb: ev.Kind}
	}
	at, _ := time.Parse("15:04:05.000", ev.Time)
	switch ev.Opcode {
	case "PrgI", "PrvI":
		var f struct {
			Index  int `json:"index"`
			Source int `json:"source"`
		}
		json.Unmarshal(ev.Value, &f)
		label, last, colour := "program", v.program, red
		if ev.Opcode == "PrvI" {
			label, last, colour = "preview", v.preview, green
		}
		was, known := last[f.Index]
		last[f.Index] = f.Source
		l := watchLine{verb: label, where: fmt.Sprintf("ME%v", table.Index(f.Index)), change: v.paint(colour, v.source(f.Source))}
		if known && was != f.Source {
			l.change = v.source(was) + " → " + l.change
		}
		return l
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
			entry := v.source(s) + " " + lamps(now)
			switch {
			case now.Program:
				entry = v.paint(red, entry)
			case now.Preview:
				entry = v.paint(green, entry)
			}
			changed = append(changed, entry)
		}
		v.tally, v.tallyKnown = map[int]atem.TallyLamps{}, true
		for s, l := range f.Tally {
			v.tally[int(s)] = l
		}
		if len(changed) == 0 {
			return watchLine{verb: "tally", change: "unchanged"}
		}
		return watchLine{verb: "tally", change: strings.Join(changed, ", ")}
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
		l := watchLine{verb: "transition", where: fmt.Sprintf("ME%v", table.Index(me)), change: styleName(v.style[me])}
		start, running := v.started[me]
		switch {
		case f.InTransition && !running:
			v.started[me] = at
			l.change += "  running 0.0s"
		case f.InTransition:
			l.change += fmt.Sprintf("  running %.1fs", elapsed(start, at))
		case running:
			delete(v.started, me)
			l.change += fmt.Sprintf("  done %.1fs", elapsed(start, at))
		default:
			l.change += "  idle"
		}
		return l
	case "InPr":
		var f struct {
			Index     int    `json:"index"`
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
		}
		json.Unmarshal(ev.Value, &f)
		v.name(f.Index, f.Name, f.ShortName)
		if v.term {
			return watchLine{verb: fmt.Sprintf("input %d", f.Index), change: strings.Join(nonEmpty(f.ShortName, f.Name), " · ")}
		}
	case "KeOn":
		if v.term {
			var f atem.KeyOnAirField
			json.Unmarshal(ev.Value, &f)
			return watchLine{verb: fmt.Sprintf("key %v", table.Index(f.Keyer)), where: fmt.Sprintf("ME%v", table.Index(f.Index)),
				change: v.onAir(f.Enabled)}
		}
	case "DskS":
		var f atem.DkeyStateField
		json.Unmarshal(ev.Value, &f)
		was, known := v.dsk[int(f.Index)]
		v.dsk[int(f.Index)] = f.OnAir
		if v.term {
			if known && was == f.OnAir {
				return watchLine{} // a DSK fading says so each frame
			}
			return watchLine{verb: fmt.Sprintf("dsk %v", table.Index(f.Index)), change: v.onAir(f.OnAir)}
		}
	}
	return watchLine{verb: prettyKey(ev), change: compact(ev.Value)}
}

// onAir is a keyer's state: on air red, as a tally lamp.
func (v *watchView) onAir(on bool) string {
	if on {
		return v.paint(red, "on air")
	}
	return "off air"
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

// dropped marks the session gone: the view holds what it last knew.
func (v *watchView) dropped() {
	v.away, v.awayProg, v.awayPrev = true, maps.Clone(v.program), maps.Clone(v.preview)
}

// back is what to say when the session is back: that it is, then each
// program and preview that changed while it was away.
func (v *watchView) back() []watchLine {
	v.away = false
	out := []watchLine{v.line(watchEvent{Kind: "ready"})}
	for _, bus := range []struct {
		label    string
		was, now map[int]int
		colour   string
	}{{"program", v.awayProg, v.program, red}, {"preview", v.awayPrev, v.preview, green}} {
		for _, me := range slices.Sorted(maps.Keys(bus.now)) {
			if was, ok := bus.was[me]; ok && was != bus.now[me] {
				out = append(out, watchLine{verb: bus.label, where: fmt.Sprintf("ME%v", table.Index(me)),
					change: v.source(was) + " → " + v.paint(bus.colour, v.source(bus.now[me]))})
			}
		}
	}
	if v.term && len(out) == 1 {
		out[0].change = "program and preview unchanged"
	}
	return out
}

func formatWatch(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		// Watching has begun: what the switcher holds now is what the
		// changes are shown against.
		p.startWatch()
		fmt.Fprintln(p.c.stderr, p.c.colourErr(dim, "watching; Ctrl-C to stop"))
		return []string{p.header()}
	}
	var lines []string
	for _, l := range p.watchLines(ev) {
		lines = append(lines, p.c.colour(dim, ev.Time)+"  "+l.text())
	}
	return lines
}

// screenWatch is watch at a terminal: the header says it watches, then
// a line per change, its time of day dim.
func screenWatch(p *printer, _ engine.Response, raw json.RawMessage) view {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		p.startWatch()
		return view{about: []string{"watching · ctrl-c to stop"}}
	}
	clock, _, _ := strings.Cut(ev.Time, ".")
	var out []string
	for _, l := range p.watchLines(ev) {
		if l != (watchLine{}) {
			out = append(out, indent+p.c.colour(dim, clock)+"  "+l.screen(p.c.colour))
		}
	}
	return view{rows: out}
}

// startWatch takes the switcher's state once watching has begun: the
// changes are shown against it.
func (p *printer) startWatch() {
	var st atem.State
	if p.c.ask(p.g, "state", nil, &st) {
		p.watch = newWatchView(&st)
	}
}

// watchLines are the lines a change makes: one, none while the dump that
// follows a reconnection comes in, or what changed meanwhile once it has.
func (p *printer) watchLines(ev watchEvent) []watchLine {
	if p.watch == nil {
		p.watch = newWatchView(nil)
	}
	v := p.watch
	v.who, v.paint, v.term = p.who(), p.c.colour, p.c.terminal()
	switch {
	case ev.Kind == "disconnected":
		l := v.line(ev)
		v.dropped()
		return []watchLine{l}
	case ev.Kind == "ready" && v.away:
		return v.back()
	case v.away:
		v.line(ev) // the dump coming back: the view follows it, quietly
		return nil
	}
	return []watchLine{v.line(ev)}
}
