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
	humanFormats["state"] = formatState
}

// source is "1 (Camera 1)", or the id alone for a source without a name.
func source(s engine.Source) string {
	if s.Name == "" {
		return fmt.Sprint(s.ID)
	}
	return fmt.Sprintf("%d (%s)", s.ID, s.Name)
}

// onAir is a keyer's state, red when on air.
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

// sessionUse is how a request's session stands, from the engine's note:
// how long it had gone unused (or that this request opened it), and when
// it drops if nothing uses it.
func (p *printer) sessionUse() string {
	sw := p.sw
	if sw == nil {
		return ""
	}
	parts := []string{"ready"}
	if p.connected {
		parts = append(parts, "connected just now")
	} else {
		parts = append(parts, "idle "+since(time.Duration(sw.IdleS)*time.Second))
	}
	if sw.Pinned {
		parts = append(parts, "pinned: open until disconnect")
	} else if sw.DropS > 0 {
		at := time.Now().Add(time.Duration(sw.DropS) * time.Second)
		parts = append(parts, fmt.Sprintf("drops at %s if unused", at.Format("15:04")))
	}
	return strings.Join(parts, ", ")
}

func formatStatus(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.StatusResult
	json.Unmarshal(raw, &r)
	c := p.c
	use := p.sessionUse()
	if use == "" {
		use = sessionLine(r.Session)
	}
	rows := [][]string{{"session", use}}
	if st := p.c.runningEngine(); st != nil && st.Commit != "" {
		rows = append(rows, []string{"engine", "commit " + st.Commit})
	}
	for _, m := range r.MEs {
		me := fmt.Sprintf("me %d", m.ME)
		rows = append(rows, []string{me, c.colour(dim, "program") + " " + c.colour(red, source(m.Program)),
			c.colour(dim, "preview") + " " + c.colour(green, source(m.Preview))})
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
	return p.table(c.dimLabels(rows))
}

func formatConnect(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var s engine.SessionStatus
	json.Unmarshal(raw, &s)
	state := "connected"
	switch {
	case s.Pinned:
		state += ", pinned: open until disconnect"
	case s.IdleCloseS > 0:
		at := time.Now().Add(time.Duration(s.IdleCloseS) * time.Second)
		state += fmt.Sprintf(", drops at %s if unused", at.Format("15:04"))
	}
	return []string{p.whoOut() + "  " + state}
}

func formatDisconnect(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.DisconnectResult
	json.Unmarshal(raw, &r)
	if !r.Closed {
		return []string{p.whoOut() + "  no session to close"}
	}
	return []string{p.whoOut() + "  disconnected"}
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
	return p.table(p.c.dimLabels(rows))
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

func init() { humanFormats["undo"] = formatUndo }

// formatUndo is undo's line, as a write's: or that there was nothing to
// undo.
func formatUndo(p *printer, r engine.Response, raw json.RawMessage) []string {
	var u engine.UndoResult
	json.Unmarshal(raw, &u)
	if !u.Undone {
		return []string{p.whoOut() + "  nothing to undo"}
	}
	return []string{p.writeLine("undo", u.What, p.undoChange(u), r.Confirmed, r.TookMS)}
}
