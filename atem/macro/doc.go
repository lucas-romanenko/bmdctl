// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

// Package macro converts ATEM macro bytecode to and from ops: the macro
// codec of atemwire's macrotransfer package, ported from bmdwire v1.3.0
// (docs/SURFACE-atem.md §3.2).
//
// A macro, as the switcher stores it and the file transfer moves it, is a
// concatenation of ops with no header, count or padding. Each op is a
// little-endian u16 total length (header included), a u16 op code and the
// op's parameters; everything is little-endian, unlike the live protocol.
// Decode turns that into Ops whose IDs and Params are what ATEM Software
// Control writes in a macro's <Op> elements, in atemwire's attribute order;
// Encode turns Ops back into bytecode. The 146 op codes atemwire knows have
// codecs; any other code round-trips as Unknown_0xNNNN with its parameter
// bytes in hex.
//
// The codec mirrors atemwire's, quirks included: values are parsed with
// Python's int() and float() rules, masked to 16 or 32 bits where atemwire
// masks them, range-checked where it checks them, and unknown names
// replaced where it replaces them (SURFACE §5.4 item 9). EncodeOp fails
// exactly where atemwire's encode_single_op raises, with the same Python
// exception class. docs/PORTING.md lists the details.
//
// The package is pure: it has no I/O and imports nothing from atem.
package macro
