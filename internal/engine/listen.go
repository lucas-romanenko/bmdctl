// SPDX-License-Identifier: MIT

package engine

import (
	"errors"
	"io"
)

// Listener accepts the CLI's connections: a Unix socket, or a named pipe
// on Windows.
type Listener interface {
	Accept() (io.ReadWriteCloser, error)
	Close() error
	// Addr is the socket path or pipe name.
	Addr() string
}

// ErrRunning is returned by Listen when another engine already listens at
// the path.
var ErrRunning = errors.New("engine: another engine is already running")
