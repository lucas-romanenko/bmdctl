// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/paths"
)

// HostEnv names the variable that names the switcher when --host doesn't.
const HostEnv = "BMDCTL_HOST"

// target is the device a command goes to.
type target struct {
	Name    string `json:"name,omitempty"` // in hosts.yml, or ""
	Address string `json:"address"`
	Device  string `json:"device,omitempty"` // from hosts.yml; "" for a bare address
	From    string `json:"from"`             // --host, BMDCTL_HOST, last used, default
}

func (t target) label() string {
	if t.Name != "" {
		return fmt.Sprintf("%s (%s)", t.Name, t.Address)
	}
	return t.Address
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
// default. Each is a name in hosts.yml or an address.
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
	if h := f.Find(spec); h != nil {
		return target{Name: h.Name, Address: h.Address, Device: h.Device, From: from}, nil
	}
	if err := hosts.CheckAddress(spec); err != nil {
		return target{}, fmt.Errorf("%s %q is neither a host in %s nor an address", from, spec, "hosts.yml")
	}
	return target{Address: spec, From: from}, nil
}

const hostsUsage = `usage: bmdctl hosts [list]
       bmdctl hosts add NAME ADDRESS [--device atem|hyperdeck|videohub|ultimatte]
       bmdctl hosts rm NAME
       bmdctl hosts default [NAME]

Names the devices you use, in $XDG_CONFIG_HOME/bmdctl/hosts.yml. --host
takes a name from it or an address; a bare address is an ATEM unless
the command line names another device. Without --host a command goes to
$BMDCTL_HOST, else the host the last --host named, else the default.
Setting the default also makes it the host commands go to now.
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
	device := "atem"
	for i := 0; i < len(tokens); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		if sub != "add" || name != "device" {
			return c.fail(exitUsage, "bmdctl hosts %s: unexpected %q", sub, tokens[i])
		}
		if !hasValue {
			if i+1 == len(tokens) {
				return c.fail(exitUsage, "bmdctl hosts add: --device needs a value")
			}
			i++
			value = tokens[i]
		}
		device = value
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
	switch sub {
	case "list":
		if !args(0, "nothing") {
			return exitUsage
		}
		return c.printHosts(g, f)
	case "add":
		if !args(2, "NAME ADDRESS") {
			return exitUsage
		}
		h := hosts.Host{Name: words[0], Address: words[1], Device: device}
		if err := f.Add(h); err != nil {
			return c.fail(exitUsage, "bmdctl hosts add: %v", err)
		}
		if code := save(); code != exitOK {
			return code
		}
		return c.emitLocal(g, h, fmt.Sprintf("added %s: %s %s", h.Name, h.Device, h.Address))
	case "rm":
		if !args(1, "NAME") {
			return exitUsage
		}
		h := f.Find(words[0])
		if h == nil {
			return c.fail(exitUsage, "bmdctl hosts rm: no host named %s", words[0])
		}
		removed := *h
		f.Remove(removed.Name)
		if code := save(); code != exitOK {
			return code
		}
		if strings.EqualFold(c.lastHost(), removed.Name) {
			c.setLastHost("")
		}
		return c.emitLocal(g, removed, "removed "+removed.Name)
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
		h := f.Find(words[0])
		if h == nil {
			return c.fail(exitUsage, "bmdctl hosts default: no host named %s (bmdctl hosts add %s ADDRESS first)", words[0], words[0])
		}
		f.Default = h.Name
		if code := save(); code != exitOK {
			return code
		}
		c.setLastHost(h.Name) // commands go to the new default now
		return c.emitLocal(g, map[string]string{"default": h.Name}, "default host: "+h.Name)
	}
	fmt.Fprint(c.stderr, hostsUsage)
	return exitUsage
}

func (c *cli) printHosts(g globals, f *hosts.File) int {
	if g.json || g.quiet {
		return c.emitLocal(g, f, "")
	}
	if len(f.Hosts) == 0 {
		fmt.Fprintln(c.stdout, "no hosts (bmdctl hosts add NAME ADDRESS)")
		return exitOK
	}
	var rows [][]string
	for _, h := range f.Hosts {
		mark := ""
		if strings.EqualFold(h.Name, f.Default) {
			mark = "default"
		}
		rows = append(rows, []string{h.Name, h.Address, h.Device, mark})
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
