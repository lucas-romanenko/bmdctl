// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro_test

import (
	"maps"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
)

// The codec's copy of VIDEO_MODE_NAMES is atem's: in bmdwire v1.3.0 the
// VideoMode op imports the one table set_video_mode uses.
func TestVideoModeNamesMatchAtem(t *testing.T) {
	if !maps.Equal(macro.VideoModeNames, atem.VideoModeNames) {
		t.Errorf("macro %v\natem  %v", macro.VideoModeNames, atem.VideoModeNames)
	}
}
