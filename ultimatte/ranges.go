// SPDX-License-Identifier: MIT

package ultimatte

// The display ranges and the annotated prelude of bmdwire v1.6.0's
// ultimattewire/profile/_ranges.py: RANGES, to_display and
// annotate_preamble.

import (
	"math"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// Range is one control's: its raw range, and its display minimum,
// default and maximum (nil for a value shown as it is: frames, pixels)
// and unit.
type Range struct {
	RawMin, RawMax                     float64
	DisplayMin, DisplayDef, DisplayMax *float64
	Unit                               string
}

func f(v float64) *float64 { return &v }

// ToDisplay is to_display: a control's raw value in its display units:
// as it is for one shown so, else mapped linearly from the raw range to
// the display one, unclamped. ok is false for a name with no range or a
// value Python's float() refuses.
func ToDisplay(name, raw string) (display float64, unit string, ok bool) {
	r, found := Ranges[name]
	if !found {
		return 0, "", false
	}
	v, err := pytext.Float(raw)
	if err != nil {
		return 0, "", false
	}
	if r.DisplayMin == nil {
		return v, r.Unit, true
	}
	if r.RawMax == r.RawMin {
		return *r.DisplayMin, r.Unit, true
	}
	pct := (v - r.RawMin) / (r.RawMax - r.RawMin)
	return *r.DisplayMin + pct*(*r.DisplayMax-*r.DisplayMin), r.Unit, true
}

// AnnotatePreamble is annotate_preamble: the prelude's lines (as
// str.splitlines() splits them, each without a trailing \r), each
// "Name: value" line under CONTROL: or CONTROL DEFAULT: that has a range
// padded to 40 characters and followed by its display value, int()-ed
// (towards zero), and unit: "Matte Density: 5000    (100%)". A name
// starting "Offset " is looked up without it. A value of inf or nan is
// int()'s error, as there (SURFACE-ultimatte.md §5.4 item 3).
func AnnotatePreamble(preamble string) (string, error) {
	var out []string
	inControl := false
	for _, line := range pytext.SplitLines(preamble) {
		stripped := strings.TrimRight(line, "\r")
		if strings.HasSuffix(stripped, ":") && !strings.HasPrefix(stripped, " ") {
			header := stripped[:len(stripped)-1]
			inControl = header == "CONTROL" || header == "CONTROL DEFAULT"
			out = append(out, stripped)
			continue
		}
		if !inControl || !strings.Contains(stripped, ":") {
			out = append(out, stripped)
			continue
		}
		name, value, _ := strings.Cut(stripped, ":")
		name, value = pytext.Strip(name), pytext.Strip(value)
		lookup := name
		if strings.HasPrefix(name, "Offset ") {
			lookup = name[len("Offset "):]
		}
		display, unit, ok := ToDisplay(lookup, value)
		if !ok {
			out = append(out, stripped)
			continue
		}
		num, err := pyIntOfFloat(display)
		if err != nil {
			return "", err
		}
		sep := " "
		if unit == "%" {
			sep = ""
		}
		left := name + ": " + value
		if n := utf8.RuneCountInString(left); n < 40 {
			left += strings.Repeat(" ", 40-n)
		}
		out = append(out, left+" ("+num+sep+unit+")")
	}
	return strings.Join(out, "\n") + "\n", nil
}

// pyIntOfFloat is str(int(x)): the float's integer part, towards zero,
// every digit of it; Python's errors for the infinities and NaN.
func pyIntOfFloat(x float64) (string, error) {
	switch {
	case math.IsInf(x, 0):
		return "", &Error{Class: "OverflowError", Msg: "cannot convert float infinity to integer", kind: ErrValue}
	case math.IsNaN(x):
		return "", &Error{Class: "ValueError", Msg: "cannot convert float NaN to integer", kind: ErrValue}
	}
	i, _ := new(big.Float).SetFloat64(math.Trunc(x)).Int(nil)
	return i.String(), nil
}
