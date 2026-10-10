// SPDX-License-Identifier: MIT

//go:build !windows

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/lucas-romanenko/bmdctl/internal/paths"
)

type unixListener struct {
	l    *net.UnixListener
	path string
	lock *os.File
}

// Listen listens at path, a Unix socket only its owner may use. A lock
// file beside it (path.lock) keeps a second engine from taking over the
// path; a socket left by an engine that died is removed.
func Listen(path string) (Listener, error) {
	if paths.IsPipe(path) {
		return nil, fmt.Errorf("engine: %s is a Windows pipe name", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	if err := ownDir(dir); err != nil {
		return nil, err
	}
	lock, err := lockFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		lock.Close()
		return nil, ErrRunning
	}
	os.Remove(path)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("engine: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		lock.Close()
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &unixListener{l, path, lock}, nil
}

// ownDir refuses a socket directory another user owns: anyone who can
// replace the socket there could answer for the engine.
func ownDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() && os.Getuid() != 0 {
		return fmt.Errorf("engine: %s belongs to another user", dir)
	}
	return nil
}

func (l *unixListener) Accept() (io.ReadWriteCloser, error) { return l.l.Accept() }
func (l *unixListener) Addr() string                        { return l.path }

func (l *unixListener) Close() error {
	err := l.l.Close() // removes the socket file
	l.lock.Close()
	return err
}

// Dial connects to the engine at path.
func Dial(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}

// NotRunning reports whether a Dial error means no engine listens there.
func NotRunning(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}
