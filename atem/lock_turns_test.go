// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Two sessions taking turns at the switcher's stores: the ports of
// bmdwire v1.9.0's test_lock_turns.py (bmdwire#68). bmdwire#64: every
// session started its transfer ids at 43, so one session took another's
// FTDC for its own; each session now draws its first id. bmdwire#63:
// after a release, the releaser and the session waiting on it both asked
// lockCourtesy after it was announced, and one transfer was aborted; the
// turn after our own release now goes to the waiter (lockHandover).

import (
	"testing"
)

// --- bmdwire#64: each session its own transfer ids ---------------------------

func TestEachSessionDrawsItsFirstTransferID(t *testing.T) { // test_each_session_draws_its_first_transfer_id
	drawn := []uint16{1000, 52000}
	old := firstTransferID
	firstTransferID = func() uint16 { v := drawn[0]; drawn = drawn[1:]; return v }
	defer func() { firstTransferID = old }()
	a := newSession(newFakeConn(), switcherAddr, nil, 0)
	b := newSession(newFakeConn(), switcherAddr, nil, 0)
	for _, s := range []*Session{a, b} {
		s.t.state, s.t.sessionID, s.t.localSeq, s.t.enableACK = stateEstablished, 0x8001, 0, true
		s.initialized, s.protoConnected = true, true
		s.enqueueTask(task(taskDownload, StoreMacros, 1))
	}
	if a.lane.transfer.tid != 1001 || b.lane.transfer.tid != 52001 {
		t.Fatalf("ids %d and %d: both used to be 43", a.lane.transfer.tid, b.lane.transfer.tid)
	}
}

func TestFirstTransferIDsAreSpreadOverTheU16Range(t *testing.T) { // test_first_transfer_ids_are_spread_over_the_u16_range
	seen := map[uint16]bool{}
	for range 400 {
		id := firstTransferID()
		if id == 0 {
			t.Fatal("drew 0")
		}
		seen[id] = true
	}
	if len(seen) <= 390 {
		t.Fatalf("%d distinct ids in 400", len(seen))
	}
}

func TestAnotherSessionsCompletionDoesNotEndOurs(t *testing.T) { // test_another_sessions_completion_does_not_end_ours
	a, _ := laneSession(t)
	b, _ := laneSession(t)
	a.lane.nextID, b.lane.nextID = 4000, 31000
	a.enqueueTask(task(taskDownload, StoreMacros, 1)) // a macro, in one session
	still := task(taskDownload, 0, 5)                 // a still, in the other
	b.enqueueTask(still)
	grant(t, b, 0)
	tidA := a.lane.transfer.tid
	feedCmd(t, a, "FTDC", append(u16be(tidA), 1, 2))
	feedCmd(t, b, "FTDC", append(u16be(tidA), 1, 2)) // the same broadcast, for B
	if b.lane.transfer != still || len(b.lane.queues[0]) != 1 || !b.lane.locks[0] {
		t.Fatalf("B lost its transfer: %+v", b.lane)
	}
	noResult(t, still)
}

func TestTransferIDsWrapAndSkipZero(t *testing.T) { // test_transfer_ids_wrap_and_skip_zero
	s, _ := laneSession(t)
	s.lane.nextID = 0xFFFF
	s.enqueueTask(task(taskDownload, StoreMacros, 1))
	if s.lane.transfer.tid != 1 {
		t.Fatalf("tid %d", s.lane.transfer.tid)
	}
	feedCmd(t, s, "FTDC", append(u16be(1), 1, 2))
	s.enqueueTask(task(taskDownload, StoreMacros, 2))
	if s.lane.transfer.tid != 2 {
		t.Fatalf("tid %d", s.lane.transfer.tid)
	}
}
