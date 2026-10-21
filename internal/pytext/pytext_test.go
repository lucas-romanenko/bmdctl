// SPDX-License-Identifier: MIT

package pytext

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"
)

// oracle is testdata/oracle.json (gen_oracle.py): CPython's own answers.
type oracle struct {
	Python, Unicode string
	Space           []rune
	Decimal         [][2]int
	OtherDigit      []rune `json:"other_digit"`
	Lower           [][2]any
	Unassigned      [][2]rune
	Cases           []struct {
		S, Strip, Lower, Repr string
		Split, Splitlines     []string
		Isdecimal, Isdigit    bool
		Int, Float            result
	}
	Decodes []struct{ B, S string }
}

// result is a call that returned a value (an int, or a float's repr) or
// raised ValueError with a message.
type result struct {
	Value *json.Number
	Float *string
	Error *string
}

func load(t *testing.T) oracle {
	t.Helper()
	raw, err := os.ReadFile("testdata/oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o oracle
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// Every character Unicode 15.0 assigns has CPython 3.12's properties:
// whitespace, decimal value, digit, lower case. Characters assigned
// since are skipped: Go's tables are newer.
func TestCharactersMatchPython(t *testing.T) {
	o := load(t)
	unassigned := func(r rune) bool {
		i, found := slices.BinarySearchFunc(o.Unassigned, r, func(rg [2]rune, r rune) int {
			switch {
			case r < rg[0]:
				return 1
			case r > rg[1]:
				return -1
			}
			return 0
		})
		_ = i
		return found
	}
	space := map[rune]bool{}
	for _, r := range o.Space {
		space[r] = true
	}
	decimal := map[rune]int{}
	for _, d := range o.Decimal {
		decimal[rune(d[0])] = d[1]
	}
	other := map[rune]bool{}
	for _, r := range o.OtherDigit {
		other[r] = true
	}
	lower := map[rune]string{}
	for _, l := range o.Lower {
		lower[rune(l[0].(float64))] = l[1].(string)
	}
	bad := 0
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff || unassigned(r) {
			continue
		}
		d, isDec := DecimalValue(r)
		want, wantDec := decimal[r]
		lw, ok := lower[r]
		if !ok {
			lw = string(r)
		}
		switch {
		case IsSpace(r) != space[r]:
			t.Errorf("IsSpace(%U) = %v", r, IsSpace(r))
		case isDec != wantDec || isDec && d != want:
			t.Errorf("DecimalValue(%U) = %d, %v; Python %d, %v", r, d, isDec, want, wantDec)
		case otherDigit(r) != other[r]:
			t.Errorf("otherDigit(%U) = %v", r, otherDigit(r))
		case Lower(string(r)) != lw:
			t.Errorf("Lower(%U) = %q; Python %q", r, Lower(string(r)), lw)
		default:
			continue
		}
		if bad++; bad > 20 {
			t.Fatal("too many")
		}
	}
}

// The string functions give what CPython's give on the oracle's strings.
func TestStringsMatchPython(t *testing.T) {
	o := load(t)
	if o.Python == "" || len(o.Cases) == 0 {
		t.Fatal("empty oracle")
	}
	for _, c := range o.Cases {
		s := c.S
		if Strip(s) != c.Strip {
			t.Errorf("Strip(%q) = %q; Python %q", s, Strip(s), c.Strip)
		}
		if got := Fields(s); !slices.Equal(got, c.Split) && !(len(got) == 0 && len(c.Split) == 0) {
			t.Errorf("Fields(%q) = %q; Python %q", s, got, c.Split)
		}
		if got := SplitLines(s); !slices.Equal(got, c.Splitlines) && !(len(got) == 0 && len(c.Splitlines) == 0) {
			t.Errorf("SplitLines(%q) = %q; Python %q", s, got, c.Splitlines)
		}
		if Lower(s) != c.Lower {
			t.Errorf("Lower(%q) = %q; Python %q", s, Lower(s), c.Lower)
		}
		if Repr(s) != c.Repr {
			t.Errorf("Repr(%q) = %s; Python %s", s, Repr(s), c.Repr)
		}
		if IsDecimal(s) != c.Isdecimal || IsDigit(s) != c.Isdigit {
			t.Errorf("IsDecimal, IsDigit(%q) = %v, %v; Python %v, %v", s, IsDecimal(s), IsDigit(s), c.Isdecimal, c.Isdigit)
		}
		n, err := Int(s)
		switch {
		case c.Int.Error != nil:
			if err == nil || err.Error() != "invalid literal for int() with base 10: "+Repr(s) {
				t.Errorf("Int(%q) = %d, %v; Python raises %s", s, n, err, *c.Int.Error)
			}
		default:
			want, perr := c.Int.Value.Int64()
			if perr != nil { // beyond int64, where Python goes on with a big integer
				if err == nil {
					t.Errorf("Int(%q) = %d; Python %s, beyond int64", s, n, *c.Int.Value)
				}
			} else if err != nil || n != want {
				t.Errorf("Int(%q) = %d, %v; Python %d", s, n, err, want)
			}
		}
		f, err := Float(s)
		switch {
		case c.Float.Error != nil:
			if err == nil || err.Error() != *c.Float.Error {
				t.Errorf("Float(%q) = %v, %v; Python raises %s", s, f, err, *c.Float.Error)
			}
		default:
			want, _ := strconv.ParseFloat(*c.Float.Float, 64)
			if err != nil || math.Float64bits(f) != math.Float64bits(want) && !(math.IsNaN(f) && math.IsNaN(want)) {
				t.Errorf("Float(%q) = %v, %v; Python %s", s, f, err, *c.Float.Float)
			}
		}
	}
	for _, d := range o.Decodes {
		b, _ := hex.DecodeString(d.B)
		if got := Decode(b); got != d.S {
			t.Errorf("Decode(%x) = %q; Python %q", b, got, d.S)
		}
	}
}
