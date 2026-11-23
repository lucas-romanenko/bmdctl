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
	"time"
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

// --- bmdwire#63: the turn after our release goes to the session waiting on it

// sharedClock gives sessions one clock the test moves by hand.
func sharedClock(ss ...*Session) *time.Time {
	c := time.Unix(1000, 0)
	for _, s := range ss {
		s.t.now = func() time.Time { return c }
	}
	return &c
}

func lkst(t *testing.T, s *Session, store uint16, taken bool) {
	t.Helper()
	b := byte(0)
	if taken {
		b = 1
	}
	feedCmd(t, s, "LKST", []byte{byte(store >> 8), byte(store), b, 0})
}

func ftdc(t *testing.T, s *Session, tid uint16) {
	t.Helper()
	feedCmd(t, s, "FTDC", append(u16be(tid), 1, 2))
}

// packets is how many reliable packets the session has sent, and
// lastCodes the commands in the last of them.
func packets(c *fakeConn) int {
	n := 0
	for _, p := range c.sent() {
		if p.flags&flagReliable != 0 {
			n++
		}
	}
	return n
}

func lastCodes(c *fakeConn) []string {
	var out []string
	for _, p := range c.sent() {
		if p.flags&flagReliable == 0 {
			continue
		}
		out = nil
		_ = decodeFields(p.data, func(code string, _ []byte) error {
			out = append(out, code)
			return nil
		})
	}
	return out
}

func lastIs(c *fakeConn, codes ...string) bool {
	got := lastCodes(c)
	if len(got) != len(codes) {
		return false
	}
	for i := range got {
		if got[i] != codes[i] {
			return false
		}
	}
	return true
}

func upload(store, slot uint16) *transferTask {
	tk := task(taskUpload, store, slot)
	tk.data, tk.dataLength = make([]byte, 64), 64
	return tk
}

// seenEachOther: B downloads one still first, as a media pool watcher
// does, and A sees B's lock announced and B's transfer end.
func seenEachOther(t *testing.T, a, b *Session) {
	t.Helper()
	b.enqueueTask(task(taskDownload, 0, 5))
	grant(t, b, 0)
	lkst(t, a, 0, true)
	lkst(t, b, 0, true)
	tid := b.lane.transfer.tid
	ftdc(t, b, tid) // B's own: B releases
	ftdc(t, a, tid) // another session's, for A
	lkst(t, a, 0, false)
	lkst(t, b, 0, false)
}

func TestTheWaiterTakesTheTurnAfterOurRelease(t *testing.T) { // test_the_waiter_takes_the_turn_after_our_release
	a, ca := laneSession(t)
	b, cb := laneSession(t)
	a.lane.nextID, b.lane.nextID = 4000, 31000
	clock := sharedClock(a, b)
	seenEachOther(t, a, b)
	*clock = clock.Add(time.Second)
	a.enqueueTask(upload(0, 1))
	a.enqueueTask(upload(0, 2))
	grant(t, a, 0)
	lkst(t, a, 0, true)
	lkst(t, b, 0, true) // A's, for B
	b.enqueueTask(task(taskDownload, 0, 5))
	if n := count(cb, "PLCK"); n != 1 {
		t.Fatalf("B asked %d times while A holds the store", n)
	}
	tidA := a.lane.transfer.tid
	ftdc(t, a, tidA) // A's first upload done
	ftdc(t, b, tidA) // another session's, for B
	if !lastIs(ca, "LOCK") {
		t.Fatalf("A sent %v, not its release alone", lastCodes(ca))
	}
	lkst(t, a, 0, false) // A's release, announced to both
	lkst(t, b, 0, false)
	aAsks := packets(ca)

	*clock = clock.Add(lockCourtesy)
	a.serviceYields()
	b.serviceYields()
	if !lastIs(cb, "PLCK") {
		t.Fatalf("B sent %v: the waiter asks after the courtesy", lastCodes(cb))
	}
	if packets(ca) != aAsks {
		t.Fatal("A, whose release it was, asked beside the waiter")
	}

	grant(t, b, 0)
	lkst(t, a, 0, true) // B's grant, announced
	lkst(t, b, 0, true)
	*clock = clock.Add(lockHandover)
	a.serviceYields()
	if packets(ca) != aAsks {
		t.Fatal("A asked for B's store")
	}

	tidB := b.lane.transfer.tid
	ftdc(t, b, tidB) // B done: B releases
	ftdc(t, a, tidB)
	if !lastIs(cb, "LOCK") {
		t.Fatalf("B sent %v", lastCodes(cb))
	}
	lkst(t, a, 0, false)
	lkst(t, b, 0, false)
	bAsks := packets(cb)
	*clock = clock.Add(lockCourtesy)
	a.serviceYields()
	b.serviceYields()
	if !lastIs(ca, "PLCK") {
		t.Fatalf("A sent %v: it asks as the waiter now", lastCodes(ca))
	}
	if packets(cb) != bAsks {
		t.Fatal("B, done, asked")
	}
}

func TestAloneTheTurnAfterOurReleaseIsNotLengthened(t *testing.T) { // test_alone_the_turn_after_our_release_is_not_lengthened
	s, c := laneSession(t)
	clock := sharedClock(s)
	s.enqueueTask(task(taskDownload, 0, 1))
	s.enqueueTask(task(taskDownload, 0, 2))
	grant(t, s, 0)
	ftdc(t, s, s.lane.transfer.tid)
	lkst(t, s, 0, false)
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	if !lastIs(c, "PLCK") {
		t.Fatalf("sent %v: nobody seen, nobody to wait for", lastCodes(c))
	}
}

func TestAnotherClientSeenLongAgoDoesNotLengthenIt(t *testing.T) { // test_another_client_seen_long_ago_does_not_lengthen_it
	s, c := laneSession(t)
	clock := sharedClock(s)
	lkst(t, s, 0, true) // someone else's lock
	lkst(t, s, 0, false)
	*clock = clock.Add(lockQuiet + time.Second)
	s.enqueueTask(task(taskDownload, 0, 1))
	s.enqueueTask(task(taskDownload, 0, 2))
	grant(t, s, 0)
	ftdc(t, s, s.lane.transfer.tid)
	lkst(t, s, 0, false)
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	if !lastIs(c, "PLCK") {
		t.Fatalf("sent %v", lastCodes(c))
	}
}

func TestAReleaseAnnouncedAfterTheEchoWindowIsAnotherClients(t *testing.T) { // test_a_release_announced_after_the_echo_window_is_another_clients
	s, c := laneSession(t)
	clock := sharedClock(s)
	lkst(t, s, 0, true) // another client, lately
	lkst(t, s, 0, false)
	s.enqueueTask(task(taskDownload, 0, 1))
	s.enqueueTask(task(taskDownload, 0, 2))
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	grant(t, s, 0)
	ftdc(t, s, s.lane.transfer.tid)        // we release; nothing announced:
	*clock = clock.Add(2 * lockEchoWindow) // handed to a waiting request
	lkst(t, s, 0, false)                   // its holder lets go
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	if !lastIs(c, "PLCK") {
		t.Fatalf("sent %v: we are its waiter, not the releaser", lastCodes(c))
	}
}

func TestOurReleaseEchoWithAnotherClientSeenWaitsTheHandover(t *testing.T) { // test_our_release_echo_with_another_client_seen_waits_the_handover
	s, c := laneSession(t)
	clock := sharedClock(s)
	lkst(t, s, 0, true) // another client, lately
	lkst(t, s, 0, false)
	s.enqueueTask(task(taskDownload, 0, 1))
	s.enqueueTask(task(taskDownload, 0, 2))
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	grant(t, s, 0)
	ftdc(t, s, s.lane.transfer.tid)
	*clock = clock.Add(40 * time.Millisecond) // a frame: our release's own LKST
	lkst(t, s, 0, false)
	asked := packets(c)
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	if packets(c) != asked {
		t.Fatal("asked with a waiter")
	}
	if want := clock.Add(lockHandover); !s.lane.yieldDue.Equal(want) {
		t.Fatalf("woken at %v, want %v", s.lane.yieldDue, want)
	}
	*clock = clock.Add(lockHandover)
	s.serviceYields()
	if !lastIs(c, "PLCK") {
		t.Fatalf("sent %v: nobody took it, ours again", lastCodes(c))
	}
}

func TestALateAnnouncementOfOurOwnGrantIsNotAnotherClient(t *testing.T) { // test_a_late_announcement_of_our_own_grant_is_not_another_client
	// A transfer (or a clear) shorter than a frame is released before the
	// switcher announces its grant: that LKST comes after our release. It
	// is ours, and must not make every later turn wait lockHandover.
	s, c := laneSession(t)
	clock := sharedClock(s)
	for slot := uint16(1); slot <= 2; slot++ {
		s.enqueueTask(task(taskDownload, 0, slot))
		if slot == 2 {
			*clock = clock.Add(lockCourtesy)
			s.serviceYields()
		}
		grant(t, s, 0)
		*clock = clock.Add(10 * time.Millisecond)
		ftdc(t, s, s.lane.transfer.tid) // done, released
		*clock = clock.Add(20 * time.Millisecond)
		lkst(t, s, 0, true)  // our grant, announced late
		lkst(t, s, 0, false) // and our release
	}
	if !s.lane.othersSeen.IsZero() {
		t.Fatalf("another client seen at %v", s.lane.othersSeen)
	}
	// The next transfers, a normal length, alone: no handover.
	s.enqueueTask(task(taskDownload, 0, 3))
	s.enqueueTask(task(taskDownload, 0, 4))
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	grant(t, s, 0)
	*clock = clock.Add(2 * time.Second)
	ftdc(t, s, s.lane.transfer.tid)
	*clock = clock.Add(40 * time.Millisecond)
	lkst(t, s, 0, false) // our release's echo
	*clock = clock.Add(lockCourtesy)
	s.serviceYields()
	if !lastIs(c, "PLCK") {
		t.Fatalf("sent %v: waited a handover for nobody", lastCodes(c))
	}
}
