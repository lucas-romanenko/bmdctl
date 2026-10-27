// SPDX-License-Identifier: MIT

package ultimatte

// The connections and the 9996 binary channel of bmdwire v1.7.0's
// ultimattewire/_protocol.py: _connect_with_retry, binary_request and
// upload_one.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// The ports: the text channel (the prelude, then blocks) and the binary
// one (preset slots and resources, one request per connection).
const (
	ControlPort  = 9998
	SettingsPort = 9996
)

// ultimattewire's timings: CONNECT_TIMEOUT (a connect, and a binary
// read's every read), READ_TIMEOUT (a write's ACK), the prelude's and a
// reply's quiet (PREAMBLE_QUIET_TIMEOUT, REPLY_QUIET_TIMEOUT), the drain
// after END PRELUDE, the read-back (READBACK_TIMEOUT, READBACK_POLL) and
// the wait before a connect's one retry (_CONNECT_RETRY_DELAY).
const (
	DefaultConnectTimeout  = 5 * time.Second
	DefaultReadTimeout     = 30 * time.Second
	DefaultPreludeQuiet    = 2 * time.Second
	DefaultReplyQuiet      = 2 * time.Second
	DefaultPreludeDrain    = 300 * time.Millisecond
	DefaultReadbackTimeout = 15 * time.Second
	DefaultReadbackPoll    = 500 * time.Millisecond
	DefaultRetryDelay      = 250 * time.Millisecond
)

// ackLen is a write's ACK: six zero bytes.
const ackLen = 6

// Client reaches Ultimatte units. ultimattewire keeps nothing between
// calls, and neither does a Client: every method opens its own
// connections and closes them. The zero value is ready to use.
type Client struct {
	// DialContext opens a TCP connection; nil means a net.Dialer's.
	// Tests point it at internal/fakeultimatte.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// The timings; zero means ultimattewire's (the Default constants).
	ConnectTimeout, ReadTimeout, PreludeQuiet, ReplyQuiet, PreludeDrain time.Duration
	ReadbackTimeout, ReadbackPoll, RetryDelay                           time.Duration

	// now and sleep are set_network's clock; tests replace them.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (c *Client) connectTimeout() time.Duration { return or(c.ConnectTimeout, DefaultConnectTimeout) }

// Error is what ultimattewire raises for the protocol's failures: Class
// names its exception (IOError for the binary channel and an empty
// prelude, UltimatteNetworkError for the text channel, ValueError for an
// argument refused, TimeoutError for a read that timed out), Msg is its
// message. errors.Is tells the kinds apart.
type Error struct {
	Class string
	Msg   string
	kind  error
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.kind }

// The kinds of Error.
var (
	// ErrRefused: the unit said no: a NAK, a non-zero ACK, a write it
	// closed on without an ACK.
	ErrRefused = errors.New("ultimatte: refused")
	// ErrShort: the unit stopped before it had answered: a short read,
	// no ACK in time, an empty prelude, no ACK to a block.
	ErrShort = errors.New("ultimatte: no full answer")
	// ErrUnsettled: a network set the unit accepted but never settled.
	ErrUnsettled = errors.New("ultimatte: did not settle")
	// ErrValue: an argument refused before anything is sent.
	ErrValue = errors.New("ultimatte: invalid argument")
)

func ioError(kind error, format string, args ...any) *Error {
	return &Error{Class: "IOError", Msg: fmt.Sprintf(format, args...), kind: kind}
}

// connect is _connect_with_retry: two attempts, RetryDelay apart.
func (c *Client) connect(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	dial := c.DialContext
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	addr := net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		var conn net.Conn
		conn, err = dial(cctx, "tcp", addr)
		cancel()
		if err == nil {
			return conn, nil
		}
		if attempt == 0 {
			select {
			case <-time.After(or(c.RetryDelay, DefaultRetryDelay)):
			case <-ctx.Done():
				return nil, err
			}
		}
	}
	return nil, err
}

// watch closes conn's reads and writes at once if ctx ends; the returned
// function stops it.
func watch(ctx context.Context, conn net.Conn) func() bool {
	return context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
}

// deadline is d from now, or the context's deadline if sooner.
func deadline(ctx context.Context, d time.Duration) time.Time {
	t := time.Now().Add(d)
	if dl, ok := ctx.Deadline(); ok && dl.Before(t) {
		return dl
	}
	return t
}

// timedOut reports whether err is a read or write that timed out:
// Python's socket.timeout.
func timedOut(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &ne) && ne.Timeout()
}

// errTimedOut is socket.timeout escaping a read: TimeoutError('timed
// out').
var errTimedOut = &Error{Class: "TimeoutError", Msg: "timed out", kind: ErrShort}

// readExact is _recv_exact: up to n bytes, fewer when the unit closes
// first; a read that times out is errTimedOut, as socket.timeout
// propagates from it.
func readExact(ctx context.Context, conn net.Conn, n int, timeout time.Duration) ([]byte, error) {
	var buf []byte
	chunk := make([]byte, min(n, 1<<20))
	for len(buf) < n {
		conn.SetReadDeadline(deadline(ctx, timeout))
		k, err := conn.Read(chunk[:min(len(chunk), n-len(buf))])
		buf = append(buf, chunk[:k]...)
		switch {
		case k > 0:
			continue
		case errors.Is(err, io.EOF):
			return buf, nil
		case timedOut(err) && ctx.Err() == nil:
			return buf, errTimedOut
		case err != nil:
			return buf, contextOr(ctx, err)
		}
	}
	return buf, nil
}

// contextOr is the context's error where it has ended, else err.
func contextOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// write sends b whole within timeout.
func write(ctx context.Context, conn net.Conn, b []byte, timeout time.Duration) error {
	conn.SetWriteDeadline(deadline(ctx, timeout))
	_, err := conn.Write(b)
	if err != nil && timedOut(err) && ctx.Err() == nil {
		return errTimedOut
	}
	return contextOr(ctx, err)
}

// BinaryRequest is binary_request: a read on 9996. It sends
// [opcode u16][len u32][payload] and returns the body of the
// [len u32][body] answer, which may be empty (a length of 0). A short
// length or body is an *Error, never a truncated answer: the archive
// would otherwise keep it and a restore push it into live hardware.
// ConnectTimeout bounds the connect and every read.
func (c *Client) BinaryRequest(ctx context.Context, host string, opcode uint16, payload []byte) ([]byte, error) {
	timeout := c.connectTimeout()
	conn, err := c.connect(ctx, host, SettingsPort, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer watch(ctx, conn)()
	frame := binary.BigEndian.AppendUint16(nil, opcode)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	if err := write(ctx, conn, append(frame, payload...), timeout); err != nil {
		return nil, err
	}
	head, err := readExact(ctx, conn, 4, timeout)
	if err != nil {
		return nil, err
	}
	if len(head) < 4 {
		return nil, ioError(ErrShort, "short read on length header (got %d of 4 bytes; unit closed before replying)", len(head))
	}
	n := binary.BigEndian.Uint32(head)
	if n == 0 {
		return []byte{}, nil
	}
	body, err := readExact(ctx, conn, int(n), timeout)
	if err != nil {
		return nil, err
	}
	if len(body) != int(n) {
		return nil, ioError(ErrShort, "short read on body (got %d of %d bytes)", len(body), n)
	}
	return body, nil
}

// UploadOne is upload_one: a write on 9996. It sends [opcode u16]
// [name_len u32][name][data_len u32][data][0x00] (the unit doesn't ACK a
// frame without the trailing zero) and reads the ACK, six zero bytes:
// fewer then the unit closing is a success (it closes that fast), any
// other byte a refusal. ReadTimeout bounds the ACK.
func (c *Client) UploadOne(ctx context.Context, host string, opcode uint16, name string, payload []byte) error {
	frame := binary.BigEndian.AppendUint16(nil, opcode)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(name)))
	frame = append(frame, name...)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(append(frame, payload...), 0)
	conn, err := c.connect(ctx, host, SettingsPort, c.connectTimeout())
	if err != nil {
		return err
	}
	defer conn.Close()
	defer watch(ctx, conn)()
	timeout := or(c.ReadTimeout, DefaultReadTimeout)
	if err := write(ctx, conn, frame, timeout); err != nil {
		return err
	}
	var ack []byte
	chunk := make([]byte, ackLen)
	for len(ack) < ackLen {
		conn.SetReadDeadline(deadline(ctx, timeout))
		k, err := conn.Read(chunk[:ackLen-len(ack)])
		ack = append(ack, chunk[:k]...)
		if k > 0 {
			continue
		}
		if timedOut(err) && ctx.Err() == nil {
			return ioError(ErrShort, "timeout waiting for ACK (got %d of %d bytes)", len(ack), ackLen)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return contextOr(ctx, err)
		}
		break
	}
	if len(ack) == 0 {
		return ioError(ErrRefused, "connection closed with no ACK bytes (frame rejected)")
	}
	for _, b := range ack {
		if b != 0 {
			return ioError(ErrRefused, "non-zero ACK from unit (likely error): %s", hex.EncodeToString(ack))
		}
	}
	return nil
}
