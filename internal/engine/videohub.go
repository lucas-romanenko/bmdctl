// SPDX-License-Identifier: MIT

package engine

// Videohub requests (docs/PORTING.md, "Videohub commands"): every one an
// operation, since a port is named by its number or its label, which
// only the hub's state resolves. Ports count from 1, as Videohub Control
// and the hub's panel number them; the wire counts from 0 (table.Index).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
	"github.com/lucas-romanenko/bmdctl/videohub"
)

// hubOps are the Videohub's operations, by path.
var hubOps = func() map[string]operation {
	m := map[string]operation{}
	for _, o := range hubOperations() {
		m[o.entry.Path] = o
	}
	return m
}()

func hubOperations() []operation {
	port := func(name, help string) table.Param {
		return table.Param{Name: name, Kind: table.String, Required: true, Help: help}
	}
	output := port("output", "an output: its number, from 1, or its label or the start of it, in any case")
	input := port("input", "an input: its number, from 1, or its label or the start of it, in any case")
	label := table.Param{Name: "label", Kind: table.String, Required: true,
		Help: "the new label, at most 64 characters; runs of spaces and line breaks become one space, and an empty one clears it"}
	return []operation{
		{entry: table.Entry{Path: "route", Summary: "Route an input to an output.", Syntax: "route <output> <input>",
			Echo: "routing:<output>", Params: []table.Param{output, input}},
			run: opHubRoute},
		{entry: table.Entry{Path: "status", Summary: "Show the router and its routing: each output's input, label and lock."},
			run: opHubStatus},
		{entry: table.Entry{Path: "outputs", Summary: "List the outputs: each one's number, label, the input routed to it and its lock."},
			run: opHubOutputs},
		{entry: table.Entry{Path: "inputs", Summary: "List the inputs: each one's number and label, and the outputs it is routed to."},
			run: opHubInputs},
		{entry: table.Entry{Path: "input rename", Summary: "Rename an input on the router.", Syntax: "input rename <input> <label>",
			Echo: "input labels:<input>", Params: []table.Param{input, label}},
			run: opHubRename},
		{entry: table.Entry{Path: "output rename", Summary: "Rename an output on the router.", Syntax: "output rename <output> <label>",
			Echo: "output labels:<output>", Params: []table.Param{output, label}},
			run: opHubRename},
		{entry: table.Entry{Path: "watch", Summary: "Print each change the router reports, as it comes: routes, labels, locks.", Stream: true,
			Params: []table.Param{{Name: "count", Kind: table.Uint, Help: "stop after this many changes"}}},
			run: opHubWatch},
		{entry: table.Entry{Path: "state", Summary: "Print what the session holds of the router, or one part of it.",
			Params: []table.Param{{Name: "path", Kind: table.String, Help: "dotted key into the state: routing.3, device.model name"}}},
			run: opHubState},
		{entry: table.Entry{Path: "connect", Summary: "Connect to a Videohub and keep the session open; an address is saved as the default host.",
			Syntax: "connect [address]",
			Params: []table.Param{
				{Name: "address", Kind: table.String,
					Help: "an address (saved to hosts.yml by the name the router gives itself, as the default) or a host's name; else the default"},
				{Name: "pin", Kind: table.Bool, Default: "true",
					Help: "keep the session open until disconnect or an hour without a command ($BMDCTL_PIN_IDLE); false lets it close after 10 idle minutes"},
				typeParam}},
			run: opConnect},
		{entry: table.Entry{Path: "disconnect", Summary: "Close the engine's session to the router, pinned or not."},
			run: opHubDisconnect, noDial: true},
	}
}

// hubExamples are the Videohub operations' examples.
var hubExamples = map[string][]string{
	"route":         {"route 5 3", `route "Monitor A" "Camera 3"`, "route projector graphics"},
	"status":        {"status"},
	"outputs":       {"outputs", "outputs --json"},
	"inputs":        {"inputs"},
	"input rename":  {`input rename 3 "Wide Shot"`, `input rename "Camera 3" ""`},
	"output rename": {`output rename 5 Director`},
	"watch":         {"watch", "watch --count 5", "watch --json"},
	"state":         {"state", "state --path routing.4"},
	"connect":       {"connect 192.168.81.95", "connect", "connect --pin false"},
	"disconnect":    {"disconnect"},
}

func registerHubOperations(r *table.Registry) error {
	var es []table.Entry
	for _, o := range hubOperations() {
		e := o.entry
		e.Device, e.Operation, e.Examples, e.Group = "videohub", true, hubExamples[e.Path], videohub.GroupOf(e.Path)
		es = append(es, e)
	}
	return r.Add(es...)
}

// runHub runs a Videohub request: the session, the notes, the operation.
func (c *call) runHub() (any, bool, error) {
	o, ok := hubOps[c.entry.Path]
	if !ok {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", c.req.Device, c.req.Path)
	}
	var err error
	if o.noDial {
		if c.args, err = c.entry.Parse(c.req.Params, atem.Resolver{}); err != nil {
			return nil, false, usageErr(err)
		}
		return o.run(c)
	}
	connecting := func(key string) { c.note(Note{Connecting: key}) }
	h, s, idle, release, err := c.e.acquireSession(c.ctx, "videohub", c.req.Host, connecting)
	if err != nil {
		return nil, false, err
	}
	defer release()
	c.host, c.hub = h, s.(*videohub.Session)
	c.hstate = c.hub.State()
	c.note(Note{Switcher: c.e.hubNote(h, c.hstate, idle)})
	if c.args, err = c.entry.Parse(c.req.Params, atem.Resolver{}); err != nil {
		return nil, false, usageErr(err)
	}
	return o.run(c)
}

// hubNote is the Switcher note for a router: its name and model, as a
// switcher's note has them.
func (e *Engine) hubNote(h *host, st videohub.State, idle time.Duration) *Switcher {
	e.mu.Lock()
	pinned := h.pinned
	e.mu.Unlock()
	return &Switcher{Host: h.key, Device: "videohub", Name: st.Name(), Product: st.Model(), Ready: true, Pinned: pinned,
		IdleS: int(idle.Seconds()), DropS: int(e.closesAfter(pinned).Seconds())}
}

// hubSnapshot is the state's Snapshot, a count the hub sent that int()
// refuses a usage error.
func (c *call) hubSnapshot() (videohub.Snapshot, error) {
	snap, err := c.hstate.Snapshot()
	if err != nil {
		return snap, codeErr(CodeUsage, "the router's device block has a count that isn't a number: %v", err)
	}
	return snap, nil
}

// HubPort is one of a router's inputs or outputs.
type HubPort struct {
	Number table.Index `json:"number"` // from 1
	Label  string      `json:"label"`  // the hub's, else "Input N" or "Output N"
}

// hubPort finds a port by its number, counted from 1, or by its label:
// exactly, ignoring case, else the one whose label it starts.
func hubPort(kind string, ports []HubPort, s string) (int, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n < 1 || n > len(ports) {
			return 0, codeErr(CodeUsage, "%s %d: the router has %s, numbered from 1", kind, n, count(len(ports), kind))
		}
		return n - 1, nil
	}
	for _, p := range ports {
		if strings.EqualFold(p.Label, s) {
			return int(p.Number), nil
		}
	}
	var hits []HubPort
	for _, p := range ports {
		if s != "" && strings.HasPrefix(strings.ToLower(p.Label), strings.ToLower(s)) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return int(hits[0].Number), nil
	case 0:
		return 0, codeErr(CodeUsage, "%s %q: no %s is labelled so (bmdctl videohub %ss lists them)", kind, s, kind, kind)
	}
	var names []string
	for _, p := range hits {
		names = append(names, fmt.Sprintf("%d %q", p.Number, p.Label))
	}
	return 0, codeErr(CodeUsage, "%s %q could be %s: give more of the label, or its number", kind, s, strings.Join(names, ", "))
}

func hubPorts(ports []videohub.Port) []HubPort {
	out := make([]HubPort, len(ports))
	for i, p := range ports {
		out[i] = HubPort{Number: table.Index(p.Index), Label: p.Label}
	}
	return out
}

func hubOutputs(outs []videohub.Output) []HubPort {
	out := make([]HubPort, len(outs))
	for i, o := range outs {
		out[i] = HubPort{Number: table.Index(o.Index), Label: o.Label}
	}
	return out
}

// hubSend sends a command and waits for the hub's ACK.
func (c *call) hubSend(cmd videohub.Command) error {
	ctx, cancel := context.WithTimeout(c.ctx, c.req.timeout())
	defer cancel()
	err := c.hub.Send(ctx, cmd)
	if errors.Is(err, videohub.ErrTimeout) {
		return codeErr(CodeNoEcho, "sent %q, but the router did not answer within %v", blockText(cmd), c.req.timeout())
	}
	return hubErr(err)
}

// hubErr gives a router's error its code: a NAK is rejected (exit 4),
// no answer in time no_echo (3), the session gone unreachable (2), what
// it refuses before sending usage.
func hubErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, videohub.ErrRefused):
		return &Error{CodeRejected, err}
	case errors.Is(err, videohub.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Error{CodeUnreachable, err}
	}
	return &Error{CodeUsage, err}
}

// blockText is a block as a result names it: its lines on one, the
// blank line gone.
func blockText(cmd videohub.Command) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(cmd.Block(), "\n", " ")), " ")
}

// inputText is the input routed to an output, as a line shows it: its
// label, or "none".
func inputText(snap videohub.Snapshot, src *int) string {
	if src == nil {
		return "none"
	}
	if *src >= 0 && *src < len(snap.Inputs) {
		return snap.Inputs[*src].Label
	}
	return "input " + strconv.Itoa(*src+1)
}

func opHubRoute(c *call) (any, bool, error) {
	snap, err := c.hubSnapshot()
	if err != nil {
		return nil, false, err
	}
	out, err := hubPort("output", hubOutputs(snap.Outputs), c.args.Value("output").(string))
	if err != nil {
		return nil, false, err
	}
	in, err := hubPort("input", hubPorts(snap.Inputs), c.args.Value("input").(string))
	if err != nil {
		return nil, false, err
	}
	cmd := videohub.Route{Dest: out, Src: in}
	r := CommandResult{Command: blockText(cmd), Args: c.displayArgs()}
	before := inputText(snap, snap.Outputs[out].Source)
	values := func(after string) {
		c.note(Note{Values: []Value{{Param: "output", After: snap.Outputs[out].Label},
			{Param: "input", Before: ptr(before), After: after}}})
	}
	if src := snap.Outputs[out].Source; src != nil && *src == in {
		r.Already, r.Command = true, ""
		values(before)
		return r, true, nil
	}
	if err := c.hubSend(cmd); err != nil {
		switch {
		case !errors.Is(err, videohub.ErrRefused):
		case snap.Outputs[out].Lock == "L":
			err = codeErr(CodeRejected, "the router refused: output %d %q is locked by another client", out+1, snap.Outputs[out].Label)
		default: // videohubwire's message counts from 0
			err = codeErr(CodeRejected, "the router refused input %d %q to output %d %q (locked?)", in+1, snap.Inputs[in].Label,
				out+1, snap.Outputs[out].Label)
		}
		return r, false, err
	}
	after, _ := c.hub.State().Snapshot()
	values(inputText(after, after.Outputs[out].Source))
	return r, true, nil
}

func opHubRename(c *call) (any, bool, error) {
	snap, err := c.hubSnapshot()
	if err != nil {
		return nil, false, err
	}
	kind := strings.Fields(c.entry.Path)[0]
	ports := hubPorts(snap.Inputs)
	labels := c.hstate.InputLabels
	if kind == "output" {
		ports, labels = hubOutputs(snap.Outputs), c.hstate.OutputLabels
	}
	i, err := hubPort(kind, ports, c.args.Value(kind).(string))
	if err != nil {
		return nil, false, err
	}
	label := c.args.Value("label").(string)
	var cmd videohub.Command = videohub.InputLabel{Index: i, Label: label}
	if kind == "output" {
		cmd = videohub.OutputLabel{Index: i, Label: label}
	}
	r := CommandResult{Command: blockText(cmd), Args: c.displayArgs()}
	before, clean := labels[i], videohub.CleanLabel(label)
	values := func(after string) {
		c.note(Note{Values: []Value{{Param: kind, After: strconv.Itoa(i + 1)}, {Param: "label", Before: ptr(before), After: after}}})
	}
	if before == clean {
		r.Already, r.Command = true, ""
		values(clean)
		return r, true, nil
	}
	if err := c.hubSend(cmd); err != nil {
		if errors.Is(err, videohub.ErrRefused) {
			err = codeErr(CodeRejected, "the router refused the label for %s %d", kind, i+1)
		}
		return r, false, err
	}
	values(clean)
	return r, true, nil
}

// HubStatusResult is a router's status: what it is and its routing.
type HubStatusResult struct {
	Session  SessionStatus `json:"session"`
	Name     string        `json:"name"`
	Model    string        `json:"model"`
	Protocol string        `json:"protocol"`
	Inputs   int           `json:"inputs"`
	Outputs  int           `json:"outputs"`
	Locked   int           `json:"locked"` // outputs another client has locked
	Routes   []HubRoute    `json:"routes"`
}

// HubRoute is an output and the input routed to it.
type HubRoute struct {
	Output     table.Index  `json:"output"` // from 1
	Label      string       `json:"label"`
	Input      *table.Index `json:"input"` // nil: the hub hasn't said
	InputLabel string       `json:"input_label,omitempty"`
	// Lock is unlocked, locked (by another client: a route to it is
	// refused) or ours, from the wire's U, L and O; another letter as
	// the hub sent it.
	Lock       string `json:"lock"`
	LockLetter string `json:"lock_letter"`
}

var lockNames = map[string]string{"U": "unlocked", "L": "locked", "O": "ours"}

func hubRoutes(snap videohub.Snapshot) []HubRoute {
	routes := []HubRoute{}
	for _, o := range snap.Outputs {
		r := HubRoute{Output: table.Index(o.Index), Label: o.Label, Lock: lockNames[o.Lock], LockLetter: o.Lock}
		if r.Lock == "" {
			r.Lock = o.Lock
		}
		if o.Source != nil {
			r.Input = table.IndexPtr(*o.Source)
			r.InputLabel = inputText(snap, o.Source)
		}
		routes = append(routes, r)
	}
	return routes
}

func opHubStatus(c *call) (any, bool, error) {
	snap, err := c.hubSnapshot()
	if err != nil {
		return nil, false, err
	}
	r := HubStatusResult{Session: c.e.sessionStatus(c.host), Name: c.hstate.Name(), Model: snap.Device.ModelName,
		Protocol: snap.Device.ProtocolVersion, Inputs: snap.Device.VideoInputs, Outputs: snap.Device.VideoOutputs,
		Routes: hubRoutes(snap)}
	for _, rt := range r.Routes {
		if rt.LockLetter == "L" {
			r.Locked++
		}
	}
	return r, false, nil
}

// HubOutputs is a router's outputs, each with what is routed to it.
type HubOutputs struct {
	Outputs []HubRoute `json:"outputs"`
}

func opHubOutputs(c *call) (any, bool, error) {
	snap, err := c.hubSnapshot()
	if err != nil {
		return nil, false, err
	}
	return HubOutputs{Outputs: hubRoutes(snap)}, false, nil
}

// HubInputs is a router's inputs, each with the outputs it is routed
// to.
type HubInputs struct {
	Inputs []HubInput `json:"inputs"`
}

// HubInput is one input and where it goes.
type HubInput struct {
	HubPort
	Outputs []table.Index `json:"outputs"`
}

func opHubInputs(c *call) (any, bool, error) {
	snap, err := c.hubSnapshot()
	if err != nil {
		return nil, false, err
	}
	r := HubInputs{Inputs: []HubInput{}}
	for _, p := range hubPorts(snap.Inputs) {
		in := HubInput{HubPort: p, Outputs: []table.Index{}}
		for _, o := range snap.Outputs {
			if o.Source != nil && *o.Source == int(p.Number) {
				in.Outputs = append(in.Outputs, table.Index(o.Index))
			}
		}
		r.Inputs = append(r.Inputs, in)
	}
	return r, false, nil
}

func opHubState(c *call) (any, bool, error) {
	raw, err := json.Marshal(c.hub.State())
	if err != nil {
		return nil, false, err
	}
	if !c.args.Has("path") || c.args.Value("path") == "" {
		return json.RawMessage(raw), false, nil
	}
	sub, err := selectPath(raw, c.args.Value("path").(string))
	if err != nil {
		return nil, false, &Error{CodeUsage, err}
	}
	return sub, false, nil
}

func opHubDisconnect(c *call) (any, bool, error) {
	key, err := deviceKey("videohub", c.req.Host)
	if err != nil {
		return nil, false, err
	}
	c.e.mu.Lock()
	h := c.e.hosts[key]
	var s *videohub.Session
	if h != nil {
		s, _ = h.sess.(*videohub.Session)
	}
	c.e.mu.Unlock()
	if s != nil && s.Connected() {
		c.note(Note{Switcher: c.e.hubNote(h, s.State(), 0)})
	}
	closed, err := c.e.disconnectKey(key)
	return DisconnectResult{Host: key, Closed: closed}, false, err
}

// opHubWatch streams the blocks the router pushes: each a ChangeEvent
// with the block's key (routing, input labels, output labels, locks,
// device, protocol) and what it carried as its value, indices counted
// from 0 as the wire's (the CLI shows them from 1).
func opHubWatch(c *call) (any, bool, error) {
	ch, cancel := c.hub.Subscribe()
	defer cancel()
	limit := uint64(0)
	if c.args.Has("count") {
		limit = c.args.Value("count").(uint64)
	}
	if err := c.out.response(Response{OK: true, Result: ChangeEvent{Kind: "watching", Time: now()},
		TookMS: time.Since(c.start).Milliseconds()}); err != nil {
		return nil, false, err
	}
	var n uint64
	for {
		select {
		case <-c.ctx.Done():
			return streamEnd{}, false, nil
		case ch, ok := <-ch:
			if !ok {
				return nil, false, codeErr(CodeUnreachable, "the session closed")
			}
			ev := ChangeEvent{Kind: "state", Key: ch.Key, Value: ch.Values, Time: now()}
			if ch.Kind == videohub.ChangeDisconnected {
				ev = ChangeEvent{Kind: "disconnected", Time: now()}
			}
			if err := c.out.response(Response{OK: true, Result: ev, TookMS: time.Since(c.start).Milliseconds()}); err != nil {
				return streamEnd{}, false, nil
			}
			if n++; limit > 0 && n >= limit {
				return streamEnd{}, false, nil
			}
		}
	}
}
