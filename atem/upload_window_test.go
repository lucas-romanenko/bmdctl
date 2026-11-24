// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// An upload's chunks go out as a congestion window lets them, and a lost
// one goes again with everything after it as the ACKs come back
// (bmdwire#105, d1b4cbe; test_upload_window.py, one for one). An upload
// budget (FTCD, 768 chunks of a 1080p still) went out whole, back to
// back. The switcher takes our packets in order only and drops the rest,
// so on a slower or longer link one lost chunk cost every chunk after it;
// the resend sent maxBurst of them and waited out the timer for each next
// lot.
//
// The switcher here is a model of the captures: it takes our packets in
// order only, drops any that come early, and acknowledges cumulatively
// every 16 packets and at the end of what arrived.

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

const windowRTT = 80 * time.Millisecond

// windowTransport is the Python tests' udp fixture: a session under way,
// one packet sent and acknowledged and one round trip measured, on a
// clock the test moves.
func windowTransport(t *testing.T) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := established(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	sendReliable(t, tr, windowChunk(0))
	now = now.Add(windowRTT)
	tr.onACK(1, true)
	c.reset()
	return tr, c, &now
}

// windowChunk is an FTDa as queueChunks sends it: transfer 7, 1,388 bytes
// of data, numbered i.
func windowChunk(i int) []byte {
	d := make([]byte, 1388)
	binary.BigEndian.PutUint32(d, uint32(i))
	return TransferData{Transfer: 7, Data: d}.Encode()
}

// queueBudget hands the transport an upload budget, as drainQueue does.
func queueBudget(t *testing.T, tr *transport, n, start int) {
	t.Helper()
	for i := start; i < start+n; i++ {
		sendReliable(t, tr, windowChunk(i))
	}
}

// windowSwitcher takes our packets in order only, drops an early one,
// and acknowledges every 16 packets and at the end of what arrived.
type windowSwitcher struct {
	tr      *transport
	c       *fakeConn
	have    int
	lose    map[uint16]bool
	read    int
	dropped int
}

func newWindowSwitcher(tr *transport, c *fakeConn, lose ...uint16) *windowSwitcher {
	sw := &windowSwitcher{tr: tr, c: c, have: tr.ackedThrough, lose: map[uint16]bool{}}
	for _, s := range lose {
		sw.lose[s] = true
	}
	return sw
}

// deliver: what we sent since the last call arrives; the ACKs it sends.
func (sw *windowSwitcher) deliver() []uint16 {
	var acks []uint16
	n := 0
	sent := sw.c.sent()
	for _, p := range sent[sw.read:] {
		if p.flags&flagReliable == 0 {
			continue
		}
		if sw.lose[p.seq] {
			delete(sw.lose, p.seq) // lost once, on the way
			continue
		}
		if int(p.seq) == (sw.have+1)&seqMask {
			sw.have = int(p.seq)
		} else if (int(p.seq)-sw.have)&seqMask < seqHalf {
			sw.dropped++
		}
		n++
		if n%16 == 0 {
			acks = append(acks, uint16(sw.have))
		}
	}
	sw.read = len(sent)
	if n%16 != 0 {
		acks = append(acks, uint16(sw.have))
	}
	return acks
}

// runWindow plays round trips until the switcher has everything through
// last, and returns the time it took.
func runWindow(t *testing.T, tr *transport, sw *windowSwitcher, now *time.Time, last int) time.Duration {
	t.Helper()
	start := *now
	for range 2000 {
		if sw.have == last {
			return now.Sub(start)
		}
		acks := sw.deliver()
		*now = now.Add(windowRTT)
		for _, a := range acks {
			tr.onACK(a, true)
		}
		if len(acks) == 0 && !tr.resendDue.IsZero() {
			if tr.resendDue.After(*now) {
				*now = tr.resendDue
			}
			tr.resendUnacked()
		}
	}
	t.Fatalf("switcher stuck at %d of %d", sw.have, last)
	return 0
}

// CWND_START is one budget: on a good link the first goes back to back as
// ATEM Software Control sends it.
func TestTheFirstBudgetGoesWholeAndTheNextAsTheWindowLetsIt(t *testing.T) { // test_the_first_budget_goes_whole_and_the_next_as_the_window_lets_it
	tr, c, _ := windowTransport(t)
	queueBudget(t, tr, 2*768, 1)
	if n := len(c.sent()); cwndStart != 768 || n != 768 {
		t.Fatalf("%d sent", n)
	}
}

func TestTheWindowOpensAsTheACKsCome(t *testing.T) { // test_the_window_opens_as_the_acks_come
	tr, c, now := windowTransport(t)
	tr.cwnd = 128 // after a loss, say
	queueBudget(t, tr, 768, 1)
	sw := newWindowSwitcher(tr, c)
	if took := runWindow(t, tr, sw, now, 769); took > 4*windowRTT { // 128, 256, 512: then the rest
		t.Fatalf("took %v", took)
	}
	if sw.dropped != 0 {
		t.Fatalf("%d dropped", sw.dropped)
	}
	var want []uint16
	for s := uint16(2); s < 770; s++ {
		want = append(want, s)
	}
	if got := seqsOf(c.sent()); !slices.Equal(got, want) {
		t.Fatal("not each once, in order")
	}
}

func TestALostChunkCostsRoundTripsNotTimers(t *testing.T) { // test_a_lost_chunk_costs_round_trips_not_timers
	tr, c, now := windowTransport(t)
	queueBudget(t, tr, 768, 1)
	sw := newWindowSwitcher(tr, c, 60)
	took := runWindow(t, tr, sw, now, 769)
	if tr.resends != 0 {
		t.Fatalf("%d timer resends: the repeated ACK said so", tr.resends)
	}
	if took > 6*windowRTT { // 1.14.5: 15 round trips, 5 timers
		t.Fatalf("took %v", took)
	}
}

// A queue on the way that takes 100 packets of a burst and drops the rest:
// the whole lot after the first one lost goes again as the window allows,
// instead of 128 at a time with a timer in between.
func TestChunksLostAtAFullQueueAreSentAgainInOneGo(t *testing.T) { // test_chunks_lost_at_a_full_queue_are_sent_again_in_one_go
	tr, c, now := windowTransport(t)
	queueBudget(t, tr, 768, 1)
	var lose []uint16
	for s := uint16(102); s < 770; s++ {
		lose = append(lose, s)
	}
	sw := newWindowSwitcher(tr, c, lose...)
	if took := runWindow(t, tr, sw, now, 769); took > 11*windowRTT { // 1.14.5: 14.6, 6 timers
		t.Fatalf("took %v", took)
	}
}

// Random loss, the round trip as short as ever: halving the window for it
// only slowed the upload (2 % loss over 80 ms: 45 s a still).
func TestLossWithoutAQueueKeepsTheWindow(t *testing.T) { // test_loss_without_a_queue_keeps_the_window
	tr, _, now := windowTransport(t)
	queueBudget(t, tr, 300, 1)
	*now = now.Add(windowRTT)
	tr.onACK(10, true)
	cwnd := tr.cwnd
	tr.onACK(10, true)
	tr.onACK(10, true) // the second repeat
	if tr.cwnd < cwnd {
		t.Fatalf("window %v, was %v", tr.cwnd, cwnd)
	}
}

func TestLossWithAQueueHalvesTheWindow(t *testing.T) { // test_loss_with_a_queue_halves_the_window
	tr, _, now := windowTransport(t)
	tr.cwnd = 128
	queueBudget(t, tr, 300, 1)
	*now = now.Add(windowRTT + 50*time.Millisecond) // 50 ms waiting in a queue
	tr.srtt = windowRTT + 50*time.Millisecond
	tr.onACK(10, true)
	flight := tr.inFlight()
	tr.onACK(10, true)
	tr.onACK(10, true)
	if want := max(cwndMin, float64(flight)*cwndBeta); tr.cwnd != want {
		t.Fatalf("window %v, want %v", tr.cwnd, want)
	}
}

func TestATimeoutWithAQueueStartsAgainSmall(t *testing.T) { // test_a_timeout_with_a_queue_starts_again_small
	tr, c, now := windowTransport(t)
	tr.srtt = windowRTT + 50*time.Millisecond
	queueBudget(t, tr, 300, 1)
	*now = tr.resendDue
	tr.resendUnacked()
	if tr.cwnd != cwndMin {
		t.Fatalf("window %v", tr.cwnd)
	}
	if n := len(resent(c)); n != cwndMin {
		t.Fatalf("%d resent", n)
	}
}

// srtt + 4 rttvar alone came to 80.0 ms on a steady 80 ms link: every ACK
// a little late was a resend nobody needed.
func TestASteadyRoundTripLeavesTheTimerRoom(t *testing.T) { // test_a_steady_round_trip_leaves_the_timer_room
	tr, _, now := windowTransport(t)
	for range 30 {
		sendReliable(t, tr, windowChunk(0))
		*now = now.Add(windowRTT)
		tr.onACK(uint16(tr.localSeq), true)
	}
	if w := tr.resendWait(); w < windowRTT+20*time.Millisecond {
		t.Fatalf("timer %v", w)
	}
}

func TestACommandWaitsItsTurnBehindTheChunks(t *testing.T) { // test_a_command_waits_its_turn_behind_the_chunks
	tr, c, now := windowTransport(t)
	queueBudget(t, tr, 300, 1)
	sendReliable(t, tr, Lock{Store: 0}.Encode())
	sw := newWindowSwitcher(tr, c)
	runWindow(t, tr, sw, now, 302)
	sent := c.sent()
	last := sent[len(sent)-1]
	if last.seq != 302 || string(last.data[4:8]) != "LOCK" {
		t.Fatalf("last %d %q", last.seq, last.data[4:8])
	}
}

func TestACKsAndHellosNeverWait(t *testing.T) { // test_acks_and_hellos_never_wait
	tr, c, _ := windowTransport(t)
	queueBudget(t, tr, cwndStart+300, 1)
	before := len(c.sent())
	if err := tr.sendLow(&packet{flags: flagACK, ack: 5, remoteSeq: ackMagic}); err != nil {
		t.Fatal(err)
	}
	if !tr.windowFull(&packet{flags: flagACK}) && !tr.windowFull(&packet{flags: flagSYN}) && len(c.sent()) == before+1 {
		return
	}
	t.Fatal("an ACK or a hello waits for the window")
}

func TestANewSessionForgetsWhatWasWaiting(t *testing.T) { // test_a_new_session_forgets_what_was_waiting
	tr, _, _ := windowTransport(t)
	queueBudget(t, tr, cwndStart+300, 1)
	if len(tr.held) == 0 {
		t.Fatal("nothing waiting")
	}
	tr.state = stateClosed
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	if len(tr.held) != 0 || tr.cwnd != cwndStart {
		t.Fatalf("%d waiting, window %v", len(tr.held), tr.cwnd)
	}
}

func ftuaPacket(n uint16) []byte { return TransferAck{Transfer: n}.Encode() }

// A download's acknowledgements (FTUA) held behind a loss held the
// switcher's own send window shut: a slow site's still download, 46 s,
// took 54 s. Only an upload's chunks burst; only they wait.
func TestOnlyUploadChunksWaitForTheWindow(t *testing.T) { // test_only_upload_chunks_wait_for_the_window
	tr, c, _ := windowTransport(t)
	tr.cwnd = 16
	queueBudget(t, tr, 16, 1) // the window full, none waiting
	before := len(c.sent())
	sendReliable(t, tr, ftuaPacket(0))
	if len(c.sent()) != before+1 {
		t.Fatal("the FTUA did not go at once")
	}
	sendReliable(t, tr, windowChunk(99))
	if len(tr.held) == 0 {
		t.Fatal("a chunk did not wait its turn")
	}
}

func TestAnAcknowledgementGoesAtOnceWhileLostChunksGoAgain(t *testing.T) { // test_an_acknowledgement_goes_at_once_while_lost_chunks_go_again
	tr, c, now := windowTransport(t)
	tr.cwnd = 32
	queueBudget(t, tr, 32, 1)
	*now = now.Add(windowRTT)
	tr.onACK(5, true)
	tr.onACK(5, true)
	tr.onACK(5, true) // a loss: go-back-N under way
	before := len(c.sent())
	sendReliable(t, tr, ftuaPacket(1))
	for _, p := range c.sent()[before:] {
		if len(p.data) >= 8 && string(p.data[4:8]) == "FTUA" {
			return
		}
	}
	t.Fatal("the FTUA waited")
}

// In order behind what is waiting: an FTFD must never overtake the data it
// describes.
func TestACommandWaitsBehindChunksThatWait(t *testing.T) { // test_a_command_waits_behind_chunks_that_wait
	tr, c, _ := windowTransport(t)
	queueBudget(t, tr, cwndStart+10, 1)
	before := len(c.sent())
	sendReliable(t, tr, ftuaPacket(2))
	if len(c.sent()) != before || string(tr.held[len(tr.held)-1].data[4:8]) != "FTUA" {
		t.Fatal("the FTUA overtook the chunks waiting")
	}
}

// Not an upload's chunk: no paced pass. A download's FTUAs resent in one
// had the pass cut short by the next FTUA sent fresh, the rest never
// resent, and the switcher held its window shut (a busy host's still
// download, 14 s instead of 10). The window down to 16 by the timeouts
// before, 40 FTUAs out.
func TestALostAcknowledgementGoesAgainAtOnceWithAllAfterIt(t *testing.T) { // test_a_lost_acknowledgement_goes_again_at_once_with_all_after_it
	tr, c, now := windowTransport(t)
	tr.cwnd = 16
	for n := range 40 {
		sendReliable(t, tr, ftuaPacket(uint16(n)))
	}
	first := uint16(tr.ackedThrough + 1)
	c.reset()
	*now = tr.resendDue
	tr.resendUnacked() // the timer: all 40 again
	again := c.sent()
	var want []uint16
	for s := first; s < first+40; s++ {
		want = append(want, s)
	}
	if got := seqsOf(again); !slices.Equal(got, want) {
		t.Fatalf("resent %v", got)
	}
	for _, p := range again {
		if p.flags&flagRetransmission == 0 {
			t.Fatalf("seq %d not flagged a retransmission", p.seq)
		}
	}
	sendReliable(t, tr, ftuaPacket(99)) // the next FTUA, fresh
	sent := c.sent()
	if last := sent[len(sent)-1]; last.seq != first+40 {
		t.Fatalf("the fresh FTUA went as %d", last.seq)
	}
}
