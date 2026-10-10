// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Switching ops (bus, transition, fade to black, aux, colour generators,
// video mode): bmdwire v1.3.0 atemwire/macrotransfer/switching.py.

import (
	"math/big"
	"strings"
)

// --- Bus / transition ---

func dProgramInput(p params) []Param {
	return []Param{{mEBI, p.str(0)}, {"input", sourceToSymbol(int(p.u16(2)))}}
}

func dPreviewInput(p params) []Param { return dProgramInput(p) }

// dAuxiliaryInput: [u8 auxiliaryIndex, u8 _, u16 source].
func dAuxiliaryInput(p params) []Param {
	return []Param{{"auxiliaryIndex", p.str(0)}, {"input", sourceToSymbol(int(p.u16(2)))}}
}

func dCut(p params) []Param         { return []Param{{mEBI, p.str(0)}} }
func dAuto(p params) []Param        { return dCut(p) }
func dFadeToBlack(p params) []Param { return dCut(p) }

// transitionStyles is _TRANSITION_STYLES.
var transitionStyles = map[int]string{0: "Mix", 1: "Dip", 2: "Wipe", 3: "DVE", 4: "Stinger"}

var transitionStyleToInt = invert(transitionStyles)

func dTransitionStyle(p params) []Param {
	me := p.str(0)
	style, ok := transitionStyles[p.at(1)]
	if !ok {
		style = p.str(1)
	}
	return []Param{{mEBI, me}, {"style", style}}
}

// resolveLayerMask is _resolve_layer_mask. A mask of 0 decodes as
// Background (SURFACE §5.4 item 9).
func resolveLayerMask(m uint16) string {
	var parts []string
	if m&0x01 != 0 {
		parts = append(parts, "Background")
	}
	for k := 1; k < 5; k++ {
		if m&(1<<k) != 0 {
			parts = append(parts, "Key"+string(rune('0'+k)))
		}
	}
	if len(parts) == 0 {
		return "Background"
	}
	s := parts[0]
	for _, x := range parts[1:] {
		s += "," + x
	}
	return s
}

func dTransitionSource(p params) []Param {
	return []Param{{mEBI, p.str(0)}, {"source", resolveLayerMask(p.u16(2))}}
}

// dFTBEnabled: [u8 mixEffectBlockIndex, u8 enabled, u16 0]. The M/E byte
// is whatever ATEM Software Control left there; it round-trips unchanged.
func dFTBEnabled(p params) []Param {
	return []Param{{mEBI, p.str(0)}, {"enabled", boolStr(p.at(1) != 0)}}
}

// dVideoMode: [u8 mode, 3 pad], named from videoModeXMLNames.
func dVideoMode(p params) []Param {
	m := p.at(0)
	name, ok := videoModeXMLNames[m]
	if !ok {
		name = p.str(0)
	}
	return []Param{{"videoMode", name}}
}

func dTransitionMixRate(p params) []Param { return dMEMarkerU16(p, "rate") }
func eTransitionMixRate(a attrs) []byte   { return eMEMarkerU16(a, "rate", famMix) }

func dTransitionDipRate(p params) []Param { return dMEMarkerU16(p, "rate") }
func eTransitionDipRate(a attrs) []byte   { return eMEMarkerU16(a, "rate", famDip) }

func dTransitionDipInput(p params) []Param { return dMEMarkerSource(p, "input") }
func eTransitionDipInput(a attrs) []byte   { return eMEMarkerSource(a, famDip, "input") }

func dTransitionWipeRate(p params) []Param { return dMEMarkerU16(p, "rate") }
func eTransitionWipeRate(a attrs) []byte   { return eMEMarkerU16(a, "rate", famWipe) }

func dTransitionWipePattern(p params) []Param {
	v := int(p.u16(2))
	name, ok := wipePatternNames[v]
	if !ok {
		name = utoa(uint16(v))
	}
	return []Param{{mEBI, p.str(0)}, {"pattern", name}}
}

func eTransitionWipePattern(a attrs) []byte {
	me := a.masked8(mEBI)
	pat := enumOrInt(wipePatternToInt, a.req("pattern"), "wipe pattern")
	return cat([]byte{me, famWipe}, u16le(pat))
}

func dTransitionWipeBorderSoftness(p params) []Param { return dMEDoubleMarkerFP(p, "softness") }
func eTransitionWipeBorderSoftness(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "softness", famWipe, opMarker026C)
}

func dTransitionWipeBorderWidth(p params) []Param { return dMEDoubleMarkerFP(p, "width") }
func eTransitionWipeBorderWidth(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "width", famWipe, opMarker026C)
}

func dTransitionWipeBorderFillInput(p params) []Param { return dMEMarkerSource(p, "input") }
func eTransitionWipeBorderFillInput(a attrs) []byte   { return eMEMarkerSource(a, famWipe, "input") }

func dTransitionWipeXPosition(p params) []Param { return dMEDoubleMarkerFP(p, "xPosition") }
func eTransitionWipeXPosition(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "xPosition", famWipe, opMarker026C)
}

func dTransitionWipeYPosition(p params) []Param { return dMEDoubleMarkerFP(p, "yPosition") }
func eTransitionWipeYPosition(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "yPosition", famWipe, opMarker026C)
}

func dTransitionWipeDVEFlipFlop(p params) []Param { return dMEValueMarkerBool(p, "flipFlop") }
func eTransitionWipeDVEFlipFlop(a attrs) []byte {
	return eMEValueMarkerBool(a, "flipFlop", opMarker026C)
}

func dTransitionWipeDVEReverse(p params) []Param { return dMEValueMarkerBool(p, "reverse") }
func eTransitionWipeDVEReverse(a attrs) []byte {
	return eMEValueMarkerBool(a, "reverse", opMarker026C)
}

func dTransitionDVERate(p params) []Param { return dMEMarkerU16(p, "rate") }
func eTransitionDVERate(a attrs) []byte   { return eMEMarkerU16(a, "rate", famDVE) }

func dTransitionDVEPattern(p params) []Param {
	v := p.at(1)
	name, ok := dvePatternNames[v]
	if !ok {
		name = p.str(1)
	}
	return []Param{{mEBI, p.str(0)}, {"pattern", name}}
}

func eTransitionDVEPattern(a attrs) []byte {
	me := a.masked8(mEBI)
	pat := enumOrInt(dvePatternToInt, a.req("pattern"), "DVE pattern")
	return cat([]byte{me, byte(mask(pat, 8))}, marker(opMarker026C))
}

func dTransitionDVEFillInput(p params) []Param { return dMEMarkerSource(p, "input") }
func eTransitionDVEFillInput(a attrs) []byte   { return eMEMarkerSource(a, famDVE, "input") }

func dTransitionDVECutInput(p params) []Param { return dMEMarkerSource(p, "input") }
func eTransitionDVECutInput(a attrs) []byte   { return eMEMarkerSource(a, famDVE, "input") }

func dTransitionDVECutInputEnable(p params) []Param { return dMEValueMarkerBool(p, "enable") }
func eTransitionDVECutInputEnable(a attrs) []byte {
	return eMEValueMarkerBool(a, "enable", opMarker026C)
}

func dTransitionStingerSourceMediaPlayer(p params) []Param {
	return []Param{{mEBI, p.str(0)}, {"mediaPlayer", utoa(p.u16(2))}}
}

func eTransitionStingerSourceMediaPlayer(a attrs) []byte {
	me := a.masked8(mEBI)
	return cat([]byte{me, famStinger}, u16le(a.int("mediaPlayer")))
}

func dTransitionStingerPreRoll(p params) []Param { return dMEMarkerU16(p, "preRoll") }
func eTransitionStingerPreRoll(a attrs) []byte   { return eMEMarkerU16(a, "preRoll", famStinger) }

func dTransitionStingerClipDuration(p params) []Param { return dMEMarkerU16(p, "clipDuration") }
func eTransitionStingerClipDuration(a attrs) []byte {
	return eMEMarkerU16(a, "clipDuration", famStinger)
}

func dTransitionStingerTriggerPoint(p params) []Param { return dMEMarkerU16(p, "triggerPoint") }
func eTransitionStingerTriggerPoint(a attrs) []byte {
	return eMEMarkerU16(a, "triggerPoint", famStinger)
}

func dTransitionStingerMixRate(p params) []Param { return dMEMarkerU16(p, "mixRate") }
func eTransitionStingerMixRate(a attrs) []byte   { return eMEMarkerU16(a, "mixRate", famStinger) }

func dTransitionStingerDVEClip(p params) []Param { return dMEDoubleMarkerFP(p, "clip") }
func eTransitionStingerDVEClip(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "clip", famStinger, opMarker026C)
}

func dTransitionStingerDVEGain(p params) []Param { return dMEDoubleMarkerFP(p, "gain") }
func eTransitionStingerDVEGain(a attrs) []byte {
	return eMEDoubleMarkerFP(a, "gain", famStinger, opMarker026C)
}

func dTransitionStingerDVEInvert(p params) []Param { return dMEValueMarkerBool(p, "invert") }
func eTransitionStingerDVEInvert(a attrs) []byte {
	return eMEValueMarkerBool(a, "invert", opMarker026C)
}

func dTransitionStingerDVEPreMultiply(p params) []Param { return dMEValueMarkerBool(p, "preMultiply") }
func eTransitionStingerDVEPreMultiply(a attrs) []byte {
	return eMEValueMarkerBool(a, "preMultiply", opMarker026C)
}

func dColorGenHue(p params) []Param { return dGenMarkerFP(p, "hue") }
func eColorGenHue(a attrs) []byte   { return eGenMarkerFP(a, "hue") }

func dColorGenSaturation(p params) []Param { return dGenMarkerFP(p, "saturation") }
func eColorGenSaturation(a attrs) []byte   { return eGenMarkerFP(a, "saturation") }

func dColorGenLuminescence(p params) []Param { return dGenMarkerFP(p, "luminescence") }
func eColorGenLuminescence(a attrs) []byte   { return eGenMarkerFP(a, "luminescence") }

func dFTBRate(p params) []Param { return dMEMarkerU16(p, "rate") }
func eFTBRate(a attrs) []byte   { return eMEMarkerU16(a, "rate", famFTBRate) }

// --- Encoders for the bus ops ---

func eProgramInput(a attrs) []byte {
	src := symbolToSource(a.req("input"))
	return cat(pyBytes(a.intOr0(mEBI), num(0)), u16le(src))
}

func ePreviewInput(a attrs) []byte { return eProgramInput(a) }

func eAuxiliaryInput(a attrs) []byte {
	src := symbolToSource(a.req("input"))
	return cat(pyBytes(a.int("auxiliaryIndex"), num(0)), u16le(src))
}

func eCut(a attrs) []byte         { return pyBytes(a.intOr0(mEBI), num(0), num(0), num(0)) }
func eAuto(a attrs) []byte        { return eCut(a) }
func eFadeToBlack(a attrs) []byte { return eCut(a) }

// eTransitionStyle: a missing style is Mix, and so is an unknown one,
// numbers included (SURFACE §5.4 item 9).
func eTransitionStyle(a attrs) []byte {
	style, ok := a["style"]
	if !ok {
		style = "Mix"
	}
	n := transitionStyleToInt[style] // 0, Mix, when unknown
	return pyBytes(a.intOr0(mEBI), num(int64(n)), num(0), num(0))
}

// packLayerMask is _pack_layer_mask.
func packLayerMask(s string) int64 {
	parts := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = pyStrip(p); p != "" {
			parts[p] = true
		}
	}
	var m int64
	if parts["Background"] {
		m |= 0x01
	}
	for k := 1; k < 5; k++ {
		if parts["Key"+string(rune('0'+k))] {
			m |= 1 << k
		}
	}
	return m
}

func eTransitionSource(a attrs) []byte {
	me := pyBytes(a.intOr0(mEBI), num(0))
	return cat(me, u16le(num(packLayerMask(a["source"]))))
}

// eFTBEnabled writes the M/E byte back as given (mod 256).
func eFTBEnabled(a attrs) []byte {
	me := a.masked8(mEBI)
	enabled := truthy3(a.req("enabled"))
	return []byte{me, b2i(enabled), 0, 0}
}

func eVideoMode(a attrs) []byte {
	mode := a.req("videoMode")
	var n *big.Int
	if v, ok := videoModeNames[mode]; ok {
		n = num(int64(v))
	} else {
		var exc *pyExc
		if n, exc = parseInt(mode, 10); exc != nil {
			raise("ValueError", "unknown videoMode %s", pyRepr(mode))
		}
	}
	if n.Sign() < 0 || n.Cmp(num(0xFF)) > 0 {
		raise("ValueError", "videoMode out of range: %s", n.String())
	}
	return []byte{byte(n.Int64()), 0, 0, 0}
}

// videoModeNames is atemwire.messages.input_video.VIDEO_MODE_NAMES, the
// names the VideoMode op's encoder accepts. It is atem.VideoModeNames; the
// codec keeps its own copy so it doesn't import the session package (a
// test checks the two agree).
var videoModeNames = map[string]uint8{
	"NTSC": 0, "525i5994": 0, "PAL": 1, "625i50": 1,
	"NTSC_widescreen": 2, "PAL_widescreen": 3,
	"720p50": 4, "720p5994": 5, "720p60": 28,
	"1080i50": 6, "1080i5994": 7, "1080i60": 29,
	"1080p2398": 8, "1080p24": 9, "1080p25": 10, "1080p2997": 11, "1080p30": 26,
	"1080p50": 12, "1080p5994": 13, "1080p60": 27,
	"2160p2398": 14, "2160p24": 15, "2160p25": 16, "2160p2997": 17, "2160p50": 18, "2160p5994": 19,
	"4320p2398": 20, "4320p24": 21, "4320p25": 22, "4320p2997": 23, "4320p50": 24, "4320p5994": 25,
}

// videoModeXMLNames is input_video.VIDEO_MODE_XML_NAMES: one canonical
// name per mode, the one the VideoMode op decodes to.
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
