// SPDX-License-Identifier: MIT

package table

// A command's syntax: its words and its positional arguments, in the
// order a person says them ("usk <keyer> fill <source>"). Required
// arguments are positional; optional ones are --name flags, except an
// optional positional ([file]) where it reads naturally. Every argument
// may also be given as --name value, the form before positional
// arguments, which help no longer shows and Deprecated reports.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Elem is one element of a command's syntax: a literal word, or a
// positional argument.
type Elem struct {
	Word     string // the literal word; "" for an argument
	Param    string // the argument's parameter
	Optional bool   // an optional argument: [name]
}

// parseSyntax reads "word <required> [optional] ...".
func parseSyntax(s string) []Elem {
	var out []Elem
	for _, f := range strings.Fields(s) {
		switch {
		case strings.HasPrefix(f, "<") && strings.HasSuffix(f, ">"):
			out = append(out, Elem{Param: f[1 : len(f)-1]})
		case strings.HasPrefix(f, "[") && strings.HasSuffix(f, "]"):
			out = append(out, Elem{Param: f[1 : len(f)-1], Optional: true})
		default:
			out = append(out, Elem{Word: f})
		}
	}
	return out
}

// Elems is the entry's syntax: Syntax, or else its path's words followed
// by each required parameter in order.
func (e Entry) Elems() []Elem {
	if e.Syntax != "" {
		return parseSyntax(e.Syntax)
	}
	var out []Elem
	for _, w := range e.Words() {
		out = append(out, Elem{Word: w})
	}
	for _, p := range e.Params {
		if p.Required {
			out = append(out, Elem{Param: p.Name})
		}
	}
	return out
}

// Usage is the syntax as written: "usk <keyer> fill <source>".
func (e Entry) Usage() string { return usage(e.Elems()) }

func usage(es []Elem) string {
	var parts []string
	for _, x := range es {
		switch {
		case x.Word != "":
			parts = append(parts, x.Word)
		case x.Optional:
			parts = append(parts, "["+x.Param+"]")
		default:
			parts = append(parts, "<"+x.Param+">")
		}
	}
	return strings.Join(parts, " ")
}

// Positional reports whether the parameter is a positional argument.
func (e Entry) Positional(name string) bool {
	return slices.ContainsFunc(e.Elems(), func(x Elem) bool { return x.Param == name })
}

// Label is how messages name a parameter: a positional one by its name,
// a flag as --name.
func (e Entry) Label(name string) string {
	if e.Positional(name) {
		return name
	}
	return "--" + name
}

// checkSyntax checks the syntax against the path and the parameters:
// its words are the path's, every required parameter is a required
// positional argument, and an optional positional is an optional one.
func (e Entry) checkSyntax() error {
	var words []string
	seen := map[string]bool{}
	for _, x := range e.Elems() {
		if x.Word != "" {
			words = append(words, x.Word)
			continue
		}
		k := slices.IndexFunc(e.Params, func(p Param) bool { return p.Name == x.Param })
		switch {
		case k < 0:
			return fmt.Errorf("table: %s %s: <%s> is no parameter", e.Device, e.Path, x.Param)
		case seen[x.Param]:
			return fmt.Errorf("table: %s %s: <%s> twice", e.Device, e.Path, x.Param)
		case e.Params[k].Required == x.Optional:
			return fmt.Errorf("table: %s %s: <%s> is required exactly when its parameter is", e.Device, e.Path, x.Param)
		}
		seen[x.Param] = true
	}
	if strings.Join(words, " ") != e.Path {
		return fmt.Errorf("table: %s %s: the syntax's words are %q", e.Device, e.Path, strings.Join(words, " "))
	}
	for _, p := range e.Params {
		if p.Required && !seen[p.Name] {
			return fmt.Errorf("table: %s %s: required --%s isn't positional", e.Device, e.Path, p.Name)
		}
	}
	return nil
}

// FlagParam finds the parameter a --name flag names: exactly, or by an
// unambiguous prefix (--sat for --saturation).
func (e Entry) FlagParam(name string) (*Param, error) {
	var hits []*Param
	for i := range e.Params {
		if e.Params[i].Name == name {
			return &e.Params[i], nil
		}
		if strings.HasPrefix(e.Params[i].Name, name) {
			hits = append(hits, &e.Params[i])
		}
	}
	switch len(hits) {
	case 0:
		return nil, usagef("%s has no --%s", e.Path, name)
	case 1:
		return hits[0], nil
	}
	var ns []string
	for _, p := range hits {
		ns = append(ns, "--"+p.Name)
	}
	return nil, usagef("--%s could be %s", name, strings.Join(ns, " or "))
}

// match binds words to the syntax, every word to an element. Arguments
// given as flags are skipped; any other argument may be left empty, to be
// filled from the values after the flags or reported missing. It returns
// the bindings and how many required arguments were left empty, binding
// before skipping wherever both fit.
func match(es []Elem, words []string, given map[string]bool) (map[string]string, int, bool) {
	if len(es) == 0 {
		if len(words) == 0 {
			return map[string]string{}, 0, true
		}
		return nil, 0, false
	}
	x := es[0]
	if x.Word != "" {
		if len(words) == 0 || words[0] != x.Word {
			return nil, 0, false
		}
		return match(es[1:], words[1:], given)
	}
	if given[x.Param] {
		return match(es[1:], words, given)
	}
	if len(words) > 0 {
		if b, n, ok := match(es[1:], words[1:], given); ok {
			b[x.Param] = words[0]
			return b, n, true
		}
	}
	b, n, ok := match(es[1:], words, given)
	if ok && !x.Optional {
		n++
	}
	return b, n, ok
}

// NoCommand is ParseLine's error when the words name no command. Prefix
// is the longest group the words start with; Partial reports that the
// words begin a command (a subcommand is missing) rather than name none.
type NoCommand struct {
	Words   []string
	Prefix  []string
	Partial bool
}

func (e *NoCommand) Error() string {
	if e.Partial && len(e.Prefix) == 0 {
		return "missing a command"
	}
	if e.Partial {
		return fmt.Sprintf("%s: missing a subcommand", strings.Join(e.Prefix, " "))
	}
	return fmt.Sprintf("unknown command %q", strings.Join(e.Words, " "))
}

// ParseLine reads a command line for one of device's commands: words are
// the words before the first flag, tokens the rest (flags, their values,
// and positional arguments given after flags). It returns the entry and
// its arguments as --name value pairs, however each was given, or a
// *NoCommand or usage error. A line that starts with an alias and names
// no command as typed is read with the word the alias stands for.
func (r *Registry) ParseLine(device string, words, tokens []string) (*Entry, map[string]string, error) {
	e, raw, err := r.parseLine(device, words, tokens)
	var nc, alias *NoCommand
	if alt, ok := r.Unalias(device, words); ok && errors.As(err, &nc) {
		e, raw, err := r.parseLine(device, alt, tokens)
		if !errors.As(err, &alias) {
			return e, raw, err
		}
		// Naming no command either way, the words get the group the
		// alias leads to, if it goes further.
		if len(alias.Prefix) > len(nc.Prefix) {
			return nil, nil, &NoCommand{Words: words, Prefix: alias.Prefix, Partial: alias.Partial}
		}
	}
	return e, raw, err
}

func (r *Registry) parseLine(device string, words, tokens []string) (*Entry, map[string]string, error) {
	var best *Entry
	var bound map[string]string
	bestSkipped, bestLiterals := 0, 0
	for _, cand := range r.Entries(device) {
		given := cand.givenFlags(tokens)
		b, skipped, ok := match(cand.Elems(), words, given)
		literals := len(cand.Words())
		if ok && (best == nil || skipped < bestSkipped || skipped == bestSkipped && literals > bestLiterals) {
			e := cand
			best, bound, bestSkipped, bestLiterals = &e, b, skipped, literals
		}
	}
	if best == nil {
		// An argument given positionally and as a flag too.
		for _, cand := range r.Entries(device) {
			if b, _, ok := match(cand.Elems(), words, map[string]bool{}); ok {
				for _, x := range cand.Elems() {
					if _, bound := b[x.Param]; bound && cand.givenFlags(tokens)[x.Param] {
						e := cand
						return &e, nil, usagef("%s given twice", x.Param)
					}
				}
			}
		}
		// A command with words left over names the command and says
		// what is extra.
		for k := len(words) - 1; k > 0; k-- {
			for _, cand := range r.Entries(device) {
				if _, _, ok := match(cand.Elems(), words[:k], map[string]bool{}); ok {
					e := cand
					return &e, nil, usagef("unexpected %q: %s", strings.Join(words[k:], " "), e.Usage())
				}
			}
		}
		return nil, nil, r.noCommand(device, words)
	}
	raw, err := best.readTokens(bound, tokens)
	return best, raw, err
}

// givenFlags are the parameters tokens give as flags.
func (e Entry) givenFlags(tokens []string) map[string]bool {
	given := map[string]bool{}
	for _, t := range tokens {
		if name, ok := strings.CutPrefix(t, "--"); ok && name != "" {
			name, _, _ = strings.Cut(name, "=")
			if p, err := e.FlagParam(name); err == nil {
				given[p.Name] = true
			}
		}
	}
	return given
}

// readTokens reads the flags and the positional values after them into
// the arguments the words bound.
func (e *Entry) readTokens(bound map[string]string, tokens []string) (map[string]string, error) {
	raw := map[string]string{}
	for k, v := range bound {
		raw[k] = v
	}
	var bare []string
	for i := 0; i < len(tokens); {
		if !isFlag(tokens[i]) {
			bare = append(bare, tokens[i])
			i++
			continue
		}
		p, value, next, err := e.flagAt(tokens, i)
		if err != nil {
			return nil, err
		}
		if _, dup := raw[p.Name]; dup {
			return nil, usagef("%s given twice", e.Label(p.Name))
		}
		raw[p.Name] = value
		i = next
	}
	// Values after the flags fill the positional arguments still empty,
	// in order.
	for _, x := range e.Elems() {
		if x.Word != "" || len(bare) == 0 {
			continue
		}
		if _, ok := raw[x.Param]; !ok {
			raw[x.Param], bare = bare[0], bare[1:]
		}
	}
	if len(bare) > 0 {
		return nil, usagef("unexpected %q: %s", bare[0], e.Usage())
	}
	return raw, nil
}

func isFlag(t string) bool { return strings.HasPrefix(t, "--") && t != "--" }

// flagAt reads the flag tokens[i]: its parameter, its value, and where
// the next token starts. A bool's value may be left out, meaning true.
func (e *Entry) flagAt(tokens []string, i int) (*Param, string, int, error) {
	name, value, hasValue := strings.Cut(tokens[i][2:], "=")
	p, err := e.FlagParam(name)
	switch {
	case err != nil:
		return nil, "", 0, err
	case hasValue:
		return p, value, i + 1, nil
	case p.Kind == Bool && (i+1 == len(tokens) || !IsBoolWord(tokens[i+1])):
		return p, "true", i + 1, nil
	case i+1 == len(tokens):
		return nil, "", 0, usagef("--%s needs a value", p.Name)
	}
	return p, tokens[i+1], i + 2, nil
}

// Deprecated reports the positional arguments tokens give in the --name
// form, which this release still reads (docs/PORTING.md), and the line as
// it is written now: the syntax with raw's values, then the other flags
// as they were typed. It reports nothing for a line already written so.
func (e *Entry) Deprecated(raw map[string]string, tokens []string) (flags []string, line string) {
	var rest []string
	for i := 0; i < len(tokens); {
		if !isFlag(tokens[i]) {
			i++ // a positional value after the flags: the syntax places it
			continue
		}
		p, _, next, err := e.flagAt(tokens, i)
		if err != nil {
			return nil, ""
		}
		if e.Positional(p.Name) {
			flags = append(flags, "--"+p.Name)
		} else {
			for _, t := range tokens[i:next] {
				rest = append(rest, shellWord(t))
			}
		}
		i = next
	}
	if len(flags) == 0 {
		return nil, ""
	}
	var out []string
	for _, x := range e.Elems() {
		v, ok := raw[x.Param]
		switch {
		case x.Word != "":
			out = append(out, x.Word)
		case ok && e.param(x.Param).Kind == Bool:
			out = append(out, map[bool]string{true: "on", false: "off"}[isTrue(v)])
		case ok:
			out = append(out, shellWord(v))
		}
	}
	return flags, strings.Join(append(out, rest...), " ")
}

func (e *Entry) param(name string) Param {
	return e.Params[slices.IndexFunc(e.Params, func(p Param) bool { return p.Name == name })]
}

func isTrue(s string) bool {
	switch strings.ToLower(s) {
	case "true", "on", "yes", "1":
		return true
	}
	return false
}

// shellWord quotes s for a shell if it needs it.
func shellWord(s string) string {
	plain := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.,:/+=@%", r)
	}
	switch {
	case s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0:
		return s
	case strings.ContainsAny(s, "$`\\\"!"):
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return `"` + s + `"`
}

// IsBoolWord reports whether s is a value a bool takes.
func IsBoolWord(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "on", "off", "yes", "no", "1", "0":
		return true
	}
	return false
}

// noCommand explains words that name no command.
func (r *Registry) noCommand(device string, words []string) error {
	nc := &NoCommand{Words: words}
	for _, e := range r.Entries(device) {
		es := e.Elems()
		lits, ok := prefixLiterals(es, words)
		if ok && len(es) > 0 && len(lits) < len(e.Words()) {
			if !nc.Partial || len(lits) > len(nc.Prefix) {
				nc.Prefix, nc.Partial = lits, true
			}
			continue
		}
		if !nc.Partial && len(lits) > len(nc.Prefix) && len(lits) < len(e.Words()) {
			nc.Prefix = lits
		}
	}
	return nc
}

// prefixLiterals walks words along the syntax as far as they fit,
// arguments taking any word. It returns the literal words passed and
// whether every word fit.
func prefixLiterals(es []Elem, words []string) ([]string, bool) {
	var lits []string
	i := 0
	for _, w := range words {
		for i < len(es) && es[i].Word == "" && es[i].Optional && i+1 < len(es) && es[i+1].Word == w {
			i++ // an optional argument left out
		}
		if i == len(es) {
			return lits, false
		}
		if es[i].Word != "" {
			if es[i].Word != w {
				return lits, false
			}
			lits = append(lits, w)
		}
		i++
	}
	return lits, true
}

// NextElem is an element that may come next, and a command it belongs to.
type NextElem struct {
	Elem
	Entry *Entry
}

// Next lists what may follow words in device's commands: literal words
// and positional arguments, for completion. It also returns the command
// the words name whole, if any. After an alias, as in ParseLine, the
// commands the line names as typed come first.
func (r *Registry) Next(device string, words []string) (next []NextElem, whole *Entry) {
	next, whole = r.next(device, words)
	if alt, ok := r.Unalias(device, words); ok && next == nil && whole == nil {
		return r.next(device, alt)
	}
	return next, whole
}

func (r *Registry) next(device string, words []string) (next []NextElem, whole *Entry) {
	for _, e := range r.Entries(device) {
		es := e.Elems()
		for _, i := range nextAfter(es, words) {
			x := e
			if i == len(es) {
				whole = &x
				continue
			}
			if !slices.ContainsFunc(next, func(n NextElem) bool { return n.Elem == es[i] }) {
				next = append(next, NextElem{es[i], &x})
			}
		}
	}
	return next, whole
}

// nextAfter returns the positions in es words can end before.
func nextAfter(es []Elem, words []string) []int {
	if len(words) == 0 {
		out := []int{0}
		for i := 0; i < len(es) && es[i].Word == "" && es[i].Optional; i++ {
			out = append(out, i+1)
		}
		return out
	}
	if len(es) == 0 {
		return nil
	}
	var out []int
	x := es[0]
	if x.Word == "" && x.Optional {
		for _, i := range nextAfter(es[1:], words) {
			out = append(out, i+1)
		}
	}
	if x.Word == "" || x.Word == words[0] {
		for _, i := range nextAfter(es[1:], words[1:]) {
			out = append(out, i+1)
		}
	}
	return out
}

// Child is one line of a group's help: the rest of a command's syntax
// after the group's words, or a subgroup's word and "...".
type Child struct {
	Usage   string
	Summary string
	Group   bool
}

// Children lists what comes after the group prefix: each command's
// remaining syntax, and subgroups as their word.
func (r *Registry) Children(device string, prefix []string) []Child {
	var out []Child
	seen := map[string]bool{}
	for _, e := range r.Entries(device) {
		w := e.Words()
		if len(w) <= len(prefix) || !slices.Equal(w[:len(prefix)], prefix) {
			continue
		}
		next := w[len(prefix)]
		if len(w) > len(prefix)+1 {
			if !seen[next] {
				seen[next] = true
				out = append(out, Child{Usage: next, Group: true})
			}
			continue
		}
		// The syntax less the group's words.
		es, dropped := e.Elems(), 0
		var rest []Elem
		for _, x := range es {
			if x.Word != "" && dropped < len(prefix) {
				dropped++
				continue
			}
			rest = append(rest, x)
		}
		out = append(out, Child{Usage: usage(rest), Summary: e.Summary})
	}
	slices.SortFunc(out, func(a, b Child) int { return strings.Compare(sortKey(a), sortKey(b)) })
	return out
}

// sortKey orders children by their first word.
func sortKey(c Child) string {
	for _, f := range strings.Fields(c.Usage) {
		if !strings.HasPrefix(f, "<") && !strings.HasPrefix(f, "[") {
			return f
		}
	}
	return c.Usage
}

// Shown is how help shows a parameter: <name> for a positional argument,
// [name] for an optional one, --name for a flag.
func (e Entry) Shown(name string) string {
	for _, x := range e.Elems() {
		if x.Param == name {
			if x.Optional {
				return "[" + name + "]"
			}
			return "<" + name + ">"
		}
	}
	return "--" + name
}

// ordered is the parameters as help lists them: the positional arguments
// in the order they're said, then the flags in the table's order.
func (e Entry) ordered() []Param {
	var args, flags []Param
	for _, p := range e.Params {
		if e.Positional(p.Name) {
			args = append(args, p)
		} else {
			flags = append(flags, p)
		}
	}
	es := e.Elems()
	pos := func(name string) int { return slices.IndexFunc(es, func(x Elem) bool { return x.Param == name }) }
	slices.SortStableFunc(args, func(a, b Param) int { return pos(a.Name) - pos(b.Name) })
	return append(args, flags...)
}

// Ordered is the parameters as help lists them: the positional arguments
// in the order they're said, then the flags.
func (e Entry) Ordered() []Param { return e.ordered() }
