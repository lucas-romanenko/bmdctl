// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
)

func event(opcode, key, at string, value any) watchEvent {
	b, _ := json.Marshal(value)
	return watchEvent{Kind: "state", Opcode: opcode, Key: key, Value: b, Time: at}
}

// The common changes get a line of their own; the rest are the pretty
// key and the JSON.
func TestWatchLines(t *testing.T) {
	var st atem.State
	json.Unmarshal([]byte(`{"mes":[{"program":{"index":0,"source":3},"preview":{"index":0,"source":5},
		"transition":{"settings":{"index":0,"style":0}}}],
		"inputs":{"3":{"index":3,"name":"Camera 3","short_name":"CAM3"},"5":{"index":5,"name":"Camera 5","short_name":"CAM5"},
		"7":{"index":7,"name":"Graphics","short_name":""}},
		"tally":{"source":{"num":2,"tally":{"3":{"program":true,"preview":false},"5":{"program":false,"preview":true}}}}}`), &st)
	v := newWatchView(&st)
	for _, c := range []struct {
		ev   watchEvent
		want string
	}{
		{event("PrgI", "PrgI:0", "12:00:00.000", map[string]int{"index": 0, "source": 5}), "program  ME1  CAM3 → CAM5"},
		{event("PrvI", "PrvI:0", "12:00:00.000", map[string]int{"index": 0, "source": 3}), "preview  ME1  CAM5 → CAM3"},
		{event("PrvI", "PrvI:0", "12:00:00.000", map[string]int{"index": 0, "source": 3}), "preview  ME1  CAM3"},
		{event("PrgI", "PrgI:1", "12:00:00.000", map[string]int{"index": 1, "source": 7}), "program  ME2  Graphics"},
		{event("PrgI", "PrgI:0", "12:00:00.000", map[string]int{"index": 0, "source": 9}), "program  ME1  CAM5 → 9"},
		{event("TlSr", "TlSr", "12:00:00.000", atem.TallySourceField{Num: 2, Tally: map[uint16]atem.TallyLamps{
			3: {}, 5: {Program: true}}}), "tally  CAM5 pgm, CAM3 off"},
		{event("TlSr", "TlSr", "12:00:00.000", atem.TallySourceField{Num: 2, Tally: map[uint16]atem.TallyLamps{
			3: {Preview: true}, 5: {Program: true, Preview: true}}}), "tally  CAM5 pgm+pvw, CAM3 pvw"},
		{event("TlSr", "TlSr", "12:00:00.000", atem.TallySourceField{Num: 2, Tally: map[uint16]atem.TallyLamps{
			3: {Preview: true}, 5: {Program: true, Preview: true}}}), "tally  unchanged"},
		{event("TrPs", "TrPs:0", "12:00:01.000", atem.TransitionPositionField{InTransition: true, FramesRemaining: 24, Position: 400}),
			"transition  ME1  mix  running 0.0s"},
		{event("TrPs", "TrPs:0", "12:00:01.400", atem.TransitionPositionField{InTransition: true, FramesRemaining: 14, Position: 4000}),
			"transition  ME1  mix  running 0.4s"},
		{event("TrPs", "TrPs:0", "12:00:02.000", atem.TransitionPositionField{}), "transition  ME1  mix  done 1.0s"},
		{event("TrPs", "TrPs:0", "12:00:02.100", atem.TransitionPositionField{}), "transition  ME1  mix  idle"},
		{event("TrSS", "TrSS:0", "12:00:03.000", map[string]int{"index": 0, "style": 2}), `transition-settings:0  {"index":0,"style":2}`},
		{event("TrPs", "TrPs:0", "12:00:03.000", atem.TransitionPositionField{InTransition: true}), "transition  ME1  wipe  running 0.0s"},
		{event("KeOn", "KeOn:0:1", "12:00:04.000", map[string]any{"index": 0, "keyer": 1, "enabled": true}),
			`key-on-air:0:1  {"enabled":true,"index":0,"keyer":1}`},
		{event("InPr", "InPr:9", "12:00:05.000", map[string]any{"index": 9, "name": "Lectern", "short_name": "LECT"}),
			`input-properties:9  {"index":9,"name":"Lectern","short_name":"LECT"}`},
		{event("PrgI", "PrgI:0", "12:00:05.000", map[string]int{"index": 0, "source": 3}), "program  ME1  LECT → CAM3"},
		{event("Abcd", "Abcd", "12:00:06.000", "q80="), `Abcd  "q80="`},
		{watchEvent{Kind: "disconnected"}, "disconnected"},
	} {
		if got := v.line(c.ev); got != c.want {
			t.Errorf("%s: %q, want %q", c.ev.Key, got, c.want)
		}
	}
	// With nothing known, a change shows alone.
	v = newWatchView(nil)
	if got := v.line(event("PrgI", "PrgI:0", "", map[string]int{"index": 0, "source": 4})); got != "program  ME1  4" {
		t.Errorf("no snapshot: %q", got)
	}
	if got := v.line(event("TlSr", "TlSr", "", atem.TallySourceField{Num: 2, Tally: map[uint16]atem.TallyLamps{
		1: {}, 4: {Program: true}}})); got != "tally  4 pgm" {
		t.Errorf("first tally: %q", got)
	}
}

// Through the engine, against the fake switcher: the clock is left out
// unless --all, program changes show the source they leave, injected
// tally and transitions render, and --json still prints the frames.
func TestWatchHuman(t *testing.T) {
	b := newBench(t)
	b.must(with("info")...) // the session, and its fake switcher
	sw := b.switchers()[0]
	watch := func(args ...string) (*lineWriter, chan int) {
		c, _, _ := b.cli("")
		w, ew := newLineWriter(), newLineWriter()
		c.stdout, c.stderr = w, ew
		done := make(chan int)
		go func() { done <- c.run(with(append([]string{"watch"}, args...)...)) }()
		if slices.Contains(args, "--json") {
			return w, done // the first frame says it watches
		}
		if l := ew.next(t); l != "watching; Ctrl-C to stop" {
			t.Fatalf("stderr %q", l)
		}
		return w, done
	}
	// The dump's program is CAM2: the line shows it leaving.
	w, done := watch("--count", "4")
	b.must(with("time-request")...) // a Time packet comes back: left out
	b.must(with("program", "CAM1")...)
	tally := fakeatem.Command("TlSr", []byte{0, 2, 0, 1, 1, 0, 2, 0, 0, 0})
	trps := make([]byte, 8)
	trps[1], trps[2] = 1, 20
	binary.BigEndian.PutUint16(trps[4:], 2500)
	sw.Send(tally, fakeatem.Command("TrPs", trps))
	b.must(with("usk", "2", "on-air", "on")...)
	for _, want := range []string{"program  ME1  CAM2 → CAM1", "tally  CAM1 pgm", "transition  ME1  dip  running 0.0s",
		`key-on-air:0:1  {"index":0,"keyer":1,"enabled":true}`} {
		if l := w.next(t); !strings.HasSuffix(l, want) || len(l) < 14 || l[12:14] != "  " {
			t.Errorf("line %q, want ...%q", l, want)
		}
	}
	if code := <-done; code != 0 {
		t.Errorf("exit %d", code)
	}
	// --all keeps the clock.
	w, done = watch("--count", "1", "--all")
	b.must(with("time-request")...)
	if l := w.next(t); !strings.Contains(l, `time  {"hours":10,"minutes":20,"seconds":30,"frames":5,"dropframe":false}`) {
		t.Errorf("--all: %q", l)
	}
	<-done
	// --json is the engine's frames, the opcode key and all.
	w, done = watch("--count", "1", "--json")
	if l := w.next(t); !strings.Contains(l, `"kind":"watching"`) {
		t.Errorf("--json first frame %q", l)
	}
	b.must(with("program", "CAM2")...)
	if l := w.next(t); !strings.Contains(l, `"kind":"state","opcode":"PrgI","key":"PrgI:0","value":{"index":0,"source":2}`) {
		t.Errorf("--json frame %q", l)
	}
	<-done
}
