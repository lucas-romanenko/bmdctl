// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// storedOpcodes is every state packet SURFACE.md §2 says atemwire stores
// under a pretty key: its 92 parsed packets less the six the transfer
// engine consumes (LKOB, FTCD, FTDC, FTDa, FTDE, *XFC) and InCm, which has
// no pretty key and is stored raw.
var storedOpcodes = []string{
	// §2.2
	"PrgI", "PrvI", "AuxS", "TrSS", "TrPr", "TrPs", "TMxP", "TDpP", "TWpP", "TDvP", "TStP", "FtbS", "FtbP", "FEna",
	// §2.3
	"KeOn", "KeBP", "KeLm", "KACk", "KACC", "KePt", "KeDV", "KeFS", "KKFP", "DskB", "DskP", "DskS", "ColV", "InPr",
	"MPfe", "MPCE", "MPrp", "MRcS", "MRPr",
	// §2.4
	"FAMP", "FASP", "AEBP", "AMBP", "AICP", "AILP", "AIXP", "MOCP", "AMLP", "FAIP", "FMTl", "FMHP", "FAMS", "FASD",
	"FMLv", "FDLv", "AMIP", "AMMO", "AMmO", "AMTl", "AMLv",
	// §2.5
	"MvPr", "MvIn", "VuMC", "SaMw", "SSrc", "SSBP", "RXMS", "CCdP", "RTMD", "RMSu", "RTMS", "RTMR", "SRSU", "STAB",
	"StRS", "SRSS", "TlIn", "TlSr", "VidM", "_VMC", "AiVM", "V3sl", "_ver", "_pin", "WhoI", "_top", "_MeC", "_mpl",
	"Time", "TCCc", "LKST",
}

var snakeCase = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// packetType strips pointers, slices, arrays and maps down to the element.
func packetType(t reflect.Type) reflect.Type {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return t
		}
	}
}

// Every State field that holds packets carries its opcode, every opcode
// atemwire stores appears exactly once, every field has a snake_case JSON
// name, and packet types (…Field) are only reached through a tagged field.
func TestStateTags(t *testing.T) {
	var tags []string
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(st reflect.Type, path string) {
		if seen[st] {
			return
		}
		seen[st] = true
		names := map[string]bool{}
		for i := range st.NumField() {
			f := st.Field(i)
			p := path + "." + f.Name
			js := f.Tag.Get("json")
			if !snakeCase.MatchString(js) {
				t.Errorf("%s: json tag %q is not snake_case", p, js)
			}
			if names[js] {
				t.Errorf("%s: json name %q used twice", p, js)
			}
			names[js] = true
			elem := packetType(f.Type)
			isPacket := strings.HasSuffix(elem.Name(), "Field")
			op, tagged := f.Tag.Lookup("atem")
			switch {
			case isPacket && !tagged && st.Name() != "VideoModeCapability":
				t.Errorf("%s holds %s but has no atem tag", p, elem.Name())
			case tagged && !isPacket:
				t.Errorf("%s has atem tag %q but holds %s", p, op, elem)
			case tagged:
				if len(op) != 4 {
					t.Errorf("%s: atem tag %q is not a 4-character code", p, op)
				}
				tags = append(tags, op)
			}
			if elem.Kind() == reflect.Struct {
				walk(elem, p)
			}
		}
	}
	walk(reflect.TypeFor[State](), "State")
	slices.Sort(tags)
	want := slices.Clone(storedOpcodes)
	slices.Sort(want)
	if !slices.Equal(tags, want) {
		t.Errorf("atem tags:\n got %v\nwant %v", tags, want)
	}
	if len(want) != 85 {
		t.Errorf("%d stored opcodes, want 92 - 6 consumed - InCm = 85", len(want))
	}
}

// fill sets every field reachable from v to a non-zero value: pointers
// allocated, slices and maps given two entries.
func fill(v reflect.Value, n *int) {
	*n++
	switch v.Kind() {
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem(), n)
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i), n)
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range 2 {
			fill(v.Index(i), n)
		}
	case reflect.Array:
		for i := range v.Len() {
			fill(v.Index(i), n)
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		for range 2 {
			k := reflect.New(v.Type().Key()).Elem()
			fill(k, n)
			e := reflect.New(v.Type().Elem()).Elem()
			fill(e, n)
			v.SetMapIndex(k, e)
		}
	case reflect.Interface:
		v.Set(reflect.ValueOf(float64(*n)))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(*n % 100))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(*n % 100))
	case reflect.Float64:
		v.SetFloat(float64(*n) / 7)
	case reflect.String:
		v.SetString(strings.Repeat("s", *n%5+1))
	}
}

// shared reports the first pointer, slice or map present in both a and b.
func shared(a, b reflect.Value, path string) string {
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() {
			return ""
		}
		if a.Pointer() == b.Pointer() {
			return path
		}
		return shared(a.Elem(), b.Elem(), path)
	case reflect.Interface:
		if a.IsNil() {
			return ""
		}
		return shared(a.Elem(), b.Elem(), path)
	case reflect.Slice:
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return path
		}
		for i := range a.Len() {
			if s := shared(a.Index(i), b.Index(i), path); s != "" {
				return s
			}
		}
	case reflect.Map:
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return path
		}
		for it := a.MapRange(); it.Next(); {
			if s := shared(it.Value(), b.MapIndex(it.Key()), path); s != "" {
				return s
			}
		}
	case reflect.Struct:
		for i := range a.NumField() {
			if s := shared(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); s != "" {
				return s
			}
		}
	case reflect.Array:
		for i := range a.Len() {
			if s := shared(a.Index(i), b.Index(i), path); s != "" {
				return s
			}
		}
	}
	return ""
}

func TestCloneSharesNothing(t *testing.T) {
	var s State
	n := 0
	fill(reflect.ValueOf(&s).Elem(), &n)
	c := s.clone()
	if !reflect.DeepEqual(s, c) {
		t.Fatal("clone differs from the original")
	}
	if p := shared(reflect.ValueOf(s), reflect.ValueOf(c), "State"); p != "" {
		t.Fatalf("clone shares %s with the original", p)
	}
	if _, err := json.Marshal(c); err != nil {
		t.Fatalf("a full state doesn't encode: %v", err)
	}
	t.Logf("%d values filled", n)
}

func TestNewStateIsEmpty(t *testing.T) {
	s := newState()
	if s.hasVideoMode() || s.Macros.LastRun != -1 {
		t.Fatalf("%+v", s)
	}
	b, err := json.Marshal(s)
	if err != nil || !json.Valid(b) {
		t.Fatal(err)
	}
}

// The ported helper methods. RGB references come from Python 3.12's
// colorsys.hls_to_rgb, which atemwire's get_rgb methods call.
func TestHLSToRGBMatchesColorsys(t *testing.T) {
	for _, c := range []struct{ h, l, s, r, g, b float64 }{
		{120 / 360.0, 0.5, 0.75, 0.125, 0.875, 0.125},
		{300 / 360.0, 0.25, 1.0, 0.5, 0.0, 0.4999999999999999},
		{0, 1.0, 0.0, 1, 1, 1},
		{359.9 / 360, 0.5, 0.5, 0.75, 0.25, 0.25083333333333313},
	} {
		r, g, b := hlsToRGB(c.h, c.l, c.s)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("hlsToRGB(%v, %v, %v) = %v %v %v, want %v %v %v", c.h, c.l, c.s, r, g, b, c.r, c.g, c.b)
		}
	}
	cg := ColorGeneratorField{Hue: 120, Saturation: 0.75, Luma: 0.5}
	if r, g, b := cg.RGB(); r != 0.125 || g != 0.875 || b != 0.125 {
		t.Errorf("ColorGeneratorField.RGB = %v %v %v", r, g, b)
	}
}

func TestVideoModeLabel(t *testing.T) {
	for _, c := range []struct {
		f     VideoModeField
		label string
		w, h  int
	}{
		{VideoModeField{Mode: 13, Resolution: 1080, Rate: 59.94, Widescreen: true}, "1080p59.94", 1920, 1080},
		{VideoModeField{Mode: 6, Resolution: 1080, Interlaced: true, Rate: 50, Widescreen: true}, "1080i50", 1920, 1080},
		{VideoModeField{Mode: 0, Resolution: 525, Interlaced: true, Rate: 59.94}, "525i59.94 4:3", 720, 480},
		{VideoModeField{Mode: 3, Resolution: 625, Interlaced: true, Rate: 50, Widescreen: true}, "625i50 16:9", 720, 576},
		{VideoModeField{Mode: 14, Resolution: 2160, Rate: 23.98, Widescreen: true}, "2160p23.98", 3840, 2160},
	} {
		if got := c.f.Label(); got != c.label {
			t.Errorf("mode %d: label %q, want %q", c.f.Mode, got, c.label)
		}
		if w, h := c.f.FrameSize(); w != c.w || h != c.h || c.f.Pixels() != c.w*c.h {
			t.Errorf("mode %d: %dx%d", c.f.Mode, w, h)
		}
	}
}

func TestSmallHelpers(t *testing.T) {
	tf := TimeField{Hours: 1, Minutes: 2, Seconds: 3, Frames: 24}
	if tf.TotalSeconds() != 3723 {
		t.Error(tf.TotalSeconds())
	}
	for plug, name := range map[uint8]string{0: "Internal", 2: "HDMI", 32: "XLR", 128: "RCA", 7: "Analog"} {
		if got := (&AudioInputField{Plug: plug}).PlugName(); got != name {
			t.Errorf("plug %d: %q", plug, got)
		}
	}
	h := MD5{0x01, 0x23, 0xab}
	b, _ := json.Marshal(h)
	if string(b) != `"0123ab00000000000000000000000000"` {
		t.Errorf("MD5 JSON %s", b)
	}
	var back MD5
	if err := json.Unmarshal(b, &back); err != nil || back != h {
		t.Errorf("MD5 round trip: %v %v", back, err)
	}
	picker := KeyPropertiesAdvancedChromaColorpickerField{SampleY: 0.5, SampleCb: 0, SampleCr: 0.5}
	if r, g, b := picker.RGB(); r != 1 || g != 0.5+0.5*-0.4681 || b != 0.5 {
		t.Errorf("picker RGB %v %v %v", r, g, b)
	}
}
