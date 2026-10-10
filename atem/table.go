// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The ATEM part of the command table (internal/table): one entry per
// command, mapping command-line parameters onto the command's fields. The
// CLI and docs/COMMANDS.md are built from it. The lock and file-transfer
// commands (SURFACE.md §1.19) are left out: a CLI reaches them only
// through transfers, never one by one.

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// RegisterCommands adds every ATEM command to r.
func RegisterCommands(r *table.Registry) error {
	var entries []table.Entry
	for _, s := range commandSpecs() {
		entries = append(entries, s.entry())
	}
	return r.Add(entries...)
}

// Resolver resolves the table's rates and sources against a switcher's
// state. Unlike atemwire's parse_rate it refuses a malformed rate instead
// of reading it as one second: a typo on the command line is an error,
// not a silent default.
type Resolver struct {
	State State
	// FPS is the display rate "seconds:frames" is counted in; 0 means
	// DisplayFPS(State).
	FPS int
	// Offline accepts any source name, as source 0, and any frame count
	// after the colon: for checking a command line before the switcher's
	// names and frame rate are known.
	Offline bool
}

// Rate reads "seconds:frames" or a frame count. Frames after the colon
// must be under the display rate.
func (r Resolver) Rate(s string) (int64, error) {
	fps := r.FPS
	if fps <= 0 {
		fps = DisplayFPS(r.State)
	}
	sec, fr, colon := strings.Cut(strings.TrimSpace(s), ":")
	if !allDigits(sec) || colon && !allDigits(fr) {
		return 0, fmt.Errorf("not seconds:frames or a frame count")
	}
	if colon && !r.Offline {
		if n, err := strconv.Atoi(fr); err != nil || n >= fps {
			return 0, fmt.Errorf("%s frames is not under the display rate, %d fps", fr, fps)
		}
	}
	return int64(ParseRate(s, fps)), nil
}

func allDigits(s string) bool {
	if s == "" || len(s) > 9 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Source reads a source id, or the long or short name of one of the
// switcher's inputs, case-insensitively.
func (r Resolver) Source(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if allDigits(s) {
		n, _ := strconv.ParseUint(s, 10, 64)
		return n, nil // the caller checks the range
	}
	if s == "" {
		return 0, fmt.Errorf("empty source")
	}
	if r.Offline {
		return 0, nil
	}
	ids := r.State.InputsNamed(s)
	switch len(ids) {
	case 0:
		if len(r.State.Inputs) == 0 {
			return 0, fmt.Errorf("no input names known: give the source id")
		}
		return 0, fmt.Errorf("no input is named %q", s)
	case 1:
		return uint64(ids[0]), nil
	}
	var cands []string
	for _, id := range ids {
		cands = append(cands, fmt.Sprintf("%d (%s)", id, strings.TrimSpace(r.State.Inputs[id].Name)))
	}
	return 0, fmt.Errorf("%q names more than one input: %s", s, strings.Join(cands, ", "))
}

// InputsNamed returns, in id order, the inputs whose long or short name is
// name, ignoring case and surrounding spaces.
func (s State) InputsNamed(name string) []uint16 {
	name = strings.TrimSpace(name)
	var ids []uint16
	for id, in := range s.Inputs {
		if in != nil && (strings.EqualFold(strings.TrimSpace(in.Name), name) || strings.EqualFold(strings.TrimSpace(in.ShortName), name)) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// spec is one command's entry before it is turned into a table.Entry.
type spec struct {
	path, summary string
	cmd           Command // zero value; its type is what Build makes
	echo          string
	params        []fp
	build         func(table.Args) (Command, error) // overrides the field mapping
}

// fp is a parameter and the command field it sets.
type fp struct {
	table.Param
	field string
}

func (s spec) entry() table.Entry {
	t := reflect.TypeOf(s.cmd)
	e := table.Entry{
		Device: "atem", Path: s.path, Summary: s.summary, Echo: s.echo,
		Unverified: s.cmd.Unverified(), Destructive: s.cmd.Destructive(),
	}
	for _, p := range s.params {
		f, ok := t.FieldByName(p.field)
		if !ok && s.build == nil {
			panic(fmt.Sprintf("atem table: %s has no field %s", t.Name(), p.field))
		}
		if ok && (p.Kind == table.Uint || p.Kind == table.Int || p.Kind == table.Rate || p.Kind == table.Enum || p.Kind == table.Source) && p.Min == nil {
			p.Min, p.Max = fieldRange(f.Type)
		}
		if ok && f.Type.Kind() == reflect.Pointer && !p.Required && p.Default == "" {
			e.AtLeastOne = true
		}
		e.Params = append(e.Params, p.Param)
	}
	build := s.build
	if build == nil {
		build = func(a table.Args) (Command, error) { return buildFields(t, s.params, a) }
	}
	e.Build = func(a table.Args) (table.Command, error) {
		c, err := build(a)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
	return e
}

// fieldRange is the range of an integer field (or the integer a pointer
// field points to).
func fieldRange(t reflect.Type) (*float64, *float64) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return table.Bounds(0, float64(uint64(1)<<(8*t.Size())-1))
	case reflect.Int8, reflect.Int16, reflect.Int32:
		hi := float64(uint64(1)<<(8*t.Size()-1) - 1)
		return table.Bounds(-hi-1, hi)
	}
	return nil, nil
}

// buildFields makes a command of type t, setting each given parameter's
// field: a pointer field is allocated, an integer is range-checked against
// the field's type.
func buildFields(t reflect.Type, params []fp, a table.Args) (Command, error) {
	v := reflect.New(t).Elem()
	for _, p := range params {
		if !a.Has(p.Name) {
			continue
		}
		f := v.FieldByName(p.field)
		if f.Kind() == reflect.Pointer {
			f.Set(reflect.New(f.Type().Elem()))
			f = f.Elem()
		}
		val := a.Value(p.Name)
		switch x := val.(type) {
		case uint64:
			switch f.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if x > math.MaxInt64 || f.OverflowInt(int64(x)) {
					return nil, fmt.Errorf("%w: --%s %d does not fit", table.ErrUsage, p.Name, x)
				}
				f.SetInt(int64(x))
			default:
				if f.OverflowUint(x) {
					return nil, fmt.Errorf("%w: --%s %d does not fit", table.ErrUsage, p.Name, x)
				}
				f.SetUint(x)
			}
		case int64:
			switch f.Kind() {
			case reflect.Uint8, reflect.Uint16, reflect.Uint32:
				if x < 0 || f.OverflowUint(uint64(x)) {
					return nil, fmt.Errorf("%w: --%s %d does not fit", table.ErrUsage, p.Name, x)
				}
				f.SetUint(uint64(x))
			default:
				if f.OverflowInt(x) {
					return nil, fmt.Errorf("%w: --%s %d does not fit", table.ErrUsage, p.Name, x)
				}
				f.SetInt(x)
			}
		default:
			f.Set(reflect.ValueOf(val))
		}
	}
	return v.Interface().(Command), nil
}

// Parameter constructors.

func req(name, field string, kind table.Kind, unit, help string) fp {
	return fp{table.Param{Name: name, Kind: kind, Unit: unit, Help: help, Required: true}, field}
}

func opt(name, field string, kind table.Kind, unit, help string) fp {
	return fp{table.Param{Name: name, Kind: kind, Unit: unit, Help: help}, field}
}

func def(name, field string, kind table.Kind, value, help string) fp {
	return fp{table.Param{Name: name, Kind: kind, Default: value, Help: help}, field}
}

func names(p fp, n map[string]uint64) fp { p.Names = n; return p }

func oneBased(p fp) fp { p.OneBased = true; return p }

var (
	pME          = oneBased(def("me", "ME", table.Uint, "1", "M/E (the library's ME field counts from 0)"))
	pKeyer       = req("keyer", "Keyer", table.Uint, "", "upstream keyer, 0-based")
	pDSK         = def("dsk", "DSK", table.Uint, "0", "downstream keyer, 0-based")
	pSource      = req("source", "Source", table.Source, "", "source id or input name")
	pMultiviewer = def("multiviewer", "Multiviewer", table.Uint, "0", "multiviewer, 0-based")
	pSuperSource = def("supersource", "SuperSource", table.Uint, "0", "SuperSource, 0-based")
)

const (
	unitFrames    = "frames, or seconds:frames at the display rate"
	unitPercent   = "%, clamped to 0-100"
	unitUnit      = "0-1, clamped"
	unitClipGain  = "%, clamped to 0-100"
	unitEdge      = "display units: ±9 top and bottom, ±16 left and right (×1000)"
	unitDB        = "dB"
	unitMS        = "ms"
	unitThousands = "×1000 on the wire, not clamped"
)

var (
	transitionStyles = map[string]uint64{"mix": 0, "dip": 1, "wipe": 2, "dve": 3, "stinger": 4}
	layerNames       = map[string]uint64{"background": 1, "key1": 2, "key2": 4, "key3": 8, "key4": 16}
	keyTypes         = map[string]uint64{"luma": 0, "chroma": 1, "pattern": 2, "dve": 3}
	keyframeNames    = map[string]uint64{"a": 1, "b": 2, "full": 3, "runToInfinite": 4}
	macroActions     = map[string]uint64{"run": 0, "stop": 1, "stop-record": 2, "insert-user-wait": 3, "continue": 4, "delete": 5}
	mixOptions       = map[string]uint64{"Off": 1, "On": 2, "AudioFollowVideo": 4}
	eqShapes         = map[string]uint64{"LowShelf": 1, "LowPass": 2, "BandPass": 4, "Notch": 8, "HighPass": 16, "HighShelf": 32}
	eqRanges         = map[string]uint64{"Low": 1, "MidLow": 2, "MidHigh": 4, "High": 8}
	expanderModes    = map[string]uint64{"expander": 0, "gate": 1}
	cameraTypes      = map[string]uint64{"bool": 0, "int8": 1, "int16": 2, "int32": 3, "int64": 4, "string": 5, "fixed16": 128}
	ssLayers         = map[string]uint64{"background": 0, "foreground": 1}
	// atemwire's _WIPE_PATTERN_NAMES and _DVE_PATTERN_NAMES
	// (macrotransfer/_helpers.py), the names ATEM Software Control's XML
	// uses.
	wipePatterns = map[string]uint64{
		"LeftToRightBar": 0, "TopToBottomBar": 1, "HorizontalBarnDoor": 2, "VerticalBarnDoor": 3,
		"CornersInFourBox": 4, "RectangleIris": 5, "DiamondIris": 6, "CircleIris": 7, "TopLeftBox": 8,
		"TopRightBox": 9, "BottomRightBox": 10, "BottomLeftBox": 11, "TopCentreBox": 12, "RightCentreBox": 13,
		"BottomCentreBox": 14, "LeftCentreBox": 15, "TopLeftDiagonal": 16, "TopRightDiagonal": 17,
	}
	dveStyles = map[string]uint64{
		"SqueezeTopLeft": 16, "SqueezeTop": 17, "SqueezeTopRight": 18, "SqueezeLeft": 19, "SqueezeRight": 20,
		"SqueezeBottomLeft": 21, "SqueezeBottom": 22, "SqueezeBottomRight": 23, "PushTopLeft": 24, "PushTop": 25,
		"PushTopRight": 26, "PushLeft": 27, "PushRight": 28, "PushBottomLeft": 29, "PushBottom": 30,
		"PushBottomRight": 31, "GraphicLogoWipe": 34,
	}
)

func videoModeNames() map[string]uint64 {
	m := map[string]uint64{}
	for k, v := range VideoModeNames {
		m[k] = uint64(v)
	}
	return m
}

// stripParams are the source and channel that address a Fairlight strip.
func stripParams() []fp {
	ch := req("channel", "Channel", table.Int, "", "-1 for a stereo strip, 0 or 1 for a split-mono half")
	ch.Min, ch.Max = table.Bounds(-128, 127)
	return []fp{req("source", "Source", table.Source, "", "audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name"), ch}
}

func commandSpecs() []spec {
	return []spec{
		// §1.1 mixeffect
		{path: "program", summary: "Put a source on an M/E's program bus.", cmd: ProgramInput{}, echo: "PrgI:<me>",
			params: []fp{pME, pSource}},
		{path: "preview", summary: "Put a source on an M/E's preview bus.", cmd: PreviewInput{}, echo: "PrvI:<me>",
			params: []fp{pME, pSource}},
		{path: "cut", summary: "Cut program and preview.", cmd: Cut{}, params: []fp{pME}},
		{path: "auto", summary: "Run the M/E's transition with its current style and rate.", cmd: Auto{}, params: []fp{pME}},

		// §1.2 aux
		{path: "aux", summary: "Route a source to an aux output.", cmd: AuxSource{}, echo: "AuxS:<aux>",
			params: []fp{req("aux", "Aux", table.Uint, "", "aux output, 0-based"), pSource}},

		// §1.3 transition
		{path: "transition settings", summary: "Set the transition style and the layers the next transition takes.",
			cmd: TransitionSettings{}, echo: "TrSS:<me>", params: []fp{pME,
				names(opt("style", "Style", table.Enum, "", "transition style"), transitionStyles),
				names(opt("next", "NextTransition", table.Flags, "", "layers the next transition takes (not none)"), layerNames)}},
		{path: "transition mix", summary: "Set the mix transition's rate.", cmd: MixSettings{}, echo: "TMxP:<me>",
			params: []fp{pME, req("rate", "Rate", table.Rate, unitFrames, "rate")}},
		{path: "transition dip", summary: "Set the dip transition.", cmd: DipSettings{}, echo: "TDpP:<me>", params: []fp{pME,
			opt("rate", "Rate", table.Rate, unitFrames, "rate"), opt("source", "Source", table.Source, "", "dip source")}},
		{path: "transition wipe", summary: "Set the wipe transition.", cmd: WipeSettings{}, echo: "TWpP:<me>", params: []fp{pME,
			opt("rate", "Rate", table.Rate, unitFrames, "rate"),
			names(opt("pattern", "Pattern", table.Enum, "", "wipe pattern"), wipePatterns),
			opt("width", "Width", table.Float, unitPercent, "border width"),
			opt("source", "Source", table.Source, "", "border fill source"),
			opt("symmetry", "Symmetry", table.Float, unitPercent, "symmetry"),
			opt("softness", "Softness", table.Float, unitPercent, "softness"),
			opt("position-x", "PositionX", table.Float, unitUnit, "horizontal position"),
			opt("position-y", "PositionY", table.Float, unitUnit, "vertical position"),
			opt("reverse", "Reverse", table.Bool, "", "reverse direction"),
			opt("flip-flop", "FlipFlop", table.Bool, "", "flip flop")}},
		{path: "transition dve", summary: "Set the DVE transition.", cmd: DVESettings{}, echo: "TDvP:<me>", params: []fp{pME,
			opt("logo-rate", "LogoRate", table.Uint, "frames", "graphic logo-wipe rate (no atemwire operation sets it)"),
			opt("rate", "Rate", table.Rate, unitFrames, "DVE rate"),
			names(opt("style", "Style", table.Enum, "", "DVE effect"), dveStyles),
			opt("fill-source", "FillSource", table.Source, "", "fill source"),
			opt("key-source", "KeySource", table.Source, "", "key source"),
			opt("key-enable", "KeyEnable", table.Bool, "", "enable the key"),
			opt("premultiplied", "KeyPremultiplied", table.Bool, "", "pre-multiplied key"),
			opt("clip", "KeyClip", table.Float, unitClipGain, "key clip"),
			opt("gain", "KeyGain", table.Float, unitClipGain, "key gain"),
			opt("invert", "KeyInvert", table.Bool, "", "invert the key"),
			opt("reverse", "Reverse", table.Bool, "", "reverse direction"),
			opt("flip-flop", "FlipFlop", table.Bool, "", "flip flop")}},
		{path: "transition stinger", summary: "Set the stinger transition.", cmd: StingerSettings{}, echo: "TStP:<me>", params: []fp{pME,
			opt("player", "MediaPlayer", table.Uint, "", "media player slot, 1-4 (not a source id)"),
			opt("premultiplied", "KeyPremultiplied", table.Bool, "", "pre-multiplied key"),
			opt("clip", "KeyClip", table.Float, "%, ×10 on the wire, not clamped", "key clip"),
			opt("gain", "KeyGain", table.Float, "%, ×10 on the wire, not clamped", "key gain"),
			opt("invert", "KeyInvert", table.Bool, "", "invert the key"),
			opt("pre-roll", "Preroll", table.Rate, unitFrames, "pre-roll"),
			opt("duration", "Duration", table.Rate, unitFrames, "clip duration"),
			opt("trigger", "TriggerPoint", table.Rate, unitFrames, "trigger point"),
			opt("rate", "Rate", table.Rate, unitFrames, "mix rate")}},
		{path: "transition preview", summary: "Turn transition preview on or off.", cmd: TransitionPreview{}, echo: "TrPr:<me>",
			params: []fp{pME, req("enabled", "Enabled", table.Bool, "", "preview the transition")}},
		{path: "transition position", summary: "Move the T-bar.", cmd: TransitionPosition{}, echo: "TrPs:<me>",
			params: []fp{pME, req("position", "Position", table.Int, "0-10000, clamped", "T-bar position")}},

		// §1.4 keyer-usk
		{path: "usk on-air", summary: "Put an upstream keyer on or off air.", cmd: KeyOnAir{}, echo: "KeOn:<me>:<keyer>",
			params: []fp{pME, pKeyer, req("on-air", "Enabled", table.Bool, "", "on air")}},
		{path: "usk fill", summary: "Set an upstream keyer's fill source.", cmd: KeyFill{}, echo: "KeBP:<me>:<keyer>",
			params: []fp{pME, pKeyer, pSource}},
		{path: "usk key", summary: "Set an upstream keyer's key (cut) source.", cmd: KeyCut{}, echo: "KeBP:<me>:<keyer>",
			params: []fp{pME, pKeyer, pSource}},
		{path: "usk type", summary: "Set an upstream keyer's type and fly mode.", cmd: KeyType{}, echo: "KeBP:<me>:<keyer>",
			params: []fp{pME, pKeyer, names(opt("type", "Type", table.Enum, "", "key type"), keyTypes),
				opt("fly", "FlyEnabled", table.Bool, "", "fly key")}},
		{path: "usk luma", summary: "Set a luma key.", cmd: KeyPropertiesLuma{}, echo: "KeLm:<me>:<keyer>",
			params: []fp{pME, pKeyer, opt("premultiplied", "Premultiplied", table.Bool, "", "pre-multiplied key"),
				opt("clip", "Clip", table.Float, unitClipGain, "clip"), opt("gain", "Gain", table.Float, unitClipGain, "gain"),
				opt("invert", "InvertKey", table.Bool, "", "invert the key")}},
		{path: "usk chroma", summary: "Set an advanced chroma key.", cmd: KeyPropertiesAdvancedChroma{}, echo: "KACk:<me>:<keyer>",
			params: []fp{pME, pKeyer,
				opt("foreground", "Foreground", table.Float, "0-1, "+unitThousands, "foreground level"),
				opt("background", "Background", table.Float, "0-1, "+unitThousands, "background level"),
				opt("key-edge", "KeyEdge", table.Float, "0-1, "+unitThousands, "key edge"),
				opt("spill", "Spill", table.Float, "0-1, "+unitThousands, "spill suppression"),
				opt("flare", "Flare", table.Float, "0-1, "+unitThousands, "flare suppression"),
				opt("brightness", "Brightness", table.Float, "about -1 to 1, "+unitThousands, "brightness"),
				opt("contrast", "Contrast", table.Float, "about -1 to 1, "+unitThousands, "contrast"),
				opt("saturation", "Saturation", table.Float, "0-2, "+unitThousands, "saturation"),
				opt("red", "Red", table.Float, "about -1 to 1, "+unitThousands, "red"),
				opt("green", "Green", table.Float, "about -1 to 1, "+unitThousands, "green"),
				opt("blue", "Blue", table.Float, "about -1 to 1, "+unitThousands, "blue")}},
		{path: "usk chroma-sample", summary: "Set an advanced chroma key's sample cursor and colour.",
			cmd: KeyPropertiesAdvancedChromaColorpicker{}, echo: "KACC:<me>:<keyer>",
			params: []fp{pME, pKeyer, opt("cursor", "Cursor", table.Bool, "", "show the sample cursor"),
				opt("preview", "Preview", table.Bool, "", "preview the key"),
				opt("x", "X", table.Float, "0-1", "cursor x"), opt("y", "Y", table.Float, "0-1", "cursor y"),
				opt("size", "Size", table.Float, "0-1, clamped to 0.062-0.9925", "cursor size"),
				opt("sample-y", "SampleY", table.Float, "0-1", "sampled luma (for restoring a key)"),
				opt("sample-cb", "SampleCb", table.Float, "-1 to 1", "sampled Cb"),
				opt("sample-cr", "SampleCr", table.Float, "-1 to 1", "sampled Cr")}},
		{path: "usk pattern", summary: "Set a pattern key.", cmd: KeyPropertiesPattern{}, echo: "KePt:<me>:<keyer>",
			params: []fp{pME, pKeyer, names(opt("pattern", "Pattern", table.Enum, "", "pattern"), wipePatterns),
				opt("size", "Size", table.Float, unitPercent, "size"), opt("symmetry", "Symmetry", table.Float, unitPercent, "symmetry"),
				opt("softness", "Softness", table.Float, unitPercent, "softness"),
				opt("position-x", "PositionX", table.Float, unitUnit, "horizontal position"),
				opt("position-y", "PositionY", table.Float, unitUnit, "vertical position"),
				opt("invert", "InvertPattern", table.Bool, "", "invert the pattern")}},
		{path: "usk mask", summary: "Set an upstream keyer's mask.", cmd: KeyPropertiesMask{}, echo: "KeBP:<me>:<keyer>",
			params: []fp{pME, pKeyer, opt("enabled", "Enabled", table.Bool, "", "mask on"),
				opt("top", "Top", table.Float, unitEdge, "top edge"), opt("bottom", "Bottom", table.Float, unitEdge, "bottom edge"),
				opt("left", "Left", table.Float, unitEdge, "left edge"), opt("right", "Right", table.Float, unitEdge, "right edge")}},
		{path: "usk dve", summary: "Set a DVE (or fly) key.", cmd: KeyPropertiesDVE{}, echo: "KeDV:<me>:<keyer>",
			params: []fp{pME, pKeyer,
				opt("size-x", "SizeX", table.Float, "0-2, clamped", "horizontal size"),
				opt("size-y", "SizeY", table.Float, "0-2, clamped", "vertical size"),
				opt("position-x", "PosX", table.Float, "display units (×1000)", "horizontal position"),
				opt("position-y", "PosY", table.Float, "display units (×1000)", "vertical position"),
				opt("rotation", "Rotation", table.Float, "degrees", "rotation"),
				opt("border", "BorderEnabled", table.Bool, "", "border on"),
				opt("shadow", "ShadowEnabled", table.Bool, "", "drop shadow on"),
				opt("bevel", "BorderBevelEnabled", table.Uint, "", "bevel type (no atemwire operation sets it)"),
				opt("outer-width", "OuterWidth", table.Float, "×100 on the wire", "outer border width"),
				opt("inner-width", "InnerWidth", table.Float, "×100 on the wire", "inner border width"),
				opt("outer-softness", "OuterSoftness", table.Float, "0-100", "outer border softness"),
				opt("inner-softness", "InnerSoftness", table.Float, "0-100", "inner border softness"),
				opt("bevel-softness", "BevelSoftness", table.Float, "0-100", "bevel softness"),
				opt("bevel-position", "BevelPosition", table.Float, "0-100", "bevel position"),
				opt("opacity", "BorderOpacity", table.Float, "0-100", "border opacity"),
				opt("hue", "BorderHue", table.Float, "degrees, ×10 on the wire", "border hue"),
				opt("saturation", "BorderSaturation", table.Float, "×10 on the wire", "border saturation"),
				opt("luma", "BorderLuma", table.Float, "×10 on the wire", "border luma"),
				opt("light-direction", "Angle", table.Float, "degrees", "light source direction"),
				opt("light-altitude", "Altitude", table.Float, "10-100", "light source altitude"),
				opt("mask", "MaskEnabled", table.Bool, "", "mask on"),
				opt("mask-top", "MaskTop", table.Float, unitEdge, "mask top"),
				opt("mask-bottom", "MaskBottom", table.Float, unitEdge, "mask bottom"),
				opt("mask-left", "MaskLeft", table.Float, unitEdge, "mask left"),
				opt("mask-right", "MaskRight", table.Float, unitEdge, "mask right"),
				opt("rate", "Rate", table.Rate, unitFrames, "fly rate")}},
		{path: "usk keyframe-set", summary: "Store the keyer's DVE as a fly keyframe.", cmd: KeyerKeyframeSet{},
			echo:   "KKFP:<me>:<keyer>:<keyframe> (A and B), KeFS:<me>:<keyer>",
			params: []fp{pME, pKeyer, names(req("keyframe", "Keyframe", table.Enum, "", "keyframe"), keyframeNames)}},
		{path: "usk keyframe-run", summary: "Fly the key to a keyframe.", cmd: KeyerKeyframeRun{},
			params: []fp{pME, pKeyer, names(req("to", "RunTo", table.Enum, "", "keyframe to fly to"), keyframeNames),
				opt("direction", "Direction", table.Uint, "",
					"direction for runToInfinite; atemwire's two encodings agree on 1-4 and 6-9 (1 top left ... 9 bottom right) and disagree on the centre (5 or 0)")}},

		// §1.5 keyer-dsk
		{path: "dsk on-air", summary: "Put a downstream keyer on or off air.", cmd: DkeyOnair{}, echo: "DskS:<dsk>",
			params: []fp{pDSK, req("on-air", "OnAir", table.Bool, "", "on air")}},
		{path: "dsk auto", summary: "Run a downstream keyer's auto transition.", cmd: DkeyAuto{}, params: []fp{pDSK}},
		{path: "dsk tie", summary: "Tie a downstream keyer to the next transition.", cmd: DkeyTie{}, echo: "DskP:<dsk>",
			params: []fp{pDSK, req("tie", "Tie", table.Bool, "", "tie")}},
		{path: "dsk rate", summary: "Set a downstream keyer's rate.", cmd: DkeyRate{}, echo: "DskP:<dsk>",
			params: []fp{pDSK, req("rate", "Rate", table.Rate, unitFrames, "rate")}},
		{path: "dsk fill", summary: "Set a downstream keyer's fill source.", cmd: DkeySetFill{}, echo: "DskB:<dsk>",
			params: []fp{pDSK, pSource}},
		{path: "dsk key", summary: "Set a downstream keyer's key source.", cmd: DkeySetKey{}, echo: "DskB:<dsk>",
			params: []fp{pDSK, pSource}},
		{path: "dsk gain", summary: "Set a downstream keyer's clip, gain, pre-multiply and invert.", cmd: DkeyGain{}, echo: "DskP:<dsk>",
			params: []fp{pDSK, opt("premultiplied", "Premultiplied", table.Bool, "", "pre-multiplied key"),
				opt("clip", "Clip", table.Float, unitClipGain, "clip"), opt("gain", "Gain", table.Float, unitClipGain, "gain"),
				opt("invert", "Invert", table.Bool, "", "invert the key")}},
		{path: "dsk mask", summary: "Set a downstream keyer's mask.", cmd: DkeyMask{}, echo: "DskP:<dsk>",
			params: []fp{pDSK, opt("enabled", "Enabled", table.Bool, "", "mask on"),
				opt("top", "Top", table.Float, unitEdge, "top edge"), opt("bottom", "Bottom", table.Float, unitEdge, "bottom edge"),
				opt("left", "Left", table.Float, unitEdge, "left edge"), opt("right", "Right", table.Float, unitEdge, "right edge")}},

		// §1.6 fade-to-black
		{path: "ftb toggle", summary: "Fade to black, or back.", cmd: FadeToBlack{}, params: []fp{pME}},
		{path: "ftb rate", summary: "Set the fade-to-black rate.", cmd: FadeToBlackConfig{}, echo: "FtbP:<me>",
			params: []fp{pME, req("rate", "Frames", table.Rate, unitFrames, "rate")}},
		{path: "ftb enable", summary: "Make fade to black available or not (always M/E 1; see docs/PORTING.md).",
			cmd: FadeToBlackEnable{}, echo: "FEna", params: []fp{req("enabled", "Enabled", table.Bool, "", "available")}},

		// §1.7 color-gen
		{path: "color", summary: "Set a colour generator.", cmd: ColorGenerator{}, echo: "ColV:<generator>",
			params: []fp{req("generator", "Generator", table.Uint, "", "colour generator, 0 or 1"),
				opt("hue", "Hue", table.Float, "degrees, not clamped", "hue"),
				opt("saturation", "Saturation", table.Float, unitPercent, "saturation"),
				opt("luma", "Luma", table.Float, unitPercent, "luma")}},

		// §1.8 media
		{path: "media capture", summary: "Capture program into the next free still slot.", cmd: CaptureStill{}},
		{path: "media clear", summary: "Clear a still slot, without the store lock (some models ignore this).",
			cmd: ClearStill{}, echo: "MPfe:<slot>", params: []fp{req("slot", "Slot", table.Uint, "", "still slot, 0-based")}},
		{path: "media player", summary: "Point a media player at a still or a clip.", cmd: MediaplayerSelect{}, echo: "MPCE:<player>",
			params: []fp{req("player", "Player", table.Uint, "", "media player, 0-based"),
				opt("still", "Still", table.Uint, "", "still slot (give this or --clip)"),
				opt("clip", "Clip", table.Uint, "", "clip slot (give this or --still)")}},

		// §1.9 macro
		{path: "macro action", summary: "Run, stop, continue or delete a macro, or stop recording.", cmd: MacroAction{},
			echo: "MRPr, MRcS or MPrp:<slot>, by action",
			params: []fp{def("slot", "Slot", table.Uint, "65535", "macro slot, 0-based; 65535 for whatever is running or recording"),
				names(req("action", "Action", table.Enum, "", "action"), macroActions)}},
		{path: "macro record", summary: "Start recording a macro.", cmd: MacroRecord{}, echo: "MRcS",
			params: []fp{req("slot", "Slot", table.Uint, "", "macro slot, 0-based"), req("name", "Name", table.String, "", "name"),
				def("description", "Description", table.String, "", "description")}},
		{path: "macro sleep", summary: "Record a wait into the macro being recorded.", cmd: MacroSleep{},
			params: []fp{req("frames", "Frames", table.Uint, "", "frames to wait")}},

		// §1.10 fairlight
		{path: "fairlight master", summary: "Set the Fairlight master bus.", cmd: FairlightMasterProperties{}, echo: "FAMP",
			params: []fp{opt("eq", "EQEnable", table.Bool, "", "EQ on"),
				opt("eq-gain", "EQGain", table.Float, "dB, clamped to ±20", "EQ gain"),
				opt("makeup", "DynamicsGain", table.Float, "dB, clamped to 0-20", "dynamics make-up gain"),
				opt("volume", "Volume", table.Float, "dB, clamped to -100-10; -inf is -100", "fader level"),
				opt("afv", "AFV", table.Bool, "", "audio follows video")}},
		{path: "fairlight strip", summary: "Set a Fairlight strip.", cmd: FairlightStripProperties{}, echo: "FASP:<source>.<channel>",
			params: append(stripParams(),
				opt("delay", "Delay", table.Uint, "frames", "delay"),
				opt("gain", "Gain", table.Float, "dB, clamped to -100-6", "input gain"),
				opt("eq", "EQEnable", table.Bool, "", "EQ on"),
				opt("eq-gain", "EQGain", table.Float, "dB, clamped to ±20", "EQ gain"),
				opt("makeup", "DynamicsGain", table.Float, "dB, clamped to 0-20", "dynamics make-up gain"),
				opt("pan", "Balance", table.Float, "-100 to 100, clamped", "pan"),
				opt("volume", "Volume", table.Float, "dB, clamped to -100-10; -inf is -100", "fader level"),
				names(opt("mix", "State", table.Enum, "", "mix option"), mixOptions))},
		{path: "fairlight eq", summary: "Set one EQ band of a Fairlight strip.", cmd: FairlightEQBandProperties{},
			echo: "AEBP:<source>.<channel>:<band>",
			params: append(stripParams(), req("band", "Band", table.Uint, "", "band, 0-5"),
				opt("enabled", "Enabled", table.Bool, "", "band on"),
				names(opt("shape", "Shape", table.Enum, "", "filter shape"), eqShapes),
				names(opt("range", "FrequencyRange", table.Enum, "", "frequency range"), eqRanges),
				opt("frequency", "Frequency", table.Int, "Hz, clamped to 30-21700", "frequency"),
				opt("gain", "Gain", table.Float, "dB, clamped to ±20", "gain"),
				opt("q", "Q", table.Float, "clamped to 0.30-10.30", "Q"))},
		{path: "fairlight master-eq", summary: "Set one EQ band of the Fairlight master bus.", cmd: FairlightMasterEQBandProperties{},
			echo: "AMBP:<band>",
			params: []fp{req("band", "Band", table.Uint, "", "band, 0-5"), opt("enabled", "Enabled", table.Bool, "", "band on"),
				names(opt("shape", "Shape", table.Enum, "", "filter shape"), eqShapes),
				names(opt("range", "FrequencyRange", table.Enum, "", "frequency range"), eqRanges),
				opt("frequency", "Frequency", table.Int, "Hz, clamped to 30-21700", "frequency"),
				opt("gain", "Gain", table.Float, "dB, clamped to ±20", "gain"),
				opt("q", "Q", table.Float, "clamped to 0.30-10.30", "Q")}},
		{path: "fairlight compressor", summary: "Set a Fairlight strip's compressor.", cmd: FairlightCompressorProperties{},
			echo: "AICP:<source>.<channel>",
			params: append(stripParams(), opt("enabled", "Enabled", table.Bool, "", "compressor on"),
				opt("threshold", "Threshold", table.Float, "dB, -60-0", "threshold"),
				opt("ratio", "Ratio", table.Float, "1.2-20", "ratio"),
				opt("attack", "Attack", table.Float, "ms, 0.7-100", "attack"),
				opt("hold", "Hold", table.Float, "ms, 0-4000", "hold"),
				opt("release", "Release", table.Float, "ms, 50-4000", "release"))},
		{path: "fairlight limiter", summary: "Set a Fairlight strip's limiter.", cmd: FairlightLimiterProperties{},
			echo: "AILP:<source>.<channel>",
			params: append(stripParams(), opt("enabled", "Enabled", table.Bool, "", "limiter on"),
				opt("threshold", "Threshold", table.Float, "dB, -60-0", "threshold"),
				opt("attack", "Attack", table.Float, "ms, 0.7-30", "attack"),
				opt("hold", "Hold", table.Float, "ms, 0-4000", "hold"),
				opt("release", "Release", table.Float, "ms, 50-4000", "release"))},
		{path: "fairlight expander", summary: "Set a Fairlight strip's expander or gate.", cmd: FairlightExpanderProperties{},
			echo: "AIXP:<source>.<channel>",
			params: append(stripParams(), opt("enabled", "Enabled", table.Bool, "", "expander on"),
				names(opt("mode", "Mode", table.Enum, "", "mode"), expanderModes),
				opt("threshold", "Threshold", table.Float, "dB, -50-0", "threshold"),
				opt("range", "Range", table.Float, "dB, 0-60", "range"),
				opt("ratio", "Ratio", table.Float, "1-3", "ratio"),
				opt("attack", "Attack", table.Float, "ms, 0.5-100", "attack"),
				opt("hold", "Hold", table.Float, "ms, 0-4000", "hold"),
				opt("release", "Release", table.Float, "ms, 50-4000", "release"))},
		{path: "fairlight master-compressor", summary: "Set the Fairlight master bus compressor.",
			cmd: FairlightMasterCompressorProperties{}, echo: "MOCP",
			params: []fp{opt("enabled", "Enabled", table.Bool, "", "compressor on"),
				opt("threshold", "Threshold", table.Float, unitDB, "threshold"), opt("ratio", "Ratio", table.Float, "", "ratio"),
				opt("attack", "Attack", table.Float, unitMS, "attack"), opt("hold", "Hold", table.Float, unitMS, "hold"),
				opt("release", "Release", table.Float, unitMS, "release")}},
		{path: "fairlight master-limiter", summary: "Set the Fairlight master bus limiter.",
			cmd: FairlightMasterLimiterProperties{}, echo: "AMLP",
			params: []fp{opt("enabled", "Enabled", table.Bool, "", "limiter on"),
				opt("threshold", "Threshold", table.Float, unitDB, "threshold"),
				opt("attack", "Attack", table.Float, unitMS, "attack"), opt("hold", "Hold", table.Float, unitMS, "hold"),
				opt("release", "Release", table.Float, unitMS, "release")}},
		{path: "fairlight levels", summary: "Start or stop the Fairlight meter streams (again after every reconnect).",
			cmd: SendFairlightLevels{}, params: []fp{req("enable", "Enable", table.Bool, "", "stream meters")}},

		// §1.11 audio-legacy
		{path: "audio input", summary: "Set a classic audio mixer input.", cmd: AudioInput{}, echo: "AMIP:<source>.0",
			params: []fp{pSource, opt("balance", "Balance", table.Int, "±10000", "balance"),
				opt("volume", "Volume", table.Uint, "0-65381", "volume"),
				opt("on", "On", table.Bool, "", "mix option on or off (wins over --afv)"),
				opt("afv", "AFV", table.Bool, "", "audio follows video")}},
		{path: "audio master", summary: "Set the classic audio mixer's master.", cmd: AudioMasterProperties{}, echo: "AMMO",
			params: []fp{opt("volume", "Volume", table.Uint, "0-65381", "volume"), opt("afv", "AFV", table.Bool, "", "audio follows video")}},
		{path: "audio monitor", summary: "Set the classic audio mixer's monitor.", cmd: AudioMonitorProperties{}, echo: "AMmO",
			params: []fp{opt("enabled", "Enabled", table.Bool, "", "monitor on"), opt("volume", "Volume", table.Uint, "", "volume"),
				opt("mute", "Mute", table.Bool, "", "mute"), opt("solo", "Solo", table.Bool, "", "solo"),
				opt("solo-source", "SoloSource", table.Source, "", "solo source"), opt("dim", "Dim", table.Bool, "", "dim"),
				opt("dim-volume", "DimVolume", table.Uint, "", "dim volume")}},
		{path: "audio levels", summary: "Start or stop the classic mixer's meter stream.", cmd: SendAudioLevels{},
			params: []fp{req("enable", "Enable", table.Bool, "", "stream meters")}},

		// §1.12 camera-control
		{path: "camera", summary: "Send one Blackmagic camera control parameter.", cmd: CameraControl{},
			echo: "CCdP:<destination>:<category>:<parameter>", build: buildCameraControl,
			params: []fp{req("destination", "Destination", table.Uint, "", "camera; 255 for all"),
				req("category", "Category", table.Uint, "", "category"), req("parameter", "Parameter", table.Uint, "", "parameter"),
				def("relative", "Relative", table.Bool, "false", "add to the current value"),
				names(def("type", "Datatype", table.Enum, "bool", "data type"), cameraTypes),
				opt("data", "Data", table.List, "", "comma-separated values of the data type")}},

		// §1.13 hyperdeck
		{path: "hyperdeck-slot", summary: "Bind a HyperDeck slot to an address and an input.", cmd: HyperdeckSettings{}, echo: "RXMS:<slot>",
			params: []fp{req("slot", "Slot", table.Uint, "", "HyperDeck slot, 0-9"),
				opt("address", "NetworkAddress", table.String, "", "deck IPv4 address; 0.0.0.0 clears the slot"),
				opt("input", "SwitcherInput", table.Source, "", "switcher input the deck feeds")}},

		// §1.14 multiviewer
		{path: "multiviewer input", summary: "Route a source to a multiviewer window.", cmd: MultiviewInput{}, echo: "MvIn:<multiviewer>:<window>",
			params: []fp{pMultiviewer, req("window", "Window", table.Uint, "", "window"), pSource}},
		{path: "multiviewer properties", summary: "Set a multiviewer's layout and program/preview swap.", cmd: MultiviewProperties{},
			echo: "MvPr:<multiviewer>",
			params: []fp{pMultiviewer, opt("layout", "Layout", table.Uint, "4-bit field: bit N splits quadrant N", "layout"),
				opt("swap", "Swap", table.Bool, "", "swap program and preview")}},

		// §1.15 supersource
		{path: "supersource box", summary: "Set a SuperSource box.", cmd: SupersourceBoxProperties{}, echo: "SSBP:<supersource>:<box>",
			params: []fp{pSuperSource, req("box", "Box", table.Uint, "", "box, 0-3"), opt("enabled", "Enabled", table.Bool, "", "box on"),
				opt("source", "Source", table.Source, "", "source"), opt("x", "X", table.Int, "-4800-4800", "x"),
				opt("y", "Y", table.Int, "-3400-3400", "y"), opt("size", "Size", table.Uint, "70-1000", "size"),
				opt("masked", "Masked", table.Bool, "", "crop on"), opt("top", "Top", table.Uint, "0-18000", "crop top"),
				opt("bottom", "Bottom", table.Uint, "0-18000", "crop bottom"), opt("left", "Left", table.Uint, "0-32000", "crop left"),
				opt("right", "Right", table.Uint, "0-32000", "crop right")}},
		{path: "supersource properties", summary: "Set SuperSource's art fill, key and layer.", cmd: SupersourceProperties{},
			echo: "SSrc:<supersource>",
			params: []fp{pSuperSource, opt("fill-source", "FillSource", table.Source, "", "art fill source"),
				opt("key-source", "KeySource", table.Source, "", "art key source"),
				names(opt("layer", "Layer", table.Enum, "", "art layer"), ssLayers),
				opt("premultiplied", "Premultiplied", table.Bool, "", "pre-multiplied key"),
				opt("clip", "Clip", table.Uint, "0-1000", "clip"), opt("gain", "Gain", table.Uint, "0-1000", "gain"),
				opt("invert", "Invert", table.Bool, "", "invert the key")}},

		// §1.16 recording
		{path: "recording settings", summary: "Set the recording file name and disks.", cmd: RecordingSettingsSet{}, echo: "RMSu",
			params: []fp{opt("filename", "Filename", table.String, "up to 128 bytes", "file name"),
				opt("disk1", "Disk1", table.Uint, "", "first disk"), opt("disk2", "Disk2", table.Uint, "", "second disk"),
				opt("in-camera", "RecordInCamera", table.Bool, "", "record in the cameras too")}},
		{path: "recording state", summary: "Start or stop recording.", cmd: RecorderStatus{}, echo: "RTMS",
			params: []fp{req("recording", "Recording", table.Bool, "", "recording")}},

		// §1.17 streaming
		{path: "streaming service", summary: "Set the streaming service.", cmd: StreamingServiceSet{}, echo: "SRSU",
			params: []fp{opt("name", "Name", table.String, "up to 64 bytes", "service name"),
				opt("url", "URL", table.String, "up to 512 bytes", "server URL"),
				opt("key", "Key", table.String, "up to 512 bytes", "stream key"),
				opt("bitrate-min", "BitrateMin", table.Uint, "bit/s", "lowest bitrate (with --bitrate-max)"),
				opt("bitrate-max", "BitrateMax", table.Uint, "bit/s", "highest bitrate (with --bitrate-min)")}},
		{path: "streaming audio-bitrate", summary: "Set the streaming audio bitrate.", cmd: StreamingAudioBitrate{}, echo: "STAB",
			params: []fp{req("min", "Min", table.Uint, "bit/s", "lowest"), req("max", "Max", table.Uint, "bit/s", "highest")}},
		{path: "streaming state", summary: "Start or stop streaming.", cmd: StreamingStatusSet{}, echo: "StRS",
			params: []fp{req("streaming", "Streaming", table.Bool, "", "streaming")}},

		// §1.18 settings
		{path: "input", summary: "Rename an input.", cmd: InputProperties{}, echo: "InPr:<source>",
			params: []fp{pSource, opt("long-name", "LongName", table.String, "up to 20 bytes", "long name"),
				opt("short-name", "ShortName", table.String, "up to 4 bytes", "short name"),
				opt("port-type", "PortType", table.Uint, "", "external port type (no atemwire operation sets it)")}},
		{path: "video-mode", summary: "Change the video mode: outputs resync and clients may be dropped.", cmd: VideoMode{},
			echo: "VidM", params: []fp{names(req("mode", "Mode", table.Enum, "", "video mode"), videoModeNames())}},
		{path: "auto-video-mode", summary: "Turn automatic input video mode detection on or off.", cmd: AutoInputVideoMode{},
			echo: "AiVM", params: []fp{req("enable", "Enable", table.Bool, "", "detect the input video mode")}},
		{path: "startup save", summary: "Make the current state the power-on state.", cmd: SaveStartupState{}},
		{path: "startup clear", summary: "Clear the saved power-on state.", cmd: ClearStartupState{}},
		{path: "time-of-day", summary: "Set the switcher's clock.", cmd: SetTimeOfDay{},
			params: []fp{def("time", "Time", table.Time, "now", "time")}},
		{path: "time-request", summary: "Ask the switcher for its time (the reply is the Time packet).", cmd: TimeRequest{}},
	}
}

// buildCameraControl reads --data as values of --type.
func buildCameraControl(a table.Args) (Command, error) {
	c := CameraControl{
		Destination: uint8(a.Value("destination").(uint64)),
		Category:    uint8(a.Value("category").(uint64)),
		Parameter:   uint8(a.Value("parameter").(uint64)),
		Relative:    a.Value("relative").(bool),
	}
	dt := a.Value("type").(uint64)
	if dt > math.MaxUint8 {
		return nil, fmt.Errorf("%w: --type %d does not fit", table.ErrUsage, dt)
	}
	c.Datatype = uint8(dt)
	if !a.Has("data") {
		return c, nil
	}
	c.Data = []any{}
	for _, s := range a.Value("data").([]string) {
		s = strings.TrimSpace(s)
		var v any
		var err error
		switch c.Datatype {
		case CameraBool:
			v, err = strconv.ParseBool(s)
		case CameraString:
			v = s
		case CameraFixed16:
			v, err = strconv.ParseFloat(s, 64)
		default:
			v, err = strconv.ParseInt(s, 0, 64)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: --data %q: not a %s", table.ErrUsage, s, cameraTypeName(c.Datatype))
		}
		c.Data = append(c.Data, v)
	}
	return c, nil
}

func cameraTypeName(dt uint8) string {
	for n, v := range cameraTypes {
		if v == uint64(dt) {
			return n
		}
	}
	return "value"
}
