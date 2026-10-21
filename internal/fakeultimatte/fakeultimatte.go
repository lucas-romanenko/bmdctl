// SPDX-License-Identifier: MIT

// Package fakeultimatte is an Ultimatte 12 keyer for tests: it speaks
// the unit's two TCP channels on any net.Conn, so ultimatte.Client, the
// engine and the CLI run end to end without a unit. Dial hands a client
// one end of a pipe for either port (9998 the text channel, 9996 the
// binary one); Listen serves both on loopback.
//
// On 9998 it sends its prelude (its label, software release, CONTROL
// values and FILE LIST, ending END PRELUDE:), then takes blocks: NETWORK
// INTERFACE n, empty as a query (ACK and the section) or with fields as
// a set (ACK and the fields echoed, or NAK for a value it won't take),
// anything else NAK. An address set makes the interface reapply: the
// next read-back reports Current Addresses 0.0.0.0/255.255.0.0, the one
// after the new address, as the hardware did. On 9996 it answers one
// request per connection: a preset slot's or a resource's bytes, or a
// write's six-zero ACK (none without the frame's trailing zero). It is
// not a keyer model: the presets and resources are opaque bytes, as
// they are to ultimattewire. docs/PORTING.md, "Fake Ultimatte", records
// it.
package fakeultimatte

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Address is what tests dial: TEST-NET-1, so nothing real answers it.
const Address = "192.0.2.41"

// Write is one write the unit took on 9996.
type Write struct {
	Opcode uint16
	Name   string
	Data   []byte
}

// iface is the network interface: current and static.
type iface struct {
	address, netmask, gateway string
	dns                       []string
	dynamic                   bool
}

// Unit is the fake keyer. Its fields are read when a connection opens;
// set them before serving.
type Unit struct {
	Label, Release string

	mu       sync.Mutex
	presets  []string // FILE LIST order
	slots    map[string][]byte
	gpi      []byte
	saved    []byte
	current  iface
	static   iface
	reapply  int // read-backs left that report 0.0.0.0
	writes   []Write
	blocks   []string
	down     bool
	refuseNW bool
}

// New returns an Ultimatte 12 4K labelled "Keyer A" on software 2.1: three
// presets, the two resources, and a static address, 192.0.2.41/22.
func New() *Unit {
	u := &Unit{Label: "Keyer A", Release: "2.1", presets: []string{"Green Screen", "Blue Screen", "Studio B"},
		slots: map[string][]byte{"Green Screen": []byte("green preset blob"), "Blue Screen": []byte("blue preset blob"),
			"Studio B": []byte("studio b preset blob")},
		gpi: []byte("GPI settings blob"), saved: bytes.Repeat([]byte("saved settings "), 20)}
	u.static = iface{address: Address, netmask: "255.255.252.0", gateway: "192.0.2.1", dns: []string{"192.0.2.53"}}
	u.current = u.static
	return u
}

// Dial is an ultimatte.Client's DialContext: a connection to the unit's
// port, over a pipe. Any other port is refused, as is every port while
// the unit is down.
func (u *Unit) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	u.mu.Lock()
	down := u.down
	u.mu.Unlock()
	if down || port != 9998 && port != 9996 {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	client, server := net.Pipe()
	if port == 9998 {
		go u.ServeControl(server)
	} else {
		go u.ServeSettings(server)
	}
	return client, nil
}

// Listen serves the text channel on control and the binary one on
// settings (127.0.0.1:0 for any port) until the listeners are closed.
func (u *Unit) Listen(control, settings string) (net.Listener, net.Listener, error) {
	lc, err := net.Listen("tcp", control)
	if err != nil {
		return nil, nil, err
	}
	ls, err := net.Listen("tcp", settings)
	if err != nil {
		lc.Close()
		return nil, nil, err
	}
	serve := func(l net.Listener, f func(net.Conn)) {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f(c)
		}
	}
	go serve(lc, u.ServeControl)
	go serve(ls, u.ServeSettings)
	return lc, ls, nil
}

// prelude is what 9998 sends on connect. Called with mu.
func (u *Unit) prelude() string {
	var b strings.Builder
	b.WriteString("PROTOCOL PREAMBLE:\nVersion: 2.1\n\n")
	fmt.Fprintf(&b, "Label: %s\nSoftware Release: %s\n\n", u.Label, u.Release)
	b.WriteString("CONTROL:\nMatte Density: 5000\nBlack Gloss: 1200\nShadow Level: 7500\nCursor X: 5000\n" +
		"Transition Rate: 30\nOffset Black Balance: 2500\nFG Saturation Master: 5000\n\n")
	b.WriteString("FILE LIST:\n" + strings.Join(u.presets, "\n") + "\n\n")
	b.WriteString("END PRELUDE:\n")
	return b.String()
}

// ServeControl speaks the text channel on c until it closes.
func (u *Unit) ServeControl(c net.Conn) {
	defer c.Close()
	u.mu.Lock()
	pre := u.prelude()
	u.mu.Unlock()
	if _, err := io.WriteString(c, pre); err != nil {
		return
	}
	r := bufio.NewReader(c)
	var block []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line != "" || len(block) == 0 {
			if line != "" {
				block = append(block, line)
			}
			continue
		}
		// The blank line submits the block.
		reply := u.handle(block)
		block = nil
		if _, err := io.WriteString(c, reply); err != nil {
			return
		}
	}
}

func (u *Unit) handle(block []string) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.blocks = append(u.blocks, strings.Join(block, "\n"))
	header, ok := strings.CutSuffix(block[0], ":")
	n, err := strconv.Atoi(strings.TrimPrefix(header, "NETWORK INTERFACE "))
	if !ok || !strings.HasPrefix(header, "NETWORK INTERFACE ") || err != nil || n != 0 || u.refuseNW {
		return "NAK\n\n"
	}
	if len(block) == 1 {
		return "ACK\n\n" + u.section() + "\n"
	}
	set := u.static
	for _, l := range block[1:] {
		k, v, _ := strings.Cut(l, ":")
		v = strings.TrimSpace(v)
		switch k {
		case "Dynamic IP":
			if v != "true" && v != "false" {
				return "NAK\n\n"
			}
			set.dynamic = v == "true"
		case "Static Addresses":
			ip, mask, ok := strings.Cut(v, "/")
			if !ok || net.ParseIP(ip).To4() == nil || net.ParseIP(mask).To4() == nil {
				return "NAK\n\n"
			}
			set.address, set.netmask = ip, mask
		case "Static Gateway":
			set.gateway = v
		case "Static DNS Servers":
			set.dns = strings.Fields(v)
		default:
			return "NAK\n\n"
		}
	}
	if set.address != u.static.address || set.netmask != u.static.netmask {
		u.reapply = 1
	}
	u.static = set
	if !set.dynamic {
		u.current.gateway, u.current.dns = set.gateway, set.dns
	}
	return "ACK\n\n" + block[0] + "\n" + strings.Join(block[1:], "\n") + "\n\n"
}

// section is NETWORK INTERFACE 0 as a query echoes it. A read after an
// address set reports 0.0.0.0 once while the interface reapplies. Called
// with mu.
func (u *Unit) section() string {
	cur := u.current
	if u.reapply > 0 {
		u.reapply--
		cur.address, cur.netmask = "0.0.0.0", "255.255.0.0"
	} else if !u.static.dynamic {
		u.current.address, u.current.netmask = u.static.address, u.static.netmask
		cur = u.current
	}
	return fmt.Sprintf("NETWORK INTERFACE 0:\nName: BMD 10G Ethernet MAC\nPriority: 0\nMAC Address: 7c:2e:0d:00:00:01\n"+
		"Dynamic IP: %t\nCurrent Addresses: %s/%s\nCurrent Gateway: %s\nCurrent DNS Servers: %s\n"+
		"Static Addresses: %s/%s\nStatic Gateway: %s\nStatic DNS Servers: %s\n",
		u.static.dynamic, cur.address, cur.netmask, cur.gateway, strings.Join(cur.dns, " "),
		u.static.address, u.static.netmask, u.static.gateway, strings.Join(u.static.dns, " "))
}

// ServeSettings answers one binary request on c, then closes it.
func (u *Unit) ServeSettings(c net.Conn) {
	defer c.Close()
	head := make([]byte, 6)
	if _, err := io.ReadFull(c, head); err != nil {
		return
	}
	op, n := binary.BigEndian.Uint16(head), binary.BigEndian.Uint32(head[2:])
	name := make([]byte, n)
	if _, err := io.ReadFull(c, name); err != nil {
		return
	}
	switch op {
	case 0x0000, 0x0003:
		u.mu.Lock()
		blob, ok := u.slots[string(name)], op == 0x0000 && slices.Contains(u.presets, string(name))
		if op == 0x0003 {
			blob, ok = map[string][]byte{"GPISettings": u.gpi, "SavedSettings": u.saved}[string(name)], true
		}
		u.mu.Unlock()
		if !ok {
			return // the unit closes: a short read
		}
		c.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(blob))), blob...))
	case 0x0100, 0x0103:
		size := make([]byte, 4)
		if _, err := io.ReadFull(c, size); err != nil {
			return
		}
		data := make([]byte, binary.BigEndian.Uint32(size)+1)
		if _, err := io.ReadFull(c, data); err != nil || data[len(data)-1] != 0 {
			return // no trailing zero: no ACK
		}
		u.write(op, string(name), data[:len(data)-1])
		c.Write(make([]byte, 6))
	}
}

func (u *Unit) write(op uint16, name string, data []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.writes = append(u.writes, Write{op, name, data})
	last := name[strings.LastIndex(name, "/")+1:]
	if op == 0x0100 {
		_, slot, _ := strings.Cut(name, "/presets/")
		if !slices.Contains(u.presets, slot) {
			u.presets = append(u.presets, slot)
		}
		u.slots[slot] = data
		return
	}
	switch last {
	case "GPISettings":
		u.gpi = data
	case "SavedSettings":
		u.saved = data
	}
}

// Writes are the writes the unit has taken on 9996.
func (u *Unit) Writes() []Write {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.writes)
}

// Blocks are the blocks the unit has been sent on 9998, without their
// blank line.
func (u *Unit) Blocks() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.blocks)
}

// Presets are the unit's preset slots, in FILE LIST order.
func (u *Unit) Presets() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.presets)
}

// Static is the configured address and netmask.
func (u *Unit) Static() (address, netmask string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.static.address, u.static.netmask
}

// Down makes the unit refuse every connection (an address change that
// put it out of reach), or answer again.
func (u *Unit) Down(down bool) { u.mu.Lock(); u.down = down; u.mu.Unlock() }

// RefuseNetwork makes the unit NAK every NETWORK INTERFACE block.
func (u *Unit) RefuseNetwork(on bool) { u.mu.Lock(); u.refuseNW = on; u.mu.Unlock() }
