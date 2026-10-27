// SPDX-License-Identifier: MIT

package ultimatte

// The 9998 prelude of bmdwire v1.6.0's ultimattewire/_preamble.py:
// read_preamble, parse_section, parse_file_list, extract_label.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// preludeEnd is the marker the prelude ends with.
var preludeEnd = []byte("END PRELUDE:")

// ReadPrelude is read_preamble: it connects to 9998 and reads what the
// unit sends, PreludeQuiet per read, until a read times out, the unit
// closes or the text holds END PRELUDE; then it reads on, PreludeDrain
// per read, until one times out or the unit closes. It returns all of
// it, decoded as Python's 'replace' does. There is no overall deadline
// or size cap (SURFACE-ultimatte.md §5.4 item 8), beyond the context's.
func (c *Client) ReadPrelude(ctx context.Context, host string) (string, error) {
	conn, err := c.connect(ctx, host, ControlPort, c.connectTimeout())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	defer watch(ctx, conn)()
	buf, err := c.prelude(ctx, conn, true)
	return pytext.Decode(buf), err
}

// prelude reads the prelude off conn: read_preamble's loop, with its
// drain, or exchange's _drain_prelude, which stops at the marker.
func (c *Client) prelude(ctx context.Context, conn net.Conn, drain bool) ([]byte, error) {
	quiet := or(c.PreludeQuiet, DefaultPreludeQuiet)
	var buf []byte
	chunk := make([]byte, 65536)
	for {
		n, err := readOnce(ctx, conn, chunk, quiet)
		buf = append(buf, chunk[:n]...)
		if err != nil {
			if errors.Is(err, errQuiet) || errors.Is(err, io.EOF) {
				return buf, nil
			}
			return buf, err
		}
		if bytes.Contains(buf, preludeEnd) {
			break
		}
	}
	if !drain {
		return buf, nil
	}
	for {
		n, err := readOnce(ctx, conn, chunk, or(c.PreludeDrain, DefaultPreludeDrain))
		buf = append(buf, chunk[:n]...)
		if errors.Is(err, errQuiet) || errors.Is(err, io.EOF) {
			return buf, nil
		}
		if err != nil {
			// The drain catches only socket.timeout: anything else
			// propagates, as there.
			return buf, err
		}
	}
}

// errQuiet is a read that timed out: socket.timeout, caught.
var errQuiet = errors.New("quiet")

// readOnce is one recv with a timeout: what came, io.EOF for the unit
// closing, errQuiet for the timeout.
func readOnce(ctx context.Context, conn net.Conn, chunk []byte, timeout time.Duration) (int, error) {
	conn.SetReadDeadline(deadline(ctx, timeout))
	n, err := conn.Read(chunk)
	switch {
	case n > 0:
		return n, nil
	case err == nil:
		return 0, nil
	case timedOut(err) && ctx.Err() == nil:
		return 0, errQuiet
	case errors.Is(err, io.EOF):
		return 0, io.EOF
	}
	return 0, contextOr(ctx, err)
}

// ParseSection is parse_section: the body of the prelude's section named
// header, stripped; "" when there is none. The section starts on the
// line after the first line that is exactly "<header>:" (trailing spaces
// and tabs allowed) and ends where its regex's lookahead first holds: a
// newline and an all-capitals header (ASCII capitals, digits, spaces,
// a colon, then whitespace holding a newline), a newline and END
// PRELUDE:, or the end.
func ParseSection(text, header string) string {
	rs := []rune(text)
	h := []rune(header + ":")
	start := -1
	for p := 0; p <= len(rs) && start < 0; p++ {
		if p > 0 && rs[p-1] != '\n' || !hasRunes(rs[p:], h) {
			continue
		}
		i := p + len(h)
		for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
			i++
		}
		if i < len(rs) && rs[i] == '\n' {
			start = i + 1
		}
	}
	if start < 0 {
		return ""
	}
	end := start
	for end < len(rs) && !sectionEnds(rs, end) {
		end++
	}
	return pytext.Strip(string(rs[start:end]))
}

func hasRunes(rs, prefix []rune) bool {
	if len(rs) < len(prefix) {
		return false
	}
	for i, r := range prefix {
		if rs[i] != r {
			return false
		}
	}
	return true
}

// sectionEnds is the lookahead (?=\n[A-Z][A-Z0-9 ]*:\s*\n|\nEND PRELUDE:)
// at q.
func sectionEnds(rs []rune, q int) bool {
	if rs[q] != '\n' {
		return false
	}
	if hasRunes(rs[q:], []rune("\nEND PRELUDE:")) {
		return true
	}
	j := q + 1
	if j >= len(rs) || rs[j] < 'A' || rs[j] > 'Z' {
		return false
	}
	for j < len(rs) && (rs[j] >= 'A' && rs[j] <= 'Z' || rs[j] >= '0' && rs[j] <= '9' || rs[j] == ' ') {
		j++
	}
	if j >= len(rs) || rs[j] != ':' {
		return false
	}
	for k := j + 1; k < len(rs) && pytext.IsSpace(rs[k]); k++ {
		if rs[k] == '\n' {
			return true
		}
	}
	return false
}

// ParseFileList is parse_file_list: the FILE LIST section's lines, as
// str.splitlines() splits them, stripped, the empty ones dropped: the
// unit's preset slots.
func ParseFileList(text string) []string {
	out := []string{}
	section := ParseSection(text, "FILE LIST")
	if section == "" {
		return out
	}
	for _, l := range pytext.SplitLines(section) {
		if l = pytext.Strip(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Label is the value of the prelude's first Label line that has one, as
// extract_label's regex ^Label:\s*(.+)$ finds it, stripped: \s* may cross
// a line end, so "Label:" alone takes the next line (SURFACE-ultimatte.md
// §5.4 item 2). ok is false when no line matches.
func Label(text string) (label string, ok bool) {
	rs := []rune(text)
	for p := 0; p < len(rs); p++ {
		if p > 0 && rs[p-1] != '\n' || !hasRunes(rs[p:], []rune("Label:")) {
			continue
		}
		i := p + len("Label:")
		w := i
		for w < len(rs) && pytext.IsSpace(rs[w]) {
			w++
		}
		// (.+) starts at the last place \s* can stop with a character
		// that isn't a newline after it.
		k := -1
		for at := w; at >= i; at-- {
			if at < len(rs) && rs[at] != '\n' {
				k = at
				break
			}
		}
		if k < 0 {
			continue
		}
		e := k
		for e < len(rs) && rs[e] != '\n' {
			e++
		}
		return pytext.Strip(string(rs[k:e])), true
	}
	return "", false
}

// ExtractLabel is extract_label: the Label, with every run of characters
// other than ASCII letters, digits, ".", "_" and "-" one "_", for a
// filename; "unknown" when the prelude has none.
func ExtractLabel(text string) string {
	l, ok := Label(text)
	if !ok {
		return "unknown"
	}
	var b strings.Builder
	run := false
	for _, r := range l {
		if r < 0x80 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			b.WriteRune(r)
			run = false
		} else if !run {
			b.WriteByte('_')
			run = true
		}
	}
	return b.String()
}

// ExtractModel is extract_model (bmdwire#52): the IDENTITY section's
// Model, "" when it has none. A 12 4K on software 2.3 says "Ultimatte
// 12 4K".
func ExtractModel(text string) string { return field(text, "IDENTITY", "Model") }

// ParseCurrentFile is parse_current_file (bmdwire#52): CURRENT FILE's
// Name and Status, the file the live settings came from and "Clean"
// while they match it or "Modified" once they have been changed on the
// unit; "" each when it has none. A restore's SavedSettings replace the
// live settings, so Modified is what a restore loses.
func ParseCurrentFile(text string) (name, status string) {
	return field(text, "CURRENT FILE", "Name"), field(text, "CURRENT FILE", "Status")
}

// field is _field: the value of a section's first line whose key, before
// its first colon (str.partition), is key once stripped; the lines as
// str.splitlines() splits them, the value stripped. "" when none is.
func field(text, header, key string) string {
	for _, l := range pytext.SplitLines(ParseSection(text, header)) {
		if k, v, ok := strings.Cut(l, ":"); ok && pytext.Strip(k) == key {
			return pytext.Strip(v)
		}
	}
	return ""
}
