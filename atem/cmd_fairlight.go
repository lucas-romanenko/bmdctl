// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "math"

// SURFACE.md §1.10 fairlight, from bmdwire v1.3.0
// atemwire/messages/fairlight.py.
//
// A strip is addressed by Source (the input's InPr index: cameras 1...N,
// 1301 XLR mic, 1401 TRS jack) and Channel: -1 for a stereo strip, 0 or 1
// for the halves of a split-mono one. Every field takes the unit of the
// atemwire operation that sets it (dB, ms, Hz, a ratio); the conversions to
// the wire's hundredths, and their clamps, are atemwire's.

// Mix options (FairlightStripProperties.State).
const (
	FairlightMixOff uint8 = 0x01
	FairlightMixOn  uint8 = 0x02
	FairlightMixAFV uint8 = 0x04
)

// EQ band shapes (Shape) and frequency ranges (FrequencyRange), bitfields.
const (
	EQLowShelf  uint8 = 0x01
	EQLowPass   uint8 = 0x02
	EQBandPass  uint8 = 0x04
	EQNotch     uint8 = 0x08
	EQHighPass  uint8 = 0x10
	EQHighShelf uint8 = 0x20

	EQRangeLow     uint8 = 1
	EQRangeMidLow  uint8 = 2
	EQRangeMidHigh uint8 = 4
	EQRangeHigh    uint8 = 8
)

// fairlightPad is the six 0xFF bytes atemwire writes before the split flag.
var fairlightPad = []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// strip writes atemwire's _split_channel: -1 is a stereo strip (split
// 0x01, channel 0), anything else split mono (0xFF, the channel as a
// signed byte).
func (w *wire) strip(source uint16, channel int) {
	w.u16(2, "source", int64(source))
	copy(w.b[8:14], fairlightPad)
	if channel == -1 {
		w.b[14], w.b[15] = 0x01, 0
		return
	}
	w.b[14] = 0xff
	w.i8(15, "channel", int64(channel))
}

// stripKey is the strip id the switcher's echo will carry.
func stripKey(code string, source uint16, channel int, more ...any) string {
	id := stripID(source, 0x01, 0)
	if channel != -1 {
		id = stripID(source, 0xFF, uint8(int8(channel)))
	}
	return key(code, append([]any{id}, more...)...)
}

// dB is atemwire's _db_to_wire: dB to hundredths, clamped; -inf and NaN
// give the floor, +inf raises.
func (w *wire) dB(name string, v float64, lo, hi int64) int64 {
	if math.IsInf(v, -1) || math.IsNaN(v) {
		return lo
	}
	return w.clampRound(name, v*100, lo, hi)
}

// FairlightMasterProperties is CFMP (FairlightMasterPropertiesCommand, op
// set_fairlight_master). EQGain ±20 dB, DynamicsGain (makeup) 0-20 dB,
// Volume -100 to +10 dB (-inf is -100).
type FairlightMasterProperties struct {
	EQEnable     *bool
	EQGain       *float64
	DynamicsGain *float64
	Volume       *float64
	AFV          *bool
}

func (c FairlightMasterProperties) Opcode() string    { return "CFMP" }
func (c FairlightMasterProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightMasterProperties) Validate() error   { return c.build().err }
func (c FairlightMasterProperties) EchoKey() string   { return "FAMP" }
func (c FairlightMasterProperties) Unverified() bool  { return false }
func (c FairlightMasterProperties) Destructive() bool { return false }

func (c FairlightMasterProperties) build() *wire {
	w := newWire("CFMP", 20)
	var mask int64
	if c.EQEnable != nil {
		mask |= 1 << 0
		w.flag(1, *c.EQEnable)
	}
	if c.EQGain != nil {
		mask |= 1 << 1
		w.i32(4, "eq_gain", w.dB("eq_gain", *c.EQGain, -2000, 2000))
	}
	if c.DynamicsGain != nil {
		mask |= 1 << 2
		w.u16(10, "dynamics_gain", w.dB("dynamics_gain", *c.DynamicsGain, 0, 2000))
	}
	if c.Volume != nil {
		mask |= 1 << 3
		w.i32(12, "volume", w.dB("volume", *c.Volume, -10000, 1000))
	}
	if c.AFV != nil {
		mask |= 1 << 4
		w.flag(16, *c.AFV)
	}
	w.u8(0, "mask", mask)
	return w
}

// FairlightStripProperties is CFSP (FairlightStripPropertiesCommand, op
// set_fairlight_strip). Gain (input) -100 to +6 dB; EQGain ±20 dB;
// DynamicsGain (makeup) 0-20 dB; Balance (pan) ±100; Volume (fader) -100
// to +10 dB; State is a FairlightMix* option; Delay is frames. Mask bit 2
// is unused.
type FairlightStripProperties struct {
	Source       uint16
	Channel      int
	Delay        *uint8
	Gain         *float64
	EQEnable     *bool
	EQGain       *float64
	DynamicsGain *float64
	Balance      *float64
	Volume       *float64
	State        *uint8
}

func (c FairlightStripProperties) Opcode() string    { return "CFSP" }
func (c FairlightStripProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightStripProperties) Validate() error   { return c.build().err }
func (c FairlightStripProperties) EchoKey() string   { return stripKey("FASP", c.Source, c.Channel) }
func (c FairlightStripProperties) Unverified() bool  { return false }
func (c FairlightStripProperties) Destructive() bool { return false }

func (c FairlightStripProperties) build() *wire {
	w := newWire("CFSP", 48)
	var mask int64
	w.strip(c.Source, c.Channel)
	if c.Delay != nil {
		mask |= 1 << 0
		w.u8(16, "delay", int64(*c.Delay))
	}
	if c.Gain != nil {
		mask |= 1 << 1
		w.i32(20, "gain", w.dB("gain", *c.Gain, -10000, 600))
	}
	if c.EQEnable != nil {
		mask |= 1 << 3
		w.flag(26, *c.EQEnable)
	}
	if c.EQGain != nil {
		mask |= 1 << 4
		w.i32(28, "eq_gain", w.dB("eq_gain", *c.EQGain, -2000, 2000))
	}
	if c.DynamicsGain != nil {
		mask |= 1 << 5
		w.i32(32, "dynamics_gain", w.dB("dynamics_gain", *c.DynamicsGain, 0, 2000))
	}
	if c.Balance != nil {
		mask |= 1 << 6
		w.i16(36, "balance", w.clampRound("balance", *c.Balance*100, -10000, 10000))
	}
	if c.Volume != nil {
		mask |= 1 << 7
		w.i32(40, "volume", w.dB("volume", *c.Volume, -10000, 1000))
	}
	if c.State != nil {
		mask |= 1 << 8
		w.u8(44, "state", int64(*c.State))
	}
	w.u16(0, "mask", mask)
	return w
}

// FairlightEQBandProperties is CEBP (FairlightEqBandPropertiesCommand, op
// set_fairlight_eq_band): one EQ band (0-5) of one strip.
type FairlightEQBandProperties struct {
	Source         uint16
	Channel        int
	Band           uint8
	Enabled        *bool
	Shape          *uint8 // EQ* shape bit
	FrequencyRange *uint8 // EQRange* bit
	Frequency      *int   // Hz, clamped 30-21700
	Gain           *float64
	Q              *float64
}

func (c FairlightEQBandProperties) Opcode() string  { return "CEBP" }
func (c FairlightEQBandProperties) Encode() []byte  { return c.build().encode() }
func (c FairlightEQBandProperties) Validate() error { return c.build().err }
func (c FairlightEQBandProperties) EchoKey() string {
	return stripKey("AEBP", c.Source, c.Channel, c.Band)
}
func (c FairlightEQBandProperties) Unverified() bool  { return false }
func (c FairlightEQBandProperties) Destructive() bool { return false }

func (c FairlightEQBandProperties) build() *wire {
	w := newWire("CEBP", 32)
	var mask int64
	w.strip(c.Source, c.Channel)
	w.u8(16, "band", int64(c.Band))
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(17, *c.Enabled)
	}
	if c.Shape != nil {
		mask |= 1 << 1
		w.u8(18, "band_filter", int64(*c.Shape))
	}
	if c.FrequencyRange != nil {
		mask |= 1 << 2
		w.u8(19, "band_range", int64(*c.FrequencyRange))
	}
	if c.Frequency != nil {
		mask |= 1 << 3
		w.u32(20, "frequency", int64(max(30, min(21700, *c.Frequency))))
	}
	if c.Gain != nil {
		mask |= 1 << 4
		w.i32(24, "gain", w.dB("gain", *c.Gain, -2000, 2000))
	}
	if c.Q != nil {
		mask |= 1 << 5
		w.i16(28, "q", w.clampRound("q", *c.Q*100, 30, 1030))
	}
	w.u8(0, "mask", mask)
	return w
}

// FairlightMasterEQBandProperties is CMBP
// (FairlightMasterEqBandPropertiesCommand, op
// set_fairlight_master_eq_band): one band of the master EQ, with the
// per-strip clamps.
type FairlightMasterEQBandProperties struct {
	Band           uint8
	Enabled        *bool
	Shape          *uint8
	FrequencyRange *uint8
	Frequency      *int
	Gain           *float64
	Q              *float64
}

func (c FairlightMasterEQBandProperties) Opcode() string    { return "CMBP" }
func (c FairlightMasterEQBandProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightMasterEQBandProperties) Validate() error   { return c.build().err }
func (c FairlightMasterEQBandProperties) EchoKey() string   { return key("AMBP", c.Band) }
func (c FairlightMasterEQBandProperties) Unverified() bool  { return false }
func (c FairlightMasterEQBandProperties) Destructive() bool { return false }

func (c FairlightMasterEQBandProperties) build() *wire {
	w := newWire("CMBP", 20)
	var mask int64
	w.u8(1, "band", int64(c.Band))
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(2, *c.Enabled)
	}
	if c.Shape != nil {
		mask |= 1 << 1
		w.u8(3, "band_filter", int64(*c.Shape))
	}
	if c.FrequencyRange != nil {
		mask |= 1 << 2
		w.u8(4, "band_range", int64(*c.FrequencyRange))
	}
	if c.Frequency != nil {
		mask |= 1 << 3
		w.u16(10, "frequency", int64(max(30, min(21700, *c.Frequency))))
	}
	if c.Gain != nil {
		mask |= 1 << 4
		w.i32(12, "gain", w.dB("gain", *c.Gain, -2000, 2000))
	}
	if c.Q != nil {
		mask |= 1 << 5
		w.u16(16, "q", w.clampRound("q", *c.Q*100, 30, 1030))
	}
	w.u8(0, "mask", mask)
	return w
}

// hundredths is the dynamics commands' conversion: ×100, rounded, no
// clamp. atemwire's operations document the live-verified ranges but don't
// enforce them.
func (w *wire) hundredths(name string, v float64) int64 { return w.round(name, v*100) }

// enabledByte is the dynamics commands' "1 if enabled else 0".
func enabledByte(v *bool) int64 {
	if v != nil && *v {
		return 1
	}
	return 0
}

// FairlightCompressorProperties is CICP
// (FairlightCompressorPropertiesCommand, op set_fairlight_compressor).
// Threshold dB (-60-0), Ratio (1.2-20), Attack ms (0.7-100), Hold ms
// (0-4000), Release ms (50-4000); ranges verified live by atemwire, not
// clamped. Mask bits 8-13.
type FairlightCompressorProperties struct {
	Source    uint16
	Channel   int
	Enabled   *bool
	Threshold *float64
	Ratio     *float64
	Attack    *float64
	Hold      *float64
	Release   *float64
}

func (c FairlightCompressorProperties) Opcode() string    { return "CICP" }
func (c FairlightCompressorProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightCompressorProperties) Validate() error   { return c.build().err }
func (c FairlightCompressorProperties) EchoKey() string   { return stripKey("AICP", c.Source, c.Channel) }
func (c FairlightCompressorProperties) Unverified() bool  { return false }
func (c FairlightCompressorProperties) Destructive() bool { return false }

func (c FairlightCompressorProperties) build() *wire {
	w := newWire("CICP", 40)
	var mask int64
	w.strip(c.Source, c.Channel)
	if c.Enabled != nil {
		mask |= 1 << 8
	}
	w.u8(16, "enabled", enabledByte(c.Enabled))
	if c.Threshold != nil {
		mask |= 1 << 9
		w.i32(20, "threshold", w.hundredths("threshold", *c.Threshold))
	}
	if c.Ratio != nil {
		mask |= 1 << 10
		w.u16(24, "ratio", w.hundredths("ratio", *c.Ratio))
	}
	if c.Attack != nil {
		mask |= 1 << 11
		w.i32(28, "attack", w.hundredths("attack", *c.Attack))
	}
	if c.Hold != nil {
		mask |= 1 << 12
		w.i32(32, "hold", w.hundredths("hold", *c.Hold))
	}
	if c.Release != nil {
		mask |= 1 << 13
		w.i32(36, "release", w.hundredths("release", *c.Release))
	}
	w.u16(0, "mask", mask)
	return w
}

// FairlightLimiterProperties is CILP (FairlightLimiterPropertiesCommand,
// op set_fairlight_limiter): the compressor's fields without a ratio.
// Attack ms 0.7-30. Mask bits 8-12.
type FairlightLimiterProperties struct {
	Source    uint16
	Channel   int
	Enabled   *bool
	Threshold *float64
	Attack    *float64
	Hold      *float64
	Release   *float64
}

func (c FairlightLimiterProperties) Opcode() string    { return "CILP" }
func (c FairlightLimiterProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightLimiterProperties) Validate() error   { return c.build().err }
func (c FairlightLimiterProperties) EchoKey() string   { return stripKey("AILP", c.Source, c.Channel) }
func (c FairlightLimiterProperties) Unverified() bool  { return false }
func (c FairlightLimiterProperties) Destructive() bool { return false }

func (c FairlightLimiterProperties) build() *wire {
	w := newWire("CILP", 36)
	var mask int64
	w.strip(c.Source, c.Channel)
	if c.Enabled != nil {
		mask |= 1 << 8
	}
	w.u8(16, "enabled", enabledByte(c.Enabled))
	for i, f := range []struct {
		v    *float64
		name string
	}{{c.Threshold, "threshold"}, {c.Attack, "attack"}, {c.Hold, "hold"}, {c.Release, "release"}} {
		if f.v != nil {
			mask |= 1 << (9 + i)
			w.i32(20+4*i, f.name, w.hundredths(f.name, *f.v))
		}
	}
	w.u16(0, "mask", mask)
	return w
}

// FairlightExpanderProperties is CIXP (FairlightExpanderPropertiesCommand,
// op set_fairlight_expander). Mode is 0 expander, 1 gate (any non-zero
// value is a gate, as int(mode) is in atemwire). Threshold dB (-50-0), Range
// dB (0-60), Ratio (1-3), Attack ms (0.5-100), Hold and Release as for the
// compressor. Mask bits 8-15.
type FairlightExpanderProperties struct {
	Source    uint16
	Channel   int
	Enabled   *bool
	Mode      *uint8
	Threshold *float64
	Range     *float64
	Ratio     *float64
	Attack    *float64
	Hold      *float64
	Release   *float64
}

func (c FairlightExpanderProperties) Opcode() string    { return "CIXP" }
func (c FairlightExpanderProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightExpanderProperties) Validate() error   { return c.build().err }
func (c FairlightExpanderProperties) EchoKey() string   { return stripKey("AIXP", c.Source, c.Channel) }
func (c FairlightExpanderProperties) Unverified() bool  { return false }
func (c FairlightExpanderProperties) Destructive() bool { return false }

func (c FairlightExpanderProperties) build() *wire {
	w := newWire("CIXP", 40)
	var mask int64
	w.strip(c.Source, c.Channel)
	if c.Enabled != nil {
		mask |= 1 << 8
	}
	w.u8(16, "enabled", enabledByte(c.Enabled))
	if c.Mode != nil {
		mask |= 1 << 9
		if *c.Mode != 0 {
			w.b[17] = 1
		}
	}
	if c.Threshold != nil {
		mask |= 1 << 10
		w.i32(20, "threshold", w.hundredths("threshold", *c.Threshold))
	}
	if c.Range != nil {
		mask |= 1 << 11
		w.u16(24, "range", w.hundredths("range", *c.Range))
	}
	if c.Ratio != nil {
		mask |= 1 << 12
		w.u16(26, "ratio", w.hundredths("ratio", *c.Ratio))
	}
	for i, f := range []struct {
		v    *float64
		name string
	}{{c.Attack, "attack"}, {c.Hold, "hold"}, {c.Release, "release"}} {
		if f.v != nil {
			mask |= 1 << (13 + i)
			w.i32(28+4*i, f.name, w.hundredths(f.name, *f.v))
		}
	}
	w.u16(0, "mask", mask)
	return w
}

// FairlightMasterCompressorProperties is CMCP
// (FairlightMasterCompressorPropertiesCommand, op
// set_fairlight_master_compressor). The threshold is a sign-extended i32
// at byte 4: an s16 at 6 made the switcher clamp negatives to 0.
type FairlightMasterCompressorProperties struct {
	Enabled   *bool
	Threshold *float64
	Ratio     *float64
	Attack    *float64
	Hold      *float64
	Release   *float64
}

func (c FairlightMasterCompressorProperties) Opcode() string    { return "CMCP" }
func (c FairlightMasterCompressorProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightMasterCompressorProperties) Validate() error   { return c.build().err }
func (c FairlightMasterCompressorProperties) EchoKey() string   { return "MOCP" }
func (c FairlightMasterCompressorProperties) Unverified() bool  { return false }
func (c FairlightMasterCompressorProperties) Destructive() bool { return false }

func (c FairlightMasterCompressorProperties) build() *wire {
	w := newWire("CMCP", 24)
	var mask int64
	if c.Enabled != nil {
		mask |= 0x01
	}
	w.u8(1, "enabled", enabledByte(c.Enabled))
	if c.Threshold != nil {
		mask |= 0x02
		w.i32(4, "threshold", w.hundredths("threshold", *c.Threshold))
	}
	if c.Ratio != nil {
		mask |= 0x04
		w.u16(8, "ratio", w.hundredths("ratio", *c.Ratio))
	}
	for i, f := range []struct {
		v    *float64
		name string
	}{{c.Attack, "attack"}, {c.Hold, "hold"}, {c.Release, "release"}} {
		if f.v != nil {
			mask |= 0x08 << i
			w.i32(12+4*i, f.name, w.hundredths(f.name, *f.v))
		}
	}
	w.u8(0, "mask", mask)
	return w
}

// FairlightMasterLimiterProperties is CMLP
// (FairlightMasterLimiterPropertiesCommand, op
// set_fairlight_master_limiter).
type FairlightMasterLimiterProperties struct {
	Enabled   *bool
	Threshold *float64
	Attack    *float64
	Hold      *float64
	Release   *float64
}

func (c FairlightMasterLimiterProperties) Opcode() string    { return "CMLP" }
func (c FairlightMasterLimiterProperties) Encode() []byte    { return c.build().encode() }
func (c FairlightMasterLimiterProperties) Validate() error   { return c.build().err }
func (c FairlightMasterLimiterProperties) EchoKey() string   { return "AMLP" }
func (c FairlightMasterLimiterProperties) Unverified() bool  { return false }
func (c FairlightMasterLimiterProperties) Destructive() bool { return false }

func (c FairlightMasterLimiterProperties) build() *wire {
	w := newWire("CMLP", 20)
	var mask int64
	if c.Enabled != nil {
		mask |= 0x01
	}
	w.u8(1, "enabled", enabledByte(c.Enabled))
	for i, f := range []struct {
		v    *float64
		name string
	}{{c.Threshold, "threshold"}, {c.Attack, "attack"}, {c.Hold, "hold"}, {c.Release, "release"}} {
		if f.v != nil {
			mask |= 0x02 << i
			w.i32(4+4*i, f.name, w.hundredths(f.name, *f.v))
		}
	}
	w.u8(0, "mask", mask)
	return w
}

// SendFairlightLevels is SFLN (SendFairlightLevelsCommand, op
// enable_fairlight_levels): start or stop the FMLv and FDLv meter streams.
// It must be sent again after every reconnect. No echo.
type SendFairlightLevels struct {
	Enable bool
}

func (c SendFairlightLevels) Opcode() string    { return "SFLN" }
func (c SendFairlightLevels) Encode() []byte    { return c.build().encode() }
func (c SendFairlightLevels) Validate() error   { return c.build().err }
func (c SendFairlightLevels) EchoKey() string   { return "" }
func (c SendFairlightLevels) Unverified() bool  { return false }
func (c SendFairlightLevels) Destructive() bool { return false }

func (c SendFairlightLevels) build() *wire {
	w := newWire("SFLN", 4)
	w.flag(0, c.Enable)
	return w
}
