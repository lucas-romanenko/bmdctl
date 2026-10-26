// SPDX-License-Identifier: MIT

// Package fakenet is a network of fake devices for tests: it answers the
// device probe's two ways of asking an address (engine.Config's ProbeATEM
// and ProbeDial), and dials the fake HyperDecks, Videohubs and
// Ultimattes, so nothing leaves the process.
package fakenet

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"syscall"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fakehyperdeck"
	"github.com/lucas-romanenko/bmdctl/internal/fakeultimatte"
	"github.com/lucas-romanenko/bmdctl/internal/fakevideohub"
)

// VideohubPreamble is the start of a Videohub's state dump, as
// videohubwire v1.3.0's tests write it (test_videohubwire_client.py,
// PREAMBLE), up to the end of its device block.
const VideohubPreamble = "PROTOCOL PREAMBLE:\nVersion: 2.7\n\nVIDEOHUB DEVICE:\nDevice present: true\n" +
	"Model name: Blackmagic Smart Videohub 40 x 40\nFriendly name: Test Hub\nUnique ID: 0123456789AB\n" +
	"Video inputs: 4\nVideo processing units: 0\nVideo outputs: 4\nVideo monitoring outputs: 0\nSerial ports: 0\n\n" +
	"INPUT LABELS:\n0 Camera 1\n1 Camera 2\n\n"

// UltimattePrelude is an Ultimatte's 9998 prelude, as ultimattewire
// v1.3.0's tests write it (conftest.py, SAMPLE_PRELUDE).
const UltimattePrelude = "Label: Keyer A\nSoftware Release: 2.0\n\nCONTROL:\nMatte Density: 5000\n" +
	"Transition Rate: 30\nFILE LIST:\npreset-a\npreset-b\n\nGPI SETTINGS:\n\nEND PRELUDE:\n"

// Net is which fake device is at which host.
type Net struct {
	mu         sync.Mutex
	everyATEM  bool
	absent     map[string]bool
	atems      map[string]bool
	decks      map[string]*fakehyperdeck.Deck
	hubs       map[string]*fakevideohub.Hub
	keyers     map[string]*fakeultimatte.Unit
	videohubs  map[string]string
	ultimattes map[string]string
}

// New returns an empty network: nothing answers anywhere.
func New() *Net {
	return &Net{absent: map[string]bool{}, atems: map[string]bool{}, decks: map[string]*fakehyperdeck.Deck{},
		hubs: map[string]*fakevideohub.Hub{}, keyers: map[string]*fakeultimatte.Unit{}, videohubs: map[string]string{},
		ultimattes: map[string]string{}}
}

// EveryATEM puts a switcher at every host that holds no other device.
func (n *Net) EveryATEM() { n.mu.Lock(); n.everyATEM = true; n.mu.Unlock() }

// Absent makes host hold nothing, whatever EveryATEM says.
func (n *Net) Absent(host string) { n.mu.Lock(); n.absent[host] = true; n.mu.Unlock() }

// ATEM puts a switcher at host: it answers the handshake.
func (n *Net) ATEM(host string) { n.mu.Lock(); n.atems[host] = true; n.mu.Unlock() }

// Deck puts a HyperDeck at host and returns it.
func (n *Net) Deck(host string, d *fakehyperdeck.Deck) *fakehyperdeck.Deck {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.decks[host] = d
	return d
}

// Hub puts a fake Videohub at host and returns it: it answers on 9990
// as a router does, where Videohub's stand-in only sends a preamble.
func (n *Net) Hub(host string, h *fakevideohub.Hub) *fakevideohub.Hub {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.hubs[host] = h
	return h
}

// Videohub puts a router at host that sends preamble on 9990, and does
// nothing more: enough for the device probe.
func (n *Net) Videohub(host, preamble string) {
	n.mu.Lock()
	n.videohubs[host] = preamble
	n.mu.Unlock()
}

// Keyer puts a fake Ultimatte at host and returns it: it answers on
// 9998 and 9996 as a unit does, where Ultimatte's stand-in only sends a
// prelude.
func (n *Net) Keyer(host string, u *fakeultimatte.Unit) *fakeultimatte.Unit {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.keyers[host] = u
	return u
}

// Ultimatte puts a keyer at host that sends prelude on 9998, and does
// nothing more: enough for the device probe.
func (n *Net) Ultimatte(host, prelude string) {
	n.mu.Lock()
	n.ultimattes[host] = prelude
	n.mu.Unlock()
}

// ProbeATEM answers the handshake for a host with a switcher, through a
// fake one; anywhere else nothing answers, at once.
func (n *Net) ProbeATEM(ctx context.Context, addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	n.mu.Lock()
	ok := n.atems[host] || n.everyATEM && !n.absent[host] && n.decks[host] == nil && n.hubs[host] == nil &&
		n.keyers[host] == nil && n.videohubs[host] == "" && n.ultimattes[host] == ""
	n.mu.Unlock()
	if !ok {
		return atem.ErrNoSwitcher
	}
	return (&atem.Dialer{Conn: fakeatem.New(nil)}).Probe(ctx, fakeatem.Address)
}

// Dial connects to a fake device's TCP port: a HyperDeck's 9993, a
// Videohub's 9990, an Ultimatte's 9998. Anything else is refused.
func (n *Net) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(ps)
	n.mu.Lock()
	deck, hub, keyer := n.decks[host], n.hubs[host], n.keyers[host]
	preamble, prelude := n.videohubs[host], n.ultimattes[host] // the stand-ins
	n.mu.Unlock()
	text := ""
	switch {
	case port == 9993 && deck != nil:
		return deck.Dial(ctx, network, addr)
	case port == 9990 && hub != nil:
		return hub.Dial(ctx, network, addr)
	case (port == 9998 || port == 9996) && keyer != nil:
		return keyer.Dial(ctx, network, addr)
	case port == 9990 && preamble != "":
		text = preamble
	case port == 9998 && prelude != "":
		text = prelude
	default:
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	client, server := net.Pipe()
	go func() {
		io.WriteString(server, text)
		io.Copy(io.Discard, server) // until the client hangs up
		server.Close()
	}()
	return client, nil
}
