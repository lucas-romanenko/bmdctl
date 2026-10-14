// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
	"github.com/lucas-romanenko/bmdctl/internal/stillcache"
)

// solidPNG is a picture of one colour.
func solidPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A profile save takes each still from the still cache when it has it,
// downloads each picture once when it hasn't, and names every file as
// the XML does: the same picture under two names in two files, two
// pictures under one name as name and name_2. A restore of it puts each
// picture back under its own slot's name.
func TestProfileSaveStillCache(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	dump[bytes.Index(dump, []byte("VidM"))+4] = 0 // NTSC: small stills
	cacheDir := filepath.Join(t.TempDir(), "stills")
	b := newBenchConfig(t, dump, engine.Config{Stills: stillcache.New(cacheDir, 1<<30, 0)})
	t.Chdir(t.TempDir())
	os.WriteFile("logo.png", testPNG(t), 0o644)
	os.WriteFile("blue.png", solidPNG(t, color.NRGBA{20, 40, 200, 255}), 0o644)
	os.WriteFile("green.png", solidPNG(t, color.NRGBA{20, 200, 40, 255}), 0o644)
	b.must(with("media", "clear", "1", "--yes")...) // the dump's still has no frame in the fake
	b.must(with("media", "upload", "4", "logo.png", "--name", "Logo")...)
	b.must(with("media", "upload", "5", "logo.png", "--name", "Logo copy")...)
	b.must(with("media", "upload", "6", "blue.png", "--name", "Background")...)
	b.must(with("media", "upload", "7", "green.png", "--name", "Background")...)
	sw := b.switchers()[0]
	blue, _, _, _ := sw.Still(5)
	green, _, _, _ := sw.Still(6)
	// Still downloads: FTSU for store 0 (macros come by FTSU too).
	stillDownloads := func() int {
		n := 0
		for _, c := range b.sent("FTSU") {
			if len(c) >= 12 && c[10] == 0 && c[11] == 0 {
				n++
			}
		}
		return n
	}

	save := func(file string) engine.ProfileResult {
		t.Helper()
		var r struct {
			Result engine.ProfileResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(b.must(with("profile", "save", file, "--json")...)), &r); err != nil {
			t.Fatal(err)
		}
		return r.Result
	}

	before := stillDownloads()
	first := save("one/show.xml")
	if got := stillDownloads() - before; got != 3 {
		t.Errorf("first save: %d downloads, want 3 (one per picture)", got)
	}
	cached := map[string]bool{}
	for _, im := range first.Images {
		cached[im.Name+" "+im.Path] = im.FromCache
	}
	want := map[string]bool{
		"Logo ATEM Media Pool/Logo.png":               false,
		"Logo copy ATEM Media Pool/Logo copy.png":     true, // the same picture: not downloaded again
		"Background ATEM Media Pool/Background.png":   false,
		"Background ATEM Media Pool/Background_2.png": false,
	}
	if len(cached) != len(want) {
		t.Errorf("images: %v", first.Images)
	}
	for k, v := range want {
		if got, ok := cached[k]; !ok || got != v {
			t.Errorf("%s: from cache %v (present %v), want %v", k, got, ok, v)
		}
	}
	xml, _ := os.ReadFile("one/show.xml")
	for _, s := range []string{
		`<Still index="3" name="Logo" path="ATEM Media Pool/Logo.png"/>`,
		`<Still index="4" name="Logo copy" path="ATEM Media Pool/Logo copy.png"/>`,
		`<Still index="5" name="Background" path="ATEM Media Pool/Background.png"/>`,
		`<Still index="6" name="Background" path="ATEM Media Pool/Background_2.png"/>`,
	} {
		if !bytes.Contains(xml, []byte(s)) {
			t.Errorf("XML lacks %s", s)
		}
	}
	files, _ := filepath.Glob(filepath.Join("one", "ATEM Media Pool", "*.png"))
	if len(files) != 4 {
		t.Errorf("files: %v", files)
	}
	bg, _ := os.ReadFile(filepath.Join("one", "ATEM Media Pool", "Background.png"))
	bg2, _ := os.ReadFile(filepath.Join("one", "ATEM Media Pool", "Background_2.png"))
	if len(bg) == 0 || bytes.Equal(bg, bg2) {
		t.Error("the two Backgrounds are one picture")
	}

	// Again: every still from the cache, no transfer at all.
	before = stillDownloads()
	second := save("two/show.xml")
	if got := stillDownloads() - before; got != 0 {
		t.Errorf("second save: %d downloads, want none", got)
	}
	for _, im := range second.Images {
		if !im.FromCache || im.Error != "" {
			t.Errorf("second save, %s: from cache %v, error %q", im.Name, im.FromCache, im.Error)
		}
	}
	for _, f := range []string{"Logo.png", "Logo copy.png", "Background.png", "Background_2.png"} {
		x, _ := os.ReadFile(filepath.Join("one", "ATEM Media Pool", f))
		y, _ := os.ReadFile(filepath.Join("two", "ATEM Media Pool", f))
		if len(x) == 0 || !bytes.Equal(x, y) {
			t.Errorf("%s differs between the saves", f)
		}
	}
	if out := b.must(with("profile", "images", "--dir", "imgs")...); !strings.Contains(out, "Background_2.png") {
		t.Errorf("profile images:\n%s", out)
	}
	tr := &transcript{b: b}
	tr.run("profile", "save", "three/show.xml")
	if !strings.Contains(tr.out.String(), "4 stills, none downloaded") {
		t.Errorf("terminal:\n%s", tr.out.String())
	}

	// The restore puts Background_2.png back into slot 7 as "Background":
	// the green picture (near, not equal: the colour conversion rounds),
	// not slot 6's blue.
	b.must(with("media", "clear", "7", "--yes")...)
	b.must(with("profile", "restore", "one/show.xml", "--yes", "--video-mode", "false", "--macros-only", "--macros", "false",
		"--media-pool-images")...)
	got, name, _, ok := sw.Still(6)
	if !ok || name != "Background" || distance(got, green) >= distance(got, blue) {
		t.Errorf("slot 7 restored as %q (%v): %d from green, %d from blue", name, ok, distance(got, green), distance(got, blue))
	}
}

// distance is how far apart two frames' bytes are.
func distance(a, b []byte) int {
	d := 0
	for i := range min(len(a), len(b)) {
		if a[i] > b[i] {
			d += int(a[i] - b[i])
		} else {
			d += int(b[i] - a[i])
		}
	}
	return d + 255*abs(len(a)-len(b))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
