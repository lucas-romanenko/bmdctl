// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// How long Dial waits, and when it gives up: ported from bmdwire v1.14.5
// atemwire/tests test_no_free_connection.py (bmdwire#75, #77),
// test_hello_answer_not_progress.py (bmdwire#91) and
// test_connect_gives_up_windows.py (bmdwire#88). The limits are shortened
// here: a 100 ms silence tick, a 100 ms stall and a 200 ms hello limit
// stand for 5 s, 5 s and 10 s.

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	testTick  = 100 * time.Millisecond
	testStall = 100 * time.Millisecond
	testHello = 200 * time.Millisecond
)

func startLimitedDial(c *fakeConn) (chan dialResult, time.Time) {
	ch := make(chan dialResult, 1)
	start := time.Now()
	go func() {
		d := &Dialer{Conn: c, silence: testTick, stall: testStall, hello: testHello}
		s, err := d.Dial(context.Background(), "127.0.0.1")
		ch <- dialResult{s, err}
	}()
	return ch, start
}

func dialOutcome(t *testing.T, ch chan dialResult) dialResult {
	t.Helper()
	select {
	case r := <-ch:
		if r.s != nil {
			t.Cleanup(func() { r.s.Close() })
		}
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Dial did not return")
	}
	return dialResult{}
}

// test_a_full_switcher_is_named_at_once: a switcher with every slot taken
// answers the hello 0x03 within a millisecond. Dial says so at once, where
// it waited out its timeout as for a switcher that is off; the answer is
// not ACKed.
func TestAFullSwitcherIsNamedAtOnce(t *testing.T) {
	c := newFakeConn()
	res, start := startLimitedDial(c)
	sw := newSwitcher(t, c)
	sw.expect("SYN", isSYN(helloConnect))
	c.in <- fakeDatagram{b: rawDatagram(helloSession, 0, flagSYN, []byte{helloNoRoom, 0, 0, 0, 0, 0, 0, 0}), from: switcherAddr}
	r := dialOutcome(t, res)
	if !errors.Is(r.err, ErrNoFreeConnection) {
		t.Fatalf("err %v", r.err)
	}
	if took := time.Since(start); took >= testTick {
		t.Fatalf("took %v: not at once", took)
	}
	for _, p := range c.sent() {
		if p.flags&flagACK != 0 {
			t.Fatalf("the refusal was ACKed: %+v", p)
		}
	}
}

// answerHellos plays a switcher that welcomes every hello at once and then
// sends nothing, until stop is closed.
func answerHellos(t *testing.T, c *fakeConn, stop <-chan struct{}) {
	go func() {
		read := 0
		for {
			c.mu.Lock()
			writes := c.writes[read:]
			read = len(c.writes)
			c.mu.Unlock()
			for _, w := range writes {
				if p, err := parsePacket(w.b); err == nil && isSYN(helloConnect)(p) {
					c.in <- fakeDatagram{b: rawDatagram(helloSession, 0, flagSYN, []byte{helloAccepted, 0, 0, 0, 0, 0, 0, 0}), from: switcherAddr}
				}
			}
			select {
			case <-c.wrote:
			case <-stop:
				return
			}
		}
	}()
}

// test_a_switcher_that_goes_quiet_fails_after_the_stall_not_the_cap and
// test_on_the_wire_the_answer_to_the_re_hello_does_not_restart_the_stall:
// a switcher that welcomes every hello at once and then sends nothing is
// given up on at the first silence tick. Its answer to the re-sent hello
// is not progress (bmdwire#91).
func TestASwitcherThatGoesQuietFailsAtTheFirstTick(t *testing.T) {
	for range 10 {
		c := newFakeConn()
		stop := make(chan struct{})
		answerHellos(t, c, stop)
		res, start := startLimitedDial(c)
		r := dialOutcome(t, res)
		took := time.Since(start)
		close(stop)
		if !errors.Is(r.err, ErrStalled) {
			t.Fatalf("err %v", r.err)
		}
		if took < testStall || took >= testStall+testTick {
			t.Fatalf("took %v, want the first tick (%v)", took, testStall)
		}
	}
}

// test_a_quiet_switcher_answering_each_re_hello_fails_at_the_first_tick,
// without the race: the tick is judged before the hello goes, from how
// long the switcher had been silent when it fired. atemwire's wait_ready
// reads silence_tick for it; here the run goroutine does both in turn.
func TestTheTickIsJudgedBeforeTheReHello(t *testing.T) {
	c := newFakeConn()
	s := newSession(c, switcherAddr, nil, testTick)
	s.stall, s.hello = testStall, testHello
	now := time.Unix(1000, 0)
	s.t.now = func() time.Time { return now }
	s.dialStart = now
	if err := s.t.connect(); err != nil {
		t.Fatal(err)
	}
	s.t.lastHeard = now // the welcome
	s.t.state, s.t.sessionID = stateEstablished, 0x8001
	now = now.Add(testStall)
	c.reset()
	s.onSilence()
	if !errors.Is(s.fatal, ErrStalled) {
		t.Fatalf("fatal %v", s.fatal)
	}
	for _, p := range c.sent() {
		if isSYN(helloConnect)(p) {
			t.Fatal("re-sent the hello: its answer would restart the stall clock")
		}
	}
}

// test_no_answer_fails_at_the_second_tick: until the switcher has answered
// at all the limit is the hello limit, two ticks.
func TestNoAnswerFailsAtTheSecondTick(t *testing.T) {
	c := newFakeConn()
	res, start := startLimitedDial(c)
	r := dialOutcome(t, res)
	took := time.Since(start)
	if !errors.Is(r.err, ErrNoAnswer) {
		t.Fatalf("err %v", r.err)
	}
	if took < testHello || took >= testHello+testTick {
		t.Fatalf("took %v, want the hello limit (%v)", took, testHello)
	}
	hellos := 0
	for _, p := range c.sent() {
		if isSYN(helloConnect)(p) {
			hellos++
		}
	}
	if hellos != 2 {
		t.Fatalf("%d hellos, want 2", hellos)
	}
}

// test_one_lost_hello_is_not_a_failed_connect (bmdwire#77): the first
// hello unanswered, the second answered and its dump sent.
func TestOneLostHelloIsNotAFailedConnect(t *testing.T) {
	c := newFakeConn()
	res, _ := startLimitedDial(c)
	sw := newSwitcher(t, c)
	sw.expect("the first hello", isSYN(helloConnect)) // lost
	sw.handshake()
	sw.dump()
	if r := dialOutcome(t, res); r.err != nil {
		t.Fatal(r.err)
	}
}

// test_a_slow_dump_that_keeps_arriving_is_waited_for: longer than the
// stall limit in all, but never that long without a packet.
func TestASlowDumpThatKeepsArrivingIsWaitedFor(t *testing.T) {
	c := newFakeConn()
	res, start := startLimitedDial(c)
	sw := newSwitcher(t, c)
	sw.handshake()
	for range 8 {
		time.Sleep(testStall / 2)
		sw.reliable(cmdBytes("Time", []byte{0, 0, 0, 0}))
	}
	sw.dump()
	if r := dialOutcome(t, res); r.err != nil {
		t.Fatal(r.err)
	}
	if took := time.Since(start); took < 3*testStall {
		t.Fatalf("took %v", took)
	}
}

// test_a_dump_that_stops_arriving_is_given_up_on.
func TestADumpThatStopsArrivingIsGivenUpOn(t *testing.T) {
	c := newFakeConn()
	res, _ := startLimitedDial(c)
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.reliable(cmdBytes("VidM", []byte{12, 0, 0, 0}))
	stopped := time.Now()
	r := dialOutcome(t, res)
	if !errors.Is(r.err, ErrStalled) {
		t.Fatalf("err %v", r.err)
	}
	if took := time.Since(stopped); took >= testStall+testTick {
		t.Fatalf("gave up %v after the last packet", took)
	}
	sw.expect("goodbye", isSYN(helloClosing))
}

// test_a_tick_before_the_wait_is_not_judged, the Go way: a session already
// dialled re-handshakes on its silence ticks and keeps asking; the stall
// limits are Dial's only.
func TestTheStallLimitsAreDialsOnly(t *testing.T) {
	c := newFakeConn()
	res, _ := startLimitedDial(c)
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.dump()
	r := dialOutcome(t, res)
	if r.err != nil {
		t.Fatal(r.err)
	}
	time.Sleep(testHello + 2*testTick) // nothing from the switcher
	if !r.s.Connected() {
		t.Fatal("a dialled session gave up on a quiet switcher")
	}
	hellos := 0
	for _, p := range c.sent() {
		if isSYN(helloConnect)(p) {
			hellos++
		}
	}
	if hellos < 3 {
		t.Fatalf("%d hellos: not asking again", hellos)
	}
}

// A dialled session refused on a re-handshake keeps asking, every silence
// timeout, as atemwire's UDP thread does: the refusal fails only a Dial.
func TestARefusalAfterDialKeepsAsking(t *testing.T) {
	s, sw := dialed(t, testTick)
	sw.expect("SYN after silence", isSYN(helloConnect))
	sw.c.in <- fakeDatagram{b: rawDatagram(helloSession, 0, flagSYN, []byte{helloNoRoom, 0, 0, 0, 0, 0, 0, 0}), from: switcherAddr}
	sw.expect("the next hello", isSYN(helloConnect))
	if !s.Connected() {
		t.Fatal("the session ended on a refusal")
	}
}
