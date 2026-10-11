// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
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
# Indexes (M/Es, keyers, DSKs, aux, media and macro slots, players) are
# offered counted from 1, as bmdctl takes them.
_bmdctl() {
    local cur=${COMP_WORDS[COMP_CWORD]} out line
    out=$("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null) || return
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
complete -F _bmdctl bmdctl atemctl hdctl vhctl ultctl
`

const zshCompletion = `#compdef bmdctl atemctl hdctl vhctl ultctl
# zsh completion for bmdctl. Load it with:
#   source <(bmdctl completion zsh)
# or save it as _bmdctl in a directory on $fpath. Indexes (M/Es, keyers,
# DSKs, aux, media and macro slots, players) are offered counted from 1,
# as bmdctl takes them.
_bmdctl() {
    local -a out
    out=("${(@f)$(${words[1]} __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}")
    if [[ ${out[1]} == ":files" ]]; then
        _files
        return
    fi
    compadd -- "${out[@]}"
}
if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
    _bmdctl "$@"
else
    compdef _bmdctl bmdctl atemctl hdctl vhctl ultctl
fi
`

const fishCompletion = `# fish completion for bmdctl. Load it with:
#   bmdctl completion fish | source
# or save it as ~/.config/fish/completions/bmdctl.fish. Indexes (M/Es,
# keyers, DSKs, aux, media and macro slots, players) are offered counted
# from 1, as bmdctl takes them.
function __bmdctl_complete
    set -l tokens (commandline -opc)
    set -l prog $tokens[1]
    set -e tokens[1]
    set -l out ($prog __complete $tokens (commandline -ct) 2>/dev/null)
    if test "$out[1]" = ":files"
        __fish_complete_path (commandline -ct)
    else
        printf '%s\n' $out
    end
end
for prog in bmdctl atemctl hdctl vhctl ultctl
    complete -c $prog -f -a '(__bmdctl_complete)'
end
`

func (c *cli) completionCommand(g globals, words []string) int {
	if g.help || len(words) != 1 {
		w, code := c.stdout, exitOK
		if !g.help {
			w, code = c.stderr, exitUsage
		}
		fmt.Fprint(w, "usage: bmdctl completion bash|zsh|fish\n\nPrints a script that completes bmdctl's commands, flags, host names and,\nwhile the engine runs, the switcher's input names and the indexes it has,\ncounted from 1.\n")
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
	"engine": {"--log-level"}, "exec": {"--continue"}, "hosts": {"--as", "--device", "--for"}, "man": {"--dir"},
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
	// The device: named, else the target host's type, as run finds it.
	device, path := "atem", words
	if slices.Contains(hosts.Devices, first) {
		device, path = first, words[1:]
	} else if t, err := c.resolveTarget(g); err == nil && t.Device != "" {
		device = t.Device
	}
	if !slices.Contains(c.reg.Devices(), device) {
		return nil
	}
	e, _, err := c.reg.ParseLine(device, path, tokens)
	var nc *table.NoCommand
	if errors.As(err, &nc) {
		e = nil
	}

	// A value for the flag before it.
	switch prev {
	case "--host":
		return match(cur, c.hostNames())
	case "--fps", "--timeout", "--as", "--for":
		return nil
	case "--log-level":
		return match(cur, []string{"debug", "info", "warn", "error"})
	case "--device":
		return match(cur, hosts.Devices)
	case "--dir":
		return []string{completeFiles}
	}
	if e != nil && strings.HasPrefix(prev, "--") && !strings.Contains(prev, "=") {
		if p, err := e.FlagParam(strings.TrimPrefix(prev, "--")); err == nil {
			if p.Kind == table.Bool && strings.HasPrefix(cur, "-") {
				return c.completeFlags(cur, e, first, tokens)
			}
			return c.completeValue(cur, e, *p, g, tokens)
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
			return match(cur, []string{"list", "add", "rm", "default", "discover"})
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
	case "version", "man", "doctor", "help":
		return nil
	}
	// The next word of a command, or the value of its next argument.
	var cands []string
	if len(words) == 0 {
		for _, b := range builtins {
			cands = append(cands, b.name)
		}
	}
	next, _ := c.reg.Next(device, path)
	for _, x := range next {
		if x.Word != "" {
			cands = append(cands, x.Word)
			continue
		}
		for _, p := range x.Entry.Params {
			if p.Name == x.Param {
				vals := c.completeValue(cur, x.Entry, p, g, tokens)
				if slices.Contains(vals, completeFiles) {
					return []string{completeFiles}
				}
				cands = append(cands, vals...)
			}
		}
	}
	slices.Sort(cands)
	return match(cur, slices.Compact(cands))
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
			// Positional arguments' --name form is accepted but not shown.
			if !e.Positional(p.Name) && !slices.ContainsFunc(tokens, func(t string) bool { return t == "--"+p.Name || strings.HasPrefix(t, "--"+p.Name+"=") }) {
				cands = append(cands, "--"+p.Name)
			}
		}
	}
	cands = append(cands, builtinFlags[builtin]...)
	slices.Sort(cands)
	return match(cur, cands)
}

// completeValue offers a parameter's values: names for an enum, flags
// and bools, files for a file, and from a running engine the switcher's
// input names for a source and the indexes it has, counted from 1.
func (c *cli) completeValue(cur string, e *table.Entry, p table.Param, g globals, tokens []string) []string {
	if p.OneBased || p.Name == "slot" && p.Kind == table.String {
		return match(cur, c.liveIndexes(e, p, g, tokens))
	}
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
		return match(cur, []string{"on", "off", "true", "false"})
	case table.File, table.OutFile, table.Dir:
		return []string{completeFiles}
	case table.Source:
		return match(cur, c.liveSources(g))
	}
	return nil
}

// hostNames are the names the hosts in hosts.yml go by.
func (c *cli) hostNames() []string {
	f, _, err := c.loadHosts()
	if err != nil {
		return nil
	}
	var out []string
	for _, h := range f.Hosts {
		out = append(out, h.Label())
	}
	return out
}

// ask runs a read-only request for the target switcher on a running
// engine (never starting one), within a second, and decodes its result
// into v.
func (c *cli) ask(g globals, path string, params map[string]string, v any) bool {
	t, err := c.resolveTarget(g)
	if err != nil || t.Device != "" && t.Device != "atem" {
		return false
	}
	conn, err := c.dialRunning()
	if err != nil {
		return false
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(c.ctx, time.Second)
	defer cancel()
	resp, err := engine.Call(ctx, conn, engine.Request{Device: "atem", Host: t.Address, Path: path, Params: params, Build: c.build()}, nil)
	if err != nil || !resp.OK {
		return false
	}
	return json.Unmarshal(resp.Result.(json.RawMessage), v) == nil
}

// liveIndexes are the values an index parameter can take on the target
// switcher, counted from 1: every ATEM's two colour generators, four
// SuperSource boxes and six EQ bands, and from a running engine as many
// M/Es, keyers, DSKs, aux outputs, media players, still or clip slots,
// SuperSources, multiviewers and HyperDeck slots as it has, and the macro
// slots it holds (with their names where a name will do).
func (c *cli) liveIndexes(e *table.Entry, p table.Param, g globals, tokens []string) []string {
	upTo := func(n int) []string {
		var out []string
		for i := range n {
			out = append(out, table.Index(i).String())
		}
		return out
	}
	switch p.Name {
	case "generator", "box", "band":
		return upTo(int(*p.Max) + 1)
	}
	if strings.HasPrefix(e.Path, "macro ") && p.Name == "slot" {
		var ml engine.MacroList
		if !c.ask(g, "macro list", map[string]string{"all": "true"}, &ml) {
			return nil
		}
		var out []string
		for _, m := range ml.Macros {
			if !m.Used && p.Kind == table.String {
				continue // run, delete and download name a macro there is
			}
			out = append(out, m.Slot.String())
			if p.Kind == table.String && m.Name != "" {
				out = append(out, m.Name)
			}
		}
		return out
	}
	var info engine.InfoResult
	if !c.ask(g, "info", nil, &info) {
		return nil
	}
	switch p.Name {
	case "me":
		return upTo(info.MEs)
	case "keyer":
		me := 0
		for i := 0; i+1 < len(tokens); i++ {
			if tokens[i] == "--me" {
				if x, err := table.ParseIndex(tokens[i+1]); err == nil {
					me = int(x)
				}
			}
		}
		if me < len(info.Keyers) {
			return upTo(info.Keyers[me])
		}
	case "dsk":
		return upTo(info.DSKs)
	case "aux":
		return upTo(info.Aux)
	case "player":
		return upTo(info.MediaPlayers)
	case "slot", "still":
		if e.Path == "hyperdeck-slot" {
			return upTo(info.HyperDecks)
		}
		return upTo(info.Stills)
	case "supersource":
		return upTo(info.SuperSources)
	case "multiviewer":
		return upTo(info.Multiviewers)
	case "clip":
		return upTo(info.Clips)
	}
	return nil
}

// liveSources are the target switcher's input names, from a running
// engine.
func (c *cli) liveSources(g globals) []string {
	var ins []engine.InputEntry
	if !c.ask(g, "inputs", nil, &ins) {
		return nil
	}
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
