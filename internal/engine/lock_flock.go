// SPDX-License-Identifier: MIT

//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package engine

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile takes an exclusive lock on path, held until the file is
// closed (or the process dies), or returns ErrRunning.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("engine: lock %s: %w", path, err)
	}
	return f, nil
}
