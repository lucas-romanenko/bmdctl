// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package profile

// What the port adds to atemwire's profile API (contexts, the pause
// option, Go errors from the target) and the Python semantics it rebuilds
// (float(), int(), inet_aton), checked directly.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// ResyncWait zero is atemwire's 3 s pause after a video mode change, and a
// context that has ended stops the apply; negative skips the pause.
func TestResyncWait(t *testing.T) {
	doc := `<Profile><VideoMode videoMode="1080p50"/><DownstreamKeys><DownstreamKey index="0"/></DownstreamKeys></Profile>`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := mustProfile(t, doc).Apply(ctx, newTarget(atem.State{}), nil)
	if !errors.Is(err, context.Canceled) || res.Applied != nil {
		t.Fatalf("%v %v", err, res.Applied)
	}
	o := DefaultApplyOptions()
	o.ResyncWait = -1
	tg := newTarget(atem.State{})
	res, err = mustProfile(t, doc).Apply(context.Background(), tg, o)
	if err != nil || !slices.Contains(res.Applied, "video_mode (-> 1080p50)") || len(ofType[atem.DkeyTie](tg.cmds)) != 1 {
		t.Fatalf("%v %v", err, res.Applied)
	}
}

type closedTarget struct{ recTarget }

func (c *closedTarget) Send(atem.Command) error { return fmt.Errorf("send: %w", atem.ErrClosed) }

// A command the session refuses because it has closed is recorded as
// atemwire records the ConnectionDeadError its send raises.
func TestSendErrorsAreRecorded(t *testing.T) {
	tg := &closedTarget{*newTarget(atem.State{})}
	res, err := mustProfile(t, `<Profile><Settings ftbEnabled="True"/></Profile>`).Apply(context.Background(), tg, nil)
	if err != nil || !slices.Equal(res.Errors, []string{"settings.ftbEnabled: ConnectionDeadError: send: atem: session closed"}) {
		t.Fatalf("%v %q", err, res.Errors)
	}
}

func TestSaveStopsWhenTheContextEnds(t *testing.T) {
	st := atem.State{Macros: atem.Macros{Properties: map[uint16]*atem.MacroPropertiesField{1: {Index: 1, IsUsed: true, Name: "m"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Save(ctx, &fakeSource{st: st}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFileRoundTrip(t *testing.T) {
	golden := string(fixtures.Bytes(t, "profile/profile_single_me_save_golden.xml"))
	path := t.TempDir() + "/p.xml"
	if err := mustProfile(t, golden).ToFile(path); err != nil {
		t.Fatal(err)
	}
	q, err := FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := q.ToXML(); out != golden {
		t.Fatal("round trip differs")
	}
}

// What CPython 3.12's float() and int() make of strings.
func TestPythonNumbers(t *testing.T) {
	for _, c := range []struct{ s, f, i string }{ // "" where it raises
		{"\x1c5", "", ""}, {"5\x1f", "", ""}, {"\x0b5\x0c", "5", "5"}, {"\u00855", "5", "5"},
		{"　5　", "5", "5"}, {"٥", "5", "5"}, {"1_0", "10", "10"}, {"1__0", "", ""},
		{"_1", "", ""}, {"1_", "", ""}, {"1_.5", "", ""}, {"1._5", "", ""}, {"1e1_0", "1e+10", ""},
		{"+.5", "0.5", ""}, {"5.", "5", ""}, {".", "", ""}, {"e5", "", ""}, {"1e", "", ""},
		{"inF", "+Inf", ""}, {"-iNfinity", "-Inf", ""}, {"+nan", "NaN", ""}, {"nan_", "", ""},
		{"0x10", "", ""}, {"\x7f5", "", ""}, {"1 0", "", ""}, {" 5", "5", "5"}, {"５", "5", "5"},
		{"\U0001d7d3", "5", "5"}, {"1e400", "+Inf", ""}, {"1e-400", "0", ""}, {"١_٢", "12", "12"},
		{"+_1", "", ""}, {"-0", "-0", "0"}, {"007", "7", "7"}, {" +5 ", "5", "5"}, {"+-5", "", ""},
	} {
		gotF, gotI := "", ""
		if f, ok := pyFloat(c.s); ok {
			gotF = strconv.FormatFloat(f, 'g', -1, 64)
		}
		if i, ok := pyInt(c.s); ok {
			gotI = strconv.FormatFloat(i, 'g', -1, 64)
		}
		if gotF != c.f || gotI != c.i {
			t.Errorf("%q: float %q int %q, want %q %q", c.s, gotF, gotI, c.f, c.i)
		}
	}
}

// What glibc's inet_aton accepts, and the address it means.
func TestInetAton(t *testing.T) {
	for s, want := range map[string]string{
		"192.0.2.10": "192.0.2.10", "10.1": "10.0.0.1", "0x7f.1": "127.0.0.1", "1.2.3.4 junk": "1.2.3.4",
		"010.0.0.1": "8.0.0.1", "4294967295": "255.255.255.255", "1.16777215": "1.255.255.255",
		"1.2.3": "1.2.0.3", "0": "0.0.0.0", "1.2.3.4\t": "1.2.3.4",
		"not an ip": "", "": "", "256.1.1.1": "", "1.2.3.4.5": "", "08.1.1.1": "", "1.2.3.0x": "",
		"1..2": "", "1.2.3.": "", "4294967296": "", "1.16777216": "", " 1.2.3.4": "", "+1.2.3.4": "",
		"1.2.3.4x": "", "1.2.3.4 ": "",
	} {
		got := ""
		if v, ok := inetAton(s); ok {
			got = fmt.Sprintf("%d.%d.%d.%d", v>>24, v>>16&0xff, v>>8&0xff, v&0xff)
		}
		if got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}
