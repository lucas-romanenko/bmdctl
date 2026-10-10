// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	humanFormats["status"] = formatStatus
	humanFormats["connect"] = formatConnect
	humanFormats["disconnect"] = formatDisconnect
	humanFormats["info"] = formatInfo
	humanFormats["watch"] = formatWatch
}

// source is "1 (Camera 1)", or the id alone for a source without a name.
func source(s engine.Source) string {
	if s.Name == "" {
		return fmt.Sprint(s.ID)
	}
	return fmt.Sprintf("%d (%s)", s.ID, s.Name)
}

func onAir(c *cli, on bool) string {
	if on {
		return c.colour(red, "on air")
	}
	return "off air"
}

func sessionLine(s engine.SessionStatus) string {
	state := "connecting"
	switch {
	case s.Ready:
		state = "ready"
	case s.Connected:
		state = "reconnecting"
	}
	switch {
	case s.Pinned:
		state += ", pinned"
	case s.IdleCloseS > 0:
		state += fmt.Sprintf(", closes after %s idle", (time.Duration(s.IdleCloseS) * time.Second).String())
	}
	return state
}

func formatStatus(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.StatusResult
	json.Unmarshal(raw, &r)
	c := p.c
	rows := [][]string{
		{"session", r.Session.Host, sessionLine(r.Session)},
		{"switcher", r.Product, r.VideoMode},
	}
	for _, m := range r.MEs {
		me := fmt.Sprintf("me %d", m.ME)
		rows = append(rows, []string{me, "program " + c.colour(red, source(m.Program)), "preview " + c.colour(green, source(m.Preview))})
		tr := fmt.Sprintf("%s %s, next %s", m.Transition.Style, m.Transition.RateText, strings.Join(m.Transition.Next, "+"))
		state := "idle"
		if m.Transition.InTransition {
			state = fmt.Sprintf("in transition, %d%%", m.Transition.Position/100)
		}
		if m.Transition.Preview {
			state += ", previewing"
		}
		rows = append(rows, []string{me, "transition " + tr, state})
		ftb := "ftb off"
		switch {
		case m.FTB.InTransition:
			ftb = "ftb fading"
		case m.FTB.Active:
			ftb = c.colour(red, "ftb black")
		}
		rows = append(rows, []string{me, ftb, fmt.Sprintf("rate %d frames", m.FTB.Rate)})
		for _, k := range m.Keyers {
			rows = append(rows, []string{fmt.Sprintf("%s key %d", me, k.Keyer), onAir(c, k.OnAir),
				fmt.Sprintf("%s, fill %s, key %s", k.Type, source(k.Fill), source(k.Key))})
		}
	}
	for _, d := range r.DSKs {
		state := onAir(c, d.OnAir)
		if d.Tie {
			state += ", tied"
		}
		rows = append(rows, []string{fmt.Sprintf("dsk %d", d.DSK), state, fmt.Sprintf("fill %s, key %s", source(d.Fill), source(d.Key))})
	}
	for _, a := range r.Aux {
		rows = append(rows, []string{fmt.Sprintf("aux %d", a.Aux), source(a.Source)})
	}
	macro := "idle"
	switch {
	case r.Macro.Running:
		macro = "running"
		if r.Macro.Slot != nil {
			macro += fmt.Sprintf(" %d %s", *r.Macro.Slot, r.Macro.Name)
		}
		if r.Macro.Waiting {
			macro += ", waiting"
		}
	case r.Macro.Recording && r.Macro.RecSlot != nil:
		macro = fmt.Sprintf("recording %d", *r.Macro.RecSlot)
	}
	rows = append(rows, []string{"macro", strings.TrimSpace(macro)})
	if r.Recording != nil {
		rows = append(rows, []string{"recording", map[bool]string{true: c.colour(red, "recording"), false: "stopped"}[*r.Recording]})
	}
	if r.Streaming != "" {
		rows = append(rows, []string{"streaming", r.Streaming})
	}
	return p.table(rows)
}

func formatConnect(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var s engine.SessionStatus
	json.Unmarshal(raw, &s)
	return p.table([][]string{{"connected", s.Host, sessionLine(s), strings.TrimSpace(s.Product + " " + s.VideoMode)}})
}

func formatDisconnect(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.DisconnectResult
	json.Unmarshal(raw, &r)
	if !r.Closed {
		return []string{"no session to " + r.Host}
	}
	return []string{"disconnected " + r.Host}
}

func formatInfo(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.InfoResult
	json.Unmarshal(raw, &r)
	keyers := make([]string, len(r.Keyers))
	for i, k := range r.Keyers {
		keyers[i] = fmt.Sprint(k)
	}
	rows := [][]string{
		{"product", fmt.Sprintf("%s (model %d)", r.Product, r.Model)},
	}
	if r.Name != "" {
		rows = append(rows, []string{"name", r.Name})
	}
	rows = append(rows,
		[]string{"protocol", r.Protocol},
		[]string{"video mode", fmt.Sprintf("%s (mode %d), %dx%d, rates counted at %d fps", r.VideoMode, r.VideoModeID, r.Resolution[0], r.Resolution[1], r.DisplayFPS)},
		[]string{"m/es", fmt.Sprintf("%d, upstream keyers %s", r.MEs, strings.Join(keyers, ", "))},
		[]string{"dsks", fmt.Sprint(r.DSKs)},
		[]string{"aux", fmt.Sprint(r.Aux)},
		[]string{"media", fmt.Sprintf("%d players, %d stills, %d clips", r.MediaPlayers, r.Stills, r.Clips)},
		[]string{"macros", fmt.Sprint(r.Macros)},
		[]string{"supersources", fmt.Sprint(r.SuperSources)},
		[]string{"multiviewers", fmt.Sprint(r.Multiviewers)},
		[]string{"hyperdecks", fmt.Sprint(r.HyperDecks)},
		[]string{"inputs", fmt.Sprint(r.Inputs)},
		[]string{"audio", r.Audio},
	)
	return p.table(rows)
}

func formatWatch(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ev struct {
		Kind  string          `json:"kind"`
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
		Time  string          `json:"time"`
	}
	json.Unmarshal(raw, &ev)
	switch ev.Kind {
	case "watching":
		fmt.Fprintln(p.c.stderr, "watching; Ctrl-C to stop")
		return nil
	case "state":
		return []string{fmt.Sprintf("%s  %-12s %s", ev.Time, p.c.colour(bold, ev.Key), compact(ev.Value))}
	}
	return []string{fmt.Sprintf("%s  %s", ev.Time, ev.Kind)}
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}
