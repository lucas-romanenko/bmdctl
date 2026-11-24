// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

// A packet the switcher sends once and never again gets through: the
// session asks for it (bmdwire#100, 9824cab). The fake here behaves as
// the bench's Constellation HD did after a dropout: it sends nothing again
// on its own, only what it is asked for.

import (
	"context"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

func TestALostPacketTheSwitcherNeverSendsAgainIsAskedFor(t *testing.T) {
	sw := newFake(t)
	sw.ResendOnlyWhenAsked(true)
	sw.DropSequences(3) // a packet of the dump, lost once
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatalf("the dump never completed: %v", err)
	}
	defer s.Close()
	// And once established: a command's echo lost once still comes.
	sw.DropToClient(1)
	if _, err := s.SendAndWait(ctxFor(t, 3*time.Second), atem.ProgramInput{Source: 4}); err != nil {
		t.Fatal(err)
	}
}
