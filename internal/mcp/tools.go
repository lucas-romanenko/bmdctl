// SPDX-License-Identifier: MIT

package mcp

// The tools: one per command-table entry, named <device>_<words>
// (atem_program, atem_usk_on_air, hyperdeck_play), their input schema
// the entry's (internal/table/schema.go) with the host to send to, and
// three of bmdctl's own: list_hosts, get_state, list_inputs.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
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

	entry   *table.Entry
	special string // list_hosts, get_state, list_inputs
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

// ToolName is an entry's tool name: atem_usk_on_air.
func ToolName(e table.Entry) string {
	return e.Device + "_" + strings.NewReplacer(" ", "_", "-", "_").Replace(e.Path)
}

var hostProp = table.Schema{"type": "string",
	"description": "the device: a host's name from list_hosts (any case, or the start of one) or an address; the default host if left out"}

func tools(reg *table.Registry) []tool {
	ro := func(title string) map[string]any {
		return map[string]any{"title": title, "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	}
	out := []tool{
		{Name: "list_hosts", Title: "List hosts", special: "list_hosts",
			Description: "The devices bmdctl knows (hosts.yml): each one's name, address and type (atem, hyperdeck, videohub, ultimatte), and the default host. Call it first to learn what a host argument can be.",
			InputSchema: table.Schema{"type": "object", "properties": table.Schema{}, "additionalProperties": false},
			Annotations: ro("List hosts")},
		{Name: "get_state", Title: "Get a device's state", special: "get_state",
			Description: "The state the engine's session holds of a device, as JSON, or one part of it by a dotted path. An ATEM's indexes count from 0 here (mes.0 is M/E 1), as the switcher's own state does; atem_status is the readable summary.",
			InputSchema: table.Schema{"type": "object", "properties": table.Schema{"host": hostProp,
				"path": table.Schema{"type": "string", "description": "a dotted key into the state: mes.0.program, inputs.1.name, transport.status (a HyperDeck)"}},
				"additionalProperties": false},
			Annotations: ro("Get a device's state")},
		{Name: "list_inputs", Title: "List an ATEM's inputs", special: "list_inputs",
			Description: "An ATEM's sources: id, short and long name, port type. A source argument takes any of these, or an alias (cam2, mp1, bars, black).",
			InputSchema: table.Schema{"type": "object", "properties": table.Schema{"host": hostProp,
				"all": table.Schema{"type": "boolean", "description": "include outputs, key masks and media player keys"}},
				"additionalProperties": false},
			Annotations: ro("List an ATEM's inputs")},
	}
	for _, d := range reg.Devices() {
		for _, e := range reg.Entries(d) {
			e := e
			schema := e.InputSchema()
			props := maps.Clone(schema["properties"].(table.Schema))
			props["host"] = hostProp
			for _, p := range e.Params {
				switch p.Kind {
				case table.File:
					props[p.Name] = withDesc(props[p.Name], "a path on this machine; the server reads the file and sends it")
				case table.OutFile, table.Dir:
					props[p.Name] = withDesc(props[p.Name], "a path on this machine the server writes to")
				}
			}
			if e.Destructive {
				props["yes"] = table.Schema{"type": "boolean",
					"description": "true only once the operator has agreed; without it nothing is done and the result says what it would affect"}
			}
			if e.Stream {
				props["seconds"] = table.Schema{"type": "integer", "minimum": 1, "maximum": 60,
					"description": "how long to collect changes for (default 5); count stops sooner"}
			}
			schema["properties"] = props
			name := engine.DeviceNames[d] + " " + e.Path
			readonly := readOnly[d+" "+e.Path]
			ann := map[string]any{"title": name, "readOnlyHint": readonly, "destructiveHint": e.Destructive, "openWorldHint": false}
			if readonly {
				ann["idempotentHint"] = true
			}
			desc := e.Describe()
			if e.Stream {
				desc += " As a tool it collects the changes for a few seconds and returns them; to follow a device, subscribe to its bmdctl://hosts/{host}/state resource."
			}
			out = append(out, tool{Name: ToolName(e), Title: name, Description: desc + " For " + article(d) + " host.",
				InputSchema: schema, Annotations: ann, entry: &e})
		}
	}
	return out
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
		text = fmt.Sprintf("Not done: this is destructive. %s. %s Ask the operator; only if they agree, call it again with yes: true.", c.Impact, c.Question)
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

// call runs a tool and returns its result.
func (s *Server) call(ctx context.Context, t *tool, args map[string]any) map[string]any {
	host, _ := args["host"].(string)
	target, err := s.cfg.Target(host)
	if err != nil && t.special != "list_hosts" {
		return toolError(engine.CodeUsage, err.Error(), nil)
	}
	switch t.special {
	case "list_hosts":
		f, err := s.cfg.Hosts()
		if err != nil {
			return toolError(engine.CodeUsage, err.Error(), nil)
		}
		return toolOK(map[string]any{"ok": true, "status": "done", "result": f})
	case "get_state":
		req := engine.Request{Device: deviceOr(target.Device), Host: target.Address, Path: "state"}
		if p, _ := args["path"].(string); p != "" {
			req.Params = map[string]string{"path": p}
		}
		return s.result(ctx, nil, req)
	case "list_inputs":
		if d := deviceOr(target.Device); d != "atem" {
			return toolError(engine.CodeUsage, fmt.Sprintf("%s is %s: it has no inputs (hyperdeck_clips lists a deck's clips)", target.Label, article(d)), nil)
		}
		req := engine.Request{Device: "atem", Host: target.Address, Path: "inputs", Params: map[string]string{}}
		if all, _ := args["all"].(bool); all {
			req.Params["all"] = "true"
		}
		return s.result(ctx, nil, req)
	}
	e := t.entry
	if target.Device != "" && target.Device != e.Device {
		return toolError(engine.CodeUsage, fmt.Sprintf("%s is %s: use the %s_ tools", target.Label, article(target.Device), target.Device), nil)
	}
	params, files := map[string]string{}, map[string][]byte{}
	for name, v := range args {
		if name == "host" || name == "yes" && e.Destructive || name == "seconds" && e.Stream {
			continue
		}
		var p *table.Param
		for i := range e.Params {
			if e.Params[i].Name == name {
				p = &e.Params[i]
			}
		}
		if p == nil {
			return toolError(engine.CodeUsage, fmt.Sprintf("%s takes no %q", t.Name, name), nil)
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
	yes, _ := args["yes"].(bool)
	req := engine.Request{Device: e.Device, Host: target.Address, Path: e.Path, Params: params, Files: files,
		Yes: yes, Ask: !yes, Notes: true}
	if e.Stream {
		return s.stream(ctx, req, args)
	}
	return s.result(ctx, e, req)
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
