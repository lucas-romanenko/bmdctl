// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

// Package fixtures embeds the packets, bytecode samples, profile XML and
// state snapshots copied from bmdwire v1.3.0's atemwire test suite. The
// README beside this file lists where each one came from.
package fixtures

import (
	"embed"
	"io/fs"
	"testing"
)

//go:embed audio dump/*.bin macro oracle/*.json profile snapshot transport
var files embed.FS

// FS returns every fixture, keyed by its path relative to this directory
// (for example "audio/AMBP-band5.bin").
func FS() fs.FS { return files }

// Bytes returns the fixture at path, failing the test if it is missing.
func Bytes(t testing.TB, path string) []byte {
	t.Helper()
	b, err := files.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	return b
}
