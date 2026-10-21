// SPDX-License-Identifier: MIT

package videohub

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// Command is a block a session sends: videohubwire's four.
type Command interface {
	// Block is the block as sent: header, body, the blank line.
	Block() string
}

// Route is route(dest, src): send input Src to output Dest, both as the
// wire counts them, from 0.
type Route struct{ Dest, Src int }

func (c Route) Block() string { return fmt.Sprintf("%s\n%d %d\n\n", hdrRouting, c.Dest, c.Src) }

// InputLabel is set_input_label(index, label): rename an input. The
// label is sent as CleanLabel makes it.
type InputLabel struct {
	Index int
	Label string
}

func (c InputLabel) Block() string {
	return fmt.Sprintf("%s\n%d %s\n\n", hdrInputs, c.Index, CleanLabel(c.Label))
}

// OutputLabel is set_output_label(index, label): rename an output.
type OutputLabel struct {
	Index int
	Label string
}

func (c OutputLabel) Block() string {
	return fmt.Sprintf("%s\n%d %s\n\n", hdrOutputs, c.Index, CleanLabel(c.Label))
}

// Ping is ping(): PING, which the hub acks.
type Ping struct{}

func (Ping) Block() string { return "PING:\n\n" }

// MaxLabel is the longest label the hub takes, in characters.
const MaxLabel = 64

// CleanLabel is what the label setters send for a label: every run of
// whitespace (Python's, Unicode's) one space, none at the ends, so a line
// break can't break the block's framing. An empty label clears the name.
func CleanLabel(l string) string { return strings.Join(pytext.Fields(l), " ") }

// check is what a method refuses before sending, in videohubwire's
// order, given the hub's counts as the state has them now.
func check(c Command, st State) error {
	switch c := c.(type) {
	case Route:
		if c.Dest < 0 || c.Src < 0 {
			return &Error{Msg: fmt.Sprintf("invalid route %d <- %d", c.Dest, c.Src)}
		}
		out, err := st.VideoOutputs()
		if err != nil {
			return err
		}
		in, err := st.VideoInputs()
		if err != nil {
			return err
		}
		if out != 0 && c.Dest >= out || in != 0 && c.Src >= in {
			return &Error{Msg: fmt.Sprintf("route %d <- %d outside this hub (%dx%d)", c.Dest, c.Src, in, out)}
		}
	case InputLabel:
		n, err := st.VideoInputs()
		if err != nil {
			return err
		}
		return checkLabel("input labels", n, c.Index, c.Label)
	case OutputLabel:
		n, err := st.VideoOutputs()
		if err != nil {
			return err
		}
		return checkLabel("output labels", n, c.Index, c.Label)
	}
	return nil
}

func checkLabel(block string, count, index int, label string) error {
	if !utf8.ValidString(label) {
		return &Error{Msg: "label is not UTF-8"}
	}
	if utf8.RuneCountInString(CleanLabel(label)) > MaxLabel {
		return &Error{Msg: "label too long (max 64 characters)"}
	}
	if index < 0 || count != 0 && index >= count {
		return &Error{Msg: fmt.Sprintf("%s index %d outside this hub", block, index)}
	}
	return nil
}

// what is how a timeout's and a NAK's messages name a command.
func what(c Command) (timeout, refused string) {
	switch c := c.(type) {
	case Route:
		return fmt.Sprintf("timed out waiting for ACK routing %d <- %d", c.Dest, c.Src),
			fmt.Sprintf("hub refused route %d <- %d (destination locked?)", c.Dest, c.Src)
	case InputLabel:
		return fmt.Sprintf("timed out waiting for ACK renaming input labels %d", c.Index),
			fmt.Sprintf("hub refused label change on input labels %d", c.Index)
	case OutputLabel:
		return fmt.Sprintf("timed out waiting for ACK renaming output labels %d", c.Index),
			fmt.Sprintf("hub refused label change on output labels %d", c.Index)
	}
	return "timed out waiting for PING ack", ""
}

// acked is what an ACK changes at once, before the hub's own broadcast:
// the route, or the label as sent.
func acked(c Command, st *State) {
	switch c := c.(type) {
	case Route:
		st.Routing[c.Dest] = c.Src
	case InputLabel:
		st.InputLabels[c.Index] = CleanLabel(c.Label)
	case OutputLabel:
		st.OutputLabels[c.Index] = CleanLabel(c.Label)
	}
}
