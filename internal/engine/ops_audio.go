// SPDX-License-Identifier: MIT

package engine

// The audio meter readout. The switcher streams meter levels only to a
// client that asked (SFLN on a Fairlight mixer, SALN on the classic one),
// and atemwire stores the Fairlight strips' levels bare, so the last
// strip's levels overwrite the others (SURFACE.md §5.4 item 6). The
// readout therefore turns the stream on, collects each packet as it
// arrives for a moment, and turns the stream off again.
//
// Where a command exists for both mixers (master, levels, meters), audio
// sends the one the switcher has and classic-audio forces the classic one.

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func audioOperations() []operation {
	forSecs := []table.Param{{Name: "for", Kind: table.Float, Default: "0.5", Unit: "seconds",
		Help: "how long to collect levels; the last of each strip's is shown"}}
	return []operation{
		{entry: table.Entry{Path: "audio meters",
			Summary: "Read the audio meters: each strip's and the master's levels in dB, from whichever mixer the switcher has.",
			Params:  forSecs},
			run: opAudioMeters},
		{entry: table.Entry{Path: "classic-audio meters",
			Summary: "Read the classic audio mixer's meters: each input's and the master's levels in dB.",
			Params:  forSecs},
			run: opAudioMeters},
	}
}

// Mixer is the audio mixer the switcher reports: "fairlight" when it
// sent the Fairlight mixer's capabilities (_FAC, which atemwire keeps
// raw) and "classic" when it sent the classic mixer's (_AMC); failing
// both, the mixer whose state it sent; "" when it sent neither. Its
// topology (_top) says nothing about audio.
func Mixer(st atem.State) string {
	_, fac := st.Raw["_FAC"]
	_, amc := st.Raw["_AMC"]
	switch {
	case fac:
		return "fairlight"
	case amc:
		return "classic"
	case st.Fairlight.Master != nil || len(st.Fairlight.Inputs) > 0:
		return "fairlight"
	case st.Audio.Master != nil || len(st.Audio.Inputs) > 0:
		return "classic"
	}
	return ""
}

// classicFor is the classic mixer's command each audio command sends
// instead on a switcher whose mixer is the classic one.
var classicFor = map[string]string{
	"audio master": "classic-audio master",
	"audio levels": "classic-audio levels",
}

// forMixer turns an audio command the classic mixer has too into the
// classic one's, on a switcher with that mixer: the same arguments, with
// --volume converted from dB to the classic mixer's scale
// (atem.LegacyDBToVolume, bmdwire v1.6.0), and a Fairlight-only one
// refused.
func (c *call) forMixer(r *table.Registry) error {
	path, ok := classicFor[c.entry.Path]
	if !ok || Mixer(c.state) != "classic" {
		return nil
	}
	classic := r.Find(c.entry.Device, path)
	raw := map[string]string{}
	for name, v := range c.req.Params {
		if !slices.ContainsFunc(classic.Params, func(p table.Param) bool { return p.Name == name }) {
			return codeErr(CodeUsage, "--%s: the switcher's audio mixer is the classic one, and %s has no --%s", name, path, name)
		}
		raw[name] = v
	}
	if c.args.Has("volume") {
		db := c.args.Value("volume").(float64)
		raw["volume"] = strconv.Itoa(int(atem.LegacyDBToVolume(&db)))
	}
	args, err := classic.Parse(raw, c.res)
	if err != nil {
		return usageErr(err)
	}
	c.entry, c.args = classic, args
	return nil
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
	mixer := Mixer(st)
	if c.entry.Path == "classic-audio meters" {
		mixer = "classic"
	}
	var on, off atem.Command
	switch mixer {
	case "fairlight":
		r.Mixer, on, off = "fairlight", atem.SendFairlightLevels{Enable: true}, atem.SendFairlightLevels{}
	case "classic":
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
		for _, src := range slices.Sorted(maps.Keys(classic.Input)) {
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
