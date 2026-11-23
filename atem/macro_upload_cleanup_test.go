// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// A failed macro upload cleans up after itself, and a slow one is given
// the time it needs: test_macro_upload_cleanup.py (bmdwire#101, 85d2f58),
// against the fake switcher. The switcher makes the slot a nameless macro
// as a macro upload starts (it pre-erases), and an upload that timed out
// or was refused left that behind ("Macro 100" on a Constellation HD).
// The timeout was the whole upload's; it is now time without progress.
// The tests of AtemProtocol.cancel_transfer are in transfer_test.go.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// bigMacro is bytecode longer than one budget of the fake's, so an
// upload held after its first budget can't finish.
func bigMacro() []byte {
	return bytes.Repeat([]byte{8, 0, 2, 0, 0, 0, 5, 0}, 3*fakeatem.ChunkSize*fakeatem.ChunkCount/8)
}

// deletes returns the macro slots the switcher was asked to delete
// (MAct 5).
func deletes(sw *fakeatem.Switcher) []uint16 {
	var out []uint16
	for _, c := range sw.Commands() {
		if len(c) >= 11 && string(c[4:8]) == "MAct" && c[10] == atem.MacroDelete {
			out = append(out, binary.BigEndian.Uint16(c[8:]))
		}
	}
	return out
}

// emptyMacroSlot finds a macro slot the session's state shows empty.
func emptyMacroSlot(t *testing.T, s *atem.Session) int {
	t.Helper()
	for slot := range 100 {
		if p := s.State().Macros.Properties[uint16(slot)]; p != nil && !p.IsUsed {
			return slot
		}
	}
	t.Fatal("no empty macro slot in the dump")
	return 0
}

// holdUploads holds every upload after its first budget until the test
// ends.
func holdUploads(t *testing.T, sw *fakeatem.Switcher) {
	release := make(chan struct{})
	sw.HoldUpload(release)
	t.Cleanup(func() { close(release) })
}

func TestATimedOutUploadIntoAnEmptySlotEmptiesItAgain(t *testing.T) { // test_a_timed_out_upload_into_an_empty_slot_empties_it_again
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	atem.SetMacroTimeout(t, 300*time.Millisecond)
	slot := emptyMacroSlot(t, s)
	holdUploads(t, sw)
	err := s.UploadMacro(context.Background(), slot, "m", "", bigMacro())
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "held a macro") {
		t.Fatalf("err %v", err)
	}
	if got := deletes(sw); len(got) != 1 || got[0] != uint16(slot) {
		t.Fatalf("deletes %v: the nameless macro it left is not deleted", got)
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return !st.MacroEntry(slot).IsUsed })
	if _, _, _, ok := sw.Macro(uint16(slot)); ok {
		t.Fatal("the slot still holds a macro")
	}
	// Its task is off the queue: the next upload goes.
	if n := atem.QueuedTransfers(s); n != 0 {
		t.Fatalf("%d transfers queued", n)
	}
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), slot, "next", "", []byte{8, 0, 2, 0, 0, 0, 5, 0}); err != nil {
		t.Fatal(err)
	}
}

func TestARefusedUploadIntoAnEmptySlotEmptiesItAgain(t *testing.T) { // test_a_refused_upload_into_an_empty_slot_empties_it_again
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	slot := emptyMacroSlot(t, s)
	sw.FailTransfers(2)
	err := s.UploadMacro(ctxFor(t, 5*time.Second), slot, "m", "", bigMacro())
	var te *atem.TransferError
	if !errors.As(err, &te) || te.Status != 2 {
		t.Fatalf("err %v", err)
	}
	if got := deletes(sw); len(got) != 1 || got[0] != uint16(slot) {
		t.Fatalf("deletes %v", got)
	}
}

func TestASlotThatHeldAMacroIsNotDeletedAndTheErrorSaysItIsGone(t *testing.T) { // test_a_slot_that_held_a_macro_is_not_deleted_and_the_error_says_it_is_gone
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	atem.SetMacroTimeout(t, 300*time.Millisecond)
	slot := emptyMacroSlot(t, s)
	sw.PutMacro(uint16(slot), []byte{8, 0, 2, 0, 0, 0, 5, 0}, "Old", "")
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.MacroEntry(slot).IsUsed })
	holdUploads(t, sw)
	err := s.UploadMacro(context.Background(), slot, "m", "", bigMacro())
	if err == nil || !strings.Contains(err.Error(), "held a macro, which the switcher erased as the upload started") {
		t.Fatalf("err %v", err)
	}
	if got := deletes(sw); len(got) != 0 {
		t.Fatalf("deletes %v", got)
	}
}

func TestASlotTheStateDoesNotKnowIsLeftAlone(t *testing.T) { // test_a_slot_the_state_does_not_know_is_left_alone
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	atem.SetMacroTimeout(t, 300*time.Millisecond)
	const slot = 1000 // beyond the dump's MPrp
	if s.State().Macros.Properties[slot] != nil {
		t.Fatal("the dump has the slot")
	}
	holdUploads(t, sw)
	if err := s.UploadMacro(context.Background(), slot, "m", "", bigMacro()); err == nil {
		t.Fatal("no error")
	}
	if got := deletes(sw); len(got) != 0 {
		t.Fatalf("deletes %v", got)
	}
}

func TestASuccessfulUploadDeletesNothing(t *testing.T) { // test_a_successful_upload_deletes_nothing
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	slot := emptyMacroSlot(t, s)
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), slot, "m", "", bigMacro()); err != nil {
		t.Fatal(err)
	}
	if got := deletes(sw); len(got) != 0 {
		t.Fatalf("deletes %v", got)
	}
	if _, name, _, ok := sw.Macro(uint16(slot)); !ok || name != "m" {
		t.Fatalf("slot holds %q, %v", name, ok)
	}
}

// bmdwire 1.14.7 gave the whole upload 10 s, however it was going. Now the
// limit is time without progress: here 0.3 s, and an upload whose budgets
// come 0.2 s apart takes over a second and finishes.
func TestASlowUploadThatKeepsMovingIsNotCutOff(t *testing.T) { // test_a_slow_upload_that_keeps_moving_is_not_cut_off
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	atem.SetMacroTimeout(t, 300*time.Millisecond)
	slot := emptyMacroSlot(t, s)
	sw.PaceUploads(200 * time.Millisecond)
	data := bytes.Repeat([]byte{8, 0, 2, 0, 0, 0, 5, 0}, 7*fakeatem.ChunkSize*fakeatem.ChunkCount/8)
	start := time.Now()
	if err := s.UploadMacro(context.Background(), slot, "slow", "", data); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < time.Second {
		t.Fatalf("took %v: not long enough to outlive the timeout", took)
	}
	if got, _, _, _ := sw.Macro(uint16(slot)); !bytes.Equal(got, data) {
		t.Fatal("the macro arrived different")
	}
}

// The macro download's timeout is time without data too (bmdwire#101's
// download_macro_bytecode; bmdwire#104's download_macro).
func TestAMacroDownloadIsNoProgressToo(t *testing.T) { // test_a_macro_download_is_no_progress_too
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	const timeout = 150 * time.Millisecond
	atem.SetMacroTimeout(t, timeout)
	slot := emptyMacroSlot(t, s)
	data := bytes.Repeat([]byte{8, 0, 2, 0, 0, 0, 5, 0}, 10*fakeatem.ChunkSize*fakeatem.ChunkCount/8)
	sw.PutMacro(uint16(slot), data, "big", "")
	sw.PaceDownloads(50 * time.Millisecond) // chunks never 150 ms apart; about 0.5 s in all
	start := time.Now()
	got, err := s.DownloadMacro(context.Background(), slot)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("%d bytes, %v", len(got), err)
	}
	if took := time.Since(start); took <= timeout {
		t.Fatalf("took %v: not long enough to outlive the timeout", took)
	}
}
