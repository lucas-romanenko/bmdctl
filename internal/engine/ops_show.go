// SPDX-License-Identifier: MIT

package engine

// show: one area of the switcher read through atemwire's readers and
// laid out for a person (the CLI formats it); status is the short
// overview of them all. bmdwire v1.3.0 has the readers, not the views.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// showAreas are what show shows.
var showAreas = map[string]uint64{"keys": 0, "dsk": 1, "transition": 2, "audio": 3, "media": 4, "macros": 5, "aux": 6, "hyperdecks": 7}

func showOperation() operation {
	return operation{entry: table.Entry{Path: "show",
		Summary: "Show one area of the switcher: keys, dsk, transition, audio, media, macros, aux or hyperdecks.",
		Params: []table.Param{
			{Name: "area", Kind: table.Enum, Names: showAreas, Required: true,
				Help: "keys, dsk, transition, audio, media, macros, aux or hyperdecks"},
			{Name: "all", Kind: table.Bool, Default: "false", Help: "media and macros: the empty slots too, at a terminal"}}},
		run: opShow}
}

// ShowResult is one area; the field named by Area is set.
type ShowResult struct {
	Area        string           `json:"area"`
	Keys        []MEKeys         `json:"keys,omitempty"`
	DSKs        []DSKView        `json:"dsks,omitempty"`
	Transitions []TransitionView `json:"transitions,omitempty"`
	Audio       *AudioView       `json:"audio,omitempty"`
	Media       *MediaList       `json:"media,omitempty"`
	Macros      *MacroList       `json:"macros,omitempty"`
	Aux         []AuxStatus      `json:"aux,omitempty"`
	HyperDecks  []HyperDeckView  `json:"hyperdecks,omitempty"`
}

// MEKeys are one M/E's upstream keyers.
type MEKeys struct {
	ME     table.Index `json:"me"`
	Keyers []KeyView   `json:"keyers"`
}

// KeyView is one upstream keyer: KeyerStatus, and how it is set.
type KeyView struct {
	KeyerStatus
	Next          bool    `json:"next"` // in the next transition
	Fly           bool    `json:"fly"`
	Masked        bool    `json:"masked"`
	Premultiplied bool    `json:"premultiplied,omitempty"` // luma
	Clip          float64 `json:"clip,omitempty"`
	Gain          float64 `json:"gain,omitempty"`
}

// DSKView is one downstream keyer: DSKStatus, and how it is set.
type DSKView struct {
	DSKStatus
	RateText      string  `json:"rate_text"`
	Masked        bool    `json:"masked"`
	Premultiplied bool    `json:"premultiplied"`
	Clip          float64 `json:"clip"`
	Gain          float64 `json:"gain"`
}

// TransitionView is one M/E's transition: the next one, each style's
// settings, and fade to black.
type TransitionView struct {
	ME table.Index `json:"me"`
	TransitionStatus
	Styles  []StyleView `json:"styles"`
	FTB     FTBStatus   `json:"ftb"`
	FTBRate string      `json:"ftb_rate"`
}

// StyleView is one transition style's settings, as a line says them.
type StyleView struct {
	Style    string   `json:"style"`
	RateText string   `json:"rate_text"`
	Settings []string `json:"settings,omitempty"`
}

// AudioView is the audio mixer: its master and a strip per input.
type AudioView struct {
	Mixer  string      `json:"mixer"` // fairlight, classic, none
	Master *StripView  `json:"master,omitempty"`
	Strips []StripView `json:"strips"`
}

// StripView is one strip: its name, level in dB (nil when silenced),
// and whether it is on (on, off, afv).
type StripView struct {
	Name     string   `json:"name"`
	Source   int      `json:"source"`
	VolumeDB *float64 `json:"volume_db"`
	Mix      string   `json:"mix,omitempty"`
}

// HyperDeckView is one HyperDeck slot the switcher has bound.
type HyperDeckView struct {
	Slot    table.Index `json:"slot"`
	Address string      `json:"address"`
	Input   Source      `json:"input"`
}

func opShow(c *call) (any, bool, error) {
	st := c.state
	area := ""
	for n, v := range showAreas {
		if v == c.args.Value("area").(uint64) {
			area = n
		}
	}
	r := ShowResult{Area: area}
	fps := atem.DisplayFPS(st)
	rate := func(frames int) string { return atem.FormatRate(frames, fps) }
	switch area {
	case "keys":
		for me := range st.MECount() {
			mk := MEKeys{ME: table.Index(me), Keyers: []KeyView{}}
			sel := st.TransitionSelection(me)
			next := []bool{sel.Key1, sel.Key2, sel.Key3, sel.Key4}
			for k := range st.MEKeyerCount(me) {
				v := KeyView{KeyerStatus: KeyerStatus{Keyer: table.Index(k), OnAir: st.USKOnAir(me, k), Type: nameOr(keyTypeNames, st.USKType(me, k)),
					Fill: sourceOf(st, st.USKFillSource(me, k)), Key: sourceOf(st, st.USKKeySource(me, k))},
					Fly: st.USKFly(me, k).FlyEnabled, Masked: st.USKMask(me, k).MaskEnabled, Next: k < len(next) && next[k]}
				if v.Type == "luma" {
					l := st.USKLuma(me, k)
					v.Premultiplied, v.Clip, v.Gain = l.LumaPreMultiplied, l.LumaClip, l.LumaGain
				}
				mk.Keyers = append(mk.Keyers, v)
			}
			r.Keys = append(r.Keys, mk)
		}
	case "dsk":
		for d := range st.DSKCount() {
			v := st.DSKState(d)
			r.DSKs = append(r.DSKs, DSKView{DSKStatus: DSKStatus{DSK: table.Index(d), OnAir: v.OnAir, Tie: v.Tie, InTransition: v.InTransition,
				Rate: v.Rate, Fill: sourceOf(st, v.FillSource), Key: sourceOf(st, v.KeySource)},
				RateText: rate(v.Rate), Masked: v.MaskEnabled, Premultiplied: v.PreMultiplied, Clip: v.Clip, Gain: v.Gain})
		}
	case "transition":
		status, _, _ := opStatus(c)
		for i, m := range status.(StatusResult).MEs {
			me := i
			v := TransitionView{ME: m.ME, TransitionStatus: m.Transition, FTB: m.FTB, FTBRate: rate(m.FTB.Rate)}
			dve := "none"
			if s := st.DVEStyle(me); s != nil {
				if dve = c.enumName("transition dve", "style", *s); dve == strconv.Itoa(*s) {
					dve = "style " + dve // one atemwire's tables don't name
				}
			}
			v.Styles = []StyleView{
				{Style: "mix", RateText: rate(st.MixRate(me))},
				{Style: "dip", RateText: rate(st.DipRate(me)), Settings: []string{sourceName(st, st.DipSource(me))}},
				{Style: "wipe", RateText: rate(st.WipeRate(me)), Settings: []string{c.enumName("transition wipe", "pattern", st.WipePattern(me)),
					fmt.Sprintf("width %s", strconv.FormatFloat(st.WipeWidth(me), 'f', -1, 64))}},
				{Style: "dve", RateText: rate(st.DVERate(me)), Settings: []string{dve}},
				{Style: "stinger", RateText: rate(st.StingerRate(me)), Settings: []string{fmt.Sprintf("MP%d", st.StingerSource(me)),
					"clip " + rate(st.StingerClipDuration(me))}},
			}
			r.Transitions = append(r.Transitions, v)
		}
	case "audio":
		r.Audio = audioView(st)
	case "media":
		m, _, _ := opMediaList(c)
		ml := m.(MediaList)
		r.Media = &ml
	case "macros":
		c.args.Set("all", true) // every slot: a terminal counts the empty ones
		m, _, _ := opMacroList(c)
		ml := m.(MacroList)
		r.Macros = &ml
	case "aux":
		status, _, _ := opStatus(c)
		r.Aux = status.(StatusResult).Aux
	case "hyperdecks":
		var slots []int
		for k := range st.HyperDecks {
			slots = append(slots, int(k))
		}
		slices.Sort(slots)
		r.HyperDecks = []HyperDeckView{}
		for _, s := range slots {
			if h := st.HyperdeckSettings(s); h.Configured {
				r.HyperDecks = append(r.HyperDecks, HyperDeckView{Slot: table.Index(s), Address: h.NetworkAddress, Input: sourceOf(st, h.Input)})
			}
		}
	}
	return r, false, nil
}

// audioView is the mixer the switcher has, a strip per input.
func audioView(st atem.State) *AudioView {
	v := &AudioView{Mixer: "none", Strips: []StripView{}}
	switch Mixer(st) {
	case "fairlight":
		v.Mixer = "fairlight"
		if m := st.FairlightMaster(); m.Present {
			v.Master = &StripView{Name: "master", VolumeDB: m.VolumeDB}
			if m.AFV {
				v.Master.Mix = "afv"
			}
		}
		strips := st.FairlightStrips()
		slices.SortStableFunc(strips, func(a, b atem.FairlightStripView) int { return a.Source - b.Source })
		for _, s := range strips {
			name := strings.TrimSpace(s.DisplayName)
			if name == "" {
				name = sourceName(st, s.Source)
			}
			v.Strips = append(v.Strips, StripView{Name: name, Source: s.Source, VolumeDB: s.VolumeDB, Mix: mixName(s.MixType)})
		}
	case "classic":
		// The legacy mixer's readers (bmdwire v1.6.0), which name a media
		// player MP<n> and an external input by its source, where InPr
		// names the video source of the same id (2001 is Color 1).
		v.Mixer = "classic"
		if m := st.LegacyAudioMaster(); m.Present {
			v.Master = &StripView{Name: "master", VolumeDB: m.VolumeDB}
			if m.AFV {
				v.Master.Mix = "afv"
			}
		}
		strips := st.LegacyAudioStrips()
		slices.SortStableFunc(strips, func(a, b atem.LegacyAudioStripView) int { return a.Source - b.Source })
		for _, s := range strips {
			name := strings.TrimSpace(s.ShortName)
			if name == "" {
				name = sourceName(st, s.Source)
			}
			v.Strips = append(v.Strips, StripView{Name: name, Source: s.Source, VolumeDB: s.VolumeDB, Mix: mixName(s.MixType)})
		}
	}
	return v
}

// mixName is a Fairlight strip's mix option as a line says it.
func mixName(t string) string {
	switch t {
	case "AudioFollowVideo":
		return "afv"
	}
	return strings.ToLower(t)
}

// enumName is an enum's value by the name a parameter of path takes for
// it, else the number.
func (c *call) enumName(path, param string, v int) string {
	if e := c.e.cfg.Registry.Find("atem", path); e != nil {
		for _, p := range e.Params {
			if p.Name != param {
				continue
			}
			for _, n := range p.SortedNames() {
				if p.Names[n] == uint64(v) {
					return n
				}
			}
		}
	}
	return strconv.Itoa(v)
}
