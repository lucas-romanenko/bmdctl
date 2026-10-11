// SPDX-License-Identifier: MIT

package engine

// The keyer and input operations that read the state: atemwire's
// toggle_usk (messages/upstream_keyer.py) and toggle_dsk
// (messages/downstream_keyer.py), bmdwire v1.3.0, and the input list.

import (
	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

var (
	pKeyer = func() table.Param {
		lo, hi := table.Bounds(0, 255)
		return table.Param{Name: "keyer", Kind: table.Uint, Min: lo, Max: hi, Required: true, OneBased: true, Help: "upstream keyer"}
	}()
	pDSK = func() table.Param {
		lo, hi := table.Bounds(0, 255)
		return table.Param{Name: "dsk", Kind: table.Uint, Min: lo, Max: hi, Default: "1", OneBased: true, Help: "downstream keyer"}
	}()
)

func keyerOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "usk toggle", Summary: "Put an upstream keyer on air if it is off, or off if it is on.",
			Syntax: "usk <keyer> toggle", Echo: "KeOn:<me>:<keyer>", Params: []table.Param{pME, pKeyer}}, run: opUSKToggle},
		{entry: table.Entry{Path: "dsk toggle", Summary: "Put a downstream keyer on air if it is off, or off if it is on.",
			Echo: "DskS:<dsk>", Params: []table.Param{pDSK}}, run: opDSKToggle},
		{entry: table.Entry{Path: "inputs", Summary: "List the switcher's sources, one per line: id, short name, long name and port type."},
			run: opInputList},
	}
}

// opUSKToggle is toggle_usk: the keyer's on-air state, inverted.
func opUSKToggle(c *call) (any, bool, error) {
	me, k := meArg(c), uint8(c.args.Value("keyer").(uint64))
	on := c.state.USKOnAir(int(me), int(k))
	return c.sendBuilt(atem.KeyOnAir{ME: me, Keyer: k, Enabled: !on}, Value{Param: "on-air", Before: ptr(onOff(on)), After: onOff(!on)})
}

// opDSKToggle is toggle_dsk.
func opDSKToggle(c *call) (any, bool, error) {
	d := uint8(c.args.Value("dsk").(uint64))
	on := c.state.DSKState(int(d)).OnAir
	return c.sendBuilt(atem.DkeyOnair{DSK: d, OnAir: !on}, Value{Param: "on-air", Before: ptr(onOff(on)), After: onOff(!on)})
}

// InputEntry is one source in the input list.
type InputEntry struct {
	atem.InputSourceView
	Type string `json:"type"` // the port type, named
}

// portTypes names InPr's port types (state.go, InputPropertiesField).
var portTypes = map[int]string{0: "external", 1: "black", 2: "bars", 3: "colour", 4: "media player",
	5: "media player key", 6: "supersource", 7: "passthrough", 128: "m/e output", 129: "aux output",
	130: "key mask", 131: "multiview output"}

func opInputList(c *call) (any, bool, error) {
	out := []InputEntry{}
	for _, v := range c.state.AllInputSources() {
		t, ok := portTypes[v.PortType]
		if !ok {
			t = "other"
		}
		out = append(out, InputEntry{v, t})
	}
	return out, false, nil
}
