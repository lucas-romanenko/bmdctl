// SPDX-License-Identifier: MIT

package engine

// undo: the engine keeps, per switcher, what the last changes it sent to
// program, preview, the aux outputs and the keyers' and DSKs' on air
// replaced (twenty deep), and undo puts the last one back. A cut or an
// auto transition keeps both buses, and undo cuts back. Only changes made
// through this engine, so from this machine; a change the switcher
// already held (sent nothing) isn't one. bmdwire v1.3.0 has no undo.

import (
	"fmt"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// undoDepth is how many changes undo can go back, per switcher.
const undoDepth = 20

// undoEntry is a change undo can put back: what it replaced.
type undoEntry struct {
	kind   string // program, preview, aux, key, dsk, cut, auto
	me     int
	index  int // the aux output, keyer or DSK
	source int // what program, preview or the aux had
	on     bool
	// preview is what preview had before a cut or an auto transition
	// (source, program's); swapped, that the next transition took the
	// background, so it swapped them.
	preview int
	swapped bool
}

// UndoResult is what undo put back.
type UndoResult struct {
	Undone bool `json:"undone"`
	// What is the change undone: program, preview, aux 2, key 1, dsk 1.
	What string `json:"what,omitempty"`
	CommandResult
}

// remember returns a function that keeps, for undo, what the change the
// request is about to make replaces: called once it is sent.
func (c *call) remember() func() {
	st, a := c.state, c.args
	var u undoEntry
	switch c.entry.Path {
	case "program":
		u = undoEntry{kind: "program", me: ix(a, "me"), source: st.ProgramSource(ix(a, "me"))}
	case "preview":
		u = undoEntry{kind: "preview", me: ix(a, "me"), source: st.PreviewSource(ix(a, "me"))}
	case "aux":
		u = undoEntry{kind: "aux", index: ix(a, "aux"), source: st.AuxSource(ix(a, "aux"))}
	case "usk on-air", "usk toggle":
		u = undoEntry{kind: "key", me: ix(a, "me"), index: ix(a, "keyer"), on: st.USKOnAir(ix(a, "me"), ix(a, "keyer"))}
	case "dsk on-air", "dsk toggle", "dsk auto":
		u = undoEntry{kind: "dsk", index: ix(a, "dsk"), on: st.DSKState(ix(a, "dsk")).OnAir}
	case "cut", "auto":
		me := ix(a, "me")
		u = undoEntry{kind: c.entry.Path, me: me, source: st.ProgramSource(me), preview: st.PreviewSource(me),
			swapped: st.TransitionSelection(me).Background}
	default:
		return func() {}
	}
	return func() {
		c.e.mu.Lock()
		defer c.e.mu.Unlock()
		s := append(c.e.undo[c.host.key], u)
		c.e.undo[c.host.key] = s[max(0, len(s)-undoDepth):]
	}
}

// pop takes the last change off h's stack; put puts one back.
func (e *Engine) pop(key string) (undoEntry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.undo[key]
	if len(s) == 0 {
		return undoEntry{}, false
	}
	e.undo[key] = s[:len(s)-1]
	return s[len(s)-1], true
}

func (e *Engine) put(key string, u undoEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.undo[key] = append(e.undo[key], u)
}

func opUndo(c *call) (any, bool, error) {
	u, ok := c.e.pop(c.host.key)
	if !ok {
		return UndoResult{}, false, nil
	}
	if u.kind == "cut" || u.kind == "auto" {
		return c.undoCut(u)
	}
	st := c.state
	var cmd atem.Command
	var what string
	var v Value
	switch u.kind {
	case "program":
		cmd, what = atem.ProgramInput{ME: uint8(u.me), Source: uint16(u.source)}, "program"
		v = Value{Param: "program", Before: ptr(sourceName(st, st.ProgramSource(u.me))), After: sourceName(st, u.source)}
	case "preview":
		cmd, what = atem.PreviewInput{ME: uint8(u.me), Source: uint16(u.source)}, "preview"
		v = Value{Param: "preview", Before: ptr(sourceName(st, st.PreviewSource(u.me))), After: sourceName(st, u.source)}
	case "aux":
		cmd, what = atem.AuxSource{Aux: uint8(u.index), Source: uint16(u.source)}, fmt.Sprintf("aux %v", table.Index(u.index))
		v = Value{Param: "aux", Before: ptr(sourceName(st, st.AuxSource(u.index))), After: sourceName(st, u.source)}
	case "key":
		cmd, what = atem.KeyOnAir{ME: uint8(u.me), Keyer: uint8(u.index), Enabled: u.on}, fmt.Sprintf("key %v", table.Index(u.index))
		v = Value{Param: "on-air", Before: ptr(onOff(st.USKOnAir(u.me, u.index))), After: onOff(u.on)}
	case "dsk":
		cmd, what = atem.DkeyOnair{DSK: uint8(u.index), OnAir: u.on}, fmt.Sprintf("dsk %v", table.Index(u.index))
		v = Value{Param: "on-air", Before: ptr(onOff(st.DSKState(u.index).OnAir)), After: onOff(u.on)}
	}
	if u.me > 0 && (u.kind == "program" || u.kind == "preview" || u.kind == "key") {
		what = fmt.Sprintf("ME%v %s", table.Index(u.me), what)
	}
	r, confirmed, err := c.send(cmd)
	if err != nil {
		c.e.put(c.host.key, u) // not undone: it can be tried again
		return nil, false, err
	}
	c.note(Note{Values: []Value{v}})
	return UndoResult{Undone: true, What: what, CommandResult: r}, confirmed, nil
}

// undoCut undoes a cut or an auto transition: program and preview go back
// to what they were. Where they are as it left them and the next
// transition still takes the background as it did, that is a cut, which
// also takes back the keys it took on or off air. Where they have changed
// since (not from here: that would be on the stack), each bus is set
// back. A transition still running is left to finish.
func (c *call) undoCut(u undoEntry) (any, bool, error) {
	st := c.state
	what := u.kind
	if u.me > 0 {
		what = fmt.Sprintf("ME%v %s", table.Index(u.me), what)
	}
	if st.TransitionInTransition(u.me) {
		c.e.put(c.host.key, u)
		return nil, false, codeErr(CodeUsage, "M/E %v is mid-transition: undo once it is done", table.Index(u.me))
	}
	prog, prev := st.ProgramSource(u.me), st.PreviewSource(u.me)
	leftProg, leftPrev := u.source, u.preview
	if u.swapped {
		leftProg, leftPrev = u.preview, u.source
	}
	vals := []Value{
		{Param: "program", Before: ptr(sourceName(st, prog)), After: sourceName(st, u.source)},
		{Param: "preview", Before: ptr(sourceName(st, prev)), After: sourceName(st, u.preview)},
	}
	var cmds []atem.Command
	switch {
	case prog == leftProg && prev == leftPrev && st.TransitionSelection(u.me).Background == u.swapped:
		cmds = []atem.Command{atem.Cut{ME: uint8(u.me)}}
	default:
		if prog != u.source {
			cmds = append(cmds, atem.ProgramInput{ME: uint8(u.me), Source: uint16(u.source)})
		}
		if prev != u.preview {
			cmds = append(cmds, atem.PreviewInput{ME: uint8(u.me), Source: uint16(u.preview)})
		}
	}
	if len(cmds) == 0 {
		// Both buses hold what they had before it: nothing to send.
		c.note(Note{Values: vals})
		return UndoResult{Undone: true, What: what, CommandResult: CommandResult{Already: true}}, true, nil
	}
	var r CommandResult
	confirmed := true
	for i, cmd := range cmds {
		res, ok, err := c.send(cmd)
		if err != nil {
			if i == 0 {
				c.e.put(c.host.key, u) // nothing undone: it can be tried again
			}
			return nil, false, err
		}
		if i == 0 {
			r = res
		}
		confirmed = confirmed && ok
	}
	c.note(Note{Values: vals})
	return UndoResult{Undone: true, What: what, CommandResult: r}, confirmed, nil
}
