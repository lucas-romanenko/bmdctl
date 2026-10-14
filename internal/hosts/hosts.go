// SPDX-License-Identifier: MIT

// Package hosts reads and writes bmdctl's hosts file, hosts.yml: the
// devices a user has named, each with its address and device type, and
// which one is the default.
//
//	version: 3
//	default: Studio
//	hosts:
//	  - name: Studio
//	    alias: st
//	    address: 192.168.81.85
//	    device: atem
//	  - name: ATEM 1 M/E Constellation HD
//	    as: backup
//	    address: 192.168.81.86
//	    device: atem
//
// A switcher's name is the one it gives itself: the name set in ATEM
// Setup, else its model. "as" is a name given with --as, which the host
// goes by instead (two switchers can give themselves one name). "alias"
// is a short name given with 'hosts alias', which a host also goes by.
// "was" is the name a host went by before its switcher renamed it, which
// --host still takes. A file without "version" was written before
// switchers named their hosts; it reads as it is, its names standing
// until each switcher's own replaces them. A file is written as version
// 2 unless a host has an alias, which bmdctl before version 3 can't read.
//
// The file is YAML, but only this shape of it: top-level "version",
// "default" and "hosts", the hosts a block list of
// name/as/alias/was/address/device mappings, values plain or quoted scalars,
// and comments. Anything else is an error with its line number, so a
// hand edit that bmdctl would misread is caught. The standard library
// has no YAML reader, and the CLI uses only the standard library.
package hosts

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Version is the newest file format, which Save writes when a host has
// an alias (version 3); without one it writes version 2, which bmdctl
// before aliases reads too.
const Version = 3

// Devices are the device types a host can be.
var Devices = []string{"atem", "hyperdeck", "videohub", "ultimatte"}

// Host is one named device.
type Host struct {
	// Name is the switcher's own name as last seen, or for a device
	// bmdctl can't ask, the name it was given.
	Name string `json:"name"`
	// As is a name given with --as, which the host goes by instead.
	As string `json:"as,omitempty"`
	// Alias is a short name given with 'hosts alias', which the host
	// goes by as well.
	Alias string `json:"alias,omitempty"`
	// Was is the name the host went by before its switcher renamed it;
	// --host still takes it.
	Was     string `json:"was,omitempty"`
	Address string `json:"address"` // host or host:port
	Device  string `json:"device"`
}

// Label is the name the host goes by: As if given, else Name.
func (h Host) Label() string {
	if h.As != "" {
		return h.As
	}
	return h.Name
}

// File is the hosts file's contents.
type File struct {
	Default string `json:"default,omitempty"` // a host's Label
	Hosts   []Host `json:"hosts"`
}

// Load reads the hosts file at path. A missing file is an empty one.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Hosts: []Host{}}, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Find returns the host going by this name or alias, ignoring case, or
// else the one that went by it before (Was), or nil.
func (f *File) Find(name string) *Host {
	if h := f.Named(name); h != nil {
		return h
	}
	for i := range f.Hosts {
		if f.Hosts[i].Was != "" && strings.EqualFold(f.Hosts[i].Was, name) {
			return &f.Hosts[i]
		}
	}
	return nil
}

// FindAddress returns the host at addr, or nil. Hosts compare without
// regard to case; ports only when both addresses give one.
func (f *File) FindAddress(addr string) *Host {
	for i := range f.Hosts {
		if SameAddress(f.Hosts[i].Address, addr) {
			return &f.Hosts[i]
		}
	}
	return nil
}

// SameAddress reports whether two addresses name one device: the same
// host, ignoring case, and the same port where both give one.
func SameAddress(a, b string) bool {
	ha, pa := splitAddress(a)
	hb, pb := splitAddress(b)
	return strings.EqualFold(ha, hb) && (pa == "" || pb == "" || pa == pb)
}

func splitAddress(addr string) (host, port string) {
	if h, p, err := net.SplitHostPort(addr); err == nil {
		return h, p
	}
	return strings.Trim(addr, "[]"), ""
}

// HostPart is an address's host: "192.168.81.85" for
// "192.168.81.85:9910".
func HostPart(addr string) string {
	h, _ := splitAddress(addr)
	return h
}

// Resolve finds what spec names: a host by its name or alias in any
// case, by the name it went by before, by its address, or by any
// unambiguous prefix of its name or alias; else an address no host has.
// It returns the host (nil for such an address) and the address.
func (f *File) Resolve(spec string) (*Host, string, error) {
	h, err := f.Match(spec)
	switch {
	case err != nil:
		return nil, "", err
	case h != nil:
		return h, h.Address, nil
	case net.ParseIP(HostPart(spec)) != nil && CheckAddress(spec) == nil:
		return nil, spec, nil
	case CheckAddress(spec) != nil:
		return nil, "", fmt.Errorf("%q is neither a host in hosts.yml nor an address", spec)
	}
	return nil, spec, nil
}

// Match is Resolve without the addresses no host has: the host spec names
// by its name or alias, the name it went by before, its address, or an
// unambiguous prefix of its name or alias; nil when it names none, and an
// error when it could be several.
func (f *File) Match(spec string) (*Host, error) {
	if h := f.Find(spec); h != nil {
		return h, nil
	}
	if net.ParseIP(HostPart(spec)) != nil && CheckAddress(spec) == nil {
		return f.FindAddress(spec), nil
	}
	var hits []*Host
	for i := range f.Hosts {
		h := &f.Hosts[i]
		if spec != "" && (hasPrefixFold(h.Label(), spec) || h.Alias != "" && hasPrefixFold(h.Alias, spec)) {
			hits = append(hits, h)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
	default:
		var names []string
		for _, h := range hits {
			names = append(names, strconv.Quote(h.Label()))
		}
		return nil, fmt.Errorf("%q could be %s: give more of the name", spec, orList(names))
	}
	if CheckAddress(spec) == nil {
		return f.FindAddress(spec), nil
	}
	return nil, nil
}

func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}

// orList is "a", "a or b", "a, b or c".
func orList(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}

// Add adds h, refusing anything Check refuses, an address already a host
// and a name another host goes by.
func (f *File) Add(h Host) error {
	if err := h.Check(); err != nil {
		return err
	}
	if o := f.FindAddress(h.Address); o != nil {
		return fmt.Errorf("%s is already a host, %s", h.Address, o.Label())
	}
	if o := f.Named(h.Label()); o != nil {
		return fmt.Errorf("another host goes by %q (%s): give this one its own name with --as NAME", o.goesBy(h.Label()), HostPart(o.Address))
	}
	f.Hosts = append(f.Hosts, h)
	return nil
}

// Named is the host going by name, as its name or its alias, ignoring
// case, or nil: Find without the names hosts went by before.
func (f *File) Named(name string) *Host {
	for i := range f.Hosts {
		if strings.EqualFold(f.Hosts[i].Label(), name) {
			return &f.Hosts[i]
		}
	}
	for i := range f.Hosts {
		if f.Hosts[i].Alias != "" && strings.EqualFold(f.Hosts[i].Alias, name) {
			return &f.Hosts[i]
		}
	}
	return nil
}

// goesBy is the name of h's that name matched, as h spells it: its alias,
// else its Label.
func (h Host) goesBy(name string) string {
	if h.Alias != "" && strings.EqualFold(h.Alias, name) {
		return h.Alias
	}
	return h.Label()
}

// SetAlias gives h the short name alias, which it goes by as well as its
// name; "" takes its alias away. An alias CheckAlias refuses, h's own
// name, and a name or alias another host goes by are refused.
func (f *File) SetAlias(h *Host, alias string) error {
	if alias == "" {
		h.Alias = ""
		return nil
	}
	if err := CheckAlias(alias); err != nil {
		return err
	}
	if strings.EqualFold(alias, h.Label()) {
		return fmt.Errorf("%s goes by %q already", h.Label(), h.Label())
	}
	if o := f.Named(alias); o != nil && o != h {
		if strings.EqualFold(o.Alias, alias) {
			return fmt.Errorf("%q is %s's alias already", o.Alias, o.Label())
		}
		return fmt.Errorf("another host goes by %q (%s)", o.Label(), HostPart(o.Address))
	}
	h.Alias = alias
	return nil
}

// Rename records that h's switcher now calls itself name. The name it
// went by is kept as Was, and the default follows it. A name another host
// goes by is refused, h unchanged.
func (f *File) Rename(h *Host, name string) error {
	if name == h.Name {
		return nil
	}
	if err := CheckName(name); err != nil {
		return err
	}
	if h.As == "" {
		if o := f.Named(name); o != nil && o != h {
			return fmt.Errorf("another host goes by %q (%s)", o.goesBy(name), HostPart(o.Address))
		}
	}
	old := h.Label()
	if h.Name != "" {
		h.Was = h.Name
	}
	h.Name = name
	if strings.EqualFold(f.Default, old) {
		f.Default = h.Label()
	}
	return nil
}

// Remove removes the named host, and the default if it was that host.
func (f *File) Remove(name string) error {
	h := f.Find(name)
	if h == nil {
		return fmt.Errorf("no host named %s", name)
	}
	if strings.EqualFold(f.Default, h.Label()) {
		f.Default = ""
	}
	label := h.Label()
	f.Hosts = slices.DeleteFunc(f.Hosts, func(x Host) bool { return x.Label() == label })
	return nil
}

// Check reports what is wrong with the file: bad hosts, two hosts going
// by one name (or alias), a default that isn't a host.
func (f *File) Check() []error {
	var errs []error
	seen := map[string]bool{}
	for _, h := range f.Hosts {
		if err := h.Check(); err != nil {
			errs = append(errs, err)
		}
		names := []string{h.Label()}
		if h.Alias != "" && !strings.EqualFold(h.Alias, h.Label()) {
			names = append(names, h.Alias)
		}
		for _, n := range names {
			k := strings.ToLower(n)
			if seen[k] {
				errs = append(errs, fmt.Errorf("%s is named twice", n))
			}
			seen[k] = true
		}
	}
	if f.Default != "" && f.Find(f.Default) == nil {
		errs = append(errs, fmt.Errorf("the default, %s, is not a host", f.Default))
	}
	return errs
}

// Check reports what is wrong with a host.
func (h Host) Check() error {
	if err := CheckName(h.Name); err != nil && h.As == "" {
		return err
	}
	if h.As != "" {
		if err := CheckName(h.As); err != nil {
			return err
		}
	}
	if h.Alias != "" {
		if err := CheckAlias(h.Alias); err != nil {
			return fmt.Errorf("%s: %w", h.Label(), err)
		}
	}
	if err := CheckAddress(h.Address); err != nil {
		return fmt.Errorf("%s: %w", h.Label(), err)
	}
	if !slices.Contains(Devices, h.Device) {
		return fmt.Errorf("%s: device %q is not one of %s", h.Label(), h.Device, strings.Join(Devices, ", "))
	}
	return nil
}

// CheckName refuses a name a command line or the hosts file couldn't
// carry: empty, with spaces around it, with control characters, or longer
// than 100 characters. Switchers name themselves, so anything else goes.
func CheckName(name string) error {
	switch {
	case name == "":
		return errors.New("a host needs a name")
	case strings.TrimSpace(name) != name:
		return fmt.Errorf("host name %q: no spaces around it", name)
	case len([]rune(name)) > 100:
		return fmt.Errorf("host name %q: longer than 100 characters", name)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return fmt.Errorf("host name %q: no control characters", name)
	}
	return nil
}

// CheckAlias refuses an alias a command line couldn't carry as one word:
// empty, with a space or a control character in it, starting with a
// dash, longer than 100 characters, or an IP address, which names a
// device itself.
func CheckAlias(alias string) error {
	switch {
	case alias == "":
		return errors.New("an alias can't be empty")
	case strings.IndexFunc(alias, unicode.IsSpace) >= 0:
		return fmt.Errorf("alias %q: one word, no spaces", alias)
	case strings.IndexFunc(alias, unicode.IsControl) >= 0:
		return fmt.Errorf("alias %q: no control characters", alias)
	case strings.HasPrefix(alias, "-"):
		return fmt.Errorf("alias %q: it can't start with a dash", alias)
	case len([]rune(alias)) > 100:
		return fmt.Errorf("alias %q: longer than 100 characters", alias)
	case net.ParseIP(HostPart(alias)) != nil:
		return fmt.Errorf("alias %q: that is an address", alias)
	}
	return nil
}

// CheckAddress accepts "host" or "host:port", host being an IP address
// or a DNS name.
func CheckAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, ""
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("address %q: bad port", addr)
		}
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if host == "" || len(host) > 253 {
		return fmt.Errorf("address %q: no host", addr)
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("address %q: not an IP address or host name", addr)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return fmt.Errorf("address %q: not an IP address or host name", addr)
			}
		}
	}
	return nil
}

// Save writes the file to path atomically, creating its directory.
func (f *File) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hosts-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(f.Marshal()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Marshal renders the file.
func (f *File) Marshal() []byte {
	var b strings.Builder
	b.WriteString("# bmdctl's hosts: 'bmdctl hosts add|rm|alias|default|discover' edit this file.\n")
	b.WriteString("# A switcher's name is the one it gives itself, kept up to date; \"as\" is\n")
	b.WriteString("# one given with --as instead, \"alias\" a short one it goes by as well,\n")
	b.WriteString("# \"was\" its name before, which --host takes.\n")
	version := 2
	if slices.ContainsFunc(f.Hosts, func(h Host) bool { return h.Alias != "" }) {
		version = Version
	}
	fmt.Fprintf(&b, "version: %d\n", version)
	if f.Default != "" {
		fmt.Fprintf(&b, "default: %s\n", scalar(f.Default))
	}
	if len(f.Hosts) == 0 {
		b.WriteString("hosts: []\n")
		return []byte(b.String())
	}
	b.WriteString("hosts:\n")
	for _, h := range f.Hosts {
		fmt.Fprintf(&b, "  - name: %s\n", scalar(h.Name))
		if h.As != "" {
			fmt.Fprintf(&b, "    as: %s\n", scalar(h.As))
		}
		if h.Alias != "" {
			fmt.Fprintf(&b, "    alias: %s\n", scalar(h.Alias))
		}
		if h.Was != "" {
			fmt.Fprintf(&b, "    was: %s\n", scalar(h.Was))
		}
		fmt.Fprintf(&b, "    address: %s\n    device: %s\n", scalar(h.Address), scalar(h.Device))
	}
	return []byte(b.String())
}

// scalar quotes s if it wouldn't read back as the same plain scalar.
func scalar(s string) string {
	if s != "" && !strings.ContainsAny(s, "#'\"[]{},&*!|>%@`\\") && strings.TrimSpace(s) == s &&
		!strings.Contains(s, ": ") && !strings.HasSuffix(s, ":") && !strings.ContainsAny(s[:1], "-?:") {
		return s
	}
	return strconv.Quote(s)
}

// Parse reads the hosts file's YAML subset.
func Parse(b []byte) (*File, error) {
	f := &File{Hosts: []Host{}}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	var cur *Host
	seen := map[string]bool{}
	itemIndent, keyIndent := -1, -1
	inHosts := false
	finish := func() error {
		if cur == nil {
			return nil
		}
		if cur.Name == "" && cur.As == "" || cur.Address == "" {
			return fmt.Errorf("a host needs a name and an address")
		}
		if cur.Device == "" {
			cur.Device = "atem"
		}
		f.Hosts = append(f.Hosts, *cur)
		cur = nil
		return nil
	}
	for i, line := range lines {
		n := i + 1
		fail := func(format string, args ...any) (*File, error) {
			return nil, fmt.Errorf("line %d: %s", n, fmt.Sprintf(format, args...))
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if strings.Contains(lead, "\t") && strings.TrimSpace(stripComment(line)) != "" {
			return fail("indent with spaces, not tabs")
		}
		content := strings.TrimRight(stripComment(line), " \t")
		if strings.TrimSpace(content) == "" || content == "---" {
			continue
		}
		indent := len(content) - len(strings.TrimLeft(content, " "))
		body := content[indent:]
		item := strings.HasPrefix(body, "- ") || body == "-"
		if indent == 0 && !(inHosts && item) {
			if err := finish(); err != nil {
				return fail("%v", err)
			}
			key, value, ok := cutKey(body)
			if !ok {
				return fail("expected \"version:\", \"default:\" or \"hosts:\"")
			}
			if seen[key] {
				return fail("%s given twice", key)
			}
			seen[key] = true
			switch key {
			case "version":
				v, err := unquote(value)
				if err != nil {
					return fail("%v", err)
				}
				if n, err := strconv.Atoi(v); err != nil || n < 1 || n > Version {
					return fail("version %s: this bmdctl reads versions 1 to %d (a newer one wrote it?)", v, Version)
				}
				inHosts = false
			case "default":
				v, err := unquote(value)
				if err != nil {
					return fail("%v", err)
				}
				f.Default = v
				inHosts = false
			case "hosts":
				switch value {
				case "":
					inHosts = true
				case "[]":
					inHosts = false
				default:
					return fail("hosts is a list: one \"- name: ...\" item per host")
				}
			default:
				return fail("unknown key %q (version, default, hosts)", key)
			}
			continue
		}
		if !inHosts {
			return fail("unexpected indented line")
		}
		if item {
			if err := finish(); err != nil {
				return fail("%v", err)
			}
			if itemIndent >= 0 && indent != itemIndent {
				return fail("list items must line up")
			}
			itemIndent = indent
			cur = &Host{}
			rest := strings.TrimLeft(strings.TrimPrefix(body, "-"), " ")
			if rest == "" {
				continue
			}
			k := indent + (len(body) - len(rest))
			if keyIndent >= 0 && k != keyIndent {
				return fail("keys must line up")
			}
			keyIndent = k
			body = rest
		} else {
			if cur == nil {
				return fail("expected \"- name: ...\"")
			}
			if keyIndent < 0 {
				keyIndent = indent
			}
			if indent != keyIndent {
				return fail("keys must line up")
			}
		}
		key, value, ok := cutKey(body)
		if !ok {
			return fail("expected \"key: value\"")
		}
		v, err := unquote(value)
		if err != nil {
			return fail("%v", err)
		}
		var dst *string
		switch key {
		case "name":
			dst = &cur.Name
		case "as":
			dst = &cur.As
		case "alias":
			dst = &cur.Alias
		case "was":
			dst = &cur.Was
		case "address":
			dst = &cur.Address
		case "device":
			dst = &cur.Device
		default:
			return fail("unknown key %q (name, as, alias, was, address, device)", key)
		}
		if *dst != "" {
			return fail("%s given twice", key)
		}
		*dst = v
	}
	if err := finish(); err != nil {
		return nil, fmt.Errorf("at the end: %v", err)
	}
	return f, nil
}

// stripComment drops a comment: '#' at the start or after a space,
// outside quotes.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}

func cutKey(s string) (key, value string, ok bool) {
	k, v, ok := strings.Cut(s, ":")
	if !ok || k == "" || strings.ContainsAny(k, " \"'") || (v != "" && v[0] != ' ') {
		return "", "", false
	}
	return k, strings.TrimSpace(v), true
}

func unquote(v string) (string, error) {
	switch {
	case strings.HasPrefix(v, `"`):
		s, err := strconv.Unquote(v)
		if err != nil {
			return "", fmt.Errorf("bad quoted value %s", v)
		}
		return s, nil
	case strings.HasPrefix(v, "'"):
		if len(v) < 2 || !strings.HasSuffix(v, "'") {
			return "", fmt.Errorf("bad quoted value %s", v)
		}
		inner := v[1 : len(v)-1]
		if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			return "", fmt.Errorf("bad quoted value %s", v)
		}
		return strings.ReplaceAll(inner, "''", "'"), nil
	case strings.ContainsAny(v, "[]{}&*!|>%@`"):
		return "", fmt.Errorf("value %s: quote it", v)
	}
	return v, nil
}
