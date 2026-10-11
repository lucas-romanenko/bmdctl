// SPDX-License-Identifier: MIT

package engine

// undo: the engine keeps, per switcher, what the last changes it sent to
// program, preview, the aux outputs and the keyers' and DSKs' on air
// replaced (twenty deep), and undo puts the last one back. Only changes
// made through this engine, so from this machine; a change the switcher
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
	kind   string // program, preview, aux, key, dsk
	me     int
	index  int // the aux output, keyer or DSK
	source int // what program, preview or the aux had
	on     bool
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
