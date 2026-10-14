// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// The quick start (docs/quickstart.md) is five commands and what they
// print, and what they print is the CLI's: this transcript, against a
// fake switcher named STUDIO A. The page's times are a terminal's, and
// the rest must match quickstart.golden; after an output change, copy
// the golden's lines into the page with times in place of #.
func TestTerminalQuickstart(t *testing.T) {
	tr := &transcript{b: newBenchDump(t, quickstartDump(t)), noHost: true}
	tr.run("connect", "192.168.1.20")
	tr.run("status")
	tr.run("inputs")
	tr.run("preview", "cam3")
	tr.run("cut")
	golden(t, "quickstart.golden", tr.out.String())

	page, err := os.ReadFile("../../docs/quickstart.md")
	if err != nil {
		t.Fatal(err)
	}
	var samples strings.Builder
	for _, block := range codeBlocks(string(page)) {
		if strings.HasPrefix(block, "$ bmdctl ") {
			samples.WriteString(block + "\n")
		}
	}
	untag := regexp.MustCompile(`<(b|dim|red|green|/)>`)
	session := regexp.MustCompile(`session \d+\w*`)
	got := session.ReplaceAllString(normalise(samples.String()), "session #")
	want := session.ReplaceAllString(untag.ReplaceAllString(tr.out.String(), ""), "session #")
	if got != want {
		t.Errorf("docs/quickstart.md's samples aren't what bmdctl prints; want (times as #):\n%s\ngot:\n%s", want, got)
	}
}

// codeBlocks returns the fenced code blocks in a Markdown page, each
// without its fences, ending in a newline.
func codeBlocks(page string) []string {
	var blocks []string
	var b strings.Builder
	in := false
	for _, line := range strings.Split(page, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if in {
				blocks = append(blocks, b.String())
				b.Reset()
			}
			in = !in
			continue
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	return blocks
}

// quickstartDump is the switcher the quick start shows: the fixture,
// named STUDIO A in ATEM Setup, at 1080p25, with black and inputs 3 to 7
// named, so that its keyers' sources read as a studio's.
func quickstartDump(t *testing.T) []byte {
	d := whoIDump(t, "STUDIO A")
	d[bytes.Index(d, []byte("VidM"))+4] = 10 // 1080p25
	var cam, bars []byte
	for b := d; len(b) >= 8; {
		n := int(binary.BigEndian.Uint16(b))
		if n < 8 || n > len(b) {
			break
		}
		if string(b[4:8]) == "InPr" {
			switch binary.BigEndian.Uint16(b[8:]) {
			case 2:
				cam = b[8:n]
			case 1000:
				bars = b[8:n]
			}
		}
		b = b[n:]
	}
	if cam == nil || bars == nil {
		t.Fatal("the fixture has no InPr for input 2 or bars")
	}
	inPr := func(like []byte, index uint16, name, short string, port byte) []byte {
		p := bytes.Clone(like)
		binary.BigEndian.PutUint16(p, index)
		clear(p[2:26])
		copy(p[2:22], name)
		copy(p[22:26], short)
		p[32] = port
		return fakeatem.Command("InPr", p)
	}
	named := inPr(bars, 0, "Black", "BLK", 1)
	for i := uint16(3); i <= 7; i++ {
		named = append(named, inPr(cam, i, fmt.Sprintf("Camera %d", i), fmt.Sprintf("CAM%d", i), cam[32])...)
	}
	i := bytes.Index(d, []byte("InCm")) - 4
	return slices.Concat(d[:i], named, d[i:])
}
