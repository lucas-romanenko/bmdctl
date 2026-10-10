// SPDX-License-Identifier: MIT

// Command bmdctl controls Blackmagic Design devices from the command line.
//
//	bmdctl version
//	bmdctl --help
//	bmdctl --host 10.0.0.20 program --source "Camera 2"
//	bmdctl --dry-run transition wipe --width 50
//
// Commands run through a background engine (internal/engine) that keeps
// a session open to each switcher; the first command starts it. Its
// device commands are built from the command table (internal/table);
// docs/COMMANDS.md lists them all.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Set by GoReleaser through -ldflags.
var (
	version = ""
	commit  = ""
	date    = ""
)

func main() {
	c := newCLI(os.Stdout, os.Stderr)
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		c.tty = true
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	c.ctx = ctx
	code := c.run(os.Args[1:])
	stop()
	os.Exit(code)
}
