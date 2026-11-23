// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

//go:build unix

package atem

import (
	"net"
	"runtime"
	"syscall"
)

// readBuffer is the receive buffer the system granted uc, in bytes: Linux
// reports double what it grants, so it is halved there, as atemwire does.
func readBuffer(uc *net.UDPConn) (int, bool) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var got int
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		got, gerr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	}); err != nil || gerr != nil {
		return 0, false
	}
	if runtime.GOOS == "linux" {
		got /= 2
	}
	return got, true
}
