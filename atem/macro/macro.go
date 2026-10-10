// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// The op table and the entry points: bmdwire v1.3.0
// atemwire/macrotransfer/__init__.py (_KNOWN_OPS, decode_macro_bytecode,
// encode_single_op, encode_macro_bytecode).

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Param is one attribute of an op, named and formatted as ATEM Software
// Control writes it on a macro XML <Op> element.
type Param struct{ Name, Value string }

// Op is one macro op: atemwire's op dict. ID is the ASC macro XML op id
// ("ProgramInput"), or "Unknown_0xNNNN" for an op code atemwire has no
// codec for. Params are the other attributes, in the order atemwire's dict
// has them, which is the order profile XML writes them in. An unknown op
// has a rawParams attribute (its parameter bytes in hex), and a decodeError
// attribute too if atemwire knew the op code but couldn't decode it.
type Op struct {
	ID     string
	Params []Param
}

// Get returns the value of the attribute name. If Params holds the name
// more than once, the last one counts, as it would building a Python dict
// from the pairs.
func (o Op) Get(name string) (string, bool) {
	for i := len(o.Params) - 1; i >= 0; i-- {
		if o.Params[i].Name == name {
			return o.Params[i].Value, true
		}
	}
	return "", false
}

// Error is an op that can't be encoded: the exception atemwire's
// encode_single_op raises, by Python class and message (str(exc), CPython
// 3.12's wording). Class is "ValueError" (an unknown op id, a missing
// attribute, a value atemwire refuses), "struct.error" (an out-of-range
// value struct.pack refuses), "OSError" (a HyperDeck address inet_aton
// refuses) or "OverflowError" (an infinite fixed-point value). The class
// matters: atemwire's profile apply drops an op that raises ValueError and
// aborts on any other (SURFACE §5.4 item 8).
type Error struct {
	Class   string
	Message string
}

func (e *Error) Error() string { return "macro: " + e.Class + ": " + e.Message }

type opCodec struct {
	code uint16
	id   string
	dec  func(params) []Param
	enc  func(attrs) []byte
}

func op(code uint16, id string, dec func(params) []Param, enc func(attrs) []byte) opCodec {
	return opCodec{code, id, dec, enc}
}

// knownOps is _KNOWN_OPS, in its order: 146 op codes, each with a decoder
// and an encoder.
var knownOps = []opCodec{
	// Bus + transition
	op(0x0002, "ProgramInput", dProgramInput, eProgramInput),
	op(0x0003, "PreviewInput", dPreviewInput, ePreviewInput),
	op(0x0004, "Cut", dCut, eCut),
	op(0x0005, "Auto", dAuto, eAuto),
	op(0x000C, "VideoMode", dVideoMode, eVideoMode),
	op(0x0202, "FadeToBlackEnabled", dFTBEnabled, eFTBEnabled),
	op(0x00A7, "FadeToBlack", dFadeToBlack, eFadeToBlack),
	op(0x0083, "TransitionStyle", dTransitionStyle, eTransitionStyle),
	op(0x0084, "TransitionSource", dTransitionSource, eTransitionSource),
	// Macro flow
	op(0x0007, "MacroSleep", dMacroSleep, eMacroSleep),
	// Aux routing
	op(0x001F, "AuxiliaryInput", dAuxiliaryInput, eAuxiliaryInput),
	// Upstream keyer
	op(0x0025, "KeyCutInput", dKeyCutInput, eKeyCutInput),
	op(0x0026, "KeyFillInput", dKeyFillInput, eKeyFillInput),
	op(0x0027, "KeyOnAir", dKeyOnAir, eKeyOnAir),
	op(0x0028, "KeyType", dKeyType, eKeyType),
	op(0x0029, "LumaKeyClip", dLumaKeyClip, eLumaKeyClip),
	op(0x002A, "LumaKeyGain", dLumaKeyGain, eLumaKeyGain),
	op(0x002B, "KeyFlyEnable", dKeyFlyEnable, eKeyFlyEnable),
	op(0x002C, "LumaKeyInvert", dLumaKeyInvert, eLumaKeyInvert),
	op(0x002D, "LumaKeyPreMultiply", dLumaKeyPreMultiply, eLumaKeyPreMultiply),
	op(0x002F, "KeyMaskEnable", dKeyMaskEnable, eKeyMaskEnable),
	// DVE / fly key: sizes, positions, mask
	op(0x0035, "DVEKeyMaskEnable", dDVEMaskEnable, eDVEMaskEnable),
	op(0x0036, "DVEKeyMaskTop", dDVEMaskTop, eDVEMaskTop),
	op(0x0037, "DVEKeyMaskBottom", dDVEMaskBottom, eDVEMaskBottom),
	op(0x0038, "DVEKeyMaskLeft", dDVEMaskLeft, eDVEMaskLeft),
	op(0x0039, "DVEKeyMaskRight", dDVEMaskRight, eDVEMaskRight),
	op(0x0047, "DVEAndFlyKeyXSize", dDVEXSize, eDVEXSize),
	op(0x0048, "DVEAndFlyKeyYSize", dDVEYSize, eDVEYSize),
	op(0x004A, "DVEAndFlyKeyXPosition", dDVEXPos, eDVEXPos),
	op(0x004B, "DVEAndFlyKeyYPosition", dDVEYPos, eDVEYPos),
	// Downstream keyer
	op(0x0096, "DownstreamKeyFillInput", dDSKFillInput, eDSKFillInput),
	op(0x0097, "DownstreamKeyCutInput", dDSKCutInput, eDSKCutInput),
	op(0x0098, "DownstreamKeyRate", dDSKRate, eDSKRate),
	op(0x0099, "DownstreamKeyAuto", dDSKAuto, eDSKAuto),
	op(0x009A, "DownstreamKeyOnAir", dDSKOnAir, eDSKOnAir),
	op(0x009B, "DownstreamKeyTie", dDSKTie, eDSKTie),
	op(0x009C, "DownstreamKeyClip", dDSKClip, eDSKClip),
	op(0x009D, "DownstreamKeyGain", dDSKGain, eDSKGain),
	op(0x009E, "DownstreamKeyMaskEnable", dDSKMaskEnable, eDSKMaskEnable),
	op(0x00A4, "DownstreamKeyPreMultiply", dDSKPreMultiply, eDSKPreMultiply),
	// Media player
	op(0x00DA, "MediaPlayerSourceStillIndex", dMediaPlayerStillIndex, eMediaPlayerStillIndex),
	op(0x00E1, "MediaPlayerSourceStill", dMediaPlayerStillOnly, eMediaPlayerStillOnly),
	// Fairlight strip and master fader
	op(0x014A, "FairlightAudioMixerInputSourceMixType", dFairlightStripMixType, eFairlightStripMixType),
	op(0x014B, "FairlightAudioMixerInputSourceFaderGain", dFairlightStripFaderGain, eFairlightStripFaderGain),
	op(0x016B, "FairlightAudioMixerMasterOutFaderGain", dFairlightMasterFaderGain, eFairlightMasterFaderGain),
	// Transition family
	op(0x0087, "TransitionMixRate", dTransitionMixRate, eTransitionMixRate),
	op(0x0088, "TransitionDipRate", dTransitionDipRate, eTransitionDipRate),
	op(0x0089, "TransitionDipInput", dTransitionDipInput, eTransitionDipInput),
	op(0x007C, "TransitionWipeRate", dTransitionWipeRate, eTransitionWipeRate),
	op(0x007D, "TransitionWipePattern", dTransitionWipePattern, eTransitionWipePattern),
	op(0x007E, "TransitionWipeBorderWidth", dTransitionWipeBorderWidth, eTransitionWipeBorderWidth),
	op(0x007F, "TransitionWipeBorderSoftness", dTransitionWipeBorderSoftness, eTransitionWipeBorderSoftness),
	op(0x0080, "TransitionWipeBorderFillInput", dTransitionWipeBorderFillInput, eTransitionWipeBorderFillInput),
	op(0x0081, "TransitionWipeAndDVEReverse", dTransitionWipeDVEReverse, eTransitionWipeDVEReverse),
	op(0x0082, "TransitionWipeAndDVEFlipFlop", dTransitionWipeDVEFlipFlop, eTransitionWipeDVEFlipFlop),
	op(0x0015, "TransitionWipeXPosition", dTransitionWipeXPosition, eTransitionWipeXPosition),
	op(0x0016, "TransitionWipeYPosition", dTransitionWipeYPosition, eTransitionWipeYPosition),
	op(0x003A, "TransitionDVERate", dTransitionDVERate, eTransitionDVERate),
	op(0x0034, "TransitionDVEPattern", dTransitionDVEPattern, eTransitionDVEPattern),
	op(0x00D7, "TransitionDVEFillInput", dTransitionDVEFillInput, eTransitionDVEFillInput),
	op(0x00D8, "TransitionDVECutInput", dTransitionDVECutInput, eTransitionDVECutInput),
	op(0x00D9, "TransitionDVECutInputEnable", dTransitionDVECutInputEnable, eTransitionDVECutInputEnable),
	op(0x008C, "TransitionStingerSourceMediaPlayer", dTransitionStingerSourceMediaPlayer, eTransitionStingerSourceMediaPlayer),
	op(0x008D, "TransitionStingerClipDuration", dTransitionStingerClipDuration, eTransitionStingerClipDuration),
	op(0x008E, "TransitionStingerTriggerPoint", dTransitionStingerTriggerPoint, eTransitionStingerTriggerPoint),
	op(0x008F, "TransitionStingerMixRate", dTransitionStingerMixRate, eTransitionStingerMixRate),
	op(0x0090, "TransitionStingerPreRoll", dTransitionStingerPreRoll, eTransitionStingerPreRoll),
	op(0x0092, "TransitionStingerDVEClip", dTransitionStingerDVEClip, eTransitionStingerDVEClip),
	op(0x0093, "TransitionStingerDVEGain", dTransitionStingerDVEGain, eTransitionStingerDVEGain),
	op(0x0094, "TransitionStingerDVEInvert", dTransitionStingerDVEInvert, eTransitionStingerDVEInvert),
	op(0x0095, "TransitionStingerDVEPreMultiply", dTransitionStingerDVEPreMultiply, eTransitionStingerDVEPreMultiply),
	// Colour generators
	op(0x001C, "ColorGeneratorHue", dColorGenHue, eColorGenHue),
	op(0x001D, "ColorGeneratorSaturation", dColorGenSaturation, eColorGenSaturation),
	op(0x001E, "ColorGeneratorLuminescence", dColorGenLuminescence, eColorGenLuminescence),
	// Fade-to-black rate
	op(0x00A5, "FadeToBlackRate", dFTBRate, eFTBRate),
	// USK mask edges
	op(0x0030, "KeyMaskTop", dUSKMaskTop, eUSKMaskTop),
	op(0x0031, "KeyMaskBottom", dUSKMaskBottom, eUSKMaskBottom),
	op(0x0032, "KeyMaskLeft", dUSKMaskLeft, eUSKMaskLeft),
	op(0x0033, "KeyMaskRight", dUSKMaskRight, eUSKMaskRight),
	// DVE and fly key rate
	op(0x0046, "DVEAndFlyKeyRate", dDVEFlyKeyRate, eDVEFlyKeyRate),
	// USK DVE border
	op(0x004E, "DVEKeyBorderEnable", dDVEBorderEnable, eDVEBorderEnable),
	op(0x005A, "DVEKeyBorderHue", dDVEBorderHue, eDVEBorderHue),
	op(0x005B, "DVEKeyBorderSaturation", dDVEBorderSaturation, eDVEBorderSaturation),
	op(0x005C, "DVEKeyBorderLuminescence", dDVEBorderLuminescence, eDVEBorderLuminescence),
	op(0x005E, "DVEKeyBorderOuterWidth", dDVEBorderOuterWidth, eDVEBorderOuterWidth),
	op(0x005F, "DVEKeyBorderInnerWidth", dDVEBorderInnerWidth, eDVEBorderInnerWidth),
	op(0x0060, "DVEKeyBorderOuterSoftness", dDVEBorderOuterSoftness, eDVEBorderOuterSoftness),
	op(0x0061, "DVEKeyBorderInnerSoftness", dDVEBorderInnerSoftness, eDVEBorderInnerSoftness),
	op(0x0062, "DVEKeyBorderOpacity", dDVEBorderOpacity, eDVEBorderOpacity),
	// USK DVE shadow
	op(0x004D, "DVEKeyShadowEnable", dDVEShadowEnable, eDVEShadowEnable),
	op(0x0058, "DVEKeyShadowDirection", dDVEShadowDirection, eDVEShadowDirection),
	op(0x0059, "DVEKeyShadowAltitude", dDVEShadowAltitude, eDVEShadowAltitude),
	// Advanced chroma key
	ackFP(0x012C, "AdvancedChromaKeyForegroundLevel", "foregroundLevel"),
	ackFP(0x012D, "AdvancedChromaKeyBackgroundLevel", "backgroundLevel"),
	ackFP(0x012E, "AdvancedChromaKeyKeyEdge", "keyEdge"),
	ackFP(0x012F, "AdvancedChromaKeySpillSuppress", "spillSuppress"),
	ackFP(0x0130, "AdvancedChromaKeyFlareSuppress", "flareSuppress"),
	ackFP(0x0131, "AdvancedChromaKeyForegroundBrightness", "foregroundBrightness"),
	ackFP(0x0132, "AdvancedChromaKeyForegroundContrast", "foregroundContrast"),
	ackFP(0x0133, "AdvancedChromaKeyForegroundColour", "foregroundColour"),
	ackFP(0x0134, "AdvancedChromaKeyForegroundRed", "foregroundRed"),
	ackFP(0x0135, "AdvancedChromaKeyForegroundGreen", "foregroundGreen"),
	ackFP(0x0136, "AdvancedChromaKeyForegroundBlue", "foregroundBlue"),
	op(0x0137, "AdvancedChromaKeySamplingModeEnabled", dACKSamplingModeEnabled, eACKSamplingModeEnabled),
	op(0x0138, "AdvancedChromaKeyPreviewEnabled", dACKPreviewEnabled, eACKPreviewEnabled),
	ackCursor(0x0139, "AdvancedChromaKeyCursorXPosition", "xPosition"),
	ackCursor(0x013A, "AdvancedChromaKeyCursorYPosition", "yPosition"),
	ackCursor(0x013B, "AdvancedChromaKeyCursorSize", "size"),
	// Fly key keyframes
	op(0x0050, "FlyKeySetKeyFrame", dFlyKeySetKeyFrame, eFlyKeySetKeyFrame),
	op(0x0052, "FlyKeyRunToFull", dFlyKeyRunToFull, eFlyKeyRunToFull),
	op(0x0054, "FlyKeyRunToKeyFrame", dFlyKeyRunToKeyFrame, eFlyKeyRunToKeyFrame),
	op(0x0056, "FlyKeyRunToInfinity", dFlyKeyRunToInfinity, eFlyKeyRunToInfinity),
	// Fairlight EQ
	op(0x014D, "FairlightAudioMixerInputSourceEqualiserGain", dEQGain, eEQGain),
	op(0x014E, "FairlightAudioMixerInputSourceEqualiserBandEnabled", dEQBandEnabled, eEQBandEnabled),
	op(0x014F, "FairlightAudioMixerInputSourceEqualiserBandShape", dEQBandShape, eEQBandShape),
	op(0x0150, "FairlightAudioMixerInputSourceEqualiserBandRange", dEQBandRange, eEQBandRange),
	op(0x0151, "FairlightAudioMixerInputSourceEqualiserBandFrequency", dEQBandFrequency, eEQBandFrequency),
	op(0x0152, "FairlightAudioMixerInputSourceEqualiserBandGain", dEQBandGain, eEQBandGain),
	op(0x0153, "FairlightAudioMixerInputSourceEqualiserBandQFactor", dEQBandQFactor, eEQBandQFactor),
	// Fairlight dynamics and headphones
	stripFP(0x0147, "FairlightAudioMixerInputSourceInputGain", fairlightFam4346, "gain"),
	stripFP(0x0156, "FairlightAudioMixerInputSourceDynamicsGain", fairlightFam4346, "gain"),
	stripEnable(0x0157, "FairlightAudioMixerInputSourceExpanderEnabled", fairlightFam024A, 0x60, 0x01B4),
	stripEnable(0x0158, "FairlightAudioMixerInputSourceExpanderGateModeEnabled", fairlightFam024A, 0x60, 0x01B4),
	stripFP(0x0159, "FairlightAudioMixerInputSourceExpanderThreshold", fairlightFam024A, "threshold"),
	stripFP(0x015A, "FairlightAudioMixerInputSourceExpanderRange", fairlightFam024A, "range"),
	stripFP(0x015B, "FairlightAudioMixerInputSourceExpanderRatio", fairlightFam024A, "ratio"),
	stripFP(0x015C, "FairlightAudioMixerInputSourceExpanderAttack", fairlightFam024A, "attack"),
	stripFP(0x015D, "FairlightAudioMixerInputSourceExpanderHold", fairlightFam024A, "hold"),
	stripFP(0x015E, "FairlightAudioMixerInputSourceExpanderRelease", fairlightFam024A, "release"),
	stripEnable(0x015F, "FairlightAudioMixerInputSourceCompressorEnabled", fairlightFam4346, 0xAC, enableTrailingMarker),
	stripFP(0x0160, "FairlightAudioMixerInputSourceCompressorThreshold", fairlightFam4346, "threshold"),
	stripFP(0x0161, "FairlightAudioMixerInputSourceCompressorRatio", fairlightFam4346, "ratio"),
	stripFP(0x0162, "FairlightAudioMixerInputSourceCompressorAttack", fairlightFam4346, "attack"),
	stripFP(0x0163, "FairlightAudioMixerInputSourceCompressorHold", fairlightFam4346, "hold"),
	stripFP(0x0164, "FairlightAudioMixerInputSourceCompressorRelease", fairlightFam4346, "release"),
	stripEnable(0x0165, "FairlightAudioMixerInputSourceLimiterEnabled", fairlightFam03E1, 0xAC, enableTrailingMarker),
	stripFP(0x0166, "FairlightAudioMixerInputSourceLimiterThreshold", fairlightFam03E1, "threshold"),
	stripFP(0x0167, "FairlightAudioMixerInputSourceLimiterAttack", fairlightFam03E1, "attack"),
	stripFP(0x0168, "FairlightAudioMixerInputSourceLimiterHold", fairlightFam03E1, "hold"),
	stripFP(0x0169, "FairlightAudioMixerInputSourceLimiterRelease", fairlightFam03E1, "release"),
	op(0x0184, "FairlightAudioMixerHeadphoneOutGain", dHPGain, eHPGain),
	op(0x0185, "FairlightAudioMixerHeadphoneOutMasterGain", dHPGain, eHPGain),
	op(0x0186, "FairlightAudioMixerHeadphoneOutTalkbackGain", dHPGain, eHPGain),
	op(0x0187, "FairlightAudioMixerHeadphoneOutSidetoneGain", dHPGain, eHPGain),
	// HyperDeck binding
	op(0x0110, "HyperDeckNetworkAddress", dHDNetworkAddress, eHDNetworkAddress),
	op(0x011A, "HyperDeckInput", dHDInput, eHDInput),
}

var (
	byCode = map[uint16]*opCodec{}
	byID   = map[string]*opCodec{} // _ENCODE_BY_ID
)

func init() {
	for i := range knownOps {
		c := &knownOps[i]
		byCode[c.code] = c
		byID[c.id] = c
	}
}

// Decode parses macro bytecode (decode_macro_bytecode): a concatenation of
// ops, each a little-endian u16 total length (header included), a u16 op
// code and the parameters.
//
// Like atemwire it never fails outright. An op code it has no codec for
// becomes an Unknown_0xNNNN op holding the parameter bytes; so does a
// known op whose parameters it can't decode (too short), with a
// decodeError attribute holding CPython's message. If the stream stops
// making sense (fewer than four bytes left, a length under 4, an op
// running past the end), Decode returns the ops before that point and an
// error saying why: the warning atemwire logs before returning the same
// ops.
func Decode(raw []byte) ([]Op, error) {
	var out []Op
	o, n := 0, len(raw)
	for o < n {
		if o+4 > n {
			return out, fmt.Errorf("macro bytecode truncated at offset %d (need 4-byte header)", o)
		}
		opLen := int(binary.LittleEndian.Uint16(raw[o:]))
		code := binary.LittleEndian.Uint16(raw[o+2:])
		if opLen < 4 {
			return out, fmt.Errorf("macro bytecode: bogus op_len %d at offset %d", opLen, o)
		}
		if o+opLen > n {
			return out, fmt.Errorf("macro bytecode: op_len %d at offset %d overruns buffer (len %d)", opLen, o, n)
		}
		p := raw[o+4 : o+opLen]
		unknown := fmt.Sprintf("Unknown_0x%04x", code)
		if c, ok := byCode[code]; ok {
			var ps []Param
			if exc := catch(func() { ps = c.dec(params(p)) }); exc != nil {
				out = append(out, Op{unknown, []Param{{"rawParams", hex.EncodeToString(p)}, {"decodeError", exc.msg}}})
			} else {
				out = append(out, Op{c.id, ps})
			}
		} else {
			out = append(out, Op{unknown, []Param{{"rawParams", hex.EncodeToString(p)}}})
		}
		o += opLen
	}
	return out, nil
}

// EncodeOp encodes one op, header included (encode_single_op). An
// Unknown_0xNNNN op is written from its rawParams. It fails with an
// *Error wherever atemwire raises, and succeeds wherever atemwire does,
// including where atemwire masks, clamps or substitutes silently (SURFACE
// §5.4 item 9).
func EncodeOp(op Op) ([]byte, error) {
	a := attrs{}
	for _, p := range op.Params {
		a[p.Name] = p.Value
	}
	var out []byte
	if exc := catch(func() { out = encodeOp(op.ID, a) }); exc != nil {
		return nil, &Error{exc.class, exc.msg}
	}
	return out, nil
}

func encodeOp(id string, a attrs) []byte {
	if hexCode, ok := strings.CutPrefix(id, "Unknown_0x"); ok {
		code, exc := parseInt(hexCode, 16)
		if exc != nil {
			raise("ValueError", "malformed Unknown op id %s", pyRepr(id))
		}
		p, exc := fromHex(a["rawParams"])
		if exc != nil {
			raise("ValueError", "op %s has malformed rawParams: %s", id, exc.msg)
		}
		return frame(code, p)
	}
	c, ok := byID[id]
	if !ok {
		raise("ValueError", "no encoder for op id %s", pyRepr(id))
	}
	var p []byte
	if exc := catch(func() { p = c.enc(a) }); exc != nil {
		if exc.class == "KeyError" {
			raise("ValueError", "op %s: missing attribute %s", id, pyRepr(exc.msg))
		}
		panic(exc)
	}
	return frame(num(int64(c.code)), p)
}

// frame is struct.pack('<HH', 4 + len(params), code) + params.
func frame(code *big.Int, p []byte) []byte {
	return cat(packH(num(int64(4+len(p)))), packH(code), p)
}

// Encode encodes a macro (encode_macro_bytecode): the ops' bytes
// concatenated. The first op that fails aborts it with that op's *Error.
func Encode(ops []Op) ([]byte, error) {
	out := []byte{}
	for _, o := range ops {
		b, err := EncodeOp(o)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}
