// SPDX-License-Identifier: MIT

//go:build !windows

package engine

import (
	"os"
	"os/exec"
	"syscall"
)

// Spawn starts exe with args as a detached background process: its own
// session, no terminal, standard streams on /dev/null, working directory
// "/". It returns once the process has started.
func Spawn(exe string, args, env []string) error {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.Env = env
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap it if it exits while we still run
	return nil
}
