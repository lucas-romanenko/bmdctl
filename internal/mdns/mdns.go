// SPDX-License-Identifier: MIT

// Package mdns browses a DNS-SD service over multicast DNS (RFC 6762,
// RFC 6763): it asks once for the service's instances, collects what
// answers for a while, and returns each instance's own name, address,
// port and TXT record. bmdctl uses it to find ATEM switchers, which
// announce themselves as _switcher_ctrl._udp with the name set in ATEM
// Setup as the instance name, the way ATEM Software Control finds them.
// The standard library has no mDNS; this is the least of it a browse
// needs.
package mdns

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"time"
)

// Group is mDNS's IPv4 multicast group and port.
var Group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Instance is one service instance that answered.
type Instance struct {
	Name  string            // its own name: the first label of its instance name
	Host  string            // the SRV target, dotted
	Port  int               // from SRV
	Addrs []net.IP          // the target's IPv4 addresses, else the responder's
	TXT   map[string]string // its TXT record's key=value strings
}

// DNS record types and the class used.
const (
	typeA    = 1
	typePTR  = 12
	typeTXT  = 16
	typeSRV  = 33
	classIN  = 1
	maxNames = 128 // compression pointers followed in one name, at most
)

// Browse asks for service's instances ("_switcher_ctrl._udp.local") on
// conn, sending to dst (Group on a network), and collects answers until
// wait passes or ctx ends. The query comes from conn's own port, so a
// responder answers that port directly (RFC 6762 §6.7, a "legacy
// unicast" query); conn needn't be bound to 5353, which a system
// responder may hold. Halfway through, instances missing their SRV, TXT
// or address are asked for once more.
func Browse(ctx context.Context, conn net.PacketConn, dst net.Addr, service string, wait time.Duration) ([]Instance, error) {
	svc := labels(service)
	if _, err := conn.WriteTo(query(1, question{svc, typePTR}), dst); err != nil {
		return nil, err
	}
	end := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(end) {
		end = d
	}
	again := time.Now().Add(wait / 2)
	r := newRecords()
	buf := make([]byte, 9000)
	for asked := false; ; {
		if ctx.Err() != nil {
			break
		}
		next := end
		if !asked && again.Before(end) {
			next = again
		}
		if time.Until(next) <= 0 {
			if next == end {
				break
			}
			asked = true
			if qs := r.missing(svc); len(qs) > 0 {
				if _, err := conn.WriteTo(query(2, qs...), dst); err != nil {
					return nil, err
				}
			}
			continue
		}
		conn.SetReadDeadline(next)
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return nil, err
		}
		r.add(buf[:n], from)
	}
	return r.instances(svc), nil
}

// question is one question of a query.
type question struct {
	name []string
	typ  uint16
}

// query is a DNS query message asking qs.
func query(id uint16, qs ...question) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[4:], uint16(len(qs)))
	for _, q := range qs {
		b = appendName(b, q.name)
		b = binary.BigEndian.AppendUint16(b, q.typ)
		b = binary.BigEndian.AppendUint16(b, classIN)
	}
	return b
}

func appendName(b []byte, name []string) []byte {
	for _, l := range name {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

// labels splits a dotted name into its labels; "local" ends it whether
// or not a final dot is given.
func labels(name string) []string {
	return strings.Split(strings.TrimSuffix(name, "."), ".")
}

// key is a name compared as DNS compares names: label by label, ASCII
// letters without regard to case.
func key(name []string) string {
	var b strings.Builder
	for _, l := range name {
		b.WriteString(strings.ToLower(l))
		b.WriteByte(0)
	}
	return b.String()
}

// records is what has answered so far.
type records struct {
	ptrs  map[string][]string // the service's key: its instances' names
	srv   map[string]srv      // instance key
	txt   map[string]map[string]string
	addrs map[string][]net.IP // host key
	from  map[string]net.IP   // instance key: who answered for it
}

type srv struct {
	port   int
	target []string
}

func newRecords() *records {
	return &records{ptrs: map[string][]string{}, srv: map[string]srv{}, txt: map[string]map[string]string{},
		addrs: map[string][]net.IP{}, from: map[string]net.IP{}}
}

// add reads one message's records. A message that doesn't parse adds
// what it had before the fault.
func (r *records) add(msg []byte, from net.Addr) {
	var ip net.IP
	if u, ok := from.(*net.UDPAddr); ok {
		ip = u.IP
	}
	if len(msg) < 12 || msg[2]&0x80 == 0 {
		return // not a response
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	rrs := int(binary.BigEndian.Uint16(msg[6:])) + int(binary.BigEndian.Uint16(msg[8:])) + int(binary.BigEndian.Uint16(msg[10:]))
	off := 12
	for range qd {
		_, n, ok := readName(msg, off)
		if !ok || n+4 > len(msg) {
			return
		}
		off = n + 4
	}
	for range rrs {
		name, n, ok := readName(msg, off)
		if !ok || n+10 > len(msg) {
			return
		}
		typ := binary.BigEndian.Uint16(msg[n:])
		size := int(binary.BigEndian.Uint16(msg[n+8:]))
		data := n + 10
		if data+size > len(msg) {
			return
		}
		off = data + size
		rd := msg[data:off]
		k := key(name)
		switch typ {
		case typePTR:
			inst, _, ok := readName(msg, data)
			if !ok {
				continue
			}
			ik := key(inst)
			if _, seen := r.from[ik]; !seen {
				r.ptrs[k] = append(r.ptrs[k], strings.Join(inst, "\x00"))
				r.from[ik] = ip
			}
		case typeSRV:
			if len(rd) < 7 {
				continue
			}
			target, _, ok := readName(msg, data+6)
			if !ok {
				continue
			}
			r.srv[k] = srv{port: int(binary.BigEndian.Uint16(rd[4:])), target: target}
		case typeTXT:
			kv := map[string]string{}
			for i := 0; i < len(rd); {
				l := int(rd[i])
				if i+1+l > len(rd) {
					break
				}
				s := string(rd[i+1 : i+1+l])
				if k, v, ok := strings.Cut(s, "="); ok {
					kv[strings.ToLower(k)] = v
				} else if s != "" {
					kv[strings.ToLower(s)] = ""
				}
				i += 1 + l
			}
			r.txt[k] = kv
		case typeA:
			if len(rd) == 4 {
				a := net.IPv4(rd[0], rd[1], rd[2], rd[3])
				if !containsIP(r.addrs[k], a) {
					r.addrs[k] = append(r.addrs[k], a)
				}
			}
		}
	}
}

func containsIP(ips []net.IP, ip net.IP) bool {
	for _, x := range ips {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

// readName reads the name at off, following compression pointers, and
// returns its labels and the offset after it where it was written.
func readName(msg []byte, off int) ([]string, int, bool) {
	var out []string
	end := -1
	for jumps := 0; ; {
		if off >= len(msg) {
			return nil, 0, false
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return out, end, true
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) || jumps >= maxNames {
				return nil, 0, false
			}
			if end < 0 {
				end = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
			jumps++
		case l&0xC0 != 0:
			return nil, 0, false
		default:
			if off+1+l > len(msg) {
				return nil, 0, false
			}
			out = append(out, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

// missing are the questions that would complete what answered: each
// instance's SRV and TXT, each target's address.
func (r *records) missing(svc []string) []question {
	var qs []question
	for _, joined := range r.ptrs[key(svc)] {
		inst := strings.Split(joined, "\x00")
		s, ok := r.srv[key(inst)]
		if !ok {
			qs = append(qs, question{inst, typeSRV})
		}
		if _, ok := r.txt[key(inst)]; !ok {
			qs = append(qs, question{inst, typeTXT})
		}
		if ok && len(r.addrs[key(s.target)]) == 0 {
			qs = append(qs, question{s.target, typeA})
		}
	}
	return qs
}

// instances assembles each instance of svc, in the order they answered.
func (r *records) instances(svc []string) []Instance {
	var out []Instance
	for _, joined := range r.ptrs[key(svc)] {
		inst := strings.Split(joined, "\x00")
		k := key(inst)
		in := Instance{Name: inst[0], TXT: r.txt[k]}
		if in.TXT == nil {
			in.TXT = map[string]string{}
		}
		if s, ok := r.srv[k]; ok {
			in.Host, in.Port = strings.Join(s.target, "."), s.port
			in.Addrs = r.addrs[key(s.target)]
		}
		if len(in.Addrs) == 0 && r.from[k] != nil {
			in.Addrs = []net.IP{r.from[k]}
		}
		out = append(out, in)
	}
	return out
}
