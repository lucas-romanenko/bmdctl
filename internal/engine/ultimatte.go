// SPDX-License-Identifier: MIT

package engine

// Ultimatte requests (docs/PORTING.md, "Ultimatte commands"): every one
// an operation on an ultimatte.Client, which, as ultimattewire, keeps no
// session: each request opens the connections it needs.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/pytext"
	"github.com/lucas-romanenko/bmdctl/internal/table"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

// ultOps are the Ultimatte's operations, by path.
var ultOps = func() map[string]operation {
	m := map[string]operation{}
	for _, o := range ultOperations() {
		m[o.entry.Path] = o
	}
	return m
}()

func ultOperations() []operation {
	iface := table.Param{Name: "interface", Kind: table.Uint, Default: "0", Help: "the network interface: 0 on an Ultimatte 12"}
	return []operation{
		{entry: table.Entry{Path: "status", Summary: "Show the keyer: its label, its presets and its network interface."},
			run: opUltStatus},
		{entry: table.Entry{Path: "presets", Summary: "List the keyer's saved presets, as its FILE LIST names them."},
			run: opUltPresets},
		{entry: table.Entry{Path: "controls", Summary: "List the keyer's control values: raw, and in the units Smart Remote shows them.",
			Params: []table.Param{{Name: "defaults", Kind: table.Bool, Default: "false",
				Help: "the defaults (CONTROL DEFAULT), not the live values"}}},
			run: opUltControls},
		{entry: table.Entry{Path: "prelude", Summary: "Print what the keyer sends on connect, as it sends it: its label, controls and presets."},
			run: opUltPrelude},
		{entry: table.Entry{Path: "archive", Summary: "Save the keyer's presets and settings to a zip, as Smart Remote 4's Archive All does.",
			Syntax: "archive [file]",
			Params: []table.Param{{Name: "file", Kind: table.OutFile,
				Help: "the zip to write; - is stdout (default: the keyer's label .zip)"}}},
			run: opUltArchive},
		{entry: table.Entry{Path: "restore", Summary: "Push an archive's presets and settings to the keyer, as Smart Remote 4's Restore does.",
			Syntax: "restore <file>", Destructive: true,
			Params: []table.Param{{Name: "file", Kind: table.File, Required: true,
				Help: "the zip: one archive wrote, or Smart Remote's Archive All"}}},
			run: opUltRestore},
		{entry: table.Entry{Path: "network show", Summary: "Show the keyer's network interface: address, mask, gateway and DNS, in use and configured.",
			Params: []table.Param{iface}},
			run: opUltNetwork},
		{entry: table.Entry{Path: "network set", Summary: "Set the keyer's network interface, and read it back once it has reapplied.",
			Destructive: true, AtLeastOne: true,
			Params: []table.Param{iface,
				{Name: "address", Kind: table.String, Help: "the static address; goes with --netmask, as one field on the wire"},
				{Name: "netmask", Kind: table.String, Help: "the static netmask, dotted (255.255.252.0), not a prefix length"},
				{Name: "gateway", Kind: table.String, Help: "the static gateway"},
				{Name: "dns", Kind: table.List, Help: "the static DNS servers, comma-separated; empty clears them"},
				{Name: "dhcp", Kind: table.Bool, Help: "take an address by DHCP (on), or use the static one (off)"}}},
			run: opUltNetworkSet},
		{entry: table.Entry{Path: "connect", Summary: "Check that an Ultimatte answers; an address is saved as the default host.",
			Syntax: "connect [address]",
			Params: []table.Param{
				{Name: "address", Kind: table.String,
					Help: "an address (saved to hosts.yml by the keyer's label, as the default) or a host's name; else the default"},
				typeParam}},
			run: opUltConnect},
	}
}

// ultExamples are the Ultimatte operations' examples.
var ultExamples = map[string][]string{
	"status":       {"status"},
	"presets":      {"presets"},
	"controls":     {"controls", "controls --defaults"},
	"prelude":      {"prelude"},
	"archive":      {"archive", "archive keyer-a.zip"},
	"restore":      {"restore keyer-a.zip"},
	"network show": {"network show", "network show --json"},
	"network set":  {"network set --address 192.168.1.42 --netmask 255.255.255.0", "network set --gateway 192.168.1.1 --dns 192.168.1.1", "network set --dhcp on"},
	"connect":      {"connect 192.168.81.97", "connect"},
}

func registerUltOperations(r *table.Registry) error {
	var es []table.Entry
	for _, o := range ultOperations() {
		e := o.entry
		e.Device, e.Operation, e.Examples, e.Group = "ultimatte", true, ultExamples[e.Path], ultimatte.GroupOf(e.Path)
		es = append(es, e)
	}
	return r.Add(es...)
}

// runUlt runs an Ultimatte request: the unit's address, then the
// operation, which opens its own connections.
func (c *call) runUlt() (any, bool, error) {
	o, ok := ultOps[c.entry.Path]
	if !ok {
		return nil, false, codeErr(CodeUsage, "%s %s has no implementation", c.req.Device, c.req.Path)
	}
	var err error
	if c.args, err = c.entry.Parse(c.req.Params, atem.Resolver{}); err != nil {
		return nil, false, usageErr(err)
	}
	if c.ultKey, err = deviceKey("ultimatte", c.req.Host); err != nil {
		return nil, false, err
	}
	c.ultHost, _, _ = net.SplitHostPort(c.ultKey)
	c.ult = c.e.cfg.Ultimatte
	return o.run(c)
}

// ultNote is the Switcher note for a keyer: its label as its prelude has
// it (raw, not extract_label's filename form).
func (c *call) ultNote(prelude string) string {
	label, _ := ultimatte.Label(prelude)
	c.note(Note{Switcher: &Switcher{Host: c.ultKey, Device: "ultimatte", Name: label, Ready: true}})
	return label
}

// ultErr gives a keyer's error its code: a refusal (a NAK, a non-zero
// ACK) rejected (exit 4), a set that never settled no_echo (3), the unit
// not answering in full or at all unreachable (2), an argument refused
// usage. A dial that failed says the port and why, without Go's "dial
// tcp ADDR": the address is the host's, which the CLI names. A NAK names
// the block, on one line.
func ultErr(err error) error {
	var ue *ultimatte.Error
	var op *net.OpError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &op) && op.Op == "dial" && op.Addr != nil:
		_, port, _ := net.SplitHostPort(op.Addr.String())
		why := op.Err
		var se *os.SyscallError
		if errors.As(why, &se) {
			why = se.Err
		}
		return &Error{CodeUnreachable, fmt.Errorf("nothing answers on TCP %s: %w", port, why)}
	case errors.Is(err, ultimatte.ErrRefused):
		if block, ok := strings.CutPrefix(err.Error(), "unit refused the block (NAK):\n"); ok {
			head, _, _ := strings.Cut(block, "\n")
			return codeErr(CodeRejected, "the keyer refused %s (NAK)", strings.TrimSuffix(head, ":"))
		}
		return &Error{CodeRejected, err}
	case errors.Is(err, ultimatte.ErrUnsettled):
		return &Error{CodeNoEcho, err}
	case errors.Is(err, ultimatte.ErrValue):
		return &Error{CodeUsage, err}
	case errors.As(err, &ue), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Error{CodeUnreachable, err}
	}
	return codeErr(CodeUnreachable, "%v", err)
}

// prelude reads the keyer's prelude and notes its label.
func (c *call) prelude() (string, error) {
	pre, err := c.ult.ReadPrelude(c.ctx, c.ultHost)
	if err != nil || pre == "" {
		return "", c.keyerErr(err)
	}
	c.ultNote(pre)
	return pre, nil
}

// keyerErr is why a keyer's prelude didn't come: err, or with none,
// ultimattewire's empty preamble.
func (c *call) keyerErr(err error) error {
	if err == nil {
		return codeErr(CodeUnreachable, "%s sent nothing on TCP 9998: is it an Ultimatte?", c.ultHost)
	}
	return ultErr(err)
}

// UltNetwork is a keyer's network interface, as NETWORK INTERFACE n has
// it: in use and configured.
type UltNetwork struct {
	Interface     int      `json:"interface"`
	Name          string   `json:"name"`
	MAC           string   `json:"mac"`
	DHCP          *bool    `json:"dhcp"`
	Address       string   `json:"address"`
	Netmask       string   `json:"netmask"`
	Gateway       string   `json:"gateway"`
	DNS           []string `json:"dns"`
	StaticAddress string   `json:"static_address"`
	StaticNetmask string   `json:"static_netmask"`
	StaticGateway string   `json:"static_gateway"`
	StaticDNS     []string `json:"static_dns"`
}

func netView(i ultimatte.NetworkInterface) *UltNetwork {
	return &UltNetwork{Interface: i.Index, Name: i.Name, MAC: i.MAC, DHCP: i.Dynamic, Address: i.Address, Netmask: i.Netmask,
		Gateway: i.Gateway, DNS: i.DNS, StaticAddress: i.StaticAddress, StaticNetmask: i.StaticNetmask,
		StaticGateway: i.StaticGateway, StaticDNS: i.StaticDNS}
}

// UltStatusResult is a keyer's status: its label, presets and network
// interface (NetworkError where the unit wouldn't say).
type UltStatusResult struct {
	Host         string      `json:"host"`
	Label        string      `json:"label"`
	Presets      []string    `json:"presets"`
	Network      *UltNetwork `json:"network,omitempty"`
	NetworkError string      `json:"network_error,omitempty"`
}

// opUltStatus reads the network interface, whose session reads the
// prelude first: one connection for both.
func opUltStatus(c *call) (any, bool, error) {
	iface, pre, err := c.ult.ReadNetworkPrelude(c.ctx, c.ultHost, 0)
	if pre == "" {
		return nil, false, c.keyerErr(err)
	}
	r := UltStatusResult{Host: c.ultKey, Label: c.ultNote(pre), Presets: ultimatte.ParseFileList(pre)}
	if err != nil {
		r.NetworkError = ultErr(err).Error()
	} else {
		r.Network = netView(iface)
	}
	return r, false, nil
}

// UltPresets is a keyer's presets.
type UltPresets struct {
	Presets []string `json:"presets"`
}

func opUltPresets(c *call) (any, bool, error) {
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	return UltPresets{Presets: ultimatte.ParseFileList(pre)}, false, nil
}

// UltControl is one control value: its name and raw value, and where it
// has a range, its value in the units the panel shows (Display) and as
// annotate_preamble writes it (DisplayText, "100%").
type UltControl struct {
	Name        string   `json:"name"`
	Value       string   `json:"value"`
	Display     *float64 `json:"display,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	DisplayText string   `json:"display_text,omitempty"`
}

// UltControls is a keyer's CONTROL (or CONTROL DEFAULT) section.
type UltControls struct {
	Section  string       `json:"section"`
	Controls []UltControl `json:"controls"`
}

func opUltControls(c *call) (any, bool, error) {
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	section := "CONTROL"
	if c.args.Value("defaults").(bool) {
		section = "CONTROL DEFAULT"
	}
	r := UltControls{Section: section, Controls: []UltControl{}}
	for _, l := range pytext.SplitLines(ultimatte.ParseSection(pre, section)) {
		name, value, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		u := UltControl{Name: pytext.Strip(name), Value: pytext.Strip(value)}
		lookup := strings.TrimPrefix(u.Name, "Offset ")
		if d, unit, ok := ultimatte.ToDisplay(lookup, u.Value); ok {
			u.Display, u.Unit = &d, unit
			if annotated, err := ultimatte.AnnotatePreamble("CONTROL:\n" + lookup + ": " + u.Value); err == nil {
				if i := strings.LastIndex(annotated, " ("); i >= 0 {
					u.DisplayText = strings.TrimSuffix(strings.TrimSpace(annotated[i+2:]), ")")
				}
			}
		}
		r.Controls = append(r.Controls, u)
	}
	return r, false, nil
}

// UltPrelude is a keyer's prelude, as it sent it.
type UltPrelude struct {
	Text string `json:"text"`
}

func opUltPrelude(c *call) (any, bool, error) {
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	return UltPrelude{Text: pre}, false, nil
}

// UltArchive is what an archive wrote: the zip's size, the keyer's label
// (as the file's name has it), the presets and resources in it, and a
// warning for each one that couldn't be read, which is left out.
type UltArchive struct {
	Bytes     int      `json:"bytes"`
	Label     string   `json:"label"`
	Presets   []string `json:"presets"`
	Resources []string `json:"resources"`
	Warnings  []string `json:"warnings"`
}

// phases notes an operation's members as steps, a step per phase: a
// phase begins with its first member and is done with the next phase's
// first, or the end; its detail counts what went and names what didn't.
type phases struct {
	c      *call
	names  []string
	of     func(member string) int // the phase a member belongs to
	at     int
	start  time.Time
	done   []string
	failed []string
	total  []int
}

func (p *phases) begin(i int) {
	p.at, p.start, p.done, p.failed = i, time.Now(), nil, nil
	p.c.note(Note{Step: &Step{Index: i}})
}

func (p *phases) member(member string, err error) {
	if i := p.of(member); i != p.at {
		p.end()
		p.begin(i)
	}
	if err != nil {
		p.failed = append(p.failed, strings.TrimPrefix(member, "presets/")+": "+err.Error())
	} else {
		p.done = append(p.done, strings.TrimPrefix(member, "presets/"))
	}
}

// end finishes the phase under way: done, with what it did.
func (p *phases) end() {
	if p.at < 0 {
		return
	}
	detail := fmt.Sprintf("%d of %d", len(p.done), p.total[p.at])
	if len(p.failed) == 0 {
		detail = strings.Join(p.done, ", ")
	} else {
		detail += "; not " + strings.Join(p.failed, "; ")
	}
	p.c.note(Note{Step: &Step{Index: p.at, Done: true, Detail: detail, TookMS: time.Since(p.start).Milliseconds()}})
	p.at = -1
}

func opUltArchive(c *call) (any, bool, error) {
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	slots := ultimatte.ParseFileList(pre)
	p := &phases{c: c, at: -1, total: []int{len(slots), len(ultimatte.Resources)}}
	p.of = func(m string) int {
		if strings.HasPrefix(m, "presets/") {
			return 0
		}
		return 1
	}
	c.steps("presets", "settings")
	if len(slots) == 0 {
		c.note(Note{Step: &Step{Index: 0, Done: true, Detail: "none"}})
	}
	z, info, err := c.ult.Archive(c.ctx, c.ultHost, time.Now(), p.member)
	if err != nil {
		return nil, false, ultErr(err)
	}
	p.end()
	name := ""
	if !c.args.Has("file") {
		name = info.Label + ".zip"
	}
	if err := c.out.file(FileFrame{Param: "file", Name: name, Data: z}); err != nil {
		return nil, false, err
	}
	r := UltArchive{Bytes: len(z), Label: info.Label, Presets: []string{}, Resources: []string{}, Warnings: info.Warnings}
	for _, s := range slots {
		if _, ok := info.SlotSizes[s]; ok {
			r.Presets = append(r.Presets, s)
		}
	}
	for _, name := range ultimatte.Resources {
		if !slices.ContainsFunc(info.Warnings, func(w string) bool { return strings.HasPrefix(w, name+":") }) {
			r.Resources = append(r.Resources, name)
		}
	}
	return r, false, nil
}

// UltRestore is what a restore pushed: each preset, each resource (nil
// where the zip has none), and where it stopped, if it did.
type UltRestore struct {
	Presets   []ultimatte.PresetPushed `json:"presets"`
	GPI       *ultimatte.Pushed        `json:"gpi"`
	Saved     *ultimatte.Pushed        `json:"saved"`
	StoppedAt *string                  `json:"stopped_at"`
}

func opUltRestore(c *call) (any, bool, error) {
	zipBytes := c.req.Files["file"]
	plan, err := ultimatte.Plan(zipBytes)
	if err != nil {
		return nil, false, codeErr(CodeUsage, "%s %s: %v", c.entry.Label("file"), c.args.Value("file"), err)
	}
	if len(plan.Presets) == 0 && !plan.GPI && !plan.Saved {
		return nil, false, codeErr(CodeUsage, "%s %s: no presets/, GPISettings or SavedSettings in it: not an archive",
			c.entry.Label("file"), c.args.Value("file"))
	}
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	label, _ := ultimatte.Label(pre)
	if !c.req.Yes {
		return nil, false, c.refuse(restoreImpact(orKeyer(label), plan), "restore it?",
			"%s %s is destructive: add --yes to run it", c.req.Device, c.req.Path)
	}
	// archive's steps: the presets, then the settings, as many as the
	// zip has of each.
	var labels []string
	var total []int
	settings := 0
	for _, has := range []bool{plan.GPI, plan.Saved} {
		if has {
			settings++
		}
	}
	for _, s := range []struct {
		name string
		n    int
	}{{"presets", len(plan.Presets)}, {"settings", settings}} {
		if s.n > 0 {
			labels, total = append(labels, s.name), append(total, s.n)
		}
	}
	c.steps(labels...)
	p := &phases{c: c, at: -1, total: total}
	p.of = func(m string) int {
		if strings.HasPrefix(m, "presets/") || len(labels) == 1 {
			return 0
		}
		return 1
	}
	ok, info, err := c.ult.Restore(c.ctx, c.ultHost, zipBytes, func(member string, err error) {
		p.member(member, err)
		if err != nil {
			c.note(Note{Step: &Step{Index: p.at, Error: err.Error(), TookMS: time.Since(p.start).Milliseconds()}})
			p.at = -1
		}
	})
	if err != nil {
		return nil, false, ultErr(err)
	}
	if !ok {
		why := ""
		switch {
		case info.GPI != nil && info.GPI.Error != nil:
			why = *info.GPI.Error
		case info.Saved != nil && info.Saved.Error != nil:
			why = *info.Saved.Error
		case len(info.Presets) > 0 && info.Presets[len(info.Presets)-1].Error != nil:
			why = *info.Presets[len(info.Presets)-1].Error
		}
		return nil, false, codeErr(CodeRejected, "the keyer refused %s: %s; what came before it was written",
			strings.TrimPrefix(*info.StoppedAt, "preset:"), why)
	}
	p.end()
	return UltRestore{Presets: info.Presets, GPI: info.GPI, Saved: info.Saved, StoppedAt: info.StoppedAt}, false, nil
}

func orKeyer(label string) string {
	if label == "" {
		return "the keyer"
	}
	return label
}

// restoreImpact is what a restore overwrites, for the confirmation: a
// preset replaces the slot of its name, the saved settings what the
// keyer is doing now.
func restoreImpact(label string, plan ultimatte.RestorePlan) string {
	var parts []string
	if n := len(plan.Presets); n > 0 {
		parts = append(parts, fmt.Sprintf("%d preset%s", n, plural(n)))
	}
	switch {
	case plan.GPI && plan.Saved:
		parts = append(parts, "the settings")
	case plan.GPI:
		parts = append(parts, "the GPI settings")
	case plan.Saved:
		parts = append(parts, "the saved settings")
	}
	s := label + " gets " + strings.Join(parts, " and ")
	switch {
	case plan.Saved:
		s += ": its live state changes"
	case len(plan.Presets) > 0:
		s += ", over any of the same name"
	}
	return s
}

func opUltNetwork(c *call) (any, bool, error) {
	iface, pre, err := c.ult.ReadNetworkPrelude(c.ctx, c.ultHost, int(c.args.Value("interface").(uint64)))
	if pre == "" {
		return nil, false, c.keyerErr(err)
	}
	c.ultNote(pre)
	if err != nil {
		return nil, false, ultErr(err)
	}
	return netView(iface), false, nil
}

func opUltNetworkSet(c *call) (any, bool, error) {
	index := int(c.args.Value("interface").(uint64))
	str := func(name string) *string {
		if !c.args.Has(name) {
			return nil
		}
		s := c.args.Value(name).(string)
		return &s
	}
	set := ultimatte.NetworkSet{Address: str("address"), Netmask: str("netmask"), Gateway: str("gateway")}
	if c.args.Has("dns") {
		dns := []string{}
		for _, d := range c.args.Value("dns").([]string) {
			if d = strings.TrimSpace(d); d != "" {
				dns = append(dns, d)
			}
		}
		set.DNS = &dns
	}
	if c.args.Has("dhcp") {
		b := c.args.Value("dhcp").(bool)
		set.Dynamic = &b
	}
	if (set.Address == nil) != (set.Netmask == nil) {
		return nil, false, codeErr(CodeUsage, "--address and --netmask are one field on the wire: give both, or neither")
	}
	cur, pre, err := c.ult.ReadNetworkPrelude(c.ctx, c.ultHost, index)
	if pre == "" {
		return nil, false, c.keyerErr(err)
	}
	c.ultNote(pre)
	if err != nil {
		return nil, false, ultErr(err)
	}
	label, _ := ultimatte.Label(pre)
	if !c.req.Yes {
		return nil, false, c.refuse(networkImpact(orKeyer(label), cur, set), "set it?",
			"%s %s is destructive: add --yes to run it", c.req.Device, c.req.Path)
	}
	after, err := c.ult.SetNetwork(c.ctx, c.ultHost, index, set)
	if err != nil {
		return nil, false, ultErr(err)
	}
	c.note(Note{Values: networkValues(cur, after, set)})
	return CommandResult{Command: ultimatte.SectionName(index), Args: c.displayArgs()}, true, nil
}

// addr is an address and its mask as the wire's one field has them.
func addr(a, m string) string {
	if a == "" {
		return "none"
	}
	return a + "/" + m
}

// networkValues are what a set changed, configured before and after.
// The address and netmask are one field on the wire, two values here.
func networkValues(cur, after ultimatte.NetworkInterface, set ultimatte.NetworkSet) []Value {
	var vs []Value
	if set.Address != nil {
		vs = append(vs, Value{Param: "address", Before: ptr(orNone(cur.StaticAddress)), After: orNone(after.StaticAddress)},
			Value{Param: "netmask", Before: ptr(orNone(cur.StaticNetmask)), After: orNone(after.StaticNetmask)})
	}
	if set.Gateway != nil {
		vs = append(vs, Value{Param: "gateway", Before: ptr(cur.StaticGateway), After: after.StaticGateway})
	}
	if set.DNS != nil {
		vs = append(vs, Value{Param: "dns", Before: ptr(dnsText(cur.StaticDNS)), After: dnsText(after.StaticDNS)})
	}
	if set.Dynamic != nil {
		vs = append(vs, Value{Param: "dhcp", Before: ptr(dhcpText(cur.Dynamic)), After: dhcpText(after.Dynamic)})
	}
	return vs
}

func dnsText(d []string) string {
	if len(d) == 0 {
		return "none"
	}
	return strings.Join(d, ", ")
}

func dhcpText(b *bool) string {
	switch {
	case b == nil:
		return "unknown"
	case *b:
		return "on"
	}
	return "off"
}

// networkImpact is what a network set changes, for the confirmation: a
// line a field, before → after, then, for an address or DHCP, that a
// wrong one puts the unit out of reach until someone visits it.
func networkImpact(label string, cur ultimatte.NetworkInterface, set ultimatte.NetworkSet) string {
	var lines []string
	change := func(field, before, after string) {
		lines = append(lines, field+" "+before+" → "+after)
	}
	if set.Address != nil {
		change("address", addr(cur.StaticAddress, cur.StaticNetmask), *set.Address+"/"+*set.Netmask)
	}
	if set.Gateway != nil {
		change("gateway", orNone(cur.StaticGateway), orNone(*set.Gateway))
	}
	if set.DNS != nil {
		change("dns", dnsText(cur.StaticDNS), dnsText(*set.DNS))
	}
	if set.Dynamic != nil {
		change("dhcp", dhcpText(cur.Dynamic), dhcpText(set.Dynamic))
	}
	if set.Address != nil || set.Dynamic != nil {
		lines = append(lines, "a wrong address puts "+label+" out of reach until someone visits it")
	}
	return strings.Join(lines, "\n")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// UltConnect is connect's answer: the keyer answered, by its label, with
// so many presets. There is no session to keep.
type UltConnect struct {
	Host    string `json:"host"`
	Label   string `json:"label"`
	Presets int    `json:"presets"`
}

func opUltConnect(c *call) (any, bool, error) {
	pre, err := c.prelude()
	if err != nil {
		return nil, false, err
	}
	label, _ := ultimatte.Label(pre)
	return UltConnect{Host: c.ultKey, Label: label, Presets: len(ultimatte.ParseFileList(pre))}, false, nil
}
