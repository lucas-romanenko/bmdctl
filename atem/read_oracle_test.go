// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// readersOracle is internal/fixtures/oracle/readers.json: scenarios of
// packets fed to bmdwire v1.3.0's AtemProtocol.save_field_data, with the
// readers called in between and what they returned.
type readersOracle struct {
	Bmdwire   string   `json:"bmdwire"`
	Readers   []string `json:"readers"`
	Helpers   []string `json:"helpers"`
	Scenarios []struct {
		Name  string       `json:"name"`
		Steps []readerStep `json:"steps"`
	} `json:"scenarios"`
}

// readerStep is one packet fed (Feed, Payload) or one call (Call, Args,
// Kwargs) and its Result (JSON, absent when atemwire raised Error).
type readerStep struct {
	Feed    string          `json:"feed"`
	Payload string          `json:"payload"`
	Call    string          `json:"call"`
	Args    []any           `json:"args"`
	Kwargs  map[string]any  `json:"kwargs"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
}

// readerArgs are a call's Python arguments, numbers as json.Number.
type readerArgs struct {
	pos []any
	kw  map[string]any
}

func (a readerArgs) num(i int) int { return jsonInt(a.pos[i]) }

func (a readerArgs) text(i int) string { return a.pos[i].(string) }

func (a readerArgs) hex(i int) []byte {
	b, err := hex.DecodeString(a.text(i))
	if err != nil {
		panic(err)
	}
	return b
}

// textKw is keyword argument name, or def (the Python default) if absent.
func (a readerArgs) textKw(name, def string) string {
	if v, ok := a.kw[name]; ok {
		return v.(string)
	}
	return def
}

func jsonInt(v any) int {
	n, err := v.(json.Number).Int64()
	if err != nil {
		panic(err)
	}
	return int(n)
}

// readerCalls maps each atemwire reader, and each state.py helper the
// readers use, to the Go method or function that ports it.
var readerCalls = map[string]func(State, readerArgs) any{
	// messages/switching.py
	"program_source": func(s State, a readerArgs) any { return s.ProgramSource(a.num(0)) },
	"preview_source": func(s State, a readerArgs) any { return s.PreviewSource(a.num(0)) },
	"aux_source":     func(s State, a readerArgs) any { return s.AuxSource(a.num(0)) },
	// messages/transition.py
	"transition_style":            func(s State, a readerArgs) any { return s.TransitionStyle(a.num(0)) },
	"transition_in_transition":    func(s State, a readerArgs) any { return s.TransitionInTransition(a.num(0)) },
	"transition_position":         func(s State, a readerArgs) any { return s.TransitionPosition(a.num(0)) },
	"transition_frames_remaining": func(s State, a readerArgs) any { return s.TransitionFramesRemaining(a.num(0)) },
	"transition_selection":        func(s State, a readerArgs) any { return s.TransitionSelection(a.num(0)) },
	"mix_rate":                    func(s State, a readerArgs) any { return s.MixRate(a.num(0)) },
	"dip_rate":                    func(s State, a readerArgs) any { return s.DipRate(a.num(0)) },
	"dip_source":                  func(s State, a readerArgs) any { return s.DipSource(a.num(0)) },
	"active_transition_rate":      func(s State, a readerArgs) any { return s.ActiveTransitionRate(a.num(0)) },
	"wipe_rate":                   func(s State, a readerArgs) any { return s.WipeRate(a.num(0)) },
	"wipe_pattern":                func(s State, a readerArgs) any { return s.WipePattern(a.num(0)) },
	"wipe_fill_source":            func(s State, a readerArgs) any { return s.WipeFillSource(a.num(0)) },
	"wipe_position_x":             func(s State, a readerArgs) any { return s.WipePositionX(a.num(0)) },
	"wipe_position_y":             func(s State, a readerArgs) any { return s.WipePositionY(a.num(0)) },
	"wipe_softness":               func(s State, a readerArgs) any { return s.WipeSoftness(a.num(0)) },
	"wipe_symmetry":               func(s State, a readerArgs) any { return s.WipeSymmetry(a.num(0)) },
	"wipe_width":                  func(s State, a readerArgs) any { return s.WipeWidth(a.num(0)) },
	"wipe_reverse":                func(s State, a readerArgs) any { return s.WipeReverse(a.num(0)) },
	"wipe_flip_flop":              func(s State, a readerArgs) any { return s.WipeFlipFlop(a.num(0)) },
	"dve_rate":                    func(s State, a readerArgs) any { return s.DVERate(a.num(0)) },
	"dve_fill_source":             func(s State, a readerArgs) any { return s.DVEFillSource(a.num(0)) },
	"dve_key_source":              func(s State, a readerArgs) any { return s.DVEKeySource(a.num(0)) },
	"dve_enable_key":              func(s State, a readerArgs) any { return s.DVEEnableKey(a.num(0)) },
	"dve_clip":                    func(s State, a readerArgs) any { return s.DVEClip(a.num(0)) },
	"dve_gain":                    func(s State, a readerArgs) any { return s.DVEGain(a.num(0)) },
	"dve_pre_multiplied":          func(s State, a readerArgs) any { return s.DVEPreMultiplied(a.num(0)) },
	"dve_invert_key":              func(s State, a readerArgs) any { return s.DVEInvertKey(a.num(0)) },
	"dve_style":                   func(s State, a readerArgs) any { return s.DVEStyle(a.num(0)) },
	"dve_reverse":                 func(s State, a readerArgs) any { return s.DVEReverse(a.num(0)) },
	"dve_flip_flop":               func(s State, a readerArgs) any { return s.DVEFlipFlop(a.num(0)) },
	"stinger_rate":                func(s State, a readerArgs) any { return s.StingerRate(a.num(0)) },
	"stinger_source":              func(s State, a readerArgs) any { return s.StingerSource(a.num(0)) },
	"stinger_clip_duration":       func(s State, a readerArgs) any { return s.StingerClipDuration(a.num(0)) },
	"stinger_trigger_point":       func(s State, a readerArgs) any { return s.StingerTriggerPoint(a.num(0)) },
	"stinger_pre_roll":            func(s State, a readerArgs) any { return s.StingerPreRoll(a.num(0)) },
	"stinger_clip":                func(s State, a readerArgs) any { return s.StingerClip(a.num(0)) },
	"stinger_gain":                func(s State, a readerArgs) any { return s.StingerGain(a.num(0)) },
	"stinger_pre_multiplied":      func(s State, a readerArgs) any { return s.StingerPreMultiplied(a.num(0)) },
	"stinger_invert_key":          func(s State, a readerArgs) any { return s.StingerInvertKey(a.num(0)) },
	// messages/upstream_keyer.py
	"usk_on_air":      func(s State, a readerArgs) any { return s.USKOnAir(a.num(0), a.num(1)) },
	"usk_type":        func(s State, a readerArgs) any { return s.USKType(a.num(0), a.num(1)) },
	"usk_fill_source": func(s State, a readerArgs) any { return s.USKFillSource(a.num(0), a.num(1)) },
	"usk_key_source":  func(s State, a readerArgs) any { return s.USKKeySource(a.num(0), a.num(1)) },
	"usk_luma":        func(s State, a readerArgs) any { return s.USKLuma(a.num(0), a.num(1)) },
	"usk_chroma":      func(s State, a readerArgs) any { return s.USKChroma(a.num(0), a.num(1)) },
	"usk_dve":         func(s State, a readerArgs) any { return s.USKDVE(a.num(0), a.num(1)) },
	"usk_pattern":     func(s State, a readerArgs) any { return s.USKPattern(a.num(0), a.num(1)) },
	"usk_mask":        func(s State, a readerArgs) any { return s.USKMask(a.num(0), a.num(1)) },
	"usk_fly":         func(s State, a readerArgs) any { return s.USKFly(a.num(0), a.num(1)) },
	"usk_fly_keyframe": func(s State, a readerArgs) any {
		return s.USKFlyKeyframe(a.num(0), a.num(1), a.text(2))
	},
	// messages/downstream_keyer.py, fade_to_black.py, color_generator.py
	"dsk_state":            func(s State, a readerArgs) any { return s.DSKState(a.num(0)) },
	"ftb_active":           func(s State, a readerArgs) any { return s.FTBActive(a.num(0)) },
	"ftb_in_transition":    func(s State, a readerArgs) any { return s.FTBInTransition(a.num(0)) },
	"ftb_frames_remaining": func(s State, a readerArgs) any { return s.FTBFramesRemaining(a.num(0)) },
	"ftb_rate":             func(s State, a readerArgs) any { return s.FTBRate(a.num(0)) },
	"ftb_disabled":         func(s State, a readerArgs) any { return s.FTBDisabled() },
	"color_generator":      func(s State, a readerArgs) any { return s.ColorGenerator(a.num(0)) },
	// messages/hyperdeck.py
	"hyperdeck_settings": func(s State, a readerArgs) any { return s.HyperdeckSettings(a.num(0)) },
	"plan_binding_push": func(s State, a readerArgs) any {
		var exclude []int
		if v, ok := a.kw["exclude_slots"]; ok {
			for _, x := range v.([]any) {
				exclude = append(exclude, jsonInt(x))
			}
		}
		slot, write := s.PlanBindingPush(a.text(0), a.num(1), exclude...)
		return []any{slot, write}
	},
	// messages/media.py, macros.py
	"mediaplayer_slots":     func(s State, a readerArgs) any { return s.MediaplayerSlots() },
	"mediaplayer_slot_info": func(s State, a readerArgs) any { return s.MediaplayerSlotInfo(a.num(0)) },
	"mediaplayer_selected":  func(s State, a readerArgs) any { return s.MediaplayerSelected(a.num(0)) },
	"macro_entry":           func(s State, a readerArgs) any { return s.MacroEntry(a.num(0)) },
	"macro_play_running":    func(s State, a readerArgs) any { return s.MacroPlayRunning() },
	"macro_play_index":      func(s State, a readerArgs) any { return s.MacroPlayIndex() },
	// messages/input_video.py
	"input_label":       func(s State, a readerArgs) any { return s.InputLabel(a.num(0)) },
	"all_input_sources": func(s State, a readerArgs) any { return s.AllInputSources() },
	"renamable_inputs":  func(s State, a readerArgs) any { return s.RenamableInputs() },
	// messages/fairlight.py
	"fairlight_master":       func(s State, a readerArgs) any { return s.FairlightMaster() },
	"fairlight_strips":       func(s State, a readerArgs) any { return s.FairlightStrips() },
	"fairlight_audio_inputs": func(s State, a readerArgs) any { return s.FairlightAudioInputs() },
	"fairlight_eq_bands": func(s State, a readerArgs) any {
		if a.pos[0] == nil {
			return s.FairlightEQBands(nil)
		}
		id := a.text(0)
		return s.FairlightEQBands(&id)
	},
	"fairlight_compressor":        func(s State, a readerArgs) any { return s.FairlightCompressor() },
	"fairlight_limiter":           func(s State, a readerArgs) any { return s.FairlightLimiter() },
	"fairlight_expander":          func(s State, a readerArgs) any { return s.FairlightExpander() },
	"fairlight_master_compressor": func(s State, a readerArgs) any { return s.FairlightMasterCompressor() },
	"fairlight_master_limiter":    func(s State, a readerArgs) any { return s.FairlightMasterLimiter() },
	"fairlight_headphones":        func(s State, a readerArgs) any { return s.FairlightHeadphones() },
	"fairlight_solo":              func(s State, a readerArgs) any { return s.FairlightSolo() },
	// messages/system_info.py
	"video_mode":            func(s State, a readerArgs) any { return s.CurrentVideoMode() },
	"available_video_modes": func(s State, a readerArgs) any { return s.AvailableVideoModes() },
	"video_resolution": func(s State, a readerArgs) any {
		def := [2]int{1920, 1080}
		if v, ok := a.kw["default"]; ok {
			def = [2]int{jsonInt(v.([]any)[0]), jsonInt(v.([]any)[1])}
		}
		return s.VideoResolution(def)
	},
	"product_name": func(s State, a readerArgs) any { return s.ProductName(a.textKw("default", "")) },
	"device_name":  func(s State, a readerArgs) any { return s.DeviceName(a.textKw("default", "")) },
	"sdi_3g_level": func(s State, a readerArgs) any { return s.SDI3GLevel(a.textKw("default", "LevelB")) },
	// state.py helpers
	"me_count":       func(s State, a readerArgs) any { return s.MECount() },
	"me_keyer_count": func(s State, a readerArgs) any { return s.MEKeyerCount(a.num(0)) },
	"_dsk_count":     func(s State, a readerArgs) any { return s.DSKCount() },
	"display_fps":    func(s State, a readerArgs) any { return DisplayFPS(s) },
	"decode_name":    func(s State, a readerArgs) any { return decodeName(string(a.hex(0))) },
	"md5_hex":        func(s State, a readerArgs) any { return md5Hex(MD5(a.hex(0))) },
}

// Every reader returns what atemwire's does, after the same packets: the
// same values (floats bit for bit), the same types (int or float, None or
// not), the same dict keys and list order.
func TestReadersMatchAtemwire(t *testing.T) {
	dec := json.NewDecoder(bytes.NewReader(fixtures.Bytes(t, "oracle/readers.json")))
	dec.UseNumber()
	var oracle readersOracle
	if err := dec.Decode(&oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Bmdwire != "v1.4.0" {
		t.Fatalf("oracle generated from bmdwire %s", oracle.Bmdwire)
	}
	if len(oracle.Readers) != 88 {
		t.Errorf("oracle lists %d readers, atemwire v1.3.0 has 88", len(oracle.Readers))
	}
	names := append(slices.Clone(oracle.Readers), oracle.Helpers...)
	for _, n := range names {
		if readerCalls[n] == nil {
			t.Errorf("%s: no Go counterpart", n)
		}
	}
	for n := range readerCalls {
		if !slices.Contains(names, n) {
			t.Errorf("%s: not an atemwire reader or helper", n)
		}
	}

	cases := map[string]int{}
	total := 0
	for _, sc := range oracle.Scenarios {
		st := newState()
		for i, step := range sc.Steps {
			where := fmt.Sprintf("%s step %d", sc.Name, i)
			if step.Feed != "" {
				payload, err := hex.DecodeString(step.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.store(step.Feed, payload); err != nil {
					t.Fatalf("%s: storing %s %s: %v", where, step.Feed, step.Payload, err)
				}
				continue
			}
			cases[step.Call]++
			total++
			where += " " + step.Call + fmt.Sprint(step.Args)
			if step.Kwargs != nil {
				where += fmt.Sprint(step.Kwargs)
			}
			if step.Error != "" {
				t.Errorf("%s: atemwire raises %s, which no Go reader does", where, step.Error)
				continue
			}
			want := decodeResult(t, step.Result)
			if step.Call == "fairlight_strips" {
				want = goStripOrder(want.([]any))
			}
			check := func(s State, label string) {
				got, err := callReader(s, step)
				if err != nil {
					t.Errorf("%s%s: %v", where, label, err)
					return
				}
				if d := readerDiff(got, want, ""); d != "" {
					t.Errorf("%s%s: %s", where, label, d)
				}
			}
			check(st, "")
			if sc.Name == "empty" {
				// A zero State, nil maps and all, reads like an empty one.
				check(State{}, " on State{}")
			}
		}
	}
	for _, n := range names {
		if cases[n] == 0 {
			t.Errorf("%s: no oracle cases", n)
		}
	}
	t.Logf("%d calls over %d readers and helpers in %d scenarios", total, len(cases), len(oracle.Scenarios))
}

func decodeResult(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// callReader runs the Go counterpart of step's call and normalizes its
// result; a panic is an error.
func callReader(s State, step readerStep) (got any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	fn := readerCalls[step.Call]
	if fn == nil {
		return nil, fmt.Errorf("no Go counterpart")
	}
	return readerValue(reflect.ValueOf(fn(s, readerArgs{pos: step.Args, kw: step.Kwargs}))), nil
}

// pyInt and pyFloat keep Python's int/float distinction through
// normalization: the oracle writes 1 for an int and 1.0 for a float.
type (
	pyInt   int64
	pyFloat float64
)

// readerValue turns a reader's Go result into the oracle's JSON shape.
func readerValue(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return readerValue(v.Elem())
	case reflect.Struct:
		out := map[string]any{}
		for i := range v.NumField() {
			out[v.Type().Field(i).Tag.Get("json")] = readerValue(v.Field(i))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		for it := v.MapRange(); it.Next(); {
			out[fmt.Sprint(it.Key().Interface())] = readerValue(it.Value())
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := []any{}
		for i := range v.Len() {
			out = append(out, readerValue(v.Index(i)))
		}
		return out
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return pyInt(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return pyInt(v.Uint())
	case reflect.Float64:
		return pyFloat(v.Float())
	}
	panic("readerValue: " + v.Kind().String())
}

// readerDiff compares a normalized Go result with the oracle's, reporting
// the first difference. Floats must match bit for bit, sign of zero
// included: no reader goes through a libm function, only +, -, *, / and
// round().
func readerDiff(got, want any, path string) string {
	switch w := want.(type) {
	case nil:
		if got != nil {
			return fmt.Sprintf("%s: got %#v, want None", path, got)
		}
	case json.Number:
		s := w.String()
		if strings.ContainsAny(s, ".eEIN") { // Python's repr of a float
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return fmt.Sprintf("%s: oracle float %q: %v", path, s, err)
			}
			g, ok := got.(pyFloat)
			if !ok || math.Float64bits(float64(g)) != math.Float64bits(f) {
				return fmt.Sprintf("%s: got %#v, want float %s", path, got, s)
			}
			return ""
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Sprintf("%s: oracle int %q: %v", path, s, err)
		}
		if g, ok := got.(pyInt); !ok || int64(g) != i {
			return fmt.Sprintf("%s: got %#v, want int %s", path, got, s)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: got %#v, want %d items", path, got, len(w))
		}
		for i := range w {
			if d := readerDiff(g[i], w[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: got %#v, want a dict", path, got)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				return fmt.Sprintf("%s: Go has key %q, atemwire doesn't", path, k)
			}
		}
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range w {
				if !yield(k) {
					return
				}
			}
		}) {
			gv, ok := g[k]
			if !ok {
				return fmt.Sprintf("%s: Go lacks key %q", path, k)
			}
			if d := readerDiff(gv, w[k], path+"."+k); d != "" {
				return d
			}
		}
	default: // bool, string
		if got != want {
			return fmt.Sprintf("%s: got %#v, want %#v", path, got, want)
		}
	}
	return ""
}

// goStripOrder puts atemwire's fairlight_strips result in the Go order.
// atemwire sorts by (_ai_type, _ai_number, subchannel), stably over the
// order the strips first arrived in; the state doesn't keep that order, so
// FairlightStrips breaks ties by source and strip id. A stable re-sort by
// the full Go key reorders only within atemwire's ties.
func goStripOrder(strips []any) []any {
	out := slices.Clone(strips)
	field := func(s any, k string) int { return jsonInt(s.(map[string]any)[k]) }
	slices.SortStableFunc(out, func(a, b any) int {
		return cmp.Or(
			cmp.Compare(field(a, "_ai_type"), field(b, "_ai_type")),
			cmp.Compare(field(a, "_ai_number"), field(b, "_ai_number")),
			cmp.Compare(field(a, "subchannel"), field(b, "subchannel")),
			cmp.Compare(field(a, "source"), field(b, "source")),
			strings.Compare(a.(map[string]any)["strip_id"].(string), b.(map[string]any)["strip_id"].(string)))
	})
	return out
}
