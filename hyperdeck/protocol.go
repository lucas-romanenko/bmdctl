// SPDX-License-Identifier: MIT

package hyperdeck

// The framing of bmdwire v1.3.0's hyperdeckwire/client.py: _recv_line,
// _read_response, Response, and the clip-line parsers.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// Response is one response block off the wire: the head's code and text
// (without the colon that marks a block), and the body's lines.
type Response struct {
	Code  int      `json:"code"`
	Text  string   `json:"text"`
	Lines []string `json:"lines,omitempty"`
}

// OK reports a success: 200-299.
func (r Response) OK() bool { return r.Code >= 200 && r.Code < 300 }

// IsError reports a failure: 100-199.
func (r Response) IsError() bool { return r.Code >= 100 && r.Code < 200 }

// IsAsync reports an asynchronous notification: 500-599. The greeting,
// 500 connection info, is one too, though it comes once and first.
func (r Response) IsAsync() bool { return r.Code >= 500 && r.Code < 600 }

// Params reads the body as "key: value" lines, each key and value
// stripped as Python strips them; a line without a colon is skipped and
// the last of two equal keys wins, as Response.params does.
func (r Response) Params() map[string]string {
	out := map[string]string{}
	for _, line := range r.Lines {
		s := pytext.Strip(line)
		if s == "" || !strings.Contains(s, ":") {
			continue
		}
		k, v, _ := strings.Cut(s, ":")
		out[pytext.Strip(k)] = pytext.Strip(v)
	}
	return out
}

// Error is a failure the deck reported (a 1xx code): hyperdeckwire's
// HyperdeckError. The codes are BMD's (docs/SURFACE-hyperdeck.md): 111
// is remote control disabled, 112 clip not found.
type Error struct {
	Code int
	Text string
}

func (e *Error) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Text) }

// ErrMalformed marks a head that doesn't start with a number:
// hyperdeckwire's OSError("malformed response head: ...").
var ErrMalformed = errors.New("malformed response head")

// repr is the response as hyperdeckwire's Response dataclass repr()s it,
// for the messages that quote one: Response(code=200, text='ok',
// lines=['k: v']).
func (r Response) repr() string {
	lines := make([]string, len(r.Lines))
	for i, l := range r.Lines {
		lines[i] = pytext.Repr(l)
	}
	return fmt.Sprintf("Response(code=%d, text=%s, lines=[%s])", r.Code, pytext.Repr(r.Text), strings.Join(lines, ", "))
}

// ErrGreeting marks a first block that isn't the greeting (500 connection
// info): not a deck to talk to, as hyperdeckwire's connect has it since
// 1.4.0 (bmdwire#21).
var ErrGreeting = errors.New("unexpected greeting")

// errPeerClosed is the stream ending: hyperdeckwire's ConnectionError.
var errPeerClosed = errors.New("peer closed connection")

// reader reads response blocks as _read_response does, from a stream that
// may also carry asynchronous messages; it doesn't skip them, its caller
// sorts them.
type reader struct{ br *bufio.Reader }

func newReader(r io.Reader) *reader { return &reader{bufio.NewReaderSize(r, 64<<10)} }

// line is _recv_line: up to the next LF, with every CR before it
// stripped, decoded as Python's 'replace' does. The stream ending, even
// in the middle of a line, is errPeerClosed: _recv_line drops a partial
// line when recv returns nothing.
func (r *reader) line() (string, error) {
	b, err := r.br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		long := append([]byte(nil), b...)
		for errors.Is(err, bufio.ErrBufferFull) {
			if len(long) > maxLine {
				return "", fmt.Errorf("hyperdeck: a line of more than %d bytes", maxLine)
			}
			b, err = r.br.ReadSlice('\n')
			long = append(long, b...)
		}
		b = long
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", errPeerClosed
		}
		return "", err
	}
	b = b[:len(b)-1]
	for len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return pytext.Decode(b), nil
}

// maxLine bounds one line: a disk list is many short lines.
const maxLine = 1 << 20

// response is _read_response without its skipping: blank heads are
// skipped, the head's code read with Python's int(), and a head ending in
// a colon starts a block, read to the first empty line.
func (r *reader) response() (Response, error) {
	for {
		head, err := r.line()
		if err != nil {
			return Response{}, err
		}
		if head == "" {
			continue
		}
		code, text, _ := strings.Cut(head, " ")
		n, err := pytext.Int(code)
		if err != nil || n < -1<<31 || n > 1<<31-1 {
			return Response{}, fmt.Errorf("hyperdeck: %w: %s", ErrMalformed, pytext.Repr(head))
		}
		resp := Response{Code: int(n), Text: text}
		if strings.HasSuffix(text, ":") {
			resp.Text = text[:len(text)-1]
			for {
				l, err := r.line()
				if err != nil {
					return Response{}, err
				}
				if l == "" {
					break
				}
				resp.Lines = append(resp.Lines, l)
			}
		}
		return resp, nil
	}
}

// Clip is a clip on the timeline (clips get) or on a disk (disk list).
// Every clip has an id, a name and a duration; a timeline clip has its
// start, a disk's its file and video formats.
type Clip struct {
	ID          int     `json:"clip_id"`
	Name        string  `json:"name"`
	Duration    string  `json:"duration"` // HH:MM:SS:FF
	Start       *string `json:"start,omitempty"`
	FileFormat  *string `json:"file_format,omitempty"`  // H.264
	VideoFormat *string `json:"video_format,omitempty"` // 1080p25
}

// looksLikeClipLine is _looks_like_clip_line: the text before the first
// colon, once the line is stripped, is a number.
func looksLikeClipLine(line string) bool {
	head, _, _ := strings.Cut(pytext.Strip(line), ":")
	return pytext.IsDecimal(head)
}

// parseClipLines is _parse_clip_lines: every clip-shaped line parsed,
// the ones that don't parse skipped, so one bad row doesn't lose the
// others.
func parseClipLines(lines []string, parse func(string) (Clip, error)) []Clip {
	clips := []Clip{}
	for _, l := range lines {
		if !looksLikeClipLine(l) {
			continue
		}
		if c, err := parse(l); err == nil {
			clips = append(clips, c)
		}
	}
	return clips
}

// clipTokens splits a clip line at its first colon into the id and the
// rest, split on single spaces as Python's split(' ') splits, so names
// keep their spaces once joined again.
func clipTokens(line string) (int, []string, error) {
	head, rest, _ := strings.Cut(pytext.Strip(line), ":")
	id, err := pytext.Int(pytext.Strip(head))
	if err != nil {
		return 0, nil, err
	}
	if id < -1<<31 || id > 1<<31-1 {
		return 0, nil, fmt.Errorf("clip id %d out of range", id)
	}
	return int(id), strings.Split(pytext.Strip(rest), " "), nil
}

// parseDiskLine is _parse_disk_line: "{id}: {name} {file format} {video
// format} {duration}", read from the right, since names hold spaces.
func parseDiskLine(line string) (Clip, error) {
	id, t, err := clipTokens(line)
	if err != nil {
		return Clip{}, err
	}
	if len(t) < 4 {
		return Clip{}, fmt.Errorf("malformed disk list line: %s", pytext.Repr(line))
	}
	n := len(t)
	file, video := t[n-3], t[n-2]
	return Clip{ID: id, Name: strings.Join(t[:n-3], " "), Duration: t[n-1], FileFormat: &file, VideoFormat: &video}, nil
}

// parseClipsLine is _parse_clips_v1_line: "{id}: {name} {start}
// {duration}", read from the right.
func parseClipsLine(line string) (Clip, error) {
	id, t, err := clipTokens(line)
	if err != nil {
		return Clip{}, err
	}
	if len(t) < 3 {
		return Clip{}, fmt.Errorf("malformed clips line: %s", pytext.Repr(line))
	}
	n := len(t)
	start := t[n-2]
	return Clip{ID: id, Name: strings.Join(t[:n-2], " "), Duration: t[n-1], Start: &start}, nil
}
