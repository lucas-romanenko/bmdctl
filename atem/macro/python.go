// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// The Python behaviour the codec inherits from the builtins atemwire's
// macrotransfer calls: int() and float() on attribute strings, round(),
// str.strip(), repr(), bytes([...]), bytes.fromhex(), struct.pack and
// unpack_from, socket.inet_aton and inet_ntoa. Each is ported from
// CPython 3.12 (the interpreter internal/fixtures/oracle/macro.json was
// recorded with), down to its exception class and message, because
// bmdwire v1.3.0's codec passes those through: an attribute string that
// int() accepts is accepted here, and a decodeError attribute holds
// CPython's message.

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

// pyExc is a Python exception raised inside a decoder or an encoder. It is
// panicked and recovered at the package boundary (Decode, EncodeOp), so the
// ported code raises at the same points, in the same order, as atemwire's.
type pyExc struct {
	class string // "ValueError", "KeyError", "IndexError", "OSError", "OverflowError", "struct.error"
	msg   string
}

func raise(class, format string, args ...any) {
	panic(&pyExc{class, fmt.Sprintf(format, args...)})
}

// catch runs fn and returns the pyExc it raised, if any. Other panics
// propagate.
func catch(fn func()) (exc *pyExc) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*pyExc)
			if !ok {
				panic(r)
			}
			exc = e
		}
	}()
	fn()
	return nil
}

// pyIsSpace is str.isspace() for one character: Unicode White_Space plus
// the four ASCII separators U+001C-U+001F, which CPython also counts.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// cIsSpace is C's isspace() in the C locale, CPython's Py_ISSPACE.
func cIsSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// pyStrip is str.strip() with no argument.
func pyStrip(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

// pyLowerIn reports whether s.lower() is one of words (all ASCII). Go's
// simple case mapping and Python's full one differ only for characters
// (U+0130) whose lower case is no ASCII word, so the answer is the same.
func pyLowerIn(s string, words ...string) bool {
	l := strings.ToLower(s)
	for _, w := range words {
		if l == w {
			return true
		}
	}
	return false
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

// truncRunes keeps the first n characters of s, as PyUnicode_FromFormat's
// "%.200R" does.
func truncRunes(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// decimalValue is unicodedata.decimal(r): the digit value of a character
// in category Nd. Unicode assigns those in runs of ten, 0 to 9, so the
// value is the offset from the start of the run.
func decimalValue(r rune) int {
	for _, rg := range unicode.Nd.R16 {
		if lo, hi := rune(rg.Lo), rune(rg.Hi); r >= lo && r <= hi && (r-lo)%rune(rg.Stride) == 0 {
			return int((r-lo)/rune(rg.Stride)) % 10
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if lo, hi := rune(rg.Lo), rune(rg.Hi); r >= lo && r <= hi && (r-lo)%rune(rg.Stride) == 0 {
			return int((r-lo)/rune(rg.Stride)) % 10
		}
	}
	return -1
}

// toASCII is CPython's _PyUnicode_TransformDecimalAndSpaceToASCII, which
// int() and float() run first: ASCII stays, other whitespace becomes a
// space, other decimal digits become ASCII digits, and the first character
// that is none of these becomes '?' and ends the string.
func toASCII(s string) string {
	var b []byte
	for _, r := range s {
		switch {
		case r < 127:
			b = append(b, byte(r))
		case pyIsSpace(r):
			b = append(b, ' ')
		default:
			d := decimalValue(r)
			if d < 0 {
				return string(append(b, '?'))
			}
			b = append(b, byte('0'+d))
		}
	}
	return string(b)
}

// digitValue is CPython's _PyLong_DigitValue: 0-9, a-z and A-Z as 0-35,
// anything else 37.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 37
}

// maxStrDigits is sys.get_int_max_str_digits(), CPython's default.
const maxStrDigits = 4300

// parseInt is int(s, base) for base 10 or 16, returning the exception
// instead of raising it.
func parseInt(s string, base int) (*big.Int, *pyExc) {
	invalid := &pyExc{"ValueError", fmt.Sprintf("invalid literal for int() with base %d: %s",
		base, truncRunes(pyRepr(s), 200))}
	t := toASCII(s)
	// PyLong_FromString reads a C string: it stops at a NUL, and the
	// caller then sees that it didn't consume everything.
	cut := false
	if i := strings.IndexByte(t, 0); i >= 0 {
		t, cut = t[:i], true
	}
	i := 0
	for i < len(t) && cIsSpace(t[i]) {
		i++
	}
	neg := false
	if i < len(t) && (t[i] == '+' || t[i] == '-') {
		neg = t[i] == '-'
		i++
	}
	if base == 16 && i+1 < len(t) && t[i] == '0' && (t[i+1] == 'x' || t[i+1] == 'X') {
		i += 2
		if i < len(t) && t[i] == '_' {
			i++
		}
	}
	if i < len(t) && t[i] == '_' {
		return nil, invalid
	}
	var digits []byte
	var prev byte
	for i < len(t) && (digitValue(t[i]) < base || t[i] == '_') {
		if t[i] == '_' {
			if prev == '_' {
				return nil, invalid
			}
		} else {
			digits = append(digits, t[i])
		}
		prev = t[i]
		i++
	}
	if prev == '_' || len(digits) == 0 {
		return nil, invalid
	}
	for i < len(t) && cIsSpace(t[i]) {
		i++
	}
	if i != len(t) {
		return nil, invalid
	}
	if base == 10 && len(digits) > maxStrDigits {
		return nil, &pyExc{"ValueError", fmt.Sprintf("Exceeds the limit (%d digits) for integer string "+
			"conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit",
			maxStrDigits, len(digits))}
	}
	if cut {
		return nil, invalid
	}
	v, _ := new(big.Int).SetString(string(digits), base)
	if neg {
		v.Neg(v)
	}
	return v, nil
}

// pyInt is int(s): it raises ValueError for a string int() rejects.
func pyInt(s string) *big.Int {
	v, exc := parseInt(s, 10)
	if exc != nil {
		panic(exc)
	}
	return v
}

// parseFloat is float(s), returning the exception instead of raising it.
func parseFloat(s string) (float64, *pyExc) {
	bad := &pyExc{"ValueError", "could not convert string to float: " + pyRepr(s)}
	t := toASCII(s)
	if strings.IndexByte(t, 0) >= 0 {
		return 0, bad
	}
	// _Py_string_to_number_with_underscores: an underscore must sit
	// between two digits; then it is dropped.
	if strings.IndexByte(t, '_') >= 0 {
		var b []byte
		var prev byte
		for i := 0; i < len(t); i++ {
			c := t[i]
			if c == '_' {
				if !isDigit(prev) {
					return 0, bad
				}
			} else {
				b = append(b, c)
				if prev == '_' && !isDigit(c) {
					return 0, bad
				}
			}
			prev = c
		}
		if prev == '_' {
			return 0, bad
		}
		t = string(b)
	}
	t = strings.TrimFunc(t, func(r rune) bool { return r < 128 && cIsSpace(byte(r)) })
	if t == "" {
		return 0, bad
	}
	body := t
	sign := 1.0
	if body[0] == '+' || body[0] == '-' {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		return math.Inf(int(sign)), nil
	case "nan":
		return math.NaN(), nil
	}
	// _Py_dg_strtod's grammar: digits, an optional point and digits (at
	// least one digit in all), an optional exponent with at least one digit.
	i, n := 0, 0
	for i < len(body) && isDigit(body[i]) {
		i, n = i+1, n+1
	}
	if i < len(body) && body[i] == '.' {
		i++
		for i < len(body) && isDigit(body[i]) {
			i, n = i+1, n+1
		}
	}
	if n == 0 {
		return 0, bad
	}
	if i < len(body) && (body[i] == 'e' || body[i] == 'E') {
		j := i + 1
		if j < len(body) && (body[j] == '+' || body[j] == '-') {
			j++
		}
		k := j
		for k < len(body) && isDigit(body[k]) {
			k++
		}
		if k > j {
			i = k
		}
	}
	if i != len(body) {
		return 0, bad
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil && !isRangeErr(err) {
		return 0, bad
	}
	return f, nil
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// pyFloat is float(s).
func pyFloat(s string) float64 {
	f, exc := parseFloat(s)
	if exc != nil {
		panic(exc)
	}
	return f
}

// pyRound is round(f) for a float: half to even, an exact integer of any
// size, and the exceptions int() raises for NaN and the infinities.
func pyRound(f float64) *big.Int {
	switch {
	case math.IsNaN(f):
		raise("ValueError", "cannot convert float NaN to integer")
	case math.IsInf(f, 0):
		raise("OverflowError", "cannot convert float infinity to integer")
	}
	v, _ := new(big.Float).SetFloat64(math.RoundToEven(f)).Int(nil)
	return v
}

// mask is v & (2**bits - 1), Python's & on an int of any sign and size.
func mask(v *big.Int, bits uint) uint64 {
	m := new(big.Int).Lsh(big.NewInt(1), bits)
	m.Sub(m, big.NewInt(1))
	return new(big.Int).And(v, m).Uint64()
}

// pyBytes is bytes([...]) over ints.
func pyBytes(vs ...*big.Int) []byte {
	out := make([]byte, len(vs))
	for i, v := range vs {
		if v.Sign() < 0 || !v.IsInt64() || v.Int64() > 255 {
			raise("ValueError", "bytes must be in range(0, 256)")
		}
		out[i] = byte(v.Int64())
	}
	return out
}

// num is a small Python int.
func num(v int64) *big.Int { return big.NewInt(v) }

// packH is struct.pack('<H', v).
func packH(v *big.Int) []byte {
	if v.Sign() < 0 || !v.IsInt64() || v.Int64() > 0xFFFF {
		raise("struct.error", "'H' format requires 0 <= number <= 65535")
	}
	return le(uint64(v.Int64()), 2)
}

// packi is struct.pack('<i', v).
func packi(v *big.Int) []byte {
	if !v.IsInt64() || v.Int64() < math.MinInt32 || v.Int64() > math.MaxInt32 {
		raise("struct.error", "'i' format requires -2147483648 <= number <= 2147483647")
	}
	return le(uint64(uint32(int32(v.Int64()))), 4)
}

// le writes the low n bytes of v, little-endian.
func le(v uint64, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(v >> (8 * i))
	}
	return b
}

// params is an op's parameter block, read the way atemwire reads it:
// indexing raises IndexError and struct.unpack_from raises struct.error
// past the end.
type params []byte

func (p params) at(i int) int {
	if i >= len(p) {
		raise("IndexError", "index out of range")
	}
	return int(p[i])
}

// str is str(params[i]).
func (p params) str(i int) string { return strconv.Itoa(p.at(i)) }

func (p params) unpack(off, size int) uint64 {
	if len(p)-off < size {
		raise("struct.error", "unpack_from requires a buffer of at least %d bytes for unpacking %d bytes "+
			"at offset %d (actual buffer size is %d)", off+size, size, off, len(p))
	}
	var v uint64
	for i := size - 1; i >= 0; i-- {
		v = v<<8 | uint64(p[off+i])
	}
	return v
}

func (p params) u16(off int) uint16 { return uint16(p.unpack(off, 2)) }
func (p params) u32(off int) uint32 { return uint32(p.unpack(off, 4)) }
func (p params) i32(off int) int32  { return int32(uint32(p.unpack(off, 4))) }
func (p params) u64(off int) uint64 { return p.unpack(off, 8) }

// fromHex is bytes.fromhex(s): pairs of hex digits, ASCII whitespace
// allowed between pairs; the error names the first bad character's
// position.
func fromHex(s string) ([]byte, *pyExc) {
	bad := func(pos int) *pyExc {
		return &pyExc{"ValueError", fmt.Sprintf("non-hexadecimal number found in fromhex() arg at position %d", pos)}
	}
	// A non-ASCII string fails at its first non-ASCII character.
	pos := 0
	for _, r := range s {
		if r >= 128 {
			return nil, bad(pos)
		}
		pos++
	}
	var out []byte
	i := 0
	for i < len(s) {
		if cIsSpace(s[i]) {
			for i < len(s) && cIsSpace(s[i]) {
				i++
			}
			if i >= len(s) {
				break
			}
		}
		top := digitValue(s[i])
		if top >= 16 {
			return nil, bad(i)
		}
		i++
		bot := 37
		if i < len(s) {
			bot = digitValue(s[i])
		}
		if bot >= 16 {
			return nil, bad(i)
		}
		i++
		out = append(out, byte(top<<4|bot))
	}
	return out, nil
}

// inetAton is socket.inet_aton(s): glibc's inet_aton, which takes one to
// four parts (a, a.b, a.b.c, a.b.c.d; the last part fills the remaining
// bytes), each in C notation (0x hex, leading-0 octal, decimal), and
// ignores anything after a whitespace character that follows the address.
func inetAton(s string) [4]byte {
	if strings.IndexByte(s, 0) >= 0 {
		raise("ValueError", "embedded null character")
	}
	illegal := func() { raise("OSError", "illegal IP address string passed to inet_aton") }
	var parts []uint64
	var val uint64
	i := 0
	for {
		if i >= len(s) || !isDigit(s[i]) {
			illegal()
		}
		val, i = strtoul(s, i)
		if val > 0xffffffff {
			illegal()
		}
		if i < len(s) && s[i] == '.' {
			if len(parts) > 2 || val > 0xff {
				illegal()
			}
			parts = append(parts, val)
			i++
			continue
		}
		break
	}
	if i < len(s) && !cIsSpace(s[i]) {
		illegal()
	}
	if val > [4]uint64{0xffffffff, 0xffffff, 0xffff, 0xff}[len(parts)] {
		illegal()
	}
	var out [4]byte
	for j, p := range parts {
		out[j] = byte(p)
	}
	for j := 0; j < 4; j++ {
		out[j] |= byte(val >> (8 * (3 - j)))
	}
	return out
}

// strtoul is C's strtoul(s+i, &end, 0) on a string that starts with a
// digit; values past 64 bits saturate (inet_aton rejects them anyway).
func strtoul(s string, i int) (uint64, int) {
	base := uint64(10)
	if s[i] == '0' {
		base = 8
		if i+2 < len(s) && (s[i+1] == 'x' || s[i+1] == 'X') && digitValue(s[i+2]) < 16 {
			base, i = 16, i+2
		}
	}
	var v uint64
	for i < len(s) && uint64(digitValue(s[i])) < base {
		d := uint64(digitValue(s[i]))
		if v > (math.MaxUint64-d)/base {
			v = math.MaxUint64
		} else {
			v = v*base + d
		}
		i++
	}
	return v, i
}

// inetNtoa is socket.inet_ntoa(b).
func inetNtoa(b []byte) string {
	if len(b) != 4 {
		raise("OSError", "packed IP wrong length for inet_ntoa")
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}
