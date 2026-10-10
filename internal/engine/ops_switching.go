// SPDX-License-Identifier: MIT

package engine

// The switching operations that read the state before they send:
// atemwire's set_transition_rate, toggle_transition_key and
// toggle_transition_background (messages/transition.py, bmdwire v1.3.0),
// and the transition style and next-transition layers set on their own.

import (
	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Parameters the operations share with the wire commands.
var (
	pME = func() table.Param {
		lo, hi := table.Bounds(0, 255)
		return table.Param{Name: "me", Kind: table.Uint, Min: lo, Max: hi, Default: "1", OneBased: true,
			Help: "M/E (the library's ME field counts from 0)"}
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
	return c.sendBuilt(atem.TransitionSettings{ME: me, NextTransition: &m})
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
		return nil, false, codeErr(CodeUsage, "--rate: %d frames does not fit a %s rate (0-%d)", frames, nameOr(styleNames, style), limit)
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
	return c.sendBuilt(cmd)
}

// sendBuilt validates a command an operation built and sends it, its
// result shaped like a wire command's.
func (c *call) sendBuilt(cmd atem.Command) (any, bool, error) {
	if v, ok := cmd.(atem.Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, false, usageErr(err)
		}
	}
	r, confirmed, err := c.send(cmd)
	r.Args = c.displayArgs()
	if err != nil {
		return nil, false, err
	}
	return r, confirmed, nil
}
