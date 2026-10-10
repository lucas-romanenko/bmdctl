// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

// Package profile reads and writes ATEM Software Control "Save Switcher
// State" XML profiles: a port of bmdwire v1.3.0 atemwire/profile
// (__init__.py, save.py, apply.py, options.py, _common.py, _enums.py,
// _xml.py; SURFACE.md §3.3).
//
// Save builds a profile from a session's state, downloading each used
// macro's bytecode. Apply walks a profile and sends the commands that
// restore it, uploading macros. Both follow atemwire exactly: the same
// sections, the same hardcoded defaults, the same skipped fields and the
// same quirks (SURFACE.md §5.4 items 1 and 8), so the XML is byte for byte
// what atemwire writes and the commands are byte for byte what its
// operations send.
//
// The XML layer (Element, FromXML, ToXML) mirrors what atemwire gets from
// Python's xml.etree.ElementTree over expat: the same tree (text, tails,
// attribute order, namespaces as "{uri}local"), the same serialization
// (ET.indent with four spaces, short empty elements without the space
// before "/>", the UTF-8 declaration) and the same refusals (NUL bytes, a
// DTD or entity declaration).
//
// download_media_pool_images, which exports media pool stills as PNG
// files, is not ported: it needs still download and PNG encoding.
package profile
