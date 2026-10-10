// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	humanFormats["input list"] = formatInputList
	humanFormats["audio meters"] = formatMeters
	humanFormats["media list"] = formatMediaList
	humanFormats["media upload"] = formatTransfer
	humanFormats["media download"] = formatTransfer
	humanFormats["media clear"] = formatTransfer
	humanFormats["macro list"] = formatMacroList
	humanFormats["macro download"] = formatTransfer
	humanFormats["macro upload"] = formatTransfer
	humanFormats["macro decode"] = formatMacroDecode
	humanFormats["macro encode"] = formatMacroEncode
}

func formatMacroList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MacroList
	json.Unmarshal(raw, &r)
	rows := [][]string{}
	for _, m := range r.Macros {
		state := ""
		switch {
		case m.Slot == r.Running:
			state = p.c.colour(red, "running")
		case m.Slot == r.Recording:
			state = p.c.colour(red, "recording")
		case !m.Used:
			state = "-"
		case m.Invalid:
			state = "invalid"
		}
		rows = append(rows, []string{fmt.Sprintf("macro %d", m.Slot), m.Name, state, m.Description})
	}
	if len(rows) == 0 {
		return []string{"no macros"}
	}
	return p.table(rows)
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

func formatMediaList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.MediaList
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, s := range r.Stills {
		if s.Used {
			rows = append(rows, []string{fmt.Sprintf("still %d", s.Slot), s.Name, s.Hash})
		} else {
			rows = append(rows, []string{fmt.Sprintf("still %d", s.Slot), "-"})
		}
	}
	for _, m := range r.Players {
		rows = append(rows, []string{fmt.Sprintf("player %d", m.Player), fmt.Sprintf("%s %d", m.Type, m.Slot), m.Name})
	}
	return p.table(rows)
}

// formatTransfer is a transfer's line: what moved where.
func formatTransfer(p *printer, r engine.Response, raw json.RawMessage) []string {
	var t engine.TransferResult
	json.Unmarshal(raw, &t)
	status := p.c.colour(green, padRight("confirmed", 9))
	if !r.Confirmed {
		status = p.c.colour(yellow, padRight("sent", 9))
	}
	what := fmt.Sprintf("slot %d", t.Slot)
	if t.Name != "" {
		what += fmt.Sprintf(" %q", t.Name)
	}
	switch p.e.Path {
	case "media upload", "macro upload":
		if t.Source != "" {
			what += fmt.Sprintf(" from %s", t.Source)
		}
		if t.Replaced != "" {
			what += fmt.Sprintf(", replacing %q", t.Replaced)
		}
	case "media download", "macro download":
		if len(p.written) > 0 {
			what += " to " + strings.Join(p.written, ", ")
		}
		if t.Width > 0 {
			what += fmt.Sprintf(", %dx%d", t.Width, t.Height)
		}
	case "media clear":
		if t.Replaced != "" {
			what += fmt.Sprintf(", was %q", t.Replaced)
		}
	}
	return []string{fmt.Sprintf("%-24s %s %5d ms  %s", p.e.Path, status, r.TookMS, what)}
}

func formatInputList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ins []engine.InputEntry
	json.Unmarshal(raw, &ins)
	rows := [][]string{}
	for _, in := range ins {
		rows = append(rows, []string{fmt.Sprint(in.Source), in.Long, in.Short, in.Type})
	}
	return p.table(rows)
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
