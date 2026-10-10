// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"image"
	"image/color"
	"math"
)

// FitFrame scales img to fit a width x height frame, keeping its aspect
// ratio, centres it on a transparent canvas and returns the canvas as
// straight RGBA, width*height*4 bytes: what RGBAToYCbCrA takes. It
// reproduces WebATEM's frame preparation for still uploads byte for byte
// (atem-web-control v2.0.2, atem_control/uploader.py, _prepare_frame), from
// the image's pixels on:
//
//   - The scale is the source height over the frame height if the source
//     is narrower than the frame, else the source width over the frame
//     width, and the scaled size is (max(1, int(w/scale)), max(1,
//     int(h/scale))), in float64 as Python computes it.
//   - The image is resized with Pillow's LANCZOS filter. Pillow resizes an
//     RGBA image premultiplied (RGBA to RGBa, resample, back to RGBA) and
//     returns an unchanged copy when the size doesn't change; so does this.
//   - The result is pasted at ((width-w)//2, (height-h)//2) onto a (0, 0,
//     0, 0) canvas with itself as the mask. Pillow blends every channel
//     against the canvas by the mask's alpha, alpha included, so a pixel
//     with alpha a lands as each channel times a/255, rounded: colour comes
//     out premultiplied and alpha as a*a/255. That is what WebATEM uploads,
//     and it is reproduced, not corrected.
//
// Decoding the file is the caller's job and is not mirrored: WebATEM opens
// it with Pillow and converts it to RGBA. Here an *image.NRGBA is used as
// it is, and any other image goes through color.NRGBAModel, which can
// round differently from Pillow's conversions. WebATEM premultiplies in
// RGBAToYCbCrA for files whose name ends in .png; that choice is the
// caller's too.
//
// An image with no pixels gives an empty (all-zero) frame; Python raises
// ZeroDivisionError there. FitFrame panics if width or height is under 1.
//
// The resampling, the RGBA/RGBa conversions and the paste are ports of
// Pillow 10.2.0's algorithms (libImaging Resample.c, Convert.c, Paste.c;
// Pillow is under the HPND licence, also known as MIT-CMU). The filter's
// sin is Go's math.Sin rather than the C library's; the two can differ in
// the last bit, which could move a 22-bit fixed-point weight by one only
// when it sits within about 1e-9 of a rounding boundary. (Against glibc,
// every weight of 7,214 size pairs, 1 to 6,000 pixels into 1 to 1,920,
// came out the same.)
func FitFrame(img image.Image, width, height int) []byte {
	if width < 1 || height < 1 {
		panic("atem: FitFrame needs a frame of at least 1x1 pixels")
	}
	src, sw, sh := nrgbaPixels(img)
	canvas := make([]byte, width*height*4)
	if sw == 0 || sh == 0 {
		return canvas
	}
	nw, nh := fitSize(sw, sh, width, height)
	scaled := pillowResize(src, sw, sh, nw, nh)
	pillowPasteMatte(canvas, width, height, scaled, nw, nh, floorDiv(width-nw, 2), floorDiv(height-nh, 2))
	return canvas
}

// fitSize is _prepare_frame's scaled size.
func fitSize(sw, sh, width, height int) (int, int) {
	targetAspect := float64(width) / float64(height)
	srcAspect := float64(sw) / float64(sh)
	var scale float64
	if srcAspect < targetAspect {
		scale = float64(sh) / float64(height)
	} else {
		scale = float64(sw) / float64(width)
	}
	return max(1, int(float64(sw)/scale)), max(1, int(float64(sh)/scale))
}

// floorDiv is Python's //.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// nrgbaPixels returns img's pixels as tightly packed straight RGBA.
func nrgbaPixels(img image.Image) (pix []byte, w, h int) {
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	pix = make([]byte, w*h*4)
	if m, ok := img.(*image.NRGBA); ok {
		for y := range h {
			i := m.PixOffset(b.Min.X, b.Min.Y+y)
			copy(pix[y*w*4:(y+1)*w*4], m.Pix[i:i+w*4])
		}
		return pix, w, h
	}
	for y := range h {
		for x := range w {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
	return pix, w, h
}

// pillowResize is Image.resize((nw, nh), LANCZOS) for an RGBA image: a copy
// when the size is unchanged, else premultiply, resample, unpremultiply.
func pillowResize(src []byte, sw, sh, nw, nh int) []byte {
	if nw == sw && nh == sh {
		return append([]byte(nil), src...)
	}
	pre := make([]byte, len(src))
	for i := 0; i < len(src); i += 4 { // Convert.c rgbA2rgba
		a := uint32(src[i+3])
		pre[i] = byte(mulDiv255(uint32(src[i]), a))
		pre[i+1] = byte(mulDiv255(uint32(src[i+1]), a))
		pre[i+2] = byte(mulDiv255(uint32(src[i+2]), a))
		pre[i+3] = src[i+3]
	}
	out := resampleLanczos(pre, sw, sh, nw, nh)
	for i := 0; i < len(out); i += 4 { // Convert.c rgba2rgbA
		a := uint32(out[i+3])
		if a == 255 || a == 0 {
			continue
		}
		out[i] = clip8u(255 * uint32(out[i]) / a)
		out[i+1] = clip8u(255 * uint32(out[i+1]) / a)
		out[i+2] = clip8u(255 * uint32(out[i+2]) / a)
	}
	return out
}

// mulDiv255 is ImagingUtils.h's MULDIV255: a*b/255, rounded.
func mulDiv255(a, b uint32) uint32 {
	t := a*b + 128
	return (t>>8 + t) >> 8
}

// div255 is ImagingUtils.h's DIV255.
func div255(a uint32) uint32 {
	t := a + 128
	return (t>>8 + t) >> 8
}

// clip8u is ImagingUtils.h's CLIP8 for an unsigned value.
func clip8u(v uint32) byte {
	if v < 256 {
		return byte(v)
	}
	return 255
}

// lanczosSupport is the LANCZOS filter's support.
const lanczosSupport = 3.0

// precisionBits is Resample.c's PRECISION_BITS for 8-bit images; a sum
// starts at half, so clip8's shift rounds it.
const (
	precisionBits = 32 - 8 - 2
	precisionHalf = 1 << (precisionBits - 1)
)

func sincFilter(x float64) float64 {
	if x == 0.0 {
		return 1.0
	}
	x = x * math.Pi
	return math.Sin(x) / x
}

func lanczosFilter(x float64) float64 {
	// truncated sinc
	if -3.0 <= x && x < 3.0 {
		return float64(sincFilter(x) * sincFilter(x/3))
	}
	return 0.0
}

// precomputeCoeffs is Resample.c's precompute_coeffs for the LANCZOS
// filter, followed by normalize_coeffs_8bpc. in0 and in1 are the box edges,
// floats in Pillow.
func precomputeCoeffs(inSize int, in0, in1 float32, outSize int) (ksize int, bounds []int, kk []int32) {
	scale := float64(in1-in0) / float64(outSize)
	filterscale := scale
	if filterscale < 1.0 {
		filterscale = 1.0
	}
	support := lanczosSupport * filterscale
	ksize = int(math.Ceil(support))*2 + 1

	pre := make([]float64, outSize*ksize)
	bounds = make([]int, outSize*2)
	for xx := range outSize {
		center := float64(in0) + float64((float64(xx)+0.5)*scale)
		ww := 0.0
		ss := 1.0 / filterscale
		// Round the value
		xmin := int(center - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		// Round the value
		xmax := int(center + support + 0.5)
		if xmax > inSize {
			xmax = inSize
		}
		xmax -= xmin
		k := pre[xx*ksize : (xx+1)*ksize]
		for x := range xmax {
			w := lanczosFilter((float64(x+xmin) - center + 0.5) * ss)
			k[x] = w
			ww += w
		}
		for x := range xmax {
			if ww != 0.0 {
				k[x] /= ww
			}
		}
		bounds[xx*2+0] = xmin
		bounds[xx*2+1] = xmax
	}

	kk = make([]int32, len(pre))
	for i, v := range pre {
		if v < 0 {
			kk[i] = int32(-0.5 + float64(v*(1<<precisionBits)))
		} else {
			kk[i] = int32(0.5 + float64(v*(1<<precisionBits)))
		}
	}
	return ksize, bounds, kk
}

// clip8 is Resample.c's clip8: the 8-bit value of a fixed-point sum.
func clip8(in int32) byte {
	v := in >> precisionBits
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// resampleLanczos is Resample.c's ImagingResampleInner with the whole
// image as the box, for a 4-band 8-bit image: a horizontal pass over the
// rows the vertical pass will use, then the vertical pass, each skipped
// when its size doesn't change.
func resampleLanczos(src []byte, sw, sh, xsize, ysize int) []byte {
	needHorizontal := xsize != sw
	needVertical := ysize != sh

	ksizeH, boundsH, kkH := precomputeCoeffs(sw, 0, float32(sw), xsize)
	ksizeV, boundsV, kkV := precomputeCoeffs(sh, 0, float32(sh), ysize)

	// First and last used rows in the source image
	yboxFirst := boundsV[0]
	yboxLast := boundsV[ysize*2-2] + boundsV[ysize*2-1]

	in, inW := src, sw
	if needHorizontal {
		// Shift bounds for vertical pass
		for i := range ysize {
			boundsV[i*2] -= yboxFirst
		}
		in = resampleHorizontal(in, inW, yboxFirst, yboxLast-yboxFirst, xsize, ksizeH, boundsH, kkH)
		inW = xsize
	}
	if needVertical {
		return resampleVertical(in, inW, ysize, ksizeV, boundsV, kkV)
	}
	if !needHorizontal {
		return append([]byte(nil), in...)
	}
	return in
}

// resampleHorizontal is ImagingResampleHorizontal_8bpc for 4 bands: rows
// offset..offset+rows-1 of in (inW pixels wide) resampled to outW pixels.
func resampleHorizontal(in []byte, inW, offset, rows, outW, ksize int, bounds []int, kk []int32) []byte {
	out := make([]byte, outW*rows*4)
	for yy := range rows {
		row := in[(yy+offset)*inW*4 : (yy+offset+1)*inW*4]
		for xx := range outW {
			xmin := bounds[xx*2+0]
			xmax := bounds[xx*2+1]
			k := kk[xx*ksize : xx*ksize+xmax]
			ss0, ss1, ss2, ss3 := int32(precisionHalf), int32(precisionHalf), int32(precisionHalf), int32(precisionHalf)
			for x, w := range k {
				p := row[(x+xmin)*4 : (x+xmin)*4+4]
				ss0 += int32(p[0]) * w
				ss1 += int32(p[1]) * w
				ss2 += int32(p[2]) * w
				ss3 += int32(p[3]) * w
			}
			o := out[(yy*outW+xx)*4 : (yy*outW+xx)*4+4]
			o[0], o[1], o[2], o[3] = clip8(ss0), clip8(ss1), clip8(ss2), clip8(ss3)
		}
	}
	return out
}

// resampleVertical is ImagingResampleVertical_8bpc for 4 bands: in (w
// pixels wide) resampled to outH rows.
func resampleVertical(in []byte, w, outH, ksize int, bounds []int, kk []int32) []byte {
	out := make([]byte, w*outH*4)
	for yy := range outH {
		ymin := bounds[yy*2+0]
		ymax := bounds[yy*2+1]
		k := kk[yy*ksize : yy*ksize+ymax]
		for xx := range w {
			ss0, ss1, ss2, ss3 := int32(precisionHalf), int32(precisionHalf), int32(precisionHalf), int32(precisionHalf)
			for y, wt := range k {
				p := in[((y+ymin)*w+xx)*4 : ((y+ymin)*w+xx)*4+4]
				ss0 += int32(p[0]) * wt
				ss1 += int32(p[1]) * wt
				ss2 += int32(p[2]) * wt
				ss3 += int32(p[3]) * wt
			}
			o := out[(yy*w+xx)*4 : (yy*w+xx)*4+4]
			o[0], o[1], o[2], o[3] = clip8(ss0), clip8(ss1), clip8(ss2), clip8(ss3)
		}
	}
	return out
}

// pillowPasteMatte is Image.paste(im, (dx, dy), im) for RGBA onto RGBA:
// Paste.c's ImagingPaste clipping and paste_mask_RGBA, which blends all
// four channels by the mask's alpha.
func pillowPasteMatte(out []byte, outW, outH int, im []byte, imW, imH, dx, dy int) {
	xsize, ysize := imW, imH
	sx, sy := 0, 0
	if dx < 0 {
		xsize += dx
		sx = -dx
		dx = 0
	}
	if dx+xsize > outW {
		xsize = outW - dx
	}
	if dy < 0 {
		ysize += dy
		sy = -dy
		dy = 0
	}
	if dy+ysize > outH {
		ysize = outH - dy
	}
	if xsize <= 0 || ysize <= 0 {
		return
	}
	for y := range ysize {
		o := out[((y+dy)*outW+dx)*4:]
		in := im[((y+sy)*imW+sx)*4:]
		for x := range xsize {
			a := uint32(in[x*4+3])
			for c := range 4 { // BLEND(a, out, in)
				o[x*4+c] = byte(div255(uint32(o[x*4+c])*(255-a) + uint32(in[x*4+c])*a))
			}
		}
	}
}
