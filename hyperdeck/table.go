// SPDX-License-Identifier: MIT

package hyperdeck

// The command table's HyperDeck entries that are one line each: what
// bmdctl hyperdeck play and the rest send. The engine adds those that
// read the deck first (goto, loop, clips, status, watch).

import (
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// TableCommand is a command as the table builds it: its line, with the
// newline, is its bytes.
type TableCommand struct{ Command }

// Encode is the line as sent.
func (c TableCommand) Encode() []byte { return []byte(c.Line() + "\n") }

// Validate is the command's own check, and the line-break check every
// command gets.
func (c TableCommand) Validate() error {
	if v, ok := c.Command.(Validator); ok {
		return v.Validate()
	}
	return checkLine(c.Command)
}

func intPtr(a table.Args, name string) *int {
	if !a.Has(name) {
		return nil
	}
	var n int
	switch v := a.Value(name).(type) {
	case int64:
		n = int(v)
	case uint64:
		n = int(v)
	}
	return &n
}

func boolPtr(a table.Args, name string) *bool {
	if !a.Has(name) {
		return nil
	}
	b := a.Value(name).(bool)
	return &b
}

// ack is what a deck answers a command it takes, the only confirmation
// it gives, and the one every command gets.
const ack = "200 ok"

// RegisterCommands adds the HyperDeck's wire commands to r.
func RegisterCommands(r *table.Registry) error {
	speedMin, speedMax := table.Bounds(-5000, 5000)
	one, most := table.Bounds(1, 255)
	clipMin, clipMax := table.Bounds(1, 1<<31-1)
	if err := r.AddGroups("hyperdeck", Groups...); err != nil {
		return err
	}
	return addGrouped(r,
		table.Entry{Device: "hyperdeck", Path: "play", Echo: ack, Summary: "Start playback, or change the one under way.",
			Params: []table.Param{
				{Name: "speed", Kind: table.Int, Min: speedMin, Max: speedMax, Unit: "%",
					Help: "100 is normal speed, 200 double, -100 backwards, 0 holds the frame"},
				{Name: "loop", Kind: table.Bool, Help: "loop: the timeline, or the clip with --single-clip; false plays to the end"},
				{Name: "single-clip", Kind: table.Bool, Help: "play the current clip only"},
				{Name: "clip", Kind: table.Uint, Min: clipMin, Max: clipMax, Help: "start at this timeline clip (clips lists them)"},
			},
			Build: func(a table.Args) (table.Command, error) {
				return TableCommand{Play{Speed: intPtr(a, "speed"), Loop: boolPtr(a, "loop"),
					SingleClip: boolPtr(a, "single-clip"), ClipID: intPtr(a, "clip")}}, nil
			},
			Examples: []string{"play", "play --speed 200", "play --clip 2 --loop --single-clip"}},
		table.Entry{Device: "hyperdeck", Path: "stop", Echo: ack, Summary: "Stop playback or recording; the deck holds the last frame.",
			Build:    func(table.Args) (table.Command, error) { return TableCommand{Stop{}}, nil },
			Examples: []string{"stop"}},
		table.Entry{Device: "hyperdeck", Path: "pause", Echo: ack, Summary: "Hold the current frame: play at speed 0, which the deck reports as stopped.",
			Build:    func(table.Args) (table.Command, error) { return TableCommand{Pause{}}, nil },
			Examples: []string{"pause"}},
		table.Entry{Device: "hyperdeck", Path: "record", Echo: ack, Summary: "Start recording on the active slot, into a clip named as given.",
			Syntax: "record [name]",
			Params: []table.Param{{Name: "name", Kind: table.String,
				Help: "the clip's name; the deck names it from its record prefix otherwise"}},
			Build: func(a table.Args) (table.Command, error) {
				name, _ := a.Value("name").(string)
				return TableCommand{Record{Name: name}}, nil
			},
			Unverified: true,
			Examples:   []string{"record", `record "Interview 2"`}},
		table.Entry{Device: "hyperdeck", Path: "next", Echo: ack, Summary: "Go to the start of the next clip on the timeline.",
			Build:      func(table.Args) (table.Command, error) { return TableCommand{GotoClipBy{1}}, nil },
			Unverified: true,
			Examples:   []string{"next"}},
		table.Entry{Device: "hyperdeck", Path: "prev", Echo: ack, Summary: "Go to the start of the previous clip on the timeline.",
			Build:      func(table.Args) (table.Command, error) { return TableCommand{GotoClipBy{-1}}, nil },
			Unverified: true,
			Examples:   []string{"prev"}},
		table.Entry{Device: "hyperdeck", Path: "slot", Echo: ack, Summary: "Make a slot the active one: its disk is what plays and records.",
			Params: []table.Param{{Name: "slot", Kind: table.Uint, Required: true, Min: one, Max: most,
				Help: "the slot, as the deck numbers them: 1 or 2 on a HyperDeck Studio"}},
			Build: func(a table.Args) (table.Command, error) {
				return TableCommand{SlotSelect{SlotID: int(a.Value("slot").(uint64))}}, nil
			},
			Examples: []string{"slot 2"}},
	)
}

// addGrouped adds entries, each in its group (GroupOf).
func addGrouped(r *table.Registry, es ...table.Entry) error {
	for i := range es {
		es[i].Group = GroupOf(es[i].Path)
	}
	return r.Add(es...)
}
