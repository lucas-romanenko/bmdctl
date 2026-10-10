// SPDX-License-Identifier: MIT

// Package hosts reads and writes bmdctl's hosts file, hosts.yml: the
// devices a user has named, each with its address and device type, and
// which one is the default.
//
//	default: studio
//	hosts:
//	  - name: studio
//	    address: 192.168.81.85
//	    device: atem
//
// The file is YAML, but only this shape of it: top-level "default" and
// "hosts", the hosts a block list of name/address/device mappings, values
// plain or quoted scalars, and comments. Anything else is an error with
// its line number, so a hand edit that bmdctl would misread is caught.
// The standard library has no YAML reader, and the CLI uses only the
// standard library.
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
)

// Devices are the device types a host can be.
var Devices = []string{"atem", "hyperdeck", "videohub", "ultimatte"}

// Host is one named device.
type Host struct {
	Name    string `json:"name"`
	Address string `json:"address"` // host or host:port
	Device  string `json:"device"`
}

// File is the hosts file's contents.
type File struct {
	Default string `json:"default,omitempty"`
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

// Find returns the host with this name, ignoring case, or nil.
func (f *File) Find(name string) *Host {
	for i := range f.Hosts {
		if strings.EqualFold(f.Hosts[i].Name, name) {
			return &f.Hosts[i]
		}
	}
	return nil
}

// Add adds h, refusing a name already there and anything Check refuses.
func (f *File) Add(h Host) error {
	if err := h.Check(); err != nil {
		return err
	}
	if f.Find(h.Name) != nil {
		return fmt.Errorf("%s is already a host (bmdctl hosts rm %s first)", h.Name, h.Name)
	}
	f.Hosts = append(f.Hosts, h)
	return nil
}

// Remove removes the named host, and the default if it was that host.
func (f *File) Remove(name string) error {
	h := f.Find(name)
	if h == nil {
		return fmt.Errorf("no host named %s", name)
	}
	if strings.EqualFold(f.Default, h.Name) {
		f.Default = ""
	}
	f.Hosts = slices.DeleteFunc(f.Hosts, func(x Host) bool { return x.Name == h.Name })
	return nil
}

// Check reports what is wrong with the file: bad hosts, two hosts with
// one name, a default that isn't a host.
func (f *File) Check() []error {
	var errs []error
	seen := map[string]bool{}
	for _, h := range f.Hosts {
		if err := h.Check(); err != nil {
			errs = append(errs, err)
		}
		k := strings.ToLower(h.Name)
		if seen[k] {
			errs = append(errs, fmt.Errorf("%s is named twice", h.Name))
		}
		seen[k] = true
	}
	if f.Default != "" && f.Find(f.Default) == nil {
		errs = append(errs, fmt.Errorf("the default, %s, is not a host", f.Default))
	}
	return errs
}

// Check reports what is wrong with a host.
func (h Host) Check() error {
	if err := CheckName(h.Name); err != nil {
		return err
	}
	if err := CheckAddress(h.Address); err != nil {
		return fmt.Errorf("%s: %w", h.Name, err)
	}
	if !slices.Contains(Devices, h.Device) {
		return fmt.Errorf("%s: device %q is not one of %s", h.Name, h.Device, strings.Join(Devices, ", "))
	}
	return nil
}

// CheckName refuses a name that could be read as an address: a name is
// a letter followed by letters, digits, '-' and '_'.
func CheckName(name string) error {
	if name == "" {
		return errors.New("a host needs a name")
	}
	for i, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r >= '0' && r <= '9' || r == '-' || r == '_')
		if !ok {
			return fmt.Errorf("host name %q: use a letter, then letters, digits, - and _", name)
		}
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
	b.WriteString("# bmdctl's hosts: 'bmdctl hosts add|rm|default' edit this file.\n")
	if f.Default != "" {
		fmt.Fprintf(&b, "default: %s\n", scalar(f.Default))
	}
	if len(f.Hosts) == 0 {
		b.WriteString("hosts: []\n")
		return []byte(b.String())
	}
	b.WriteString("hosts:\n")
	for _, h := range f.Hosts {
		fmt.Fprintf(&b, "  - name: %s\n    address: %s\n    device: %s\n", scalar(h.Name), scalar(h.Address), scalar(h.Device))
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
		if cur.Name == "" || cur.Address == "" {
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
				return fail("expected \"default:\" or \"hosts:\"")
			}
			if seen[key] {
				return fail("%s given twice", key)
			}
			seen[key] = true
			switch key {
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
				return fail("unknown key %q (default, hosts)", key)
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
		case "address":
			dst = &cur.Address
		case "device":
			dst = &cur.Device
		default:
			return fail("unknown key %q (name, address, device)", key)
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
