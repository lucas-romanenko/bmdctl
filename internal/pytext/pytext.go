// SPDX-License-Identifier: MIT

// Package pytext is what bmdwire's text handling does in Python, ported
// for the MIT device packages (hyperdeck, videohub, ultimatte): decoding
// with decode('utf-8', 'replace'), str.strip(), int(), repr(), and the
// rest the three libraries' parsing depends on, each matching CPython 3.12
// (docs/PORTING.md, "HyperDeck"). atem keeps its own, under its licence.
package pytext

import (
	"errors"
	"fmt"
	"math"
	"strconv"
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

// Int is Python's int() of a str in base 10: surrounding whitespace
// (numberStrip's), an optional sign, decimal digits of any script with single underscores
// between them. It errors with Python's ValueError message where Python
// raises, and also for a value beyond int64, where Python would carry on
// with a big integer.
func Int(s string) (int64, error) {
	bad := fmt.Errorf("invalid literal for int() with base 10: %s", Repr(s))
	t := numberStrip(s)
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

// numberStrip is the strip int() and float() do, which isn't
// str.strip()'s: CPython turns non-ASCII whitespace into spaces and then
// strips ASCII whitespace only, so U+001C-U+001F, which str.strip()
// removes, stay and make the number invalid.
func numberStrip(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return IsSpace(r) && (r < 0x1c || r > 0x1f) })
}

// Fields is Python's str.split() with no argument: the runs of characters
// between runs of whitespace (IsSpace), none of them empty.
func Fields(s string) []string { return strings.FieldsFunc(s, IsSpace) }

// SplitLines is Python's str.splitlines(): the lines of s without their
// ends, where a line ends at \n, \r, \r\n, \v, \f, U+001C-U+001E, U+0085,
// U+2028 or U+2029. An end at the very end starts no further line, so ""
// has none.
func SplitLines(s string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, s[start:i])
			i += n
			if r == '\r' && i < len(s) && s[i] == '\n' {
				i++
			}
			start = i
		default:
			i += n
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// Lower is Python's str.lower(): each character's lower case, with
// Python's two full mappings, U+0130 to "i̇" (i and U+0307) and a capital
// sigma at the end of a word to ς (Final_Sigma), where Go's ToLower maps
// one character to one.
func Lower(s string) string {
	if !strings.ContainsAny(s, "İΣ") {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch r {
		case 0x130:
			b.WriteString("i̇")
		case 0x3a3:
			if finalSigma(rs, i) {
				b.WriteRune(0x3c2)
			} else {
				b.WriteRune(0x3c3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// finalSigma is CPython's Final_Sigma test for the capital sigma at i: a
// cased letter before it and none after it, case-ignorable characters
// skipped both ways.
func finalSigma(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && caseIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !cased(rs[j]) {
		return false
	}
	k := i + 1
	for k < len(rs) && caseIgnorable(rs[k]) {
		k++
	}
	return k == len(rs) || !cased(rs[k])
}

// cased is Unicode's Cased property: upper, lower or title case letters,
// and Other_Lowercase and Other_Uppercase.
func cased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

// caseIgnorable is Unicode's Case_Ignorable property: marks, format
// characters, modifier letters and symbols, and the apostrophes and
// stops Word_Break counts as MidLetter, MidNumLet or Single_Quote.
func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', 0xb7, 0x387, 0x55f, 0x5f4, 0x2018, 0x2019, 0x2024, 0x2027, 0xfe13, 0xfe52, 0xfe55, 0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// IsDigit is Python's str.isdigit(): non-empty and every character a
// digit, which counts decimal digits (IsDecimal) and the digits that
// aren't decimal (superscripts, subscripts, circled and parenthesized
// digits); int() refuses the second kind.
func IsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if _, ok := DecimalValue(r); !ok && !otherDigit(r) {
			return false
		}
	}
	return true
}

// otherDigit is a character with Unicode's Numeric_Type Digit: a digit
// that is not a decimal one.
func otherDigit(r rune) bool {
	for _, rg := range otherDigits {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

// otherDigits are Unicode 15.0's Numeric_Type=Digit ranges, as CPython
// 3.12's unicodedata has them (testdata/oracle.json checks them).
var otherDigits = [][2]rune{
	{0xb2, 0xb3}, {0xb9, 0xb9}, {0x1369, 0x1371}, {0x19da, 0x19da}, {0x2070, 0x2070}, {0x2074, 0x2079},
	{0x2080, 0x2089}, {0x2460, 0x2468}, {0x2474, 0x247c}, {0x2488, 0x2490}, {0x24ea, 0x24ea},
	{0x24f5, 0x24fd}, {0x24ff, 0x24ff}, {0x2776, 0x277e}, {0x2780, 0x2788}, {0x278a, 0x2792},
	{0x10a40, 0x10a43}, {0x10e60, 0x10e68}, {0x11052, 0x1105a}, {0x1f100, 0x1f10a},
}

// Float is Python's float() of a str: surrounding whitespace
// (numberStrip's); a sign;
// inf, infinity or nan in any case; or decimal digits of any script, with
// single underscores between them, a point and an exponent. It errors
// with Python's ValueError message where float() raises.
func Float(s string) (float64, error) {
	bad := fmt.Errorf("could not convert string to float: %s", Repr(s))
	t := numberStrip(s)
	var b strings.Builder
	for _, r := range t {
		if d, ok := DecimalValue(r); ok {
			b.WriteByte(byte('0' + d))
		} else if r < utf8.RuneSelf {
			b.WriteByte(byte(r))
		} else {
			return 0, bad
		}
	}
	a := b.String()
	body := strings.TrimLeft(a, "+-")
	if len(a)-len(body) > 1 {
		return 0, bad
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if strings.HasPrefix(a, "-") {
			return math.Inf(-1), nil
		}
		return math.Inf(1), nil
	case "nan":
		return math.NaN(), nil
	}
	if !floatLiteral(body) {
		return 0, bad
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(a, "_", ""), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, bad
	}
	return f, nil
}

// floatLiteral reports whether s (ASCII, unsigned) is Python's float
// syntax: digits with single underscores between them, an optional point
// with digits on at least one side, an optional exponent with digits.
func floatLiteral(s string) bool {
	digits := func(t string) bool {
		if t == "" || t[0] == '_' || t[len(t)-1] == '_' || strings.Contains(t, "__") {
			return false
		}
		for i := 0; i < len(t); i++ {
			if (t[i] < '0' || t[i] > '9') && t[i] != '_' {
				return false
			}
		}
		return true
	}
	mant, exp, hasExp := strings.Cut(strings.ToLower(s), "e")
	if hasExp {
		if !digits(strings.TrimLeft(exp, "+-")) || len(exp)-len(strings.TrimLeft(exp, "+-")) > 1 {
			return false
		}
	}
	whole, frac, hasPoint := strings.Cut(mant, ".")
	if !hasPoint {
		return digits(whole)
	}
	return (whole == "" || digits(whole)) && (frac == "" || digits(frac)) && (whole != "" || frac != "")
}
