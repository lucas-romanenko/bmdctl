// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
)

// Still frames travel as 10-bit BT.709 Y'CbCrA 4:2:2: every 8 bytes hold
// two pixels (two lumas, two alphas, one Cb and one Cr), so a frame is
// width*height*4 bytes, the same length as its 8-bit RGBA form. Stills are
// sent RLE-compressed in those 8-byte words.
//
// RGBAToYCbCrA, YCbCrAToRGBA and RLEEncode port bmdwire v1.3.0's C
// extension, atemwire/mediaconvertmodule.c (rgb_to_atem, atem_to_rgb,
// rle_encode), and RLEDecode ports the pure-Python rle_decode in
// atemwire/imaging.py. They match the C bit for bit: every float32 and
// float64 step is rounded where the C rounds it, and each product that
// feeds a sum is converted explicitly, so the Go compiler can't fuse the
// two into one FMA (the C extension is compiled without FMA). The width
// and height the C functions take are unused there and absent here.

// The C file's #define'd constants as gcc folds them, each operation
// rounded to double. Go would fold the same expressions exactly and get a
// different last bit for three of them. No 10-bit input turns that bit
// into a different byte (gen_imaging.py searched them all), but the port
// keeps gcc's values rather than rely on it.
const (
	bt709BiRange = 0x1.0f72a125f9cc2p-7 // (1.0 - 0.0722) / (224 / 2)
	bt709RiRange = 0x1.ccbdd201fd4efp-8 // (1.0 - 0.2126) / (224 / 2)
	bt709CoeffBG = 0x1.9d7e9177b21cbp-4 // 0.0722 / 0.7152
	bt709CoeffRG = 0x1.3064cbe238202p-2 // 0.2126 / 0.7152
)

// rleHeader is the word that marks a run: the count (big-endian) and the
// repeated word follow it.
const rleHeader = 0xFEFEFEFEFEFEFEFE

// maxRLEOutput bounds what RLEDecode will produce: twice the largest frame
// atemwire knows (8K, 7680x4320x4 = 132,710,400 bytes). atemwire has no
// bound; a run longer than memory raises MemoryError there.
const maxRLEOutput = 1 << 28

// RGBAToYCbCrA converts a straight-alpha RGBA frame (4 bytes a pixel, two
// pixels at a time) to the switcher's Y'CbCrA. With premultiply, colour is
// multiplied by alpha first; WebATEM asks for that for PNG files, whose
// alpha is straight. As in the C, each pair's Cb and Cr come from its
// second pixel alone. It is bmdwire v1.3.0's mediaconvert.rgb_to_atem: the
// length must be a multiple of 8, and an empty frame gives an empty one.
func RGBAToYCbCrA(rgba []byte, premultiply bool) ([]byte, error) {
	if len(rgba)%8 != 0 {
		return nil, fmt.Errorf("atem: frame length %d is not a multiple of 8", len(rgba))
	}
	out := make([]byte, len(rgba))
	for i := 0; i < len(rgba); i += 8 {
		p := rgba[i : i+8 : i+8]
		r1 := float32(p[0]) / 255
		g1 := float32(p[1]) / 255
		b1 := float32(p[2]) / 255
		r2 := float32(p[4]) / 255
		g2 := float32(p[5]) / 255
		b2 := float32(p[6]) / 255

		if premultiply {
			a1 := float32(p[3]) / 255
			a2 := float32(p[7]) / 255
			r1 = r1 * a1
			g1 = g1 * a1
			b1 = b1 * a1
			r2 = r2 * a2
			g2 = g2 * a2
			b2 = b2 * a2
		}

		y1 := float32(0.2126*r1) + float32(0.7152*g1) + float32(0.0722*b1)
		y2 := float32(0.2126*r2) + float32(0.7152*g2) + float32(0.0722*b2)
		cb := (b2 - y2) / 1.8556
		cr := (r2 - y2) / 1.5748

		a10a := uint16((int(p[3])<<2)*219/255 + 15<<2 + 1)
		a10b := uint16((int(p[7])<<2)*219/255 + 15<<2 + 1)
		y10a := cClamp(uint16(float32(y1*876)+64), 64, 940)
		y10b := cClamp(uint16(float32(y2*876)+64), 64, 940)
		// fmax(0.0f, ...) widens to double before the truncating cast.
		cb10 := cClamp(uint16(cFmax(0, float64(float32(cb*896)+512))), 44, 960)
		cr10 := cClamp(uint16(cFmax(0, float64(float32(cr*896)+512))), 44, 960)

		w := out[i : i+8 : i+8]
		w[0] = byte(a10a >> 4)
		w[1] = byte((a10a&0x0f)<<4 | cb10>>6)
		w[2] = byte((cb10&0x3f)<<2 | y10a>>8)
		w[3] = byte(y10a)
		w[4] = byte(a10b >> 4)
		w[5] = byte((a10b&0x0f)<<4 | cr10>>6)
		w[6] = byte((cr10&0x3f)<<2 | y10b>>8)
		w[7] = byte(y10b)
	}
	return out, nil
}

// YCbCrAToRGBA converts a Y'CbCrA frame to straight-alpha RGBA, clamping
// every channel to 0-255 (switcher captures mark opaque pixels with alpha
// near 1023, above the 937 RGBAToYCbCrA writes). It is bmdwire v1.3.0's
// mediaconvert.atem_to_rgb: the length must be a multiple of 8, and an
// empty frame gives an empty one.
func YCbCrAToRGBA(frame []byte) ([]byte, error) {
	if len(frame)%8 != 0 {
		return nil, fmt.Errorf("atem: frame length %d is not a multiple of 8", len(frame))
	}
	out := make([]byte, len(frame))
	for i := 0; i < len(frame); i += 8 {
		b := frame[i : i+8 : i+8]
		// The C reads through a plain char, signed on x86-64, so bytes 0
		// and 4 are sign-extended before the shift and the sum is then
		// truncated to unsigned short. The masked bytes lose the sign.
		a1 := uint16(int32(int8(b[0]))<<4 + int32(b[1]&0xf0)>>4)
		a2 := uint16(int32(int8(b[4]))<<4 + int32(b[5]&0xf0)>>4)
		cb := uint16(b[1]&0x0f)<<6 + uint16(b[2]&0xfc)>>2
		cr := uint16(b[5]&0x0f)<<6 + uint16(b[6]&0xfc)>>2
		y1 := uint16(b[2]&0x03)<<8 + uint16(b[3])
		y2 := uint16(b[6]&0x03)<<8 + uint16(b[7])

		cbf := float32(bt709BiRange * float64(int32(cb)<<6-128<<8))
		crf := float32(bt709RiRange * float64(int32(cr)<<6-128<<8))
		y1f := float32((float64(int32(y1)<<6) - 16<<8) / 219)
		y2f := float32((float64(int32(y2)<<6) - 16<<8) / 219)

		// The sums are float; fmin and fmax take them as double, and the
		// green difference is computed in double throughout.
		r1f := float32(clamp255(float64(y1f + crf)))
		g1f := float32(clamp255(float64(y1f) - float64(float64(cbf)*bt709CoeffBG) - float64(float64(crf)*bt709CoeffRG)))
		b1f := float32(clamp255(float64(y1f + cbf)))
		r2f := float32(clamp255(float64(y2f + crf)))
		g2f := float32(clamp255(float64(y2f) - float64(float64(cbf)*bt709CoeffBG) - float64(float64(crf)*bt709CoeffRG)))
		b2f := float32(clamp255(float64(y2f + cbf)))

		a1f := float32(clamp255(float64(int32(a1)-16) / 3.6))
		a2f := float32(clamp255(float64(int32(a2)-16) / 3.6))

		o := out[i : i+8 : i+8]
		o[0] = byte(r1f)
		o[1] = byte(g1f)
		o[2] = byte(b1f)
		o[3] = byte(a1f)
		o[4] = byte(r2f)
		o[5] = byte(g2f)
		o[6] = byte(b2f)
		o[7] = byte(a2f)
	}
	return out, nil
}

// FrameImage decodes a Y'CbCrA frame of width x height pixels into an
// image, through YCbCrAToRGBA: what atemwire's profile save does with
// atem_to_rgb and Pillow's Image.frombytes('RGBA', ...) before writing a
// PNG. Unlike frombytes, which ignores surplus bytes, the frame must be
// exactly width*height*4 bytes.
func FrameImage(frame []byte, width, height int) (*image.NRGBA, error) {
	if width < 0 || height < 0 || len(frame) != width*height*4 {
		return nil, fmt.Errorf("atem: frame of %d bytes is not %dx%d pixels", len(frame), width, height)
	}
	pix, err := YCbCrAToRGBA(frame)
	if err != nil {
		return nil, err
	}
	return &image.NRGBA{Pix: pix, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}, nil
}

// RLEEncode compresses a frame in 8-byte words: the first word of a run is
// written as is, and the repeats after it either as themselves (up to two)
// or as the header word, a big-endian count and the word. It is bmdwire
// v1.3.0's mediaconvert.rle_encode: the length must be a multiple of 8, and
// a word of eight 0xFE bytes (the header, which no RGBAToYCbCrA output
// contains) is an error, since the stream can't represent it.
func RLEEncode(frame []byte) ([]byte, error) {
	if len(frame)%8 != 0 {
		return nil, fmt.Errorf("atem: RLE input length %d is not a multiple of 8", len(frame))
	}
	n := len(frame) / 8
	// Words are only compared, so their byte order doesn't matter.
	word := func(i int) uint64 { return binary.LittleEndian.Uint64(frame[8*i:]) }
	out := make([]byte, 0, len(frame))
	put := func(i int) { out = append(out, frame[8*i:8*i+8]...) }
	flush := func(c, last int) {
		if c > 2 {
			out = binary.BigEndian.AppendUint64(out, rleHeader)
			out = binary.BigEndian.AppendUint64(out, uint64(c))
			put(last)
		} else {
			for range c {
				put(last)
			}
		}
	}
	c, i := 0, 0
	for i = 0; i < n; i++ {
		if word(i) == rleHeader {
			return nil, errors.New("atem: RLE input contains the reserved header word 0xFEFEFEFEFEFEFEFE")
		}
		if i != 0 && word(i-1) == word(i) {
			c++
			// The C compares the word index with the length in bytes, so
			// this always continues; a run at the very end is written by
			// the flush after the loop.
			if i+1 < len(frame) {
				continue
			}
		}
		flush(c, i-1)
		put(i)
		c = 0
	}
	flush(c, i-1)
	return out, nil
}

// RLEDecode expands an RLE stream: each word of eight 0xFE bytes is
// followed by a big-endian count and the word to repeat; anything else is
// copied. It is bmdwire v1.3.0's atemwire/imaging.py rle_decode and errors
// where that raises: a ragged tail made only of 0xFE bytes (IndexError), a
// header without a whole count after it (struct.error), and a run too long
// to hold (OverflowError). A run with its word cut short repeats what is
// left of it, and a ragged tail that isn't all 0xFE is copied, as there.
// A run that would take the output past 256 MiB is an error too: atemwire
// would try to allocate it and raise MemoryError only if that failed.
func RLEDecode(data []byte) ([]byte, error) {
	result := make([]byte, 0, len(data))
	for offset := 0; offset < len(data); {
		block := data[offset:min(offset+8, len(data))]
		header := true
		for k := range 8 {
			if offset+k >= len(data) {
				return nil, fmt.Errorf("atem: RLE stream ends in a partial run header at byte %d", offset)
			}
			if data[offset+k] != 0xfe {
				header = false
				break
			}
		}
		if !header {
			result = append(result, block...)
			offset += 8
			continue
		}
		offset += 8
		if offset+8 > len(data) {
			return nil, fmt.Errorf("atem: RLE stream ends in a run count at byte %d", offset)
		}
		count := binary.BigEndian.Uint64(data[offset:])
		offset += 8
		block = data[offset:min(offset+8, len(data))]
		room := max(0, maxRLEOutput-len(result))
		if count > math.MaxInt64 || len(block) > 0 && count > uint64(room/len(block)) {
			return nil, fmt.Errorf("atem: RLE run of %d words at byte %d is too long", count, offset-16)
		}
		if len(block) > 0 {
			result = slices.Grow(result, int(count)*len(block))
			for range count {
				result = append(result, block...)
			}
		}
		offset += 8
	}
	return result, nil
}

// cClamp is the C file's clamp, whose temporary is a signed short: a value
// above 32767 wraps negative there and comes back unclamped.
func cClamp(v, lo, hi uint16) uint16 {
	t := int16(v)
	if v < lo {
		t = int16(lo)
	}
	if int32(t) > int32(hi) {
		return hi
	}
	return uint16(t)
}

// cFmax is C99 fmax: a NaN argument is ignored.
func cFmax(x, y float64) float64 {
	if x != x || x < y {
		return y
	}
	return x
}

// cFmin is C99 fmin: a NaN argument is ignored.
func cFmin(x, y float64) float64 {
	if x != x || y < x {
		return y
	}
	return x
}

// clamp255 is the C's fmax(0, fmin(255, x)).
func clamp255(x float64) float64 { return cFmax(0, cFmin(255, x)) }
