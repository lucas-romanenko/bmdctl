// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// registry builds the command table from every device package.
func registry() *table.Registry {
	r := table.New()
	if err := atem.RegisterCommands(r); err != nil {
		panic(err) // a malformed table is a programming error
	}
	return r
}

const topUsage = `usage: bmdctl <command> [arguments]

Commands:
  version   print bmdctl's version
  atem      control an ATEM switcher (bmdctl atem --help)
`

// run is main without the process: it takes the arguments after the
// program name and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, topUsage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, topUsage)
		return exitOK
	case "version", "--version":
		fmt.Fprintln(stdout, versionString())
		return exitOK
	case "atem":
		return runDevice(registry(), "atem", args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "bmdctl: unknown command %q\n\n%s", args[0], topUsage)
	return exitUsage
}

func versionString() string {
	v, c, d := version, commit, date
	if v == "" {
		v = "(devel)"
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" {
				v = bi.Main.Version
			}
			for _, s := range bi.Settings {
				switch {
				case s.Key == "vcs.revision" && c == "":
					c = s.Value[:min(12, len(s.Value))]
				case s.Key == "vcs.time" && d == "":
					d = s.Value
				}
			}
		}
	}
	var extra []string
	for _, s := range []string{c, d} {
		if s != "" {
			extra = append(extra, s)
		}
	}
	if len(extra) == 0 {
		return "bmdctl " + v
	}
	return fmt.Sprintf("bmdctl %s (%s)", v, strings.Join(extra, ", "))
}

// invocation is a device command line, parsed.
type invocation struct {
	dryRun bool
	help   bool
	fps    int
	words  []string          // the command's path
	params map[string]string // --name value
}

// parseInvocation splits "[--dry-run] [--fps N] words... [--name value...]".
// --dry-run, --fps and --help are accepted anywhere.
func parseInvocation(args []string) (invocation, error) {
	inv := invocation{fps: atem.FallbackFPS, params: map[string]string{}}
	flagsStarted := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if flagsStarted {
				return inv, fmt.Errorf("unexpected %q after the flags", a)
			}
			inv.words = append(inv.words, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch name {
		case "h", "help":
			inv.help = true
			continue
		case "dry-run":
			inv.dryRun = true
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return inv, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if name == "fps" {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return inv, fmt.Errorf("--fps %q: not a positive integer", value)
			}
			inv.fps = n
			continue
		}
		if len(inv.words) > 0 {
			flagsStarted = true
		}
		if _, dup := inv.params[name]; dup {
			return inv, fmt.Errorf("--%s given twice", name)
		}
		inv.params[name] = value
	}
	return inv, nil
}

func runDevice(r *table.Registry, device string, args []string, stdout, stderr io.Writer) int {
	inv, err := parseInvocation(args)
	if err != nil {
		fmt.Fprintf(stderr, "bmdctl %s: %v\n", device, err)
		return exitUsage
	}
	e, prefix, rest := r.Resolve(device, inv.words)
	if e == nil {
		w := stdout
		code := exitOK
		if !inv.help {
			w, code = stderr, exitUsage
			switch {
			case len(rest) > 0:
				fmt.Fprintf(stderr, "bmdctl %s: unknown command %q\n\n", device, strings.Join(inv.words, " "))
			case len(prefix) > 0:
				fmt.Fprintf(stderr, "bmdctl %s %s: missing a subcommand\n\n", device, strings.Join(prefix, " "))
			}
		}
		groupHelp(w, r, device, prefix)
		return code
	}
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "bmdctl %s %s: unexpected %q\n", device, e.Path, strings.Join(rest, " "))
		return exitUsage
	}
	if inv.help {
		entryHelp(stdout, device, *e)
		return exitOK
	}
	a, err := e.Parse(inv.params, func(s string) int { return atem.ParseRate(s, inv.fps) })
	if err != nil {
		fmt.Fprintf(stderr, "bmdctl %s %s: %v\n", device, e.Path, strings.TrimPrefix(err.Error(), "usage: "))
		return exitUsage
	}
	cmd, err := e.Build(a)
	if err == nil {
		if v, ok := cmd.(atem.Validator); ok {
			err = v.Validate()
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "bmdctl %s %s: %v\n", device, e.Path, strings.TrimPrefix(err.Error(), "usage: "))
		if errors.Is(err, table.ErrUsage) {
			return exitUsage
		}
		return exitError
	}
	if e.Destructive {
		fmt.Fprintf(stderr, "bmdctl: %s %s is destructive\n", device, e.Path)
	}
	if e.Unverified {
		fmt.Fprintf(stderr, "bmdctl: %s %s is unverified on hardware\n", device, e.Path)
	}
	if !inv.dryRun {
		fmt.Fprintf(stderr, "bmdctl %s: sending to a switcher comes in a later phase; add --dry-run to print the bytes\n", device)
		return exitUsage
	}
	fmt.Fprintf(stdout, "%x\n", cmd.Encode())
	return exitOK
}

func groupHelp(w io.Writer, r *table.Registry, device string, prefix []string) {
	path := strings.TrimSpace(device + " " + strings.Join(prefix, " "))
	fmt.Fprintf(w, "usage: bmdctl %s [--dry-run] [--fps N] <command> [--parameter value ...]\n\n", path)
	if len(prefix) == 0 {
		fmt.Fprintf(w, "Flags:\n")
		fmt.Fprintf(w, "  --dry-run  print the command's wire bytes and send nothing\n")
		fmt.Fprintf(w, "  --fps N    display frame rate for rates given as seconds:frames (default %d)\n\n", atem.FallbackFPS)
	}
	fmt.Fprintf(w, "Commands:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range r.Children(device, prefix) {
		name, summary := c[0], c[1]
		if group, ok := strings.CutSuffix(name, " ..."); ok {
			var subs []string
			for _, sub := range r.Children(device, append(slices.Clone(prefix), group)) {
				subs = append(subs, strings.TrimSuffix(sub[0], " ..."))
			}
			summary = strings.Join(subs, ", ")
		}
		fmt.Fprintf(tw, "  %s\t%s\n", name, summary)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nRun 'bmdctl %s <command> --help' for a command's parameters.\n", path)
}

func entryHelp(w io.Writer, device string, e table.Entry) {
	fmt.Fprintf(w, "usage: bmdctl %s [--dry-run] %s", device, e.Path)
	for _, p := range e.Params {
		if p.Required {
			fmt.Fprintf(w, " --%s <%s>", p.Name, p.Kind)
		}
	}
	if len(e.Params) > 0 {
		fmt.Fprint(w, " [--parameter value ...]")
	}
	fmt.Fprintf(w, "\n\n%s\n", e.Summary)
	if e.Destructive {
		fmt.Fprintln(w, "Destructive: hard to undo, or drops connections.")
	}
	if e.Unverified {
		fmt.Fprintln(w, "Unverified: the wire format is pinned but unconfirmed on a switcher.")
	}
	if e.Echo != "" {
		fmt.Fprintf(w, "Echo: %s\n", e.Echo)
	}
	if len(e.Params) == 0 {
		return
	}
	fmt.Fprintf(w, "\nParameters:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range e.Params {
		need := ""
		switch {
		case p.Required:
			need = "required"
		case p.Default != "":
			need = "default " + p.Default
		}
		fmt.Fprintf(tw, "  --%s\t%s\t%s\t%s\n", p.Name, paramValue(p), need, p.Help)
	}
	tw.Flush()
	if e.AtLeastOne {
		fmt.Fprintln(w, "\nGive at least one of the optional parameters.")
	}
	for _, p := range e.Params {
		if len(p.Names) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n--%s values:\n", p.Name)
		line := " "
		for _, n := range sortedNames(p) {
			item := fmt.Sprintf(" %s=%d", n, p.Names[n])
			if len(line)+len(item) > 78 {
				fmt.Fprintln(w, line)
				line = " "
			}
			line += item
		}
		fmt.Fprintln(w, line)
	}
}

func paramValue(p table.Param) string {
	parts := []string{p.Kind.String()}
	if r := p.Range(); r != "" {
		parts = append(parts, r)
	}
	if p.Unit != "" {
		parts = append(parts, p.Unit)
	}
	return strings.Join(parts, ", ")
}

func sortedNames(p table.Param) []string {
	type nv struct {
		n string
		v uint64
	}
	var ns []nv
	for n, v := range p.Names {
		ns = append(ns, nv{n, v})
	}
	for i := range ns {
		for j := i + 1; j < len(ns); j++ {
			if ns[j].v < ns[i].v || ns[j].v == ns[i].v && ns[j].n < ns[i].n {
				ns[i], ns[j] = ns[j], ns[i]
			}
		}
	}
	var out []string
	for _, x := range ns {
		out = append(out, x.n)
	}
	return out
}
