// SPDX-License-Identifier: MIT

//go:build windows

package main

import "io/fs"

// statUID: Windows files have no Unix owner.
func statUID(fs.FileInfo) (int, bool) { return 0, false }
