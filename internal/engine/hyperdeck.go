// SPDX-License-Identifier: MIT

package engine

// HyperDeck requests (docs/PORTING.md, "HyperDeck commands"): the
// wire commands hyperdeck.RegisterCommands adds, and the operations
// that read the deck first.

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
var deckOps = byPath(deckOperations())

func deckOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "status", Summary: "Show the transport, timecode, clip, slots and recording time left."},
			run: opDeckStatus},
		{entry: table.Entry{Path: "goto", Summary: "Go to a timeline clip or a timecode, without playing.",
			Params: []table.Param{{Name: "clip", Kind: table.String, Required: true,
				Help: "a timeline clip: its number, or its name or the start of it, in any case; or a timecode, HH:MM:SS:FF"}}},
			run: opDeckGoto},
		{entry: table.Entry{Path: "loop", Summary: "Turn looping on or off for the playback under way.",
			Params: []table.Param{{Name: "on", Kind: table.Bool, Required: true, Help: "on loops the timeline, or the clip with play --single-clip"}}},
			run: opDeckLoop},
		{entry: table.Entry{Path: "clips", Summary: "List the timeline's clips, or a disk's.",
			Params: []table.Param{
				{Name: "disk", Kind: table.Bool, Default: "false", Help: "the active slot's disk, not the timeline"},
				{Name: "slot", Kind: table.Uint, Help: "this slot's disk (implies --disk)"}}},
			run: opDeckClips},
		{entry: table.Entry{Path: "timeline add", Summary: "Put a file of the active disk on the timeline: at the end, or before a timeline clip.",
			Syntax: "timeline add <name>",
			Params: []table.Param{
				{Name: "name", Kind: table.String, Required: true, Help: "the file's name on the active disk, as clips --disk lists it"},
				{Name: "before", Kind: table.Uint, Help: "put it before this timeline clip (clips lists them); else at the end"}}},
			run: opDeckClipsAdd},
		{entry: table.Entry{Path: "timeline remove", Summary: "Take a clip off the timeline; the file stays on the disk.",
			Syntax: "timeline remove <clip>",
			Params: []table.Param{{Name: "clip", Kind: table.String, Required: true,
				Help: "a timeline clip: its number, or its name or the start of it, in any case"}}},
			run: opDeckClipsRemove},
		{entry: table.Entry{Path: "timeline clear", Summary: "Empty the timeline; the files stay on the disk.", Destructive: true},
			run: opDeckClipsClear},
		{entry: table.Entry{Path: "remote", Summary: "Turn the deck's remote control on or off; off, it refuses every other write, bmdctl's included.",
			Params: []table.Param{{Name: "on", Kind: table.Bool, Required: true, Help: "on lets clients control the deck; off refuses their writes until it is on again"}}},
			run: opDeckRemote},
		{entry: table.Entry{Path: "configuration show", Summary: "Show the deck's configuration: each setting as the deck names it, and its value."},
			run: opDeckConfigurationShow},
		{entry: table.Entry{Path: "configuration set", Summary: "Change one of the deck's configuration settings, as configuration show names it.",
			Syntax: "configuration set <setting> <value>",
			Params: []table.Param{
				{Name: "setting", Kind: table.String, Required: true, Where: true,
					Help: "a setting configuration show lists (video input, record prefix, …); - or _ will do for its spaces"},
				{Name: "value", Kind: table.String, Required: true, Help: "its new value, as the deck spells it"}}},
			run: opDeckConfigurationSet},
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
	"status":             {"status"},
	"goto":               {"goto 2", "goto interview", "goto 00:01:30:00"},
	"loop":               {"loop on", "loop off"},
	"clips":              {"clips", "clips --disk", "clips --slot 2"},
	"timeline add":       {`timeline add "Interview.mov"`, `timeline add Outro.mov --before 2`},
	"timeline remove":    {"timeline remove 2", "timeline remove outro"},
	"timeline clear":     {"timeline clear", "timeline clear --yes"},
	"remote":             {"remote on", "remote off"},
	"configuration show": {"configuration show", "configuration show --json"},
	"configuration set":  {`configuration set "record prefix" Interview`, "configuration set video-input HDMI"},
	"watch":              {"watch", "watch --count 5", "watch --json"},
	"state":              {"state", "state --path transport.status"},
	"connect":            {"connect 192.168.81.90", "connect", "connect --pin false"},
	"disconnect":         {"disconnect"},
}

func registerDeckOperations(r *table.Registry) error {
	return r.Add(entries("hyperdeck", deckOperations(), deckExamples, hyperdeck.GroupOf)...)
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
	case "stop", "pause": // pause is play at speed 0, which reports stopped
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
		id, err := c.clipNamed(to, true)
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

// opDeckClipsAdd is timeline add, clips_add: a file of the active disk onto the
// timeline, at the end or before a timeline clip.
func opDeckClipsAdd(c *call) (any, bool, error) {
	cmd := hyperdeck.ClipsAdd{Name: c.args.Value("name").(string)}
	clips, err := c.timeline()
	if err != nil {
		return nil, false, err
	}
	if c.args.Has("before") {
		n := len(clips)
		b := int(c.args.Value("before").(uint64))
		if b < 1 || b > n {
			return nil, false, codeErr(CodeUsage, "--before %d: the timeline has %s", b, count(n, "clip"))
		}
		cmd.BeforeClipID = &b
	}
	if err := cmd.Validate(); err != nil {
		return nil, false, usageErr(err)
	}
	return c.timelineEdit(cmd, len(clips))
}

// opDeckClipsRemove is timeline remove, clips_remove: a timeline clip, by its number or
// name, off the timeline; the clips after it move up.
func opDeckClipsRemove(c *call) (any, bool, error) {
	id, err := c.clipNamed(strings.TrimSpace(c.args.Value("clip").(string)), false) // reads the timeline
	if err != nil {
		return nil, false, err
	}
	return c.timelineEdit(hyperdeck.ClipsRemove{ClipID: id}, len(c.deck.State().Timeline))
}

// opDeckClipsClear is timeline clear, clips_clear: the timeline emptied, nothing on disk
// deleted. Destructive: it says how many clips go, and asks.
func opDeckClipsClear(c *call) (any, bool, error) {
	clips, err := c.timeline()
	if err != nil {
		return nil, false, err
	}
	n := len(clips)
	if n == 0 {
		c.note(Note{Values: []Value{{Param: "timeline", Before: ptr("empty"), After: "empty"}}})
		return CommandResult{Already: true, Args: c.displayArgs()}, true, nil
	}
	if !c.req.Yes {
		impact := fmt.Sprintf("%s's timeline loses its %s; the files stay on the disk", orDeck(c.dstate.Name()), count(n, "clip"))
		return nil, false, c.refuse(impact, "clear it?", "%s %s is destructive: add --yes to run it", c.req.Device, c.req.Path)
	}
	return c.timelineEdit(hyperdeck.ClipsClear{}, n)
}

// timeline reads the timeline afresh: what the session holds of it is as
// of the last read, since a deck sends no notification when it changes
// (a record adds a clip, another client edits it).
func (c *call) timeline() ([]hyperdeck.Clip, error) {
	if err := c.readTimeline(); err != nil {
		return nil, err
	}
	return c.deck.State().Timeline, nil
}

// clearSettle is how long timeline clear reads the timeline back for,
// waiting for it to be empty.
const clearSettle = time.Second

// readTimeline reads the timeline into the session's state: an empty one
// is none, not the deck's 107 (Session.ClipsGet, bmdwire#57).
func (c *call) readTimeline() error {
	ctx, cancel := context.WithTimeout(c.ctx, c.req.timeout())
	defer cancel()
	_, err := c.deck.ClipsGet(ctx)
	return deckErr(hyperdeck.ClipsGet{}, err, c.req.timeout())
}

// timelineEdit sends an edit of the timeline, then reads the timeline
// again: a deck sends no notification for one. The note is the timeline
// before (as just read) and after, in clips.
func (c *call) timelineEdit(cmd hyperdeck.Command, before int) (any, bool, error) {
	timeline := func(n int) string {
		if n == 0 {
			return "empty"
		}
		return count(n, "clip")
	}
	r := CommandResult{Command: cmd.Line(), Args: c.displayArgs()}
	if _, err := c.deckSend(cmd); err != nil {
		return nil, false, err
	}
	if err := c.readTimeline(); err != nil {
		return r, true, err
	}
	// A clear is answered before the timeline is empty (bmdwire#59,
	// 80bd754: 0.16 s on a Studio HD Mini): read it back until it is,
	// clearSettle at most. Still not empty then, the result says so.
	if _, clear := cmd.(hyperdeck.ClipsClear); clear {
		for deadline := time.Now().Add(clearSettle); len(c.deck.State().Timeline) > 0 && time.Now().Before(deadline); {
			select {
			case <-c.ctx.Done():
				return r, true, c.ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
			if err := c.readTimeline(); err != nil {
				return r, true, err
			}
		}
	}
	c.note(Note{Values: []Value{{Param: "timeline", Before: ptr(timeline(before)), After: timeline(len(c.deck.State().Timeline))}}})
	return r, true, nil
}

// opDeckRemote is remote_enable: remote control on or off, read back
// after. Off, the deck refuses every other write (111), from any client.
func opDeckRemote(c *call) (any, bool, error) {
	on := c.args.Value("on").(bool)
	onOff := map[bool]string{true: "on", false: "off"}
	enabled := func(st hyperdeck.State) bool { return st.Remote["enabled"] == "true" }
	before := enabled(c.dstate)
	r := CommandResult{Args: c.displayArgs()}
	if before == on {
		r.Already = true
		c.note(Note{Values: []Value{{Param: "remote", Before: ptr(onOff[on]), After: onOff[on]}}})
		return r, true, nil
	}
	cmd := hyperdeck.RemoteEnable{Enable: on}
	r.Command = cmd.Line()
	if _, err := c.deckSend(cmd); err != nil {
		return nil, false, err
	}
	if err := c.deckAsk(hyperdeck.RemoteInfo{}); err != nil {
		return r, true, err
	}
	c.note(Note{Values: []Value{{Param: "remote", Before: ptr(onOff[before]), After: onOff[enabled(c.deck.State())]}}})
	return r, true, nil
}

// DeckConfiguration is configuration show's answer: the deck's
// configuration, each setting as the deck names it.
type DeckConfiguration struct {
	Settings map[string]string `json:"settings"`
}

// opDeckConfigurationShow reads the configuration afresh.
func opDeckConfigurationShow(c *call) (any, bool, error) {
	if err := c.deckAsk(hyperdeck.Configuration{}); err != nil {
		return nil, false, err
	}
	return DeckConfiguration{Settings: c.deck.State().Configuration}, false, nil
}

// opDeckConfigurationSet is set_configuration with one setting: checked
// against the deck's own configuration, read afresh, and read back
// after. A "213 deck rebooting" is a success the deck may close the
// connection on: then nothing is read back, and the write is sent, not
// confirmed.
func opDeckConfigurationSet(c *call) (any, bool, error) {
	name, value := settingName(c.args.Value("setting").(string)), c.args.Value("value").(string)
	if err := c.deckAsk(hyperdeck.Configuration{}); err != nil {
		return nil, false, err
	}
	conf := c.deck.State().Configuration
	before, known := conf[name]
	if !known {
		if len(conf) == 0 {
			return nil, false, codeErr(CodeUsage, "setting %q: the deck reports no configuration", name)
		}
		return nil, false, codeErr(CodeUsage, "setting %q: the deck has none so named; it has %s", name,
			strings.Join(slices.Sorted(maps.Keys(conf)), ", "))
	}
	// The setting says where (the entry's Where), the value what changed;
	// an empty one, as a record prefix may be, is none.
	orNone := func(v string) string {
		if v == "" {
			return "none"
		}
		return v
	}
	note := func(after string, more ...Value) {
		c.note(Note{Values: append([]Value{{Param: "setting", After: name},
			{Param: "value", Before: ptr(orNone(before)), After: orNone(after)}}, more...)})
	}
	r := CommandResult{Args: c.displayArgs()}
	if before == value {
		r.Already = true
		note(value)
		return r, true, nil
	}
	cmd := hyperdeck.SetConfiguration{Settings: []hyperdeck.Setting{{Name: name, Value: value}}}
	if err := cmd.Validate(); err != nil {
		return nil, false, usageErr(err)
	}
	r.Command = cmd.Line()
	resp, err := c.deckSend(cmd)
	if err != nil {
		return nil, false, err
	}
	if resp.Code == 213 {
		note(value, Value{Param: "deck", After: "rebooting"})
		return r, false, nil
	}
	if err := c.deckAsk(hyperdeck.Configuration{}); err != nil {
		return r, true, err
	}
	note(c.deck.State().Configuration[name])
	return r, true, nil
}

// settingName is a configuration setting as the deck names it: lower
// case, "-" and "_" read as spaces, as hyperdeckwire's set_configuration
// reads its keyword arguments' underscores.
func settingName(s string) string {
	s = strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

func orDeck(name string) string {
	if name == "" {
		return "the deck"
	}
	return name
}

// clipNamed finds a timeline clip by its number, or by its name or the
// start of one, ignoring case. orTimecode: the command takes a timecode
// too (goto), so the error says s isn't one either.
func (c *call) clipNamed(s string, orTimecode bool) (int, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, nil
	}
	if err := c.readTimeline(); err != nil {
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
		if orTimecode {
			return 0, codeErr(CodeUsage, "clip %q: no timeline clip is named so, and it isn't a timecode (bmdctl clips lists them)", s)
		}
		return 0, codeErr(CodeUsage, "clip %q: no timeline clip is named so (bmdctl clips lists them)", s)
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
	if err := c.readTimeline(); err != nil {
		return nil, false, err
	}
	st := c.deck.State()
	r := DeckClips{Source: "timeline", Clips: st.Timeline, Status: st.Status()}
	r.Current, _ = st.ClipID()
	return r, false, nil
}

func opDeckState(c *call) (any, bool, error) { return stateAt(c, c.deck.State()) }

func opDeckDisconnect(c *call) (any, bool, error) {
	return disconnectDevice(c, "hyperdeck", func(h *host, d *hyperdeck.Session) *Switcher { return c.e.deckNote(h, d.State(), 0) })
}

// opDeckWatch streams the notifications the deck sends: each a
// ChangeEvent with the block's key (transport, slot:2, remote,
// configuration), the deck's code as its opcode, and what it carried as
// its value.
func opDeckWatch(c *call) (any, bool, error) {
	ch, cancel := c.deck.Subscribe()
	defer cancel()
	return streamChanges(c, ch, func(ch hyperdeck.Change) (ChangeEvent, bool) {
		if ch.Kind == hyperdeck.ChangeDisconnected {
			return ChangeEvent{Kind: "disconnected", Time: now()}, true
		}
		return ChangeEvent{Kind: "state", Opcode: strconv.Itoa(ch.Code), Key: ch.Key, Value: ch.Params, Time: now()}, true
	})
}
