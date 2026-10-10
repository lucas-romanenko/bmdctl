// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// The first group of tests ports bmdwire v1.3.0's
// atemwire/tests/test_imaging_alpha.py, one for one: the alpha clamp in
// atem_to_rgb. The second ports test_mediaconvert_hardening.py. The rest
// cover what the Go port adds.

// decodeAlphas is test_imaging_alpha.py's _decode_alphas: one block with
// the given wire alpha on both pixels (neutral grey), and the two 8-bit
// alphas it decodes to.
func decodeAlphas(t *testing.T, alpha int) (byte, byte) {
	t.Helper()
	rgba, err := YCbCrAToRGBA(packBlock(alpha, 512, 502, alpha, 512, 502))
	if err != nil {
		t.Fatal(err)
	}
	return rgba[3], rgba[7]
}

// test_switcher_opaque_alpha_clamps_to_255
func TestSwitcherOpaqueAlphaClampsTo255(t *testing.T) {
	// Pre-clamp these wrapped: 1023 -> 23, 940 -> 0, 4095 -> 109.
	for _, alpha := range []int{1023, 940, 4095} {
		if a1, a2 := decodeAlphas(t, alpha); a1 != 255 || a2 != 255 {
			t.Errorf("wire alpha %d: %d, %d, want 255, 255", alpha, a1, a2)
		}
	}
}

// test_encoder_max_alpha_decodes_to_255
func TestEncoderMaxAlphaDecodesTo255(t *testing.T) {
	// 937 is what RGBAToYCbCrA writes for 8-bit 255; already in range,
	// must stay exact.
	if a1, a2 := decodeAlphas(t, 937); a1 != 255 || a2 != 255 {
		t.Errorf("%d, %d, want 255, 255", a1, a2)
	}
}

// test_low_alpha_clamps_to_zero_not_bright
func TestLowAlphaClampsToZeroNotBright(t *testing.T) {
	// (0 - 16) / 3.6 is negative; the unsigned cast would wrap bright.
	if a1, a2 := decodeAlphas(t, 0); a1 != 0 || a2 != 0 {
		t.Errorf("%d, %d, want 0, 0", a1, a2)
	}
}

// test_opaque_rgba_roundtrip_keeps_full_alpha
func TestOpaqueRGBARoundtripKeepsFullAlpha(t *testing.T) {
	rgba := bytes.Repeat([]byte{200, 100, 50, 255}, 2)
	wire, err := RGBAToYCbCrA(rgba, false)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := YCbCrAToRGBA(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded[3] != 255 || decoded[7] != 255 {
		t.Errorf("alphas %d, %d, want 255, 255", decoded[3], decoded[7])
	}
}

var rleHeaderWord = bytes.Repeat([]byte{0xfe}, 8)

// test_rle_header_word_raises_instead_of_aborting
func TestRLEHeaderWordRaisesInsteadOfAborting(t *testing.T) {
	data := append(append(make([]byte, 8), rleHeaderWord...), bytes.Repeat([]byte{1}, 8)...)
	if _, err := RLEEncode(data); err == nil {
		t.Error("no error")
	}
}

// test_rle_header_word_first_raises
func TestRLEHeaderWordFirstRaises(t *testing.T) {
	if _, err := RLEEncode(rleHeaderWord); err == nil {
		t.Error("no error")
	}
}

// test_rle_rejects_ragged_length
func TestRLERejectsRaggedLength(t *testing.T) {
	if _, err := RLEEncode(make([]byte, 9)); err == nil {
		t.Error("no error")
	}
}

// test_rle_rejects_one_byte_input
func TestRLERejectsOneByteInput(t *testing.T) {
	// The old len==1 branch copied a full 8-byte word through 1-byte
	// buffers on both sides.
	if _, err := RLEEncode([]byte{0xfe}); err == nil {
		t.Error("no error")
	}
}

// test_atem_to_rgb_rejects_ragged_length
func TestYCbCrAToRGBARejectsRaggedLength(t *testing.T) {
	if _, err := YCbCrAToRGBA(make([]byte, 12)); err == nil {
		t.Error("no error")
	}
}

// test_rgb_to_atem_rejects_ragged_length
func TestRGBAToYCbCrARejectsRaggedLength(t *testing.T) {
	if _, err := RGBAToYCbCrA(make([]byte, 12), false); err == nil {
		t.Error("no error")
	}
}

// test_empty_inputs_return_empty
func TestEmptyInputsReturnEmpty(t *testing.T) {
	for name, f := range map[string]func([]byte) ([]byte, error){
		"RLEEncode":    RLEEncode,
		"YCbCrAToRGBA": YCbCrAToRGBA,
		"RGBAToYCbCrA": func(b []byte) ([]byte, error) { return RGBAToYCbCrA(b, false) },
	} {
		if got, err := f(nil); err != nil || got == nil || len(got) != 0 {
			t.Errorf("%s: %v, %v, want empty", name, got, err)
		}
	}
}

// test_rle_characterization_literal_then_run
func TestRLECharacterizationLiteralThenRun(t *testing.T) {
	// [W, X, X, X, X] -> W, X literal, then a run block for the remaining
	// three repeats: HEADER, count=3 (big-endian u64), X.
	w1 := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	x := bytes.Repeat([]byte{0x11}, 8)
	out, err := RLEEncode(append(append([]byte{}, w1...), bytes.Repeat(x, 4)...))
	if err != nil {
		t.Fatal(err)
	}
	want := append(append(append(append(append([]byte{}, w1...), x...), rleHeaderWord...),
		binary.BigEndian.AppendUint64(nil, 3)...), x...)
	if !bytes.Equal(out, want) {
		t.Errorf("% x\nwant % x", out, want)
	}
}

// test_rle_incompressible_passthrough
func TestRLEIncompressiblePassthrough(t *testing.T) {
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i)
	}
	if out, err := RLEEncode(data); err != nil || !bytes.Equal(out, data) {
		t.Errorf("% x, %v", out, err)
	}
}

// test_input_buffers_are_released has no counterpart: it checks that the
// C extension releases the Py_buffer it takes, which Go has no form of.

// The C file's constants are written as the doubles gcc folds them to,
// operation by operation; Go's exact constant arithmetic gives a different
// last bit for three of them. (gen_imaging.py's search found no input on
// which the difference changes a byte, but the port keeps gcc's values.)
func TestBT709ConstantsAreFoldedLikeGCC(t *testing.T) {
	// Variables, so the arithmetic is float64 at run time.
	one, r, g, b, half := 1.0, 0.2126, 0.7152, 0.0722, 112.0
	if (one-b)/half != bt709BiRange || (one-r)/half != bt709RiRange || b/g != bt709CoeffBG || r/g != bt709CoeffRG {
		t.Error("constants differ from the float64 operations")
	}
	exact := []float64{(1 - 0.0722) / 112, 0.0722 / 0.7152, 0.2126 / 0.7152}
	if exact[0] == bt709BiRange || exact[1] == bt709CoeffBG || exact[2] == bt709CoeffRG {
		t.Error("exact folding no longer differs; the comment is out of date")
	}
}

// The C's clamp holds its value in a signed short, so one above 32767
// comes back unclamped. No conversion reaches it; the port keeps it.
func TestCClampKeepsTheSignedShort(t *testing.T) {
	for _, c := range []struct{ v, want uint16 }{{10, 64}, {64, 64}, {500, 500}, {940, 940}, {941, 940}, {32767, 940}, {32768, 32768}, {65535, 65535}} {
		if got := cClamp(c.v, 64, 940); got != c.want {
			t.Errorf("cClamp(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}

// RLEDecode refuses a run that would take its output past 256 MiB without
// allocating it (atemwire would try, and raise MemoryError if it failed),
// and still expands a run of nothing, however long, to nothing.
func TestRLEDecodeBoundsTheOutput(t *testing.T) {
	run := func(count uint64, block []byte) []byte {
		return append(binary.BigEndian.AppendUint64(append([]byte{}, rleHeaderWord...), count), block...)
	}
	word := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for _, data := range [][]byte{
		run(1<<40, word),
		run(maxRLEOutput/8+1, word),
		append(append([]byte{}, word...), run(maxRLEOutput/8, word)...),
		run(maxRLEOutput/3+1, word[:3]),
	} {
		if out, err := RLEDecode(data); err == nil {
			t.Errorf("% x: %d bytes, want an error", data, len(out))
		}
	}
	if out, err := RLEDecode(run(1<<62, nil)); err != nil || out == nil || len(out) != 0 {
		t.Errorf("a long run of nothing: %d bytes, %v", len(out), err)
	}
}

// FrameImage is YCbCrAToRGBA wrapped in an image of the given size, which
// must match the frame exactly.
func TestFrameImage(t *testing.T) {
	frame := oracleNoise(99, 4*6*3)
	img, err := FrameImage(frame, 6, 3)
	if err != nil {
		t.Fatal(err)
	}
	rgba, _ := YCbCrAToRGBA(frame)
	if img.Bounds() != image.Rect(0, 0, 6, 3) || !bytes.Equal(img.Pix, rgba) {
		t.Errorf("image %v doesn't hold the decoded frame", img.Bounds())
	}
	for _, size := range [][2]int{{6, 2}, {5, 3}, {7, 3}, {-6, -3}, {0, 0}} {
		if _, err := FrameImage(frame, size[0], size[1]); err == nil {
			t.Errorf("%dx%d: no error", size[0], size[1])
		}
	}
	if img, err := FrameImage(nil, 0, 0); err != nil || len(img.Pix) != 0 {
		t.Errorf("empty frame: %v", err)
	}
}

// An *image.NRGBA is read as it is, wherever its rectangle starts and
// whatever its stride; any other image goes through color.NRGBAModel.
func TestFitFrameReadsAnyImage(t *testing.T) {
	src := &image.NRGBA{Pix: oraclePattern("noise", 30, 20, 7), Stride: 4 * 30, Rect: image.Rect(0, 0, 30, 20)}
	if got := FitFrame(src, 16, 9); len(got) != 16*9*4 {
		t.Errorf("%d bytes", len(got))
	}

	sub := src.SubImage(image.Rect(5, 4, 25, 16)).(*image.NRGBA)
	tight := image.NewNRGBA(image.Rect(0, 0, 20, 12))
	for y := range 12 {
		for x := range 20 {
			tight.SetNRGBA(x, y, sub.NRGBAAt(5+x, 4+y))
		}
	}
	if !bytes.Equal(FitFrame(sub, 16, 9), FitFrame(tight, 16, 9)) {
		t.Error("a sub-image fits differently from its copy")
	}

	premul := image.NewRGBA(src.Rect)
	for y := range 20 {
		for x := range 30 {
			premul.Set(x, y, src.At(x, y))
		}
	}
	viaModel := image.NewNRGBA(src.Rect)
	for y := range 20 {
		for x := range 30 {
			viaModel.SetNRGBA(x, y, color.NRGBAModel.Convert(premul.At(x, y)).(color.NRGBA))
		}
	}
	if !bytes.Equal(FitFrame(premul, 16, 9), FitFrame(viaModel, 16, 9)) {
		t.Error("an *image.RGBA doesn't fit as its NRGBA conversion")
	}
}

// An image with no pixels gives a transparent frame (Python raises
// ZeroDivisionError); a frame under 1x1 is a programming error.
func TestFitFrameEdges(t *testing.T) {
	got := FitFrame(image.NewNRGBA(image.Rect(3, 3, 3, 10)), 4, 2)
	if !bytes.Equal(got, make([]byte, 4*2*4)) {
		t.Errorf("empty source: % x", got)
	}
	for _, size := range [][2]int{{0, 1}, {1, 0}, {-1, 5}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%dx%d: no panic", size[0], size[1])
				}
			}()
			FitFrame(image.NewNRGBA(image.Rect(0, 0, 2, 2)), size[0], size[1])
		}()
	}
}
