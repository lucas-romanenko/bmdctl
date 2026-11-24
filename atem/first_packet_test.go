// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// A new session's reliable packets are numbered from 1 (bmdwire#13,
// bmdwire 1.14.7, 79b847f and d959fe4, test_first_packet_baseline.py).
// The ACK and the delivery cursor started at whatever reliable packet came
// first. With the switcher's first packet lost, the second set them: the
// ACK acknowledged the lost one too, so the switcher never sent it again,
// and its resend was dropped as a duplicate. When it carried the video
// mode the session had to handshake again for a fresh dump. The first
// reliable packet after the handshake was seq 1 on 10 of 10 connects to
// two Constellation HD units (2026-11-23), so the ACK and the cursor start
// there once the handshake completes.

import (
	"slices"
	"testing"
	"time"
)

const firstSession = 0x8001

// firstTransport is a transport on a clock the test moves, its SYN sent.
func firstTransport(t *testing.T) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := newTestTransport(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	return tr, c, &now
}

// feedFirst sends one datagram through receive and handle and returns the
// sequence numbers of what reached the session.
func feedFirst(t *testing.T, tr *transport, session, seq uint16, flags uint8) []uint16 {
	t.Helper()
	data := []byte("\x00\x0c\x00\x00Time\x00\x00\x00\x00")
	if flags&flagSYN != 0 {
		data = []byte{helloAccepted, 0, 0, 0, 0, 0, 0, 0}
	}
	got, err := tr.receive(rawDatagram(session, seq, flags, data), switcherAddr)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint16
	for _, p := range got {
		if _, err := tr.handle(p); err != nil {
			t.Fatal(err)
		}
		if p.flags&flagReliable != 0 {
			seqs = append(seqs, p.seq)
		}
	}
	return seqs
}

// acks is the number each ACK we sent carries.
func acks(c *fakeConn) []uint16 {
	var out []uint16
	for _, p := range c.sent() {
		if p.flags&flagACK != 0 && !p.handshake {
			out = append(out, p.ack)
		}
	}
	return out
}

func TestAfterTheHandshakeTheACKAndTheCursorStartAt1(t *testing.T) { // test_after_the_handshake_the_ack_and_the_cursor_start_at_1
	tr, _, _ := firstTransport(t)
	feedFirst(t, tr, helloSession, 0, flagSYN)
	if tr.ackNumber != 0 || !tr.deliverKnown || tr.deliverNext != 1 || !tr.rxEstablished {
		t.Fatalf("ack %d, cursor %d (known %v), established %v", tr.ackNumber, tr.deliverNext, tr.deliverKnown, tr.rxEstablished)
	}
}

func TestALostFirstPacketIsWaitedForAndDeliveredFirst(t *testing.T) { // test_a_lost_first_packet_is_waited_for_and_delivered_first
	tr, c, _ := firstTransport(t)
	feedFirst(t, tr, helloSession, 0, flagSYN)
	c.reset() // the handshake's own ACK
	var got []uint16
	for _, seq := range []uint16{2, 3} { // 1 lost on the way
		got = append(got, feedFirst(t, tr, firstSession, seq, flagReliable)...)
	}
	if len(got) != 0 {
		t.Fatalf("delivered %v ahead of the lost packet", got)
	}
	if tr.ackNumber != 0 {
		t.Fatalf("ack %d: the ACK passed the lost packet", tr.ackNumber)
	}
	if a := acks(c); !slices.Equal(a, []uint16{0}) {
		t.Fatalf("ACKs %v, want [0]", a)
	}
	got = feedFirst(t, tr, firstSession, 1, flagReliable|flagRetransmission)
	if !slices.Equal(got, []uint16{1, 2, 3}) || tr.ackNumber != 3 {
		t.Fatalf("delivered %v, ack %d", got, tr.ackNumber)
	}
}

func TestAReHandshakeStartsAt1Again(t *testing.T) { // test_a_re_handshake_starts_at_1_again
	tr, _, now := firstTransport(t)
	feedFirst(t, tr, helloSession, 0, flagSYN)
	for seq := uint16(1); seq <= 3; seq++ {
		feedFirst(t, tr, firstSession, seq, flagReliable)
	}
	*now = now.Add(silenceTimeout)
	if err := tr.timeout(); err != nil {
		t.Fatal(err)
	}
	if tr.rxEstablished || tr.deliverKnown {
		t.Fatal("the new session kept the old one's start")
	}
	feedFirst(t, tr, helloSession, 0, flagSYN)
	if tr.ackNumber != 0 || tr.deliverNext != 1 {
		t.Fatalf("ack %d, cursor %d", tr.ackNumber, tr.deliverNext)
	}
	if got := feedFirst(t, tr, firstSession+1, 1, flagReliable); !slices.Equal(got, []uint16{1}) {
		t.Fatalf("delivered %v", got)
	}
}

// d959fe4: a packet 1 that has not come firstPacketWait after the
// handshake is not waited for: the next packet to come starts delivery
// from the first that did, and the ACK moves up to it.
func TestPacket1IsNotWaitedForForEver(t *testing.T) {
	tr, c, now := firstTransport(t)
	feedFirst(t, tr, helloSession, 0, flagSYN)
	for _, seq := range []uint16{2, 3} {
		if got := feedFirst(t, tr, firstSession, seq, flagReliable); len(got) != 0 {
			t.Fatalf("delivered %v", got)
		}
	}
	*now = now.Add(firstPacketWait - time.Millisecond)
	if got := feedFirst(t, tr, firstSession, 4, flagReliable); len(got) != 0 {
		t.Fatalf("delivered %v before firstPacketWait", got)
	}
	*now = now.Add(time.Millisecond)
	c.reset()
	got := feedFirst(t, tr, firstSession, 5, flagReliable)
	if !slices.Equal(got, []uint16{2, 3, 4, 5}) || tr.firstRX != 2 {
		t.Fatalf("delivered %v, first %d", got, tr.firstRX)
	}
	if tr.ackNumber != 5 || !slices.Contains(acks(c), 5) {
		t.Fatalf("ack %d, sent %v", tr.ackNumber, acks(c))
	}
	// From here on it is an ordinary session: packet 1, late, is behind
	// the cursor and dropped; 6 is delivered.
	if got := feedFirst(t, tr, firstSession, 1, flagReliable|flagRetransmission); len(got) != 0 {
		t.Fatalf("delivered %v", got)
	}
	if got := feedFirst(t, tr, firstSession, 6, flagReliable); !slices.Equal(got, []uint16{6}) {
		t.Fatalf("delivered %v", got)
	}
}
