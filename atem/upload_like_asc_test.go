// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Transfers as ATEM Software Control sends them (bmdwire#36, bd6ae77;
// test_upload_like_asc.py): the release and the next lock request in one
// packet, FTFD with the first budget, each budget whole; a release
// announced while we hold the store changes nothing. The transport half:
// a refused send is retried and keeps its sequence number, and what the
// switcher hasn't acknowledged is resent after resendAfter.

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"slices"
	"syscall"
	"testing"
)

const ascChunk = 1392 // the switcher's 1396, in whole 8-byte words

// ascData is ten chunks with no run of 0xFE words (an RLE header) to cut
// short.
func ascData() []byte {
	b := make([]byte, ascChunk*10)
	for i := range b {
		b[i] = byte(i * 7 % 251)
	}
	return b
}

func uploadTask(slot uint16) *transferTask {
	tk := task(taskUpload, StoreStills, slot)
	tk.data = ascData()
	tk.dataLength, tk.size = len(tk.data), len(tk.data)
	return tk
}

// feedBudget grants count chunks of 1396 bytes to the transfer in flight.
func feedBudget(t *testing.T, s *Session, count uint16) {
	t.Helper()
	p := make([]byte, 12)
	copy(p, u16be(s.lane.transfer.tid))
	binary.BigEndian.PutUint16(p[6:], 1396)
	binary.BigEndian.PutUint16(p[8:], count)
	feedCmd(t, s, "FTCD", p)
}

func feedDone(t *testing.T, s *Session, tid uint16) {
	t.Helper()
	feedCmd(t, s, "FTDC", append(u16be(tid), 1, 2))
}

func feedLockState(t *testing.T, s *Session, store uint16, taken bool) {
	t.Helper()
	p := append(u16be(store), 0, 0)
	if taken {
		p[2] = 1
	}
	feedCmd(t, s, "LKST", p)
}

// packetCodes is each reliable datagram the session sent, as opcodes.
func packetCodes(c *fakeConn) [][]string {
	var out [][]string
	for _, p := range c.sent() {
		if p.flags&flagReliable == 0 {
			continue
		}
		var codes []string
		_ = decodeFields(p.data, func(code string, _ []byte) error {
			codes = append(codes, code)
			return nil
		})
		out = append(out, codes)
	}
	return out
}

func lastPacket(c *fakeConn) []string {
	ps := packetCodes(c)
	if len(ps) == 0 {
		return nil
	}
	return ps[len(ps)-1]
}

func TestAnUploadGoesAsATEMSoftwareControlSendsIt(t *testing.T) {
	s, c := laneSession(t)
	tk := uploadTask(4)
	s.enqueueTask(tk)
	if p := packetCodes(c); len(p) != 1 || !slices.Equal(p[0], []string{"PLCK"}) {
		t.Fatalf("packets %v", p)
	}
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	if p := lastPacket(c); !slices.Equal(p, []string{"FTSD"}) {
		t.Fatalf("after the grant %v", p)
	}
	c.reset()
	feedBudget(t, s, 4)
	if p := packetCodes(c); len(p) != 5 || !slices.Equal(p[0], []string{"FTFD"}) {
		t.Fatalf("first budget: %v, want FTFD then the whole budget", p)
	}
	feedBudget(t, s, 4)
	feedBudget(t, s, 4)
	var got []byte
	for _, d := range commands(c, "FTDa") {
		got = append(got, d[4:]...)
	}
	if !bytes.Equal(got, tk.data) {
		t.Fatalf("%d bytes of chunks, not the data", len(got))
	}
	s.queueFlushed() // the drain, reported
	if n := count(c, "FTFD"); n != 1 {
		t.Fatalf("%d FTFD", n)
	}
	feedDone(t, s, tid)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) || commands(c, "LOCK")[0][2] != 0 {
		t.Fatalf("nothing queued: the release alone, got %v", p)
	}
	if r := result(t, tk); r.err != nil {
		t.Fatal(r.err)
	}
}

// firstOfTwoDone queues two transfers to slots 1 and 2 and finishes the
// first.
func firstOfTwoDone(t *testing.T, s *Session, kind taskKind) {
	t.Helper()
	for slot := uint16(1); slot <= 2; slot++ {
		if kind == taskUpload {
			s.enqueueTask(uploadTask(slot))
		} else {
			s.enqueueTask(task(taskDownload, StoreStills, slot))
		}
	}
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	if kind == taskUpload {
		feedBudget(t, s, 10)
	}
	feedDone(t, s, tid)
}

func TestTheReleaseAndTheNextRequestShareAPacket(t *testing.T) {
	for _, kind := range []taskKind{taskUpload, taskDownload} {
		s, c := laneSession(t)
		firstOfTwoDone(t, s, kind)
		if p := lastPacket(c); !slices.Equal(p, []string{"LOCK", "PLCK"}) {
			t.Fatalf("kind %d: handover %v, want LOCK and PLCK in one packet", kind, p)
		}
		lock, plck := commands(c, "LOCK"), commands(c, "PLCK")
		if lock[len(lock)-1][2] != 0 || binary.BigEndian.Uint16(plck[len(plck)-1][2:]) != 2 {
			t.Fatalf("kind %d: not a release and slot 2's request", kind)
		}
		// Granted at once, as the switcher does: no wait for an LKST.
		grant(t, s, 0)
		if s.lane.transfer == nil || s.lane.transfer.slot != 2 || !s.lane.requested {
			t.Fatalf("kind %d: slot 2 not started on the grant", kind)
		}
	}
}

func TestAClearNextTakesTheWholeStoreInTheSamePacket(t *testing.T) {
	s, c := laneSession(t)
	s.enqueueTask(uploadTask(1))
	s.enqueueTask(task(taskClear, StoreStills, 5))
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	feedBudget(t, s, 10)
	feedDone(t, s, tid)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK", "LOCK"}) {
		t.Fatalf("handover %v", p)
	}
	locks := commands(c, "LOCK")
	if locks[len(locks)-2][2] != 0 || locks[len(locks)-1][2] != 1 {
		t.Fatal("not a release then a whole-store request")
	}
}

func TestALateReleaseAnnouncementKeepsTheNewGrant(t *testing.T) {
	s, c := laneSession(t)
	firstOfTwoDone(t, s, taskUpload)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	sent := len(c.sent())
	// The first frame's release, announced a frame late, after the LKOB.
	feedLockState(t, s, 0, false)
	if !s.lane.locks[0] {
		t.Fatal("the late announcement cost us the new grant")
	}
	if len(c.sent()) != sent {
		t.Fatal("asked again as the holder")
	}
	feedLockState(t, s, 0, true)
	feedBudget(t, s, 10)
	feedDone(t, s, tid)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("the second frame not given back: %v", p)
	}
	if s.lane.locks[0] {
		t.Fatal("still held")
	}
}

func TestFTDE6StillLeavesTheRequestToTheReleaseBroadcast(t *testing.T) {
	s, c := laneSession(t)
	s.enqueueTask(uploadTask(1))
	grant(t, s, 0)
	feedCmd(t, s, "FTDE", append(u16be(s.lane.transfer.tid), 6, 0))
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("after FTDE 6 %v: another session is queued, no request with the release", p)
	}
	feedLockState(t, s, 0, false)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("the release broadcast didn't re-request: %v", p)
	}
}

func TestARetriedUploadStartsAgainWithItsMetadata(t *testing.T) {
	s, c := laneSession(t)
	tk := uploadTask(3)
	s.enqueueTask(tk)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	feedBudget(t, s, 4)
	feedCmd(t, s, "FTDE", append(u16be(tid), 1, 0)) // try again
	if p := lastPacket(c); !slices.Equal(p, []string{"FTSD"}) {
		t.Fatalf("retry %v", p)
	}
	c.reset()
	feedBudget(t, s, 10)
	if n := count(c, "FTFD"); n != 1 {
		t.Fatalf("%d FTFD for the retry", n)
	}
	var got []byte
	for _, d := range commands(c, "FTDa") {
		got = append(got, d[4:]...)
	}
	if !bytes.Equal(got, tk.data) {
		t.Fatal("the retry didn't send the data from the start")
	}
}

// --- the transport ----------------------------------------------------------

func enobufs() error {
	return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", syscall.ENOBUFS)}
}

func TestARefusedSendIsTriedAgain(t *testing.T) {
	tr, c := retransmitTransport(t)
	c.refuse, c.refuseErr = 2, enobufs()
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	if n := len(c.sent()); n != 1 || tr.localSeq != 1 {
		t.Fatalf("%d sent, localSeq %d", n, tr.localSeq)
	}
}

func TestAPacketTheNetworkKeepsRefusingKeepsItsNumber(t *testing.T) {
	tr, c := retransmitTransport(t)
	c.refuse, c.refuseErr = sendRetries, enobufs()
	sendReliable(t, tr, []byte("\x00\x08LOST"))
	if len(c.sent()) != 0 || tr.localSeq != 1 || tr.retransmitBuf[1] == nil {
		t.Fatalf("localSeq %d, buffered %v", tr.localSeq, tr.retransmitBuf[1] != nil)
	}
	sendReliable(t, tr, []byte("\x00\x08NEXT"))
	tr.retransmitFrom(1) // the switcher asks for the gap
	var seqs []uint16
	for _, p := range resent(c) {
		seqs = append(seqs, p.seq)
	}
	if !slices.Equal(seqs, []uint16{1, 2}) {
		t.Fatalf("resent %v", seqs)
	}
}

func established(t *testing.T) (*transport, *fakeConn) {
	t.Helper()
	tr, c := retransmitTransport(t)
	tr.state = stateEstablished
	return tr, c
}

func TestWhatTheSwitcherHasNotAcknowledgedIsResent(t *testing.T) {
	tr, c := established(t)
	for range 3 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	if tr.resendDue.IsZero() {
		t.Fatal("no resend timer")
	}
	tr.onACK(1) // it has 1, not 2 and 3
	tr.resendUnacked()
	if len(resent(c)) != 0 {
		t.Fatal("resent before resendAfter")
	}
	tr.resendDue = tr.now().Add(-1)
	tr.resendUnacked()
	var seqs []uint16
	for _, p := range resent(c) {
		seqs = append(seqs, p.seq)
	}
	if !slices.Equal(seqs, []uint16{2, 3}) || tr.resendDue.IsZero() {
		t.Fatalf("resent %v, timer %v", seqs, tr.resendDue)
	}
}

func TestAnACKForEverythingStopsTheTimer(t *testing.T) {
	tr, c := established(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	tr.onACK(2)
	if !tr.resendDue.IsZero() {
		t.Fatal("timer still set")
	}
	tr.resendDue = tr.now().Add(-1)
	tr.resendUnacked()
	if len(resent(c)) != 0 {
		t.Fatal("resent what was acknowledged")
	}
}

func TestNothingIsResentBeforeTheFirstACK(t *testing.T) {
	tr, c := established(t)
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	tr.resendDue = tr.now().Add(-1)
	tr.resendUnacked()
	if len(resent(c)) != 0 {
		t.Fatal("resent with no ACK to go by")
	}
}

func TestTheSwitchersACKsAreRead(t *testing.T) {
	tr, _ := established(t)
	for range 3 {
		sendReliable(t, tr, []byte("\x00\x08TEST"))
	}
	b := rawDatagram(0x8001, 0, flagACK, nil)
	binary.BigEndian.PutUint16(b[4:], 2)
	if _, err := tr.receive(b, switcherAddr); err != nil {
		t.Fatal(err)
	}
	if tr.ackedThrough != 2 {
		t.Fatalf("ackedThrough %d", tr.ackedThrough)
	}
}
