// SPDX-License-Identifier: MIT

package ultimatte

// ultimattewire's media.py (bmdwire v1.14.0, bmdwire#78): loading a still
// into an Ultimatte's media pool. Not in the text protocol; captures of
// Software Control on a 12 4K (software 2.3, 2026-11-11 and -12) show it
// is the ordinary 9996 write frame with opcode 0x0101, the file as it is
// (a PNG or a JPG; the frame doesn't say which), on a fresh connection,
// answered with one zero byte. The 9998 session then gets an IMAGE LIST
// push with the name. The unit takes any size into the pool but puts on a
// frame buffer only an image of the running format's size, so
// UploadImage can check that first. Removing one is on 9998
// (Session.DeleteImage).

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// OpWriteImage is the media pool write's opcode.
const OpWriteImage = 0x0101

// The image kinds ImageSize tells apart.
const (
	PNG  = "png"
	JPEG = "jpeg"
)

// HeaderLimit is how far into a file its frame size is looked for: a
// JPEG's SOF comes after its APPn segments (EXIF, a thumbnail, ICC), each
// at most 64 KiB.
const HeaderLimit = 1 << 20

var (
	pngSignature  = []byte("\x89PNG\r\n\x1a\n")
	jpegSignature = []byte{0xff, 0xd8, 0xff}
)

// ErrImage is UltimatteImageError's kind: the image can't go to this
// unit (not a PNG or JPG, the wrong size, a bad name, a stream of unknown
// length), or the unit refused the upload.
var ErrImage = errors.New("ultimatte: image refused")

func imageError(format string, args ...any) *Error {
	return &Error{Class: "UltimatteImageError", Msg: fmt.Sprintf(format, args...), kind: ErrImage}
}

// PNGSize is png_size: the width and height in a PNG's IHDR.
func PNGSize(data []byte) (width, height int, err error) {
	if len(data) < 24 || !bytes.HasPrefix(data, pngSignature) || string(data[12:16]) != "IHDR" {
		return 0, 0, imageError("not a PNG file")
	}
	return int(binary.BigEndian.Uint32(data[16:])), int(binary.BigEndian.Uint32(data[20:])), nil
}

// JPEGSize is jpeg_size: the width and height in a JPEG's frame header
// (SOF), looked for in its first HeaderLimit bytes.
func JPEGSize(data []byte) (width, height int, err error) {
	w, h, found, err := jpegScan(data[:min(len(data), HeaderLimit)])
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, 0, imageError("JPEG ends before its frame header (SOF)")
	}
	return w, h, nil
}

// ImageSize is image_size: what an image's first bytes are (PNG or JPEG)
// and its size; anything else is refused, since only those two were seen
// going into the pool.
func ImageSize(data []byte) (kind string, width, height int, err error) {
	kind, width, height, found, err := probe(data[:min(len(data), HeaderLimit)])
	if err == nil && !found {
		err = imageError("the image ends before its size")
	}
	return kind, width, height, err
}

// probe is _probe: found false when more bytes are needed.
func probe(head []byte) (kind string, width, height int, found bool, err error) {
	if bytes.HasPrefix(pngSignature, head) && len(head) < 24 {
		return "", 0, 0, false, nil
	}
	if bytes.HasPrefix(head, pngSignature) {
		w, h, err := PNGSize(head)
		return PNG, w, h, err == nil, err
	}
	if bytes.HasPrefix(jpegSignature, head) && len(head) < 3 {
		return "", 0, 0, false, nil
	}
	if bytes.HasPrefix(head, jpegSignature) {
		w, h, found, err := jpegScan(head)
		return JPEG, w, h, found, err
	}
	return "", 0, 0, false, imageError("not a PNG or JPG file (the only formats seen going into the media pool)")
}

// isSOF: the start-of-frame markers, which carry the size: C0-CF less
// DHT (C4), JPG (C8) and DAC (CC).
func isSOF(m byte) bool { return m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc }

// jpegScan is _jpeg_scan: a JPEG's marker segments walked to its SOF;
// found false if data ends first.
func jpegScan(data []byte) (width, height int, found bool, err error) {
	if !bytes.HasPrefix(data, jpegSignature) {
		return 0, 0, false, imageError("not a JPEG file")
	}
	i, n := 2, len(data)
	for {
		if i >= n {
			return 0, 0, false, nil
		}
		if data[i] != 0xff {
			return 0, 0, false, imageError("broken JPEG: no marker at byte %d", i)
		}
		for i < n && data[i] == 0xff { // fill bytes
			i++
		}
		if i >= n {
			return 0, 0, false, nil
		}
		marker := data[i]
		i++
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 { // no length
			continue
		}
		if marker == 0xd9 || marker == 0xda {
			return 0, 0, false, imageError("broken JPEG: image data before a frame header")
		}
		if i+2 > n {
			return 0, 0, false, nil
		}
		length := int(binary.BigEndian.Uint16(data[i:]))
		if length < 2 {
			return 0, 0, false, imageError("broken JPEG: segment length %d", length)
		}
		if isSOF(marker) {
			if i+7 > n {
				return 0, 0, false, nil
			}
			return int(binary.BigEndian.Uint16(data[i+5:])), int(binary.BigEndian.Uint16(data[i+3:])), true, nil
		}
		i += length
	}
}

var formatLines = regexp.MustCompile(`(?i)^\s*(\d+)[pi]`)

var formatSizes = map[int][2]int{720: {1280, 720}, 1080: {1920, 1080}, 2160: {3840, 2160}, 4320: {7680, 4320}}

// FormatSize is format_size: the frame size of a video format name
// (1080p25 is 1920x1080) by its line count; ok false for a name it can't
// read (auto detect, empty).
func FormatSize(videoFormat string) (width, height int, ok bool) {
	m := formatLines.FindStringSubmatch(videoFormat)
	if m == nil {
		return 0, 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	s, ok := formatSizes[n]
	return s[0], s[1], ok
}

// CheckImageName is check_name: the name stripped, refused when empty or
// holding a slash, a backslash or a line break.
func CheckImageName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || strings.ContainsAny(n, "/\\\n\r") {
		return "", imageError("bad image name %s", pyRepr(n))
	}
	return n, nil
}

// UploadImage is media.upload_image: a PNG or JPG sent to the unit's
// media pool as name, returning its width and height. r is read from
// where it stands and sent in StreamChunk pieces as it is read; size is
// the bytes to send, or -1 for an *os.File or an io.Seeker to say itself
// (any other reader needs it). Only the first chunks, read for the
// image's size, are held in memory. progress, when not nil, is called
// after every chunk; an error from it abandons the upload and is
// returned as it is.
//
// With videoFormat (the DEVICE block's running format) an image the unit
// couldn't put on a frame buffer is refused before anything is sent. A
// failure of the write is an *Error of kind ErrImage naming the host and
// the image.
func (c *Client) UploadImage(ctx context.Context, host, name string, r io.Reader, size int64, videoFormat string, progress func(sent, size int64) error) (width, height int, err error) {
	name, err = CheckImageName(name)
	if err != nil {
		return 0, 0, err
	}
	if size < 0 {
		if size, err = remaining(r); err != nil {
			return 0, 0, err
		}
	}
	head, _, width, height, err := peek(r)
	if err != nil {
		return 0, 0, err
	}
	if videoFormat != "" {
		if w, h, ok := FormatSize(videoFormat); ok && (w != width || h != height) {
			return 0, 0, imageError("%s is %dx%d; the unit runs %s (%dx%d) and won't assign a different size",
				name, width, height, videoFormat, w, h)
		}
	}
	var abandoned error
	wrapped := progress
	if progress != nil {
		wrapped = func(sent, size int64) error {
			abandoned = progress(sent, size)
			return abandoned
		}
	}
	err = c.UploadStream(ctx, host, OpWriteImage, name, size, io.MultiReader(bytes.NewReader(head), r), wrapped)
	switch {
	case err == nil:
		return width, height, nil
	case abandoned != nil && errors.Is(err, abandoned), ctx.Err() != nil:
		return 0, 0, err
	}
	return 0, 0, &Error{Class: "UltimatteImageError", Msg: fmt.Sprintf("%s: upload of %s failed: %v", host, name, err), kind: ErrImage}
}

// remaining is _remaining: the bytes left in a file from where it stands.
func remaining(r io.Reader) (int64, error) {
	if f, ok := r.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
			if here, err := f.Seek(0, io.SeekCurrent); err == nil {
				return st.Size() - here, nil
			}
		}
	}
	if s, ok := r.(io.Seeker); ok {
		if here, err := s.Seek(0, io.SeekCurrent); err == nil {
			if end, err := s.Seek(0, io.SeekEnd); err == nil {
				if _, err := s.Seek(here, io.SeekStart); err == nil {
					return end - here, nil
				}
			}
		}
	}
	return 0, imageError("this file can't tell its length: give size= (the frame carries it first)")
}

// peek is _peek: chunks read until the image's kind and size are known;
// head is every byte read, to be sent first.
func peek(r io.Reader) (head []byte, kind string, width, height int, err error) {
	buf := make([]byte, StreamChunk)
	for {
		if len(head) > 0 {
			kind, w, h, found, err := probe(head[:min(len(head), HeaderLimit)])
			if err != nil {
				return nil, "", 0, 0, err
			}
			if found {
				return head, kind, w, h, nil
			}
		}
		if len(head) >= HeaderLimit {
			return nil, "", 0, 0, imageError("no image size in the first %d bytes", HeaderLimit)
		}
		k, rerr := r.Read(buf)
		head = append(head, buf[:k]...)
		if k > 0 {
			continue
		}
		if rerr == io.EOF {
			if len(head) > 0 {
				return nil, "", 0, 0, imageError("the image ends before its size")
			}
			return nil, "", 0, 0, imageError("the image is empty")
		}
		if rerr != nil {
			return nil, "", 0, 0, rerr
		}
	}
}
