// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Transfers as ATEM Software Control sends them (bmdwire#36, bd6ae77;
// test_upload_like_asc.py): FTFD with the first budget, each budget whole;
// a release announced while we hold the store changes nothing; and, since
// bmdwire#38 and #40, another client's turn: after every release the next
// request waits until the store is announced free and lockCourtesy more,
// never while another client holds it. The transport half:
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
	"time"
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

// after moves the test's clock on by d and lets the lane's yields run, as
// its timer would.
func after(s *Session, clock *time.Time, d time.Duration) {
	*clock = clock.Add(d)
	s.serviceYields()
}

func TestTheStoreIsLeftFreeUntilTheReleaseIsSeen(t *testing.T) {
	for _, kind := range []taskKind{taskUpload, taskDownload} {
		s, c := laneSession(t)
		clock := moveClock(s)
		firstOfTwoDone(t, s, kind)
		if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) || commands(c, "LOCK")[len(commands(c, "LOCK"))-1][2] != 0 {
			t.Fatalf("kind %d: %v, want the release alone", kind, p)
		}
		asked := len(packetCodes(c))
		feedLockState(t, s, 0, false) // the release announced
		after(s, clock, lockCourtesy/2)
		if len(packetCodes(c)) != asked {
			t.Fatalf("kind %d: asked before anyone could take it", kind)
		}
		after(s, clock, lockCourtesy)
		if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
			t.Fatalf("kind %d: %v, want slot 2's request", kind, p)
		}
		grant(t, s, 0)
		if s.lane.transfer == nil || s.lane.transfer.slot != 2 || !s.lane.requested {
			t.Fatalf("kind %d: slot 2 not started on the grant", kind)
		}
	}
}

func TestWithoutAnAnnouncementWeAskAfterLockQuiet(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	firstOfTwoDone(t, s, taskUpload)
	asked := len(packetCodes(c))
	after(s, clock, lockQuiet*9/10)
	if len(packetCodes(c)) != asked {
		t.Fatal("asked before lockQuiet")
	}
	after(s, clock, lockQuiet/5)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v after lockQuiet", p)
	}
}

// Not announced free after our release: someone waiting was handed it.
// Their transfers ending (FTDC, another session's id) keep us waiting.
func TestAStoreHandedToAnotherClientWaitsWhileItWorks(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	firstOfTwoDone(t, s, taskUpload)
	asked := len(packetCodes(c))
	for range 3 {
		after(s, clock, lockQuiet*6/10)
		feedDone(t, s, 0x6c02)
	}
	if len(packetCodes(c)) != asked {
		t.Fatal("asked while another client worked")
	}
	feedLockState(t, s, 0, false) // done: announced free
	after(s, clock, lockCourtesy)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v once free", p)
	}
}

// The timer set for the turn fires after someone at work moved it: it is
// set again for the new time, and nothing is asked before.
func TestAYieldMovedBySomeoneAtWorkIsTimedAgain(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	firstOfTwoDone(t, s, taskUpload)
	asked := len(packetCodes(c))
	*clock = clock.Add(lockQuiet / 2)
	feedDone(t, s, 0x6c02)
	after(s, clock, lockQuiet/2) // the first timer
	if len(packetCodes(c)) != asked {
		t.Fatal("asked while another client worked")
	}
	if want := clock.Add(lockQuiet / 2); !s.lane.yieldDue.Equal(want) {
		t.Fatalf("timer at %v, want %v", s.lane.yieldDue, want)
	}
	after(s, clock, lockQuiet/2)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v at the new time", p)
	}
}

func TestAnotherClientsTurnLastsUntilTheyRelease(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	firstOfTwoDone(t, s, taskUpload)
	asked := len(packetCodes(c))
	feedLockState(t, s, 0, true) // taken, not by us
	after(s, clock, lockQuiet*2)
	if len(packetCodes(c)) != asked {
		t.Fatal("asked while another client held it")
	}
	feedLockState(t, s, 0, false)
	after(s, clock, lockCourtesy)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v once they let go", p)
	}
}

func TestAStoreAnotherClientHoldsIsNotAskedFor(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	feedLockState(t, s, 0, true) // someone else's
	s.enqueueTask(task(taskDownload, StoreStills, 3))
	if len(packetCodes(c)) != 0 {
		t.Fatal("asked for another client's store")
	}
	feedLockState(t, s, 0, false)
	if len(packetCodes(c)) != 0 {
		t.Fatal("asked before they could take it again")
	}
	after(s, clock, lockCourtesy)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v once free", p)
	}
}

// ASC releases one still and takes the store for its next in one packet,
// announced as a release and a lock together.
func TestAnotherClientReleasingAndRetakingKeepsItsTurn(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	feedLockState(t, s, 0, true)
	s.enqueueTask(task(taskDownload, StoreStills, 3))
	feedLockState(t, s, 0, false)
	feedLockState(t, s, 0, true)
	after(s, clock, lockCourtesy*2)
	if len(packetCodes(c)) != 0 {
		t.Fatal("asked in their turn")
	}
}

// WebATEM's media pool watcher queues one still at a time, the next after
// decoding the last: nothing is queued at the release (bmdwire#40).
func TestATransferQueuedAfterTheReleaseWaitsToo(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	s.enqueueTask(task(taskDownload, StoreStills, 1))
	grant(t, s, 0)
	feedDone(t, s, s.lane.transfer.tid)
	after(s, clock, 250*time.Millisecond) // the decode
	s.enqueueTask(task(taskDownload, StoreStills, 2))
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("%v: asked before the store was seen free", p)
	}
	feedLockState(t, s, 0, false)
	after(s, clock, lockCourtesy)
	if p := lastPacket(c); !slices.Equal(p, []string{"PLCK"}) {
		t.Fatalf("%v once free", p)
	}
}

func TestAClearNextTakesTheWholeStoreAfterTheYield(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	s.enqueueTask(uploadTask(1))
	s.enqueueTask(task(taskClear, StoreStills, 5))
	grant(t, s, 0)
	feedBudget(t, s, 10)
	feedDone(t, s, s.lane.transfer.tid)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("%v, want the release alone", p)
	}
	after(s, clock, lockQuiet)
	locks := commands(c, "LOCK")
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) || locks[len(locks)-1][2] != 1 {
		t.Fatalf("%v, want a whole-store request", p)
	}
}

// The announcement can come after lockQuiet, and so after the LKOB for
// the next request.
func TestALateReleaseAnnouncementKeepsTheNewGrant(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	firstOfTwoDone(t, s, taskUpload)
	after(s, clock, lockQuiet)
	grant(t, s, 0)
	tid := s.lane.transfer.tid
	sent := len(c.sent())
	feedLockState(t, s, 0, false)
	if !s.lane.locks[0] {
		t.Fatal("the late announcement cost us the new grant")
	}
	if len(c.sent()) != sent {
		t.Fatal("asked again as the holder")
	}
	feedBudget(t, s, 10)
	feedDone(t, s, tid)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) || s.lane.locks[0] {
		t.Fatalf("the second frame not given back: %v", p)
	}
}

func TestFTDE6StillLeavesTheRequestToTheReleaseBroadcast(t *testing.T) {
	s, c := laneSession(t)
	clock := moveClock(s)
	s.enqueueTask(uploadTask(1))
	grant(t, s, 0)
	feedCmd(t, s, "FTDE", append(u16be(s.lane.transfer.tid), 6, 0))
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("after FTDE 6 %v: another session is queued, no request with the release", p)
	}
	after(s, clock, lockCourtesy*3)
	if p := lastPacket(c); !slices.Equal(p, []string{"LOCK"}) {
		t.Fatalf("%v: asked before the store was seen free", p)
	}
	feedLockState(t, s, 0, false)
	after(s, clock, lockCourtesy)
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
