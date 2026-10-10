// SPDX-License-Identifier: MIT

//go:build !windows

package main

import (
	"io/fs"
	"syscall"
)

// statUID is a file's owner.
func statUID(fi fs.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
