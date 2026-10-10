// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The Profile type of bmdwire v1.3.0 atemwire/profile/__init__.py:
// construction from XML, serialization and the accessors. Save is in
// save.go, Apply in apply.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
)

// Profile is an ATEM Software Control "Save Switcher State" profile: the
// parsed XML tree, kept whole so whatever Apply doesn't read round-trips.
type Profile struct {
	root *Element
}

// New wraps an element tree: Profile(root).
func New(root *Element) *Profile { return &Profile{root: root} }

// Source is what Save reads: a session's state and its macro store.
// *atem.Session satisfies it.
type Source interface {
	State() atem.State
	DownloadMacro(ctx context.Context, slot int) ([]byte, error)
}

// Target is what Apply drives: it reads the switcher's state (its M/E
// count and video mode), sends commands and uploads macros.
// *atem.Session satisfies it. A Target that also has Connected() bool and
// reports false gets its macros skipped, as atemwire skips them without a
// connected protocol.
type Target interface {
	State() atem.State
	Send(cmd atem.Command) error
	UploadMacro(ctx context.Context, slot int, name, description string, bytecode []byte) error
}

// A Session is both.
var (
	_ Source = (*atem.Session)(nil)
	_ Target = (*atem.Session)(nil)
)

// FromXML parses a profile: Profile.from_xml. Like atemwire it refuses
// bytes that aren't UTF-8, any NUL (a UTF-16 document would hide a DTD
// from the next check) and any DTD or entity declaration (an
// entity-expansion guard) before parsing. The root's tag and version are
// not checked.
func FromXML(data []byte) (*Profile, error) {
	if !utf8.Valid(data) {
		return nil, unicodeDecodeError(data)
	}
	s := string(data)
	if strings.IndexByte(s, 0) >= 0 {
		return nil, &Error{"ValueError", "profile XML must be UTF-8 text (NUL byte found — wide-encoding content is refused)"}
	}
	if hasDTDDecl(s) {
		return nil, &Error{"ValueError", "profile XML must not contain a DTD/DOCTYPE or <!ENTITY> declaration (entity-expansion guard)"}
	}
	root, err := parseXML(s)
	if err != nil {
		return nil, err
	}
	return &Profile{root: root}, nil
}

// FromFile reads and parses a profile: Profile.from_file.
func FromFile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FromXML(data)
}

// unicodeDecodeError is the UnicodeDecodeError bytes.decode('utf-8')
// raises, with CPython's message.
func unicodeDecodeError(data []byte) error {
	i := 0
	for i < len(data) {
		r, n := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && n <= 1 {
			break
		}
		i += n
	}
	n := maximalSubpart(string(data[i:]))
	reason := "invalid continuation byte"
	switch c := data[i]; {
	case c < 0xc2 || c > 0xf4:
		reason = "invalid start byte"
	case i+n == len(data):
		reason = "unexpected end of data"
	}
	what := fmt.Sprintf("byte 0x%02x in position %d", data[i], i)
	if n > 1 {
		what = fmt.Sprintf("bytes in position %d-%d", i, i+n-1)
	}
	return &Error{"UnicodeDecodeError", fmt.Sprintf("'utf-8' codec can't decode %s: %s", what, reason)}
}

// hasDTDDecl is _DTD_DECL_RE.search: "<!" then DOCTYPE or ENTITY, case
// insensitively as Python's re does it (where I also matches U+0130 and
// U+0131), then a word boundary in Python's Unicode sense.
func hasDTDDecl(s string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], "<!")
		if j < 0 {
			return false
		}
		i += j + 2
		for _, word := range []string{"DOCTYPE", "ENTITY"} {
			if end, ok := matchFold(s[i:], word); ok {
				r, _ := utf8.DecodeRuneInString(s[i+end:])
				if i+end == len(s) || !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_') {
					return true
				}
			}
		}
	}
}

func matchFold(s, word string) (int, bool) {
	n := 0
	for _, w := range word {
		r, size := utf8.DecodeRuneInString(s[n:])
		if size == 0 {
			return 0, false
		}
		ok := r == w || r == unicode.ToLower(w) || w == 'I' && (r == 0x130 || r == 0x131)
		if !ok {
			return 0, false
		}
		n += size
	}
	return n, true
}

// ToXML serializes the profile as ATEM Software Control writes it:
// Profile.to_xml. The UTF-8 declaration, four-space indentation, "/>"
// with no space before it, and a final newline. Like atemwire it
// re-indents the profile's own tree first. It fails only for a tree whose
// tag or attribute name starts "{" with no "}" (ElementTree raises too).
func (p *Profile) ToXML() (string, error) {
	indent(p.root, "    ")
	body, err := serialize(p.root)
	if err != nil {
		return "", err
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + compactEmptyTags(body) + "\n", nil
}

// ToFile writes ToXML to path: Profile.to_file.
func (p *Profile) ToFile(path string) error {
	s, err := p.ToXML()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// Root is the XML tree, for inspection and editing.
func (p *Profile) Root() *Element { return p.root }

// MajorVersion is the root's majorVersion (_int, default 2). It fails, as
// atemwire raises OverflowError, for an infinite value.
func (p *Profile) MajorVersion() (int, error) {
	return intAttr(p.root.attr("majorVersion"), MajorVersion)
}

// MinorVersion is the root's minorVersion (_int, default 1).
func (p *Profile) MinorVersion() (int, error) {
	return intAttr(p.root.attr("minorVersion"), MinorVersion)
}

// intAttr is _int as a Go int; a value beyond int64 is an error here,
// where Python's int would hold it.
func intAttr(s *string, def int) (int, error) {
	v, err := attrInt(s, float64(def))
	if err != nil {
		return 0, err
	}
	if !fitsInt(v, -1<<63, 1<<63-1024) {
		return 0, &Error{"OverflowError", "int too large for Go: " + intString(v)}
	}
	return int(v), nil
}

// Product is the root's product attribute, "" if absent.
func (p *Profile) Product() string { return p.root.GetDefault("product", "") }

// VideoMode is <VideoMode videoMode>, the raw string, "" if absent.
func (p *Profile) VideoMode() string {
	if n := p.root.Find("VideoMode"); n != nil {
		return n.GetDefault("videoMode", "")
	}
	return ""
}

// Macro is one <Macro> of the <MacroPool>: Profile.macros()'s dict.
type Macro struct {
	Index       int
	Name        string
	Description string
	// Ops are the <Op> elements: the id and every other attribute in
	// order, the macro codec's form.
	Ops []macro.Op
}

// Macros lists the <MacroPool>'s macros: Profile.macros().
func (p *Profile) Macros() ([]Macro, error) {
	out := []Macro{}
	pool := p.root.Find("MacroPool")
	if pool == nil {
		return out, nil
	}
	for _, m := range pool.FindAll("Macro") {
		idx, err := intAttr(m.attr("index"), 0)
		if err != nil {
			return nil, err
		}
		mac := Macro{Index: idx, Name: m.GetDefault("name", ""), Description: m.GetDefault("description", ""), Ops: []macro.Op{}}
		for _, o := range m.FindAll("Op") {
			op := macro.Op{ID: o.GetDefault("id", "")}
			for _, a := range o.Attrs {
				if a.Name != "id" {
					op.Params = append(op.Params, macro.Param{Name: a.Name, Value: a.Value})
				}
			}
			mac.Ops = append(mac.Ops, op)
		}
		out = append(out, mac)
	}
	return out, nil
}

// errorClass is the Python class and message NoteError writes for err:
// the class an *Error or *macro.Error carries; ConnectionDeadError, what
// atemwire's send raises on a dead connection, for a session that has
// closed or dropped; RuntimeError for anything else.
func errorClass(err error) (class, msg string) {
	var pe *Error
	var me *macro.Error
	switch {
	case errors.As(err, &pe):
		return pe.Class, pe.Message
	case errors.As(err, &me):
		return me.Class, me.Message
	case errors.Is(err, atem.ErrClosed), errors.Is(err, atem.ErrDisconnected), errors.Is(err, atem.ErrNotReady):
		return "ConnectionDeadError", err.Error()
	}
	return "RuntimeError", err.Error()
}
