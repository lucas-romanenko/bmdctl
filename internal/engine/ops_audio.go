// SPDX-License-Identifier: MIT

package engine

// The audio meter readout. The switcher streams meter levels only to a
// client that asked (SFLN on a Fairlight mixer, SALN on the classic one),
// and atemwire stores the Fairlight strips' levels bare, so the last
// strip's levels overwrite the others (SURFACE.md §5.4 item 6). The
// readout therefore turns the stream on, collects each packet as it
// arrives for a moment, and turns the stream off again.

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func audioOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "audio meters",
			Summary: "Read the audio meters: each strip's and the master's levels in dB, Fairlight or classic.",
			Params: []table.Param{{Name: "for", Kind: table.Float, Default: "0.5", Unit: "seconds",
				Help: "how long to collect levels; the last of each strip's is shown"}}},
			run: opAudioMeters},
	}
}

// Meter is one strip's (or the master's) levels, in dB.
type Meter struct {
	Strip  string      `json:"strip,omitempty"` // Fairlight strip id; the source on the classic mixer
	Name   string      `json:"name,omitempty"`
	Level  [2]float64  `json:"level"` // left, right: the fader's output on Fairlight
	Peak   [2]float64  `json:"peak"`
	Input  *[4]float64 `json:"input,omitempty"`  // Fairlight: level L R, peak L R before processing
	Output *[4]float64 `json:"output,omitempty"` // Fairlight: after dynamics, before the fader
	// Gain reduction, Fairlight only.
	ExpanderGR   *float64 `json:"expander_gr,omitempty"`
	CompressorGR *float64 `json:"compressor_gr,omitempty"`
	LimiterGR    *float64 `json:"limiter_gr,omitempty"`
}

// MetersResult is a meter readout.
type MetersResult struct {
	Mixer   string  `json:"mixer"` // fairlight or classic
	Seconds float64 `json:"seconds"`
	Master  *Meter  `json:"master"` // nil if no master levels arrived
	Monitor *Meter  `json:"monitor,omitempty"`
	Strips  []Meter `json:"strips"`
}

func opAudioMeters(c *call) (any, bool, error) {
	st := c.state
	secs := c.args.Value("for").(float64)
	if !(secs > 0 && secs <= 60) {
		return nil, false, codeErr(CodeUsage, "--for %v: give 0 to 60 seconds", secs)
	}
	r := MetersResult{Seconds: secs, Strips: []Meter{}}
	var on, off atem.Command
	switch {
	case st.Fairlight.Master != nil || len(st.Fairlight.Inputs) > 0:
		r.Mixer, on, off = "fairlight", atem.SendFairlightLevels{Enable: true}, atem.SendFairlightLevels{}
	case st.Audio.Master != nil || len(st.Audio.Inputs) > 0:
		r.Mixer, on, off = "classic", atem.SendAudioLevels{Enable: true}, atem.SendAudioLevels{}
	default:
		return nil, false, codeErr(CodeUsage, "the switcher has reported no audio mixer")
	}
	changes, cancel := c.sess.Subscribe()
	defer cancel()
	if err := c.sess.Send(on); err != nil {
		return nil, false, sessionErr(err)
	}
	defer c.sess.Send(off)
	strips := map[string]*atem.FairlightMeterLevelsField{}
	var master *atem.FairlightMasterLevelsField
	var classic *atem.AudioMeterLevelsField
	deadline := time.NewTimer(time.Duration(secs * float64(time.Second)))
	defer deadline.Stop()
collect:
	for {
		select {
		case <-c.ctx.Done():
			return nil, false, codeErr(CodeUnreachable, "%v", c.ctx.Err())
		case <-deadline.C:
			break collect
		case ch, ok := <-changes:
			if !ok {
				return nil, false, codeErr(CodeUnreachable, "the session closed")
			}
			switch v := ch.Value.(type) {
			case *atem.FairlightMeterLevelsField:
				strips[v.StripID] = v
			case *atem.FairlightMasterLevelsField:
				master = v
			case *atem.AudioMeterLevelsField:
				classic = v
			}
		}
	}
	if r.Mixer == "fairlight" {
		if master != nil {
			r.Master = &Meter{Name: "Master", Level: [2]float64{master.Level[0], master.Level[1]},
				Peak: [2]float64{master.Level[2], master.Level[3]}, Input: &master.Input, Output: &master.Output,
				CompressorGR: &master.CompressorGR, LimiterGR: &master.LimiterGR}
		}
		seen := map[string]bool{}
		add := func(id, name string) {
			v := strips[id]
			if v == nil || seen[id] {
				return
			}
			seen[id] = true
			r.Strips = append(r.Strips, Meter{Strip: id, Name: name, Level: [2]float64{v.Level[0], v.Level[1]},
				Peak: [2]float64{v.Level[2], v.Level[3]}, Input: &v.Input, Output: &v.Output,
				ExpanderGR: &v.ExpanderGR, CompressorGR: &v.CompressorGR, LimiterGR: &v.LimiterGR})
		}
		for _, s := range st.FairlightStrips() {
			add(s.StripID, s.DisplayNameLong)
		}
		for _, id := range sortedKeysStr(strips) { // strips the state hasn't described
			add(id, "")
		}
		return r, false, nil
	}
	if classic != nil {
		r.Master = &Meter{Name: "Master", Level: [2]float64{classic.Master[0], classic.Master[1]},
			Peak: [2]float64{classic.Master[2], classic.Master[3]}}
		r.Monitor = &Meter{Name: "Monitor", Level: [2]float64{classic.Monitor[0], classic.Monitor[1]},
			Peak: [2]float64{classic.Monitor[2], classic.Monitor[3]}}
		for _, src := range sortedKeysU16(classic.Input) {
			v := classic.Input[src]
			r.Strips = append(r.Strips, Meter{Strip: strconv.Itoa(int(src)), Name: sourceOf(st, int(src)).Name,
				Level: [2]float64{v[0], v[1]}, Peak: [2]float64{v[2], v[3]}})
		}
	}
	return r, false, nil
}

func sortedKeysStr[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	// Strip ids sort by source, then subchannel.
	slices.SortFunc(ks, func(a, b string) int {
		as, ac, _ := strings.Cut(a, ".")
		bs, bc, _ := strings.Cut(b, ".")
		ai, _ := strconv.Atoi(as)
		bi, _ := strconv.Atoi(bs)
		if ai != bi {
			return ai - bi
		}
		return strings.Compare(ac, bc)
	})
	return ks
}

func sortedKeysU16[V any](m map[uint16]V) []uint16 {
	ks := make([]uint16, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.SortFunc(ks, func(a, b uint16) int { return int(a) - int(b) })
	return ks
}
