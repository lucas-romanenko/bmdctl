// SPDX-License-Identifier: MIT

package ultimatte

// ultimattewire's client.py (bmdwire v1.14.0, bmdwire#78): Session is its
// Ultimatte, one session on TCP 9998 speaking Blackmagic's documented
// "Ultimatte 12 Ethernet Protocol" v2.0 (September 2022). It is the
// Videohub's block protocol: a block is a CAPS header ending in a colon,
// key: value (or bare) lines and a blank line, which submits it, in
// either direction. On connect the unit sends its whole state, ending
// END PRELUDE, then pushes the changed items of a block whenever
// anything changes. A block the client sends gets ACK or NAK (each a
// block of its own), then the update; files and GPI events can get a
// MESSAGE block instead.
//
// Like ultimattewire's it is synchronous: no goroutine, no timer, nothing
// on the wire unless a method is called. Pushed blocks are applied (and
// handed to subscribers) whenever it reads, or when Poll asks. Every wait
// is a no-progress wait, ending once the unit has sent nothing for its
// idle timeout however long it kept talking before; the cursor colour
// stream of a 12 4K doesn't count as talking. Where a 12 4K was seen to
// differ from the document (CURRENT FILE as Name:/Status: lines, Dynamic
// IP with a space, values in lower case, frame buffers by image name)
// both forms are read. None of it has met a unit, here or in
// ultimattewire: it is tested against fakes built from the document and
// from captures of Software Control (internal/fakeultimatte).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// The session's timings: the longest the unit may stay silent while it
// owes an answer (the dump, an ACK, an update), counted from the last
// byte received, and while a function runs (a 12 4K said nothing for
// 4.0 s after Sample Wall / Sample Floor, captured 2026-11-12).
const (
	DefaultIdleTimeout         = 3 * time.Second
	DefaultFunctionIdleTimeout = 10 * time.Second
)

// PreludeEnd is the line that ends the status dump.
const PreludeEnd = "END PRELUDE:"

// FrameBuffers are the document's FRAME BUFFER example's buffers, and
// FrameBufferLayers its two layers.
var (
	FrameBuffers      = []string{"BG 1", "LY 1", "BG 2", "LY 2", "GM", "HM"}
	FrameBufferLayers = []string{"BG", "LY"}
)

// NamedBuffers are the names a 12 4K gives its buffers in FRAME BUFFER
// (Background 1: <image>), as Software Control assigns them, in order,
// with the short name of each.
var NamedBuffers = [][2]string{{"Background 1", "BG 1"}, {"Layer 1", "LY 1"},
	{"Background 2", "BG 2"}, {"Layer 2", "LY 2"},
	{"Garbage Matte", "GM"}, {"Holdout Matte", "HM"}}

func namedShort(key string) (string, bool) {
	for _, n := range NamedBuffers {
		if n[0] == key {
			return n[1], true
		}
	}
	return "", false
}

var namedLayers = map[string]string{"Background": "BG", "Layer": "LY"}

// refreshSpelling: the bare headers as Software Control spells them when
// it asks a 12 4K for one block (captured 2026-11-11 and -12).
var refreshSpelling = map[string]string{"FRAME BUFFER": "Frame Buffer", "IMAGE LIST": "Image List",
	"FILE LIST": "File List", "CURRENT FILE": "Current File"}

// CursorColorKeys are the controls a 12 4K pushes about every 40 ms while
// a cursor is on (captured 2026-11-12), lower case: the colour under the
// wall cursor and the floor one.
var CursorColorKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{"wall", "floor"} {
		for _, c := range []string{"red", "green", "blue"} {
			m[w+" color "+c] = true
		}
	}
	return m
}()

// Identity is the IDENTITY block.
type Identity struct {
	Model    string `json:"model"`
	Label    string `json:"label"`
	UniqueID string `json:"unique_id"`
}

// VersionInfo is the VERSION block.
type VersionInfo struct {
	ProductID       string `json:"product_id"`
	HardwareVersion string `json:"hardware_version"`
	SoftwareVersion string `json:"software_version"`
	SoftwareRelease string `json:"software_release"`
}

// DeviceInfo is the DEVICE block: the format the unit is running (the
// CONTROL block's Video Format is the setting, often auto detect), the
// reference, and each input's lock state as sent (Locked, ...).
type DeviceInfo struct {
	VideoFormat     string            `json:"video_format"`
	ReferenceSource string            `json:"reference_source"`
	Inputs          map[string]string `json:"inputs"`
}

var spaces = regexp.MustCompile(`\s+`)

func inputKey(name string) string {
	k := pytext.Lower(spaces.ReplaceAllString(name, ""))
	return strings.TrimSuffix(k, "in")
}

// InputStatus is the lock state of an input ("FG", "FG In", "REF"; the
// document spells it REFIn and REF In alike); ok false when not reported.
func (d DeviceInfo) InputStatus(name string) (string, bool) {
	want := inputKey(name)
	for _, k := range slices.Sorted(maps.Keys(d.Inputs)) {
		if inputKey(k) == want {
			return d.Inputs[k], true
		}
	}
	return "", false
}

// Locked is whether an input is locked; nil when not reported.
func (d DeviceInfo) Locked(name string) *bool {
	s, ok := d.InputStatus(name)
	if !ok {
		return nil
	}
	v := pytext.Lower(pytext.Strip(s)) == "locked"
	return &v
}

// CurrentFile is CURRENT FILE. The document shows one bare line, the file
// name; a 12 4K sends Name: and Status: (Clean, or Modified once settings
// changed on the unit and weren't saved).
type CurrentFile struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Modified is nil when the unit gave no status.
func (f CurrentFile) Modified() *bool {
	if f.Status == "" {
		return nil
	}
	v := pytext.Lower(pytext.Strip(f.Status)) == "modified"
	return &v
}

// GPIList is one GPI LIST block: GPI ID, its current event Index, and its
// events (file names) in order.
type GPIList struct {
	ID     int      `json:"id"`
	Index  *int     `json:"index"`
	Events []string `json:"events"`
}

// FrameBuffer is one buffer's enable and index, nil where not reported.
type FrameBuffer struct {
	Name   string `json:"name"`
	Enable *bool  `json:"enable"`
	Index  *int   `json:"index"`
}

// FrameBufferState is FRAME BUFFER: Buffers by name (BG 1, LY 1, BG 2,
// LY 2, GM, HM), Mix and Duration (milliseconds) by layer (BG, LY; an int,
// or the string sent when it isn't one), Images a frame buffer number's
// image (Frame Buffer 1: Image 1). A unit that names images per buffer
// (Background 1: Newsroom.png, the 12 4K) fills Assigned by the short
// name; "" is no image.
type FrameBufferState struct {
	Count    *int                    `json:"count"`
	Buffers  map[string]*FrameBuffer `json:"buffers"`
	Mix      map[string]any          `json:"mix"`
	Duration map[string]any          `json:"duration"`
	Images   map[int]string          `json:"images"`
	Assigned map[string]string       `json:"assigned"`
	Other    map[string]string       `json:"other"`
}

// ImageNumber is the frame buffer number holding image (exact name, then
// any case); ok false for none.
func (f FrameBufferState) ImageNumber(image string) (int, bool) {
	nums := slices.Sorted(maps.Keys(f.Images))
	for _, n := range nums {
		if f.Images[n] == image {
			return n, true
		}
	}
	for _, n := range nums {
		if pytext.Lower(f.Images[n]) == pytext.Lower(image) {
			return n, true
		}
	}
	return 0, false
}

// State is everything the unit has told the session, kept current by its
// pushes. Control values are typed by the table (ParseValue): an int, a
// bool or the string sent.
type State struct {
	ProtocolVersion string                   `json:"protocol_version"`
	Identity        Identity                 `json:"identity"`
	Network         map[string]string        `json:"network"`
	Interfaces      map[int]NetworkInterface `json:"interfaces"`
	Version         VersionInfo              `json:"version"`
	Device          DeviceInfo               `json:"device"`
	VideoFormats    []string                 `json:"video_formats"`
	Controls        map[string]any           `json:"controls"`
	ControlDefaults map[string]any           `json:"control_defaults"`
	CurrentFile     CurrentFile              `json:"current_file"`
	FileList        []string                 `json:"file_list"`
	GPILists        map[int]GPIList          `json:"gpi_lists"`
	ImageList       []string                 `json:"image_list"`
	// IMAGE LIST's Capacity: and Available:, in bytes (a 12 4K sends them).
	ImageCapacity   *int                `json:"image_capacity"`
	ImageAvailable  *int                `json:"image_available"`
	FrameBuffer     FrameBufferState    `json:"frame_buffer"`
	CameraID        *int                `json:"camera_id"`
	Messages        []map[string]string `json:"messages"`
	Other           map[string][]string `json:"other"`
	PreludeComplete bool                `json:"prelude_complete"`
}

func newState() *State {
	return &State{Network: map[string]string{}, Interfaces: map[int]NetworkInterface{},
		Device: DeviceInfo{Inputs: map[string]string{}}, VideoFormats: []string{},
		Controls: map[string]any{}, ControlDefaults: map[string]any{}, FileList: []string{},
		GPILists: map[int]GPIList{}, ImageList: []string{}, Messages: []map[string]string{},
		Other: map[string][]string{},
		FrameBuffer: FrameBufferState{Buffers: map[string]*FrameBuffer{}, Mix: map[string]any{},
			Duration: map[string]any{}, Images: map[int]string{}, Assigned: map[string]string{},
			Other: map[string]string{}}}
}

// Family is the model as a Model constant ("" for one the document
// doesn't name).
func (s *State) Family() string { return ModelFamily(s.Identity.Model) }

var protocolRe = regexp.MustCompile(`^\s*(\d+)\.(\d+)`)

// Protocol is the PROTOCOL PREAMBLE Version as (major, minor); nil when
// it doesn't read.
func (s *State) Protocol() *[2]int {
	m := protocolRe.FindStringSubmatch(s.ProtocolVersion)
	if m == nil {
		return nil
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return &[2]int{a, b}
}

// Tally is Tally Active (read only): whether the unit's output is on
// air; nil when not reported as a bool.
func (s *State) Tally() *bool {
	if v, ok := s.Controls["Tally Active"].(bool); ok {
		return &v
	}
	return nil
}

// MarshalJSON is to_dict: the state as plain data, with family and tally,
// and without the interfaces' raw text.
func (s *State) MarshalJSON() ([]byte, error) {
	type plain State
	ifaces := map[int]NetworkInterface{}
	for k, v := range s.Interfaces {
		v.Raw = ""
		ifaces[k] = v
	}
	cp := plain(*s)
	cp.Interfaces = ifaces
	return json.Marshal(struct {
		plain
		Interfaces map[int]map[string]any `json:"interfaces"`
		Family     string                 `json:"family"`
		Tally      *bool                  `json:"tally"`
	}{cp, withoutRaw(ifaces), s.Family(), s.Tally()})
}

func withoutRaw(ifaces map[int]NetworkInterface) map[int]map[string]any {
	out := map[int]map[string]any{}
	for k, v := range ifaces {
		b, _ := json.Marshal(v)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		delete(m, "raw")
		out[k] = m
	}
	return out
}

// Update is one block the unit sent, as subscribers get it. Changes is
// what the block said, typed: a map of its items for the key-value blocks
// (map[string]any for CONTROL, CONTROL DEFAULT and FRAME BUFFER, else
// map[string]string), the new list for FILE LIST, IMAGE LIST and VIDEO
// FORMATS, a GPIList for GPI LIST (nil without an ID), a CurrentFile for
// CURRENT FILE, nil for END PRELUDE.
type Update struct {
	Header  string
	Changes any
	Lines   []string
	State   *State
}

// Options are a write's: NoValidate sends the value as given (a bool as
// on/off) instead of checking it against the table first; NoWait returns
// at the ACK instead of the unit's update.
type Options struct {
	NoValidate, NoWait bool
}

// ControlValue is one control to set.
type ControlValue struct {
	Name  string
	Value any
}

type subscriber struct {
	fn   func(Update)
	want map[string]bool
}

// Session is one session on an Ultimatte's TCP 9998: ultimattewire's
// Ultimatte. Open one with Client.Open and Close it. It is not safe for
// concurrent use, as ultimattewire's isn't.
type Session struct {
	Host string
	// IdleTimeout and FunctionIdleTimeout are the no-progress waits
	// (DefaultIdleTimeout, DefaultFunctionIdleTimeout); set them from
	// the Client's.
	IdleTimeout, FunctionIdleTimeout time.Duration
	// State is what the unit has said; it is replaced by Open only.
	State *State
	// Log gets a subscriber's panic, which is recovered, not raised.
	Log *slog.Logger

	client *Client
	conn   net.Conn
	buf    []byte
	subs   []*subscriber
	last   *Update
}

// Open is Ultimatte.connect: the session opened (with the retry) and the
// unit's state read up to END PRELUDE. The Client's IdleTimeout and
// FunctionIdleTimeout are the session's.
func (c *Client) Open(ctx context.Context, host string) (*Session, error) {
	s := &Session{Host: host, IdleTimeout: or(c.IdleTimeout, DefaultIdleTimeout),
		FunctionIdleTimeout: or(c.FunctionIdleTimeout, DefaultFunctionIdleTimeout),
		State:               newState(), client: c, Log: slog.New(slog.DiscardHandler)}
	where := net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(ControlPort))
	conn, err := c.connect(ctx, host, ControlPort, c.connectTimeout())
	if err != nil {
		var ne net.Error
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			return nil, connectionError(err, "%s refused the connection (nothing listening on the Ultimatte text port: wrong address, or not an Ultimatte)", where)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
			return nil, connectionError(err, "%s did not answer within %s (off, unplugged, or not reachable from here)", where, pySeconds(c.connectTimeout()))
		}
		return nil, connectionError(err, "%s: %v", where, err)
	}
	s.conn = conn
	if err := s.readPrelude(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func connectionError(cause error, format string, args ...any) *Error {
	return &Error{Class: "UltimatteConnectionError", Msg: fmt.Sprintf(format, args...), kind: &wrapped{ErrConnection, cause}}
}

func timeoutError(format string, args ...any) *Error {
	return &Error{Class: "UltimatteTimeout", Msg: fmt.Sprintf(format, args...), kind: ErrTimeout}
}

// wrapped is a kind with the error that caused it.
type wrapped struct{ kind, cause error }

func (w *wrapped) Error() string   { return w.kind.Error() }
func (w *wrapped) Unwrap() []error { return []error{w.kind, w.cause} }

// pySeconds is a duration as Python's {:g} of its seconds.
func pySeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'g', 6, 64) + "s"
}

// Connected is whether the session is open.
func (s *Session) Connected() bool { return s.conn != nil }

// Close ends the session; closing a closed one does nothing.
func (s *Session) Close() error {
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}

func (s *Session) where() string {
	return net.JoinHostPort(strings.Trim(s.Host, "[]"), strconv.Itoa(ControlPort))
}

func (s *Session) readPrelude(ctx context.Context) error {
	gotAny := false
	for {
		block, err := s.recvBlock(ctx, s.IdleTimeout)
		if errors.Is(err, ErrConnection) {
			if gotAny {
				return connectionError(err, "%s closed the connection in the middle of its status dump (before END PRELUDE)", s.where())
			}
			return connectionError(err, "%s accepted the connection and closed it without a word (no free connection on the unit, or not an Ultimatte)", s.where())
		}
		if err != nil {
			return err
		}
		if block == nil {
			note := ""
			if !gotAny {
				note = " (nothing at all: not an Ultimatte?)"
			}
			return timeoutError("%s: status dump stalled, nothing for %s before END PRELUDE%s", s.where(), pySeconds(s.IdleTimeout), note)
		}
		gotAny = true
		if s.apply(block) == PreludeEnd {
			s.State.PreludeComplete = true
			return nil
		}
	}
}

// Subscribe calls fn with every block the unit sends from now on (only
// those with one of headers, given without the colon, if any), in the
// caller's goroutine inside whichever method is reading. A panic in fn is
// logged, not raised. It returns the function that unsubscribes.
func (s *Session) Subscribe(fn func(Update), headers ...string) (unsubscribe func()) {
	sub := &subscriber{fn: fn}
	if len(headers) > 0 {
		sub.want = map[string]bool{}
		for _, h := range headers {
			sub.want[strings.ToUpper(strings.TrimSpace(strings.TrimRight(h, ":")))] = true
		}
	}
	s.subs = append(s.subs, sub)
	return func() {
		if i := slices.Index(s.subs, sub); i >= 0 {
			s.subs = slices.Delete(s.subs, i, i+1)
		}
	}
}

func (s *Session) require() error {
	if s.conn == nil {
		return &Error{Class: "UltimatteConnectionError", Msg: "not connected", kind: ErrConnection}
	}
	return nil
}

// Poll reads what the unit pushes: it waits up to timeout of silence,
// applies every block that arrives (subscribers are called) and returns
// them. Poll(ctx, 0) takes only what is already waiting.
func (s *Session) Poll(ctx context.Context, timeout time.Duration) ([]Update, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	var out []Update
	for {
		wait := max(timeout, 0)
		if len(out) > 0 {
			wait = 0
		}
		block, err := s.recvBlock(ctx, wait)
		if err != nil {
			return out, err
		}
		if block == nil {
			return out, nil
		}
		s.apply(block)
		out = append(out, *s.last)
	}
}

// WaitFor reads pushes until pred(state) holds; an UltimatteTimeout if
// the unit goes quiet for idle first (0: IdleTimeout). For checking that
// the unit really holds a change.
func (s *Session) WaitFor(ctx context.Context, pred func(*State) bool, idle time.Duration) error {
	if err := s.require(); err != nil {
		return err
	}
	clock := newIdleClock(or(idle, s.IdleTimeout))
	for !pred(s.State) {
		block, err := s.recvBlock(ctx, clock.left())
		if err != nil {
			return err
		}
		if block == nil {
			return timeoutError("%s: the expected state did not arrive (%s)", s.Host, clock.silence())
		}
		clock.saw(block)
		s.apply(block)
	}
	return nil
}

// Refresh asks the unit to resend a whole block (Refresh(ctx, "CONTROL"),
// "NETWORK INTERFACE 0") and returns its lines once applied. A header with
// several blocks (GPI LIST) returns the first. FRAME BUFFER, IMAGE LIST,
// FILE LIST and CURRENT FILE go out as Software Control spells them
// (Frame Buffer:).
func (s *Session) Refresh(ctx context.Context, header string) ([]string, error) {
	h := strings.ToUpper(strings.TrimSpace(strings.TrimRight(header, ":")))
	sent := h
	if sp, ok := refreshSpelling[h]; ok {
		sent = sp
	}
	return s.command(ctx, sent, nil, func(got string, _ []string) bool { return got == h+":" }, false, 0)
}

// GetControl is a control's value from state, no traffic; ok false when
// the unit hasn't reported it.
func (s *Session) GetControl(name string) (any, bool) {
	v, ok := s.State.Controls[s.wireName(name)]
	return v, ok
}

// SetControl sets one control and returns the value the unit reports
// back. value is checked against the document's range or choices, the
// model's footnotes and the protocol version before anything is sent
// (unless NoValidate). Without NoWait it returns once the unit's CONTROL
// update carrying the control arrives: the document says to trust that,
// not the ACK, and a 12 4K echoes a value the control already has too. A
// unit that ACKs and then won't (MESSAGE: Control Locked - Enable
// 'Cursor' function) is an UltimatteMessageError.
func (s *Session) SetControl(ctx context.Context, name string, value any, o Options) (any, error) {
	got, err := s.SetControls(ctx, []ControlValue{{name, value}}, o)
	if err != nil {
		return nil, err
	}
	return got[s.wireName(name)], nil
}

// SetControls sets several controls in one CONTROL block. Every value is
// checked first, so a bad one sends nothing. It returns the values as the
// unit reported them (as sent, with NoWait).
func (s *Session) SetControls(ctx context.Context, values []ControlValue, o Options) (map[string]any, error) {
	if len(values) == 0 {
		return nil, &Error{Class: "ValueError", Msg: "nothing to set", kind: ErrValue}
	}
	var lines, names []string
	for _, v := range values {
		wire := s.wireName(v.Name)
		text, err := s.format(wire, v.Value, !o.NoValidate)
		if err != nil {
			return nil, err
		}
		lines = append(lines, wire+": "+text)
		names = append(names, wire)
	}
	if o.NoWait {
		if _, err := s.command(ctx, "CONTROL", lines, nil, false, 0); err != nil {
			return nil, err
		}
		out := map[string]any{}
		for i, n := range names {
			_, v, _ := strings.Cut(lines[i], ": ")
			out[n] = ParseValue(n, v)
		}
		return out, nil
	}
	seen := map[string]bool{}
	done := func(h string, block []string) bool {
		if h == "CONTROL:" {
			for _, ln := range block[1:] {
				k, _, _ := strings.Cut(ln, ":")
				seen[pytext.Lower(pytext.Strip(k))] = true
			}
		}
		for _, n := range names {
			if !seen[pytext.Lower(n)] {
				return false
			}
		}
		return true
	}
	if _, err := s.command(ctx, "CONTROL", lines, done, true, 0); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, n := range names {
		out[n] = s.State.Controls[n]
	}
	return out, nil
}

// OffsetControl changes a numeric control by delta (Offset <name>:
// <delta>) and returns the value the unit reports (nil with NoWait).
// "Only controls with numerical ranges support this relative mode."
func (s *Session) OffsetControl(ctx context.Context, name string, delta any, o Options) (any, error) {
	wire := s.wireName(name)
	text := fmt.Sprint(delta)
	if !o.NoValidate {
		c, ok := Lookup(wire)
		if !ok {
			return nil, controlValueError("no control %s in the document", pyRepr(name))
		}
		var err error
		if text, err = FormatOffset(c, delta, s.Context()); err != nil {
			return nil, err
		}
	}
	line := "Offset " + wire + ": " + text
	if o.NoWait {
		_, err := s.command(ctx, "CONTROL", []string{line}, nil, false, 0)
		return nil, err
	}
	if _, err := s.command(ctx, "CONTROL", []string{line}, hasKeys("CONTROL:", wire), true, 0); err != nil {
		return nil, err
	}
	return s.State.Controls[wire], nil
}

// RunFunction runs a function control (Factory Defaults, Auto Screen
// Sample, Sample Wall, Matte Reset, ...): it sends yes and returns once
// the unit says it is done, by yes as the document says, or by a whole
// CONTROL DEFAULT block, which is how a 12 4K ended Sample Wall / Sample
// Floor (captured 2026-11-12), never saying yes. idle (0:
// FunctionIdleTimeout) is how long the unit may stay silent meanwhile.
func (s *Session) RunFunction(ctx context.Context, name string, idle time.Duration) error {
	wire := s.wireName(name)
	c, ok := Lookup(wire)
	if !ok || c.Kind != KindFunction {
		return controlValueError("%s is not a function control", pyRepr(name))
	}
	if _, err := FormatValue(c, true, s.Context()); err != nil {
		return err
	}
	done := func(h string, block []string) bool {
		if h == "CONTROL DEFAULT:" {
			return true
		}
		if h != "CONTROL:" {
			return false
		}
		for _, ln := range block[1:] {
			k, v, _ := strings.Cut(ln, ":")
			if pytext.Lower(pytext.Strip(k)) == pytext.Lower(wire) {
				if yes, ok := ParseBool(v); ok && yes {
					return true
				}
			}
		}
		return false
	}
	_, err := s.command(ctx, "CONTROL", []string{wire + ": yes"}, done, true, or(idle, s.FunctionIdleTimeout))
	return err
}

// wireName is the name as the unit spells it: its own key if it sent
// one, else the document's.
func (s *Session) wireName(name string) string {
	key := strings.Join(pytext.Fields(name), " ")
	for k := range s.State.Controls {
		if pytext.Lower(k) == pytext.Lower(key) {
			return k
		}
	}
	if c, ok := Lookup(key); ok {
		return c.Name
	}
	return key
}

func (s *Session) format(wire string, value any, validate bool) (string, error) {
	c, known := Lookup(wire)
	if !validate || !known {
		if !known && validate {
			if _, ok := s.State.Controls[wire]; !ok {
				return "", controlValueError("no control %s in the document or on this unit (validate=False sends it anyway)", pyRepr(wire))
			}
		}
		if b, ok := value.(bool); ok {
			if b {
				return "on", nil
			}
			return "off", nil
		}
		text := fmt.Sprint(value)
		if strings.ContainsAny(text, "\n\r") {
			return "", controlValueError("a value can't contain a line break")
		}
		return text, nil
	}
	return FormatValue(c, value, s.Context())
}

// Context is what the value checks know about this unit: model, protocol,
// the running format and its VIDEO FORMATS list.
func (s *Session) Context() Context {
	st := s.State
	return Context{Family: st.Family(), Protocol: st.Protocol(), VideoFormat: st.Device.VideoFormat,
		VideoFormats: slices.Clone(st.VideoFormats)}
}

func oneLine(text string) (string, error) {
	if strings.ContainsAny(text, "\n\r") {
		return "", controlValueError("a name or value can't contain a line break")
	}
	return text, nil
}

func oneLines(texts ...string) error {
	for _, t := range texts {
		if _, err := oneLine(t); err != nil {
			return err
		}
	}
	return nil
}

// LoadFile loads a saved file (preset) and returns CURRENT FILE as the
// unit reports it after; a MESSAGE answer is an UltimatteMessageError.
func (s *Session) LoadFile(ctx context.Context, name string) (CurrentFile, error) {
	if err := oneLines(name); err != nil {
		return CurrentFile{}, err
	}
	_, err := s.command(ctx, "FILE", []string{"Load: " + name}, func(h string, _ []string) bool { return h == "CURRENT FILE:" }, true, 0)
	return s.State.CurrentFile, err
}

// SaveFile saves the live settings as file name and returns FILE LIST.
func (s *Session) SaveFile(ctx context.Context, name string) ([]string, error) {
	return s.fileOp(ctx, []string{"Save: " + name}, name)
}

// DeleteFile deletes file name and returns FILE LIST.
func (s *Session) DeleteFile(ctx context.Context, name string) ([]string, error) {
	return s.fileOp(ctx, []string{"Delete: " + name}, name)
}

// RenameFile renames file name to newName and returns FILE LIST.
func (s *Session) RenameFile(ctx context.Context, name, newName string) ([]string, error) {
	return s.fileOp(ctx, []string{"Rename: " + name, "To: " + newName}, name, newName)
}

func (s *Session) fileOp(ctx context.Context, lines []string, names ...string) ([]string, error) {
	if err := oneLines(names...); err != nil {
		return nil, err
	}
	_, err := s.command(ctx, "FILE", lines, func(h string, _ []string) bool { return h == "FILE LIST:" }, true, 0)
	return s.State.FileList, err
}

// GPIInsert inserts filename into GPI gpi's event list at index at (-1:
// the end) and returns the GPI LIST the unit sends after.
func (s *Session) GPIInsert(ctx context.Context, gpi int, filename string, at int) (GPIList, error) {
	if at < -1 {
		return GPIList{}, controlValueError("At is an index, or -1 for the end")
	}
	if err := oneLines(filename); err != nil {
		return GPIList{}, err
	}
	return s.gpi(ctx, gpi, []string{"Insert: " + filename, "At: " + strconv.Itoa(at)})
}

// GPIRemove removes event index from GPI gpi's list; 0 removes them all.
func (s *Session) GPIRemove(ctx context.Context, gpi, index int) (GPIList, error) {
	if index < 0 {
		return GPIList{}, controlValueError("Remove takes an event index, or 0 for all")
	}
	return s.gpi(ctx, gpi, []string{"Remove: " + strconv.Itoa(index)})
}

// GPISetIndex sets GPI gpi's current event index.
func (s *Session) GPISetIndex(ctx context.Context, gpi, index int) (GPIList, error) {
	if index < 0 {
		return GPIList{}, controlValueError("an event index can't be negative")
	}
	return s.gpi(ctx, gpi, []string{"Index: " + strconv.Itoa(index)})
}

func (s *Session) gpi(ctx context.Context, gpi int, lines []string) (GPIList, error) {
	if family := s.State.Family(); !HasGPI(family) {
		return GPIList{}, unsupportedError("the %s has no GPI inputs (no GPI LIST)", family)
	}
	if gpi < 1 {
		return GPIList{}, controlValueError("GPI IDs start at 1")
	}
	expect := func(h string, block []string) bool {
		id, ok := gpiID(block)
		return h == "GPI LIST:" && ok && id == gpi
	}
	if _, err := s.command(ctx, "GPI", append([]string{"ID: " + strconv.Itoa(gpi)}, lines...), expect, true, 0); err != nil {
		return GPIList{}, err
	}
	if g, ok := s.State.GPILists[gpi]; ok {
		return g, nil
	}
	return GPIList{ID: gpi, Events: []string{}}, nil
}

// SetFrameBuffer assigns frame buffer index (an image slot, see
// State.FrameBuffer.Images) to buffer (BG 1, LY 1, BG 2, LY 2, GM, HM)
// and/or enables it, in one block, index first as the document's
// example. nil leaves either alone; both nil is refused.
func (s *Session) SetFrameBuffer(ctx context.Context, buffer string, index *int, enable *bool) (FrameBuffer, error) {
	name, err := s.bufferName(buffer)
	if err != nil {
		return FrameBuffer{}, err
	}
	var lines []string
	if index != nil {
		count := s.State.FrameBuffer.Count
		if *index < 0 || count != nil && *index > *count {
			c := "None"
			if count != nil {
				c = strconv.Itoa(*count)
			}
			return FrameBuffer{}, controlValueError("frame buffer index %d outside 0-%s", *index, c)
		}
		lines = append(lines, fmt.Sprintf("%s Frame Buffer Index: %d", name, *index))
	}
	if enable != nil {
		lines = append(lines, fmt.Sprintf("%s Frame Buffer Enable: %s", name, onOff(*enable)))
	}
	if len(lines) == 0 {
		return FrameBuffer{}, &Error{Class: "ValueError", Msg: "nothing to set", kind: ErrValue}
	}
	var keys []string
	for _, ln := range lines {
		k, _, _ := strings.Cut(ln, ":")
		keys = append(keys, k)
	}
	if _, err := s.command(ctx, "FRAME BUFFER", lines, hasKeys("FRAME BUFFER:", keys...), false, 0); err != nil {
		return FrameBuffer{}, err
	}
	if b, ok := s.State.FrameBuffer.Buffers[name]; ok {
		return *b, nil
	}
	return FrameBuffer{Name: name}, nil
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// AssignImage puts the image called image on buffer. On a unit that
// names images per buffer (the 12 4K) it sends Frame Buffer: /
// Background 1: <image> as Software Control does, and returns once the
// unit's FRAME BUFFER shows it there; a unit that ACKs and then refuses
// (an image not the running format's size, no FG input locked) is an
// UltimatteMessageError, and enable doesn't apply. After an assignment
// to a background or a mask the 12 4K sets that input's BM/GM/HM Input
// Offset: 10000 and Input Level: 6878 itself (captured 2026-11-12);
// those pushes are applied when the session next reads. Otherwise it is
// the document's form: the image's Frame Buffer n number on buffer.
func (s *Session) AssignImage(ctx context.Context, buffer, image string, enable bool) (FrameBuffer, error) {
	fb := s.State.FrameBuffer
	_, numbered := fb.ImageNumber(image)
	if len(fb.Assigned) > 0 || !numbered && slices.Contains(s.State.ImageList, image) {
		return s.assignNamed(ctx, buffer, image)
	}
	n, ok := fb.ImageNumber(image)
	if !ok {
		return FrameBuffer{}, controlValueError("no frame buffer holds an image called %s", pyRepr(image))
	}
	return s.SetFrameBuffer(ctx, buffer, &n, &enable)
}

func (s *Session) assignNamed(ctx context.Context, buffer, image string) (FrameBuffer, error) {
	short, err := s.bufferName(buffer)
	if err != nil {
		return FrameBuffer{}, err
	}
	var wire string
	for _, n := range NamedBuffers {
		if n[1] == short {
			wire = n[0]
		}
	}
	if wire == "" {
		return FrameBuffer{}, controlValueError("no frame buffer %s (the document's are %s)", pyRepr(buffer), strings.Join(FrameBuffers, ", "))
	}
	if err := oneLines(image); err != nil {
		return FrameBuffer{}, err
	}
	image = strings.TrimSpace(image)
	if image != "" && !slices.Contains(s.State.ImageList, image) {
		pool := strings.Join(s.State.ImageList, ", ")
		if pool == "" {
			pool = "empty"
		}
		return FrameBuffer{}, controlValueError("no image %s in the media pool (%s)", pyRepr(image), pool)
	}
	// header spelled as Software Control sends it
	if _, err := s.command(ctx, "Frame Buffer", []string{wire + ": " + image}, hasKeys("FRAME BUFFER:", wire), true, 0); err != nil {
		return FrameBuffer{}, err
	}
	if got := s.State.FrameBuffer.Assigned[short]; got != image {
		return FrameBuffer{}, &Error{Class: "UltimatteError", Msg: fmt.Sprintf("%s: %s shows %s after assigning %s", s.Host, wire, pyRepr(got), pyRepr(image)), kind: ErrRefused}
	}
	return FrameBuffer{Name: short}, nil
}

// AssignedImage is the image on buffer by name ("" none); ok false when
// the unit hasn't said.
func (s *Session) AssignedImage(buffer string) (image string, ok bool, err error) {
	short, err := s.bufferName(buffer)
	if err != nil {
		return "", false, err
	}
	fb := s.State.FrameBuffer
	if v, ok := fb.Assigned[short]; ok {
		return v, true, nil
	}
	if b, ok := fb.Buffers[short]; ok && b.Index != nil {
		if b.Enable != nil && *b.Enable {
			return fb.Images[*b.Index], true, nil
		}
		return "", true, nil
	}
	return "", false, nil
}

// UploadImage loads a PNG or JPG into the media pool as name (over 9996,
// Client.UploadImage) and returns IMAGE LIST once this session has seen
// the name in it. size -1 asks r (a file) for its length. With
// checkFormat an image not the running format's size is refused before
// sending. idle (0: FunctionIdleTimeout) is how long the unit may stay
// silent before the list comes.
func (s *Session) UploadImage(ctx context.Context, name string, r io.Reader, size int64, progress func(sent, size int64) error, checkFormat bool, idle time.Duration) ([]string, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	name, err := CheckImageName(name)
	if err != nil {
		return nil, err
	}
	format := ""
	if checkFormat {
		format = s.State.Device.VideoFormat
	}
	if _, _, err := s.client.UploadImage(ctx, s.Host, name, r, size, format, progress); err != nil {
		return nil, err
	}
	err = s.WaitFor(ctx, func(st *State) bool { return slices.Contains(st.ImageList, name) }, or(idle, s.FunctionIdleTimeout))
	return s.State.ImageList, err
}

// DeleteImage removes name from the media pool with Image: / Delete:, as
// Software Control sends it. The unit clears it from any buffer holding
// it, then sends IMAGE LIST; this returns that list once the name is
// gone from it.
func (s *Session) DeleteImage(ctx context.Context, name string) ([]string, error) {
	if err := oneLines(name); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if !slices.Contains(s.State.ImageList, name) {
		return nil, controlValueError("no image %s in the media pool", pyRepr(name))
	}
	expect := func(h string, _ []string) bool {
		return h == "IMAGE LIST:" && !slices.Contains(s.State.ImageList, name)
	}
	_, err := s.command(ctx, "Image", []string{"Delete: " + name}, expect, true, 0)
	return s.State.ImageList, err
}

// FlushSettings sends CONTROL: / FLUSH SETTINGS: YES, which Software
// Control sends before an Archive: the live state written into the
// unit's saved settings. Not in the document. A unit that won't (no FG
// input locked) sends a MESSAGE after the ACK: UltimatteMessageError.
// What it sends when the flush works isn't known, so after the ACK this
// reads until quiet (0: half a second) passes with nothing.
func (s *Session) FlushSettings(ctx context.Context, quiet time.Duration) error {
	text := "CONTROL:\nFLUSH SETTINGS: YES\n"
	if _, err := s.command(ctx, "CONTROL", []string{"FLUSH SETTINGS: YES"}, nil, false, 0); err != nil {
		return err
	}
	clock := newIdleClock(or(quiet, 500*time.Millisecond))
	for {
		block, err := s.recvBlock(ctx, clock.left())
		if err != nil || block == nil {
			return err
		}
		clock.saw(block)
		s.apply(block)
		if strings.TrimSpace(block[0]) == "MESSAGE:" {
			return messageError(kvDict(block[1:], true), text)
		}
	}
}

// SetFrameBufferMix is BG / LY Frame Buffer Mix. The document gives no
// range; only a negative value is refused.
func (s *Session) SetFrameBufferMix(ctx context.Context, layer string, value int) (any, error) {
	return s.fbLayer(ctx, layer, "Frame Buffer Mix", value, func(f FrameBufferState) map[string]any { return f.Mix })
}

// SetTransitionDuration is BG / LY Transition Duration, in milliseconds.
func (s *Session) SetTransitionDuration(ctx context.Context, layer string, ms int) (any, error) {
	return s.fbLayer(ctx, layer, "Transition Duration", ms, func(f FrameBufferState) map[string]any { return f.Duration })
}

func (s *Session) fbLayer(ctx context.Context, layer, what string, value int, of func(FrameBufferState) map[string]any) (any, error) {
	l := strings.ToUpper(strings.TrimSpace(layer))
	if !slices.Contains(FrameBufferLayers, l) {
		return nil, controlValueError("frame buffer layer is BG or LY, not %s", pyRepr(l))
	}
	if value < 0 {
		return nil, controlValueError("%s %s can't be %d", l, what, value)
	}
	key := l + " " + what
	if _, err := s.command(ctx, "FRAME BUFFER", []string{fmt.Sprintf("%s: %d", key, value)}, hasKeys("FRAME BUFFER:", key), false, 0); err != nil {
		return nil, err
	}
	return of(s.State.FrameBuffer)[l], nil
}

func (s *Session) bufferName(buffer string) (string, error) {
	b := strings.ToUpper(strings.Join(pytext.Fields(buffer), " "))
	for _, n := range NamedBuffers {
		if strings.ToUpper(n[0]) == b {
			return n[1], nil
		}
	}
	for _, k := range FrameBuffers {
		if strings.ToUpper(k) == b {
			return k, nil
		}
	}
	for _, k := range slices.Sorted(maps.Keys(s.State.FrameBuffer.Buffers)) {
		if strings.ToUpper(k) == b {
			return k, nil
		}
	}
	return "", controlValueError("no frame buffer %s (the document's are %s)", pyRepr(buffer), strings.Join(FrameBuffers, ", "))
}

// SetCameraID is CAMERACONTROL Camera Id: the camera this unit's SDI
// camera control addresses, "only available on the Ultimatte 12 HD Mini".
func (s *Session) SetCameraID(ctx context.Context, id int) (*int, error) {
	if family := s.State.Family(); family != "" && !HasCameraControl(family) {
		return nil, unsupportedError("CAMERACONTROL is only on the %s, not the %s", ModelHDMini, family)
	}
	if id < 0 {
		return nil, controlValueError("camera id can't be %d", id)
	}
	if _, err := s.command(ctx, "CAMERACONTROL", []string{"Camera Id: " + strconv.Itoa(id)}, hasKeys("CAMERACONTROL:", "Camera Id"), false, 0); err != nil {
		return nil, err
	}
	return s.State.CameraID, nil
}

// SendBlock sends any block and waits for ACK and, with expect, for the
// first block with that header, whose lines it returns (nil without).
// For what the document doesn't list.
func (s *Session) SendBlock(ctx context.Context, header string, lines []string, expect string) ([]string, error) {
	h := strings.ToUpper(strings.TrimSpace(strings.TrimRight(header, ":")))
	if err := oneLines(lines...); err != nil {
		return nil, err
	}
	var want func(string, []string) bool
	if expect != "" {
		w := strings.ToUpper(strings.TrimSpace(strings.TrimRight(expect, ":"))) + ":"
		want = func(got string, _ []string) bool { return got == w }
	}
	return s.command(ctx, h, lines, want, false, 0)
}

func messageError(message pairs, block string) *Error {
	var parts []string
	for _, k := range message.order {
		if k == "" {
			parts = append(parts, message.m[k])
		} else {
			parts = append(parts, k+": "+message.m[k])
		}
	}
	return &Error{Class: "UltimatteMessageError", Msg: "unit answered with a message: " + strings.Join(parts, "; "),
		Message: message.m, Block: block, kind: ErrMessage}
}

// command sends header, lines and the blank line; waits for ACK or NAK,
// then (with expect) for the block expect accepts. Every other block on
// the way is applied; with messages, a MESSAGE after the ACK is an
// UltimatteMessageError. A timeout closes the session: a late ACK would
// be taken as the next command's.
func (s *Session) command(ctx context.Context, header string, lines []string, expect func(string, []string) bool, messages bool, idle time.Duration) ([]string, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	var text strings.Builder
	text.WriteString(header + ":\n")
	for _, ln := range lines {
		text.WriteString(ln + "\n")
	}
	text.WriteString("\n")
	idle = or(idle, s.IdleTimeout)
	if err := write(ctx, s.conn, []byte(text.String()), idle); err != nil {
		s.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, connectionError(err, "%s: connection lost sending %s (%v)", s.Host, header, err)
	}
	acked := false
	var matched []string
	clock := newIdleClock(idle)
	for {
		block, err := s.recvBlock(ctx, clock.left())
		if err != nil {
			return nil, err
		}
		if block == nil {
			s.Close()
			what := "ACK"
			if acked {
				what = "update after the ACK"
			}
			return nil, timeoutError("%s: no %s for %s (%s); session closed, connect again", s.Host, what, header, clock.silence())
		}
		clock.saw(block)
		h := strings.TrimSpace(block[0])
		switch h {
		case "ACK":
			acked = true
		case "NAK":
			return nil, &Error{Class: "UltimatteNak", Msg: "unit refused the block (NAK):\n" + strings.TrimSpace(text.String()),
				Block: text.String(), kind: ErrRefused}
		default:
			s.apply(block)
			if h == "MESSAGE:" && messages && acked {
				return nil, messageError(kvDict(block[1:], true), text.String())
			}
			if expect != nil && matched == nil && expect(h, block) {
				matched = block
			}
		}
		if acked && (expect == nil || matched != nil) {
			return matched, nil
		}
	}
}

var blockEnd = regexp.MustCompile(`\n\r?\n`)

// pollWindow is how long a read that takes "only what is already here"
// (an idle of 0) waits: Go checks a read deadline already passed before
// it looks at what has arrived, so 0 would never read anything.
const pollWindow = time.Millisecond

// recvBlock is _recv_block: one blank-line-terminated block (lines,
// header first), or nil once the unit has been silent for idle (0: only
// what is already here). Bytes arriving restart the wait, so a long dump
// that keeps coming never times out. The unit closing is an
// UltimatteConnectionError.
func (s *Session) recvBlock(ctx context.Context, idle time.Duration) ([]string, error) {
	chunk := make([]byte, 65536)
	for {
		s.buf = bytes.TrimLeft(s.buf, "\r\n") // blank lines between blocks
		if bytes.HasPrefix(s.buf, []byte(PreludeEnd)) {
			// The document ends END PRELUDE with a blank line; a unit that
			// sends only the line ends the dump too.
			if nl := bytes.IndexByte(s.buf, '\n'); nl >= 0 {
				s.buf = s.buf[nl+1:]
				return []string{PreludeEnd}, nil
			}
		}
		if loc := blockEnd.FindIndex(s.buf); loc != nil {
			raw := s.buf[:loc[0]]
			s.buf = slices.Clone(s.buf[loc[1]:])
			lines := strings.Split(pytext.Decode(raw), "\n")
			for i, ln := range lines {
				lines[i] = strings.TrimRight(ln, "\r")
			}
			return lines, nil
		}
		if s.conn == nil {
			return nil, &Error{Class: "UltimatteConnectionError", Msg: "not connected", kind: ErrConnection}
		}
		s.conn.SetReadDeadline(deadline(ctx, max(idle, pollWindow)))
		k, err := s.conn.Read(chunk)
		s.buf = append(s.buf, chunk[:k]...)
		if k > 0 {
			continue
		}
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case timedOut(err):
			return nil, nil
		case errors.Is(err, io.EOF):
			s.Close()
			return nil, connectionError(err, "%s: the unit closed the connection", s.Host)
		case err != nil:
			s.Close()
			return nil, connectionError(err, "%s: connection lost (%v)", s.Host, err)
		}
	}
}

// apply applies one block to state, tells subscribers and returns the
// header.
func (s *Session) apply(lines []string) string {
	header := strings.TrimSpace(lines[0])
	var body []string
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) != "" {
			body = append(body, ln)
		}
	}
	name := strings.TrimSuffix(header, ":")
	upper := strings.ToUpper(name)
	changes := s.applyBody(upper, header, body)
	u := Update{Header: header, Changes: changes, Lines: lines, State: s.State}
	s.last = &u
	for _, sub := range slices.Clone(s.subs) {
		if sub.want != nil && !sub.want[upper] {
			continue
		}
		s.call(sub.fn, u)
	}
	return header
}

func (s *Session) call(fn func(Update), u Update) {
	defer func() {
		if r := recover(); r != nil {
			s.Log.Error("ultimatte: subscriber failed", "header", u.Header, "panic", r)
		}
	}()
	fn(u)
}

var interfaceHeader = regexp.MustCompile(`^NETWORK INTERFACE (\d+)$`)

func (s *Session) applyBody(name, header string, body []string) any {
	st := s.State
	switch name {
	case "PROTOCOL PREAMBLE":
		kv := kvDict(body, false)
		if v, ok := kv.m["Version"]; ok {
			st.ProtocolVersion = v
		}
		return kv.m
	case "IDENTITY":
		kv := kvDict(body, false)
		set(&st.Identity.Model, kv, "Model")
		set(&st.Identity.Label, kv, "Label")
		set(&st.Identity.UniqueID, kv, "Unique ID")
		return kv.m
	case "VERSION":
		kv := kvDict(body, false)
		set(&st.Version.ProductID, kv, "Product ID")
		set(&st.Version.HardwareVersion, kv, "Hardware Version")
		set(&st.Version.SoftwareVersion, kv, "Software Version")
		set(&st.Version.SoftwareRelease, kv, "Software Release")
		return kv.m
	case "NETWORK":
		kv := kvDict(body, false)
		maps.Copy(st.Network, kv.m)
		return kv.m
	case "DEVICE":
		kv := kvDict(body, false)
		for _, k := range kv.order {
			switch pytext.Lower(k) {
			case "video format":
				st.Device.VideoFormat = kv.m[k]
			case "reference source":
				st.Device.ReferenceSource = kv.m[k]
			default:
				st.Device.Inputs[k] = kv.m[k]
			}
		}
		return kv.m
	case "VIDEO FORMATS":
		st.VideoFormats = stripped(body)
		return slices.Clone(st.VideoFormats)
	case "CONTROL", "CONTROL DEFAULT":
		target := st.Controls
		if name == "CONTROL DEFAULT" {
			target = st.ControlDefaults
		}
		changes := map[string]any{}
		for _, p := range kvPairs(body) {
			v := ParseValue(p[0], p[1])
			target[p[0]], changes[p[0]] = v, v
		}
		return changes
	case "CURRENT FILE":
		kv := kvDict(body, false)
		n, hasName := kv.m["Name"]
		status, hasStatus := kv.m["Status"]
		switch {
		case hasName || hasStatus:
			if hasName {
				st.CurrentFile.Name = n
			}
			if hasStatus {
				st.CurrentFile.Status = status
			}
		case len(body) > 0:
			st.CurrentFile = CurrentFile{Name: strings.TrimSpace(body[0])}
		}
		return st.CurrentFile
	case "FILE LIST":
		st.FileList = stripped(body)
		return slices.Clone(st.FileList)
	case "IMAGE LIST":
		images := []string{}
		for _, ln := range body {
			k, v, sep := strings.Cut(ln, ":")
			key := pytext.Lower(pytext.Strip(k))
			n, isInt := ParseInt(v)
			switch {
			case sep && key == "capacity" && isInt:
				st.ImageCapacity = &n
			case sep && key == "available" && isInt:
				st.ImageAvailable = &n
			default:
				images = append(images, strings.TrimSpace(ln))
			}
		}
		st.ImageList = images
		return slices.Clone(images)
	case "GPI LIST":
		return s.applyGPI(body)
	case "FRAME BUFFER":
		return s.applyFrameBuffer(body)
	case "CAMERACONTROL":
		kv := kvDict(body, false)
		for _, k := range kv.order {
			if pytext.Lower(k) == "camera id" {
				if n, ok := ParseInt(kv.m[k]); ok {
					st.CameraID = &n
				} else {
					st.CameraID = nil
				}
			}
		}
		return kv.m
	case "MESSAGE":
		kv := kvDict(body, true).m
		st.Messages = append(st.Messages, kv)
		if len(st.Messages) > 50 {
			st.Messages = slices.Clone(st.Messages[len(st.Messages)-50:])
		}
		return kv
	case "END PRELUDE", "ACK", "NAK":
		return nil
	}
	if m := interfaceHeader.FindStringSubmatch(name); m != nil {
		index, _ := strconv.Atoi(m[1])
		old := ""
		if iface, ok := st.Interfaces[index]; ok && iface.Raw != "" {
			if _, rest, ok := strings.Cut(iface.Raw, "\n"); ok {
				old = rest
			}
		}
		// a push carries only what changed: merge into what we have
		iface, err := ParseInterface(header+"\n"+mergeKVText(old, body), index)
		if err == nil {
			st.Interfaces[index] = iface
		}
		return kvDict(body, false).m
	}
	// "clients should ignore blocks they do not recognize"; kept raw.
	st.Other[header] = slices.Clone(body)
	if st.Other[header] == nil {
		st.Other[header] = []string{}
	}
	return slices.Clone(st.Other[header])
}

func (s *Session) applyGPI(body []string) any {
	id, ok := gpiID(append([]string{"GPI LIST:"}, body...))
	if !ok {
		return nil
	}
	gl := GPIList{ID: id, Events: []string{}}
	for _, ln := range body {
		k, v, sep := strings.Cut(ln, ":")
		key := pytext.Lower(pytext.Strip(k))
		n, isInt := ParseInt(v)
		if sep && key == "id" && isInt {
			continue
		}
		if sep && key == "index" && isInt {
			gl.Index = &n
			continue
		}
		gl.Events = append(gl.Events, strings.TrimSpace(ln))
	}
	s.State.GPILists[id] = gl
	return gl
}

var (
	fbEnableIndex = regexp.MustCompile(`(?i)^(.+?) Frame Buffer (Enable|Index)$`)
	fbMix         = regexp.MustCompile(`(?i)^(BG|LY) Frame Buffer Mix$`)
	fbDuration    = regexp.MustCompile(`(?i)^(BG|LY) Transition Duration$`)
	namedMix      = regexp.MustCompile(`^(Background|Layer) Mix$`)
	namedDuration = regexp.MustCompile(`^(Background|Layer) Transition Duration$`)
	fbNumber      = regexp.MustCompile(`(?i)^Frame Buffer (\d+)$`)
)

func intOr(v string) any {
	if n, ok := ParseInt(v); ok {
		return n
	}
	return v
}

func (s *Session) applyFrameBuffer(body []string) map[string]any {
	fb := &s.State.FrameBuffer
	changes := map[string]any{}
	for _, p := range kvPairs(body) {
		k, v := p[0], p[1]
		short, named := namedShort(k)
		m := fbEnableIndex.FindStringSubmatch(k)
		switch {
		case pytext.Lower(k) == "number of frame buffers":
			if n, ok := ParseInt(v); ok {
				fb.Count = &n
				changes[k] = n
			} else {
				fb.Count = nil
				changes[k] = nil
			}
		case fbMix.MatchString(k):
			fb.Mix[strings.ToUpper(k[:2])] = intOr(v)
			changes[k] = intOr(v)
		case fbDuration.MatchString(k):
			fb.Duration[strings.ToUpper(k[:2])] = intOr(v)
			changes[k] = intOr(v)
		case named:
			fb.Assigned[short] = v
			changes[k] = v
		case namedMix.MatchString(k):
			fb.Mix[namedLayers[strings.Fields(k)[0]]] = intOr(v)
			changes[k] = intOr(v)
		case namedDuration.MatchString(k):
			fb.Duration[namedLayers[strings.Fields(k)[0]]] = intOr(v)
			changes[k] = intOr(v)
		case fbNumber.MatchString(k):
			n, _ := strconv.Atoi(fbNumber.FindStringSubmatch(k)[1])
			fb.Images[n] = v
			changes[k] = v
		case m != nil:
			buf, ok := fb.Buffers[m[1]]
			if !ok {
				buf = &FrameBuffer{Name: m[1]}
				fb.Buffers[m[1]] = buf
			}
			if pytext.Lower(m[2]) == "enable" {
				if b, ok := ParseBool(v); ok {
					buf.Enable = &b
				} else {
					buf.Enable = nil
				}
				changes[k] = buf.Enable
			} else {
				if n, ok := ParseInt(v); ok {
					buf.Index = &n
				} else {
					buf.Index = nil
				}
				changes[k] = buf.Index
			}
		default:
			fb.Other[k] = v
			changes[k] = v
		}
	}
	return changes
}

// idleClock is _IdleClock: the no-progress clock of one wait, idle from
// the last block that wasn't the cursor colour stream. The stream's
// blocks are applied; they just don't restart it.
type idleClock struct {
	idle     time.Duration
	since    time.Time
	streamed bool
}

func newIdleClock(idle time.Duration) *idleClock {
	return &idleClock{idle: idle, since: time.Now()}
}

func (c *idleClock) left() time.Duration { return max(c.idle-time.Since(c.since), 0) }

func (c *idleClock) saw(block []string) {
	if isCursorStream(block) {
		c.streamed = true
	} else {
		c.since = time.Now()
	}
}

func (c *idleClock) silence() string {
	s := "nothing from the unit for " + pySeconds(c.idle)
	if c.streamed {
		s += " but the cursor colour stream"
	}
	return s
}

// isCursorStream: a CONTROL push carrying nothing but the colour under a
// cursor.
func isCursorStream(block []string) bool {
	if strings.TrimSpace(block[0]) != "CONTROL:" {
		return false
	}
	pairs := kvPairs(block[1:])
	for _, p := range pairs {
		if !CursorColorKeys[pytext.Lower(p[0])] {
			return false
		}
	}
	return len(pairs) > 0
}

func kvPairs(body []string) [][2]string {
	var out [][2]string
	for _, ln := range body {
		if k, v, ok := strings.Cut(ln, ":"); ok {
			out = append(out, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	return out
}

// pairs are a block's key: value items in order, the last of a key
// winning at the first's place, as a Python dict built from them.
type pairs struct {
	m     map[string]string
	order []string
}

// kvDict is _kv_dict: key: value lines, a bare line under "" with
// keepBare.
func kvDict(body []string, keepBare bool) pairs {
	p := pairs{m: map[string]string{}}
	put := func(k, v string) {
		if _, ok := p.m[k]; !ok {
			p.order = append(p.order, k)
		}
		p.m[k] = v
	}
	for _, ln := range body {
		if k, v, ok := strings.Cut(ln, ":"); ok {
			put(strings.TrimSpace(k), strings.TrimSpace(v))
		} else if keepBare && strings.TrimSpace(ln) != "" {
			put("", strings.TrimSpace(ln))
		}
	}
	return p
}

func set(field *string, kv pairs, key string) {
	if v, ok := kv.m[key]; ok {
		*field = v
	}
}

func stripped(body []string) []string {
	out := make([]string, len(body))
	for i, ln := range body {
		out[i] = strings.TrimSpace(ln)
	}
	return out
}

// mergeKVText is _merge_kv_text: a partial key: value push merged into a
// block's previous lines.
func mergeKVText(old string, body []string) string {
	var lines []string
	for _, ln := range strings.Split(old, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	keys := map[string]int{}
	for i, ln := range lines {
		k, _, _ := strings.Cut(ln, ":")
		keys[strings.TrimSpace(k)] = i
	}
	for _, ln := range body {
		k, _, _ := strings.Cut(ln, ":")
		k = strings.TrimSpace(k)
		if i, ok := keys[k]; ok {
			lines[i] = ln
		} else {
			keys[k] = len(lines)
			lines = append(lines, ln)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func gpiID(block []string) (int, bool) {
	for _, ln := range block[1:] {
		if k, v, ok := strings.Cut(ln, ":"); ok && pytext.Lower(strings.TrimSpace(k)) == "id" {
			return ParseInt(v)
		}
	}
	return 0, false
}

// hasKeys is _has_keys: true once blocks with header have carried every
// key between them.
func hasKeys(header string, keys ...string) func(string, []string) bool {
	want := map[string]bool{}
	for _, k := range keys {
		want[pytext.Lower(strings.TrimSpace(k))] = true
	}
	seen := map[string]bool{}
	return func(h string, block []string) bool {
		if h == header {
			for _, ln := range block[1:] {
				k, _, _ := strings.Cut(ln, ":")
				seen[pytext.Lower(strings.TrimSpace(k))] = true
			}
		}
		for k := range want {
			if !seen[k] {
				return false
			}
		}
		return true
	}
}
