// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Upload chunks too big for the path go again smaller (bmdwire#95,
// test_chunks_too_big.py; WebATEM #63). An ATEM Mini Pro over a VPN at
// 80 ms got the lock request, FTSD and FTFD and not one data chunk in
// 90 s; it takes our packets in order only, so nothing after the first
// chunk counted either.

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
)

func clearChunkLimits(t *testing.T) {
	t.Helper()
	chunkLimitsMu.Lock()
	chunkLimits = map[string]int{}
	chunkLimitsMu.Unlock()
	t.Cleanup(func() {
		chunkLimitsMu.Lock()
		chunkLimits = map[string]int{}
		chunkLimitsMu.Unlock()
	})
}

func ftda(n byte, size int) []byte {
	return TransferData{Transfer: 7, Data: bytes.Repeat([]byte{n}, size)}.Encode()
}

// stuckUpload is an FTFD the switcher acknowledged, then chunks it never
// gets.
func stuckUpload(t *testing.T, chunks int) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := established(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	sendReliable(t, tr, append([]byte("\x00\xdcFTFD"), make([]byte, 212)...))
	now = now.Add(80 * time.Millisecond)
	tr.onACK(1, true)
	for n := range chunks {
		sendReliable(t, tr, ftda(byte(n), 1392))
	}
	return tr, c, &now
}

func timerFires(tr *transport, now *time.Time, times int, answering bool) *takenBack {
	var tb *takenBack
	for range times {
		if !tr.resendDue.IsZero() {
			*now = tr.resendDue
		} else {
			*now = now.Add(time.Second)
		}
		if answering {
			tr.lastHeard = *now // its Time packets keep coming
		}
		tb = tr.resendUnacked()
	}
	return tb
}

func TestChunksThatNeverGetThroughAreTakenBack(t *testing.T) {
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	if tb := timerFires(tr, now, tooBigAfter, true); tb != nil {
		t.Fatal("taken back before tooBigAfter resends")
	}
	tb := timerFires(tr, now, 1, true)
	if tb == nil || tb.transfer != 7 || tb.limit != 1224 || !slices.Equal(tb.sizes, slices.Repeat([]int{1392}, 10)) {
		t.Fatalf("taken back %+v", tb)
	}
	if tr.localSeq != 1 || len(tr.retransmitBuf) != 1 {
		t.Fatalf("localSeq %d, %d buffered: their sequence numbers are free again", tr.localSeq, len(tr.retransmitBuf))
	}
	if tr.maxChunk != 1224 || chunkLimit(switcherAddr) != 1224 {
		t.Fatalf("maxChunk %d, kept %d", tr.maxChunk, chunkLimit(switcherAddr))
	}
}

func TestALostChunkOnAQuietLinkIsOnlyResent(t *testing.T) {
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	if tb := timerFires(tr, now, tooBigAfter+2, false); tb != nil || tr.maxChunk != 0 {
		t.Fatalf("taken back %+v with nothing from the switcher", tb)
	}
}

func TestAnAcknowledgementStartsTheCountAgain(t *testing.T) {
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	timerFires(tr, now, tooBigAfter, true)
	tr.onACK(2, true) // the first chunk got there
	if tb := timerFires(tr, now, tooBigAfter, true); tb != nil {
		t.Fatalf("taken back %+v", tb)
	}
}

func TestOnlyDataChunksAreMadeSmaller(t *testing.T) {
	clearChunkLimits(t)
	tr, _ := established(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	tr.onACK(1, true)
	sendReliable(t, tr, append([]byte("\x00\x0cLOCK"), 0, 0, 0, 0))
	if tb := timerFires(tr, &now, tooBigAfter+2, true); tb != nil {
		t.Fatalf("taken back %+v", tb)
	}
}

func TestASmallPacketAfterTheStuckChunksMeansANewSession(t *testing.T) { // test_a_small_packet_after_the_stuck_chunks_means_a_new_session
	// The lock release after the chunks is small enough to have got
	// through, and the units keep what comes after a gap (the hardware
	// bench, 2026-11-24): its sequence number cannot be used again for
	// other data. A new session, the size learnt kept (bmdwire#100,
	// e4119f7). Until then the chunks were taken back and the release
	// went again at once under the stuck chunk's number.
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 3)
	sendReliable(t, tr, Lock{Store: 0, State: false}.Encode())
	if tb := timerFires(tr, now, tooBigAfter+1, true); tb != nil {
		t.Fatalf("taken back %+v under the same numbers", tb)
	}
	if !strings.Contains(tr.newSessionAsked, "too big") {
		t.Fatalf("new session asked for: %q", tr.newSessionAsked)
	}
	if tr.maxChunk != 1224 || chunkLimit(switcherAddr) != 1224 {
		t.Fatalf("maxChunk %d, kept %d", tr.maxChunk, chunkLimit(switcherAddr))
	}
}

// request is the switcher asking for our packet seq again.
func request(t *testing.T, tr *transport, seq uint16) {
	t.Helper()
	b := rawDatagram(tr.sessionID, 0, flagRequestRetransmission, nil)
	b[6], b[7] = byte(seq>>8), byte(seq)
	if _, err := tr.receive(b, switcherAddr); err != nil {
		t.Fatal(err)
	}
}

func TestTheSwitcherAskingAgainForTheStuckChunkFindsTheSizeFast(t *testing.T) { // test_the_switcher_asking_again_for_the_stuck_chunk_finds_the_size_fast
	// A switcher that asks (a Mini Pro, a Constellation): its requests for
	// the chunk it lacks count as resends it answered, without waiting
	// for our backed-off timer (a macro upload over a 1,280 VPN: 10 s
	// before; bmdwire#100, e4119f7).
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	start := *now
	for range tooBigAfter + 1 {
		*now = now.Add(100 * time.Millisecond)
		request(t, tr, 2)
	}
	if len(tr.takenBackQueue) != 1 {
		t.Fatalf("%d taken back", len(tr.takenBackQueue))
	}
	if tb := tr.takenBackQueue[0]; tb.limit != 1224 || len(tb.sizes) != 10 || now.Sub(start) >= time.Second {
		t.Fatalf("taken back %+v after %v", tb, now.Sub(start))
	}
}

func TestRequestsCloserThanResendMinCountOnce(t *testing.T) { // test_requests_closer_than_resend_min_count_once
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	for range 10 {
		*now = now.Add(resendMin / 4)
		request(t, tr, 2)
	}
	if len(tr.takenBackQueue) != 0 {
		t.Fatalf("taken back %+v", tr.takenBackQueue)
	}
}

func TestUploadChunksGoAgainFromWhereTheyStarted(t *testing.T) {
	clearChunkLimits(t)
	for _, tc := range []struct {
		name string
		data []byte
		back int
	}{
		{"all of a budget", ascData(), 10},
		{"part of a budget", ascData(), 4},
		// A macro: the last chunk short. The offset runs past the end
		// after it; the first 80 bytes went missing when it was not
		// clamped.
		{"a last short chunk", slices.Repeat(func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(), 16), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c := laneSession(t)
			tk := uploadTask(4)
			tk.data = tc.data
			tk.dataLength, tk.size = len(tk.data), len(tk.data)
			s.enqueueTask(tk)
			grant(t, s, 0)
			feedBudget(t, s, 768)
			first := commands(c, "FTDa")
			var sizes []int
			for _, d := range first[len(first)-tc.back:] {
				sizes = append(sizes, len(d)-4)
			}
			s.t.maxChunk = 1224
			s.chunksTakenBack(&takenBack{transfer: s.lane.transfer.tid, sizes: sizes, limit: 1224})
			all := commands(c, "FTDa")
			var got []byte
			for _, d := range all[:len(first)-tc.back] {
				got = append(got, d[4:]...)
			}
			for _, d := range all[len(first):] {
				if len(d)-4 > 1224 {
					t.Fatalf("a chunk of %d bytes", len(d)-4)
				}
				got = append(got, d[4:]...)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("%d bytes, not the data", len(got))
			}
		})
	}
}

func TestNeverMoreChunksThanTheBudget(t *testing.T) {
	clearChunkLimits(t)
	s, c := laneSession(t)
	s.enqueueTask(uploadTask(4))
	grant(t, s, 0)
	feedBudget(t, s, 10)
	s.t.maxChunk = 1224
	s.chunksTakenBack(&takenBack{transfer: s.lane.transfer.tid, sizes: slices.Repeat([]int{1392}, 10), limit: 1224})
	if n := len(commands(c, "FTDa")); n != 20 || s.lane.transfer.offset != 1224*10 {
		t.Fatalf("%d chunks, offset %d", n, s.lane.transfer.offset)
	}
}

func TestTheNextSessionToThatSwitcherStartsSmall(t *testing.T) {
	clearChunkLimits(t)
	setChunkLimit(switcherAddr, 1224)
	s, c := laneSession(t)
	tk := uploadTask(4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	feedBudget(t, s, 768)
	var got []byte
	for i, d := range commands(c, "FTDa") {
		if all := commands(c, "FTDa"); i < len(all)-1 && len(d)-4 != 1224 {
			t.Fatalf("chunk %d of %d bytes", i, len(d)-4)
		}
		got = append(got, d[4:]...)
	}
	if !bytes.Equal(got, tk.data) {
		t.Fatalf("%d bytes, not the data", len(got))
	}
}

// TestAnUploadOverAPathThatDropsBigDatagrams: the whole upload, the
// switcher modelled as taking our packets in order only, behind a path
// that drops datagrams over 1,400 bytes.
func TestAnUploadOverAPathThatDropsBigDatagrams(t *testing.T) {
	clearChunkLimits(t)
	s, c := laneSession(t)
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	tk := uploadTask(4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	have, read, left := 0, 0, 4
	s.t.onACK(uint16(have), true)
	feedBudget(t, s, 4)
	var got []byte
	for range 200 {
		c.mu.Lock()
		writes := slices.Clone(c.writes[read:])
		read = len(c.writes)
		c.mu.Unlock()
		for _, w := range writes {
			if len(w.b) > 1400 {
				continue // dropped on the way
			}
			p, err := parsePacket(w.b)
			if err != nil || p.flags&flagReliable == 0 || int(p.seq) != (have+1)&seqMask {
				continue // in order only
			}
			have = int(p.seq)
			if len(p.data) >= 12 && string(p.data[4:8]) == "FTDa" {
				got = append(got, p.data[12:]...)
				left--
			}
		}
		now = now.Add(80 * time.Millisecond)
		s.t.lastHeard = now
		s.t.onACK(uint16(have), true)
		if len(got) >= len(tk.data) {
			break
		}
		if left == 0 {
			left = 4
			feedBudget(t, s, 4)
		}
		if !s.t.resendDue.IsZero() && !now.Before(s.t.resendDue) {
			if tb := s.t.resendUnacked(); tb != nil {
				s.chunksTakenBack(tb)
			}
		}
	}
	if !bytes.Equal(got, tk.data) {
		t.Fatalf("%d of %d bytes reached the switcher", len(got), len(tk.data))
	}
}

// A link down for a moment is not a path too small (bmdwire#97,
// test_chunks_too_big.py).

var rxSeq uint16

// timePacket is one of the switcher's Time packets (every 40 ms), through
// the receive path.
func timePacket(t *testing.T, tr *transport) {
	t.Helper()
	rxSeq++
	data := append([]byte("\x00\x10\x00\x00Time"), make([]byte, 8)...)
	if _, err := tr.receive(rawDatagram(tr.sessionID, rxSeq, flagReliable, data), switcherAddr); err != nil {
		t.Fatal(err)
	}
}

// heardUntil is the switcher's Time packets, 40 ms apart, up to until.
func heardUntil(t *testing.T, tr *transport, now *time.Time, until time.Time) {
	t.Helper()
	for !now.Add(40 * time.Millisecond).After(until) {
		*now = now.Add(40 * time.Millisecond)
		timePacket(t, tr)
	}
	*now = until
}

func timerFiresHeard(t *testing.T, tr *transport, now *time.Time, times int, heard bool) *takenBack {
	t.Helper()
	var tb *takenBack
	for range times {
		due := now.Add(time.Second)
		if !tr.resendDue.IsZero() {
			due = tr.resendDue
		}
		if heard {
			heardUntil(t, tr, now, due)
		}
		*now = due
		if got := tr.resendUnacked(); got != nil {
			tb = got
		}
	}
	return tb
}

// stuckUploadOnALAN is stuckUpload on a LAN: the ACK in 2 ms, so the
// resend timer runs at its minimum and doubles (50, 100, 200, 400 ms).
func stuckUploadOnALAN(t *testing.T, chunks int) (*transport, *time.Time) {
	t.Helper()
	tr, _ := established(t)
	tr.sessionID = 0x8001
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	timePacket(t, tr)
	sendReliable(t, tr, append([]byte("\x00\xdcFTFD"), make([]byte, 212)...))
	now = now.Add(2 * time.Millisecond)
	tr.onACK(1, true)
	for n := range chunks {
		sendReliable(t, tr, ftda(byte(n), 1392))
	}
	return tr, &now
}

func TestChunksStuckWhileTheCableIsOutAreNotTakenBack(t *testing.T) {
	// The cable out for 3 s mid-upload: four resends take 0.75 s on a LAN,
	// under resendMax, and v0.3.1 took the chunks back as too big; their
	// sequence numbers went again with other data the switcher may have
	// had already, and the still arrived with the wrong MD5.
	clearChunkLimits(t)
	tr, now := stuckUploadOnALAN(t, 10)
	out := *now
	if tb := timerFiresHeard(t, tr, now, tooBigAfter+3, false); tb != nil {
		t.Fatalf("taken back %+v with the cable out: resends nobody answered do not count", tb)
	}
	if now.Sub(out) >= 3*time.Second {
		t.Fatalf("%v: not the case v0.3.1 got wrong", now.Sub(out))
	}
	// The cable back in: heard again, and the next resend reaches the
	// switcher, which acknowledges it.
	heardUntil(t, tr, now, out.Add(3*time.Second))
	if tb := timerFiresHeard(t, tr, now, 1, true); tb != nil {
		t.Fatalf("taken back %+v after the cable came back", tb)
	}
	tr.onACK(11, true)
	if tr.maxChunk != 0 || tr.localSeq != 11 {
		t.Fatalf("maxChunk %d, localSeq %d: no sequence number used twice", tr.maxChunk, tr.localSeq)
	}
}

func TestResendsIntoSilenceDoNotCountTowardsTooBig(t *testing.T) {
	clearChunkLimits(t)
	tr, now := stuckUploadOnALAN(t, 10)
	timerFiresHeard(t, tr, now, 6, false)
	if tb := timerFiresHeard(t, tr, now, tooBigAfter-1, true); tb != nil {
		t.Fatalf("taken back %+v counting resends into silence", tb)
	}
	if tb := timerFiresHeard(t, tr, now, 1, true); tb == nil || tb.limit != 1224 {
		t.Fatalf("taken back %+v, want limit 1224", tb)
	}
}

func TestASwitcherThatSendsEveryHalfSecondIsHeard(t *testing.T) {
	// WebATEM #63 on v0.3.2's rule (bmdwire 1.14.8): a switcher that sends
	// less often than a Constellation's every 40 ms never passed "no gap
	// over 250 ms", and its uploads hung (bmdwire#100).
	clearChunkLimits(t)
	tr, _, now := stuckUpload(t, 10)
	var tb *takenBack
	for range tooBigAfter + 1 {
		due := tr.resendDue
		for !now.Add(500 * time.Millisecond).After(due) {
			*now = now.Add(500 * time.Millisecond)
			timePacket(t, tr)
		}
		*now = due.Add(500 * time.Millisecond) // heard just after each resend
		timePacket(t, tr)
		if got := tr.resendUnacked(); got != nil {
			tb = got
		}
	}
	if tb == nil || tb.limit != 1224 {
		t.Fatalf("taken back %+v, want limit 1224", tb)
	}
}

func TestChunksTooBigOnALANAreStillTakenBack(t *testing.T) {
	clearChunkLimits(t)
	tr, now := stuckUploadOnALAN(t, 10)
	if tb := timerFiresHeard(t, tr, now, tooBigAfter+1, true); tb == nil || tb.limit != 1224 {
		t.Fatalf("taken back %+v, want limit 1224", tb)
	}
}

func TestMoreChunksTakenBackThanTheSendQueueHoldsAllGoAgain(t *testing.T) { // test_more_chunks_taken_back_than_the_send_queue_holds_all_go_again
	// Three budgets of 768 in flight when the take-back came (a switcher
	// granting them back to back): 2,304 chunks to send again, more than
	// the bulk queue (bulkQueueMax, 1,024) holds. Queued at once, the
	// oldest were dropped and the still came out short (bmdwire#100,
	// e151696).
	clearChunkLimits(t)
	s, c := laneSession(t)
	tk := uploadTask(0)
	tk.data = make([]byte, ascChunk*2304)
	for i := range tk.data {
		tk.data[i] = byte(i * 7 % 251)
	}
	tk.dataLength, tk.size = len(tk.data), len(tk.data)
	s.enqueueTask(tk)
	grant(t, s, 0)
	for range 3 {
		feedBudget(t, s, 768)
	}
	first := commands(c, "FTDa")
	if len(first) != 2304 {
		t.Fatalf("%d chunks sent", len(first))
	}
	s.t.maxChunk = 1224
	s.chunksTakenBack(&takenBack{transfer: s.lane.transfer.tid, sizes: slices.Repeat([]int{ascChunk}, 2304), limit: 1224})
	var again []byte
	for _, d := range commands(c, "FTDa")[2304:] {
		again = append(again, d[4:]...)
	}
	// As many chunks as the budget owed, now of 1,224 bytes, in order
	// from where the first one started; the rest with the next budget.
	if len(again) != 2304*1224 || !bytes.Equal(again, tk.data[:len(again)]) {
		t.Fatalf("%d bytes sent again, not the data from its start: some lost to the queue", len(again))
	}
}

// The session does what the transport asks: the new session starts, the
// upload carried over to it, to go again in the smaller chunks.
func TestANewSessionAskedForCarriesTheUploadOver(t *testing.T) {
	clearChunkLimits(t)
	s, c := laneSession(t)
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	tk := uploadTask(4)
	s.enqueueTask(tk)
	grant(t, s, 0)
	feedBudget(t, s, 3)
	s.t.onACK(uint16(s.t.localSeq-3), true)                     // all but the chunks got there
	sendReliable(t, s.t, Lock{Store: 0, State: false}.Encode()) // small, after them
	c.reset()
	timerFires(s.t, &now, tooBigAfter+1, true)
	s.transportAsks()
	if h, g := hellos(c); h != 1 || g != 1 {
		t.Fatalf("%d hellos, %d goodbyes: no new session", h, g)
	}
	if len(s.lane.carried) != 1 || s.lane.carried[0] != tk || s.t.maxChunk != 1224 {
		t.Fatalf("carried %v, maxChunk %d", s.lane.carried, s.t.maxChunk)
	}
	noResult(t, tk)
}
