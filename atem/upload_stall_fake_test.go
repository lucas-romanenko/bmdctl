// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// An upload the switcher stops granting budgets to goes again in a new
// session, end to end (bmdwire#100): the fake holds the upload's second
// budget for good, as the bench's Constellation HD did after a dropout.

import (
	"context"
	"crypto/md5"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// dialStall dials sw with the upload stall limit shortened to d.
func dialStall(t *testing.T, sw *fakeatem.Switcher, d time.Duration) *atem.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dl := &atem.Dialer{Conn: sw}
	atem.SetUploadStall(dl, d)
	s, err := dl.Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAnUploadTheSwitcherStopsGrantingGoesAgainInANewSession(t *testing.T) {
	sw := newFake(t)
	s := dialStall(t, sw, 300*time.Millisecond)
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	sw.HoldUpload(never)
	frame := testFrame()
	rle, _ := atem.RLEEncode(frame)
	var mu sync.Mutex
	var reports []atem.Progress
	ctx := atem.WithProgress(ctxFor(t, 10*time.Second), func(p atem.Progress) {
		mu.Lock()
		reports = append(reports, p)
		mu.Unlock()
	})
	if err := s.UploadStillFrame(ctx, 3, frame, "Logo", ""); err != nil {
		t.Fatal(err)
	}
	got, name, hash, ok := sw.Still(3)
	if !ok || name != "Logo" || hash != md5.Sum(frame) || len(got) != len(rle) {
		t.Fatalf("stored %d bytes as %q", len(got), name)
	}
	if h := sw.Handshakes(); h != 2 {
		t.Fatalf("%d handshakes, want a second session", h)
	}
	// The progress is never more than what is true: it went back to
	// nothing for the fresh start, said so, and ended at the whole.
	mu.Lock()
	defer mu.Unlock()
	back := -1
	for i, p := range reports {
		if p.Retry == 1 && p.Retries == 2 && p.Done == 0 {
			back = i
			break
		}
	}
	if back < 1 {
		t.Fatalf("no report of the fresh start at nothing: %+v", reports)
	}
	for _, p := range reports[back:] {
		if p.Retry != 1 {
			t.Fatalf("a report after the retry without it: %+v", p)
		}
	}
	if last := reports[len(reports)-1]; last.Done != len(rle) {
		t.Fatalf("ends at %+v, want %d", last, len(rle))
	}
}

func TestAnUploadThatNeverMovesFailsSayingWhere(t *testing.T) {
	sw := newFake(t)
	s := dialStall(t, sw, 300*time.Millisecond)
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	sw.HoldUpload(never)
	retried := 0
	ctx := atem.WithProgress(ctxFor(t, 10*time.Second), func(p atem.Progress) {
		if p.Retry > retried {
			retried = p.Retry
			sw.HoldUpload(never) // and the next session's too
		}
	})
	err := s.UploadStillFrame(ctx, 3, testFrame(), "Logo", "")
	var se *atem.UploadStalledError
	if !errors.As(err, &se) || !errors.Is(err, atem.ErrUploadStalled) || se.Retries != 2 || retried != 2 {
		t.Fatalf("err %v after %d retries", err, retried)
	}
	if se.Phase == "" || se.Phase[len(se.Phase)-3:] != "cut" {
		t.Fatalf("phase %q: it stalled with its data going", se.Phase)
	}
	if _, _, _, ok := sw.Still(3); ok {
		t.Fatal("a still stored")
	}
	// The session carries on.
	if err := s.UploadMacro(ctxFor(t, 5*time.Second), 1, "after", "", []byte{8, 0, 7, 0, 1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
}
