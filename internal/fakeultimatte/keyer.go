// SPDX-License-Identifier: MIT

package fakeultimatte

// Keyer is ultimattewire's own two fakes (bmdwire v1.14.0,
// tests/fake_ultimatte.py and tests/fake_captured_4k.py) for
// ultimatte.Session: a unit speaking the text protocol as Blackmagic's
// "Ultimatte 12 Ethernet Protocol" v2.0 describes it and nothing more,
// and, from NewCaptured4K, a 12 4K doing what a real one (software 2.3)
// did in captures of Software Control on 2026-11-11 and -12 where that
// differs from the document. Unit, above, stays the archive's and the
// network's fake.
//
// On every connection to 9998 a Keyer sends the documented status dump
// in the documented order, then answers every documented block (status
// dump requests, CONTROL with its offsets and functions, FILE, GPI,
// FRAME BUFFER, CAMERACONTROL on the HD Mini, NETWORK INTERFACE 0): ACK
// or NAK, then the update, pushed to every connected client. Where the
// document is silent it picks what ultimattewire's fake picks: a value it
// won't take is a NAK; a FILE or GPI command it can't carry out is ACK
// then a MESSAGE (texts made up but for Event limit exceeded); values go
// out in lower case. Nothing here is the unit.
//
// The captured 12 4K also takes the 0x0101 image write on 9996, names
// images per buffer in FRAME BUFFER, sends IMAGE LIST with Capacity: and
// Available:, CURRENT FILE as Name: and Status:, takes Image: / Delete:
// and FLUSH SETTINGS, echoes every CONTROL write and marks CURRENT FILE
// Modified, streams the colour under a cursor that is on, refuses a
// cursor move with its cursor off, and runs Sample Wall / Sample Floor as
// the unit did: ACK, silence, then the cursor off, the new colours and a
// whole CONTROL DEFAULT, never yes.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

// Models are the Keyer's models by the short names ultimattewire's fake
// takes.
var Models = map[string]string{"8K": ultimatte.Model8K, "4K": ultimatte.Model4K,
	"HD": ultimatte.ModelHD, "HD Mini": ultimatte.ModelHDMini}

// deviceInputs are the DEVICE lines per model, from the document's note
// (the 8K example has all nine and spells the reference REFIn).
var (
	deviceInputs = map[string][]string{
		ultimatte.ModelHDMini: {"FG In", "BG In"},
		ultimatte.ModelHD:     {"FG In", "BG In", "MONITOR In", "G MATTE In", "H MATTE In", "REFIn"},
	}
	allInputs = []string{"FG In", "BG In", "MONITOR In", "G MATTE In", "H MATTE In",
		"REFIn", "BG MATTE In", "LAYER In", "LAYER MATTE In"}
)

// GPIEventLimit is the fake's limit on a GPI's events; the document only
// names the warning.
const GPIEventLimit = 4

// Capacity is the captured 12 4K's IMAGE LIST Capacity; the pool counts
// space in 4 KiB blocks.
const (
	Capacity  = 6736052224
	poolBlock = 4096
	locked    = "Control Locked - Enable 'Cursor' function"
)

var (
	namedBuffers = []string{"Background 1", "Background 2", "Layer 1", "Layer 2", "Garbage Matte", "Holdout Matte"}
	cursors      = map[string][2]string{"Wall": {"Cursor X", "Cursor Y"}, "Floor": {"Cursor 2 X", "Cursor 2 Y"}}
	inputOf      = map[string]string{"Background 1": "BM", "Background 2": "BM", "Garbage Matte": "GM", "Holdout Matte": "HM"}
)

// ordered is a Python dict of strings: insertion order kept.
type ordered struct {
	keys []string
	m    map[string]string
}

func newOrdered() *ordered { return &ordered{m: map[string]string{}} }

func (o *ordered) set(k, v string) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *ordered) clone() *ordered {
	return &ordered{keys: slices.Clone(o.keys), m: func() map[string]string {
		m := map[string]string{}
		for k, v := range o.m {
			m[k] = v
		}
		return m
	}()}
}

func (o *ordered) lines() []string {
	out := make([]string, len(o.keys))
	for i, k := range o.keys {
		out[i] = k + ": " + o.m[k]
	}
	return out
}

type gpiState struct {
	index  int
	events []string
}

type poolImage struct {
	w, h int
	data []byte
}

// peer is one connected client: what is sent to it goes through a queue
// its own goroutine writes, so a push never waits on a client that isn't
// reading (a pipe has no buffer).
type peer struct {
	conn net.Conn
	out  chan []byte
	done chan struct{}
}

func newPeer(c net.Conn) *peer {
	p := &peer{conn: c, out: make(chan []byte, 1<<16), done: make(chan struct{})}
	go func() {
		for {
			select {
			case b := <-p.out:
				if _, err := c.Write(b); err != nil {
					c.Close()
					return
				}
			case <-p.done:
				return
			}
		}
	}()
	return p
}

func (p *peer) send(s string) {
	select {
	case p.out <- []byte(s):
	case <-p.done:
	}
}

// Keyer is one fake unit. Set its exported fields before a client
// connects; the methods are safe from any goroutine.
type Keyer struct {
	mu sync.Mutex

	Model, Protocol, Label, VideoFormat string
	// CloseOnConnect closes every 9998 connection at once; StallAfter
	// (when >= 0) sends that many bytes of the dump, then nothing; Mute
	// takes blocks and answers nothing; FunctionSilent ACKs a function
	// and never says yes.
	CloseOnConnect bool
	StallAfter     int
	Mute           bool
	FunctionSilent bool

	controls, defaults *ordered
	currentFile        string
	files              []string
	gpis               map[int]*gpiState
	images             []string
	fbCount            int
	fb                 *ordered
	cameraID           string
	inputs             []string
	received           []string
	peers              map[*peer]bool
	stopped            bool

	// The captured 12 4K's.
	captured        bool
	FGLocked        bool
	pool            map[string]poolImage
	poolOrder       []string
	named           *ordered
	flushes         int
	uploads         [][]byte
	fileStatus      string
	StreamInterval  time.Duration
	CursorEchoDelay time.Duration
	SampleSilence   time.Duration
	SilentControls  map[string]bool
	sampling        bool
	stop            chan struct{}
	streaming       sync.Once
}

func defaultValue(c ultimatte.Control) string {
	switch c.Kind {
	case ultimatte.KindRange:
		return strconv.Itoa(*c.Min)
	case ultimatte.KindBool, ultimatte.KindReadOnly:
		return "off"
	case ultimatte.KindFunction:
		return "no"
	}
	return strings.ToLower(c.Choices[0])
}

func protocolOf(p string) [2]int {
	a, b, _ := strings.Cut(p, ".")
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	return [2]int{x, y}
}

// NewKeyer is FakeUltimatte(model, protocol): model "8K", "4K", "HD" or
// "HD Mini", protocol "2.0" or older ("1.2" has no Output Range).
func NewKeyer(model, protocol string) *Keyer {
	m := Models[model]
	k := &Keyer{Model: m, Protocol: protocol, Label: m, VideoFormat: "1080p60", StallAfter: -1,
		controls: newOrdered(), currentFile: "Ultimatte Defaults", files: []string{"Studio A", "Studio B"},
		gpis: map[int]*gpiState{}, images: []string{"Blue Sky", "Newsroom"}, fbCount: 46, fb: newOrdered(),
		cameraID: "1", peers: map[*peer]bool{}}
	if m == ultimatte.Model8K {
		k.VideoFormat = "2160p60"
	}
	p := protocolOf(protocol)
	for _, c := range ultimatte.Controls {
		if !c.AvailableOn(m) {
			continue
		}
		if c.MinProtocol != nil && (p[0] < c.MinProtocol[0] || p[0] == c.MinProtocol[0] && p[1] < c.MinProtocol[1]) {
			continue
		}
		k.controls.set(c.Name, defaultValue(c))
	}
	k.controls.set("Matte Density", "5000")
	k.controls.set("Backing Color", "green")
	k.controls.set("Video Format", "auto detect")
	k.controls.set("Monitor Out", "program")
	k.defaults = k.controls.clone()
	if m != ultimatte.ModelHDMini {
		for i := 1; i <= 5; i++ {
			k.gpis[i] = &gpiState{events: []string{}}
		}
		k.gpis[1].events = []string{"Studio A", "Studio B"}
	}
	for _, n := range []string{"BG 1", "LY 1", "BG 2", "LY 2", "GM", "HM"} {
		k.fb.set(n+" Frame Buffer Enable", "off")
		k.fb.set(n+" Frame Buffer Index", "0")
	}
	for _, kv := range [][2]string{{"BG Frame Buffer Mix", "0"}, {"LY Frame Buffer Mix", "0"},
		{"BG Transition Duration", "0"}, {"LY Transition Duration", "0"},
		{"Frame Buffer 1", "Blue Sky"}, {"Frame Buffer 2", "Newsroom"}} {
		k.fb.set(kv[0], kv[1])
	}
	k.inputs = allInputs
	if in, ok := deviceInputs[m]; ok {
		k.inputs = in
	}
	return k
}

// NewCaptured4K is FakeCaptured4K: a 12 4K at 1080p25 with an empty
// pool, the cursors where the capture had them, and the timings scaled
// down (a 40 ms colour stream as on the unit, a 50 ms cursor echo where
// the unit took 550, a 0.3 s sample silence where it took 4 s). Set its
// fields before the first Dial, which starts the colour stream; Close
// stops it.
func NewCaptured4K() *Keyer {
	k := NewKeyer("4K", "2.0")
	k.captured = true
	k.VideoFormat = "1080p25"
	k.FGLocked = true
	k.pool = map[string]poolImage{}
	k.named = newOrdered()
	for _, n := range namedBuffers {
		k.named.set(n, "")
	}
	k.fileStatus = "Clean"
	for _, kv := range [][2]string{{"Matte Density", "2500"}, {"Cursor X", "192"}, {"Cursor Y", "108"},
		{"Cursor 2 X", "1728"}, {"Cursor 2 Y", "972"}} {
		k.controls.set(kv[0], kv[1])
	}
	k.defaults = k.controls.clone()
	k.StreamInterval = 40 * time.Millisecond
	k.CursorEchoDelay = 50 * time.Millisecond
	k.SampleSilence = 300 * time.Millisecond
	k.SilentControls = map[string]bool{}
	k.stop = make(chan struct{})
	return k
}

// Close drops every client and stops the colour stream; later dials are
// refused, as a unit switched off.
func (k *Keyer) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return
	}
	k.stopped = true
	for p := range k.peers {
		close(p.done)
		p.conn.Close()
	}
	k.peers = map[*peer]bool{}
	if k.stop != nil {
		close(k.stop)
	}
}

// Dial is an ultimatte.Client's DialContext: 9998 (and, for the captured
// 12 4K, 9996) over a pipe; anything else, or a closed keyer, refused.
func (k *Keyer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	k.mu.Lock()
	stopped, captured := k.stopped, k.captured
	k.mu.Unlock()
	if captured && !stopped {
		// Started here rather than in NewCaptured4K, so a test sets the
		// timings before anything reads them.
		k.streaming.Do(func() { go k.stream() })
	}
	if stopped || port != 9998 && !(port == 9996 && captured) {
		to, _ := net.ResolveTCPAddr("tcp", addr)
		return nil, &net.OpError{Op: "dial", Net: network, Addr: to, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	}
	client, server := net.Pipe()
	if port == 9998 {
		go k.session(server)
	} else {
		go k.mediaSession(server)
	}
	return client, nil
}

// Received is every block a client sent, as text (a status dump request
// is its bare header line).
func (k *Keyer) Received() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.received)
}

// Sent is Received without the status dump requests.
func (k *Keyer) Sent() []string {
	var out []string
	for _, b := range k.Received() {
		if strings.Contains(b, "\n") {
			out = append(out, b)
		}
	}
	return out
}

// Control is a control's value as the unit holds it.
func (k *Keyer) Control(name string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.controls.m[name]
}

// AddFile adds a file without telling anyone, as a change the unit
// doesn't push.
func (k *Keyer) AddFile(name string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.files = append(k.files, name)
}

// SetFGLocked locks the FG input or not (the captured 12 4K refuses an
// assignment and a flush without one).
func (k *Keyer) SetFGLocked(on bool) { k.mu.Lock(); k.FGLocked = on; k.mu.Unlock() }

// SetMute makes the keyer take blocks and answer nothing.
func (k *Keyer) SetMute(on bool) { k.mu.Lock(); k.Mute = on; k.mu.Unlock() }

// Flushes is how many FLUSH SETTINGS the captured 12 4K took.
func (k *Keyer) Flushes() int { k.mu.Lock(); defer k.mu.Unlock(); return k.flushes }

// Uploads are the raw 9996 frames the captured 12 4K received.
func (k *Keyer) Uploads() [][]byte { k.mu.Lock(); defer k.mu.Unlock(); return slices.Clone(k.uploads) }

// Pool is the captured 12 4K's media pool: each image's bytes.
func (k *Keyer) Pool() map[string][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := map[string][]byte{}
	for n, im := range k.pool {
		out[n] = im.data
	}
	return out
}

// PutImage puts an image in the captured 12 4K's pool and, with buffer,
// on that named buffer (Background 1, ...), without telling anyone.
func (k *Keyer) PutImage(name string, w, h int, buffer string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.addToPool(name, poolImage{w, h, nil})
	if buffer != "" {
		k.named.set(buffer, name)
	}
}

func (k *Keyer) addToPool(name string, im poolImage) {
	if _, ok := k.pool[name]; !ok {
		k.poolOrder = append(k.poolOrder, name)
	}
	k.pool[name] = im
}

func render(header string, body []string) string {
	var b strings.Builder
	b.WriteString(header + ":\n")
	for _, ln := range body {
		b.WriteString(ln + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// block is the full status block header as the unit would send it; ok
// false for one it doesn't have.
func (k *Keyer) block(h string) (string, bool) {
	if k.captured {
		switch h {
		case "DEVICE":
			fg := "Not Locked"
			if k.FGLocked {
				fg = "Locked"
			}
			return render(h, []string{"Video Format: " + k.VideoFormat, "Reference Source: Not Locked",
				"FG In: " + fg, "BG In: Not Locked"}), true
		case "CURRENT FILE":
			return render(h, []string{"Name: " + k.currentFile, "Status: " + k.fileStatus}), true
		case "IMAGE LIST":
			used := 0
			for _, im := range k.pool {
				used += (len(im.data) + poolBlock - 1) / poolBlock * poolBlock
			}
			return render(h, append([]string{fmt.Sprintf("Capacity: %d", Capacity),
				fmt.Sprintf("Available: %d", Capacity-13287424-used)}, k.poolOrder...)), true
		case "FRAME BUFFER":
			return render(h, append(k.named.lines(), "Background Mix: 0", "Layer Mix: 0",
				"Background Transition Duration: 0", "Layer Transition Duration: 0")), true
		}
	}
	var body []string
	switch h {
	case "PROTOCOL PREAMBLE":
		body = []string{"Version: " + k.Protocol}
	case "IDENTITY":
		body = []string{"Model: " + k.Model, "Label: " + k.Label, "Unique ID: 12345678"}
	case "NETWORK":
		body = []string{"Interface count: 1", "Default Interface: 0",
			"Static DNS Servers: 8.8.8.8, 8.8.4.4", "Current DNS Servers: 8.8.8.8, 8.8.4.4"}
	case "NETWORK INTERFACE 0":
		body = []string{"Name: Cadence GigE Ethernet MAC", "Priority: 0",
			"MAC Address: 7c:2e:0d:00:00:01", "DynamicIP: false",
			"Current Addresses: 10.0.0.2/255.255.255.0", "Current Gateway: 10.0.0.1",
			"Static Addresses: 10.0.0.2/255.255.255.0", "Static Gateway: 10.0.0.1"}
	case "VERSION":
		body = []string{"Product ID: BE84", "Hardware Version: 0100",
			"Software Version: 09A89B7A", "Software Release: " + k.Protocol}
	case "DEVICE":
		body = []string{"Video Format: " + k.VideoFormat, "Reference Source: Foreground"}
		for _, in := range k.inputs {
			body = append(body, in+": Locked")
		}
	case "VIDEO FORMATS":
		for _, f := range ultimatte.VideoFormatsFor(ultimatte.Context{Family: k.Model}) {
			body = append(body, strings.ToLower(f))
		}
	case "CONTROL":
		body = k.controls.lines()
	case "CONTROL DEFAULT":
		body = k.defaults.lines()
	case "CURRENT FILE":
		body = []string{k.currentFile}
	case "FILE LIST":
		body = slices.Clone(k.files)
	case "GPI LIST":
		if len(k.gpis) == 0 {
			return "", false
		}
		var b strings.Builder
		for i := 1; i <= 5; i++ {
			if _, ok := k.gpis[i]; ok {
				b.WriteString(k.gpiBlock(i))
			}
		}
		return b.String(), true
	case "IMAGE LIST":
		body = slices.Clone(k.images)
	case "FRAME BUFFER":
		body = append([]string{fmt.Sprintf("Number Of Frame Buffers: %d", k.fbCount)}, k.fb.lines()...)
	case "CAMERACONTROL":
		if k.Model != ultimatte.ModelHDMini {
			return "", false
		}
		body = []string{"Camera Id: " + k.cameraID}
	default:
		return "", false
	}
	return render(h, body), true
}

func (k *Keyer) gpiBlock(i int) string {
	g := k.gpis[i]
	return render("GPI LIST", append([]string{fmt.Sprintf("ID: %d", i), fmt.Sprintf("Index: %d", g.index)}, g.events...))
}

// Prelude is the status dump, in the document's order.
func (k *Keyer) Prelude() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.prelude()
}

func (k *Keyer) prelude() string {
	order := []string{"PROTOCOL PREAMBLE", "IDENTITY", "NETWORK", "NETWORK INTERFACE 0", "VERSION", "DEVICE",
		"VIDEO FORMATS", "CONTROL", "CONTROL DEFAULT", "CURRENT FILE", "FILE LIST"}
	if len(k.gpis) > 0 {
		order = append(order, "GPI LIST")
	}
	order = append(order, "IMAGE LIST", "FRAME BUFFER")
	if k.Model == ultimatte.ModelHDMini {
		order = append(order, "CAMERACONTROL")
	}
	var b strings.Builder
	for _, h := range order {
		s, _ := k.block(h)
		b.WriteString(s)
	}
	return b.String() + "END PRELUDE:\n\n"
}

func (k *Keyer) session(c net.Conn) {
	k.mu.Lock()
	if k.CloseOnConnect || k.stopped {
		k.mu.Unlock()
		c.Close()
		return
	}
	dump := k.prelude()
	if k.StallAfter >= 0 {
		k.mu.Unlock()
		go c.Write([]byte(dump[:k.StallAfter]))
		return
	}
	p := newPeer(c)
	p.send(dump)
	k.peers[p] = true
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		if k.peers[p] {
			delete(k.peers, p)
			close(p.done)
		}
		k.mu.Unlock()
		c.Close()
	}()
	var buf []byte
	chunk := make([]byte, 65536)
	for {
		n, err := c.Read(chunk)
		buf = append(buf, bytes.ReplaceAll(chunk[:n], []byte("\r\n"), []byte("\n"))...)
		for {
			i := bytes.Index(buf, []byte("\n\n"))
			if i < 0 {
				break
			}
			text := strings.TrimLeft(string(buf[:i]), "\n")
			buf = buf[i+2:]
			if text != "" {
				k.handle(p, strings.Split(text, "\n"))
			}
		}
		if err != nil {
			return
		}
	}
}

func (k *Keyer) broadcast(s string) {
	for p := range k.peers {
		p.send(s)
	}
}

// Broadcast pushes s to every connected client as is.
func (k *Keyer) Broadcast(s string) { k.mu.Lock(); k.broadcast(s); k.mu.Unlock() }

// Panel is a change made on the front panel: applied and pushed to
// everyone. items are key, value pairs.
func (k *Keyer) Panel(header string, items ...[2]string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var lines []string
	for _, kv := range items {
		switch header {
		case "CONTROL":
			k.controls.set(kv[0], kv[1])
		case "FRAME BUFFER":
			k.fb.set(kv[0], kv[1])
		}
		lines = append(lines, kv[0]+": "+kv[1])
	}
	k.broadcast(render(header, lines))
}

func kvs(body []string) [][2]string {
	var out [][2]string
	for _, ln := range body {
		if a, b, ok := strings.Cut(ln, ":"); ok {
			out = append(out, [2]string{strings.TrimSpace(a), strings.TrimSpace(b)})
		}
	}
	return out
}

func lowerMap(items [][2]string) map[string]string {
	m := map[string]string{}
	for _, kv := range items {
		m[strings.ToLower(kv[0])] = kv[1]
	}
	return m
}

func (k *Keyer) handle(p *peer, lines []string) {
	if k.captured && k.handle4K(p, lines) {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.handleLocked(p, lines)
}

func (k *Keyer) handleLocked(p *peer, lines []string) {
	k.received = append(k.received, strings.Join(lines, "\n"))
	if k.Mute {
		return
	}
	header := strings.TrimSpace(lines[0])
	var body []string
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) != "" {
			body = append(body, ln)
		}
	}
	if !strings.HasSuffix(header, ":") {
		p.send("NAK\n\n")
		return
	}
	name := strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(header, ":")))
	if len(body) == 0 {
		if s, ok := k.block(name); ok {
			p.send("ACK\n\n" + s)
		} else {
			p.send("NAK\n\n")
		}
		return
	}
	items := kvs(body)
	switch name {
	case "CONTROL":
		k.control(p, items)
	case "FILE":
		k.file(p, items)
	case "GPI":
		k.gpi(p, items)
	case "FRAME BUFFER":
		k.frameBuffer(p, items)
	case "CAMERACONTROL":
		k.camera(p, items)
	case "NETWORK INTERFACE 0":
		// Only the echo; the network tests own the network behaviour.
		var ls []string
		for _, kv := range items {
			ls = append(ls, kv[0]+": "+kv[1])
		}
		p.send("ACK\n\n" + render("NETWORK INTERFACE 0", ls))
	default:
		p.send("NAK\n\n")
	}
}

func (k *Keyer) control(p *peer, items [][2]string) {
	changes := newOrdered()
	var functions []string
	for _, kv := range items {
		key, value := kv[0], kv[1]
		offset := strings.HasPrefix(strings.ToLower(key), "offset ")
		name := key
		if offset {
			name = key[7:]
		}
		c, ok := ultimatte.Lookup(name)
		if _, have := k.controls.m[c.Name]; !ok || !have {
			p.send("NAK\n\n")
			return
		}
		if offset {
			d, ok := ultimatte.ParseInt(value)
			if c.Kind != ultimatte.KindRange || !ok {
				p.send("NAK\n\n")
				return
			}
			cur, _ := strconv.Atoi(k.controls.m[c.Name])
			hi := 1000000
			if c.Max != nil {
				hi = *c.Max
			}
			changes.set(c.Name, strconv.Itoa(max(*c.Min, min(hi, cur+d))))
			continue
		}
		if c.Kind == ultimatte.KindFunction {
			if v, ok := ultimatte.ParseBool(value); !ok || !v {
				p.send("NAK\n\n")
				return
			}
			functions = append(functions, c.Name)
			continue
		}
		text, err := ultimatte.FormatValue(c, value, ultimatte.Context{Family: k.Model, VideoFormat: k.VideoFormat})
		if err != nil {
			p.send("NAK\n\n")
			return
		}
		changes.set(c.Name, text)
	}
	p.send("ACK\n\n")
	if len(changes.keys) > 0 {
		for _, n := range changes.keys {
			k.controls.set(n, changes.m[n])
		}
		k.broadcast(render("CONTROL", changes.lines()))
	}
	for _, f := range functions {
		if f == "Factory Defaults" {
			diff := newOrdered()
			for _, n := range k.defaults.keys {
				if k.controls.m[n] != k.defaults.m[n] {
					diff.set(n, k.defaults.m[n])
				}
			}
			for _, n := range diff.keys {
				k.controls.set(n, diff.m[n])
			}
			if len(diff.keys) > 0 {
				k.broadcast(render("CONTROL", diff.lines()))
			}
		}
		if !k.FunctionSilent {
			k.broadcast(render("CONTROL", []string{f + ": yes"}))
		}
	}
}

func (k *Keyer) file(p *peer, items [][2]string) {
	kv := lowerMap(items)
	p.send("ACK\n\n")
	notFound := render("MESSAGE", []string{"Error: File not found"})
	if name, ok := kv["load"]; ok {
		if !slices.Contains(k.files, name) {
			p.send(notFound)
			return
		}
		k.currentFile = name
		s, _ := k.block("CURRENT FILE")
		k.broadcast(s)
		return
	}
	switch {
	case kv["save"] != "":
		if !slices.Contains(k.files, kv["save"]) {
			k.files = append(k.files, kv["save"])
		}
	case kv["delete"] != "":
		i := slices.Index(k.files, kv["delete"])
		if i < 0 {
			p.send(notFound)
			return
		}
		k.files = slices.Delete(k.files, i, i+1)
	case kv["rename"] != "" && kv["to"] != "":
		i := slices.Index(k.files, kv["rename"])
		if i < 0 {
			p.send(notFound)
			return
		}
		k.files[i] = kv["to"]
	default:
		p.send(render("MESSAGE", []string{"Error: Bad file command"}))
		return
	}
	s, _ := k.block("FILE LIST")
	k.broadcast(s)
}

func (k *Keyer) gpi(p *peer, items [][2]string) {
	kv := lowerMap(items)
	id, ok := ultimatte.ParseInt(kv["id"])
	g := k.gpis[id]
	if !ok || g == nil {
		p.send("NAK\n\n")
		return
	}
	p.send("ACK\n\n")
	noSuch := render("MESSAGE", []string{"Error: No such event"})
	if ins, ok := kv["insert"]; ok {
		if len(g.events) >= GPIEventLimit {
			p.send(render("MESSAGE", []string{"Warning: Event limit exceeded"}))
			return
		}
		at, ok := ultimatte.ParseInt(kv["at"])
		if _, given := kv["at"]; !given {
			at, ok = -1, true
		}
		if !ok || at < 0 || at > len(g.events) {
			g.events = append(g.events, ins)
		} else {
			g.events = slices.Insert(g.events, at, ins)
		}
	} else if r, ok := kv["remove"]; ok {
		n, isInt := ultimatte.ParseInt(r)
		switch {
		case isInt && n == 0:
			g.events, g.index = []string{}, 0
		case !isInt || n < 1 || n > len(g.events):
			p.send(noSuch)
			return
		default:
			g.events = slices.Delete(g.events, n-1, n)
		}
	} else if ix, ok := kv["index"]; ok {
		n, isInt := ultimatte.ParseInt(ix)
		if !isInt || n < 0 {
			p.send(noSuch)
			return
		}
		g.index = n
	}
	k.broadcast(k.gpiBlock(id))
}

func (k *Keyer) frameBuffer(p *peer, items [][2]string) {
	changes := newOrdered()
	for _, kv := range items {
		key, value := kv[0], kv[1]
		match := ""
		for _, f := range k.fb.keys {
			if strings.EqualFold(f, key) {
				match = f
				break
			}
		}
		if match == "" || strings.HasPrefix(match, "Frame Buffer ") {
			p.send("NAK\n\n")
			return
		}
		if strings.HasSuffix(match, "Enable") {
			b, ok := ultimatte.ParseBool(value)
			if !ok {
				p.send("NAK\n\n")
				return
			}
			value = "off"
			if b {
				value = "on"
			}
		} else {
			n, ok := ultimatte.ParseInt(value)
			if !ok || n < 0 || strings.HasSuffix(match, "Index") && n > k.fbCount {
				p.send("NAK\n\n")
				return
			}
			value = strconv.Itoa(n)
		}
		changes.set(match, value)
	}
	p.send("ACK\n\n")
	for _, n := range changes.keys {
		k.fb.set(n, changes.m[n])
	}
	k.broadcast(render("FRAME BUFFER", changes.lines()))
}

func (k *Keyer) camera(p *peer, items [][2]string) {
	n, ok := ultimatte.ParseInt(lowerMap(items)["camera id"])
	if k.Model != ultimatte.ModelHDMini || !ok {
		p.send("NAK\n\n")
		return
	}
	k.cameraID = strconv.Itoa(n)
	p.send("ACK\n\n")
	s, _ := k.block("CAMERACONTROL")
	k.broadcast(s)
}

// ---- the captured 12 4K ------------------------------------------------

// handle4K takes what the captured 12 4K does differently; false leaves
// the block to the documented unit.
func (k *Keyer) handle4K(p *peer, lines []string) bool {
	header := strings.ToUpper(strings.TrimRight(strings.TrimSpace(lines[0]), ":"))
	var body []string
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) != "" {
			body = append(body, ln)
		}
	}
	var items [][2]string
	for _, ln := range body {
		a, b, _ := strings.Cut(ln, ":")
		items = append(items, [2]string{strings.TrimSpace(a), strings.TrimSpace(b)})
	}
	text := strings.Join(lines, "\n")
	switch {
	case header == "IMAGE" && len(body) > 0:
		k.mu.Lock()
		defer k.mu.Unlock()
		k.received = append(k.received, text)
		k.image(p, items)
	case header == "FRAME BUFFER" && len(body) > 0:
		k.mu.Lock()
		defer k.mu.Unlock()
		k.received = append(k.received, text)
		k.namedFrameBuffer(p, items)
	case header == "CONTROL" && len(items) > 0 && strings.ToUpper(items[0][0]) == "FLUSH SETTINGS":
		k.mu.Lock()
		defer k.mu.Unlock()
		k.received = append(k.received, text)
		p.send("ACK\n\n")
		if !k.FGLocked {
			p.send(render("MESSAGE", []string{"Invalid operation - No Foreground source present"}))
			return true
		}
		k.flushes++
	case header == "CONTROL" && len(items) > 0:
		k.control4K(p, lines, items)
	default:
		return false
	}
	return true
}

func (k *Keyer) image(p *peer, items [][2]string) {
	name, ok := lowerMap(items)["delete"]
	if !ok {
		p.send("NAK\n\n")
		return
	}
	p.send("ACK\n\n")
	if _, ok := k.pool[name]; ok {
		delete(k.pool, name)
		k.poolOrder = slices.DeleteFunc(k.poolOrder, func(n string) bool { return n == name })
	}
	for _, b := range k.named.keys {
		if k.named.m[b] == name {
			k.named.set(b, "")
		}
	}
	fb, _ := k.block("FRAME BUFFER")
	il, _ := k.block("IMAGE LIST")
	k.broadcast(fb)
	k.broadcast(il)
}

func (k *Keyer) namedFrameBuffer(p *peer, items [][2]string) {
	for _, kv := range items {
		if _, ok := k.named.m[kv[0]]; !ok {
			p.send("NAK\n\n")
			return
		}
	}
	p.send("ACK\n\n")
	for _, kv := range items {
		if im, ok := k.pool[kv[1]]; kv[1] != "" && ok {
			if im.w != 1920 || im.h != 1080 {
				p.send(render("MESSAGE", []string{fmt.Sprintf("Mismatched formats: Image format is %dx%d and video format is 1920x1080", im.w, im.h)}))
				return
			}
			if !k.FGLocked {
				p.send(render("MESSAGE", []string{"Invalid operation - No Foreground source present"}))
				return
			}
		}
		v := ""
		if _, ok := k.pool[kv[1]]; ok {
			v = kv[1]
		}
		k.named.set(kv[0], v)
	}
	var ls []string
	for _, kv := range items {
		ls = append(ls, kv[0]+": "+k.named.m[kv[0]])
	}
	k.broadcast(render("FRAME BUFFER", ls))
	for _, kv := range items {
		prefix, ok := inputOf[kv[0]]
		if !ok || k.named.m[kv[0]] == "" {
			continue
		}
		cd, _ := k.block("CONTROL DEFAULT")
		k.broadcast(cd)
		for _, c := range [][2]string{{prefix + " Input Offset", "10000"}, {prefix + " Input Level", "6878"}} {
			k.controls.set(c[0], c[1])
			k.broadcast(render("CONTROL", []string{c[0] + ": " + c[1]}))
		}
		dev, _ := k.block("DEVICE")
		k.broadcast(dev)
		cd, _ = k.block("CONTROL DEFAULT")
		k.broadcast(cd)
	}
}

func (k *Keyer) control4K(p *peer, lines []string, items [][2]string) {
	text := strings.Join(lines, "\n")
	var names []string
	for _, kv := range items {
		n := kv[0]
		if strings.HasPrefix(strings.ToLower(n), "offset ") {
			n = n[7:]
		}
		names = append(names, n)
	}
	for _, where := range []string{"Wall", "Floor"} {
		for _, n := range names {
			if strings.EqualFold(n, "Sample "+where) {
				k.mu.Lock()
				k.received = append(k.received, text)
				k.mu.Unlock()
				k.sample(p, where)
				return
			}
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, where := range []string{"Wall", "Floor"} {
		keys := cursors[where]
		for _, n := range names {
			if (n == keys[0] || n == keys[1]) && k.controls.m[where+" Cursor Position Enable"] != "on" {
				k.received = append(k.received, text)
				p.send("ACK\n\n")
				p.send(render("MESSAGE", []string{locked}))
				return
			}
		}
	}
	if len(items) == 1 && (items[0][0] == "Wall Cursor Position Enable" || items[0][0] == "Floor Cursor Position Enable") {
		k.received = append(k.received, text)
		k.cursorEnable(p, items[0][0], items[0][1])
		return
	}
	for _, n := range names {
		if k.SilentControls[n] {
			k.received = append(k.received, text)
			p.send("ACK\n\n")
			return
		}
	}
	k.handleLocked(p, lines)
	if k.fileStatus == "Clean" {
		k.fileStatus = "Modified"
		s, _ := k.block("CURRENT FILE")
		k.broadcast(s)
	}
}

func (k *Keyer) cursorEnable(p *peer, key, value string) {
	v := strings.ToLower(strings.TrimSpace(value))
	p.send("ACK\n\n")
	if v != "on" && v != "true" && v != "yes" {
		k.controls.set(key, "off")
		k.broadcast(render("CONTROL", []string{key + ": off"}))
		return
	}
	where := strings.Fields(key)[0]
	time.AfterFunc(k.CursorEchoDelay, func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.pushColour(where)
		k.controls.set(key, "on")
		k.broadcast(render("CONTROL", []string{key + ": on"}))
	})
}

func (k *Keyer) pushColour(where string) {
	for _, c := range [][2]string{{"Blue", "2956"}, {"Green", "3744"}, {"Red", "5490"}} {
		k.broadcast(render("CONTROL", []string{where + " Color " + c[0] + ": " + c[1]}))
	}
}

func (k *Keyer) stream() {
	for {
		k.mu.Lock()
		d := k.StreamInterval
		k.mu.Unlock()
		select {
		case <-k.stop:
			return
		case <-time.After(d):
		}
		k.mu.Lock()
		if !k.sampling {
			for _, where := range []string{"Wall", "Floor"} {
				if k.controls.m[where+" Cursor Position Enable"] == "on" {
					k.pushColour(where)
				}
			}
		}
		k.mu.Unlock()
	}
}

// sample runs on this client's reading goroutine, so whatever it sends
// meanwhile waits, as on the unit.
func (k *Keyer) sample(p *peer, where string) {
	p.send("ACK\n\n")
	k.mu.Lock()
	k.sampling = true
	silence := k.SampleSilence
	k.mu.Unlock()
	time.Sleep(silence)
	k.mu.Lock()
	defer k.mu.Unlock()
	defer func() { k.sampling = false }()
	key := where + " Cursor Position Enable"
	k.controls.set(key, "off")
	k.broadcast(render("CONTROL", []string{key + ": off"}))
	upd := newOrdered()
	if where == "Wall" {
		upd.set("Floor Color Blue", "5958")
		upd.set("Floor Color Green", "6757")
		upd.set("Floor Color Red", "8333")
	}
	upd.set("Direct Light Blue", "10000")
	upd.set("Direct Light Green", "3253")
	upd.set("Direct Light Red", "10000")
	for _, n := range upd.keys {
		k.controls.set(n, upd.m[n])
		k.broadcast(render("CONTROL", []string{n + ": " + upd.m[n]}))
		k.defaults.set(n, upd.m[n])
	}
	cd, _ := k.block("CONTROL DEFAULT")
	k.broadcast(cd)
}

// mediaSession is the captured 12 4K's 9996: the image write.
func (k *Keyer) mediaSession(c net.Conn) {
	defer c.Close()
	exact := func(n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := io.ReadFull(c, b)
		return b, err
	}
	head, err := exact(6)
	if err != nil {
		return
	}
	op := binary.BigEndian.Uint16(head)
	name, err := exact(int(binary.BigEndian.Uint32(head[2:])))
	if err != nil {
		return
	}
	n, err := exact(4)
	if err != nil {
		return
	}
	data, err := exact(int(binary.BigEndian.Uint32(n)))
	if err != nil {
		return
	}
	tail, err := exact(1)
	if err != nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	frame := binary.BigEndian.AppendUint16(nil, op)
	k.uploads = append(k.uploads, append(append(append(frame, name...), data...), tail...))
	w, h, ok := sizeOf(data)
	if op != 0x0101 || tail[0] != 0 || !ok {
		return
	}
	k.addToPool(string(name), poolImage{w, h, data})
	c.Write([]byte{0}) // one byte, as captured
	s, _ := k.block("IMAGE LIST")
	k.broadcast(s)
}

// sizeOf is the fake's own reading of a PNG's or JPEG's size, without
// the ultimatte package's.
func sizeOf(data []byte) (w, h int, ok bool) {
	if bytes.HasPrefix(data, []byte("\x89PNG")) && len(data) >= 24 {
		return int(binary.BigEndian.Uint32(data[16:])), int(binary.BigEndian.Uint32(data[20:])), true
	}
	i := 2
	for bytes.HasPrefix(data, []byte{0xff, 0xd8}) && i+9 <= len(data) {
		marker, length := data[i+1], int(binary.BigEndian.Uint16(data[i+2:]))
		if marker >= 0xc0 && marker <= 0xc3 {
			return int(binary.BigEndian.Uint16(data[i+7:])), int(binary.BigEndian.Uint16(data[i+5:])), true
		}
		i += 2 + length
	}
	return 0, 0, false
}

// PNG is a PNG header the unit (and ultimatte.PNGSize) can read the size
// from.
func PNG(w, h int) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	b := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	b = append(append(b, "IHDR"...), ihdr...)
	return append(b, "\x00\x00\x00\x00\x00\x00\x00\x00IEND\xaeB`\x82"...)
}

// JPG is a JPEG header (JFIF APP0, an optional padding APP1 of appBytes,
// SOF0) the unit could read the size from, then an end.
func JPG(w, h, appBytes int) []byte {
	b := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16}
	b = append(b, "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"...)
	if appBytes > 0 {
		b = append(b, 0xff, 0xe1)
		b = binary.BigEndian.AppendUint16(b, uint16(appBytes+2))
		b = append(b, make([]byte, appBytes)...)
	}
	b = append(b, 0xff, 0xc0, 0, 17, 8)
	b = binary.BigEndian.AppendUint16(b, uint16(h))
	b = binary.BigEndian.AppendUint16(b, uint16(w))
	b = append(b, 3, 0x01, 0x22, 0, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01)
	return append(b, 0xff, 0xd9)
}
