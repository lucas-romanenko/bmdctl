// SPDX-License-Identifier: MIT

package mcp

// The tools: one per command group of the table (atem.Groups,
// hyperdeck.Groups: atem_switching, atem_usk, hyperdeck_transport, ...),
// the subcommand a parameter whose enum is the group's commands, the
// other parameters theirs; list_hosts; and wait_for_change (wait.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema table.Schema   `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`

	device  string
	subs    map[string]*table.Entry // by subcommand
	special string                  // list_hosts, wait_for_change
}

// readOnly are the entries that change nothing on a device: they read
// it, or the session to it, or (decode, encode) nothing at all.
var readOnly = map[string]bool{
	"atem status": true, "atem info": true, "atem state": true, "atem inputs": true, "atem show": true,
	"atem media list": true, "atem macro list": true, "atem video-modes": true, "atem time": true,
	"atem time-request": true, "atem audio meters": true, "atem classic-audio meters": true,
	"atem media download": true, "atem macro download": true, "atem profile save": true, "atem profile images": true,
	"atem macro decode": true, "atem macro encode": true, "atem watch": true, "atem connect": true, "atem disconnect": true,
	"hyperdeck status": true, "hyperdeck clips": true, "hyperdeck state": true, "hyperdeck watch": true,
	"hyperdeck connect": true, "hyperdeck disconnect": true,
}

// asksFirst are the entries that are not destructive but can be, by
// their arguments, and what then needs confirm.
var asksFirst = map[string]string{
	"atem media upload": "Over a used slot it needs confirm: true.",
	"atem macro upload": "Over a used slot it needs confirm: true.",
	"atem macro record": "Over a used slot it needs confirm: true.",
	"atem macro action": "The delete action needs confirm: true.",
}

// ToolName is a group's tool name: atem_switching.
func ToolName(device, group string) string {
	return device + "_" + strings.NewReplacer(" ", "_", "-", "_").Replace(group)
}

// Subcommand is an entry's subcommand in its group's tool: its path
// less the group's own word where it starts with it ("transition wipe"
// in transition is "wipe"), else its path ("ftb toggle" in switching).
func Subcommand(e table.Entry) string {
	g := e.GroupName()
	if rest, ok := strings.CutPrefix(e.Path, g+" "); ok {
		return rest
	}
	return e.Path
}

var hostProp = table.Schema{"type": "string",
	"description": "the device: a host's name or alias from list_hosts (any case, or the start of one) or an address; the default host if left out"}

func tools(reg *table.Registry) []tool {
	out := []tool{{Name: "list_hosts", Title: "List hosts", special: "list_hosts",
		Description: "The devices bmdctl knows (hosts.yml): each one's name, alias (a short name it goes by as well), address and type (atem, hyperdeck, videohub, ultimatte), and the default host. Call it first to learn what a host argument can be, and which tools a host takes: an atem host the atem_ tools, a hyperdeck host the hyperdeck_ tools.",
		InputSchema: table.Schema{"type": "object", "properties": table.Schema{}, "additionalProperties": false},
		Annotations: map[string]any{"title": "List hosts", "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}},
		waitTool}
	for _, d := range reg.Devices() {
		for _, g := range reg.Groups(d) {
			out = append(out, groupTool(d, g, reg.GroupEntries(d, g.Name)))
		}
	}
	return out
}

// variant is one schema a parameter has, the subcommands that give it
// so, and those of them that require it.
type variant struct {
	schema         table.Schema
	subs, required []string
}

// describe is a variant's schema, saying which subcommands take it
// (where not all of the tool's do) and which require it.
func (v variant) describe(all int) table.Schema {
	s := v.schema
	if len(v.subs) < all {
		s = withPrefix(s, "for "+strings.Join(v.subs, ", "))
	}
	switch {
	case len(v.required) == 0:
	case len(v.required) == all:
		s = withDesc(s, "required")
	default:
		s = withDesc(s, "required by "+strings.Join(v.required, ", "))
	}
	return s
}

// groupTool is a group's tool: its subcommands, the union of their
// parameters (each saying which subcommands take it, where not all do;
// one that differs between them a choice of its schemas), the host, and
// confirm where a subcommand is destructive or can be.
func groupTool(device string, g table.Group, es []table.Entry) tool {
	t := tool{Name: ToolName(device, g.Name), Title: engine.DeviceNames[device] + " " + g.Name, device: device,
		subs: map[string]*table.Entry{}}
	var names, lines, destructive, streams []string
	asks := false
	params := map[string][]variant{}
	var order []string
	allRead := true
	for i := range es {
		e := &es[i]
		sub := Subcommand(*e)
		names = append(names, sub)
		t.subs[sub] = e
		line := fmt.Sprintf("%s: %s. Usage: %s.", sub, strings.TrimSuffix(e.Summary, "."), e.Usage())
		if e.Destructive {
			destructive = append(destructive, sub)
			line += " Destructive: needs confirm: true."
		}
		if a := asksFirst[device+" "+e.Path]; a != "" {
			asks = true
			line += " " + a
		}
		if e.Unverified {
			line += " Unverified on hardware."
		}
		if e.Stream {
			streams = append(streams, sub)
			line += " Collects changes for seconds (default 5)."
		}
		lines = append(lines, line)
		allRead = allRead && readOnly[device+" "+e.Path]
		for _, p := range e.Params {
			ps := p.Schema()
			switch p.Kind {
			case table.File:
				ps = withDesc(ps, "a path on this machine; the server reads the file and sends it")
			case table.OutFile, table.Dir:
				ps = withDesc(ps, "a path on this machine the server writes to")
			}
			vs := params[p.Name]
			if vs == nil {
				order = append(order, p.Name)
			}
			k := slices.IndexFunc(vs, func(v variant) bool { return sameSchema(v.schema, ps) })
			if k < 0 {
				vs = append(vs, variant{schema: ps})
				k = len(vs) - 1
			}
			vs[k].subs = append(vs[k].subs, sub)
			if p.Required {
				vs[k].required = append(vs[k].required, sub)
			}
			params[p.Name] = vs
		}
	}
	props := table.Schema{
		"subcommand": table.Schema{"type": "string", "enum": names,
			"description": "what to do; the other arguments are the chosen subcommand's: " + strings.Join(names, ", ")},
		"host": hostProp,
	}
	for _, name := range order {
		vs := params[name]
		if len(vs) == 1 {
			props[name] = vs[0].describe(len(es))
			continue
		}
		var alts []any
		for _, v := range vs {
			alts = append(alts, v.describe(len(es)))
		}
		props[name] = table.Schema{"anyOf": alts, "description": "differs by subcommand: see each choice"}
	}
	if len(destructive) > 0 || asks {
		var what []string
		if len(destructive) > 0 {
			what = append(what, strings.Join(destructive, ", "))
		}
		if asks {
			what = append(what, "the subcommands whose line below says so")
		}
		props["confirm"] = table.Schema{"type": "boolean",
			"description": "true only once the operator has agreed, for this one call: " + strings.Join(what, " and ") +
				" do nothing without it, and say what they would affect"}
	}
	if len(streams) > 0 {
		props["seconds"] = table.Schema{"type": "integer", "minimum": 1, "maximum": 60,
			"description": "for " + strings.Join(streams, ", ") + ": how long to collect changes (default 5); count stops sooner"}
	}
	t.InputSchema = table.Schema{"type": "object", "properties": props, "required": []string{"subcommand"}, "additionalProperties": false}
	desc := []string{g.Summary, "For " + article(device) + " host. Subcommands:"}
	desc = append(desc, lines...)
	t.Description = strings.Join(desc, "\n")
	t.Annotations = map[string]any{"title": t.Title, "readOnlyHint": allRead, "destructiveHint": len(destructive) > 0, "openWorldHint": false}
	if allRead {
		t.Annotations["idempotentHint"] = true
	}
	return t
}

// sameSchema reports whether two parameter schemas say the same.
func sameSchema(a, b table.Schema) bool { return reflect.DeepEqual(a, b) }

func withPrefix(s table.Schema, prefix string) table.Schema {
	s = maps.Clone(s)
	if d, _ := s["description"].(string); d != "" {
		s["description"] = prefix + ": " + d
	} else {
		s["description"] = prefix
	}
	return s
}

func article(device string) string {
	return engine.DeviceArticle(device) + " " + engine.DeviceNames[device]
}

func withDesc(v any, more string) table.Schema {
	s := maps.Clone(v.(table.Schema))
	if d, _ := s["description"].(string); d != "" {
		more = d + "; " + more
	}
	s["description"] = more
	return s
}

// toolError is a tool's failure, as a result the model can read and act
// on (isError), not a protocol error.
func toolError(code, msg string, extra map[string]any) map[string]any {
	out := map[string]any{"ok": false, "status": "error", "code": code, "error": msg}
	maps.Copy(out, extra)
	text := "Error (" + code + "): " + msg
	if c, ok := extra["confirm"].(*engine.Confirm); ok {
		text = fmt.Sprintf("Not done: this is destructive. %s. %s Ask the operator; only if they agree, call it again with confirm: true.", c.Impact, c.Question)
	}
	b, _ := json.Marshal(out)
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text + "\n" + string(b)}},
		"structuredContent": out, "isError": true}
}

func toolOK(out map[string]any) map[string]any {
	b, _ := json.Marshal(out)
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}},
		"structuredContent": out, "isError": false}
}

// call runs a tool and returns its result: list_hosts, wait_for_change,
// or the subcommand of a group's tool, with its arguments.
func (s *Server) call(ctx context.Context, t *tool, args map[string]any) map[string]any {
	if t.special == "wait_for_change" {
		return s.waitForChange(ctx, args)
	}
	if t.special == "list_hosts" {
		f, err := s.cfg.Hosts()
		if err != nil {
			return toolError(engine.CodeUsage, err.Error(), nil)
		}
		return toolOK(map[string]any{"ok": true, "status": "done", "result": f})
	}
	sub, _ := args["subcommand"].(string)
	e := t.subs[sub]
	if e == nil {
		names := slices.Sorted(maps.Keys(t.subs))
		return toolError(engine.CodeUsage, fmt.Sprintf("%s: subcommand %q is not one of %s", t.Name, sub, strings.Join(names, ", ")), nil)
	}
	host, _ := args["host"].(string)
	target, err := s.cfg.Target(host)
	if err != nil {
		return toolError(engine.CodeUsage, err.Error(), nil)
	}
	if target.Device != "" && target.Device != e.Device {
		return toolError(engine.CodeUsage, fmt.Sprintf("%s is %s: use the %s_ tools", target.Label, article(target.Device), target.Device), nil)
	}
	params, files := map[string]string{}, map[string][]byte{}
	for name, v := range args {
		if name == "subcommand" || name == "host" || name == "confirm" || name == "seconds" && e.Stream {
			continue
		}
		var p *table.Param
		for i := range e.Params {
			if e.Params[i].Name == name {
				p = &e.Params[i]
			}
		}
		if p == nil {
			var takes []string
			for _, q := range e.Params {
				takes = append(takes, q.Name)
			}
			what := "nothing more"
			if len(takes) > 0 {
				what = strings.Join(takes, ", ")
			}
			return toolError(engine.CodeUsage, fmt.Sprintf("%s %s takes no %q; it takes %s", t.Name, sub, name, what), nil)
		}
		if v == nil {
			continue
		}
		str, ok := table.StringValue(v)
		if !ok {
			return toolError(engine.CodeUsage, fmt.Sprintf("%s: a string, a number, a boolean or a list", name), nil)
		}
		switch p.Kind {
		case table.File:
			if str == "-" {
				return toolError(engine.CodeUsage, name+": a path, not -", nil)
			}
			b, err := s.cfg.ReadFile(str)
			if err != nil {
				return toolError(engine.CodeUsage, err.Error(), nil)
			}
			files[name] = b
		case table.OutFile:
			if str == "-" {
				return toolError(engine.CodeUsage, name+": a path, not - (stdout carries the protocol)", nil)
			}
		}
		params[name] = str
	}
	confirm, _ := args["confirm"].(bool)
	req := engine.Request{Device: e.Device, Host: target.Address, Path: e.Path, Params: params, Files: files,
		Yes: confirm, Ask: !confirm, Notes: true}
	var out map[string]any
	if e.Stream {
		out = s.stream(ctx, req, args)
	} else {
		out = s.result(ctx, e, req)
	}
	return withSubcommand(out, sub)
}

// withSubcommand adds the subcommand to a result, in its structured
// content and the JSON that ends its text.
func withSubcommand(out map[string]any, sub string) map[string]any {
	sc, ok := out["structuredContent"].(map[string]any)
	if !ok {
		return out
	}
	sc["subcommand"] = sub
	b, _ := json.Marshal(sc)
	c := out["content"].([]any)[0].(map[string]any)
	text := c["text"].(string)
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		c["text"] = text[:i+1] + string(b)
	} else {
		c["text"] = string(b)
	}
	return out
}

// result runs a request and says how it went: ok, status (confirmed,
// sent, already, rejected for a write; done for a read), what changed,
// before and after, and the files written.
func (s *Server) result(ctx context.Context, e *table.Entry, req engine.Request) map[string]any {
	resp, fr, err := s.run(ctx, req)
	if err != nil {
		return toolError(engine.CodeOf(err), err.Error(), nil)
	}
	var device string
	var changed []map[string]any
	var confirm *engine.Confirm
	for _, n := range fr.notes {
		switch {
		case n.Switcher != nil:
			device = n.Switcher.Name
		case n.Values != nil:
			for _, v := range n.Values {
				c := map[string]any{"param": v.Param, "after": v.After}
				if v.Before != nil {
					c["before"] = *v.Before
				}
				if v.Kept {
					c["kept"] = true
				}
				changed = append(changed, c)
			}
		case n.Confirm != nil:
			confirm = n.Confirm
		}
	}
	if !resp.OK {
		extra := map[string]any{}
		if confirm != nil {
			extra["confirm"] = confirm
		}
		return toolError(resp.Code, resp.Error, extra)
	}
	out := map[string]any{"ok": true, "confirmed": resp.Confirmed, "result": resp.Result, "took_ms": resp.TookMS}
	if device != "" {
		out["device"] = device
	}
	status := "done"
	if e != nil && !readOnly[e.Device+" "+e.Path] {
		var r struct {
			Already bool `json:"already"`
		}
		raw, _ := resp.Result.(json.RawMessage)
		json.Unmarshal(raw, &r)
		switch {
		case r.Already:
			status = "already"
		case slicesAny(changed, func(c map[string]any) bool { return c["kept"] == true }):
			status = "rejected"
		case resp.Confirmed:
			status = "confirmed"
		default:
			status = "sent"
		}
	}
	out["status"] = status
	if len(changed) > 0 {
		out["changed"] = changed
	}
	var written []string
	for _, f := range fr.files {
		path, err := s.cfg.WriteFile(e, req.Params, f)
		if err != nil {
			return toolError(engine.CodeUsage, "writing the file: "+err.Error(), nil)
		}
		written = append(written, path)
	}
	if len(written) > 0 {
		out["written"] = written
	}
	return toolOK(out)
}

func slicesAny(cs []map[string]any, f func(map[string]any) bool) bool {
	for _, c := range cs {
		if f(c) {
			return true
		}
	}
	return false
}

// stream runs a streaming entry (watch) for seconds, or until count
// changes, and returns what came.
func (s *Server) stream(ctx context.Context, req engine.Request, args map[string]any) map[string]any {
	secs := 5.0
	if v, ok := args["seconds"].(float64); ok && v >= 1 && v <= 60 {
		secs = v
	}
	if _, ok := req.Params["count"]; !ok {
		req.Params["count"] = "20"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(secs*float64(time.Second)))
	defer cancel()
	events := []json.RawMessage{}
	var failed *engine.Response
	err := s.cfg.Call(ctx, req, func(f engine.Frame) error {
		if f.Response == nil {
			return nil
		}
		if !f.Response.OK {
			r := *f.Response
			failed = &r
			return nil
		}
		raw, _ := f.Response.Result.(json.RawMessage)
		if !strings.Contains(string(raw), `"kind":"watching"`) {
			events = append(events, raw)
		}
		return nil
	})
	if failed != nil {
		return toolError(failed.Code, failed.Error, nil)
	}
	if err != nil && ctx.Err() == nil {
		return toolError(engine.CodeOf(err), err.Error(), nil)
	}
	return toolOK(map[string]any{"ok": true, "status": "done", "events": events, "result": events})
}
