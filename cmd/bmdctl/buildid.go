// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"io"
	"os"
)

// buildID names the build of the running executable: the Go toolchain's
// build ID (what go tool buildid prints), which changes whenever the
// binary's code does, or a hash of the file when it has none. Every
// request carries it, and an engine started from another build stops
// (docs/PORTING.md), so a binary replaced under a running engine gets an
// engine of its own. "" when the executable can't be read.
func buildID() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if id := goBuildID(exe); id != "" {
		return id
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// goBuildID reads a Go executable's build ID: from its ELF note, or from
// the marked string at the start of its text, which Mach-O and PE files
// carry within their first 32 KiB. "" when there is none.
func goBuildID(exe string) string {
	if f, err := elf.Open(exe); err == nil {
		defer f.Close()
		s := f.Section(".note.go.buildid")
		if s == nil {
			return ""
		}
		b, err := s.Data()
		if err != nil || len(b) < 16 {
			return ""
		}
		// A note: name size, description size, type, the name ("Go"
		// padded to 4 bytes), then the description, the ID.
		namesz, descsz := int(f.ByteOrder.Uint32(b)), int(f.ByteOrder.Uint32(b[4:]))
		start := 12 + (namesz+3)&^3
		if namesz != 4 || string(b[12:15]) != "Go\x00" || start+descsz > len(b) {
			return ""
		}
		return string(b[start : start+descsz])
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 32<<10)
	n, _ := io.ReadFull(f, head)
	_, rest, ok := bytes.Cut(head[:n], []byte("\xff Go build ID: \""))
	if !ok {
		return ""
	}
	id, _, ok := bytes.Cut(rest, []byte(`"`))
	if !ok {
		return ""
	}
	return string(id)
}
