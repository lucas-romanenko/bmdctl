// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The transfer lane, driven directly as bmdwire v1.3.0's tests drive
// AtemProtocol: ports of the engine tests in test_lock_delivery.py,
// test_download_still.py, test_session_hygiene_atemwire.py and
// test_teardown_releases_locks.py. Each Go test names the Python test it
// mirrors. The end-to-end flows against the fake switcher are in
// transfer_fake_test.go.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// laneSession is the Python tests' proto fixture: a session whose lane is
// called directly, established and ready, writing to a fakeConn. Nothing
// runs its goroutines.
func laneSession(t *testing.T) (*Session, *fakeConn) {
	t.Helper()
	c := newFakeConn()
	s := newSession(c, switcherAddr, nil, 0)
	s.t.state = stateEstablished
	s.t.sessionID = 0x8001
	s.t.localSeq = 0
	s.t.enableACK = true
	s.initialized = true
	s.protoConnected = true
	return s, c
}

// sentCodes returns every command the session has sent, in order, as opcodes.
func sentCodes(c *fakeConn) []string {
	var out []string
	for _, p := range c.sent() {
		if p.flags&flagReliable == 0 {
			continue
		}
		_ = decodeFields(p.data, func(code string, _ []byte) error {
			out = append(out, code)
			return nil
		})
	}
	return out
}

// commands returns the payloads of the commands with this opcode.
func commands(c *fakeConn, code string) [][]byte {
	var out [][]byte
	for _, p := range c.sent() {
		_ = decodeFields(p.data, func(k string, payload []byte) error {
			if k == code {
				out = append(out, bytes.Clone(payload))
			}
			return nil
		})
	}
	return out
}

func count(c *fakeConn, code string) int { return len(commands(c, code)) }

// feedCmd hands the session's packet handlers one command, as save_field_data.
func feedCmd(t *testing.T, s *Session, code string, payload []byte) {
	t.Helper()
	s.onData(cmdBytes(code, payload))
	if s.fatal != nil {
		t.Fatal(s.fatal)
	}
}

func grant(t *testing.T, s *Session, store uint16) {
	t.Helper()
	feedCmd(t, s, "LKOB", []byte{byte(store >> 8), byte(store), 0, 0})
}

func u16be(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }

func task(kind taskKind, store, slot uint16) *transferTask {
	return &transferTask{kind: kind, store: store, slot: slot, done: make(chan transferResult, 1)}
}

func result(t *testing.T, tk *transferTask) transferResult {
	t.Helper()
	select {
	case r := <-tk.done:
		return r
	default:
		t.Fatal("the task has no result")
		return transferResult{}
	}
}

func noResult(t *testing.T, tk *transferTask) {
	t.Helper()
	select {
	case r := <-tk.done:
		t.Fatalf("the task finished: %+v", r)
	default:
	}
}

// --- test_lock_delivery.py: the lock dance ---------------------------------

func TestOneLockRequestForSeveralQueuedDownloads(t *testing.T) { // test_one_lock_request_for_several_queued_downloads
	s, c := laneSession(t)
	for slot := uint16(1); slot <= 3; slot++ {
		s.enqueueTask(task(taskDownload, 0, slot))
	}
	if n := count(c, "PLCK"); n != 1 {
		t.Fatalf("three downloads queued together sent %d PLCKs", n)
	}
	grant(t, s, 0)
	if n := count(c, "FTSU"); n != 1 {
		t.Fatalf("%d FTSU", n)
	}
}

// After a second, an unanswered lock request is sent again on the next
// trigger (LOCK_REREQUEST_SECONDS); before it, it isn't.
func TestLockRequestIsRepeatedAfterASecond(t *testing.T) {
	s, c := laneSession(t)
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	s.enqueueTask(task(taskDownload, 0, 1))
	now = now.Add(999 * time.Millisecond)
	s.enqueueTask(task(taskDownload, 0, 2))
	now = now.Add(time.Millisecond)
	s.enqueueTask(task(taskDownload, 0, 3))
	if n := count(c, "PLCK"); n != 2 {
		t.Fatalf("%d PLCKs, want 2", n)
	}
}

func TestFTDE6ReleasesKeepsTheTaskAndReRequestsOnRelease(t *testing.T) { // test_ftde_6_releases_keeps_the_task_and_re_requests_on_release
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	if count(c, "FTSU") != 1 {
		t.Fatal("no FTSU")
	}
	feedCmd(t, s, "FTDE", append(u16be(tid), 6, 0))
	if len(s.lane.queues[0]) != 1 {
		t.Fatal("the task must survive: 6 is a collision, not a rejection")
	}
	if count(c, "LOCK") != 1 {
		t.Fatal("we still hold the lock after 6; it must be given back")
	}
	if s.lane.locks[0] || s.lane.transfer != nil || s.lane.requested {
		t.Fatalf("lane %+v", s.lane)
	}
	if count(c, "PLCK") != 1 {
		t.Fatal("no blind re-request while the other session is queued")
	}
	// The store frees (our echo, or the other session's release): re-request.
	feedCmd(t, s, "LKST", []byte{0, 0, 0, 0})
	if count(c, "PLCK") != 2 {
		t.Fatal("no re-request on the release")
	}
	grant(t, s, 0)
	if count(c, "FTSU") != 2 {
		t.Fatal("the kept task didn't run under the new grant")
	}
	noResult(t, tk)
}

func TestFTDE6IsNotRaisedAsAFatalError(t *testing.T) { // test_ftde_6_is_not_raised_as_a_fatal_error
	s, _ := laneSession(t)
	tk := task(taskDownload, 0, 4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	feedCmd(t, s, "FTDE", append(u16be(s.lane.transfer.tid), 6, 0))
	noResult(t, tk)
}

// Statuses 1 (try again) and 5 (no lock) are recovered too: 1 resends the
// request with the same transfer id, 5 asks for the lock again.
func TestFTDE1And5AreRecovered(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	feedCmd(t, s, "FTDE", append(u16be(tid), 1, 0))
	if ftsu := commands(c, "FTSU"); len(ftsu) != 2 || binary.BigEndian.Uint16(ftsu[1]) != tid {
		t.Fatalf("FTSU %x", ftsu)
	}
	feedCmd(t, s, "FTDE", append(u16be(tid), 5, 0))
	if count(c, "PLCK") != 2 || s.lane.locks[0] {
		t.Fatal("no lock re-request after 5")
	}
	grant(t, s, 0)
	if ftsu := commands(c, "FTSU"); len(ftsu) != 3 || binary.BigEndian.Uint16(ftsu[2]) != tid+1 {
		t.Fatalf("FTSU %x", ftsu)
	}
	noResult(t, tk)
}

func TestLockIsReleasedBeforeTheFrameIsDecodedAndDelivered(t *testing.T) { // test_lock_is_released_before_the_frame_is_decoded_and_delivered
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 7)
	s.enqueueTask(tk)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	feedCmd(t, s, "FTDa", append(append(u16be(tid), 0, 8), make([]byte, 8)...))
	feedCmd(t, s, "FTDC", append(u16be(tid), 1, 2))
	w := sentCodes(c)
	if w[len(w)-1] != "LOCK" {
		t.Fatalf("wire %v: the unlock must go out with the FTDC", w)
	}
	if r := result(t, tk); r.err != nil || !bytes.Equal(r.data, make([]byte, 8)) {
		t.Fatalf("result %+v", r)
	}
	if s.lane.locks[0] || s.lane.transfer != nil {
		t.Fatalf("lane %+v", s.lane)
	}
	// Each chunk was acknowledged with FTUA(tid, 0).
	if ack := commands(c, "FTUA"); len(ack) != 1 || !bytes.Equal(ack[0], append(u16be(tid), 0, 0)) {
		t.Fatalf("FTUA %x", ack)
	}
}

func TestLockRequestFlagClearsOnReleaseBroadcast(t *testing.T) { // test_lock_request_flag_clears_on_release_broadcast
	s, c := laneSession(t)
	s.enqueueTask(task(taskDownload, 0, 1))
	if _, ok := s.lane.lockRequested[0]; !ok {
		t.Fatal("request not noted")
	}
	feedCmd(t, s, "LKST", []byte{0, 0, 0, 0}) // someone released
	if count(c, "PLCK") != 2 {
		t.Fatal("the pounce re-requests; a queued duplicate is harmless")
	}
}

func TestLaneResetsClearTheRequestFlag(t *testing.T) { // test_lane_resets_clear_the_request_flag
	s, _ := laneSession(t)
	tk := task(taskDownload, 0, 1)
	s.enqueueTask(tk)
	if err := s.cancelTask(tk, context.Canceled); err == nil {
		t.Fatal("no error for a cancelled task")
	}
	s.enqueueTask(task(taskDownload, 0, 1))
	s.resetLane(ErrDisconnected)
	if len(s.lane.lockRequested) != 0 {
		t.Fatal("the request flag survived the reset")
	}
}

// test_download_transfer_fails_fast_on_fatal_rejection: a fatal status
// fails the task at once, drops it and releases the lock.
func TestFatalStatusFailsTheTaskAtOnce(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 3)
	next := task(taskDownload, 0, 4)
	s.enqueueTask(tk)
	s.enqueueTask(next)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	feedCmd(t, s, "FTDE", append(u16be(tid), 2, 0))
	var te *TransferError
	if r := result(t, tk); !errors.As(r.err, &te) || te.Status != 2 || te.Slot != 3 || !strings.Contains(r.err.Error(), "not-found") {
		t.Fatalf("result %+v", r)
	}
	if len(s.lane.queues[0]) != 1 || s.lane.queues[0][0] != next || count(c, "LOCK") != 1 {
		t.Fatalf("queue %v, LOCKs %d", s.lane.queues[0], count(c, "LOCK"))
	}
	noResult(t, next)
}

// --- test_download_still.py ------------------------------------------------

// test_download_macro_uses_macro_store: store 0xFFFF takes no lock, its
// FTSU carries the macro magic, and an empty slot (FTDC with no FTDa)
// yields empty bytes, not RLE-decoded.
func TestDownloadMacroUsesTheMacroStore(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskDownload, StoreMacros, 5)
	s.enqueueTask(tk)
	if count(c, "PLCK") != 0 || count(c, "LOCK") != 0 {
		t.Fatal("the macro store took a lock")
	}
	ftsu := commands(c, "FTSU")
	if len(ftsu) != 1 || !bytes.Equal(ftsu[0], []byte{0, 43, 0xff, 0xff, 0, 0, 0, 5, 3, 0xd0, 0x9b, 0x8c}) {
		t.Fatalf("FTSU %x", ftsu)
	}
	feedCmd(t, s, "FTDC", []byte{0, 43, 1, 2})
	if r := result(t, tk); r.err != nil || r.data == nil || len(r.data) != 0 {
		t.Fatalf("result %+v", r)
	}
}

func TestTimeoutNamesForeignLockHolder(t *testing.T) { // test_timeout_names_foreign_lock_holder
	s, _ := laneSession(t)
	tk := task(taskDownload, 0, 3)
	s.enqueueTask(tk) // PLCK sent, LKOB never comes
	if err := s.cancelTask(tk, context.DeadlineExceeded); !errors.Is(err, ErrLockNotGranted) ||
		!errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "slot 3") {
		t.Fatalf("err %v", err)
	}
	s2, _ := laneSession(t)
	tk = task(taskDownload, 0, 3)
	s2.enqueueTask(tk)
	grant(t, s2, 0) // lock granted; data stalled
	if err := s2.cancelTask(tk, context.DeadlineExceeded); errors.Is(err, ErrLockNotGranted) {
		t.Fatalf("err %v", err)
	}
}

// test_timeout_aborts_transfer_state, for one task: a transfer in flight
// that its caller gives up on is abandoned like a fatal one, its lock
// released, and the lane goes on to the next.
func TestCancelledTransferInFlightIsAbandoned(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 3)
	next := task(taskDownload, 0, 4)
	s.enqueueTask(tk)
	s.enqueueTask(next)
	grant(t, s, 0)
	s.cancelTask(tk, context.DeadlineExceeded)
	if s.lane.transfer != nil || s.lane.requested || s.lane.queues[0][0] != next || count(c, "LOCK") != 1 {
		t.Fatalf("lane %+v", s.lane)
	}
	// A straggler for the abandoned transfer changes nothing.
	feedCmd(t, s, "FTDC", append(u16be(43), 1, 2))
	noResult(t, next)
	feedCmd(t, s, "LKST", []byte{0, 0, 0, 0})
	grant(t, s, 0)
	if ftsu := commands(c, "FTSU"); len(ftsu) != 2 || binary.BigEndian.Uint32(ftsu[1][4:]) != 4 {
		t.Fatalf("FTSU %x", ftsu)
	}
}

func TestQueueClearRequestsFullLockNotPLCK(t *testing.T) { // test_queue_clear_requests_full_lock_not_plck
	s, c := laneSession(t)
	tk := task(taskClear, 0, 6)
	s.enqueueTask(tk)
	if len(s.lane.queues[0]) != 1 {
		t.Fatal("not queued until LKOB")
	}
	if w := sentCodes(c); !slices.Equal(w, []string{"LOCK"}) || !bytes.Equal(commands(c, "LOCK")[0], []byte{0, 0, 1, 0}) {
		t.Fatalf("wire %v %x", w, commands(c, "LOCK"))
	}
}

func TestQueueClearDispatchesCSTLUnderLockThenReleases(t *testing.T) { // test_queue_clear_dispatches_cstl_under_lock_then_releases
	s, c := laneSession(t)
	tk := task(taskClear, 0, 6)
	s.enqueueTask(tk)
	c.reset()
	grant(t, s, 0)
	if len(s.lane.queues[0]) != 0 {
		t.Fatal("task not popped")
	}
	if w := sentCodes(c); !slices.Equal(w, []string{"CSTL", "LOCK"}) {
		t.Fatalf("wire %v", w)
	}
	if commands(c, "CSTL")[0][0] != 6 || commands(c, "LOCK")[0][2] != 0 {
		t.Fatal("wrong CSTL or LOCK")
	}
	if r := result(t, tk); r.err != nil {
		t.Fatal(r.err)
	}
	if s.lane.locks[0] || !s.lane.releasing[0] {
		t.Fatal("lock belief not reset, or the release not ours until announced")
	}
}

func TestDequeueClearRemovesPendingTaskOnly(t *testing.T) { // test_dequeue_clear_removes_pending_task_only
	s, _ := laneSession(t)
	tk := task(taskClear, 0, 6)
	s.enqueueTask(tk)
	if err := s.cancelTask(tk, context.DeadlineExceeded); !errors.Is(err, ErrLockNotGranted) {
		t.Fatalf("err %v", err)
	}
	if len(s.lane.queues[0]) != 0 {
		t.Fatal("not dequeued")
	}
	if err := s.cancelTask(tk, context.DeadlineExceeded); err != nil {
		t.Fatal("cancelling twice did something")
	}
	// A clear already dispatched is not withdrawn: its caller succeeds.
	tk = task(taskClear, 0, 7)
	s.lane.locks[0] = true
	s.enqueueTask(tk)
	if err := s.cancelTask(tk, context.DeadlineExceeded); err != nil {
		t.Fatalf("dispatched clear: %v", err)
	}
	if r := result(t, tk); r.err != nil {
		t.Fatal(r.err)
	}
}

// Before the state dump completes, a transfer is refused before anything
// goes on the wire (test_dead_connection_raises_before_wire,
// test_clear_still_dead_connection_raises_before_wire).
func TestTransferBeforeReadyIsRefused(t *testing.T) {
	c := newFakeConn()
	s := newSession(c, switcherAddr, nil, 0)
	go s.run()
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.DownloadStill(ctx, 0); !errors.Is(err, ErrNotReady) {
		t.Fatalf("download: %v", err)
	}
	if err := s.ClearStill(ctx, 4); !errors.Is(err, ErrNotReady) {
		t.Fatalf("clear: %v", err)
	}
	if n := count(c, "PLCK") + count(c, "LOCK"); n != 0 {
		t.Fatal("a lock was requested")
	}
}

// --- test_session_hygiene_atemwire.py ---------------------------------------

// test_ftdc_releases_lock_even_if_download_done_handler_raises: the queue
// head is popped and the lock released on FTDC whatever happens to the
// result; here the RLE stream doesn't decode, which atemwire logs and its
// caller times out on, and the caller gets the error.
func TestFTDCReleasesTheLockEvenIfTheFrameDoesNotDecode(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskDownload, 0, 3)
	s.enqueueTask(tk)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	// An RLE header with no count after it.
	feedCmd(t, s, "FTDa", append(append(u16be(tid), 0, 8), bytes.Repeat([]byte{0xfe}, 8)...))
	feedCmd(t, s, "FTDC", append(u16be(tid), 1, 2))
	if len(s.lane.queues[0]) != 0 || count(c, "LOCK") != 1 {
		t.Fatal("the media lock was stranded")
	}
	if r := result(t, tk); r.err == nil {
		t.Fatal("no error for a frame that doesn't decode")
	}
}

func TestFTDaWithNoTransferIsIgnored(t *testing.T) { // test_ftda_with_no_transfer_is_ignored
	s, c := laneSession(t)
	feedCmd(t, s, "FTDa", append([]byte{0, 7, 0, 4}, "DATA"...))
	if len(c.sent()) != 0 {
		t.Fatal("answered a straggler")
	}
}

func TestFTDEWithNoTransferIsIgnored(t *testing.T) { // test_ftde_with_no_transfer_is_ignored
	s, _ := laneSession(t)
	s.lane.requested = true // must be left alone
	feedCmd(t, s, "FTDE", []byte{0, 9, 1, 0})
	if !s.lane.requested {
		t.Fatal("touched the lane")
	}
}

func TestFTDEForeignTIDDoesNotFailOurTransfer(t *testing.T) { // test_ftde_foreign_tid_does_not_fail_our_transfer
	s, _ := laneSession(t)
	tk := task(taskDownload, 0, 3)
	s.enqueueTask(tk)
	grant(t, s, 0)
	feedCmd(t, s, "FTDE", []byte{0, 99, 2, 0})
	if s.lane.transfer != tk || len(s.lane.queues[0]) != 1 || !s.lane.requested || !s.lane.locks[0] {
		t.Fatalf("lane %+v", s.lane)
	}
	noResult(t, tk)
}

func TestDisconnectResetsTransferLane(t *testing.T) { // test_disconnect_resets_transfer_lane
	s, _ := laneSession(t)
	tk := task(taskDownload, 0, 3)
	s.enqueueTask(tk)
	grant(t, s, 0)
	s.lane.buffer = [][]byte{[]byte("partial")}
	s.disconnected()
	l := s.lane
	if l.transfer != nil || l.requested || len(l.queues) != 0 || l.buffer != nil || len(l.locks) != 0 {
		t.Fatalf("lane %+v", l)
	}
	if r := result(t, tk); !errors.Is(r.err, ErrDisconnected) {
		t.Fatalf("result %+v", r)
	}
	if l.nextID != 43 {
		t.Fatalf("transfer id %d: it carries on across a reconnect, as atemwire's", l.nextID)
	}
}

// --- test_teardown_releases_locks.py ---------------------------------------

func TestAHeldLockIsReleasedOnClose(t *testing.T) { // test_a_held_lock_is_released_straight_to_the_socket
	s, c := laneSession(t)
	s.lane.locks[0] = true
	s.lane.locks[1] = true      // test_every_held_store_is_released
	s.lane.locks[0xffff] = true // test_the_macro_store_is_skipped
	s.lane.locks[2] = false     // test_nothing_is_sent_when_no_lock_is_held
	s.shutdown()
	w := sentCodes(c)
	if !slices.Equal(w, []string{"LOCK", "LOCK"}) {
		t.Fatalf("wire %v", w)
	}
	if l := commands(c, "LOCK"); !bytes.Equal(l[0], []byte{0, 0, 0, 0}) || !bytes.Equal(l[1], []byte{0, 1, 0, 0}) {
		t.Fatalf("LOCK %x", l)
	}
	if sent := c.sent(); sent[len(sent)-1].flags != flagSYN {
		t.Fatal("the releases must go before the goodbye")
	}
}

func TestTeardownNeverFailsOnADeadSocket(t *testing.T) { // test_it_never_raises_on_a_dead_socket
	s, c := laneSession(t)
	s.lane.locks[0] = true
	c.writeErr = errors.New("bad file descriptor")
	s.shutdown()
	if s.lane.locks[0] {
		t.Fatal("lock belief not reset")
	}
}

// --- the bulk queue (test_lock_delivery.py, transport half) -----------------

// test_flush_sentinel_keeps_the_packet: when the bulk queue has drained,
// the packet that noticed it is still handled, after the flush. Here it is
// the budget grant (FTCD) of an upload whose earlier chunks are out.
func TestFlushKeepsThePacket(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskUpload, StoreMacros, 1)
	tk.data, tk.dataLength = make([]byte, 64), 64
	s.enqueueTask(tk)
	tid := s.lane.transfer.tid
	s.t.bulkEnabled = true // chunks went out; the drain isn't reported yet
	c.reset()
	budget := make([]byte, 12)
	copy(budget, u16be(tid))
	binary.BigEndian.PutUint16(budget[6:], 32)
	binary.BigEndian.PutUint16(budget[8:], 1)
	s.onDatagram(datagram{b: rawDatagram(0x8001, 1, flagReliable, cmdBytes("FTCD", budget)), from: switcherAddr})
	if s.fatal != nil {
		t.Fatal(s.fatal)
	}
	// The flush found data left and no budget; then the FTCD queued and
	// sent one 32-byte chunk.
	if d := commands(c, "FTDa"); len(d) != 1 || len(d[0]) != 36 || tk.offset != 32 {
		t.Fatalf("FTDa %d, cut %d", len(d), tk.offset)
	}
}

// Chunks never split an RLE run's header from its count and word: a
// chunk that would end on a header, or on a header and its count, is cut
// short (_queue_chunks).
func TestChunksDoNotSplitRLERuns(t *testing.T) {
	fe := bytes.Repeat([]byte{0xfe}, 8)
	word := func(b byte) []byte { return bytes.Repeat([]byte{b}, 8) }
	count := func(n uint64) []byte { return binary.BigEndian.AppendUint64(nil, n) }
	for _, c := range []struct {
		name string
		data []byte
		want []int // chunk lengths
	}{
		{"header at the end", slices.Concat(word(1), word(2), fe, count(4), word(3)), []int{16, 24}},
		{"header and count at the end", slices.Concat(word(1), fe, count(4), word(3)), []int{8, 24}},
		{"run whole", slices.Concat(fe, count(4), word(3), word(4)), []int{24, 8}},
	} {
		s, conn := laneSession(t)
		tk := task(taskUpload, StoreMacros, 1)
		tk.data, tk.dataLength = c.data, 999
		s.enqueueTask(tk)
		s.lane.budget = &FileTransferContinueDataField{Transfer: s.lane.transfer.tid, Size: 24, Count: 5}
		s.queueChunks()
		for range 3 {
			s.lane.bulkNext = time.Time{}
			s.bulkTrigger(false)
		}
		var got []int
		var joined []byte
		for _, d := range commands(conn, "FTDa") {
			got = append(got, len(d)-4)
			joined = append(joined, d[4:]...)
		}
		if !slices.Equal(got, c.want) || !bytes.Equal(joined, c.data) {
			t.Errorf("%s: chunks %v, want %v", c.name, got, c.want)
		}
	}
}

// queue_trigger sends at most five packets a time, and the session spaces
// the batches 3 ms apart where atemwire sleeps.
func TestBulkQueueIsPaced(t *testing.T) {
	s, c := laneSession(t)
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	for i := range 12 {
		s.t.queuePacket(cmdBytes("FTDa", []byte{0, 43, 0, 1, byte(i)}))
	}
	s.bulkTrigger(true)
	s.bulkTrigger(true) // too soon: deferred
	if n := count(c, "FTDa"); n != 5 || s.lane.bulkPending != 1 {
		t.Fatalf("%d sent, %d pending", n, s.lane.bulkPending)
	}
	now = now.Add(bulkDelay)
	s.onBulkTimer()
	if n := count(c, "FTDa"); n != 10 {
		t.Fatalf("%d sent", n)
	}
	now = now.Add(bulkDelay)
	s.bulkTrigger(true)
	if n := count(c, "FTDa"); n != 12 {
		t.Fatalf("%d sent", n)
	}
	if _, flushed, _ := s.t.queueTrigger(); !flushed {
		t.Fatal("no flush once drained")
	}
	if _, flushed, _ := s.t.queueTrigger(); flushed {
		t.Fatal("flush reported twice")
	}
}

// Under aggressive_drain a batch is ten packets, 1 ms apart, and the
// queue drains on the timer without waiting for packets from the
// switcher; the next packet after it reports the drain.
func TestAggressiveDrainEmptiesTheQueue(t *testing.T) {
	s, c := laneSession(t)
	s.t.setAggressiveDrain()
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	for i := range 25 {
		s.t.queuePacket(cmdBytes("FTDa", []byte{0, 43, 0, 1, byte(i)}))
	}
	s.bulkTrigger(true)
	if n := count(c, "FTDa"); n != 10 || s.lane.bulkPending != 1 {
		t.Fatalf("%d sent, %d pending", n, s.lane.bulkPending)
	}
	for _, want := range []int{20, 25} {
		now = now.Add(drainDelay)
		s.onBulkTimer()
		if n := count(c, "FTDa"); n != want {
			t.Fatalf("%d sent, want %d", n, want)
		}
	}
	if s.lane.bulkPending != 0 {
		t.Fatalf("%d pending with the queue empty", s.lane.bulkPending)
	}
	if _, flushed, _ := s.t.queueTrigger(); !flushed {
		t.Fatal("no flush once drained")
	}
}

// While a transfer is queued or running, the outbox doesn't space its
// packets (atemwire's _transfer_active).
func TestOutboxIsNotSpacedDuringATransfer(t *testing.T) {
	s, c := laneSession(t)
	s.pace = time.NewTimer(time.Hour)
	s.lastFlush = time.Now()
	s.queue(cmdBytes("CPgI", []byte{0, 0, 0, 1}))
	s.flush()
	if count(c, "CPgI") != 0 {
		t.Fatal("not spaced without a transfer")
	}
	s.enqueueTask(task(taskDownload, StoreMacros, 1))
	s.flush()
	if count(c, "CPgI") != 1 {
		t.Fatal("spaced during a transfer")
	}
}

// An empty upload sends FTFD with the request: the switcher sends no
// budget for it.
func TestEmptyUploadSendsFTFDInline(t *testing.T) {
	s, c := laneSession(t)
	tk := task(taskUpload, StoreMacros, 9)
	tk.name = "Empty"
	s.enqueueTask(tk)
	if c.sent()[0].data == nil || !slices.Equal(sentCodes(c), []string{"FTSD", "FTFD"}) || len(c.sent()) != 1 {
		t.Fatalf("wire %v in %d packets", sentCodes(c), len(c.sent()))
	}
	if d := commands(c, "FTSD")[0]; binary.BigEndian.Uint16(d[12:]) != 0x0300 || binary.BigEndian.Uint32(d[8:]) != 0 {
		t.Fatalf("FTSD %x", d)
	}
}
