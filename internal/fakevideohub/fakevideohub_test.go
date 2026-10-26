// SPDX-License-Identifier: MIT

package fakevideohub

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/videohub"
)

func dial(t *testing.T, h *Hub) *videohub.Session {
	t.Helper()
	s, err := (&videohub.Dialer{DialContext: h.Dial}).Dial(context.Background(), Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Over loopback, through videohub.Dial's own net.Dialer: Listen serves a
// real TCP connection as Dial serves a pipe.
func TestOverLoopback(t *testing.T) {
	h := New()
	l, err := h.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, err := videohub.Dial(context.Background(), l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if snap, err := s.Snapshot(); err != nil || snap.Device.FriendlyName != "Studio Router" || len(snap.Outputs) != 16 {
		t.Fatalf("%+v %v", snap, err)
	}
}

func TestPreambleAndRoute(t *testing.T) {
	h := New()
	s := dial(t, h)
	snap, err := s.Snapshot()
	if err != nil || snap.Device.ModelName != h.Model || snap.Device.FriendlyName != "Studio Router" ||
		len(snap.Inputs) != 16 || len(snap.Outputs) != 16 || snap.Outputs[4].Label != "Monitor A" || *snap.Outputs[4].Source != 4 {
		t.Fatalf("%+v %v", snap, err)
	}
	other := dial(t, h)
	ch, stop := other.Subscribe()
	defer stop()
	if err := s.Route(context.Background(), 4, 6); err != nil {
		t.Fatal(err)
	}
	if h.Routing()[4] != 6 {
		t.Fatalf("hub routing %v", h.Routing())
	}
	select {
	case c := <-ch:
		if c.Key != "routing" || c.Values["4"] != "6" {
			t.Fatalf("%+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the other client wasn't told")
	}
}

func TestLockedOutputRefuses(t *testing.T) {
	h := New()
	s := dial(t, h)
	h.LockOutput(2, true)
	err := s.Route(context.Background(), 2, 0)
	if !errors.Is(err, videohub.ErrRefused) {
		t.Fatalf("%v", err)
	}
	if l := s.State().Locks[2]; l != "L" {
		t.Errorf("lock %q", l)
	}
	if err := s.Route(context.Background(), 99, 0); err == nil {
		t.Error("out of range routed")
	}
}

func TestLabelsAndPanelRoutes(t *testing.T) {
	h := New()
	s := dial(t, h)
	if err := s.SetOutputLabel(context.Background(), 4, "Director"); err != nil {
		t.Fatal(err)
	}
	if _, outs := h.Labels(); outs[4] != "Director" {
		t.Fatalf("%v", outs)
	}
	ch, stop := s.Subscribe()
	defer stop()
	h.Route(0, 15)
	for { // the label's own broadcast may come first
		select {
		case c := <-ch:
			if c.Key != "routing" {
				continue
			}
			if c.Values["0"] != "15" {
				t.Fatalf("%+v", c)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no push")
		}
		break
	}
	if !slices.Contains(h.Commands(), "OUTPUT LABELS:\n4 Director") {
		t.Errorf("commands %q", h.Commands())
	}
}

func TestNoMarkerAndDrop(t *testing.T) {
	h := New()
	h.NoMarker = true
	s := dial(t, h) // ends on the quiet
	if n, _ := s.State().VideoInputs(); n != 16 {
		t.Fatalf("inputs %d", n)
	}
	h.Drop()
	deadline := time.Now().Add(2 * time.Second)
	for s.Connected() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.Connected() {
		t.Fatal("still connected")
	}
}
