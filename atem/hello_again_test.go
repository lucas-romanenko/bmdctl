// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Our hello goes again after 1 s without an answer, and the goodbye goes
// three times (bmdwire#103, 32b8393; test_hello_again.py, one for one).
// The hello used to go only on the silence timeout, every 5 s: a hello or
// its answer lost on a lossy link cost 5 s, and after a pulled cable or a
// reboot the session came back up to 5 s after the switcher could answer.
// One goodbye lost left the session on the switcher until it reaped it.

import (
	"testing"
	"time"
)

// helloTransport is the Python tests' udp fixture: a transport whose
// clock the test moves.
func helloTransport(t *testing.T) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := newTestTransport(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	return tr, c, &now
}

func sentHellos(c *fakeConn, op byte) []packet {
	var out []packet
	for _, p := range c.sent() {
		if p.flags&flagSYN != 0 && len(p.data) > 0 && p.data[0] == op {
			out = append(out, p)
		}
	}
	return out
}

func TestAnUnansweredHelloGoesAgainAfterASecondThenEveryTwo(t *testing.T) { // test_an_unanswered_hello_goes_again_after_a_second_then_every_two
	tr, c, now := helloTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	if n := len(sentHellos(c, helloConnect)); n != 1 {
		t.Fatalf("%d hellos", n)
	}
	*now = now.Add(helloRetry - 10*time.Millisecond)
	if wait, ok := tr.checkHello(); !ok || wait != 10*time.Millisecond {
		t.Fatalf("next in %v, %v", wait, ok)
	}
	if n := len(sentHellos(c, helloConnect)); n != 1 {
		t.Fatalf("%d hellos", n)
	}
	*now = now.Add(10 * time.Millisecond)
	tr.checkHello()
	if n := len(sentHellos(c, helloConnect)); n != 2 {
		t.Fatalf("%d hellos: not again after helloRetry", n)
	}
	*now = now.Add(helloRetryMax)
	tr.checkHello()
	h := sentHellos(c, helloConnect)
	if len(h) != 3 {
		t.Fatalf("%d hellos: not again after helloRetryMax", len(h))
	}
	for _, p := range h {
		if p.session != helloSession || p.seq != h[0].seq {
			t.Fatalf("hello %+v: not the same hello", p)
		}
	}
	if tr.state != stateSYNSent || tr.hellosAgain != 2 {
		t.Fatalf("state %v, %d again", tr.state, tr.hellosAgain)
	}
}

func TestTheSilenceWindowIsNotMovedByAHelloAgain(t *testing.T) { // test_the_silence_window_is_not_moved_by_a_hello_again
	tr, _, now := helloTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	heard := tr.lastRX
	*now = now.Add(helloRetry)
	tr.checkHello()
	if !tr.lastRX.Equal(heard) {
		t.Fatal("the silence window moved")
	}
}

func TestAnAnsweredHelloIsNotSentAgain(t *testing.T) { // test_an_answered_hello_is_not_sent_again
	tr, c, now := helloTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	tr.state = stateEstablished // the switcher answered
	*now = now.Add(10 * time.Second)
	if _, ok := tr.checkHello(); ok {
		t.Fatal("a hello is due")
	}
	if n := len(sentHellos(c, helloConnect)); n != 1 {
		t.Fatalf("%d hellos", n)
	}
}

func TestTheGoodbyeGoesThreeTimes(t *testing.T) { // test_the_goodbye_goes_three_times
	tr, c := retransmitTransport(t)
	tr.localSeq = 5
	tr.state = stateEstablished
	tr.closeSession()
	byes := sentHellos(c, helloClosing)
	if goodbyes != 3 || len(byes) != 3 {
		t.Fatalf("%d goodbyes", len(byes))
	}
	for _, p := range byes {
		if p.session != 0x8001 {
			t.Fatalf("goodbye under %#x", p.session)
		}
	}
	tr.closeSession()
	if n := len(sentHellos(c, helloClosing)); n != 3 {
		t.Fatalf("%d goodbyes: once closed, nothing more", n)
	}
}
