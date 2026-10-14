// SPDX-License-Identifier: MIT

// Package paths says where bmdctl keeps its files: the hosts file, the
// engine's socket and log, the CLI's small state and the engine's cache.
// Every function takes the environment as a function, so tests can point
// it anywhere.
//
// On Unix (macOS included) the XDG base directories apply: configuration
// under $XDG_CONFIG_HOME (~/.config), state and logs under
// $XDG_STATE_HOME (~/.local/state), the cache under $XDG_CACHE_HOME
// (~/.cache), the socket in $XDG_RUNTIME_DIR. On Windows configuration
// goes under %AppData%, state and the cache under %LocalAppData%, and the
// engine listens on a named pipe.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Env looks up an environment variable; os.Getenv is one.
type Env func(string) string

// SocketEnv names the variable that overrides the engine's socket path (a
// pipe name on Windows), for running the engine in a container or under a
// service manager.
const SocketEnv = "BMDCTL_SOCKET"

func home(env Env) (string, error) {
	h := env("HOME")
	if runtime.GOOS == "windows" {
		h = env("USERPROFILE")
	}
	if h == "" {
		return "", errors.New("no home directory: set HOME")
	}
	return h, nil
}

// xdg returns $name, or home/def when it is unset or not absolute (as
// the XDG spec says to ignore a relative path).
func xdg(env Env, name, def string) (string, error) {
	if d := env(name); d != "" && filepath.IsAbs(d) {
		return d, nil
	}
	h, err := home(env)
	if err != nil {
		return "", fmt.Errorf("%w or %s", err, name)
	}
	return filepath.Join(h, def), nil
}

// Config is the directory holding hosts.yml.
func Config(env Env) (string, error) {
	if runtime.GOOS == "windows" && env("XDG_CONFIG_HOME") == "" {
		if d := env("AppData"); d != "" {
			return filepath.Join(d, "bmdctl"), nil
		}
	}
	d, err := xdg(env, "XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "bmdctl"), nil
}

// State is the directory holding the engine's log and the last host used.
func State(env Env) (string, error) {
	if runtime.GOOS == "windows" && env("XDG_STATE_HOME") == "" {
		if d := env("LocalAppData"); d != "" {
			return filepath.Join(d, "bmdctl"), nil
		}
	}
	d, err := xdg(env, "XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "bmdctl"), nil
}

// Cache is the directory for what the engine keeps only to save work: the
// media pool's stills (internal/stillcache), in its stills folder.
func Cache(env Env) (string, error) {
	if runtime.GOOS == "windows" && env("XDG_CACHE_HOME") == "" {
		if d := env("LocalAppData"); d != "" {
			return filepath.Join(d, "bmdctl", "cache"), nil
		}
	}
	d, err := xdg(env, "XDG_CACHE_HOME", ".cache")
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "bmdctl"), nil
}

// Socket is where the engine listens: $BMDCTL_SOCKET if set, else
// $XDG_RUNTIME_DIR/bmdctl.sock. Without XDG_RUNTIME_DIR (macOS, most
// containers) it is bmdctl-<uid>/bmdctl.sock in the temporary directory,
// a directory only the user may enter. On Windows it is the named pipe
// \\.\pipe\bmdctl-<user>.
func Socket(env Env) string {
	if s := env(SocketEnv); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		u := env("USERNAME")
		if u == "" {
			u = "user"
		}
		return `\\.\pipe\bmdctl-` + strings.ToLower(u)
	}
	if d := env("XDG_RUNTIME_DIR"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "bmdctl.sock")
	}
	return filepath.Join(tempDir(env), fmt.Sprintf("bmdctl-%d", os.Getuid()), "bmdctl.sock")
}

func tempDir(env Env) string {
	if d := env("TMPDIR"); d != "" {
		return d
	}
	return "/tmp"
}

// IsPipe reports whether a socket path names a Windows named pipe.
func IsPipe(path string) bool { return strings.HasPrefix(path, `\\.\pipe\`) }
