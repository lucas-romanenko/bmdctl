// SPDX-License-Identifier: MIT

package main

// A HyperDeck's views (docs/PORTING.md, "HyperDeck commands"): the
// header's transport, status, clips and watch, at a terminal and down a
// pipe. Writes are the switcher's writes (screen.go's writeView), from
// the engine's values notes.

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func init() {
	screenFormats["hyperdeck status"] = screenDeckStatus
	screenFormats["hyperdeck clips"] = screenDeckClips
	screenFormats["hyperdeck watch"] = screenDeckWatch
	screenFormats["hyperdeck connect"] = screenConnect
	screenFormats["hyperdeck disconnect"] = screenDisconnect
	screenFormats["hyperdeck state"] = screenState_
	humanFormats["hyperdeck status"] = formatDeckStatus
	humanFormats["hyperdeck clips"] = formatDeckClips
	humanFormats["hyperdeck watch"] = formatDeckWatch
	humanFormats["hyperdeck connect"] = formatConnect
	humanFormats["hyperdeck disconnect"] = formatDisconnect
	humanFormats["hyperdeck state"] = formatState
	for _, p := range []string{"status", "clips", "state"} {
		reports["hyperdeck "+p] = true
	}
}

// formatKey is the key an entry's formats are found by: its path for an
// ATEM's, "hyperdeck status" for another device's.
func formatKey(e *table.Entry) string {
	if e.Device == "atem" {
		return e.Path
	}
	return e.Device + " " + e.Path
}

// deckLiveText is the header's transport: "stopped · clip 2".
func deckLiveText(l *engine.DeckLive) string {
	parts := []string{l.Status}
	if l.Clip > 0 {
		parts = append(parts, fmt.Sprintf("clip %d", l.Clip))
	}
	return strings.Join(nonEmpty(parts...), " · ")
}

// clock is seconds as a deck's time left shows: 2h00m, 45m, 30s.
func clock(s int) string { return ago(time.Duration(s) * time.Second) }

func (p *printer) deckStatusRows(r engine.DeckStatusResult, term bool) [][]string {
	c := p.c
	t := r.Transport
	status := t.Status
	if term && status == "record" {
		status = c.colour(red, status)
	}
	parts := []string{status}
	if t.Speed != 0 && t.Speed != 100 {
		parts = append(parts, fmt.Sprintf("%d%%", t.Speed))
	}
	parts = append(parts, "loop "+onOffText(t.Loop), "single clip "+onOffText(t.SingleClip))
	sep := ", "
	if term {
		sep = " · "
	}
	rows := [][]string{{"transport", strings.Join(parts, sep)}, {"timecode", t.Timecode}}
	switch cl := r.Clip; {
	case cl == nil:
		rows = append(rows, []string{"clip", c.colour(dim, "none")})
	default:
		rows = append(rows, []string{"clip", strings.TrimSpace(fmt.Sprintf("%s  %s  %s",
			c.colour(dim, strconv.Itoa(cl.ID)), cl.Name, c.colour(dim, cl.Duration)))})
	}
	rows = append(rows, []string{"timeline", count(r.Clips, "clip")})
	for _, s := range r.Slots {
		left := ""
		// An empty slot has no time to record into: a terminal leaves its
		// "0s left" out. A pipe keeps it, as it always printed it.
		if s.RemainingS != nil && !(term && s.Status == "empty") {
			left = clock(*s.RemainingS) + " left"
		}
		row := []string{fmt.Sprintf("slot %d", s.Slot), strings.Join(nonEmpty(s.Volume, s.Status, left), sep)}
		if s.Active {
			row = append(row, "active")
		}
		rows = append(rows, row)
	}
	remote := "on"
	if !r.Remote {
		remote = "off: the deck refuses commands"
	}
	return append(rows, []string{"remote", remote})
}

func onOffText(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// screenDeckStatus is a deck's status: the transport, timecode, clip,
// timeline, each slot with the time it can still record (the active one
// marked at the line's end) and remote control.
func screenDeckStatus(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.DeckStatusResult
	json.Unmarshal(raw, &r)
	v := view{noLive: true, model: true, gap: true}
	if p.connectedIn == 0 {
		var s []string
		if r.Session.Since != nil {
			s = append(s, "session "+ago(time.Since(*r.Session.Since)))
		}
		if p.sw != nil {
			s = append(s, "idle "+ago(time.Duration(p.sw.IdleS)*time.Second))
		}
		if r.Session.Pinned {
			s = append(s, "pinned")
		}
		if len(s) > 0 {
			v.about = []string{strings.Join(s, ", ")}
		}
	}
	for _, row := range p.deckStatusRows(r, true) {
		line := labelled(row[0], row[1])
		if len(row) > 2 {
			line = trailing(line, p.c.colour(dim, "● "+row[2]))
		}
		v.rows = append(v.rows, line)
	}
	return v
}

func formatDeckStatus(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.DeckStatusResult
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, row := range p.deckStatusRows(r, false) {
		if len(row) > 2 {
			row = []string{row[0], row[1] + ", " + row[2]}
		}
		rows = append(rows, row)
	}
	rows = append(rows, []string{"session", sessionLine(r.Session)})
	return p.table(rows)
}

// screenDeckClips is the timeline's clips (or a disk's): the number dim,
// the name, the duration dim; the clip the transport is on ends with its
// status.
func screenDeckClips(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.DeckClips
	json.Unmarshal(raw, &r)
	c := p.c
	v := view{gap: true}
	what := "timeline"
	if r.Source == "disk" {
		what = fmt.Sprintf("slot %d's disk", r.Slot)
	}
	v.about = []string{what + " · " + count(len(r.Clips), "clip")}
	if len(r.Clips) == 0 {
		v.rows = []string{indent + c.colour(dim, "(empty)")}
		return v
	}
	idWidth := len(strconv.Itoa(r.Clips[len(r.Clips)-1].ID))
	var cells [][]string
	for _, cl := range r.Clips {
		row := []string{c.colour(dim, padLeft(strconv.Itoa(cl.ID), idWidth)), cl.Name}
		if r.Source == "disk" {
			row = append(row, c.colour(dim, strings.Join(nonEmpty(deref(cl.FileFormat), deref(cl.VideoFormat)), " ")))
		}
		cells = append(cells, append(row, c.colour(dim, cl.Duration)))
	}
	for i, line := range rows(cells) {
		if cl := r.Clips[i]; r.Source == "timeline" && cl.ID == r.Current {
			status := r.Status
			if status == "record" {
				status = c.colour(red, status)
			}
			line = trailing(line, "● "+status)
		}
		v.rows = append(v.rows, line)
	}
	return v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatDeckClips(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.DeckClips
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, cl := range r.Clips {
		row := []string{strconv.Itoa(cl.ID), cl.Name}
		if r.Source == "disk" {
			row = append(row, deref(cl.FileFormat), deref(cl.VideoFormat))
		} else {
			row = append(row, deref(cl.Start))
		}
		row = append(row, cl.Duration)
		if r.Source == "timeline" && cl.ID == r.Current {
			row = append(row, r.Status)
		}
		rows = append(rows, row)
	}
	return p.table(rows)
}

// deckWatch is what a deck's watch remembers to show a change against:
// each block's fields as the last snapshot or change left them, and the
// timeline's names.
type deckWatch struct {
	blocks map[string]map[string]string // transport, slot:1, remote, configuration
	names  map[string]string            // clip id: name
}

// startDeckWatch takes the deck's state once watching has begun.
func (p *printer) startDeckWatch() {
	w := &deckWatch{blocks: map[string]map[string]string{}, names: map[string]string{}}
	var st struct {
		Transport     map[string]string            `json:"transport"`
		Slots         map[string]map[string]string `json:"slots"`
		Remote        map[string]string            `json:"remote"`
		Configuration map[string]string            `json:"configuration"`
		Timeline      []struct {
			ID   int    `json:"clip_id"`
			Name string `json:"name"`
		} `json:"timeline"`
	}
	if p.c.askEngine(engine.Request{Device: "hyperdeck", Host: p.t.Address, Path: "state"}, &st) {
		w.blocks["transport"], w.blocks["remote"], w.blocks["configuration"] = st.Transport, st.Remote, st.Configuration
		for id, s := range st.Slots {
			w.blocks["slot:"+id] = s
		}
		for _, cl := range st.Timeline {
			w.names[strconv.Itoa(cl.ID)] = cl.Name
		}
	}
	p.deckWatch = w
}

// line is one notification as a watch line: its block, then each field
// it carried as before → after (the transport's status and speed as one,
// a clip by its name).
func (w *deckWatch) line(ev watchEvent) watchLine {
	if ev.Kind == "disconnected" {
		return watchLine{verb: "session dropped", own: true}
	}
	var vals map[string]string
	json.Unmarshal(ev.Value, &vals)
	old := w.blocks[ev.Key]
	if old == nil {
		old = map[string]string{}
	}
	now := maps.Clone(old)
	maps.Copy(now, vals)
	w.blocks[ev.Key] = now
	verb := strings.Replace(ev.Key, ":", " ", 1)
	var changes []string
	keys := slices.Sorted(maps.Keys(vals))
	if ev.Key == "transport" {
		if _, ok := vals["status"]; ok || vals["speed"] != "" {
			changes = append(changes, arrow(statusText(old), statusText(now)))
		}
		if v, ok := vals["clip id"]; ok {
			verb = "clip"
			changes = append(changes, arrow(w.clip(old["clip id"]), w.clip(v)))
		}
		if _, ok := vals["slot id"]; ok {
			changes = append(changes, "slot "+arrow(old["slot id"], now["slot id"]))
		}
	}
	for _, k := range keys {
		if ev.Key == "transport" && (k == "status" || k == "speed" || k == "clip id" || k == "slot id") {
			continue
		}
		changes = append(changes, k+" "+arrow(old[k], vals[k]))
	}
	return watchLine{verb: verb, change: strings.Join(changes, ", ")}
}

func (w *deckWatch) clip(id string) string {
	if n := w.names[id]; n != "" {
		return n
	}
	if id == "" || id == "none" {
		return "none"
	}
	return "clip " + id
}

func statusText(m map[string]string) string {
	// The speed as the engine's transportText shows it in a result: for
	// any status but stopped and record. A deck reports slow play as
	// forward (speed 50), which said only "forward" here.
	s := m["status"]
	if sp := m["speed"]; s != "" && s != "stopped" && s != "record" && sp != "" && sp != "100" && sp != "0" {
		s += " " + sp + "%"
	}
	return s
}

// arrow is before → after, or after alone where they agree or before is
// unknown.
func arrow(before, after string) string {
	if before == "" || before == after {
		return after
	}
	return before + " → " + after
}

func formatDeckWatch(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		p.startDeckWatch()
		fmt.Fprintln(p.c.stderr, p.c.colourErr(dim, "watching; Ctrl-C to stop"))
		return []string{p.header()}
	}
	if p.deckWatch == nil {
		p.startDeckWatch()
	}
	return []string{p.c.colour(dim, ev.Time) + "  " + p.deckWatch.line(ev).text()}
}

func screenDeckWatch(p *printer, _ engine.Response, raw json.RawMessage) view {
	var ev watchEvent
	json.Unmarshal(raw, &ev)
	if ev.Kind == "watching" {
		p.startDeckWatch()
		return view{about: []string{"watching · ctrl-c to stop"}}
	}
	if p.deckWatch == nil {
		p.startDeckWatch()
	}
	clock, _, _ := strings.Cut(ev.Time, ".")
	l := p.deckWatch.line(ev)
	if ev.Key == "transport" && strings.HasSuffix(l.change, "record") {
		l.change = strings.TrimSuffix(l.change, "record") + p.c.colour(red, "record")
	}
	return view{rows: []string{indent + p.c.colour(dim, clock) + "  " + l.screen(p.c.colour)}}
}
