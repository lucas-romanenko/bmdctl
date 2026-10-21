// SPDX-License-Identifier: MIT

package videohub

// What a Session adds to videohubwire (docs/PORTING.md, "Videohub"): it
// reads all the time, publishes the blocks the hub pushes, matches
// answers to commands in order, and ends rather than read a late answer
// as the next command's.

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPushesArePublished(t *testing.T) {
	s, c := connect(t, preamble(t))
	ch, stop := s.Subscribe()
	defer stop()
	c.push([]byte("VIDEO OUTPUT ROUTING:\n1 3\n2 x\n\nCONFIGURATION:\nTake Mode: true\n\nINPUT LABELS:\n0 Renamed\n\n"))
	for _, want := range []Change{
		{Kind: ChangeState, Key: "routing", Values: map[string]string{"1": "3"}},
		{Kind: ChangeState, Key: "input labels", Values: map[string]string{"0": "Renamed"}},
	} {
		select {
		case got := <-ch:
			if got.Kind != want.Kind || got.Key != want.Key || !equalMaps(got.Values, want.Values) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s change", want.Key)
		}
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// An ACK with no command waiting is dropped, not kept for the next
// command: videohubwire's caller would have read it as its own.
func TestAnAnswerToNothingIsDropped(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.push([]byte("ACK\n\n"))
	settled(t, s, c)
	c.reply([]byte("NAK\n\n"))
	if err := s.Route(context.Background(), 1, 3); !errors.Is(err, ErrRefused) {
		t.Fatalf("route answered by the stray ACK: %v", err)
	}
}

// A context that ends first abandons the wait; the answer, when it comes,
// is dropped, not given to the next command.
func TestALateAnswerIsDropped(t *testing.T) {
	s, c := connect(t, preamble(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Route(ctx, 1, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("route: %v", err)
	}
	c.push([]byte("ACK\n\n"))
	settled(t, s, c)
	c.reply([]byte("NAK\n\n"))
	if err := s.Route(context.Background(), 2, 1); !errors.Is(err, ErrRefused) {
		t.Fatalf("second route took the first's answer: %v", err)
	}
	if s.State().Routing[1] != 3 {
		t.Errorf("the abandoned route's ACK wasn't applied: %v", s.State().Routing)
	}
}

// A timeout ends the session: the commands still waiting fail, and the
// subscribers hear it.
func TestATimeoutEndsTheSession(t *testing.T) {
	s, _ := connect(t, preamble(t))
	ch, _ := s.Subscribe()
	err := s.Route(context.Background(), 1, 3)
	if !errors.Is(err, ErrTimeout) || s.Connected() {
		t.Fatalf("%v; connected %v", err, s.Connected())
	}
	select {
	case c := <-ch:
		if c.Kind != ChangeDisconnected {
			t.Fatalf("got %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no disconnect")
	}
	if _, ok := <-ch; ok {
		t.Error("channel still open")
	}
	if err := s.Ping(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("ping after: %v", err)
	}
	if st := s.State(); st.Routing[0] != 0 || len(st.Device) == 0 {
		t.Errorf("the state at the end is gone: %+v", st)
	}
}

// The hub closing ends the session with videohubwire's message.
func TestTheHubClosingEndsTheSession(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.push([]byte{})
	err := s.Ping(context.Background())
	if !errors.Is(err, ErrClosed) || err.Error() != "connection closed by hub" {
		t.Fatalf("%v", err)
	}
}

// A NAK to a ping is applied as a block and waited past, as ping() does.
func TestANakToPingIsWaitedPast(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("NAK\n\nACK\n\n"))
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotAfterARoute(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("ACK\n\n"))
	if err := s.Send(context.Background(), Route{Dest: 3, Src: 0}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot()
	if err != nil || *snap.Outputs[3].Source != 0 || s.State().Name() != "Test Hub" {
		t.Fatalf("%+v %v", snap.Outputs[3], err)
	}
}
