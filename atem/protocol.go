// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The protocol layer's packet handling, from bmdwire v1.3.0
// atemwire/protocol.py: decode_packet splits a packet's data into
// commands, save_field_data stores each one.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

// errCorrupt is atemwire's ConnectionError: a command header with length
// zero, which only transport corruption produces. The session answers it
// with a clean reconnect.
var errCorrupt = errors.New("atem: zero-length command header")

// decodeFields is decode_packet: walk the commands in a packet's data,
// each an 8-byte header (u16 length including the header, 2 bytes, 4-byte
// code) and its payload, and hand each to save in order. It stops at the
// first error, losing the rest of the packet, as an exception escaping
// atemwire's loop() does.
func decodeFields(data []byte, save func(code string, payload []byte) error) error {
	for off := 0; off < len(data); {
		if len(data)-off < 8 {
			return fmt.Errorf("atem: truncated command header at byte %d of %d", off, len(data))
		}
		n := int(binary.BigEndian.Uint16(data[off:]))
		if n == 0 {
			return errCorrupt
		}
		// Python slices data[off+8:off+n]: short lengths give an empty
		// payload and an overlong one is cut at the end of the packet.
		var payload []byte
		if lo, hi := off+8, min(off+n, len(data)); hi > lo {
			payload = data[lo:hi]
		}
		if err := save(string(data[off+4:off+8]), payload); err != nil {
			return err
		}
		off += n
	}
	return nil
}

// store is the storage half of save_field_data: put one command into the
// state and return the change to publish. A nil change means the command
// was consumed without being stored.
func (s *State) store(code string, payload []byte) (*Change, error) {
	if !utf8.ValidString(code) {
		// atemwire's fieldname.decode() raises here.
		return nil, fmt.Errorf("atem: command code %q is not UTF-8", code)
	}
	b := bytes.Clone(payload)
	s.Raw[code] = b
	return &Change{Kind: ChangeState, Opcode: code, Key: code, Value: b}, nil
}

// cloneValue copies a Change value so a receiver can't reach the state.
func cloneValue(v any) any {
	if v == nil {
		return nil
	}
	return deepCopy(v)
}
