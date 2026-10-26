// SPDX-License-Identifier: MIT

package engine

// HyperDeck requests (docs/PORTING.md, "HyperDeck commands"): the
// wire commands hyperdeck.RegisterCommands adds, and the operations
// that read the deck first.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/hyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// DeckLive is the transport as a request found it: sent once, before the
// command, for a terminal's header, as OnAir is for a switcher.
type DeckLive struct {
	Status   string `json:"status"`
	Clip     int    `json:"clip,omitempty"` // 0: none
	ClipName string `json:"clip_name,omitempty"`
	Timecode string `json:"timecode,omitempty"`
	Slot     int    `json:"slot,omitempty"`
}

// deckOps are the HyperDeck's operations, by path.
var deckOps = func() map[string]operation {
	m := map[string]operation{}
	for _, o := range deckOperations() {
		m[o.entry.Path] = o
	}
	return m
}()

func deckOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "status", Summary: "Show the transport, timecode, clip, slots and recording time left."},
			run: opDeckStatus},
		{entry: table.Entry{Path: "goto", Summary: "Go to a timeline clip or a timecode, without playing.",
			Params: []table.Param{{Name: "clip", Kind: table.String, Required: true,
				Help: "a timeline clip: its number, or its name or the start of it, in any case; or a timecode, HH:MM:SS:FF"}}},
			run: opDeckGoto},
		{entry: table.Entry{Path: "loop", Summary: "Turn looping on or off for the playback under way.",
			Params:     []table.Param{{Name: "on", Kind: table.Bool, Required: true, Help: "on loops the timeline, or the clip with play --single-clip"}},
			Unverified: true},
			run: opDeckLoop},
		{entry: table.Entry{Path: "clips", Summary: "List the timeline's clips, or a disk's.",
			Params: []table.Param{
				{Name: "disk", Kind: table.Bool, Default: "false", Help: "the active slot's disk, not the timeline"},
				{Name: "slot", Kind: table.Uint, Help: "this slot's disk (implies --disk)"}}},
			run: opDeckClips},
		{entry: table.Entry{Path: "watch", Summary: "Print each change the deck reports, as it comes.", Stream: true,
			Params: []table.Param{{Name: "count", Kind: table.Uint, Help: "stop after this many changes"}}},
			run: opDeckWatch},
		{entry: table.Entry{Path: "state", Summary: "Print what the session holds of the deck, or one part of it.",
			Params: []table.Param{{Name: "path", Kind: table.String, Help: "dotted key into the state: transport.status, slots.1"}}},
			run: opDeckState},
		{entry: table.Entry{Path: "connect", Summary: "Connect to a HyperDeck and keep the session open; an address is saved as the default host.",
			Syntax: "connect [address]",
			Params: []table.Param{
				{Name: "address", Kind: table.String,
					Help: "an address (saved to hosts.yml by the name the deck gives itself, as the default) or a host's name; else the default"},
				{Name: "pin", Kind: table.Bool, Default: "true",
					Help: "keep the session open until disconnect or an hour without a command ($BMDCTL_PIN_IDLE); false lets it close after 10 idle minutes"},
				typeParam}},
			run: opConnect},
		{entry: table.Entry{Path: "disconnect", Summary: "Close the engine's session to the deck, pinned or not."},
			run: opDeckDisconnect, noDial: true},
	}
}

// deckExamples are the HyperDeck operations' examples.
var deckExamples = map[string][]string{
	"status":     {"status"},
	"goto":       {"goto 2", "goto interview", "goto 00:01:30:00"},
	"loop":       {"loop on", "loop off"},
	"clips":      {"clips", "clips --disk", "clips --slot 2"},
	"watch":      {"watch", "watch --count 5", "watch --json"},
	"state":      {"state", "state --path transport.status"},
	"connect":    {"connect 192.168.81.90", "connect", "connect --pin false"},
	"disconnect": {"disconnect"},
}

func registerDeckOperations(r *table.Registry) error {
	var es []table.Entry
	for _, o := range deckOperations() {
		e := o.entry
		e.Device, e.Operation, e.Examples, e.Group = "hyperdeck", true, deckExamples[e.Path], hyperdeck.GroupOf(e.Path)
		es = append(es, e)
	}
	return r.Add(es...)
}

// runDeck runs a HyperDeck request: the session, a fresh transport for
// the command to start from, the notes, then the command.
func (c *call) runDeck() (any, bool, error) {
	o, isOp := deckOps[c.entry.Path]
	if c.entry.Operation && !isOp {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", c.req.Device, c.req.Path)
	}
	if o.noDial {
		var err error
		if c.args, err = c.entry.Parse(c.req.Params, atem.Resolver{}); err != nil {
			return nil, false, usageErr(err)
		}
		return o.run(c)
	}
	connecting := func(key string) { c.note(Note{Connecting: key}) }
	h, s, idle, release, err := c.e.acquireSession(c.ctx, "hyperdeck", c.req.Host, connecting)
	if err != nil {
		return nil, false, err
	}
	defer release()
	c.host, c.deck = h, s.(*hyperdeck.Session)
	if err := c.deckAsk(hyperdeck.TransportInfo{}); err != nil {
		return nil, false, err
	}
	c.dstate = c.deck.State()
	c.note(Note{Switcher: c.e.deckNote(h, c.dstate, idle)})
	c.note(Note{Deck: deckLive(c.dstate)})
	if c.args, err = c.entry.Parse(c.req.Params, atem.Resolver{}); err != nil {
		return nil, false, usageErr(err)
	}
	if !c.entry.Operation {
		return c.sendDeckEntry()
	}
	return o.run(c)
}

// deckAsk sends a command and waits for its answer, which the session
// takes into its state.
func (c *call) deckAsk(cmd hyperdeck.Command) error {
	_, err := c.deckSend(cmd)
	return err
}

func (c *call) deckSend(cmd hyperdeck.Command) (hyperdeck.Response, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.req.timeout())
	defer cancel()
	r, err := c.deck.SendAndWait(ctx, cmd)
	return r, deckErr(cmd, err, c.req.timeout())
}

// deckErr gives a deck's error its code: a refusal is rejected (exit 4),
// no answer within wait (the request's timeout) no_echo (3), the session
// gone unreachable (2).
func deckErr(cmd hyperdeck.Command, err error, wait time.Duration) error {
	var de *hyperdeck.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &de):
		return codeErr(CodeRejected, "the deck refused %s: %s (%d)", strings.SplitN(cmd.Line(), ":", 2)[0], de.Text, de.Code)
	case errors.Is(err, hyperdeck.ErrTimeout):
		return codeErr(CodeNoEcho, "sent %q, but the deck did not answer within %v", cmd.Line(), wait)
	case errors.Is(err, hyperdeck.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Error{CodeUnreachable, err}
	}
	return &Error{CodeUsage, err}
}

// deckNote is the Switcher note for a deck: its name, model and video
// format, as a switcher's note has them.
func (e *Engine) deckNote(h *host, st hyperdeck.State, idle time.Duration) *Switcher {
	e.mu.Lock()
	pinned := h.pinned
	e.mu.Unlock()
	sw := &Switcher{Host: h.key, Device: "hyperdeck", Name: st.Name(), Product: st.Model(),
		VideoMode: st.Transport["video format"], Ready: true, Pinned: pinned, IdleS: int(idle.Seconds()),
		DropS: int(e.closesAfter(pinned).Seconds())}
	return sw
}

func deckLive(st hyperdeck.State) *DeckLive {
	l := &DeckLive{Status: st.Status(), Timecode: st.Timecode()}
	if id, ok := st.ClipID(); ok {
		l.Clip = id
		if cl, ok := st.Clip(id); ok {
			l.ClipName = cl.Name
		}
	}
	l.Slot, _ = st.SlotID()
	return l
}

// transportText is the transport as a line shows it: its status, with
// the speed where it isn't the status's own (play at 200%).
func transportText(st hyperdeck.State) string {
	s := st.Status()
	if s == "" {
		return "?"
	}
	if sp, ok := st.Speed(); ok && s != "stopped" && s != "record" && sp != 100 && sp != 0 {
		s += fmt.Sprintf(" %d%%", sp)
	}
	return s
}

// clipText is a timeline clip as a line shows it: its name, else its
// number; "none" off the timeline.
func clipText(st hyperdeck.State) string {
	id, ok := st.ClipID()
	if !ok {
		return "none"
	}
	if cl, ok := st.Clip(id); ok && cl.Name != "" {
		return cl.Name
	}
	return "clip " + strconv.Itoa(id)
}

// refresh reads the transport again, for what a command did.
func (c *call) refresh() (hyperdeck.State, error) {
	if err := c.deckAsk(hyperdeck.TransportInfo{}); err != nil {
		return hyperdeck.State{}, err
	}
	return c.deck.State(), nil
}

// sendDeckEntry sends a HyperDeck wire command, and says what it
// changed: the transport, the clip or the slot, before and after. One
// that would change nothing (stop while stopped, the active slot again)
// is not sent: "already".
func (c *call) sendDeckEntry() (any, bool, error) {
	built, err := c.entry.Build(c.args)
	if err != nil {
		return nil, false, usageErr(err)
	}
	cmd, ok := built.(hyperdeck.TableCommand)
	if !ok {
		return nil, false, fmt.Errorf("%T is not a HyperDeck command", built)
	}
	if err := cmd.Validate(); err != nil {
		return nil, false, usageErr(err)
	}
	before := c.dstate
	r := CommandResult{Command: cmd.Line(), Args: c.displayArgs()}
	param, read := "transport", transportText
	switch c.entry.Path {
	case "next", "prev":
		param, read = "clip", clipText
	case "slot":
		param, read = "slot", func(st hyperdeck.State) string { id, _ := st.SlotID(); return strconv.Itoa(id) }
	}
	if c.deckAlready(before) {
		r.Already = true
		c.note(Note{Values: []Value{{Param: param, Before: ptr(read(before)), After: read(before)}}})
		return r, true, nil
	}
	if _, err := c.deckSend(cmd.Command); err != nil {
		return r, false, err
	}
	after, err := c.refresh()
	if err != nil {
		return r, true, err
	}
	r.State = after.Transport
	c.note(Note{Values: []Value{{Param: param, Before: ptr(read(before)), After: read(after)}}})
	return r, true, nil
}

// deckAlready reports whether a wire command would change nothing: stop
// while stopped, play as it plays already, the active slot again.
func (c *call) deckAlready(st hyperdeck.State) bool {
	switch c.entry.Path {
	case "stop":
		return st.Status() == "stopped"
	case "slot":
		id, ok := st.SlotID()
		return ok && uint64(id) == c.args.Value("slot").(uint64)
	case "play":
		if c.args.Has("loop") || c.args.Has("single-clip") || c.args.Has("clip") {
			return false
		}
		want := int64(100)
		if c.args.Has("speed") {
			want = c.args.Value("speed").(int64)
		}
		sp, _ := st.Speed()
		if want == 0 {
			return st.Status() == "stopped"
		}
		return st.Status() == "play" && int64(sp) == want
	}
	return false
}

// DeckStatusResult is a deck's status: what it is, the transport, the
// clip it is on, its slots and how long each can still record.
type DeckStatusResult struct {
	Session   SessionStatus `json:"session"`
	Name      string        `json:"name"`
	Model     string        `json:"model"`
	Transport DeckTransport `json:"transport"`
	Clip      *DeckClip     `json:"clip,omitempty"` // none off the timeline
	Clips     int           `json:"clips"`          // on the timeline
	Slots     []DeckSlot    `json:"slots"`
	Remote    bool          `json:"remote"` // remote control is on: writes are taken
}

// DeckTransport is the transport.
type DeckTransport struct {
	Status      string `json:"status"` // preview, stopped, play, forward, rewind, jog, shuttle, record
	Speed       int    `json:"speed"`  // %
	Loop        bool   `json:"loop"`
	SingleClip  bool   `json:"single_clip"`
	Timecode    string `json:"timecode"`
	VideoFormat string `json:"video_format,omitempty"`
}

// DeckClip is a timeline clip.
type DeckClip struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Start    string `json:"start,omitempty"`
	Duration string `json:"duration"`
}

// DeckSlot is one of the deck's slots.
type DeckSlot struct {
	Slot        int    `json:"slot"`
	Active      bool   `json:"active"`
	Status      string `json:"status"` // empty, mounting, error, mounted
	Volume      string `json:"volume,omitempty"`
	RemainingS  *int   `json:"remaining_s,omitempty"` // recording time left, seconds
	VideoFormat string `json:"video_format,omitempty"`
}

func opDeckStatus(c *call) (any, bool, error) {
	n, _ := strconv.Atoi(c.dstate.Device["slot count"])
	for id := 1; id <= min(n, 16); id++ {
		if err := c.deckAsk(hyperdeck.SlotInfo{SlotID: &id}); err != nil {
			return nil, false, err
		}
	}
	if err := c.deckAsk(hyperdeck.RemoteInfo{}); err != nil {
		return nil, false, err
	}
	st := c.deck.State()
	sp, _ := st.Speed()
	r := DeckStatusResult{Session: c.e.sessionStatus(c.host), Name: st.Name(), Model: st.Model(),
		Transport: DeckTransport{Status: st.Status(), Speed: sp, Loop: st.Loop(), SingleClip: st.SingleClip(),
			Timecode: st.Timecode(), VideoFormat: st.Transport["video format"]},
		Clips: len(st.Timeline), Slots: []DeckSlot{}, Remote: st.Remote["enabled"] == "true"}
	if id, ok := st.ClipID(); ok {
		r.Clip = &DeckClip{ID: id}
		if cl, ok := st.Clip(id); ok {
			r.Clip.Name, r.Clip.Duration = cl.Name, cl.Duration
			if cl.Start != nil {
				r.Clip.Start = *cl.Start
			}
		}
	}
	active, _ := st.SlotID()
	ids := make([]int, 0, len(st.Slots))
	for id := range st.Slots {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		m := st.Slots[id]
		s := DeckSlot{Slot: id, Active: id == active, Status: m["status"], Volume: m["volume name"], VideoFormat: m["video format"]}
		if t, ok := st.RecordingTime(id); ok {
			s.RemainingS = &t
		}
		r.Slots = append(r.Slots, s)
	}
	return r, false, nil
}

// opDeckGoto goes to a timecode, a clip by number, or a clip by its name
// (exactly, ignoring case, else the one whose name it starts).
func opDeckGoto(c *call) (any, bool, error) {
	to := strings.TrimSpace(c.args.Value("clip").(string))
	before := c.dstate
	var cmd hyperdeck.Command
	param, read := "clip", clipText
	switch {
	case hyperdeck.IsTimecode(to):
		cmd, param = hyperdeck.GotoTimecode{Timecode: to}, "timecode"
		read = func(st hyperdeck.State) string { return st.Timecode() }
	default:
		id, err := c.clipNamed(to)
		if err != nil {
			return nil, false, err
		}
		cmd = hyperdeck.GotoClip{ClipID: id}
	}
	if _, err := c.deckSend(cmd); err != nil {
		return nil, false, err
	}
	after, err := c.refresh()
	if err != nil {
		return nil, true, err
	}
	c.note(Note{Values: []Value{{Param: param, Before: ptr(read(before)), After: read(after)}}})
	return CommandResult{Command: cmd.Line(), Args: c.displayArgs(), State: after.Transport}, true, nil
}

// clipNamed finds a timeline clip by its number, or by its name or the
// start of one, ignoring case.
func (c *call) clipNamed(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, nil
	}
	if err := c.deckAsk(hyperdeck.ClipsGet{}); err != nil {
		return 0, err
	}
	clips := c.deck.State().Timeline
	for _, cl := range clips {
		if strings.EqualFold(cl.Name, s) {
			return cl.ID, nil
		}
	}
	var hits []hyperdeck.Clip
	for _, cl := range clips {
		if strings.HasPrefix(strings.ToLower(cl.Name), strings.ToLower(s)) {
			hits = append(hits, cl)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0].ID, nil
	case 0:
		return 0, codeErr(CodeUsage, "clip %q: no timeline clip is named so, and it isn't a timecode (bmdctl clips lists them)", s)
	}
	var names []string
	for _, cl := range hits {
		names = append(names, fmt.Sprintf("%d %q", cl.ID, cl.Name))
	}
	return 0, codeErr(CodeUsage, "clip %q could be %s: give more of the name, or its number", s, strings.Join(names, ", "))
}

// opDeckLoop turns looping on or off for the playback under way: play
// again, as it plays, with loop set. The protocol keeps no loop setting
// apart from a playback, so a stopped deck has nothing to loop.
func opDeckLoop(c *call) (any, bool, error) {
	on := c.args.Value("on").(bool)
	st := c.dstate
	onOff := map[bool]string{true: "on", false: "off"}
	if st.Status() != "play" {
		return nil, false, codeErr(CodeUsage, "the deck is %s: loop changes a playback under way (bmdctl play --loop starts one looping)", orUnknown(st.Status()))
	}
	r := CommandResult{Args: c.displayArgs()}
	if st.Loop() == on {
		r.Already, r.Command = true, ""
		c.note(Note{Values: []Value{{Param: "loop", Before: ptr(onOff[on]), After: onOff[on]}}})
		return r, true, nil
	}
	sp, _ := st.Speed()
	single := st.SingleClip()
	cmd := hyperdeck.Play{Speed: &sp, Loop: &on, SingleClip: &single}
	r.Command = cmd.Line()
	if _, err := c.deckSend(cmd); err != nil {
		return nil, false, err
	}
	after, err := c.refresh()
	if err != nil {
		return nil, true, err
	}
	r.State = after.Transport
	c.note(Note{Values: []Value{{Param: "loop", Before: ptr(onOff[st.Loop()]), After: onOff[after.Loop()]}}})
	return r, true, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "in an unknown state"
	}
	return s
}

// DeckClips is a list of clips: the timeline's, or a slot's disk.
type DeckClips struct {
	Source  string           `json:"source"`         // timeline, disk
	Slot    int              `json:"slot,omitempty"` // the disk's slot
	Current int              `json:"current,omitempty"`
	Status  string           `json:"status,omitempty"` // the transport's, for the current clip
	Clips   []hyperdeck.Clip `json:"clips"`
}

func opDeckClips(c *call) (any, bool, error) {
	if c.args.Value("disk").(bool) || c.args.Has("slot") {
		var slot *int
		if c.args.Has("slot") {
			n := int(c.args.Value("slot").(uint64))
			slot = &n
		}
		if err := c.deckAsk(hyperdeck.DiskList{SlotID: slot}); err != nil {
			return nil, false, err
		}
		id, _ := c.dstate.SlotID()
		if slot != nil {
			id = *slot
		}
		clips := c.deck.State().Disks[id]
		if clips == nil {
			clips = []hyperdeck.Clip{}
		}
		return DeckClips{Source: "disk", Slot: id, Clips: clips}, false, nil
	}
	if err := c.deckAsk(hyperdeck.ClipsGet{}); err != nil {
		return nil, false, err
	}
	st := c.deck.State()
	r := DeckClips{Source: "timeline", Clips: st.Timeline, Status: st.Status()}
	r.Current, _ = st.ClipID()
	return r, false, nil
}

func opDeckState(c *call) (any, bool, error) {
	raw, err := json.Marshal(c.deck.State())
	if err != nil {
		return nil, false, err
	}
	if !c.args.Has("path") || c.args.Value("path") == "" {
		return json.RawMessage(raw), false, nil
	}
	sub, err := selectPath(raw, c.args.Value("path").(string))
	if err != nil {
		return nil, false, &Error{CodeUsage, err}
	}
	return sub, false, nil
}

func opDeckDisconnect(c *call) (any, bool, error) {
	key, err := deviceKey("hyperdeck", c.req.Host)
	if err != nil {
		return nil, false, err
	}
	c.e.mu.Lock()
	h := c.e.hosts[key]
	var d *hyperdeck.Session
	if h != nil {
		d, _ = h.sess.(*hyperdeck.Session)
	}
	c.e.mu.Unlock()
	if d != nil && d.Connected() {
		c.note(Note{Switcher: c.e.deckNote(h, d.State(), 0)})
	}
	closed, err := c.e.disconnectKey(key)
	return DisconnectResult{Host: key, Closed: closed}, false, err
}

// opDeckWatch streams the notifications the deck sends: each a
// ChangeEvent with the block's key (transport, slot:2, remote,
// configuration), the deck's code as its opcode, and what it carried as
// its value.
func opDeckWatch(c *call) (any, bool, error) {
	ch, cancel := c.deck.Subscribe()
	defer cancel()
	limit := uint64(0)
	if c.args.Has("count") {
		limit = c.args.Value("count").(uint64)
	}
	if err := c.out.response(Response{OK: true, Result: ChangeEvent{Kind: "watching", Time: now()},
		TookMS: time.Since(c.start).Milliseconds()}); err != nil {
		return nil, false, err
	}
	var n uint64
	for {
		select {
		case <-c.ctx.Done():
			return streamEnd{}, false, nil
		case ch, ok := <-ch:
			if !ok {
				return nil, false, codeErr(CodeUnreachable, "the session closed")
			}
			ev := ChangeEvent{Kind: "state", Opcode: strconv.Itoa(ch.Code), Key: ch.Key, Value: ch.Params, Time: now()}
			if ch.Kind == hyperdeck.ChangeDisconnected {
				ev = ChangeEvent{Kind: "disconnected", Time: now()}
			}
			if err := c.out.response(Response{OK: true, Result: ev, TookMS: time.Since(c.start).Milliseconds()}); err != nil {
				return streamEnd{}, false, nil
			}
			if n++; limit > 0 && n >= limit {
				return streamEnd{}, false, nil
			}
		}
	}
}
