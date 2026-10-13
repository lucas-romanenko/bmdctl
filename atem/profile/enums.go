// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The format constants and enum tables of bmdwire v1.3.0
// atemwire/profile/_enums.py, the video mode name tables profile save and
// apply share (atemwire/messages/input_video.py), and the name tables the
// Fairlight operations take (atemwire/messages/fairlight.py).
// _enums.py's _source_symbol is not ported: nothing in atemwire calls it.

// The profile format version Save writes: PROFILE_MAJOR_VERSION and
// PROFILE_MINOR_VERSION.
const (
	MajorVersion = 2
	MinorVersion = 1
)

// wipePatternNames is WIPE_PATTERN_NAMES: the wipe and key pattern names
// by wire value (0-17), as ATEM Software Control writes them.
var wipePatternNames = []string{
	"LeftToRightBar", "TopToBottomBar",
	"HorizontalBarnDoor", "VerticalBarnDoor",
	"CornersInFourBox",
	"RectangleIris", "DiamondIris", "CircleIris",
	"TopLeftBox", "TopRightBox", "BottomRightBox", "BottomLeftBox",
	"TopCentreBox", "RightCentreBox", "BottomCentreBox", "LeftCentreBox",
	"TopLeftDiagonal", "TopRightDiagonal",
}

// dveEffectNames is DVE_EFFECT_NAMES: the DVE transition effects, wire
// 16-31.
var dveEffectNames = map[int]string{
	16: "SqueezeTopLeft", 17: "SqueezeTop", 18: "SqueezeTopRight",
	19: "SqueezeLeft", 20: "SqueezeRight", 21: "SqueezeBottomLeft",
	22: "SqueezeBottom", 23: "SqueezeBottomRight",
	24: "PushTopLeft", 25: "PushTop", 26: "PushTopRight",
	27: "PushLeft", 28: "PushRight", 29: "PushBottomLeft",
	30: "PushBottom", 31: "PushBottomRight",
}

// uskTypeNames is USK_TYPE_NAMES.
var uskTypeNames = map[int]string{0: "Luma", 1: "Chroma", 2: "Pattern", 3: "DVE"}

// transStyleNames is TRANS_STYLE_NAMES.
var transStyleNames = map[int]string{0: "Mix", 1: "Dip", 2: "Wipe", 3: "DVE", 4: "Stinger"}

// mixOptionNames is MIX_OPTION_NAMES: a Fairlight strip's mix option bit.
var mixOptionNames = map[int]string{0x01: "Off", 0x02: "On", 0x04: "AudioFollowVideo"}

// The Fairlight strip ids, from bmdwire#23 (d86ed31). An <AudioSource id>
// is the strip's 8-byte wire address (the one CFSP, CEBP and FASP carry:
// six 0xFF bytes, the split byte, the channel byte) read as a signed i64;
// macro ops carry the same value unsigned as sourceId. Captured from an
// ASC Save Switcher State with input 1 in Dual Mono:
//
//	-65280  ff..ff 01 00  a stereo strip: channel -1 in the setters
//	-256    ff..ff ff 00  split, channel 0: a dual-mono left half, and
//	                      the natively mono XLR (1301)
//	-255    ff..ff ff 01  split, channel 1: a dual-mono right half
const (
	audioSourceStereoID = -65280 // AUDIO_SOURCE_STEREO_ID
	audioSourceSplitID  = -256   // AUDIO_SOURCE_SPLIT_ID, plus the channel
)

// audioSourceID is audio_source_id: the <AudioSource id> of the strip the
// setters address as channel, -1 for stereo, 0 or 1 for a split half.
func audioSourceID(channel int) int {
	if channel == -1 {
		return audioSourceStereoID
	}
	return audioSourceSplitID + channel
}

// audioSourceChannel is audio_source_channel: the setters' channel for an
// <AudioSource id>, 0 or 1 when its split byte is 0xFF, else -1 (stereo).
// The id is int()'s, masked to 64 bits, so the unsigned form a macro's
// sourceId holds names the same channel. An absent id, or one int()
// refuses, is stereo, as apply always took it.
func audioSourceChannel(id *string) int {
	if id == nil {
		return -1
	}
	low, ok := pyIntMod(*id, 1<<16)
	if !ok {
		return -1
	}
	if low>>8 == 0xFF && low&0xFF <= 1 {
		return int(low & 0xFF)
	}
	return -1
}

// eqShapeNames is EQ_SHAPE_NAMES: an EQ band's filter bit.
var eqShapeNames = map[int]string{
	0x01: "LowShelf", 0x02: "LowPass", 0x04: "BandPass",
	0x08: "Notch", 0x10: "HighPass", 0x20: "HighShelf",
}

// eqFreqRangeNames is EQ_FREQ_RANGE_NAMES: an EQ band's frequency range bit.
var eqFreqRangeNames = map[int]string{1: "Low", 2: "MidLow", 4: "MidHigh", 8: "High"}

// extPortTypes is EXT_PORT_TYPE_NAMES in its order: _ext_port_type_name
// takes the first bit set.
var extPortTypes = []struct {
	bit  int
	name string
}{{1, "SDI"}, {2, "HDMI"}, {4, "Component"}, {8, "Composite"}, {16, "SVideo"}}

// inverse turns a name table around: the *_TO_INT tables.
func inverse(m map[int]string) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

var (
	wipePatternToInt = func() map[string]int {
		m := map[string]int{}
		for i, n := range wipePatternNames {
			m[n] = i
		}
		return m
	}()
	dveEffectToInt  = inverse(dveEffectNames)
	uskTypeToInt    = inverse(uskTypeNames)
	transStyleToInt = inverse(transStyleNames)
)

// videoModeNames is input_video.VIDEO_MODE_NAMES: every name a profile's
// videoMode may hold, canonical or alias, to its mode number.
var videoModeNames = map[string]int{
	"NTSC": 0, "525i5994": 0,
	"PAL": 1, "625i50": 1,
	"NTSC_widescreen": 2,
	"PAL_widescreen":  3,
	"720p50":          4, "720p5994": 5, "720p60": 28,
	"1080i50": 6, "1080i5994": 7, "1080i60": 29,
	"1080p2398": 8, "1080p24": 9, "1080p25": 10,
	"1080p2997": 11, "1080p30": 26,
	"1080p50": 12, "1080p5994": 13, "1080p60": 27,
	"2160p2398": 14, "2160p24": 15, "2160p25": 16, "2160p2997": 17,
	"2160p50": 18, "2160p5994": 19,
	"4320p2398": 20, "4320p24": 21, "4320p25": 22, "4320p2997": 23,
	"4320p50": 24, "4320p5994": 25,
}

// videoModeXMLNames is input_video.VIDEO_MODE_XML_NAMES: the canonical
// name of each of the 30 modes, the one Save writes. SD 16:9 is
// NTSC_widescreen and PAL_widescreen.
var videoModeXMLNames = map[int]string{
	0: "525i5994", 1: "625i50",
	2: "NTSC_widescreen", 3: "PAL_widescreen",
	4: "720p50", 5: "720p5994", 28: "720p60",
	6: "1080i50", 7: "1080i5994", 29: "1080i60",
	8: "1080p2398", 9: "1080p24", 10: "1080p25",
	11: "1080p2997", 26: "1080p30",
	12: "1080p50", 13: "1080p5994", 27: "1080p60",
	14: "2160p2398", 15: "2160p24", 16: "2160p25", 17: "2160p2997",
	18: "2160p50", 19: "2160p5994",
	20: "4320p2398", 21: "4320p24", 22: "4320p25", 23: "4320p2997",
	24: "4320p50", 25: "4320p5994",
}

// The two SD labels atemwire wrote before 1.3.0 for both aspects
// (apply.py): "525i59.94" says nothing about 4:3 or 16:9 and is skipped;
// "625i50" is also ASC's own name for PAL 4:3 and applies as that.
const (
	sdLabelWithoutAspect     = "525i59.94"
	sdLabelAmbiguousBefore13 = "625i50"
)

// legacyVideoModeLabels is _LEGACY_VIDEO_MODE_LABELS: the decimal spellings
// atemwire up to 1.2.0 saved (get_label() without its aspect) where they
// differ from the canonical names.
var legacyVideoModeLabels = map[string]int{
	"720p59.94":  5,
	"1080i59.94": 7,
	"1080p23.98": 8, "1080p29.97": 11, "1080p59.94": 13,
	"2160p23.98": 14, "2160p29.97": 17, "2160p59.94": 19,
	"4320p23.98": 20, "4320p29.97": 23, "4320p59.94": 25,
}

// videoModeNumber is _video_mode_number: the mode number of a canonical or
// legacy name.
func videoModeNumber(name string) (int, bool) {
	if m, ok := videoModeNames[name]; ok {
		return m, true
	}
	m, ok := legacyVideoModeLabels[name]
	return m, ok
}

// The name tables of the Fairlight operations (fairlight.py):
// FAIRLIGHT_MIX_OPTIONS, FAIRLIGHT_EQ_SHAPES, FAIRLIGHT_EQ_FREQ_RANGES.
var (
	fairlightMixOptions   = map[string]uint8{"Off": 0x01, "On": 0x02, "AudioFollowVideo": 0x04}
	fairlightEQShapes     = map[string]uint8{"LowShelf": 0x01, "LowPass": 0x02, "BandPass": 0x04, "Notch": 0x08, "HighPass": 0x10, "HighShelf": 0x20}
	fairlightEQFreqRanges = map[string]uint8{"Low": 1, "MidLow": 2, "MidHigh": 4, "High": 8}
)

// defaultMultiviewModes is save.py's _DEFAULT_MULTIVIEW_VIDEO_MODES: core
// mode to multiview mode, as the reference export lists them.
var defaultMultiviewModes = [][2]string{
	{"720p50", "720p50"}, {"720p5994", "720p5994"}, {"720p60", "720p60"},
	{"1080i50", "1080i50"}, {"1080i5994", "1080i5994"}, {"1080i60", "1080i60"},
	{"1080p2398", "1080p2398"}, {"1080p24", "1080p24"}, {"1080p25", "1080p25"},
	{"1080p2997", "1080i5994"}, {"1080p30", "1080p30"}, {"1080p50", "1080i50"},
	{"1080p5994", "1080i5994"}, {"1080p60", "1080p60"},
}

// eqBandDefault is one row of _build_eq_bands' defaults: what a band the
// switcher hasn't reported is written as.
type eqBandDefault struct {
	index     int
	enabled   bool
	shape     string
	freqRange string
	frequency int
	q         float64
}

var defaultEQBandsByIndex = []eqBandDefault{
	{0, false, "HighPass", "Low", 46, 0.71},
	{1, true, "LowShelf", "Low", 49, 1},
	{2, true, "BandPass", "MidLow", 171, 2},
	{3, true, "BandPass", "MidHigh", 798, 2},
	{4, true, "HighShelf", "High", 7260, 1},
	{5, false, "LowPass", "High", 12900, 0.71},
}
