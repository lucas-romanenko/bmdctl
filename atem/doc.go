// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

// Package atem controls Blackmagic Design ATEM switchers over their UDP
// protocol (port 9910).
//
// It is a Go port of atemwire from bmdwire v1.3.0 and mirrors its
// behaviour. docs/SURFACE-atem.md (copied from bmdwire v1.3.0) is the
// specification; docs/PORTING.md records every place this package
// deliberately differs and why.
//
// # Sessions
//
// Dial connects and returns once the switcher has sent its whole initial
// state. A Session then tracks the switcher's state, sends commands and
// reports changes:
//
//	s, err := atem.Dial(ctx, "192.0.2.10")
//	...
//	defer s.Close()
//	err = s.Send(cmd)                // queue a command, don't wait
//	c, err := s.SendAndWait(ctx, cmd) // wait for the switcher's echo
//	st := s.State()                  // a copy of everything known
//	changes, cancel := s.Subscribe() // every change from now on
//
// Connected and Ready are different things, as atemwire's is_connected and
// is_ready are. Connected is true from Dial's return until Close or a fatal
// socket error, including while the transport re-handshakes after 5 s of
// silence. Ready's channel is closed only while the state is complete: a
// re-handshake empties the state and Ready returns a new, open channel
// until the next state dump finishes.
//
// # Threading contract
//
// Every Session method is safe to call from any goroutine, any number of
// times, including Close.
//
// One owner goroutine holds everything that changes: the transport and its
// sequence numbers, the state, the queue of commands waiting to go out, the
// SendAndWait waiters and the subscribers. Nothing else reads or writes
// them, so there is no mutex on the state. A second goroutine does nothing
// but read datagrams from the socket and pass them to the owner; every
// write to the socket and every decision about a datagram is the owner's.
//
// The methods reach the owner over an unbuffered channel, so each request
// runs on the owner between two datagrams:
//
//   - State returns a deep copy taken between packets, never half-applied.
//   - Send validates and encodes the command, hands it to the owner's queue
//     and returns. It does not wait for the bytes to leave, nor for the
//     switcher to act on them.
//   - SendAndWait registers a waiter for the command's EchoKey and queues
//     the command in the same step, so the echo can't arrive in between. It
//     returns when a state packet with that key is stored, when ctx ends,
//     or when the session drops or closes.
//   - Subscribe's channel gets every Change. The owner never blocks on a
//     subscriber: one that falls 1024 changes behind has its channel closed,
//     so it learns it missed something instead of missing it silently.
//   - Connected and Ready read values the owner publishes atomically; they
//     never wait for the owner.
//
// The owner never blocks on a caller, so a slow caller can't hold up the
// ACKs the switcher needs. Values handed out (State, Change.Value) are
// copies the caller owns.
package atem
