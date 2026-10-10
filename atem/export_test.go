// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "time"

// SetSilence shortens a Dialer's 5 s silence timeout, for the tests in
// package atem_test.
func SetSilence(d *Dialer, silence time.Duration) { d.silence = silence }
