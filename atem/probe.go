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

// Probe sends the SYN Dial sends (hello 0x01 under session 0x1337) and
// waits for the switcher's SYN reply, without answering it: a handshake
// left half done, which the switcher forgets, opens no session. It is
// bmdctl's, for telling what device is at an address (docs/PORTING.md,
// "Device probe"); atemwire's probe() connects fully. Without a context
// deadline it waits ConnectTimeout. A Dialer's Conn is used, and closed.
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
	found := make(chan error, 1)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				found <- err
				return
			}
			ua, ok := from.(*net.UDPAddr)
			if !ok || !ua.IP.Equal(raddr.IP) {
				continue
			}
			p, err := parsePacket(buf[:n])
			if err != nil || p.flags&flagSYN == 0 || p.session != helloSession || len(p.data) == 0 {
				continue
			}
			found <- nil
			return
		}
	}()
	select {
	case err := <-found:
		if err != nil {
			return fmt.Errorf("atem: probe %s: %w", addr, err)
		}
		return nil
	case <-ctx.Done():
		conn.Close() // ends the read
		return fmt.Errorf("%w at %s: %w", ErrNoSwitcher, addr, ctx.Err())
	}
}
