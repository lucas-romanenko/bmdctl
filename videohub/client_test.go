// SPDX-License-Identifier: MIT

package videohub

// bmdwire v1.6.0's videohubwire/tests/test_videohubwire_client.py,
// ported one for one: each test names its Python counterpart. The
// preamble is the tests' own, a Smart Videohub 40 x 40's shape on
// protocol 2.7 (testdata/fixtures).

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"net"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// preamble is PREAMBLE + END_PRELUDE, the tests' usual stream.
func preamble(t *testing.T) []byte {
	return append(fixture(t, "preamble.bin"), fixture(t, "end-prelude.bin")...)
}

// connect is the tests' _connect: a session on a scripted hub.
func connect(t *testing.T, chunks ...[]byte) (*Session, *scripted) {
	t.Helper()
	c := newScripted(chunks...)
	s, err := testDialer(c).Dial(context.Background(), "192.0.2.31")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, c
}

// dialErr dials a scripted hub that must fail.
func dialErr(t *testing.T, chunks ...[]byte) (error, *scripted) {
	t.Helper()
	c := newScripted(chunks...)
	s, err := testDialer(c).Dial(context.Background(), "192.0.2.31")
	if err == nil {
		s.Close()
		t.Fatal("Dial succeeded")
	}
	return err, c
}

func wantMsg(t *testing.T, err error, sub string) {
	t.Helper()
	var ve *Error
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), sub) {
		t.Fatalf("error %v (%T), want a VideohubError with %q", err, err, sub)
	}
}

// ---------------------------------------------------------------------------
// Preamble / state
// ---------------------------------------------------------------------------

// test_preamble_with_end_prelude_marker
func TestPreambleWithEndPreludeMarker(t *testing.T) {
	s, _ := connect(t, preamble(t))
	st := s.State()
	in, _ := st.VideoInputs()
	out, _ := st.VideoOutputs()
	if st.ProtocolVersion != "2.7" || st.Device["model name"] != "Blackmagic Smart Videohub 40 x 40" || in != 4 || out != 4 {
		t.Fatalf("%+v", st)
	}
}

// test_preamble_without_end_prelude_marker: older firmware never sends
// the marker, so the preamble ends when the wire goes quiet.
func TestPreambleWithoutEndPreludeMarker(t *testing.T) {
	s, _ := connect(t, fixture(t, "preamble.bin"))
	if r := s.State().Routing; !maps.Equal(r, map[int]int{0: 0, 1: 2, 2: 1, 3: 3}) {
		t.Fatalf("routing %v", r)
	}
}

// test_labels_with_spaces_and_fallbacks: port 3's label is empty on the
// wire, so it falls back to a name.
func TestLabelsWithSpacesAndFallbacks(t *testing.T) {
	s, _ := connect(t, preamble(t))
	snap, err := s.Snapshot()
	if err != nil || snap.Inputs[2].Label != "Replay Out" || snap.Inputs[3].Label != "Input 4" || snap.Outputs[3].Label != "Output 4" {
		t.Fatalf("%+v %v", snap, err)
	}
}

// test_state_snapshot_shape
func TestStateSnapshotShape(t *testing.T) {
	s, _ := connect(t, preamble(t))
	snap, _ := s.Snapshot()
	var sources []int
	var locks []string
	for _, o := range snap.Outputs {
		sources = append(sources, *o.Source)
		locks = append(locks, o.Lock)
	}
	if snap.Device.FriendlyName != "Test Hub" || snap.Device.VideoInputs != 4 ||
		!equal(sources, []int{0, 2, 1, 3}) || !equal(locks, []string{"U", "L", "O", "U"}) {
		t.Fatalf("%+v", snap)
	}
}

func equal[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// test_state_drains_pushed_updates: another client re-routes.
func TestStateDrainsPushedUpdates(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.push([]byte("VIDEO OUTPUT ROUTING:\n1 3\n\n"))
	settled(t, s, c)
	if snap, _ := s.Snapshot(); *snap.Outputs[1].Source != 3 {
		t.Fatalf("output 1 from %d", *snap.Outputs[1].Source)
	}
}

// test_preamble_garbage_raises
func TestPreambleGarbageRaises(t *testing.T) {
	err, _ := dialErr(t, []byte("220 Welcome to some FTP server\r\n"))
	wantMsg(t, err, "no state preamble")
}

// test_closed_connection_raises: an empty recv is the peer closing.
func TestClosedConnectionRaises(t *testing.T) {
	err, _ := dialErr(t, []byte{})
	wantMsg(t, err, "connection closed by hub")
}

// ---------------------------------------------------------------------------
// route()
// ---------------------------------------------------------------------------

// test_route_sends_wire_bytes_and_acks, with the optimistic apply.
func TestRouteSendsWireBytesAndAcks(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("ACK\n\n"))
	if err := s.Route(context.Background(), 1, 3); err != nil {
		t.Fatal(err)
	}
	if c.written() != "VIDEO OUTPUT ROUTING:\n1 3\n\n" || s.State().Routing[1] != 3 {
		t.Fatalf("sent %q, routing %v", c.written(), s.State().Routing)
	}
}

// test_route_nak_raises
func TestRouteNakRaises(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("NAK\n\n"))
	err := s.Route(context.Background(), 1, 3)
	wantMsg(t, err, "refused")
	if !errors.Is(err, ErrRefused) {
		t.Errorf("%v is not ErrRefused", err)
	}
}

// test_route_applies_interleaved_push_before_ack
func TestRouteAppliesInterleavedPushBeforeAck(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("VIDEO OUTPUT ROUTING:\n3 0\n\nACK\n\n"))
	if err := s.Route(context.Background(), 1, 3); err != nil {
		t.Fatal(err)
	}
	if r := s.State().Routing; r[3] != 0 || r[1] != 3 {
		t.Fatalf("routing %v", r)
	}
}

// test_route_bounds_checked_against_hub_size
func TestRouteBoundsCheckedAgainstHubSize(t *testing.T) {
	s, c := connect(t, preamble(t))
	ctx := context.Background()
	wantMsg(t, s.Route(ctx, 99, 0), "outside")
	wantMsg(t, s.Route(ctx, 0, 99), "outside")
	wantMsg(t, s.Route(ctx, -1, 0), "invalid route")
	if c.written() != "" {
		t.Errorf("sent %q", c.written())
	}
}

// test_route_timeout_without_response. Here the session ends with it
// (docs/PORTING.md, "Videohub"): the hub's late answer would be read as
// the next command's.
func TestRouteTimeoutWithoutResponse(t *testing.T) {
	s, _ := connect(t, preamble(t))
	err := s.Route(context.Background(), 1, 3)
	wantMsg(t, err, "timed out")
	if !errors.Is(err, ErrTimeout) || s.Connected() {
		t.Errorf("%v; connected %v", err, s.Connected())
	}
}

// ---------------------------------------------------------------------------
// Label editing
// ---------------------------------------------------------------------------

// test_set_input_label_sends_wire_bytes_and_acks
func TestSetInputLabelSendsWireBytesAndAcks(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("ACK\n\n"))
	if err := s.SetInputLabel(context.Background(), 2, "Replay B"); err != nil {
		t.Fatal(err)
	}
	if c.written() != "INPUT LABELS:\n2 Replay B\n\n" || s.State().InputLabels[2] != "Replay B" {
		t.Fatalf("sent %q, labels %v", c.written(), s.State().InputLabels)
	}
}

// test_set_output_label_sends_wire_bytes_and_acks
func TestSetOutputLabelSendsWireBytesAndAcks(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("ACK\n\n"))
	if err := s.SetOutputLabel(context.Background(), 0, "Wall Monitor 1"); err != nil {
		t.Fatal(err)
	}
	if c.written() != "OUTPUT LABELS:\n0 Wall Monitor 1\n\n" || s.State().OutputLabels[0] != "Wall Monitor 1" {
		t.Fatalf("sent %q, labels %v", c.written(), s.State().OutputLabels)
	}
}

// test_set_label_nak_raises_and_keeps_old_label
func TestSetLabelNakRaisesAndKeepsOldLabel(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("NAK\n\n"))
	wantMsg(t, s.SetInputLabel(context.Background(), 0, "Nope"), "refused")
	if l := s.State().InputLabels[0]; l != "Camera 1" {
		t.Fatalf("label %q", l)
	}
}

// test_set_label_strips_newlines_that_would_break_framing
func TestSetLabelStripsNewlinesThatWouldBreakFraming(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("ACK\n\n"))
	if err := s.SetInputLabel(context.Background(), 0, "CAM\n1\r\nEVIL"); err != nil {
		t.Fatal(err)
	}
	if c.written() != "INPUT LABELS:\n0 CAM 1 EVIL\n\n" {
		t.Fatalf("sent %q", c.written())
	}
}

// test_set_label_length_and_bounds
func TestSetLabelLengthAndBounds(t *testing.T) {
	s, c := connect(t, preamble(t))
	ctx := context.Background()
	wantMsg(t, s.SetInputLabel(ctx, 0, strings.Repeat("x", 65)), "too long")
	wantMsg(t, s.SetInputLabel(ctx, 99, "x"), "outside")
	wantMsg(t, s.SetOutputLabel(ctx, -1, "x"), "outside")
	if c.written() != "" {
		t.Errorf("sent %q", c.written())
	}
}

// test_set_label_applies_interleaved_push_before_ack
func TestSetLabelAppliesInterleavedPushBeforeAck(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("VIDEO OUTPUT ROUTING:\n3 1\n\nACK\n\n"))
	if err := s.SetInputLabel(context.Background(), 1, "Camera 2B"); err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.Routing[3] != 1 || st.InputLabels[1] != "Camera 2B" {
		t.Fatalf("%+v", st)
	}
}

// ---------------------------------------------------------------------------
// Connect / reconnect / close
// ---------------------------------------------------------------------------

// test_connect_retries_once_after_transient_oserror
func TestConnectRetriesOnceAfterTransientOSError(t *testing.T) {
	var attempts []string
	c := newScripted(preamble(t))
	d := testDialer(c)
	d.DialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
		attempts = append(attempts, addr)
		if len(attempts) == 1 {
			return nil, errors.New("connection refused")
		}
		return c, nil
	}
	s, err := d.Dial(context.Background(), "192.0.2.31")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if in, _ := s.State().VideoInputs(); !equal(attempts, []string{"192.0.2.31:9990", "192.0.2.31:9990"}) || in != 4 {
		t.Fatalf("attempts %v, inputs %d", attempts, in)
	}
}

// test_connect_second_failure_propagates
func TestConnectSecondFailurePropagates(t *testing.T) {
	d := testDialer(nil)
	d.DialContext = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unreachable") }
	if s, err := d.Dial(context.Background(), "192.0.2.31"); s != nil || err == nil || err.Error() != "unreachable" {
		t.Fatalf("%v, %v", s, err)
	}
}

// test_reconnect_closes_previous_socket. A Session doesn't reconnect: a
// new one is dialled, and closing the first closes its connection only.
func TestReconnectClosesPreviousSocket(t *testing.T) {
	first, second := newScripted(preamble(t)), newScripted(preamble(t))
	conns := []net.Conn{first, second}
	d := testDialer(nil)
	d.DialContext = func(context.Context, string, string) (net.Conn, error) {
		c := conns[0]
		conns = conns[1:]
		return c, nil
	}
	a, err := d.Dial(context.Background(), "192.0.2.31")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Dial(context.Background(), "192.0.2.31")
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	if !first.isClosed() || second.isClosed() {
		t.Fatalf("closed %v %v", first.isClosed(), second.isClosed())
	}
	b.Close()
	if !second.isClosed() {
		t.Fatal("second not closed")
	}
}

// test_context_manager_closes_and_close_is_idempotent
func TestCloseClosesAndIsIdempotent(t *testing.T) {
	s, c := connect(t, preamble(t))
	if out, _ := s.State().VideoOutputs(); out != 4 {
		t.Fatalf("outputs %d", out)
	}
	s.Close()
	s.Close()
	if !c.isClosed() || s.Connected() {
		t.Fatal("not closed")
	}
	wantMsg(t, s.Ping(context.Background()), "not connected")
}

// test_preamble_failure_closes_socket
func TestPreambleFailureClosesSocket(t *testing.T) {
	err, c := dialErr(t, []byte("garbage\r\n"))
	wantMsg(t, err, "no state preamble")
	if !c.isClosed() {
		t.Fatal("not closed")
	}
}

// test_hub_with_no_routing_block_and_no_marker_fails_to_connect: quiet
// completion needs a routing block.
func TestHubWithNoRoutingBlockAndNoMarkerFailsToConnect(t *testing.T) {
	pre, _, _ := bytes.Cut(fixture(t, "preamble.bin"), []byte("VIDEO OUTPUT ROUTING:"))
	err, _ := dialErr(t, pre)
	wantMsg(t, err, "no state preamble")
}

// ---------------------------------------------------------------------------
// ping() and parsing edge cases
// ---------------------------------------------------------------------------

// test_ping_acks_and_applies_interleaved_push
func TestPingAcksAndAppliesInterleavedPush(t *testing.T) {
	s, c := connect(t, preamble(t))
	c.reply([]byte("INPUT LABELS:\n0 Renamed\n\nACK\n\n"))
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.written() != "PING:\n\n" || s.State().InputLabels[0] != "Renamed" {
		t.Fatalf("sent %q, labels %v", c.written(), s.State().InputLabels)
	}
}

// test_ping_timeout_raises
func TestPingTimeoutRaises(t *testing.T) {
	s, _ := connect(t, preamble(t))
	wantMsg(t, s.Ping(context.Background()), "PING")
}

// test_malformed_indexed_line_is_skipped_not_fatal
func TestMalformedIndexedLineIsSkippedNotFatal(t *testing.T) {
	var log bytes.Buffer
	c := newScripted(append(append(fixture(t, "preamble.bin"), "VIDEO OUTPUT ROUTING:\nnot-a-number 3\n2 x\n\n"...),
		fixture(t, "end-prelude.bin")...))
	d := testDialer(c)
	d.Logger = slog.New(slog.NewTextHandler(&log, nil))
	s, err := d.Dial(context.Background(), "192.0.2.31")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.State().Routing; !maps.Equal(r, map[int]int{0: 0, 1: 2, 2: 1, 3: 3}) || !strings.Contains(log.String(), "skipping malformed line") {
		t.Fatalf("routing %v, log %s", r, log.String())
	}
}

// test_counts_fall_back_to_label_indices_when_device_omits_them
func TestCountsFallBackToLabelIndicesWhenDeviceOmitsThem(t *testing.T) {
	pre := bytes.ReplaceAll(bytes.ReplaceAll(fixture(t, "preamble.bin"), []byte("Video inputs: 4\n"), nil),
		[]byte("Video outputs: 4\n"), nil)
	s, _ := connect(t, append(pre, fixture(t, "end-prelude.bin")...))
	if snap, _ := s.Snapshot(); snap.Device.VideoInputs != 4 || snap.Device.VideoOutputs != 4 {
		t.Fatalf("%+v", snap.Device)
	}
}

// test_unknown_blocks_are_ignored
func TestUnknownBlocksAreIgnored(t *testing.T) {
	extra := "CONFIGURATION:\nTake Mode: true\n\nSERIAL PORT LABELS:\n0 Deck\n\n"
	s, _ := connect(t, append(append(fixture(t, "preamble.bin"), extra...), fixture(t, "end-prelude.bin")...))
	st := s.State()
	if in, _ := st.VideoInputs(); in != 4 || !maps.Equal(st.Routing, map[int]int{0: 0, 1: 2, 2: 1, 3: 3}) {
		t.Fatalf("%+v", st)
	}
}
