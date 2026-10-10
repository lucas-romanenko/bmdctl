// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// sampleArg is a valid command-line value for a parameter.
func sampleArg(p table.Param) string {
	switch p.Kind {
	case table.Enum, table.Flags:
		return sortedFirst(p.Names)
	case table.Bool:
		return "true"
	case table.Float:
		return "1.5"
	case table.String:
		if p.Name == "address" {
			return "192.0.2.1"
		}
		return "x"
	case table.Rate:
		return "0:10"
	case table.Time:
		return "1789128000"
	case table.List:
		return "1"
	}
	return "1"
}

func sortedFirst(m map[string]uint64) string {
	best := ""
	for n, v := range m {
		if best == "" || v < m[best] || v == m[best] && n < best {
			best = n
		}
	}
	return best
}

// Every command but the lock and transfer ones (SURFACE.md §1.19) has one
// entry; each builds its own type, with the type's flags.
func TestCommandTable(t *testing.T) {
	r := table.New()
	if err := RegisterCommands(r); err != nil {
		t.Fatal(err)
	}
	entries := r.Entries("atem")
	byType := map[string]int{}
	for _, e := range entries {
		raw := map[string]string{}
		for _, p := range e.Params {
			if p.Required {
				raw[p.Name] = sampleArg(p)
			}
		}
		if e.AtLeastOne {
			for _, p := range e.Params {
				if !p.Required && p.Default == "" && p.Name != "clip" {
					raw[p.Name] = sampleArg(p)
					break
				}
			}
		}
		a, err := e.Parse(raw, Resolver{})
		if err != nil {
			t.Errorf("%s: %v", e.Path, err)
			continue
		}
		c, err := e.Build(a)
		if err != nil {
			t.Errorf("%s: build: %v", e.Path, err)
			continue
		}
		cmd := c.(Command)
		name := reflect.TypeOf(cmd).Name()
		byType[name]++
		if e.Unverified != cmd.Unverified() || e.Destructive != cmd.Destructive() {
			t.Errorf("%s: flags differ from %s", e.Path, name)
		}
		if (e.Echo == "") != (cmd.EchoKey() == "") {
			t.Errorf("%s: echo %q but %s.EchoKey() is %q", e.Path, e.Echo, name, cmd.EchoKey())
		}
		if v, ok := cmd.(Validator); ok {
			if err := v.Validate(); err != nil && name != "MediaplayerSelect" {
				t.Errorf("%s: sample command invalid: %v", e.Path, err)
			}
		}
	}
	transfer := map[string]bool{"Lock": true, "PartialLock": true, "TransferDownloadRequest": true,
		"TransferUploadRequest": true, "TransferData": true, "TransferFileData": true, "TransferAck": true}
	for name := range allCommands {
		switch {
		case transfer[name] && byType[name] != 0:
			t.Errorf("%s is in the table", name)
		case !transfer[name] && byType[name] != 1:
			t.Errorf("%s has %d entries", name, byType[name])
		}
	}
	if len(entries) != 75 {
		t.Errorf("%d entries, want 75", len(entries))
	}
}

// The resolver reads rates strictly and sources by id or by name.
func TestResolver(t *testing.T) {
	st := newState()
	st.VideoMode.Mode = &VideoModeField{Mode: 13, Rate: 59.94}
	st.Inputs[1] = &InputPropertiesField{Index: 1, Name: "Camera 1", ShortName: "CAM1"}
	st.Inputs[2] = &InputPropertiesField{Index: 2, Name: "Stage Left ", ShortName: "SL"}
	st.Inputs[3] = &InputPropertiesField{Index: 3, Name: "Wide", ShortName: "CAM"}
	st.Inputs[4] = &InputPropertiesField{Index: 4, Name: "Tight", ShortName: "cam"}
	r := Resolver{State: st}
	for in, want := range map[string]int64{"50": 50, "1:00": 30, "1:29": 59, " 0:05 ": 5, "0": 0} {
		if got, err := r.Rate(in); err != nil || got != want {
			t.Errorf("rate %q: %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "1:30", "1:", ":5", "x", "1.5", "-1", "1:2:3", "１"} {
		if got, err := r.Rate(in); err == nil {
			t.Errorf("rate %q: %d accepted", in, got)
		}
	}
	if got, err := (Resolver{State: st, FPS: 25}).Rate("1:00"); err != nil || got != 25 {
		t.Errorf("--fps 25: %d %v", got, err)
	}
	if got, err := (Resolver{Offline: true}).Rate("1:40"); err != nil || got != 65 {
		t.Errorf("offline 1:40: %d %v", got, err)
	}
	for in, want := range map[string]uint64{"5": 5, "3010": 3010, "camera 1": 1, "CAM1": 1, "stage left": 2, "sl": 2} {
		if got, err := r.Source(in); err != nil || got != want {
			t.Errorf("source %q: %d %v, want %d", in, got, err, want)
		}
	}
	for in, msg := range map[string]string{"Camera 9": "no input is named", "cam": "more than one input: 3 (Wide), 4 (Tight)", "": "empty"} {
		if _, err := r.Source(in); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("source %q: %v, want %q", in, err, msg)
		}
	}
	if _, err := (Resolver{}).Source("Camera 1"); err == nil || !strings.Contains(err.Error(), "give the source id") {
		t.Errorf("no names known: %v", err)
	}
	if got, err := (Resolver{Offline: true}).Source("Anything"); err != nil || got != 0 {
		t.Errorf("offline name: %d %v", got, err)
	}
}
