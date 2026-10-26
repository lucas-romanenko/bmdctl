// SPDX-License-Identifier: MIT

// Package fakehyperdeck is a HyperDeck for tests: it speaks the HyperDeck
// Ethernet Protocol (TCP 9993) on any net.Conn, so hyperdeck.Session,
// the engine and the CLI run end to end without a deck. Dial hands a
// session one end of a pipe; Listen serves loopback, for what dials a
// port itself (the device probe, doctor).
//
// It holds two slots of clips, a timeline (the active slot's clips, as a
// HyperDeck Studio's is), a transport, remote control and the
// configuration, answers the commands hyperdeck sends with the codes and
// blocks BMD's protocol document gives, and sends each connection the
// notifications it turned on (508 transport info and the rest) carrying
// what changed. It is not a deck model: the timecode moves only on goto,
// recording adds a clip when it stops, and nothing about a real deck's
// timing is claimed. docs/PORTING.md, "Fake HyperDeck", records it.
package fakehyperdeck

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Address is what tests dial: TEST-NET-1, so nothing real answers it.
const Address = "192.0.2.20"

// Clip is a clip on a slot's disk.
type Clip struct {
	Name        string
	FileFormat  string // H.264
	VideoFormat string // 1080p25
	Frames      int    // its duration
}

type slot struct {
	clips         []Clip
	volume        string
	recordingTime int // seconds left
}

// Deck is the fake deck. Its fields are read when a connection opens or
// a command needs them; set them before serving.
type Deck struct {
	Model, Name, ProtocolVersion, UniqueID, Software string
	FPS                                              int // frames per second of its timecode

	mu       sync.Mutex
	slots    []*slot // index 0 is slot id 1
	active   int     // slot id
	timeline []Clip
	status   string // stopped, play, record, preview
	speed    int
	clipID   int // 0: none
	frame    int // the transport's position on the timeline, in frames
	loop     bool
	single   bool
	remote   bool
	config   map[string]string
	conns    map[*conn]bool
	commands []string
	silent   bool
	reject   int
	recName  string
	recCount int
}

// conn is one client's connection and the notifications it turned on.
type conn struct {
	c      net.Conn
	w      *bufio.Writer
	notify map[string]bool
}

// New returns a HyperDeck Studio HD Mini with two SD slots, three clips
// on the first and one on the second, the first active, stopped on its
// first clip, remote control on.
func New() *Deck {
	d := &Deck{Model: "HyperDeck Studio HD Mini", Name: "Deck 1", ProtocolVersion: "1.13",
		UniqueID: "7c2e0d18dc78", Software: "8.1.1", FPS: 25,
		slots: []*slot{
			{volume: "SD1", recordingTime: 7200, clips: []Clip{
				{"Intro Loop animation.mp4", "H.264", "1080p25", 25*63 + 17},
				{"Interview.mov", "QuickTimeProResHQ", "1080p25", 25 * 30},
				{"Outro.mov", "QuickTimeProResHQ", "1080p25", 25 * 10},
			}},
			{volume: "SD2", recordingTime: 3600, clips: []Clip{{"Backup.mov", "QuickTimeProResHQ", "1080p25", 25 * 5}}},
		},
		active: 1, status: "stopped", clipID: 1, remote: true, conns: map[*conn]bool{},
		config: map[string]string{"audio input": "embedded", "video input": "SDI", "file format": "QuickTimeProResHQ",
			"record prefix": "", "timecode input": "internal"}}
	d.timeline = slices.Clone(d.slots[0].clips)
	return d
}

// Dial is a hyperdeck.Dialer's DialContext: a connection to the deck,
// over a pipe, whatever the address.
func (d *Deck) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	client, server := net.Pipe()
	go d.Serve(server)
	return client, nil
}

// Listen serves the deck on addr (127.0.0.1:0 for any port) until the
// listener is closed.
func (d *Deck) Listen(addr string) (net.Listener, error) {
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
			go d.Serve(c)
		}
	}()
	return l, nil
}

// Serve speaks the protocol on c until the client quits or c fails.
func (d *Deck) Serve(c net.Conn) {
	defer c.Close()
	cn := &conn{c: c, w: bufio.NewWriter(c), notify: map[string]bool{}}
	d.mu.Lock()
	if d.reject > 0 {
		d.reject--
		d.mu.Unlock()
		io.WriteString(c, "120 connection rejected\r\n")
		return
	}
	d.conns[cn] = true
	d.writeTo(cn, d.block(500, "connection info", "protocol version", d.ProtocolVersion, "model", d.Model))
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.conns, cn)
		d.mu.Unlock()
	}()
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		d.mu.Lock()
		d.commands = append(d.commands, line)
		if line == "quit" {
			d.mu.Unlock()
			return
		}
		if !d.silent {
			reply, notes := d.handle(cn, line)
			d.writeTo(cn, reply)
			for _, n := range notes {
				d.broadcast(n.kind, n.text)
			}
		}
		d.mu.Unlock()
	}
}

// Commands returns every line the deck has been sent, in order.
func (d *Deck) Commands() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.commands)
}

// SetRemote turns remote control on or off, as the deck's front panel
// does; writes are refused with 111 while it is off.
func (d *Deck) SetRemote(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.remote = on
	d.broadcast("remote", d.block(510, "remote info", "enabled", strconv.FormatBool(on)))
}

// Silence makes the deck take commands without answering them.
func (d *Deck) Silence(on bool) {
	d.mu.Lock()
	d.silent = on
	d.mu.Unlock()
}

// Reject answers the next n connections 120 connection rejected and
// closes them, as a deck with all the clients it takes does.
func (d *Deck) Reject(n int) {
	d.mu.Lock()
	d.reject = n
	d.mu.Unlock()
}

// Drop closes every connection, as a deck going away does.
func (d *Deck) Drop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for c := range d.conns {
		c.c.Close()
	}
}

// Press plays the deck's front panel: the transport goes to status (play,
// stopped, record), and every connection hears of it.
func (d *Deck) Press(status string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	note := d.setStatus(status, d.speedFor(status))
	if note != "" {
		d.broadcast("transport", note)
	}
}

// Send writes raw text to every connection: what a test needs a deck to
// say that this one wouldn't.
func (d *Deck) Send(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for c := range d.conns {
		d.writeTo(c, text)
	}
}

// Status is the transport's status.
func (d *Deck) Status() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

// Slot returns the clips on a slot (1 or 2).
func (d *Deck) Slot(id int) []Clip {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id < 1 || id > len(d.slots) {
		return nil
	}
	return slices.Clone(d.slots[id-1].clips)
}

func (d *Deck) writeTo(c *conn, text string) {
	if text == "" {
		return
	}
	c.w.WriteString(text)
	if c.w.Flush() != nil {
		c.c.Close()
	}
}

// broadcast sends a notification to the connections that turned its kind
// on. A pipe's write blocks until the client reads it; the client reads
// all the time.
func (d *Deck) broadcast(kind, text string) {
	if text == "" {
		return
	}
	for c := range d.conns {
		if c.notify[kind] {
			d.writeTo(c, text)
		}
	}
}

// block writes a response block: the head, then "key: value" lines from
// kv's pairs, then a blank line; with no pairs, the head alone.
func (d *Deck) block(code int, text string, kv ...string) string {
	if len(kv) == 0 && code < 200 || code == 200 {
		return fmt.Sprintf("%d %s\r\n", code, text)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s:\r\n", code, text)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, "%s: %s\r\n", kv[i], kv[i+1])
	}
	b.WriteString("\r\n")
	return b.String()
}

type note struct{ kind, text string }

func refuse(code int, text string) (string, []note) { return fmt.Sprintf("%d %s\r\n", code, text), nil }

const ok = "200 ok\r\n"

// handle answers one command line.
func (d *Deck) handle(c *conn, line string) (string, []note) {
	verb, args, hasArgs := strings.Cut(line, ":")
	verb, args = strings.TrimSpace(verb), strings.TrimSpace(args)
	p := params(args)
	write := func() bool { return d.remote }
	switch verb {
	case "ping":
		return ok, nil
	case "device info":
		return d.block(204, "device info", "protocol version", d.ProtocolVersion, "model", d.Model,
			"unique id", d.UniqueID, "slot count", strconv.Itoa(len(d.slots)), "software version", d.Software,
			"name", d.Name), nil
	case "remote":
		if !hasArgs {
			return d.block(210, "remote info", "enabled", strconv.FormatBool(d.remote), "override", "false"), nil
		}
		on, okb := p.bool("enable")
		if !okb {
			return refuse(102, "invalid value")
		}
		d.remote = on
		return ok, []note{{"remote", d.block(510, "remote info", "enabled", strconv.FormatBool(on))}}
	case "notify":
		if !hasArgs {
			var kv []string
			for _, k := range []string{"remote", "transport", "slot", "configuration", "dropped frames"} {
				kv = append(kv, k, strconv.FormatBool(c.notify[k]))
			}
			return d.block(209, "notify", kv...), nil
		}
		for k, v := range p {
			switch k {
			case "remote", "transport", "slot", "configuration", "dropped frames", "display timecode",
				"timeline position", "playrange", "cache", "dynamic range", "slate", "clips", "disk", "nas":
				c.notify[k] = v == "true"
			default:
				return refuse(101, "unsupported parameter")
			}
		}
		return ok, nil
	case "transport info":
		return d.transportBlock(208), nil
	case "slot info":
		id := d.active
		if s, okn := p.int("slot id"); okn {
			id = s
		}
		if id < 1 || id > len(d.slots) {
			return refuse(102, "invalid value")
		}
		return d.slotBlock(202, id), nil
	case "slot select":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		id, okn := p.int("slot id")
		if !okn || id < 1 || id > len(d.slots) {
			return refuse(102, "invalid value")
		}
		if d.status == "record" {
			return refuse(150, "invalid state")
		}
		d.active, d.timeline, d.clipID, d.frame = id, slices.Clone(d.slots[id-1].clips), 1, 0
		if len(d.timeline) == 0 {
			d.clipID = 0
		}
		return ok, []note{{"transport", d.block(508, "transport info", "slot id", strconv.Itoa(id),
			"clip id", d.clipIDText(), "display timecode", d.timecode(), "timecode", d.timecode())}}
	case "configuration":
		if !hasArgs {
			var kv []string
			for _, k := range slices.Sorted(maps.Keys(d.config)) {
				kv = append(kv, k, d.config[k])
			}
			return d.block(211, "configuration", kv...), nil
		}
		if !write() {
			return refuse(111, "remote control disabled")
		}
		var kv []string
		for k, v := range p {
			d.config[k] = v
			kv = append(kv, k, v)
		}
		return ok, []note{{"configuration", d.block(511, "configuration", kv...)}}
	case "disk list":
		id := d.active
		if s, okn := p.int("slot id"); okn {
			id = s
		}
		if id < 1 || id > len(d.slots) {
			return refuse(102, "invalid value")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "206 disk list:\r\nslot id: %d\r\n", id)
		for i, cl := range d.slots[id-1].clips {
			fmt.Fprintf(&b, "%d: %s %s %s %s\r\n", i+1, cl.Name, cl.FileFormat, cl.VideoFormat, d.tc(cl.Frames))
		}
		b.WriteString("\r\n")
		return b.String(), nil
	case "clips count":
		return d.block(214, "clips count", "clip count", strconv.Itoa(len(d.timeline))), nil
	case "clips get":
		var b strings.Builder
		fmt.Fprintf(&b, "205 clips info:\r\nclip count: %d\r\n", len(d.timeline))
		start := 0
		for i, cl := range d.timeline {
			fmt.Fprintf(&b, "%d: %s %s %s\r\n", i+1, cl.Name, d.tc(start), d.tc(cl.Frames))
			start += cl.Frames
		}
		b.WriteString("\r\n")
		return b.String(), nil
	case "clips clear":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		d.timeline, d.clipID, d.frame = nil, 0, 0
		return ok, nil
	case "clips add":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		name := p.rest("name")
		i := slices.IndexFunc(d.slots[d.active-1].clips, func(cl Clip) bool { return cl.Name == name })
		if i < 0 {
			return refuse(112, "clip not found")
		}
		d.timeline = append(d.timeline, d.slots[d.active-1].clips[i])
		if d.clipID == 0 {
			d.clipID = 1
		}
		return ok, nil
	case "clips remove":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		id, okn := p.int("clip id")
		if !okn || id < 1 || id > len(d.timeline) {
			return refuse(112, "clip not found")
		}
		d.timeline = slices.Delete(d.timeline, id-1, id)
		return ok, nil
	case "play":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		if d.status == "record" {
			return refuse(150, "invalid state")
		}
		if len(d.timeline) == 0 {
			return refuse(107, "timeline empty")
		}
		if id, okn := p.int("clip id"); okn {
			if id < 1 || id > len(d.timeline) {
				return refuse(112, "clip not found")
			}
			d.goToClip(id)
		}
		speed := 100
		if s, okn := p.int("speed"); okn {
			if s < -5000 || s > 5000 {
				return refuse(102, "invalid value")
			}
			speed = s
		}
		var kv []string
		if b, okb := p.bool("loop"); okb && b != d.loop {
			d.loop = b
			kv = append(kv, "loop", strconv.FormatBool(b))
		}
		if b, okb := p.bool("single clip"); okb && b != d.single {
			d.single = b
			kv = append(kv, "single clip", strconv.FormatBool(b))
		}
		status := "play"
		if speed == 0 {
			status = "stopped" // play at speed 0 holds the frame, reported stopped
		}
		n := d.setStatus(status, speed, kv...)
		return ok, notes("transport", n)
	case "stop":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		return ok, notes("transport", d.setStatus("stopped", 0))
	case "record":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		if d.status == "record" {
			return refuse(150, "invalid state")
		}
		d.recName = p.rest("name")
		return ok, notes("transport", d.setStatus("record", 0))
	case "goto":
		if !write() {
			return refuse(111, "remote control disabled")
		}
		if d.status == "record" {
			return refuse(150, "invalid state")
		}
		if v, okv := p["clip id"]; okv {
			if len(d.timeline) == 0 {
				return refuse(107, "timeline empty")
			}
			id, err := strconv.Atoi(strings.TrimPrefix(v, "+"))
			if err != nil {
				return refuse(102, "invalid value")
			}
			if strings.HasPrefix(v, "+") || strings.HasPrefix(v, "-") {
				id = min(max(d.clipID+id, 1), len(d.timeline))
			} else if id < 1 || id > len(d.timeline) {
				return refuse(112, "clip not found")
			}
			d.goToClip(id)
		} else if v, okv := p["timecode"]; okv {
			f, okf := d.frames(v)
			if !okf {
				return refuse(102, "invalid value")
			}
			if f >= d.length() {
				return refuse(109, "out of range")
			}
			d.goToFrame(f)
		} else {
			return refuse(101, "unsupported parameter")
		}
		return ok, notes("transport", d.block(508, "transport info", "clip id", d.clipIDText(),
			"display timecode", d.timecode(), "timecode", d.timecode()))
	}
	return refuse(100, "syntax error")
}

func notes(kind, text string) []note {
	if text == "" {
		return nil
	}
	return []note{{kind, text}}
}

func (d *Deck) speedFor(status string) int {
	if status == "play" {
		return 100
	}
	return 0
}

// setStatus moves the transport and returns the notification saying
// what changed, with any other changed fields in kv.
func (d *Deck) setStatus(status string, speed int, kv ...string) string {
	if d.status == "record" && status != "record" {
		name := d.recName
		if name == "" {
			d.recCount++
			name = fmt.Sprintf("Clip %04d.mov", d.recCount)
		} else if !strings.Contains(name, ".") {
			name += ".mov"
		}
		s := d.slots[d.active-1]
		s.clips = append(s.clips, Clip{name, d.config["file format"], "1080p25", d.FPS * 2})
		d.timeline = slices.Clone(s.clips)
		d.recName = ""
	}
	if status != d.status {
		d.status = status
		kv = append(kv, "status", status)
	}
	if speed != d.speed {
		d.speed = speed
		kv = append(kv, "speed", strconv.Itoa(speed))
	}
	if len(kv) == 0 {
		return ""
	}
	return d.block(508, "transport info", kv...)
}

func (d *Deck) goToClip(id int) {
	d.clipID, d.frame = id, 0
	for _, cl := range d.timeline[:id-1] {
		d.frame += cl.Frames
	}
}

func (d *Deck) goToFrame(f int) {
	d.frame = f
	start := 0
	for i, cl := range d.timeline {
		if f < start+cl.Frames {
			d.clipID = i + 1
			return
		}
		start += cl.Frames
	}
}

func (d *Deck) length() int {
	n := 0
	for _, cl := range d.timeline {
		n += cl.Frames
	}
	return n
}

func (d *Deck) clipIDText() string {
	if d.clipID == 0 {
		return "none"
	}
	return strconv.Itoa(d.clipID)
}

func (d *Deck) transportBlock(code int) string {
	return d.block(code, "transport info", "status", d.status, "speed", strconv.Itoa(d.speed),
		"slot id", strconv.Itoa(d.active), "clip id", d.clipIDText(), "single clip", strconv.FormatBool(d.single),
		"display timecode", d.timecode(), "timecode", d.timecode(), "video format", "1080p25",
		"loop", strconv.FormatBool(d.loop), "timeline", "0")
}

func (d *Deck) slotBlock(code, id int) string {
	s := d.slots[id-1]
	return d.block(code, "slot info", "slot id", strconv.Itoa(id), "status", "mounted", "volume name", s.volume,
		"recording time", strconv.Itoa(s.recordingTime), "video format", "1080p25", "blocked", "false")
}

func (d *Deck) timecode() string { return d.tc(d.frame) }

// tc is frames as HH:MM:SS:FF.
func (d *Deck) tc(frames int) string {
	f := frames % d.FPS
	s := frames / d.FPS
	return fmt.Sprintf("%02d:%02d:%02d:%02d", s/3600, s/60%60, s%60, f)
}

func (d *Deck) frames(tc string) (int, bool) {
	parts := strings.FieldsFunc(tc, func(r rune) bool { return r == ':' || r == ';' })
	if len(parts) != 4 {
		return 0, false
	}
	var n [4]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		n[i] = v
	}
	if n[3] >= d.FPS {
		return 0, false
	}
	return ((n[0]*60+n[1])*60+n[2])*d.FPS + n[3], true
}

// paramSet is a command's "key: value" parameters.
type paramSet map[string]string

// keys a command can carry, longest first, so "single clip" isn't read
// as a value of "clip".
var keys = []string{"display timecode", "timeline position", "dropped frames", "default standard", "dynamic range",
	"configuration", "single clip", "file format", "video input", "audio input", "record prefix", "timecode",
	"transport", "clip id", "slot id", "playrange", "timeline", "override", "enable", "remote", "speed", "loop",
	"slot", "cache", "slate", "clips", "disk", "name", "nas"}

// params reads "key: value key: value". A name takes the rest of the
// line, as the deck reads it.
func params(s string) paramSet {
	p := paramSet{}
	for s != "" {
		k := ""
		for _, key := range keys {
			if strings.HasPrefix(s, key+":") {
				k = key
				break
			}
		}
		if k == "" { // unknown key: up to its colon
			i := strings.Index(s, ":")
			if i < 0 {
				return p
			}
			k = strings.TrimSpace(s[:i])
		}
		s = strings.TrimSpace(s[len(k)+1:])
		if k == "name" {
			p[k] = s
			return p
		}
		v, rest, _ := strings.Cut(s, " ")
		p[k], s = v, strings.TrimSpace(rest)
	}
	return p
}

func (p paramSet) int(k string) (int, bool) {
	n, err := strconv.Atoi(p[k])
	return n, err == nil
}

func (p paramSet) bool(k string) (bool, bool) {
	switch p[k] {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func (p paramSet) rest(k string) string { return p[k] }
