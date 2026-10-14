// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// globals are the flags every command line accepts, anywhere on it.
type globals struct {
	host    string
	hostSet bool
	// hostWord: host came from the line's first word (bmdctl HOST ...),
	// not --host, so it doesn't become the last host used.
	hostWord bool
	json     bool
	quiet    bool
	yes      bool
	dryRun   bool
	help     bool
	noColor  bool
	stamp    bool // -t: results start with the time of day, at a terminal
	fps      int  // 0: the switcher's
	timeout  time.Duration
}

// globalFlags maps each global flag (--name) to whether it takes a value.
// A command's parameters can't use these names.
var globalFlags = map[string]bool{
	"host": true, "fps": true, "timeout": true,
	"json": false, "quiet": false, "yes": false, "dry-run": false, "help": false, "no-color": false, "timestamps": false,
}

// shortFlags are the one-letter forms (-q), which never clash with a
// parameter: parameters are always --name.
var shortFlags = map[string]string{"q": "quiet", "y": "yes", "h": "help", "t": "timestamps"}

// splitLine separates a command line into the global flags, the leading
// words (the command's path, and a built-in's arguments) and the
// command's own flag tokens, which start at the first flag that isn't
// global. Global flags are taken out wherever they are.
func splitLine(args []string) (g globals, words, tokens []string, err error) {
	inFlags := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// Everything after is words: lets a built-in take "-" or a
			// name starting with a dash.
			words = append(words, args[i+1:]...)
			break
		}
		var name, value string
		var hasValue, global, takesValue bool
		switch {
		case strings.HasPrefix(a, "--"):
			name, value, hasValue = strings.Cut(a[2:], "=")
			takesValue, global = globalFlags[name]
		case strings.HasPrefix(a, "-") && len(a) == 2:
			name, global = shortFlags[a[1:]]
		}
		if !global {
			if strings.HasPrefix(a, "-") && a != "-" && !isNumber(a) {
				inFlags = true
			}
			if inFlags {
				tokens = append(tokens, a)
			} else {
				words = append(words, a)
			}
			continue
		}
		if takesValue && !hasValue {
			if i+1 == len(args) {
				return g, nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		} else if !takesValue && hasValue {
			return g, nil, nil, fmt.Errorf("--%s takes no value", name)
		}
		if err := g.set(name, value); err != nil {
			return g, nil, nil, err
		}
	}
	return g, words, tokens, nil
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func (g *globals) set(name, value string) error {
	switch name {
	case "host":
		if value == "" {
			return errors.New("--host needs a value")
		}
		g.host, g.hostSet = value, true
	case "fps":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 || n > 120 {
			return fmt.Errorf("--fps %q: not a frame rate from 1 to 120", value)
		}
		g.fps = n
	case "timeout":
		d, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("--timeout %q: %v", value, err)
		}
		g.timeout = d
	case "json":
		g.json = true
	case "quiet":
		g.quiet = true
	case "yes":
		g.yes = true
	case "dry-run":
		g.dryRun = true
	case "help":
		g.help = true
	case "no-color":
		g.noColor = true
	case "timestamps":
		g.stamp = true
	}
	return nil
}

// parseDuration reads a Go duration ("2s", "500ms") or plain seconds.
func parseDuration(s string) (time.Duration, error) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		s = fmt.Sprint(f) + "s"
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errors.New("not a positive duration like 2s or 500ms")
	}
	return d, nil
}
