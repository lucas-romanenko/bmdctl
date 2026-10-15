// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// Still and macro transfers end to end against the fake switcher, which
// serves and accepts them (internal/fakeatem/transfers.go): round trips,
// the FTDE statuses the engine recovers from and a fatal one, a lock held
// by another client, and the lane across a disconnect.

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// testFrame is a 1080p frame (the dump's video mode) in the switcher's
// format: mostly flat, so it compresses, with a few hundred kilobytes of
// detail, so it takes many chunks and several budgets.
func testFrame() []byte {
	f := make([]byte, 1920*1080*4)
	for i := 0; i < len(f); i += 8 {
		copy(f[i:], []byte{0x3a, 0x98, 0x02, 0x00, 0x3a, 0x98, 0x02, 0x00})
	}
	for i := 0; i < 300_000; i++ {
		f[i] = byte(i*7) & 0x3f
	}
	return f
}

func ctxFor(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func TestStillRoundTrip(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	frame := testFrame()
	if err := s.UploadStillFrame(ctxFor(t, 10*time.Second), 3, frame, "Logo", "the logo"); err != nil {
		t.Fatal(err)
	}
	stored, name, hash, ok := sw.Still(3)
	if !ok || name != "Logo" || hash != md5.Sum(frame) {
		t.Fatalf("the switcher has %v %q %x", ok, name, hash)
	}
	if got, err := atem.RLEDecode(stored); err != nil || !bytes.Equal(got, frame) {
		t.Fatalf("the switcher's copy differs (%v)", err)
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool {
		f := st.MediaPool.Files[3]
		return f != nil && f.IsUsed && f.Hash == md5.Sum(frame)
	})
	got, err := s.DownloadStill(ctxFor(t, 10*time.Second), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatal("the downloaded frame differs from the uploaded one")
	}
	// The lock went back after each frame.
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.Locks[0] != nil && !st.Locks[0].State })
	if sw.LockedBySession(0) || s.LockHolder(0) != atem.LockFree {
		t.Fatal("the store lock is still held")
	}
}

// UploadStill fits the image into the video mode's frame and converts it,
// as WebATEM's uploader does; the switcher gets exactly that frame.
func TestUploadStillFitsAndConverts(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 4), uint8(y * 5), 200, uint8(128 + x)})
		}
	}
	want, err := atem.RGBAToYCbCrA(atem.FitFrame(img, 1920, 1080), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UploadStill(ctxFor(t, 10*time.Second), 0, img, atem.StillOptions{Name: "card", Premultiply: true}); err != nil {
		t.Fatal(err)
	}
	stored, name, hash, _ := sw.Still(0)
	if got, err := atem.RLEDecode(stored); err != nil || !bytes.Equal(got, want) || name != "card" || hash != md5.Sum(want) {
		t.Fatalf("the switcher got another frame (%v)", err)
	}
}

// A still that doesn't compress (noise: the RLE is as long as the frame,
// as a photograph's nearly is) arrives whole under aggressive drain, the
// switcher's budgets its only pacing.
func TestUploadStillAggressiveDrain(t *testing.T) {
	sw := newFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: sw, AggressiveDrain: true}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	img := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
	rand.NewChaCha8([32]byte{1}).Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	want, err := atem.RGBAToYCbCrA(atem.FitFrame(img, 1920, 1080), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UploadStill(ctxFor(t, 30*time.Second), 3, img, atem.StillOptions{Name: "noise"}); err != nil {
		t.Fatal(err)
	}
	stored, name, hash, _ := sw.Still(3)
	if got, err := atem.RLEDecode(stored); err != nil || !bytes.Equal(got, want) || name != "noise" || hash != md5.Sum(want) {
		t.Fatalf("the switcher got another frame (%v)", err)
	}
}

func TestClearStill(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	rle, _ := atem.RLEEncode(make([]byte, 64))
	sw.PutStill(5, rle, "old", [16]byte{1})
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.MediaPool.Files[5] != nil && st.MediaPool.Files[5].IsUsed })
	if err := s.ClearStill(ctxFor(t, 5*time.Second), 5); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return !st.MediaPool.Files[5].IsUsed })
	if _, _, _, ok := sw.Still(5); ok {
		t.Fatal("the switcher still has the slot")
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return !st.Locks[0].State })
}

func TestMacroRoundTrip(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	code := bytes.Repeat([]byte{8, 0, 2, 0, 0, 0, 5, 0}, 500) // 500 ProgramInput ops, 4000 bytes
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), 2, "Intro", "roll the intro", code); err != nil {
		t.Fatal(err)
	}
	got, name, desc, ok := sw.Macro(2)
	if !ok || !bytes.Equal(got, code) || name != "Intro" || desc != "roll the intro" {
		t.Fatalf("the switcher has %v %q %q", ok, name, desc)
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool {
		p := st.Macros.Properties[2]
		return p != nil && p.IsUsed && p.Name == "Intro"
	})
	down, err := s.DownloadMacro(ctxFor(t, 5*time.Second), 2)
	if err != nil || !bytes.Equal(down, code) {
		t.Fatalf("download: %v", err)
	}
	// An empty slot downloads as empty bytes. Empty bytecode leaves a macro
	// with its name and no ops, used, which downloads as empty bytes too.
	if down, err := s.DownloadMacro(ctxFor(t, 5*time.Second), 7); err != nil || down == nil || len(down) != 0 {
		t.Fatalf("empty slot: %x %v", down, err)
	}
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), 2, "empty-test", "", nil); err != nil {
		t.Fatal(err)
	}
	if got, name, _, ok := sw.Macro(2); !ok || len(got) != 0 || name != "empty-test" {
		t.Fatalf("the switcher has %v %x %q", ok, got, name)
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool {
		p := st.Macros.Properties[2]
		return p != nil && p.IsUsed && p.Name == "empty-test"
	})
	if down, err := s.DownloadMacro(ctxFor(t, 5*time.Second), 2); err != nil || len(down) != 0 {
		t.Fatalf("name-only macro: %x %v", down, err)
	}
	// The macro store never takes a lock.
	if sw.LockedBySession(0xffff) || s.State().Locks[0xffff] != nil {
		t.Fatal("a macro transfer took a lock")
	}
}

// FTDE statuses 1 (try again), 5 (no lock) and 6 (another session
// collided) are recovered inside the engine and never reach the caller.
func TestRecoveredTransferStatuses(t *testing.T) {
	for _, status := range []uint8{1, 5, 6} {
		sw := newFake(t)
		s := dialFake(t, sw, 0)
		rle, _ := atem.RLEEncode(make([]byte, 4096))
		sw.PutStill(1, rle, "x", [16]byte{})
		sw.FailTransfers(status)
		got, err := s.DownloadStill(ctxFor(t, 5*time.Second), 1)
		if err != nil || len(got) != 4096 {
			t.Fatalf("status %d: %d bytes, %v", status, len(got), err)
		}
		if err := s.UploadMacro(ctxFor(t, 5*time.Second), 0, "m", "", []byte{8, 0, 7, 0, 25, 0, 0, 0}); err != nil {
			t.Fatalf("status %d, then a macro upload: %v", status, err)
		}
	}
}

// Any other status is fatal for that transfer: the caller gets a
// *TransferError at once, the lock goes back, and the next transfer works.
func TestFatalTransferStatus(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	sw.FailTransfers(2)
	start := time.Now()
	_, err := s.DownloadStill(ctxFor(t, 10*time.Second), 1)
	var te *atem.TransferError
	if !errors.As(err, &te) || te.Status != 2 || te.Store != 0 || te.Slot != 1 {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a fatal status took the timeout to report")
	}
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return !st.Locks[0].State })
	if _, err := s.DownloadStill(ctxFor(t, 5*time.Second), 1); err != nil {
		t.Fatalf("the next transfer: %v", err)
	}
}

// A lock held by another client is visible, a transfer waits for it, and
// one that gives up says the lock was never granted. When the other
// client lets go, a waiting transfer goes through.
func TestLockHeldByAnotherClient(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	rle, _ := atem.RLEEncode(make([]byte, 1024))
	sw.PutStill(2, rle, "x", [16]byte{})
	sw.HoldLock(0)
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.Locks[0] != nil && st.Locks[0].State })
	if h := s.LockHolder(0); h != atem.LockOtherClient {
		t.Fatalf("lock holder: %v", h)
	}
	_, err := s.DownloadStill(ctxFor(t, 300*time.Millisecond), 2)
	if !errors.Is(err, atem.ErrLockNotGranted) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if err := s.ClearStill(ctxFor(t, 300*time.Millisecond), 2); !errors.Is(err, atem.ErrLockNotGranted) {
		t.Fatalf("clear: %v", err)
	}
	// The withdrawn clear never runs: the still survives the release.
	done := make(chan error, 1)
	go func() {
		_, err := s.DownloadStill(ctxFor(t, 5*time.Second), 2)
		done <- err
	}()
	waitQueued(t, s, 1)
	sw.ReleaseLock(0)
	if err := <-done; err != nil {
		t.Fatalf("the waiting download: %v", err)
	}
	if _, _, _, ok := sw.Still(2); !ok {
		t.Fatal("a withdrawn clear emptied the slot")
	}
}

// bmdwire#14, the transfer half: an *XFC never pops a queued transfer.
func TestXFCLeavesAQueuedTransferAlone(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	rle, _ := atem.RLEEncode(make([]byte, 1024))
	sw.PutStill(2, rle, "x", [16]byte{})
	sw.HoldLock(0)
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.Locks[0] != nil && st.Locks[0].State })
	done := make(chan error, 1)
	go func() {
		_, err := s.DownloadStill(ctxFor(t, 5*time.Second), 2)
		done <- err
	}()
	waitQueued(t, s, 1)
	// The release follows the *XFC on the wire, so the session handles
	// the *XFC with the download still queued.
	sw.Send(fakeatem.Command("*XFC", []byte{0, 0, 0, 2, 0, 0, 0, 0}))
	sw.ReleaseLock(0)
	if err := <-done; err != nil {
		t.Fatalf("the queued download: %v", err)
	}
}

// A disconnect fails every waiting transfer with ErrDisconnected, and the
// new session's transfers work.
func TestDisconnectFailsWaitingTransfers(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	sw.HoldLock(0)
	waitState(t, s, 2*time.Second, func(st atem.State) bool { return st.Locks[0] != nil && st.Locks[0].State })
	done := make(chan error, 1)
	go func() {
		_, err := s.DownloadStill(ctxFor(t, 5*time.Second), 2)
		done <- err
	}()
	waitQueued(t, s, 1)
	sw.Send(make([]byte, 8)) // corruption: re-handshake
	if err := <-done; !errors.Is(err, atem.ErrDisconnected) {
		t.Fatalf("err %v", err)
	}
	select {
	case <-s.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("not ready again")
	}
	sw.ReleaseLock(0)
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), 1, "after", "", []byte{8, 0, 7, 0, 1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
}

// Transfers run alongside commands: a command sent during a download is
// echoed while it is in flight.
func TestCommandsDuringATransfer(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	frame := testFrame()
	rle, _ := atem.RLEEncode(frame)
	sw.PutStill(4, rle, "big", md5.Sum(frame))
	done := make(chan error, 1)
	go func() {
		got, err := s.DownloadStill(ctxFor(t, 10*time.Second), 4)
		if err == nil && !bytes.Equal(got, frame) {
			err = errors.New("frame differs")
		}
		done <- err
	}()
	if _, err := s.SendAndWait(ctxFor(t, 5*time.Second), atem.ProgramInput{Source: 3}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A transfer under WithProgress reports, on the caller's goroutine, how
// far it has come: an upload the bytes queued of what it sends, a still
// download the bytes received of the frame's size; a macro download
// nothing, as atemwire's transfer-progress skips the macro store.
func TestTransferProgress(t *testing.T) {
	sw := newFake(t)
	s := dialFake(t, sw, 0)
	frame := testFrame()
	rle, _ := atem.RLEEncode(frame)
	var reports []atem.Progress
	record := func(p atem.Progress) { reports = append(reports, p) }
	check := func(what string, total int) {
		t.Helper()
		if len(reports) < 2 {
			t.Fatalf("%s: %d reports", what, len(reports))
		}
		for i, p := range reports {
			if p.Total != total || p.Done <= 0 || p.Done > p.Total || i > 0 && p.Done < reports[i-1].Done {
				t.Fatalf("%s: report %d is %+v of %d", what, i, p, total)
			}
		}
		reports = nil
	}
	if err := s.UploadStillFrame(atem.WithProgress(ctxFor(t, 10*time.Second), record), 3, frame, "Logo", ""); err != nil {
		t.Fatal(err)
	}
	if last := reports[len(reports)-1]; last.Done != len(rle) {
		t.Errorf("upload ends at %+v, want %d", last, len(rle))
	}
	check("upload", len(rle))
	if _, err := s.DownloadStill(atem.WithProgress(ctxFor(t, 10*time.Second), record), 3); err != nil {
		t.Fatal(err)
	}
	check("download", 1920*1080*4)
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), 2, "Intro", "", bytes.Repeat([]byte{8, 0, 2, 0, 0, 0, 5, 0}, 500)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadMacro(atem.WithProgress(ctxFor(t, 5*time.Second), record), 2); err != nil || len(reports) != 0 {
		t.Errorf("macro download: %v, %d reports", err, len(reports))
	}
}
