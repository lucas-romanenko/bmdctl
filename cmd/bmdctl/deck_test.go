// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
)

// deckBench is a bench with a HyperDeck at fakehyperdeck's address,
// connected and the default host.
func deckBench(t *testing.T) (*bench, *fakehyperdeck.Deck) {
	t.Helper()
	b := newBench(t)
	d := b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	b.must("connect", fakehyperdeck.Address)
	return b, d
}

// connect ADDRESS finds the deck, opens its session and saves it by the
// name it gives itself.
func TestConnectADeck(t *testing.T) {
	b, _ := deckBench(t)
	f := b.hostsFile(t)
	h := f.FindAddress(fakehyperdeck.Address)
	if h == nil || h.Device != "hyperdeck" || h.Name != "Deck 1" || f.Default != "Deck 1" {
		t.Errorf("host %+v, default %q", h, f.Default)
	}
	// Commands go to it: its own, and a switcher's says what it is.
	if out := b.must("status"); !strings.Contains(out, "transport  stopped, loop off, single clip off") {
		t.Errorf("status:\n%s", out)
	}
	code, _, errOut := b.run("program", "2")
	if code != exitUsage || errOut != "bmdctl: Deck 1 (192.0.2.20) is a HyperDeck; bmdctl hyperdeck --help\n" {
		t.Errorf("%d %q", code, errOut)
	}
	if out := b.must("hyperdeck", "--help"); !strings.Contains(out, "  play  ") || !strings.Contains(out, "goto <clip>") {
		t.Errorf("help:\n%s", out)
	}
	// And the other way: a deck's command to a switcher.
	b.must("connect", "192.0.2.10")
	code, _, errOut = b.run("play")
	if code != exitUsage || errOut != "bmdctl: ATEM 1 M/E Constellation HD (192.0.2.10) is an ATEM; bmdctl atem --help\n" {
		t.Errorf("%d %q", code, errOut)
	}
	if out := b.must("hyperdeck", "--host", "deck 1", "status"); !strings.Contains(out, "Deck 1 (192.0.2.20)") {
		t.Errorf("a device word and a host: %q", out)
	}
}

// Each command's line down a pipe: the deck, what was done, before →
// after, confirmed, the time.
func TestDeckWrites(t *testing.T) {
	b, d := deckBench(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"play"}, "play  stopped → play"},
		{[]string{"play", "--speed", "200"}, "play  play → play 200%"},
		{[]string{"loop", "on"}, "loop  off → on"},
		{[]string{"loop", "on"}, "loop  on"},
		{[]string{"stop"}, "stop  play 200% → stopped"},
		{[]string{"stop"}, "stop  stopped"},
		{[]string{"next"}, "next  Intro Loop animation.mp4 → Interview.mov"},
		{[]string{"prev"}, "prev  Interview.mov → Intro Loop animation.mp4"},
		{[]string{"goto", "3"}, "goto  Intro Loop animation.mp4 → Outro.mov"},
		{[]string{"goto", "inter"}, "goto  Outro.mov → Interview.mov"},
		{[]string{"goto", "00:00:05:00"}, "goto  00:01:03:17 → 00:00:05:00"},
		{[]string{"slot", "2"}, "slot  1 → 2"},
		{[]string{"record", "Take"}, "record  stopped → record"},
		{[]string{"stop"}, "stop  record → stopped"},
	} {
		out := strings.Join(strings.Fields(b.must(c.args...)), " ")
		want := strings.Join(strings.Fields("Deck 1 (192.0.2.20)  "+c.want), " ")
		if !strings.HasPrefix(out, want+" confirmed") {
			t.Errorf("%v: %q, want %q", c.args, out, c.want)
		}
	}
	if clips := d.Slot(2); len(clips) != 2 || clips[1].Name != "Take.mov" {
		t.Errorf("slot 2: %+v", clips)
	}
	want := []string{"play", "play: speed: 200", "play: loop: true single clip: false speed: 200", "stop",
		"goto: clip id: +1", "goto: clip id: -1", "goto: clip id: 3", "goto: clip id: 2", "goto: timecode: 00:00:05:00",
		"slot select: slot id: 2", "record: name: Take", "stop"}
	var got []string
	for _, l := range d.Commands() {
		switch {
		case strings.HasPrefix(l, "play"), l == "stop", strings.HasPrefix(l, "goto"), strings.HasPrefix(l, "slot select"),
			strings.HasPrefix(l, "record"):
			got = append(got, l)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s", strings.Join(got, "\n"))
	}
}

// What the deck refuses, and what bmdctl does.
func TestDeckRefusals(t *testing.T) {
	b, d := deckBench(t)
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"loop", "on"}, exitUsage, "the deck is stopped: loop changes a playback under way (bmdctl play --loop starts one looping)"},
		{[]string{"goto", "nothing like it"}, exitUsage, `clip "nothing like it": no timeline clip is named so, and it isn't a timecode`},
		{[]string{"goto", "00:00:99:00"}, exitUsage, `clip "00:00:99:00": no timeline clip is named so`},
		{[]string{"goto", "9"}, exitRejected, "the deck refused goto: clip not found (112)"},
		{[]string{"slot", "0"}, exitUsage, "out of range"},
		{[]string{"play", "--speed", "6000"}, exitUsage, "out of range"},
		{[]string{"record", "a\nplay"}, exitUsage, "line break"},
	} {
		code, _, errOut := b.run(c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d %q", c.args, code, errOut, c.code, c.msg)
		}
	}
	d.SetRemote(false)
	code, _, errOut := b.run("play")
	if code != exitRejected || !strings.Contains(errOut, "the deck refused play: remote control disabled (111)") {
		t.Errorf("%d %q", code, errOut)
	}
	// A deck that stops answering: the message names the wait it was
	// given (--timeout), not the session's default.
	d.Silence(true)
	code, _, errOut = b.run("--timeout", "200ms", "play")
	if code != exitNoEcho || !strings.Contains(errOut, `sent "transport info", but the deck did not answer within 200ms`) {
		t.Errorf("silent: %d %q", code, errOut)
	}
}

// Reads: status, clips, state, watch, down a pipe and as JSON.
func TestDeckReads(t *testing.T) {
	b, d := deckBench(t)
	out := b.must("clips")
	want := "Deck 1 (192.0.2.20)  HyperDeck Studio HD Mini  1080p25\n" +
		"1  Intro Loop animation.mp4  00:00:00:00  00:01:03:17  stopped\n" +
		"2  Interview.mov             00:01:03:17  00:00:30:00\n" +
		"3  Outro.mov                 00:01:33:17  00:00:10:00\n"
	if out != want {
		t.Errorf("clips:\n%s", out)
	}
	if out := b.must("clips", "--slot", "2"); !strings.Contains(out, "1  Backup.mov  QuickTimeProResHQ  1080p25  00:00:05:00") {
		t.Errorf("clips --slot 2:\n%s", out)
	}
	var st engine.DeckStatusResult
	var resp struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal([]byte(b.must("--json", "status")), &resp)
	json.Unmarshal(resp.Result, &st)
	if st.Name != "Deck 1" || st.Transport.Status != "stopped" || st.Clip == nil || st.Clip.Name != "Intro Loop animation.mp4" ||
		len(st.Slots) != 2 || !st.Slots[0].Active || *st.Slots[1].RemainingS != 3600 || !st.Remote {
		t.Errorf("%+v", st)
	}
	if out := b.must("state", "--path", "transport.status"); out != "stopped\n" {
		t.Errorf("state: %q", out)
	}
	// watch: a change from the deck's front panel, as it comes. The CLI
	// says it watches once it has the state to show changes against: a
	// change before then may be in that state already, with no before.
	c, o, _ := b.cli("")
	ew := newLineWriter()
	c.stderr = ew
	done := make(chan int)
	go func() { done <- c.run([]string{"watch", "--count", "2"}) }()
	if l := ew.next(t); l != "watching; Ctrl-C to stop" {
		t.Fatalf("stderr %q", l)
	}
	d.Press("play")
	d.SetRemote(false)
	<-done
	got := o.String()
	if !strings.Contains(got, "transport  stopped → play") || !strings.Contains(got, "remote  enabled true → false") {
		t.Errorf("watch:\n%s", got)
	}
}

// At a terminal: the header names the deck with its transport; writes
// are verb, before → after and the time; status and clips lay out rows.
func TestTerminalDeck(t *testing.T) {
	b := newBench(t)
	b.net.Deck(fakehyperdeck.Address, fakehyperdeck.New())
	tr := &transcript{b: b}
	tr.run("connect", fakehyperdeck.Address)
	tr.run("--host", "deck 1", "status")
	tr.run("--host", "deck 1", "clips")
	tr.run("--host", "deck 1", "play", "--speed", "50")
	tr.run("--host", "deck 1", "loop", "on")
	tr.run("--host", "deck 1", "next")
	tr.run("--host", "deck 1", "stop")
	tr.run("--host", "deck 1", "stop")
	tr.run("--host", "deck 1", "goto", "outro")
	tr.run("--host", "deck 1", "record", "Take 2")
	tr.run("--host", "deck 1", "stop")
	tr.run("--host", "deck 1", "loop", "off")
	tr.run("--host", "deck 1", "program", "2")
	tr.run("--host", "deck 1", "clips", "--disk")
	tr.run("--host", "deck 1", "disconnect")
	golden(t, "deck.golden", tr.out.String())
}

// Every HyperDeck command runs against the fake deck, as every ATEM
// command does against the fake switcher (TestEveryCommandRuns).
func TestEveryDeckCommandRuns(t *testing.T) {
	b, d := deckBench(t)
	special := map[string][]string{
		"loop":  {"loop", "on"}, // once play has started
		"watch": {"watch", "--count", "1"},
		"goto":  {"goto", "2"},
		"slot":  {"slot", "2"}, // once record has stopped
	}
	asked := func() int { return strings.Count(strings.Join(d.Commands(), "\n"), "transport info") }
	c, _, _ := b.cli("")
	entries := c.reg.Entries("hyperdeck")
	b.must("play")
	ran := 0
	for _, e := range entries {
		if e.Path == "disconnect" {
			continue // last
		}
		args := special[e.Path]
		if args == nil {
			args = strings.Fields(e.Path)
		}
		if e.Path == "watch" {
			n := asked()
			go func() {
				for deadline := time.Now().Add(5 * time.Second); asked() < n+2 && time.Now().Before(deadline); {
					time.Sleep(10 * time.Millisecond)
				}
				d.Press("play") // stopped since record: a change to see
			}()
		}
		if code, out, errOut := b.run(args...); code != 0 {
			t.Errorf("%v: exit %d\n%s%s", args, code, out, errOut)
		}
		ran++
		if e.Path == "record" {
			b.must("stop") // a deck selects no slot while it records
		}
	}
	b.must("disconnect")
	if ran != len(entries)-1 || ran < 13 {
		t.Errorf("ran %d of %d", ran, len(entries))
	}
}
