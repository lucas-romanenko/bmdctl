// SPDX-License-Identifier: MIT

package engine

// Macros: the pool listed, run, stopped and deleted (MAct), moved through
// the transfer engine, and converted between bytecode and ops with the
// macro codec (atem/macro). A macro as a file is ATEM Software Control's
// <Macro index name description> element holding its <Op> elements, the
// form profiles keep them in, written by the profile package's serializer;
// JSON (a list of op objects, id first) and raw bytecode are read too.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
	"github.com/lucas-romanenko/bmdctl/atem/profile"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

var (
	pMacroSlot   = table.Param{Name: "slot", Kind: table.String, Required: true, Help: "macro slot, or the macro's name"}
	macroFormats = map[string]uint64{"xml": 0, "json": 1, "bin": 2}
)

func macroOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "macro list", Summary: "List the macros: slot, name, description; and what runs or records.",
			Params: []table.Param{{Name: "all", Kind: table.Bool, Default: "false", Help: "include empty slots"}}},
			run: opMacroList},
		{entry: table.Entry{Path: "macro run", Summary: "Run a macro.", Echo: "MRPr", Params: []table.Param{pMacroSlot}},
			run: macroAction(atem.MacroRun)},
		{entry: table.Entry{Path: "macro stop", Summary: "Stop the macro that is running.", Echo: "MRPr"},
			run: macroAction(atem.MacroStop)},
		{entry: table.Entry{Path: "macro delete", Summary: "Delete a macro.", Destructive: true, Echo: "MPrp:<slot>",
			Params: []table.Param{pMacroSlot}},
			run: macroAction(atem.MacroDelete)},
		{entry: table.Entry{Path: "macro download", Summary: "Download a macro as XML ops, JSON ops or bytecode.",
			Syntax: "macro download <slot> [file]",
			Params: []table.Param{pMacroSlot,
				{Name: "file", Kind: table.OutFile, Help: "where to write it; - is stdout (default: the macro's name, .xml)"},
				{Name: "format", Kind: table.Enum, Names: macroFormats, Help: "default: from --file's extension, else xml"}}},
			run: opMacroDownload},
		{entry: table.Entry{Path: "macro upload", Summary: "Upload a macro from XML ops, JSON ops or bytecode.", Echo: "MPrp:<slot>",
			Params: []table.Param{slotParam(0xFFFE, "macro slot; replacing a used one needs --yes"),
				{Name: "file", Kind: table.File, Required: true, Help: "the macro; - reads stdin"},
				{Name: "name", Kind: table.String, Help: "the macro's name (default: the file's <Macro name>, else its file name)"},
				{Name: "description", Kind: table.String, Help: "the macro's description (default: the file's)"}}},
			run: opMacroUpload},
		{entry: table.Entry{Path: "macro decode", Summary: "Turn macro bytecode into XML ops (JSON with --json).", Local: true,
			Params: []table.Param{{Name: "file", Kind: table.File, Required: true, Help: "the bytecode; - reads stdin"}}},
			run: opMacroDecode},
		{entry: table.Entry{Path: "macro encode", Summary: "Turn XML or JSON ops into macro bytecode.", Local: true,
			Params: []table.Param{{Name: "file", Kind: table.File, Required: true, Help: "the ops; - reads stdin"},
				{Name: "out", Kind: table.OutFile, Required: true, Help: "where to write the bytecode; - is stdout"}}},
			run: opMacroEncode},
	}
}

// macroSlot reads a slot: a number, or the name of a macro the switcher
// holds, ignoring case.
func (c *call) macroSlot() (int, error) {
	s := strings.TrimSpace(c.args.Value("slot").(string))
	if _, err := strconv.Atoi(s); err == nil {
		i, err := table.ParseIndex(s)
		if err != nil || i > 0xFFFE {
			return 0, codeErr(CodeUsage, "%s %s: a macro slot is 1-65535", c.entry.Label("slot"), s)
		}
		return int(i), nil
	}
	var slots []int
	for k, p := range c.state.Macros.Properties {
		if p != nil && p.IsUsed && strings.EqualFold(c.state.MacroEntry(int(k)).Name, s) {
			slots = append(slots, int(k))
		}
	}
	slices.Sort(slots)
	switch len(slots) {
	case 0:
		return 0, codeErr(CodeUsage, "%s %q: no macro has that name", c.entry.Label("slot"), s)
	case 1:
		return slots[0], nil
	}
	return 0, codeErr(CodeUsage, "%s %q: macros %v share that name; give the slot", c.entry.Label("slot"), s, slots)
}

// MacroEntry is one macro slot.
type MacroEntry struct {
	Slot        table.Index `json:"slot"`
	Used        bool        `json:"used"`
	Invalid     bool        `json:"invalid,omitempty"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
}

// MacroList is the macro pool.
type MacroList struct {
	Macros    []MacroEntry `json:"macros"`
	Running   *table.Index `json:"running,omitempty"`   // the slot, if one runs
	Recording *table.Index `json:"recording,omitempty"` // the slot, if one records
}

func opMacroList(c *call) (any, bool, error) {
	st := c.state
	all := c.args.Value("all").(bool)
	r := MacroList{Macros: []MacroEntry{}}
	var slots []int
	for k := range st.Macros.Properties {
		slots = append(slots, int(k))
	}
	slices.Sort(slots)
	for _, s := range slots {
		p := st.Macros.Properties[uint16(s)]
		if p == nil || !p.IsUsed && !all {
			continue
		}
		r.Macros = append(r.Macros, MacroEntry{Slot: table.Index(s), Used: p.IsUsed, Invalid: p.IsInvalid,
			Name: st.MacroEntry(s).Name, Description: strings.ToValidUTF8(p.Description, "�")})
	}
	if p := st.Macros.PlayStatus; p != nil && p.Running && p.Index != 0xFFFF {
		r.Running = table.IndexPtr(int(p.Index))
	}
	if rs := st.Macros.RecordStatus; rs != nil && rs.IsRecording {
		r.Recording = table.IndexPtr(int(rs.Index))
	}
	return r, false, nil
}

// macroAction sends MAct: run and delete name a slot, stop the one
// running.
func macroAction(action uint8) op {
	return func(c *call) (any, bool, error) {
		slot := 0xFFFF
		if c.args.Has("slot") {
			var err error
			if slot, err = c.macroSlot(); err != nil {
				return nil, false, err
			}
			if !c.state.MacroEntry(slot).IsUsed {
				return nil, false, codeErr(CodeUsage, "macro slot %v is empty", table.Index(slot))
			}
		}
		var change []Value
		if c.args.Has("slot") {
			change = []Value{{Param: "slot", After: fmt.Sprintf("%v (%s)", table.Index(slot), c.state.MacroEntry(slot).Name)}}
		}
		r, confirmed, err := c.sendBuilt(atem.MacroAction{Slot: uint16(slot), Action: action}, change...)
		if cr, ok := r.(CommandResult); ok && c.args.Has("slot") {
			cr.Args["slot"] = change[0].After
			r = cr
		}
		return r, confirmed, err
	}
}

// checkMacroRecord: recording over a macro replaces it.
func checkMacroRecord(c *call) error { return c.overwriteMacro(int(c.args.Value("slot").(uint64))) }

// checkMacroAction: a delete through macro action is as destructive as
// macro delete.
func checkMacroAction(c *call) error {
	if c.args.Value("action").(uint64) == uint64(atem.MacroDelete) && !c.req.Yes {
		impact := "the macro running"
		if c.args.Has("slot") {
			impact = c.macroImpact(int(c.args.Value("slot").(uint64)))
		}
		return c.refuse(impact, "delete it?", "deleting a macro is destructive: add --yes")
	}
	return nil
}

func (c *call) overwriteMacro(slot int) error {
	if e := c.state.MacroEntry(slot); e.IsUsed && !c.req.Yes {
		return c.refuse(c.macroImpact(slot), "replace it?", "macro slot %v holds %q: add --yes to replace it", table.Index(slot), e.Name)
	}
	return nil
}

// MacroResult is a macro moved or converted.
type MacroResult struct {
	TransferResult
	Ops     []OpJSON `json:"ops,omitempty"`
	Format  string   `json:"format,omitempty"`
	Warning string   `json:"warning,omitempty"` // what the decoder stopped at
	XML     string   `json:"xml,omitempty"`     // decode's document
}

func opMacroDownload(c *call) (any, bool, error) {
	slot, err := c.macroSlot()
	if err != nil {
		return nil, false, err
	}
	entry := c.state.MacroEntry(slot)
	if !entry.IsUsed {
		return nil, false, codeErr(CodeUsage, "macro slot %v is empty", table.Index(slot))
	}
	format := "xml"
	file, hasFile := "", c.args.Has("file")
	if hasFile {
		file = c.args.Value("file").(string)
		switch strings.ToLower(path.Ext(file)) {
		case ".json":
			format = "json"
		case ".bin", ".macro":
			format = "bin"
		}
	}
	if c.args.Has("format") {
		format = []string{"xml", "json", "bin"}[c.args.Value("format").(uint64)]
	}
	ctx, cancel := c.transferCtx()
	defer cancel()
	bytecode, err := c.sess.DownloadMacro(ctx, slot)
	if err != nil {
		return nil, false, transferErr(err)
	}
	desc := ""
	if p := c.state.Macros.Properties[uint16(slot)]; p != nil {
		desc = strings.ToValidUTF8(p.Description, "�")
	}
	ops, decodeErr := macro.Decode(bytecode)
	r := MacroResult{TransferResult: TransferResult{Slot: table.Index(slot), Name: entry.Name, Description: desc, Bytes: len(bytecode)},
		Format: format}
	if decodeErr != nil {
		r.Warning = decodeErr.Error()
	}
	var data []byte
	switch format {
	case "bin":
		data = bytecode
	case "json":
		data, err = json.MarshalIndent(macroFile{Name: entry.Name, Description: desc, Ops: opsJSON(ops)}, "", "  ")
		data = append(data, '\n')
	default:
		var s string
		s, err = macroXML(slot, entry.Name, desc, ops)
		data = []byte(s)
	}
	if err != nil {
		return nil, false, err
	}
	f := FileFrame{Param: "file", Data: data}
	if !hasFile {
		f.Name = fileName(entry.Name, "macro-"+table.Index(slot).String()) + "." + format
	}
	if err := c.out.file(f); err != nil {
		return nil, false, err
	}
	r.Source = fmt.Sprintf("%d ops", len(ops))
	return r, true, nil
}

func opMacroUpload(c *call) (any, bool, error) {
	slot := int(c.args.Value("slot").(uint64))
	m, err := readMacroFile(c.req.Files["file"])
	if err != nil {
		return nil, false, codeErr(CodeUsage, "%s %s: %v", c.entry.Label("file"), c.args.Value("file"), err)
	}
	if m.Name == "" {
		file := c.args.Value("file").(string)
		m.Name = strings.TrimSuffix(path.Base(strings.ReplaceAll(file, `\`, "/")), path.Ext(file))
		if file == "-" {
			m.Name = "Macro " + table.Index(slot).String()
		}
	}
	if c.args.Has("name") {
		m.Name = c.args.Value("name").(string)
	}
	if c.args.Has("description") {
		m.Description = c.args.Value("description").(string)
	}
	if err := c.overwriteMacro(slot); err != nil {
		return nil, false, err
	}
	r := MacroResult{TransferResult: TransferResult{Slot: table.Index(slot), Name: m.Name, Description: m.Description,
		Bytes: len(m.Bytecode), Source: fmt.Sprintf("%d ops", m.Ops)}}
	if e := c.state.MacroEntry(slot); e.IsUsed {
		r.Replaced = e.Name
	}
	ctx, cancel := c.transferCtx()
	defer cancel()
	if err := c.sess.UploadMacro(c.withProgress(ctx, bytesMoved), slot, m.Name, m.Description, m.Bytecode); err != nil {
		return nil, false, transferErr(err)
	}
	return r, true, nil
}

func opMacroDecode(c *call) (any, bool, error) {
	bytecode := c.req.Files["file"]
	ops, decodeErr := macro.Decode(bytecode)
	r := MacroResult{TransferResult: TransferResult{Bytes: len(bytecode)}, Ops: opsJSON(ops)}
	if decodeErr != nil {
		r.Warning = decodeErr.Error()
	}
	var err error
	if r.XML, err = macroXML(-1, "", "", ops); err != nil {
		return nil, false, err
	}
	return r, false, nil
}

func opMacroEncode(c *call) (any, bool, error) {
	m, err := readMacroFile(c.req.Files["file"])
	if err != nil {
		return nil, false, codeErr(CodeUsage, "%s %s: %v", c.entry.Label("file"), c.args.Value("file"), err)
	}
	if m.Ops < 0 {
		return nil, false, codeErr(CodeUsage, "%s %s is bytecode already", c.entry.Label("file"), c.args.Value("file"))
	}
	if err := c.out.file(FileFrame{Param: "out", Data: m.Bytecode}); err != nil {
		return nil, false, err
	}
	return MacroResult{TransferResult: TransferResult{Name: m.Name, Bytes: len(m.Bytecode), Source: fmt.Sprintf("%d ops", m.Ops)}}, false, nil
}

// macroXML is the macro as ATEM Software Control's <Macro> element; a
// negative slot leaves out the index, name and description.
func macroXML(slot int, name, description string, ops []macro.Op) (string, error) {
	root := profile.NewElement("Macro")
	if slot >= 0 {
		root.Set("index", strconv.Itoa(slot))
		root.Set("name", name)
		root.Set("description", description)
	}
	for _, o := range ops {
		e := root.SubElement("Op")
		e.Attrs = append(e.Attrs, profile.Attr{Name: "id", Value: o.ID})
		for _, p := range o.Params {
			e.Attrs = append(e.Attrs, profile.Attr{Name: p.Name, Value: p.Value})
		}
	}
	return profile.New(root).ToXML()
}

// parsedMacro is a macro file read: its bytecode, and what the file said.
type parsedMacro struct {
	Name, Description string
	Ops               int // -1: the file was bytecode
	Bytecode          []byte
}

// macroFile is a macro as JSON.
type macroFile struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Ops         []OpJSON `json:"ops"`
}

// readMacroFile reads XML (a <Macro>, or a profile or <MacroPool> holding
// exactly one), JSON (a list of ops, or {name, description, ops}) or, for
// anything else, bytecode. Ops are encoded with the macro codec.
func readMacroFile(data []byte) (parsedMacro, error) {
	t := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	var m parsedMacro
	var ops []macro.Op
	switch {
	case len(t) == 0:
		return m, errors.New("empty")
	case t[0] == '<':
		p, err := profile.FromXML(t)
		if err != nil {
			return m, err
		}
		el := p.Root()
		if el.Tag != "Macro" {
			macros := el.FindAll("MacroPool/Macro")
			if el.Tag == "MacroPool" {
				macros = el.FindAll("Macro")
			}
			if len(macros) != 1 {
				return m, fmt.Errorf("holds %d macros, not one (profile restore --macros restores a pool)", len(macros))
			}
			el = macros[0]
		}
		m.Name, m.Description = el.GetDefault("name", ""), el.GetDefault("description", "")
		for _, o := range el.FindAll("Op") {
			op := macro.Op{ID: o.GetDefault("id", "")}
			for _, a := range o.Attrs {
				if a.Name != "id" {
					op.Params = append(op.Params, macro.Param{Name: a.Name, Value: a.Value})
				}
			}
			ops = append(ops, op)
		}
	case t[0] == '[' || t[0] == '{':
		var f macroFile
		var err error
		if t[0] == '[' {
			err = json.Unmarshal(t, &f.Ops)
		} else {
			err = json.Unmarshal(t, &f)
		}
		if err != nil {
			return m, err
		}
		m.Name, m.Description = f.Name, f.Description
		for _, o := range f.Ops {
			ops = append(ops, macro.Op(o))
		}
	default:
		m.Ops, m.Bytecode = -1, data
		return m, nil
	}
	b, err := macro.Encode(ops)
	if err != nil {
		return m, err
	}
	m.Ops, m.Bytecode = len(ops), b
	return m, nil
}

// OpJSON is a macro op as a JSON object: "id" first, then each attribute
// in order, values as strings.
type OpJSON macro.Op

func opsJSON(ops []macro.Op) []OpJSON {
	out := make([]OpJSON, len(ops))
	for i, o := range ops {
		out[i] = OpJSON(o)
	}
	return out
}

// MarshalJSON keeps the attributes' order.
func (o OpJSON) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"id":`)
	id, _ := json.Marshal(o.ID)
	b.Write(id)
	for _, p := range o.Params {
		k, _ := json.Marshal(p.Name)
		v, _ := json.Marshal(p.Value)
		b.WriteByte(',')
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads an op object in order. Numbers keep their text;
// true and false become "True" and "False", as ASC's XML writes them.
func (o *OpJSON) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errors.New("an op is a JSON object")
	}
	*o = OpJSON{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		vt, err := dec.Token()
		if err != nil {
			return err
		}
		var v string
		switch x := vt.(type) {
		case string:
			v = x
		case json.Number:
			v = x.String()
		case bool:
			v = map[bool]string{true: "True", false: "False"}[x]
		default:
			return fmt.Errorf("op attribute %v: give a string, number or bool", kt)
		}
		if kt == "id" {
			o.ID = v
		} else {
			o.Params = append(o.Params, macro.Param{Name: kt.(string), Value: v})
		}
	}
	if o.ID == "" {
		return errors.New(`an op needs an "id"`)
	}
	return nil
}
