// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	humanFormats["inputs"] = formatInputs
	humanFormats["audio meters"] = formatMeters
	humanFormats["classic-audio meters"] = formatMeters
	humanFormats["media list"] = formatMediaList
	humanFormats["media upload"] = formatTransfer
	humanFormats["media download"] = formatTransfer
	humanFormats["media clear"] = formatTransfer
	humanFormats["macro list"] = formatMacroList
	humanFormats["macro download"] = formatTransfer
	humanFormats["macro upload"] = formatTransfer
	humanFormats["macro decode"] = formatMacroDecode
	humanFormats["macro encode"] = formatMacroEncode
	humanFormats["profile save"] = formatProfile
	humanFormats["profile restore"] = formatProfile
	humanFormats["profile images"] = formatProfile
	humanFormats["time"] = formatTime
	humanFormats["video-modes"] = formatVideoModes
}

func formatTime(p *printer, r engine.Response, raw json.RawMessage) []string {
	var t engine.TimeResult
	json.Unmarshal(raw, &t)
	line := "time  " + t.Timecode
	if t.Mode != "" {
		line += "  " + t.Mode
	}
	return []string{line}
}

func formatVideoModes(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ms []engine.VideoModeEntry
	json.Unmarshal(raw, &ms)
	if len(ms) == 0 {
		return []string{"the switcher has not listed its video modes"}
	}
	var rows [][]string
	for _, m := range ms {
		mark := ""
		if m.Current {
			mark = p.c.colour(bold, "current")
		}
		rows = append(rows, []string{fmt.Sprint(m.ID), m.Name, mark})
	}
	return p.table(rows)
}

// formatProfile is a line for the profile, then one per error, section
// skipped and image.
func formatProfile(p *printer, r engine.Response, raw json.RawMessage) []string {
	var pr engine.ProfileResult
	json.Unmarshal(raw, &pr)
	var lines []string
	switch p.e.Path {
	case "profile save":
		file := p.params["file"]
		lines = append(lines, fmt.Sprintf("%s  saved %s: %s %s, %d bytes", p.whoOut(), file, pr.Product, pr.VideoMode, pr.Bytes))
	case "profile restore":
		status := pr.Summary
		if len(pr.Errors) > 0 {
			status = p.c.colour(red, pr.Summary)
		}
		lines = append(lines, fmt.Sprintf("%s  restored %s (%s): %s", p.whoOut(), p.params["file"], strings.TrimSpace(pr.Product+" "+pr.VideoMode), status))
	case "profile images":
		lines = append(lines, fmt.Sprintf("%s  saved the media pool's stills in %s", p.whoOut(), p.params["dir"]))
	}
	var rows [][]string
	for _, e := range pr.Errors {
		rows = append(rows, []string{p.c.colour(red, "error"), e})
	}
	for _, s := range pr.Skipped {
		rows = append(rows, []string{"skipped", s})
	}
	for _, im := range pr.Images {
		what := fmt.Sprintf("still %d", im.Slot)
		if im.Error != "" {
			rows = append(rows, []string{p.c.colour(red, "image"), what, im.Name, im.Error})
		} else {
			rows = append(rows, []string{"image", what, im.Name, im.Path})
		}
	}
	return append(lines, p.table(rows)...)
}

func formatMacroList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MacroList
	json.Unmarshal(raw, &r)
	rows := [][]string{}
	for _, m := range r.Macros {
		state := ""
		switch {
		case r.Running != nil && m.Slot == *r.Running:
			state = p.c.colour(red, "running")
		case r.Recording != nil && m.Slot == *r.Recording:
			state = p.c.colour(red, "recording")
		case !m.Used:
			state = p.c.colour(dim, "-")
		case m.Invalid:
			state = "invalid"
		}
		rows = append(rows, []string{fmt.Sprintf("macro %d", m.Slot), m.Name, state, m.Description})
	}
	if len(rows) == 0 {
		return []string{"no macros"}
	}
	return p.table(p.c.dimLabels(rows))
}

func formatMacroDecode(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MacroResult
	json.Unmarshal(raw, &r)
	if r.Warning != "" {
		fmt.Fprintf(p.c.stderr, "bmdctl %s: %s\n", p.label(), r.Warning)
	}
	return strings.Split(strings.TrimRight(r.XML, "\n"), "\n")
}

func formatMacroEncode(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MacroResult
	json.Unmarshal(raw, &r)
	return []string{fmt.Sprintf("encoded %s, %d bytes, to %s", r.Source, r.Bytes, strings.Join(p.written, ", "))}
}

// formatMediaList is a line per still slot, then per media player: a
// player on program red, on preview green, by the switcher's tally.
func formatMediaList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MediaList
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, s := range r.Stills {
		if s.Used {
			rows = append(rows, []string{fmt.Sprintf("still %d", s.Slot), s.Name, s.Hash})
		} else {
			rows = append(rows, []string{fmt.Sprintf("still %d", s.Slot), p.c.colour(dim, "-")})
		}
	}
	var tally map[int]atem.TallyLamps
	if len(r.Players) > 0 {
		tally = p.tally()
	}
	for _, m := range r.Players {
		// Media player n is source 3000 + 10n, its key the next one.
		code := tallyColour(tally[3000+10*int(m.Player+1)])
		rows = append(rows, []string{fmt.Sprintf("player %d", m.Player), paintIf(p.c.colour, code, fmt.Sprintf("%s %d", m.Type, m.Slot)),
			paintIf(p.c.colour, code, m.Name)})
	}
	return p.table(p.c.dimLabels(rows))
}

// formatTransfer is a transfer's line, laid out as a write's: the
// switcher, the command and its slot, what moved where, the status.
func formatTransfer(p *printer, r engine.Response, raw json.RawMessage) []string {
	var t engine.TransferResult
	json.Unmarshal(raw, &t)
	change := ""
	if t.Name != "" {
		change = strconv.Quote(t.Name)
	}
	add := func(sep, s string) {
		if change != "" {
			s = sep + s
		}
		change += s
	}
	switch p.e.Path {
	case "media upload", "macro upload":
		if t.Source != "" {
			add(" ", "from "+t.Source)
		}
		if t.Replaced != "" {
			add(", ", fmt.Sprintf("replacing %q", t.Replaced))
		}
	case "media download", "macro download":
		if len(p.written) > 0 {
			add(" ", "to "+strings.Join(p.written, ", "))
		}
		if t.Width > 0 {
			add(", ", fmt.Sprintf("%dx%d", t.Width, t.Height))
		}
	case "media clear":
		if t.Replaced != "" {
			add(", ", fmt.Sprintf("was %q", t.Replaced))
		}
	}
	return []string{p.writeLine(fmt.Sprintf("%s %d", p.e.Path, t.Slot), "", change, r.Confirmed, r.TookMS)}
}

// formatInputs is one source per line: id, short name, long name, port
// type; the names of a source on program red, on preview green, by the
// switcher's tally.
func formatInputs(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ins []engine.InputEntry
	json.Unmarshal(raw, &ins)
	tally := p.tally()
	rows := [][]string{}
	for _, in := range ins {
		code := tallyColour(tally[in.Source])
		rows = append(rows, []string{fmt.Sprint(in.Source), paintIf(p.c.colour, code, in.Short), paintIf(p.c.colour, code, in.Long), in.Type})
	}
	return p.table(rows)
}

// tally is the switcher's tally by source (TlSr), for colouring a list
// as the lamps are: asked of the engine only when stdout takes colour,
// and nil when it can't say.
func (p *printer) tally() map[int]atem.TallyLamps {
	if !p.c.colours(p.c.tty) {
		return nil
	}
	var f atem.TallySourceField
	if !p.c.ask(p.g, "state", map[string]string{"path": "tally.source"}, &f) {
		return nil
	}
	out := map[int]atem.TallyLamps{}
	for src, l := range f.Tally {
		out[int(src)] = l
	}
	return out
}

func formatMeters(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MetersResult
	json.Unmarshal(raw, &r)
	row := func(m engine.Meter) []string {
		cells := []string{m.Strip, m.Name, fmt.Sprintf("%6.1f %6.1f dB", m.Level[0], m.Level[1]),
			fmt.Sprintf("peak %6.1f %6.1f", m.Peak[0], m.Peak[1])}
		var gr []string
		for _, g := range []struct {
			name string
			v    *float64
		}{{"exp", m.ExpanderGR}, {"comp", m.CompressorGR}, {"lim", m.LimiterGR}} {
			if g.v != nil && *g.v != 0 {
				gr = append(gr, fmt.Sprintf("%s %.1f", g.name, *g.v))
			}
		}
		if len(gr) > 0 {
			cells = append(cells, "gr "+strings.Join(gr, " "))
		}
		return cells
	}
	var rows [][]string
	if r.Master != nil {
		rows = append(rows, row(*r.Master))
	}
	if r.Monitor != nil {
		rows = append(rows, row(*r.Monitor))
	}
	for _, m := range r.Strips {
		rows = append(rows, row(m))
	}
	if len(rows) == 0 {
		return []string{fmt.Sprintf("no %s meter levels arrived in %vs", r.Mixer, r.Seconds)}
	}
	return p.table(rows)
}
