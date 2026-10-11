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
	Uint    Kind = iota // a non-negative integer
	Int                 // an integer
	Float               // a decimal number (NaN and Inf parse, and are the command's to refuse)
	Bool                // true/false, on/off, yes/no, 1/0
	String              // any text
	Enum                // one of Names, or the number itself
	Flags               // comma-separated Names, or the number itself; values are OR-ed
	Rate                // "seconds:frames" or a frame count, converted at the display frame rate
	Time                // RFC 3339, Unix seconds, or "now"
	List                // comma-separated values; the entry's Build interprets them
	Source              // a source id, or an input's name, which the Resolver looks up
	File                // a file the CLI reads and sends with the request; "-" is stdin
	OutFile             // a file the CLI writes from the response; "-" is stdout
	Dir                 // a directory the CLI writes the response's files into
)

func (k Kind) String() string {
	return [...]string{"uint", "int", "float", "bool", "string", "enum", "flags", "rate", "time", "list",
		"source", "file", "file", "dir"}[k]
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
	// OneBased: an Index, typed counted from 1 where the field counts
	// from 0 (--me 1 is M/E index 0). Min and Max are the field's range.
	OneBased bool
}

// Entry is one command.
type Entry struct {
	Device  string // "atem"
	Path    string // space-separated words after the device: "transition wipe"
	Summary string // one line
	Params  []Param
	// Syntax is the command as typed, the path's words with the
	// positional arguments where they're said: "usk <keyer> fill
	// <source>", "media download <slot> [file]". Empty means the path
	// followed by each required parameter in order (syntax.go).
	Syntax string
	// Build makes the command from parsed arguments. Nil for an
	// operation.
	Build func(Args) (Command, error)
	// Operation: not one wire command but something the device's engine
	// runs by path: reading state, a transfer, a read-modify-write.
	Operation bool
	// Local: an operation that needs no device; the CLI runs it itself.
	Local bool
	// Stream: the operation answers with results until it is stopped.
	Stream bool
	// Echo documents the state the device reports back, with parameter
	// names in angle brackets: "PrgI:<me>". Empty when there is none.
	Echo        string
	Unverified  bool // the wire format is pinned but unconfirmed on hardware
	Destructive bool // hard to undo, or drops connections
	// AtLeastOne: the command does nothing unless one of its optional
	// parameters is given (a masked command).
	AtLeastOne bool
	// Examples are up to three real command lines, as typed after
	// "bmdctl" without the device word, the plainest first: --help opens
	// with them, and docs/COMMANDS.md and the man page show them.
	Examples []string
	// Group is the group the entry belongs to, one its device declares
	// (AddGroups): what one MCP tool gathers. Empty means the path's
	// first word.
	Group string
}

// Words returns the entry's path as words.
func (e Entry) Words() []string { return strings.Fields(e.Path) }

// GroupName is the entry's group: Group, else its first word.
func (e Entry) GroupName() string {
	if e.Group != "" {
		return e.Group
	}
	return e.Words()[0]
}

// Group is a set of a device's entries that belong together:
// "switching" is program, preview, cut, auto and the rest.
type Group struct {
	Name    string
	Summary string
}

// Registry holds entries.
type Registry struct {
	entries []Entry
	aliases map[string]string  // "device word": the first word it stands for
	groups  map[string][]Group // by device, in the order declared
}

// AddGroups declares a device's groups, in the order they are listed.
func (r *Registry) AddGroups(device string, gs ...Group) error {
	if r.groups == nil {
		r.groups = map[string][]Group{}
	}
	for _, g := range gs {
		if g.Name == "" || slices.ContainsFunc(r.groups[device], func(x Group) bool { return x.Name == g.Name }) {
			return fmt.Errorf("table: %s group %q is empty or added twice", device, g.Name)
		}
		r.groups[device] = append(r.groups[device], g)
	}
	return nil
}

// Groups returns a device's groups that hold entries: the declared ones
// in their order, then, sorted, any an entry names that its device
// didn't declare (a first word), with no summary.
func (r *Registry) Groups(device string) []Group {
	used := map[string]bool{}
	for _, e := range r.Entries(device) {
		used[e.GroupName()] = true
	}
	var out []Group
	for _, g := range r.groups[device] {
		if used[g.Name] {
			out = append(out, g)
			delete(used, g.Name)
		}
	}
	var rest []string
	for n := range used {
		rest = append(rest, n)
	}
	slices.Sort(rest)
	for _, n := range rest {
		out = append(out, Group{Name: n})
	}
	return out
}

// GroupEntries returns the entries in a device's group, sorted by path.
func (r *Registry) GroupEntries(device, group string) []Entry {
	var out []Entry
	for _, e := range r.Entries(device) {
		if e.GroupName() == group {
			out = append(out, e)
		}
	}
	return out
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

// Add adds entries, refusing duplicates and malformed parameters.
func (r *Registry) Add(entries ...Entry) error {
	for _, e := range entries {
		if e.Device == "" || e.Path == "" || (e.Build == nil) != e.Operation {
			return fmt.Errorf("table: entry %q %q is incomplete", e.Device, e.Path)
		}
		if e.Local && !e.Operation {
			return fmt.Errorf("table: %s %s: only an operation can be local", e.Device, e.Path)
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
			if p.OneBased && p.Kind != Uint {
				return fmt.Errorf("table: %s %s: %s: only a uint can be one-based", e.Device, e.Path, p.Name)
			}
		}
		if err := e.checkSyntax(); err != nil {
			return err
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

// Alias makes words, at the start of a command line, stand for the words
// to wherever the line names no command as typed: a second name the
// parser reads and help, completion's lists and COMMANDS.md leave out
// (fairlight for audio, input list for inputs).
func (r *Registry) Alias(device, words, to string) error {
	tw := strings.Fields(to)
	if !slices.ContainsFunc(r.Entries(device), func(e Entry) bool {
		w := e.Words()
		return len(w) >= len(tw) && slices.Equal(w[:len(tw)], tw)
	}) {
		return fmt.Errorf("table: %s alias %s: no command starts %s", device, words, to)
	}
	if r.aliases == nil {
		r.aliases = map[string]string{}
	}
	r.aliases[device+" "+strings.Join(strings.Fields(words), " ")] = strings.Join(tw, " ")
	return nil
}

// Unalias returns words with the longest alias they start with replaced
// by the words it stands for, and whether they started with one.
func (r *Registry) Unalias(device string, words []string) ([]string, bool) {
	for n := len(words); n > 0; n-- {
		if to, ok := r.aliases[device+" "+strings.Join(words[:n], " ")]; ok {
			return append(strings.Fields(to), words[n:]...), true
		}
	}
	return words, false
}

// Find returns the entry with exactly this path, or nil.
func (r *Registry) Find(device, path string) *Entry {
	for i := range r.entries {
		if r.entries[i].Device == device && r.entries[i].Path == path {
			e := r.entries[i]
			return &e
		}
	}
	return nil
}

// Args holds an entry's parsed arguments.
type Args struct {
	values map[string]any
	// FPS is the display frame rate Rate parameters were converted at.
	FPS int
}

// Has reports whether name was given (or has a default).
func (a Args) Has(name string) bool { _, ok := a.values[name]; return ok }

// Value returns name's parsed value: uint64 (Uint, Enum, Flags, Source),
// int64 (Int, Rate), float64, bool, string (String, File, OutFile, Dir),
// time.Time or []string (List). A one-based Uint is the field's value,
// one less than what was typed.
func (a Args) Value(name string) any { return a.values[name] }

// Set sets name's value, for callers that build Args themselves.
func (a *Args) Set(name string, v any) {
	if a.values == nil {
		a.values = map[string]any{}
	}
	a.values[name] = v
}

// ErrUsage marks errors in what the user typed.
var ErrUsage = errors.New("usage")

func usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

// Resolver converts the values whose meaning depends on the device: a
// rate's frames depend on its frame rate, a source name on its inputs.
// Errors are reported as usage errors.
type Resolver interface {
	// Rate converts "seconds:frames" or a frame count to frames.
	Rate(s string) (int64, error)
	// Source converts a source id or name to an id.
	Source(s string) (uint64, error)
}

// Parse turns raw "--name value" pairs into Args for e, applying
// defaults and checking that required parameters are present. res
// converts rates and sources.
func (e Entry) Parse(raw map[string]string, res Resolver) (Args, error) {
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
				return a, usagef("missing <%s>: %s", p.Name, e.Usage())
			}
			if p.Default == "" {
				continue
			}
			s = p.Default
		} else if !p.Required && p.Default == "" {
			given++
		}
		v, err := p.parse(s, res)
		if err != nil {
			return a, usagef("%s %q: %v", e.Label(p.Name), s, err)
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

func (p Param) parse(s string, res Resolver) (any, error) {
	switch p.Kind {
	case Uint:
		if p.OneBased {
			i, err := ParseIndex(s)
			if err != nil {
				return nil, fmt.Errorf("%v: %s", err, p.Range())
			}
			return uint64(i), p.inRange(float64(i))
		}
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
		n, err := res.Rate(s)
		if err != nil {
			return nil, err
		}
		return n, p.inRange(float64(n))
	case Source:
		n, err := res.Source(s)
		if err != nil {
			return nil, err
		}
		return n, p.inRange(float64(n))
	case File, OutFile, Dir:
		if s == "" {
			return nil, errors.New("empty path")
		}
		return s, nil
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

// Range describes the accepted range as typed on the command line, or "".
func (p Param) Range() string {
	if p.OneBased && p.Min != nil && p.Max != nil {
		return Index(*p.Min).String() + "-" + Index(*p.Max).String()
	}
	lo, hi := p.Min, p.Max
	switch {
	case lo != nil && hi != nil && *lo < 0:
		return trim(*lo) + " to " + trim(*hi)
	case lo != nil && hi != nil:
		return trim(*lo) + "-" + trim(*hi)
	case lo != nil:
		return ">= " + trim(*lo)
	case hi != nil:
		return "<= " + trim(*hi)
	}
	return ""
}

// SortedNames returns an Enum's or Flags' names in value order.
func (p Param) SortedNames() []string { return p.names() }

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
