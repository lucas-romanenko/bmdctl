// SPDX-License-Identifier: MIT

package hyperdeck

// bmdwire v1.3.0's hyperdeckwire/tests/test_hyperdeckwire_client.py, one
// test for one, on the fixtures copied from it (testdata/fixtures), then
// the ones 1.8.0 added (bmdwire#60), their bytes inline as theirs are;
// those of 1.4.0 to 1.7.1 are the oracle's (testdata/oracle). The
// Python tests' _FakeSocket hands the client its script as it reads; here
// a session reads eagerly, so the script's first chunk is the greeting
// and each next one is given when the client writes, as a deck answers.

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/fixtures/" + name)
	if err != nil || len(b) == 0 {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// scriptConn gives its first chunk at once and each next one when the
// client writes; with none left, the stream ends.
type scriptConn struct {
	*gatedConn
	ends []int
	next int
}

func (c *scriptConn) Write(p []byte) (int, error) {
	c.next++
	w := window{eof: c.next >= len(c.ends)}
	if !w.eof {
		w.upTo = c.ends[c.next]
	}
	c.gatedConn.arm(w)
	return c.gatedConn.Write(p)
}

func scripted(t *testing.T, chunks ...[]byte) (*Session, *scriptConn) {
	t.Helper()
	var stream []byte
	var ends []int
	for _, c := range chunks {
		stream = append(stream, c...)
		ends = append(ends, len(stream))
	}
	c := &scriptConn{gatedConn: newGated(stream, window{upTo: ends[0]}), ends: ends}
	d := Dialer{Bare: true, DialContext: func(context.Context, string, string) (net.Conn, error) { return c, nil }}
	s, err := d.Dial(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, c
}

func clientWithScript(t *testing.T, extra ...string) (*Session, *scriptConn) {
	t.Helper()
	chunks := [][]byte{fixture(t, "greeting.bin")}
	for _, e := range extra {
		chunks = append(chunks, fixture(t, e))
	}
	return scripted(t, chunks...)
}

var ctx = context.Background()

func TestConnectParsesGreeting(t *testing.T) {
	s, _ := clientWithScript(t)
	if s.Model() != "HyperDeck Studio HD Mini" || s.ProtocolVersion() != "1.13" {
		t.Errorf("model %q, protocol %q", s.Model(), s.ProtocolVersion())
	}
}

func TestSimpleAckResponseIsOKNoLines(t *testing.T) {
	s, c := clientWithScript(t, "ok.bin")
	r, err := s.Request(ctx, Stop{})
	if err != nil || r.Code != 200 || r.Text != "ok" || len(r.Lines) != 0 || !r.OK() {
		t.Errorf("%+v, %v", r, err)
	}
	if got := c.sent(); string(got) != "stop\n" {
		t.Errorf("sent %q", got)
	}
}

func TestMultiLineResponseTerminatesOnBlankLine(t *testing.T) {
	s, _ := clientWithScript(t, "device-info.bin")
	info, err := s.DeviceInfo(ctx)
	want := map[string]string{"model": "HyperDeck Studio HD Mini", "protocol version": "1.13",
		"unique id": "7c2e0d18dc78", "slot count": "3", "software version": "8.1.1"}
	if err != nil || !reflect.DeepEqual(info, want) {
		t.Errorf("%q, %v", info, err)
	}
}

func TestErrorResponseRaisesHyperdeckErrorWithCode(t *testing.T) {
	s, _ := clientWithScript(t, "remote-disabled.bin")
	var de *Error
	if err := s.Stop(ctx); !errors.As(err, &de) || de.Code != 111 || de.Text != "remote control disabled" {
		t.Errorf("%v", err)
	}
}

func TestAsyncMessageSkippedSoRealResponseReturns(t *testing.T) {
	s, _ := clientWithScript(t, "async-transport-stopped-then-ok.bin")
	if r, err := s.Request(ctx, Stop{}); err != nil || r.Code != 200 || r.Text != "ok" {
		t.Errorf("%+v, %v", r, err)
	}
}

// test_async_message_returned_when_skip_async_false reads the async
// message itself; here notifications go to the state and subscribers.
func TestAsyncMessageReachesTheState(t *testing.T) {
	s, _ := clientWithScript(t, "async-transport-play.bin")
	ch, stop := s.Subscribe()
	defer stop()
	s.Send(Ping{})
	select {
	case c := <-ch:
		if c.Code != 508 || c.Key != "transport" || c.Params["status"] != "play" {
			t.Errorf("%+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change")
	}
	if st := s.State(); st.Status() != "play" {
		t.Errorf("status %q", st.Status())
	}
}

func TestDiskLineParserHandlesFilenameWithSpaces(t *testing.T) {
	c, err := parseDiskLine("1: Intro Loop animation.mp4 H.264 1080p25 00:01:03:17")
	f, v := "H.264", "1080p25"
	want := Clip{ID: 1, Name: "Intro Loop animation.mp4", Duration: "00:01:03:17", FileFormat: &f, VideoFormat: &v}
	if err != nil || !reflect.DeepEqual(c, want) {
		t.Errorf("%+v, %v", c, err)
	}
}

func TestClipsV1LineParserHandlesFilenameWithSpaces(t *testing.T) {
	c, err := parseClipsLine("2: Intro Loop animation2.mp4 00:01:03:17 00:01:03:17")
	start := "00:01:03:17"
	want := Clip{ID: 2, Name: "Intro Loop animation2.mp4", Duration: "00:01:03:17", Start: &start}
	if err != nil || !reflect.DeepEqual(c, want) {
		t.Errorf("%+v, %v", c, err)
	}
}

func TestDiskLineParserRejectsMalformedLine(t *testing.T) {
	if _, err := parseDiskLine("not-a-clip-line"); err == nil {
		t.Error("no error")
	}
}

func TestDiskListFiltersHeaderLinesAndReturnsClips(t *testing.T) {
	s, _ := clientWithScript(t, "disk-list.bin")
	clips, err := s.DiskList(ctx, nil)
	if err != nil || len(clips) != 2 || clips[0].ID != 1 || clips[0].Name != "Intro Loop animation.mp4" ||
		clips[1].ID != 2 || clips[1].Name != "Other Clip.mov" {
		t.Errorf("%+v, %v", clips, err)
	}
}

func sends(t *testing.T, want string, f func(*Session) error, answers ...string) {
	t.Helper()
	s, c := clientWithScript(t, answers...)
	if err := f(s); err != nil {
		t.Fatal(err)
	}
	if got := c.sent(); string(got) != want {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestPlayWithLoopAndSingleClipBuildsCorrectCommand(t *testing.T) {
	sends(t, "play: loop: true single clip: true\n", func(s *Session) error {
		on := true
		return s.Play(ctx, PlayOptions{Loop: &on, SingleClip: &on})
	}, "ok.bin")
}

func TestPlayWithNoArgsSendsPlainPlay(t *testing.T) {
	sends(t, "play\n", func(s *Session) error { return s.Play(ctx, PlayOptions{}) }, "ok.bin")
}

func TestPauseSendsPlaySpeedZero(t *testing.T) {
	sends(t, "play: speed: 0\n", func(s *Session) error { return s.Pause(ctx) }, "ok.bin")
}

func TestClipsAddPreservesSpacesInName(t *testing.T) {
	sends(t, "clips add: name: Intro Loop animation2.mp4\n", func(s *Session) error {
		return s.ClipsAdd(ctx, "Intro Loop animation2.mp4", nil)
	}, "ok.bin")
}

func TestClipsAddWithBeforeClipIDInserts(t *testing.T) {
	three := 3
	sends(t, "clips add: clip id: 3 name: newer.mp4\n", func(s *Session) error {
		return s.ClipsAdd(ctx, "newer.mp4", &three)
	}, "ok.bin")
}

func TestGotoClipSendsClipIDParam(t *testing.T) {
	sends(t, "goto: clip id: 5\n", func(s *Session) error { return s.GotoClip(ctx, 5) }, "ok.bin")
}

func TestRemoteEnableTrueSetsRemoteToTrue(t *testing.T) {
	sends(t, "remote: enable: true\n", func(s *Session) error { return s.RemoteEnable(ctx, true) }, "ok.bin")
}

func TestSetConfigurationSingleParamBuildsCommand(t *testing.T) {
	sends(t, "configuration: file format: H.264High\n", func(s *Session) error {
		return s.SetConfiguration(ctx, Setting{"file_format", "H.264High"})
	}, "ok.bin")
}

func TestSetConfigurationStacksMultipleParamsInOneCommand(t *testing.T) {
	sends(t, "configuration: file format: QuickTimeProResHQ default standard: 2160p25 video input: SDI\n",
		func(s *Session) error {
			return s.SetConfiguration(ctx, Setting{"file_format", "QuickTimeProResHQ"},
				Setting{"default_standard", "2160p25"}, Setting{"video_input", "SDI"})
		}, "ok.bin")
}

func TestSetConfigurationTranslatesBoolToProtocolStrings(t *testing.T) {
	sends(t, "configuration: record cache: true append timestamp: false\n", func(s *Session) error {
		return s.SetConfiguration(ctx, Setting{"record_cache", true}, Setting{"append_timestamp", false})
	}, "ok.bin")
}

func TestSetConfigurationNoArgsIsNoop(t *testing.T) {
	sends(t, "", func(s *Session) error { return s.SetConfiguration(ctx) })
}

func TestSetConfigurationRaisesOnProtocolError(t *testing.T) {
	s, _ := clientWithScript(t, "invalid-value.bin")
	var de *Error
	if err := s.SetConfiguration(ctx, Setting{"file_format", "NotARealFormat"}); !errors.As(err, &de) || de.Code != 102 {
		t.Errorf("%v", err)
	}
}

func TestSetConfigurationAccepts213DeckRebootingAsSuccess(t *testing.T) {
	s, _ := clientWithScript(t, "deck-rebooting.bin")
	if err := s.SetConfiguration(ctx, Setting{"file_format", "QuickTimeProResHQ"}); err != nil {
		t.Error(err)
	}
}

func TestClipsCountReturnsInt(t *testing.T) {
	s, _ := clientWithScript(t, "clips-count.bin")
	if n, err := s.ClipsCount(ctx); n != 7 || err != nil {
		t.Errorf("%d, %v", n, err)
	}
}

func TestContextManagerConnectsAndCloses(t *testing.T) {
	s, c := clientWithScript(t)
	if s.Model() != "HyperDeck Studio HD Mini" {
		t.Errorf("model %q", s.Model())
	}
	s.Close()
	if got := c.sent(); !bytes.Contains(got, []byte("quit\n")) {
		t.Errorf("sent %q", got)
	}
	if s.Connected() {
		t.Error("still connected")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	s, _ := clientWithScript(t)
	s.Close()
	if err := s.Close(); err != nil {
		t.Error(err)
	}
}

func TestResponseParamsParsesKeyValuePairs(t *testing.T) {
	r := Response{Code: 204, Text: "device info", Lines: []string{"protocol version: 1.13",
		"model: HyperDeck Studio HD Mini", "  unique id: abcdef  "}}
	want := map[string]string{"protocol version": "1.13", "model": "HyperDeck Studio HD Mini", "unique id": "abcdef"}
	if got := r.Params(); !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
}

func TestResponseParamsSkipsLinesWithoutColon(t *testing.T) {
	if got := (Response{Code: 200, Text: "something", Lines: []string{"no-colon-line"}}).Params(); len(got) != 0 {
		t.Errorf("%q", got)
	}
}

// Every fixture is listed, with its size, in testdata/README.md.
// ---- bmdwire v1.8.0 (bmdwire#60) -------------------------------------------

// inline is a session whose deck answers each command with the next of
// answers, given as bytes.
func inline(t *testing.T, answers ...string) (*Session, *scriptConn) {
	t.Helper()
	chunks := [][]byte{fixture(t, "greeting.bin")}
	for _, a := range answers {
		chunks = append(chunks, []byte(a))
	}
	return scripted(t, chunks...)
}

func sendsInline(t *testing.T, want string, f func(*Session) error, answers ...string) {
	t.Helper()
	s, c := inline(t, answers...)
	if err := f(s); err != nil {
		t.Fatal(err)
	}
	if got := c.sent(); string(got) != want {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func refusedInline(t *testing.T, code int, f func(*Session) error, answer string) {
	t.Helper()
	s, _ := inline(t, answer)
	var de *Error
	if err := f(s); !errors.As(err, &de) || de.Code != code {
		t.Errorf("%v, want %d", err, code)
	}
}

const okLine = "200 ok\r\n"

// test_play_with_false_loop_and_single_clip_sends_them_false
func TestPlayWithFalseLoopAndSingleClipSendsThemFalse(t *testing.T) {
	off := false
	sendsInline(t, "play: loop: false single clip: false\n", func(s *Session) error {
		return s.Play(ctx, PlayOptions{Loop: &off, SingleClip: &off})
	}, okLine)
}

// test_play_leaves_out_what_it_is_not_given
func TestPlayLeavesOutWhatItIsNotGiven(t *testing.T) {
	on, speed := true, 50
	sendsInline(t, "play: single clip: true speed: 50\n", func(s *Session) error {
		return s.Play(ctx, PlayOptions{SingleClip: &on, Speed: &speed})
	}, okLine)
}

// test_goto_clip_by_sends_a_signed_offset
func TestGotoClipBySendsASignedOffset(t *testing.T) {
	for offset, want := range map[int]string{1: "goto: clip id: +1\n", -1: "goto: clip id: -1\n", 3: "goto: clip id: +3\n"} {
		sendsInline(t, want, func(s *Session) error { return s.GotoClipBy(ctx, offset) }, okLine)
	}
}

// test_goto_clip_by_zero_is_refused_unsent
func TestGotoClipByZeroIsRefusedUnsent(t *testing.T) {
	s, c := inline(t, okLine)
	if err := s.GotoClipBy(ctx, 0); err == nil || !strings.HasSuffix(err.Error(), "goto_clip_by: an offset of 0 clips moves nowhere") {
		t.Errorf("%v", err)
	}
	if got := c.sent(); len(got) != 0 {
		t.Errorf("sent %q", got)
	}
}

// test_goto_clip_by_past_the_end_raises_109
func TestGotoClipByPastTheEndRaises109(t *testing.T) {
	refusedInline(t, 109, func(s *Session) error { return s.GotoClipBy(ctx, 1) }, "109 out of range\r\n")
}

// test_goto_timecode_sends_it
func TestGotoTimecodeSendsIt(t *testing.T) {
	for _, tc := range []string{"00:01:30:00", "23:59:59:29", "00:00:10;02"} {
		sendsInline(t, "goto: timecode: "+tc+"\n", func(s *Session) error { return s.GotoTimecode(ctx, tc) }, okLine)
	}
}

// test_goto_timecode_refuses_anything_else_unsent, but for an int, which
// GotoTimecode can't be given.
func TestGotoTimecodeRefusesAnythingElseUnsent(t *testing.T) {
	for _, tc := range []string{"1:00:00:00", "24:00:00:00", "00:60:00:00", "00:00:60:00", "00;00:00:00",
		"00:00:00:0", "00:00:00:000", " 00:00:00:00", "00:00:00:00\n", "", "٠٠:٠٠:٠٠:٠٠"} {
		s, c := inline(t, okLine)
		if err := s.GotoTimecode(ctx, tc); err == nil || !strings.Contains(err.Error(), "not a timecode") {
			t.Errorf("%q: %v", tc, err)
		}
		if got := c.sent(); len(got) != 0 {
			t.Errorf("%q: sent %q", tc, got)
		}
	}
}

// test_record_sends_its_name_if_given (None and ” are both "")
func TestRecordSendsItsNameIfGiven(t *testing.T) {
	for name, want := range map[string]string{"": "record\n", "Interview 2": "record: name: Interview 2\n"} {
		sendsInline(t, want, func(s *Session) error { return s.Record(ctx, name) }, okLine)
	}
}

// test_record_with_no_input_raises_110
func TestRecordWithNoInputRaises110(t *testing.T) {
	refusedInline(t, 110, func(s *Session) error { return s.Record(ctx, "") }, "110 no input\r\n")
}

// test_notify_reads_which_are_on
func TestNotifyReadsWhichAreOn(t *testing.T) {
	s, c := inline(t, "209 notify:\r\nremote: false\r\ntransport: true\r\nslot: false\r\nconfiguration: false\r\n\r\n")
	got, err := s.Notify(ctx)
	want := map[string]string{"remote": "false", "transport": "true", "slot": "false", "configuration": "false"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v %v", got, err)
	}
	if sent := c.sent(); string(sent) != "notify\n" {
		t.Errorf("sent %q", sent)
	}
}

// test_set_notify_stacks_its_kinds_in_one_line
func TestSetNotifyStacksItsKindsInOneLine(t *testing.T) {
	sendsInline(t, "notify: transport: true slot: true display timecode: false\n", func(s *Session) error {
		return s.SetNotify(ctx, Setting{"transport", true}, Setting{"slot", true}, Setting{"display_timecode", false})
	}, okLine)
}

// test_set_notify_no_args_is_noop
func TestSetNotifyNoArgsIsNoop(t *testing.T) {
	sendsInline(t, "", func(s *Session) error { return s.SetNotify(ctx) })
}

// test_set_notify_raises_on_protocol_error
func TestSetNotifyRaisesOnProtocolError(t *testing.T) {
	refusedInline(t, 101, func(s *Session) error { return s.SetNotify(ctx, Setting{"timeline_position", true}) },
		"101 unsupported parameter\r\n")
}

// test_notifications_among_answers_are_kept_in_order, through Subscribe,
// where read_notifications returns them; its other tests (the backlog,
// the wait, a partial block, a reconnect) are read_notifications' own.
func TestNotificationsAmongAnswersAreKeptInOrder(t *testing.T) {
	s, _ := inline(t, "508 transport info:\r\nstatus: play\r\n\r\n"+okLine,
		"502 slot info:\r\nslot id: 1\r\nstatus: mounted\r\n\r\n"+okLine)
	ch, stop := s.Subscribe()
	defer stop()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SlotSelect(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 2 {
		select {
		case c := <-ch:
			got = append(got, c.Key)
		case <-time.After(2 * time.Second):
			t.Fatalf("notifications %q", got)
		}
	}
	if !slices.Equal(got, []string{"transport", "slot:1"}) {
		t.Errorf("notifications %q", got)
	}
}

func TestFixturesListed(t *testing.T) {
	readme, err := os.ReadFile("testdata/README.md")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("testdata/fixtures")
	if err != nil || len(entries) == 0 {
		t.Fatalf("%v", err)
	}
	for _, e := range entries {
		b := fixture(t, e.Name())
		row := "| `" + e.Name() + "` | " + strconv.Itoa(len(b)) + " |"
		if !bytes.Contains(readme, []byte(row)) {
			t.Errorf("README.md has no row %s", row)
		}
	}
}
