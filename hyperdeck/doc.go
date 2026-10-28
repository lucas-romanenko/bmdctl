// SPDX-License-Identifier: MIT

// Package hyperdeck controls Blackmagic Design HyperDeck recorders over
// the HyperDeck Ethernet Protocol (TCP 9993, line-oriented text), ported
// from bmdwire v1.8.0's hyperdeckwire (client.py): the framing, the
// commands and the answers' parsing, checked against what hyperdeckwire
// itself does (testdata/oracle). A Session has atem.Session's shape: Send,
// SendAndWait (the deck answers every command), State, Subscribe (the
// deck's asynchronous notifications), Ready, Connected and Close; the
// state and the notifications are bmdctl's, as hyperdeckwire keeps none.
// docs/SURFACE-hyperdeck.md is hyperdeckwire's inventory and
// docs/PORTING.md, "HyperDeck", the decisions.
//
//	s, err := hyperdeck.Dial(ctx, "192.0.2.11")
//	clips, err := s.ClipsGet(ctx)
//	on := true
//	err = s.Play(ctx, hyperdeck.PlayOptions{Loop: &on, SingleClip: &on})
//	fmt.Println(s.State().Status())
package hyperdeck
