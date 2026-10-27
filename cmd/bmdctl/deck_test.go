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
		{[]string{"play"}, "play  stopped → play"},
		{[]string{"pause"}, "pause  play → stopped"},
		{[]string{"pause"}, "pause  stopped"},
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
		"slot select: slot id: 2", "record: name: Take", "stop", "play", "play: speed: 0"}
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
	// As the test deck answered (docs/HARDWARE.md, the nineteenth run;
	// bmdwire#53): next and prev past the timeline's ends are refused,
	// not held there.
	b.must("goto", "3")
	if code, _, errOut := b.run("next"); code != exitRejected || !strings.Contains(errOut, "the deck refused goto: out of range (109)") {
		t.Errorf("next on the last clip: %d %q", code, errOut)
	}
	b.must("goto", "1")
	if code, _, errOut := b.run("prev"); code != exitRejected || !strings.Contains(errOut, "the deck refused goto: out of range (109)") {
		t.Errorf("prev on the first clip: %d %q", code, errOut)
	}
	// stop clears loop and single clip.
	b.must("play", "--loop", "--single-clip")
	b.must("stop")
	if out := b.must("state", "--path", "transport.loop"); out != "false\n" {
		t.Errorf("loop after stop: %q", out)
	}
	// With no input, record is refused and the deck is in preview all the
	// same; stop leaves it there, and a goto brings it out.
	d.NoInput(true)
	if code, _, errOut := b.run("record", "take"); code != exitRejected || !strings.Contains(errOut, "the deck refused record: no input (110)") {
		t.Errorf("record with no input: %d %q", code, errOut)
	}
	for _, step := range []struct {
		args []string
		want string
	}{{nil, "preview"}, {[]string{"stop"}, "preview"}, {[]string{"goto", "1"}, "stopped"}} {
		if step.args != nil {
			b.must(step.args...)
		}
		if out := b.must("state", "--path", "transport.status"); out != step.want+"\n" {
			t.Errorf("after %v: %q, want %s", step.args, out, step.want)
		}
	}
	d.NoInput(false)
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
	tr.run("--host", "deck 1", "timeline", "add", "Outro.mov", "--before", "1")
	tr.run("--host", "deck 1", "timeline", "remove", "intro")
	yes := "y\n"
	tr.in = &yes
	tr.run("--host", "deck 1", "timeline", "clear")
	tr.run("--host", "deck 1", "configuration", "show")
	tr.run("--host", "deck 1", "configuration", "set", "video-input", "HDMI")
	tr.run("--host", "deck 1", "remote", "off")
	tr.run("--host", "deck 1", "remote", "on")
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
		// Outro.mov is on slot 1's disk; remove needs a clip on the
		// timeline, whatever ran before it.
		"timeline add":      {"timeline", "add", "Outro.mov"},
		"timeline remove":   {"timeline", "remove", "1"},
		"timeline clear":    {"timeline", "clear", "--yes"},
		"remote":            {"remote", "on"},
		"configuration set": {"configuration", "set", "record prefix", "Take"},
	}
	setup := map[string][][]string{
		"timeline add":    {{"slot", "1"}},
		"timeline remove": {{"slot", "1"}, {"timeline", "add", "Outro.mov"}},
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
		for _, s := range setup[e.Path] {
			b.must(s...)
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

// As the test deck showed them (docs/HARDWARE.md, the nineteenth run): a
// watch line names slow play's speed, as the command's result line does
// (the deck reports it as forward, speed 50), and a terminal's status
// leaves an empty slot's "0s left" out, where a pipe keeps it.
func TestDeckSpeedAndEmptySlotAsADeckSentThem(t *testing.T) {
	w := &deckWatch{blocks: map[string]map[string]string{"transport": {"status": "stopped", "speed": "0"}},
		names: map[string]string{}}
	ev := watchEvent{Kind: "state", Key: "transport", Value: json.RawMessage(`{"status":"forward","speed":"50"}`)}
	if got := w.line(ev).text(); got != "transport  stopped → forward 50%" {
		t.Errorf("watch: %q", got)
	}
	var o, e strings.Builder
	p := &printer{c: newCLI(&o, &e)}
	zero, left := 0, 2640
	r := engine.DeckStatusResult{Slots: []engine.DeckSlot{
		{Slot: 1, Status: "mounted", Volume: "UNTITLED", RemainingS: &left, Active: true},
		{Slot: 2, Status: "empty", RemainingS: &zero}}}
	slot2 := func(term bool) string {
		for _, row := range p.deckStatusRows(r, term) {
			if row[0] == "slot 2" {
				return row[1]
			}
		}
		return ""
	}
	if got := slot2(true); got != "empty" {
		t.Errorf("terminal: %q", got)
	}
	if got := slot2(false); got != "empty, 0s left" {
		t.Errorf("pipe: %q", got)
	}
}

// The timeline: timeline add (at the end, or before a clip), timeline
// remove (by number or name) and timeline clear (destructive), each saying the
// timeline before and after; the deck sends no notification for them.
func TestDeckTimeline(t *testing.T) {
	b, d := deckBench(t)
	if code, _, errOut := b.run("timeline", "clear"); code != exitDestructive || !strings.Contains(errOut, "destructive: add --yes") {
		t.Errorf("clear without --yes: %d %q", code, errOut)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"timeline", "add", "Outro.mov"}, "timeline add  3 clips → 4 clips"},
		{[]string{"timeline", "add", "Interview.mov", "--before", "1"}, "timeline add  4 clips → 5 clips"},
		{[]string{"timeline", "remove", "1"}, "timeline remove  5 clips → 4 clips"},
		{[]string{"timeline", "remove", "intro"}, "timeline remove  4 clips → 3 clips"},
		{[]string{"timeline", "clear", "--yes"}, "timeline clear  3 clips → empty"},
		{[]string{"timeline", "clear", "--yes"}, "timeline clear  empty"},
	} {
		out := strings.Join(strings.Fields(b.must(c.args...)), " ")
		if want := strings.Join(strings.Fields("Deck 1 (192.0.2.20)  "+c.want), " "); !strings.HasPrefix(out, want) {
			t.Errorf("%v: %q, want %q", c.args, out, want)
		}
	}
	var got []string
	for _, l := range d.Commands() {
		if strings.HasPrefix(l, "clips add") || strings.HasPrefix(l, "clips remove") || l == "clips clear" {
			got = append(got, l)
		}
	}
	want := []string{"clips add: name: Outro.mov", "clips add: clip id: 1 name: Interview.mov",
		"clips remove: clip id: 1", "clips remove: clip id: 1", "clips clear"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sent %q, want %q", got, want)
	}
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"timeline", "add", "Nowhere.mov"}, exitRejected, "the deck refused clips add: clip not found (112)"},
		{[]string{"timeline", "add", "Outro.mov", "--before", "9"}, exitUsage, "--before 9: the timeline has 0 clips"},
		{[]string{"timeline", "remove", "nothing"}, exitUsage, `clip "nothing": no timeline clip is named so (bmdctl clips lists them)`},
	} {
		if code, _, errOut := b.run(c.args...); code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d %q", c.args, code, errOut, c.code, c.msg)
		}
	}
}

// The setup group: remote off makes the deck refuse writes and on gives
// them back; configuration show lists the settings, configuration set
// changes one by its name (- or _ for its spaces), already when it holds
// the value, sent when the deck reboots for it.
func TestDeckSetup(t *testing.T) {
	b, d := deckBench(t)
	line := func(args ...string) string {
		t.Helper()
		return strings.Join(strings.Fields(b.must(args...)), " ")
	}
	if got := line("remote", "off"); !strings.HasPrefix(got, "Deck 1 (192.0.2.20) remote on → off confirmed") {
		t.Errorf("remote off: %q", got)
	}
	if code, _, errOut := b.run("play"); code != exitRejected || !strings.Contains(errOut, "remote control disabled (111)") {
		t.Errorf("play with remote off: %d %q", code, errOut)
	}
	if got := line("remote", "on"); !strings.HasPrefix(got, "Deck 1 (192.0.2.20) remote off → on confirmed") {
		t.Errorf("remote on: %q", got)
	}
	if got := line("configuration", "show"); !strings.Contains(got, "video input SDI") || !strings.Contains(got, "file format QuickTimeProResHQ") {
		t.Errorf("configuration show: %q", got)
	}
	d.RebootOn("timecode input")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"configuration", "set", "video_input", "HDMI"}, "configuration set video input SDI → HDMI confirmed"},
		{[]string{"configuration", "set", "Video Input", "HDMI"}, "configuration set video input HDMI confirmed"},
		{[]string{"configuration", "set", "record-prefix", "Take"}, "configuration set record prefix none → Take confirmed"},
		{[]string{"configuration", "set", "timecode input", "external"}, "configuration set timecode input value internal → external, deck rebooting sent"},
	} {
		if got := line(c.args...); !strings.HasPrefix(got, "Deck 1 (192.0.2.20) "+c.want) {
			t.Errorf("%v: %q, want %q", c.args, got, c.want)
		}
	}
	if code, _, errOut := b.run("configuration", "set", "nope", "x"); code != exitUsage ||
		!strings.Contains(errOut, `setting "nope": the deck has none so named; it has audio input, file format, record prefix, timecode input, video input`) {
		t.Errorf("unknown setting: %d %q", code, errOut)
	}
	var sent []string
	for _, l := range d.Commands() {
		if strings.HasPrefix(l, "configuration:") || strings.HasPrefix(l, "remote:") {
			sent = append(sent, l)
		}
	}
	want := []string{"remote: enable: false", "remote: enable: true", "configuration: video input: HDMI",
		"configuration: record prefix: Take", "configuration: timecode input: external"}
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Errorf("sent %q, want %q", sent, want)
	}
}
