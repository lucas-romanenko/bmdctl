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

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeclock"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
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
	// A tool per command group, about fifteen.
	if len(names) < 10 || len(names) > 20 {
		t.Errorf("%d tools", len(names))
	}
	for _, n := range []string{"list_hosts", "wait_for_change", "atem_switching", "atem_usk", "atem_media", "atem_system", "atem_session",
		"atem_sources", "hyperdeck_transport", "hyperdeck_session"} {
		if names[n] == nil {
			t.Errorf("no tool %s", n)
		}
	}
	ann := func(n string) map[string]any { return names[n]["annotations"].(map[string]any) }
	if ann("atem_system")["destructiveHint"] != true || ann("atem_media")["destructiveHint"] != true ||
		ann("atem_switching")["destructiveHint"] != false || ann("atem_switching")["readOnlyHint"] != false ||
		ann("atem_session")["readOnlyHint"] != true || ann("atem_session")["destructiveHint"] != false {
		t.Errorf("annotations: %v %v %v", ann("atem_system"), ann("atem_switching"), ann("atem_session"))
	}
	schema := func(n string) map[string]any { return names[n]["inputSchema"].(map[string]any) }
	if p := schema("atem_system")["properties"].(map[string]any); p["confirm"] == nil || p["host"] == nil || p["mode"] == nil {
		t.Errorf("atem_system properties: %v", p)
	}
	if p := schema("atem_switching")["properties"].(map[string]any); p["confirm"] != nil || p["subcommand"] == nil {
		t.Errorf("atem_switching properties: %v", p)
	}
	if r := schema("atem_switching")["required"].([]any); len(r) != 1 || r[0] != "subcommand" {
		t.Errorf("atem_switching required: %v", r)
	}

	// Writes say how they went: confirmed with what changed, then already.
	res, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "program", "host": "studio", "source": "cam1"})
	if isErr || res["status"] != "confirmed" || res["subcommand"] != "program" ||
		!strings.Contains(text, `{"after":"CAM1","before":"CAM2","param":"source"}`) || !strings.Contains(text, `"subcommand":"program"`) {
		t.Errorf("program: %v %s", isErr, text)
	}
	if res, _, text := m.tool("atem_switching", map[string]any{"subcommand": "program", "host": "studio", "source": 1}); res["status"] != "already" {
		t.Errorf("program again: %s", text)
	}
	if res, _, text := m.tool("atem_dsk", map[string]any{"subcommand": "auto", "host": "studio"}); res["status"] != "sent" {
		t.Errorf("dsk auto: %s", text)
	}
	// A destructive subcommand does nothing without confirm, and says
	// what it would affect.
	res, isErr, text = m.tool("atem_system", map[string]any{"subcommand": "video-mode", "host": "studio", "mode": "525i5994"})
	if !isErr || res["code"] != "destructive" || !strings.Contains(text, "Not done: this is destructive. video mode 1080p50 → 525i5994") ||
		!strings.Contains(text, "call it again with confirm: true") {
		t.Errorf("video-mode: %s", text)
	}
	if len(b.sent("CVdM")) != 0 {
		t.Error("video mode sent without confirm")
	}
	if res, isErr, text := m.tool("atem_system", map[string]any{"subcommand": "video-mode", "host": "studio", "mode": "525i5994", "confirm": true}); isErr || res["status"] != "confirmed" {
		t.Errorf("video-mode confirmed: %s", text)
	}
	// Reads.
	if res, isErr, text := m.tool("atem_session", map[string]any{"subcommand": "state", "host": "studio", "path": "mes.0.program.source"}); isErr || string(mustJSON(res["result"])) != "1" {
		t.Errorf("state: %s", text)
	}
	if _, isErr, text := m.tool("atem_sources", map[string]any{"subcommand": "inputs", "host": "studio"}); isErr || !strings.Contains(text, "Camera 1") {
		t.Errorf("inputs: %.300s", text)
	}
	if _, isErr, text := m.tool("list_hosts", nil); isErr || !strings.Contains(text, `"Deck 1"`) {
		t.Errorf("list_hosts: %s", text)
	}
	// A deck's tools; a switcher's tool to a deck says whose it is.
	if res, isErr, text := m.tool("hyperdeck_transport", map[string]any{"subcommand": "play", "host": "deck", "speed": 50}); isErr || res["status"] != "confirmed" {
		t.Errorf("hyperdeck play: %s", text)
	}
	if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "cut", "host": "deck"}); !isErr || !strings.Contains(text, "is a HyperDeck: use the hyperdeck_ tools") {
		t.Errorf("cut on a deck: %s", text)
	}
	if res, _, text := m.tool("hyperdeck_session", map[string]any{"subcommand": "watch", "host": "deck", "seconds": 1}); res["status"] != "done" {
		t.Errorf("hyperdeck watch: %s", text)
	}
	// Files: sent from, and written to, this machine; replacing a used
	// slot needs confirm too.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "logo.png"), testPNG(t), 0o644)
	upload := map[string]any{"subcommand": "upload", "host": "studio", "slot": 2, "file": filepath.Join(dir, "logo.png")}
	if res, isErr, text := m.tool("atem_media", upload); isErr || res["status"] != "confirmed" {
		t.Errorf("upload: %s", text)
	}
	if res, isErr, text := m.tool("atem_media", upload); !isErr || res["code"] != "destructive" || !strings.Contains(text, "confirm: true") {
		t.Errorf("upload over a used slot: %s", text)
	}
	upload["confirm"] = true
	if res, isErr, text := m.tool("atem_media", upload); isErr || res["status"] != "confirmed" {
		t.Errorf("upload confirmed: %s", text)
	}
	out := filepath.Join(dir, "got.png")
	if res, isErr, text := m.tool("atem_media", map[string]any{"subcommand": "download", "host": "studio", "slot": 2, "file": out}); isErr || res["written"] == nil {
		t.Errorf("download: %s", text)
	}
	if b, err := os.ReadFile(out); err != nil || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Errorf("downloaded: %v", err)
	}
	// What a subcommand takes, and what there is.
	if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "program", "host": "studio", "nonsense": 1}); !isErr ||
		!strings.Contains(text, `atem_switching program takes no \"nonsense\"; it takes me, source`) {
		t.Errorf("unknown argument: %s", text)
	}
	if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "frobnicate", "host": "studio"}); !isErr ||
		!strings.Contains(text, `is not one of auto, aux, cut`) {
		t.Errorf("unknown subcommand: %s", text)
	}
	if _, isErr, text := m.tool("atem_switching", map[string]any{"host": "studio"}); !isErr || !strings.Contains(text, `subcommand \"\" is not one of`) {
		t.Errorf("no subcommand: %s", text)
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
	m.tool("atem_switching", map[string]any{"subcommand": "program", "host": "studio", "source": 5})
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
	m.tool("atem_switching", map[string]any{"subcommand": "preview", "host": "studio", "source": 3})
	n := m.note("notifications/resources/updated")["params"].(map[string]any)
	if n["uri"] != uri || n["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != float64(id) {
		t.Errorf("updated: %v", n)
	}
	m.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
	if r := m.request("ping", nil, true)["result"].(map[string]any); r["resultType"] != "complete" {
		t.Errorf("ping: %v", r)
	}
}

// A tool's host takes what --host does: a host's alias, its name, the
// start of either, or an address; list_hosts says each host's alias.
func TestMCPHostForms(t *testing.T) {
	b := newBench(t)
	b.net.Deck("192.0.2.20", fakehyperdeck.New())
	b.must("hosts", "add", "192.0.2.10", "--as", "BC 1.2 - TESTTt")
	b.must("hosts", "add", "192.0.2.20", "--as", "Deck One", "--device", "hyperdeck")
	b.must("hosts", "alias", "BC 1.2 - TESTTt", "bc")
	b.must("hosts", "alias", "deck one", "d1")
	m := b.mcp(t)
	m.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"}}, false)
	if _, _, text := m.tool("list_hosts", nil); !strings.Contains(text, `"alias":"bc"`) || !strings.Contains(text, `"alias":"d1"`) {
		t.Errorf("list_hosts: %s", text)
	}
	for _, host := range []string{"bc", "BC", "bc 1.2", "192.0.2.10"} {
		before := len(b.sent("DCut"))
		if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "cut", "host": host}); isErr {
			t.Errorf("host %q: %s", host, text)
		}
		if got := b.dialled(); len(b.sent("DCut")) != before+1 || len(got) != 1 || got[0] != "192.0.2.10:9910" {
			t.Errorf("host %q: dialled %v", host, got)
		}
	}
	// The deck by its alias and the start of it: an ATEM tool says what it is.
	for _, host := range []string{"d1", "D", "deck"} {
		if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "cut", "host": host}); !isErr ||
			!strings.Contains(text, "Deck One (192.0.2.20) is a HyperDeck: use the hyperdeck_ tools") {
			t.Errorf("host %q: %v %s", host, isErr, text)
		}
	}
	if _, isErr, text := m.tool("atem_switching", map[string]any{"subcommand": "cut", "host": "no such host!"}); !isErr || !strings.Contains(text, `"no such host!" is neither a host in hosts.yml nor an address`) {
		t.Errorf("no host: %v %s", isErr, text)
	}
}

// wait_for_change (#2): a cursor taken before a write, then a wait from
// it, has the write's change; a deck's changes come the same way.
func TestMCPWaitForChange(t *testing.T) {
	b := newBench(t)
	deck := fakehyperdeck.New()
	b.net.Deck(fakehyperdeck.Address, deck)
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("hosts", "add", fakehyperdeck.Address, "--as", "deck")
	m := b.mcp(t)
	m.request("initialize", map[string]any{"protocolVersion": "2025-11-25"}, false)
	res, isErr, text := m.tool("wait_for_change", map[string]any{"host": "studio", "timeout_s": 0})
	cursor, _ := res["cursor"].(string)
	if isErr || cursor == "" || res["timed_out"] != true {
		t.Fatalf("a cursor: %s", text)
	}
	m.tool("atem_switching", map[string]any{"subcommand": "program", "host": "studio", "source": 5})
	res, isErr, text = m.tool("wait_for_change", map[string]any{"host": "studio", "cursor": cursor, "timeout_s": 10, "filter": []string{"PrgI"}})
	if isErr || res["cursor"] == cursor || !strings.Contains(text, `"key":"PrgI:0","value":{"index":0,"source":5}`) || strings.Contains(text, `"TlSr"`) {
		t.Errorf("the program change: %s", text)
	}
	res, isErr, text = m.tool("wait_for_change", map[string]any{"host": "deck", "timeout_s": 0})
	if isErr || res["cursor"] == "" {
		t.Fatalf("a deck's cursor: %s", text)
	}
	m.tool("hyperdeck_transport", map[string]any{"subcommand": "play", "host": "deck"})
	if _, isErr, text := m.tool("wait_for_change", map[string]any{"host": "deck", "cursor": res["cursor"], "timeout_s": 10}); isErr || !strings.Contains(text, `"key":"transport"`) {
		t.Errorf("the deck's change: %s", text)
	}
}

// An MCP subscription is a watch: the session it uses isn't idle while
// it lasts, so a pinned one outlives the hour (#7), and closes an hour
// after the subscription ends.
func TestMCPSubscriptionKeepsSession(t *testing.T) {
	clock := fakeclock.New(time.Date(2026, 10, 11, 16, 55, 0, 0, time.UTC))
	b := newBenchConfig(t, fixtures.Bytes(t, "dump/initial-dump.bin"), engine.Config{Clock: clock})
	b.must("hosts", "add", "192.0.2.10", "--as", "studio")
	b.must("studio", "connect")
	session := func() *engine.SessionStatus {
		var st struct{ Result engine.Status }
		json.Unmarshal([]byte(b.must("engine", "status", "--json")), &st)
		if len(st.Result.Sessions) == 0 {
			return nil
		}
		return &st.Result.Sessions[0]
	}
	m := b.mcp(t)
	m.request("initialize", map[string]any{"protocolVersion": "2025-11-25"}, false)
	uri := "bmdctl://hosts/studio/state"
	m.request("resources/subscribe", map[string]any{"uri": uri}, false)
	eventually(t, "the subscription's watch", func() bool { s := session(); return s != nil && s.Busy == 1 })
	clock.Advance(3 * time.Hour)
	if s := session(); s == nil || s.Busy != 1 || !s.Pinned || s.Stale {
		t.Fatalf("subscribed three hours: %+v", s)
	}
	m.request("resources/unsubscribe", map[string]any{"uri": uri}, false)
	eventually(t, "the watch to end", func() bool { s := session(); return s != nil && s.Busy == 0 })
	clock.Advance(59 * time.Minute)
	if s := session(); s == nil || s.IdleS != 59*60 {
		t.Fatalf("59 minutes after: %+v", s)
	}
	clock.Advance(time.Minute)
	if s := session(); s != nil {
		t.Fatalf("an hour after: %+v", s)
	}
}
