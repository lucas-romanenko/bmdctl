// SPDX-License-Identifier: MIT

package main

// bmdctl alone: the engine and each switcher, one line each. It asks a
// running engine and never starts one, so it opens no session.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
)

// overviewHost is one line of the overview: a host in hosts.yml, or a
// switcher the engine has a session to that isn't one.
type overviewHost struct {
	Name    string           `json:"name,omitempty"`
	Address string           `json:"address"`
	Device  string           `json:"device"`
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
	var list []overviewHost
	used := map[int]bool{}
	for _, h := range f.Hosts {
		oh := overviewHost{Name: h.Label(), Address: h.Address, Device: h.Device, Default: strings.EqualFold(h.Label(), f.Default)}
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
				list = append(list, overviewHost{Name: sw.Name, Address: sw.Host, Device: "atem", Session: &ov.Switchers[i]})
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
	c.banner()
	for _, l := range c.overviewLines(ov, list, len(f.Hosts) == 0) {
		fmt.Fprintln(c.stdout, l)
	}
	return exitOK
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
		if h.Device != "atem" {
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
		hostRows = append(hostRows, row)
	}
	out := append(p.table(c.dimLabels(rows)), p.table(hostRows)...)
	if noHosts {
		out = append(out, "no hosts: bmdctl hosts add ADDRESS, or bmdctl hosts discover",
			c.colour(dim, "bmdctl help lists the commands"))
	}
	return out
}
