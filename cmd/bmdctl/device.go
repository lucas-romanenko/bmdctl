// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// runDevice runs one of a device's commands.
func (c *cli) runDevice(g globals, device string, words, tokens []string) int {
	e, prefix, rest := c.reg.Resolve(device, words)
	if e == nil {
		w, code := c.stdout, exitOK
		if !g.help {
			w, code = c.stderr, exitUsage
			switch {
			case len(rest) > 0:
				fmt.Fprintf(c.stderr, "bmdctl %s: unknown command %q\n\n", device, strings.Join(words, " "))
			case len(prefix) > 0:
				fmt.Fprintf(c.stderr, "bmdctl %s %s: missing a subcommand\n\n", device, strings.Join(prefix, " "))
			}
		}
		c.groupHelp(w, device, prefix)
		return code
	}
	if len(rest) > 0 {
		return c.fail(exitUsage, "bmdctl %s %s: unexpected %q", device, e.Path, strings.Join(rest, " "))
	}
	if g.help {
		c.entryHelp(c.stdout, device, *e)
		return exitOK
	}
	raw, err := parseParams(e, tokens)
	if err == nil {
		err = precheck(e, raw, g)
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s %s: %s", device, e.Path, strings.TrimPrefix(err.Error(), "usage: "))
	}
	if g.dryRun {
		return c.dryRun(device, e, raw, g)
	}
	req := engine.Request{Device: device, Path: e.Path, Params: raw, Yes: g.yes, FPS: g.fps,
		TimeoutMS: g.timeout.Milliseconds()}
	return c.request(g, e, req)
}

// request sends a device command to the engine (or runs a local one) and
// prints what comes back.
func (c *cli) request(g globals, e *table.Entry, req engine.Request) int {
	label := strings.TrimSpace(req.Device + " " + e.Path)
	if e.Destructive && !g.yes {
		return c.fail(exitDestructive, "bmdctl %s: destructive: add --yes to run it", label)
	}
	files, err := c.readFiles(e, req.Params)
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s: %v", label, err)
	}
	req.Files = files
	if e.Unverified && !g.json {
		fmt.Fprintf(c.stderr, "bmdctl: %s is unverified on hardware: atemwire restored it from pyatem without checking it on a switcher\n", label)
	}
	out := &printer{c: c, g: g, e: e, params: req.Params}
	if e.Local {
		resp := engine.RunLocal(c.ctx, c.reg, req, out.file)
		line, _ := json.Marshal(resp)
		out.response(line, resp)
		return out.code()
	}
	t, err := c.resolveTarget(g)
	if errors.Is(err, errNoHost) && !e.Operation {
		err = fmt.Errorf("%w; or add --dry-run to print the bytes", err)
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s: %v", label, err)
	}
	if t.Device != "" && t.Device != req.Device {
		return c.fail(exitUsage, "bmdctl %s: %s is a %s, not an %s", label, t.label(), t.Device, req.Device)
	}
	req.Host = t.Address
	code := c.send(g, e, req, out, label)
	if g.hostSet && code != exitUnreachable {
		c.setLastHost(g.host)
	}
	return code
}

// send sends a request to the engine and prints its replies.
func (c *cli) send(g globals, e *table.Entry, req engine.Request, out *printer, label string) int {
	conn, err := c.connectEngine()
	if err != nil {
		return c.fail(codeFor(err), "bmdctl %s: %v", label, err)
	}
	defer conn.Close()
	_, err = engine.Call(c.ctx, conn, req, func(line []byte, f *engine.FileFrame, r *engine.Response) error {
		if f != nil {
			return out.file(*f)
		}
		out.response(line, *r)
		return nil
	})
	switch {
	case err != nil && c.ctx.Err() != nil && e.Stream:
		return out.code() // interrupted: a stream's normal end
	case err != nil:
		return c.fail(codeFor(err), "bmdctl %s: %v", label, err)
	}
	return out.code()
}

// precheck reads a command line without a switcher: names and rates are
// checked only for their form, and a wire command is built and validated.
func precheck(e *table.Entry, raw map[string]string, g globals) error {
	a, err := e.Parse(raw, atem.Resolver{FPS: g.fps, Offline: true})
	if err != nil || e.Operation {
		return err
	}
	return build(e, a)
}

func build(e *table.Entry, a table.Args) error {
	cmd, err := e.Build(a)
	if err != nil {
		return err
	}
	if v, ok := cmd.(atem.Validator); ok {
		return v.Validate()
	}
	return nil
}

// dryRun prints a wire command's bytes, header included, and sends
// nothing.
func (c *cli) dryRun(device string, e *table.Entry, raw map[string]string, g globals) int {
	label := device + " " + e.Path
	if e.Operation {
		return c.fail(exitUsage, "bmdctl %s: --dry-run prints one command's bytes, and this isn't one command", label)
	}
	fps := g.fps
	if fps == 0 {
		fps = atem.FallbackFPS
	}
	a, err := e.Parse(raw, atem.Resolver{FPS: fps})
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s: %s", label, strings.TrimPrefix(err.Error(), "usage: "))
	}
	cmd, err := e.Build(a)
	if err == nil {
		if v, ok := cmd.(atem.Validator); ok {
			err = v.Validate()
		}
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s: %s", label, strings.TrimPrefix(err.Error(), "usage: "))
	}
	if e.Destructive {
		fmt.Fprintf(c.stderr, "bmdctl: %s is destructive (sending it would need --yes)\n", label)
	}
	if e.Unverified {
		fmt.Fprintf(c.stderr, "bmdctl: %s is unverified on hardware\n", label)
	}
	if !g.quiet {
		fmt.Fprintf(c.stdout, "%x\n", cmd.Encode())
	}
	return exitOK
}

// readFiles reads every File parameter's file ("-" is stdin).
func (c *cli) readFiles(e *table.Entry, raw map[string]string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, p := range e.Params {
		name, ok := raw[p.Name]
		if p.Kind != table.File || !ok {
			continue
		}
		var b []byte
		var err error
		if name == "-" {
			b, err = io.ReadAll(c.stdin)
		} else {
			b, err = os.ReadFile(name)
		}
		if err != nil {
			return nil, err
		}
		files[p.Name] = b
	}
	if len(files) == 0 {
		return nil, nil
	}
	return files, nil
}

// outPath is where a file frame goes: the OutFile parameter's path for
// the file itself, beside it for a named one, or inside a Dir parameter's
// directory. Names can't climb out.
func outPath(e *table.Entry, raw map[string]string, f engine.FileFrame) (string, error) {
	k := slices.IndexFunc(e.Params, func(p table.Param) bool { return p.Name == f.Param })
	if k < 0 || (e.Params[k].Kind != table.OutFile && e.Params[k].Kind != table.Dir) {
		return "", fmt.Errorf("the engine sent a file for --%s", f.Param)
	}
	p := e.Params[k]
	base, ok := raw[p.Name]
	if !ok {
		base = p.Default
	}
	if f.Name == "" {
		if p.Kind == table.Dir {
			return "", fmt.Errorf("the engine sent an unnamed file for --%s", f.Param)
		}
		return base, nil
	}
	clean := path.Clean(f.Name)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(clean, 0) {
		return "", fmt.Errorf("the engine sent a file named %q", f.Name)
	}
	if p.Kind == table.OutFile {
		if base == "-" {
			base = "."
		} else {
			base = filepath.Dir(base)
		}
	}
	return filepath.Join(base, filepath.FromSlash(clean)), nil
}

func (c *cli) groupHelp(w io.Writer, device string, prefix []string) {
	path := strings.Join(prefix, " ")
	fmt.Fprintf(w, "usage: bmdctl [%s] %s<command> [--parameter value ...]\n\n", device, strings.TrimSpace(path+" "))
	fmt.Fprintf(w, "Commands:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, ch := range c.reg.Children(device, prefix) {
		name, summary := ch[0], ch[1]
		if group, ok := strings.CutSuffix(name, " ..."); ok {
			var subs []string
			for _, sub := range c.reg.Children(device, append(slices.Clone(prefix), group)) {
				subs = append(subs, strings.TrimSuffix(sub[0], " ..."))
			}
			summary = strings.Join(subs, ", ")
		}
		fmt.Fprintf(tw, "  %s\t%s\n", name, summary)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nRun 'bmdctl %s %s<command> --help' for a command's parameters.\n", device, strings.TrimSpace(path+" "))
	if len(prefix) == 0 {
		fmt.Fprintln(w)
		globalHelp(w)
	}
}

func (c *cli) entryHelp(w io.Writer, device string, e table.Entry) {
	fmt.Fprintf(w, "usage: bmdctl [%s] %s", device, e.Path)
	for _, p := range e.Params {
		if p.Required {
			fmt.Fprintf(w, " --%s <%s>", p.Name, p.Kind)
		}
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return !p.Required }) {
		fmt.Fprint(w, " [--parameter value ...]")
	}
	fmt.Fprintf(w, "\n\n%s\n", e.Summary)
	if e.Destructive {
		fmt.Fprintln(w, "Destructive: hard to undo, or drops connections. Needs --yes.")
	}
	if e.Unverified {
		fmt.Fprintln(w, "Unverified: the wire format is pinned but unconfirmed on a switcher.")
	}
	if e.Local {
		fmt.Fprintln(w, "Runs in the CLI; needs no switcher.")
	}
	if e.Echo != "" {
		fmt.Fprintf(w, "Echo: %s\n", e.Echo)
	}
	if len(e.Params) > 0 {
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
			for _, n := range p.SortedNames() {
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
	var notes []string
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.OneBased }) {
		notes = append(notes, "--me counts M/Es from 1 here; the atem library's ME field counts from 0.")
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Kind == table.Source }) {
		notes = append(notes, "A source is an id or one of the switcher's input names, long or short, in any case.")
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Kind == table.Rate }) {
		notes = append(notes, "A rate is a frame count or seconds:frames at the switcher's display rate.")
	}
	if len(notes) > 0 {
		fmt.Fprintf(w, "\n%s\n", strings.Join(notes, "\n"))
	}
}

func paramValue(p table.Param) string {
	parts := []string{p.Kind.String()}
	if r := p.Range(); r != "" {
		parts = append(parts, r)
	}
	if p.Kind == table.OutFile {
		parts = append(parts, "written")
	}
	if p.Unit != "" {
		parts = append(parts, p.Unit)
	}
	return strings.Join(parts, ", ")
}

var errNoEngine = errors.New("no engine")
