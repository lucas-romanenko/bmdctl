// SPDX-License-Identifier: MIT

package main

// An Ultimatte's views (docs/PORTING.md, "Ultimatte commands"): status,
// presets, controls, the prelude, the network interface, archive,
// restore and connect, at a terminal and down a pipe. A network set is a
// write, from the engine's values note: down a pipe writeView's line, at
// a terminal a line a field.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	for path, f := range map[string]func(*printer, engine.Response, json.RawMessage) view{
		"status": screenUltStatus, "presets": screenUltPresets, "controls": screenUltControls, "prelude": screenUltPrelude,
		"network show": screenUltNetwork, "network set": screenUltNetworkSet, "archive": screenUltArchive,
		"restore": screenUltRestore, "connect": screenUltConnect,
	} {
		screenFormats["ultimatte "+path] = f
	}
	for path, f := range map[string]func(*printer, engine.Response, json.RawMessage) []string{
		"status": formatUltStatus, "presets": formatUltPresets, "controls": formatUltControls, "prelude": formatUltPrelude,
		"network show": formatUltNetwork, "archive": formatUltArchive, "restore": formatUltRestore, "connect": formatUltConnect,
	} {
		humanFormats["ultimatte "+path] = f
	}
	for _, p := range []string{"status", "presets", "controls", "network show"} {
		reports["ultimatte "+p] = true
	}
}

// netRows are a network interface's rows: the address in use, the
// configured one where it isn't that, the gateway, DNS, DHCP, MAC.
func netRows(n engine.UltNetwork) [][]string {
	rows := [][]string{{"address", addrMask(n.Address, n.Netmask)}}
	if n.StaticAddress != n.Address || n.StaticNetmask != n.Netmask {
		rows = append(rows, []string{"configured", addrMask(n.StaticAddress, n.StaticNetmask)})
	}
	rows = append(rows, []string{"gateway", orNoneText(n.Gateway)}, []string{"dns", orNoneText(strings.Join(n.DNS, ", "))})
	if n.StaticGateway != n.Gateway {
		rows = append(rows, []string{"configured", "gateway " + orNoneText(n.StaticGateway)})
	}
	dhcp := "unknown"
	if n.DHCP != nil {
		dhcp = map[bool]string{true: "on", false: "off: static"}[*n.DHCP]
	}
	return append(rows, []string{"dhcp", dhcp}, []string{"mac", orNoneText(n.MAC)})
}

func addrMask(a, m string) string {
	if a == "" {
		return "none"
	}
	if m == "" {
		return a
	}
	return a + "/" + m
}

func orNoneText(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// termRows are label/value rows at a terminal: "none" dim.
func (p *printer) termRows(rows [][]string) []string {
	var out []string
	for _, r := range rows {
		v := r[1]
		if v == "none" || v == "unknown" {
			v = p.c.colour(dim, v)
		}
		out = append(out, labelled(r[0], v))
	}
	return out
}

func ultStatusRows(r engine.UltStatusResult) [][]string {
	presets := "none"
	if len(r.Presets) > 0 {
		presets = strings.Join(r.Presets, ", ")
	}
	rows := [][]string{{"presets", presets}}
	switch {
	case r.Network != nil:
		rows = append(rows, netRows(*r.Network)...)
	case r.NetworkError != "":
		rows = append(rows, []string{"network", "unknown: " + r.NetworkError})
	}
	return rows
}

func screenUltStatus(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.UltStatusResult
	json.Unmarshal(raw, &r)
	return view{about: []string{"Ultimatte"}, gap: true, rows: p.termRows(ultStatusRows(r))}
}

func formatUltStatus(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.UltStatusResult
	json.Unmarshal(raw, &r)
	return p.table(ultStatusRows(r))
}

// numbered is a list at a terminal, each item's number dim.
func (p *printer) numbered(items []string, none string) []string {
	if len(items) == 0 {
		return []string{indent + p.c.colour(dim, none)}
	}
	w := len(strconv.Itoa(len(items)))
	var cells [][]string
	for i, it := range items {
		cells = append(cells, []string{p.c.colour(dim, padLeft(strconv.Itoa(i+1), w)), it})
	}
	return rows(cells)
}

func screenUltPresets(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.UltPresets
	json.Unmarshal(raw, &r)
	return view{about: []string{count(len(r.Presets), "preset")}, gap: true, rows: p.numbered(r.Presets, "no presets saved")}
}

func formatUltPresets(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.UltPresets
	json.Unmarshal(raw, &r)
	return append([]string{}, r.Presets...)
}

// screenUltControls is the control values: the name, the raw value dim,
// and in the panel's units where the control has a range.
func screenUltControls(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.UltControls
	json.Unmarshal(raw, &r)
	what := "control"
	if r.Section == "CONTROL DEFAULT" {
		what = "default"
	}
	v := view{about: []string{count(len(r.Controls), what)}, gap: true}
	if len(r.Controls) == 0 {
		v.rows = []string{indent + p.c.colour(dim, "no values")}
		return v
	}
	var cells [][]string
	for _, ct := range r.Controls {
		cells = append(cells, []string{ct.Name, p.c.colour(dim, ct.Value), ct.DisplayText})
	}
	v.rows = rows(cells)
	return v
}

func formatUltControls(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.UltControls
	json.Unmarshal(raw, &r)
	var rows [][]string
	for _, ct := range r.Controls {
		rows = append(rows, []string{ct.Name, ct.Value, ct.DisplayText})
	}
	return p.table(rows)
}

func screenUltPrelude(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.UltPrelude
	json.Unmarshal(raw, &r)
	return view{bare: true, whole: true, rows: strings.Split(strings.TrimRight(r.Text, "\n"), "\n")}
}

func formatUltPrelude(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.UltPrelude
	json.Unmarshal(raw, &r)
	return strings.Split(strings.TrimRight(r.Text, "\n"), "\n")
}

func screenUltNetwork(p *printer, _ engine.Response, raw json.RawMessage) view {
	var n engine.UltNetwork
	json.Unmarshal(raw, &n)
	return view{about: []string{"interface " + strconv.Itoa(n.Interface)}, gap: true, rows: p.termRows(netRows(n))}
}

func formatUltNetwork(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var n engine.UltNetwork
	json.Unmarshal(raw, &n)
	return p.table(netRows(n))
}

// archiveFile is where an archive went: the file given, else the
// keyer's label .zip, as the engine named it.
func (p *printer) archiveFile(r engine.UltArchive) string {
	if f := p.params["file"]; f != "" {
		return f
	}
	return r.Label + ".zip"
}

// ultArchiveAbout is what an archive holds: down a pipe the resources by
// name, at a terminal counted (the step above names them), and its size.
func ultArchiveAbout(r engine.UltArchive, term bool) []string {
	parts := []string{count(len(r.Presets), "preset")}
	if term {
		parts = []string{size(int64(r.Bytes)), count(len(r.Presets), "preset")}
	}
	switch {
	case len(r.Resources) > 0 && term:
		parts = append(parts, count(len(r.Resources), "setting"))
	case len(r.Resources) > 0:
		parts = append(parts, strings.Join(r.Resources, ", "))
	}
	if !term {
		parts = append(parts, fmt.Sprintf("%d bytes", r.Bytes))
	}
	if n := len(r.Warnings); n > 0 {
		parts = append(parts, count(n, "read")+" failed")
	}
	return parts
}

func screenUltArchive(p *printer, r engine.Response, raw json.RawMessage) view {
	var a engine.UltArchive
	json.Unmarshal(raw, &a)
	c := p.c
	out := []string{c.resultLinePath("archive", "", p.archiveFile(a), "  "+strings.Join(ultArchiveAbout(a, true), " · "), took(r.TookMS))}
	for _, w := range a.Warnings {
		out = append(out, labelled(c.colour(dim, "not read"), w))
	}
	return view{rows: out}
}

func formatUltArchive(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var a engine.UltArchive
	json.Unmarshal(raw, &a)
	out := []string{fmt.Sprintf("%s  saved %s: %s", p.whoOut(), p.archiveFile(a), strings.Join(ultArchiveAbout(a, false), ", "))}
	for _, w := range a.Warnings {
		out = append(out, "not read: "+w)
	}
	return out
}

// ultRestoreAbout is what a restore pushed, as ultArchiveAbout has it.
func ultRestoreAbout(r engine.UltRestore, term bool) []string {
	var parts, settings []string
	if n := len(r.Presets); n > 0 {
		parts = append(parts, count(n, "preset"))
	}
	if r.GPI != nil {
		settings = append(settings, "GPISettings")
	}
	if r.Saved != nil {
		settings = append(settings, "SavedSettings")
	}
	switch {
	case len(settings) > 0 && term:
		parts = append(parts, count(len(settings), "setting"))
	case len(settings) > 0:
		parts = append(parts, settings...)
	}
	return parts
}

func screenUltRestore(p *printer, r engine.Response, raw json.RawMessage) view {
	var rs engine.UltRestore
	json.Unmarshal(raw, &rs)
	return view{rows: []string{p.c.resultLinePath("restore", "", p.params["file"], "  "+strings.Join(ultRestoreAbout(rs, true), " · "),
		took(r.TookMS))}}
}

func formatUltRestore(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var rs engine.UltRestore
	json.Unmarshal(raw, &rs)
	return []string{fmt.Sprintf("%s  restored %s: %s", p.whoOut(), p.params["file"], strings.Join(ultRestoreAbout(rs, false), ", "))}
}

// screenUltNetworkSet is a network set at a terminal: a line a field
// set, before → after, the time on the last, so an address and its
// netmask never share one.
func screenUltNetworkSet(p *printer, r engine.Response, raw json.RawMessage) view {
	_, vals := p.sentEntry(raw)
	var out []string
	for i, v := range vals {
		change := v.After
		if v.Before != nil && *v.Before != v.After {
			change = *v.Before + " → " + v.After
		}
		status := ""
		if i == len(vals)-1 {
			status = took(r.TookMS)
		}
		out = append(out, p.c.resultLine(v.Param, change, status))
	}
	return view{rows: out}
}

// screenUltConnect is connect: the header, then what connect ADDRESS
// saved, or that the keyer answered: there is no session to keep.
func screenUltConnect(p *printer, _ engine.Response, raw json.RawMessage) view {
	var r engine.UltConnect
	json.Unmarshal(raw, &r)
	c := p.c
	var out []string
	switch {
	case p.saved.added:
		out = append(out, indent+c.colour(bold, fmt.Sprintf("saved as %q, now the default", p.saved.name)))
	case p.saved.defaulted:
		out = append(out, indent+c.colour(bold, fmt.Sprintf("%q is now the default", p.saved.name)))
	default:
		out = append(out, c.resultLine("connect", "answers · "+count(r.Presets, "preset")+" · no session is kept", ""))
	}
	return view{about: []string{"Ultimatte"}, rows: out}
}

func formatUltConnect(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var r engine.UltConnect
	json.Unmarshal(raw, &r)
	line := fmt.Sprintf("%s  answers, %s", p.whoOut(), count(r.Presets, "preset"))
	if p.saved.name != "" {
		line += fmt.Sprintf("; saved as %q, the default", p.saved.name)
	}
	return []string{line}
}
