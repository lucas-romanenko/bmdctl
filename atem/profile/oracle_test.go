// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The oracle replays: internal/fixtures/oracle/profile.json, recorded from
// bmdwire v1.3.0 with bmdwire#22 to #24 merged (v1.3.0-9-g785aac7) by
// gen_profile.py, and the golden XML of test_profile_granular.py.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// entry is one mixerstate entry: key, index path, attributes.
type entry struct {
	Key   string
	Path  []any
	Attrs map[string]any
}

func (e *entry) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) != 3 {
		return fmt.Errorf("entry %s: %v", b, err)
	}
	for i, dst := range []any{&e.Key, &e.Path, &e.Attrs} {
		dec := json.NewDecoder(bytes.NewReader(raw[i]))
		dec.UseNumber()
		if err := dec.Decode(dst); err != nil {
			return err
		}
	}
	return nil
}

type saveCase struct {
	Name      string                       `json:"name"`
	State     []entry                      `json:"state"`
	StateFrom string                       `json:"state_from"` // the state of that save
	StandIn   bool                         `json:"stand_in"`
	Options   map[string]any               `json:"options"`
	Downloads map[string]map[string]string `json:"downloads"`
	Asked     []int                        `json:"asked"`
	XML       string                       `json:"xml"`
	Reparse   any                          `json:"reparse"`
}

type event struct {
	Cmd    *int     `json:"cmd,omitempty"`
	Drop   string   `json:"drop,omitempty"`
	Sleep  *float64 `json:"sleep,omitempty"`
	Upload *struct {
		Slot        int    `json:"slot"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Bytecode    string `json:"bytecode"`
	} `json:"upload,omitempty"`
}

type pyExc struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

type applyCase struct {
	Name        string            `json:"name"`
	XML         *string           `json:"xml"`
	FromSave    string            `json:"from_save"`
	Live        []entry           `json:"live"`
	Options     map[string]any    `json:"options"`
	Factory     string            `json:"factory"`
	Connected   bool              `json:"connected"`
	FailUploads map[string]string `json:"fail_uploads"`
	Events      []event           `json:"events"`
	Result      *struct {
		Applied []string `json:"applied"`
		Skipped []string `json:"skipped"`
		Errors  []string `json:"errors"`
		Summary string   `json:"summary"`
	} `json:"result"`
	Raised       *pyExc `json:"raised"`
	Macros       []macroJSON
	MacrosRaised *pyExc `json:"macros_raised"`
}

type macroJSON struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Ops         [][]any
}

type parseCase struct {
	XML   *string        `json:"xml"`
	Hex   string         `json:"hex"`
	Error *pyExc         `json:"error"`
	Out   *string        `json:"out"`
	Props map[string]any `json:"props"`
}

type profileOracle struct {
	Bmdwire string `json:"bmdwire"`
	Golden  struct {
		State []entry `json:"state"`
	} `json:"golden"`
	Saves    []saveCase  `json:"saves"`
	Applies  []applyCase `json:"applies"`
	Commands []string    `json:"commands"`
	Parses   []parseCase `json:"parses"`
}

var (
	oracleOnce sync.Once
	oracleData profileOracle
	oracleErr  error
)

func loadOracle(t *testing.T) *profileOracle {
	t.Helper()
	data := fixtures.Bytes(t, "oracle/profile.json")
	oracleOnce.Do(func() {
		oracleErr = json.Unmarshal(data, &oracleData)
		if oracleErr == nil && oracleData.Bmdwire != "v1.3.0-9-g785aac7" {
			oracleErr = fmt.Errorf("oracle generated from bmdwire %s", oracleData.Bmdwire)
		}
	})
	if oracleErr != nil {
		t.Fatal(oracleErr)
	}
	return &oracleData
}

// --- building an atem.State from mixerstate entries ----------------------------

// byteStrings are the string fields holding bytes atemwire leaves
// undecoded; the oracle has them in hex.
var byteStrings = map[string]bool{
	"MediaplayerFileInfoField.Name":    true,
	"MacroPropertiesField.Name":        true,
	"MacroPropertiesField.Description": true,
}

type builder struct {
	st      atem.State
	lenient bool // stand-in objects carry only some attributes
	// overflow lists the stand-in values no atem field can hold.
	overflow []string
}

func newBuilder(lenient bool) *builder {
	return &builder{lenient: lenient, st: atem.State{
		Inputs:     map[uint16]*atem.InputPropertiesField{},
		HyperDecks: map[uint16]*atem.HyperdeckSettingsField{},
		MediaPool:  atem.MediaPool{Files: map[uint16]*atem.MediaplayerFileInfoField{}},
		Macros:     atem.Macros{Properties: map[uint16]*atem.MacroPropertiesField{}, LastRun: -1},
		Fairlight: atem.Fairlight{
			MasterEQBands: map[uint8]*atem.AtemMasterEQBandPropertiesField{},
			Strips:        map[string]*atem.FairlightStrip{},
			Inputs:        map[uint16]*atem.FairlightAudioInputField{},
		},
	}}
}

// fill sets a packet struct's fields from attributes by JSON name.
func (b *builder) fill(dst any, attrs map[string]any, where string) error {
	v := reflect.ValueOf(dst).Elem()
	tn := v.Type().Name()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		tag := f.Tag.Get("json")
		val, ok := attrs[tag]
		if !ok {
			if b.lenient {
				continue
			}
			return fmt.Errorf("%s: no attribute %s", where, tag)
		}
		fv := v.Field(i)
		switch x := val.(type) {
		case nil:
			continue
		case bool:
			fv.SetBool(x)
		case json.Number:
			switch fv.Kind() {
			case reflect.Float64:
				n, _ := x.Float64()
				fv.SetFloat(n)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				n, err := x.Int64()
				if err != nil || fv.OverflowInt(n) {
					b.overflow = append(b.overflow, fmt.Sprintf("%s.%s=%s", where, tag, x))
					continue
				}
				fv.SetInt(n)
			case reflect.Uint8, reflect.Uint16, reflect.Uint32:
				n, err := x.Int64()
				if err != nil || n < 0 || fv.OverflowUint(uint64(n)) {
					b.overflow = append(b.overflow, fmt.Sprintf("%s.%s=%s", where, tag, x))
					continue
				}
				fv.SetUint(uint64(n))
			default:
				return fmt.Errorf("%s.%s: number for %s", where, tag, fv.Type())
			}
		case string:
			switch {
			case fv.Type() == reflect.TypeOf(atem.MD5{}):
				var h atem.MD5
				if err := h.UnmarshalText([]byte(x)); err != nil {
					return err
				}
				fv.Set(reflect.ValueOf(h))
			case byteStrings[tn+"."+f.Name]:
				raw, err := hex.DecodeString(x)
				if err != nil {
					return fmt.Errorf("%s.%s: %v", where, tag, err)
				}
				fv.SetString(string(raw))
			case fv.Kind() == reflect.String:
				fv.SetString(x)
			default:
				return fmt.Errorf("%s.%s: string for %s", where, tag, fv.Type())
			}
		default:
			return fmt.Errorf("%s.%s: can't set %T", where, tag, val)
		}
	}
	return nil
}

func pathInt(e entry, i int) (int, error) {
	if i >= len(e.Path) {
		return 0, fmt.Errorf("%s: path %v too short", e.Key, e.Path)
	}
	n, ok := e.Path[i].(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s: path %v", e.Key, e.Path)
	}
	v, err := n.Int64()
	return int(v), err
}

func growTo[T any](s []T, n int) []T {
	for len(s) <= n {
		var zero T
		s = append(s, zero)
	}
	return s
}

// place puts one entry where atem.State keeps that packet.
func (b *builder) place(e entry) error {
	s := &b.st
	where := fmt.Sprintf("%s%v", e.Key, e.Path)
	idx := func(i int) int {
		n, err := pathInt(e, i)
		if err != nil {
			panic(err)
		}
		return n
	}
	me := func() *atem.ME {
		m := idx(0)
		s.MEs = growTo(s.MEs, m)
		return &s.MEs[m]
	}
	usk := func() *atem.USK {
		m, k := me(), idx(1)
		m.USKs = growTo(m.USKs, k)
		return &m.USKs[k]
	}
	dsk := func() *atem.DSK {
		d := idx(0)
		s.DSKs = growTo(s.DSKs, d)
		return &s.DSKs[d]
	}
	strip := func() *atem.FairlightStrip {
		id, _ := e.Path[0].(string)
		if s.Fairlight.Strips[id] == nil {
			s.Fairlight.Strips[id] = &atem.FairlightStrip{EQBands: map[uint8]*atem.AtemEQBandPropertiesField{}}
		}
		return s.Fairlight.Strips[id]
	}
	window := func() *atem.MultiviewerWindow {
		mv, w := idx(0), idx(1)
		s.Multiviewers = growTo(s.Multiviewers, mv)
		s.Multiviewers[mv].Windows = growTo(s.Multiviewers[mv].Windows, w)
		return &s.Multiviewers[mv].Windows[w]
	}
	// set fills a new packet and stores it with put.
	set := func(dst any, put func()) error {
		if err := b.fill(dst, e.Attrs, where); err != nil {
			return err
		}
		put()
		return nil
	}
	switch e.Key {
	case "program-bus-input":
		v := &atem.ProgramBusInputField{}
		return set(v, func() { me().Program = v })
	case "preview-bus-input":
		v := &atem.PreviewBusInputField{}
		return set(v, func() { me().Preview = v })
	case "transition-settings":
		v := &atem.TransitionSettingsField{}
		return set(v, func() { me().Transition.Settings = v })
	case "transition-position":
		v := &atem.TransitionPositionField{}
		return set(v, func() { me().Transition.Position = v })
	case "transition-mix":
		v := &atem.TransitionMixField{}
		return set(v, func() { me().Transition.Mix = v })
	case "transition-dip":
		v := &atem.TransitionDipField{}
		return set(v, func() { me().Transition.Dip = v })
	case "transition-wipe":
		v := &atem.TransitionWipeField{}
		return set(v, func() { me().Transition.Wipe = v })
	case "transition-dve":
		v := &atem.TransitionDVEField{}
		return set(v, func() { me().Transition.DVE = v })
	case "transition-stinger":
		v := &atem.TransitionStingerField{}
		return set(v, func() { me().Transition.Stinger = v })
	case "fade-to-black":
		v := &atem.FadeToBlackField{}
		return set(v, func() { me().FTB.Rate = v })
	case "fade-to-black-state":
		v := &atem.FadeToBlackStateField{}
		return set(v, func() { me().FTB.State = v })
	case "mixer-effect-config":
		v := &atem.MixerEffectConfigField{}
		return set(v, func() { me().Config = v })
	case "key-on-air":
		v := &atem.KeyOnAirField{}
		return set(v, func() { usk().OnAir = v })
	case "key-properties-base":
		v := &atem.KeyPropertiesBaseField{}
		return set(v, func() { usk().Base = v })
	case "key-properties-luma":
		v := &atem.KeyPropertiesLumaField{}
		return set(v, func() { usk().Luma = v })
	case "key-properties-advanced-chroma":
		v := &atem.KeyPropertiesAdvancedChromaField{}
		return set(v, func() { usk().AdvancedChroma = v })
	case "key-properties-advanced-chroma-colorpicker":
		v := &atem.KeyPropertiesAdvancedChromaColorpickerField{}
		return set(v, func() { usk().ChromaPicker = v })
	case "key-properties-pattern":
		v := &atem.KeyPropertiesPatternField{}
		return set(v, func() { usk().Pattern = v })
	case "key-properties-dve":
		v := &atem.KeyPropertiesDVEField{}
		return set(v, func() { usk().DVE = v })
	case "key-properties-fly":
		v := &atem.KeyPropertiesFlyField{}
		return set(v, func() { usk().Fly = v })
	case "key-properties-fly-keyframe":
		v := &atem.KeyPropertiesFlyKeyframeField{}
		return set(v, func() {
			u := usk()
			if u.Keyframes == nil {
				u.Keyframes = map[uint8]*atem.KeyPropertiesFlyKeyframeField{}
			}
			u.Keyframes[uint8(idx(2))] = v
		})
	case "dkey-properties-base":
		v := &atem.DkeyPropertiesBaseField{}
		return set(v, func() { dsk().Base = v })
	case "dkey-properties":
		v := &atem.DkeyPropertiesField{}
		return set(v, func() { dsk().Properties = v })
	case "dkey-state":
		v := &atem.DkeyStateField{}
		return set(v, func() { dsk().State = v })
	case "aux-output-source":
		v := &atem.AuxOutputSourceField{}
		return set(v, func() { s.Aux = growTo(s.Aux, idx(0)); s.Aux[idx(0)] = v })
	case "color-generator":
		v := &atem.ColorGeneratorField{}
		return set(v, func() {
			s.ColorGenerators = growTo(s.ColorGenerators, idx(0))
			s.ColorGenerators[idx(0)] = v
		})
	case "mediaplayer-selected":
		v := &atem.MediaplayerSelectedField{}
		return set(v, func() { s.MediaPlayers = growTo(s.MediaPlayers, idx(0)); s.MediaPlayers[idx(0)] = v })
	case "input-properties":
		v := &atem.InputPropertiesField{}
		return set(v, func() { s.Inputs[uint16(idx(0))] = v })
	case "hyperdeck-settings":
		v := &atem.HyperdeckSettingsField{}
		return set(v, func() { s.HyperDecks[uint16(idx(0))] = v })
	case "mediaplayer-file-info":
		v := &atem.MediaplayerFileInfoField{}
		return set(v, func() { s.MediaPool.Files[uint16(idx(0))] = v })
	case "macro-properties":
		v := &atem.MacroPropertiesField{}
		return set(v, func() { s.Macros.Properties[uint16(idx(0))] = v })
	case "fade-to-black-enabled":
		v := &atem.FadeToBlackEnabledField{}
		return set(v, func() { s.FTBEnabled = v })
	case "video-mode":
		v := &atem.VideoModeField{}
		return set(v, func() { s.VideoMode.Mode = v })
	case "sdi-3g-level":
		v := &atem.SDI3GLevelField{}
		return set(v, func() { s.VideoMode.SDI3GLevel = v })
	case "product-name":
		v := &atem.ProductNameField{}
		return set(v, func() { s.Identity.Product = v })
	case "topology":
		v := &atem.TopologyField{}
		return set(v, func() { s.Topology = v })
	case "multiviewer-properties":
		v := &atem.MultiviewerPropertiesField{}
		return set(v, func() {
			s.Multiviewers = growTo(s.Multiviewers, idx(0))
			s.Multiviewers[idx(0)].Properties = v
		})
	case "multiviewer-input":
		v := &atem.MultiviewerInputField{}
		return set(v, func() { window().Input = v })
	case "multiviewer-vu":
		v := &atem.MultiviewerVUField{}
		return set(v, func() { window().VU = v })
	case "multiviewer-safe-area":
		v := &atem.MultiviewerSafeAreaField{}
		return set(v, func() { window().SafeArea = v })
	case "fairlight-master-properties":
		v := &atem.FairlightMasterPropertiesField{}
		return set(v, func() { s.Fairlight.Master = v })
	case "fairlight-master-compressor-properties":
		v := &atem.FairlightMasterCompressorPropertiesField{}
		return set(v, func() { s.Fairlight.MasterCompressor = v })
	case "fairlight-master-limiter-properties":
		v := &atem.FairlightMasterLimiterPropertiesField{}
		return set(v, func() { s.Fairlight.MasterLimiter = v })
	case "atem-master-eq-band-properties":
		v := &atem.AtemMasterEQBandPropertiesField{}
		return set(v, func() { s.Fairlight.MasterEQBands[uint8(idx(0))] = v })
	case "fairlight-headphones":
		v := &atem.FairlightHeadphonesField{}
		return set(v, func() { s.Fairlight.Headphones = v })
	case "fairlight-audio-input":
		v := &atem.FairlightAudioInputField{}
		return set(v, func() { s.Fairlight.Inputs[uint16(idx(0))] = v })
	case "fairlight-strip-properties":
		v := &atem.FairlightStripPropertiesField{}
		return set(v, func() { strip().Properties = v })
	case "atem-eq-band-properties":
		v := &atem.AtemEQBandPropertiesField{}
		return set(v, func() { strip().EQBands[uint8(idx(1))] = v })
	case "fairlight-compressor-properties":
		v := &atem.FairlightCompressorPropertiesField{}
		return set(v, func() { strip().Compressor = v })
	case "fairlight-limiter-properties":
		v := &atem.FairlightLimiterPropertiesField{}
		return set(v, func() { strip().Limiter = v })
	case "fairlight-expander-properties":
		v := &atem.FairlightExpanderPropertiesField{}
		return set(v, func() { strip().Expander = v })
	}
	return fmt.Errorf("no place for %s", where)
}

func buildState(t *testing.T, entries []entry, lenient bool) (atem.State, []string) {
	t.Helper()
	b := newBuilder(lenient)
	for _, e := range entries {
		if err := b.place(e); err != nil {
			t.Fatal(err)
		}
	}
	return b.st, b.overflow
}

// --- options ----------------------------------------------------------------------

func meOptionsJSON(t *testing.T, v any) []MixEffectOptions {
	var out []MixEffectOptions
	for _, m := range v.([]any) {
		mm := m.(map[string]any)
		o := MixEffectOptions{
			Program: mm["program"].(bool), Preview: mm["preview"].(bool),
			NextTransition: mm["next_transition"].(bool), TransitionStyle: mm["transition_style"].(bool),
			FadeToBlack: mm["fade_to_black"].(bool), USK: []bool{},
		}
		for _, u := range mm["usk"].([]any) {
			o.USK = append(o.USK, u.(bool))
		}
		if len(mm) != 6 {
			t.Fatalf("MixEffectOptions fields %v", mm)
		}
		out = append(out, o)
	}
	return out
}

func saveOptionsJSON(t *testing.T, m map[string]any) *SaveOptions {
	o := &SaveOptions{MEs: meOptionsJSON(t, m["mes"])}
	fields := map[string]*bool{
		"downstream_keys": &o.DownstreamKeys, "color_generators": &o.ColorGenerators,
		"fairlight": &o.Fairlight, "camera_control": &o.CameraControl,
		"media_pool_metadata": &o.MediaPoolMetadata, "media_pool_images": &o.MediaPoolImages,
		"media_players": &o.MediaPlayers, "auxiliaries": &o.Auxiliaries, "counters": &o.Counters,
		"settings": &o.Settings, "video_mode": &o.VideoMode, "hyperdecks": &o.HyperDecks, "macros": &o.Macros,
	}
	setBools(t, m, fields)
	return o
}

func applyOptionsJSON(t *testing.T, m map[string]any) *ApplyOptions {
	o := &ApplyOptions{MEs: meOptionsJSON(t, m["mes"])}
	fields := map[string]*bool{
		"restore_multiview": &o.RestoreMultiview, "restore_camera_control": &o.RestoreCameraControl,
		"restore_talkback": &o.RestoreTalkback, "restore_hyperdecks": &o.RestoreHyperDecks,
		"restore_fly_keyframes": &o.RestoreFlyKeyframes, "restore_downstream_keys": &o.RestoreDownstreamKeys,
		"restore_color_generators": &o.RestoreColorGenerators, "restore_audio": &o.RestoreAudio,
		"restore_video_mode": &o.RestoreVideoMode, "restore_inputs": &o.RestoreInputs,
		"restore_macros": &o.RestoreMacros, "restore_aux": &o.RestoreAux,
		"restore_media_players": &o.RestoreMediaPlayers, "restore_settings_flags": &o.RestoreSettingsFlags,
		"restore_media_pool_images": &o.RestoreMediaPoolImages,
	}
	setBools(t, m, fields)
	return o
}

func setBools(t *testing.T, m map[string]any, fields map[string]*bool) {
	t.Helper()
	for k, v := range m {
		if k == "mes" {
			continue
		}
		p, ok := fields[k]
		if !ok {
			t.Fatalf("option %s has no Go field", k)
		}
		*p = v.(bool)
	}
	if len(m) != len(fields)+1 {
		t.Fatalf("options %v: want %d fields", m, len(fields)+1)
	}
}

// --- fakes --------------------------------------------------------------------------

// fakeSource is a Save source: a state and recorded macro downloads.
type fakeSource struct {
	st        atem.State
	downloads map[int][]byte
	errs      map[int]string
	asked     []int
}

func (f *fakeSource) State() atem.State { return f.st }

func (f *fakeSource) DownloadMacro(ctx context.Context, slot int) ([]byte, error) {
	f.asked = append(f.asked, slot)
	if msg, ok := f.errs[slot]; ok {
		return nil, errors.New(msg)
	}
	if b, ok := f.downloads[slot]; ok {
		return b, nil
	}
	return nil, errors.New("no such slot")
}

// recTarget is an Apply target recording what it is sent.
type recTarget struct {
	st        atem.State
	connected bool
	fail      map[int]string
	events    []recEvent
	cmds      []atem.Command
}

type recEvent struct {
	kind  string // cmd, drop, upload, sleep
	bytes []byte
	drop  string
	up    [4]string
	sleep time.Duration
}

func (r *recTarget) State() atem.State { return r.st }
func (r *recTarget) Connected() bool   { return r.connected }

func (r *recTarget) Send(cmd atem.Command) error {
	if v, ok := cmd.(atem.Validator); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("profile sent an unencodable %s: %w", cmd.Opcode(), err)
		}
	}
	r.events = append(r.events, recEvent{kind: "cmd", bytes: cmd.Encode()})
	r.cmds = append(r.cmds, cmd)
	return nil
}

func (r *recTarget) UploadMacro(ctx context.Context, slot int, name, description string, bytecode []byte) error {
	r.events = append(r.events, recEvent{kind: "upload", up: [4]string{strconv.Itoa(slot), name, description, hex.EncodeToString(bytecode)}})
	if msg, ok := r.fail[slot]; ok {
		return errors.New(msg)
	}
	return nil
}

// runApply applies p to t with the pause and drops recorded as events.
func runApply(p *Profile, t *recTarget, opts *ApplyOptions) (*ApplyResult, error) {
	a := p.newApplier(context.Background(), t, opts)
	a.sleep = func(_ context.Context, d time.Duration) error {
		t.events = append(t.events, recEvent{kind: "sleep", sleep: d})
		return nil
	}
	a.onDrop = func(op string) { t.events = append(t.events, recEvent{kind: "drop", drop: op}) }
	err := a.run()
	return a.res, err
}

// --- tests --------------------------------------------------------------------------

// The golden is what Profile.to_xml writes for test_profile_granular.py's
// state with the default options: bmdwire's
// test_single_me_save_byte_identical_to_pre_4c1_golden. That state holds
// one value no atem.State can: a stinger media player of 3010 in TStP's u8
// field. The test checks that is the only one and has the stinger source
// reader return it.
func TestSaveGolden(t *testing.T) {
	o := loadOracle(t)
	st, overflow := buildState(t, o.Golden.State, true)
	if want := []string{"transition-stinger[0].mediaplayer=3010"}; !slices.Equal(overflow, want) {
		t.Fatalf("unrepresentable values %v, want %v", overflow, want)
	}
	s := newSaver(context.Background(), &fakeSource{st: st}, nil)
	s.stingerSource = stingerOverride(st, overflow)
	p, err := s.build()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.ToXML()
	if err != nil {
		t.Fatal(err)
	}
	want := string(fixtures.Bytes(t, "profile/profile_single_me_save_golden.xml"))
	if got != want {
		t.Fatalf("golden mismatch:\n%s", firstDiff(got, want))
	}
}

// stingerOverride is st.StingerSource with the stinger media players a
// stand-in state holds beyond u8.
func stingerOverride(st atem.State, overflow []string) func(int) int {
	over := map[int]int{}
	for _, o := range overflow {
		var me, v int
		if _, err := fmt.Sscanf(o, "transition-stinger[%d].mediaplayer=%d", &me, &v); err == nil {
			over[me] = v
		}
	}
	return func(me int) int {
		if v, ok := over[me]; ok {
			return v
		}
		return st.StingerSource(me)
	}
}

func firstDiff(got, want string) string {
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			return fmt.Sprintf("line %d:\n got %q\nwant %q", i+1, g, w)
		}
	}
	return "equal lines"
}

// Every save writes what bmdwire's Profile.from_atem writes for the same
// state and options, downloads the same macro slots in the same order,
// and parses back to the same XML (or is refused the same way).
func TestSaveMatchesAtemwire(t *testing.T) {
	o := loadOracle(t)
	states := map[string][]entry{}
	for _, c := range o.Saves {
		if c.StateFrom != "" {
			c.State = states[c.StateFrom]
		}
		states[c.Name] = c.State
		st, overflow := buildState(t, c.State, c.StandIn)
		src := &fakeSource{st: st, downloads: map[int][]byte{}, errs: map[int]string{}}
		for k, d := range c.Downloads {
			slot, _ := strconv.Atoi(k)
			if msg, ok := d["error"]; ok {
				src.errs[slot] = msg
				continue
			}
			b, err := hex.DecodeString(d["hex"])
			if err != nil {
				t.Fatal(err)
			}
			src.downloads[slot] = b
		}
		s := newSaver(context.Background(), src, saveOptionsJSON(t, c.Options))
		s.stingerSource = stingerOverride(st, overflow)
		p, err := s.build()
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		got, err := p.ToXML()
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got != c.XML {
			t.Errorf("%s: %s", c.Name, firstDiff(got, c.XML))
			continue
		}
		if !slices.Equal(src.asked, c.Asked) && !(len(src.asked) == 0 && len(c.Asked) == 0) {
			t.Errorf("%s: downloaded %v, want %v", c.Name, src.asked, c.Asked)
		}
		back, err := FromXML([]byte(c.XML))
		switch want := c.Reparse.(type) {
		case bool:
			if err != nil {
				t.Errorf("%s: reparse: %v", c.Name, err)
				break
			}
			again, _ := back.ToXML()
			if (again == c.XML) != want {
				t.Errorf("%s: reparse round trip %v, want %v", c.Name, again == c.XML, want)
			}
		case map[string]any:
			if class, _ := errClassOf(err); class != want["class"] {
				t.Errorf("%s: reparse error %v, want %v", c.Name, err, want)
			}
		}
	}
	t.Logf("%d saves", len(o.Saves))
}

// errClassOf is the Python class of an error FromXML or Apply returns.
func errClassOf(err error) (string, string) {
	var pe *ParseError
	if errors.As(err, &pe) {
		return "ParseError", pe.Error()
	}
	if err == nil {
		return "", ""
	}
	return errorClass(err)
}

// Every apply sends what bmdwire's Profile.apply sends for the same
// document, options and switcher: the same commands byte for byte in the
// same order, the same drops (commands its connection would fail to
// encode), the same macro uploads and the same pause, then the same
// ApplyResult, or the same exception.
func TestApplyMatchesAtemwire(t *testing.T) {
	o := loadOracle(t)
	saved := map[string]string{}
	for _, s := range o.Saves {
		saved[s.Name] = s.XML
	}
	var total int
	for _, c := range o.Applies {
		src := saved[c.FromSave]
		if c.XML != nil {
			src = *c.XML
		} else if c.FromSave == "golden" {
			src = string(fixtures.Bytes(t, "profile/profile_single_me_save_golden.xml"))
		}
		p, err := FromXML([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		live, _ := buildState(t, c.Live, false)
		var opts *ApplyOptions
		switch c.Factory {
		case "default":
			opts = DefaultApplyOptions()
		case "macros_only":
			opts = MacrosOnly()
		default:
			opts = applyOptionsJSON(t, c.Options)
		}
		target := &recTarget{st: live, connected: c.Connected, fail: map[int]string{}}
		for k, v := range c.FailUploads {
			slot, _ := strconv.Atoi(k)
			target.fail[slot] = v
		}
		res, err := runApply(p, target, opts)
		total += len(target.events)
		if d := diffEvents(target.events, c.Events, o.Commands); d != "" {
			t.Errorf("%s: %s", c.Name, d)
			continue
		}
		if c.Raised != nil {
			class, msg := errClassOf(err)
			if class != c.Raised.Class || msg != c.Raised.Message {
				t.Errorf("%s: error %s: %s, want %s: %s", c.Name, class, msg, c.Raised.Class, c.Raised.Message)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if !slices.Equal(res.Applied, c.Result.Applied) || !slices.Equal(res.Skipped, c.Result.Skipped) ||
			!slices.Equal(res.Errors, c.Result.Errors) || res.Summary() != c.Result.Summary {
			t.Errorf("%s: result\n got %q\n     %q\n     %q\nwant %q\n     %q\n     %q", c.Name,
				res.Applied, res.Skipped, res.Errors, c.Result.Applied, c.Result.Skipped, c.Result.Errors)
		}
		if c.MacrosRaised != nil {
			_, err := p.Macros()
			if class, msg := errClassOf(err); class != c.MacrosRaised.Class || msg != c.MacrosRaised.Message {
				t.Errorf("%s: Macros error %v", c.Name, err)
			}
		} else if c.Macros != nil {
			checkMacros(t, c.Name, p, c.Macros)
		}
	}
	t.Logf("%d applies, %d events, %d distinct commands", len(o.Applies), total, len(o.Commands))
}

func checkMacros(t *testing.T, name string, p *Profile, want []macroJSON) {
	t.Helper()
	got, err := p.Macros()
	if err != nil {
		t.Errorf("%s: Macros: %v", name, err)
		return
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d macros, want %d", name, len(got), len(want))
		return
	}
	for i, w := range want {
		g := got[i]
		if g.Index != w.Index || g.Name != w.Name || g.Description != w.Description || len(g.Ops) != len(w.Ops) {
			t.Errorf("%s: macro %d: %+v, want %+v", name, i, g, w)
			continue
		}
		for j, op := range w.Ops {
			id := op[0].(string)
			var params []string
			for _, kv := range op[1].([]any) {
				pair := kv.([]any)
				params = append(params, pair[0].(string)+"="+pair[1].(string))
			}
			var gp []string
			for _, p := range g.Ops[j].Params {
				gp = append(gp, p.Name+"="+p.Value)
			}
			if g.Ops[j].ID != id || !slices.Equal(gp, params) {
				t.Errorf("%s: macro %d op %d: %v %v, want %s %v", name, i, j, g.Ops[j].ID, gp, id, params)
			}
		}
	}
}

func diffEvents(got []recEvent, want []event, commands []string) string {
	for i := range max(len(got), len(want)) {
		if i >= len(got) {
			return fmt.Sprintf("event %d: missing %+v", i, want[i])
		}
		if i >= len(want) {
			return fmt.Sprintf("event %d: extra %s %x %s", i, got[i].kind, got[i].bytes, got[i].drop)
		}
		g, w := got[i], want[i]
		switch {
		case w.Cmd != nil:
			if g.kind != "cmd" || hex.EncodeToString(g.bytes) != commands[*w.Cmd] {
				return fmt.Sprintf("event %d: got %s %x %s, want command %s", i, g.kind, g.bytes, g.drop, commands[*w.Cmd])
			}
		case w.Drop != "":
			if g.kind != "drop" || g.drop != w.Drop {
				return fmt.Sprintf("event %d: got %s %x %s, want drop %s", i, g.kind, g.bytes, g.drop, w.Drop)
			}
		case w.Sleep != nil:
			if g.kind != "sleep" || g.sleep.Seconds() != *w.Sleep {
				return fmt.Sprintf("event %d: got %s %v, want sleep %v", i, g.kind, g.sleep, *w.Sleep)
			}
		case w.Upload != nil:
			u := [4]string{strconv.Itoa(w.Upload.Slot), w.Upload.Name, w.Upload.Description, w.Upload.Bytecode}
			if g.kind != "upload" || g.up != u {
				return fmt.Sprintf("event %d: got %s %q, want upload %q", i, g.kind, g.up, u)
			}
		}
	}
	return ""
}

// The XML layer parses and serializes as ElementTree over expat does:
// the same output, the same accessors, or a refusal of the same kind.
func TestParseMatchesAtemwire(t *testing.T) {
	o := loadOracle(t)
	for i, c := range o.Parses {
		var data []byte
		if c.XML != nil {
			data = []byte(*c.XML)
		} else {
			var err error
			if data, err = hex.DecodeString(c.Hex); err != nil {
				t.Fatal(err)
			}
		}
		where := fmt.Sprintf("parse %d %q", i, data)
		p, err := FromXML(data)
		if c.Error != nil {
			class, msg := errClassOf(err)
			if class != c.Error.Class {
				t.Errorf("%s: error %v (%s), want %s: %s", where, err, class, c.Error.Class, c.Error.Message)
			} else if class != "ParseError" && msg != c.Error.Message {
				t.Errorf("%s: message %q, want %q", where, msg, c.Error.Message)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", where, err)
			continue
		}
		got, err := p.ToXML()
		if err != nil || got != *c.Out {
			t.Errorf("%s: got %q (%v), want %q", where, got, err, *c.Out)
			continue
		}
		for prop, want := range c.Props {
			var got any
			var err error
			switch prop {
			case "major_version":
				got, err = p.MajorVersion()
			case "minor_version":
				got, err = p.MinorVersion()
			case "product":
				got = p.Product()
			case "video_mode":
				got = p.VideoMode()
			}
			if m, ok := want.(map[string]any); ok {
				r := m["raised"].(map[string]any)
				if class, msg := errClassOf(err); class != r["class"] || msg != r["message"] {
					t.Errorf("%s: %s error %v, want %v", where, prop, err, r)
				}
				continue
			}
			if f, ok := want.(float64); ok {
				want = int(f)
			}
			if err != nil || got != want {
				t.Errorf("%s: %s = %v (%v), want %v", where, prop, got, err, want)
			}
		}
	}
	t.Logf("%d parses", len(o.Parses))
}
