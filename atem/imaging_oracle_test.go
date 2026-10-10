// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// imagingOracle is internal/fixtures/oracle/imaging.json: what bmdwire
// v1.3.0's mediaconvert C extension (compiled unmodified), its pure-Python
// rle_decode, and WebATEM's _prepare_frame (on Pillow 10.2.0) did with
// each input. gen_imaging.py says how it was made.
type imagingOracle struct {
	Bmdwire   string        `json:"bmdwire"`
	Pillow    string        `json:"pillow"`
	WebATEM   string        `json:"webatem"`
	RGBToATEM []imagingCase `json:"rgb_to_atem"`
	ATEMToRGB []imagingCase `json:"atem_to_rgb"`
	RLEEncode []imagingCase `json:"rle_encode"`
	RLEDecode []imagingCase `json:"rle_decode"`
	Fit       []fitCase     `json:"fit"`
	CFlags    string        `json:"cflags"`
	Compiler  string        `json:"compiler"`
	Generator string        `json:"generator"`
}

// imagingCase is one call. The input is In (hex), Gen (a generator this
// file reimplements) or Runs ([word hex, count] pairs); the result is Out
// (hex), OutSHA256 for a long one, or Error with Python's exception name
// and, for the C functions, its message.
type imagingCase struct {
	In          string      `json:"in"`
	Gen         *imagingGen `json:"gen"`
	Runs        [][]any     `json:"runs"`
	Premultiply bool        `json:"premultiply"`
	Out         string      `json:"out"`
	OutSHA256   string      `json:"out_sha256"`
	OutLen      int         `json:"out_len"`
	Error       string      `json:"error"`
	Message     string      `json:"message"`
	DecodesBack *bool       `json:"decodes_back"`
}

type imagingGen struct {
	Kind string `json:"kind"`
	Seed uint32 `json:"seed"`
	N    int    `json:"n"`
	Mask int    `json:"mask"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// fitCase is one _prepare_frame call on a generated source image.
type fitCase struct {
	Src          imagingGen `json:"src"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	Scaled       [2]int     `json:"scaled"`
	ScaledSHA256 string     `json:"scaled_sha256"`
	Out          string     `json:"out"`
	OutSHA256    string     `json:"out_sha256"`
	OutLen       int        `json:"out_len"`
	YCbCrASHA256 string     `json:"ycbcra_sha256"`
}

func loadImagingOracle(t *testing.T) *imagingOracle {
	t.Helper()
	var o imagingOracle
	if err := json.Unmarshal(fixtures.Bytes(t, "oracle/imaging.json"), &o); err != nil {
		t.Fatal(err)
	}
	if o.Bmdwire != "v1.3.0" || o.Pillow != "10.2.0" || o.WebATEM != "v2.0.2" {
		t.Fatalf("oracle generated from bmdwire %s, Pillow %s, WebATEM %s", o.Bmdwire, o.Pillow, o.WebATEM)
	}
	for name, n := range map[string]int{"rgb_to_atem": len(o.RGBToATEM), "atem_to_rgb": len(o.ATEMToRGB),
		"rle_encode": len(o.RLEEncode), "rle_decode": len(o.RLEDecode), "fit": len(o.Fit)} {
		if n == 0 {
			t.Errorf("no %s cases", name)
		}
	}
	return &o
}

// oracleNoise is gen_imaging.py's noise: xorshift32 states, little-endian.
func oracleNoise(seed uint32, n int) []byte {
	out := make([]byte, (n+3)/4*4)
	x := seed
	for i := 0; i < len(out); i += 4 {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		binary.LittleEndian.PutUint32(out[i:], x)
	}
	return out[:n]
}

// oracleSweep is gen_imaging.py's sweep: 65536 pairs of equal pixels, pair
// k with value k&255 in the masked channels and alpha k>>8.
func oracleSweep(mask int) []byte {
	out := make([]byte, 0, 65536*8)
	for k := range 65536 {
		v, a := byte(k), byte(k>>8)
		px := [4]byte{0, 0, 0, a}
		for c := range 3 {
			if mask&(1<<c) != 0 {
				px[c] = v
			}
		}
		out = append(out, px[:]...)
		out = append(out, px[:]...)
	}
	return out
}

// oracleColours is one chunk of gen_imaging.py's colours_chunks: pairs
// first..first+n-1 of every 24-bit colour in both pixel slots.
func oracleColours(first, n int) []byte {
	out := make([]byte, 0, 8*n)
	for k := first; k < first+n; k++ {
		r, g, b := byte(k>>16), byte(k>>8), byte(k)
		out = append(out, ^r, ^g, ^b, byte(k>>4), r, g, b, byte(k*167))
	}
	return out
}

// oracleYCbCr is gen_imaging.py's ycbcr: 2^20 words covering every (Y, Cb)
// and (Y, Cr) pair, with Cr equal to Cb (mask 0) or mirrored (mask 1).
func oracleYCbCr(mask int) []byte {
	out := make([]byte, 0, 8<<20)
	for k := range 1 << 20 {
		y, c := k&1023, k>>10
		cr := c
		if mask == 1 {
			cr = 1023 - c
		}
		out = append(out, packBlock(k&4095, c, y, (k>>8)&4095, cr, 1023-y)...)
	}
	return out
}

// packBlock is test_imaging_alpha.py's _pack_block: one 8-byte group, two
// pixels, in the bit layout atem_to_rgb reads.
func packBlock(a1, cb, y1, a2, cr, y2 int) []byte {
	return []byte{
		byte(a1 >> 4),
		byte((a1&0x0f)<<4 | (cb>>6)&0x0f),
		byte((cb&0x3f)<<2 | (y1>>8)&0x03),
		byte(y1),
		byte(a2 >> 4),
		byte((a2&0x0f)<<4 | (cr>>6)&0x0f),
		byte((cr&0x3f)<<2 | (y2>>8)&0x03),
		byte(y2),
	}
}

// oraclePattern is gen_imaging.py's pattern: a w x h straight-RGBA image.
func oraclePattern(kind string, w, h int, seed uint32) []byte {
	n := w * h
	switch kind {
	case "noise":
		return oracleNoise(seed, 4*n)
	case "noise-opaque", "noise-binary":
		b := oracleNoise(seed, 4*n)
		for i := 3; i < len(b); i += 4 {
			switch {
			case kind == "noise-opaque" || b[i] >= 128:
				b[i] = 255
			default:
				b[i] = 0
			}
		}
		return b
	case "gradient":
		b := make([]byte, 0, 4*n)
		for y := range h {
			for x := range w {
				b = append(b, byte(x*255/max(1, w-1)), byte(y*255/max(1, h-1)), byte((x^y)&255),
					byte(255-(x+y)*255/max(1, w+h-2)))
			}
		}
		return b
	case "checker":
		b := make([]byte, 0, 4*n)
		for y := range h {
			for x := range w {
				if (x/int(seed)+y/int(seed))%2 != 0 {
					b = append(b, 255, 255, 255, 255)
				} else {
					b = append(b, 0, 0, 0, 255)
				}
			}
		}
		return b
	case "solid":
		return bytes.Repeat(binary.BigEndian.AppendUint32(nil, seed), n)
	}
	panic("unknown pattern " + kind)
}

func (c *imagingCase) input(t *testing.T) []byte {
	t.Helper()
	switch {
	case c.Gen != nil && c.Gen.Kind == "noise":
		return oracleNoise(c.Gen.Seed, c.Gen.N)
	case c.Gen != nil && c.Gen.Kind == "sweep":
		return oracleSweep(c.Gen.Mask)
	case c.Gen != nil && c.Gen.Kind == "ycbcr":
		return oracleYCbCr(c.Gen.Mask)
	case c.Gen != nil:
		t.Fatalf("unknown generator %q", c.Gen.Kind)
	case c.Runs != nil:
		var b []byte
		for _, r := range c.Runs {
			w, err := hex.DecodeString(r[0].(string))
			if err != nil {
				t.Fatal(err)
			}
			b = append(b, bytes.Repeat(w, int(r[1].(float64)))...)
		}
		return b
	}
	b, err := hex.DecodeString(c.In)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sweep reports whether the case is one of the colour sweeps (every 24-bit
// colour, the 65,536-pair channel sweeps, every (Y, Cb) and (Y, Cr) pair).
// They take most of the oracle's time, so -short skips them.
func (c *imagingCase) sweep() bool {
	return c.Gen != nil && (c.Gen.Kind == "colours" || c.Gen.Kind == "sweep" || c.Gen.Kind == "ycbcr")
}

func (c *imagingCase) String() string {
	switch {
	case c.Gen != nil:
		return fmt.Sprintf("%+v", *c.Gen)
	case c.Runs != nil:
		return fmt.Sprintf("runs %v", c.Runs)
	case len(c.In) > 64:
		return fmt.Sprintf("in %s... (%d bytes)", c.In[:64], len(c.In)/2)
	}
	return "in " + c.In
}

// check compares a result with the case's: the same error (and, when the
// case has one, the same message after "atem: "), or the same bytes.
func (c *imagingCase) check(t *testing.T, where string, got []byte, err error) {
	t.Helper()
	if c.Error != "" {
		switch {
		case err == nil:
			t.Errorf("%s: atemwire raises %s(%q), Go returned %d bytes", where, c.Error, c.Message, len(got))
		case c.Message != "" && err.Error() != "atem: "+c.Message:
			t.Errorf("%s: error %q, atemwire's is %q", where, err, c.Message)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: %v", where, err)
		return
	}
	checkBytes(t, where, got, c.Out, c.OutSHA256, c.OutLen)
}

func checkBytes(t *testing.T, where string, got []byte, wantHex, wantSHA string, wantLen int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: nil result", where)
	}
	if wantSHA != "" {
		if sum := sha256.Sum256(got); hex.EncodeToString(sum[:]) != wantSHA || len(got) != wantLen {
			t.Errorf("%s: %d bytes with SHA-256 %x, want %d bytes with %s", where, len(got), sum, wantLen, wantSHA)
		}
		return
	}
	if h := hex.EncodeToString(got); h != wantHex {
		t.Errorf("%s:\n got %s\nwant %s", where, h, wantHex)
	}
}

// RGBAToYCbCrA, YCbCrAToRGBA, RLEEncode and RLEDecode do what bmdwire
// v1.3.0's rgb_to_atem, atem_to_rgb, rle_encode and rle_decode did with
// every input in the oracle: the same bytes, or an error where they raised.
func TestImagingMatchesAtemwire(t *testing.T) {
	o := loadImagingOracle(t)
	t.Run("rgb_to_atem", func(t *testing.T) {
		for i, c := range o.RGBToATEM {
			if c.sweep() && testing.Short() {
				continue
			}
			where := fmt.Sprintf("case %d %s premultiply=%v", i, &c, c.Premultiply)
			if c.Gen != nil && c.Gen.Kind == "colours" {
				// 128 MiB, converted and hashed a chunk at a time as the
				// generator does: each 8-byte group converts on its own.
				h, n := sha256.New(), 0
				for first := 0; first < 1<<24; first += 1 << 20 {
					got, err := RGBAToYCbCrA(oracleColours(first, 1<<20), c.Premultiply)
					if err != nil {
						t.Fatalf("%s: %v", where, err)
					}
					h.Write(got)
					n += len(got)
				}
				if sum := hex.EncodeToString(h.Sum(nil)); sum != c.OutSHA256 || n != c.OutLen {
					t.Errorf("%s: %d bytes with SHA-256 %s, want %d bytes with %s", where, n, sum, c.OutLen, c.OutSHA256)
				}
				continue
			}
			got, err := RGBAToYCbCrA(c.input(t), c.Premultiply)
			c.check(t, where, got, err)
		}
	})
	t.Run("atem_to_rgb", func(t *testing.T) {
		for i, c := range o.ATEMToRGB {
			if c.sweep() && testing.Short() {
				continue
			}
			got, err := YCbCrAToRGBA(c.input(t))
			c.check(t, fmt.Sprintf("case %d %s", i, &c), got, err)
		}
	})
	t.Run("rle_encode", func(t *testing.T) {
		for i, c := range o.RLEEncode {
			in := c.input(t)
			where := fmt.Sprintf("case %d %s", i, &c)
			got, err := RLEEncode(in)
			c.check(t, where, got, err)
			if c.DecodesBack == nil {
				continue
			}
			// rle_decode(rle_encode(x)) == x is what atemwire found too.
			back, err := RLEDecode(got)
			if ok := err == nil && bytes.Equal(back, in); ok != *c.DecodesBack {
				t.Errorf("%s: decodes back %v (%v), atemwire's %v", where, ok, err, *c.DecodesBack)
			}
		}
	})
	t.Run("rle_decode", func(t *testing.T) {
		for i, c := range o.RLEDecode {
			got, err := RLEDecode(c.input(t))
			c.check(t, fmt.Sprintf("case %d %s", i, &c), got, err)
		}
	})
}

// FitFrame does what WebATEM's _prepare_frame did with every source image
// in the oracle, byte for byte: the same scaled size, the same resized
// image, the same canvas and, where recorded, the same Y'CbCrA frame.
func TestFitFrameMatchesWebATEM(t *testing.T) {
	o := loadImagingOracle(t)
	for i, c := range o.Fit {
		where := fmt.Sprintf("case %d %+v into %dx%d", i, c.Src, c.Width, c.Height)
		src := oraclePattern(c.Src.Kind, c.Src.W, c.Src.H, c.Src.Seed)
		if nw, nh := fitSize(c.Src.W, c.Src.H, c.Width, c.Height); nw != c.Scaled[0] || nh != c.Scaled[1] {
			t.Errorf("%s: scaled to %dx%d, want %dx%d", where, nw, nh, c.Scaled[0], c.Scaled[1])
			continue
		}
		scaled := pillowResize(src, c.Src.W, c.Src.H, c.Scaled[0], c.Scaled[1])
		checkBytes(t, where+" resized", scaled, "", c.ScaledSHA256, len(scaled))
		img := &image.NRGBA{Pix: src, Stride: 4 * c.Src.W, Rect: image.Rect(0, 0, c.Src.W, c.Src.H)}
		got := FitFrame(img, c.Width, c.Height)
		checkBytes(t, where, got, c.Out, c.OutSHA256, c.OutLen)
		if c.YCbCrASHA256 != "" {
			frame, err := RGBAToYCbCrA(got, true)
			if err != nil {
				t.Fatal(err)
			}
			checkBytes(t, where+" as Y'CbCrA", frame, "", c.YCbCrASHA256, len(frame))
		}
	}
}
