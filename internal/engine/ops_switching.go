// SPDX-License-Identifier: MIT

package engine

// The switching operations that read the state before they send:
// atemwire's set_transition_rate, toggle_transition_key and
// toggle_transition_background (messages/transition.py, bmdwire v1.3.0),
// and the transition style and next-transition layers set on their own.

import (
	"slices"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Parameters the operations share with the wire commands.
var (
	pME = func() table.Param {
		lo, hi := table.Bounds(0, 255)
		return table.Param{Name: "me", Kind: table.Uint, Min: lo, Max: hi, Default: "1", OneBased: true,
			Help: "M/E"}
	}()
	transitionStyles = map[string]uint64{"mix": 0, "dip": 1, "wipe": 2, "dve": 3, "stinger": 4}
	layerNames       = map[string]uint64{"background": 1, "key1": 2, "key2": 4, "key3": 8, "key4": 16}
	unitFrames       = "frames, or seconds:frames at the display rate"
)

func switchingOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "transition style", Summary: "Set the transition style.", Echo: "TrSS:<me>",
			Params: []table.Param{pME, {Name: "style", Kind: table.Enum, Names: transitionStyles, Required: true, Help: "transition style"}}},
			run: opTransitionStyle},
		{entry: table.Entry{Path: "transition next", Summary: "Set the layers the next transition takes, or toggle one.", Echo: "TrSS:<me>",
			Params: []table.Param{pME,
				{Name: "next", Kind: table.Flags, Names: layerNames, Help: "the layers, replacing the selection (not none)"},
				{Name: "toggle", Kind: table.Enum, Names: layerNames, Help: "flip one layer in the current selection"}}},
			run: opTransitionNext},
		{entry: table.Entry{Path: "transition rate", Summary: "Set the rate of the M/E's current transition style.",
			Echo:   "TMxP, TDpP, TWpP, TDvP or TStP:<me>, by style",
			Params: []table.Param{pME, {Name: "rate", Kind: table.Rate, Unit: unitFrames, Required: true, Help: "rate"}}},
			run: opTransitionRate},
	}
}

func meArg(c *call) uint8 { return uint8(c.args.Value("me").(uint64)) }

func opTransitionStyle(c *call) (any, bool, error) {
	style := uint8(c.args.Value("style").(uint64))
	return c.sendBuilt(atem.TransitionSettings{ME: meArg(c), Style: &style})
}

// opTransitionNext sets the next-transition layers outright
// (set_next_transition_layers) or flips one (toggle_transition_key,
// toggle_transition_background). atemwire sends nothing when a toggle
// would leave no layer selected; here that is an error, since a command
// line asked for something.
func opTransitionNext(c *call) (any, bool, error) {
	me := meArg(c)
	hasNext, hasToggle := c.args.Has("next"), c.args.Has("toggle")
	if hasNext == hasToggle {
		return nil, false, codeErr(CodeUsage, "give --next or --toggle, not both or neither")
	}
	var mask uint64
	if hasNext {
		mask = c.args.Value("next").(uint64)
	} else {
		sel := c.state.TransitionSelection(int(me))
		for i, on := range []bool{sel.Background, sel.Key1, sel.Key2, sel.Key3, sel.Key4} {
			if on {
				mask |= 1 << i
			}
		}
		mask ^= c.args.Value("toggle").(uint64)
		if mask == 0 {
			return nil, false, codeErr(CodeUsage, "toggling that off would leave the next transition no layer")
		}
	}
	if mask > 0xFF {
		return nil, false, codeErr(CodeUsage, "--next %d does not fit", mask)
	}
	m := uint8(mask)
	next := c.entry.Params[slicesIndex(c.entry.Params, "next")]
	fps := atem.DisplayFPS(c.state)
	change := Value{Param: "next", Before: ptr(showValue(next, selection(c.state, int(me)), c.state, fps)),
		After: showValue(next, mask, c.state, fps)}
	return c.sendBuilt(atem.TransitionSettings{ME: me, NextTransition: &m}, change)
}

// opTransitionRate is set_transition_rate: the rate goes to whichever
// style the M/E has now (mix when the switcher hasn't said).
func opTransitionRate(c *call) (any, bool, error) {
	me := meArg(c)
	frames := c.args.Value("rate").(int64)
	style := c.state.TransitionStyle(int(me))
	limit := int64(0xFF)
	if style == 4 {
		limit = 0xFFFF
	}
	if frames < 0 || frames > limit {
		return nil, false, codeErr(CodeUsage, "%s: %d frames does not fit a %s rate (0-%d)", c.entry.Label("rate"), frames, nameOr(styleNames, style), limit)
	}
	r8, r16 := uint8(frames), uint16(frames)
	var cmd atem.Command
	switch style {
	case 0:
		cmd = atem.MixSettings{ME: me, Rate: r8}
	case 1:
		cmd = atem.DipSettings{ME: me, Rate: &r8}
	case 2:
		cmd = atem.WipeSettings{ME: me, Rate: &r8}
	case 3:
		cmd = atem.DVESettings{ME: me, Rate: &r8}
	case 4:
		cmd = atem.StingerSettings{ME: me, Rate: &r16}
	default:
		return nil, false, codeErr(CodeUsage, "unknown transition style %d", style)
	}
	fps := c.args.FPS
	if fps <= 0 {
		fps = atem.DisplayFPS(c.state)
	}
	change := Value{Param: "rate", Before: ptr(atem.FormatRate(c.state.ActiveTransitionRate(int(me)), fps)),
		After: atem.FormatRate(int(frames), fps)}
	return c.sendBuilt(cmd, change)
}

// sendBuilt validates a command an operation built and sends it, its
// result shaped like a wire command's. change, if given, is what it
// changes, for the note; else the note is its arguments'.
func (c *call) sendBuilt(cmd atem.Command, change ...Value) (any, bool, error) {
	if v, ok := cmd.(atem.Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, false, usageErr(err)
		}
	}
	held := len(change) == 0 && c.alreadyHeld() ||
		len(change) > 0 && !slices.ContainsFunc(change, func(v Value) bool { return v.Before == nil || *v.Before != v.After })
	if held {
		// The switcher holds it already: nothing to send.
		c.noteValues(true, change)
		return CommandResult{Command: cmd.Opcode(), Echo: cmd.EchoKey(), Args: c.displayArgs(), Already: true}, true, nil
	}
	undoable := c.remember()
	r, confirmed, err := c.send(cmd)
	r.Args = c.displayArgs()
	if err != nil {
		return nil, false, err
	}
	undoable()
	c.noteValues(confirmed, change)
	return r, confirmed, nil
}
