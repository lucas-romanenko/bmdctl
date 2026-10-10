// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// testPNG is a small image with a transparent border.
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 36))
	for y := 4; y < 32; y++ {
		for x := 4; x < 60; x++ {
			img.Set(x, y, color.NRGBA{200, 30, 30, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Group e, end to end against the fake switcher: the media pool listed,
// stills uploaded, downloaded and cleared, capture and the players.
func TestMediaCommands(t *testing.T) {
	// NTSC: frames a sixth the size of 1080p keep the transfers quick.
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0
	b := newBenchDump(t, dump)
	dir := t.TempDir()
	t.Chdir(dir)
	logo := filepath.Join(dir, "logo.png")
	os.WriteFile(logo, testPNG(t), 0o644)

	out := b.must(with("media", "upload", "3", logo)...)
	if !strings.Contains(out, "confirmed") || !strings.Contains(out, `slot 3 "logo" from 64x36 png`) {
		t.Errorf("upload: %q", out)
	}
	out = b.must(with("media", "list")...)
	if !strings.Contains(out, "still 3   logo") || !strings.Contains(out, "still 4   -") || !strings.Contains(out, "player 1  still 1") {
		t.Errorf("list:\n%s", out)
	}
	// Replacing a used slot is destructive.
	if code, _, errOut := b.run(with("media", "upload", "3", logo, "--name", "Other")...); code != exitDestructive ||
		!strings.Contains(errOut, `still slot 3 holds "logo": add --yes`) {
		t.Errorf("replace without --yes: %d %q", code, errOut)
	}
	out = b.must(with("media", "upload", "3", logo, "--name", "Other", "--yes")...)
	if !strings.Contains(out, `replacing "logo"`) {
		t.Errorf("replace: %q", out)
	}
	// From stdin.
	if code, out, errOut := b.runIn(string(testPNG(t)), with("media", "upload", "5", "-")...); code != 0 || !strings.Contains(out, `"still"`) {
		t.Errorf("stdin: %d %q %q", code, out, errOut)
	}

	// Download: the frame comes back as a PNG of the video mode's size.
	got := filepath.Join(dir, "out", "got.png")
	out = b.must(with("media", "download", "3", got)...)
	if !strings.Contains(out, "to "+got+", 720x480") {
		t.Errorf("download: %q", out)
	}
	f, err := os.Open(got)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil || img.Bounds().Dx() != 720 || img.Bounds().Dy() != 480 {
		t.Fatalf("downloaded: %v %v", err, img.Bounds())
	}
	if r, _, _, a := img.At(360, 240).RGBA(); a < 0xF000 || r < 0xB000 {
		t.Errorf("centre pixel %v", img.At(360, 240))
	}
	// Without --file it is named after the slot, in the working directory.
	b.must(with("media", "download", "3")...)
	if _, err := os.Stat(filepath.Join(dir, "Other.png")); err != nil {
		t.Errorf("default name: %v", err)
	}
	// To stdout: the PNG and nothing else.
	_, out, _ = b.run(with("media", "download", "3", "-")...)
	if !strings.HasPrefix(out, "\x89PNG") {
		t.Errorf("stdout: %.20q", out)
	}
	if code, _, errOut := b.run(with("media", "download", "7")...); code != exitUsage || !strings.Contains(errOut, "still slot 7 is empty") {
		t.Errorf("empty slot: %d %q", code, errOut)
	}

	// Clearing is destructive; under the lock the switcher empties the
	// slot and says so.
	if code, _, _ := b.run(with("media", "clear", "3")...); code != exitDestructive {
		t.Errorf("clear without --yes: %d", code)
	}
	out = b.must(with("media", "clear", "3", "--yes")...)
	if !strings.Contains(out, "confirmed") || !strings.Contains(out, `was "Other"`) {
		t.Errorf("clear: %q", out)
	}
	if out := b.must(with("media", "list")...); !strings.Contains(out, "still 3   -") {
		t.Errorf("after clear:\n%s", out)
	}
	// The unlocked clear goes out bare; the fake, like some models,
	// ignores it.
	if code, _, _ := b.run(with("media", "clear-unlocked", "5", "--yes", "--timeout", "200ms")...); code != exitNoEcho {
		t.Errorf("clear-unlocked: %d", code)
	}
	if _, got := b.mustSend(t, "Capt", with("media", "capture")...); got != "" {
		t.Errorf("capture: %s", got)
	}
	// Player 2 and still 6, counted from 1: player 1 and still 5 on the wire.
	if _, got := b.mustSend(t, "MPSS", with("media", "player", "2", "--still", "6")...); got != "0301010500000000" {
		t.Errorf("player: %s", got)
	}
	if code, _, errOut := b.run(with("media", "upload", "3", filepath.Join(dir, "Other.png"), "--yes", "--slot", "4")...); code != exitUsage || !strings.Contains(errOut, "slot given twice") {
		t.Errorf("twice: %d %q", code, errOut)
	}
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("not an image"), 0o644)
	if code, _, errOut := b.run(with("media", "upload", "6", filepath.Join(dir, "x.txt"))...); code != exitUsage || !strings.Contains(errOut, "not a PNG, JPEG or GIF") {
		t.Errorf("not an image: %d %q", code, errOut)
	}
}
