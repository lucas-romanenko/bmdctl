// SPDX-License-Identifier: MIT

package main

// connect ADDRESS and the device probe (docs/PORTING.md, "Device
// probe"): what answers at an address decides which device's connect
// runs, and hosts.yml records it with the device's own name.

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
)

// connectCommand is connect: to the host commands go to, or with an
// address to whatever answers there, saved to hosts.yml by its own name
// as the default. The device is the line's device word, else --type,
// else the host's type in hosts.yml, else what the probe finds.
func (c *cli) connectCommand(g globals, named string, words, tokens []string) int {
	parse := named
	if !slices.Contains(c.reg.Devices(), parse) {
		parse = "atem" // every device's connect takes the same arguments
	}
	_, raw, err := c.reg.ParseLine(parse, words, tokens)
	if err != nil || g.help {
		return c.runDevice(g, parse, words, tokens) // help, and usage errors, as ever
	}
	typ := named
	if v, ok := raw["type"]; ok {
		switch {
		case !slices.Contains(hosts.Devices, v):
			return c.fail(exitUsage, "bmdctl connect: --type %q is not one of %s", v, strings.Join(hosts.Devices, ", "))
		case named != "" && v != named:
			return c.fail(exitUsage, "bmdctl connect: --type %s, but the line names %s", v, named)
		}
		typ = v
	}
	addr := raw["address"]
	if addr == "" {
		// The host commands go to, as hosts.yml has it.
		if typ == "" {
			typ = "atem"
			if t, err := c.resolveTarget(g); err == nil && t.Device != "" {
				typ = t.Device
			}
		}
		return c.connectDevice(g, typ, words, tokens)
	}
	if typ == "" {
		if f, _, err := c.loadHosts(); err == nil {
			if h, _, err := f.Resolve(addr); err == nil && h != nil {
				typ = h.Device
			}
		}
	} else if err := c.retype(addr, typ); err != nil {
		return c.fail(exitUsage, "bmdctl connect: %v", err)
	}
	if typ == "" {
		r, err := c.probe(g, addr)
		if err != nil {
			return c.fail(codeFor(err), "bmdctl connect: %v", err)
		}
		typ, c.probed = r.Device, r
	}
	return c.connectDevice(g, typ, words, tokens)
}

// connectDevice runs a device's connect, or for a device bmdctl has no
// commands for, saves it.
func (c *cli) connectDevice(g globals, device string, words, tokens []string) int {
	if slices.Contains(c.reg.Devices(), device) {
		return c.runDevice(g, device, words, tokens)
	}
	_, raw, _ := c.reg.ParseLine("atem", words, tokens)
	if raw["address"] == "" {
		t, _ := c.resolveTarget(g)
		return c.fail(exitUsage, "bmdctl connect: %s is %s %s; bmdctl has no %s commands yet",
			t.label(), engine.DeviceArticle(device), engine.DeviceNames[device], engine.DeviceNames[device])
	}
	return c.saveDevice(g, raw["address"], device)
}

// retype makes the host at addr, if there is one, the device --type
// says.
func (c *cli) retype(addr, device string) error {
	f, path, err := c.loadHosts()
	if err != nil {
		return err
	}
	h, _, err := f.Resolve(addr)
	if err != nil || h == nil || h.Device == device {
		return nil
	}
	h.Device = device
	return f.Save(path)
}

// probe asks the engine what answers at addr: an ATEM, a HyperDeck, a
// Videohub or an Ultimatte, and its name where it says it.
func (c *cli) probe(g globals, addr string) (*engine.ProbeResult, error) {
	stop := c.spin("asking " + shortAddress(addr) + " what it is")
	defer stop()
	var resp *engine.Response
	err := c.call(engine.Request{Device: "engine", Path: "probe", Host: addr}, func() {}, func(f engine.Frame) error {
		if f.Response != nil {
			resp = f.Response
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, err
	case resp == nil:
		return nil, exitf(exitUnreachable, "the engine hung up")
	case !resp.OK:
		msg := resp.Error
		if resp.Code == engine.CodeUnreachable {
			if c.terminal() {
				// The ports are the engine's message's; the line has room
				// for what didn't answer and what to do.
				msg, _, _ = strings.Cut(msg, " as an ATEM")
			}
			msg += " (bmdctl connect ADDRESS --type DEVICE connects without asking)"
		}
		return nil, &errExit{exitCode(resp.Code), errors.New(msg)}
	}
	var r engine.ProbeResult
	if err := json.Unmarshal(resp.Result.(json.RawMessage), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// saveDevice is connect ADDRESS for a device bmdctl has no commands for
// yet: it is saved by the name it gives itself, as the default, and
// that is all.
func (c *cli) saveDevice(g globals, addr, device string) int {
	start := time.Now()
	r := c.probed
	if r == nil || r.Device != device {
		// --type said what it is; its name is still the device's to say.
		if p, err := c.probe(g, addr); err == nil && p.Device == device {
			r = p
		} else {
			r = &engine.ProbeResult{Address: addr, Device: device}
		}
	}
	name := r.Name
	if name == "" {
		name = hosts.HostPart(addr)
	}
	t := target{Address: addr}
	saved := c.remember(g, t, device, name)
	what := engine.DeviceNames[device]
	type result struct {
		engine.ProbeResult
		Saved   string `json:"saved,omitempty"`
		Added   bool   `json:"added"`
		Default bool   `json:"default"`
	}
	res := result{ProbeResult: *r, Saved: saved.name, Added: saved.added, Default: saved.name != ""}
	res.Address = addr
	switch {
	case g.quiet:
	case g.json:
		b, _ := json.Marshal(engine.Response{OK: true, Result: res, TookMS: time.Since(start).Milliseconds()})
		fmt.Fprintf(c.stdout, "%s\n", b)
	case c.terminal():
		head := c.colour(bold, name)
		parts := nonEmpty(r.Model)
		if r.Model != what {
			parts = append(parts, what)
		}
		if len(parts) > 0 {
			head += "  " + c.colour(dim, strings.Join(parts, " · "))
		}
		fmt.Fprintln(c.stdout, fit(head, lineWidth))
		switch {
		case saved.added:
			fmt.Fprintln(c.stdout, indent+c.colour(bold, fmt.Sprintf("saved as %q, now the default", saved.name)))
		case saved.defaulted:
			fmt.Fprintln(c.stdout, indent+c.colour(bold, fmt.Sprintf("%q is now the default", saved.name)))
		}
		fmt.Fprintln(c.stdout, indent+c.colour(dim, fmt.Sprintf("bmdctl has no %s commands yet", what)))
	default:
		line := fmt.Sprintf("%s (%s)  %s", name, shortAddress(addr), what)
		if saved.name != "" {
			line += fmt.Sprintf("  saved as %q, the default", saved.name)
		}
		fmt.Fprintf(c.stdout, "%s; bmdctl has no %s commands yet\n", line, what)
	}
	return exitOK
}
