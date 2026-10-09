// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import "net"

// SURFACE.md §1.13 hyperdeck, from bmdwire v1.3.0
// atemwire/messages/hyperdeck.py. This is the deck binding only; deck
// transport through the switcher is not implemented in atemwire.

// HyperdeckSettings is CXMS (HyperdeckSettingsCommand, op
// set_hyperdeck_settings): bind a HyperDeck slot (0-9) to an address and a
// switcher input. NetworkAddress is dotted IPv4; "0.0.0.0" clears the
// slot. Bytes 10-15 (auto-roll, frame delay) are sent as 0, as in atemwire.
// Reverse-engineered and live-validated by atemwire.
type HyperdeckSettings struct {
	Slot           uint16
	NetworkAddress *string
	SwitcherInput  *uint16
}

func (c HyperdeckSettings) Opcode() string    { return "CXMS" }
func (c HyperdeckSettings) Encode() []byte    { return c.build().encode() }
func (c HyperdeckSettings) Validate() error   { return c.build().err }
func (c HyperdeckSettings) EchoKey() string   { return key("RXMS", c.Slot) }
func (c HyperdeckSettings) Unverified() bool  { return false }
func (c HyperdeckSettings) Destructive() bool { return false }

func (c HyperdeckSettings) build() *wire {
	w := newWire("CXMS", 16)
	var mask int64
	w.u16(2, "slot", int64(c.Slot))
	if c.NetworkAddress != nil {
		mask |= 0x01
		ip := net.ParseIP(*c.NetworkAddress).To4()
		if ip == nil {
			w.fail("network address %q is not dotted IPv4", *c.NetworkAddress)
		}
		copy(w.b[4:8], ip)
	}
	if c.SwitcherInput != nil {
		mask |= 0x02
		w.u16(8, "switcher_input", int64(*c.SwitcherInput))
	}
	w.u8(0, "mask", mask)
	return w
}
