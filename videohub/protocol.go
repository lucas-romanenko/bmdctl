// SPDX-License-Identifier: MIT

package videohub

// The framing and parsing of bmdwire v1.6.0's videohubwire/client.py:
// _recv_block, _apply_block, _kv and _apply_indexed.

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// Error is a failure videohubwire raises as VideohubError: a NAK, no
// answer, the connection closing, no preamble, or an argument refused
// before sending. Its message is videohubwire's; errors.Is tells the
// kinds apart (ErrRefused, ErrTimeout, ErrClosed), and an argument
// refused before sending is none of them.
type Error struct {
	Msg  string
	kind error
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.kind }

// The kinds of Error.
var (
	// ErrRefused: the hub answered NAK (a destination locked elsewhere,
	// typically).
	ErrRefused = errors.New("videohub: refused")
	// ErrTimeout: the hub didn't answer in time. The session ends with
	// it (docs/PORTING.md, "Videohub": a late answer would be read as the
	// next command's).
	ErrTimeout = errors.New("videohub: no answer in time")
	// ErrClosed: the session has ended, by Close or because the
	// connection failed; the error wraps the cause.
	ErrClosed = errors.New("videohub: session closed")
)

// errHubClosed is the hub closing the connection: VideohubError('connection
// closed by hub').
var errHubClosed = &Error{Msg: "connection closed by hub", kind: ErrClosed}

// maxBlock bounds one block: a 288 x 288 hub's labels are a few KB.
const maxBlock = 1 << 20

// reader reads blocks as _recv_block does: the bytes up to the first
// "\n\n" in the stream, decoded as Python's 'replace' does and split at
// "\n", the header first. A block already buffered is returned before
// the stream is read again; the stream ending is errHubClosed, a partial
// block with it dropped.
type reader struct {
	r   io.Reader
	buf []byte
}

func newReader(r io.Reader) *reader { return &reader{r: r} }

func (r *reader) block() ([]string, error) {
	chunk := make([]byte, 4096)
	for {
		if i := bytes.Index(r.buf, []byte("\n\n")); i >= 0 {
			raw := r.buf[:i]
			r.buf = r.buf[i+2:]
			return strings.Split(pytext.Decode(raw), "\n"), nil
		}
		if len(r.buf) > maxBlock {
			return nil, &Error{Msg: "a block of more than 1 MiB", kind: ErrClosed}
		}
		n, err := r.r.Read(chunk)
		r.buf = append(r.buf, chunk[:n]...)
		if n > 0 {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil, errHubClosed
		}
		if err != nil {
			return nil, err
		}
	}
}

// header is a block's first line, stripped: what _apply_block returns.
func header(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return pytext.Strip(lines[0])
}

// body is a block's lines after the header that aren't empty once
// stripped.
func body(lines []string) []string {
	var out []string
	if len(lines) == 0 {
		return out
	}
	for _, l := range lines[1:] {
		if pytext.Strip(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// Block headers videohubwire acts on.
const (
	hdrProtocol = "PROTOCOL PREAMBLE:"
	hdrDevice   = "VIDEOHUB DEVICE:"
	hdrInputs   = "INPUT LABELS:"
	hdrOutputs  = "OUTPUT LABELS:"
	hdrRouting  = "VIDEO OUTPUT ROUTING:"
	hdrLocks    = "VIDEO OUTPUT LOCKS:"
	hdrEnd      = "END PRELUDE:"
	hdrACK      = "ACK"
	hdrNAK      = "NAK"
)

// keys is the block's key: what a Change names it by.
var keys = map[string]string{
	hdrProtocol: "protocol", hdrDevice: "device", hdrInputs: "input labels", hdrOutputs: "output labels",
	hdrRouting: "routing", hdrLocks: "locks",
}

// apply is _apply_block: it takes one block into st and returns its
// header, and the values it carried, keyed as the block keys them (the
// key, lower-cased, or the index); nil for a block videohubwire ignores.
// A line that doesn't parse is logged and skipped.
func apply(st *State, lines []string, log *slog.Logger) (string, map[string]string) {
	h := header(lines)
	b := body(lines)
	vals := map[string]string{}
	switch h {
	case hdrProtocol:
		for _, kv := range keyValues(b) {
			if kv[0] == "version" {
				st.ProtocolVersion = kv[1]
				vals[kv[0]] = kv[1]
			}
		}
	case hdrDevice:
		for _, kv := range keyValues(b) {
			st.Device[kv[0]] = kv[1]
			vals[kv[0]] = kv[1]
		}
	case hdrInputs, hdrOutputs, hdrLocks:
		m := map[string]map[int]string{hdrInputs: st.InputLabels, hdrOutputs: st.OutputLabels, hdrLocks: st.Locks}[h]
		indexed(b, log, func(i int, v string) error {
			m[i] = v
			vals[itoa(i)] = v
			return nil
		})
	case hdrRouting:
		indexed(b, log, func(i int, v string) error {
			src, err := pytext.Int(v)
			if err != nil {
				return err
			}
			st.Routing[i] = int(src)
			vals[itoa(i)] = itoa(int(src))
			return nil
		})
	default:
		return h, nil
	}
	return h, vals
}

// keyValues is _kv: each line split at its first colon, the key stripped
// and lower-cased, the value stripped; a line without a colon skipped.
func keyValues(lines []string) [][2]string {
	var out [][2]string
	for _, l := range lines {
		k, v, ok := strings.Cut(l, ":")
		if ok {
			out = append(out, [2]string{pytext.Lower(pytext.Strip(k)), pytext.Strip(v)})
		}
	}
	return out
}

// indexed is _apply_indexed: each line stripped and split at its first
// space into the index, read with int(), and the rest, stripped, given to
// set. A line whose index or value doesn't parse is logged and skipped.
func indexed(lines []string, log *slog.Logger, set func(int, string) error) {
	for _, l := range lines {
		tok, rest, _ := strings.Cut(pytext.Strip(l), " ")
		i, err := pytext.Int(tok)
		if err == nil {
			err = set(int(i), pytext.Strip(rest))
		}
		if err != nil {
			log.Warn("videohub: skipping malformed line", "line", pytext.Repr(l))
		}
	}
}
