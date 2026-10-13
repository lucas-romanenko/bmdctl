// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The atemwire operations profile apply calls (bmdwire v1.3.0
// atemwire/messages/*.py), as the atem commands that send the same bytes.
//
// Where an operation can fail matters, because atemwire's apply records
// what an operation raises (ApplyResult.Errors) but never sees what goes
// wrong encoding the command it queued: ATEMConnection's worker calls
// get_command later, logs the failure and drops the command. So each
// function here first makes the checks the operation makes when called
// (round() or int() of a NaN or an infinity, an unknown enum name) and
// returns the same exception; anything that only fails encoding (an int
// that doesn't fit its wire field, a finite value out of range, a NaN a
// Send class converts) is dropped silently by send.

import (
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// send is conn.send on an ATEMConnection: a command whose encoding fails
// (Validate, atemwire's get_command raising) is logged and dropped, as
// the worker drops it; anything else goes to the target, whose error
// (a closed session) is the operation's.
func (a *applier) send(cmd atem.Command) error {
	if v, ok := cmd.(atem.Validator); ok {
		if err := v.Validate(); err != nil {
			return a.drop(cmd.Opcode(), err.Error())
		}
	}
	return a.t.Send(cmd)
}

// drop logs a command the worker would drop.
func (a *applier) drop(opcode, why string) error {
	a.log.Warn("dropping bad command", "opcode", opcode, "err", why)
	if a.onDrop != nil {
		a.onDrop(opcode)
	}
	return nil
}

func u8(v float64) (uint8, bool) {
	if fitsInt(v, 0, 0xff) {
		return uint8(v), true
	}
	return 0, false
}

func u16(v float64) (uint16, bool) {
	if fitsInt(v, 0, 0xffff) {
		return uint16(v), true
	}
	return 0, false
}

// firstErr returns the first non-nil error: the first conversion an
// operation makes that raises.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// --- switching, media, video, inputs, HyperDeck -----------------------------

func (a *applier) setProgram(me int, source float64) error {
	src, ok := u16(source)
	if !ok {
		return a.drop("CPgI", "source out of range")
	}
	return a.send(atem.ProgramInput{ME: uint8(me), Source: src})
}

func (a *applier) setPreview(me int, source float64) error {
	src, ok := u16(source)
	if !ok {
		return a.drop("CPvI", "source out of range")
	}
	return a.send(atem.PreviewInput{ME: uint8(me), Source: src})
}

func (a *applier) setAuxOutput(aux, source float64) error {
	idx, ok1 := u8(aux)
	src, ok2 := u16(source)
	if !ok1 || !ok2 {
		return a.drop("CAuS", "aux or source out of range")
	}
	return a.send(atem.AuxSource{Aux: idx, Source: src})
}

// setMediaPlayer is set_media_player_still or set_media_player_clip.
func (a *applier) setMediaPlayer(player, slot float64, clip bool) error {
	p, ok1 := u8(player)
	s, ok2 := u8(slot)
	if !ok1 || !ok2 {
		return a.drop("MPSS", "player or slot out of range")
	}
	cmd := atem.MediaplayerSelect{Player: p}
	if clip {
		cmd.Clip = &s
	} else {
		cmd.Still = &s
	}
	return a.send(cmd)
}

func (a *applier) setVideoMode(mode int) error {
	return a.send(atem.VideoMode{Mode: uint8(mode)})
}

// setInputLabel is set_input_label: a nil name leaves that label alone.
func (a *applier) setInputLabel(source float64, long, short *string) error {
	src, ok := u16(source)
	if !ok {
		return a.drop("CInL", "source out of range")
	}
	return a.send(atem.InputProperties{Source: src, LongName: long, ShortName: short})
}

// setHyperdeckSettings is set_hyperdeck_settings with both fields: the
// address goes through inet_aton when the command is encoded, so any form
// it accepts is sent as the dotted quad it means, and anything it refuses
// is dropped.
func (a *applier) setHyperdeckSettings(slot float64, ip string, input float64) error {
	sl, ok1 := u16(slot)
	in, ok2 := u16(input)
	addr, ok3 := inetAton(ip)
	if !ok1 || !ok2 || !ok3 {
		return a.drop("CXMS", "slot, address or input not encodable")
	}
	dotted := strconv.Itoa(int(addr>>24)) + "." + strconv.Itoa(int(addr>>16&0xff)) + "." +
		strconv.Itoa(int(addr>>8&0xff)) + "." + strconv.Itoa(int(addr&0xff))
	return a.send(atem.HyperdeckSettings{Slot: sl, NetworkAddress: &dotted, SwitcherInput: &in})
}

// inetAton is glibc's inet_aton, which Python's socket.inet_aton calls:
// one to four parts, each decimal, octal (leading 0) or hexadecimal (0x),
// the last filling the remaining bytes, and anything after a whitespace
// character ignored.
func inetAton(s string) (uint32, bool) {
	maxLast := []uint64{0xffffffff, 0xffffff, 0xffff, 0xff}
	var parts []uint64
	i := 0
	for {
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		// strtoul(cp, &end, 0)
		base, j := 10, i
		if s[j] == '0' {
			base = 8
			if j+1 < len(s) && (s[j+1] == 'x' || s[j+1] == 'X') && j+2 < len(s) && isHex(s[j+2]) {
				base, j = 16, j+2
			}
		}
		var v uint64
		start := j
		for j < len(s) {
			d := digitVal(s[j])
			if d < 0 || d >= base {
				break
			}
			v = v*uint64(base) + uint64(d)
			if v > 0xffffffff {
				return 0, false
			}
			j++
		}
		if j == start && base != 8 {
			return 0, false
		}
		i = j
		if i < len(s) && s[i] == '.' {
			if len(parts) > 2 || v > 0xff {
				return 0, false
			}
			parts = append(parts, v)
			i++
			continue
		}
		if i < len(s) && !strings.ContainsRune(" \t\n\v\f\r", rune(s[i])) {
			return 0, false
		}
		if v > maxLast[len(parts)] {
			return 0, false
		}
		var out uint32
		for k, p := range parts {
			out |= uint32(p) << (24 - 8*k)
		}
		return out | uint32(v), true
	}
}

func isHex(c byte) bool { return digitVal(c) >= 0 && digitVal(c) < 16 }

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// --- transition ------------------------------------------------------------

func (a *applier) setTransitionStyle(me, style int) error {
	s := uint8(style)
	return a.send(atem.TransitionSettings{ME: uint8(me), Style: &s})
}

// setNextTransitionLayers is set_next_transition_layers: one absolute
// mask, and nothing sent for an empty one.
func (a *applier) setNextTransitionLayers(me int, layers [5]bool) error {
	var mask uint8
	for i, on := range layers {
		if on {
			mask |= 1 << i
		}
	}
	if mask == 0 {
		return nil
	}
	return a.send(atem.TransitionSettings{ME: uint8(me), NextTransition: &mask})
}

// The rate setters: atemwire's apply passes str(_int(rate)), which
// parse_rate reads back as that many frames at any frame rate.

func (a *applier) setMixRate(me int, frames float64) error {
	r, ok := u8(frames)
	if !ok {
		return a.drop("CTMx", "rate out of range")
	}
	return a.send(atem.MixSettings{ME: uint8(me), Rate: r})
}

func (a *applier) setDipRate(me int, frames float64) error {
	r, ok := u8(frames)
	if !ok {
		return a.drop("CTDp", "rate out of range")
	}
	return a.send(atem.DipSettings{ME: uint8(me), Rate: &r})
}

func (a *applier) setDipSource(me int, source float64) error {
	s, ok := u16(source)
	if !ok {
		return a.drop("CTDp", "source out of range")
	}
	return a.send(atem.DipSettings{ME: uint8(me), Source: &s})
}

// wipe sets one field of CTWp.
func (a *applier) wipe(me int, set func(*atem.WipeSettings)) error {
	c := atem.WipeSettings{ME: uint8(me)}
	set(&c)
	return a.send(c)
}

func (a *applier) setWipeRate(me int, frames float64) error {
	r, ok := u8(frames)
	if !ok {
		return a.drop("CTWp", "rate out of range")
	}
	return a.wipe(me, func(c *atem.WipeSettings) { c.Rate = &r })
}

func (a *applier) setWipeSource(me int, source float64) error {
	s, ok := u16(source)
	if !ok {
		return a.drop("CTWp", "source out of range")
	}
	return a.wipe(me, func(c *atem.WipeSettings) { c.Source = &s })
}

// setWipeFloat is the wipe setters of a percent or 0-1 value, which round
// when called.
func (a *applier) setWipeFloat(me int, v float64, set func(*atem.WipeSettings, *float64)) error {
	if err := toInt(v); err != nil {
		return err
	}
	return a.wipe(me, func(c *atem.WipeSettings) { set(c, &v) })
}

// stinger sets one field of CTSt.
func (a *applier) stinger(me int, set func(*atem.StingerSettings)) error {
	c := atem.StingerSettings{ME: uint8(me)}
	set(&c)
	return a.send(c)
}

func (a *applier) setStingerSource(me int, mp float64) error {
	m, ok := u8(mp)
	if !ok {
		return a.drop("CTSt", "media player out of range")
	}
	return a.stinger(me, func(c *atem.StingerSettings) { c.MediaPlayer = &m })
}

// setStingerFrames is the stinger setters that take a rate string.
func (a *applier) setStingerFrames(me int, frames float64, set func(*atem.StingerSettings, *uint16)) error {
	f, ok := u16(frames)
	if !ok {
		return a.drop("CTSt", "frames out of range")
	}
	return a.stinger(me, func(c *atem.StingerSettings) { set(c, &f) })
}

func (a *applier) setStingerFloat(me int, v float64, set func(*atem.StingerSettings, *float64)) error {
	if err := toInt(v); err != nil {
		return err
	}
	return a.stinger(me, func(c *atem.StingerSettings) { set(c, &v) })
}

// dve sets one field of CTDv.
func (a *applier) dve(me int, set func(*atem.DVESettings)) error {
	c := atem.DVESettings{ME: uint8(me)}
	set(&c)
	return a.send(c)
}

func (a *applier) setDVERate(me int, frames float64) error {
	r, ok := u8(frames)
	if !ok {
		return a.drop("CTDv", "rate out of range")
	}
	return a.dve(me, func(c *atem.DVESettings) { c.Rate = &r })
}

func (a *applier) setDVESource(me int, source float64, set func(*atem.DVESettings, *uint16)) error {
	s, ok := u16(source)
	if !ok {
		return a.drop("CTDv", "source out of range")
	}
	return a.dve(me, func(c *atem.DVESettings) { set(c, &s) })
}

func (a *applier) setDVEFloat(me int, v float64, set func(*atem.DVESettings, *float64)) error {
	if err := toInt(v); err != nil {
		return err
	}
	return a.dve(me, func(c *atem.DVESettings) { set(c, &v) })
}

func (a *applier) setFTBRate(me int, frames float64) error {
	f, ok := u8(frames)
	if !ok {
		return a.drop("FtbC", "rate out of range")
	}
	return a.send(atem.FadeToBlackConfig{ME: uint8(me), Frames: f})
}

// --- upstream keyer --------------------------------------------------------

// keyer is int(keyer): the keyer's wire byte, false where it won't fit.
func keyer(k int) (uint8, bool) { return u8(float64(k)) }

func (a *applier) setUSKType(me, k, keyType int) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKTp", "keyer out of range")
	}
	t := uint8(keyType)
	return a.send(atem.KeyType{ME: uint8(me), Keyer: kk, Type: &t})
}

func (a *applier) setUSKFlyEnabled(me, k int, on bool) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKTp", "keyer out of range")
	}
	return a.send(atem.KeyType{ME: uint8(me), Keyer: kk, FlyEnabled: &on})
}

func (a *applier) setUSKSource(me, k int, source float64, fill bool) error {
	kk, ok1 := keyer(k)
	s, ok2 := u16(source)
	if !ok1 || !ok2 {
		if fill {
			return a.drop("CKeF", "keyer or source out of range")
		}
		return a.drop("CKeC", "keyer or source out of range")
	}
	if fill {
		return a.send(atem.KeyFill{ME: uint8(me), Keyer: kk, Source: s})
	}
	return a.send(atem.KeyCut{ME: uint8(me), Keyer: kk, Source: s})
}

func (a *applier) setUSKOnAir(me, k int, on bool) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKOn", "keyer out of range")
	}
	return a.send(atem.KeyOnAir{ME: uint8(me), Keyer: kk, Enabled: on})
}

// mask sets one field of CKMs.
func (a *applier) mask(me, k int, set func(*atem.KeyPropertiesMask)) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKMs", "keyer out of range")
	}
	c := atem.KeyPropertiesMask{ME: uint8(me), Keyer: kk}
	set(&c)
	return a.send(c)
}

// configureUSKLuma is configure_usk_luma with every field: clip then gain
// round when called.
func (a *applier) configureUSKLuma(me, k int, clip, gain float64, pre, invert bool) error {
	if err := firstErr(toInt(clip), toInt(gain)); err != nil {
		return err
	}
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKLm", "keyer out of range")
	}
	return a.send(atem.KeyPropertiesLuma{ME: uint8(me), Keyer: kk, Premultiplied: &pre, Clip: &clip, Gain: &gain, InvertKey: &invert})
}

// pattern sets one field of CKPt.
func (a *applier) pattern(me, k int, set func(*atem.KeyPropertiesPattern)) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKPt", "keyer out of range")
	}
	c := atem.KeyPropertiesPattern{ME: uint8(me), Keyer: kk}
	set(&c)
	return a.send(c)
}

// keyDVE sets one field of CKDV.
func (a *applier) keyDVE(me, k int, set func(*atem.KeyPropertiesDVE)) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CKDV", "keyer out of range")
	}
	c := atem.KeyPropertiesDVE{ME: uint8(me), Keyer: kk}
	set(&c)
	return a.send(c)
}

func (a *applier) setUSKDVERate(me, k int, frames float64) error {
	r, ok := u8(frames)
	if !ok {
		return a.drop("CKDV", "rate out of range")
	}
	return a.keyDVE(me, k, func(c *atem.KeyPropertiesDVE) { c.Rate = &r })
}

// chroma sets one field of CACK.
func (a *applier) chroma(me, k int, v float64, set func(*atem.KeyPropertiesAdvancedChroma, *float64)) error {
	if err := toInt(v); err != nil {
		return err
	}
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CACK", "keyer out of range")
	}
	c := atem.KeyPropertiesAdvancedChroma{ME: uint8(me), Keyer: kk}
	set(&c, &v)
	return a.send(c)
}

// picker sends one CACC after the conversions the operation makes.
func (a *applier) picker(me, k int, vals []float64, set func(*atem.KeyPropertiesAdvancedChromaColorpicker)) error {
	for _, v := range vals {
		if err := toInt(v); err != nil {
			return err
		}
	}
	kk, ok := keyer(k)
	if !ok {
		return a.drop("CACC", "keyer out of range")
	}
	c := atem.KeyPropertiesAdvancedChromaColorpicker{ME: uint8(me), Keyer: kk}
	set(&c)
	return a.send(c)
}

func (a *applier) setKeyerFlyKeyframe(me, k int, keyframe uint8) error {
	kk, ok := keyer(k)
	if !ok {
		return a.drop("SFKF", "keyer out of range")
	}
	return a.send(atem.KeyerKeyframeSet{ME: uint8(me), Keyer: kk, Keyframe: keyframe})
}

// --- downstream keyer --------------------------------------------------------

func (a *applier) dskIndex(dsk float64, opcode string) (uint8, bool) {
	d, ok := u8(dsk)
	if !ok {
		a.drop(opcode, "dsk out of range")
	}
	return d, ok
}

func (a *applier) setDSKSource(dsk, source float64, fill bool) error {
	s, ok := u16(source)
	d, ok2 := u8(dsk)
	if !ok || !ok2 {
		if fill {
			return a.drop("CDsF", "dsk or source out of range")
		}
		return a.drop("CDsC", "dsk or source out of range")
	}
	if fill {
		return a.send(atem.DkeySetFill{DSK: d, Source: s})
	}
	return a.send(atem.DkeySetKey{DSK: d, Source: s})
}

func (a *applier) setDSKRate(dsk, frames float64) error {
	r, ok := u8(frames)
	d, ok2 := u8(dsk)
	if !ok || !ok2 {
		return a.drop("CDsR", "dsk or rate out of range")
	}
	return a.send(atem.DkeyRate{DSK: d, Rate: r})
}

func (a *applier) dskMask(dsk float64, set func(*atem.DkeyMask)) error {
	d, ok := a.dskIndex(dsk, "CDsM")
	if !ok {
		return nil
	}
	c := atem.DkeyMask{DSK: d}
	set(&c)
	return a.send(c)
}

func (a *applier) configureDSKGain(dsk, clip, gain float64, pre, invert bool) error {
	if err := firstErr(toInt(clip), toInt(gain)); err != nil {
		return err
	}
	d, ok := a.dskIndex(dsk, "CDsG")
	if !ok {
		return nil
	}
	return a.send(atem.DkeyGain{DSK: d, Premultiplied: &pre, Clip: &clip, Gain: &gain, Invert: &invert})
}

func (a *applier) setDSKOnAir(dsk float64, on bool) error {
	d, ok := a.dskIndex(dsk, "CDsL")
	if !ok {
		return nil
	}
	return a.send(atem.DkeyOnair{DSK: d, OnAir: on})
}

func (a *applier) setDSKTie(dsk float64, tie bool) error {
	d, ok := a.dskIndex(dsk, "CDsT")
	if !ok {
		return nil
	}
	return a.send(atem.DkeyTie{DSK: d, Tie: tie})
}

// --- colour generators -------------------------------------------------------

// setColorGenerator sends one field of CClV. None of the three setters
// can raise when called: hue converts when encoded, saturation and luma
// are clamped (NaN included) by _percent_to_unit.
func (a *applier) setColorGenerator(gen float64, set func(*atem.ColorGenerator)) error {
	g, ok := u8(gen)
	if !ok {
		return a.drop("CClV", "generator out of range")
	}
	c := atem.ColorGenerator{Generator: g}
	set(&c)
	return a.send(c)
}

// --- Fairlight ---------------------------------------------------------------

// dbErr is the check _db_to_wire makes when called: NaN and -inf clamp to
// the bottom of the range, +inf raises.
func dbErr(v *float64) error {
	if v != nil && *v > 0 && toInt(*v) != nil {
		return errInf
	}
	return nil
}

func (a *applier) setFairlightMaster(c atem.FairlightMasterProperties) error {
	if err := firstErr(dbErr(c.EQGain), dbErr(c.DynamicsGain), dbErr(c.Volume)); err != nil {
		return err
	}
	return a.send(c)
}

// stripFields is what _apply_fairlight passes set_fairlight_strip.
type stripFields struct {
	gain, pan, volume, eqGain, dynGain *float64
	eqEnable                           *bool
	mixOption                          *string
	delay                              *float64
	delayErr                           error // _int(delayFrames) raised
}

// setFairlightStrip is set_fairlight_strip: the arguments (delayFrames'
// _int among them) are evaluated first, then the mix option is looked up,
// then each dB and the pan converted in the command's field order.
func (a *applier) setFairlightStrip(source float64, channel int, f stripFields) error {
	if f.delayErr != nil {
		return f.delayErr
	}
	var state *uint8
	if f.mixOption != nil {
		v, ok := fairlightMixOptions[*f.mixOption]
		if !ok {
			return &Error{"ValueError", "unknown mix_option " + pyRepr(*f.mixOption)}
		}
		state = &v
	}
	var panErr error
	if f.pan != nil {
		panErr = toInt(*f.pan)
	}
	if err := firstErr(dbErr(f.gain), dbErr(f.eqGain), dbErr(f.dynGain), panErr, dbErr(f.volume)); err != nil {
		return err
	}
	src, ok := u16(source)
	var delay *uint8
	if f.delay != nil {
		d, ok2 := u8(*f.delay)
		ok = ok && ok2
		delay = &d
	}
	if !ok {
		return a.drop("CFSP", "source or delay out of range")
	}
	return a.send(atem.FairlightStripProperties{
		Source: src, Channel: channel, Delay: delay, Gain: f.gain, EQEnable: f.eqEnable,
		EQGain: f.eqGain, DynamicsGain: f.dynGain, Balance: f.pan, Volume: f.volume, State: state,
	})
}

// bandFields is what _apply_eq_band and _apply_master_eq_band pass.
type bandFields struct {
	enabled          bool
	shape, freqRange *string
	frequency        *float64
	freqErr          error // _int(frequency) raised
	gain, q          float64
}

// bandConvert is the part of set_fairlight_eq_band and
// set_fairlight_master_eq_band that runs when called: the frequency's
// _int, the shape and range names, the frequency clamp to 30-21700, the
// gain and the Q.
func bandConvert(f bandFields) (shape, freqRange *uint8, freq *int, err error) {
	if f.freqErr != nil {
		return nil, nil, nil, f.freqErr
	}
	if f.shape != nil {
		v, ok := fairlightEQShapes[*f.shape]
		if !ok {
			return nil, nil, nil, &Error{"ValueError", "unknown EQ shape " + pyRepr(*f.shape)}
		}
		shape = &v
	}
	if f.freqRange != nil {
		v, ok := fairlightEQFreqRanges[*f.freqRange]
		if !ok {
			return nil, nil, nil, &Error{"ValueError", "unknown EQ frequency_range " + pyRepr(*f.freqRange)}
		}
		freqRange = &v
	}
	if f.frequency != nil {
		hz := int(max(30, min(21700, *f.frequency)))
		freq = &hz
	}
	if err := firstErr(dbErr(&f.gain), toInt(f.q)); err != nil {
		return nil, nil, nil, err
	}
	return shape, freqRange, freq, nil
}

func (a *applier) setFairlightEQBand(source float64, channel int, band float64, f bandFields) error {
	shape, freqRange, freq, err := bandConvert(f)
	if err != nil {
		return err
	}
	src, ok1 := u16(source)
	b, ok2 := u8(band)
	if !ok1 || !ok2 {
		return a.drop("CEBP", "source or band out of range")
	}
	return a.send(atem.FairlightEQBandProperties{
		Source: src, Channel: channel, Band: b, Enabled: &f.enabled, Shape: shape,
		FrequencyRange: freqRange, Frequency: freq, Gain: &f.gain, Q: &f.q,
	})
}

func (a *applier) setFairlightMasterEQBand(band float64, f bandFields) error {
	shape, freqRange, freq, err := bandConvert(f)
	if err != nil {
		return err
	}
	b, ok := u8(band)
	if !ok {
		return a.drop("CMBP", "band out of range")
	}
	return a.send(atem.FairlightMasterEQBandProperties{
		Band: b, Enabled: &f.enabled, Shape: shape, FrequencyRange: freqRange,
		Frequency: freq, Gain: &f.gain, Q: &f.q,
	})
}

// The dynamics setters pass their floats to the Send classes, which only
// convert them when encoded: nothing raises when called.

func (a *applier) fairlightSource(source float64, opcode string) (uint16, bool) {
	s, ok := u16(source)
	if !ok {
		a.drop(opcode, "source out of range")
	}
	return s, ok
}
