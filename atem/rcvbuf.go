// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"log/slog"
	"sync/atomic"
)

// rcvbufWarned: the receive buffer's cap is reported once per process, not
// once per switcher, as atemwire's _rcvbuf_warned.
var rcvbufWarned atomic.Bool

// checkReadBuffer is _check_rcvbuf (bmdwire#73, 1.11.0): say once per
// process when the system granted a smaller UDP receive buffer than Dial
// asked for. Linux caps SO_RCVBUF at net.core.rmem_max without a word, and
// its default (208 KB) is a fraction of one 4K still in flight: the kernel
// then drops what has not been read yet (a 4K Plus, 2026-11-04: 39,258
// receive buffer errors in 220,040 datagrams).
func checkReadBuffer(log *slog.Logger, asked, got int) {
	if asked == 0 || got >= asked || !rcvbufWarned.CompareAndSwap(false, true) {
		return
	}
	if log == nil {
		return
	}
	log.Warn("atem: the system caps the UDP receive buffer (net.core.rmem_max on Linux); large still downloads can overflow it and stall. Raise the cap, e.g. sysctl -w net.core.rmem_max=16777216",
		"granted_kb", got/1024, "asked_mb", asked>>20)
}
