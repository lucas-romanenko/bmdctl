// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The Python semantics atemwire/profile relies on: str.strip(), float() and
// int() of a string, repr() of a str, bytes.decode('utf-8', 'replace'),
// and the attribute helpers of _xml.py (_fmt, _bool, _int, _float) built
// on them (bmdwire v1.3.0).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a Python exception atemwire raises or records, by class and
// message (str(exc), CPython 3.12's wording). Apply records the ones its
// operations raise in ApplyResult.Errors as "section: Class: message", and
// returns the ones that escape atemwire's apply (an OverflowError from
// _int, a non-ValueError from the macro encoder) as its error.
type Error struct {
	Class   string
	Message string
}

func (e *Error) Error() string { return "profile: " + e.Class + ": " + e.Message }

// The two messages float-to-int conversion raises with.
var (
	errNaN = &Error{"ValueError", "cannot convert float NaN to integer"}
	errInf = &Error{"OverflowError", "cannot convert float infinity to integer"}
)

// toInt is the check Python's int() or round() makes converting a float:
// NaN raises ValueError, an infinity OverflowError.
func toInt(v float64) error {
	switch {
	case math.IsNaN(v):
		return errNaN
	case math.IsInf(v, 0):
		return errInf
	}
	return nil
}

// pyIsSpace is str.isspace() for one character: Unicode's White_Space
// property (Go's unicode.IsSpace) plus the four information separators
// U+001C-U+001F, which Python counts and Go doesn't.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f
}

// pyStrip is str.strip() with no argument.
func pyStrip(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

// pyDecimal is the decimal value of r, or -1: Python's
// Py_UNICODE_TODECIMAL, for the Nd characters float() and int() accept.
// Every Nd range is made of whole runs of ten digits, zero first.
func pyDecimal(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	if r < 0x80 || !unicode.Is(unicode.Nd, r) {
		return -1
	}
	for _, rg := range unicode.Nd.R16 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10
		}
	}
	return -1
}

// pyNumberASCII is _PyUnicode_TransformDecimalAndSpaceToASCII followed by
// the strip float() and int() do: characters below 127 stay as they are,
// other whitespace becomes ' ', other decimal digits become ASCII, and
// anything else makes the string invalid; then the ASCII whitespace C's
// isspace() knows is stripped (not U+001C-U+001F, which str.strip()
// would take).
func pyNumberASCII(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 127:
			b.WriteRune(r)
		case pyIsSpace(r):
			b.WriteByte(' ')
		default:
			d := pyDecimal(r)
			if d < 0 {
				return "", false
			}
			b.WriteByte(byte('0' + d))
		}
	}
	return strings.Trim(b.String(), " \t\n\v\f\r"), true
}

// pyUnderscores removes the underscores of a numeric literal, each of which
// must sit between two digits (_Py_string_to_number_with_underscores).
func pyUnderscores(s string) (string, bool) {
	if !strings.Contains(s, "_") {
		return s, true
	}
	var b strings.Builder
	prev := byte(0)
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			if !digit(prev) {
				return "", false
			}
		} else {
			b.WriteByte(c)
			if prev == '_' && !digit(c) {
				return "", false
			}
		}
		prev = c
	}
	if prev == '_' {
		return "", false
	}
	return b.String(), true
}

// pyFloat is Python's float() of a str: surrounding whitespace, Unicode
// digits and single underscores between digits allowed, "inf", "infinity"
// and "nan" in any case with an optional sign, no hexadecimal.
func pyFloat(s string) (float64, bool) {
	t, ok := pyNumberASCII(s)
	if !ok {
		return 0, false
	}
	if t, ok = pyUnderscores(t); !ok {
		return 0, false
	}
	body := t
	if body != "" && (body[0] == '+' || body[0] == '-') {
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if t[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case "nan":
		return math.NaN(), true
	}
	// [digits] [. [digits]] with at least one digit, then [eE [sign] digits].
	i, n := 0, len(body)
	digits := func() int {
		start := i
		for i < n && body[i] >= '0' && body[i] <= '9' {
			i++
		}
		return i - start
	}
	m := digits()
	if i < n && body[i] == '.' {
		i++
		m += digits()
	}
	if m == 0 {
		return 0, false
	}
	if i < n && (body[i] == 'e' || body[i] == 'E') {
		i++
		if i < n && (body[i] == '+' || body[i] == '-') {
			i++
		}
		if digits() == 0 {
			return 0, false
		}
	}
	if i != n {
		return 0, false
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil && !math.IsInf(v, 0) && v != 0 {
		return 0, false
	}
	return v, true
}

// pyIntDigitLimit is CPython's default int max str digits.
const pyIntDigitLimit = 4300

// pyIntDigits is what Python's int() of a str in base 10 accepts:
// surrounding whitespace, a sign, Unicode digits and single underscores
// between digits. It returns the sign, the ASCII digits and whether the
// string was a valid int, which int() refuses past 4300 digits.
func pyIntDigits(s string) (neg bool, digits string, ok bool) {
	t, ok := pyNumberASCII(s)
	if !ok {
		return false, "", false
	}
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg = t[0] == '-'
		t = t[1:]
	}
	if t == "" || t[0] == '_' {
		return false, "", false
	}
	if t, ok = pyUnderscores(t); !ok {
		return false, "", false
	}
	if t == "" || strings.IndexFunc(t, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return false, "", false
	}
	if len(t) > pyIntDigitLimit {
		return false, "", false
	}
	return neg, t, true
}

// pyInt is Python's int() of a str in base 10 (pyIntDigits). It returns
// the value as a float64 (exact for anything a wire field can hold) and
// whether the string was a valid int.
func pyInt(s string) (float64, bool) {
	neg, t, ok := pyIntDigits(s)
	if !ok {
		return 0, false
	}
	v, _ := strconv.ParseFloat(t, 64)
	if neg {
		v = -v
	}
	return v + 0, true // + 0 turns -0 into 0, as Python's int has no -0
}

// pyIntMod is int(s) % m, exact for any int() accepts: the low bits of
// int(s) & mask when m is a power of two, as Python's two's-complement &
// gives them for a negative int.
func pyIntMod(s string, m uint64) (uint64, bool) {
	neg, t, ok := pyIntDigits(s)
	if !ok {
		return 0, false
	}
	var r uint64
	for i := 0; i < len(t); i++ {
		r = (r*10 + uint64(t[i]-'0')) % m
	}
	if neg && r != 0 {
		r = m - r
	}
	return r, true
}

// attrFloat is _float(s, default): an absent or empty attribute, or one
// float() rejects, gives default.
func attrFloat(s *string, def float64) float64 {
	if s == nil || *s == "" {
		return def
	}
	if v, ok := pyFloat(*s); ok {
		return v
	}
	return def
}

// attrInt is _int(s, default): int(float(s)), with default for an absent
// or empty attribute, one float() rejects and NaN. An infinity raises
// OverflowError, which _int doesn't catch: callers return it, ending the
// apply as the exception ends atemwire's. The value is a float64 holding
// an integer, so the huge ints Python makes stay exact.
func attrInt(s *string, def float64) (float64, error) {
	if s == nil || *s == "" {
		return def, nil
	}
	v, ok := pyFloat(*s)
	if !ok || math.IsNaN(v) {
		return def, nil
	}
	if math.IsInf(v, 0) {
		return 0, errInf
	}
	return math.Trunc(v) + 0, nil
}

// attrBool is _bool(s, default): str(s).strip().lower() is one of true, 1,
// yes, on.
func attrBool(s *string, def bool) bool {
	if s == nil {
		return def
	}
	switch strings.ToLower(pyStrip(*s)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// intString is str() of an int held in a float64.
func intString(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// fitsInt is whether the int v lies in [lo, hi].
func fitsInt(v float64, lo, hi int64) bool { return v >= float64(lo) && v <= float64(hi) }

// fmtFloat is _fmt of a float: nan, inf, -inf, "0" for zero, else up to six
// decimals with trailing zeros (and a trailing point) stripped.
func fmtFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "nan"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	case v == 0:
		return "0"
	}
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 6, 64), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

// fmtValue is _fmt: True/False for a bool, decimal for an int, fmtFloat for
// a float, a string as it is.
func fmtValue(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case float64:
		return fmtFloat(x)
	case string:
		return x
	}
	panic(fmt.Sprintf("profile: fmtValue of %T", v))
}

// setAttrs is _set_attrs(elem, **attrs): each name, value pair formatted
// with _fmt, in order.
func setAttrs(e *Element, kv ...any) {
	for i := 0; i < len(kv); i += 2 {
		e.Set(kv[i].(string), fmtValue(kv[i+1]))
	}
}

// pyRepr is repr() of a str.
func pyRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

// decodeName is decode_name (state.py) of a name the state keeps as bytes:
// cut at the first NUL, decoded as UTF-8 with errors='replace' (one U+FFFD
// per maximal invalid subpart, as CPython does) and stripped.
func decodeName(raw string) string {
	if i := strings.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	return pyStrip(decodeReplace(raw))
}

// decodeReplace is bytes.decode('utf-8', 'replace').
func decodeReplace(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || n > 1 {
			b.WriteRune(r)
			i += n
			continue
		}
		// An invalid sequence: skip the maximal prefix of a valid one.
		b.WriteRune(utf8.RuneError)
		i += maximalSubpart(s[i:])
	}
	return b.String()
}

// maximalSubpart is the length of the longest prefix of s that starts a
// well-formed UTF-8 sequence (at least 1): what one U+FFFD replaces.
func maximalSubpart(s string) int {
	c := s[0]
	var need int
	lo, hi := byte(0x80), byte(0xbf)
	switch {
	case c >= 0xc2 && c <= 0xdf:
		need = 1
	case c == 0xe0:
		need, lo = 2, 0xa0
	case c >= 0xe1 && c <= 0xec, c == 0xee, c == 0xef:
		need = 2
	case c == 0xed:
		need, hi = 2, 0x9f
	case c == 0xf0:
		need, lo = 3, 0x90
	case c >= 0xf1 && c <= 0xf3:
		need = 3
	case c == 0xf4:
		need, hi = 3, 0x8f
	default:
		return 1
	}
	n := 1
	for k := 0; k < need && n < len(s); k++ {
		b := s[n]
		if k == 0 {
			if b < lo || b > hi {
				return n
			}
		} else if b < 0x80 || b > 0xbf {
			return n
		}
		n++
	}
	return n
}
