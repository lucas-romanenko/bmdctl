// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// ErrNoSwitcher: nothing answered the handshake.
var ErrNoSwitcher = errors.New("atem: no switcher answered")

// Probe reports whether a switcher answers at addr ("host" or
// "host:port", UDP 9910 by default) with the default Dialer.
func Probe(ctx context.Context, addr string) error { return (&Dialer{}).Probe(ctx, addr) }

// Probe tells whether a switcher answers at addr, and leaves nothing
// open on it: it sends the SYN Dial sends (hello 0x01 under session
// 0x1337); on the switcher's acceptance (hello 0x02) it completes the
// handshake with the ACK Dial sends, takes the session id from the
// switcher's first packet, and says goodbye under it (SYN, hello 0x04),
// as Close does. A half-open handshake would hold one of the switcher's
// few client slots until it timed out. Another answer (a refusal) opens
// nothing. It is bmdctl's, for telling what device is at an address
// (docs/PORTING.md, "Device probe"); atemwire's probe() connects fully.
// Without a context deadline it waits ConnectTimeout. A Dialer's Conn is
// used, and closed.
func (d *Dialer) Probe(ctx context.Context, addr string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ConnectTimeout)
		defer cancel()
	}
	raddr, err := resolve(ctx, addr)
	if err != nil {
		return err
	}
	conn := d.Conn
	if conn == nil {
		uc, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return fmt.Errorf("atem: probe %s: %w", addr, err)
		}
		conn = uc
	}
	defer conn.Close()
	syn := &packet{flags: flagSYN, session: helloSession, seq: 1,
		data: []byte{helloConnect, 0, 0, 0, 0, 0, 0, 0}}
	if _, err := conn.WriteTo(syn.marshal(), raddr); err != nil {
		return fmt.Errorf("atem: probe %s: %w", addr, err)
	}
	type reply struct {
		p   packet
		err error
	}
	replies := make(chan reply, 16)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				select {
				case replies <- reply{err: err}:
				default: // nobody waits any more
				}
				return
			}
			ua, ok := from.(*net.UDPAddr)
			if !ok || !ua.IP.Equal(raddr.IP) {
				continue
			}
			if p, err := parsePacket(buf[:n]); err == nil {
				select {
				case replies <- reply{p: p}:
				default: // the dump's packets: one is all it takes
				}
			}
		}
	}()
	next := func() (packet, error) {
		select {
		case r := <-replies:
			return r.p, r.err
		case <-ctx.Done():
			conn.Close() // ends the read
			return packet{}, fmt.Errorf("%w at %s: %w", ErrNoSwitcher, addr, ctx.Err())
		}
	}
	// The switcher's answer to the SYN.
	for {
		p, err := next()
		if err != nil {
			if errors.Is(err, ErrNoSwitcher) {
				return err
			}
			return fmt.Errorf("atem: probe %s: %w", addr, err)
		}
		if p.flags&flagSYN == 0 || p.session != helloSession || len(p.data) == 0 {
			continue
		}
		if p.data[0] != helloAccepted {
			return nil // a switcher, refusing a session: none is open
		}
		break
	}
	// Accepted: complete the handshake, then say goodbye under the id the
	// switcher's first packet carries.
	ack := &packet{flags: flagACK, session: helloSession}
	if _, err := conn.WriteTo(ack.marshal(), raddr); err != nil {
		return fmt.Errorf("atem: probe %s: %w", addr, err)
	}
	for {
		p, err := next()
		if err != nil {
			// A switcher answered; its session will time out.
			return nil
		}
		if p.session == helloSession {
			continue // a resent SYN reply
		}
		bye := &packet{flags: flagSYN, session: p.session, seq: 1, data: []byte{helloClosing, 0, 0, 0, 0, 0, 0, 0}}
		if _, err := conn.WriteTo(bye.marshal(), raddr); err != nil {
			return fmt.Errorf("atem: probe %s: goodbye: %w", addr, err)
		}
		return nil
	}
}
