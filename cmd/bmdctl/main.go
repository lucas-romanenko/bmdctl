// SPDX-License-Identifier: MIT

// Command bmdctl controls Blackmagic Design devices from the command line.
//
//	bmdctl version
//	bmdctl atem --help
//	bmdctl atem --host 10.0.0.20 program --source 5
//	bmdctl atem --dry-run transition wipe --me 0 --width 50
//
// Its device commands are built from the command table (internal/table);
// docs/COMMANDS.md lists them all.
package main

import (
	"os"
)

// Set by GoReleaser through -ldflags.
var (
	version = ""
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
