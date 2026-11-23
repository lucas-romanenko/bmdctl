// SPDX-License-Identifier: MIT

package ultimatte

// The media pool upload without a session: bmdwire v1.14.0's
// test_media.py, the tests of media.py's helpers and of upload_image
// itself; the session's are in session_test.go.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testPNG is fake_captured_4k.png: a PNG header the unit (and PNGSize)
// can read the size from.
func testPNG(w, h int) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	b := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	b = append(append(b, "IHDR"...), ihdr...)
	return append(b, "\x00\x00\x00\x00\x00\x00\x00\x00IEND\xaeB`\x82"...)
}

// testJPG is fake_captured_4k.jpg: JFIF APP0, an optional padding APP1,
// SOF0, an end.
func testJPG(w, h, appBytes int) []byte {
	b := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16}
	b = append(b, "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"...)
	if appBytes > 0 {
		b = append(b, 0xff, 0xe1)
		b = binary.BigEndian.AppendUint16(b, uint16(appBytes+2))
		b = append(b, make([]byte, appBytes)...)
	}
	b = append(b, 0xff, 0xc0, 0, 17, 8)
	b = binary.BigEndian.AppendUint16(b, uint16(h))
	b = binary.BigEndian.AppendUint16(b, uint16(w))
	b = append(b, 3, 0x01, 0x22, 0, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01)
	return append(b, 0xff, 0xd9)
}

func wantImageError(t *testing.T, err error, sub string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Class != "UltimatteImageError" || !errors.Is(err, ErrImage) || !strings.Contains(e.Msg, sub) {
		t.Errorf("%v, want an UltimatteImageError with %q", err, sub)
	}
}

// test_png_size_and_format_size
func TestPNGSizeAndFormatSize(t *testing.T) {
	if w, h, err := PNGSize(testPNG(1920, 1080)); err != nil || w != 1920 || h != 1080 {
		t.Error(w, h, err)
	}
	_, _, err := PNGSize(append([]byte("GIF89a"), make([]byte, 30)...))
	wantImageError(t, err, "not a PNG")
	for f, want := range map[string][2]int{"1080p25": {1920, 1080}, "2160p50": {3840, 2160}, "1080i59.94": {1920, 1080}, "auto detect": {}} {
		w, h, ok := FormatSize(f)
		if [2]int{w, h} != want || ok != (want != [2]int{}) {
			t.Errorf("%s: %d %d %v", f, w, h, ok)
		}
	}
}

// test_bad_names_are_refused
func TestBadImageNamesAreRefused(t *testing.T) {
	for _, n := range []string{"", "a/b.png", "a\\b.png", "a\nb.png"} {
		_, err := CheckImageName(n)
		wantImageError(t, err, "bad image name")
	}
}

// test_upload_frame_is_the_captured_one
func TestUploadFrameIsTheCapturedOne(t *testing.T) {
	data := testPNG(1920, 1080)
	s := newScript([]byte{0})
	c, d := client(s)
	if w, h, err := c.UploadImage(ctx, host, "Studio.png", bytes.NewReader(data), -1, "", nil); err != nil || w != 1920 || h != 1080 {
		t.Fatal(w, h, err)
	}
	if !bytes.Equal(s.written(), expectedFrame(0x0101, "Studio.png", data)) || d.addrs[0] != "192.0.2.21:9996" {
		t.Fatalf("%x to %v", s.written(), d.addrs)
	}
}

// test_wrong_size_is_refused_before_sending, and its JPG twin
func TestWrongSizeIsRefusedBeforeSending(t *testing.T) {
	for name, data := range map[string][]byte{"Big.png": testPNG(3840, 2160), "Big.jpg": testJPG(3840, 2160, 0)} {
		c, d := client()
		_, _, err := c.UploadImage(ctx, host, name, bytes.NewReader(data), -1, "1080p25", nil)
		wantImageError(t, err, "3840x2160")
		if len(d.addrs) != 0 {
			t.Errorf("%s: dialled", name)
		}
	}
}

// test_jpeg_size_from_the_frame_header
func TestJPEGSizeFromTheFrameHeader(t *testing.T) {
	if w, h, err := JPEGSize(testJPG(1920, 1080, 0)); err != nil || w != 1920 || h != 1080 {
		t.Error(w, h, err)
	}
	if k, w, h, err := ImageSize(testJPG(3840, 2160, 0)); err != nil || k != JPEG || w != 3840 || h != 2160 {
		t.Error(k, w, h, err)
	}
	if k, w, h, err := ImageSize(testPNG(1920, 1080)); err != nil || k != PNG || w != 1920 || h != 1080 {
		t.Error(k, w, h, err)
	}
	// an EXIF block in front of the SOF is walked over
	if w, h, err := JPEGSize(testJPG(1280, 720, 60000)); err != nil || w != 1280 || h != 720 {
		t.Error(w, h, err)
	}
	_, _, err := JPEGSize(testJPG(1920, 1080, 0)[:12]) // ends before the SOF
	wantImageError(t, err, "ends before")
	_, _, _, err = ImageSize(append([]byte("GIF89a"), make([]byte, 30)...))
	wantImageError(t, err, "PNG or JPG")
}

// test_jpg_frame_is_the_captured_one: 0101 00000011 "jpg-test-1080.jpg"
// 0000855e <JPEG> 00
func TestJPGFrameIsTheCapturedOne(t *testing.T) {
	data := testJPG(1920, 1080, 0)
	data = append(data, make([]byte, 34142-len(data))...)
	s := newScript([]byte{0})
	c, _ := client(s)
	if _, _, err := c.UploadImage(ctx, host, "jpg-test-1080.jpg", bytes.NewReader(data), -1, "1080p25", nil); err != nil {
		t.Fatal(err)
	}
	head := []byte("\x01\x01\x00\x00\x00\x11jpg-test-1080.jpg\x00\x00\x85\x5e\xff\xd8\xff\xe0")
	if !bytes.HasPrefix(s.written(), head) || !bytes.Equal(s.written(), expectedFrame(0x0101, "jpg-test-1080.jpg", data)) {
		t.Fatalf("%x", s.written()[:40])
	}
}

// pieces is a reader that gives at most n bytes a read, as an iterable
// of chunks does.
type pieces struct {
	r io.Reader
	n int
}

func (p pieces) Read(b []byte) (int, error) { return p.r.Read(b[:min(len(b), p.n)]) }

// test_upload_streams_from_a_path_a_file_and_chunks
func TestUploadStreamsFromAFileAndChunks(t *testing.T) {
	data := testJPG(1920, 1080, 50000)
	path := filepath.Join(t.TempDir(), "Studio.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var calls []int64
	s := newScript([]byte{0})
	c, _ := client(s)
	if _, _, err := c.UploadImage(ctx, host, "A.jpg", f, -1, "", func(sent, size int64) error {
		calls = append(calls, sent)
		if size != int64(len(data)) {
			t.Errorf("size %d", size)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls[len(calls)-1] != int64(len(data)) || !slices.IsSorted(calls) || !bytes.Equal(s.written(), expectedFrame(0x0101, "A.jpg", data)) {
		t.Errorf("%v", calls)
	}
	s = newScript([]byte{0})
	c, _ = client(s)
	if _, _, err := c.UploadImage(ctx, host, "C.jpg", pieces{bytes.NewReader(data), 1000}, int64(len(data)), "", nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.written(), expectedFrame(0x0101, "C.jpg", data)) {
		t.Error("chunks")
	}
}

// test_streams_that_cannot_say_their_length
func TestStreamsThatCannotSayTheirLength(t *testing.T) {
	data := testJPG(1920, 1080, 0)
	c, _ := client()
	_, _, err := c.UploadImage(ctx, host, "A.jpg", pieces{bytes.NewReader(data), 1 << 20}, -1, "", nil)
	wantImageError(t, err, "size=")
	for _, cs := range []struct {
		r    io.Reader
		size int
		msg  string
	}{
		{bytes.NewReader(data), len(data) + 10, "ended after"},
		{io.MultiReader(bytes.NewReader(data), strings.NewReader("x")), len(data), "longer"},
	} {
		s := newScript([]byte{0})
		c, _ := client(s)
		_, _, err := c.UploadImage(ctx, host, "B.jpg", pieces{cs.r, 1 << 20}, int64(cs.size), "", nil)
		wantImageError(t, err, cs.msg)
		if w := s.written(); w[len(w)-1] == 0 {
			t.Error("trailing zero sent")
		}
	}
}

// test_a_progress_callback_can_abandon_the_upload
func TestAProgressCallbackCanAbandonTheUpload(t *testing.T) {
	stop := errors.New("stop")
	s := newScript([]byte{0})
	c, _ := client(s)
	_, _, err := c.UploadImage(ctx, host, "A.png", bytes.NewReader(testPNG(1920, 1080)), -1, "", func(int64, int64) error { return stop })
	if err != stop {
		t.Fatal(err)
	}
}
