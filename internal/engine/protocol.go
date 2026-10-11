// SPDX-License-Identifier: MIT

package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Version is the request protocol's version. The engine refuses a request
// carrying another one, so a CLI never talks to an engine built from
// different code without knowing it.
const Version = 1

// maxFrame bounds one frame, request or reply. A frame can carry a still
// or a profile's media pool, base64 encoded.
const maxFrame = 1 << 30

// Request is what the CLI sends: one JSON object on one line.
type Request struct {
	V      int    `json:"v"`
	Device string `json:"device"`         // "atem", or "engine" for the engine itself
	Host   string `json:"host,omitempty"` // "host" or "host:port"
	Path   string `json:"path"`           // the command's words: "transition wipe"
	// Params are the command's --name value pairs, as typed.
	Params map[string]string `json:"params,omitempty"`
	// Files holds the contents of the files the command reads, by
	// parameter name, read by the CLI: the engine may run where the
	// caller's files aren't (a container, another user's service).
	Files map[string][]byte `json:"files,omitempty"`
	// Yes allows a destructive command.
	Yes bool `json:"yes,omitempty"`
	// Ask: a person can be asked to confirm a destructive command, so
	// one without Yes is looked at against the switcher, and what it
	// would affect said in a Confirm note, before it is refused.
	Ask bool `json:"ask,omitempty"`
	// FPS, if set, is the display rate for "seconds:frames" rates
	// instead of the switcher's.
	FPS int `json:"fps,omitempty"`
	// TimeoutMS bounds the wait for the switcher's echo; 0 means 5 s.
	TimeoutMS int64 `json:"timeout_ms,omitempty"`
	// Build is the build of the client sending it. A background engine of
	// another build answers CodeStale to a device request and stops, for
	// the client to start one of its own; requests to the engine itself
	// (status, stop) are answered whatever the build.
	Build string `json:"build,omitempty"`
	// Notes asks for note frames before the response (Note): the
	// switcher reached, a connect under way, what a command sets. The
	// CLI asks always and prints them only for people, so a --json
	// line is the same either way.
	Notes bool `json:"notes,omitempty"`
}

// Error codes, each with its own exit status in the CLI.
const (
	CodeUsage       = "usage"       // the request is wrong: exit 1
	CodeUnreachable = "unreachable" // no switcher, or no engine: exit 2
	CodeNoEcho      = "no_echo"     // sent, not confirmed in time: exit 3
	CodeRejected    = "rejected"    // the switcher refused it: exit 4
	CodeDestructive = "destructive" // needs Yes: exit 5
	// CodeStale: the engine runs another build than the client's and is
	// stopping; the client starts a new one and retries, once. Exit 2 if
	// it persists.
	CodeStale = "stale"
)

// Response is the engine's answer: {ok, confirmed, result, took_ms} on
// success, {ok:false, code, error} on failure. A streaming command
// sends one success per result and ends with a failure or by the caller
// hanging up.
type Response struct {
	OK        bool
	Confirmed bool // the switcher reported the change back
	Result    any  // a json.RawMessage when read by Call
	TookMS    int64
	Code      string
	Error     string
}

// MarshalJSON writes exactly the fields of the response's shape.
func (r Response) MarshalJSON() ([]byte, error) {
	if !r.OK {
		return json.Marshal(struct {
			OK    bool   `json:"ok"`
			Code  string `json:"code"`
			Error string `json:"error"`
		}{false, r.Code, r.Error})
	}
	return json.Marshal(struct {
		OK        bool  `json:"ok"`
		Confirmed bool  `json:"confirmed"`
		Result    any   `json:"result"`
		TookMS    int64 `json:"took_ms"`
	}{true, r.Confirmed, r.Result, r.TookMS})
}

// UnmarshalJSON reads either shape; Result stays raw JSON.
func (r *Response) UnmarshalJSON(b []byte) error {
	var w struct {
		OK        bool            `json:"ok"`
		Confirmed bool            `json:"confirmed"`
		Result    json.RawMessage `json:"result"`
		TookMS    int64           `json:"took_ms"`
		Code      string          `json:"code"`
		Error     string          `json:"error"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*r = Response{OK: w.OK, Confirmed: w.Confirmed, TookMS: w.TookMS, Code: w.Code, Error: w.Error}
	if len(w.Result) > 0 {
		r.Result = w.Result
	}
	return nil
}

// FileFrame carries a file the command produced, before its response.
// Param names the parameter it belongs to. Name is empty for the file the
// parameter names, or a relative slash-separated path: inside the
// directory a Dir parameter names, or beside the file an OutFile
// parameter names.
type FileFrame struct {
	Param string `json:"param"`
	Name  string `json:"name,omitempty"`
	Data  []byte `json:"data"`
}

// Note is a frame for people, sent before the response to a request that
// asks for notes. The CLI prints it on a terminal and never with --json,
// so it carries what only a person reads: how the session was had, the
// switcher's name and model, a command's before and after. Each note
// sets one field.
type Note struct {
	// Connecting is the session's host:port when the request opens the
	// session, waits for another request's dial, or waits for a session
	// that went silent to come back. A Switcher note follows once it is
	// ready.
	Connecting string `json:"connecting,omitempty"`
	// Switcher is the switcher the request reached, as its session holds
	// it: every request that uses a session gets one.
	Switcher *Switcher `json:"switcher,omitempty"`
	// Values are what a command sets, each before and after, in the
	// units it takes. The CLI shows a write from them.
	Values []Value `json:"values,omitempty"`
	// OnAir is what M/E 1 has on program and preview: sent with the
	// Switcher note, and again once a command is done, so a terminal's
	// header shows what is live.
	OnAir *OnAir `json:"on_air,omitempty"`
	// Alias: a source typed as an alias that is also an input's name;
	// the alias won (sources.go).
	Alias *Alias `json:"alias,omitempty"`
	// NoInput: a source typed names no input; sent before the failure,
	// with the inputs it could have been.
	NoInput *NoInput `json:"no_input,omitempty"`
	// Progress is how far a transfer has come (progress.go); at most
	// ten a second.
	Progress *Progress `json:"progress,omitempty"`
	// Steps announces an operation's steps; a Step note says each one
	// begun, then done or failed.
	Steps []string `json:"steps,omitempty"`
	Step  *Step    `json:"step,omitempty"`
	// Confirm: what a destructive command would affect, before it is
	// refused for want of Yes, to a request that may ask (Ask).
	Confirm *Confirm `json:"confirm,omitempty"`
}

// Alias is a source an alias named, and the input whose name it shadowed.
type Alias struct {
	Typed    string `json:"typed"`
	Source   Source `json:"source"`
	Shadowed Source `json:"shadowed"`
}

// NoInput is a source that names nothing, and the switcher's first six
// inputs.
type NoInput struct {
	Typed  string   `json:"typed"`
	Inputs []Source `json:"inputs"`
}

// OnAir is M/E 1's program and preview sources: by id, and by name as a
// line shows them (short name, else long name, else id).
type OnAir struct {
	Program   string `json:"program"`
	Preview   string `json:"preview"`
	ProgramID int    `json:"program_id"`
	PreviewID int    `json:"preview_id"`
}

// Switcher is a switcher as the engine's session to it knows it.
type Switcher struct {
	Host string `json:"host"` // the session's key, host:port
	// Name is what the switcher calls itself: the name set in ATEM
	// Setup (WhoI), else its product name (_pin), as bmdwire v1.3.0's
	// state.py atem_name is.
	Name      string `json:"name,omitempty"`
	Product   string `json:"product,omitempty"`
	VideoMode string `json:"video_mode,omitempty"`
	Ready     bool   `json:"ready"`
	Pinned    bool   `json:"pinned,omitempty"`
	// IdleS is how long the session had gone without a request: before
	// this one, in a note.
	IdleS int `json:"idle_s"`
	// DropS is how long the session may go unused before it closes, from
	// the end of the request in a note, from now in an overview; 0 when
	// pinned.
	DropS int    `json:"drop_s,omitempty"`
	Error string `json:"error,omitempty"` // the last dial's failure, with no session
	// NameWidth is the widest of the switcher's sources as a line shows
	// them (short name, else long name, else id), in characters: the CLI
	// sizes a write's before → after column to it. In a note only.
	NameWidth int `json:"name_width,omitempty"`
}

// Value is one thing a command sets: its parameter, or for a command
// with none of its own (cut), what it moves.
type Value struct {
	Param string `json:"param"`
	// Before is the value the switcher held, as a person reads it;
	// absent when the state doesn't say.
	Before *string `json:"before,omitempty"`
	// After is what the switcher reported back for a confirmed command,
	// else what was sent.
	After string `json:"after"`
	// Kept: the switcher confirmed the command but kept the value it
	// held, not the one sent (a terminal says "rejected").
	Kept bool `json:"kept,omitempty"`
}

// frame is one line from the engine: a file, a note or a response.
type frame struct {
	File *FileFrame `json:"file,omitempty"`
	Note *Note      `json:"note,omitempty"`
}

// Frame is one line of a reply, as Call reads it: the raw line and the
// file, note or response in it.
type Frame struct {
	Line     []byte
	File     *FileFrame
	Note     *Note
	Response *Response
}

// Error is a failure with its code.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func codeErr(code string, format string, args ...any) error {
	return &Error{code, fmt.Errorf(format, args...)}
}

// CodeOf returns err's code: its own, or CodeUsage.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUsage
}

// Call sends req over conn and passes every frame of the reply to onFrame.
// It returns the last response, or an error if the engine hung up before
// sending one. A streaming reply ends when the engine closes the
// connection or ctx ends. onFrame may be nil.
func Call(ctx context.Context, conn io.ReadWriteCloser, req Request, onFrame func(Frame) error) (Response, error) {
	if onFrame == nil {
		onFrame = func(Frame) error { return nil }
	}
	req.V = Version
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return Response{}, codeErr(CodeUnreachable, "engine: %v", err)
	}
	br := bufio.NewReaderSize(conn, 64<<10)
	var last *Response
	for {
		line, err := readLine(br)
		if err != nil {
			if ctx.Err() != nil {
				return Response{}, ctx.Err()
			}
			if last != nil && errors.Is(err, io.EOF) {
				return *last, nil
			}
			return Response{}, codeErr(CodeUnreachable, "engine hung up: %v", err)
		}
		var f frame
		if err := json.Unmarshal(line, &f); err != nil {
			return Response{}, fmt.Errorf("engine: bad frame: %v", err)
		}
		if f.File != nil || f.Note != nil {
			if err := onFrame(Frame{Line: line, File: f.File, Note: f.Note}); err != nil {
				return Response{}, err
			}
			continue
		}
		var r Response
		if err := json.Unmarshal(line, &r); err != nil {
			return Response{}, fmt.Errorf("engine: bad response: %v", err)
		}
		if err := onFrame(Frame{Line: line, Response: &r}); err != nil {
			return Response{}, err
		}
		last = &r
	}
}

func readLine(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := br.ReadSlice('\n')
		buf.Write(chunk)
		if buf.Len() > maxFrame {
			return nil, errors.New("frame too long")
		}
		if err == nil {
			return bytes.TrimRight(buf.Bytes(), "\r\n"), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if err == io.EOF && buf.Len() > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
}

// timeout is a request's echo wait.
func (r Request) timeout() time.Duration {
	if r.TimeoutMS > 0 {
		return time.Duration(r.TimeoutMS) * time.Millisecond
	}
	return DefaultEchoTimeout
}

// DefaultEchoTimeout bounds the wait for a command's echo.
const DefaultEchoTimeout = 5 * time.Second
