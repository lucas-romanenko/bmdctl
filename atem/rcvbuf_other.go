// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

//go:build !unix

package atem

import "net"

// readBuffer can't say on this system (atemwire's getsockopt there is a
// different call); nothing is reported.
func readBuffer(*net.UDPConn) (int, bool) { return 0, false }
