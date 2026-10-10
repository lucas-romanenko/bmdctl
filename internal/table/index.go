// SPDX-License-Identifier: MIT

package table

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Index is a position the atem package counts from 0 (an M/E, a keyer, a
// DSK, an aux output, a media or macro slot, a media player), as people
// count it: from 1, as ATEM Software Control and the panels do. Its value
// is the package's; typed on the command line, in JSON and in text it is
// one more. This type is the only place the one is added or taken away.
type Index int

// String is the index counted from 1.
func (i Index) String() string { return strconv.Itoa(int(i) + 1) }

// Format prints the index counted from 1 with any verb (%d as much as
// %v), so no format string can print the value by mistake.
func (i Index) Format(f fmt.State, verb rune) {
	switch verb {
	case 'd', 'x', 'X', 'o':
		fmt.Fprintf(f, fmt.FormatString(f, verb), int(i)+1)
		return
	}
	s := i.String()
	if w, ok := f.Width(); ok && len(s) < w {
		pad := strings.Repeat(" ", w-len(s))
		if f.Flag('-') {
			s += pad
		} else {
			s = pad + s
		}
	}
	f.Write([]byte(s))
}

// MarshalJSON writes the index counted from 1.
func (i Index) MarshalJSON() ([]byte, error) { return []byte(i.String()), nil }

// UnmarshalJSON reads an index counted from 1.
func (i *Index) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return errors.New("table: an index is a whole number")
	}
	*i = Index(n - 1)
	return nil
}

// ParseIndex reads an index as typed, counted from 1.
func ParseIndex(s string) (Index, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, errors.New("not a whole number counted from 1")
	}
	if n == 0 {
		return 0, errors.New("counted from 1")
	}
	return Index(n - 1), nil
}

// IndexPtr is a pointer to i, for results that may have no index.
func IndexPtr(i int) *Index { x := Index(i); return &x }
