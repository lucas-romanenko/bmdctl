// SPDX-License-Identifier: MIT

//go:build windows

package engine

// The engine's named pipe, through kernel32 and advapi32 directly: the
// standard library has no named pipe API.
//
// Both ends open the pipe for overlapped I/O (pipeConn). On a handle
// opened for synchronous I/O Windows runs one call at a time: the
// engine's read that watches for the caller hanging up (serveConn) held
// up its own response's write, and every request hung (found when CI
// first ran on Windows).

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
	procReadFile            = kernel32.NewProc("ReadFile")
	procWriteFile           = kernel32.NewProc("WriteFile")
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
	errorAborted       = syscall.Errno(995)
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
	c, err := newPipeConn(h, l.name, true)
	if err != nil {
		syscall.CloseHandle(h)
		return nil, err, false
	}
	return c, nil, false
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

// pipeConn is one end of a connected pipe opened for overlapped I/O. A
// read and a write can be in flight at once, each waiting on its own
// event. (os.NewFile would hand such a handle to the runtime's poller,
// but os.File keeps a file offset its concurrent reads and writes race
// on.) The kernel writes into the OVERLAPPED structures and buffers until
// the call completes, so they belong to the connection, on the heap, and
// Close waits for every call in flight before it closes the handle.
type pipeConn struct {
	h      syscall.Handle
	name   string
	server bool
	rd, wr pipeOp

	mu       sync.Mutex
	closed   bool
	inflight sync.WaitGroup
}

// pipeOp is one direction's call: its OVERLAPPED, its event and its
// buffer. Reads and writes are each one at a time (mu).
type pipeOp struct {
	mu  sync.Mutex
	ov  syscall.Overlapped
	buf []byte
}

func newPipeConn(h syscall.Handle, name string, server bool) (*pipeConn, error) {
	c := &pipeConn{h: h, name: name, server: server}
	for _, op := range []*pipeOp{&c.rd, &c.wr} {
		ev, _, e := procCreateEventW.Call(0, 1, 0, 0)
		if ev == 0 {
			c.closeEvents()
			return nil, e
		}
		op.ov.HEvent = syscall.Handle(ev)
		op.buf = make([]byte, pipeBuffer)
	}
	return c, nil
}

func (c *pipeConn) closeEvents() {
	for _, op := range []*pipeOp{&c.rd, &c.wr} {
		if op.ov.HEvent != 0 {
			syscall.CloseHandle(op.ov.HEvent)
		}
	}
}

// call runs one ReadFile or WriteFile of n bytes of op's buffer and waits
// for it.
func (c *pipeConn) call(proc *syscall.LazyProc, op *pipeOp, n int) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.inflight.Add(1)
	c.mu.Unlock()
	defer c.inflight.Done()
	ev := op.ov.HEvent
	op.ov = syscall.Overlapped{HEvent: ev}
	r, _, e := proc.Call(uintptr(c.h), uintptr(unsafe.Pointer(&op.buf[0])), uintptr(n), 0, uintptr(unsafe.Pointer(&op.ov)))
	if r == 0 && !errors.Is(e, syscall.ERROR_IO_PENDING) {
		return 0, e
	}
	var done uint32
	if r, _, e := procGetOverlappedResult.Call(uintptr(c.h), uintptr(unsafe.Pointer(&op.ov)), uintptr(unsafe.Pointer(&done)), 1); r == 0 {
		return int(done), e
	}
	return int(done), nil
}

func (c *pipeConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.rd.mu.Lock()
	defer c.rd.mu.Unlock()
	n, err := c.call(procReadFile, &c.rd, min(len(p), len(c.rd.buf)))
	copy(p, c.rd.buf[:n])
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, syscall.ERROR_BROKEN_PIPE), errors.Is(err, errorNotConnected):
		// The other end closed, or (an engine before this one)
		// disconnected: the end of the reply.
		return n, io.EOF
	case errors.Is(err, errorAborted), errors.Is(err, net.ErrClosed):
		return n, net.ErrClosed
	}
	return n, &os.PathError{Op: "read", Path: c.name, Err: err}
}

func (c *pipeConn) Write(p []byte) (int, error) {
	c.wr.mu.Lock()
	defer c.wr.mu.Unlock()
	written := 0
	for written < len(p) {
		n := copy(c.wr.buf, p[written:])
		m, err := c.call(procWriteFile, &c.wr, n)
		written += m
		if err != nil {
			if errors.Is(err, errorAborted) || errors.Is(err, net.ErrClosed) {
				return written, net.ErrClosed
			}
			return written, &os.PathError{Op: "write", Path: c.name, Err: err}
		}
		if m < n {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

// Close ends the calls in flight and closes the handle. The engine's end
// first waits for the client to read what was written, and does not
// disconnect: the client would then read "no process is on the other end
// of the pipe" (ERROR_PIPE_NOT_CONNECTED) rather than the end of the
// reply.
func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	if c.server {
		syscall.FlushFileBuffers(c.h)
	}
	// A call that had passed its check but not yet started when the
	// first cancel went misses it: cancel again until none is left.
	idle := make(chan struct{})
	go func() { c.inflight.Wait(); close(idle) }()
	for {
		syscall.CancelIoEx(c.h, nil)
		select {
		case <-idle:
		case <-time.After(10 * time.Millisecond):
			continue
		}
		break
	}
	c.closeEvents()
	return syscall.CloseHandle(c.h)
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
			c, err := newPipeConn(h, path, false)
			if err != nil {
				syscall.CloseHandle(h)
				return nil, err
			}
			return c, nil
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
