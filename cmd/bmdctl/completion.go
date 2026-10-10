// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// The shell completion scripts ask "bmdctl __complete WORDS..." for the
// candidates for the last word (empty when starting a new one) and add
// what it prints, one per line. A first line of ":files" asks the shell to
// complete file names instead.

const completeFiles = ":files"

const bashCompletion = `# bash completion for bmdctl. Load it with:
#   source <(bmdctl completion bash)
_bmdctl() {
    local cur=${COMP_WORDS[COMP_CWORD]} out line
    out=$(bmdctl __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null) || return
    COMPREPLY=()
    if [[ $out == ":files" ]]; then
        compopt -o filenames 2>/dev/null
        mapfile -t COMPREPLY < <(compgen -f -- "$cur")
        return
    fi
    while IFS= read -r line; do
        [[ -n $line ]] && COMPREPLY+=("$(printf '%q' "$line")")
    done <<< "$out"
}
complete -F _bmdctl bmdctl
`

const zshCompletion = `#compdef bmdctl
# zsh completion for bmdctl. Load it with:
#   source <(bmdctl completion zsh)
# or save it as _bmdctl in a directory on $fpath.
_bmdctl() {
    local -a out
    out=("${(@f)$(bmdctl __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}")
    if [[ ${out[1]} == ":files" ]]; then
        _files
        return
    fi
    compadd -- "${out[@]}"
}
if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
    _bmdctl "$@"
else
    compdef _bmdctl bmdctl
fi
`

const fishCompletion = `# fish completion for bmdctl. Load it with:
#   bmdctl completion fish | source
# or save it as ~/.config/fish/completions/bmdctl.fish.
function __bmdctl_complete
    set -l tokens (commandline -opc)
    set -e tokens[1]
    set -l out (bmdctl __complete $tokens (commandline -ct) 2>/dev/null)
    if test "$out[1]" = ":files"
        __fish_complete_path (commandline -ct)
    else
        printf '%s\n' $out
    end
end
complete -c bmdctl -f -a '(__bmdctl_complete)'
`

func (c *cli) completionCommand(g globals, words []string) int {
	if g.help || len(words) != 1 {
		w, code := c.stdout, exitOK
		if !g.help {
			w, code = c.stderr, exitUsage
		}
		fmt.Fprint(w, "usage: bmdctl completion bash|zsh|fish\n\nPrints a script that completes bmdctl's commands, flags, host names and,\nwhile the engine runs, the switcher's input names.\n")
		return code
	}
	script := map[string]string{"bash": bashCompletion, "zsh": zshCompletion, "fish": fishCompletion}[words[0]]
	if script == "" {
		return c.fail(exitUsage, "bmdctl completion: %q is not bash, zsh or fish", words[0])
	}
	fmt.Fprint(c.stdout, script)
	return exitOK
}

// completeCommand prints the candidates for the last of args.
func (c *cli) completeCommand(args []string) int {
	for _, s := range c.complete(args) {
		fmt.Fprintln(c.stdout, s)
	}
	return exitOK
}

// builtinFlags are the flags of the built-ins that take their own.
var builtinFlags = map[string][]string{
	"engine": {"--log-level"}, "exec": {"--continue"}, "hosts": {"--device"}, "man": {"--dir"},
}

// complete returns the candidates for the last of args, the word being
// typed, given the words before it.
func (c *cli) complete(args []string) []string {
	if len(args) == 0 {
		args = []string{""}
	}
	cur, before := args[len(args)-1], args[:len(args)-1]
	g, words, tokens, _ := splitLine(before)
	prev := ""
	if len(before) > 0 {
		prev = before[len(before)-1]
	}
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	device, path := "atem", words
	if slices.Contains(hosts.Devices, first) {
		device, path = first, words[1:]
		if first != "atem" {
			return nil
		}
	}
	e, prefix, _ := c.reg.Resolve(device, path)

	// A value for the flag before it.
	switch prev {
	case "--host":
		return match(cur, c.hostNames())
	case "--fps", "--timeout":
		return nil
	case "--log-level":
		return match(cur, []string{"debug", "info", "warn", "error"})
	case "--device":
		return match(cur, hosts.Devices)
	case "--dir":
		return []string{completeFiles}
	}
	if e != nil && strings.HasPrefix(prev, "--") && !strings.Contains(prev, "=") {
		if p := param(e, strings.TrimPrefix(prev, "--")); p != nil {
			if p.Kind == table.Bool && strings.HasPrefix(cur, "-") {
				return c.completeFlags(cur, e, first, tokens)
			}
			return c.completeValue(cur, *p, g)
		}
	}

	// A flag.
	if strings.HasPrefix(cur, "-") {
		return c.completeFlags(cur, e, first, tokens)
	}
	if len(tokens) > 0 {
		return nil // past the words: only flags and their values
	}

	// A word.
	switch first {
	case "hosts":
		switch {
		case len(words) == 1:
			return match(cur, []string{"list", "add", "rm", "default"})
		case len(words) == 2 && (words[1] == "rm" || words[1] == "default"):
			return match(cur, c.hostNames())
		}
		return nil
	case "engine":
		if len(words) == 1 {
			return match(cur, []string{"stop", "status"})
		}
		return nil
	case "completion":
		if len(words) == 1 {
			return match(cur, []string{"bash", "zsh", "fish"})
		}
		return nil
	case "exec":
		if len(words) == 1 {
			return []string{completeFiles}
		}
		return nil
	case "version", "man", "help":
		return nil
	}
	if e != nil {
		return nil // a whole command: its flags come next
	}
	var cands []string
	if len(words) == 0 {
		for _, b := range builtins {
			cands = append(cands, b.name)
		}
	}
	for _, ch := range c.reg.Children(device, prefix) {
		cands = append(cands, strings.TrimSuffix(ch[0], " ..."))
	}
	return match(cur, cands)
}

func param(e *table.Entry, name string) *table.Param {
	for i := range e.Params {
		if e.Params[i].Name == name {
			return &e.Params[i]
		}
	}
	return nil
}

// completeFlags offers the global flags and the command's own, less the
// ones already given.
func (c *cli) completeFlags(cur string, e *table.Entry, builtin string, tokens []string) []string {
	var cands []string
	for f := range globalFlags {
		cands = append(cands, "--"+f)
	}
	if e != nil {
		for _, p := range e.Params {
			if !slices.ContainsFunc(tokens, func(t string) bool { return t == "--"+p.Name || strings.HasPrefix(t, "--"+p.Name+"=") }) {
				cands = append(cands, "--"+p.Name)
			}
		}
	}
	cands = append(cands, builtinFlags[builtin]...)
	slices.Sort(cands)
	return match(cur, cands)
}

// completeValue offers a parameter's values: names for an enum, flags
// and bools, the switcher's input names for a source (when the engine is
// running), files for a file.
func (c *cli) completeValue(cur string, p table.Param, g globals) []string {
	switch p.Kind {
	case table.Enum, table.Flags:
		names := p.SortedNames()
		if p.Kind == table.Flags {
			if i := strings.LastIndex(cur, ","); i >= 0 {
				for j, n := range names {
					names[j] = cur[:i+1] + n
				}
			}
		}
		return match(cur, names)
	case table.Bool:
		return match(cur, []string{"true", "false"})
	case table.File, table.OutFile, table.Dir:
		return []string{completeFiles}
	case table.Source:
		return match(cur, c.liveSources(g))
	}
	return nil
}

// hostNames are the names in hosts.yml.
func (c *cli) hostNames() []string {
	f, _, err := c.loadHosts()
	if err != nil {
		return nil
	}
	var out []string
	for _, h := range f.Hosts {
		out = append(out, h.Name)
	}
	return out
}

// liveSources asks a running engine for the target switcher's input
// names (never starting one), within a second.
func (c *cli) liveSources(g globals) []string {
	t, err := c.resolveTarget(g)
	if err != nil || t.Device != "" && t.Device != "atem" {
		return nil
	}
	conn, err := c.dialRunning()
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, time.Second)
	defer cancel()
	resp, err := engine.Call(ctx, conn, engine.Request{Device: "atem", Host: t.Address, Path: "input list"},
		func([]byte, *engine.FileFrame, *engine.Response) error { return nil })
	if err != nil || !resp.OK {
		return nil
	}
	var ins []engine.InputEntry
	json.Unmarshal(resp.Result.(json.RawMessage), &ins)
	var out []string
	for _, in := range ins {
		out = append(out, in.Long)
	}
	return out
}

// match keeps the candidates that start with cur, ignoring case.
func match(cur string, cands []string) []string {
	var out []string
	for _, s := range cands {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(cur)) {
			out = append(out, s)
		}
	}
	return out
}
