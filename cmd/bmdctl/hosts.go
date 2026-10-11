// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
)

// HostEnv names the variable that names the switcher when --host doesn't.
const HostEnv = "BMDCTL_HOST"

// target is the device a command goes to.
type target struct {
	Host    *hosts.Host `json:"host,omitempty"` // a copy of its entry in hosts.yml, or nil
	Address string      `json:"address"`
	Device  string      `json:"device,omitempty"` // from hosts.yml; "" for a bare address
	From    string      `json:"from"`             // --host, BMDCTL_HOST, last used, default
}

// label is "<name> (<address>)", or the address alone for one no host
// has.
func (t target) label() string {
	if t.Host != nil {
		return fmt.Sprintf("%s (%s)", t.Host.Label(), shortAddress(t.Address))
	}
	return shortAddress(t.Address)
}

func (c *cli) hostsPath() (string, error) {
	d, err := paths.Config(c.env)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "hosts.yml"), nil
}

func (c *cli) lastHostPath() (string, error) {
	d, err := paths.State(c.env)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "last-host"), nil
}

func (c *cli) loadHosts() (*hosts.File, string, error) {
	path, err := c.hostsPath()
	if err != nil {
		return nil, "", err
	}
	f, err := hosts.Load(path)
	return f, path, err
}

// lastHost is the host the last --host named, or "".
func (c *cli) lastHost() string {
	path, err := c.lastHostPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (c *cli) setLastHost(spec string) error {
	path, err := c.lastHostPath()
	if err != nil {
		return err
	}
	if spec == "" {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(spec+"\n"), 0o600)
}

var errNoHost = errors.New("no switcher named: give --host NAME|ADDRESS, set " + HostEnv +
	", or make one the default with 'bmdctl hosts default NAME'")

// resolveTarget finds the device a command goes to: --host, else
// BMDCTL_HOST, else the host the last --host named, else hosts.yml's
// default. Each is a host's name in any case, any unambiguous start of
// one, the name it went by before, or an address (hosts.File.Resolve).
func (c *cli) resolveTarget(g globals) (target, error) {
	f, _, err := c.loadHosts()
	if err != nil {
		return target{}, err
	}
	var spec, from string
	switch {
	case g.hostSet:
		spec, from = g.host, "--host"
	case c.env(HostEnv) != "":
		spec, from = c.env(HostEnv), HostEnv
	case c.lastHost() != "":
		spec, from = c.lastHost(), "last used"
	case f.Default != "":
		spec, from = f.Default, "default"
	default:
		return target{}, errNoHost
	}
	h, addr, err := f.Resolve(spec)
	if err != nil {
		return target{}, fmt.Errorf("%s %v (bmdctl hosts lists them)", from, err)
	}
	t := target{Address: addr, From: from}
	if h != nil {
		cp := *h
		t.Host, t.Device = &cp, h.Device
	}
	return t, nil
}

// adopt takes the name a switcher gives itself (the engine's note): a
// host whose switcher now calls itself something else is renamed in
// hosts.yml, the name it went by kept as "was" so --host still takes it,
// the default and the last host following, and the rename said on stderr.
// A name another host goes by isn't taken; that is said instead.
func (c *cli) adopt(g globals, t *target, sw engine.Switcher) {
	if t.Host == nil || t.Device != "atem" || sw.Name == "" || t.Host.Name == sw.Name {
		return
	}
	f, path, err := c.loadHosts()
	if err != nil {
		return
	}
	h := f.FindAddress(t.Address)
	if h == nil || h.Name == sw.Name {
		return
	}
	old, oldLabel, addr := h.Name, h.Label(), shortAddress(t.Address)
	if err := f.Rename(h, sw.Name); err != nil {
		c.notice("%s (%s) now calls itself %q, but %v: give it a name of its own with bmdctl hosts add %s --as NAME",
			oldLabel, addr, sw.Name, err, addr)
		return
	}
	if err := f.Save(path); err != nil {
		c.notice("hosts.yml: %v", err)
		return
	}
	if strings.EqualFold(c.lastHost(), oldLabel) {
		c.setLastHost(h.Label())
	}
	cp := *h
	t.Host = &cp
	switch {
	case old == "": // added with --as while it couldn't be asked
	case h.As == "":
		c.notice("%s (%s) calls itself %q now: hosts.yml updated, and --host %q still works", old, addr, sw.Name, old)
	default:
		c.notice("%s (%s): the switcher calls itself %q now, not %q: hosts.yml updated", h.As, addr, sw.Name, old)
	}
}

const hostsUsage = `usage: bmdctl hosts [list]
       bmdctl hosts add ADDRESS [--as NAME] [--device atem|hyperdeck|videohub|ultimatte]
       bmdctl hosts discover [--for SECONDS]
       bmdctl hosts rm NAME
       bmdctl hosts default [NAME]

Names the switchers you use, in $XDG_CONFIG_HOME/bmdctl/hosts.yml, by
the names they give themselves. 'hosts add' connects to the switcher at
ADDRESS and asks its name: the one set in ATEM Setup, else its model.
--as gives it another, for two switchers with one name; a HyperDeck,
Videohub or Ultimatte needs one, as bmdctl can't ask them yet. Adding an
address again asks again. 'hosts discover' adds every switcher that
announces itself on the network (Bonjour), by its own name. When a
switcher is renamed in ATEM Setup, the next command to it renames its
host and says so; --host still takes the old name.

--host takes a host's name in any case, any unambiguous start of one,
or an address; a bare address is an ATEM unless the command line names
another device. Without --host a command goes to $BMDCTL_HOST, else the
host the last --host named, else the default. Setting the default also
makes it the host commands go to now.
`

func (c *cli) hostsCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, hostsUsage)
		return exitOK
	}
	sub := "list"
	if len(words) > 0 {
		sub, words = words[0], words[1:]
	}
	device, as, wait := "atem", "", 2*time.Second
	for i := 0; i < len(tokens); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		ok := sub == "add" && (name == "device" || name == "as") || sub == "discover" && name == "for"
		if !ok {
			return c.fail(exitUsage, "bmdctl hosts %s: unexpected %q", sub, tokens[i])
		}
		if !hasValue {
			if i+1 == len(tokens) {
				return c.fail(exitUsage, "bmdctl hosts %s: --%s needs a value", sub, name)
			}
			i++
			value = tokens[i]
		}
		switch name {
		case "device":
			device = value
		case "as":
			as = value
		case "for":
			d, err := parseDuration(value)
			if err != nil || d > time.Minute {
				return c.fail(exitUsage, "bmdctl hosts discover: --for %q: not a duration up to a minute", value)
			}
			wait = d
		}
	}
	f, path, err := c.loadHosts()
	if err != nil {
		return c.fail(exitUsage, "bmdctl hosts: %v", err)
	}
	args := func(n int, names string) bool {
		if len(words) != n {
			c.fail(exitUsage, "bmdctl hosts %s: takes %s\n\n%s", sub, names, hostsUsage)
			return false
		}
		return true
	}
	save := func() int {
		if err := f.Save(path); err != nil {
			return c.fail(exitUsage, "bmdctl hosts %s: %v", sub, err)
		}
		return exitOK
	}
	// match finds the host a word names, as --host would; an address no
	// host has is no host.
	match := func(spec string) (*hosts.Host, int) {
		h, _, err := f.Resolve(spec)
		if err == nil && h == nil {
			err = fmt.Errorf("no host named %s (bmdctl hosts lists them)", spec)
		}
		if err != nil {
			return nil, c.fail(exitUsage, "bmdctl hosts %s: %v", sub, err)
		}
		return h, exitOK
	}
	switch sub {
	case "list":
		if !args(0, "nothing") {
			return exitUsage
		}
		return c.printHosts(g, f)
	case "add":
		if len(words) == 2 && as == "" {
			// hosts add NAME ADDRESS, the form before switchers named
			// their hosts.
			c.notice("hosts add NAME ADDRESS is deprecated, use: bmdctl hosts add %s --as %s", words[1], shellQuote(words[0]))
			as, words = words[0], words[1:]
		}
		if !args(1, "ADDRESS") {
			return exitUsage
		}
		return c.hostsAdd(g, f, path, words[0], as, device)
	case "discover":
		if !args(0, "nothing") {
			return exitUsage
		}
		return c.hostsDiscover(g, f, path, wait)
	case "rm":
		if !args(1, "NAME") {
			return exitUsage
		}
		h, code := match(words[0])
		if h == nil {
			return code
		}
		removed := *h
		f.Remove(removed.Label())
		if code := save(); code != exitOK {
			return code
		}
		if strings.EqualFold(c.lastHost(), removed.Label()) || removed.Was != "" && strings.EqualFold(c.lastHost(), removed.Was) {
			c.setLastHost("")
		}
		return c.emitLocal(g, removed, "removed "+removed.Label())
	case "default":
		if len(words) == 0 {
			if f.Default == "" {
				return c.emitLocal(g, map[string]string{"default": ""}, "no default host")
			}
			return c.emitLocal(g, map[string]string{"default": f.Default}, f.Default)
		}
		if !args(1, "NAME") {
			return exitUsage
		}
		h, code := match(words[0])
		if h == nil {
			return code
		}
		f.Default = h.Label()
		if code := save(); code != exitOK {
			return code
		}
		c.setLastHost(h.Label()) // commands go to the new default now
		return c.emitLocal(g, map[string]string{"default": h.Label()}, "default host: "+h.Label())
	}
	fmt.Fprint(c.stderr, hostsUsage)
	return exitUsage
}

// hostsAdd adds the device at addr, by the name its switcher gives itself
// unless as gives one. An address already a host is asked again: it goes
// by its switcher's name, or as.
func (c *cli) hostsAdd(g globals, f *hosts.File, path, addr, as, device string) int {
	if err := hosts.CheckAddress(addr); err != nil {
		return c.fail(exitUsage, "bmdctl hosts add: %v", err)
	}
	if !slices.Contains(hosts.Devices, device) {
		return c.fail(exitUsage, "bmdctl hosts add: device %q is not one of %s", device, strings.Join(hosts.Devices, ", "))
	}
	if as != "" {
		if err := hosts.CheckName(as); err != nil {
			return c.fail(exitUsage, "bmdctl hosts add: --as: %v", err)
		}
	}
	h := hosts.Host{Address: addr, Device: device, As: as}
	var sw *engine.Switcher
	if device != "atem" {
		if as == "" {
			return c.fail(exitUsage, "bmdctl hosts add: bmdctl can't ask a %s its name yet: give it one with --as NAME", device)
		}
		h.Name, h.As = as, ""
	} else {
		var err error
		switch sw, err = c.identify(g, addr); {
		case err == nil:
			h.Name = sw.Name
		case as != "":
			c.notice("%s didn't answer, so its name is unknown (%v); added as %s", shortAddress(addr), err, as)
		default:
			return c.fail(codeFor(err), "bmdctl hosts add: %v", err)
		}
	}
	if old := f.FindAddress(addr); old != nil {
		before := old.Label()
		renamed := *old
		if h.Name != "" {
			renamed.Name = h.Name
		}
		renamed.As, renamed.Device = h.As, h.Device
		if o := f.Named(renamed.Label()); o != nil && o != old {
			return c.fail(exitUsage, "bmdctl hosts add: another host goes by %q (%s): give this one its own name with --as NAME",
				o.Label(), shortAddress(o.Address))
		}
		if renamed.Label() != before {
			renamed.Was = before
		}
		*old = renamed
		if strings.EqualFold(f.Default, before) {
			f.Default = old.Label()
		}
		if err := f.Save(path); err != nil {
			return c.fail(exitUsage, "bmdctl hosts add: %v", err)
		}
		if strings.EqualFold(c.lastHost(), before) {
			c.setLastHost(old.Label())
		}
		msg := fmt.Sprintf("%s (%s) is a host already", old.Label(), shortAddress(addr))
		if old.Label() != before {
			msg = fmt.Sprintf("%s (%s) goes by %s now", before, shortAddress(addr), old.Label())
		}
		return c.emitLocal(g, *old, msg)
	}
	if err := f.Add(h); err != nil {
		return c.fail(exitUsage, "bmdctl hosts add: %v", err)
	}
	if err := f.Save(path); err != nil {
		return c.fail(exitUsage, "bmdctl hosts add: %v", err)
	}
	msg := fmt.Sprintf("added %s (%s)", h.Label(), shortAddress(addr))
	if h.As != "" && h.Name != "" {
		msg += fmt.Sprintf(", which calls itself %q", h.Name)
	}
	return c.emitLocal(g, h, msg)
}

// identify asks the switcher at addr what it calls itself, through the
// engine (so its session stays open for the next command), saying on
// stderr that it connects.
func (c *cli) identify(g globals, addr string) (*engine.Switcher, error) {
	who := shortAddress(addr)
	asking := "asking " + who + " its name"
	stop := c.spin(asking)
	defer stop()
	var sw *engine.Switcher
	var resp *engine.Response
	spawned := func() { c.banner(); c.sayConnecting(g, who) }
	err := c.call(engine.Request{Device: "atem", Host: addr, Path: "info", Notes: true}, spawned,
		func(f engine.Frame) error {
			switch {
			case f.Response != nil:
				resp = f.Response
			case f.Note != nil && f.Note.Connecting != "":
				c.sayConnecting(g, who)
			case f.Note != nil && f.Note.Switcher != nil:
				sw = f.Note.Switcher
				if c.connecting && !g.json && !g.quiet {
					c.sayConnected(sw.Name, who, *sw, asking)
				}
			}
			return nil
		})
	switch {
	case err != nil:
		return nil, err
	case resp == nil:
		return nil, exitf(exitUnreachable, "the engine hung up")
	case !resp.OK:
		return nil, &errExit{exitCode(resp.Code), errors.New(resp.Error)}
	case sw == nil:
		// An engine run by hand from another build sends no notes; info
		// has the names.
		var info engine.InfoResult
		json.Unmarshal(resp.Result.(json.RawMessage), &info)
		sw = &engine.Switcher{Name: info.Name, Product: info.Product, VideoMode: info.VideoMode}
		if sw.Name == "" {
			sw.Name = info.Product
		}
	}
	return sw, nil
}

// hostsDiscover adds every switcher that announces itself within wait,
// by its own name. One already a host is renamed if its name changed.
func (c *cli) hostsDiscover(g globals, f *hosts.File, path string, wait time.Duration) int {
	stop := c.spin(fmt.Sprintf("looking for switchers for %v", wait))
	if !g.json && !g.quiet && c.spinner == nil {
		fmt.Fprintf(c.stderr, "looking for switchers for %v …\n", wait)
	}
	found, err := c.discover(c.ctx, wait)
	stop()
	if err != nil {
		return c.fail(exitUnreachable, "bmdctl hosts discover: %v", err)
	}
	type result struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Status  string `json:"status"` // added, known, renamed, skipped
		Detail  string `json:"detail,omitempty"`
	}
	results := []result{}
	changed := false
	for _, in := range found {
		addr := ""
		for _, ip := range in.Addrs {
			if ip.To4() != nil {
				addr = ip.String()
				break
			}
		}
		if addr == "" || hosts.CheckName(in.Name) != nil {
			continue
		}
		r := result{Name: in.Name, Address: addr, Status: "added"}
		if h := f.FindAddress(addr); h != nil {
			r.Status, r.Name = "known", h.Label()
			if h.Name != in.Name {
				old := h.Label()
				if err := f.Rename(h, in.Name); err != nil {
					r.Detail = fmt.Sprintf("calls itself %q, but %v", in.Name, err)
				} else {
					r.Status, r.Name, r.Detail, changed = "renamed", h.Label(), "was "+old, true
					if strings.EqualFold(c.lastHost(), old) {
						c.setLastHost(h.Label())
					}
				}
			}
		} else if err := f.Add(hosts.Host{Name: in.Name, Address: addr, Device: "atem"}); err != nil {
			r.Status, r.Detail = "skipped", err.Error()
		} else {
			changed = true
		}
		results = append(results, r)
	}
	if changed {
		if err := f.Save(path); err != nil {
			return c.fail(exitUsage, "bmdctl hosts discover: %v", err)
		}
	}
	switch {
	case g.quiet:
	case g.json:
		b, _ := json.Marshal(engine.Response{OK: true, Result: map[string]any{"found": results}})
		fmt.Fprintf(c.stdout, "%s\n", b)
	case len(results) == 0:
		fmt.Fprintf(c.stdout, "no switcher announced itself in %v; Bonjour doesn't cross VLANs or VPNs, so add one by address: bmdctl hosts add ADDRESS\n", wait)
	default:
		var rows [][]string
		for _, r := range results {
			rows = append(rows, []string{r.Status, fmt.Sprintf("%s (%s)", r.Name, r.Address), r.Detail})
		}
		for _, l := range (&printer{c: c}).table(rows) {
			fmt.Fprintln(c.stdout, l)
		}
	}
	return exitOK
}

func (c *cli) printHosts(g globals, f *hosts.File) int {
	if g.json || g.quiet {
		return c.emitLocal(g, f, "")
	}
	if len(f.Hosts) == 0 {
		fmt.Fprintln(c.stdout, "no hosts (bmdctl hosts add ADDRESS, or bmdctl hosts discover)")
		return exitOK
	}
	var rows [][]string
	for _, h := range f.Hosts {
		mark := ""
		if strings.EqualFold(h.Label(), f.Default) {
			mark = "default"
		}
		var notes []string
		if h.As != "" && h.Name != "" {
			notes = append(notes, fmt.Sprintf("calls itself %q", h.Name))
		}
		if h.Was != "" {
			notes = append(notes, fmt.Sprintf("was %q", h.Was))
		}
		rows = append(rows, []string{h.Label(), h.Address, h.Device, mark, strings.Join(notes, ", ")})
	}
	for _, l := range (&printer{c: c}).table(rows) {
		fmt.Fprintln(c.stdout, l)
	}
	return exitOK
}

// emitLocal prints the result of something the CLI did itself: as an
// engine-shaped response with --json, else the human line.
func (c *cli) emitLocal(g globals, result any, human string) int {
	switch {
	case g.quiet:
	case g.json:
		b, _ := json.Marshal(engine.Response{OK: true, Result: result})
		fmt.Fprintf(c.stdout, "%s\n", b)
	case human != "":
		fmt.Fprintln(c.stdout, human)
	}
	return exitOK
}

// shellQuote quotes s for a shell where it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
