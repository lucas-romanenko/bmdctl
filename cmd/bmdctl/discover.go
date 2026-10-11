// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/mdns"
)

// switcherService is the DNS-SD service ATEM switchers announce
// themselves under, the name set in ATEM Setup as the instance name: the
// one ATEM Software Control browses, and WebATEM (atem-web-control's
// discovery.py) found on a VLAN of 250 switchers. Blackmagic's other
// devices announce _blackmagic._tcp; bmdctl has no commands for them yet.
const switcherService = "_switcher_ctrl._udp.local"

// browse finds the switchers that announce themselves within wait: the
// real discover, on the network. One switcher answering at several
// addresses, or twice, is one switcher (its TXT "unique id", else its
// address).
func browse(ctx context.Context, wait time.Duration) ([]mdns.Instance, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	found, err := mdns.Browse(ctx, conn, mdns.Group, switcherService, wait)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []mdns.Instance
	for _, in := range found {
		k := in.TXT["unique id"]
		if k == "" && len(in.Addrs) > 0 {
			k = in.Addrs[0].String()
		}
		if k != "" && seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, in)
	}
	return out, nil
}
