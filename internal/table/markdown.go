// SPDX-License-Identifier: MIT

package table

import (
	"fmt"
	"strings"
)

// Markdown renders every entry as the reference docs/COMMANDS.md holds.
func (r *Registry) Markdown() string {
	var b strings.Builder
	b.WriteString("# bmdctl commands\n\n")
	b.WriteString("Generated from the command table (`internal/table`); do not edit. After\n")
	b.WriteString("changing a table, run `go test ./cmd/bmdctl -run TestCommandsDoc -update`.\n\n")
	b.WriteString("Each command takes its parameters as `--name value` flags. **Unverified**\n")
	b.WriteString("commands have a pinned wire format that nobody has confirmed on a\n")
	b.WriteString("switcher; **destructive** ones are hard to undo or drop connections. The\n")
	b.WriteString("echo is the state the device reports back once it has applied the\n")
	b.WriteString("command; a command without one can't be confirmed.\n")
	for _, d := range r.Devices() {
		fmt.Fprintf(&b, "\n## %s\n", d)
		for _, e := range r.Entries(d) {
			fmt.Fprintf(&b, "\n### `bmdctl %s %s`\n\n%s\n", d, e.Path, e.Summary)
			var notes []string
			if e.Unverified {
				notes = append(notes, "**Unverified.**")
			}
			if e.Destructive {
				notes = append(notes, "**Destructive.**")
			}
			if e.AtLeastOne {
				notes = append(notes, "Give at least one optional parameter.")
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
				fmt.Fprintf(&b, "\n%s\n", strings.Join(notes, " "))
			}
			if len(e.Params) == 0 {
				continue
			}
			b.WriteString("\n| parameter | value | | |\n|---|---|---|---|\n")
			for _, p := range e.Params {
				need := "optional"
				switch {
				case p.Required:
					need = "required"
				case p.Default != "":
					need = "default " + p.Default
				}
				fmt.Fprintf(&b, "| `--%s` | %s | %s | %s |\n", p.Name, p.valueDoc(), need, escape(p.Help))
			}
		}
	}
	return b.String()
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
