// SPDX-License-Identifier: MIT

//go:build windows

package engine

// The engine's named pipe, through kernel32 and advapi32 directly: the
// standard library has no named pipe API.
//
// Both ends open the pipe for overlapped I/O, which os.NewFile hands to
// the runtime's poller. On a handle opened for synchronous I/O Windows
// runs one call at a time: the engine's read that watches for the caller
// hanging up (serveConn) held up its own response's write, and every
// request hung (found when CI first ran on Windows).

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
	procCreateEventW        = kernel32.NewProc("CreateEventW")
	procGetOverlappedResult = kernel32.NewProc("GetOverlappedResult")
	procConvertStringSDToSD = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
)

const (
	pipeAccessDuplex          = 0x00000003
	fileFlagOverlapped        = syscall.FILE_FLAG_OVERLAPPED
	fileFlagFirstPipeInstance = 0x00080000
	pipeRejectRemoteClients   = 0x00000008 // PIPE_TYPE_BYTE, PIPE_READMODE_BYTE and PIPE_WAIT are 0
	pipeUnlimitedInstances    = 255
	pipeBuffer                = 64 << 10

	errorFileNotFound  = syscall.Errno(2)
	errorAccessDenied  = syscall.Errno(5)
	errorPipeBusy      = syscall.Errno(231)
	errorPipeConnected = syscall.Errno(535)
	errorNoData        = syscall.Errno(232)
	errorNotConnected  = syscall.Errno(233)
)

type pipeListener struct {
	name string
	sa   *syscall.SecurityAttributes

	mu        sync.Mutex
	next      syscall.Handle // the instance the next client connects to
	accepting bool           // an Accept is waiting on next
	closed    bool
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
	mode := uintptr(pipeAccessDuplex | fileFlagOverlapped)
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
		if l.closed {
			l.closeNext()
			l.mu.Unlock()
			return nil, net.ErrClosed
		}
		h := l.next
		l.accepting = true
		l.mu.Unlock()
		ok, e := connectPipe(h)
		c, err, retry := l.accepted(h, ok, e)
		if !retry {
			return c, err
		}
	}
}

// connectPipe waits for a client on the instance h, which is open for
// overlapped I/O, so ConnectNamedPipe returns at once and the wait is on
// an event. ok is set once a client is connected, e otherwise says why.
func connectPipe(h syscall.Handle) (ok bool, e error) {
	ev, _, e := procCreateEventW.Call(0, 1, 0, 0)
	if ev == 0 {
		return false, e
	}
	defer syscall.CloseHandle(syscall.Handle(ev))
	ov := &syscall.Overlapped{HEvent: syscall.Handle(ev)}
	r, _, e := procConnectNamedPipe.Call(uintptr(h), uintptr(unsafe.Pointer(ov)))
	switch {
	case r != 0, errors.Is(e, errorPipeConnected):
		return true, nil
	case !errors.Is(e, syscall.ERROR_IO_PENDING):
		return false, e
	}
	if _, err := syscall.WaitForSingleObject(syscall.Handle(ev), syscall.INFINITE); err != nil {
		return false, err
	}
	var n uint32
	if r, _, e := procGetOverlappedResult.Call(uintptr(h), uintptr(unsafe.Pointer(ov)), uintptr(unsafe.Pointer(&n)), 0); r == 0 {
		return false, e
	}
	return true, nil
}

// accepted finishes an Accept: the connected instance h becomes a
// connection and a new instance waits for the next client. retry is set
// when the client left before it was seen.
func (l *pipeListener) accepted(h syscall.Handle, ok bool, e error) (io.ReadWriteCloser, error, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.accepting = false
	if l.closed {
		l.closeNext()
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

// closeNext closes the instance waiting for a client. An instance left
// open after the engine stops listening answers every client "busy", and
// the next bmdctl waited on it for ever. l.mu is held.
func (l *pipeListener) closeNext() {
	if l.next != syscall.InvalidHandle {
		syscall.CloseHandle(l.next)
		l.next = syscall.InvalidHandle
	}
}

// Close stops listening. With no Accept waiting it closes the instance
// at once; otherwise it cancels the wait, and connects to the pipe itself
// in case the wait had not begun, and Accept closes the instance.
func (l *pipeListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	if !l.accepting {
		l.closeNext()
		l.mu.Unlock()
		return nil
	}
	h := l.next
	l.mu.Unlock()
	syscall.CancelIoEx(h, nil)
	if f, err := os.OpenFile(l.name, os.O_RDWR, 0); err == nil {
		f.Close()
	}
	return nil
}

type pipeConn struct {
	*os.File
	h syscall.Handle
}

// Close waits for the client to read what was written, then closes the
// instance. It does not disconnect it first: the client would then read
// "no process is on the other end of the pipe" (ERROR_PIPE_NOT_CONNECTED)
// rather than the end of the reply.
func (c *pipeConn) Close() error {
	syscall.FlushFileBuffers(c.h)
	return c.File.Close()
}

// clientConn is the client's end. A pipe its engine disconnected reads
// as the end of the reply, as a closed one does.
type clientConn struct{ *os.File }

func (c clientConn) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	if errors.Is(err, errorNotConnected) {
		err = io.EOF
	}
	return n, err
}

// Dial opens the engine's pipe, waiting while every instance is busy. It
// is opened for overlapped I/O, so closing it (Call does when ctx ends)
// ends a read in flight.
func Dial(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
			syscall.OPEN_EXISTING, fileFlagOverlapped, 0)
		if err == nil {
			return clientConn{os.NewFile(uintptr(h), path)}, nil
		}
		if !errors.Is(err, errorPipeBusy) {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
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
