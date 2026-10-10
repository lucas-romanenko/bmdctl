// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

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
			mark = p.c.colour(green, "current")
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
		lines = append(lines, fmt.Sprintf("saved %s: %s %s, %d bytes", file, pr.Product, pr.VideoMode, pr.Bytes))
	case "profile restore":
		status := p.c.colour(green, pr.Summary)
		if len(pr.Errors) > 0 {
			status = p.c.colour(red, pr.Summary)
		}
		lines = append(lines, fmt.Sprintf("restored %s (%s): %s", p.params["file"], strings.TrimSpace(pr.Product+" "+pr.VideoMode), status))
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
			rows = append(rows, []string{p.c.colour(yellow, "image"), what, im.Name, im.Error})
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

// formatInputs is one source per line: id, short name, long name, port
// type.
func formatInputs(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ins []engine.InputEntry
	json.Unmarshal(raw, &ins)
	rows := [][]string{}
	for _, in := range ins {
		rows = append(rows, []string{fmt.Sprint(in.Source), in.Short, in.Long, in.Type})
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
