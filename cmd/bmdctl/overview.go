// SPDX-License-Identifier: MIT

package main

// bmdctl alone: the engine and each switcher, one line each. It asks a
// running engine and never starts one, so it opens no session.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
)

// overviewHost is one line of the overview: a host in hosts.yml, or a
// switcher the engine has a session to that isn't one.
type overviewHost struct {
	Name    string `json:"name,omitempty"`
	Alias   string `json:"alias,omitempty"`
	Address string `json:"address"`
	Device  string `json:"device"`
	// Default: the host a command that names none goes to now
	// ($BMDCTL_HOST, else the last --host, else hosts.yml's default).
	Default bool             `json:"default,omitempty"`
	Session *engine.Switcher `json:"session,omitempty"`
}

func (c *cli) overview(g globals) int {
	f, _, err := c.loadHosts()
	if err != nil {
		return c.fail(exitUsage, "bmdctl: %v", err)
	}
	var ov *engine.Overview
	if conn, err := c.dialRunning(); err == nil {
		ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
		resp, err := engine.Call(ctx, conn, engine.Request{Device: "engine", Path: "overview"}, nil)
		cancel()
		conn.Close()
		if err == nil && resp.OK {
			ov = &engine.Overview{}
			json.Unmarshal(resp.Result.(json.RawMessage), ov)
		}
	} else if !errors.Is(err, errNoEngine) {
		return c.fail(codeFor(err), "bmdctl: %v", err)
	}
	// The default: where a command that names no host goes now, a host
	// or an address no host has.
	current, currentAddr := "", ""
	if t, err := c.resolveTarget(globals{}); err == nil {
		if t.Host != nil {
			current = t.Host.Label()
		} else {
			currentAddr = t.Address
		}
	}
	var list []overviewHost
	used := map[int]bool{}
	for _, h := range f.Hosts {
		oh := overviewHost{Name: h.Label(), Alias: h.Alias, Address: h.Address, Device: h.Device, Default: strings.EqualFold(h.Label(), current)}
		if ov != nil {
			for i, sw := range ov.Switchers {
				if !used[i] && hosts.SameAddress(sw.Host, h.Address) {
					used[i], oh.Session = true, &ov.Switchers[i]
					break
				}
			}
		}
		list = append(list, oh)
	}
	if ov != nil {
		for i, sw := range ov.Switchers {
			if !used[i] {
				list = append(list, overviewHost{Name: sw.Name, Address: sw.Host, Device: cmp.Or(sw.Device, "atem"), Session: &ov.Switchers[i],
					Default: currentAddr != "" && hosts.SameAddress(sw.Host, currentAddr)})
			}
		}
	}
	switch {
	case g.quiet:
		return exitOK
	case g.json:
		res := map[string]any{"engine": nil, "hosts": list}
		if ov != nil {
			res["engine"] = ov.Engine
		}
		b, _ := json.Marshal(engine.Response{OK: true, Result: res})
		fmt.Fprintf(c.stdout, "%s\n", b)
		return exitOK
	}
	lines := c.overviewLines(ov, list, len(f.Hosts) == 0)
	if c.terminal() {
		c.banner()
		lines = c.overviewScreen(ov, list)
	}
	for _, l := range lines {
		fmt.Fprintln(c.stdout, l)
	}
	return exitOK
}

// overviewScreen is the overview at a terminal: a row per switcher, its
// alias if any host has one, its name bold, its address, model and video
// mode, and at the end of the line its session: how long it has been
// open and idle, or not connected, dim. Where the rows don't fit 72
// columns the model goes, as a header's does first, then every row is
// cut at one column. The default's row starts with *, in the indent, and
// the last line says what * is. The engine is bmdctl version's.
func (c *cli) overviewScreen(ov *engine.Overview, list []overviewHost) []string {
	out := []string{""}
	if len(list) == 0 {
		out = append(out, indent+c.colour(dim, "no switchers yet"), "",
			indent+c.colour(dim, "bmdctl connect ADDRESS adds one"))
		return out
	}
	var cells [][]string
	var sessions []string
	for _, h := range list {
		row := []string{c.colour(bold, h.Name), shortAddress(h.Address), "", "", c.colour(dim, "not connected")}
		if h.Device == "ultimatte" {
			row[2], row[4] = "Ultimatte", c.colour(dim, "connects per command")
		}
		if h.Name == "" {
			row[0] = c.colour(bold, shortAddress(h.Address))
		}
		if !c.hasCommands(h.Device) {
			row[2], row[4] = h.Device, c.colour(dim, "no commands yet")
		}
		if s := h.Session; s != nil {
			if s.Product != h.Name {
				row[2] = s.Product
			}
			row[3] = s.VideoMode
			switch {
			case s.Error != "":
				row[4] = c.colour(red, "failed") + " " + c.colour(dim, s.Error)
			case !s.Ready:
				row[4] = c.colour(dim, "connecting")
			default:
				// How old it is, then dim how long it has been idle (or
				// that a watch or a subscription is using it) and that
				// it is pinned; past its idle timeout, stale, plain.
				row[4] = "●"
				ss := sessionOf(ov, s.Host)
				use := "idle " + ago(time.Duration(s.IdleS)*time.Second)
				if ss != nil {
					row[4] += " " + ago(sessionAge(*ss))
					if ss.Busy > 0 {
						use = "in use"
					}
				}
				switch {
				case ss != nil && ss.Stale:
					row[4] += c.colour(dim, " · "+use+" · ") + "stale"
				case s.Pinned:
					row[4] += c.colour(dim, " · "+use+" · pinned")
				default:
					row[4] += c.colour(dim, " · "+use)
				}
			}
		}
		if aliased(list) {
			row = append([]string{h.Alias}, row...)
		}
		cells = append(cells, row[:len(row)-1])
		sessions = append(sessions, row[len(row)-1])
	}
	room := lineWidth - 2 - statusWidth
	for _, s := range sessions {
		room = min(room, lineWidth-2-width(s))
	}
	lines := rows(cells)
	if slices.ContainsFunc(lines, func(l string) bool { return width(l) > room }) {
		model := 2
		if aliased(list) {
			model = 3
		}
		// The model goes; a short tag says what kind of device each is,
		// so a deck still reads as one.
		for i := range cells {
			cells[i][model] = c.colour(dim, deviceTag(list[i].Device))
		}
		lines = rows(cells)
	}
	marked := false
	for i, r := range lines {
		r = fit(r, room)
		if list[i].Default {
			r, marked = "*"+r[1:], true
		}
		out = append(out, trailing(r, sessions[i]))
	}
	if mine := c.build(); ov != nil && ov.Engine.Build != "" && mine != "" && mine != ov.Engine.Build {
		out = append(out, "", indent+c.colour(dim, "the engine runs another build of bmdctl · bmdctl engine stop"))
	}
	if marked {
		return append(out, "", indent+c.colour(dim, "* default · bmdctl use HOST · bmdctl help"))
	}
	return append(out, "", indent+c.colour(dim, "bmdctl help"))
}

// aliased reports whether a host in the list has an alias: the overview
// then has a column for them, before the names.
func aliased(list []overviewHost) bool {
	return slices.ContainsFunc(list, func(h overviewHost) bool { return h.Alias != "" })
}

// hasCommands says whether bmdctl has commands for a host's device: an
// ATEM's or a HyperDeck's line is a switcher's, the others say they have
// none yet. A host with no device is an ATEM.
func (c *cli) hasCommands(device string) bool {
	return device == "" || slices.Contains(c.reg.Devices(), device)
}

// sessionOf is the engine's status of its session to host, or nil.
func sessionOf(ov *engine.Overview, host string) *engine.SessionStatus {
	if ov == nil {
		return nil
	}
	for i, s := range ov.Engine.Sessions {
		if s.Host == host {
			return &ov.Engine.Sessions[i]
		}
	}
	return nil
}

// sessionAge is how long a session has been open: by the engine's clock,
// else (an engine from before it said, or a session just opened) from
// when it opened by this machine's.
func sessionAge(s engine.SessionStatus) time.Duration {
	if s.AgeS > 0 || s.Since == nil {
		return time.Duration(s.AgeS) * time.Second
	}
	return max(0, time.Since(*s.Since))
}

// sessionState is how a ready session stands, for a line of a pipe's
// overview or engine status: how old it is, how long it has been idle
// (or that a request, a watch or a subscription, is using it), pinned,
// and stale past its idle timeout.
func sessionState(ss *engine.SessionStatus, sw *engine.Switcher) []string {
	var parts []string
	idle, busy, stale := 0, false, false
	if sw != nil {
		idle = sw.IdleS
	}
	if ss != nil {
		parts = append(parts, "session "+since(sessionAge(*ss)))
		idle, busy, stale = max(idle, ss.IdleS), ss.Busy > 0, ss.Stale
	}
	if busy {
		parts = append(parts, "in use")
	} else {
		parts = append(parts, "idle "+since(time.Duration(idle)*time.Second))
	}
	switch {
	case stale:
		parts = append(parts, "stale")
	case ss != nil && ss.Pinned || sw != nil && sw.Pinned:
		parts = append(parts, "pinned")
	}
	return parts
}

// deviceTag is a device type in a few letters, for where its model has no
// room: ATEM, HD, VH, ULT.
func deviceTag(device string) string {
	return map[string]string{"atem": "ATEM", "hyperdeck": "HD", "videohub": "VH", "ultimatte": "ULT"}[device]
}

// overviewLines are the overview for a person: the engine, then a line
// per host with its model, video mode and how its session stands. With
// no hosts configured, how to add one and where the commands are. At a
// terminal the labels are dim, a switcher's name bold and its address
// dim, as everywhere.
func (c *cli) overviewLines(ov *engine.Overview, list []overviewHost, noHosts bool) []string {
	p := &printer{c: c}
	var rows [][]string
	if ov == nil {
		rows = append(rows, []string{"engine", "not running; the next command starts it"})
	} else {
		st := ov.Engine
		n := len(st.Sessions)
		line := fmt.Sprintf("running, pid %d, up %s, %d session%s", st.PID, since(time.Since(st.Started)), n, plural(n))
		if st.Commit != "" {
			line += ", commit " + st.Commit
		}
		// Builds, not commits, tell an engine of another bmdctl: two
		// builds of one dirty tree share a commit.
		if mine := c.build(); st.Build != "" && mine != "" && mine != st.Build {
			line += " (not this bmdctl's build)"
		}
		rows = append(rows, []string{"engine", line})
	}
	var hostRows [][]string
	for _, h := range list {
		row := []string{paintWho(c.colour, h.Name, shortAddress(h.Address)), "", "", c.colour(dim, "no session")}
		if h.Device == "ultimatte" {
			row[1], row[3] = "Ultimatte", c.colour(dim, "connects per command")
		}
		if !c.hasCommands(h.Device) {
			row[1] = h.Device
			row[3] = "no commands yet"
		}
		if s := h.Session; s != nil {
			row[1], row[2] = s.Product, s.VideoMode
			switch {
			case s.Error != "":
				row[3] = c.colour(red, "failed: "+s.Error)
			case !s.Ready:
				row[3] = "connecting"
			default:
				row[3] = strings.Join(append([]string{"ready"}, sessionState(sessionOf(ov, s.Host), s)...), ", ")
			}
		}
		if h.Default {
			row = append(row, c.colour(dim, "(default)"))
		}
		if aliased(list) {
			row = append([]string{h.Alias}, row...)
		}
		hostRows = append(hostRows, row)
	}
	out := append(p.table(c.dimLabels(rows)), p.table(hostRows)...)
	if noHosts {
		out = append(out, "no hosts: bmdctl hosts add ADDRESS, or bmdctl hosts discover",
			c.colour(dim, "bmdctl help lists the commands"))
	}
	return out
}
