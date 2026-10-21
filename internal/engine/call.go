// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/hyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/table"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
	"github.com/lucas-romanenko/bmdctl/videohub"
)

// call is one request on its way through the engine.
type call struct {
	ctx   context.Context
	e     *Engine
	req   Request
	entry *table.Entry
	args  table.Args
	out   sink
	start time.Time

	// Set for a request that needs a switcher.
	host  *host
	sess  *atem.Session
	state atem.State // as the request found it
	// Set for a request to a HyperDeck (hyperdeck.go).
	deck   *hyperdeck.Session
	dstate hyperdeck.State // as the request found it
	// Set for a request to a Videohub (videohub.go).
	hub    *videohub.Session
	hstate videohub.State // as the request found it
	// Set for a request to an Ultimatte (ultimatte.go): its client, its
	// host:9998 and its host.
	ult             *ultimatte.Client
	ultKey, ultHost string
	res             table.Resolver
	// noInput is what a source that named nothing could have been, for
	// a note if the arguments don't parse.
	noInput *NoInput
}

// op runs an operation: its result, whether the switcher confirmed it,
// or an error carrying its code.
type op func(c *call) (any, bool, error)

// checkIndices refuses an M/E, keyer or DSK the switcher doesn't have,
// where it has said how many it has: a switcher ignores a command for one
// it lacks, and the caller would see only that no echo came.
func (c *call) checkIndices() error {
	idx := func(name string) (int, bool) {
		if !c.args.Has(name) {
			return 0, false
		}
		v, ok := c.args.Value(name).(uint64)
		return int(v), ok
	}
	// Only counts the switcher reported: the readers' defaults (one M/E,
	// four keyers) are guesses.
	top := c.state.Topology
	me, hasME := idx("me")
	if n := c.state.MECount(); hasME && top != nil && top.MEUnits > 0 && me >= n {
		return codeErr(CodeUsage, "--me %v: the switcher has %d M/E%s", table.Index(me), n, plural(n))
	}
	if k, ok := idx("keyer"); ok && me < len(c.state.MEs) && c.state.MEs[me].Config != nil {
		if n := c.state.MEKeyerCount(me); k >= n {
			return codeErr(CodeUsage, "%s %v: M/E %v has %d upstream keyer%s, 1-%d", c.entry.Label("keyer"), table.Index(k), table.Index(me), n, plural(n), n)
		}
	}
	if d, ok := idx("dsk"); ok && top != nil && top.DownstreamKeyers > 0 {
		if n := c.state.DSKCount(); d >= n {
			return codeErr(CodeUsage, "--dsk %v: the switcher has %d downstream keyer%s, 1-%d", table.Index(d), n, plural(n), n)
		}
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// CommandResult is the result of a wire command.
type CommandResult struct {
	// Args are the arguments as sent, for display: indexes counted from
	// 1, sources with their names, enums by name.
	Args map[string]string `json:"args,omitempty"`
	// Command is the command's opcode.
	Command string `json:"command"`
	// Sent is the path of the command sent, where it isn't the one asked
	// for: classic-audio master for audio master on a switcher with the
	// classic mixer. Args are then that command's.
	Sent string `json:"sent,omitempty"`
	// Echo is the state key the engine waited for, if the command has one.
	Echo string `json:"echo,omitempty"`
	// State is the echoed state packet.
	State any `json:"state,omitempty"`
	// Already: the switcher held every value asked for, so nothing was
	// sent.
	Already bool `json:"already,omitempty"`
}

// sendEntry builds the entry's wire command from the arguments and sends
// it.
func (c *call) sendEntry() (any, bool, error) {
	built, err := c.entry.Build(c.args)
	if err != nil {
		return nil, false, usageErr(err)
	}
	cmd, ok := built.(atem.Command)
	if !ok {
		return nil, false, fmt.Errorf("%T is not an ATEM command", built)
	}
	if v, ok := cmd.(atem.Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, false, usageErr(err)
		}
	}
	if c.alreadyHeld() {
		r := CommandResult{Command: cmd.Opcode(), Echo: cmd.EchoKey(), Args: c.displayArgs(), Already: true}
		c.noteValues(false, nil)
		return r, true, nil
	}
	undoable := c.remember()
	r, confirmed, err := c.send(cmd)
	r.Args = c.displayArgs()
	if c.entry.Path != c.req.Path {
		r.Sent = c.entry.Path
	}
	if err == nil {
		undoable()
		c.noteValues(confirmed, nil)
	}
	return r, confirmed, err
}

// alreadyHeld reports whether the switcher holds every value a command
// sets already, by the command's reader: then sending it would change
// nothing, and nothing is sent. An argument that says where (named in
// the echo key: the M/E, the keyer) isn't a value; one the reader
// doesn't know means the command is sent.
func (c *call) alreadyHeld() bool {
	r := readers[c.entry.Path]
	if r == nil || c.entry.Path != c.req.Path {
		return false
	}
	held := r(c.state, c.args)
	compared := false
	for _, p := range c.entry.Params {
		if !c.args.Has(p.Name) {
			continue
		}
		v, ok := held[p.Name]
		switch {
		case !ok && strings.Contains(c.entry.Echo, "<"+p.Name+">"):
			continue
		case !ok || !reflect.DeepEqual(v, c.args.Value(p.Name)):
			return false
		}
		compared = true
	}
	return compared
}

// noteValues sends a command's values in a note: its arguments' (from
// the state its echo left, when confirmed), or for an operation that says
// what it changes, the arguments that say where (the indexes) and change.
func (c *call) noteValues(confirmed bool, change []Value) {
	if !c.req.Notes {
		return
	}
	var vals []Value
	if change != nil {
		for _, v := range c.argValues(nil, nil) {
			if p := c.entry.Params[slicesIndex(c.entry.Params, v.Param)]; p.OneBased {
				vals = append(vals, v)
			}
		}
		vals = append(vals, change...)
	} else {
		var post *atem.State
		if confirmed && readers[c.entry.Path] != nil {
			st := c.sess.State()
			post = &st
		}
		vals = c.commandValues(post)
	}
	if len(vals) > 0 {
		c.note(Note{Values: vals})
	}
}

func slicesIndex(ps []table.Param, name string) int {
	for i, p := range ps {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// send sends cmd. A command with a state echo waits for it, the only
// confirmation the protocol has (SURFACE.md §4.4); one without is sent and
// left, as atemwire's send() leaves it, unless the switcher reports what
// it did at once (effect).
func (c *call) send(cmd atem.Command) (CommandResult, bool, error) {
	r := CommandResult{Command: cmd.Opcode(), Echo: cmd.EchoKey()}
	if r.Echo == "" {
		if keys, all := c.effect(cmd); len(keys) > 0 {
			confirmed, err := c.sendAndWaitFor(cmd, keys, all)
			return r, confirmed, err
		}
		return r, false, sessionErr(c.sess.Send(cmd))
	}
	ctx, cancel := context.WithTimeout(c.ctx, c.req.timeout())
	defer cancel()
	ch, err := c.sess.SendAndWait(ctx, cmd)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && c.ctx.Err() == nil {
			return r, false, codeErr(CodeNoEcho, "sent, but the switcher did not report %s within %v", r.Echo, c.req.timeout())
		}
		return r, false, sessionErr(err)
	}
	r.State = ch.Value
	return r, true, nil
}

// effect is what the switcher reports at once of a command that has no
// echo: the state keys, and whether it reports all of them or one. A cut
// swaps program and preview when the next transition takes the
// background and they differ (PrgI and PrvI); an auto transition starts
// (TrPs), or is done at once (PrgI). Nothing else is waited for: a cut of
// the keys alone is sent and left.
func (c *call) effect(cmd atem.Command) (keys []string, all bool) {
	switch x := cmd.(type) {
	case atem.Cut:
		me := int(x.ME)
		if !c.state.TransitionSelection(me).Background || c.state.ProgramSource(me) == c.state.PreviewSource(me) {
			return nil, false
		}
		return []string{fmt.Sprintf("PrgI:%d", me), fmt.Sprintf("PrvI:%d", me)}, true
	case atem.Auto:
		return []string{fmt.Sprintf("TrPs:%d", x.ME), fmt.Sprintf("PrgI:%d", x.ME)}, false
	}
	return nil, false
}

// sendAndWaitFor sends cmd and waits for the switcher to report keys (all
// of them, or the first): then the session's state holds what cmd did, so
// the next request reads it and not the state before (a cut, then undo).
// Not reported within the timeout, cmd is sent and unconfirmed, as it was
// before the wait.
func (c *call) sendAndWaitFor(cmd atem.Command, keys []string, all bool) (bool, error) {
	changes, stop := c.sess.Subscribe() // before sending: the report can't slip past
	defer stop()
	if err := c.sess.Send(cmd); err != nil {
		return false, sessionErr(err)
	}
	ctx, cancel := context.WithTimeout(c.ctx, c.req.timeout())
	defer cancel()
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for {
		select {
		case ch, ok := <-changes:
			if !ok {
				return false, nil // the session ended, or this fell behind
			}
			if !want[ch.Key] {
				continue
			}
			delete(want, ch.Key)
			if !all || len(want) == 0 {
				return true, nil
			}
		case <-ctx.Done():
			if c.ctx.Err() != nil {
				return false, sessionErr(c.ctx.Err())
			}
			return false, nil
		}
	}
}

// sessionErr gives a session error its code: the session gone is
// unreachable, anything else (a command that won't encode) is usage.
func sessionErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, atem.ErrClosed), errors.Is(err, atem.ErrDisconnected), errors.Is(err, atem.ErrNotReady),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Error{CodeUnreachable, err}
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{CodeUsage, err}
}

// displayArgs renders the parsed arguments for people: one-based
// parameters as typed, sources with their input names, enums by name.
func (c *call) displayArgs() map[string]string {
	out := map[string]string{}
	for _, p := range c.entry.Params {
		if !c.args.Has(p.Name) {
			continue
		}
		out[p.Name] = displayValue(p, c.args.Value(p.Name), c.state)
	}
	return out
}

func displayValue(p table.Param, v any, st atem.State) string {
	switch x := v.(type) {
	case uint64:
		switch {
		case p.OneBased:
			return table.Index(x).String()
		case p.Kind == table.Source:
			return sourceLabel(st, int(x))
		case p.Kind == table.Enum:
			for _, n := range p.SortedNames() {
				if p.Names[n] == x {
					return n
				}
			}
		case p.Kind == table.Flags:
			var ns []string
			for _, n := range p.SortedNames() {
				if b := p.Names[n]; b != 0 && x&b == b {
					ns = append(ns, n)
				}
			}
			if len(ns) > 0 {
				return strings.Join(ns, ",")
			}
		}
	case int64:
		if p.Kind == table.Rate {
			return fmt.Sprintf("%d frames", x)
		}
	case time.Time:
		return x.Format(time.RFC3339)
	case []string:
		return strings.Join(x, ",")
	}
	return fmt.Sprint(v)
}

// sourceLabel is "5 (Camera 5)", or "5" for a source without a name.
func sourceLabel(st atem.State, id int) string {
	if id >= 0 && id <= 0xFFFF {
		if in := st.Inputs[uint16(id)]; in != nil && strings.TrimSpace(in.Name) != "" {
			return fmt.Sprintf("%d (%s)", id, strings.TrimSpace(in.Name))
		}
	}
	return fmt.Sprint(id)
}

// runLocal runs a local operation, which needs no switcher.
func (c *call) runLocal() (any, bool, error) {
	var err error
	c.res = atem.Resolver{FPS: c.req.FPS}
	c.args, err = c.entry.Parse(c.req.Params, c.res)
	if err != nil {
		return nil, false, usageErr(err)
	}
	o, ok := c.e.ops[c.entry.Path]
	if !ok {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", c.req.Device, c.req.Path)
	}
	return o.run(c)
}

// RunLocal runs a local operation in this process, without an engine:
// the CLI's way to run a command that needs no switcher.
func RunLocal(ctx context.Context, reg *table.Registry, req Request, onFile func(FileFrame) error) Response {
	e := New(Config{Registry: reg})
	defer e.cancel()
	req.V = Version
	return e.Handle(ctx, req, funcSink{onFile})
}

type funcSink struct{ onFile func(FileFrame) error }

func (s funcSink) file(f FileFrame) error {
	if s.onFile == nil {
		return nil
	}
	return s.onFile(f)
}
func (funcSink) note(Note) error         { return nil }
func (funcSink) response(Response) error { return nil }

// wireChecks are what a wire command is checked for against the state
// before it is sent, by path.
var wireChecks = map[string]func(c *call) error{
	"macro record": checkMacroRecord,
	"macro action": checkMacroAction,
	"video-mode":   checkVideoMode,
}

// operation is one of the engine's table entries that isn't one wire
// command, with its implementation.
type operation struct {
	entry table.Entry
	run   op
	// noDial: the operation takes the host as given and opens no session.
	noDial bool
}

// atemOperations maps each ATEM operation's path to its definition.
func atemOperations() map[string]operation {
	m := map[string]operation{}
	for _, o := range operations() {
		m[o.entry.Path] = o
	}
	return m
}

// RegisterOperations adds the engine's operations to r.
func RegisterOperations(r *table.Registry) error {
	var es []table.Entry
	for _, o := range operations() {
		e := o.entry
		e.Device, e.Operation, e.Examples, e.Group = "atem", true, opExamples[e.Path], atem.GroupOf(e.Path)
		es = append(es, e)
	}
	if err := r.Add(es...); err != nil {
		return err
	}
	if err := registerDeckOperations(r); err != nil {
		return err
	}
	if err := registerHubOperations(r); err != nil {
		return err
	}
	if err := registerUltOperations(r); err != nil {
		return err
	}
	// input list, inputs' name before the first hardware session's
	// second run.
	return r.Alias("atem", "input list", "inputs")
}

// operations lists every operation, group by group.
func operations() []operation {
	var ops []operation
	ops = append(ops, sessionOperations()...)
	ops = append(ops, switchingOperations()...)
	ops = append(ops, keyerOperations()...)
	ops = append(ops, audioOperations()...)
	ops = append(ops, mediaOperations()...)
	ops = append(ops, macroOperations()...)
	ops = append(ops, profileOperations()...)
	ops = append(ops, systemOperations()...)
	ops = append(ops, showOperation())
	return ops
}
