// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The XML layer atemwire gets from Python's xml.etree.ElementTree (the
// Element tree, ET.fromstring over expat with namespace processing,
// ET.indent and ET.tostring), as bmdwire v1.3.0 atemwire/profile uses it.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Element is one XML element, as ElementTree holds it: a tag ("{uri}local"
// for a namespaced name), attributes in document order, children, the text
// before the first child and the tail after the element's end tag.
type Element struct {
	Tag      string
	Attrs    []Attr
	Children []*Element
	Text     string
	Tail     string
}

// Attr is one attribute: its name ("{uri}local" when namespaced) and value.
type Attr struct{ Name, Value string }

// NewElement returns an element with no attributes or children.
func NewElement(tag string) *Element { return &Element{Tag: tag} }

// Get returns the value of attribute name: elem.get(name).
func (e *Element) Get(name string) (string, bool) {
	for _, a := range e.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// GetDefault is elem.get(name, def).
func (e *Element) GetDefault(name, def string) string {
	if v, ok := e.Get(name); ok {
		return v
	}
	return def
}

// attr is elem.get(name) as a pointer: nil is Python's None.
func (e *Element) attr(name string) *string {
	if v, ok := e.Get(name); ok {
		return &v
	}
	return nil
}

// Set sets attribute name, in place if it exists (as a dict assignment
// keeps the key's position), else at the end.
func (e *Element) Set(name, value string) {
	for i := range e.Attrs {
		if e.Attrs[i].Name == name {
			e.Attrs[i].Value = value
			return
		}
	}
	e.Attrs = append(e.Attrs, Attr{name, value})
}

// SubElement appends a new child with tag and returns it: ET.SubElement.
func (e *Element) SubElement(tag string) *Element {
	c := NewElement(tag)
	e.Children = append(e.Children, c)
	return c
}

// FindAll returns the elements a path of tags separated by "/" selects, in
// document order: ElementTree's findall for the simple paths atemwire
// uses.
func (e *Element) FindAll(path string) []*Element {
	cur := []*Element{e}
	for _, tag := range strings.Split(path, "/") {
		var next []*Element
		for _, p := range cur {
			for _, c := range p.Children {
				if c.Tag == tag {
					next = append(next, c)
				}
			}
		}
		cur = next
	}
	return cur
}

// Find returns the first element FindAll would, or nil: ElementTree's find.
func (e *Element) Find(path string) *Element {
	if all := e.FindAll(path); len(all) > 0 {
		return all[0]
	}
	return nil
}

// iter is elem.iter(): e and its descendants, depth first.
func (e *Element) iter(f func(*Element)) {
	f(e)
	for _, c := range e.Children {
		c.iter(f)
	}
}

// ---------------------------------------------------------------------------
// Parsing: ET.fromstring, which is expat with namespace processing feeding
// a TreeBuilder.
// ---------------------------------------------------------------------------

// ParseError is a document expat refuses: ElementTree's ParseError. The
// messages are expat's for the common cases; the positions are close to
// expat's but not guaranteed to match.
type ParseError struct {
	Msg    string
	Line   int
	Column int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: line %d, column %d", e.Msg, e.Line, e.Column)
}

const (
	nsXML   = "http://www.w3.org/XML/1998/namespace"
	nsXMLNS = "http://www.w3.org/2000/xmlns/"
)

type xmlParser struct {
	s      string
	i      int
	err    *ParseError
	stack  []*Element
	names  []string // raw (prefixed) names of the open elements
	scopes []map[string]string
	root   *Element
	// TreeBuilder state.
	data []string
	last *Element
	tail bool
}

// parseXML parses a whole document. s must be valid UTF-8.
func parseXML(s string) (root *Element, err error) {
	p := &xmlParser{s: s, scopes: []map[string]string{{"xml": nsXML}}}
	defer func() {
		if r := recover(); r != nil {
			pe, ok := r.(*ParseError)
			if !ok {
				panic(r)
			}
			root, err = nil, pe
		}
	}()
	p.document()
	return p.root, nil
}

func (p *xmlParser) fail(msg string) {
	p.failAt(msg, p.i)
}

func (p *xmlParser) failAt(msg string, at int) {
	if at > len(p.s) {
		at = len(p.s)
	}
	line := 1 + strings.Count(p.s[:at], "\n")
	col := utf8.RuneCountInString(p.s[strings.LastIndexByte(p.s[:at], '\n')+1 : at])
	panic(&ParseError{Msg: msg, Line: line, Column: col})
}

func (p *xmlParser) eof() bool { return p.i >= len(p.s) }

func (p *xmlParser) peek() rune {
	if p.i >= len(p.s) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return r
}

func (p *xmlParser) has(prefix string) bool { return strings.HasPrefix(p.s[p.i:], prefix) }

func isS(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// isXMLChar is XML 1.0's Char production.
func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xa || r == 0xd || r >= 0x20 && r <= 0xd7ff ||
		r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff
}

// isNameStart and isNameChar are XML name characters without the colon,
// which namespace processing reserves. Like expat, no character outside
// the Basic Multilingual Plane is a name character. For the rest of the
// BMP beyond ASCII expat uses XML 1.0's Appendix B tables; this uses Go's
// Unicode letter, digit and mark categories and Appendix B's extenders,
// which agree except for characters Unicode added since.
func isNameStart(r rune) bool {
	if r < 0x80 {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'
	}
	return r <= 0xffff && unicode.IsLetter(r)
}

func isNameChar(r rune) bool {
	if r < 0x80 {
		return isNameStart(r) || r >= '0' && r <= '9' || r == '.' || r == '-'
	}
	if r > 0xffff {
		return false
	}
	switch {
	case unicode.IsLetter(r), unicode.Is(unicode.Nd, r), unicode.Is(unicode.Mn, r), unicode.Is(unicode.Mc, r):
		return true
	case r == 0xb7, r == 0x2d0, r == 0x2d1, r == 0x387, r == 0x640, r == 0xe46, r == 0xec6, r == 0x3005,
		r >= 0x3031 && r <= 0x3035, r >= 0x309d && r <= 0x309e, r >= 0x30fc && r <= 0x30fe:
		return true
	}
	return false
}

// ncname reads a name without a colon.
func (p *xmlParser) ncname() string {
	start := p.i
	if r := p.peek(); !isNameStart(r) {
		p.fail("not well-formed (invalid token)")
	}
	for !p.eof() {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if !isNameChar(r) {
			break
		}
		p.i += n
	}
	return p.s[start:p.i]
}

// qname reads NCName (':' NCName)?, the only names namespace processing
// accepts.
func (p *xmlParser) qname() string {
	start := p.i
	p.ncname()
	if p.peek() == ':' {
		p.i++
		p.ncname()
		if p.peek() == ':' {
			p.fail("not well-formed (invalid token)")
		}
	}
	return p.s[start:p.i]
}

func (p *xmlParser) skipS() bool {
	start := p.i
	for !p.eof() && isS(rune(p.s[p.i])) {
		p.i++
	}
	return p.i > start
}

// char reads one character of the document, which must be an XML Char.
func (p *xmlParser) char() rune {
	r, n := utf8.DecodeRuneInString(p.s[p.i:])
	if !isXMLChar(r) {
		p.fail("not well-formed (invalid token)")
	}
	p.i += n
	return r
}

func (p *xmlParser) document() {
	if p.has("\uFEFF") {
		p.i += len("\uFEFF")
	}
	if p.has("<?xml") && p.i+5 < len(p.s) && (isS(rune(p.s[p.i+5])) || p.s[p.i+5] == '?') {
		p.xmlDecl()
	}
	p.misc(false)
	if p.eof() {
		p.fail("no element found")
	}
	if p.peek() != '<' {
		p.fail("syntax error")
	}
	p.element()
	p.misc(true)
	if !p.eof() {
		p.fail("junk after document element")
	}
}

// misc skips whitespace, comments and processing instructions, before the
// root element or after it.
func (p *xmlParser) misc(after bool) {
	for !p.eof() {
		switch {
		case p.skipS():
		case p.has("<!--"):
			p.comment()
		case p.has("<?"):
			p.pi()
		default:
			if after {
				return
			}
			if p.has("<!") {
				p.fail("syntax error")
			}
			return
		}
	}
}

func (p *xmlParser) xmlDecl() {
	bad := func() { p.fail("XML declaration not well-formed") }
	p.i += len("<?xml")
	pseudo := func(name string) (string, bool) {
		save := p.i
		if !p.skipS() || !p.has(name) {
			p.i = save
			return "", false
		}
		p.i += len(name)
		p.skipS()
		if p.peek() != '=' {
			bad()
		}
		p.i++
		p.skipS()
		q := p.peek()
		if q != '"' && q != '\'' {
			bad()
		}
		p.i++
		end := strings.IndexRune(p.s[p.i:], q)
		if end < 0 {
			bad()
		}
		v := p.s[p.i : p.i+end]
		p.i += end + 1
		return v, true
	}
	v, ok := pseudo("version")
	if !ok || v == "" || strings.IndexFunc(v, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r))
	}) >= 0 {
		bad()
	}
	if enc, ok := pseudo("encoding"); ok {
		if enc == "" || !(enc[0] >= 'a' && enc[0] <= 'z' || enc[0] >= 'A' && enc[0] <= 'Z') ||
			strings.IndexFunc(enc, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r))
			}) >= 0 {
			bad()
		}
	}
	if sa, ok := pseudo("standalone"); ok && sa != "yes" && sa != "no" {
		bad()
	}
	p.skipS()
	if !p.has("?>") {
		bad()
	}
	p.i += 2
}

func (p *xmlParser) comment() {
	p.i += len("<!--")
	for {
		if p.eof() {
			p.fail("unclosed token")
		}
		if p.has("--") {
			if !p.has("-->") {
				p.fail("not well-formed (invalid token)")
			}
			p.i += 3
			return
		}
		p.char()
	}
}

func (p *xmlParser) pi() {
	start := p.i
	p.i += 2
	if p.eof() {
		p.fail("unclosed token")
	}
	target := p.ncname()
	if strings.EqualFold(target, "xml") {
		if target == "xml" {
			p.failAt("XML or text declaration not at start of entity", start)
		}
		p.fail("not well-formed (invalid token)")
	}
	if p.has("?>") {
		p.i += 2
		return
	}
	if !p.skipS() {
		p.fail("not well-formed (invalid token)")
	}
	for {
		if p.eof() {
			p.fail("unclosed token")
		}
		if p.has("?>") {
			p.i += 2
			return
		}
		p.char()
	}
}

// reference reads an entity or character reference after the '&' and
// returns its text.
func (p *xmlParser) reference() string {
	start := p.i
	p.i++ // '&'
	if p.peek() == '#' {
		p.i++
		base := 10
		if p.peek() == 'x' {
			base = 16
			p.i++
		}
		ds := p.i
		for !p.eof() {
			c := p.s[p.i]
			if c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				p.i++
				continue
			}
			break
		}
		if p.i == ds || p.peek() != ';' {
			p.fail("not well-formed (invalid token)")
		}
		digits := p.s[ds:p.i]
		p.i++
		n, err := strconv.ParseUint(digits, base, 32)
		if err != nil || !isXMLChar(rune(n)) {
			p.failAt("reference to invalid character number", start)
		}
		return string(rune(n))
	}
	name := p.ncname()
	if p.peek() != ';' {
		p.fail("not well-formed (invalid token)")
	}
	p.i++
	switch name {
	case "amp":
		return "&"
	case "lt":
		return "<"
	case "gt":
		return ">"
	case "quot":
		return `"`
	case "apos":
		return "'"
	}
	p.failAt("undefined entity", start)
	return ""
}

type rawAttr struct {
	name, value string
	at          int
}

func (p *xmlParser) attValue() string {
	q := p.peek()
	if q != '"' && q != '\'' {
		p.fail("not well-formed (invalid token)")
	}
	p.i++
	var b strings.Builder
	for {
		if p.eof() {
			p.fail("unclosed token")
		}
		c := p.s[p.i]
		switch {
		case rune(c) == q:
			p.i++
			return b.String()
		case c == '<':
			p.fail("not well-formed (invalid token)")
		case c == '&':
			b.WriteString(p.reference())
		case c == '\r':
			// Line ends become \n first, then attribute-value
			// normalisation turns each whitespace character into a space.
			p.i++
			if p.has("\n") {
				p.i++
			}
			b.WriteByte(' ')
		case c == '\n' || c == '\t':
			p.i++
			b.WriteByte(' ')
		default:
			b.WriteRune(p.char())
		}
	}
}

func (p *xmlParser) element() {
	tagAt := p.i
	p.i++ // '<'
	raw := p.qname()
	var attrs []rawAttr
	for {
		sp := p.skipS()
		if p.eof() {
			p.fail("unclosed token")
		}
		if p.has("/>") || p.peek() == '>' {
			break
		}
		if !sp {
			p.fail("not well-formed (invalid token)")
		}
		at := p.i
		name := p.qname()
		p.skipS()
		if p.peek() != '=' {
			p.fail("not well-formed (invalid token)")
		}
		p.i++
		p.skipS()
		attrs = append(attrs, rawAttr{name, p.attValue(), at})
	}
	for i := range attrs {
		for j := 0; j < i; j++ {
			if attrs[j].name == attrs[i].name {
				p.failAt("duplicate attribute", attrs[i].at)
			}
		}
	}
	e := p.start(raw, attrs, tagAt)
	if p.has("/>") {
		p.i += 2
		p.end(e)
		return
	}
	p.i++ // '>'
	p.content()
	// End tag.
	endAt := p.i
	p.i += 2 // "</"
	name := p.qname()
	if name != raw {
		p.failAt("mismatched tag", endAt+2)
	}
	p.skipS()
	if p.peek() != '>' {
		p.fail("not well-formed (invalid token)")
	}
	p.i++
	p.end(e)
}

// content reads an element's content up to its end tag.
func (p *xmlParser) content() {
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			p.data = append(p.data, b.String())
			b.Reset()
		}
	}
	for {
		if p.eof() {
			p.fail("no element found")
		}
		c := p.s[p.i]
		switch {
		case c == '<':
			switch {
			case p.has("</"):
				flush()
				return
			case p.has("<!--"):
				p.comment()
			case p.has("<![CDATA["):
				p.i += len("<![CDATA[")
				for {
					if p.eof() {
						p.fail("unclosed CDATA section")
					}
					if p.has("]]>") {
						p.i += 3
						break
					}
					if p.s[p.i] == '\r' {
						p.i++
						if p.has("\n") {
							p.i++
						}
						b.WriteByte('\n')
						continue
					}
					b.WriteRune(p.char())
				}
			case p.has("<?"):
				p.pi()
			case p.has("<!"):
				p.fail("not well-formed (invalid token)")
			default:
				flush()
				p.element()
			}
		case c == '&':
			b.WriteString(p.reference())
		case c == '\r':
			p.i++
			if p.has("\n") {
				p.i++
			}
			b.WriteByte('\n')
		case c == ']' && p.has("]]>"):
			p.fail("not well-formed (invalid token)")
		default:
			b.WriteRune(p.char())
		}
	}
}

// start does expat's namespace processing on a start tag and hands the
// element to the tree builder.
func (p *xmlParser) start(raw string, attrs []rawAttr, at int) *Element {
	scope := p.scopes[len(p.scopes)-1]
	copied := false
	bind := func(prefix, uri string) {
		if !copied {
			m := make(map[string]string, len(scope)+1)
			for k, v := range scope {
				m[k] = v
			}
			scope, copied = m, true
		}
		if uri == "" {
			delete(scope, prefix)
		} else {
			scope[prefix] = uri
		}
	}
	var plain []rawAttr
	for _, a := range attrs {
		switch {
		case a.name == "xmlns":
			if a.value == nsXML || a.value == nsXMLNS {
				p.failAt("prefix must not be bound to one of the reserved namespace names", at)
			}
			bind("", a.value)
		case strings.HasPrefix(a.name, "xmlns:"):
			prefix := a.name[len("xmlns:"):]
			switch {
			case prefix == "xmlns":
				p.failAt("reserved prefix (xmlns) must not be declared or undeclared", at)
			case prefix == "xml":
				if a.value != nsXML {
					p.failAt("reserved prefix (xml) must not be undeclared or bound to another namespace name", at)
				}
			case a.value == "":
				p.failAt("must not undeclare prefix", at)
			case a.value == nsXML || a.value == nsXMLNS:
				p.failAt("prefix must not be bound to one of the reserved namespace names", at)
			default:
				bind(prefix, a.value)
			}
		default:
			plain = append(plain, a)
		}
	}
	expand := func(name string, element bool) string {
		prefix, local, ok := strings.Cut(name, ":")
		if !ok {
			if element {
				if uri := scope[""]; uri != "" {
					return "{" + uri + "}" + name
				}
			}
			return name
		}
		uri, bound := scope[prefix]
		if !bound || prefix == "" {
			p.failAt("unbound prefix", at)
		}
		return "{" + uri + "}" + local
	}
	e := &Element{Tag: expand(raw, true)}
	for _, a := range plain {
		name := expand(a.name, false)
		if _, dup := e.Get(name); dup {
			p.failAt("duplicate attribute", at)
		}
		e.Attrs = append(e.Attrs, Attr{name, a.value})
	}
	// TreeBuilder.start.
	p.flush()
	if n := len(p.stack); n > 0 {
		parent := p.stack[n-1]
		parent.Children = append(parent.Children, e)
	} else {
		p.root = e
	}
	p.stack = append(p.stack, e)
	p.names = append(p.names, raw)
	p.scopes = append(p.scopes, scope)
	p.last, p.tail = e, false
	return e
}

// end is TreeBuilder.end.
func (p *xmlParser) end(e *Element) {
	p.flush()
	p.stack = p.stack[:len(p.stack)-1]
	p.names = p.names[:len(p.names)-1]
	p.scopes = p.scopes[:len(p.scopes)-1]
	p.last, p.tail = e, true
}

// flush is TreeBuilder._flush: character data since the last start or end
// tag becomes the last element's text or tail. Comments and processing
// instructions don't flush, so the text on either side of one joins up.
func (p *xmlParser) flush() {
	if len(p.data) == 0 {
		return
	}
	text := strings.Join(p.data, "")
	p.data = nil
	if p.last == nil {
		return
	}
	if p.tail {
		p.last.Tail = text
	} else {
		p.last.Text = text
	}
}

// ---------------------------------------------------------------------------
// Serialization: ET.indent(root, space='    ') and ET.tostring(root,
// encoding='unicode', short_empty_elements=True).
// ---------------------------------------------------------------------------

// indent is ET.indent with level 0: it rewrites whitespace-only text and
// tails so each child sits on its own line, indented by space per level.
func indent(root *Element, space string) {
	if len(root.Children) == 0 {
		return
	}
	indentations := []string{"\n"}
	var walk func(e *Element, level int)
	walk = func(e *Element, level int) {
		childLevel := level + 1
		if childLevel >= len(indentations) {
			indentations = append(indentations, indentations[level]+space)
		}
		childIndent := indentations[childLevel]
		if e.Text == "" || pyStrip(e.Text) == "" {
			e.Text = childIndent
		}
		var child *Element
		for _, child = range e.Children {
			if len(child.Children) > 0 {
				walk(child, childLevel)
			}
			if child.Tail == "" || pyStrip(child.Tail) == "" {
				child.Tail = childIndent
			}
		}
		if pyStrip(child.Tail) == "" {
			child.Tail = indentations[level]
		}
	}
	walk(root, 0)
}

// wellKnownPrefixes is ElementTree's _namespace_map.
var wellKnownPrefixes = map[string]string{
	"http://www.w3.org/XML/1998/namespace":        "xml",
	"http://www.w3.org/1999/xhtml":                "html",
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#": "rdf",
	"http://schemas.xmlsoap.org/wsdl/":            "wsdl",
	"http://www.w3.org/2001/XMLSchema":            "xs",
	"http://www.w3.org/2001/XMLSchema-instance":   "xsi",
	"http://purl.org/dc/elements/1.1/":            "dc",
}

// qnames is ElementTree's _namespaces: the serialized name of every tag and
// attribute name in the tree, and the prefixes it assigns, numbered in
// document order.
func qnames(root *Element) (map[string]string, map[string]string, error) {
	names := map[string]string{}
	prefixes := map[string]string{} // uri → prefix
	var err error
	add := func(q string) {
		if _, ok := names[q]; ok || err != nil {
			return
		}
		if !strings.HasPrefix(q, "{") {
			names[q] = q
			return
		}
		i := strings.LastIndexByte(q, '}')
		if i < 0 {
			err = fmt.Errorf("profile: cannot serialize %q (ValueError: not enough values to unpack)", q)
			return
		}
		uri, local := q[1:i], q[i+1:]
		prefix, ok := prefixes[uri]
		if !ok {
			prefix, ok = wellKnownPrefixes[uri]
			if !ok {
				prefix = "ns" + strconv.Itoa(len(prefixes))
			}
			if prefix != "xml" {
				prefixes[uri] = prefix
			}
		}
		if prefix != "" {
			names[q] = prefix + ":" + local
		} else {
			names[q] = local
		}
	}
	root.iter(func(e *Element) {
		add(e.Tag)
		for _, a := range e.Attrs {
			add(a.Name)
		}
	})
	return names, prefixes, err
}

// escapeCDATA is ElementTree's _escape_cdata.
func escapeCDATA(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// escapeAttrib is ElementTree's _escape_attrib (Python 3.12).
func escapeAttrib(s string) string {
	if !strings.ContainsAny(s, "&<>\"\r\n\t") {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "\r", "&#13;")
	s = strings.ReplaceAll(s, "\n", "&#10;")
	return strings.ReplaceAll(s, "\t", "&#09;")
}

// serialize is ET.tostring(root, encoding='unicode',
// short_empty_elements=True): no declaration, namespace declarations on
// the root sorted by prefix, attributes in order, " />" for an element
// with neither text nor children.
func serialize(root *Element) (string, error) {
	names, prefixes, err := qnames(root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var write func(e *Element, ns map[string]string)
	write = func(e *Element, ns map[string]string) {
		tag := names[e.Tag]
		b.WriteString("<" + tag)
		if len(ns) > 0 {
			type decl struct{ uri, prefix string }
			var decls []decl
			for uri, prefix := range ns {
				decls = append(decls, decl{uri, prefix})
			}
			sort.Slice(decls, func(i, j int) bool { return decls[i].prefix < decls[j].prefix })
			for _, d := range decls {
				k := ""
				if d.prefix != "" {
					k = ":" + d.prefix
				}
				b.WriteString(" xmlns" + k + `="` + escapeAttrib(d.uri) + `"`)
			}
		}
		for _, a := range e.Attrs {
			b.WriteString(" " + names[a.Name] + `="` + escapeAttrib(a.Value) + `"`)
		}
		if e.Text != "" || len(e.Children) > 0 {
			b.WriteString(">")
			b.WriteString(escapeCDATA(e.Text))
			for _, c := range e.Children {
				write(c, nil)
			}
			b.WriteString("</" + tag + ">")
		} else {
			b.WriteString(" />")
		}
		b.WriteString(escapeCDATA(e.Tail))
	}
	write(root, prefixes)
	return b.String(), nil
}

// compactEmptyTags is re.sub(r'\s+/>', '/>', body): Python's \s is any
// character str.isspace() accepts.
func compactEmptyTags(s string) string {
	if !strings.Contains(s, "/>") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "/>")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := i
		for j > 0 {
			r, n := utf8.DecodeLastRuneInString(s[:j])
			if !pyIsSpace(r) {
				break
			}
			j -= n
		}
		b.WriteString(s[:j])
		b.WriteString("/>")
		s = s[i+2:]
	}
}
