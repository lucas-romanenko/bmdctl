// SPDX-License-Identifier: MIT

package fakeatem_test

import (
	"context"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

func dial(t *testing.T, sw *fakeatem.Switcher) *atem.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newSwitcher(t *testing.T) *fakeatem.Switcher {
	return fakeatem.New(fixtures.Bytes(t, "dump/initial-dump.bin"))
}

// The end-to-end test: dial the fake, put source 5 on program and wait
// for the switcher's echo; the state shows it.
func TestEndToEnd(t *testing.T) {
	s := dial(t, newSwitcher(t))
	if src := s.State().MEs[0].Program.Source; src != 2 {
		t.Fatalf("program before: %d, want 2 from the dump", src)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.SendAndWait(ctx, atem.ProgramInput{ME: 0, Source: 5})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key != "PrgI:0" || c.Value.(*atem.ProgramBusInputField).Source != 5 {
		t.Fatalf("echo %+v", c)
	}
	if src := s.State().MEs[0].Program.Source; src != 5 {
		t.Fatalf("program after: %d, want 5", src)
	}
}

// The dump replays into typed state, and the session asks for the time
// once it is complete.
func TestDumpIsReplayed(t *testing.T) {
	sw := newSwitcher(t)
	s := dial(t, sw)
	st := s.State()
	if st.Identity.Product == nil || st.Identity.Product.Name != "ATEM 1 M/E Constellation HD" {
		t.Errorf("product %+v", st.Identity.Product)
	}
	if st.VideoMode.Mode.Label() != "1080p50" || atem.DisplayFPS(st) != 25 {
		t.Errorf("video mode %+v", st.VideoMode.Mode)
	}
	if len(st.Inputs) != 8 || st.Inputs[3011].Name != "Media Player 1 Key" || st.Inputs[3011].PortType != 5 {
		t.Errorf("inputs %d %+v", len(st.Inputs), st.Inputs[3011])
	}
	if len(st.MEs) != 1 || len(st.MEs[0].USKs) != 4 || !st.MEs[0].USKs[0].OnAir.Enabled || st.MEs[0].USKs[1].OnAir.Enabled {
		t.Errorf("keyers %+v", st.MEs)
	}
	if st.MEs[0].Transition.Wipe.Pattern != 5 || st.MEs[0].Transition.Stinger.Duration != 150 {
		t.Errorf("transition %+v", st.MEs[0].Transition)
	}
	if len(st.DSKs) != 2 || !st.DSKs[0].State.OnAir || st.DSKs[0].Properties.Top != 9000 {
		t.Errorf("DSKs %+v", st.DSKs)
	}
	strip := st.Fairlight.Strips["2001.0"]
	if strip == nil || strip.Properties.Pan != -2000 || strip.Properties.State != 4 {
		t.Errorf("strip %+v", strip)
	}
	if st.Fairlight.Strips["1.0"].EQBands[0].BandFrequency != 100 || st.Fairlight.MasterCompressor.Ratio != 397 {
		t.Errorf("fairlight %+v", st.Fairlight)
	}
	if st.Macros.Properties[2].Name != "Half Time" || st.Macros.LastRun != -1 {
		t.Errorf("macros %+v", st.Macros)
	}
	if st.HyperDecks[0].NetworkAddress != "10.0.0.50" || st.Time.Timecode.TotalSeconds() != 37230 {
		t.Errorf("hyperdeck %+v time %+v", st.HyperDecks[0], st.Time.Timecode)
	}
	if _, ok := st.Raw["InCm"]; !ok || len(st.Raw) != 1 {
		t.Errorf("raw %v", st.Raw)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !hasCommand(sw, "TiRq") {
		if time.Now().After(deadline) {
			t.Fatal("no TiRq after the dump")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hasCommand(sw *fakeatem.Switcher, code string) bool {
	for _, c := range sw.Commands() {
		if string(c[4:8]) == code {
			return true
		}
	}
	return false
}

// A dump packet lost on the way to the session is resent because the
// session's ACK stalls at the gap; the packets after it wait, parked, and
// the dump still arrives whole and in order.
func TestLostDumpPacketIsRecovered(t *testing.T) {
	sw := newSwitcher(t)
	sw.DropSequences(2) // the dump's second packet, which ends with InCm
	s := dial(t, sw)
	if st := s.State(); len(st.Inputs) != 8 || st.Fairlight.Solo == nil {
		t.Fatalf("%d inputs", len(st.Inputs))
	}
}

// Losing the dump's first packet, which here carries the video mode, is in
// atem/fake_test.go (TestLosingTheFirstDumpPacketFailsDialFast).

// A command lost on the way to the switcher is recovered when the next
// one reveals the gap: the switcher asks for a retransmission and the
// session resends both, in order.
func TestLostCommandIsRecovered(t *testing.T) {
	sw := newSwitcher(t)
	s := dial(t, sw)
	time.Sleep(50 * time.Millisecond) // let the TiRq through first
	sw.DropFromClient(1)
	if err := s.Send(atem.AuxSource{Aux: 1, Source: 3010}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond) // a separate packet
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.SendAndWait(ctx, atem.PreviewInput{Source: 3}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.State().Aux[1].Source != 3010 {
		if time.Now().After(deadline) {
			t.Fatal("the lost command never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var order []string
	for _, c := range sw.Commands() {
		order = append(order, string(c[4:8]))
	}
	if len(order) < 3 || order[len(order)-2] != "CAuS" || order[len(order)-1] != "CPvI" {
		t.Fatalf("switcher saw %v", order)
	}
}

func TestDelayedACKs(t *testing.T) {
	sw := newSwitcher(t)
	sw.DelayACKs(150 * time.Millisecond)
	s := dial(t, sw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i, src := range []uint16{4, 5, 6} {
		if _, err := s.SendAndWait(ctx, atem.ProgramInput{Source: src}); err != nil {
			t.Fatalf("command %d: %v", i, err)
		}
	}
	if s.State().MEs[0].Program.Source != 6 {
		t.Fatal("state not updated")
	}
}

// After 5 s of silence the session drops its state and handshakes again;
// once the switcher answers, the new dump makes it ready again.
func TestSilenceAndReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 10 s")
	}
	sw := newSwitcher(t)
	s := dial(t, sw)
	changes, cancel := s.Subscribe()
	defer cancel()
	sw.Silence(true)
	deadline := time.After(8 * time.Second)
	for disconnected := false; !disconnected; {
		select {
		case c := <-changes:
			disconnected = c.Kind == atem.ChangeDisconnected
		case <-deadline:
			t.Fatal("no disconnect")
		}
	}
	if !s.Connected() || s.State().MEs != nil {
		t.Fatal("expected connected with empty state during the re-handshake")
	}
	sw.Silence(false)
	select {
	case <-s.Ready():
	case <-time.After(8 * time.Second):
		t.Fatal("not ready again")
	}
	if sw.Handshakes() != 2 || s.State().MEs[0].Program == nil {
		t.Fatalf("handshakes %d", sw.Handshakes())
	}
}
