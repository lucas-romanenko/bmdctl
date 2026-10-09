// SPDX-License-Identifier: MIT

// Package table is the command table: one entry per command a device
// package offers, with its parameters, how to build it, and what to warn
// about. Device packages fill a Registry; the CLI builds its subcommands
// from it and docs/COMMANDS.md is generated from it, so the three can't
// drift apart.
package table

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Command is what an entry builds. It only needs to encode itself; the
// device package's own command type carries the rest.
type Command interface {
	Encode() []byte
}

// Kind is how a parameter's value is written on the command line.
type Kind int

const (
	Uint   Kind = iota // a non-negative integer
	Int                // an integer
	Float              // a decimal number (NaN and Inf parse, and are the command's to refuse)
	Bool               // true/false, on/off, yes/no, 1/0
	String             // any text
	Enum               // one of Names, or the number itself
	Flags              // comma-separated Names, or the number itself; values are OR-ed
	Rate               // "seconds:frames" or a frame count, converted at the display frame rate
	Time               // RFC 3339, Unix seconds, or "now"
	List               // comma-separated values; the entry's Build interprets them
)

func (k Kind) String() string {
	return [...]string{"uint", "int", "float", "bool", "string", "enum", "flags", "rate", "time", "list"}[k]
}

// Param is one parameter of an entry.
type Param struct {
	Name     string            // the flag, without dashes: "me", "width"
	Kind     Kind              // how its value is written
	Min, Max *float64          // the accepted range for Uint and Int, if narrower than the field
	Unit     string            // what the number means: "frames", "%", "dB", "0-1"
	Help     string            // one line
	Required bool              // must be given
	Default  string            // used when not given (implies not required)
	Names    map[string]uint64 // Enum and Flags values by name
}

// Entry is one command.
type Entry struct {
	Device  string // "atem"
	Path    string // space-separated words after the device: "transition wipe"
	Summary string // one line
	Params  []Param
	// Build makes the command from parsed arguments.
	Build func(Args) (Command, error)
	// Echo documents the state the device reports back, with parameter
	// names in angle brackets: "PrgI:<me>". Empty when there is none.
	Echo        string
	Unverified  bool // the wire format is pinned but unconfirmed on hardware
	Destructive bool // hard to undo, or drops connections
	// AtLeastOne: the command does nothing unless one of its optional
	// parameters is given (a masked command).
	AtLeastOne bool
}

// Words returns the entry's path as words.
func (e Entry) Words() []string { return strings.Fields(e.Path) }

// Registry holds entries.
type Registry struct {
	entries []Entry
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

// Add adds entries, refusing duplicates and malformed parameters.
func (r *Registry) Add(entries ...Entry) error {
	for _, e := range entries {
		if e.Device == "" || e.Path == "" || e.Build == nil {
			return fmt.Errorf("table: entry %q %q is incomplete", e.Device, e.Path)
		}
		for _, x := range r.entries {
			if x.Device == e.Device && x.Path == e.Path {
				return fmt.Errorf("table: %s %s added twice", e.Device, e.Path)
			}
			if x.Device == e.Device && (strings.HasPrefix(x.Path, e.Path+" ") || strings.HasPrefix(e.Path, x.Path+" ")) {
				return fmt.Errorf("table: %s %q and %q: a command can't also be a group", e.Device, x.Path, e.Path)
			}
		}
		seen := map[string]bool{}
		for _, p := range e.Params {
			if seen[p.Name] {
				return fmt.Errorf("table: %s %s: parameter %s twice", e.Device, e.Path, p.Name)
			}
			seen[p.Name] = true
			if (p.Kind == Enum || p.Kind == Flags) && len(p.Names) == 0 {
				return fmt.Errorf("table: %s %s: %s has no names", e.Device, e.Path, p.Name)
			}
		}
		r.entries = append(r.entries, e)
	}
	return nil
}

// Devices returns the devices with entries, sorted.
func (r *Registry) Devices() []string {
	var ds []string
	for _, e := range r.entries {
		if !slices.Contains(ds, e.Device) {
			ds = append(ds, e.Device)
		}
	}
	slices.Sort(ds)
	return ds
}

// Entries returns a device's entries, sorted by path.
func (r *Registry) Entries(device string) []Entry {
	var es []Entry
	for _, e := range r.entries {
		if e.Device == device {
			es = append(es, e)
		}
	}
	slices.SortFunc(es, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return es
}

// Resolve finds the entry the leading words name. It returns the entry
// and the words left over; if the words only name a group (or nothing),
// the entry is nil and prefix is the longest matching group.
func (r *Registry) Resolve(device string, words []string) (e *Entry, prefix []string, rest []string) {
	es := r.Entries(device)
	for n := len(words); n > 0; n-- {
		path := strings.Join(words[:n], " ")
		for i := range es {
			if es[i].Path == path {
				return &es[i], words[:n], words[n:]
			}
		}
	}
	for n := len(words); n > 0; n-- {
		path := strings.Join(words[:n], " ") + " "
		for _, x := range es {
			if strings.HasPrefix(x.Path, path) {
				return nil, words[:n], words[n:]
			}
		}
	}
	return nil, nil, words
}

// Children lists what comes after prefix: subgroups (with a trailing
// "...") and commands, each with a summary.
func (r *Registry) Children(device string, prefix []string) [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	p := strings.Join(prefix, " ")
	for _, e := range r.Entries(device) {
		w := e.Words()
		if len(w) <= len(prefix) || (p != "" && !strings.HasPrefix(e.Path, p+" ")) {
			continue
		}
		next := w[len(prefix)]
		if len(w) == len(prefix)+1 {
			out = append(out, [2]string{next, e.Summary})
		} else if !seen[next] {
			seen[next] = true
			out = append(out, [2]string{next + " ...", ""})
		}
	}
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	return out
}

// Args holds an entry's parsed arguments.
type Args struct {
	values map[string]any
	// FPS is the display frame rate Rate parameters were converted at.
	FPS int
}

// Has reports whether name was given (or has a default).
func (a Args) Has(name string) bool { _, ok := a.values[name]; return ok }

// Value returns name's parsed value: uint64 (Uint, Enum, Flags), int64
// (Int, Rate), float64, bool, string, time.Time or []string (List).
func (a Args) Value(name string) any { return a.values[name] }

// ErrUsage marks errors in what the user typed.
var ErrUsage = errors.New("usage")

func usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

// Parse turns raw "--name value" pairs into Args for e, applying
// defaults and checking that required parameters are present. parseRate
// converts a Rate value to frames.
func (e Entry) Parse(raw map[string]string, parseRate func(string) int) (Args, error) {
	a := Args{values: map[string]any{}}
	for name := range raw {
		if !slices.ContainsFunc(e.Params, func(p Param) bool { return p.Name == name }) {
			return a, usagef("%s %s has no --%s", e.Device, e.Path, name)
		}
	}
	given := 0
	for _, p := range e.Params {
		s, ok := raw[p.Name]
		if !ok {
			if p.Required {
				return a, usagef("--%s is required", p.Name)
			}
			if p.Default == "" {
				continue
			}
			s = p.Default
		} else if !p.Required && p.Default == "" {
			given++
		}
		v, err := p.parse(s, parseRate)
		if err != nil {
			return a, usagef("--%s %q: %v", p.Name, s, err)
		}
		a.values[p.Name] = v
	}
	if e.AtLeastOne && given == 0 {
		return a, usagef("nothing to set: give at least one of %s", strings.Join(e.optional(), ", "))
	}
	return a, nil
}

func (e Entry) optional() []string {
	var out []string
	for _, p := range e.Params {
		if !p.Required && p.Default == "" {
			out = append(out, "--"+p.Name)
		}
	}
	return out
}

func (p Param) parse(s string, parseRate func(string) int) (any, error) {
	switch p.Kind {
	case Uint:
		n, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			return nil, errors.New("not a non-negative integer")
		}
		return n, p.inRange(float64(n))
	case Int:
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return nil, errors.New("not an integer")
		}
		return n, p.inRange(float64(n))
	case Float:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, errors.New("not a number")
		}
		return f, nil
	case Bool:
		switch strings.ToLower(s) {
		case "true", "on", "yes", "1":
			return true, nil
		case "false", "off", "no", "0":
			return false, nil
		}
		return nil, errors.New("not true or false")
	case String:
		return s, nil
	case Enum:
		if v, ok := p.Names[s]; ok {
			return v, nil
		}
		n, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("not one of %s", strings.Join(p.names(), ", "))
		}
		return n, p.inRange(float64(n))
	case Flags:
		if n, err := strconv.ParseUint(s, 0, 64); err == nil {
			return n, p.inRange(float64(n))
		}
		var v uint64
		for _, w := range strings.Split(s, ",") {
			b, ok := p.Names[strings.TrimSpace(w)]
			if !ok {
				return nil, fmt.Errorf("%q is not one of %s", w, strings.Join(p.names(), ", "))
			}
			v |= b
		}
		return v, nil
	case Rate:
		n := parseRate(s)
		return int64(n), p.inRange(float64(n))
	case Time:
		if s == "now" {
			return time.Now(), nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0), nil
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, errors.New("not RFC 3339, Unix seconds or now")
		}
		return t, nil
	case List:
		if s == "" {
			return []string{}, nil
		}
		return strings.Split(s, ","), nil
	}
	return nil, fmt.Errorf("unknown kind %d", p.Kind)
}

func (p Param) inRange(f float64) error {
	if p.Min != nil && f < *p.Min || p.Max != nil && f > *p.Max {
		return fmt.Errorf("out of range %s", p.Range())
	}
	return nil
}

// Range describes the accepted range, or "".
func (p Param) Range() string {
	switch {
	case p.Min != nil && p.Max != nil && *p.Min < 0:
		return trim(*p.Min) + " to " + trim(*p.Max)
	case p.Min != nil && p.Max != nil:
		return trim(*p.Min) + "-" + trim(*p.Max)
	case p.Min != nil:
		return ">= " + trim(*p.Min)
	case p.Max != nil:
		return "<= " + trim(*p.Max)
	}
	return ""
}

func (p Param) names() []string {
	var ns []string
	for n := range p.Names {
		ns = append(ns, n)
	}
	slices.SortFunc(ns, func(a, b string) int {
		if c := compareUint(p.Names[a], p.Names[b]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return ns
}

func compareUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func trim(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Bounds returns pointers to lo and hi, for Param.Min and Param.Max.
func Bounds(lo, hi float64) (*float64, *float64) { return &lo, &hi }
