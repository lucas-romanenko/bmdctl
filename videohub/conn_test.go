// SPDX-License-Identifier: MIT

package videohub

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// scripted is the hub's end of a session for the tests: it gives the
// session the chunks pushed to it (an empty one is the hub closing, as
// an empty recv is in videohubwire's tests), keeps what the session
// writes, and gives the replies armed for a write once it is written,
// since the session reads all the time and an early answer would be an
// answer to nothing.
type scripted struct {
	mu      sync.Mutex
	cond    *sync.Cond
	chunks  [][]byte
	armed   [][]byte
	sent    bytes.Buffer
	closed  bool
	eof     bool
	waiting bool // a Read is waiting, with nothing to give
}

func newScripted(chunks ...[]byte) *scripted {
	c := &scripted{}
	c.cond = sync.NewCond(&c.mu)
	c.push(chunks...)
	return c
}

// push gives the session more of the stream.
func (c *scripted) push(chunks ...[]byte) {
	c.mu.Lock()
	c.chunks = append(c.chunks, chunks...)
	c.cond.Broadcast()
	c.mu.Unlock()
}

// reply arms chunks for the next write.
func (c *scripted) reply(chunks ...[]byte) {
	c.mu.Lock()
	c.armed = append(c.armed, chunks...)
	c.mu.Unlock()
}

// disarm drops a reply no write released: a call refused before it sent
// anything.
func (c *scripted) disarm() {
	c.mu.Lock()
	c.armed = nil
	c.mu.Unlock()
}

func (c *scripted) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.chunks) == 0 && !c.closed && !c.eof {
		c.waiting = true
		c.cond.Broadcast()
		c.cond.Wait()
	}
	c.waiting = false
	switch {
	case c.closed:
		return 0, net.ErrClosed
	case c.eof:
		return 0, io.EOF
	}
	if len(c.chunks[0]) == 0 {
		c.eof = true
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	if c.chunks[0] = c.chunks[0][n:]; len(c.chunks[0]) == 0 {
		c.chunks = c.chunks[1:]
	}
	return n, nil
}

func (c *scripted) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	c.sent.Write(p)
	c.chunks = append(c.chunks, c.armed...)
	c.armed = nil
	c.cond.Broadcast()
	return len(p), nil
}

func (c *scripted) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}

func (c *scripted) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *scripted) written() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent.String()
}

// idle waits until the session has read everything pushed and waits for
// more.
func (c *scripted) idle(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for !(c.waiting && len(c.chunks) == 0) && !c.closed && !c.eof {
		if time.Now().After(deadline) {
			t.Fatal("the session never read what was pushed")
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
		c.mu.Lock()
	}
}

func (c *scripted) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *scripted) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *scripted) SetDeadline(time.Time) error        { return nil }
func (c *scripted) SetReadDeadline(time.Time) error    { return nil }
func (c *scripted) SetWriteDeadline(t time.Time) error { return nil }

// testDialer dials conn, with the tests' short timings: a preamble's
// quiet 20 ms, an answer's wait 200 ms, no wait before the retry.
func testDialer(conn net.Conn) *Dialer {
	return &Dialer{DialContext: func(context.Context, string, string) (net.Conn, error) { return conn, nil },
		quiet: 20 * time.Millisecond, readTimeout: 200 * time.Millisecond, retryDelay: time.Microsecond}
}

// settled waits until the session has taken in everything pushed to c.
func settled(t *testing.T, s *Session, c *scripted) {
	t.Helper()
	c.idle(t)
	s.do(func() {})
}
