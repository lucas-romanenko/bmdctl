// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// The option and result types of bmdwire v1.3.0
// atemwire/profile/options.py, field for field. Python's dataclass
// defaults are the constructors here (DefaultSaveOptions,
// DefaultApplyOptions, NewMixEffectOptions): a Go zero value selects
// nothing.

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// PlatformMaxMEs is PLATFORM_MAX_MES: the most M/Es any ATEM has, the
// length of the default MEs lists. Save and apply only loop the switcher's
// real M/E count, so extra entries do nothing.
const PlatformMaxMEs = 4

// MixEffectOptions selects the elements of one <MixEffectBlock>: Program,
// Preview, NextTransition (the selection mask), TransitionStyle (the style
// and the five per-style parameter elements), FadeToBlack and one Key per
// upstream keyer (USK[i] is the Key with index i). SaveOptions uses it to
// choose what to write and ApplyOptions what to restore.
type MixEffectOptions struct {
	Program         bool
	Preview         bool
	NextTransition  bool
	TransitionStyle bool
	FadeToBlack     bool
	USK             []bool
}

// NewMixEffectOptions is MixEffectOptions(): everything selected, four
// keyers.
func NewMixEffectOptions() MixEffectOptions {
	return MixEffectOptions{true, true, true, true, true, []bool{true, true, true, true}}
}

// MixEffectOptionsNone is MixEffectOptions.none(): nothing selected.
func MixEffectOptionsNone() MixEffectOptions {
	return MixEffectOptions{USK: []bool{false, false, false, false}}
}

// MixEffectOptionsApplyDefault is MixEffectOptions.apply_default():
// everything but the cuts to air, Program and Preview.
func MixEffectOptionsApplyDefault() MixEffectOptions {
	m := NewMixEffectOptions()
	m.Program, m.Preview = false, false
	return m
}

// AnySelected is any_selected: whether the M/E has anything selected.
func (m MixEffectOptions) AnySelected() bool {
	if m.Program || m.Preview || m.NextTransition || m.TransitionStyle || m.FadeToBlack {
		return true
	}
	for _, u := range m.USK {
		if u {
			return true
		}
	}
	return false
}

// meOptions is _PerMeOptions.me_options: the block for M/E me, or nothing
// selected beyond the list.
func meOptions(mes []MixEffectOptions, me int) MixEffectOptions {
	if me >= 0 && me < len(mes) {
		return mes[me]
	}
	return MixEffectOptionsNone()
}

// SaveOptions chooses what Save writes: per-M/E blocks under MEs, then one
// flag per section outside the M/E grid. Each flag gates exactly the XML
// element it names.
type SaveOptions struct {
	// MEs[i] selects what <MixEffectBlock index="i"> holds. An M/E with
	// nothing selected, or beyond the list, gets no block.
	MEs []MixEffectOptions

	DownstreamKeys  bool
	ColorGenerators bool
	Fairlight       bool
	CameraControl   bool

	MediaPoolMetadata bool // the <MediaPool> element
	// MediaPoolImages is never read: atemwire's save writes no pixels.
	MediaPoolImages bool
	MediaPlayers    bool

	Auxiliaries bool
	Counters    bool

	Settings   bool // <Settings>, <Inputs> included
	VideoMode  bool
	HyperDecks bool
	Macros     bool // <MacroPool> and <MacroControl>

	// MacroTimeout bounds each macro's download; zero means atemwire's
	// 10 s. Not in atemwire's SaveOptions: its timeout is download_macro's
	// default.
	MacroTimeout time.Duration
	// Logger receives what atemwire logs (a failed macro download);
	// nil discards it.
	Logger *slog.Logger
}

// DefaultSaveOptions is SaveOptions(): everything on, four M/Es.
func DefaultSaveOptions() *SaveOptions {
	o := &SaveOptions{
		DownstreamKeys: true, ColorGenerators: true, Fairlight: true, CameraControl: true,
		MediaPoolMetadata: true, MediaPoolImages: true, MediaPlayers: true,
		Auxiliaries: true, Counters: true,
		Settings: true, VideoMode: true, HyperDecks: true, Macros: true,
	}
	for range PlatformMaxMEs {
		o.MEs = append(o.MEs, NewMixEffectOptions())
	}
	return o
}

// MEOptions is me_options: the block for M/E me, nothing selected beyond
// the list.
func (o *SaveOptions) MEOptions(me int) MixEffectOptions { return meOptions(o.MEs, me) }

// ApplyOptions chooses what Apply restores: per-M/E blocks under MEs, then
// one flag per section outside the M/E grid.
type ApplyOptions struct {
	// MEs[i] selects what of <MixEffectBlock index="i"> is restored.
	// Blocks beyond the list, or beyond the switcher's M/E count, are
	// skipped.
	MEs []MixEffectOptions

	// No setter exists for these three: on or off, Apply only notes them
	// as skipped.
	RestoreMultiview     bool
	RestoreCameraControl bool
	RestoreTalkback      bool

	// RestoreHyperDecks binds each saved HyperDeck slot and clears the
	// ones saved as 0.0.0.0 (SURFACE.md §5.4 item 8).
	RestoreHyperDecks bool
	// RestoreFlyKeyframes drives the live keyer DVE through each saved
	// keyframe to store it, which shows on air.
	RestoreFlyKeyframes bool

	RestoreDownstreamKeys  bool
	RestoreColorGenerators bool
	RestoreAudio           bool

	// RestoreVideoMode changes the video mode when it differs: outputs
	// drop while the switcher resyncs.
	RestoreVideoMode     bool
	RestoreInputs        bool // input names
	RestoreMacros        bool
	RestoreAux           bool
	RestoreMediaPlayers  bool
	RestoreSettingsFlags bool // ftbEnabled
	// RestoreMediaPoolImages is never read: Apply restores no pixels.
	RestoreMediaPoolImages bool

	// ResyncWait is the pause after a video mode change, before the rest of
	// the profile goes out: zero means atemwire's 3 s, a negative value no
	// pause. Not in atemwire's ApplyOptions (its pause is fixed).
	ResyncWait time.Duration
	// MacroTimeout bounds each macro's upload; zero means atemwire's 10 s.
	MacroTimeout time.Duration
	// Logger receives what atemwire logs (the video mode warnings, dropped
	// macro ops, failed uploads); nil discards it.
	Logger *slog.Logger
}

// DefaultApplyOptions is ApplyOptions(): every section with a setter on
// except the cuts to air (Program and Preview of each M/E) and fly
// keyframes.
func DefaultApplyOptions() *ApplyOptions {
	o := &ApplyOptions{
		RestoreHyperDecks:      true,
		RestoreDownstreamKeys:  true,
		RestoreColorGenerators: true,
		RestoreAudio:           true,
		RestoreVideoMode:       true,
		RestoreInputs:          true,
		RestoreMacros:          true,
		RestoreAux:             true,
		RestoreMediaPlayers:    true,
		RestoreSettingsFlags:   true,
		RestoreMediaPoolImages: true,
	}
	for range PlatformMaxMEs {
		o.MEs = append(o.MEs, MixEffectOptionsApplyDefault())
	}
	return o
}

// MacrosOnly is ApplyOptions.macros_only(): the <MacroPool> and nothing
// else.
func MacrosOnly() *ApplyOptions {
	o := &ApplyOptions{RestoreMacros: true}
	for range PlatformMaxMEs {
		o.MEs = append(o.MEs, MixEffectOptionsNone())
	}
	return o
}

// MEOptions is me_options: the block for M/E me, nothing selected beyond
// the list.
func (o *ApplyOptions) MEOptions(me int) MixEffectOptions { return meOptions(o.MEs, me) }

// ApplyResult is what Apply did, section by section.
type ApplyResult struct {
	Applied []string
	Skipped []string
	Errors  []string

	log *slog.Logger
}

// NoteApplied is note_applied.
func (r *ApplyResult) NoteApplied(section string) { r.Applied = append(r.Applied, section) }

// NoteSkipped is note_skipped: "section: reason", or the section alone.
func (r *ApplyResult) NoteSkipped(section, reason string) {
	if reason != "" {
		section += ": " + reason
	}
	r.Skipped = append(r.Skipped, section)
}

// NoteError is note_error: "section: Class: message". The class is the
// Python exception's for an *Error, and for the *macro.Error kind, else
// errorClass's.
func (r *ApplyResult) NoteError(section string, err error) {
	class, msg := errorClass(err)
	r.Errors = append(r.Errors, fmt.Sprintf("%s: %s: %s", section, class, msg))
	if r.log != nil {
		r.log.Warn("profile apply error", "section", section, "err", msg)
	}
}

// Summary is summary(): "applied: N, skipped: N, errors: N" for the
// non-empty lists, or "no changes".
func (r *ApplyResult) Summary() string {
	var parts []string
	if len(r.Applied) > 0 {
		parts = append(parts, fmt.Sprintf("applied: %d", len(r.Applied)))
	}
	if len(r.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("skipped: %d", len(r.Skipped)))
	}
	if len(r.Errors) > 0 {
		parts = append(parts, fmt.Sprintf("errors: %d", len(r.Errors)))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}
