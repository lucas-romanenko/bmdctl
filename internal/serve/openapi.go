// SPDX-License-Identifier: MIT

package serve

// The OpenAPI document, generated from the command table: a path per
// command, its arguments the table's parameters.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

type obj = map[string]any

func (a *api) openapi(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, OpenAPI(a.cfg.Registry, a.cfg.Version))
}

// responses are a command's answers: the engine's, as --json prints
// them.
func responses() obj {
	ref := obj{"$ref": "#/components/schemas/Response"}
	out := obj{"200": obj{"description": "done: confirmed, sent, or already held", "content": obj{"application/json": obj{"schema": ref}}}}
	for _, c := range []string{engine.CodeUsage, engine.CodeDestructive, engine.CodeRejected, engine.CodeNoEcho, engine.CodeUnreachable} {
		desc := map[string]string{
			engine.CodeUsage:       "usage: the request is wrong",
			engine.CodeDestructive: "destructive: send ?yes=true, once a person has agreed (confirm says what it would affect)",
			engine.CodeRejected:    "rejected: the device refused it",
			engine.CodeNoEcho:      "no_echo: sent, not confirmed in time",
			engine.CodeUnreachable: "unreachable: the device or the engine didn't answer",
		}[c]
		out[strconv.Itoa(Status(c))] = obj{"description": desc, "content": obj{"application/json": obj{"schema": ref}}}
	}
	return out
}

// OpenAPI is the API's OpenAPI 3.1 document.
func OpenAPI(reg *table.Registry, version string) obj {
	host := obj{"name": "host", "in": "path", "required": true, "schema": obj{"type": "string"},
		"description": "a host's name or alias in hosts.yml, in any case, or any unambiguous start of one; or an address (an ATEM's, unless the path names the device)"}
	query := func(name, typ, desc string) obj {
		return obj{"name": name, "in": "query", "schema": obj{"type": typ}, "description": desc}
	}
	opts := []any{host,
		query("yes", "boolean", "allow a destructive command: send it only once a person has agreed"),
		query("timeout", "string", "how long to wait for the device's confirmation, like 2s (default 5s)"),
		query("fps", "integer", "the frame rate for rates given as seconds:frames (default: the device's)")}
	paths := obj{
		"/hosts": obj{"get": obj{"operationId": "list_hosts", "summary": "The hosts in hosts.yml: name, alias, address, device, and the default.",
			"responses": obj{"200": obj{"description": "{ok, result: {default, hosts}}", "content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/Response"}}}}}}},
		"/hosts/{host}/state": obj{"get": obj{"operationId": "get_state", "summary": "The device's state, as the engine's session holds it.",
			"parameters": []any{host, query("path", "string", "a dotted key into the state: mes.0.program, transport.status")},
			"responses":  responses()}},
		"/hosts/{host}/events": obj{"get": obj{"operationId": "events", "summary": "The device's changes as server-sent events: each a data: line holding one of watch's frames, the first {\"kind\":\"watching\"}.",
			"parameters": []any{host, query("count", "integer", "stop after this many changes"),
				query("filter", "string", "an ATEM's: only keys starting with one of these, comma-separated (PrgI,KeOn:0)"),
				query("all", "boolean", "an ATEM's: include the clock and the tallies")},
			"responses": obj{"200": obj{"description": "text/event-stream", "content": obj{"text/event-stream": obj{"schema": obj{"type": "string"}}}}}}},
	}
	for _, d := range reg.Devices() {
		for _, e := range reg.Entries(d) {
			if e.Stream {
				continue // GET /hosts/{host}/events
			}
			p := "/hosts/{host}/" + strings.ReplaceAll(e.Path, " ", "/")
			if d != "atem" {
				p = "/hosts/{host}/" + d + "/" + strings.ReplaceAll(e.Path, " ", "/")
			}
			op := obj{"operationId": d + "_" + strings.NewReplacer(" ", "_", "-", "_").Replace(e.Path),
				"summary": e.Summary, "description": e.Describe(), "tags": []string{d},
				"parameters": opts, "responses": responses(),
				"x-destructive": e.Destructive, "x-unverified": e.Unverified}
			schema := e.InputSchema()
			hasFile := false
			for _, prm := range e.Params {
				if prm.Kind == table.File {
					hasFile = true
					prop := schema["properties"].(table.Schema)[prm.Name].(table.Schema)
					prop["format"] = "binary"
				}
			}
			ct := "application/json"
			if hasFile {
				ct = "multipart/form-data"
			}
			op["requestBody"] = obj{"required": len(schema["required"].([]string)) > 0, "content": obj{ct: obj{"schema": schema}}}
			if m, ok := paths[p].(obj); ok {
				m["post"] = op // beside GET /hosts/{host}/state
			} else {
				paths[p] = obj{"post": op}
			}
		}
	}
	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": "bmdctl", "version": version,
			"description": "Blackmagic Design devices through bmdctl's engine: every command of its table. " +
				"No authentication: it listens on 127.0.0.1 unless told otherwise. A host's device type decides which commands it takes; " +
				"a HyperDeck's are also at /hosts/{host}/hyperdeck/... . Indexes count from 1. " +
				"An answer is the engine's, as bmdctl --json prints it: {ok, confirmed, result, took_ms} or {ok: false, code, error}."},
		"servers": []any{obj{"url": "http://" + DefaultListen}},
		"paths":   paths,
		"components": obj{"schemas": obj{"Response": obj{"type": "object", "properties": obj{
			"ok":        obj{"type": "boolean"},
			"confirmed": obj{"type": "boolean", "description": "the device reported the change back; false: sent, unconfirmed"},
			"result":    obj{"description": "the command's result; a write's has already: true when the device held it and nothing was sent"},
			"took_ms":   obj{"type": "integer"},
			"code":      obj{"type": "string", "enum": []string{"usage", "unreachable", "engine", "stale", "no_echo", "rejected", "destructive"}},
			"error":     obj{"type": "string"},
			"files": obj{"type": "array", "description": "what the command wrote, base64",
				"items": obj{"type": "object", "properties": obj{"param": obj{"type": "string"}, "name": obj{"type": "string"}, "data": obj{"type": "string", "contentEncoding": "base64"}}}},
			"confirm": obj{"type": "object", "description": "what a destructive command refused for want of ?yes=true would affect",
				"properties": obj{"impact": obj{"type": "string"}, "question": obj{"type": "string"}}},
		}, "required": []string{"ok"}}}},
	}
}
