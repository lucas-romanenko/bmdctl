// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.17 streaming, from bmdwire v1.3.0
// atemwire/messages/streaming.py. All three commands were restored from
// upstream pyatem and are unverified.

// StreamingServiceSet is CRSS (StreamingServiceSetCommand, op
// set_streaming_service). Name is cut to 64 bytes, URL and Key to 512. The
// two bitrates (bit/s) share one mask bit: set both or neither.
type StreamingServiceSet struct {
	Name       *string
	URL        *string
	Key        *string
	BitrateMin *uint32
	BitrateMax *uint32
}

func (c StreamingServiceSet) Opcode() string    { return "CRSS" }
func (c StreamingServiceSet) Encode() []byte    { return c.build().encode() }
func (c StreamingServiceSet) Validate() error   { return c.build().err }
func (c StreamingServiceSet) EchoKey() string   { return "SRSU" }
func (c StreamingServiceSet) Unverified() bool  { return true }
func (c StreamingServiceSet) Destructive() bool { return false }

func (c StreamingServiceSet) build() *wire {
	w := newWire("CRSS", 1100)
	var mask int64
	if (c.BitrateMin == nil) != (c.BitrateMax == nil) {
		w.fail("bitrate_min and bitrate_max must be given together")
	}
	if c.Name != nil {
		mask |= 1 << 0
		w.str(1, 64, *c.Name)
	}
	if c.URL != nil {
		mask |= 1 << 1
		w.str(65, 512, *c.URL)
	}
	if c.Key != nil {
		mask |= 1 << 2
		w.str(577, 512, *c.Key)
	}
	if c.BitrateMin != nil {
		mask |= 1 << 3
		w.u32(1092, "bitrate_min", int64(*c.BitrateMin))
	}
	if c.BitrateMax != nil {
		mask |= 1 << 3
		w.u32(1096, "bitrate_max", int64(*c.BitrateMax))
	}
	w.u8(0, "mask", mask)
	return w
}

// StreamingAudioBitrate is STAB (StreamingAudioBitrateCommand, op
// set_streaming_audio_bitrate), in bit/s; in practice always 128000.
type StreamingAudioBitrate struct {
	Min, Max uint32
}

func (c StreamingAudioBitrate) Opcode() string    { return "STAB" }
func (c StreamingAudioBitrate) Encode() []byte    { return c.build().encode() }
func (c StreamingAudioBitrate) Validate() error   { return c.build().err }
func (c StreamingAudioBitrate) EchoKey() string   { return "STAB" }
func (c StreamingAudioBitrate) Unverified() bool  { return true }
func (c StreamingAudioBitrate) Destructive() bool { return false }

func (c StreamingAudioBitrate) build() *wire {
	w := newWire("STAB", 8)
	w.u32(0, "bitrate_min", int64(c.Min))
	w.u32(4, "bitrate_max", int64(c.Max))
	return w
}

// StreamingStatusSet is StrR (StreamingStatusSetCommand; ops
// start_streaming, stop_streaming).
type StreamingStatusSet struct {
	Streaming bool
}

func (c StreamingStatusSet) Opcode() string    { return "StrR" }
func (c StreamingStatusSet) Encode() []byte    { return c.build().encode() }
func (c StreamingStatusSet) Validate() error   { return c.build().err }
func (c StreamingStatusSet) EchoKey() string   { return "StRS" }
func (c StreamingStatusSet) Unverified() bool  { return true }
func (c StreamingStatusSet) Destructive() bool { return false }

func (c StreamingStatusSet) build() *wire {
	w := newWire("StrR", 4)
	w.flag(0, c.Streaming)
	return w
}
