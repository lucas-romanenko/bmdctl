// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

func TestProbeFindsTheSwitcher(t *testing.T) {
	sw := fakeatem.New(fixtures.Bytes(t, "dump/initial-dump.bin"))
	if err := (&atem.Dialer{Conn: sw}).Probe(context.Background(), fakeatem.Address); err != nil {
		t.Fatal(err)
	}
	if hs := sw.Handshakes(); hs != 0 {
		t.Errorf("%d sessions opened", hs)
	}
}

func TestProbeFindsNothing(t *testing.T) {
	sw := fakeatem.New(fixtures.Bytes(t, "dump/initial-dump.bin"))
	sw.Silence(true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := (&atem.Dialer{Conn: sw}).Probe(ctx, fakeatem.Address); !errors.Is(err, atem.ErrNoSwitcher) {
		t.Errorf("%v", err)
	}
}
