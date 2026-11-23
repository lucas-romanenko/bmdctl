// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Upload chunks too big for the path go again smaller (bmdwire#CHUNKPR,
// test_chunks_too_big.py; WebATEM #63). An ATEM Mini Pro over a VPN at
// 80 ms got the lock request, FTSD and FTFD and not one data chunk in
// 90 s; it takes our packets in order only, so nothing after the first
// chunk counted either.

import (
	"bytes"
	"slices"
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

func TestOtherPacketsAmongThemGoAgainNowInOrder(t *testing.T) {
	clearChunkLimits(t)
	tr, c, now := stuckUpload(t, 3)
	sendReliable(t, tr, Lock{Store: 0, State: false}.Encode())
	c.reset()
	tb := timerFires(tr, now, tooBigAfter+1, true)
	if tb == nil || len(tb.sizes) != 3 {
		t.Fatalf("taken back %+v", tb)
	}
	sent := c.sent()
	last := sent[len(sent)-1]
	if last.seq != 2 || string(last.data[4:8]) != "LOCK" || last.flags&flagRetransmission != 0 {
		t.Fatalf("last sent seq %d %q flags %#x", last.seq, last.data[4:8], last.flags)
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
