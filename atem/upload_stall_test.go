// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// An upload is done only when the switcher says so; one that stops moving
// goes again in a new session, then fails (bmdwire#100, d19c1cd, 174840f,
// 5587e15, e151696; test_upload_stall.py). The hardware bench, 2026-11-24
// (Constellation HD, fw 2.30, both units): the cable out for 3 s in the
// middle of a still upload. Afterwards everything was acknowledged on both
// sides, the switcher answered every second and never granted the next
// budget: the still sat at 47 % until the caller gave up. Started again in
// the same session, the lock was asked for, acknowledged and never
// granted.

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// stallSession is test_upload_stall.py's proto fixture: an upload to
// still slot 3 in flight, four budgets of it sent, on a clock the test
// moves, its log kept and its progress followed.
func stallSession(t *testing.T) (*Session, *fakeConn, *transferTask, *time.Time, *bytes.Buffer) {
	t.Helper()
	s, c := laneSession(t)
	var logs bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	s.t.log = s.log
	clock := moveClock(s)
	tk := uploadTask(3)
	tk.progress = make(chan Progress, 1)
	s.enqueueTask(tk)
	grant(t, s, 0)
	feedBudget(t, s, 4)
	s.t.ackedThrough = 50
	c.reset()
	return s, c, tk, clock, &logs
}

// hellos counts the hellos (SYN 0x01) and goodbyes (SYN 0x04) sent.
func hellos(c *fakeConn) (hello, goodbye int) {
	for _, p := range c.sent() {
		if p.flags&flagSYN == 0 || len(p.data) == 0 {
			continue
		}
		switch p.data[0] {
		case helloConnect:
			hello++
		case helloClosing:
			goodbye++
		}
	}
	return hello, goodbye
}

// stall lets the lane look, moves the clock d on and lets it look again.
func stall(s *Session, clock *time.Time, d time.Duration) {
	s.serviceYields()
	*clock = clock.Add(d)
	s.serviceYields()
}

func TestNoProgressForUploadStallStartsItAgainInANewSession(t *testing.T) { // test_no_progress_for_upload_stall_starts_it_again_in_a_new_session
	s, c, tk, clock, logs := stallSession(t)
	stall(s, clock, uploadStall-10*time.Millisecond)
	if h, _ := hellos(c); h != 0 || s.lane.yieldDue.IsZero() {
		t.Fatalf("%d hellos, timer %v: it waits, and wakes to look again", h, s.lane.yieldDue)
	}
	*clock = clock.Add(10 * time.Millisecond)
	s.serviceYields()
	if h, g := hellos(c); h != 1 || g != 1 {
		t.Fatalf("%d hellos, %d goodbyes: a new session", h, g)
	}
	if !strings.Contains(logs.String(), "5568 of 13920 bytes cut") {
		t.Fatalf("the phase not logged:\n%s", logs)
	}
	if n := count(c, "LOCK"); n != 0 {
		t.Fatal("its lock given back in this session: there the lock got no answer")
	}
	if p := <-tk.progress; p != (Progress{Done: 0, Total: tk.size, Retry: 1, Retries: uploadRetries}) {
		t.Fatalf("progress %+v: from nothing again, retry 1 of %d", p, uploadRetries)
	}
	if len(s.lane.carried) != 1 || s.lane.carried[0] != tk {
		t.Fatal("not carried over to the new session")
	}
	noResult(t, tk)
}

func TestWhateverMovesWhileItsDataGoesIsProgress(t *testing.T) { // test_whatever_moves_while_its_data_goes_is_progress
	s, c, tk, clock, _ := stallSession(t)
	s.serviceYields()
	for step := range 10 {
		*clock = clock.Add(uploadStall - time.Second)
		switch step % 3 {
		case 0:
			s.t.ackedThrough++
		case 1:
			tk.offset += 1392
		case 2:
			s.lane.grants++
		}
		s.serviceYields()
	}
	if h, _ := hellos(c); h != 0 {
		t.Fatal("a new session for an upload that moves")
	}
	noResult(t, tk)
}

func TestOurRequestsAcknowledgedWhileItWaitsForItsLockAreNotProgress(t *testing.T) { // test_our_requests_acknowledged_while_it_waits_for_its_lock_are_not_progress
	// The hardware bench, 15:00 to 15:55: started again, the lock asked
	// for and acknowledged, never granted; the acknowledgements kept the
	// stall timer from ever running out, no second retry, no error.
	s, c := laneSession(t)
	var logs bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	clock := moveClock(s)
	s.enqueueTask(uploadTask(3))
	s.t.ackedThrough = 1
	s.serviceYields()
	for range int(uploadStall / time.Second) {
		*clock = clock.Add(time.Second)
		s.t.ackedThrough++ // our lock request, again
		s.serviceYields()
	}
	if h, _ := hellos(c); h != 1 {
		t.Fatalf("%d hellos, want a new session", h)
	}
	if !strings.Contains(logs.String(), `phase="waiting for its lock"`) {
		t.Fatalf("phase not named:\n%s", logs.String())
	}
}

func TestAfterUploadRetriesItFailsWithTheReason(t *testing.T) { // test_after_upload_retries_it_fails_with_the_reason
	s, c, tk, clock, _ := stallSession(t)
	for range uploadRetries {
		stall(s, clock, uploadStall)
	}
	noResult(t, tk)
	stall(s, clock, uploadStall)
	if h, _ := hellos(c); h != uploadRetries {
		t.Fatalf("%d new sessions, want %d", h, uploadRetries)
	}
	err := result(t, tk).err
	var se *UploadStalledError
	if !errors.As(err, &se) || !errors.Is(err, ErrUploadStalled) || se.Retries != uploadRetries || se.Slot != 3 {
		t.Fatalf("err %v", err)
	}
	// Carried over to a session that never came: that is where it stalled.
	if se.Phase != "waiting for a new session" || !strings.Contains(err.Error(), "fresh starts") {
		t.Fatalf("err %v", err)
	}
	if len(s.lane.carried) != 0 || len(s.lane.queues[0]) != 0 {
		t.Fatal("not off its queue")
	}
}

func TestTheLockHeldByAnotherClientWaitsLongerButNotForEver(t *testing.T) { // test_the_lock_held_by_another_client_waits_longer_but_not_for_ever
	s, c := laneSession(t)
	clock := moveClock(s)
	feedLockState(t, s, 0, true) // taken, not by us
	s.enqueueTask(uploadTask(3))
	stall(s, clock, uploadStall*5)
	if h, _ := hellos(c); h != 0 {
		t.Fatal("a new session while another client holds the lock")
	}
	*clock = clock.Add(lockWaitMax)
	s.serviceYields()
	if h, _ := hellos(c); h != 1 {
		t.Fatalf("%d hellos after lockWaitMax", h)
	}
}

func TestUploadsAreCarriedOverALostSessionAndAskedForAgain(t *testing.T) { // test_uploads_are_carried_over_a_lost_session_and_asked_for_again
	s, c, tk, _, _ := stallSession(t)
	other := uploadTask(4)
	s.enqueueTask(other)
	if !tk.metadataSent {
		t.Fatal("no FTFD with the first budget")
	}
	s.disconnected() // the session lost
	if len(s.lane.carried) != 2 || s.lane.carried[0] != tk || s.lane.carried[1] != other {
		t.Fatalf("carried %v", s.lane.carried)
	}
	if tk.offset != 0 || tk.tid != 0 || tk.metadataSent {
		t.Fatalf("offset %d, tid %d, metadata sent %v: not from the beginning", tk.offset, tk.tid, tk.metadataSent)
	}
	noResult(t, tk)
	noResult(t, other)
	c.reset()
	s.onReady() // the next session
	q := s.lane.queues[0]
	if len(q) != 2 || q[0] != tk || q[1] != other || count(c, "PLCK") != 1 {
		t.Fatalf("queued %v, %d PLCK", q, count(c, "PLCK"))
	}
	grant(t, s, 0)
	if s.lane.transfer != tk || tk.tid == 0 || count(c, "FTSD") != 1 {
		t.Fatal("not asked for again")
	}
}

func TestASessionThatNeverComesBackFailsTheUploadInTheEnd(t *testing.T) { // test_a_session_that_never_comes_back_fails_the_upload_in_the_end
	s, _, tk, clock, _ := stallSession(t)
	s.disconnected()
	for range uploadRetries + 1 {
		stall(s, clock, uploadStall)
	}
	var se *UploadStalledError
	if err := result(t, tk).err; !errors.As(err, &se) || se.Phase != "waiting for a new session" {
		t.Fatalf("err %v", err)
	}
	if len(s.lane.carried) != 0 {
		t.Fatal("still carried")
	}
}

func TestADownloadIsLeftToItsCaller(t *testing.T) { // test_a_download_is_left_to_its_caller
	s, c := laneSession(t)
	clock := moveClock(s)
	tk := task(taskDownload, 0, 3)
	s.enqueueTask(tk)
	grant(t, s, 0)
	stall(s, clock, uploadStall*5)
	if h, _ := hellos(c); h != 0 {
		t.Fatal("a new session for a download")
	}
	noResult(t, tk)
}

// A caller that gives up on an upload carried over takes it off the list,
// and is not told another client held the lock: its session was missing.
func TestACarriedUploadItsCallerGivesUpOnIsDropped(t *testing.T) {
	s, _, tk, _, _ := stallSession(t)
	s.disconnected()
	err := s.cancelTask(tk, errors.New("timed out"))
	if err == nil || errors.Is(err, ErrLockNotGranted) || len(s.lane.carried) != 0 {
		t.Fatalf("err %v, carried %v", err, s.lane.carried)
	}
}

// Close fails an upload carried over: there is no next session.
func TestCloseFailsAnUploadCarriedOver(t *testing.T) {
	s, _, tk, _, _ := stallSession(t)
	s.disconnected()
	s.shutdown()
	if err := result(t, tk).err; !errors.Is(err, ErrClosed) {
		t.Fatalf("err %v", err)
	}
}
