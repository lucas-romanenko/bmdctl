// SPDX-License-Identifier: MIT

package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// An upload given up after its fresh starts (bmdwire#100) is a transfer
// not finished in time, said plainly: how long nothing moved, after how
// many retries, and where it stopped, each line within a terminal's 72
// columns after "error  " even with a byte count.
func TestAStalledUploadSaysSo(t *testing.T) {
	stalled := &atem.UploadStalledError{Store: 0, Slot: 3, Phase: "5000000 of 9000000 bytes cut", After: 6 * time.Second, Retries: 2}
	err := transferErr(fmt.Errorf("wrapped: %w", stalled))
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeNoEcho {
		t.Fatalf("err %#v", err)
	}
	msg := e.Error()
	for _, want := range []string{"stalled", "6 s", "2 retries", "5000000 of 9000000 bytes cut"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q does not say %q", msg, want)
		}
	}
	for _, l := range strings.Split(msg, "\n") {
		if n := utf8.RuneCountInString(l); n > 72-len("error  ") {
			t.Errorf("%q: %d columns", l, n)
		}
	}
}
