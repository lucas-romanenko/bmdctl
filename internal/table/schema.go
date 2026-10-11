// SPDX-License-Identifier: MIT

package table

// JSON Schema for the table's entries: what bmdctl serve's OpenAPI
// document and bmdctl mcp's tools say each command takes. A value is
// what the command line takes, as JSON: numbers, booleans and strings,
// and for a list or flags an array or a comma-separated string. Indexes
// count from 1, as typed.

import (
	"strings"
)

// Schema is a JSON Schema object.
type Schema = map[string]any

// Schema is the parameter's JSON Schema: its type, range and names, and
// a description that says them in words too (help, range, unit, default),
// for a reader that only reads descriptions.
func (p Param) Schema() Schema {
	s := Schema{}
	desc := []string{p.Help}
	switch p.Kind {
	case Uint, Int:
		s["type"] = "integer"
		if p.Min != nil {
			s["minimum"] = bound(p, *p.Min)
		}
		if p.Max != nil {
			s["maximum"] = bound(p, *p.Max)
		}
	case Float:
		s["type"] = "number"
	case Bool:
		s["type"] = "boolean"
	case Enum:
		s["type"] = "string"
		s["enum"] = p.SortedNames()
	case Flags:
		s["type"] = []string{"array", "string"}
		s["items"] = Schema{"type": "string", "enum": p.SortedNames()}
		desc = append(desc, "several of "+strings.Join(p.SortedNames(), ", ")+" (an array, or comma-separated)")
	case Rate:
		s["type"] = []string{"integer", "string"}
		desc = append(desc, `frames, or "seconds:frames" at the device's display rate`)
	case Time:
		s["type"] = "string"
		desc = append(desc, `RFC 3339, Unix seconds, or "now"`)
	case List:
		s["type"] = []string{"array", "string"}
		s["items"] = Schema{"type": "string"}
		desc = append(desc, "an array, or comma-separated")
	case Source:
		s["type"] = []string{"integer", "string"}
		desc = append(desc, "a source id, an alias (cam2, mp1, bars, black, col1, pgm, pvw, aux3) or an input's short or long name, or the start of one")
	case File:
		s["type"] = "string"
		desc = append(desc, "a file to send")
	case OutFile:
		s["type"] = "string"
		desc = append(desc, "a file to write")
	case Dir:
		s["type"] = "string"
		desc = append(desc, "a directory to write files into")
	default:
		s["type"] = "string"
	}
	if r := p.Range(); r != "" {
		desc = append(desc, "range "+r)
	}
	if p.Unit != "" {
		desc = append(desc, "unit "+p.Unit)
	}
	if p.OneBased {
		desc = append(desc, "counted from 1")
	}
	if p.Default != "" {
		desc = append(desc, "default "+p.Default)
	}
	s["description"] = strings.Join(nonEmptyStrings(desc), "; ")
	return s
}

// bound is a range bound as typed: one more for a one-based index.
func bound(p Param, f float64) any {
	if p.OneBased {
		f++
	}
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}

func nonEmptyStrings(ss []string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// InputSchema is the schema of the entry's arguments as one JSON object,
// one property per parameter, the required ones required.
func (e Entry) InputSchema() Schema {
	props := Schema{}
	required := []string{}
	for _, p := range e.Params {
		props[p.Name] = p.Schema()
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return Schema{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// Describe is the entry for a reader with nothing but text: its summary,
// what it is (unverified, destructive, streaming, local), its usage and
// examples.
func (e Entry) Describe() string {
	var parts []string
	parts = append(parts, e.Summary)
	if e.Destructive {
		parts = append(parts, "Destructive: hard to undo, or drops connections.")
	}
	if e.Unverified {
		parts = append(parts, "Unverified on hardware: the wire format is pinned but nobody has confirmed it on a device.")
	}
	if e.Stream {
		parts = append(parts, "Streams until stopped.")
	}
	parts = append(parts, "Usage: "+e.Usage()+".")
	if len(e.Examples) > 0 {
		parts = append(parts, "Examples: "+strings.Join(e.Examples, "; ")+".")
	}
	return strings.Join(parts, " ")
}

// StringValue is a JSON value as the command line types it: a string as
// it is, a number in its shortest form, a bool as true or false, an
// array joined with commas. ok is false for an object or null.
func StringValue(v any) (s string, ok bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case float64:
		return trim(x), true
	case interface{ String() string }: // json.Number
		return x.String(), true
	case []any:
		var parts []string
		for _, y := range x {
			s, ok := StringValue(y)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), true
	}
	return "", false
}
