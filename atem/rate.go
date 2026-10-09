// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Transition, fade-to-black, DSK and DVE rates: commands take frames;
// atemwire's operations take "seconds:frames" strings and convert them at
// the switcher's display frame rate. From bmdwire v1.3.0
// atemwire/helpers.py (parse_rate, format_rate) and state.py (display_fps).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// FallbackFPS is the display rate used before the switcher has reported
// its video mode.
const FallbackFPS = 25

// ParseRate is atemwire's parse_rate: "seconds:frames" ("1:12") or a bare
// frame count ("50") to frames at fps. As in atemwire, a malformed string
// silently becomes fps frames (one second), and fps <= 0 means FallbackFPS.
func ParseRate(s string, fps int) int {
	if fps <= 0 {
		fps = FallbackFPS
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return fps
		}
		sec, err1 := pyAtoi(parts[0])
		fr, err2 := pyAtoi(parts[1])
		if err1 != nil || err2 != nil {
			return fps
		}
		return sec*fps + fr
	}
	n, err := pyAtoi(s)
	if err != nil {
		return fps
	}
	return n
}

// pyAtoi is Python's int() on a string: surrounding whitespace, one sign,
// single underscores between digits, and any Unicode decimal digit.
func pyAtoi(s string) (int, error) {
	s = strings.TrimSpace(s)
	digits := strings.TrimLeft(s, "+-")
	if len(s)-len(digits) > 1 || strings.HasPrefix(digits, "_") || strings.HasSuffix(digits, "_") ||
		strings.Contains(digits, "__") {
		return 0, fmt.Errorf("invalid literal %q", s)
	}
	var b strings.Builder
	b.WriteString(s[:len(s)-len(digits)])
	for _, r := range digits {
		switch {
		case r == '_':
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case unicode.IsDigit(r):
			// Decimal digits come in runs of whole 0-9 sets.
			start := r
			for unicode.IsDigit(start - 1) {
				start--
			}
			b.WriteByte(byte('0' + (r-start)%10))
		default:
			return 0, fmt.Errorf("invalid literal %q", s)
		}
	}
	return strconv.Atoi(b.String())
}

// FormatRate is atemwire's format_rate: frames as "seconds:frames" at fps.
func FormatRate(frames, fps int) string {
	if fps <= 0 {
		fps = FallbackFPS
	}
	q, r := frames/fps, frames%fps
	if r < 0 { // Python's floor division and modulo
		q, r = q-1, r+fps
	}
	return fmt.Sprintf("%d:%02d", q, r)
}

// DisplayFPS is atemwire's display_fps: the frame rate ATEM Software
// Control counts rates in. Below 48 fps it is the field rate; at 48 and
// above it is half (50 gives 25, 59.94 gives 30). Before the switcher has
// reported a video mode it is FallbackFPS.
func DisplayFPS(s State) int {
	m := s.VideoMode.Mode
	if m == nil || m.Rate <= 0 {
		return FallbackFPS
	}
	if m.Rate >= 48 {
		return int(math.RoundToEven(m.Rate / 2))
	}
	return int(math.RoundToEven(m.Rate))
}
