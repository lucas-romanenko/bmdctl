// SPDX-License-Identifier: MIT

package engine

// The session operations: status, connect, disconnect, info, state,
// watch.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func sessionOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "status", Summary: "Show what is on air: program, preview, transitions, keyers, macros."},
			run: opStatus},
		{entry: table.Entry{Path: "connect", Summary: "Open a session to the switcher and keep it open until disconnect.",
			Params: []table.Param{{Name: "pin", Kind: table.Bool, Default: "true",
				Help: "keep the session open until disconnect; false lets it close after 10 idle minutes"}}},
			run: opConnect},
		{entry: table.Entry{Path: "disconnect", Summary: "Close the engine's session to the switcher, pinned or not."},
			run: opDisconnect, noDial: true},
		{entry: table.Entry{Path: "info", Summary: "Show the switcher's model, firmware, video mode and what it has."},
			run: opInfo},
		{entry: table.Entry{Path: "state", Summary: "Print the switcher's state as the session holds it, or one part of it.",
			Params: []table.Param{{Name: "path", Kind: table.String,
				Help: "dotted key into the state: mes.0.program, inputs.1.name, fairlight.master"}}},
			run: opState},
		{entry: table.Entry{Path: "watch", Summary: "Print each change the switcher reports, as it comes.", Stream: true,
			Params: []table.Param{
				{Name: "filter", Kind: table.List, Help: "only keys starting with one of these: PrgI,KeOn:0"},
				{Name: "count", Kind: table.Uint, Help: "stop after this many changes"},
				{Name: "all", Kind: table.Bool, Default: "false",
					Help: "include the clock's Time packets and the index and Fairlight tallies, left out unless --filter names them"}}},
			run: opWatch},
	}
}

// Source is a source id with its input's names.
type Source struct {
	ID    int    `json:"id"`
	Name  string `json:"name,omitempty"`
	Short string `json:"short,omitempty"`
}

func sourceOf(st atem.State, id int) Source {
	s := Source{ID: id}
	if l := st.InputLabel(id); l.Long != "" || l.Short != "" {
		s.Name, s.Short = l.Long, l.Short
	}
	return s
}

// StatusResult is what is on air.
type StatusResult struct {
	Session   SessionStatus `json:"session"`
	Product   string        `json:"product"`
	VideoMode string        `json:"video_mode"`
	MEs       []MEStatus    `json:"mes"`
	DSKs      []DSKStatus   `json:"dsks"`
	Aux       []AuxStatus   `json:"aux"`
	Macro     MacroStatus   `json:"macro"`
	Recording *bool         `json:"recording,omitempty"` // nil: no recorder
	Streaming string        `json:"streaming,omitempty"` // "": no streamer
}

// MEStatus is one M/E.
type MEStatus struct {
	ME         table.Index      `json:"me"`
	Program    Source           `json:"program"`
	Preview    Source           `json:"preview"`
	Transition TransitionStatus `json:"transition"`
	FTB        FTBStatus        `json:"ftb"`
	Keyers     []KeyerStatus    `json:"keyers"`
}

// TransitionStatus is one M/E's transition.
type TransitionStatus struct {
	Style        string   `json:"style"`
	Rate         int      `json:"rate"`      // frames
	RateText     string   `json:"rate_text"` // seconds:frames
	Next         []string `json:"next"`
	InTransition bool     `json:"in_transition"`
	Position     int      `json:"position"` // 0-10000
	Preview      bool     `json:"preview"`
}

// FTBStatus is one M/E's fade to black.
type FTBStatus struct {
	Active       bool `json:"active"`
	InTransition bool `json:"in_transition"`
	Rate         int  `json:"rate"`
}

// KeyerStatus is one upstream keyer.
type KeyerStatus struct {
	Keyer table.Index `json:"keyer"`
	OnAir bool        `json:"on_air"`
	Type  string      `json:"type"`
	Fill  Source      `json:"fill"`
	Key   Source      `json:"key"`
}

// DSKStatus is one downstream keyer.
type DSKStatus struct {
	DSK          table.Index `json:"dsk"`
	OnAir        bool        `json:"on_air"`
	Tie          bool        `json:"tie"`
	InTransition bool        `json:"in_transition"`
	Rate         int         `json:"rate"`
	Fill         Source      `json:"fill"`
	Key          Source      `json:"key"`
}

// AuxStatus is one aux output.
type AuxStatus struct {
	Aux    table.Index `json:"aux"`
	Source Source      `json:"source"`
}

// MacroStatus is the macro player and recorder.
type MacroStatus struct {
	Running   bool         `json:"running"`
	Waiting   bool         `json:"waiting"`
	Slot      *table.Index `json:"slot,omitempty"` // none when idle
	Name      string       `json:"name,omitempty"`
	Recording bool         `json:"recording"`
	RecSlot   *table.Index `json:"recording_slot,omitempty"` // none when not recording
}

var (
	styleNames   = []string{"mix", "dip", "wipe", "dve", "stinger"}
	keyTypeNames = []string{"luma", "chroma", "pattern", "dve"}
)

func nameOr(names []string, i int) string {
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return strconv.Itoa(i)
}

func opStatus(c *call) (any, bool, error) {
	st := c.state
	fps := atem.DisplayFPS(st)
	r := StatusResult{Session: c.e.sessionStatus(c.host), Product: st.ProductName(""), VideoMode: videoModeLabel(st),
		MEs: []MEStatus{}, DSKs: []DSKStatus{}, Aux: []AuxStatus{}}
	for me := range st.MECount() {
		sel := st.TransitionSelection(me)
		var next []string
		for i, on := range []bool{sel.Background, sel.Key1, sel.Key2, sel.Key3, sel.Key4} {
			if on {
				next = append(next, []string{"background", "key1", "key2", "key3", "key4"}[i])
			}
		}
		rate := st.ActiveTransitionRate(me)
		m := MEStatus{ME: table.Index(me), Program: sourceOf(st, st.ProgramSource(me)), Preview: sourceOf(st, st.PreviewSource(me)),
			Transition: TransitionStatus{Style: nameOr(styleNames, st.TransitionStyle(me)), Rate: rate,
				RateText: atem.FormatRate(rate, fps), Next: next, InTransition: st.TransitionInTransition(me),
				Position: int(st.TransitionPosition(me))},
			FTB:    FTBStatus{Active: st.FTBActive(me), InTransition: st.FTBInTransition(me), Rate: st.FTBRate(me)},
			Keyers: []KeyerStatus{}}
		if p := st.MEs[me].Transition.Preview; p != nil {
			m.Transition.Preview = p.Enabled
		}
		for k := range st.MEKeyerCount(me) {
			m.Keyers = append(m.Keyers, KeyerStatus{Keyer: table.Index(k), OnAir: st.USKOnAir(me, k), Type: nameOr(keyTypeNames, st.USKType(me, k)),
				Fill: sourceOf(st, st.USKFillSource(me, k)), Key: sourceOf(st, st.USKKeySource(me, k))})
		}
		r.MEs = append(r.MEs, m)
	}
	for d := range st.DSKCount() {
		v := st.DSKState(d)
		r.DSKs = append(r.DSKs, DSKStatus{DSK: table.Index(d), OnAir: v.OnAir, Tie: v.Tie, InTransition: v.InTransition, Rate: v.Rate,
			Fill: sourceOf(st, v.FillSource), Key: sourceOf(st, v.KeySource)})
	}
	for i, a := range st.Aux {
		if a != nil {
			r.Aux = append(r.Aux, AuxStatus{Aux: table.Index(i), Source: sourceOf(st, st.AuxSource(i))})
		}
	}
	r.Macro = MacroStatus{Running: st.MacroPlayRunning()}
	if p := st.Macros.PlayStatus; p != nil {
		r.Macro.Waiting = p.Waiting
		if p.Running && p.Index != 0xFFFF {
			r.Macro.Slot = table.IndexPtr(int(p.Index))
			r.Macro.Name = st.MacroEntry(int(p.Index)).Name
		}
	}
	if rs := st.Macros.RecordStatus; rs != nil && rs.IsRecording {
		r.Macro.Recording, r.Macro.RecSlot = true, table.IndexPtr(int(rs.Index))
	}
	if s := st.Recording.Status; s != nil {
		r.Recording = &s.IsRecording
	}
	if s := st.Streaming.Status; s != nil {
		r.Streaming = streamingStatus(s.Status)
	}
	return r, false, nil
}

// sessionStatus is h's entry in the engine's status.
func (e *Engine) sessionStatus(h *host) SessionStatus {
	for _, s := range e.status().Sessions {
		if s.Host == h.key {
			return s
		}
	}
	return SessionStatus{Host: h.key}
}

func opConnect(c *call) (any, bool, error) {
	pin := c.args.Value("pin").(bool)
	c.e.pin(c.host, pin)
	ss := c.e.sessionStatus(c.host)
	ss.Pinned, ss.IdleCloseS = pin, 0
	if !pin {
		ss.IdleCloseS = int(c.e.idle.Seconds()) // from when this request ends
	}
	return ss, false, nil
}

// DisconnectResult says whether there was a session to close.
type DisconnectResult struct {
	Host   string `json:"host"`
	Closed bool   `json:"closed"`
}

func opDisconnect(c *call) (any, bool, error) {
	key, err := hostKey(c.req.Host)
	if err != nil {
		return nil, false, err
	}
	closed, err := c.e.disconnect(key)
	return DisconnectResult{Host: key, Closed: closed}, false, err
}

// InfoResult is what the switcher is and has.
type InfoResult struct {
	Product      string              `json:"product"`
	Model        int                 `json:"model"`
	Name         string              `json:"name,omitempty"` // as set in ATEM Setup
	Protocol     string              `json:"protocol"`       // the firmware's protocol version
	VideoMode    string              `json:"video_mode"`
	VideoModeID  int                 `json:"video_mode_id"`
	Resolution   [2]int              `json:"resolution"`
	DisplayFPS   int                 `json:"display_fps"`
	MEs          int                 `json:"mes"`
	Keyers       []int               `json:"keyers"` // per M/E
	DSKs         int                 `json:"dsks"`
	Aux          int                 `json:"aux"`
	MediaPlayers int                 `json:"media_players"`
	Stills       int                 `json:"stills"`
	Clips        int                 `json:"clips"`
	Macros       int                 `json:"macros"`
	SuperSources int                 `json:"supersources"`
	Multiviewers int                 `json:"multiviewers"`
	HyperDecks   int                 `json:"hyperdecks"`
	Inputs       int                 `json:"inputs"`
	Audio        string              `json:"audio"` // fairlight, classic, or none
	Topology     *atem.TopologyField `json:"topology,omitempty"`
}

func opInfo(c *call) (any, bool, error) {
	st := c.state
	r := InfoResult{Product: st.ProductName(""), Name: st.DeviceName(""), VideoMode: videoModeLabel(st),
		DisplayFPS: atem.DisplayFPS(st), MEs: st.MECount(), DSKs: st.DSKCount(), Inputs: len(st.Inputs),
		Macros: len(st.Macros.Properties), Topology: st.Topology, Audio: "none", Keyers: []int{}}
	if p := st.Identity.Product; p != nil {
		r.Model = int(p.Model)
	}
	if f := st.Identity.Firmware; f != nil {
		r.Protocol = fmt.Sprintf("%d.%d", f.Major, f.Minor)
	}
	if m := st.VideoMode.Mode; m != nil {
		r.VideoModeID = int(m.Mode)
	}
	r.Resolution = st.VideoResolution([2]int{0, 0})
	for me := range r.MEs {
		r.Keyers = append(r.Keyers, st.MEKeyerCount(me))
	}
	if t := st.Topology; t != nil {
		r.Aux, r.MediaPlayers, r.SuperSources = int(t.AuxOutputs), int(t.MediaPlayers), int(t.SuperSources)
		r.Multiviewers, r.HyperDecks = int(t.Multiviewers), int(t.HyperDecks)
	}
	if s := st.MediaPool.Slots; s != nil {
		r.Stills, r.Clips = int(s.Stills), int(s.Clips)
	}
	if m := Mixer(st); m != "" {
		r.Audio = m
	}
	return r, false, nil
}

func opState(c *call) (any, bool, error) {
	raw, err := json.Marshal(c.state)
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

// selectPath follows a dotted path of JSON keys and array indices into
// raw, keeping the order of the part it returns.
func selectPath(raw json.RawMessage, path string) (json.RawMessage, error) {
	cur := raw
	var walked []string
	for _, k := range strings.Split(path, ".") {
		where := strings.Join(walked, ".")
		if where == "" {
			where = "the state"
		}
		switch t := bytes.TrimLeft(cur, " \t\r\n"); {
		case len(t) > 0 && t[0] == '{':
			var m map[string]json.RawMessage
			if err := json.Unmarshal(cur, &m); err != nil {
				return nil, err
			}
			v, ok := m[k]
			if !ok {
				for mk, mv := range m {
					if strings.EqualFold(mk, k) {
						v, ok = mv, true
					}
				}
			}
			if !ok {
				keys := make([]string, 0, len(m))
				for mk := range m {
					keys = append(keys, mk)
				}
				slices.Sort(keys)
				return nil, fmt.Errorf("--path %s: %s has no %q (it has %s)", path, where, k, strings.Join(keys, ", "))
			}
			cur = v
		case len(t) > 0 && t[0] == '[':
			var a []json.RawMessage
			if err := json.Unmarshal(cur, &a); err != nil {
				return nil, err
			}
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(a) {
				return nil, fmt.Errorf("--path %s: %s is a list of %d; %q is not an index into it", path, where, len(a), k)
			}
			cur = a[i]
		default:
			return nil, fmt.Errorf("--path %s: %s is %s, not an object or a list", path, where, t)
		}
		walked = append(walked, k)
	}
	return cur, nil
}

// ChangeEvent is one change watch reports.
type ChangeEvent struct {
	Kind   string `json:"kind"` // state, ready, disconnected
	Opcode string `json:"opcode,omitempty"`
	Key    string `json:"key,omitempty"`
	Value  any    `json:"value,omitempty"`
	Time   string `json:"time"`
}

// watchHidden are the packets watch leaves out unless --all or --filter
// names them: the clock's ticks, and the tallies the tally-source line
// already says (by index, and Fairlight's).
var watchHidden = map[string]bool{"Time": true, "TlIn": true, "FMTl": true}

// streamEnd is what a streaming operation returns when it has said all
// it will: the engine sends no final response.
type streamEnd struct{}

func opWatch(c *call) (any, bool, error) {
	ch, cancel := c.sess.Subscribe()
	defer cancel()
	var filters []string
	if c.args.Has("filter") {
		filters = c.args.Value("filter").([]string)
	}
	limit := uint64(0)
	if c.args.Has("count") {
		limit = c.args.Value("count").(uint64)
	}
	all := c.args.Value("all").(bool)
	// Watching has begun once the subscription exists: tell the caller,
	// so a script can act knowing nothing will be missed.
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
			ev := ChangeEvent{Opcode: ch.Opcode, Key: ch.Key, Value: ch.Value, Time: now()}
			switch ch.Kind {
			case atem.ChangeState:
				ev.Kind = "state"
			case atem.ChangeReady:
				ev.Kind = "ready"
			case atem.ChangeDisconnected:
				ev.Kind = "disconnected"
			}
			matches := slices.ContainsFunc(filters, func(f string) bool { return strings.HasPrefix(ch.Key, strings.TrimSpace(f)) })
			if ev.Kind == "state" && (len(filters) > 0 && !matches || watchHidden[ch.Opcode] && !all && !matches) {
				continue // noise, or said already, unless asked for
			}
			if err := c.out.response(Response{OK: true, Result: ev, TookMS: time.Since(c.start).Milliseconds()}); err != nil {
				return streamEnd{}, false, nil // the caller hung up
			}
			if n++; limit > 0 && n >= limit {
				return streamEnd{}, false, nil
			}
		}
	}
}

// streamingStatus names StRS's status as SURFACE.md §2.5 lists what was
// observed: -1 unknown, 0 nothing, 1 idle, 2 connecting, 4 on air, 22 and
// 36 stopping.
func streamingStatus(v int16) string {
	switch v {
	case -1:
		return "unknown"
	case 0:
		return "none"
	case 1:
		return "idle"
	case 2:
		return "connecting"
	case 4:
		return "on air"
	case 22, 36:
		return "stopping"
	}
	return strconv.Itoa(int(v))
}

func now() string { return time.Now().Format("15:04:05.000") }
