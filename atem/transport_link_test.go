// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Ported from bmdwire v1.14.5 atemwire/tests: test_resend_paced.py
// (bmdwire#80, 1.13.0) and test_transport_keeps_up.py (bmdwire#73,
// 1.11.0). Each Go test names the Python test it mirrors; Python tests
// with no counterpart say why where they would be.

import (
	"log/slog"
	"slices"
	"testing"
	"time"
)

// --- test_resend_paced.py ---------------------------------------------------

// pacedTransport is the Python udp fixture: established, session 0x8001,
// on a clock the test moves.
func pacedTransport(t *testing.T) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := retransmitTransport(t)
	tr.state = stateEstablished
	now := time.Unix(100, 0)
	tr.now = func() time.Time { return now }
	return tr, c, &now
}

func TestResendTimerStartsAtResendAfter(t *testing.T) { // test_before_anything_is_measured_the_timer_is_resend_after
	tr, _, _ := pacedTransport(t)
	if got := tr.resendWait(); got != resendAfter {
		t.Fatalf("%v", got)
	}
}

func TestAFastLinkResendsAfterResendMin(t *testing.T) { // test_a_fast_link_resends_after_resend_min
	tr, _, now := pacedTransport(t)
	for range 20 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
		*now = now.Add(2 * time.Millisecond)
		tr.onACK(uint16(tr.localSeq), false)
	}
	if got := tr.resendWait(); got != resendMin {
		t.Fatalf("%v", got)
	}
}

func TestASlowLinkWaitsLonger(t *testing.T) { // test_a_slow_link_waits_longer
	tr, _, now := pacedTransport(t)
	for range 20 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
		*now = now.Add(300 * time.Millisecond)
		tr.onACK(uint16(tr.localSeq), false)
	}
	if got := tr.resendWait(); got < 300*time.Millisecond || got > resendMax {
		t.Fatalf("%v", got)
	}
}

func TestTheTimerFollowsTheMeasurement(t *testing.T) { // test_the_timer_follows_the_measurement
	tr, _, now := pacedTransport(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	*now = now.Add(2 * time.Millisecond)
	tr.onACK(1, false)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	if want := now.Add(resendMin); !tr.resendDue.Equal(want) {
		t.Fatalf("due %v, want %v", tr.resendDue, want)
	}
}

func TestAResentPacketIsNotTimed(t *testing.T) { // test_a_resent_packet_is_not_timed
	// Karn: its ACK may answer either copy.
	tr, c, now := pacedTransport(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	*now = now.Add(2 * time.Millisecond)
	tr.onACK(1, false)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	srtt := tr.srtt
	*now = now.Add(time.Second)
	tr.resendUnacked()
	if got := seqsOf(resent(c)); !slices.Equal(got, []uint16{2}) {
		t.Fatalf("resent %v", got)
	}
	tr.onACK(2, false)
	if tr.srtt != srtt {
		t.Fatalf("srtt %v, was %v", tr.srtt, srtt)
	}
}

func TestUnansweredResendsBackOffToResendMax(t *testing.T) { // test_unanswered_resends_back_off_to_resend_max
	tr, _, now := pacedTransport(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	*now = now.Add(2 * time.Millisecond)
	tr.onACK(1, false)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	var waits []time.Duration
	for range 8 {
		*now = tr.resendDue
		tr.resendUnacked()
		waits = append(waits, tr.resendDue.Sub(*now))
	}
	if !slices.Equal(waits[:3], []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}) || waits[7] != resendMax {
		t.Fatalf("waits %v", waits)
	}
	tr.onACK(2, false)
	if tr.backoff != 1 {
		t.Fatal("an ACK did not end the backing off")
	}
}

func TestTheSameACKTwiceMoreResendsAtOnce(t *testing.T) { // test_the_same_ack_twice_more_resends_at_once
	tr, c, _ := pacedTransport(t)
	for range 5 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	tr.onACK(2, true) // 3 lost; 4 and 5 arrive
	tr.onACK(2, true)
	if len(resent(c)) != 0 {
		t.Fatal("resent after one repeat")
	}
	tr.onACK(2, true)
	if got := seqsOf(resent(c)); !slices.Equal(got, []uint16{3, 4, 5}) || tr.fastResends != 1 {
		t.Fatalf("resent %v, fast %d", got, tr.fastResends)
	}
	tr.onACK(2, true)
	if got := seqsOf(resent(c)); !slices.Equal(got, []uint16{3, 4, 5}) {
		t.Fatalf("resent %v: more than once per ACK", got)
	}
	tr.onACK(5, true)
	if !tr.resendDue.IsZero() {
		t.Fatal("timer still set")
	}
}

func TestADataPacketRepeatingTheACKIsNoSignOfLoss(t *testing.T) { // test_a_data_packet_repeating_the_ack_is_no_sign_of_loss
	tr, c, _ := pacedTransport(t)
	for range 3 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	for range 4 {
		tr.onACK(1, false)
	}
	if len(resent(c)) != 0 {
		t.Fatal("resent")
	}
}

func TestNothingOutstandingNothingResent(t *testing.T) { // test_nothing_outstanding_nothing_resent
	tr, c, _ := pacedTransport(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	for range 4 {
		tr.onACK(1, true)
	}
	if len(resent(c)) != 0 {
		t.Fatal("resent")
	}
}

// rx is the Python _rx: one reliable packet from the switcher (a Time
// command) through receive.
func rx(t *testing.T, tr *transport, seq uint16, retransmitted bool) []packet {
	t.Helper()
	flags := uint8(flagReliable)
	if retransmitted {
		flags |= flagRetransmission
	}
	got, err := tr.receive(rawDatagram(0x8001, seq, flags, cmdBytes("Time", []byte{0, 0, 0, 0})), switcherAddr)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func acksSent(c *fakeConn) []uint16 {
	var out []uint16
	for _, p := range c.sent() {
		if p.flags&flagACK != 0 {
			out = append(out, p.ack)
		}
	}
	return out
}

func TestPacketsParkedBehindAGapAreNotAcknowledgedAgain(t *testing.T) { // test_packets_parked_behind_a_gap_are_not_acknowledged_again
	tr, c, _ := pacedTransport(t)
	rx(t, tr, 1, false)
	rx(t, tr, 2, false)
	if got := acksSent(c); !slices.Equal(got, []uint16{1, 2}) {
		t.Fatalf("acks %v", got)
	}
	for _, seq := range []uint16{4, 5, 6} { // 3 lost
		rx(t, tr, seq, false)
	}
	if got := acksSent(c); !slices.Equal(got, []uint16{1, 2}) {
		t.Fatalf("acks %v: the same ACK again says nothing", got)
	}
	rx(t, tr, 3, true) // the gap fills
	if got := acksSent(c); got[len(got)-1] != 6 {
		t.Fatalf("acks %v", got)
	}
}

func TestAResendOfWhatWeHaveIsAcknowledgedAgain(t *testing.T) { // test_a_resend_of_what_we_have_is_acknowledged_again
	// Our ACK was lost: the switcher resends, and must hear it again.
	tr, c, _ := pacedTransport(t)
	rx(t, tr, 1, false)
	rx(t, tr, 2, false)
	if got := rx(t, tr, 2, true); got != nil {
		t.Fatalf("delivered the resend again: %v", got)
	}
	if got := acksSent(c); !slices.Equal(got, []uint16{1, 2, 2}) {
		t.Fatalf("acks %v", got)
	}
}

func TestALostPacket0IsNotTakenForReceived(t *testing.T) { // test_a_lost_packet_0_is_not_taken_for_received
	// The switcher's sequence wraps 32767 -> 0, and its ACK-only packets
	// all carry sequence 0. Its real packet 0 lost, the ACK must stop
	// before it and its resend must be delivered.
	tr, _, _ := pacedTransport(t)
	for _, seq := range []uint16{32765, 32766, 32767} {
		rx(t, tr, seq, false)
	}
	ackOnly := (&packet{flags: flagACK, session: 0x8001}).marshal()
	if _, err := tr.receive(ackOnly, switcherAddr); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []uint16{1, 2} { // 0 lost
		if got := rx(t, tr, seq, false); got != nil {
			t.Fatalf("delivered %d ahead of the gap", seq)
		}
	}
	if tr.ackNumber != 32767 {
		t.Fatalf("ack %d: past the missing 0", tr.ackNumber)
	}
	got := rx(t, tr, 0, true)
	if tr.ackNumber != 2 {
		t.Fatalf("ack %d", tr.ackNumber)
	}
	if seqs := seqsOf(got); !slices.Equal(seqs, []uint16{0, 1, 2}) {
		t.Fatalf("delivered %v: its resend, then what waited for it", seqs)
	}
}

// --- test_transport_keeps_up.py ---------------------------------------------

// burst is a drain on the transport alone, as the session runs it: every
// datagram with batching on, an ACK every ackEvery, one after.
func burst(t *testing.T, tr *transport, seqs []uint16, retransmitted bool) int {
	t.Helper()
	tr.batching = true
	delivered := 0
	for _, seq := range seqs {
		delivered += len(rx(t, tr, seq, retransmitted))
		if tr.acksOwed >= ackEvery {
			if err := tr.flushACK(); err != nil {
				t.Fatal(err)
			}
		}
	}
	tr.batching = false
	if err := tr.flushACK(); err != nil {
		t.Fatal(err)
	}
	return delivered
}

func span(from, n int) []uint16 {
	var out []uint16
	for i := range n {
		out = append(out, uint16(from+i))
	}
	return out
}

func TestABurstIsAcknowledgedOnceAfterItIsRead(t *testing.T) { // test_a_burst_is_acknowledged_once_after_it_is_read
	tr, c, _ := pacedTransport(t)
	if n := burst(t, tr, span(100, 20), false); n != 20 {
		t.Fatalf("%d delivered", n)
	}
	if got := acksSent(c); !slices.Equal(got, []uint16{119}) {
		t.Fatalf("acks %v, want one cumulative ACK", got)
	}
}

func TestALongBurstIsAcknowledgedAsItGoes(t *testing.T) { // test_a_long_burst_is_acknowledged_as_it_goes
	tr, c, _ := pacedTransport(t)
	burst(t, tr, span(100, 3*ackEvery+5), false)
	n := uint16(ackEvery)
	if got := acksSent(c); !slices.Equal(got, []uint16{100 + n - 1, 100 + 2*n - 1, 100 + 3*n - 1, 100 + 3*n + 4}) {
		t.Fatalf("acks %v", got)
	}
}

func TestAResendOfSomethingWeHaveIsAcknowledgedAgain(t *testing.T) { // test_a_resend_of_something_we_have_is_acknowledged_again
	tr, c, _ := pacedTransport(t)
	burst(t, tr, span(100, 10), false)
	c.reset()
	if n := burst(t, tr, span(100, 10), true); n != 0 { // our ACK never got there
		t.Fatalf("%d resends handed on twice", n)
	}
	if got := acksSent(c); !slices.Equal(got, []uint16{109}) {
		t.Fatalf("acks %v: the switcher has to hear it again", got)
	}
}

func TestAResendAloneIsAcknowledgedAtOnceOutsideADrain(t *testing.T) { // test_a_resend_alone_is_acknowledged_at_once_outside_a_drain
	tr, c, _ := pacedTransport(t)
	rx(t, tr, 100, false)
	rx(t, tr, 100, true)
	if got := acksSent(c); !slices.Equal(got, []uint16{100, 100}) {
		t.Fatalf("acks %v", got)
	}
}

func TestTheACKStaysOnTheGapThroughADrain(t *testing.T) { // test_the_ack_stays_on_the_gap_through_a_drain
	tr, c, _ := pacedTransport(t)
	burst(t, tr, []uint16{100, 101, 103, 104}, false) // 102 lost
	if got := acksSent(c); !slices.Equal(got, []uint16{101}) {
		t.Fatalf("acks %v", got)
	}
	burst(t, tr, []uint16{102}, true)
	if got := acksSent(c); got[len(got)-1] != 104 {
		t.Fatalf("acks %v", got)
	}
}

func TestAnEmptyDrainSendsNoACK(t *testing.T) { // test_an_empty_socket_ends_the_drain_quietly
	tr, c, _ := pacedTransport(t)
	burst(t, tr, nil, false)
	if len(acksSent(c)) != 0 {
		t.Fatal("ACKed nothing")
	}
}

func TestTheSeenWindowKeepsTheNewest(t *testing.T) { // test_the_seen_window_keeps_the_newest_and_answers_at_once
	var h seqHistory
	for i := range receivedHistory + 5 {
		h.add(uint16(i))
	}
	if h.contains(0) || h.contains(4) || !h.contains(5) || !h.contains(receivedHistory+4) {
		t.Fatal("window")
	}
	if receivedHistory != 2*defaultDeliveryParkWindow || defaultDeliveryParkWindow != 4096 {
		t.Fatalf("windows %d, %d", receivedHistory, defaultDeliveryParkWindow)
	}
}

// test_the_socket_queue_hands_over_everything_waiting has no counterpart:
// the owner goroutine writes to the socket itself, so there is no queue
// between threads to hand over.

// The session's drain (the Python _drain_socket): the datagram that woke
// it and every one already waiting, then one ACK.
func TestTheSessionDrainsWhatIsWaitingThenACKsOnce(t *testing.T) { // test_the_udp_thread_drains_a_real_socket
	c := newFakeConn()
	s := newSession(c, switcherAddr, nil, 0)
	s.t.state, s.t.sessionID, s.t.localSeq = stateEstablished, 0x8001, 0
	dg := func(seq uint16) datagram {
		return datagram{b: rawDatagram(0x8001, seq, flagReliable, cmdBytes("Time", []byte{0, 0, 0, 0})), from: switcherAddr}
	}
	for seq := uint16(101); seq < 100+3*ackEvery+5; seq++ {
		s.datagrams <- dg(seq)
	}
	if err := s.drain(dg(100)); err != nil {
		t.Fatal(err)
	}
	n := uint16(ackEvery)
	if got := acksSent(c); !slices.Equal(got, []uint16{100 + n - 1, 100 + 2*n - 1, 100 + 3*n - 1, 100 + 3*n + 4}) {
		t.Fatalf("acks %v", got)
	}
	if len(s.datagrams) != 0 || s.t.batching {
		t.Fatalf("%d left, batching %v", len(s.datagrams), s.t.batching)
	}
}

// --- the receive buffer (test_a_capped_receive_buffer_is_reported_once) -----

func TestACappedReceiveBufferIsReportedOnce(t *testing.T) { // test_a_capped_receive_buffer_is_reported_once, test_a_receive_buffer_granted_in_full_is_not_reported
	rcvbufWarned.Store(false)
	t.Cleanup(func() { rcvbufWarned.Store(false) })
	var l logRecords
	log := slog.New(&l)
	checkReadBuffer(log, 16<<20, 16<<20)
	if len(l.matching("receive buffer")) != 0 {
		t.Fatal("a buffer granted in full was reported")
	}
	checkReadBuffer(log, 16<<20, 208<<10)
	checkReadBuffer(log, 16<<20, 208<<10)
	if got := l.matching("receive buffer"); len(got) != 1 {
		t.Fatalf("%d reports, want one per process", len(got))
	}
}
