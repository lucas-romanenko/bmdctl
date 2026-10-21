// SPDX-License-Identifier: MIT

// Package videohub controls Blackmagic Design Videohub routers over the
// Videohub Ethernet Protocol (TCP 9990, text blocks), ported from bmdwire
// v1.6.0's videohubwire (client.py): the framing, the preamble, the four
// commands (route, input and output labels, ping) and the six blocks it
// parses, checked against what videohubwire itself does
// (testdata/oracle). A Session has atem.Session's shape: Send (the hub
// acks every command), State, Snapshot (videohubwire's state()),
// Subscribe (the blocks the hub pushes as anything changes), Ready,
// Connected and Close. docs/SURFACE-videohub.md is videohubwire's
// inventory and docs/PORTING.md, "Videohub", the decisions.
//
//	s, err := videohub.Dial(ctx, "192.0.2.31")
//	snap, err := s.Snapshot()
//	err = s.Route(ctx, 3, 12) // input 13 to output 4: the wire counts from 0
package videohub
