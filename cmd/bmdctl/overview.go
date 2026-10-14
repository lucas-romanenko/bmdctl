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
	Name    string           `json:"name,omitempty"`
	Alias   string           `json:"alias,omitempty"`
	Address string           `json:"address"`
	Device  string           `json:"device"`
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
// open, or not connected, dim. The default's row starts with *, in the
// indent, and the last line says what * is. The engine is bmdctl
// version's.
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
				row[4] = "●"
				if since := sessionSince(ov, s.Host); since != nil {
					row[4] += " " + ago(time.Since(*since))
				}
				if s.Pinned {
					row[4] += c.colour(dim, " pinned")
				}
			}
		}
		if aliased(list) {
			row = append([]string{h.Alias}, row...)
		}
		cells = append(cells, row[:len(row)-1])
		sessions = append(sessions, row[len(row)-1])
	}
	marked := false
	for i, r := range rows(cells) {
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

// sessionSince is when the engine opened its session to host.
func sessionSince(ov *engine.Overview, host string) *time.Time {
	if ov == nil {
		return nil
	}
	for _, s := range ov.Engine.Sessions {
		if s.Host == host {
			return s.Since
		}
	}
	return nil
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
			case s.Pinned:
				row[3] = fmt.Sprintf("ready, idle %s, pinned", since(time.Duration(s.IdleS)*time.Second))
			default:
				row[3] = fmt.Sprintf("ready, idle %s", since(time.Duration(s.IdleS)*time.Second))
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
