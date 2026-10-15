// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"encoding/binary"
	"net"
	"sync"
	"time"
)

// fakeConn is a net.PacketConn for tests: it records every write and
// serves reads from a channel the test feeds.
type fakeConn struct {
	mu       sync.Mutex
	writes   []fakeWrite
	writeErr error
	// refuse is how many writes, from now, fail with refuseErr before
	// writes work again (a kernel refusing for a moment).
	refuse    int
	refuseErr error
	wrote     chan struct{} // signalled after every write

	in        chan fakeDatagram
	done      chan struct{}
	closeOnce sync.Once
}

type fakeWrite struct {
	b  []byte
	to net.Addr
	at time.Time
}

type fakeDatagram struct {
	b    []byte
	from net.Addr
}

var (
	switcherAddr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: DefaultPort}
	foreignAddr  = &net.UDPAddr{IP: net.IPv4(10, 0, 0, 66), Port: DefaultPort}
)

func newFakeConn() *fakeConn {
	return &fakeConn{
		wrote: make(chan struct{}, 1024),
		in:    make(chan fakeDatagram, 1024),
		done:  make(chan struct{}),
	}
}

func (c *fakeConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case d := <-c.in:
		return copy(p, d.b), d.from, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *fakeConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.refuse > 0 {
		c.refuse--
		return 0, c.refuseErr
	}
	c.writes = append(c.writes, fakeWrite{append([]byte(nil), p...), addr, time.Now()})
	select {
	case c.wrote <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (c *fakeConn) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return nil
}

func (c *fakeConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}
}
func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }

// sent parses every datagram written so far.
func (c *fakeConn) sent() []packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]packet, 0, len(c.writes))
	for _, w := range c.writes {
		p, err := parsePacket(w.b)
		if err != nil {
			panic(err)
		}
		out = append(out, p)
	}
	return out
}

func (c *fakeConn) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = nil
}

// rawDatagram builds a switcher datagram the way the Python tests' _raw and
// make_request_raw helpers do.
func rawDatagram(session, seq uint16, flags uint8, data []byte) []byte {
	b := make([]byte, headerLen+len(data))
	binary.BigEndian.PutUint16(b[0:], uint16(flags)<<11|uint16(len(b)))
	binary.BigEndian.PutUint16(b[2:], session)
	binary.BigEndian.PutUint16(b[10:], seq)
	copy(b[headerLen:], data)
	return b
}
