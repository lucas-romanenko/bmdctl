// SPDX-License-Identifier: MIT

package table

import (
	"fmt"
	"strings"
)

// MarkdownHead is the head of the reference: its title, that it is
// generated, and how to read an entry.
func (r *Registry) MarkdownHead() string {
	var b strings.Builder
	b.WriteString("# bmdctl commands\n\n")
	b.WriteString("Generated from the command table (`internal/table`); do not edit. After\n")
	b.WriteString("changing a table, run `go generate ./cmd/bmdctl`.\n\n")
	b.WriteString("A command's required arguments follow its words, in the order they're\n")
	b.WriteString("said (`<name>`; `[name]` may be left out); everything optional is a\n")
	b.WriteString("`--name value` flag, which may be shortened to any unambiguous prefix.\n")
	b.WriteString("**Unverified** commands have a pinned wire format that nobody has confirmed on a\n")
	b.WriteString("switcher; **destructive** ones are hard to undo or drop connections. The\n")
	b.WriteString("echo is the state the device reports back once it has applied the\n")
	b.WriteString("command; a command without one can't be confirmed. Its key names the\n")
	b.WriteString("state as the device's library does, so an ATEM's indexes in it count\n")
	b.WriteString("from 0.\n")
	return b.String()
}

// Markdown renders every entry in one reference: the head, then each
// device's entries under a heading of its own.
func (r *Registry) Markdown() string {
	var b strings.Builder
	b.WriteString(r.MarkdownHead())
	for _, d := range r.Devices() {
		fmt.Fprintf(&b, "\n## %s\n", d)
		for _, e := range r.Entries(d) {
			writeEntry(&b, d, e, "###")
		}
	}
	return b.String()
}

// GroupMarkdown renders a device's group, each entry under a "##"
// heading: docs/commands/<device>-<group>.md is the head of the page,
// then this.
func (r *Registry) GroupMarkdown(device, group string) string {
	var b strings.Builder
	for _, e := range r.GroupEntries(device, group) {
		writeEntry(&b, device, e, "##")
	}
	return b.String()
}

// writeEntry is one entry: its usage under heading, its summary, its
// examples, what it is (unverified, destructive, its echo) and a table
// of its arguments.
func writeEntry(b *strings.Builder, d string, e Entry, heading string) {
	fmt.Fprintf(b, "\n%s `bmdctl %s %s`\n\n%s\n", heading, d, e.Usage(), e.Summary)
	if len(e.Examples) > 0 {
		b.WriteString("\n```\n")
		for _, ex := range e.Examples {
			fmt.Fprintf(b, "bmdctl %s\n", ex)
		}
		b.WriteString("```\n")
	}
	var notes []string
	if e.Unverified {
		notes = append(notes, "**Unverified.**")
	}
	if e.Destructive {
		notes = append(notes, "**Destructive.**")
	}
	if e.AtLeastOne {
		notes = append(notes, "Give at least one flag.")
	}
	if e.Local {
		notes = append(notes, "Runs in the CLI; needs no switcher.")
	}
	if e.Stream {
		notes = append(notes, "Runs until interrupted.")
	}
	switch {
	case e.Echo != "":
		notes = append(notes, fmt.Sprintf("Echo: `%s`.", e.Echo))
	case !e.Operation:
		notes = append(notes, "No echo.")
	}
	if len(notes) > 0 {
		fmt.Fprintf(b, "\n%s\n", strings.Join(notes, " "))
	}
	if len(e.Params) == 0 {
		return
	}
	b.WriteString("\n| argument | value | | |\n|---|---|---|---|\n")
	for _, p := range e.ordered() {
		need := "optional"
		switch {
		case p.Required:
			need = "required"
		case p.Default != "":
			need = "default " + p.Default
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", e.Shown(p.Name), p.valueDoc(), need, escape(p.Help))
	}
}

// valueDoc describes a parameter's value: kind, range, unit, names.
func (p Param) valueDoc() string {
	parts := []string{p.Kind.String()}
	if r := p.Range(); r != "" {
		parts = append(parts, r)
	}
	if p.OneBased {
		parts = append(parts, "counted from 1")
	}
	if p.Kind == OutFile {
		parts = append(parts, "written")
	}
	if p.Unit != "" {
		parts = append(parts, p.Unit)
	}
	s := strings.Join(parts, ", ")
	if len(p.Names) > 0 {
		var ns []string
		for _, n := range p.names() {
			ns = append(ns, fmt.Sprintf("%s=%d", n, p.Names[n]))
		}
		s += ": " + strings.Join(ns, " ")
	}
	return escape(s)
}

func escape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
