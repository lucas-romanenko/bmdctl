// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile_test

// Apply and save end to end: a real *atem.Session against the fake
// switcher (internal/fakeatem), which accepts macro uploads, serves macro
// downloads and echoes aux and input-name commands.

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/macro"
	"github.com/lucas-romanenko/bmdctl/atem/profile"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// canonical returns ops as the codec decodes their bytecode: the form a
// saved profile holds.
func canonical(t *testing.T, ops []macro.Op) ([]macro.Op, []byte) {
	t.Helper()
	bc, err := macro.Encode(ops)
	if err != nil {
		t.Fatal(err)
	}
	out, err := macro.Decode(bc)
	if err != nil {
		t.Fatal(err)
	}
	return out, bc
}

func opXML(ops []macro.Op) string {
	var b strings.Builder
	for _, op := range ops {
		fmt.Fprintf(&b, `<Op id=%q`, op.ID)
		for _, p := range op.Params {
			fmt.Fprintf(&b, ` %s=%q`, p.Name, p.Value)
		}
		b.WriteString("/>")
	}
	return b.String()
}

func TestApplyThenSaveAgainstTheFakeSwitcher(t *testing.T) {
	sw := fakeatem.New(fixtures.Bytes(t, "dump/initial-dump.bin"))
	t.Cleanup(func() { sw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := (&atem.Dialer{Conn: sw}).Dial(ctx, fakeatem.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// A macro the profile doesn't mention stays as it is.
	keep, keepBC := canonical(t, []macro.Op{{ID: "MacroSleep", Params: []macro.Param{{Name: "frames", Value: "25"}}}})
	sw.PutMacro(9, keepBC, "Keep", "left alone")

	macros := map[int]struct {
		name, desc string
		ops        []macro.Op
		bc         []byte
	}{}
	for slot, ops := range map[int][]macro.Op{
		0: {
			{ID: "ProgramInput", Params: []macro.Param{{Name: "mixEffectBlockIndex", Value: "0"}, {Name: "input", Value: "Camera2"}}},
			{ID: "Cut", Params: []macro.Param{{Name: "mixEffectBlockIndex", Value: "0"}}},
		},
		3: {
			{ID: "PreviewInput", Params: []macro.Param{{Name: "mixEffectBlockIndex", Value: "0"}, {Name: "input", Value: "Camera4"}}},
			{ID: "MacroSleep", Params: []macro.Param{{Name: "frames", Value: "50"}}},
			{ID: "Auto", Params: []macro.Param{{Name: "mixEffectBlockIndex", Value: "0"}}},
		},
	} {
		c, bc := canonical(t, ops)
		macros[slot] = struct {
			name, desc string
			ops        []macro.Op
			bc         []byte
		}{fmt.Sprintf("Macro %d", slot), fmt.Sprintf("restored into slot %d", slot), c, bc}
	}
	var pool strings.Builder
	for _, slot := range []int{0, 3} {
		m := macros[slot]
		fmt.Fprintf(&pool, `<Macro index="%d" name=%q description=%q>%s</Macro>`, slot, m.name, m.desc, opXML(m.ops))
	}
	// A macro of nothing but an op the codec can't encode goes up as empty
	// bytecode with its name and description, as atemwire's does.
	pool.WriteString(`<Macro index="5" name="Dropped" description="no ops left"><Op id="NoSuchOp"/></Macro>`)
	doc := `<Profile majorVersion="2" minorVersion="1">` +
		`<Auxiliaries><Auxiliary id="8001" input="3"/></Auxiliaries>` +
		`<Settings><Inputs><Input id="2" shortName="CAM2" longName="Second camera"/></Inputs></Settings>` +
		`<MacroPool>` + pool.String() + `</MacroPool></Profile>`
	p, err := profile.FromXML([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}

	opts := profile.MacrosOnly()
	opts.RestoreAux, opts.RestoreInputs = true, true
	res, err := p.Apply(ctx, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 || !slices.Contains(res.Applied, "aux_outputs") || !slices.Contains(res.Applied, "inputs (1)") ||
		!slices.Contains(res.Applied, "macros (3/3 restored, 1 with dropped ops: slot 5 ('Dropped') dropped 1 op (NoSuchOp) — metadata only)") {
		t.Fatalf("applied %q, skipped %q, errors %q", res.Applied, res.Skipped, res.Errors)
	}

	// The switcher holds the bytecode, names and descriptions.
	for slot, m := range macros {
		bc, name, desc, ok := sw.Macro(uint16(slot))
		if !ok || !bytes.Equal(bc, m.bc) || name != m.name || desc != m.desc {
			t.Fatalf("slot %d: %v %x %q %q", slot, ok, bc, name, desc)
		}
	}
	// The empty upload leaves a macro with its name and description and no
	// ops, as a switcher keeps it.
	if bc, name, desc, ok := sw.Macro(5); !ok || len(bc) != 0 || name != "Dropped" || desc != "no ops left" {
		t.Fatalf("slot 5: %v %x %q %q", ok, bc, name, desc)
	}

	// The echoes and macro properties reach the session's state.
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := s.State()
		ready := st.AuxSource(0) == 3 && st.InputLabel(2).Long == "Second camera"
		for _, slot := range []int{0, 3, 5, 9} {
			ready = ready && st.MacroEntry(slot).IsUsed
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the state never caught up")
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved, err := profile.Save(ctx, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := saved.Macros()
	if err != nil {
		t.Fatal(err)
	}
	want := []profile.Macro{
		{Index: 0, Name: macros[0].name, Description: macros[0].desc, Ops: macros[0].ops},
		// The dump's macro, whose bytecode the fake doesn't have: saved
		// with its name only, as atemwire saves a failed download.
		{Index: 2, Name: "Half Time", Description: "", Ops: []macro.Op{}},
		{Index: 3, Name: macros[3].name, Description: macros[3].desc, Ops: macros[3].ops},
		// The name-only macro the restore left, saved as it was restored.
		{Index: 5, Name: "Dropped", Description: "no ops left", Ops: []macro.Op{}},
		{Index: 9, Name: "Keep", Description: "left alone", Ops: keep},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("saved macros\n got %+v\nwant %+v", got, want)
	}
	aux := saved.Root().Find("Auxiliaries/Auxiliary")
	if aux == nil || aux.GetDefault("id", "") != "8001" || aux.GetDefault("input", "") != "3" {
		t.Fatalf("aux %+v", aux)
	}
	found := false
	for _, in := range saved.Root().FindAll("Settings/Inputs/Input") {
		if in.GetDefault("id", "") == "2" {
			found = in.GetDefault("longName", "") == "Second camera" && in.GetDefault("shortName", "") == "CAM2"
		}
	}
	if !found {
		t.Fatal("the renamed input isn't in the saved profile")
	}

	// And the saved profile applies back as it was: the same uploads, the
	// name-only macro among them.
	out, err := saved.ToXML()
	if err != nil {
		t.Fatal(err)
	}
	again, err := profile.FromXML([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	res, err = again.Apply(ctx, s, profile.MacrosOnly())
	if err != nil || len(res.Errors) != 0 || !slices.Contains(res.Applied, "macros (5/5 restored, 0 failed)") {
		t.Fatalf("%v %q %q", err, res.Applied, res.Errors)
	}
	if bc, _, _, _ := sw.Macro(9); !bytes.Equal(bc, keepBC) {
		t.Fatalf("slot 9 now %x", bc)
	}
}
