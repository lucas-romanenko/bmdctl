// SPDX-License-Identifier: MIT

package mdns

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// conn is a net.PacketConn with responders behind it instead of a
// network: each query written is handed to answer, and what it returns
// is read back, from the responder's address.
type conn struct {
	mu       sync.Mutex
	queries  [][]byte
	in       chan datagram
	deadline time.Time
	answer   func(q []byte) []datagram
}

type datagram struct {
	b    []byte
	from net.Addr
}

func newConn(answer func(q []byte) []datagram) *conn {
	return &conn{in: make(chan datagram, 64), answer: answer}
}

func (c *conn) WriteTo(b []byte, _ net.Addr) (int, error) {
	c.mu.Lock()
	c.queries = append(c.queries, append([]byte(nil), b...))
	c.mu.Unlock()
	for _, d := range c.answer(b) {
		c.in <- d
	}
	return len(b), nil
}

func (c *conn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.mu.Lock()
	d := c.deadline
	c.mu.Unlock()
	select {
	case p := <-c.in:
		return copy(b, p.b), p.from, nil
	case <-time.After(time.Until(d)):
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}
func (c *conn) Close() error                     { return nil }
func (c *conn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *conn) SetDeadline(t time.Time) error    { return c.SetReadDeadline(t) }
func (c *conn) SetWriteDeadline(time.Time) error { return nil }

// msg builds a DNS response. Names are written whole the first time and
// as a pointer to that place after, as responders compress them.
type msg struct {
	b     []byte
	names map[string]int
	n     int
}

func newMsg() *msg {
	m := &msg{b: make([]byte, 12), names: map[string]int{}}
	m.b[2] = 0x84 // a response, authoritative
	return m
}

func (m *msg) name(labels ...string) {
	for i := range labels {
		k := strings.Join(labels[i:], "\x00")
		if off, ok := m.names[k]; ok {
			m.b = binary.BigEndian.AppendUint16(m.b, 0xC000|uint16(off))
			return
		}
		m.names[k] = len(m.b)
		m.b = append(m.b, byte(len(labels[i])))
		m.b = append(m.b, labels[i]...)
	}
	m.b = append(m.b, 0)
}

// rr appends a record; data writes its data.
func (m *msg) rr(name []string, typ uint16, data func()) *msg {
	m.name(name...)
	m.b = binary.BigEndian.AppendUint16(m.b, typ)
	m.b = binary.BigEndian.AppendUint16(m.b, 0x8001) // cache flush, IN
	m.b = binary.BigEndian.AppendUint32(m.b, 120)
	at := len(m.b)
	m.b = append(m.b, 0, 0)
	data()
	binary.BigEndian.PutUint16(m.b[at:], uint16(len(m.b)-at-2))
	m.n++
	binary.BigEndian.PutUint16(m.b[6:], uint16(m.n)) // all as answers
	return m
}

var svc = []string{"_switcher_ctrl", "_udp", "local"}

func ptr(m *msg, inst string) *msg {
	return m.rr(svc, typePTR, func() { m.name(append([]string{inst}, svc...)...) })
}

func srvRR(m *msg, inst, host string, port uint16) *msg {
	return m.rr(append([]string{inst}, svc...), typeSRV, func() {
		m.b = append(m.b, 0, 0, 0, 0)
		m.b = binary.BigEndian.AppendUint16(m.b, port)
		m.name(host, "local")
	})
}

func txt(m *msg, inst string, kvs ...string) *msg {
	return m.rr(append([]string{inst}, svc...), typeTXT, func() {
		for _, s := range kvs {
			m.b = append(m.b, byte(len(s)))
			m.b = append(m.b, s...)
		}
	})
}

func a(m *msg, host string, ip net.IP) *msg {
	return m.rr([]string{host, "local"}, typeA, func() { m.b = append(m.b, ip.To4()...) })
}

func from(ip string) net.Addr { return &net.UDPAddr{IP: net.ParseIP(ip), Port: 5353} }

// questions reads a query's questions as "name type".
func questions(q []byte) []string {
	var out []string
	off := 12
	for range binary.BigEndian.Uint16(q[4:]) {
		name, n, ok := readName(q, off)
		if !ok {
			return append(out, "bad")
		}
		out = append(out, strings.Join(name, ".")+" "+map[uint16]string{1: "A", 12: "PTR", 16: "TXT", 33: "SRV"}[binary.BigEndian.Uint16(q[n:])])
		off = n + 4
	}
	return out
}

func TestBrowse(t *testing.T) {
	// One switcher answers the PTR with everything (its name has a dot
	// in it); another answers the PTR alone and the rest when asked; a
	// third never gives an address; noise and broken messages besides.
	var mu sync.Mutex
	asked := map[string]bool{}
	c := newConn(func(q []byte) []datagram {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range questions(q) {
			asked[s] = true
		}
		if binary.BigEndian.Uint16(q[0:]) == 1 {
			full := newMsg()
			ptr(full, "Studio 1.1")
			srvRR(full, "Studio 1.1", "atem-1", 9910)
			txt(full, "Studio 1.1", "unique id=7c2e0d", "name=ATEM Constellation HD", "flag")
			a(full, "atem-1", net.IPv4(192, 0, 2, 10))
			loop := newMsg()
			loop.b = append(loop.b, 0xC0, 12) // a name pointing at itself
			binary.BigEndian.PutUint16(loop.b[6:], 1)
			query := append([]byte(nil), q...) // another client's query
			return []datagram{
				{full.b, from("192.0.2.10")},
				{ptr(newMsg(), "Mini").b, from("192.0.2.11")},
				{ptr(newMsg(), "Quiet").b, from("192.0.2.12")},
				{loop.b, from("192.0.2.13")},
				{full.b[:30], from("192.0.2.14")},
				{query, from("192.0.2.15")},
			}
		}
		m := newMsg()
		srvRR(m, "Mini", "mini", 9910)
		txt(m, "Mini", "unique id=99")
		a(m, "mini", net.IPv4(192, 0, 2, 11))
		return []datagram{{m.b, from("192.0.2.11")}}
	})
	got, err := Browse(context.Background(), c, Group, "_switcher_ctrl._udp.local.", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	want := []Instance{
		{Name: "Studio 1.1", Host: "atem-1.local", Port: 9910, Addrs: []net.IP{net.IPv4(192, 0, 2, 10)},
			TXT: map[string]string{"unique id": "7c2e0d", "name": "ATEM Constellation HD", "flag": ""}},
		{Name: "Mini", Host: "mini.local", Port: 9910, Addrs: []net.IP{net.IPv4(192, 0, 2, 11)}, TXT: map[string]string{"unique id": "99"}},
		{Name: "Quiet", Addrs: []net.IP{net.ParseIP("192.0.2.12")}, TXT: map[string]string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// The first query asks for the instances; the second for what they
	// left out, once.
	if len(c.queries) != 2 || !reflect.DeepEqual(questions(c.queries[0]), []string{"_switcher_ctrl._udp.local PTR"}) {
		t.Fatalf("queries %q", c.queries)
	}
	if q := questions(c.queries[1]); !reflect.DeepEqual(q, []string{"Mini._switcher_ctrl._udp.local SRV", "Mini._switcher_ctrl._udp.local TXT",
		"Quiet._switcher_ctrl._udp.local SRV", "Quiet._switcher_ctrl._udp.local TXT"}) {
		t.Errorf("second query %q", q)
	}
}

// Nothing answers: no instances, after the wait or the context's end,
// whichever is first.
func TestBrowseNothing(t *testing.T) {
	c := newConn(func([]byte) []datagram { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := Browse(ctx, c, Group, "_switcher_ctrl._udp.local", 5*time.Second)
	if err != nil || len(got) != 0 || time.Since(start) > time.Second {
		t.Errorf("%v %v after %v", got, err, time.Since(start))
	}
}
