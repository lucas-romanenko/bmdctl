// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem_test

import (
	"context"
	"errors"
	"net"
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
	// The handshake completed and the session said goodbye to: nothing
	// dangles, neither a SYN answered nor a session open.
	if hs, bye := sw.Handshakes(), sw.Goodbyes(); hs != 1 || bye != 1 {
		t.Errorf("%d handshakes, %d goodbyes", hs, bye)
	}
	if sw.Dangling() {
		t.Error("the probe left a session dangling")
	}
}

// What the fake calls dangling: a SYN answered and never completed.
func TestAHalfOpenHandshakeDangles(t *testing.T) {
	sw := fakeatem.New(nil)
	defer sw.Close()
	syn := []byte{0x10, 0x14, 0x13, 0x37, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	sw.WriteTo(syn, &net.UDPAddr{IP: net.ParseIP(fakeatem.Address), Port: 9910})
	if !sw.Dangling() {
		t.Error("an answered SYN doesn't dangle")
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
