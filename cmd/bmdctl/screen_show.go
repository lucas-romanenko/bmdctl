// SPDX-License-Identifier: MIT

package main

// show's view of one area (engine.ShowResult), in the layout of status:
// a label in the verb column, then the area's rows. A pipe gets the same
// rows without colour, under its header.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	screenFormats["show"] = screenShow
	humanFormats["show"] = func(p *printer, r engine.Response, raw json.RawMessage) []string { return screenShow(p, r, raw).rows }
	reports["show"] = true
}

func screenShow(p *printer, r engine.Response, raw json.RawMessage) view {
	var s engine.ShowResult
	json.Unmarshal(raw, &s)
	c := p.c
	v := view{about: []string{s.Area}, gap: true}
	empty := func(what string) []string { return []string{indent + c.colour(dim, what)} }
	// Keyers' and DSKs' rows: a label, what each is, how it is set dim
	// after that in one column, and its on-air state at the end of the
	// line.
	var labels, whats, hows []string
	var ons []bool
	keyed := func() []string {
		w := 0
		for _, what := range whats {
			w = max(w, width(what))
		}
		var out []string
		for i, what := range whats {
			if hows[i] != "" {
				what = padRight(what, w) + "  " + c.colour(dim, hows[i])
			}
			out = append(out, trailing(labelled(labels[i], what), c.onAir(ons[i])))
		}
		return out
	}
	switch s.Area {
	case "keys":
		var all []engine.KeyerStatus
		for _, m := range s.Keys {
			for _, k := range m.Keyers {
				all = append(all, k.KeyerStatus)
			}
		}
		typeWidth := keyerTypeWidth(all)
		for _, m := range s.Keys {
			for i, k := range m.Keyers {
				label := ""
				if i == 0 {
					label = fmt.Sprintf("ME %v", m.ME)
				}
				var how []string
				if k.Next {
					how = append(how, "next")
				}
				if k.Fly {
					how = append(how, "fly")
				}
				if k.Masked {
					how = append(how, "masked")
				}
				if k.Type == "luma" {
					if k.Premultiplied {
						how = append(how, "premultiplied")
					} else {
						how = append(how, fmt.Sprintf("clip %s gain %s", num(k.Clip), num(k.Gain)))
					}
				}
				labels = append(labels, label)
				whats = append(whats, keyerRow(k.KeyerStatus, typeWidth))
				hows = append(hows, strings.Join(how, " · "))
				ons = append(ons, k.OnAir)
			}
		}
		v.rows = keyed()
		if len(v.rows) == 0 {
			v.rows = empty("no upstream keyers")
		}
	case "dsk":
		for _, d := range s.DSKs {
			how := []string{d.RateText}
			if d.Tie {
				how = append(how, "tie")
			}
			if d.Masked {
				how = append(how, "masked")
			}
			if d.Premultiplied {
				how = append(how, "premultiplied")
			} else {
				how = append(how, fmt.Sprintf("clip %s gain %s", num(d.Clip), num(d.Gain)))
			}
			labels = append(labels, fmt.Sprintf("dsk %v", d.DSK))
			whats = append(whats, short(d.Fill)+" / "+short(d.Key))
			hows = append(hows, strings.Join(how, " · "))
			ons = append(ons, d.OnAir)
		}
		v.rows = keyed()
		if len(v.rows) == 0 {
			v.rows = empty("no downstream keyers")
		}
	case "transition":
		for _, t := range s.Transitions {
			state := "idle"
			if t.InTransition {
				state = fmt.Sprintf("%d%%", t.Position/100)
			}
			if t.Preview {
				state += " · previewing"
			}
			v.rows = append(v.rows, labelled(fmt.Sprintf("ME %v", t.ME),
				strings.Join([]string{t.Style + " " + t.RateText, "next " + strings.Join(t.Next, "+"), state}, " · ")))
			var cells [][]string
			for _, st := range t.Styles {
				cells = append(cells, []string{strings.Repeat(" ", verbWidth), st.Style, st.RateText, strings.Join(st.Settings, " · ")})
			}
			ftb := "off"
			if t.FTB.Active {
				ftb = c.colour(red, "black")
			}
			cells = append(cells, []string{strings.Repeat(" ", verbWidth), "FTB", t.FTBRate, ftb})
			v.rows = append(v.rows, rows(cells)...)
		}
	case "audio":
		a := s.Audio
		if a == nil || a.Mixer == "none" {
			v.rows = empty("no audio mixer")
			break
		}
		v.about = append(v.about, a.Mixer)
		var cells [][]string
		strips := a.Strips
		if a.Master != nil {
			strips = append([]engine.StripView{*a.Master}, strips...)
		}
		for _, st := range strips {
			db := "-inf"
			if st.VolumeDB != nil {
				db = fmt.Sprintf("%.1f", *st.VolumeDB)
			}
			mix := st.Mix
			if mix == "off" {
				mix = c.colour(dim, mix)
			}
			cells = append(cells, []string{padRight(st.Name, verbWidth), padLeft(db, 6) + " dB", mix})
		}
		v.rows = rows(cells)
	case "media":
		if s.Media != nil {
			raw, _ := json.Marshal(s.Media)
			v = screenMediaList(p, r, raw)
			v.about = append([]string{"media"}, v.about...)
		}
	case "macros":
		if s.Macros != nil {
			raw, _ := json.Marshal(s.Macros)
			v = screenMacroList(p, r, raw)
			v.about = append([]string{"macros"}, v.about...)
		}
	case "aux":
		v.rows = auxRows(s.Aux)
		if len(s.Aux) == 0 {
			v.rows = empty("no aux outputs")
		}
	case "hyperdecks":
		var cells [][]string
		for _, h := range s.HyperDecks {
			cells = append(cells, []string{c.colour(dim, padLeft(h.Slot.String(), 2)), h.Address, short(h.Input)})
		}
		v.rows = rows(cells)
		if len(cells) == 0 {
			v.rows = empty("no HyperDecks bound · bmdctl hyperdeck-slot binds one")
		}
	}
	return v
}

// num is a setting as a person reads it: 50, 12.5.
func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
