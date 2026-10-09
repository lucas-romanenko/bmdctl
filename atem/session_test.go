// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
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
	t       *testing.T
	c       *fakeConn
	read    int    // writes consumed so far
	seq     uint16 // last reliable sequence sent
	session uint16
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
	for _, code := range []string{"_ver", "VidM", "PrgI", "InCm"} {
		if _, ok := st.Raw[code]; !ok {
			t.Errorf("%s not in state", code)
		}
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
		c, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", payload: []byte{0, 0, 0, 5}, echo: "PrgI"})
		change = c
		done <- dialResult{err: err}
	}()
	sw.expect("CPgI", carries("CPgI"))
	sw.reliable(cmdBytes("PrgI", []byte{0, 0, 0, 5}))
	if r := <-done; r.err != nil {
		t.Fatal(r.err)
	}
	if change.Kind != ChangeState || change.Key != "PrgI" || !bytes.Equal(change.Value.([]byte), []byte{0, 0, 0, 5}) {
		t.Fatalf("change %+v", change)
	}
}

func TestSendAndWaitErrors(t *testing.T) {
	s, sw := dialed(t, 0)
	if _, err := s.SendAndWait(context.Background(), testCmd{op: "DCut"}); !errors.Is(err, ErrNoEcho) {
		t.Fatalf("no echo: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.SendAndWait(ctx, testCmd{op: "CPgI", echo: "PrgI"}); !errors.Is(err, context.DeadlineExceeded) {
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
		if c.Kind != ChangeState || c.Opcode != "PrvI" || c.Key != "PrvI" {
			t.Fatalf("change %+v", c)
		}
		c.Value.([]byte)[3] = 99 // the copy is ours to change
		if s.State().Raw["PrvI"][3] != 2 {
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
		_, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", echo: "PrgI"})
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
	if len(s.State().Raw) != 0 {
		t.Fatal("state not emptied")
	}
	if _, err := s.SendAndWait(context.Background(), testCmd{op: "CPgI", echo: "PrgI"}); !errors.Is(err, ErrNotReady) {
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
	if _, ok := s.State().Raw["PrgI"]; !ok {
		t.Fatal("state not refilled")
	}
}

// A zero-length command header is atemwire's ConnectionError: disconnect,
// goodbye, a fresh handshake.
func TestCorruptionForcesAReconnect(t *testing.T) {
	s, sw := dialed(t, 0)
	ch, _ := s.Subscribe()
	sw.reliable(cmdBytes("PrvI", []byte{0, 0, 0, 2}), make([]byte, 8))
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

// A truncated command header loses the rest of its packet only.
func TestTruncatedPacketKeepsWhatCameBefore(t *testing.T) {
	s, sw := dialed(t, 0)
	sw.reliable(cmdBytes("PrvI", []byte{0, 0, 0, 2}), []byte{0, 12, 0})
	sw.reliable(cmdBytes("AuxS", []byte{0, 0, 0, 3}))
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := s.State()
		if _, ok := st.Raw["AuxS"]; ok {
			if _, ok := st.Raw["PrvI"]; !ok {
				t.Fatal("the field before the truncation was lost")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the next packet was not processed")
		}
		time.Sleep(5 * time.Millisecond)
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
