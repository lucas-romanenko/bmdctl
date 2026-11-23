// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// A dial that ran out of time says how long the device was given, as the
// dial gave it: a HyperDeck's connect and greeting ConnectTimeout
// (hyperdeck.Dial bounds them, whatever the engine's deadline), a
// Videohub two tries of its ConnectTimeout.
func TestDialMessages(t *testing.T) {
	late := fmt.Errorf("dial: %w", context.DeadlineExceeded)
	for key, want := range map[string]string{
		"192.0.2.20:9993": "no HyperDeck answered at 192.0.2.20:9993 within 5s",
		"192.0.2.31:9990": "no Videohub answered at 192.0.2.31:9990 (tried twice, 3s each)",
		"192.0.2.10:9910": "no switcher answered at 192.0.2.10:9910 within 1m0s",
	} {
		if got := dialMessage(key, late); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

// An ATEM's Dial says why it gave up (bmdwire#75, #77): no room, no answer
// to two hellos, or nothing more after the answer.
func TestDialMessagesForAnATEM(t *testing.T) {
	const key = "192.0.2.10:9910"
	for err, want := range map[error]string{
		atem.ErrNoFreeConnection: key + ": no free connection on this switcher (ATEM Software Control, panels or other clients are using all its slots)",
		atem.ErrNoAnswer:         "no switcher answered at " + key + " within 10s",
		atem.ErrStalled:          "the switcher at " + key + " stopped sending during the connect (nothing for 5s)",
	} {
		if got := dialMessage(key, fmt.Errorf("atem: dial %s: %w", key, err)); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}
