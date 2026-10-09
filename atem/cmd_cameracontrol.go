// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/binary"
	"math"
)

// SURFACE.md §1.12 camera-control, from bmdwire v1.3.0
// atemwire/messages/camera_control.py.

// Camera control data types (CameraControl.Datatype).
const (
	CameraBool    uint8 = 0
	CameraInt8    uint8 = 1
	CameraInt16   uint8 = 2
	CameraInt32   uint8 = 3
	CameraInt64   uint8 = 4
	CameraString  uint8 = 5
	CameraFixed16 uint8 = 128
)

// CameraControl is CCmd (CameraControlCommand, op camera_control): one
// Blackmagic camera control parameter. Destination 255 is broadcast. Data
// holds the values: bool for CameraBool (any value is taken for its truth,
// as in Python); integers for the integer types; numbers for
// CameraFixed16, sent as int(v × 2048); strings or []byte for
// CameraString. A nil Data sends no data block; an empty one sends eight
// zero bytes, as atemwire does. atemwire has no category or parameter
// table. Restored from upstream pyatem; unverified.
type CameraControl struct {
	Destination, Category, Parameter uint8
	Relative                         bool
	Datatype                         uint8
	Data                             []any
}

func (c CameraControl) Opcode() string { return "CCmd" }
func (c CameraControl) Encode() []byte { return c.build().encode() }
func (c CameraControl) Validate() error {
	return c.build().err
}
func (c CameraControl) EchoKey() string {
	return key("CCdP", c.Destination, c.Category, c.Parameter)
}
func (c CameraControl) Unverified() bool  { return true }
func (c CameraControl) Destructive() bool { return false }

func (c CameraControl) build() *wire {
	w := newWire("CCmd", 16)
	w.u8(0, "destination", int64(c.Destination))
	w.u8(1, "category", int64(c.Category))
	w.u8(2, "parameter", int64(c.Parameter))
	w.flag(3, c.Relative)
	w.u8(4, "datatype", int64(c.Datatype))
	// atemwire's _COUNT_AT_9: the element count goes in byte 9 for these
	// two types and byte 7 for the rest.
	if c.Datatype == CameraFixed16 || c.Datatype == CameraInt32 {
		w.u8(9, "count", int64(len(c.Data)))
	} else {
		w.u8(7, "count", int64(len(c.Data)))
	}
	if c.Data == nil {
		return w
	}
	data := c.encodeData(w)
	for len(data) < 8 {
		data = append(data, 0)
	}
	w.b = append(w.b, data...)
	return w
}

// encodeData is CameraControlCommand._encode.
func (c CameraControl) encodeData(w *wire) []byte {
	var out []byte
	for _, v := range c.Data {
		switch c.Datatype {
		case CameraBool:
			if truthy(v) {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		case CameraInt8, CameraInt16, CameraInt32, CameraInt64:
			n, ok := integer(v)
			if !ok {
				w.fail("camera control: %v (%T) is not an integer", v, v)
				return nil
			}
			out = c.appendInt(w, out, n)
		case CameraFixed16:
			f, ok := number(v)
			if !ok {
				w.fail("camera control: %v (%T) is not a number", v, v)
				return nil
			}
			n := w.truncate("fixed16", f*2048)
			if n < math.MinInt16 || n > math.MaxInt16 {
				w.fail("fixed16 %v does not fit", v)
				return nil
			}
			out = binary.BigEndian.AppendUint16(out, uint16(n))
		case CameraString:
			switch s := v.(type) {
			case string:
				out = append(out, s...)
			case []byte:
				out = append(out, s...)
			default:
				w.fail("camera control: %v (%T) is not a string", v, v)
				return nil
			}
		default:
			w.fail("camera control: data type %d has no element format", c.Datatype)
			return nil
		}
	}
	return out
}

func (c CameraControl) appendInt(w *wire, out []byte, n int64) []byte {
	lim := map[uint8][2]int64{
		CameraInt8:  {math.MinInt8, math.MaxInt8},
		CameraInt16: {math.MinInt16, math.MaxInt16},
		CameraInt32: {math.MinInt32, math.MaxInt32},
		CameraInt64: {math.MinInt64, math.MaxInt64},
	}[c.Datatype]
	if n < lim[0] || n > lim[1] {
		w.fail("camera control: %d does not fit data type %d", n, c.Datatype)
		return out
	}
	switch c.Datatype {
	case CameraInt8:
		return append(out, byte(n))
	case CameraInt16:
		return binary.BigEndian.AppendUint16(out, uint16(n))
	case CameraInt32:
		return binary.BigEndian.AppendUint32(out, uint32(n))
	}
	return binary.BigEndian.AppendUint64(out, uint64(n))
}

// truthy is Python's bool() for the values a caller can put in Data.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case []byte:
		return len(x) != 0
	}
	if n, ok := number(v); ok {
		return n != 0
	}
	return true
}

// integer accepts what struct.pack's integer formats do: ints and bools,
// not floats.
func integer(v any) (int64, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	}
	return 0, false
}

func number(v any) (float64, bool) {
	if n, ok := integer(v); ok {
		return float64(n), true
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	return 0, false
}
