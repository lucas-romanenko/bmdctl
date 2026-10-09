// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Command is one command a client sends to the switcher: the Go
// counterpart of an atemwire Send class together with the operation that
// wraps it (bmdwire v1.3.0, atemwire/messages).
type Command interface {
	// Opcode is the four-character command code, for example "CPgI".
	Opcode() string
	// Encode returns the bytes that go on the wire: the 8-byte command
	// header (length, two zero bytes, opcode) and the payload, as
	// atemwire's get_command() does.
	Encode() []byte
	// EchoKey is the Change.Key of the state packet the switcher sends
	// back once it has applied the command, for example "PrgI:0". It is
	// "" for commands with no echo of their own (SURFACE.md §2.7).
	EchoKey() string
	// Unverified reports a command whose bytes are pinned by atemwire's
	// tests but which nobody has confirmed against a switcher: the 20
	// restored-upstream classes and CKMs (SURFACE.md §5.4 item 14).
	Unverified() bool
	// Destructive reports a command that changes what the switcher does
	// in a way that is hard to undo or drops clients: VideoMode,
	// SaveStartupState, ClearStartupState and ClearStill.
	Destructive() bool
}

// Validator is implemented by commands whose fields can hold values the
// wire cannot carry. Session.Send calls it before encoding; where atemwire
// raises (struct.error from a value out of range, ValueError from a bad
// argument combination) Validate returns an error instead.
type Validator interface {
	Validate() error
}

// maxCommandBytes is the largest command a session sends, and the most
// command bytes it packs into one packet (atemwire's per-packet BUDGET in
// ATEMConnection._drain_cmd_queue).
const maxCommandBytes = 1200
