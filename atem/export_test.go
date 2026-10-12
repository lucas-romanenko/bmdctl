// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "time"

// SetSilence shortens a Dialer's 5 s silence timeout, for the tests in
// package atem_test.
func SetSilence(d *Dialer, silence time.Duration) { d.silence = silence }

// QueuedTransfers is how many transfers wait on s's lane to start, for
// the tests in package atem_test: one that waits for a lock is queued
// until it gets it.
func QueuedTransfers(s *Session) int {
	ch := make(chan int, 1)
	if s.do(func() {
		n := 0
		for _, q := range s.lane.queues {
			n += len(q)
		}
		ch <- n
	}) != nil {
		return 0
	}
	return <-ch
}
