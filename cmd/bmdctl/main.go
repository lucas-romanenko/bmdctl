// SPDX-License-Identifier: MIT

// Command bmdctl controls Blackmagic Design devices from the command line.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "bmdctl: no commands yet")
	os.Exit(2)
}
