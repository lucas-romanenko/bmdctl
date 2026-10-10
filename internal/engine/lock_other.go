// SPDX-License-Identifier: MIT

//go:build !windows && !(linux || darwin || freebsd || openbsd || netbsd || dragonfly)

package engine

import (
	"fmt"
	"os"
)

// lockFile opens path without locking it: this platform's syscall package
// has no flock. Listen still refuses a socket an engine answers on.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return f, nil
}
