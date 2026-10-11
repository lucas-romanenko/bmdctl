// SPDX-License-Identifier: MIT

package hyperdeck

// The Session against the fake deck (internal/fakehyperdeck): what
// bmdctl adds to hyperdeckwire (the state, notifications, timeouts that
// end the session) and the commands it sends from BMD's document.

import (
	"context"
	"errors"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
)

func dialFake(t *testing.T, d *fakehyperdeck.Deck) *Session {
	t.Helper()
	s, err := (&Dialer{DialContext: d.Dial}).Dial(context.Background(), fakehyperdeck.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDialReadsTheDeck(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	select {
	case <-s.Ready():
	default:
		t.Error("not ready after Dial")
	}
	st := s.State()
	if st.Name() != "Deck 1" || st.Model() != "HyperDeck Studio HD Mini" || st.Status() != "stopped" {
		t.Errorf("name %q, model %q, status %q", st.Name(), st.Model(), st.Status())
	}
	if id, ok := st.ClipID(); !ok || id != 1 {
		t.Errorf("clip %d %v", id, ok)
	}
	if len(st.Timeline) != 3 || st.Timeline[0].Name != "Intro Loop animation.mp4" || *st.Timeline[1].Start != "00:01:03:17" {
		t.Errorf("timeline %+v", st.Timeline)
	}
	if n, ok := st.RecordingTime(2); !ok || n != 3600 || len(st.Slots) != 2 {
		t.Errorf("slot 2 %d %v, slots %v", n, ok, st.Slots)
	}
	if st.Remote["enabled"] != "true" || st.Configuration["video input"] != "SDI" {
		t.Errorf("remote %v, configuration %v", st.Remote, st.Configuration)
	}
	want := []string{"notify: transport: true", "notify: slot: true", "notify: remote: true",
		"notify: configuration: true", "device info", "transport info", "remote", "configuration", "clips get",
		"slot info: slot id: 1", "slot info: slot id: 2"}
	if got := d.Commands(); !slices.Equal(got, want) {
		t.Errorf("sent %q", got)
	}
}

func next(t *testing.T, ch <-chan Change) Change {
	t.Helper()
	select {
	case c, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no change")
	}
	return Change{}
}

func TestNotificationsReachStateAndSubscribers(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	ch, stop := s.Subscribe()
	defer stop()
	d.Press("play")
	c := next(t, ch)
	if c.Kind != ChangeState || c.Code != 508 || c.Key != "transport" ||
		!reflect.DeepEqual(c.Params, map[string]string{"status": "play", "speed": "100"}) || c.Value["slot id"] != "1" {
		t.Errorf("%+v", c)
	}
	if st := s.State(); st.Status() != "play" || st.Timecode() != "00:00:00:00" {
		t.Errorf("status %q, timecode %q", st.Status(), st.Timecode())
	}
	d.SetRemote(false)
	if c := next(t, ch); c.Key != "remote" || c.Value["enabled"] != "false" || c.Value["override"] != "false" {
		t.Errorf("%+v", c)
	}
}

func TestTransportCommands(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	ch, stop := s.Subscribe()
	defer stop()
	speed, yes, no := 200, true, false
	for _, c := range []Command{Play{Speed: &speed, Loop: &yes}, Play{Loop: &no}, GotoClipBy{1}, GotoClipBy{-1},
		GotoClip{3}, GotoTimecode{"00:00:10:00"}, SlotSelect{2}, Record{"Take"}, Stop{}, Ping{}} {
		if _, err := s.SendAndWait(ctx, c); err != nil {
			t.Fatalf("%s: %v", c.Line(), err)
		}
	}
	want := []string{"play: loop: true speed: 200", "play: loop: false", "goto: clip id: +1", "goto: clip id: -1",
		"goto: clip id: 3", "goto: timecode: 00:00:10:00", "slot select: slot id: 2", "record: name: Take", "stop", "ping"}
	if got := d.Commands(); !slices.Equal(got[len(got)-len(want):], want) {
		t.Errorf("sent %q", got)
	}
	if clips := d.Slot(2); len(clips) != 2 || clips[1].Name != "Take.mov" {
		t.Errorf("slot 2 %+v", clips)
	}
	// The state follows from the notifications alone.
	deadline := time.Now().Add(2 * time.Second)
	for s.State().Status() != "stopped" && time.Now().Before(deadline) {
		<-ch
	}
	if st := s.State(); st.Status() != "stopped" || st.Transport["slot id"] != "2" || st.Loop() {
		t.Errorf("transport %v", st.Transport)
	}
}

func TestARefusalIsAnError(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	d.SetRemote(false)
	var de *Error
	if err := s.Stop(ctx); !errors.As(err, &de) || de.Code != 111 {
		t.Errorf("%v", err)
	}
	if !s.Connected() {
		t.Error("a refusal ended the session")
	}
	if _, err := s.SendAndWait(ctx, Raw("frobnicate")); !errors.As(err, &de) || de.Code != 100 {
		t.Errorf("%v", err)
	}
}

func TestATimeoutEndsTheSession(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	d.Silence(true)
	c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := s.SendAndWait(c, Ping{}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("%v", err)
	}
	for deadline := time.Now().Add(2 * time.Second); s.Connected(); {
		if time.Now().After(deadline) {
			t.Fatal("still connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Ping(ctx); !errors.Is(err, ErrClosed) || !errors.Is(err, ErrTimeout) {
		t.Errorf("%v", err)
	}
}

func TestALateAnswerIsDropped(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	d.Silence(true)
	c, cancel := context.WithCancel(ctx)
	done := make(chan error)
	go func() { _, err := s.Request(c, Ping{}); done <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	d.Send("102 invalid value\r\n") // the abandoned ping's answer, late
	d.Silence(false)
	if err := s.Ping(ctx); err != nil {
		t.Errorf("the next command got the late answer: %v", err)
	}
}

func TestAnAnswerToNothingIsDropped(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	ch, stop := s.Subscribe()
	defer stop()
	// The notification after it says the session has read the stray
	// answer: then a command can't be what it answers.
	d.Send("102 invalid value\r\n508 transport info:\r\nstatus: play\r\n\r\n")
	next(t, ch)
	if err := s.Ping(ctx); err != nil {
		t.Errorf("%v", err)
	}
}

func TestADroppedConnectionEndsTheSession(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	ch, _ := s.Subscribe()
	d.Drop()
	if c := next(t, ch); c.Kind != ChangeDisconnected {
		t.Errorf("%+v", c)
	}
	if _, ok := <-ch; ok {
		t.Error("channel open")
	}
	if s.Connected() {
		t.Error("connected")
	}
	if err := s.Ping(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("%v", err)
	}
	select {
	case <-s.Ready():
		t.Error("ready after the end")
	default:
	}
}

func TestARejectedConnectionFailsDial(t *testing.T) {
	d := fakehyperdeck.New()
	d.Reject(1)
	_, err := (&Dialer{DialContext: d.Dial}).Dial(ctx, fakehyperdeck.Address)
	var de *Error
	if !errors.As(err, &de) || de.Code != 120 {
		t.Errorf("%v", err)
	}
}

func TestALineBreakIsRefused(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	n := len(d.Commands())
	for _, c := range []Command{ClipsAdd{Name: "a.mov\nplay"}, Record{Name: "x\r"}, Raw("ping\nstop"), Raw(" ")} {
		if err := s.Send(c); err == nil {
			t.Errorf("%q sent", c.Line())
		}
	}
	if got := d.Commands(); len(got) != n {
		t.Errorf("sent %q", got[n:])
	}
}

func TestCommandsRefuseNothing(t *testing.T) {
	for _, c := range []Validator{SetConfiguration{}, GotoClipBy{0}, GotoTimecode{"1:00"}, GotoTimecode{"00:00:00:0x"},
		GotoTimecode{"00:00:60:00"}, GotoTimecode{"24:00:00:00"}} {
		if c.Validate() == nil {
			t.Errorf("%+v valid", c)
		}
	}
	for _, tc := range []string{"00:00:10:00", "23:59:59;29"} {
		if !IsTimecode(tc) {
			t.Errorf("%s not a timecode", tc)
		}
	}
}

func TestCloseSendsQuit(t *testing.T) {
	d := fakehyperdeck.New()
	s := dialFake(t, d)
	s.Close()
	// The deck has read the line once Close returns, and notes it just
	// after.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if got := d.Commands(); got[len(got)-1] == "quit" {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("sent %q", got)
		}
	}
	if s.Connected() {
		t.Error("connected")
	}
}

func TestOverLoopback(t *testing.T) {
	d := fakehyperdeck.New()
	l, err := d.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, err := Dial(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if info, err := s.DeviceInfo(ctx); err != nil || info["name"] != "Deck 1" {
		t.Errorf("%v %v", info, err)
	}
	if !strings.HasPrefix(s.Addr(), "127.0.0.1:") {
		t.Errorf("addr %s", s.Addr())
	}
	_, port, _ := net.SplitHostPort(withPort("192.0.2.11"))
	if port != "9993" {
		t.Errorf("default port %s", port)
	}
}
