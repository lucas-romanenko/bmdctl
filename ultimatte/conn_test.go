// SPDX-License-Identifier: MIT

package ultimatte

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// timeout in a script is a read that times out: socket.timeout() in
// ultimattewire's FakeSocket.
type timeout struct{}

// script is the unit's end of one connection, as ultimattewire's tests'
// FakeSocket is: each read gets the next chunk (or as much of it as fits),
// a timeout in the script is a read timing out, and a script run dry is
// the unit closing. It keeps what was written and each read's timeout.
type script struct {
	mu       sync.Mutex
	chunks   []any // []byte, or timeout
	sent     bytes.Buffer
	closed   bool
	timeouts []time.Duration
	deadline time.Time
}

func newScript(chunks ...any) *script { return &script{chunks: chunks} }

func (s *script) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, net.ErrClosed
	}
	for len(s.chunks) > 0 {
		switch c := s.chunks[0].(type) {
		case timeout:
			s.chunks = s.chunks[1:]
			return 0, os.ErrDeadlineExceeded
		case []byte:
			if len(c) == 0 {
				s.chunks = s.chunks[1:]
				continue
			}
			n := copy(p, c)
			if s.chunks[0] = c[n:]; len(c[n:]) == 0 {
				s.chunks = s.chunks[1:]
			}
			return n, nil
		}
	}
	return 0, io.EOF
}

func (s *script) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent.Write(p)
	return len(p), nil
}

func (s *script) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *script) isClosed() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }

func (s *script) written() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.sent.Bytes())
}

func (s *script) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.timeouts = append(s.timeouts, time.Until(t).Round(10*time.Millisecond))
	s.mu.Unlock()
	return nil
}

func (s *script) SetDeadline(t time.Time) error      { return nil }
func (s *script) SetWriteDeadline(t time.Time) error { return nil }
func (s *script) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (s *script) RemoteAddr() net.Addr               { return &net.TCPAddr{} }

// dials is a Client's DialContext: each connect takes the next of
// conns (an error is that connect failing), and the addresses are kept.
type dials struct {
	mu    sync.Mutex
	conns []any // *script, or error
	addrs []string
}

func (d *dials) dial(_ context.Context, _, addr string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addrs = append(d.addrs, addr)
	if len(d.conns) == 0 {
		return nil, errors.New("nothing more to dial")
	}
	c := d.conns[0]
	d.conns = d.conns[1:]
	if err, ok := c.(error); ok {
		return nil, err
	}
	return c.(*script), nil
}

// client is a Client on scripted connections, with no wait before a
// retry and a clock that moves only when set_network sleeps.
func client(conns ...any) (*Client, *dials) {
	d := &dials{conns: conns}
	c := &Client{DialContext: d.dial, RetryDelay: time.Nanosecond}
	t := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return t }
	c.sleep = func(_ context.Context, dt time.Duration) error { t = t.Add(dt); return nil }
	return c, d
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
