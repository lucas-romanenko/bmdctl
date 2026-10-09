// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Ported from bmdwire v1.3.0 atemwire/tests: test_transport_ack_gap.py,
// test_transport_inorder.py, test_transport_connect_reset.py,
// test_transport_datagram_guard.py, test_transport_retransmit.py,
// test_transport_close_session.py and the transport half of
// test_lock_delivery.py. Each Go test names the Python test it mirrors.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

func newTestTransport(t *testing.T) (*transport, *fakeConn) {
	t.Helper()
	c := newFakeConn()
	return newTransport(c, switcherAddr, nil), c
}

// --- test_transport_ack_gap.py -------------------------------------------

// arrive is the Python test's helper: one packet's sequence path through
// _receive_packet_low, without the socket.
func arrive(tr *transport, seq uint16, reliable bool) uint16 {
	p := packet{seq: seq}
	if reliable {
		p.flags = flagReliable
	}
	tr.remoteSeq = seq
	tr.received.add(seq)
	tr.updateACK(p)
	return tr.ackNumber
}

func expectACKs(t *testing.T, tr *transport, steps ...[2]int) {
	t.Helper()
	for _, s := range steps {
		if got := arrive(tr, uint16(s[0]), true); got != uint16(s[1]) {
			t.Fatalf("arrive(%d): ack %d, want %d", s[0], got, s[1])
		}
	}
}

func TestACKInOrderAcksEach(t *testing.T) { // test_in_order_acks_each
	tr, _ := newTestTransport(t)
	expectACKs(t, tr, [2]int{10, 10}, [2]int{11, 11}, [2]int{12, 12})
}

func TestACKGapStallsUntilFilled(t *testing.T) { // test_gap_stalls_ack_until_filled
	tr, _ := newTestTransport(t)
	expectACKs(t, tr, [2]int{10, 10}, [2]int{11, 11}, [2]int{13, 11}, [2]int{12, 13})
}

func TestACKOutOfOrderBurstThenFill(t *testing.T) { // test_out_of_order_burst_then_fill
	tr, _ := newTestTransport(t)
	expectACKs(t, tr, [2]int{10, 10}, [2]int{13, 10}, [2]int{12, 10}, [2]int{11, 13})
}

func TestACKDuplicateDoesNotAdvancePastGap(t *testing.T) { // test_duplicate_does_not_advance_past_gap
	tr, _ := newTestTransport(t)
	expectACKs(t, tr, [2]int{10, 10}, [2]int{11, 11}, [2]int{11, 11})
}

func TestACKFifteenBitWraparound(t *testing.T) { // test_fifteen_bit_wraparound
	tr, _ := newTestTransport(t)
	expectACKs(t, tr, [2]int{32766, 32766}, [2]int{32767, 32767}, [2]int{0, 0}, [2]int{1, 1})
}

func TestACKHandshakePacketsMirrorLatestBeforeBaseline(t *testing.T) { // test_handshake_packets_mirror_latest_before_baseline
	tr, _ := newTestTransport(t)
	if got := arrive(tr, 5, false); got != 5 {
		t.Fatalf("unreliable 5: ack %d", got)
	}
	if tr.rxEstablished {
		t.Fatal("an unreliable frame set the baseline")
	}
	expectACKs(t, tr, [2]int{1, 1})
	if !tr.rxEstablished {
		t.Fatal("the first reliable packet did not set the baseline")
	}
	expectACKs(t, tr, [2]int{2, 2})
}

func TestACKNoLossPathIsUnchanged(t *testing.T) { // test_no_loss_path_is_unchanged
	tr, _ := newTestTransport(t)
	for s := 100; s < 160; s++ {
		expectACKs(t, tr, [2]int{s, s})
	}
}

// --- test_transport_inorder.py -------------------------------------------

func inorderTransport(t *testing.T) *transport {
	tr, _ := newTestTransport(t)
	tr.sessionID = 0x8001
	return tr
}

// feed is the Python test's feed(): stage one datagram carrying the
// CTst fixture and run it through receive.
func feed(t *testing.T, tr *transport, seq uint16, flags uint8) []uint16 {
	t.Helper()
	data := fixtures.Bytes(t, "transport/CTst-command.bin")
	got, err := tr.receive(datagram(0x8001, seq, flags, data), switcherAddr)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint16
	for _, p := range got {
		if !bytes.Equal(p.data, data) {
			t.Fatalf("seq %d: data %x", p.seq, p.data)
		}
		seqs = append(seqs, p.seq)
	}
	return seqs
}

func expectFeed(t *testing.T, tr *transport, seq uint16, flags uint8, want ...uint16) {
	t.Helper()
	if got := feed(t, tr, seq, flags); !slices.Equal(got, want) {
		t.Fatalf("feed %d: delivered %v, want %v", seq, got, want)
	}
}

func TestInOrderPassthrough(t *testing.T) { // test_in_order_passthrough
	tr := inorderTransport(t)
	for _, s := range []uint16{10, 11, 12} {
		expectFeed(t, tr, s, flagReliable, s)
	}
}

func TestInOrderGapParksThenDrains(t *testing.T) { // test_gap_parks_then_drains_in_order
	tr := inorderTransport(t)
	expectFeed(t, tr, 10, flagReliable, 10)
	expectFeed(t, tr, 12, flagReliable)
	expectFeed(t, tr, 13, flagReliable)
	expectFeed(t, tr, 11, flagReliable, 11, 12, 13)
	expectFeed(t, tr, 14, flagReliable, 14)
}

func TestInOrderDuplicateBehindCursorDropped(t *testing.T) { // test_duplicate_behind_cursor_dropped
	tr := inorderTransport(t)
	expectFeed(t, tr, 10, flagReliable, 10)
	expectFeed(t, tr, 11, flagReliable, 11)
	expectFeed(t, tr, 10, flagReliable)
}

func TestInOrderFlaggedRetransmitOfReceivedDropped(t *testing.T) { // test_flagged_retransmit_of_received_dropped
	tr := inorderTransport(t)
	expectFeed(t, tr, 10, flagReliable, 10)
	expectFeed(t, tr, 12, flagReliable)
	expectFeed(t, tr, 12, flagReliable|flagRetransmission)
	expectFeed(t, tr, 11, flagReliable, 11, 12)
}

func TestInOrderWraparoundGap(t *testing.T) { // test_wraparound_gap
	tr := inorderTransport(t)
	expectFeed(t, tr, 0x7FFE, flagReliable, 0x7FFE)
	expectFeed(t, tr, 0x0001, flagReliable)
	expectFeed(t, tr, 0x7FFF, flagReliable, 0x7FFF)
	expectFeed(t, tr, 0x0000, flagReliable, 0x0000, 0x0001)
}

func TestInOrderAbsurdJumpResyncs(t *testing.T) { // test_absurd_jump_resyncs
	tr := inorderTransport(t)
	expectFeed(t, tr, 10, flagReliable, 10)
	expectFeed(t, tr, 12, flagReliable)
	far := uint16((11 + defaultDeliveryParkWindow + 5) & seqMask)
	expectFeed(t, tr, far, flagReliable, far)
	if len(tr.parked) != 0 {
		t.Fatalf("stale parked packets kept: %v", tr.parked)
	}
	expectFeed(t, tr, (far+1)&seqMask, flagReliable, (far+1)&seqMask)
}

func TestInOrderNonReliableBypassesGate(t *testing.T) { // test_non_reliable_bypasses_gate
	tr := inorderTransport(t)
	expectFeed(t, tr, 10, flagReliable, 10)
	expectFeed(t, tr, 12, flagReliable)
	expectFeed(t, tr, 0, flagACK, 0)
	if len(tr.parked) == 0 {
		t.Fatal("the open gap was disturbed by a control frame")
	}
}

// --- test_transport_connect_reset.py -------------------------------------

func TestConnectClearsStaleReceiveState(t *testing.T) { // test_connect_clears_stale_receive_state
	tr, c := newTestTransport(t)
	tr.received.add(1234)
	tr.received.add(1235)
	tr.hadTraffic = true
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	if tr.received.contains(1234) || tr.received.contains(1235) || tr.received.size != 0 {
		t.Fatal("stale sequence numbers survived connect")
	}
	if tr.hadTraffic {
		t.Fatal("hadTraffic survived connect")
	}
	if tr.state != stateSYNSent {
		t.Fatalf("state %v", tr.state)
	}
	if n := len(c.sent()); n != 1 {
		t.Fatalf("%d packets sent, want one SYN", n)
	}
}

// Not in the Python suite: pins the SYN atemwire sends (session 0x1337,
// sequence 1 because local_sequence_number goes -1 -> 0 first, hello 0x01).
func TestConnectSendsSYN(t *testing.T) {
	tr, c := newTestTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	want := []byte{0x10, 0x14, 0x13, 0x37, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	if got := c.writes[0].b; !bytes.Equal(got, want) {
		t.Fatalf("SYN %x, want %x", got, want)
	}
	if c.writes[0].to != switcherAddr {
		t.Fatalf("SYN sent to %v", c.writes[0].to)
	}
	if err := tr.connect(); err == nil {
		t.Fatal("connect on an open transport did not fail")
	}
}

// --- test_transport_datagram_guard.py ------------------------------------

func validFrame() []byte { return datagram(0x1234, 0, flagReliable, nil) }

func TestForeignPeerDatagramIsDropped(t *testing.T) { // test_foreign_peer_datagram_is_dropped
	tr, c := newTestTransport(t)
	got, err := tr.receive(validFrame(), foreignAddr)
	if err != nil || got != nil {
		t.Fatalf("delivered %v, %v", got, err)
	}
	if len(c.sent()) != 0 {
		t.Fatal("answered a foreign peer")
	}
}

func TestMalformedDatagramIsDroppedNotFatal(t *testing.T) { // test_malformed_datagram_is_dropped_not_fatal
	tr, _ := newTestTransport(t)
	for _, b := range [][]byte{{1, 2, 3}, bytes.Repeat([]byte{0xff}, 20)} {
		got, err := tr.receive(b, switcherAddr)
		if err != nil || got != nil {
			t.Fatalf("%x: delivered %v, %v", b, got, err)
		}
	}
}

func TestValidDatagramFromPeerStillParses(t *testing.T) { // test_valid_datagram_from_peer_still_parses
	tr, _ := newTestTransport(t)
	got, err := tr.receive(validFrame(), switcherAddr)
	if err != nil || len(got) != 1 || got[0].session != 0x1234 {
		t.Fatalf("delivered %v, %v", got, err)
	}
}

// --- test_transport_retransmit.py ----------------------------------------

func retransmitTransport(t *testing.T) (*transport, *fakeConn) {
	tr, c := newTestTransport(t)
	tr.sessionID = 0x8001
	tr.localSeq = 0
	return tr, c
}

func sendReliable(t *testing.T, tr *transport, data []byte) {
	t.Helper()
	if err := tr.sendReliable(data); err != nil {
		t.Fatal(err)
	}
}

func resent(c *fakeConn) []packet {
	var out []packet
	for _, p := range c.sent() {
		if p.flags&flagRetransmission != 0 {
			out = append(out, p)
		}
	}
	return out
}

func seqsOf(ps []packet) []uint16 {
	var out []uint16
	for _, p := range ps {
		out = append(out, p.seq)
	}
	return out
}

func TestReliableSendsAreBufferedBySequence(t *testing.T) { // test_reliable_sends_are_buffered_by_sequence
	tr, _ := retransmitTransport(t)
	sendReliable(t, tr, []byte("\x00\x08AAAA"))
	sendReliable(t, tr, []byte("\x00\x08BBBB"))
	if string(tr.retransmitBuf[1].data) != "\x00\x08AAAA" || string(tr.retransmitBuf[2].data) != "\x00\x08BBBB" {
		t.Fatalf("buffer %v", tr.retransmitBuf)
	}
}

func TestACKsDoNotClobberTheBuffer(t *testing.T) { // test_acks_do_not_clobber_the_buffer
	tr, _ := retransmitTransport(t)
	sendReliable(t, tr, []byte("\x00\x08CMD1"))
	if err := tr.sendLow(&packet{flags: flagACK}); err != nil {
		t.Fatal(err)
	}
	b := tr.retransmitBuf[1]
	if b.flags&flagReliable == 0 || string(b.data) != "\x00\x08CMD1" || len(tr.retransmitBuf) != 1 {
		t.Fatalf("buffer %v", tr.retransmitBuf)
	}
}

func TestBufferIsPrunedToTheWindow(t *testing.T) { // test_buffer_is_pruned_to_the_window
	tr, _ := retransmitTransport(t)
	tr.retransmitWindow = 8
	for range 12 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	var keys []uint16
	for k := range tr.retransmitBuf {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []uint16{5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Fatalf("buffered %v", keys)
	}
}

func TestRetransmitResendsInOrderWithFlagAndOriginalSeqs(t *testing.T) { // test_retransmit_resends_in_order_with_flag_and_original_seqs
	tr, c := retransmitTransport(t)
	payload := func(i int) []byte { return []byte{0, 8, 'C', 'M', byte('0' + i/10), byte('0' + i%10)} }
	for i := 1; i <= 5; i++ {
		sendReliable(t, tr, payload(i))
	}
	c.reset()
	if n := tr.retransmitFrom(3); n != 3 {
		t.Fatalf("resent %d", n)
	}
	out := c.sent()
	if !slices.Equal(seqsOf(out), []uint16{3, 4, 5}) {
		t.Fatalf("resent %v", seqsOf(out))
	}
	for _, p := range out {
		if p.flags&flagRetransmission == 0 || p.flags&flagReliable == 0 || !bytes.Equal(p.data, payload(int(p.seq))) || p.session != 0x8001 {
			t.Fatalf("resent %+v", p)
		}
	}
}

func TestRetransmitSkipsSequencesMissingFromBuffer(t *testing.T) { // test_retransmit_skips_sequences_missing_from_buffer
	tr, c := retransmitTransport(t)
	for range 5 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	delete(tr.retransmitBuf, 4)
	c.reset()
	if n := tr.retransmitFrom(3); n != 2 || !slices.Equal(seqsOf(c.sent()), []uint16{3, 5}) {
		t.Fatalf("resent %d: %v", n, seqsOf(c.sent()))
	}
}

func TestRequestOutsideWindowResendsNothing(t *testing.T) { // test_request_outside_window_resends_nothing
	tr, c := retransmitTransport(t)
	tr.retransmitWindow = 8
	for range 5 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	c.reset()
	if n := tr.retransmitFrom(32765); n != 0 || len(c.sent()) != 0 {
		t.Fatalf("resent %d", n)
	}
}

func TestRetransmitIsFifteenBitWrapAware(t *testing.T) { // test_retransmit_is_fifteen_bit_wrap_aware
	tr, c := retransmitTransport(t)
	tr.localSeq = 32765
	for range 4 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	c.reset()
	if n := tr.retransmitFrom(32767); n != 3 || !slices.Equal(seqsOf(c.sent()), []uint16{32767, 0, 1}) {
		t.Fatalf("resent %d: %v", n, seqsOf(c.sent()))
	}
}

func TestSecondRequestForSameRangeResendsAgain(t *testing.T) { // test_second_request_for_same_range_resends_again
	tr, c := retransmitTransport(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	c.reset()
	if tr.retransmitFrom(1) != 1 || tr.retransmitFrom(1) != 1 || len(c.sent()) != 2 {
		t.Fatalf("sent %d", len(c.sent()))
	}
}

func TestVolleyIsCappedPerRequest(t *testing.T) { // test_volley_is_capped_per_request
	tr, c := retransmitTransport(t)
	tr.maxBurst = 4
	for range 10 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	c.reset()
	if n := tr.retransmitFrom(1); n != 4 || !slices.Equal(seqsOf(c.sent()), []uint16{1, 2, 3, 4}) {
		t.Fatalf("resent %d: %v", n, seqsOf(c.sent()))
	}
}

// requestRaw is make_request_raw: a 12-byte REQUEST_RETRANSMISSION control
// packet with the requested sequence spliced into header bytes 6-7.
func requestRaw(requestSeq uint16) []byte {
	b := datagram(0x8001, 0, flagRequestRetransmission, nil)
	binary.BigEndian.PutUint16(b[6:], requestSeq)
	return b
}

func TestParsePacketReadsRequestSeqFromBytes6And7(t *testing.T) { // test_from_bytes_parses_request_seq_from_bytes_6_7
	p, err := parsePacket(requestRaw(0x1234))
	if err != nil {
		t.Fatal(err)
	}
	if p.retransmitFrom != 0x1234 || p.remoteSeq == p.retransmitFrom {
		t.Fatalf("%+v", p)
	}
}

func TestReceivePathServicesARetransmissionRequest(t *testing.T) { // test_receive_path_services_a_retransmission_request
	tr, c := retransmitTransport(t)
	for range 3 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	c.reset()
	if _, err := tr.receive(requestRaw(2), switcherAddr); err != nil {
		t.Fatal(err)
	}
	if got := seqsOf(resent(c)); !slices.Equal(got, []uint16{2, 3}) {
		t.Fatalf("resent %v", got)
	}
}

func TestReceivePathIgnoresStaleBytes8And9(t *testing.T) { // test_receive_path_ignores_stale_bytes_8_9_field
	tr, c := retransmitTransport(t)
	for range 5 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	c.reset()
	raw := requestRaw(4)
	binary.BigEndian.PutUint16(raw[8:], 0)
	if _, err := tr.receive(raw, switcherAddr); err != nil {
		t.Fatal(err)
	}
	if got := seqsOf(resent(c)); !slices.Equal(got, []uint16{4, 5}) {
		t.Fatalf("resent %v", got)
	}
}

// --- test_transport_close_session.py -------------------------------------

func TestCloseSessionSendsGoodbyeAndClosesState(t *testing.T) { // test_close_session_sends_goodbye_and_closes_state
	tr, c := retransmitTransport(t)
	tr.state = stateEstablished
	tr.closeSession()
	sent := c.sent()
	if len(sent) != 1 {
		t.Fatalf("%d packets", len(sent))
	}
	p := sent[0]
	if p.flags&flagSYN == 0 || p.data[0] != helloClosing || p.session != 0x8001 {
		t.Fatalf("goodbye %+v", p)
	}
	if tr.state != stateClosed {
		t.Fatalf("state %v", tr.state)
	}
}

func TestCloseSessionNoopUnlessEstablished(t *testing.T) { // test_close_session_noop_unless_established
	tr, c := retransmitTransport(t)
	for _, s := range []transportState{stateClosed, stateSYNSent} {
		tr.state = s
		tr.closeSession()
		if len(c.sent()) != 0 {
			t.Fatalf("goodbye sent in state %v", s)
		}
	}
	tr.state = stateEstablished
	tr.closeSession()
	tr.closeSession()
	if len(c.sent()) != 1 {
		t.Fatalf("%d goodbyes", len(c.sent()))
	}
}

func TestSendDropsUnserializablePacket(t *testing.T) { // test_send_packet_low_drops_unserializable_packet
	tr, c := retransmitTransport(t)
	tr.sessionKnown = false
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	if len(c.sent()) != 0 || tr.localSeq != 0 {
		t.Fatalf("sent %d, localSeq %d", len(c.sent()), tr.localSeq)
	}
}

// test_udp_thread_death_leaves_disconnect_sentinel has no counterpart: the
// Go transport has no thread. What kills atemwire's UDP thread is a
// non-transient send error, and that is returned instead; a transient one
// drops the packet without using up its sequence number.
func TestSendErrors(t *testing.T) {
	tr, c := retransmitTransport(t)
	c.writeErr = &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", syscall.ENETUNREACH)}
	if err := tr.sendReliable([]byte("\x00\x08TEST")); err != nil {
		t.Fatalf("transient error returned: %v", err)
	}
	if tr.localSeq != 0 || len(tr.retransmitBuf) != 0 {
		t.Fatalf("dropped packet consumed seq: localSeq %d", tr.localSeq)
	}
	c.writeErr = &net.OpError{Op: "write", Net: "udp", Err: net.ErrClosed}
	if err := tr.sendReliable([]byte("\x00\x08TEST")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("fatal error not returned: %v", err)
	}
}

// --- test_lock_delivery.py (transport half) ------------------------------

func TestConnectionReadyKeepsThePacket(t *testing.T) { // test_connection_ready_sentinel_keeps_the_packet
	tr, _ := retransmitTransport(t)
	tr.state = stateEstablished
	tr.enableACK = true
	tr.hadTraffic = true
	lkob := fixtures.Bytes(t, "transport/LKOB-command.bin")
	p, err := parsePacket(datagram(0x8001, 19, flagReliable, lkob))
	if err != nil {
		t.Fatal(err)
	}
	tr.markNextConnected = true
	h, err := tr.handle(p)
	if err != nil {
		t.Fatal(err)
	}
	if !h.ready {
		t.Fatal("ready not reported")
	}
	if !bytes.Equal(h.data, lkob) {
		t.Fatal("the first packet after the state dump was lost")
	}
	if h, _ := tr.handle(p); h.ready {
		t.Fatal("ready reported twice")
	}
}

// test_stash_is_cleared_on_connect has no counterpart (the Go transport
// never stashes). Its neighbour: connect does not reset markNextConnected,
// in atemwire or here.
func TestConnectKeepsMarkNextConnected(t *testing.T) {
	tr, _ := newTestTransport(t)
	tr.markNextConnected = true
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	if !tr.markNextConnected {
		t.Fatal("connect reset markNextConnected; atemwire does not")
	}
}

// test_flush_sentinel_keeps_the_packet is about the paced bulk send queue
// used by uploads; it arrives with transfers (see docs/PORTING.md).

func receiveLow(t *testing.T, state transportState) (*transport, []packet) {
	t.Helper()
	tr, c := newTestTransport(t)
	tr.state = state
	if _, err := tr.receive(datagram(0x8001, 1, flagReliable, fixtures.Bytes(t, "transport/LKOB-command.bin")), switcherAddr); err != nil {
		t.Fatal(err)
	}
	return tr, c.sent()
}

func TestReliableDumpPacketIsACKedOnceEstablished(t *testing.T) { // test_reliable_dump_packet_is_acked_once_established
	tr, sent := receiveLow(t, stateEstablished)
	if len(sent) != 1 || sent[0].flags != flagACK || sent[0].ack != 1 || sent[0].remoteSeq != ackMagic {
		t.Fatalf("sent %+v", sent)
	}
	if !tr.enableACK {
		t.Fatal("enableACK not set")
	}
}

func TestNoACKBeforeTheSessionIsEstablished(t *testing.T) { // test_no_ack_before_the_session_is_established
	tr, sent := receiveLow(t, stateSYNSent)
	if len(sent) != 0 || tr.enableACK {
		t.Fatalf("sent %+v, enableACK %v", sent, tr.enableACK)
	}
}

// --- the handshake and the silence timeout (no Python counterpart) --------

func TestHandshake(t *testing.T) {
	tr, c := newTestTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	c.reset()
	// The switcher accepts under the temporary id ...
	got, err := tr.receive(datagram(helloSession, 0, flagSYN, []byte{helloAccepted, 0, 0, 0, 0, 0, 0, 0}), switcherAddr)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := tr.handle(got[0]); err != nil {
		t.Fatal(err)
	}
	if tr.state != stateEstablished {
		t.Fatalf("state %v", tr.state)
	}
	sent := c.sent()
	if len(sent) != 1 || sent[0].flags != flagACK || sent[0].session != helloSession {
		t.Fatalf("handshake ACK %+v", sent)
	}
	if tr.sessionKnown {
		t.Fatal("temporary session id kept after the handshake ACK")
	}
	// ... and the first dump packet carries the real one, which every ACK
	// then uses.
	c.reset()
	if _, err := tr.receive(datagram(0x8123, 1, flagReliable, fixtures.Bytes(t, "transport/LKOB-command.bin")), switcherAddr); err != nil {
		t.Fatal(err)
	}
	if !tr.sessionKnown || tr.sessionID != 0x8123 {
		t.Fatalf("session %#x known=%v", tr.sessionID, tr.sessionKnown)
	}
	if sent := c.sent(); len(sent) != 1 || sent[0].session != 0x8123 || sent[0].ack != 1 {
		t.Fatalf("ACK %+v", sent)
	}
}

func TestHandshakeRefusedStaysSYNSent(t *testing.T) {
	tr, c := newTestTransport(t)
	if err := tr.connect(); err != nil {
		t.Fatal(err)
	}
	c.reset()
	got, _ := tr.receive(datagram(helloSession, 0, flagSYN, []byte{0x03, 0, 0, 0, 0, 0, 0, 0}), switcherAddr)
	if _, err := tr.handle(got[0]); err != nil {
		t.Fatal(err)
	}
	if tr.state != stateSYNSent || len(c.sent()) != 1 {
		t.Fatalf("state %v, sent %d", tr.state, len(c.sent()))
	}
}

func TestSilenceTimeoutReconnects(t *testing.T) {
	tr, c := newTestTransport(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	tr.state = stateEstablished
	tr.lastActivity = now
	now = now.Add(silenceTimeout - time.Millisecond)
	if tr.silent() {
		t.Fatal("silent too early")
	}
	// Any datagram counts as activity, even one that is then dropped.
	if _, err := tr.receive(validFrame(), foreignAddr); err != nil {
		t.Fatal(err)
	}
	now = now.Add(silenceTimeout - time.Millisecond)
	if tr.silent() {
		t.Fatal("a foreign datagram did not count as activity")
	}
	now = now.Add(time.Millisecond)
	if !tr.silent() {
		t.Fatal("not silent after 5 s")
	}
	if err := tr.timeout(); err != nil {
		t.Fatal(err)
	}
	if tr.state != stateSYNSent || len(c.sent()) != 1 || c.sent()[0].flags != flagSYN {
		t.Fatalf("state %v sent %+v", tr.state, c.sent())
	}
	if tr.silent() {
		t.Fatal("the re-SYN did not count as activity")
	}
}
