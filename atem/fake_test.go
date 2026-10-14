// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// Sessions against the fake switcher (internal/fakeatem) for the places
// where bmdctl deliberately differs from bmdwire v1.3.0 (docs/PORTING.md,
// "Deliberate differences"). Each test fails on bmdwire's behaviour.

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

func newFake(t *testing.T) *fakeatem.Switcher {
	t.Helper()
	sw := fakeatem.New(fixtures.Bytes(t, "dump/initial-dump.bin"))
	t.Cleanup(func() { sw.Close() })
	return sw
}

// dialFake dials sw; silence, if not zero, replaces the 5 s timeout.
func dialFake(t *testing.T, sw *fakeatem.Switcher, silence time.Duration) *atem.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d := &atem.Dialer{Conn: sw}
	if silence > 0 {
		atem.SetSilence(d, silence)
	}
	s, err := d.Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// waitFor waits up to d for a change of kind k.
func waitFor(t *testing.T, ch <-chan atem.Change, k atem.ChangeKind, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				t.Fatal("subscription closed")
			}
			if c.Kind == k {
				return
			}
		case <-deadline:
			t.Fatalf("no change of kind %d within %v", k, d)
		}
	}
}

// waitState polls the session's state until ok returns true.
func waitState(t *testing.T, s *atem.Session, d time.Duration, ok func(atem.State) bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok(s.State()) {
		if time.Now().After(deadline) {
			t.Fatal("the state never got there")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitQueued waits until n transfers are queued on s's lane: a test that
// starts one on another goroutine and must act while it waits (for a lock
// another client holds) waits here, not for a sleep to be long enough.
func waitQueued(t *testing.T, s *atem.Session, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for atem.QueuedTransfers(s) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d transfers queued, want %d", atem.QueuedTransfers(s), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// bmdwire#11: the silence timeout counts only datagrams accepted from the
// switcher. A session that keeps sending to a switcher that has gone quiet,
// while a foreign peer and garbage keep arriving, still times out; in
// atemwire each of those restarts the 5 s window, forever.
func TestSilenceIgnoresOwnSendsAndForeignDatagrams(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, time.Second) // longer than the fake's 500 ms pings
	ch, cancel := s.Subscribe()
	defer cancel()
	sw.Silence(true)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		foreign := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 66), Port: 9910}
		switcher := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 9910}
		valid := []byte{0x08, 0x0c, 0x80, 0x01, 0, 0, 0, 0, 0, 0, 0, 1} // reliable, no data
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			_ = s.Send(atem.ProgramInput{Source: 1})
			sw.Inject(foreign, valid)
			sw.Inject(switcher, []byte{1, 2, 3}) // malformed
		}
	}()
	waitFor(t, ch, atem.ChangeDisconnected, 3*time.Second)
}

// bmdwire#12: packets from the previous session that arrive during a
// re-handshake are dropped unseen. In atemwire the first of them becomes
// the new session's sequence baseline, so the new dump looks like
// duplicates, and one arriving after the handshake ACK becomes the session
// id: the session never gets ready again.
func TestStalePacketsDuringARehandshake(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	ch, cancel := s.Subscribe()
	defer cancel()
	sw.SendStale(3)
	sw.Send(make([]byte, 8)) // a zero-length command header: re-handshake at once
	waitFor(t, ch, atem.ChangeDisconnected, 2*time.Second)
	waitFor(t, ch, atem.ChangeReady, 3*time.Second)
	if st := s.State(); st.VideoMode.Mode == nil || len(st.MEs) != 1 || st.MEs[0].Program == nil {
		t.Fatal("the new dump didn't arrive whole")
	}
	if _, ok := s.State().Raw["Stal"]; ok {
		t.Fatal("a stale packet's data was delivered")
	}
	if n := sw.Handshakes(); n != 2 {
		t.Fatalf("%d handshakes", n)
	}
	// The new session works: its commands reach the switcher under the
	// new id and are echoed.
	ctx, cancelWait := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelWait()
	if _, err := s.SendAndWait(ctx, atem.ProgramInput{Source: 4}); err != nil {
		t.Fatal(err)
	}
}

// bmdwire#13 (bmdwire#27): a state dump that ends without its video mode
// lost its first packet for good, so the session handshakes again for a
// fresh dump at once, and Dial returns a session that has one.
func TestLosingTheFirstDumpPacketRehandshakes(t *testing.T) {
	sw := newFake(t)
	sw.DropSequences(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	s, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer s.Close()
	if s.State().VideoMode.Mode == nil {
		t.Error("no video mode")
	}
	if n := sw.Handshakes(); n != 2 {
		t.Errorf("%d handshakes, want 2", n)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Dial took %v", d)
	}
}

// After incompleteDumpRetries fresh dumps that lost their first packet
// too, Dial fails, saying which sequence number came first, rather than
// handshaking for ever (atemwire then carries on without a video mode).
func TestAFirstDumpPacketAlwaysLostFailsDial(t *testing.T) {
	sw := newFake(t)
	sw.DropSequencesAlways(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
	if !errors.Is(err, atem.ErrIncompleteDump) || !strings.Contains(err.Error(), "sequence 2") {
		t.Fatalf("Dial: %v", err)
	}
	if n := sw.Handshakes(); n != 4 {
		t.Errorf("%d handshakes, want 4 (the first and three more)", n)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Dial took %v to fail", d)
	}
}

// bmdwire#14: an *XFC (the TCP proxy's transfer-complete event) on UDP is
// dropped and the rest of its packet is processed. atemwire raises
// KeyError looking up a transfer queue and loses the PrgI after it.
func TestXFCDoesNotLoseTheRestOfThePacket(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	sw.Send(fakeatem.Command("*XFC", []byte{0, 0, 0, 3, 1, 0, 0, 0}), fakeatem.Command("PrgI", []byte{0, 0, 0, 9}))
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.MEs[0].Program.Source == 9 })
}

// bmdwire#15: a store lock taken by another client is stored as well as
// its release, per store, so a caller can see it. atemwire drops a lock
// taken and stores only releases.
func TestAnotherClientsLockIsVisible(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	sw.HoldLock(0)
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.Locks[0] != nil && st.Locks[0].State })
	sw.ReleaseLock(0)
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return !st.Locks[0].State })
}
