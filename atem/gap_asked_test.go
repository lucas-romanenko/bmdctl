// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// A packet of the switcher's lost is asked for, and a request from the
// switcher says what it has (bmdwire#100, 9824cab, test_gap_asked_for.py).
// The hardware bench, 2026-11-24 (Constellation HD, fw 2.30): the cable
// out for 3 s in the middle of a still upload on a clean path. Everything
// we had sent was acknowledged afterwards, and the upload sat at 47 %
// until the caller gave up: the switcher's next budget (FTCD) had been
// lost in the dropout, the packets after it were parked behind the gap,
// and the switcher never sent it again on its own. We now ask for it.

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

// gapTransport is an established transport on a clock the test moves.
func gapTransport(t *testing.T) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := established(t)
	now := time.Unix(100, 0)
	tr.now = func() time.Time { return now }
	return tr, c, &now
}

// rxTime is one of the switcher's reliable packets (a Time packet) through
// the receive path.
func rxTime(t *testing.T, tr *transport, seq uint16) {
	t.Helper()
	data := append([]byte("\x00\x0c\x00\x00Time"), 0, 0, 0, 0)
	if _, err := tr.receive(rawDatagram(tr.sessionID, seq, flagReliable, data), switcherAddr); err != nil {
		t.Fatal(err)
	}
}

// requests is the sequence each retransmission request we sent asks for.
func requests(c *fakeConn) []uint16 {
	var out []uint16
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.writes {
		if binary.BigEndian.Uint16(w.b)>>11&flagRequestRetransmission != 0 {
			out = append(out, binary.BigEndian.Uint16(w.b[6:]))
		}
	}
	return out
}

func TestAGapThatStaysOpenIsAskedForAndAgainUntilItFills(t *testing.T) { // test_a_gap_that_stays_open_is_asked_for_and_again_until_it_fills
	tr, c, now := gapTransport(t)
	for _, seq := range []uint16{1, 2, 4, 5, 6} { // 3 lost in a dropout
		rxTime(t, tr, seq)
	}
	if due := tr.checkGap(); !due.Equal(now.Add(gapAskAfter)) {
		t.Fatalf("due %v, want %v from now", due.Sub(*now), gapAskAfter)
	}
	if got := requests(c); len(got) != 0 {
		t.Fatalf("asked %v before gapAskAfter", got)
	}
	*now = now.Add(gapAskAfter)
	tr.checkGap()
	if got := requests(c); !slices.Equal(got, []uint16{3}) {
		t.Fatalf("asked %v, want the first missing one, 3", got)
	}
	*now = now.Add(gapAskEvery)
	tr.checkGap()
	if got := requests(c); !slices.Equal(got, []uint16{3, 3}) {
		t.Fatalf("asked %v, want 3 again while the gap stays open", got)
	}
	rxTime(t, tr, 3) // it came
	*now = now.Add(10 * time.Second)
	if due := tr.checkGap(); !due.IsZero() {
		t.Fatalf("still due at %v with the gap filled", due)
	}
	if got := requests(c); !slices.Equal(got, []uint16{3, 3}) {
		t.Fatalf("asked %v after it came", got)
	}
}

func TestARequestOfOursIsNotOneOfOurReliablePackets(t *testing.T) { // test_a_request_of_ours_is_not_one_of_our_reliable_packets
	tr, c, now := gapTransport(t)
	rxTime(t, tr, 1)
	rxTime(t, tr, 3)
	*now = now.Add(gapAskAfter)
	tr.checkGap()
	if tr.localSeq != 0 || len(tr.retransmitBuf) != 0 {
		t.Fatalf("localSeq %d, %d buffered", tr.localSeq, len(tr.retransmitBuf))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.writes {
		if binary.BigEndian.Uint16(w.b)>>11&flagRequestRetransmission == 0 {
			continue
		}
		if len(w.b) != headerLen || binary.BigEndian.Uint16(w.b[2:]) != 0x8001 || binary.BigEndian.Uint16(w.b[10:]) != 0 {
			t.Fatalf("request % x", w.b)
		}
		return
	}
	t.Fatal("nothing asked")
}

func TestNoGapNothingAsked(t *testing.T) { // test_no_gap_nothing_asked
	tr, c, now := gapTransport(t)
	for _, seq := range []uint16{1, 2, 3} {
		rxTime(t, tr, seq)
	}
	*now = now.Add(5 * time.Second)
	if due := tr.checkGap(); !due.IsZero() || len(requests(c)) != 0 {
		t.Fatalf("due %v, asked %v", due, requests(c))
	}
}

func TestTheSwitchersRequestSaysWhatItHas(t *testing.T) { // test_the_switchers_request_says_what_it_has
	// Our packets 1-40 sent; its ACKs for 11-30 lost in a dropout, then
	// it asks for 31: it has 1-30, so those count as acknowledged and
	// only 31 on go again (nothing it has is resent or taken back as too
	// big).
	tr, c, _ := gapTransport(t)
	for n := range 40 {
		sendReliable(t, tr, append([]byte("\x00\x0c\x00\x00TEST"), 0, 0, 0, byte(n)))
	}
	tr.onACK(10, true)
	c.reset()
	req := rawDatagram(tr.sessionID, 0, flagRequestRetransmission, nil)
	binary.BigEndian.PutUint16(req[6:], 31)
	if _, err := tr.receive(req, switcherAddr); err != nil {
		t.Fatal(err)
	}
	if tr.ackedThrough != 30 {
		t.Fatalf("acked through %d, want 30", tr.ackedThrough)
	}
	var seqs []uint16
	for _, p := range c.sent() {
		if p.flags&flagReliable != 0 {
			seqs = append(seqs, p.seq)
		}
	}
	want := make([]uint16, 0, 10)
	for s := uint16(31); s <= 40; s++ {
		want = append(want, s)
	}
	if !slices.Equal(seqs, want) {
		t.Fatalf("resent %v, want %v", seqs, want)
	}
}
