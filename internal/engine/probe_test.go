// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fakenet"
)

func probeRig(t *testing.T) (*rig, *fakenet.Net) {
	n := fakenet.New()
	n.ATEM("192.0.2.10")
	n.Deck("192.0.2.20", fakehyperdeck.New())
	n.Videohub("192.0.2.30", fakenet.VideohubPreamble)
	n.Ultimatte("192.0.2.40", fakenet.UltimattePrelude)
	return newRig(t, Config{ProbeATEM: n.ProbeATEM, ProbeDial: n.Dial}), n
}

func TestProbeFindsEachDevice(t *testing.T) {
	r, _ := probeRig(t)
	for addr, want := range map[string]ProbeResult{
		"192.0.2.10":      {Address: "192.0.2.10", Device: "atem"},
		"192.0.2.20":      {Address: "192.0.2.20", Device: "hyperdeck", Name: "Deck 1", Model: "HyperDeck Studio HD Mini"},
		"192.0.2.30":      {Address: "192.0.2.30", Device: "videohub", Name: "Test Hub", Model: "Blackmagic Smart Videohub 40 x 40"},
		"192.0.2.40":      {Address: "192.0.2.40", Device: "ultimatte", Name: "Keyer A", Model: "Ultimatte"},
		"192.0.2.20:9993": {Address: "192.0.2.20:9993", Device: "hyperdeck", Name: "Deck 1", Model: "HyperDeck Studio HD Mini"},
	} {
		resp := r.must(Request{Device: "engine", Path: "probe", Host: addr})
		var got ProbeResult
		json.Unmarshal(resp.Result.(json.RawMessage), &got)
		if got != want {
			t.Errorf("%s: %+v, want %+v", addr, got, want)
		}
	}
}

func TestProbeFindsNothing(t *testing.T) {
	r, _ := probeRig(t)
	start := time.Now()
	resp, err := r.call(context.Background(), Request{V: Version, Device: "engine", Path: "probe", Host: "192.0.2.99"})
	if err != nil || resp.OK || resp.Code != CodeUnreachable || !strings.Contains(resp.Error, "nothing answered at 192.0.2.99") {
		t.Errorf("%+v %v", resp, err)
	}
	if d := time.Since(start); d > ProbeTimeout+time.Second {
		t.Errorf("took %v", d)
	}
}

func TestProbeTellsAPreambleByItsPort(t *testing.T) {
	r, n := probeRig(t)
	// An Ultimatte's prelude on a Videohub's port, and the reverse: each
	// port's own device is the only one that matches it there.
	n.Videohub("192.0.2.50", "PROTOCOL PREAMBLE:\nVersion: 2.1\n\nEND PRELUDE:\n")
	resp := r.must(Request{Device: "engine", Path: "probe", Host: "192.0.2.50"})
	var got ProbeResult
	json.Unmarshal(resp.Result.(json.RawMessage), &got)
	if got.Device != "videohub" || got.Name != "" {
		t.Errorf("%+v", got)
	}
}
