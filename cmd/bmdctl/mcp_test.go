// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
)

// mcpClient talks to bmdctl mcp over pipes, as an agent's host does.
type mcpClient struct {
	t      *testing.T
	in     *io.PipeWriter
	mu     sync.Mutex
	next   int
	waits  map[string]chan map[string]any
	notes  []map[string]any
	noteCh chan struct{}
}

func (b *bench) mcp(t *testing.T) *mcpClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c, _, _ := b.cli("")
	c.stdin, c.stdout = inR, outW
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	done := make(chan struct{})
	go func() { c.run([]string{"mcp"}); outW.Close(); close(done) }()
	m := &mcpClient{t: t, in: inW, waits: map[string]chan map[string]any{}, noteCh: make(chan struct{}, 100)}
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
				t.Errorf("not JSON: %s", sc.Text())
				continue
			}
			m.mu.Lock()
			if id, ok := msg["id"]; ok && msg["method"] == nil {
				key, _ := json.Marshal(id)
				if ch := m.waits[string(key)]; ch != nil {
					ch <- msg
				}
			} else {
				m.notes = append(m.notes, msg)
				m.noteCh <- struct{}{}
			}
			m.mu.Unlock()
		}
	}()
	t.Cleanup(func() { inW.Close(); cancel(); <-done })
	return m
}

func (m *mcpClient) send(v any) {
	b, _ := json.Marshal(v)
	m.in.Write(append(b, '\n'))
}

// request sends a request and returns its answer; modern puts the
// current revision's _meta on it.
func (m *mcpClient) request(method string, params map[string]any, modern bool) map[string]any {
	m.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	if modern {
		params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	}
	m.mu.Lock()
	m.next++
	id := m.next
	ch := make(chan map[string]any, 1)
	m.waits[strings.TrimSpace(string(mustJSON(id)))] = ch
	m.mu.Unlock()
	m.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	select {
	case r := <-ch:
		return r
	case <-time.After(15 * time.Second):
		m.t.Fatalf("%s: no answer", method)
	}
	return nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// tool calls a tool and returns its structured result and whether it is
// an error.
func (m *mcpClient) tool(name string, args map[string]any) (map[string]any, bool, string) {
	m.t.Helper()
	r := m.request("tools/call", map[string]any{"name": name, "arguments": args}, false)
	res, ok := r["result"].(map[string]any)
	if !ok {
		m.t.Fatalf("%s: %v", name, r)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	return res["structuredContent"].(map[string]any), res["isError"] == true, text
}

// note waits for a notification of method.
func (m *mcpClient) note(method string) map[string]any {
	m.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		m.mu.Lock()
		for i, n := range m.notes {
			if n["method"] == method {
				m.notes = append(m.notes[:i], m.notes[i+1:]...)
				m.mu.Unlock()
				return n
			}
		}
		m.mu.Unlock()
		select {
		case <-m.noteCh:
		case <-deadline:
			m.t.Fatalf("no %s", method)
		}
	}
}

func TestMCPTools(t *testing.T) {
	b := newBench(t)
	b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", fakehyperdeck.Address)
	m := b.mcp(t)

	init := m.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"}}, false)["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "bmdctl" ||
		init["capabilities"].(map[string]any)["resources"].(map[string]any)["subscribe"] != true {
		t.Errorf("initialize: %v", init)
	}
	m.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	list := m.request("tools/list", nil, false)["result"].(map[string]any)["tools"].([]any)
	names := map[string]map[string]any{}
	valid := regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	for _, x := range list {
		tl := x.(map[string]any)
		n := tl["name"].(string)
		if names[n] != nil || !valid.MatchString(n) {
			t.Errorf("tool name %q", n)
		}
		names[n] = tl
		if tl["inputSchema"].(map[string]any)["type"] != "object" || tl["description"] == "" {
			t.Errorf("%s: %v", n, tl)
		}
	}
	for _, n := range []string{"list_hosts", "get_state", "list_inputs", "atem_program", "atem_usk_on_air", "atem_video_mode",
		"atem_media_upload", "hyperdeck_play", "hyperdeck_goto", "atem_watch"} {
		if names[n] == nil {
			t.Errorf("no tool %s", n)
		}
	}
	ann := func(n string) map[string]any { return names[n]["annotations"].(map[string]any) }
	if ann("atem_video_mode")["destructiveHint"] != true || ann("atem_program")["destructiveHint"] != false ||
		ann("atem_status")["readOnlyHint"] != true || ann("atem_program")["readOnlyHint"] != false {
		t.Errorf("annotations: %v %v %v", ann("atem_video_mode"), ann("atem_program"), ann("atem_status"))
	}
	if p := names["atem_video_mode"]["inputSchema"].(map[string]any)["properties"].(map[string]any); p["yes"] == nil || p["host"] == nil {
		t.Errorf("video_mode properties: %v", p)
	}

	// Writes say how they went: confirmed with what changed, then already.
	res, isErr, text := m.tool("atem_program", map[string]any{"host": "studio", "source": "cam1"})
	if isErr || res["status"] != "confirmed" || !strings.Contains(text, `{"after":"CAM1","before":"CAM2","param":"source"}`) {
		t.Errorf("program: %v %s", isErr, text)
	}
	if res, _, text := m.tool("atem_program", map[string]any{"host": "studio", "source": 1}); res["status"] != "already" {
		t.Errorf("program again: %s", text)
	}
	if res, _, text := m.tool("atem_dsk_auto", map[string]any{"host": "studio"}); res["status"] != "sent" {
		t.Errorf("dsk auto: %s", text)
	}
	// A destructive one does nothing without yes, and says what it would.
	res, isErr, text = m.tool("atem_video_mode", map[string]any{"host": "studio", "mode": "525i5994"})
	if !isErr || res["code"] != "destructive" || !strings.Contains(text, "Not done: this is destructive. video mode 1080p50 → 525i5994") ||
		!strings.Contains(text, "call it again with yes: true") {
		t.Errorf("video-mode: %s", text)
	}
	if len(b.sent("CVdM")) != 0 {
		t.Error("video mode sent without yes")
	}
	if res, isErr, text := m.tool("atem_video_mode", map[string]any{"host": "studio", "mode": "525i5994", "yes": true}); isErr || res["status"] != "confirmed" {
		t.Errorf("video-mode yes: %s", text)
	}
	// Reads, and bmdctl's own tools.
	if res, isErr, text := m.tool("get_state", map[string]any{"host": "studio", "path": "mes.0.program.source"}); isErr || string(mustJSON(res["result"])) != "1" {
		t.Errorf("get_state: %s", text)
	}
	if _, isErr, text := m.tool("list_inputs", map[string]any{"host": "studio"}); isErr || !strings.Contains(text, "Camera 1") {
		t.Errorf("list_inputs: %.300s", text)
	}
	if _, isErr, text := m.tool("list_hosts", nil); isErr || !strings.Contains(text, `"Deck 1"`) {
		t.Errorf("list_hosts: %s", text)
	}
	if _, isErr, text := m.tool("list_inputs", map[string]any{"host": "deck"}); !isErr || !strings.Contains(text, "is a HyperDeck: it has no inputs") {
		t.Errorf("list_inputs on a deck: %s", text)
	}
	// A deck's tools; a switcher's tool to a deck says whose it is.
	if res, isErr, text := m.tool("hyperdeck_play", map[string]any{"host": "deck", "speed": 50}); isErr || res["status"] != "confirmed" {
		t.Errorf("hyperdeck_play: %s", text)
	}
	if _, isErr, text := m.tool("atem_cut", map[string]any{"host": "deck"}); !isErr || !strings.Contains(text, "is a HyperDeck: use the hyperdeck_ tools") {
		t.Errorf("atem_cut on a deck: %s", text)
	}
	if res, _, text := m.tool("hyperdeck_watch", map[string]any{"host": "deck", "seconds": 1}); res["status"] != "done" {
		t.Errorf("hyperdeck_watch: %s", text)
	}
	// Files: sent from, and written to, this machine.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "logo.png"), testPNG(t), 0o644)
	if res, isErr, text := m.tool("atem_media_upload", map[string]any{"host": "studio", "slot": 2, "file": filepath.Join(dir, "logo.png")}); isErr || res["status"] != "confirmed" {
		t.Errorf("upload: %s", text)
	}
	out := filepath.Join(dir, "got.png")
	if res, isErr, text := m.tool("atem_media_download", map[string]any{"host": "studio", "slot": 2, "file": out}); isErr || res["written"] == nil {
		t.Errorf("download: %s", text)
	}
	if b, err := os.ReadFile(out); err != nil || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Errorf("downloaded: %v", err)
	}
	// What a tool takes, and what there is.
	if _, isErr, text := m.tool("atem_program", map[string]any{"host": "studio", "nonsense": 1}); !isErr || !strings.Contains(text, `takes no "nonsense"`) {
		t.Errorf("unknown argument: %s", text)
	}
	if r := m.request("tools/call", map[string]any{"name": "frobnicate"}, false); r["error"].(map[string]any)["code"] != float64(-32602) {
		t.Errorf("unknown tool: %v", r)
	}
}

func TestMCPResources(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	m := b.mcp(t)
	m.request("initialize", map[string]any{"protocolVersion": "2025-11-25"}, false)
	res := m.request("resources/list", nil, false)["result"].(map[string]any)["resources"].([]any)
	uri := "bmdctl://hosts/studio/state"
	if len(res) != 1 || res[0].(map[string]any)["uri"] != uri {
		t.Fatalf("resources: %v", res)
	}
	read := m.request("resources/read", map[string]any{"uri": uri}, false)["result"].(map[string]any)["contents"].([]any)
	if !strings.HasPrefix(read[0].(map[string]any)["text"].(string), `{"mes":[`) {
		t.Errorf("read: %.200v", read)
	}
	// The handshake era's subscription: a change is a notification.
	m.request("resources/subscribe", map[string]any{"uri": uri}, false)
	m.tool("atem_program", map[string]any{"host": "studio", "source": 5})
	if n := m.note("notifications/resources/updated"); n["params"].(map[string]any)["uri"] != uri {
		t.Errorf("updated: %v", n)
	}
	if r := m.request("resources/read", map[string]any{"uri": "bmdctl://nowhere"}, false); r["error"] == nil {
		t.Errorf("read nowhere: %v", r)
	}
}

// The current revision: no handshake, the version in every request's
// _meta, server/discover, subscriptions/listen.
func TestMCPModern(t *testing.T) {
	b := newBench(t)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	m := b.mcp(t)
	d := m.request("server/discover", nil, true)["result"].(map[string]any)
	if d["resultType"] != "complete" || !strings.Contains(string(mustJSON(d["supportedVersions"])), `"2026-07-28"`) ||
		d["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)["name"] != "bmdctl" {
		t.Errorf("discover: %v", d)
	}
	tl := m.request("tools/list", nil, true)["result"].(map[string]any)
	if tl["resultType"] != "complete" || tl["ttlMs"] == nil {
		t.Errorf("tools/list: %v", tl["resultType"])
	}
	r := m.request("tools/list", map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "1999-01-01"}}, false)
	if e := r["error"].(map[string]any); e["code"] != float64(-32022) || !strings.Contains(string(mustJSON(e["data"])), `"requested":"1999-01-01"`) {
		t.Errorf("unsupported: %v", r)
	}
	// A subscription: acknowledged, then a notification per change, each
	// with the subscription's id; cancelled, nothing more.
	uri := "bmdctl://hosts/studio/state"
	m.mu.Lock()
	m.next++
	id := m.next
	m.mu.Unlock()
	m.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "subscriptions/listen", "params": map[string]any{
		"_meta":         map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}},
		"notifications": map[string]any{"resourceSubscriptions": []string{uri}}}})
	ack := m.note("notifications/subscriptions/acknowledged")["params"].(map[string]any)
	if ack["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != float64(id) {
		t.Errorf("ack: %v", ack)
	}
	m.tool("atem_preview", map[string]any{"host": "studio", "source": 3})
	n := m.note("notifications/resources/updated")["params"].(map[string]any)
	if n["uri"] != uri || n["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != float64(id) {
		t.Errorf("updated: %v", n)
	}
	m.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
	if r := m.request("ping", nil, true)["result"].(map[string]any); r["resultType"] != "complete" {
		t.Errorf("ping: %v", r)
	}
}
