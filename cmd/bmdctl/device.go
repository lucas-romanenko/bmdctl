// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/profile"
	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// runDevice runs one of a device's commands.
func (c *cli) runDevice(g globals, device string, words, tokens []string) int {
	e, raw, err := c.reg.ParseLine(device, words, tokens)
	var nc *table.NoCommand
	if errors.As(err, &nc) {
		if g.help {
			c.groupHelp(c.stdout, device, nc.Prefix)
			return exitOK
		}
		msg := fmt.Sprintf("bmdctl %s: %v", device, nc)
		if c.json {
			return c.fail(exitUsage, "%s", msg)
		}
		fmt.Fprintf(c.stderr, "%s\n\n", msg)
		c.groupHelp(c.stderr, device, nc.Prefix)
		return exitUsage
	}
	if g.help {
		c.entryHelp(c.stdout, device, *e)
		return exitOK
	}
	if err == nil {
		c.noticeOldForm(e, raw, tokens)
		err = precheck(e, raw, g)
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s %s: %s", device, e.Path, strings.TrimPrefix(err.Error(), "usage: "))
	}
	if g.dryRun {
		return c.dryRun(device, e, raw, g)
	}
	if a := raw["address"]; e.Path == "connect" && a != "" {
		// connect ADDRESS goes to that switcher, as --host would.
		g.host, g.hostSet = a, true
	}
	req := engine.Request{Device: device, Path: e.Path, Params: raw, Yes: g.yes, FPS: g.fps,
		TimeoutMS: g.timeout.Milliseconds()}
	return c.request(g, e, req)
}

// noticeOldForm says, once per invocation, that a line gives a positional
// argument in the --name form, and how the line is written now.
func (c *cli) noticeOldForm(e *table.Entry, raw map[string]string, tokens []string) {
	flags, line := e.Deprecated(raw, tokens)
	if len(flags) == 0 || c.noticed {
		return
	}
	c.noticed = true
	names, verb := flags[0], "is"
	if n := len(flags); n > 1 {
		names, verb = strings.Join(flags[:n-1], ", ")+" and "+flags[n-1], "are"
	}
	fmt.Fprintf(c.stderr, "bmdctl: %s %s deprecated, use: %s\n", names, verb, line)
}

// request sends a device command to the engine (or runs a local one) and
// prints what comes back.
func (c *cli) request(g globals, e *table.Entry, req engine.Request) int {
	label := strings.TrimSpace(req.Device + " " + e.Path)
	if e.Destructive && !g.yes {
		if c.terminal() {
			c.screenError(danger(e), termStep(exitDestructive))
			return exitDestructive
		}
		return c.fail(exitDestructive, "bmdctl %s: destructive: add --yes to run it", label)
	}
	files, err := c.readFiles(e, req.Params)
	if err == nil && prepare[e.Path] != nil {
		err = prepare[e.Path](req.Params, files)
	}
	if err != nil {
		return c.fail(exitUsage, "bmdctl %s: %v", label, err)
	}
	req.Files = files
	if e.Unverified && !g.json {
		c.unverified(label)
	}
	out := &printer{c: c, g: g, e: e, params: req.Params, typed: req.Params}
	if _, all := req.Params["all"]; c.terminal() && e.Path == "macro list" && !all {
		// A terminal counts the empty slots it hides.
		req.Params = maps.Clone(req.Params)
		if req.Params == nil {
			req.Params = map[string]string{}
		}
		req.Params["all"] = "true"
		out.params = req.Params
	}
	if !e.Local {
		var err error
		if out.t, err = c.resolveTarget(g); err != nil {
			if errors.Is(err, errNoHost) && !e.Operation {
				err = fmt.Errorf("%w; or add --dry-run to print the bytes", err)
			}
			return c.fail(exitUsage, "bmdctl %s: %v", label, err)
		}
	}
	if e.Local {
		// Run it here, then read the response back as if it had come
		// from an engine, so it prints the same way.
		line, _ := json.Marshal(engine.RunLocal(c.ctx, c.reg, req, out.file))
		var resp engine.Response
		json.Unmarshal(line, &resp)
		out.response(line, resp)
		return out.code()
	}
	t := out.t
	if t.Device != "" && t.Device != req.Device {
		return c.fail(exitUsage, "bmdctl %s: %s is a %s, not an %s", label, t.label(), t.Device, req.Device)
	}
	req.Host = t.Address
	code := c.send(g, e, req, out, label)
	if g.hostSet && code != exitUnreachable && !c.noRemember {
		// The host by the name it goes by, not the start of it typed,
		// which a host added later could make ambiguous.
		last := g.host
		if out.t.Host != nil {
			last = out.t.Host.Label()
		}
		c.setLastHost(last)
	}
	return code
}

// errStale ends a call answered by an engine of another build.
var errStale = errors.New("the engine runs another build")

// send sends a request to the engine and prints its replies. At a
// terminal the spinner shows while it waits, until the response takes
// its place.
func (c *cli) send(g globals, e *table.Entry, req engine.Request, out *printer, label string) int {
	req.Notes = true
	stop := c.spin(out.waiting())
	defer stop()
	// Starting the engine means opening a session too, unless the
	// command opens none.
	spawned := func() {
		if e.Path != "disconnect" {
			c.sayConnecting(g, out.who())
		}
	}
	err := c.call(req, spawned, func(f engine.Frame) error {
		switch {
		case f.File != nil:
			return out.file(*f.File)
		case f.Note != nil:
			out.note(*f.Note)
		default:
			stop()
			out.response(f.Line, *f.Response)
		}
		return nil
	})
	stop()
	switch {
	case err != nil && c.ctx.Err() != nil && e.Stream:
		return out.code() // interrupted: a stream's normal end
	case err != nil:
		return c.fail(codeFor(err), "bmdctl %s: %v", label, err)
	}
	return out.code()
}

// call sends a request to the engine, starting one (and then calling
// spawned) if none runs, and passes each frame of the reply to onFrame.
// An engine started from another build answers "stale" and stops; the
// request then goes, once, to a new engine of this build.
func (c *cli) call(req engine.Request, spawned func(), onFrame func(engine.Frame) error) error {
	req.Build = c.build()
	err := errStale
	for tries := 0; errors.Is(err, errStale); tries++ {
		if tries > 0 {
			c.waitEngineGone()
		}
		var conn io.ReadWriteCloser
		if conn, err = c.connectEngine(spawned); err != nil {
			return err
		}
		_, err = engine.Call(c.ctx, conn, req, func(f engine.Frame) error {
			if f.Response != nil && f.Response.Code == engine.CodeStale && tries == 0 {
				return errStale
			}
			return onFrame(f)
		})
		conn.Close()
	}
	return err
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
	a, err := e.Parse(raw, engine.Sources(atem.Resolver{FPS: fps}, nil))
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
	return files, nil
}

// prepare adds what a command sends besides its parameters' files, by
// path.
var prepare = map[string]func(params map[string]string, files map[string][]byte) error{
	"profile restore": profileImages,
}

// profileImages reads the images a profile's <MediaPool> names, from
// beside the profile, for the engine to upload after the rest: the part
// atemwire leaves to its caller. A missing image is the engine's to
// report, so it isn't an error here.
func profileImages(params map[string]string, files map[string][]byte) error {
	if v, ok := params["media-pool-images"]; ok && !isTrue(v) {
		return nil
	}
	p, err := profile.FromXML(files["file"])
	if err != nil {
		return nil // the engine reports it
	}
	pool := p.Root().Find("MediaPool")
	if pool == nil {
		return nil
	}
	dir := "."
	if f := params["file"]; f != "-" {
		dir = filepath.Dir(f)
	}
	for _, s := range pool.FindAll("Stills/Still") {
		index, err := strconv.Atoi(s.GetDefault("index", ""))
		rel := s.GetDefault("path", "")
		if err != nil || rel == "" || filepath.IsAbs(rel) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		files[engine.ImageParam(index)] = b
	}
	return nil
}

func isTrue(s string) bool {
	switch strings.ToLower(s) {
	case "true", "on", "yes", "1":
		return true
	}
	return false
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
	if path != "" {
		path += " "
	}
	fmt.Fprintf(w, "usage: bmdctl [%s] %s<command> [arguments] [--flag value ...]\n\n", device, path)
	fmt.Fprintf(w, "Commands:\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, ch := range c.reg.Children(device, prefix) {
		usage, summary := ch.Usage, ch.Summary
		if ch.Group {
			var subs []string
			for _, sub := range c.reg.Children(device, append(slices.Clone(prefix), ch.Usage)) {
				subs = append(subs, firstWord(sub.Usage))
			}
			usage, summary = ch.Usage+" ...", strings.Join(subs, ", ")
		}
		fmt.Fprintf(tw, "  %s\t%s\n", usage, summary)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nRun 'bmdctl %s %s<command> --help' for a command's arguments.\n", device, path)
	if len(prefix) == 0 {
		fmt.Fprintln(w)
		globalHelp(w)
	}
}

// firstWord is a child's word: its first that isn't an argument.
func firstWord(usage string) string {
	for _, f := range strings.Fields(usage) {
		if !strings.HasPrefix(f, "<") && !strings.HasPrefix(f, "[") {
			return f
		}
	}
	return usage
}

func (c *cli) entryHelp(w io.Writer, device string, e table.Entry) {
	fmt.Fprintf(w, "usage: bmdctl [%s] %s", device, e.Usage())
	var args, flags []table.Param
	for _, p := range e.Params {
		if e.Positional(p.Name) {
			args = append(args, p)
		} else {
			flags = append(flags, p)
		}
	}
	if len(flags) > 0 {
		fmt.Fprint(w, " [--flag value ...]")
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
	// Arguments in the order they're said, then the flags.
	slices.SortStableFunc(args, func(a, b table.Param) int { return position(e, a.Name) - position(e, b.Name) })
	for _, part := range []struct {
		title string
		ps    []table.Param
	}{{"Arguments", args}, {"Flags", flags}} {
		if len(part.ps) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s:\n", part.title)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, p := range part.ps {
			need := ""
			switch {
			case p.Default != "":
				need = "default " + p.Default
			case !p.Required && e.Positional(p.Name):
				need = "optional"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", e.Shown(p.Name), paramValue(p), need, p.Help)
		}
		tw.Flush()
	}
	if e.AtLeastOne {
		fmt.Fprintln(w, "\nGive at least one of the flags.")
	}
	for _, p := range e.Params {
		if len(p.Names) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s values:\n", e.Shown(p.Name))
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
	var notes []string
	var counted []string
	for _, p := range e.Params {
		if p.OneBased || p.Name == "slot" && p.Kind == table.String {
			counted = append(counted, e.Shown(p.Name))
		}
	}
	switch len(counted) {
	case 0:
	case 1:
		notes = append(notes, counted[0]+" counts from 1, as ATEM Software Control does; the atem library counts from 0.")
	default:
		notes = append(notes, strings.Join(counted, " and ")+" count from 1, as ATEM Software Control does; the atem library counts from 0.")
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Kind == table.Source }) {
		notes = append(notes, "A source is an input's number; cam2, input 2, mp1, bars, black, col1, pgm, pvw, cf1 or aux3,\nwhatever its name; or an input's short or long name, or the start of one, in any case.")
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Kind == table.Rate }) {
		notes = append(notes, "A rate is a frame count or seconds:frames at the switcher's display rate.")
	}
	if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Kind == table.Bool && e.Positional(p.Name) }) {
		notes = append(notes, "On and off are on, off, true, false, yes, no, 1 or 0.")
	}
	if len(notes) > 0 {
		fmt.Fprintf(w, "\n%s\n", strings.Join(notes, "\n"))
	}
}

// position is a positional argument's place in the syntax.
func position(e table.Entry, name string) int {
	return slices.IndexFunc(e.Elems(), func(x table.Elem) bool { return x.Param == name })
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
