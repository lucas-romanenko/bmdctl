// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"fmt"
	"testing"
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
		"192.0.2.10:9910": "no switcher answered at 192.0.2.10:9910 within 15s",
	} {
		if got := dialMessage(key, late); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}
