// SPDX-License-Identifier: MIT

//go:build windows

package engine

// The engine's named pipe, through kernel32 and advapi32 directly: the
// standard library has no named pipe API.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lucas-romanenko/bmdctl/internal/paths"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	advapi32                = syscall.NewLazyDLL("advapi32.dll")
	procCreateNamedPipeW    = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe    = kernel32.NewProc("ConnectNamedPipe")
	procDisconnectNamedPipe = kernel32.NewProc("DisconnectNamedPipe")
	procConvertStringSDToSD = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
)

const (
	pipeAccessDuplex          = 0x00000003
	fileFlagFirstPipeInstance = 0x00080000
	pipeRejectRemoteClients   = 0x00000008 // PIPE_TYPE_BYTE, PIPE_READMODE_BYTE and PIPE_WAIT are 0
	pipeUnlimitedInstances    = 255
	pipeBuffer                = 64 << 10

	errorFileNotFound  = syscall.Errno(2)
	errorAccessDenied  = syscall.Errno(5)
	errorPipeBusy      = syscall.Errno(231)
	errorPipeConnected = syscall.Errno(535)
	errorNoData        = syscall.Errno(232)
)

type pipeListener struct {
	name string
	sa   *syscall.SecurityAttributes

	mu     sync.Mutex
	next   syscall.Handle // the instance the next client connects to
	closed bool
}

// Listen creates the named pipe path, which only the current user (and
// SYSTEM) may open and no remote client may reach. Creating its first
// instance fails if another engine already has the name.
func Listen(path string) (Listener, error) {
	if !paths.IsPipe(path) {
		return nil, fmt.Errorf(`engine: %s is not a pipe name (\\.\pipe\...)`, path)
	}
	sa, err := ownerOnly()
	if err != nil {
		return nil, fmt.Errorf("engine: pipe security: %w", err)
	}
	l := &pipeListener{name: path, sa: sa}
	h, err := l.create(true)
	if err != nil {
		if errors.Is(err, errorAccessDenied) || errors.Is(err, errorPipeBusy) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("engine: %s: %w", path, err)
	}
	l.next = h
	return l, nil
}

// ownerOnly is a security descriptor granting the current user and SYSTEM
// full access, and nobody else any.
func ownerOnly() (*syscall.SecurityAttributes, error) {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid, err := u.User.Sid.String()
	if err != nil {
		return nil, err
	}
	sddl, err := syscall.UTF16PtrFromString("D:P(A;;GA;;;" + sid + ")(A;;GA;;;SY)")
	if err != nil {
		return nil, err
	}
	var sd uintptr
	if r, _, e := procConvertStringSDToSD.Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return nil, e
	}
	return &syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func (l *pipeListener) create(first bool) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(l.name)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	mode := uintptr(pipeAccessDuplex)
	if first {
		mode |= fileFlagFirstPipeInstance
	}
	r, _, e := procCreateNamedPipeW.Call(uintptr(unsafe.Pointer(name)), mode, pipeRejectRemoteClients,
		pipeUnlimitedInstances, pipeBuffer, pipeBuffer, 0, uintptr(unsafe.Pointer(l.sa)))
	if syscall.Handle(r) == syscall.InvalidHandle {
		return syscall.InvalidHandle, e
	}
	return syscall.Handle(r), nil
}

func (l *pipeListener) Addr() string { return l.name }

func (l *pipeListener) Accept() (io.ReadWriteCloser, error) {
	for {
		l.mu.Lock()
		h, closed := l.next, l.closed
		l.mu.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		r, _, e := procConnectNamedPipe.Call(uintptr(h), 0)
		c, err, retry := l.accepted(h, r != 0 || errors.Is(e, errorPipeConnected), e)
		if !retry {
			return c, err
		}
	}
}

// accepted finishes an Accept: the connected instance h becomes a
// connection and a new instance waits for the next client. retry is set
// when the client left before it was seen.
func (l *pipeListener) accepted(h syscall.Handle, ok bool, e error) (io.ReadWriteCloser, error, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		syscall.CloseHandle(h)
		return nil, net.ErrClosed, false
	}
	if !ok {
		if errors.Is(e, errorNoData) {
			procDisconnectNamedPipe.Call(uintptr(h))
			return nil, nil, true
		}
		return nil, e, false
	}
	next, err := l.create(false)
	if err != nil {
		syscall.CloseHandle(h)
		return nil, err, false
	}
	l.next = next
	return &pipeConn{File: os.NewFile(uintptr(h), l.name), h: h}, nil, false
}

// Close stops listening. ConnectNamedPipe can't be interrupted, so Close
// connects to the pipe itself to wake a waiting Accept.
func (l *pipeListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()
	if f, err := os.OpenFile(l.name, os.O_RDWR, 0); err == nil {
		f.Close()
	}
	return nil
}

type pipeConn struct {
	*os.File
	h syscall.Handle
}

// Close waits for the client to read what was written (closing a pipe
// discards unread data), then disconnects.
func (c *pipeConn) Close() error {
	syscall.FlushFileBuffers(c.h)
	procDisconnectNamedPipe.Call(uintptr(c.h))
	return c.File.Close()
}

// Dial opens the engine's pipe, waiting while every instance is busy.
func Dial(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	for {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, errorPipeBusy) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// NotRunning reports whether a Dial error means no engine listens there.
func NotRunning(err error) bool { return errors.Is(err, errorFileNotFound) }
