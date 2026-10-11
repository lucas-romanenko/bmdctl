// SPDX-License-Identifier: MIT

package engine

// How a source typed on a command line is found, in this order, case
// ignored: a number is that input; an alias names an input or one of
// the switcher's fixed sources whatever its label (cam2, input 2, mp1,
// bars, black, col1, pgm, pvw, cf1, aux3); else an input's short or
// long name; else the one input a name starts. atemwire has no such
// names (bmdwire v1.3.0 takes ids); docs/PORTING.md, "Operations",
// records them. A name an alias shadows, and a source nothing names,
// are told to a person in notes (Note.Alias, Note.NoInput), so the
// response's error stays what --json always had.

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

var (
	inputAlias = regexp.MustCompile(`^(?i)(?:cam|camera|in|input) *(\d{1,4})$`)
	fixedAlias = regexp.MustCompile(`^(?i)(mp|col|cf|aux)(\d{1,2})$`)
)

// fixedBase is where each numbered fixed source starts: media player n
// is 3000 + 10n, colour generator n 2000 + n, clean feed n 7000 + n,
// aux output n 8000 + n.
var fixedBase = map[string][2]int{"mp": {3000, 10}, "col": {2000, 1}, "cf": {7000, 1}, "aux": {8000, 1}}

// fixedNames are the fixed sources an alias names: black, colour bars,
// and M/E 1's program and preview outputs.
var fixedNames = map[string]int{"black": 0, "blk": 0, "bars": 1000, "pgm": 10010, "pvw": 10011}

// SourceAlias is the source an alias names, whatever the switcher calls
// it: cam2, camera 2, in2, input 2; mp1, bars, black, blk, col1, pgm,
// pvw, cf1, aux3. It needs no switcher.
func SourceAlias(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if m := inputAlias.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, n > 0
	}
	if m := fixedAlias.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		b := fixedBase[strings.ToLower(m[1])]
		return b[0] + b[1]*n, n > 0
	}
	id, ok := fixedNames[strings.ToLower(s)]
	return id, ok
}

// Sources resolves sources as a person types them (above), and rates as
// r does. A nil call takes no notes: the CLI's --dry-run, which has no
// switcher, so only numbers and aliases.
func Sources(r atem.Resolver, c *call) table.Resolver { return sources{r, c} }

type sources struct {
	atem.Resolver
	c *call
}

func (r sources) Source(s string) (uint64, error) {
	t := strings.TrimSpace(s)
	if t == "" || r.Offline || isNumber(t) {
		return r.Resolver.Source(s)
	}
	st := r.State
	if id, ok := SourceAlias(t); ok && (len(st.Inputs) == 0 || id == 0 || st.Inputs[uint16(id)] != nil) {
		if named := st.InputsNamed(t); len(named) > 0 && int(named[0]) != id {
			r.c.noteAlias(t, id, int(named[0]))
		}
		return uint64(id), nil
	}
	if len(st.InputsNamed(t)) > 0 || len(st.Inputs) == 0 {
		return r.Resolver.Source(s) // a name, or the switcher has given none
	}
	switch ids := inputsStarting(st, t); len(ids) {
	case 0:
	case 1:
		return uint64(ids[0]), nil
	default:
		var cands []string
		for _, id := range ids[:min(len(ids), 6)] {
			cands = append(cands, sourceName(st, int(id)))
		}
		if len(ids) > 6 {
			cands = append(cands, "…")
		}
		return 0, fmt.Errorf("starts more than one input's name: %s", strings.Join(cands, ", "))
	}
	r.c.noteNoInput(t)
	return r.Resolver.Source(s) // no input is named that
}

func isNumber(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// inputsStarting are the inputs whose short or long name starts with s,
// ignoring case, in id order.
func inputsStarting(st atem.State, s string) []uint16 {
	var ids []uint16
	for id, in := range st.Inputs {
		if in == nil {
			continue
		}
		for _, n := range []string{in.ShortName, in.Name} {
			if n = strings.TrimSpace(n); len(n) >= len(s) && strings.EqualFold(n[:len(s)], s) {
				ids = append(ids, id)
				break
			}
		}
	}
	slices.Sort(ids)
	return ids
}

// noteAlias tells a person that an alias won over an input's name.
func (c *call) noteAlias(typed string, id, shadowed int) {
	if c == nil {
		return
	}
	c.note(Note{Alias: &Alias{Typed: typed, Source: sourceOf(c.state, id), Shadowed: sourceOf(c.state, shadowed)}})
}

// noteNoInput tells a person what a source that names nothing could have
// been: the switcher's first six inputs.
func (c *call) noteNoInput(typed string) {
	if c == nil {
		return
	}
	n := &NoInput{Typed: typed, Inputs: []Source{}}
	for _, v := range c.state.AllInputSources() {
		if len(n.Inputs) == 6 {
			break
		}
		n.Inputs = append(n.Inputs, Source{ID: v.Source, Name: v.Long, Short: v.Short})
	}
	c.noInput = n
}
