// SPDX-License-Identifier: MIT

package main

// The terminal's views of the reads, the transfers and the session
// commands (screen.go has the layout). Rows repeat nothing the header
// says, collapse what repeats (aux 1–6, empty slots) and leave out what a
// person rarely wants unless --all asks.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	screenFormats["status"] = screenStatus
	screenFormats["connect"] = screenConnect
	screenFormats["disconnect"] = screenDisconnect
	screenFormats["info"] = screenInfo
	screenFormats["state"] = screenState_
	screenFormats["watch"] = screenWatch
	screenFormats["inputs"] = screenInputs
	screenFormats["media list"] = screenMediaList
	screenFormats["macro list"] = screenMacroList
	screenFormats["video-modes"] = screenVideoModes
	screenFormats["time"] = screenTime
	screenFormats["audio meters"] = screenMeters
	screenFormats["classic-audio meters"] = screenMeters
	screenFormats["macro decode"] = screenMacroDecode
	screenFormats["macro encode"] = screenMacroEncode
	for _, path := range []string{"media upload", "media download", "media clear", "macro upload", "macro download"} {
		screenFormats[path] = screenTransfer
	}
	for _, path := range []string{"profile save", "profile restore", "profile images"} {
		screenFormats[path] = screenProfile
	}
}

// short is a source as a line shows it: its short name, else its long
// name, else its id.
func short(s engine.Source) string {
	switch {
	case strings.TrimSpace(s.Short) != "":
		return strings.TrimSpace(s.Short)
	case strings.TrimSpace(s.Name) != "":
		return strings.TrimSpace(s.Name)
	}
	return strconv.Itoa(s.ID)
}

// padLeft right-aligns s in n columns.
func padLeft(s string, n int) string {
	if w := width(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

// labelled is a row under a label in the verb column.
func labelled(label, rest string) string { return indent + verbCell(label) + rest }

// hidden says how many rows --all would add, dim.
func (p *printer) hidden(n int, what string) string {
	return indent + p.c.colour(dim, count(n, what)+" hidden · --all shows them")
}

func screenStatus(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.StatusResult
	json.Unmarshal(raw, &r)
	c := p.c
	v := view{noLive: true, model: true, gap: true}
	if p.connectedIn == 0 {
		var s []string
		if r.Session.Since != nil {
			s = append(s, "session "+ago(sessionAge(r.Session)))
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
	// Keyers and DSKs end in their on-air state, at the line's end.
	var out []string
	var keyers []engine.KeyerStatus
	for _, m := range r.MEs {
		keyers = append(keyers, m.Keyers...)
	}
	typeWidth := keyerTypeWidth(keyers)
	for _, m := range r.MEs {
		out = append(out, labelled(fmt.Sprintf("ME %v", m.ME),
			padRight(c.colour(red, "PGM  "+short(m.Program)), 20)+c.colour(green, "PVW  "+short(m.Preview))))
		t := m.Transition
		parts := []string{strings.TrimSpace(t.Style + " " + t.RateText), "next " + strings.Join(t.Next, "+")}
		if t.InTransition {
			parts = append(parts, fmt.Sprintf("%d%%", t.Position/100))
		}
		if t.Preview {
			parts = append(parts, "previewing")
		}
		switch {
		case m.FTB.InTransition:
			parts = append(parts, "FTB fading")
		case m.FTB.Active:
			parts = append(parts, c.colour(red, "FTB black"))
		default:
			parts = append(parts, "FTB off")
		}
		out = append(out, labelled("", strings.Join(parts, " · ")))
		for i, k := range m.Keyers {
			label := ""
			if i == 0 {
				label = "keys"
			}
			out = append(out, trailing(labelled(label, keyerRow(k, typeWidth)), c.onAir(k.OnAir)))
		}
	}
	for _, d := range r.DSKs {
		state := c.onAir(d.OnAir)
		if d.Tie {
			state += c.colour(dim, ", tied")
		}
		out = append(out, trailing(labelled(fmt.Sprintf("dsk %v", d.DSK), short(d.Fill)+" / "+short(d.Key)), state))
	}
	out = append(out, auxRows(r.Aux)...)
	macro := c.colour(dim, "idle")
	switch {
	case r.Macro.Running:
		macro = "running"
		if r.Macro.Slot != nil {
			macro += fmt.Sprintf(" %v %s", *r.Macro.Slot, r.Macro.Name)
		}
		if r.Macro.Waiting {
			macro += ", waiting"
		}
	case r.Macro.Recording && r.Macro.RecSlot != nil:
		macro = fmt.Sprintf("recording %v", *r.Macro.RecSlot)
	}
	out = append(out, labelled("macro", strings.TrimSpace(macro)))
	if r.Recording != nil {
		rec := c.colour(dim, "stopped")
		if *r.Recording {
			rec = c.colour(red, "recording")
		}
		out = append(out, labelled("recording", rec))
	}
	if r.Streaming != "" {
		s := r.Streaming
		if s == "on air" {
			s = c.colour(red, s)
		}
		out = append(out, labelled("streaming", s))
	}
	v.rows = out
	return v
}

// onAir is a keyer's or DSK's state: on air red, off dim.
func (c *cli) onAir(on bool) string {
	if on {
		return c.colour(red, "on air")
	}
	return c.colour(dim, "off")
}

// keyerRow is an upstream keyer as status and show keys both lay it out:
// its number, its type (padded to typeWidth), fill / key. Its on-air
// state goes at the end of the line (trailing).
func keyerRow(k engine.KeyerStatus, typeWidth int) string {
	return fmt.Sprintf("%v  %s  %s / %s", k.Keyer, padRight(k.Type, typeWidth), short(k.Fill), short(k.Key))
}

// keyerTypeWidth is the widest keyer type: the type column's width.
func keyerTypeWidth(ks []engine.KeyerStatus) int {
	w := 0
	for _, k := range ks {
		w = max(w, width(k.Type))
	}
	return w
}

// auxRows are the aux outputs, a run of outputs on one source as one
// row: "aux 1–6  PGM".
func auxRows(aux []engine.AuxStatus) []string {
	var out []string
	for i := 0; i < len(aux); {
		j := i + 1
		for j < len(aux) && aux[j].Source.ID == aux[i].Source.ID && aux[j].Aux == aux[j-1].Aux+1 {
			j++
		}
		label := fmt.Sprintf("aux %v", aux[i].Aux)
		if j-i > 1 {
			label = fmt.Sprintf("aux %v–%v", aux[i].Aux, aux[j-1].Aux)
		}
		out = append(out, labelled(label, short(aux[i].Source)))
		i = j
	}
	return out
}

// screenConnect is connect: the header, then what connect ADDRESS saved
// (and, the first time ever, that Tab completes the switcher's names), or
// how long the session stays open.
func screenConnect(p *printer, _ engine.Response, raw json.RawMessage) view {
	var s engine.SessionStatus
	json.Unmarshal(raw, &s)
	c := p.c
	var out []string
	switch {
	case p.saved.added:
		out = append(out, indent+c.colour(bold, fmt.Sprintf("saved as %q, now the default", p.saved.name)))
	case p.saved.defaulted:
		out = append(out, indent+c.colour(bold, fmt.Sprintf("%q is now the default", p.saved.name)))
	default:
		state := "pinned until disconnect"
		switch {
		case s.Pinned && s.IdleCloseS > 0:
			state = "pinned until disconnect or " + span(time.Duration(s.IdleCloseS)*time.Second) + " idle"
		case !s.Pinned:
			state = "closes after " + span(time.Duration(s.IdleCloseS)*time.Second) + " idle"
		}
		out = append(out, c.resultLine("connect", state, ""))
	}
	if p.saved.name != "" && p.e.Device == "atem" && c.tellOnce("tab-completion") {
		out = append(out, indent+c.colour(dim, "tab completes input names"))
	}
	return view{rows: out}
}

func screenDisconnect(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.DisconnectResult
	json.Unmarshal(raw, &r)
	state := "disconnected"
	if !r.Closed {
		state = p.c.colour(dim, "not connected")
	}
	return view{bare: true, rows: []string{p.c.colour(bold, p.name()) + "  " + state}}
}

func screenInfo(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.InfoResult
	json.Unmarshal(raw, &r)
	keyers := make([]string, len(r.Keyers))
	for i, k := range r.Keyers {
		keyers[i] = fmt.Sprint(k)
	}
	row := func(label, value string) string { return labelled(label, value) }
	out := []string{row("product", fmt.Sprintf("%s %s", r.Product, p.c.colour(dim, fmt.Sprintf("model %d", r.Model))))}
	if r.Name != "" {
		out = append(out, row("name", r.Name))
	}
	out = append(out,
		row("protocol", r.Protocol),
		row("video", fmt.Sprintf("%s · %d×%d · rates at %d fps", r.VideoMode, r.Resolution[0], r.Resolution[1], r.DisplayFPS)),
		row("m/e", fmt.Sprintf("%d · keyers %s · dsk %d · aux %d", r.MEs, strings.Join(keyers, ", "), r.DSKs, r.Aux)),
		row("media", strings.Join([]string{count(r.MediaPlayers, "player"), count(r.Stills, "still"), count(r.Clips, "clip"), count(r.Macros, "macro")}, " · ")),
		row("outputs", strings.Join([]string{count(r.Multiviewers, "multiviewer"), count(r.SuperSources, "supersource"), count(r.HyperDecks, "hyperdeck")}, " · ")),
		row("inputs", fmt.Sprint(r.Inputs)),
		row("audio", r.Audio),
	)
	return view{gap: true, rows: out}
}

// screenState_ is state: a leaf's value alone, for scripts; anything
// bigger one dotted.key value row per leaf.
func screenState_(p *printer, _ engine.Response, raw json.RawMessage) view {
	if v := parseOrdered(raw); v.obj == nil && v.arr == nil {
		return view{bare: true, rows: []string{v.scalar}}
	}
	return view{gap: true, rows: rows(flatten(raw))}
}

// hiddenPorts are the sources inputs leaves out unless --all: the
// outputs, the key masks and the media players' keys.
var hiddenPorts = map[int]bool{5: true, 128: true, 129: true, 130: true, 131: true}

// screenInputs is a source per row: an external input's number dim (an
// internal source's nobody types), its short and long names, and what
// is on program and preview marked.
func screenInputs(p *printer, _ engine.Response, raw json.RawMessage) view {
	var ins []engine.InputEntry
	json.Unmarshal(raw, &ins)
	c := p.c
	idWidth := 3
	for _, in := range ins {
		if in.PortType == 0 {
			idWidth = max(idWidth, len(strconv.Itoa(in.Source)))
		}
	}
	var cells [][]string
	var marks []string
	hidden := 0
	for _, in := range ins {
		if hiddenPorts[in.PortType] && !p.flagOn("all") {
			hidden++
			continue
		}
		id := ""
		if in.PortType == 0 {
			id = strconv.Itoa(in.Source)
		}
		mark := ""
		if l := p.live; l != nil {
			switch in.Source {
			case l.ProgramID:
				mark = c.colour(red, "● PGM")
			case l.PreviewID:
				mark = c.colour(green, "○ PVW")
			}
		}
		cells = append(cells, []string{c.colour(dim, padLeft(id, idWidth)), in.Short, in.Long})
		marks = append(marks, mark)
	}
	out := rows(cells)
	for i := range out {
		out[i] = c.leadered(out[i], marks[i])
	}
	if hidden > 0 {
		out = append(out, "", indent+c.colour(dim, "bmdctl inputs --all adds outputs, masks and keys"))
	}
	return view{gap: true, rows: out}
}

// flagOn reports whether a bool flag was given true on the line.
func (p *printer) flagOn(name string) bool {
	v, ok := p.typed[name]
	return ok && (v == "" || isTrue(v))
}

// screenMediaList is a still slot per row, its number dim, and the
// players showing it; empty slots no player shows are counted, not
// listed, unless --all.
func screenMediaList(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.MediaList
	json.Unmarshal(raw, &r)
	c := p.c
	shows := map[int][]string{}
	for _, m := range r.Players {
		if m.Type == "still" {
			shows[int(m.Slot)] = append(shows[int(m.Slot)], fmt.Sprintf("MP%v", m.Player))
		}
	}
	idWidth := len(strconv.Itoa(len(r.Stills)))
	var cells [][]string
	hidden := 0
	for _, s := range r.Stills {
		players := shows[int(s.Slot)]
		name := s.Name
		switch {
		case !s.Used && len(players) == 0 && !p.flagOn("all"):
			hidden++
			continue
		case !s.Used:
			name = c.colour(dim, "(empty)")
		case name == "":
			name = c.colour(dim, "(no name)")
		}
		cells = append(cells, []string{c.colour(dim, padLeft(s.Slot.String(), idWidth)), name, c.colour(dim, strings.Join(players, " "))})
	}
	out := rows(cells)
	if hidden > 0 {
		out = append(out, p.hidden(hidden, "empty slot"))
	}
	about := count(len(r.Stills), "still")
	if r.Clips > 0 {
		about += " · " + count(r.Clips, "clip")
	}
	return view{about: []string{about}, gap: true, rows: out}
}

// screenMacroList is a used macro slot per row; empty slots are
// counted, not listed, unless --all. A terminal always asks the engine
// for every slot, to count them.
func screenMacroList(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.MacroList
	json.Unmarshal(raw, &r)
	c := p.c
	idWidth := 1
	for _, m := range r.Macros {
		idWidth = max(idWidth, len(m.Slot.String()))
	}
	var cells [][]string
	var states []string // running, recording, invalid: at the line's end
	hidden := 0
	for _, m := range r.Macros {
		state := ""
		switch {
		case r.Running != nil && m.Slot == *r.Running:
			state = "running"
		case r.Recording != nil && m.Slot == *r.Recording:
			state = "recording"
		case m.Invalid:
			state = "invalid"
		}
		name := m.Name
		switch {
		case !m.Used && state == "" && !p.flagOn("all"):
			hidden++
			continue
		case !m.Used:
			name = c.colour(dim, "(empty)")
		}
		cells = append(cells, []string{c.colour(dim, padLeft(m.Slot.String(), idWidth)), name, m.Description})
		states = append(states, state)
	}
	out := rows(cells)
	for i := range out {
		out[i] = trailing(out[i], states[i])
	}
	if hidden > 0 {
		out = append(out, p.hidden(hidden, "empty slot"))
	}
	if len(cells) == 0 && hidden == 0 {
		out = []string{indent + c.colour(dim, "no macros")}
	}
	return view{about: []string{count(len(r.Macros), "slot")}, gap: true, rows: out}
}

func screenVideoModes(p *printer, _ engine.Response, raw json.RawMessage) view {
	var ms []engine.VideoModeEntry
	json.Unmarshal(raw, &ms)
	if len(ms) == 0 {
		return view{rows: []string{indent + p.c.colour(dim, "the switcher has not listed its video modes")}}
	}
	idWidth := 0
	for _, m := range ms {
		idWidth = max(idWidth, len(strconv.Itoa(m.ID)))
	}
	var cells [][]string
	for _, m := range ms {
		mark := ""
		if m.Current {
			mark = "current"
		}
		cells = append(cells, []string{p.c.colour(dim, padLeft(strconv.Itoa(m.ID), idWidth)), m.Name, mark})
	}
	return view{gap: true, rows: rows(cells)}
}

func screenTime(p *printer, _ engine.Response, raw json.RawMessage) view {
	var t engine.TimeResult
	json.Unmarshal(raw, &t)
	return view{rows: []string{p.c.resultLine("time", strings.Join(nonEmpty(t.Timecode, t.Mode), " · "), "")}}
}

func screenMeters(p *printer, r engine.Response, raw json.RawMessage) view {
	lines := formatMeters(p, r, raw)
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return view{gap: true, rows: lines}
}

func screenMacroDecode(p *printer, r engine.Response, raw json.RawMessage) view {
	return view{bare: true, whole: true, rows: formatMacroDecode(p, r, raw)}
}

func screenMacroEncode(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.MacroResult
	json.Unmarshal(raw, &r)
	change := fmt.Sprintf("%s → %s  %s", r.Source, strings.Join(p.written, ", "), size(int64(r.Bytes)))
	return view{bare: true, rows: []string{p.c.resultLine("encode", change, "")}}
}

// size is a number of bytes as a person reads it: 900 B, 70 KB, 1.2 MB.
func size(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%d KB", (n+500)/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// screenTransfer is a transfer's line: the verb (upload, download,
// clear), the slot, the name and what it moved.
func screenTransfer(p *printer, r engine.Response, raw json.RawMessage) view {
	var t engine.TransferResult
	json.Unmarshal(raw, &t)
	c := p.c
	words := p.e.Words()
	verb := words[len(words)-1]
	slot := fmt.Sprintf("slot %v", t.Slot)
	if words[0] == "macro" {
		slot = fmt.Sprintf("macro %v", t.Slot)
	}
	parts := []string{slot}
	switch verb {
	case "upload":
		parts = append(parts, nonEmpty(t.Name)...)
		if t.Width > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", t.Width, t.Height))
		}
		if t.Replaced != "" {
			parts = append(parts, c.colour(dim, fmt.Sprintf("was %q", t.Replaced)))
		}
	case "download":
		if len(p.written) > 0 {
			// The file written, shortened from the left where the line
			// is too wide, so the size after it stays.
			before := strings.Join(append(parts, nonEmpty(t.Name)...), "  ")
			if t.Name != "" {
				before += " → "
			} else {
				before += "  → "
			}
			after := ""
			if t.Width > 0 {
				after = fmt.Sprintf("  %d×%d", t.Width, t.Height)
			}
			return view{rows: []string{c.resultLinePath(verb, before, strings.Join(p.written, ", "), after, took(r.TookMS))}}
		}
		parts = append(parts, nonEmpty(t.Name)...)
		if t.Width > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", t.Width, t.Height))
		}
	case "clear":
		if t.Replaced != "" {
			parts = append(parts, c.colour(dim, fmt.Sprintf("was %q", t.Replaced)))
		}
	}
	return view{rows: []string{c.resultLine(verb, strings.Join(parts, "  "), took(r.TookMS))}}
}

// screenProfile is a profile's line: the file and what it held (what
// each step did, where it had steps), then a row per error and per image
// that failed. A step that failed is said in place first.
func screenProfile(p *printer, r engine.Response, raw json.RawMessage) view {
	var pr engine.ProfileResult
	json.Unmarshal(raw, &pr)
	c := p.c
	stills, kept, failed := 0, 0, false
	for _, im := range pr.Images {
		switch {
		case im.Error != "":
			failed = true
		case im.FromCache:
			stills++
			kept++
		default:
			stills++
		}
	}
	// "20 stills", "20 stills, 8 downloaded", as the engine's step says it.
	stillsAbout := count(stills, "still")
	switch {
	case kept == 0:
	case kept == stills:
		stillsAbout += ", none downloaded"
	default:
		stillsAbout += fmt.Sprintf(", %d downloaded", stills-kept)
	}
	var verb, what string
	var about []string
	switch p.e.Path {
	case "profile save":
		verb, what = "profile", p.params["file"]
		about = append(about, size(int64(pr.Bytes)))
		if p.steps != nil {
			about = append(about, p.details...)
		} else if len(pr.Images) > 0 {
			about = append(about, stillsAbout)
		}
	case "profile restore":
		verb, what = "restore", p.params["file"]
		if p.steps != nil && p.failed == nil {
			about = p.details[min(1, len(p.details)):] // the setup's said already, above
		} else {
			about = nonEmpty(pr.Summary)
		}
	case "profile images":
		verb, what = "images", p.params["dir"]
		if what == "" {
			what = engine.MediaPoolDir
		}
		about = []string{stillsAbout}
	}
	if p.failed != nil {
		p.stepFailed(p.failed.Error, "")
	}
	rest := ""
	if len(about) > 0 {
		rest = "  " + strings.Join(about, " · ")
	}
	if what == "" {
		what, rest = strings.TrimSpace(rest), ""
	}
	out := []string{c.resultLinePath(verb, "", what, rest, took(r.TookMS))}
	for _, e := range pr.Errors {
		out = append(out, labelled(c.colour(red, "error"), e))
	}
	if failed {
		for _, im := range pr.Images {
			if im.Error != "" {
				out = append(out, labelled(c.colour(red, "error"), fmt.Sprintf("slot %v %q: %s", im.Slot, im.Name, im.Error)))
			}
		}
	}
	return view{rows: out}
}

// count is n things: "1 still", "20 stills".
func count(n int, thing string) string { return fmt.Sprintf("%d %s%s", n, thing, plural(n)) }

func init() { screenFormats["undo"] = screenUndo }

// screenUndo is undo as a write: the verb undo, what it put back, before
// → after, the time; or that there was nothing to undo, dim.
func screenUndo(p *printer, r engine.Response, raw json.RawMessage) view {
	var u engine.UndoResult
	json.Unmarshal(raw, &u)
	if !u.Undone {
		return view{rows: []string{indent + p.c.colour(dim, "nothing to undo")}}
	}
	return view{rows: []string{p.c.resultLine("undo", strings.TrimSpace(u.What+"  "+p.undoChange(u)), took(r.TookMS))}}
}

// undoChange is what undo changed: before → after, the after in its
// tally colour; an undone cut or auto transition, what each bus has now,
// as a cut says it.
func (p *printer) undoChange(u engine.UndoResult) string {
	if len(p.vals) == 2 && p.vals[0].Param == "program" && p.vals[1].Param == "preview" {
		return p.c.colour(red, p.vals[0].After) + " on program · " + p.c.colour(green, p.vals[1].After) + " on preview"
	}
	for _, v := range p.vals {
		after := v.After
		switch {
		case v.Param == "program", v.Param == "on-air" && v.After == "on":
			after = p.c.colour(red, after)
		case v.Param == "preview":
			after = p.c.colour(green, after)
		}
		if v.Before != nil && *v.Before != v.After {
			return *v.Before + " → " + after
		}
		return after
	}
	return ""
}
