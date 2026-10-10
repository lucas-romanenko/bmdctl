// SPDX-License-Identifier: MIT

package engine

// The system operations: the switcher's clock read, and its video modes.

import (
	"fmt"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func systemOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "time", Summary: "Read the switcher's clock: ask for it (TiRq) and wait for the Time packet.",
			Echo: "Time"}, run: opTime},
		{entry: table.Entry{Path: "video-modes", Summary: "List the video modes the switcher supports, the current one marked."},
			run: opVideoModes},
	}
}

// TimeResult is the switcher's clock.
type TimeResult struct {
	Timecode  string `json:"timecode"` // hh:mm:ss:ff, ";" before the frames when drop-frame
	DropFrame bool   `json:"drop_frame"`
	Mode      string `json:"mode,omitempty"` // free run or time of day (TCCc)
}

func opTime(c *call) (any, bool, error) {
	changes, cancel := c.sess.Subscribe()
	defer cancel()
	if err := c.sess.Send(atem.TimeRequest{}); err != nil {
		return nil, false, sessionErr(err)
	}
	timer := time.NewTimer(c.req.timeout())
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return nil, false, codeErr(CodeUnreachable, "%v", c.ctx.Err())
		case <-timer.C:
			return nil, false, codeErr(CodeNoEcho, "asked, but the switcher did not report its Time within %v", c.req.timeout())
		case ch, ok := <-changes:
			if !ok {
				return nil, false, codeErr(CodeUnreachable, "the session closed")
			}
			t, isTime := ch.Value.(*atem.TimeField)
			if !isTime {
				continue
			}
			sep := ":"
			if t.DropFrame {
				sep = ";"
			}
			r := TimeResult{Timecode: fmt.Sprintf("%02d:%02d:%02d%s%02d", t.Hours, t.Minutes, t.Seconds, sep, t.Frames), DropFrame: t.DropFrame}
			if cfg := c.sess.State().Time.Config; cfg != nil {
				r.Mode = map[uint8]string{0: "free run", 1: "time of day"}[cfg.Mode]
			}
			return r, true, nil
		}
	}
}

// VideoModeEntry is one supported video mode.
type VideoModeEntry struct {
	ID      int    `json:"id"`
	Name    string `json:"name"` // as --mode takes it
	Current bool   `json:"current,omitempty"`
}

func opVideoModes(c *call) (any, bool, error) {
	out := []VideoModeEntry{}
	cur := -1
	if m := c.state.VideoMode.Mode; m != nil {
		cur = int(m.Mode)
	}
	for _, v := range c.state.AvailableVideoModes() {
		out = append(out, VideoModeEntry{ID: v.ID, Name: videoModeName(v.ID), Current: v.ID == cur})
	}
	return out, false, nil
}

// videoModeName is the name --mode takes for a mode: the first of
// atemwire's VIDEO_MODE_NAMES for it, preferring the one that reads as a
// format ("1080p50" over an alias).
func videoModeName(id int) string {
	best := ""
	for name, v := range atem.VideoModeNames {
		if int(v) != id {
			continue
		}
		if best == "" || strings.IndexAny(name, "0123456789") == 0 && strings.IndexAny(best, "0123456789") != 0 ||
			(strings.IndexAny(name, "0123456789") == 0) == (strings.IndexAny(best, "0123456789") == 0) && name < best {
			best = name
		}
	}
	if best == "" {
		return fmt.Sprint(id)
	}
	return best
}

// checkVideoMode refuses a video mode the switcher says it can't run,
// before it drops everything to try.
func checkVideoMode(c *call) error {
	modes := c.state.AvailableVideoModes()
	if len(modes) == 0 {
		return nil // the switcher hasn't said
	}
	want := int(c.args.Value("mode").(uint64))
	var names []string
	for _, m := range modes {
		if m.ID == want {
			return nil
		}
		names = append(names, videoModeName(m.ID))
	}
	return codeErr(CodeUsage, "--mode %s: the switcher supports %s", videoModeName(want), strings.Join(names, ", "))
}
