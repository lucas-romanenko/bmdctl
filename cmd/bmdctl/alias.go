// SPDX-License-Identifier: MIT

package main

// The alias binaries (docs/PORTING.md, "Alias binaries"): bmdctl run as
// atemctl, hdctl, vhctl or ultctl is bmdctl with that device's word in
// front, and its help is that device's alone.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

// aliases are the names bmdctl answers to as one device's command.
var aliases = map[string]string{"atemctl": "atem", "hdctl": "hyperdeck", "vhctl": "videohub", "ultctl": "ultimatte"}

// aliasFor is the device a program name (argv[0]) stands for, and the
// name as typed; "" for bmdctl itself. Case and a .exe suffix are
// ignored, for Windows.
func aliasFor(argv0 string) (device, name string) {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(argv0, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	if d, ok := aliases[base]; ok {
		return d, base
	}
	return "", ""
}

// prog is the program's name as help shows it: the alias, else bmdctl.
func (c *cli) prog() string {
	if c.aliasName != "" {
		return c.aliasName
	}
	return "bmdctl"
}

// usageHead is a device's usage line's start: "bmdctl [hyperdeck]", or
// "hdctl" under the alias.
func (c *cli) usageHead(device string) string {
	if c.alias == device && c.aliasName != "" {
		return c.aliasName
	}
	return "bmdctl [" + device + "]"
}

// aliasArgs is an alias's command line as bmdctl reads it: the device
// word in front of a device command (global flags are read anywhere), a
// built-in left as it is. Nothing but flags, or help alone, is the
// device's help; another device's word is refused.
func (c *cli) aliasArgs(args []string) (out []string, help bool, err error) {
	_, words, tokens, err := splitLine(args)
	if err != nil {
		return args, false, nil // the usual message
	}
	if len(words) == 0 && len(tokens) == 0 || len(words) == 1 && words[0] == "help" {
		return nil, true, nil
	}
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	switch {
	case first == "__complete":
		// Completion under the alias completes the device's line.
		i := slices.Index(args, "__complete")
		return append(append(slices.Clone(args[:i+1]), c.alias), args[i+1:]...), false, nil
	case slices.ContainsFunc(builtins, func(b struct{ name, summary string }) bool { return b.name == first }):
		return args, false, nil
	case first == c.alias:
		return args, false, nil
	case slices.Contains([]string{"atem", "hyperdeck", "videohub", "ultimatte"}, first):
		return nil, false, exitf(exitUsage, "%s runs %s commands; bmdctl %s ... runs %s %s's",
			c.aliasName, engine.DeviceNames[c.alias], first, engine.DeviceArticle(first), engine.DeviceNames[first])
	}
	return append([]string{c.alias}, args...), false, nil
}

// aliasHelp is help under an alias: that device's commands, and the
// flags.
func (c *cli) aliasHelp() int {
	if !slices.Contains(c.reg.Devices(), c.alias) {
		fmt.Fprintf(c.stdout, "%s: bmdctl has no %s commands yet; %s connect ADDRESS saves one to hosts.yml\n",
			c.aliasName, engine.DeviceNames[c.alias], c.aliasName)
		return exitOK
	}
	c.groupHelp(c.stdout, c.alias, nil)
	return exitOK
}
