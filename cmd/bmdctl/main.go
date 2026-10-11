// SPDX-License-Identifier: MIT

// Command bmdctl controls Blackmagic Design devices from the command line.
//
//	bmdctl
//	bmdctl help
//	bmdctl hosts add 10.0.0.20
//	bmdctl --host studio program "Camera 2"
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

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	c := newCLI(os.Stdout, os.Stderr)
	c.tty, c.ttyErr, c.ttyIn = isTerminal(os.Stdout), isTerminal(os.Stderr), isTerminal(os.Stdin)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	c.ctx = ctx
	code := c.run(os.Args[1:])
	stop()
	os.Exit(code)
}
