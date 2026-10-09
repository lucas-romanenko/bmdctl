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
	"strings"
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

// store is save_field_data: parse one command with its Recv
// counterpart, put it where atemwire's KEY_FORMAT (or the strip id) puts
// it, and return the change to publish. A nil change means the command was
// consumed without being stored. A parse error is atemwire's exception out
// of the Recv constructor: the caller drops the rest of the packet.
func (s *State) store(code string, payload []byte) (*Change, error) {
	if !utf8.ValidString(code) {
		// atemwire's fieldname.decode() raises here.
		return nil, fmt.Errorf("atem: command code %q is not UTF-8", code)
	}
	p, ok := parsers[code]
	if !ok {
		b := bytes.Clone(payload)
		s.Raw[code] = b
		return &Change{Kind: ChangeState, Opcode: code, Key: code, Value: b}, nil
	}
	v, err := p.parse(payload)
	if err != nil {
		return nil, err
	}
	if p.store == nil {
		return nil, nil
	}
	key := p.store(s, v)
	if key == "" {
		return nil, nil
	}
	return &Change{Kind: ChangeState, Opcode: code, Key: key, Value: v}, nil
}

// cloneValue copies a Change value so a receiver can't reach the state.
func cloneValue(v any) any {
	if v == nil {
		return nil
	}
	return deepCopy(v)
}

// parser is one opcode's entry: atemwire's Recv class (parse) and where
// save_field_data puts the result (store).
type parser struct {
	code string
	// name is atemwire's mixerstate key (the Recv class's PRETTY).
	name  string
	parse func([]byte) (any, error)
	// store files the value in the state and returns its Change.Key, or
	// "" to drop it unstored. nil means the transfer engine consumes it.
	store func(*State, any) string
}

// entry builds a parser from typed functions.
func entry[T any](code, name string, parse func([]byte) (*T, error), store func(*State, *T) string) parser {
	p := parser{code: code, name: name, parse: func(b []byte) (any, error) {
		v, err := parse(b)
		if err != nil {
			return nil, err
		}
		return v, nil
	}}
	if store != nil {
		p.store = func(s *State, v any) string { return store(s, v.(*T)) }
	}
	return p
}

// parsers is every Recv class atemwire registers, by opcode: the four
// tables of SURFACE.md §2, one file each.
var parsers = func() map[string]parser {
	m := make(map[string]parser)
	for _, list := range [][]parser{mixeffectParsers, keyerParsers, audioParsers, systemParsers} {
		for _, p := range list {
			if _, dup := m[p.code]; dup {
				panic("atem: two parsers for " + p.code)
			}
			m[p.code] = p
		}
	}
	return m
}()

// key is a Change.Key: the opcode and the packet's indices.
func key(code string, idx ...any) string {
	for _, i := range idx {
		code += ":" + fmt.Sprint(i)
	}
	return code
}

// at returns element i of *s, growing the slice to reach it.
func at[T any](s *[]T, i int) *T {
	if i >= len(*s) {
		*s = append(*s, make([]T, i+1-len(*s))...)
	}
	return &(*s)[i]
}

// atLeast and exactly check a payload's length the way atemwire's struct
// calls do: unpack_from (and every declarative Recv field) needs at least
// n bytes, struct.unpack exactly n.
func atLeast(code string, b []byte, n int) error {
	if len(b) < n {
		return fmt.Errorf("atem: %s payload is %d bytes, needs at least %d", code, len(b), n)
	}
	return nil
}

func exactly(code string, b []byte, n int) error {
	if len(b) != n {
		return fmt.Errorf("atem: %s payload is %d bytes, needs exactly %d", code, len(b), n)
	}
	return nil
}

func u16(b []byte, off int) uint16 { return binary.BigEndian.Uint16(b[off:]) }
func i16(b []byte, off int) int16  { return int16(binary.BigEndian.Uint16(b[off:])) }
func u32(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }
func i32(b []byte, off int) int32  { return int32(binary.BigEndian.Uint32(b[off:])) }
func i64(b []byte, off int) int64  { return int64(binary.BigEndian.Uint64(b[off:])) }
func flag(b []byte, off int) bool  { return b[off] != 0 }

// cstr is atemwire's string field: the bytes up to the first NUL, decoded
// as UTF-8 with errors="replace".
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return decodeReplace(b)
}

// decodeReplace is Python's bytes.decode("utf-8", "replace"): each maximal
// invalid subpart becomes one U+FFFD.
func decodeReplace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || n > 1 {
			sb.Write(b[i : i+n])
			i += n
			continue
		}
		sb.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return sb.String()
}

// maximalSubpart is the length of the invalid sequence at the start of b
// that CPython's decoder replaces with a single U+FFFD: a lead byte and the
// continuation bytes after it that could still have completed it.
func maximalSubpart(b []byte) int {
	lo, hi := byte(0x80), byte(0xBF)
	var need int
	switch c := b[0]; {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c == 0xF4:
		need, hi = 3, 0x8F
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	default:
		return 1
	}
	n := 1
	for k := 0; k < need && n < len(b); k++ {
		c := b[n]
		if k == 0 && (c < lo || c > hi) || k > 0 && (c < 0x80 || c > 0xBF) {
			return n
		}
		n++
	}
	return n
}

// stripID is atemwire's strip id: "<source>.<subchannel>" for a split-mono
// strip (is_split 0xFF), "<source>.0" otherwise.
func stripID(source uint16, isSplit, subchannel uint8) string {
	if isSplit == 0xFF {
		return fmt.Sprintf("%d.%d", source, subchannel)
	}
	return fmt.Sprintf("%d.0", source)
}
