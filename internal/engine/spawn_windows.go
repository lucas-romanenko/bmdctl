// SPDX-License-Identifier: MIT

//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess        = 0x00000008
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

// Spawn starts exe with args as a detached background process with no
// console. It asks to leave the caller's job object, so closing the
// terminal doesn't end it, and starts it within the job if that is not
// allowed.
func Spawn(exe string, args, env []string) error {
	start := func(flags uint32) error {
		cmd := exec.Command(exe, args...)
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	if err := start(detachedProcess | createNewProcessGroup | createBreakawayFromJob); err == nil {
		return nil
	}
	return start(detachedProcess | createNewProcessGroup)
}
