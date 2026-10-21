// SPDX-License-Identifier: MIT

// Package pytext is what bmdwire's text handling does in Python, ported
// for the MIT device packages (hyperdeck, videohub, ultimatte): decoding
// with decode('utf-8', 'replace'), str.strip(), int(), repr(), and the
// rest the three libraries' parsing depends on, each matching CPython 3.12
// (docs/PORTING.md, "HyperDeck"). atem keeps its own, under its licence.
package pytext

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsSpace is Python's str.isspace() for one character: Go's
// unicode.IsSpace and the four information separators U+001C-U+001F.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f
}

// Strip is Python's str.strip() with no argument.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// Decode is Python's bytes.decode('utf-8', 'replace'): one U+FFFD for
// each maximal subpart of an ill-formed sequence (a lead byte and the
// continuation bytes that could still have completed it), where Go's own
// helpers give one per byte or one per run.
func Decode(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		n, lo, hi := 0, byte(0x80), byte(0xbf)
		switch {
		case c >= 0xc2 && c <= 0xdf:
			n = 2
		case c == 0xe0:
			n, lo = 3, 0xa0
		case c >= 0xe1 && c <= 0xec, c == 0xee, c == 0xef:
			n = 3
		case c == 0xed:
			n, hi = 3, 0x9f
		case c == 0xf0:
			n, lo = 4, 0x90
		case c >= 0xf1 && c <= 0xf3:
			n = 4
		case c == 0xf4:
			n, hi = 4, 0x8f
		}
		if n == 0 { // a byte no sequence starts with
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		for k := 1; k < n && j < len(b); k++ {
			first, last := byte(0x80), byte(0xbf)
			if k == 1 {
				first, last = lo, hi
			}
			if b[j] < first || b[j] > last {
				break
			}
			j++
		}
		if j-i == n {
			out.Write(b[i:j])
		} else {
			out.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return out.String()
}

// Int is Python's int() of a str in base 10: surrounding whitespace,
// an optional sign, decimal digits of any script with single underscores
// between them. It errors with Python's ValueError message where Python
// raises, and also for a value beyond int64, where Python would carry on
// with a big integer.
func Int(s string) (int64, error) {
	bad := fmt.Errorf("invalid literal for int() with base 10: %s", Repr(s))
	t := Strip(s)
	neg := false
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg, t = t[0] == '-', t[1:]
	}
	if t == "" {
		return 0, bad
	}
	var n int64
	prevDigit := false
	for _, r := range t {
		if r == '_' {
			if !prevDigit {
				return 0, bad
			}
			prevDigit = false
			continue
		}
		d, ok := DecimalValue(r)
		if !ok {
			return 0, bad
		}
		if n > (1<<63-1-int64(d))/10 {
			return 0, fmt.Errorf("%s does not fit in 64 bits", Repr(s))
		}
		n, prevDigit = n*10+int64(d), true
	}
	if !prevDigit { // a trailing underscore
		return 0, bad
	}
	if neg {
		n = -n
	}
	return n, nil
}

// DecimalValue is a decimal digit's value in any script. Unicode encodes
// every set of decimal digits (category Nd) as a run of ten, zero first,
// so the value is the distance from the start of its run of digits,
// modulo ten.
func DecimalValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	if !unicode.IsDigit(r) {
		return 0, false
	}
	start := r
	for unicode.IsDigit(start - 1) {
		start--
	}
	return int(r-start) % 10, true
}

// IsDecimal reports whether s is non-empty and made only of decimal
// digits. hyperdeckwire's clip-line filter calls str.isdigit(), which
// also counts digits that aren't decimal (superscripts, circled digits);
// a head made of those passes its filter and then fails int(), so the
// line is skipped either way.
func IsDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if _, ok := DecimalValue(r); !ok {
			return false
		}
	}
	return true
}

// Repr is Python's repr() of a str: single quotes unless the string
// holds one and no double quote, backslash escapes for the quote, the
// backslash, tab, newline and carriage return, and \x, \u or \U escapes
// for anything else unprintable.
func Repr(s string) string {
	q := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == rune(q) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(q)
	return b.String()
}
