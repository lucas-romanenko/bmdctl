// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// cmdBytes builds one command: the 8-byte header and the payload.
func cmdBytes(code string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(b, uint16(len(b)))
	copy(b[4:], code)
	copy(b[8:], payload)
	return b
}

// testCmd is a Command for session tests.
type testCmd struct {
	op      string
	payload []byte
	echo    string
	invalid error
}

func (c testCmd) Opcode() string    { return c.op }
func (c testCmd) Encode() []byte    { return cmdBytes(c.op, c.payload) }
func (c testCmd) EchoKey() string   { return c.echo }
func (c testCmd) Unverified() bool  { return false }
func (c testCmd) Destructive() bool { return false }
func (c testCmd) Validate() error   { return c.invalid }

// switcher plays the far end of a fakeConn: it reads what the session
// writes and answers with datagrams.
type switcher struct {
	t          *testing.T
	c          *fakeConn
	read       int    // writes consumed so far
	seq        uint16 // last reliable sequence sent
	session    uint16
	handshakes int
}

func newSwitcher(t *testing.T, c *fakeConn) *switcher {
	return &switcher{t: t, c: c, session: 0x8001}
}

// expect waits for the next write matching pred, skipping others.
func (sw *switcher) expect(what string, pred func(packet) bool) packet {
	sw.t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		sw.c.mu.Lock()
		for sw.read < len(sw.c.writes) {
			w := sw.c.writes[sw.read]
			sw.read++
			p, err := parsePacket(w.b)
			if err != nil {
				sw.c.mu.Unlock()
				sw.t.Fatalf("session wrote a malformed packet: %v", err)
			}
			if pred(p) {
				sw.c.mu.Unlock()
				return p
			}
		}
		sw.c.mu.Unlock()
		select {
		case <-sw.c.wrote:
		case <-deadline:
			sw.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func isSYN(hello byte) func(packet) bool {
	return func(p packet) bool { return p.flags&flagSYN != 0 && len(p.data) > 0 && p.data[0] == hello }
}

func carries(code string) func(packet) bool {
	return func(p packet) bool { return p.flags&flagReliable != 0 && bytes.Contains(p.data, []byte(code)) }
}

func (sw *switcher) send(flags uint8, seq uint16, data []byte) {
	sw.c.in <- fakeDatagram{b: rawDatagram(sw.session, seq, flags, data), from: switcherAddr}
}

// handshake answers the session's SYN and waits for the ACK that ends the
// handshake.
func (sw *switcher) handshake() {
	sw.t.Helper()
	sw.expect("SYN", isSYN(helloConnect))
	sw.c.in <- fakeDatagram{b: rawDatagram(helloSession, 0, flagSYN, []byte{helloAccepted, 0, 0, 0, 0, 0, 0, 0}), from: switcherAddr}
	sw.expect("handshake ACK", func(p packet) bool { return p.flags == flagACK && p.session == helloSession })
	// Every handshake gets a new session id, as on a switcher: 0x8001,
	// 0x8002 ...
	sw.handshakes++
	sw.session = 0x8000 | uint16(sw.handshakes)
	sw.seq = 0
}

// reliable sends one reliable packet holding cmds.
func (sw *switcher) reliable(cmds ...[]byte) {
	sw.seq = (sw.seq + 1) & seqMask
	sw.send(flagReliable, sw.seq, bytes.Join(cmds, nil))
}

// ping is the post-dump control packet: reliable, no data.
func (sw *switcher) ping() { sw.reliable() }

func (sw *switcher) dump() {
	sw.reliable(cmdBytes("_ver", []byte{0, 2, 0, 30}), cmdBytes("VidM", []byte{12, 0, 0, 0}))
	sw.reliable(cmdBytes("PrgI", []byte{0, 0, 0, 1}), cmdBytes("InCm", []byte{1, 0, 0, 0}))
	sw.ping()
}

type dialResult struct {
	s   *Session
	err error
}

func startDial(ctx context.Context, c *fakeConn, silence time.Duration) chan dialResult {
	ch := make(chan dialResult, 1)
	go func() {
		d := &Dialer{Conn: c, silence: silence}
		s, err := d.Dial(ctx, "127.0.0.1")
		ch <- dialResult{s, err}
	}()
	return ch
}

// dialed returns a session that has completed its handshake and dump.
func dialed(t *testing.T, silence time.Duration) (*Session, *switcher) {
	t.Helper()
	c := newFakeConn()
	res := startDial(context.Background(), c, silence)
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.dump()
	r := <-res
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { r.s.Close() })
	return r.s, sw
}

func closedWithin(ch <-chan struct{}, d time.Duration) bool {
	select {
	case <-ch:
		return true
	default:
	}
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

func TestDialHandshakeDumpReady(t *testing.T) {
	s, sw := dialed(t, 0)
	if !s.Connected() {
		t.Fatal("not connected after Dial")
	}
	if !closedWithin(s.Ready(), 0) {
		t.Fatal("Ready not closed after Dial")
	}
	st := s.State()
	if st.Identity.Firmware == nil || st.Identity.Firmware.Version != "2.30" {
		t.Errorf("firmware %+v", st.Identity.Firmware)
	}
	if st.VideoMode.Mode == nil || st.VideoMode.Mode.Label() != "1080p50" {
		t.Errorf("video mode %+v", st.VideoMode.Mode)
	}
	if len(st.MEs) != 1 || st.MEs[0].Program == nil || st.MEs[0].Program.Source != 1 {
		t.Errorf("MEs %+v", st.MEs)
	}
	if _, ok := st.Raw["InCm"]; !ok {
		t.Error("InCm not stored raw")
	}
	// The dump's ready triggers one TiRq, atemwire's only time request.
	sw.expect("TiRq", carries("TiRq"))
}

// test_wait_ready_waits_for_the_end_of_the_dump: the video mode arriving
// first is not ready; the end of the dump (InCm, then the next packet) is.
func TestDialWaitsForTheEndOfTheDump(t *testing.T) {
	c := newFakeConn()
	res := startDial(context.Background(), c, 0)
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.reliable(cmdBytes("VidM", []byte{12, 0, 0, 0}))
	sw.reliable(cmdBytes("InCm", []byte{1, 0, 0, 0}))
	select {
	case r := <-res:
		t.Fatalf("Dial returned before the packet after InCm: %v", r.err)
	case <-time.After(50 * time.Millisecond):
	}
	sw.ping()
	if r := <-res; r.err != nil {
		t.Fatal(r.err)
	} else {
		r.s.Close()
	}
}

// test_wait_ready_times_out_if_the_dump_never_completes, and atemwire's
// connect failure path: the session says goodbye before giving up.
func TestDialTimesOutIfTheDumpNeverCompletes(t *testing.T) {
	c := newFakeConn()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res := startDial(ctx, c, 0)
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.reliable(cmdBytes("VidM", []byte{12, 0, 0, 0}))
	r := <-res
	if !errors.Is(r.err, context.DeadlineExceeded) {
		t.Fatalf("err %v", r.err)
	}
	sw.expect("goodbye", isSYN(helloClosing))
}

func TestDialDefaultPort(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"127.0.0.1", "127.0.0.1:9910"},
		{"127.0.0.1:9911", "127.0.0.1:9911"},
	} {
		a, err := resolve(context.Background(), tc.in)
		if err != nil || a.String() != tc.want {
			t.Errorf("resolve(%q) = %v, %v", tc.in, a, err)
		}
	}
}

func TestSendLoneCommandGoesAtOnce(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.expect("TiRq", carries("TiRq"))
	time.Sleep(2 * burstSpacing)
	start := time.Now()
	if err := s.Send(testCmd{op: "CPgI", payload: []byte{0, 0, 0, 5}}); err != nil {
		t.Fatal(err)
	}
	p := sw.expect("CPgI", carries("CPgI"))
	if !bytes.Equal(p.data, cmdBytes("CPgI", []byte{0, 0, 0, 5})) {
		t.Fatalf("data %x", p.data)
	}
	if d := time.Since(start); d >= burstSpacing {
		t.Fatalf("a lone command waited %v", d)
	}
}

// A burst leaves in packets of at most 1200 bytes, in order, at least
// burstSpacing apart (atemwire's BUDGET and BURST_SEND_SPACING).
func TestSendBurstIsPackedAndPaced(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.expect("TiRq", carries("TiRq"))
	const n = 150 // 150 * 20 bytes = 3000: three packets
	for i := range n {
		if err := s.Send(testCmd{op: "CTst", payload: binary.BigEndian.AppendUint32(make([]byte, 8), uint32(i))}); err != nil {
			t.Fatal(err)
		}
	}
	var got []byte
	for len(got) < n*20 {
		p := sw.expect("burst", carries("CTst"))
		if len(p.data) > maxCommandBytes {
			t.Fatalf("packet of %d bytes", len(p.data))
		}
		got = append(got, p.data...)
	}
	for i := range n {
		c := got[i*20 : (i+1)*20]
		if string(c[4:8]) != "CTst" || binary.BigEndian.Uint32(c[16:]) != uint32(i) {
			t.Fatalf("command %d out of order: %x", i, c)
		}
	}
	var times []time.Time
	sw.c.mu.Lock()
	for _, w := range sw.c.writes {
		if bytes.Contains(w.b, []byte("CTst")) {
			times = append(times, w.at)
		}
	}
	sw.c.mu.Unlock()
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < burstSpacing-time.Millisecond {
			t.Fatalf("packets %d and %d only %v apart", i-1, i, gap)
		}
	}
	if len(times) > 6 {
		t.Fatalf("%d packets for %d bytes: not packed", len(times), n*20)
	}
}

func TestSendRejects(t *testing.T) {
	s, _ := dialed(t, 0)
	if err := s.Send(testCmd{op: "CTst", invalid: errors.New("bad")}); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("invalid command: %v", err)
	}
	if err := s.Send(testCmd{op: "CTst", payload: make([]byte, maxCommandBytes)}); err == nil {
		t.Fatal("oversize command accepted")
	}
}

func TestSendAndWaitReturnsTheEcho(t *testing.T) {
	s, sw := dialed(t, 0)
	done := make(chan dialResult, 1)
	var change Change
	go func() {
		c, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", payload: []byte{0, 0, 0, 5}, echo: "PrgI:0"})
		change = c
		done <- dialResult{err: err}
	}()
	sw.expect("CPgI", carries("CPgI"))
	sw.reliable(cmdBytes("PrgI", []byte{0, 0, 0, 5}))
	if r := <-done; r.err != nil {
		t.Fatal(r.err)
	}
	if f, ok := change.Value.(*ProgramBusInputField); change.Kind != ChangeState || change.Key != "PrgI:0" || !ok || f.Source != 5 {
		t.Fatalf("change %+v", change)
	}
	if s.State().MEs[0].Program.Source != 5 {
		t.Fatal("state not updated")
	}
}

func TestSendAndWaitErrors(t *testing.T) {
	s, sw := dialed(t, 0)
	if _, err := s.SendAndWait(context.Background(), testCmd{op: "DCut"}); !errors.Is(err, ErrNoEcho) {
		t.Fatalf("no echo: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.SendAndWait(ctx, testCmd{op: "CPgI", echo: "PrgI:0"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	sw.expect("CPgI", carries("CPgI")) // it was still sent
	if n := len(waitersOf(s)); n != 0 {
		t.Fatalf("%d waiters left behind", n)
	}
}

func waitersOf(s *Session) map[string][]*waiter {
	ch := make(chan map[string][]*waiter, 1)
	_ = s.do(func() { ch <- s.waiters })
	return <-ch
}

func TestSubscribe(t *testing.T) {
	s, sw := dialed(t, 0)
	ch, cancel := s.Subscribe()
	sw.reliable(cmdBytes("PrvI", []byte{0, 0, 0, 2, 0, 0, 0, 0}))
	select {
	case c := <-ch:
		f, ok := c.Value.(*PreviewBusInputField)
		if c.Kind != ChangeState || c.Opcode != "PrvI" || c.Key != "PrvI:0" || !ok || f.Source != 2 {
			t.Fatalf("change %+v", c)
		}
		f.Source = 99 // the copy is ours to change
		if s.State().MEs[0].Preview.Source != 2 {
			t.Fatal("a subscriber reached into the state")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change")
	}
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel open after cancel")
	}
	cancel() // idempotent
}

func TestSubscriberThatFallsBehindIsClosed(t *testing.T) {
	s, sw := dialed(t, 0)
	ch, _ := s.Subscribe()
	for range (subscriberBuffer + 10) / 10 {
		var cmds [][]byte
		for range 10 {
			cmds = append(cmds, cmdBytes("Spam", nil))
		}
		sw.reliable(cmds...)
	}
	sw.reliable(cmdBytes("Last", nil))
	// Don't read until every change has been published.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := s.State().Raw["Last"]; ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("packets not processed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	n := 0
	for range ch {
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("read %d changes before the close, want %d", n, subscriberBuffer)
	}
	if !s.Connected() {
		t.Fatal("a slow subscriber hurt the session")
	}
}

// After 5 s of silence (shortened here) the transport handshakes again:
// the state empties, Ready reopens and pending waiters fail, but the
// session stays connected, as atemwire's is_connected does. A new dump
// makes it ready again.
func TestSilenceDisconnectsAndRecovers(t *testing.T) {
	s, sw := dialed(t, 150*time.Millisecond)
	ch, _ := s.Subscribe()
	wait := make(chan error, 1)
	go func() {
		_, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", echo: "PrgI:0"})
		wait <- err
	}()
	sw.expect("SYN after silence", isSYN(helloConnect))
	if err := <-wait; !errors.Is(err, ErrDisconnected) {
		t.Fatalf("waiter: %v", err)
	}
	for c := range ch {
		if c.Kind == ChangeDisconnected {
			break
		}
	}
	if closedWithin(s.Ready(), 0) {
		t.Fatal("Ready still closed after the disconnect")
	}
	if !s.Connected() {
		t.Fatal("Connected went false during a re-handshake")
	}
	if st := s.State(); len(st.Raw) != 0 || st.MEs != nil || st.VideoMode.Mode != nil {
		t.Fatal("state not emptied")
	}
	if _, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", echo: "PrgI:0"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("SendAndWait during the gap: %v", err)
	}
	// The SYN above was answered by nobody; answer the next one.
	sw.read = 0
	sw.c.reset()
	sw.handshake()
	sw.dump()
	if !closedWithin(s.Ready(), 2*time.Second) {
		t.Fatal("not ready after the new dump")
	}
	if st := s.State(); len(st.MEs) != 1 || st.MEs[0].Program == nil {
		t.Fatal("state not refilled")
	}
}

// A zero-length command header is atemwire's ConnectionError: disconnect,
// goodbye, a fresh handshake.
func TestCorruptionForcesAReconnect(t *testing.T) {
	s, sw := dialed(t, 0)
	ch, _ := s.Subscribe()
	sw.reliable(cmdBytes("Xxxx", []byte{0, 0, 0, 2}), make([]byte, 8))
	sw.expect("goodbye", isSYN(helloClosing))
	sw.expect("SYN", isSYN(helloConnect))
	for c := range ch {
		if c.Kind == ChangeDisconnected {
			break
		}
	}
	if !s.Connected() {
		t.Fatal("not connected")
	}
}

// waitState polls State until ok returns true.
func waitState(t *testing.T, s *Session, ok func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := s.State()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("state never got there")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A truncated command header loses the rest of its packet only.
func TestTruncatedPacketKeepsWhatCameBefore(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.reliable(cmdBytes("Aaaa", []byte{1}), []byte{0, 12, 0})
	sw.reliable(cmdBytes("Bbbb", []byte{2}))
	st := waitState(t, s, func(st State) bool { _, ok := st.Raw["Bbbb"]; return ok })
	if _, ok := st.Raw["Aaaa"]; !ok {
		t.Fatal("the field before the truncation was lost")
	}
}

// A packet a parser rejects loses the rest of its packet, as an exception
// out of an atemwire Recv constructor does, and the session carries on.
// Here a 4-byte PrvI (it needs 5) takes the AuxS after it down with it.
func TestParseErrorLosesTheRestOfThePacket(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.reliable(cmdBytes("PrvI", []byte{0, 0, 0, 2}), cmdBytes("AuxS", []byte{0, 0, 0, 3}))
	sw.reliable(cmdBytes("Bbbb", []byte{2}))
	st := waitState(t, s, func(st State) bool { _, ok := st.Raw["Bbbb"]; return ok })
	if st.MEs[0].Preview != nil || st.Aux != nil {
		t.Fatalf("stored past the parse error: %+v %+v", st.MEs[0].Preview, st.Aux)
	}
	if !s.Connected() {
		t.Fatal("a bad packet ended the session")
	}
}

// With no transfer engine queued, atemwire answers a lock grant by
// releasing the lock at once (_transfer_trigger with an empty queue).
func TestUnexpectedLockGrantIsReleased(t *testing.T) {
	_, sw := dialed(t, 0)
	sw.reliable(cmdBytes("LKOB", []byte{0, 0, 0, 0}))
	p := sw.expect("LOCK release", carries("LOCK"))
	if !bytes.Equal(p.data, cmdBytes("LOCK", []byte{0, 0, 0, 0})) {
		t.Fatalf("release %x", p.data)
	}
}

// The last-run macro latch (test_macro_slot_0_latches_as_last_run): slot 0
// latches, idle keeps the latch, another slot replaces it, and a reconnect
// keeps it.
func TestLastRunMacroLatch(t *testing.T) {
	s, sw := dialed(t, 150*time.Millisecond)
	if s.State().Macros.LastRun != -1 {
		t.Fatal("latched before any macro ran")
	}
	mrpr := func(running bool, index uint16) {
		var f byte
		if running {
			f = 1
		}
		sw.reliable(cmdBytes("MRPr", []byte{f, 0, byte(index >> 8), byte(index)}))
	}
	mrpr(true, 0)
	waitState(t, s, func(st State) bool { return st.Macros.LastRun == 0 })
	mrpr(false, 0xFFFF)
	waitState(t, s, func(st State) bool { return st.Macros.PlayStatus != nil && st.Macros.PlayStatus.Index == 0xFFFF })
	if s.State().Macros.LastRun != 0 {
		t.Fatal("idle cleared the latch")
	}
	mrpr(true, 4)
	waitState(t, s, func(st State) bool { return st.Macros.LastRun == 4 })
	// Go silent: the disconnect empties the state but not the latch.
	sw.expect("SYN after silence", isSYN(helloConnect))
	waitState(t, s, func(st State) bool { return st.Macros.PlayStatus == nil })
	if s.State().Macros.LastRun != 4 {
		t.Fatal("the reconnect cleared the latch")
	}
}

func TestClose(t *testing.T) {
	s, sw := dialed(t, 0)
	ch, _ := s.Subscribe()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	sw.expect("goodbye", isSYN(helloClosing))
	if s.Connected() {
		t.Fatal("connected after Close")
	}
	if closedWithin(s.Ready(), 0) {
		t.Fatal("ready after Close")
	}
	if err := s.Send(testCmd{op: "CTst"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close: %v", err)
	}
	if len(s.State().Raw) != 0 {
		t.Fatal("state after Close")
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscriber open after Close")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// A non-transient send error ends the session, as it kills atemwire's UDP
// thread and with it the worker.
func TestFatalSendErrorEndsTheSession(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.c.mu.Lock()
	sw.c.writeErr = &net.OpError{Op: "write", Net: "udp", Err: net.ErrClosed}
	sw.c.mu.Unlock()
	_ = s.Send(testCmd{op: "CTst"})
	deadline := time.Now().Add(2 * time.Second)
	for s.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("still connected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	err := s.Send(testCmd{op: "CTst"})
	if !errors.Is(err, ErrClosed) || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Send: %v", err)
	}
}

// logRecords is a slog.Handler keeping each record's level and message.
type logRecords struct {
	mu   sync.Mutex
	recs []logRecord
}

type logRecord struct {
	level slog.Level
	msg   string
}

func (l *logRecords) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecords) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logRecords) WithGroup(string) slog.Handler            { return l }
func (l *logRecords) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append(l.recs, logRecord{r.Level, r.Message})
	return nil
}

// matching is the records whose message contains sub.
func (l *logRecords) matching(sub string) []logRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []logRecord
	for _, r := range l.recs {
		if strings.Contains(r.msg, sub) {
			out = append(out, r)
		}
	}
	return out
}

// dialedLogging is dialed with a logger, and a state dump of cmds (with a
// video mode and InCm).
func dialedLogging(t *testing.T, cmds ...[]byte) (*Session, *switcher, *logRecords) {
	t.Helper()
	logs := &logRecords{}
	c := newFakeConn()
	res := make(chan dialResult, 1)
	go func() {
		d := &Dialer{Conn: c, Logger: slog.New(logs)}
		s, err := d.Dial(context.Background(), "127.0.0.1")
		res <- dialResult{s, err}
	}()
	sw := newSwitcher(t, c)
	sw.handshake()
	sw.reliable(append(append([][]byte{cmdBytes("_ver", []byte{0, 2, 0, 30}), cmdBytes("VidM", []byte{12, 0, 0, 0})}, cmds...),
		cmdBytes("InCm", []byte{1, 0, 0, 0}))...)
	sw.ping()
	r := <-res
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { r.s.Close() })
	return r.s, sw, logs
}

// test_audio_legacy.py::test_first_of_each_legacy_packet_is_logged_raw_and_decoded:
// the first AMIP per source and the first AMMO, AMmO and AMLv, at Info
// (bmdwire#49, 45fafa4).
func TestFirstOfEachLegacyPacketIsLogged(t *testing.T) {
	s, sw, logs := dialedLogging(t)
	ammo := []byte{0x80, 0, 0, 0, 1, 0, 0, 0}
	amip1 := amip(1, 0, 1, 2, 16384, -5000, 0)
	sw.reliable(
		cmdBytes("AMMO", ammo),
		cmdBytes("AMMO", make([]byte, 8)),
		cmdBytes("AMIP", amip1),
		cmdBytes("AMIP", amip(1, 0, 1, 1, 0, 0, 0)),     // the same source
		cmdBytes("AMIP", amip(2, 0, 1, 1, 32768, 0, 4)), // a new one
		cmdBytes("AMmO", make([]byte, 12)),
		cmdBytes("AMLv", amlv([4]uint32{classicUnity, classicUnity, classicUnity, classicUnity}, [4]uint32{}, []uint16{1},
			[][4]uint32{{classicUnity, classicUnity, classicUnity, classicUnity}})),
		cmdBytes("AMLv", amlv([4]uint32{}, [4]uint32{}, nil, nil)),
	)
	waitState(t, s, func(st State) bool { return st.Audio.Levels != nil && st.Audio.Levels.Count == 0 })
	var lines []string
	for _, r := range logs.matching("legacy-audio:") {
		if r.level != slog.LevelInfo {
			t.Errorf("%s at %s", r.msg, r.level)
		}
		if !strings.Contains(r.msg, "mixer at connect") {
			lines = append(lines, r.msg)
		}
	}
	var heads []string
	for _, l := range lines {
		heads = append(heads, strings.Split(l, ",")[0])
	}
	if want := []string{"atem: legacy-audio: AMMO", "atem: legacy-audio: AMIP source 1", "atem: legacy-audio: AMIP source 2",
		"atem: legacy-audio: AMmO", "atem: legacy-audio: AMLv"}; !slices.Equal(heads, want) {
		t.Fatalf("logged %q", lines)
	}
	if want := "atem: legacy-audio: AMMO, 8 bytes: " + hex.EncodeToString(ammo) + " -> volume=32768 afv=True volume_db=0.0"; lines[0] != want {
		t.Errorf("AMMO:\n got %s\nwant %s", lines[0], want)
	}
	if want := "atem: legacy-audio: AMIP source 1, 16 bytes: " + hex.EncodeToString(amip1) +
		" -> index=1 type=0 number=0 plug=1 state=2 volume=16384 balance=-5000 volume_db=-6.02 plug_name=SDI"; lines[1] != want {
		t.Errorf("AMIP:\n got %s\nwant %s", lines[1], want)
	}
	if !strings.HasPrefix(lines[2], "atem: legacy-audio: AMIP source 2, 20 bytes: ") {
		t.Errorf("AMIP before 8.0: %s", lines[2])
	}
	if !strings.Contains(lines[4], "master=(0.0, 0.0, 0.0, 0.0)") || !strings.Contains(lines[4], "input={1: (0.0, 0.0, 0.0, 0.0)}") {
		t.Errorf("AMLv: %s", lines[4])
	}
}

// test_audio_legacy.py::test_the_mixer_is_logged_at_connect: Info for a
// legacy mixer or none, Debug for Fairlight, which logs nothing at Info.
// (test_a_failing_description_never_costs_the_packet has no counterpart:
// formatting the line here can't fail.)
func TestTheMixerIsLoggedAtConnect(t *testing.T) {
	for _, c := range []struct {
		name  string
		dump  [][]byte
		level slog.Level
		line  string
	}{
		{"legacy", [][]byte{cmdBytes("AMMO", []byte{0x80, 0, 0, 0, 0, 0, 0, 0})}, slog.LevelInfo, "atem: legacy-audio: mixer at connect: legacy"},
		{"none", nil, slog.LevelInfo, "atem: legacy-audio: mixer at connect: unknown (the state dump had neither FAMP nor AMMO)"},
		{"fairlight", [][]byte{cmdBytes("FAMP", make([]byte, 20))}, slog.LevelDebug, "atem: audio mixer at connect: fairlight"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, logs := dialedLogging(t, c.dump...)
			if got := logs.matching("mixer at connect"); !slices.Equal(got, []logRecord{{c.level, c.line}}) {
				t.Errorf("logged %+v", got)
			}
			if c.level == slog.LevelDebug {
				if got := logs.matching("legacy-audio:"); len(got) != 0 {
					t.Errorf("a Fairlight switcher logged %+v", got)
				}
			}
		})
	}
}
