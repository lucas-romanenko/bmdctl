// SPDX-License-Identifier: MIT

package engine

// Notes: what a request tells a person besides its result (protocol.go,
// Note). A command's values are read from the state before it is sent,
// through the atemwire readers status uses, in the units the command takes
// (bmdwire v1.3.0 pairs each reader with the operation that sets the same
// value, so the units agree); after it, from the state its echo left.
// docs/PORTING.md, "Output conventions", says how the CLI shows them.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// note sends n to the caller, if it asked for notes.
func (c *call) note(n Note) {
	if c.req.Notes {
		c.out.note(n)
	}
}

// reader reads from a switcher's state the value of each parameter a
// command sets, by parameter name, as Args would hold it had it been
// typed: uint64 for sources, enums, flags and indexes (counted from 0),
// int64 for rates and integers, float64, bool or string.
type reader func(st atem.State, a table.Args) map[string]any

// readers are the commands whose values the state holds, by path. A
// command without one shows what it sent alone.
var readers = map[string]reader{
	"program": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.ProgramSource(ix(a, "me")))}
	},
	"preview": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.PreviewSource(ix(a, "me")))}
	},
	"aux": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.AuxSource(ix(a, "aux")))}
	},
	"transition settings": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		return map[string]any{"style": u(st.TransitionStyle(me)), "next": selection(st, me)}
	},
	"transition style": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"style": u(st.TransitionStyle(ix(a, "me")))}
	},
	"transition mix": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"rate": int64(st.MixRate(ix(a, "me")))}
	},
	"transition dip": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		return map[string]any{"rate": int64(st.DipRate(me)), "source": u(st.DipSource(me))}
	},
	"transition wipe": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		return map[string]any{"rate": int64(st.WipeRate(me)), "pattern": u(st.WipePattern(me)), "width": st.WipeWidth(me),
			"source": u(st.WipeFillSource(me)), "symmetry": st.WipeSymmetry(me), "softness": st.WipeSoftness(me),
			"position-x": st.WipePositionX(me), "position-y": st.WipePositionY(me), "reverse": st.WipeReverse(me),
			"flip-flop": st.WipeFlipFlop(me)}
	},
	"transition dve": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		m := map[string]any{"rate": int64(st.DVERate(me)), "fill-source": u(st.DVEFillSource(me)),
			"key-source": u(st.DVEKeySource(me)), "key-enable": st.DVEEnableKey(me), "premultiplied": st.DVEPreMultiplied(me),
			"clip": st.DVEClip(me), "gain": st.DVEGain(me), "invert": st.DVEInvertKey(me), "reverse": st.DVEReverse(me),
			"flip-flop": st.DVEFlipFlop(me)}
		if s := st.DVEStyle(me); s != nil {
			m["style"] = u(*s)
		}
		return m
	},
	"transition stinger": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		return map[string]any{"player": u(st.StingerSource(me)), "premultiplied": st.StingerPreMultiplied(me),
			"clip": st.StingerClip(me), "gain": st.StingerGain(me), "invert": st.StingerInvertKey(me),
			"pre-roll": int64(st.StingerPreRoll(me)), "duration": int64(st.StingerClipDuration(me)),
			"trigger": int64(st.StingerTriggerPoint(me)), "rate": int64(st.StingerRate(me))}
	},
	"transition preview": func(st atem.State, a table.Args) map[string]any {
		me := ix(a, "me")
		if me < len(st.MEs) && st.MEs[me].Transition.Preview != nil {
			return map[string]any{"enabled": st.MEs[me].Transition.Preview.Enabled}
		}
		return nil
	},
	"tbar": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"position": int64(st.TransitionPosition(ix(a, "me")))}
	},
	"usk on-air": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"on-air": st.USKOnAir(ix(a, "me"), ix(a, "keyer"))}
	},
	"usk fill": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.USKFillSource(ix(a, "me"), ix(a, "keyer")))}
	},
	"usk key": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.USKKeySource(ix(a, "me"), ix(a, "keyer")))}
	},
	"usk type": func(st atem.State, a table.Args) map[string]any {
		me, k := ix(a, "me"), ix(a, "keyer")
		return map[string]any{"type": u(st.USKType(me, k)), "fly": st.USKFly(me, k).FlyEnabled}
	},
	"usk luma": func(st atem.State, a table.Args) map[string]any {
		v := st.USKLuma(ix(a, "me"), ix(a, "keyer"))
		return map[string]any{"premultiplied": v.LumaPreMultiplied, "clip": v.LumaClip, "gain": v.LumaGain, "invert": v.LumaInvert}
	},
	"usk pattern": func(st atem.State, a table.Args) map[string]any {
		v := st.USKPattern(ix(a, "me"), ix(a, "keyer"))
		return map[string]any{"pattern": u(v.PatternStyle), "size": v.PatternSize, "symmetry": v.PatternSymmetry,
			"softness": v.PatternSoftness, "position-x": v.PatternPositionX, "position-y": v.PatternPositionY,
			"invert": v.PatternInvert}
	},
	"usk mask": func(st atem.State, a table.Args) map[string]any {
		v := st.USKMask(ix(a, "me"), ix(a, "keyer"))
		return map[string]any{"enabled": v.MaskEnabled, "top": v.MaskTop, "bottom": v.MaskBottom, "left": v.MaskLeft, "right": v.MaskRight}
	},
	"dsk on-air": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"on-air": st.DSKState(ix(a, "dsk")).OnAir}
	},
	"dsk tie": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"tie": st.DSKState(ix(a, "dsk")).Tie}
	},
	"dsk rate": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"rate": int64(st.DSKState(ix(a, "dsk")).Rate)}
	},
	"dsk fill": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.DSKState(ix(a, "dsk")).FillSource)}
	},
	"dsk key": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"source": u(st.DSKState(ix(a, "dsk")).KeySource)}
	},
	"dsk gain": func(st atem.State, a table.Args) map[string]any {
		v := st.DSKState(ix(a, "dsk"))
		return map[string]any{"premultiplied": v.PreMultiplied, "clip": v.Clip, "gain": v.Gain, "invert": v.InvertKey}
	},
	"dsk mask": func(st atem.State, a table.Args) map[string]any {
		v := st.DSKState(ix(a, "dsk"))
		return map[string]any{"enabled": v.MaskEnabled, "top": v.MaskTop, "bottom": v.MaskBottom, "left": v.MaskLeft, "right": v.MaskRight}
	},
	"ftb rate": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"rate": int64(st.FTBRate(ix(a, "me")))}
	},
	"ftb enable": func(st atem.State, a table.Args) map[string]any {
		return map[string]any{"enabled": !st.FTBDisabled()}
	},
	"color": func(st atem.State, a table.Args) map[string]any {
		v := st.ColorGenerator(ix(a, "generator"))
		return map[string]any{"hue": v.Hue, "saturation": v.Saturation, "luma": v.Luma}
	},
	"media player": func(st atem.State, a table.Args) map[string]any {
		v := st.MediaplayerSelected(ix(a, "player"))
		switch v.SourceType {
		case 1:
			return map[string]any{"still": u(v.Slot)}
		case 2:
			return map[string]any{"clip": u(v.Slot)}
		}
		return nil
	},
	"multiviewer input": func(st atem.State, a table.Args) map[string]any {
		mv, w := ix(a, "multiviewer"), ix(a, "window")
		if mv < len(st.Multiviewers) && w < len(st.Multiviewers[mv].Windows) {
			if in := st.Multiviewers[mv].Windows[w].Input; in != nil {
				return map[string]any{"source": uint64(in.Source)}
			}
		}
		return nil
	},
	"input rename": func(st atem.State, a table.Args) map[string]any {
		if !a.Has("source") {
			return nil
		}
		l := st.InputLabel(int(a.Value("source").(uint64)))
		return map[string]any{"long-name": l.Long, "short-name": l.Short}
	},
	"video-mode": func(st atem.State, a table.Args) map[string]any {
		if m := st.CurrentVideoMode(); m != nil {
			return map[string]any{"mode": u(m.ID)}
		}
		return nil
	},
	"audio master": func(st atem.State, a table.Args) map[string]any {
		v := st.FairlightMaster()
		if !v.Present {
			return nil
		}
		m := map[string]any{"eq": v.EQEnable, "eq-gain": v.EQGainDB, "makeup": v.DynamicsMakeupDB, "afv": v.AFV}
		if v.VolumeDB != nil {
			m["volume"] = *v.VolumeDB
		}
		return m
	},
}

// moves are what a command without values of its own changes, by path,
// before and as asked: a cut or an auto transition takes preview to
// program when the next transition takes the background. None has an
// echo: a cut or an auto is confirmed by what the switcher reports of it
// (call.go, effect), the others the CLI shows as sent.
var moves = map[string]func(c *call) []Value{
	"cut":  programMove,
	"auto": programMove,
	"ftb toggle": func(c *call) []Value {
		black := map[bool]string{true: "black", false: "off"}
		on := c.state.FTBActive(ix(c.args, "me"))
		return []Value{{Param: "ftb", Before: ptr(black[on]), After: black[!on]}}
	},
	"dsk auto": func(c *call) []Value {
		on := c.state.DSKState(ix(c.args, "dsk")).OnAir
		return []Value{{Param: "on-air", Before: ptr(onOff(on)), After: onOff(!on)}}
	},
}

func programMove(c *call) []Value {
	me := ix(c.args, "me")
	if !c.state.TransitionSelection(me).Background {
		return nil
	}
	prog, prev := c.state.ProgramSource(me), c.state.PreviewSource(me)
	return []Value{{Param: "program", Before: ptr(sourceName(c.state, prog)), After: sourceName(c.state, prev)}}
}

// noteOnAir sends M/E 1's program and preview as the request found them,
// before it does anything: a terminal's header says what was live when
// the command ran, read once.
func (c *call) noteOnAir() {
	st := c.state
	if !c.req.Notes || len(st.MEs) == 0 {
		return
	}
	prog, prev := st.ProgramSource(0), st.PreviewSource(0)
	c.note(Note{OnAir: &OnAir{Program: sourceName(st, prog), Preview: sourceName(st, prev), ProgramID: prog, PreviewID: prev}})
}

// commandValues are a wire command's values: every argument given, with
// its value before where the state holds it, and after from post, the
// state the command's echo left (nil: as sent).
func (c *call) commandValues(post *atem.State) []Value {
	var before, after map[string]any
	if r := readers[c.entry.Path]; r != nil {
		before = r(c.state, c.args)
		if post != nil {
			after = r(*post, c.args)
		}
	}
	vals := c.argValues(before, after)
	if m := moves[c.entry.Path]; m != nil {
		vals = append(vals, m(c)...)
	}
	return vals
}

// argValues shows each argument given, in the entry's order: its value in
// after if there, else as given, and its value in before.
func (c *call) argValues(before, after map[string]any) []Value {
	st := c.state
	fps := c.args.FPS
	if fps <= 0 {
		fps = atem.DisplayFPS(st)
	}
	var out []Value
	for _, p := range c.entry.Params {
		if !c.args.Has(p.Name) {
			continue
		}
		asked := showValue(p, c.args.Value(p.Name), st, fps)
		v := Value{Param: p.Name, After: asked}
		if x, ok := after[p.Name]; ok {
			v.After = showValue(p, x, st, fps)
		}
		if x, ok := before[p.Name]; ok {
			v.Before = ptr(showValue(p, x, st, fps))
			// The switcher answered, holding what it held: it didn't
			// take the value sent.
			_, reported := after[p.Name]
			v.Kept = reported && *v.Before == v.After && asked != v.After
		}
		out = append(out, v)
	}
	return out
}

// showValue is a value as a person reads it, the way watch shows one:
// an index counted from 1, a source by its short name, an enum by name,
// flags joined with +, on or off, a rate as seconds:frames, a string
// quoted.
func showValue(p table.Param, v any, st atem.State, fps int) string {
	switch x := v.(type) {
	case uint64:
		switch {
		case p.OneBased:
			return table.Index(x).String()
		case p.Kind == table.Source:
			return sourceName(st, int(x))
		case p.Kind == table.Enum:
			for _, n := range p.SortedNames() {
				if p.Names[n] == x {
					return n
				}
			}
		case p.Kind == table.Flags:
			var ns []string
			for _, n := range p.SortedNames() {
				if b := p.Names[n]; b != 0 && x&b == b {
					ns = append(ns, n)
				}
			}
			if len(ns) > 0 {
				return strings.Join(ns, "+")
			}
			return "none"
		}
	case int64:
		if p.Kind == table.Rate {
			return atem.FormatRate(int(x), fps)
		}
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return onOff(x)
	case string:
		return strconv.Quote(x)
	case time.Time:
		return x.Format(time.RFC3339)
	case []string:
		return strings.Join(x, ",")
	}
	return fmt.Sprint(v)
}

// sourceName is a source as watch shows it: its short name, else its long
// name, else its id.
func sourceName(st atem.State, id int) string {
	l := st.InputLabel(id)
	if s := strings.TrimSpace(l.Short); s != "" {
		return s
	}
	if s := strings.TrimSpace(l.Long); s != "" {
		return s
	}
	return strconv.Itoa(id)
}

// selection is an M/E's next-transition layers as TransitionSettings'
// mask.
func selection(st atem.State, me int) uint64 {
	sel := st.TransitionSelection(me)
	var mask uint64
	for i, on := range []bool{sel.Background, sel.Key1, sel.Key2, sel.Key3, sel.Key4} {
		if on {
			mask |= 1 << i
		}
	}
	return mask
}

// ix is an index argument's value, counted from 0; 0 when not given.
func ix(a table.Args, name string) int {
	if v, ok := a.Value(name).(uint64); ok {
		return int(v)
	}
	return 0
}

func u(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func ptr[T any](v T) *T { return &v }
