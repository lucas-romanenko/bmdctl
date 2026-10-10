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

// CommandResult is the result of a wire command.
type CommandResult struct {
	// Args are the arguments as sent, for display: M/Es counted from 1,
	// sources with their names, enums by name.
	Args map[string]string `json:"args,omitempty"`
	// Command is the command's opcode.
	Command string `json:"command"`
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
			return fmt.Sprint(x + 1)
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
	run := c.e.ops[c.entry.Path]
	if run == nil {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", c.req.Device, c.req.Path)
	}
	return run(c)
}

// RunLocal runs a local operation in this process, without an engine:
// the CLI's way to run a command that needs no switcher.
func RunLocal(ctx context.Context, reg *table.Registry, req Request, onFile func(FileFrame) error) Response {
	e := New(Config{Registry: reg})
	defer e.cancel()
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

// operations are the engine's table entries that aren't one wire
// command, with their implementations.
type operation struct {
	entry table.Entry
	run   op
}

// atemOperations maps each ATEM operation's path to its implementation.
func atemOperations() map[string]op {
	m := map[string]op{}
	for _, o := range operations() {
		m[o.entry.Path] = o.run
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
	return nil
}
