// SPDX-License-Identifier: MIT

package ultimatte

// ultimattewire's controls.py (bmdwire v1.14.0): every control in
// Blackmagic's "Ultimatte 12 Ethernet Protocol" v2.0 (September 2022),
// as a table: its wire name, its kind, its documented range or choices,
// and the models that don't have it. The document's Controls table is
// the only source; none of it has been run against a unit, here or in
// ultimattewire, beyond the CONTROL names the archive already read.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// The models the document names, as IDENTITY's Model maps to them
// (ModelFamily). ModelUnknown is a model it doesn't name: the model
// checks are then skipped rather than guessed.
const (
	Model8K      = "Ultimatte 12 8K"
	Model4K      = "Ultimatte 12 4K"
	ModelHD      = "Ultimatte 12 HD"
	ModelHDMini  = "Ultimatte 12 HD Mini"
	Model12      = "Ultimatte 12"
	ModelUnknown = ""
)

// ModelFamily is model_family: an IDENTITY Model as one of the Model
// constants, any case and spacing; "" for one the document doesn't name.
func ModelFamily(model string) string {
	m := pytext.Lower(strings.Join(pytext.Fields(model), " "))
	rest, ok := strings.CutPrefix(m, "ultimatte 12")
	if !ok {
		return ModelUnknown
	}
	switch strings.TrimSpace(rest) {
	case "hd mini":
		return ModelHDMini
	case "hd":
		return ModelHD
	case "8k":
		return Model8K
	case "4k":
		return Model4K
	case "":
		return Model12
	}
	return ModelUnknown
}

// InputsAll are the DEVICE block's input lock lines of a model the
// document's note doesn't cut down (the 12 8K's example has all nine; the
// 12 4K and the original 12 are taken to have them too).
var InputsAll = []string{"FG In", "BG In", "MONITOR In", "G MATTE In", "H MATTE In",
	"REF In", "BG MATTE In", "LAYER In", "LAYER MATTE In"}

var modelInputs = map[string][]string{
	ModelHDMini: {"FG In", "BG In"},
	ModelHD:     {"FG In", "BG In", "G MATTE In", "H MATTE In", "MONITOR In", "REF In"},
}

// ModelInputs is model_inputs: the input lock lines family has.
func ModelInputs(family string) []string {
	if in, ok := modelInputs[family]; ok {
		return in
	}
	return InputsAll
}

// HasGPI: "The Ultimatte 12 HD Mini does not have GPI inputs".
func HasGPI(family string) bool { return family != ModelHDMini }

// HasCameraControl: CAMERACONTROL is "only available on the Ultimatte 12
// HD Mini".
func HasCameraControl(family string) bool { return family == ModelHDMini }

// Kind is what a control takes.
type Kind string

const (
	// KindRange: an integer, Min..Max inclusive; the only kind that takes
	// the relative "Offset <name>: <delta>" form.
	KindRange Kind = "range"
	// KindBool: On/Off.
	KindBool Kind = "bool"
	// KindChoice: one of Choices, compared without regard to case (a 12
	// 4K reports them in lower case).
	KindChoice Kind = "choice"
	// KindFunction: footnote 5, send Yes to run it.
	KindFunction Kind = "function"
	// KindReadOnly: Tally Active.
	KindReadOnly Kind = "readonly"
)

// The footnote limits: not on the HD Mini (*), not on the HD or the HD
// Mini (**).
const (
	NotMini = "*"
	NotHD   = "**"
)

// Control is one row of the document's Controls table. Min and Max are
// set for a range (Max nil for the Window Position ones, whose maximum is
// the running format's height or width, Window); MinProtocol, when set,
// is the protocol version it needs (Output Range, 2.0).
type Control struct {
	Name        string
	Kind        Kind
	Min, Max    *int
	Choices     []string
	Limit       string
	MinProtocol *[2]int
	Note        string
	Window      string // "height" or "width"
}

// AvailableOn is available_on: false where a footnote says family lacks
// it; true for a family the document doesn't name.
func (c Control) AvailableOn(family string) bool {
	if family == "" {
		return true
	}
	switch c.Limit {
	case NotMini:
		return family != ModelHDMini
	case NotHD:
		return family != ModelHD && family != ModelHDMini
	}
	return true
}

func intp(v int) *int { return &v }

type opt func(*Control)

func limit(l string) opt       { return func(c *Control) { c.Limit = l } }
func note(n string) opt        { return func(c *Control) { c.Note = n } }
func window(w string) opt      { return func(c *Control) { c.Window = w } }
func minProtocol(a, b int) opt { return func(c *Control) { c.MinProtocol = &[2]int{a, b} } }

func mk(c Control, opts []opt) Control {
	for _, o := range opts {
		o(&c)
	}
	return c
}

func rr(name string, lo, hi int, opts ...opt) Control {
	return mk(Control{Name: name, Kind: KindRange, Min: intp(lo), Max: intp(hi)}, opts)
}
func r(name string, opts ...opt) Control { return rr(name, 0, 10000, opts...) }
func b(name string, opts ...opt) Control {
	return mk(Control{Name: name, Kind: KindBool}, opts)
}
func ch(name string, choices []string, opts ...opt) Control {
	return mk(Control{Name: name, Kind: KindChoice, Choices: choices}, opts)
}
func fn(name string, opts ...opt) Control {
	return mk(Control{Name: name, Kind: KindFunction}, opts)
}
func win(name, w string) Control {
	return Control{Name: name, Kind: KindRange, Min: intp(0), Window: w}
}

const footnote1 = "device dependent: 0-6 on the 12 8K at protocol 2.0, 0-3 on the Ultimatte 12 at 1.2"

// MonitorOut, LayerOrder and VideoFormats are the document's choices.
var (
	MonitorOut = []string{"Program", "FG", "BG", "Combined Matte", "Internal Matte", "Fill",
		"Layer In", "Background Matte In", "Garbage Matte In", "Holdout Matte In", "Layer Matte In",
		"Processed LM", "Processed HM", "Processed GM", "Processed BM", "Screen Correction"}
	LayerOrder = []string{"FG/Layer/BG Layer/BG", "Layer/FG/BG Layer/BG", "Layer/BG Layer/FG/BG",
		"BG Layer/Layer/FG/BG", "BG Layer/FG/Layer/BG", "FG/BG Layer/Layer/BG",
		"FG/BG Layer/BG", "BG Layer/FG/BG", "FG/Layer/BG", "Layer/FG/BG"}
	VideoFormats = []string{"Auto Detect", "525i59.94 NTSC 4:3", "625i50 PAL 4:3",
		"720p60", "720p59.94", "720p50",
		"1080i60", "1080i59.94", "1080i50",
		"1080p60", "1080p59.94", "1080p50", "1080p30", "1080p29.97",
		"1080p25", "1080p24", "1080p23.98",
		"1080PsF30", "1080PsF29.97", "1080PsF25", "1080PsF24", "1080PsF23.98",
		"2160p60", "2160p59.94", "2160p50", "2160p30", "2160p29.97",
		"2160p25", "2160p24", "2160p23.98",
		"4320p60", "4320p59.94", "4320p50", "4320p30", "4320p29.97",
		"4320p25", "4320p24", "4320p23.98"}
)

// Controls is the document's table, 289 rows in its order, page by page.
var Controls = []Control{
	r("Matte Density"), r("Black Gloss"), r("Blue Density"),
	r("Green Density"), r("Red Density"), r("Shadow Level"),
	r("Shadow Threshold"),
	rr("Matte Correct Horizontal Size", 0, 6),
	rr("Matte Correct Vertical Size", 0, 3),
	r("Cursor X"), r("Cursor Y"), r("Cursor 2 X"), r("Cursor 2 Y"),
	r("Veil Master"), r("Veil Red"), r("Veil Green"), r("Veil Blue"),
	rr("Veil Correct Horizontal Size", 0, 6),
	rr("Veil Correct Vertical Size", 0, 6, note(footnote1)),
	r("Wall Color Red"), r("Wall Color Green"), r("Wall Color Blue"),
	r("Floor Color Red"), r("Floor Color Green"), r("Floor Color Blue"),
	r("Cleanup Level"), r("Cleanup Dark Recover"),
	r("Cleanup Light Recover"), r("Cleanup Strength"),
	r("GM Cleanup Level"), r("GM Cleanup Dark Recover"),
	r("GM Cleanup Light Recover"), r("GM Cleanup Strength"),
	r("Correction Level"), r("Noise Level"), r("Black Balance"),
	r("Gray Balance"),
	// page 11
	r("White Balance"), r("Flare Level"), r("Cool"), r("Skin Tone"),
	r("Light Warm"), r("Dark Warm"),
	rr("Flare Correct Horizontal Size", 0, 6),
	rr("Flare Correct Vertical Size", 0, 6, note(footnote1)),
	r("Ambiance Master"), r("Ambiance Red"), r("Ambiance Green"),
	r("Ambiance Blue"), r("Ambiance Strength"),
	r("Direct Light Red"), r("Direct Light Green"),
	r("Direct Light Blue"), r("Direct Light Mix"),
	r("Vertical Blur"),
	r("FG Saturation Red"), r("FG Saturation Green"),
	r("FG Saturation Blue"), r("FG Saturation Master"),
	r("FG Contrast Red"), r("FG Contrast Green"), r("FG Contrast Blue"),
	r("FG Contrast Master"),
	r("FG Black Red"), r("FG Black Green"), r("FG Black Blue"),
	r("FG Black Master"),
	r("FG White Red"), r("FG White Green"), r("FG White Blue"),
	r("FG White Master"),
	r("FG Contrast Crossover"), r("Fade Mix"),
	r("BG Saturation Red"),
	// page 12
	r("BG Saturation Green"), r("BG Saturation Blue"),
	r("BG Saturation Master"),
	r("BG Contrast Red"), r("BG Contrast Green"), r("BG Contrast Blue"),
	r("BG Contrast Master"),
	r("BG Black Red"), r("BG Black Green"), r("BG Black Blue"),
	r("BG Black Master"),
	r("BG White Red"), r("BG White Green"), r("BG White Blue"),
	r("BG White Master"),
	r("BG Contrast Crossover"), r("BG Filter"),
	r("Test Signal Master"), r("Test Signal Red"),
	r("Test Signal Green"), r("Test Signal Blue"),
	r("LY Saturation Red"), r("LY Saturation Green"),
	r("LY Saturation Blue"), r("LY Saturation Master"),
	r("LY Contrast Red"), r("LY Contrast Green"), r("LY Contrast Blue"),
	r("LY Contrast Master"),
	r("LY Black Red"), r("LY Black Green"), r("LY Black Blue"),
	r("LY Black Master"),
	r("LY White Red"), r("LY White Green"), r("LY White Blue"),
	r("LY White Master"),
	// page 13
	r("LY Contrast Crossover"), r("LY Filter"),
	r("LY Test Signal Master"), r("LY Test Signal Red"),
	r("LY Test Signal Green"), r("LY Test Signal Blue"),
	r("LY Fade Mix"),
	r("Lighting Level Red"), r("Lighting Level Green"),
	r("Lighting Level Blue"), r("Lighting Level Master"),
	r("Lighting Minimum Level"),
	win("Window Position Top", "height"),
	win("Window Position Bottom", "height"),
	win("Window Position Left", "width"),
	win("Window Position Right", "width"),
	r("Window Softness Top"), r("Window Softness Bottom"),
	r("Window Softness Left"), r("Window Softness Right"),
	r("Window Skew Top"), r("Window Skew Bottom"),
	r("Window Skew Left"), r("Window Skew Right"),
	r("Window Skew Offset Top"), r("Window Skew Offset Bottom"),
	r("Window Skew Offset Left"), r("Window Skew Offset Right"),
	rr("Transition Rate", 1, 120),
	rr("BM Process Horizontal", 0, 3), rr("BM Process Vertical", 0, 3),
	r("BM Filter"), r("BM Input Level"), r("BM Input Offset"),
	rr("GM Process Horizontal", 0, 3), rr("GM Process Vertical", 0, 3),
	r("GM Filter"),
	// page 14
	r("GM Input Level"), r("GM Input Offset"),
	rr("HM Process Horizontal", 0, 3), rr("HM Process Vertical", 0, 3),
	r("HM Filter"), r("HM Input Level"), r("HM Input Offset"),
	rr("LM Process Horizontal", 0, 3), rr("LM Process Vertical", 0, 3),
	r("LM Filter"), r("LM Input Level"), r("LM Input Offset"),
	r("Noise Cursor X"), r("Noise Cursor Y"),
	rr("FG Input Frame Delay", 0, 14),
	r("FG Input U Position"), r("FG Input V Position"),
	r("FG Input UV Position"),
	r("Talent Highlight Level", limit(NotHD)),
	r("Monitor Highlight Level"), r("Matte Out Level"),
	rr("Output Offset", -1500, 1500),
	rr("GP Out Delay", 1, 120, limit(NotMini)),
	rr("GP 1 Input Delay", 1, 120, limit(NotMini)),
	rr("GP 2 Input Delay", 1, 120, limit(NotMini)),
	rr("GP 3 Input Delay", 1, 120, limit(NotMini)),
	rr("GP 4 Input Delay", 1, 120, limit(NotMini)),
	rr("GP 5 Input Delay", 1, 120, limit(NotMini)),
	b("Matte Enable"), b("Screen Correct"), b("GM Cleanup Enable"),
	b("Noise Enable"), b("Noise Cursor Enable"), b("FG Freeze"),
	b("FG Advanced Contrast Enable"), b("Advanced Flare Enable"),
	b("HM Flare Enable"),
	// page 15
	b("Ambiance Enable"), b("BG Gradient Enable"), b("BG Freeze"),
	b("BG Advanced Contrast Enable"), b("BG Test Signal Enable"),
	b("LY Input Enable"), b("LY Advanced Contrast Enable"),
	b("LY Freeze", limit(NotHD)), b("LY Test Signal Enable"),
	b("Lighting Enable"), b("Window Enable"), b("Window BM Enable"),
	b("Window GM Enable"), b("Window HM Enable"), b("Window LM Enable"),
	b("Window Invert"), b("Wall Cursor Position Enable"),
	b("Floor Cursor Position Enable"), b("Dual Cursor"),
	b("Manual Color Enable"),
	// The table's "Custom Powerup(deprecated)": the wire name is taken to
	// be without the annotation.
	b("Custom Powerup", note("deprecated in the document")),
	b("BM Enable"), b("BM Invert"), b("BM Process Invert"),
	b("BM Freeze", limit(NotHD)),
	b("GM Enable"), b("GM Invert"), b("GM Process Invert"),
	b("GM Freeze", limit(NotMini)),
	b("HM Enable"), b("HM Invert"), b("HM Process Invert"),
	b("HM Freeze", limit(NotMini)),
	b("LM Invert"), b("LM Process Invert"),
	b("Monitor To Program"), b("Monitor To Talent", limit(NotHD)),
	// page 16
	b("Fill Linear Mix Correction", limit(NotMini)),
	b("Talent Mirror", limit(NotHD)), b("Monitor Cascade"),
	b("Matte Out Invert", limit(NotMini)),
	b("On Air Enable"), b("On Air Lockout"),
	b("Matte View Range"), b("Matte View Invert"),
	b("Monitor Out RGB"), b("Monitor Out Red Only"),
	b("Monitor Out Green Only"), b("Monitor Out Blue Only"),
	b("GP Out Save", limit(NotMini)),
	b("Quickload 1"), b("Quickload 2"), b("Quickload 3"),
	b("Quickload 4"), b("Quickload 5"),
	b("Quicksave 1"), b("Quicksave 2"), b("Quicksave 3"),
	b("Quicksave 4"), b("Quicksave 5"),
	b("GP 1 Input Enable", limit(NotMini)),
	b("GP 2 Input Enable", limit(NotMini)),
	b("GP 3 Input Enable", limit(NotMini)),
	b("GP 4 Input Enable", limit(NotMini)),
	b("GP 5 Input Enable", limit(NotMini)),
	b("GP 1 High Enable", limit(NotMini)),
	b("GP 2 High Enable", limit(NotMini)),
	b("GP 3 High Enable", limit(NotMini)),
	b("GP 4 High Enable", limit(NotMini)),
	b("GP 5 High Enable", limit(NotMini)),
	{Name: "Tally Active", Kind: KindReadOnly, Note: "On/Off {Read Only}"},
	ch("3G SDI level", []string{"A", "B"}),
	ch("Color Space", []string{"Rec.709", "Rec.2020"}, limit(NotMini)),
	ch("Filter Mode", []string{"Median", "Average"}),
	// page 17
	ch("Filter Median", []string{"0", "1", "2", "3", "4"}),
	ch("Filter Average", []string{"0", "1", "2", "3", "4"}),
	ch("LY In Mix Mode", []string{"Realistic", "Linear", "Additive"}),
	ch("Backing Color", []string{"Red", "Green", "Blue"}),
	ch("Cursor Position", []string{"Default", "Last"}),
	ch("GP Out Level", []string{"High", "Low"}, limit(NotMini)),
	ch("Output Range", []string{"Normal", "Full"}, minProtocol(2, 0)),
	ch("Monitor Out", MonitorOut),
	ch("Layer Order", LayerOrder),
	ch("Video Format", VideoFormats, note("device dependent: 4320p on the 12 8K only, no PsF on the HD Mini")),
	fn("Factory Defaults"), fn("User Defaults"), fn("Auto Screen Sample"),
	fn("Screen Capture"), fn("Noise Select"), fn("Sample Wall"),
	fn("Sample Floor"), fn("Matte Reset"), fn("Cleanup Reset"),
	fn("GM Cleanup Reset"), fn("FG Color Reset"), fn("BG Color Reset"),
	fn("Flare Reset"),
	// page 18
	fn("Ambiance Reset"), fn("BG Test Signal Color Reset"),
	fn("LY Color Reset"), fn("LY Test Signal Color Reset"),
	fn("Window Reset"), fn("Window Skew Reset"),
	fn("GP Out", limit(NotMini)),
}

var byKey = func() map[string]Control {
	m := make(map[string]Control, len(Controls))
	for _, c := range Controls {
		m[pytext.Lower(c.Name)] = c
	}
	return m
}()

// Lookup is lookup: the documented control called name, any case and
// spacing.
func Lookup(name string) (Control, bool) {
	c, ok := byKey[pytext.Lower(strings.Join(pytext.Fields(name), " "))]
	return c, ok
}

// ParseBool is parse_bool: on/true/yes/1 and off/false/no/0, any case.
func ParseBool(text string) (v, ok bool) {
	switch pytext.Lower(pytext.Strip(text)) {
	case "on", "true", "yes", "1":
		return true, true
	case "off", "false", "no", "0":
		return false, true
	}
	return false, false
}

// ParseInt is parse_int: a decimal integer, a leading + dropped.
func ParseInt(text string) (int, bool) {
	t := strings.TrimPrefix(pytext.Strip(text), "+")
	n, err := pytext.Int(t)
	return int(n), err == nil
}

// ParseValue is parse_value: a CONTROL line's value typed by the table.
// A range gives an int, an On/Off, Tally Active or a function a bool, a
// choice the string as the unit sent it; a value that doesn't parse as
// its kind, and every control the table lacks, stays the string, so
// reading never fails on an odd unit.
func ParseValue(name, text string) any {
	raw := pytext.Strip(text)
	c, ok := Lookup(name)
	if !ok {
		return raw
	}
	switch c.Kind {
	case KindRange:
		if n, ok := ParseInt(raw); ok {
			return n
		}
	case KindBool, KindReadOnly, KindFunction:
		if v, ok := ParseBool(raw); ok {
			return v
		}
	}
	return raw
}

var frames = map[string][2]int{"525": {720, 486}, "625": {720, 576}, "720": {1280, 720},
	"1080": {1920, 1080}, "2160": {3840, 2160}, "4320": {7680, 4320}}

var leadingDigits = regexp.MustCompile(`^\s*(\d{3,4})`)

// FrameSize is frame_size: the width and height of a format name
// (1080p25, 2160p59.94, 525i59.94 NTSC 4:3); ok false for auto detect or
// a name it doesn't read.
func FrameSize(videoFormat string) (width, height int, ok bool) {
	m := leadingDigits.FindStringSubmatch(videoFormat)
	if m == nil {
		return 0, 0, false
	}
	f, ok := frames[m[1]]
	return f[0], f[1], ok
}

// Context is what the value checks know about the unit: its family, its
// protocol version (nil: not known), the running format and its own
// VIDEO FORMATS list.
type Context struct {
	Family       string
	Protocol     *[2]int
	VideoFormat  string
	VideoFormats []string
}

func controlValueError(format string, args ...any) *Error {
	return &Error{Class: "ControlValueError", Msg: fmt.Sprintf(format, args...), kind: ErrControlValue}
}

func unsupportedError(format string, args ...any) *Error {
	return &Error{Class: "UnsupportedError", Msg: fmt.Sprintf(format, args...), kind: ErrUnsupported}
}

// pyRepr is how a value shows in ultimattewire's messages ({value!r}).
func pyRepr(v any) string {
	switch x := v.(type) {
	case string:
		return pytext.Repr(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	}
	return fmt.Sprint(v)
}

func protoString(p [2]int) string { return fmt.Sprintf("%d.%d", p[0], p[1]) }

// FormatValue is format_value: value checked against c and given as the
// wire text, or an *Error of kind ErrControlValue (a value the control
// can't take) or ErrUnsupported (a control this model or protocol lacks).
// A bool goes out on/off and a choice in lower case, as a 12 4K reports
// them. value is a bool, an int or a string.
func FormatValue(c Control, value any, ctx Context) (string, error) {
	if !c.AvailableOn(ctx.Family) {
		return "", unsupportedError("'%s' is not available on the %s (protocol document, footnote %s)", c.Name, ctx.Family, c.Limit)
	}
	if c.MinProtocol != nil && ctx.Protocol != nil && less(*ctx.Protocol, *c.MinProtocol) {
		return "", unsupportedError("'%s' needs protocol %s or later; the unit speaks %s", c.Name, protoString(*c.MinProtocol), protoString(*ctx.Protocol))
	}
	switch c.Kind {
	case KindReadOnly:
		return "", controlValueError("'%s' is read only", c.Name)
	case KindFunction:
		if value != nil && value != true {
			if v, ok := ParseBool(fmt.Sprint(value)); !ok || !v {
				return "", controlValueError("'%s' is a function: the only value is yes", c.Name)
			}
		}
		return "yes", nil
	case KindBool:
		v, ok := value.(bool)
		if !ok {
			v, ok = ParseBool(fmt.Sprint(value))
		}
		if !ok {
			return "", controlValueError("'%s' is on/off, not %s", c.Name, pyRepr(value))
		}
		if v {
			return "on", nil
		}
		return "off", nil
	case KindRange:
		var v int
		switch x := value.(type) {
		case int:
			v = x
		case bool:
			return "", controlValueError("'%s' takes an integer, not %s", c.Name, pyRepr(value))
		default:
			n, ok := ParseInt(fmt.Sprint(value))
			if !ok {
				return "", controlValueError("'%s' takes an integer, not %s", c.Name, pyRepr(value))
			}
			v = n
		}
		hi, hasHi := MaximumFor(c, ctx)
		if c.Min != nil && v < *c.Min || hasHi && v > hi {
			his := "?"
			if hasHi {
				his = strconv.Itoa(hi)
			}
			return "", controlValueError("'%s' is %d-%s, not %d", c.Name, *c.Min, his, v)
		}
		return strconv.Itoa(v), nil
	}
	text := strings.Join(pytext.Fields(fmt.Sprint(value)), " ")
	allowed := c.Choices
	if c.Name == "Video Format" {
		allowed = VideoFormatsFor(ctx)
	}
	for _, choice := range allowed {
		if strings.EqualFold(choice, text) {
			return strings.ToLower(choice), nil
		}
	}
	return "", controlValueError("'%s' is one of %s; not %s", c.Name, strings.Join(allowed, ", "), pyRepr(value))
}

func less(a, b [2]int) bool { return a[0] < b[0] || a[0] == b[0] && a[1] < b[1] }

// MaximumFor is maximum_for: c's maximum, the running format's height or
// width for a Window Position control; ok false when there is none to
// check (a window control in an unknown format).
func MaximumFor(c Control, ctx Context) (int, bool) {
	if c.Window == "" {
		if c.Max == nil {
			return 0, false
		}
		return *c.Max, true
	}
	w, h, ok := FrameSize(ctx.VideoFormat)
	if !ok {
		return 0, false
	}
	if c.Window == "height" {
		return h, true
	}
	return w, true
}

// VideoFormatsFor is video_formats_for: the unit's own VIDEO FORMATS list
// when it sent one, else the document's cut by footnote 4 (4320p on the
// 12 8K only, no PsF on the HD Mini).
func VideoFormatsFor(ctx Context) []string {
	if len(ctx.VideoFormats) > 0 {
		return ctx.VideoFormats
	}
	var out []string
	for _, f := range VideoFormats {
		if strings.HasPrefix(f, "4320") && ctx.Family != "" && ctx.Family != Model8K {
			continue
		}
		if strings.Contains(f, "PsF") && ctx.Family == ModelHDMini {
			continue
		}
		out = append(out, f)
	}
	return out
}

// FormatOffset is format_offset: a relative change checked ("Only
// controls with numerical ranges support this relative mode"). The unit
// clamps or refuses the result.
func FormatOffset(c Control, delta any, ctx Context) (string, error) {
	if c.Kind != KindRange {
		return "", controlValueError("'%s' has no numerical range; Offset needs one", c.Name)
	}
	if !c.AvailableOn(ctx.Family) {
		return "", unsupportedError("'%s' is not available on the %s", c.Name, ctx.Family)
	}
	switch x := delta.(type) {
	case int:
		return strconv.Itoa(x), nil
	case bool:
	default:
		if n, ok := ParseInt(fmt.Sprint(delta)); ok {
			return strconv.Itoa(n), nil
		}
	}
	return "", controlValueError("an offset is an integer, not %s", pyRepr(delta))
}
