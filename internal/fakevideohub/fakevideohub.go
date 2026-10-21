// SPDX-License-Identifier: MIT

// Package fakevideohub is a Videohub router for tests: it speaks the
// Videohub Ethernet Protocol (TCP 9990) on any net.Conn, so
// videohub.Session, the engine and the CLI run end to end without a hub.
// Dial hands a session one end of a pipe; Listen serves loopback, for
// what dials a port itself (the device probe, doctor, a built bmdctl).
//
// It holds the routing, the labels and the output locks, sends each
// connection its preamble (ending END PRELUDE:), answers the blocks
// videohub sends with ACK or NAK as BMD's protocol document says, and
// pushes every change to every connection, the one that made it too, as
// a hub does. A lock taken by another client (LockOutput) reads L, and a
// route to that output is refused. docs/PORTING.md, "Fake Videohub",
// records it.
package fakevideohub

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Address is what tests dial: TEST-NET-1, so nothing real answers it.
const Address = "192.0.2.31"

// Hub is the fake router. Its fields are read when a connection opens;
// set them before serving.
type Hub struct {
	Model, Name, Version, UniqueID string
	// NoMarker leaves END PRELUDE: off the preamble, as older firmware
	// does.
	NoMarker bool

	mu       sync.Mutex
	inLabels []string
	outLabel []string
	routing  []int
	locked   []bool // by another client
	conns    map[*conn]bool
	commands []string
	silent   bool
}

// conn is one client's connection. What the hub sends it goes through
// out to its own writer, so a client that stops reading holds up nobody.
type conn struct {
	c   net.Conn
	out chan string
}

// send queues s for the client; past 4096 waiting it is dropped.
func (c *conn) send(s string) {
	select {
	case c.out <- s:
	default:
	}
}

func (c *conn) write() {
	for s := range c.out {
		if _, err := c.c.Write([]byte(s)); err != nil {
			return
		}
	}
}

// New returns a Smart Videohub 12G 16x16 named "Studio Router": inputs
// and outputs labelled, each output routed from the input with its
// number, nothing locked.
func New() *Hub {
	h := &Hub{Model: "Blackmagic Smart Videohub 12G 16x16", Name: "Studio Router", Version: "2.8",
		UniqueID: "7C2E0D0A1B2C", conns: map[*conn]bool{}}
	ins := []string{"Camera 1", "Camera 2", "Camera 3", "Camera 4", "Replay", "Graphics", "Program", "Preview",
		"Multiview", "Playback 1", "Playback 2", "Studio Feed", "Remote 1", "Remote 2", "Bars", "Black"}
	outs := []string{"Switcher 1", "Switcher 2", "Switcher 3", "Switcher 4", "Monitor A", "Monitor B", "Stream Encoder",
		"Recorder 1", "Recorder 2", "Projector", "Wall 1", "Wall 2", "Truck", "Spare 1", "Spare 2", "Test"}
	for i := range 16 {
		h.inLabels = append(h.inLabels, ins[i])
		h.outLabel = append(h.outLabel, outs[i])
		h.routing = append(h.routing, i)
		h.locked = append(h.locked, false)
	}
	return h
}

// Dial is a videohub.Dialer's DialContext: a connection to the hub, over
// a pipe, whatever the address.
func (h *Hub) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	client, server := net.Pipe()
	go h.Serve(server)
	return client, nil
}

// Listen serves the hub on addr (127.0.0.1:0 for any port) until the
// listener is closed.
func (h *Hub) Listen(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go h.Serve(c)
		}
	}()
	return l, nil
}

// Serve speaks the protocol on c until it closes.
func (h *Hub) Serve(c net.Conn) {
	cn := &conn{c: c, out: make(chan string, 4096)}
	go cn.write()
	h.mu.Lock()
	h.conns[cn] = true
	cn.send(h.preamble())
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, cn)
		close(cn.out)
		h.mu.Unlock()
		c.Close()
	}()
	r := bufio.NewReader(c)
	var buf []byte
	for {
		chunk := make([]byte, 4096)
		n, err := r.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for {
			i := bytes.Index(buf, []byte("\n\n"))
			if i < 0 {
				break
			}
			block := string(buf[:i])
			buf = buf[i+2:]
			h.handle(cn, block)
		}
		if err != nil {
			return
		}
	}
}

// preamble is the state dump a connection opens with. Called with mu.
func (h *Hub) preamble() string {
	var b strings.Builder
	fmt.Fprintf(&b, "PROTOCOL PREAMBLE:\nVersion: %s\n\n", h.Version)
	fmt.Fprintf(&b, "VIDEOHUB DEVICE:\nDevice present: true\nModel name: %s\nFriendly name: %s\nUnique ID: %s\n"+
		"Video inputs: %d\nVideo processing units: 0\nVideo outputs: %d\nVideo monitoring outputs: 0\nSerial ports: 0\n\n",
		h.Model, h.Name, h.UniqueID, len(h.inLabels), len(h.outLabel))
	b.WriteString(h.block("INPUT LABELS:", len(h.inLabels), func(i int) string { return h.inLabels[i] }))
	b.WriteString(h.block("OUTPUT LABELS:", len(h.outLabel), func(i int) string { return h.outLabel[i] }))
	b.WriteString(h.block("VIDEO OUTPUT LOCKS:", len(h.locked), h.lock))
	b.WriteString(h.block("VIDEO OUTPUT ROUTING:", len(h.routing), func(i int) string { return strconv.Itoa(h.routing[i]) }))
	b.WriteString("CONFIGURATION:\nTake Mode: false\n\n")
	if !h.NoMarker {
		b.WriteString("END PRELUDE:\n\n")
	}
	return b.String()
}

func (h *Hub) lock(i int) string {
	if h.locked[i] {
		return "L"
	}
	return "U"
}

func (h *Hub) block(header string, n int, value func(int) string) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	for i := range n {
		fmt.Fprintf(&b, "%d %s\n", i, value(i))
	}
	b.WriteString("\n")
	return b.String()
}

// handle answers one block from cn.
func (h *Hub) handle(cn *conn, block string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := strings.Split(block, "\n")
	header := strings.TrimSpace(lines[0])
	h.commands = append(h.commands, block)
	if h.silent {
		return
	}
	var body []string
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) != "" {
			body = append(body, l)
		}
	}
	reply := cn.send
	switch header {
	case "PING:":
		reply("ACK\n\n")
	case "VIDEO OUTPUT ROUTING:", "INPUT LABELS:", "OUTPUT LABELS:":
		if len(body) == 0 { // the request form: the block again
			reply("ACK\n\n")
			reply(h.whole(header))
			return
		}
		changes, ok := h.parse(header, body)
		if !ok {
			reply("NAK\n\n")
			return
		}
		reply("ACK\n\n")
		var b strings.Builder
		b.WriteString(header + "\n")
		for _, c := range changes {
			fmt.Fprintf(&b, "%d %s\n", c.i, c.v)
		}
		b.WriteString("\n")
		h.broadcast(b.String())
	default:
		reply("NAK\n\n")
	}
}

type change struct {
	i int
	v string
}

// parse checks and applies a block's lines: every index in range, every
// route's input in range and its output unlocked, or nothing changes.
func (h *Hub) parse(header string, body []string) ([]change, bool) {
	var cs []change
	for _, l := range body {
		f, rest, _ := strings.Cut(strings.TrimSpace(l), " ")
		i, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		switch header {
		case "VIDEO OUTPUT ROUTING:":
			src, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil || i < 0 || i >= len(h.routing) || src < 0 || src >= len(h.inLabels) || h.locked[i] {
				return nil, false
			}
		case "INPUT LABELS:":
			if i < 0 || i >= len(h.inLabels) {
				return nil, false
			}
		case "OUTPUT LABELS:":
			if i < 0 || i >= len(h.outLabel) {
				return nil, false
			}
		}
		cs = append(cs, change{i, strings.TrimSpace(rest)})
	}
	for _, c := range cs {
		switch header {
		case "VIDEO OUTPUT ROUTING:":
			h.routing[c.i], _ = strconv.Atoi(c.v)
		case "INPUT LABELS:":
			h.inLabels[c.i] = c.v
		case "OUTPUT LABELS:":
			h.outLabel[c.i] = c.v
		}
	}
	return cs, true
}

// whole is a block as the preamble sends it. Called with mu.
func (h *Hub) whole(header string) string {
	switch header {
	case "INPUT LABELS:":
		return h.block(header, len(h.inLabels), func(i int) string { return h.inLabels[i] })
	case "OUTPUT LABELS:":
		return h.block(header, len(h.outLabel), func(i int) string { return h.outLabel[i] })
	}
	return h.block(header, len(h.routing), func(i int) string { return strconv.Itoa(h.routing[i]) })
}

// broadcast sends a block to every connection. Called with mu.
func (h *Hub) broadcast(s string) {
	for c := range h.conns {
		c.send(s)
	}
}

// Route routes input src to output dest as another client (a panel)
// would: the hub pushes it to every connection.
func (h *Hub) Route(dest, src int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routing[dest] = src
	h.broadcast(fmt.Sprintf("VIDEO OUTPUT ROUTING:\n%d %d\n\n", dest, src))
}

// LockOutput locks an output, or unlocks it, as another client would:
// it reads L (U) and a route to it is refused.
func (h *Hub) LockOutput(dest int, locked bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.locked[dest] = locked
	h.broadcast(fmt.Sprintf("VIDEO OUTPUT LOCKS:\n%d %s\n\n", dest, h.lock(dest)))
}

// Silence makes the hub take blocks and answer none.
func (h *Hub) Silence(on bool) { h.mu.Lock(); h.silent = on; h.mu.Unlock() }

// Drop closes every connection.
func (h *Hub) Drop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		c.c.Close()
	}
}

// Commands are the blocks the hub has been sent, without their blank
// line.
func (h *Hub) Commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.commands...)
}

// Routing is the hub's routing: output → input, 0-based.
func (h *Hub) Routing() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.routing...)
}

// Labels are the hub's input and output labels.
func (h *Hub) Labels() (inputs, outputs []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.inLabels...), append([]string(nil), h.outLabel...)
}
