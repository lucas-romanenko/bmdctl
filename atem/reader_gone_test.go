// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// A read goroutine that has ended never strands a caller (bmdwire#100,
// 61d596b, test_udp_thread_gone.py). atemwire's UDP thread puts one None
// on the receive queue as it ends, and the first loop() took it; every
// loop() after that waited on the queue for good, a caller's deadline
// with it (a bench whose own code ended the thread mid-upload: 20 minutes
// until Ctrl-C). Here the read goroutine hands its error to the run
// goroutine, which ends the session: whoever waits is told at once, and
// every call after returns at once.

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

// readerDies is the fake switcher whose reads fail once die is closed:
// the read goroutine meets something unexpected and ends.
type readerDies struct {
	*fakeatem.Switcher
	die  chan struct{}
	once sync.Once
}

func (c *readerDies) ReadFrom(p []byte) (int, net.Addr, error) {
	n, from, err := c.Switcher.ReadFrom(p)
	select {
	case <-c.die:
		return 0, nil, errors.New("something unexpected in the read goroutine")
	default:
	}
	return n, from, err
}

func (c *readerDies) kill() { c.once.Do(func() { close(c.die) }) }

func TestACallerIsToldAtOnceWhenTheReadGoroutineEnds(t *testing.T) { // test_a_caller_deadline_holds_with_the_thread_gone
	sw := newFake(t)
	conn := &readerDies{Switcher: sw, die: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: conn}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	sw.HoldUpload(release) // the upload stops part way, as on the bench
	moved := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		ctx := atem.WithProgress(ctxFor(t, time.Minute), func(atem.Progress) {
			select {
			case moved <- struct{}{}:
			default:
			}
		})
		done <- s.UploadStillFrame(ctx, 3, testFrame(), "x", "")
	}()
	select {
	case <-moved:
	case <-time.After(5 * time.Second):
		t.Fatal("the upload never started")
	}
	conn.kill()
	start := time.Now()
	select {
	case err := <-done:
		if !errors.Is(err, atem.ErrClosed) {
			t.Fatalf("err %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the upload waited on with the read goroutine gone")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("told after %v", d)
	}
}

func TestEveryCallReturnsAtOnceOnceTheReadGoroutineHasEnded(t *testing.T) { // test_loop_returns_again_and_again_once_the_thread_is_gone
	sw := newFake(t)
	conn := &readerDies{Switcher: sw, die: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: conn}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	conn.kill()
	deadline := time.Now().Add(5 * time.Second)
	for s.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("the session lives on with its read goroutine gone")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for range 3 {
		start := time.Now()
		if err := s.Send(atem.ProgramInput{Source: 2}); !errors.Is(err, atem.ErrClosed) {
			t.Fatalf("Send: %v", err)
		}
		if _, err := s.DownloadMacro(context.Background(), 1); !errors.Is(err, atem.ErrClosed) {
			t.Fatalf("DownloadMacro: %v", err)
		}
		if _, err := s.SendAndWait(context.Background(), atem.ProgramInput{Source: 2}); !errors.Is(err, atem.ErrClosed) {
			t.Fatalf("SendAndWait: %v", err)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("returned after %v", d)
		}
	}
}
