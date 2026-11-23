// SPDX-License-Identifier: MIT

package ultimatte_test

// Session (ultimattewire's Ultimatte, the 9998 text protocol client)
// against the fake unit built from the protocol document, against
// scripted connections for what a 12 4K was seen to send differently,
// and against the fake 12 4K built from captures of Software Control:
// bmdwire v1.14.0's test_client.py and the session tests of
// test_media.py, one for one. Timings are scaled down as there.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/fakeultimatte"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

const idle = time.Second

var ctx = context.Background()

func keyer(t *testing.T, model string) *fakeultimatte.Keyer {
	t.Helper()
	k := fakeultimatte.NewKeyer(model, "2.0")
	t.Cleanup(k.Close)
	return k
}

func captured(t *testing.T) *fakeultimatte.Keyer {
	t.Helper()
	k := fakeultimatte.NewCaptured4K()
	t.Cleanup(k.Close)
	return k
}

func open(t *testing.T, k *fakeultimatte.Keyer, idleTimeout, function time.Duration) *ultimatte.Session {
	t.Helper()
	c := &ultimatte.Client{DialContext: k.Dial, IdleTimeout: idleTimeout, FunctionIdleTimeout: function}
	s, err := c.Open(ctx, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func session(t *testing.T, k *fakeultimatte.Keyer) *ultimatte.Session {
	t.Helper()
	return open(t, k, idle, idle)
}

func wantErr(t *testing.T, err, kind error, sub string) {
	t.Helper()
	if !errors.Is(err, kind) || !strings.Contains(err.Error(), sub) {
		t.Errorf("%v, want %v with %q", err, kind, sub)
	}
}

func bp(v bool) *bool { return &v }
func ip(v int) *int   { return &v }

// ---- the status dump --------------------------------------------------------

// test_connect_reads_every_block_of_the_dump
func TestConnectReadsEveryBlockOfTheDump(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	s := u.State
	if !s.PreludeComplete || s.ProtocolVersion != "2.0" || *s.Protocol() != [2]int{2, 0} {
		t.Fatal(s.ProtocolVersion)
	}
	if s.Identity != (ultimatte.Identity{Model: "Ultimatte 12 8K", Label: "Ultimatte 12 8K", UniqueID: "12345678"}) || s.Family() != ultimatte.Model8K {
		t.Error(s.Identity)
	}
	if s.Network["Interface count"] != "1" {
		t.Error(s.Network)
	}
	i := s.Interfaces[0]
	if i.Name != "Cadence GigE Ethernet MAC" || i.Address != "10.0.0.2" || i.Netmask != "255.255.255.0" || i.Gateway != "10.0.0.1" ||
		i.Dynamic == nil || *i.Dynamic { // the document's "DynamicIP"
		t.Errorf("%+v", i)
	}
	if s.Version != (ultimatte.VersionInfo{ProductID: "BE84", HardwareVersion: "0100", SoftwareVersion: "09A89B7A", SoftwareRelease: "2.0"}) {
		t.Error(s.Version)
	}
	d := s.Device
	if d.VideoFormat != "2160p60" || d.ReferenceSource != "Foreground" || len(d.Inputs) != 9 {
		t.Error(d)
	}
	if *d.Locked("FG") != true || *d.Locked("REF In") != true || *d.Locked("LAYER MATTE") != true || d.Locked("nope") != nil {
		t.Error("locked")
	}
	if len(s.VideoFormats) != 38 || s.VideoFormats[0] != "auto detect" {
		t.Error(s.VideoFormats)
	}
	if len(s.Controls) != 289 || len(s.ControlDefaults) != 289 {
		t.Error(len(s.Controls), len(s.ControlDefaults))
	}
	c := s.Controls
	if c["Matte Density"] != 5000 || c["Matte Enable"] != false || c["Backing Color"] != "green" ||
		c["Tally Active"] != false || *s.Tally() || c["Factory Defaults"] != false {
		t.Error("controls")
	}
	if s.CurrentFile.Name != "Ultimatte Defaults" || s.CurrentFile.Status != "" || s.CurrentFile.Modified() != nil {
		t.Error(s.CurrentFile)
	}
	if !slices.Equal(s.FileList, []string{"Studio A", "Studio B"}) || len(s.GPILists) != 5 ||
		!slices.Equal(s.GPILists[1].Events, []string{"Studio A", "Studio B"}) || *s.GPILists[1].Index != 0 {
		t.Error(s.FileList, s.GPILists)
	}
	if !slices.Equal(s.ImageList, []string{"Blue Sky", "Newsroom"}) {
		t.Error(s.ImageList)
	}
	fb := s.FrameBuffer
	if *fb.Count != 46 || len(fb.Buffers) != 6 || *fb.Buffers["BG 1"].Enable || *fb.Buffers["BG 1"].Index != 0 {
		t.Error(fb)
	}
	if !reflect.DeepEqual(fb.Mix, map[string]any{"BG": 0, "LY": 0}) || !reflect.DeepEqual(fb.Duration, map[string]any{"BG": 0, "LY": 0}) ||
		!reflect.DeepEqual(fb.Images, map[int]string{1: "Blue Sky", 2: "Newsroom"}) {
		t.Error(fb)
	}
	if n, ok := fb.ImageNumber("newsroom"); !ok || n != 2 {
		t.Error(n)
	}
	if s.CameraID != nil || len(s.Other) != 0 {
		t.Error(s.CameraID, s.Other)
	}
	u.Close()
	if u.Connected() {
		t.Error("connected")
	}
}

// test_each_model_sends_its_subset
func TestEachModelSendsItsSubset(t *testing.T) {
	for _, c := range []struct {
		model                  string
		inputs, controls, gpis int
		camera                 *int
	}{{"8K", 9, 289, 5, nil}, {"4K", 9, 289, 5, nil}, {"HD", 6, 284, 5, nil}, {"HD Mini", 2, 260, 0, ip(1)}} {
		s := session(t, keyer(t, c.model)).State
		if len(s.Device.Inputs) != c.inputs || len(s.Controls) != c.controls || len(s.GPILists) != c.gpis ||
			(s.CameraID == nil) != (c.camera == nil) || c.camera != nil && *s.CameraID != *c.camera {
			t.Errorf("%s: %d %d %d %v", c.model, len(s.Device.Inputs), len(s.Controls), len(s.GPILists), s.CameraID)
		}
	}
}

// test_protocol_1_2_has_no_output_range
func TestProtocol12HasNoOutputRange(t *testing.T) {
	k := fakeultimatte.NewKeyer("8K", "1.2")
	t.Cleanup(k.Close)
	u := session(t, k)
	if _, ok := u.State.Controls["Output Range"]; ok {
		t.Error("Output Range")
	}
	_, err := u.SetControl(ctx, "Output Range", "full", ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrUnsupported, "Output Range")
}

// test_state_to_dict_is_json
func TestStateToDictIsJSON(t *testing.T) {
	u := session(t, keyer(t, "HD Mini"))
	b, err := json.Marshal(u.State)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	fb := d["frame_buffer"].(map[string]any)
	iface := d["interfaces"].(map[string]any)["0"].(map[string]any)
	if d["family"] != ultimatte.ModelHDMini || !reflect.DeepEqual(fb["images"], map[string]any{"1": "Blue Sky", "2": "Newsroom"}) ||
		iface["address"] != "10.0.0.2" || iface["raw"] != nil || d["tally"] != false || d["camera_id"] != 1.0 {
		t.Errorf("%s", b)
	}
}

// ---- what a 12 4K sends, scripted ---------------------------------------------

// scripted is test_client.py's ScriptedSocket: each read gets the next
// chunk whole, delay after each; a script run dry is a read timing out,
// an empty chunk the unit closing.
type scripted struct {
	mu     sync.Mutex
	chunks [][]byte
	delay  time.Duration
	sent   bytes.Buffer
}

func (s *scripted) Read(p []byte) (int, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.chunks) == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	c := s.chunks[0]
	if len(c) == 0 {
		return 0, net.ErrClosed
	}
	n := copy(p, c)
	if s.chunks[0] = c[n:]; len(s.chunks[0]) == 0 {
		s.chunks = s.chunks[1:]
	}
	return n, nil
}

func (s *scripted) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent.Write(p)
}

func (s *scripted) written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent.String()
}

func (s *scripted) Close() error                     { return nil }
func (s *scripted) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (s *scripted) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (s *scripted) SetDeadline(time.Time) error      { return nil }
func (s *scripted) SetReadDeadline(time.Time) error  { return nil }
func (s *scripted) SetWriteDeadline(time.Time) error { return nil }

func script(delay time.Duration, chunks ...string) *scripted {
	s := &scripted{delay: delay}
	for _, c := range chunks {
		s.chunks = append(s.chunks, []byte(c))
	}
	return s
}

func openScripted(sock *scripted, idleTimeout time.Duration) (*ultimatte.Session, error) {
	c := &ultimatte.Client{IdleTimeout: idleTimeout, RetryDelay: time.Nanosecond,
		DialContext: func(context.Context, string, string) (net.Conn, error) { return sock, nil }}
	return c.Open(ctx, "192.0.2.21")
}

func mustOpenScripted(t *testing.T, sock *scripted) *ultimatte.Session {
	t.Helper()
	u, err := openScripted(sock, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// Sections as a 12 4K with software 2.3 sent them (2026-10-27), with CR LF
// line ends, a block the document doesn't have, a line it doesn't have,
// and END PRELUDE with no blank line after.
const prelude124K = "PROTOCOL PREAMBLE:\r\nVersion: 2.3\r\n\r\n" +
	"IDENTITY:\r\nModel: Ultimatte 12 4K\r\nLabel: Studio Keyer\r\n" +
	"Unique ID: 00000000000000000000000000000000\r\n\r\n" +
	"VERSION:\r\nProduct ID: BE8F\r\nHardware Version: 0100\r\n" +
	"Software Version: 2AE2D1CA\r\nSoftware Release: 2.3\r\n\r\n" +
	"NETWORK INTERFACE 0:\r\nName: BMD 10G Ethernet MAC\r\nDynamic IP: true\r\n" +
	"Current Addresses: 192.0.2.98/255.255.252.0\r\n\r\n" +
	"CONTROL:\r\nMonitor Out: fg\r\nActive Unit: off\r\nVideo Format: auto detect\r\n" +
	"Matte Density: 2600\r\n\r\n" +
	"SOMETHING NEW:\r\nFoo: 1\r\n\r\n" +
	"CURRENT FILE:\r\nName: 3\r\nStatus: Modified\r\n\r\n" +
	"FILE LIST:\r\n1\r\n2\r\n\r\n" +
	"END PRELUDE:\r\n"

// test_the_12_4k_forms_are_read
func TestThe124KFormsAreRead(t *testing.T) {
	u := mustOpenScripted(t, script(0, prelude124K[:100], prelude124K[100:]))
	s := u.State
	if !s.PreludeComplete || *s.Protocol() != [2]int{2, 3} || s.Family() != ultimatte.Model4K {
		t.Fatal(s.ProtocolVersion)
	}
	if s.CurrentFile.Name != "3" || !*s.CurrentFile.Modified() || !slices.Equal(s.FileList, []string{"1", "2"}) {
		t.Error(s.CurrentFile, s.FileList)
	}
	if s.Controls["Monitor Out"] != "fg" || s.Controls["Active Unit"] != "off" || s.Controls["Matte Density"] != 2600 {
		t.Error(s.Controls)
	}
	if !*s.Interfaces[0].Dynamic || !reflect.DeepEqual(s.Other, map[string][]string{"SOMETHING NEW:": {"Foo: 1"}}) {
		t.Error(s.Other)
	}
}

// test_a_control_the_unit_has_but_the_document_lacks_can_be_set
func TestAControlTheUnitHasButTheDocumentLacksCanBeSet(t *testing.T) {
	sock := script(0, prelude124K, "ACK\n\nCONTROL:\nActive Unit: on\n\n")
	u := mustOpenScripted(t, sock)
	if v, err := u.SetControl(ctx, "active unit", true, ultimatte.Options{}); err != nil || v != "on" {
		t.Fatal(v, err)
	}
	if sock.written() != "CONTROL:\nActive Unit: on\n\n" {
		t.Errorf("%q", sock.written())
	}
}

// test_a_control_nobody_has_is_refused_unless_unvalidated
func TestAControlNobodyHasIsRefusedUnlessUnvalidated(t *testing.T) {
	sock := script(0, prelude124K, "NAK\n\n")
	u := mustOpenScripted(t, sock)
	_, err := u.SetControl(ctx, "Made Up", 1, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrControlValue, "Made Up")
	if sock.written() != "" {
		t.Error("sent")
	}
	_, err = u.SetControl(ctx, "Made Up", 1, ultimatte.Options{NoValidate: true})
	wantErr(t, err, ultimatte.ErrRefused, "NAK")
	if sock.written() != "CONTROL:\nMade Up: 1\n\n" {
		t.Errorf("%q", sock.written())
	}
}

// test_a_slow_dump_that_keeps_coming_is_not_timed_out: chunks 50 ms
// apart, several times the idle timeout in all.
func TestASlowDumpThatKeepsComingIsNotTimedOut(t *testing.T) {
	var chunks []string
	for i := 0; i < len(prelude124K); i += 40 {
		chunks = append(chunks, prelude124K[i:min(i+40, len(prelude124K))])
	}
	if len(chunks) < 12 {
		t.Fatal(len(chunks))
	}
	start := time.Now()
	u := mustOpenScripted(t, script(50*time.Millisecond, chunks...))
	if !u.State.PreludeComplete || time.Since(start) <= 200*time.Millisecond {
		t.Error(time.Since(start))
	}
}

// test_a_stalled_dump_times_out_on_silence
func TestAStalledDumpTimesOutOnSilence(t *testing.T) {
	_, err := openScripted(script(0, prelude124K[:80]), 200*time.Millisecond)
	wantErr(t, err, ultimatte.ErrTimeout, "stalled")
}

// test_silence_from_the_start_says_so
func TestSilenceFromTheStartSaysSo(t *testing.T) {
	_, err := openScripted(script(0), 200*time.Millisecond)
	wantErr(t, err, ultimatte.ErrTimeout, "not an Ultimatte")
}

// test_a_close_mid_dump_is_named
func TestACloseMidDumpIsNamed(t *testing.T) {
	_, err := openScripted(script(0, prelude124K[:80], ""), 200*time.Millisecond)
	wantErr(t, err, ultimatte.ErrConnection, "middle of its status dump")
}

// test_a_close_without_a_word_is_named
func TestACloseWithoutAWordIsNamed(t *testing.T) {
	k := keyer(t, "8K")
	k.CloseOnConnect = true
	_, err := (&ultimatte.Client{DialContext: k.Dial, IdleTimeout: idle}).Open(ctx, "127.0.0.1")
	wantErr(t, err, ultimatte.ErrConnection, "without a word")
}

// test_a_refused_connection_is_named
func TestARefusedConnectionIsNamed(t *testing.T) {
	refuse := func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	}
	_, err := (&ultimatte.Client{DialContext: refuse, RetryDelay: time.Nanosecond}).Open(ctx, "127.0.0.1")
	wantErr(t, err, ultimatte.ErrConnection, "refused")
}

// test_connect_retries_once
func TestConnectRetriesOnce(t *testing.T) {
	sock := script(0, prelude124K)
	calls := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		calls++
		if calls == 1 {
			return nil, os.ErrDeadlineExceeded
		}
		return sock, nil
	}
	u, err := (&ultimatte.Client{DialContext: dial, RetryDelay: time.Nanosecond, IdleTimeout: 200 * time.Millisecond}).Open(ctx, "192.0.2.21")
	if err != nil || !u.State.PreludeComplete || calls != 2 {
		t.Fatal(err, calls)
	}
}

// test_errors_share_one_base: every one an *ultimatte.Error.
func TestErrorsShareOneBase(t *testing.T) {
	_, err := openScripted(script(0), time.Millisecond)
	var e *ultimatte.Error
	if !errors.As(err, &e) || e.Class != "UltimatteTimeout" {
		t.Error(err)
	}
}

// test_commands_need_a_session
func TestCommandsNeedASession(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	u.Close()
	_, err := u.SetControl(ctx, "Matte Density", 1, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrConnection, "not connected")
}

// ---- controls -----------------------------------------------------------------

func set(t *testing.T, u *ultimatte.Session, name string, value any) any {
	t.Helper()
	v, err := u.SetControl(ctx, name, value, ultimatte.Options{})
	if err != nil {
		t.Fatalf("%s %v: %v", name, value, err)
	}
	return v
}

// test_set_control_sends_the_documented_block_and_returns_the_echo
func TestSetControlSendsTheDocumentedBlockAndReturnsTheEcho(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	if set(t, u, "Matte Density", 100) != 100 || u.State.Controls["Matte Density"] != 100 || k.Control("Matte Density") != "100" {
		t.Error("echo")
	}
	if !slices.Equal(k.Sent(), []string{"CONTROL:\nMatte Density: 100"}) {
		t.Error(k.Sent())
	}
}

// test_values_go_out_lower_case_and_come_back_typed
func TestValuesGoOutLowerCaseAndComeBackTyped(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	if set(t, u, "Backing Color", "Blue") != "blue" || set(t, u, "Matte Enable", true) != true ||
		set(t, u, "matte enable", "off") != false || set(t, u, "Monitor Out", "Processed GM") != "processed gm" ||
		set(t, u, "Video Format", "1080p25") != "1080p25" {
		t.Error("typed")
	}
	want := []string{"CONTROL:\nBacking Color: blue", "CONTROL:\nMatte Enable: on", "CONTROL:\nMatte Enable: off",
		"CONTROL:\nMonitor Out: processed gm", "CONTROL:\nVideo Format: 1080p25"}
	if !slices.Equal(k.Sent(), want) {
		t.Error(k.Sent())
	}
}

// test_bad_values_never_reach_the_wire
func TestBadValuesNeverReachTheWire(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	for _, c := range []struct {
		name  string
		value any
	}{{"Matte Density", 10001}, {"Transition Rate", 0}, {"Backing Color", "purple"}, {"Matte Enable", 2},
		{"Window Position Top", 2161}, {"Filter Mode", "max"}} {
		_, err := u.SetControl(ctx, c.name, c.value, ultimatte.Options{})
		wantErr(t, err, ultimatte.ErrControlValue, "")
	}
	_, err := u.SetControl(ctx, "Tally Active", true, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrControlValue, "read only")
	_, err = u.SetControl(ctx, "Made Up", "a\nb", ultimatte.Options{NoValidate: true})
	wantErr(t, err, ultimatte.ErrControlValue, "line break")
	if set(t, u, "Window Position Top", 2160) != 2160 { // 2160p60
		t.Error("2160")
	}
	if !slices.Equal(k.Sent(), []string{"CONTROL:\nWindow Position Top: 2160"}) {
		t.Error(k.Sent())
	}
}

// test_unvalidated_values_go_out_and_a_nak_is_raised
func TestUnvalidatedValuesGoOutAndANakIsRaised(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	_, err := u.SetControl(ctx, "Matte Density", 99999, ultimatte.Options{NoValidate: true})
	wantErr(t, err, ultimatte.ErrRefused, "Matte Density: 99999")
	if u.State.Controls["Matte Density"] != 5000 {
		t.Error(u.State.Controls["Matte Density"])
	}
}

// test_model_footnotes_refuse_before_sending
func TestModelFootnotesRefuseBeforeSending(t *testing.T) {
	for _, c := range []struct {
		model, name string
		value       any
	}{{"HD Mini", "GP Out Delay", 5}, {"HD Mini", "GM Freeze", true}, {"HD Mini", "Color Space", "Rec.2020"},
		{"HD Mini", "GP Out", true}, {"HD", "Talent Mirror", true}, {"HD", "Monitor To Talent", true},
		{"HD", "Talent Highlight Level", 1}} {
		k := keyer(t, c.model)
		u := session(t, k)
		_, err := u.SetControl(ctx, c.name, c.value, ultimatte.Options{})
		wantErr(t, err, ultimatte.ErrUnsupported, "")
		if len(k.Sent()) != 0 {
			t.Error(k.Sent())
		}
	}
}

// test_video_format_is_checked_against_the_units_own_list
func TestVideoFormatIsCheckedAgainstTheUnitsOwnList(t *testing.T) {
	u := session(t, keyer(t, "HD Mini"))
	for _, f := range []string{"1080PsF25", "4320p25"} {
		_, err := u.SetControl(ctx, "Video Format", f, ultimatte.Options{})
		wantErr(t, err, ultimatte.ErrControlValue, "")
	}
	if set(t, u, "Video Format", "1080p50") != "1080p50" {
		t.Error("1080p50")
	}
}

// test_set_controls_is_one_block
func TestSetControlsIsOneBlock(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	got, err := u.SetControls(ctx, []ultimatte.ControlValue{{"Red Density", 10}, {"Green Density", 20}, {"Shadow Level", 30}}, ultimatte.Options{})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"Red Density": 10, "Green Density": 20, "Shadow Level": 30}) {
		t.Fatal(got, err)
	}
	_, err = u.SetControls(ctx, []ultimatte.ControlValue{{"Red Density", 1}, {"Green Density", -1}}, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrControlValue, "")
	_, err = u.SetControls(ctx, nil, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrValue, "nothing to set")
	if !slices.Equal(k.Sent(), []string{"CONTROL:\nRed Density: 10\nGreen Density: 20\nShadow Level: 30"}) {
		t.Error(k.Sent())
	}
}

// test_offset_is_relative
func TestOffsetIsRelative(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	for _, c := range []struct{ d, want int }{{10, 5010}, {-20, 4990}} {
		if v, err := u.OffsetControl(ctx, "Matte Density", c.d, ultimatte.Options{}); err != nil || v != c.want {
			t.Error(v, err)
		}
	}
	_, err := u.OffsetControl(ctx, "Matte Enable", 1, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrControlValue, "")
	_, err = u.OffsetControl(ctx, "Made Up", 1, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrControlValue, "")
	if !slices.Equal(k.Sent(), []string{"CONTROL:\nOffset Matte Density: 10", "CONTROL:\nOffset Matte Density: -20"}) {
		t.Error(k.Sent())
	}
}

// test_set_without_waiting
func TestSetWithoutWaiting(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	if v, err := u.SetControl(ctx, "Matte Density", 7, ultimatte.Options{NoWait: true}); err != nil || v != 7 {
		t.Fatal(v, err)
	}
	if v, err := u.OffsetControl(ctx, "Matte Density", 1, ultimatte.Options{NoWait: true}); err != nil || v != nil {
		t.Fatal(v, err)
	}
	if err := u.WaitFor(ctx, func(s *ultimatte.State) bool { return s.Controls["Matte Density"] == 8 }, 0); err != nil {
		t.Fatal(err)
	}
}

// test_functions_wait_for_yes
func TestFunctionsWaitForYes(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	set(t, u, "Matte Density", 1)
	if err := u.RunFunction(ctx, "Factory Defaults", 0); err != nil {
		t.Fatal(err)
	}
	if u.State.Controls["Matte Density"] != 5000 || u.State.Controls["Factory Defaults"] != true {
		t.Error(u.State.Controls["Matte Density"])
	}
	if err := u.RunFunction(ctx, "auto screen sample", 0); err != nil {
		t.Fatal(err)
	}
	wantErr(t, u.RunFunction(ctx, "Matte Density", 0), ultimatte.ErrControlValue, "not a function")
	sent := k.Sent()
	if !slices.Equal(sent[len(sent)-2:], []string{"CONTROL:\nFactory Defaults: yes", "CONTROL:\nAuto Screen Sample: yes"}) {
		t.Error(sent)
	}
}

// test_a_function_that_never_finishes_times_out
func TestAFunctionThatNeverFinishesTimesOut(t *testing.T) {
	k := keyer(t, "8K")
	k.FunctionSilent = true
	u := session(t, k)
	start := time.Now()
	wantErr(t, u.RunFunction(ctx, "Matte Reset", 300*time.Millisecond), ultimatte.ErrTimeout, "update after the ACK")
	if time.Since(start) > 2*time.Second {
		t.Error(time.Since(start))
	}
}

// test_a_unit_that_never_answers_times_out
func TestAUnitThatNeverAnswersTimesOut(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	k.SetMute(true)
	_, err := u.SetControl(ctx, "Matte Density", 1, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrTimeout, "no ACK")
	wantErr(t, err, ultimatte.ErrTimeout, "connect again")
	if u.Connected() {
		t.Error("connected")
	}
}

// ---- pushes and subscriptions -------------------------------------------------

func waitFor(t *testing.T, u *ultimatte.Session, pred func(*ultimatte.State) bool) {
	t.Helper()
	if err := u.WaitFor(ctx, pred, 0); err != nil {
		t.Fatal(err)
	}
}

// test_panel_changes_reach_subscribers
func TestPanelChangesReachSubscribers(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	var seen, controlsOnly []ultimatte.Update
	off := u.Subscribe(func(x ultimatte.Update) { seen = append(seen, x) })
	u.Subscribe(func(x ultimatte.Update) { controlsOnly = append(controlsOnly, x) }, "CONTROL")
	k.Panel("CONTROL", [2]string{"Matte Density", "1234"}, [2]string{"Tally Active", "on"})
	k.Panel("FRAME BUFFER", [2]string{"BG 1 Frame Buffer Enable", "on"})
	var updates []ultimatte.Update
	for len(updates) < 2 {
		got, err := u.Poll(ctx, time.Second)
		if err != nil || len(got) == 0 {
			t.Fatal(err)
		}
		updates = append(updates, got...)
	}
	if updates[0].Header != "CONTROL:" || updates[1].Header != "FRAME BUFFER:" ||
		!reflect.DeepEqual(updates[0].Changes, map[string]any{"Matte Density": 1234, "Tally Active": true}) {
		t.Error(updates)
	}
	if !*u.State.Tally() || !*u.State.FrameBuffer.Buffers["BG 1"].Enable || len(seen) != 2 || len(controlsOnly) != 1 {
		t.Error(len(seen), len(controlsOnly))
	}
	off()
	k.Panel("CONTROL", [2]string{"Matte Density", "1"})
	waitFor(t, u, func(s *ultimatte.State) bool { return s.Controls["Matte Density"] == 1 })
	if len(seen) != 2 || len(controlsOnly) != 2 {
		t.Error(len(seen), len(controlsOnly))
	}
	if got, err := u.Poll(ctx, 0); err != nil || len(got) != 0 {
		t.Error(got, err)
	}
}

// test_another_clients_change_is_pushed
func TestAnotherClientsChangeIsPushed(t *testing.T) {
	k := keyer(t, "8K")
	a, b := session(t, k), session(t, k)
	set(t, a, "Black Gloss", 77)
	waitFor(t, b, func(s *ultimatte.State) bool { return s.Controls["Black Gloss"] == 77 })
}

// test_pushes_during_a_command_are_applied
func TestPushesDuringACommandAreApplied(t *testing.T) {
	k := keyer(t, "8K")
	a, b := session(t, k), session(t, k)
	var got []any
	a.Subscribe(func(x ultimatte.Update) { got = append(got, x.Changes) })
	set(t, b, "Cool", 5)
	time.Sleep(200 * time.Millisecond) // b's push is now waiting for a
	set(t, a, "Skin Tone", 6)
	if a.State.Controls["Cool"] != 5 || !reflect.DeepEqual(got, []any{map[string]any{"Cool": 5}, map[string]any{"Skin Tone": 6}}) {
		t.Error(got)
	}
}

// test_a_failing_subscriber_is_logged_not_raised
func TestAFailingSubscriberIsLoggedNotRaised(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	var log bytes.Buffer
	u.Log = slog.New(slog.NewTextHandler(&log, nil))
	u.Subscribe(func(ultimatte.Update) { panic("subscriber bug") })
	if set(t, u, "Cool", 1) != 1 || !strings.Contains(log.String(), "subscriber failed") {
		t.Error(log.String())
	}
}

// test_wait_for_times_out_on_silence
func TestWaitForTimesOutOnSilence(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	wantErr(t, u.WaitFor(ctx, func(*ultimatte.State) bool { return false }, 200*time.Millisecond), ultimatte.ErrTimeout, "")
}

// test_a_lost_session_is_named
func TestALostSessionIsNamed(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	k.Close()
	_, err := u.Poll(ctx, time.Second)
	wantErr(t, err, ultimatte.ErrConnection, "")
	if u.Connected() {
		t.Error("connected")
	}
}

// test_unsolicited_message_is_kept
func TestUnsolicitedMessageIsKept(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	k.Broadcast("MESSAGE:\nWarning: Something\n\n")
	waitFor(t, u, func(s *ultimatte.State) bool { return len(s.Messages) > 0 })
	if !reflect.DeepEqual(u.State.Messages, []map[string]string{{"Warning": "Something"}}) {
		t.Error(u.State.Messages)
	}
}

// ---- status dump requests -------------------------------------------------------

// test_refresh_every_block
func TestRefreshEveryBlock(t *testing.T) {
	spelled := map[string]string{"FRAME BUFFER": "Frame Buffer", "IMAGE LIST": "Image List",
		"FILE LIST": "File List", "CURRENT FILE": "Current File"}
	for _, c := range []struct {
		header string
		n      int
	}{{"PROTOCOL PREAMBLE", 1}, {"IDENTITY", 3}, {"NETWORK", 4}, {"NETWORK INTERFACE 0", 8}, {"VERSION", 4},
		{"DEVICE", 11}, {"VIDEO FORMATS", 38}, {"CONTROL", 289}, {"CONTROL DEFAULT", 289}, {"CURRENT FILE", 1},
		{"FILE LIST", 2}, {"GPI LIST", 4}, {"IMAGE LIST", 2}, {"FRAME BUFFER", 19}} {
		k := keyer(t, "8K")
		u := session(t, k)
		lines, err := u.Refresh(ctx, strings.ToLower(c.header))
		if err != nil {
			t.Fatal(c.header, err)
		}
		n := 0
		for _, ln := range lines[1:] {
			if ln != "" {
				n++
			}
		}
		if lines[0] != c.header+":" || n != c.n {
			t.Errorf("%s: %q, %d lines", c.header, lines[0], n)
		}
		want := c.header
		if s, ok := spelled[c.header]; ok {
			want = s
		}
		if !slices.Equal(k.Received(), []string{want + ":"}) {
			t.Errorf("%s: %q", c.header, k.Received())
		}
	}
}

// test_refresh_replaces_state
func TestRefreshReplacesState(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	k.AddFile("Quiet") // changed without a push
	if _, err := u.Refresh(ctx, "FILE LIST"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(u.State.FileList, []string{"Studio A", "Studio B", "Quiet"}) {
		t.Error(u.State.FileList)
	}
}

// test_refresh_of_a_block_the_model_lacks_is_a_nak
func TestRefreshOfABlockTheModelLacksIsANak(t *testing.T) {
	u := session(t, keyer(t, "HD Mini"))
	_, err := u.Refresh(ctx, "GPI LIST")
	wantErr(t, err, ultimatte.ErrRefused, "NAK")
	if lines, err := u.Refresh(ctx, "CAMERACONTROL"); err != nil || lines[1] != "Camera Id: 1" {
		t.Error(lines, err)
	}
}

// test_an_unknown_block_is_a_nak
func TestAnUnknownBlockIsANak(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	_, err := u.SendBlock(ctx, "NOT A BLOCK", []string{"X: 1"}, "")
	wantErr(t, err, ultimatte.ErrRefused, "NAK")
}

// test_send_block_raw
func TestSendBlockRaw(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	if lines, err := u.SendBlock(ctx, "control", []string{"Matte Density: 9"}, "CONTROL"); err != nil ||
		!slices.Equal(lines, []string{"CONTROL:", "Matte Density: 9"}) {
		t.Error(lines, err)
	}
	if lines, err := u.SendBlock(ctx, "CONTROL", []string{"Cool: 3"}, ""); err != nil || lines != nil {
		t.Error(lines, err)
	}
	_, err := u.SendBlock(ctx, "CONTROL", []string{"a\nb"}, "")
	wantErr(t, err, ultimatte.ErrControlValue, "line break")
}

// test_network_interface_push_merges_into_state
func TestNetworkInterfacePushMergesIntoState(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	k.Broadcast("NETWORK INTERFACE 0:\nStatic Gateway: 10.0.0.254\n\n")
	waitFor(t, u, func(s *ultimatte.State) bool { return s.Interfaces[0].StaticGateway == "10.0.0.254" })
	if i := u.State.Interfaces[0]; i.Address != "10.0.0.2" || i.Name != "Cadence GigE Ethernet MAC" {
		t.Errorf("%+v", i)
	}
}

// ---- files --------------------------------------------------------------------

// test_files
func TestFiles(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	if f, err := u.LoadFile(ctx, "Studio B"); err != nil || f.Name != "Studio B" {
		t.Fatal(f, err)
	}
	for _, c := range []struct {
		do   func() ([]string, error)
		want []string
	}{
		{func() ([]string, error) { return u.SaveFile(ctx, "Evening") }, []string{"Studio A", "Studio B", "Evening"}},
		{func() ([]string, error) { return u.RenameFile(ctx, "Evening", "Late") }, []string{"Studio A", "Studio B", "Late"}},
		{func() ([]string, error) { return u.DeleteFile(ctx, "Studio A") }, []string{"Studio B", "Late"}},
	} {
		if got, err := c.do(); err != nil || !slices.Equal(got, c.want) {
			t.Error(got, err)
		}
	}
	want := []string{"FILE:\nLoad: Studio B", "FILE:\nSave: Evening", "FILE:\nRename: Evening\nTo: Late", "FILE:\nDelete: Studio A"}
	if !slices.Equal(k.Sent(), want) {
		t.Error(k.Sent())
	}
}

// test_a_file_message_raises
func TestAFileMessageRaises(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	_, err := u.LoadFile(ctx, "Nope")
	wantErr(t, err, ultimatte.ErrMessage, "File not found")
	var e *ultimatte.Error
	if !errors.As(err, &e) || !reflect.DeepEqual(e.Message, map[string]string{"Error": "File not found"}) {
		t.Error(err)
	}
	if !reflect.DeepEqual(u.State.Messages[len(u.State.Messages)-1], map[string]string{"Error": "File not found"}) ||
		u.State.CurrentFile.Name != "Ultimatte Defaults" {
		t.Error(u.State.Messages)
	}
	_, err = u.SaveFile(ctx, "two\nlines")
	wantErr(t, err, ultimatte.ErrControlValue, "line break")
}

// test_load_by_another_client_is_pushed
func TestLoadByAnotherClientIsPushed(t *testing.T) {
	k := keyer(t, "8K")
	a, b := session(t, k), session(t, k)
	if _, err := a.LoadFile(ctx, "Studio A"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, b, func(s *ultimatte.State) bool { return s.CurrentFile.Name == "Studio A" })
}

// ---- GPI ----------------------------------------------------------------------

// test_gpi_events
func TestGPIEvents(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	check := func(g ultimatte.GPIList, err error, events []string) {
		t.Helper()
		if err != nil || !slices.Equal(g.Events, events) {
			t.Error(g, err)
		}
	}
	g, err := u.GPIInsert(ctx, 2, "Studio A", -1)
	check(g, err, []string{"Studio A"})
	g, err = u.GPIInsert(ctx, 2, "Studio B", 0)
	check(g, err, []string{"Studio B", "Studio A"})
	if g, err := u.GPISetIndex(ctx, 2, 1); err != nil || *g.Index != 1 {
		t.Error(g, err)
	}
	g, err = u.GPIRemove(ctx, 2, 1)
	check(g, err, []string{"Studio A"})
	g, err = u.GPIRemove(ctx, 2, 0)
	check(g, err, []string{})
	want := []string{"GPI:\nID: 2\nInsert: Studio A\nAt: -1", "GPI:\nID: 2\nInsert: Studio B\nAt: 0",
		"GPI:\nID: 2\nIndex: 1", "GPI:\nID: 2\nRemove: 1", "GPI:\nID: 2\nRemove: 0"}
	if !slices.Equal(k.Sent(), want) {
		t.Error(k.Sent())
	}
}

// test_gpi_event_limit_message
func TestGPIEventLimitMessage(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	for range fakeultimatte.GPIEventLimit - 2 {
		if _, err := u.GPIInsert(ctx, 1, "Studio A", -1); err != nil {
			t.Fatal(err)
		}
	}
	_, err := u.GPIInsert(ctx, 1, "Studio A", -1)
	wantErr(t, err, ultimatte.ErrMessage, "Event limit exceeded")
}

// test_gpi_refusals
func TestGPIRefusals(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	for _, call := range []func() (ultimatte.GPIList, error){
		func() (ultimatte.GPIList, error) { return u.GPIInsert(ctx, 0, "x", -1) },
		func() (ultimatte.GPIList, error) { return u.GPIInsert(ctx, 1, "x", -2) },
		func() (ultimatte.GPIList, error) { return u.GPIRemove(ctx, 1, -1) },
		func() (ultimatte.GPIList, error) { return u.GPISetIndex(ctx, 1, -1) },
	} {
		_, err := call()
		wantErr(t, err, ultimatte.ErrControlValue, "")
	}
	_, err := u.GPIInsert(ctx, 9, "Studio A", -1)
	wantErr(t, err, ultimatte.ErrRefused, "NAK")
	_, err = session(t, keyer(t, "HD Mini")).GPIInsert(ctx, 1, "Studio A", -1)
	wantErr(t, err, ultimatte.ErrUnsupported, "no GPI")
}

// ---- frame buffers -------------------------------------------------------------

// test_assign_image_to_a_frame_buffer
func TestAssignImageToAFrameBuffer(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	buf, err := u.AssignImage(ctx, "bg 1", "Newsroom", true)
	if err != nil || buf.Name != "BG 1" || *buf.Index != 2 || !*buf.Enable {
		t.Fatal(buf, err)
	}
	if buf, err := u.SetFrameBuffer(ctx, "BG 1", nil, bp(false)); err != nil || *buf.Enable {
		t.Error(buf, err)
	}
	if v, err := u.SetFrameBufferMix(ctx, "bg", 5000); err != nil || v != 5000 {
		t.Error(v, err)
	}
	if v, err := u.SetTransitionDuration(ctx, "LY", 1500); err != nil || v != 1500 {
		t.Error(v, err)
	}
	if !reflect.DeepEqual(u.State.FrameBuffer.Duration, map[string]any{"BG": 0, "LY": 1500}) {
		t.Error(u.State.FrameBuffer.Duration)
	}
	want := []string{"FRAME BUFFER:\nBG 1 Frame Buffer Index: 2\nBG 1 Frame Buffer Enable: on",
		"FRAME BUFFER:\nBG 1 Frame Buffer Enable: off", "FRAME BUFFER:\nBG Frame Buffer Mix: 5000",
		"FRAME BUFFER:\nLY Transition Duration: 1500"}
	if !slices.Equal(k.Sent(), want) {
		t.Error(k.Sent())
	}
}

// test_frame_buffer_refusals
func TestFrameBufferRefusals(t *testing.T) {
	k := keyer(t, "8K")
	u := session(t, k)
	for _, call := range []func() error{
		func() error { _, err := u.SetFrameBuffer(ctx, "BG 3", nil, bp(true)); return err },
		func() error { _, err := u.SetFrameBuffer(ctx, "BG 1", ip(47), nil); return err },
		func() error { _, err := u.SetFrameBuffer(ctx, "BG 1", ip(-1), nil); return err },
		func() error { _, err := u.SetFrameBufferMix(ctx, "GM", 1); return err },
		func() error { _, err := u.SetTransitionDuration(ctx, "BG", -1); return err },
		func() error { _, err := u.AssignImage(ctx, "BG 1", "Not There", true); return err },
	} {
		wantErr(t, call(), ultimatte.ErrControlValue, "")
	}
	_, err := u.SetFrameBuffer(ctx, "BG 1", nil, nil)
	wantErr(t, err, ultimatte.ErrValue, "nothing to set")
	if len(k.Sent()) != 0 {
		t.Error(k.Sent())
	}
}

// ---- camera control ------------------------------------------------------------

// test_camera_id_on_the_hd_mini
func TestCameraIDOnTheHDMini(t *testing.T) {
	k := keyer(t, "HD Mini")
	u := session(t, k)
	if id, err := u.SetCameraID(ctx, 4); err != nil || *id != 4 {
		t.Fatal(id, err)
	}
	_, err := u.SetCameraID(ctx, -1)
	wantErr(t, err, ultimatte.ErrControlValue, "")
	if !slices.Equal(k.Sent(), []string{"CAMERACONTROL:\nCamera Id: 4"}) {
		t.Error(k.Sent())
	}
}

// test_camera_control_elsewhere_is_refused
func TestCameraControlElsewhereIsRefused(t *testing.T) {
	k := keyer(t, "8K")
	_, err := session(t, k).SetCameraID(ctx, 4)
	wantErr(t, err, ultimatte.ErrUnsupported, "HD Mini")
	if len(k.Sent()) != 0 {
		t.Error(k.Sent())
	}
}

// ---- what a 12 4K did on 2026-11-12 (Software Control captures) ---------------
//
// Replayed byte for byte from the capture, then the same behaviour against
// the fake 12 4K with its timing scaled down.

const prelude8142 = "PROTOCOL PREAMBLE:\nVersion: 2.3\n\n" +
	"IDENTITY:\nModel: Ultimatte 12 4K\nLabel: Ultimatte Editor\n" +
	"Unique ID: DF92D2DAAA8244C4ADA6D318E183B58D\n\n" +
	"DEVICE:\nVideo Format: 1080p25\nReference Source: foreground\n" +
	"FG In: Locked\nBG In: Not Locked\n\n" +
	"CONTROL:\nMatte Density: 2500\nCursor X: 192\nCursor Y: 108\n" +
	"Cursor 2 X: 1728\nCursor 2 Y: 972\nWall Cursor Position Enable: off\n" +
	"Floor Cursor Position Enable: off\nMonitor Out: program\n\n" +
	"CURRENT FILE:\nName: Ultimatte Defaults\nStatus: Clean\n\n" +
	"END PRELUDE:\n\n"

// test_capture_same_value_write_is_echoed: Matte Density: 2500 while at 2500
func TestCaptureSameValueWriteIsEchoed(t *testing.T) {
	sock := script(0, prelude8142, "ACK\n\n", "CONTROL:\nMatte Density: 2500\n\n",
		"CURRENT FILE:\nName: Ultimatte Defaults\nStatus: Modified\n\n")
	u := mustOpenScripted(t, sock)
	if set(t, u, "Matte Density", 2500) != 2500 || !strings.HasSuffix(sock.written(), "CONTROL:\nMatte Density: 2500\n\n") {
		t.Error(sock.written())
	}
	if _, err := u.Poll(ctx, 0); err != nil || !*u.State.CurrentFile.Modified() {
		t.Error(err, u.State.CurrentFile)
	}
}

// test_capture_cursor_enable_echo_after_colour_pushes: the echo came
// 550 ms after the ACK, after a colour push
func TestCaptureCursorEnableEchoAfterColourPushes(t *testing.T) {
	u := mustOpenScripted(t, script(0, prelude8142, "ACK\n\n", "CONTROL:\nWall Color Blue: 2956\n\n",
		"CONTROL:\nWall Color Green: 3744\n\n", "CONTROL:\nWall Color Red: 5490\n\n",
		"CONTROL:\nWall Cursor Position Enable: on\n\n", "CONTROL:\nWall Color Blue: 2956\n\n"))
	if set(t, u, "Wall Cursor Position Enable", true) != true || u.State.Controls["Wall Color Red"] != 5490 {
		t.Error(u.State.Controls)
	}
}

// test_capture_cursor_move_with_the_cursor_off_raises
func TestCaptureCursorMoveWithTheCursorOffRaises(t *testing.T) {
	u := mustOpenScripted(t, script(0, prelude8142, "ACK\n\n", "MESSAGE:\nControl Locked - Enable 'Cursor' function\n\n"))
	_, err := u.OffsetControl(ctx, "Cursor X", -15, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrMessage, "Control Locked")
	if !reflect.DeepEqual(u.State.Messages[len(u.State.Messages)-1], map[string]string{"": "Control Locked - Enable 'Cursor' function"}) {
		t.Error(u.State.Messages)
	}
}

// test_capture_sample_wall_completes_on_control_default: ACK, 4 s of
// nothing, cursor off, colours, CONTROL DEFAULT; no yes
func TestCaptureSampleWallCompletesOnControlDefault(t *testing.T) {
	sock := script(0, prelude8142, "ACK\n\n", "CONTROL:\nWall Cursor Position Enable: off\n\n",
		"CONTROL:\nFloor Color Blue: 5958\n\n", "CONTROL:\nFloor Color Green: 6757\n\n",
		"CONTROL:\nFloor Color Red: 8333\n\n", "CONTROL:\nDirect Light Blue: 10000\n\n",
		"CONTROL:\nDirect Light Green: 3253\n\n", "CONTROL:\nDirect Light Red: 10000\n\n",
		"CONTROL DEFAULT:\nMatte Density: 2500\nDirect Light Green: 3253\n\n")
	u := mustOpenScripted(t, sock)
	if err := u.RunFunction(ctx, "Sample Wall", 0); err != nil {
		t.Fatal(err)
	}
	c := u.State.Controls
	if !strings.HasSuffix(sock.written(), "CONTROL:\nSample Wall: yes\n\n") || c["Wall Cursor Position Enable"] != false ||
		c["Floor Color Red"] != 8333 || c["Floor Color Green"] != 6757 || c["Floor Color Blue"] != 5958 || c["Direct Light Green"] != 3253 {
		t.Error(c)
	}
}

// test_capture_monitor_out_full_lower_case_names
func TestCaptureMonitorOutFullLowerCaseNames(t *testing.T) {
	sock := script(0, prelude8142, "ACK\n\n", "CONTROL:\nMonitor Out: combined matte\n\n")
	u := mustOpenScripted(t, sock)
	if set(t, u, "Monitor Out", "Combined Matte") != "combined matte" || !strings.HasSuffix(sock.written(), "CONTROL:\nMonitor Out: combined matte\n\n") {
		t.Error(sock.written())
	}
	_, err := u.SetControl(ctx, "Monitor Out", "matte", ultimatte.Options{}) // no such choice
	wantErr(t, err, ultimatte.ErrControlValue, "")
}

func session4K(t *testing.T, k *fakeultimatte.Keyer, idleTimeout, function time.Duration) *ultimatte.Session {
	t.Helper()
	return open(t, k, idleTimeout, function)
}

// test_4k_sample_wall_waits_out_the_silence
func Test4KSampleWallWaitsOutTheSilence(t *testing.T) {
	k := captured(t)
	k.SampleSilence = 500 * time.Millisecond // 4 s on the unit
	u := session4K(t, k, 300*time.Millisecond, 1500*time.Millisecond)
	set(t, u, "Wall Cursor Position Enable", true)
	start := time.Now()
	if err := u.RunFunction(ctx, "Sample Wall", 0); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 450*time.Millisecond || u.State.Controls["Wall Cursor Position Enable"] != false ||
		u.State.Controls["Floor Color Red"] != 8333 || u.State.ControlDefaults["Direct Light Green"] != 3253 {
		t.Error(time.Since(start), u.State.Controls["Wall Cursor Position Enable"])
	}
}

// test_4k_sample_floor_without_yes
func Test4KSampleFloorWithoutYes(t *testing.T) {
	k := captured(t)
	k.SampleSilence = 200 * time.Millisecond
	u := session4K(t, k, 300*time.Millisecond, time.Second)
	set(t, u, "Floor Cursor Position Enable", true)
	var seen []map[string]any
	u.Subscribe(func(x ultimatte.Update) { seen = append(seen, x.Changes.(map[string]any)) }, "CONTROL")
	if err := u.RunFunction(ctx, "Sample Floor", 0); err != nil {
		t.Fatal(err)
	}
	if u.State.Controls["Floor Cursor Position Enable"] != false {
		t.Error("cursor on")
	}
	for _, c := range seen {
		if _, ok := c["Sample Floor"]; ok {
			t.Error("yes") // never "yes"
		}
	}
}

// test_4k_sample_silence_is_the_function_timeout_s_to_cover
func Test4KSampleSilenceIsTheFunctionTimeoutsToCover(t *testing.T) {
	k := captured(t)
	k.SampleSilence = 800 * time.Millisecond
	u := session4K(t, k, 300*time.Millisecond, 300*time.Millisecond)
	wantErr(t, u.RunFunction(ctx, "Sample Wall", 0), ultimatte.ErrTimeout, "")
}

// test_default_function_timeout_covers_the_captured_silence: 4.0 s seen
// twice
func TestDefaultFunctionTimeoutCoversTheCapturedSilence(t *testing.T) {
	if ultimatte.DefaultFunctionIdleTimeout < 8*time.Second {
		t.Error(ultimatte.DefaultFunctionIdleTimeout)
	}
}

// test_4k_cursor_moves_need_their_cursor
func Test4KCursorMovesNeedTheirCursor(t *testing.T) {
	u := session4K(t, captured(t), 300*time.Millisecond, idle)
	_, err := u.OffsetControl(ctx, "Cursor X", 15, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrMessage, "Enable 'Cursor'")
	if !u.Connected() { // a refusal, not a broken session
		t.Fatal("closed")
	}
	set(t, u, "Wall Cursor Position Enable", true)
	if v, err := u.OffsetControl(ctx, "Cursor X", 15, ultimatte.Options{}); err != nil || v != 207 {
		t.Error(v, err)
	}
	_, err = u.OffsetControl(ctx, "Cursor 2 Y", -15, ultimatte.Options{}) // floor cursor still off
	wantErr(t, err, ultimatte.ErrMessage, "")
	set(t, u, "Floor Cursor Position Enable", true)
	if v, err := u.OffsetControl(ctx, "Cursor 2 Y", -15, ultimatte.Options{}); err != nil || v != 957 {
		t.Error(v, err)
	}
}

// test_4k_same_value_write_is_echoed
func Test4KSameValueWriteIsEchoed(t *testing.T) {
	k := captured(t)
	u := session4K(t, k, 300*time.Millisecond, idle)
	if v, _ := u.GetControl("Matte Density"); v != 2500 {
		t.Error(v)
	}
	if set(t, u, "Matte Density", 2500) != 2500 {
		t.Error("echo")
	}
	waitFor(t, u, func(s *ultimatte.State) bool { m := s.CurrentFile.Modified(); return m != nil && *m })
	if r := k.Received(); r[len(r)-1] != "CONTROL:\nMatte Density: 2500" {
		t.Error(r)
	}
}

// test_4k_colour_stream_does_not_hold_a_wait_open
func Test4KColourStreamDoesNotHoldAWaitOpen(t *testing.T) {
	k := captured(t)
	k.StreamInterval = 10 * time.Millisecond
	k.SilentControls["Matte Density"] = true
	u := session4K(t, k, 300*time.Millisecond, idle)
	if set(t, u, "Wall Cursor Position Enable", true) != true {
		t.Fatal("cursor")
	}
	start := time.Now()
	_, err := u.SetControl(ctx, "Matte Density", 3000, ultimatte.Options{})
	wantErr(t, err, ultimatte.ErrTimeout, "cursor colour stream")
	if time.Since(start) > 1500*time.Millisecond {
		t.Error(time.Since(start))
	}
	u = session4K(t, k, 300*time.Millisecond, idle)
	start = time.Now()
	wantErr(t, u.WaitFor(ctx, func(*ultimatte.State) bool { return false }, 0), ultimatte.ErrTimeout, "")
	if time.Since(start) > 1500*time.Millisecond || u.State.Controls["Wall Color Red"] != 5490 { // still applied
		t.Error(time.Since(start))
	}
}

// test_4k_writes_go_through_the_colour_stream
func Test4KWritesGoThroughTheColourStream(t *testing.T) {
	k := captured(t)
	k.StreamInterval = 5 * time.Millisecond
	u := session4K(t, k, 300*time.Millisecond, idle)
	set(t, u, "Wall Cursor Position Enable", true)
	set(t, u, "Floor Cursor Position Enable", true)
	for _, v := range []int{2600, 2700, 2500} {
		if set(t, u, "Matte Density", v) != v {
			t.Error(v)
		}
	}
	if v, err := u.OffsetControl(ctx, "Cursor 2 X", -15, ultimatte.Options{}); err != nil || v != 1713 {
		t.Error(v, err)
	}
}

// test_4k_refresh_spells_the_header_as_software_control
func Test4KRefreshSpellsTheHeaderAsSoftwareControl(t *testing.T) {
	k := captured(t)
	u := session4K(t, k, 300*time.Millisecond, idle)
	for _, h := range []string{"FRAME BUFFER", "IMAGE LIST", "FILE LIST", "CURRENT FILE"} {
		if lines, err := u.Refresh(ctx, h); err != nil || lines[0] != h+":" {
			t.Error(lines, err)
		}
	}
	if !slices.Equal(k.Received(), []string{"Frame Buffer:", "Image List:", "File List:", "Current File:"}) {
		t.Error(k.Received())
	}
}

// ---- the media pool, test_media.py's session tests -----------------------------

func assigned(t *testing.T, u *ultimatte.Session, buffer string) string {
	t.Helper()
	v, ok, err := u.AssignedImage(buffer)
	if err != nil || !ok {
		t.Fatal(buffer, ok, err)
	}
	return v
}

// test_reads_the_4k_image_list_and_named_frame_buffers
func TestReadsThe4KImageListAndNamedFrameBuffers(t *testing.T) {
	k := captured(t)
	k.PutImage("Old.png", 1920, 1080, "Background 1")
	u := session(t, k)
	s := u.State
	if !slices.Equal(s.ImageList, []string{"Old.png"}) || *s.ImageCapacity != 6736052224 || *s.ImageAvailable <= 0 {
		t.Error(s.ImageList)
	}
	if s.FrameBuffer.Assigned["BG 1"] != "Old.png" || s.FrameBuffer.Assigned["GM"] != "" ||
		!reflect.DeepEqual(s.FrameBuffer.Mix, map[string]any{"BG": 0, "LY": 0}) {
		t.Error(s.FrameBuffer)
	}
	if assigned(t, u, "BG 1") != "Old.png" || assigned(t, u, "Background 1") != "Old.png" {
		t.Error("assigned")
	}
}

func upload(t *testing.T, u *ultimatte.Session, name string, data []byte, check bool) []string {
	t.Helper()
	list, err := u.UploadImage(ctx, name, bytes.NewReader(data), -1, nil, check, 0)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// test_upload_assign_delete
func TestUploadAssignDelete(t *testing.T) {
	k := captured(t)
	u := session(t, k)
	if l := upload(t, u, "Test.png", fakeultimatte.PNG(1920, 1080), true); !slices.Equal(l, []string{"Test.png"}) {
		t.Error(l)
	}
	if _, err := u.AssignImage(ctx, "BG 1", "Test.png", true); err != nil {
		t.Fatal(err)
	}
	if assigned(t, u, "BG 1") != "Test.png" || !slices.Contains(k.Received(), "Frame Buffer:\nBackground 1: Test.png") {
		t.Error(k.Received())
	}
	if l, err := u.DeleteImage(ctx, "Test.png"); err != nil || len(l) != 0 {
		t.Error(l, err)
	}
	if !slices.Contains(k.Received(), "Image:\nDelete: Test.png") || assigned(t, u, "BG 1") != "" { // the unit cleared it
		t.Error(k.Received())
	}
}

// test_assign_refused_without_fg_is_an_error
func TestAssignRefusedWithoutFGIsAnError(t *testing.T) {
	k := captured(t)
	k.SetFGLocked(false)
	u := session(t, k)
	upload(t, u, "Test.png", fakeultimatte.PNG(1920, 1080), true)
	_, err := u.AssignImage(ctx, "BG 1", "Test.png", true)
	wantErr(t, err, ultimatte.ErrMessage, "No Foreground")
}

// test_assign_refused_for_mismatched_format
func TestAssignRefusedForMismatchedFormat(t *testing.T) {
	u := session(t, captured(t))
	upload(t, u, "Big.png", fakeultimatte.PNG(3840, 2160), false)
	_, err := u.AssignImage(ctx, "BG 1", "Big.png", true)
	wantErr(t, err, ultimatte.ErrMessage, "Mismatched formats")
}

// test_assign_and_delete_need_an_image_in_the_pool
func TestAssignAndDeleteNeedAnImageInThePool(t *testing.T) {
	u := session(t, captured(t))
	if len(u.State.ImageList) != 0 || len(u.State.FrameBuffer.Assigned) == 0 {
		t.Fatal(u.State.ImageList)
	}
	_, err := u.AssignImage(ctx, "BG 1", "Missing.png", true)
	wantErr(t, err, ultimatte.ErrControlValue, "")
	_, err = u.DeleteImage(ctx, "Missing.png")
	wantErr(t, err, ultimatte.ErrControlValue, "")
}

// test_flush_settings
func TestFlushSettings(t *testing.T) {
	k := captured(t)
	if err := session(t, k).FlushSettings(ctx, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if k.Flushes() != 1 || !slices.Contains(k.Received(), "CONTROL:\nFLUSH SETTINGS: YES") {
		t.Error(k.Flushes(), k.Received())
	}
}

// test_flush_refused_without_fg
func TestFlushRefusedWithoutFG(t *testing.T) {
	k := captured(t)
	k.SetFGLocked(false)
	wantErr(t, session(t, k).FlushSettings(ctx, 200*time.Millisecond), ultimatte.ErrMessage, "No Foreground")
	if k.Flushes() != 0 {
		t.Error(k.Flushes())
	}
}

// test_documented_form_still_assigns_by_number
func TestDocumentedFormStillAssignsByNumber(t *testing.T) {
	u := session(t, keyer(t, "8K"))
	if _, err := u.AssignImage(ctx, "BG 1", "Newsroom", true); err != nil {
		t.Fatal(err)
	}
	if assigned(t, u, "BG 1") != "Newsroom" {
		t.Error("assigned")
	}
}

// test_session_uploads_and_assigns_a_jpg
func TestSessionUploadsAndAssignsAJPG(t *testing.T) {
	k := captured(t)
	u := session(t, k)
	var seen []int64
	l, err := u.UploadImage(ctx, "jpg-test-1080.jpg", bytes.NewReader(fakeultimatte.JPG(1920, 1080, 0)), -1,
		func(sent, _ int64) error { seen = append(seen, sent); return nil }, true, 0)
	if err != nil || !slices.Equal(l, []string{"jpg-test-1080.jpg"}) || len(seen) == 0 {
		t.Fatal(l, err)
	}
	for _, b := range []string{"Background 1", "Background 2"} {
		if _, err := u.AssignImage(ctx, b, "jpg-test-1080.jpg", true); err != nil {
			t.Fatal(err)
		}
	}
	if assigned(t, u, "BG 2") != "jpg-test-1080.jpg" {
		t.Error("BG 2")
	}
	// the unit sets the BG matte input itself
	waitFor(t, u, func(s *ultimatte.State) bool { return s.Controls["BM Input Level"] == 6878 })
	if u.State.Controls["BM Input Offset"] != 10000 {
		t.Error(u.State.Controls["BM Input Offset"])
	}
}

// test_masks_assign_by_name
func TestMasksAssignByName(t *testing.T) {
	for _, c := range [][3]string{{"Garbage Matte", "GM", "GM"}, {"Holdout Matte", "HM", "HM"}, {"GM", "GM", "GM"}, {"hm", "HM", "HM"}} {
		buffer, short, prefix := c[0], c[1], c[2]
		k := captured(t)
		u := session(t, k)
		upload(t, u, "mask.png", fakeultimatte.PNG(1920, 1080), true)
		if _, err := u.AssignImage(ctx, buffer, "mask.png", true); err != nil {
			t.Fatal(err)
		}
		long := map[string]string{"GM": "Garbage Matte", "HM": "Holdout Matte"}[short]
		if assigned(t, u, short) != "mask.png" || !slices.Contains(k.Received(), "Frame Buffer:\n"+long+": mask.png") {
			t.Error(buffer, k.Received())
		}
		waitFor(t, u, func(s *ultimatte.State) bool { return s.Controls[prefix+" Input Level"] == 6878 })
		if u.State.Controls[prefix+" Input Offset"] != 10000 || u.State.Controls[prefix+" Enable"] != false { // not turned on
			t.Error(buffer, u.State.Controls[prefix+" Enable"])
		}
	}
}

// test_upload_streams_from_a_path_a_file_and_chunks, against the unit:
// what it took is what was sent.
func TestUploadedImagesLandInThePool(t *testing.T) {
	k := captured(t)
	u := session(t, k)
	data := fakeultimatte.JPG(1920, 1080, 50000)
	upload(t, u, "A.jpg", data, true)
	if !bytes.Equal(k.Pool()["A.jpg"], data) || len(k.Uploads()) != 1 {
		t.Error(len(k.Uploads()))
	}
}
