// SPDX-License-Identifier: MIT

package engine

// What a destructive command would do, for a person to confirm. A
// request without Yes is refused (CodeDestructive) as ever; one that
// says a person can be asked (Ask) gets, first, a Confirm note: what it
// would affect, as the switcher's state says, and the question to put.
// The CLI asks, and sends the request again with Yes on "y".

import (
	"fmt"

	"github.com/lucas-romanenko/bmdctl/atem/profile"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Confirm is what a destructive command would affect and the question a
// person is asked: "slot 1 holds "logo", used by MP1 on program", "clear
// it?".
type Confirm struct {
	Impact   string `json:"impact"`
	Question string `json:"question"`
}

// refuse refuses a destructive command for want of Yes, noting first,
// when a person can be asked, what it would affect.
func (c *call) refuse(impact, question, format string, args ...any) error {
	if c.req.Ask {
		c.note(Note{Confirm: &Confirm{Impact: impact, Question: question}})
	}
	return codeErr(CodeDestructive, format, args...)
}

// refuseEntry refuses a destructive entry without Yes, saying what it
// would affect.
func (c *call) refuseEntry() error {
	impact, question := c.impact()
	return c.refuse(impact, question, "%s %s is destructive: add --yes to run it", c.req.Device, c.req.Path)
}

// impact is what a destructive entry would affect, and the question.
func (c *call) impact() (string, string) {
	st := c.state
	switch c.entry.Path {
	case "media clear", "media clear-unlocked":
		slot := ix(c.args, "slot")
		if !st.MediaplayerSlotInfo(slot).IsUsed {
			return fmt.Sprintf("slot %v is empty", table.Index(slot)), "clear it?"
		}
		return c.stillImpact(slot), "clear it?"
	case "macro delete":
		slot, err := c.macroSlot()
		if err != nil {
			return "", "delete it?"
		}
		return c.macroImpact(slot), "delete it?"
	case "video-mode":
		to := videoModeName(int(c.args.Value("mode").(uint64)))
		if m := st.CurrentVideoMode(); m != nil {
			return fmt.Sprintf("video mode %s → %s: every output resyncs", videoModeName(m.ID), to), "change it?"
		}
		return "video mode → " + to + ": every output resyncs", "change it?"
	case "profile restore":
		what := fmt.Sprint(c.args.Value("file"))
		if p, err := profile.FromXML(c.req.Files["file"]); err == nil {
			what += fmt.Sprintf(" (%s, %s)", p.Product(), p.VideoMode())
		}
		return what + " rewrites " + SwitcherName(st) + "'s setup", "restore it?"
	case "startup save":
		return SwitcherName(st) + " starts up as it is now, from now on", "save it?"
	case "startup clear":
		return SwitcherName(st) + " starts up with its factory settings", "clear it?"
	}
	return c.req.Path + " can't be undone", "do it?"
}

// stillImpact is what a still slot holds and who shows it: "slot 1 holds
// "logo", used by MP1 on program".
func (c *call) stillImpact(slot int) string {
	st := c.state
	s := fmt.Sprintf("slot %v holds %q", table.Index(slot), st.MediaplayerSlotInfo(slot).Name)
	for i, p := range st.MediaPlayers {
		if p == nil {
			continue
		}
		if v := st.MediaplayerSelected(i); v.SourceType == 1 && v.Slot == slot {
			s += fmt.Sprintf(", used by MP%v", table.Index(i))
			switch src := 3000 + 10*(i+1); {
			case len(st.MEs) > 0 && st.ProgramSource(0) == src:
				s += " on program"
			case len(st.MEs) > 0 && st.PreviewSource(0) == src:
				s += " on preview"
			}
			break
		}
	}
	return s
}

// macroImpact is what a macro slot holds: "macro 4 is "Open", running".
func (c *call) macroImpact(slot int) string {
	e := c.state.MacroEntry(slot)
	s := fmt.Sprintf("macro %v is %q", table.Index(slot), e.Name)
	if p := c.state.Macros.PlayStatus; p != nil && p.Running && int(p.Index) == slot {
		s += ", running"
	}
	return s
}
