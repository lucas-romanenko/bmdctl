// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "fmt"

// SURFACE.md §1.11 audio-legacy, the pre-Fairlight mixer, from bmdwire
// v1.3.0 atemwire/messages/audio_legacy.py. All four commands were restored
// from upstream pyatem and are unverified. Their operations take wire units.

// AudioInput is CAMI (AudioInputCommand, op set_audio_input). On and AFV
// become the mix option: On sets 0 (off) or 1 (on) and wins over AFV,
// which sets 0 or 2 (audio follows video). Volume 0-65381, Balance ±10000.
type AudioInput struct {
	Source  uint16
	Balance *int16
	Volume  *uint16
	On      *bool
	AFV     *bool
}

func (c AudioInput) Opcode() string    { return "CAMI" }
func (c AudioInput) Encode() []byte    { return c.build().encode() }
func (c AudioInput) Validate() error   { return c.build().err }
func (c AudioInput) EchoKey() string   { return key("AMIP", fmt.Sprintf("%d.0", c.Source)) }
func (c AudioInput) Unverified() bool  { return true }
func (c AudioInput) Destructive() bool { return false }

func (c AudioInput) build() *wire {
	w := newWire("CAMI", 12)
	var mask int64
	w.u16(2, "source", int64(c.Source))
	switch {
	case c.On != nil:
		mask |= 1 << 0
		w.flag(4, *c.On)
	case c.AFV != nil:
		mask |= 1 << 0
		if *c.AFV {
			w.b[4] = 2
		}
	}
	if c.Volume != nil {
		mask |= 1 << 1
		w.u16(6, "volume", int64(*c.Volume))
	}
	if c.Balance != nil {
		mask |= 1 << 2
		w.i16(8, "balance", int64(*c.Balance))
	}
	w.u8(0, "mask", mask)
	return w
}

// AudioMasterProperties is CAMM (AudioMasterPropertiesCommand, op
// set_audio_master). Volume 0-65381.
type AudioMasterProperties struct {
	Volume *uint16
	AFV    *bool
}

func (c AudioMasterProperties) Opcode() string    { return "CAMM" }
func (c AudioMasterProperties) Encode() []byte    { return c.build().encode() }
func (c AudioMasterProperties) Validate() error   { return c.build().err }
func (c AudioMasterProperties) EchoKey() string   { return "AMMO" }
func (c AudioMasterProperties) Unverified() bool  { return true }
func (c AudioMasterProperties) Destructive() bool { return false }

func (c AudioMasterProperties) build() *wire {
	w := newWire("CAMM", 8)
	var mask int64
	if c.Volume != nil {
		mask |= 1 << 0
		w.u16(2, "volume", int64(*c.Volume))
	}
	if c.AFV != nil {
		mask |= 1 << 2
		w.flag(6, *c.AFV)
	}
	w.u8(0, "mask", mask)
	return w
}

// AudioHeadphoneProperties is CAMH (AudioHeadphonePropertiesCommand, op
// set_audio_headphones, bmdwire#93): the headphone output of a legacy unit
// that sends AMHP, in wire units (a legacy volume, 0-65381). The layout
// is LibAtem's AudioMixerHeadphoneSetCommand: mask at 0, bits 0-3 for the
// four gains at 2, 4, 6 and 8. Not checked on a switcher.
type AudioHeadphoneProperties struct {
	Gain           *uint16
	ProgramOutGain *uint16
	TalkbackGain   *uint16
	SidetoneGain   *uint16
}

func (c AudioHeadphoneProperties) Opcode() string    { return "CAMH" }
func (c AudioHeadphoneProperties) Encode() []byte    { return c.build().encode() }
func (c AudioHeadphoneProperties) Validate() error   { return c.build().err }
func (c AudioHeadphoneProperties) EchoKey() string   { return "AMHP" }
func (c AudioHeadphoneProperties) Unverified() bool  { return true }
func (c AudioHeadphoneProperties) Destructive() bool { return false }

func (c AudioHeadphoneProperties) build() *wire {
	w := newWire("CAMH", 12)
	var mask int64
	for i, f := range []struct {
		v    *uint16
		name string
	}{{c.Gain, "gain"}, {c.ProgramOutGain, "program_out_gain"}, {c.TalkbackGain, "talkback_gain"}, {c.SidetoneGain, "sidetone_gain"}} {
		if f.v != nil {
			mask |= 1 << i
			w.u16(2+2*i, f.name, int64(*f.v))
		}
	}
	w.u8(0, "mask", mask)
	return w
}

// AudioMonitorProperties is CAMm (AudioMonitorPropertiesCommand, op
// set_audio_monitor).
type AudioMonitorProperties struct {
	Enabled    *bool
	Volume     *uint16
	Mute       *bool
	Solo       *bool
	SoloSource *uint16
	Dim        *bool
	DimVolume  *uint16
}

func (c AudioMonitorProperties) Opcode() string    { return "CAMm" }
func (c AudioMonitorProperties) Encode() []byte    { return c.build().encode() }
func (c AudioMonitorProperties) Validate() error   { return c.build().err }
func (c AudioMonitorProperties) EchoKey() string   { return "AMmO" }
func (c AudioMonitorProperties) Unverified() bool  { return true }
func (c AudioMonitorProperties) Destructive() bool { return false }

func (c AudioMonitorProperties) build() *wire {
	w := newWire("CAMm", 12)
	var mask int64
	if c.Enabled != nil {
		mask |= 1 << 0
		w.flag(1, *c.Enabled)
	}
	if c.Volume != nil {
		mask |= 1 << 1
		w.u16(2, "volume", int64(*c.Volume))
	}
	if c.Mute != nil {
		mask |= 1 << 2
		w.flag(4, *c.Mute)
	}
	if c.Solo != nil {
		mask |= 1 << 3
		w.flag(5, *c.Solo)
	}
	if c.SoloSource != nil {
		mask |= 1 << 4
		w.u16(6, "solo_source", int64(*c.SoloSource))
	}
	if c.Dim != nil {
		mask |= 1 << 5
		w.flag(8, *c.Dim)
	}
	if c.DimVolume != nil {
		mask |= 1 << 6
		w.u16(10, "dim_volume", int64(*c.DimVolume))
	}
	w.u8(0, "mask", mask)
	return w
}

// SendAudioLevels is SALN (SendAudioLevelsCommand, op enable_audio_levels):
// start or stop the AMLv meter stream. No echo.
type SendAudioLevels struct {
	Enable bool
}

func (c SendAudioLevels) Opcode() string    { return "SALN" }
func (c SendAudioLevels) Encode() []byte    { return c.build().encode() }
func (c SendAudioLevels) Validate() error   { return c.build().err }
func (c SendAudioLevels) EchoKey() string   { return "" }
func (c SendAudioLevels) Unverified() bool  { return true }
func (c SendAudioLevels) Destructive() bool { return false }

func (c SendAudioLevels) build() *wire {
	w := newWire("SALN", 4)
	w.flag(0, c.Enable)
	return w
}
