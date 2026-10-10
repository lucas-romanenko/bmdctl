// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
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
	res   atem.Resolver
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
	r, confirmed, err := c.send(cmd)
	r.Args = c.displayArgs()
	if c.entry.Path != c.req.Path {
		r.Sent = c.entry.Path
	}
	return r, confirmed, err
}

// send sends cmd. A command with a state echo waits for it, the only
// confirmation the protocol has (SURFACE.md §4.4); one without is sent and
// left, as atemwire's send() leaves it.
func (c *call) send(cmd atem.Command) (CommandResult, bool, error) {
	r := CommandResult{Command: cmd.Opcode(), Echo: cmd.EchoKey()}
	if r.Echo == "" {
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
		e.Device, e.Operation = "atem", true
		es = append(es, e)
	}
	return r.Add(es...)
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
	return ops
}
