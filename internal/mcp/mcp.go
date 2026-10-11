// SPDX-License-Identifier: MIT

// Package mcp is bmdctl's Model Context Protocol server (bmdctl mcp): a
// tool per command group of the table, the command its subcommand
// argument, plus list_hosts, and each host's state as a resource a
// client can subscribe to, over stdio. It speaks both eras of the protocol: the
// current revision (2026-07-28: the version in every request's _meta,
// server/discover, subscriptions/listen) and the initialize-handshake
// revisions before it (2025-11-25 back to 2024-11-05: resources/subscribe),
// whichever the client opens with. docs/PORTING.md, "mcp", records the
// decisions; docs/AGENTS.md says how to add it to Claude Code and
// Claude Desktop.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Protocol versions: the current, per-request-metadata revision, and the
// handshake revisions a client may still initialize with.
const ModernVersion = "2026-07-28"

var legacyVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Meta keys of the current revision.
const (
	metaVersion      = "io.modelcontextprotocol/protocolVersion"
	metaServerInfo   = "io.modelcontextprotocol/serverInfo"
	metaSubscription = "io.modelcontextprotocol/subscriptionId"
)

// JSON-RPC and MCP error codes.
const (
	codeParse              = -32700
	codeInvalidRequest     = -32600
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
	codeInternal           = -32603
	codeUnsupportedVersion = -32022
	codeResourceNotFound   = -32002
)

// Target is the device a tool's host argument names.
type Target struct {
	Address, Device, Label string
}

// Config is what the server needs from bmdctl.
type Config struct {
	Registry *table.Registry
	// Call sends a request to the engine and passes each frame of its
	// reply to onFrame, until the reply ends or ctx does.
	Call func(ctx context.Context, req engine.Request, onFrame func(engine.Frame) error) error
	// Hosts reads hosts.yml.
	Hosts func() (*hosts.File, error)
	// Target resolves a host argument as --host does; "" is the host
	// commands go to without one.
	Target func(spec string) (Target, error)
	// ReadFile reads a file a command sends; WriteFile writes one a
	// command produced, for entry e with params, and returns its path.
	ReadFile  func(path string) ([]byte, error)
	WriteFile func(e *table.Entry, params map[string]string, f engine.FileFrame) (string, error)
	Version   string
	Logger    *slog.Logger
}

// Server serves one client on one stream.
type Server struct {
	cfg   Config
	tools []tool
	byKey map[string]*tool

	wmu sync.Mutex
	enc *json.Encoder

	mu        sync.Mutex
	legacy    bool                          // the client initialized
	inflight  map[string]context.CancelFunc // by request id
	cancelled map[string]bool               // request ids the client cancelled
	subs      map[string]context.CancelFunc // legacy resources/subscribe, by URI
	wg        sync.WaitGroup
}

// New returns a server for cfg.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{cfg: cfg, inflight: map[string]context.CancelFunc{}, cancelled: map[string]bool{}, subs: map[string]context.CancelFunc{}}
	s.tools = tools(cfg.Registry)
	s.byKey = map[string]*tool{}
	for i := range s.tools {
		s.byKey[s.tools[i].Name] = &s.tools[i]
	}
	return s
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// Serve reads requests from r and writes answers to w until r ends or ctx
// does. Each request runs on its own goroutine; answers go out whole,
// one per line.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.enc = json.NewEncoder(w)
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.wg.Wait()
	}()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m message
		if err := json.Unmarshal([]byte(line), &m); err != nil || m.JSONRPC != "2.0" {
			s.reply(nil, nil, &rpcError{Code: codeParse, Message: "not a JSON-RPC 2.0 message"})
			continue
		}
		if m.Method == "" {
			continue // a response: this server sends no requests
		}
		if len(m.ID) == 0 {
			s.notification(m)
			continue
		}
		rctx, rcancel := context.WithCancel(ctx)
		s.mu.Lock()
		s.inflight[string(m.ID)] = rcancel
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.inflight, string(m.ID))
				delete(s.cancelled, string(m.ID))
				s.mu.Unlock()
				rcancel()
			}()
			s.handle(rctx, m)
		}()
	}
	return sc.Err()
}

// write sends one message, whole.
func (s *Server) write(v any) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.enc.Encode(v); err != nil {
		s.cfg.Logger.Warn("mcp: write", "err", err)
	}
}

func (s *Server) reply(id json.RawMessage, result any, err *rpcError) {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if id == nil {
		out["id"] = nil
	}
	if err != nil {
		out["error"] = err
	} else {
		out["result"] = result
	}
	s.write(out)
}

func (s *Server) notify(method string, params any) {
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *Server) notification(m message) {
	switch m.Method {
	case "notifications/cancelled":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		json.Unmarshal(m.Params, &p)
		s.mu.Lock()
		if c, ok := s.inflight[string(p.RequestID)]; ok {
			s.cancelled[string(p.RequestID)] = true
			c()
		}
		s.mu.Unlock()
	case "notifications/initialized":
	}
}

// requestVersion is the protocol version a request's _meta declares, ""
// for none (a handshake-era request).
func requestVersion(params json.RawMessage) string {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	json.Unmarshal(params, &p)
	var v string
	json.Unmarshal(p.Meta[metaVersion], &v)
	return v
}

func supportedVersions() []string { return append([]string{ModernVersion}, legacyVersions...) }

// handle answers one request.
func (s *Server) handle(ctx context.Context, m message) {
	version := requestVersion(m.Params)
	modern := version != ""
	if modern && version != ModernVersion {
		s.reply(m.ID, nil, &rpcError{Code: codeUnsupportedVersion, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": supportedVersions(), "requested": version}})
		return
	}
	result, err := s.dispatch(ctx, m, modern)
	if err != nil {
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: codeInternal, Message: err.Error()}
		}
		s.reply(m.ID, nil, re)
		return
	}
	if result == nil {
		return // a stream that has answered for itself
	}
	if modern {
		if obj, ok := result.(map[string]any); ok {
			if _, set := obj["resultType"]; !set {
				obj["resultType"] = "complete"
			}
		}
	}
	s.reply(m.ID, result, nil)
}

func (s *Server) serverInfo() map[string]any {
	return map[string]any{"name": "bmdctl", "title": "bmdctl", "version": s.cfg.Version,
		"description": "Blackmagic Design ATEM switchers and HyperDeck recorders, through bmdctl's engine"}
}

// Instructions are what the server tells a model about using it.
const Instructions = `bmdctl controls Blackmagic Design devices on the local network: ATEM switchers and HyperDeck recorders. There is a tool per command group (atem_switching, atem_transition, atem_usk, atem_dsk, atem_media, ... and hyperdeck_transport, hyperdeck_session), each with a subcommand argument and that subcommand's arguments. list_hosts names the devices; a tool's host argument takes a host's name or an address, and leaving it out uses the default host. Read before you write: atem_session with subcommand status says what is on program and preview; tell the operator what is on air before a cut or an auto. Results say "status": confirmed (the device reported it back), sent (no confirmation exists for it), already (the device held it; nothing was sent) or rejected. Destructive subcommands refuse without confirm: true and say what they would affect: ask the operator and pass confirm only once they agree, for that one call. Indexes (M/Es, keyers, DSKs, aux outputs, media and macro slots) count from 1. Subscribe to bmdctl://hosts/{host}/state to hear of changes.`

func (s *Server) capabilities() map[string]any {
	return map[string]any{
		"tools":     map[string]any{"listChanged": false},
		"resources": map[string]any{"subscribe": true, "listChanged": false},
	}
}

// dispatch runs a method; a nil result with a nil error means it has
// answered for itself.
func (s *Server) dispatch(ctx context.Context, m message, modern bool) (any, error) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(m.Params, &p)
		v := legacyVersions[0]
		if slices.Contains(legacyVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		s.mu.Lock()
		s.legacy = true
		s.mu.Unlock()
		return map[string]any{"protocolVersion": v, "capabilities": s.capabilities(), "serverInfo": s.serverInfo(),
			"instructions": Instructions}, nil
	case "server/discover":
		return map[string]any{"supportedVersions": supportedVersions(), "capabilities": s.capabilities(),
			"_meta": map[string]any{metaServerInfo: s.serverInfo()}, "instructions": Instructions,
			"ttlMs": 3600000, "cacheScope": "public"}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		out := map[string]any{"tools": s.tools}
		if modern {
			out["ttlMs"], out["cacheScope"] = 3600000, "public"
		}
		return out, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		t := s.byKey[p.Name]
		if t == nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown tool: " + p.Name}
		}
		return s.call(ctx, t, p.Arguments), nil
	case "resources/list":
		out, err := s.resourceList()
		if err == nil && modern {
			out["ttlMs"], out["cacheScope"] = 5000, "private"
		}
		return out, err
	case "resources/templates/list":
		out := map[string]any{"resourceTemplates": []any{map[string]any{
			"uriTemplate": "bmdctl://hosts/{host}/state", "name": "state", "title": "A device's state",
			"description": "The state the engine's session holds of a host's device, as JSON", "mimeType": "application/json"}}}
		if modern {
			out["ttlMs"], out["cacheScope"] = 3600000, "public"
		}
		return out, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(m.Params, &p)
		out, err := s.read(ctx, p.URI)
		if err == nil && modern {
			out["ttlMs"], out["cacheScope"] = 0, "private"
		}
		return out, err
	case "resources/subscribe": // handshake era
		var p struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(m.Params, &p)
		if err := s.subscribe(p.URI); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	case "resources/unsubscribe":
		var p struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(m.Params, &p)
		s.mu.Lock()
		if c, ok := s.subs[p.URI]; ok {
			c()
			delete(s.subs, p.URI)
		}
		s.mu.Unlock()
		return map[string]any{}, nil
	case "subscriptions/listen": // the current revision
		return nil, s.listen(ctx, m)
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	case "logging/setLevel", "completion/complete":
		return map[string]any{}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + m.Method}
}

// stateURI is a host's state resource.
func stateURI(host string) string { return "bmdctl://hosts/" + host + "/state" }

func hostOfURI(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "bmdctl://hosts/")
	if !ok {
		return "", false
	}
	host, ok := strings.CutSuffix(rest, "/state")
	if !ok || host == "" {
		return "", false
	}
	return host, true
}

func (s *Server) resourceList() (map[string]any, error) {
	f, err := s.cfg.Hosts()
	if err != nil {
		return nil, err
	}
	res := []any{}
	for _, h := range f.Hosts {
		if s.cfg.Registry.Find(h.Device, "state") == nil {
			continue // a device with no commands yet
		}
		res = append(res, map[string]any{"uri": stateURI(h.Label()), "name": h.Label() + " state",
			"title":       fmt.Sprintf("%s (%s, %s)", h.Label(), h.Address, h.Device),
			"description": "What the engine's session holds of the device: subscribe to hear of each change",
			"mimeType":    "application/json"})
	}
	return map[string]any{"resources": res}, nil
}

func (s *Server) read(ctx context.Context, uri string) (map[string]any, error) {
	host, ok := hostOfURI(uri)
	if !ok {
		return nil, &rpcError{Code: codeResourceNotFound, Message: "Resource not found: " + uri}
	}
	t, err := s.cfg.Target(host)
	if err != nil {
		return nil, &rpcError{Code: codeResourceNotFound, Message: err.Error()}
	}
	resp, _, err := s.run(ctx, engine.Request{Device: deviceOr(t.Device), Host: t.Address, Path: "state"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, &rpcError{Code: codeInternal, Message: resp.Error}
	}
	text, _ := resp.Result.(json.RawMessage)
	return map[string]any{"contents": []any{map[string]any{"uri": uri, "mimeType": "application/json", "text": string(text)}}}, nil
}

func deviceOr(d string) string {
	if d == "" {
		return "atem"
	}
	return d
}

// watchURI streams a host's changes, calling changed (at most every
// 250 ms, so a burst is one notification) until ctx ends.
func (s *Server) watchURI(ctx context.Context, uri string, changed func()) error {
	host, ok := hostOfURI(uri)
	if !ok {
		return &rpcError{Code: codeResourceNotFound, Message: "Resource not found: " + uri}
	}
	t, err := s.cfg.Target(host)
	if err != nil {
		return &rpcError{Code: codeResourceNotFound, Message: err.Error()}
	}
	started := make(chan error, 1)
	var once sync.Once
	go func() {
		var last time.Time
		pending := false
		var timer *time.Timer
		var mu sync.Mutex
		err := s.cfg.Call(ctx, engine.Request{Device: deviceOr(t.Device), Host: t.Address, Path: "watch"}, func(f engine.Frame) error {
			if f.Response == nil {
				return nil
			}
			if !f.Response.OK {
				once.Do(func() { started <- errors.New(f.Response.Error) })
				return nil
			}
			var ev struct {
				Kind string `json:"kind"`
			}
			raw, _ := f.Response.Result.(json.RawMessage)
			json.Unmarshal(raw, &ev)
			if ev.Kind == "watching" {
				once.Do(func() { started <- nil })
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			if wait := 250*time.Millisecond - time.Since(last); wait > 0 {
				if !pending {
					pending = true
					timer = time.AfterFunc(wait, func() {
						mu.Lock()
						pending, last = false, time.Now()
						mu.Unlock()
						changed()
					})
				}
				return nil
			}
			last = time.Now()
			changed()
			return nil
		})
		if timer != nil {
			timer.Stop()
		}
		once.Do(func() {
			if err == nil {
				err = errors.New("the engine ended the watch")
			}
			started <- err
		})
	}()
	select {
	case err := <-started:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// subscribe is the handshake era's resources/subscribe: the stream runs
// until unsubscribe or the end of the session.
func (s *Server) subscribe(uri string) error {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if old, ok := s.subs[uri]; ok {
		old()
	}
	s.subs[uri] = cancel
	s.mu.Unlock()
	err := s.watchURI(ctx, uri, func() { s.notify("notifications/resources/updated", map[string]any{"uri": uri}) })
	if err != nil {
		cancel()
		s.mu.Lock()
		delete(s.subs, uri)
		s.mu.Unlock()
		var re *rpcError
		if errors.As(err, &re) {
			return re
		}
		return &rpcError{Code: codeInternal, Message: err.Error()}
	}
	return nil
}

// listen is the current revision's subscriptions/listen: acknowledged,
// then a notifications/resources/updated per change of each subscribed
// resource, until the client cancels it; at the end, the request's own
// answer.
func (s *Server) listen(ctx context.Context, m message) error {
	var p struct {
		Notifications struct {
			ResourceSubscriptions []string `json:"resourceSubscriptions"`
		} `json:"notifications"`
	}
	json.Unmarshal(m.Params, &p)
	var id any
	json.Unmarshal(m.ID, &id)
	meta := map[string]any{metaSubscription: id}
	var agreed []string
	for _, uri := range p.Notifications.ResourceSubscriptions {
		if _, ok := hostOfURI(uri); ok {
			agreed = append(agreed, uri)
		}
	}
	// Each watch runs before the acknowledgment, so no change after it
	// is missed; notifications wait for it, which comes first.
	acked := make(chan struct{})
	var watched []string
	for _, uri := range agreed {
		uri := uri
		if err := s.watchURI(ctx, uri, func() {
			<-acked
			s.notify("notifications/resources/updated", map[string]any{"_meta": meta, "uri": uri})
		}); err != nil {
			s.cfg.Logger.Warn("mcp: watch", "uri", uri, "err", err)
			continue
		}
		watched = append(watched, uri)
	}
	ack := map[string]any{}
	if len(watched) > 0 {
		ack["resourceSubscriptions"] = watched
	}
	s.notify("notifications/subscriptions/acknowledged", map[string]any{"_meta": meta, "notifications": ack})
	close(acked)
	<-ctx.Done()
	s.mu.Lock()
	byClient := s.cancelled[string(m.ID)]
	s.mu.Unlock()
	if !byClient {
		// The server is ending it: a graceful close. A cancelled one
		// gets nothing more.
		s.reply(m.ID, map[string]any{"resultType": "complete", "_meta": meta}, nil)
	}
	return nil
}

// run sends a request to the engine and returns its last response, the
// notes and files that came with it.
func (s *Server) run(ctx context.Context, req engine.Request) (engine.Response, frames, error) {
	var fr frames
	var resp *engine.Response
	err := s.cfg.Call(ctx, req, func(f engine.Frame) error {
		switch {
		case f.File != nil:
			fr.files = append(fr.files, *f.File)
		case f.Note != nil:
			fr.notes = append(fr.notes, *f.Note)
		case f.Response != nil:
			r := *f.Response
			resp = &r
		}
		return nil
	})
	if err == nil && resp == nil {
		err = errors.New("the engine hung up")
	}
	if err != nil {
		return engine.Response{}, fr, err
	}
	return *resp, fr, nil
}

type frames struct {
	files []engine.FileFrame
	notes []engine.Note
}
