// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakevideohub"
)

// hubBench is a bench with a Videohub at fakevideohub's address,
// connected and the default host.
func hubBench(t *testing.T) (*bench, *fakevideohub.Hub) {
	t.Helper()
	b := newBench(t)
	h := b.net.Hub(fakevideohub.Address, fakevideohub.New())
	b.must("connect", fakevideohub.Address)
	return b, h
}

// connect ADDRESS finds the router, opens its session and saves it by
// the name it gives itself.
func TestConnectAHub(t *testing.T) {
	b, _ := hubBench(t)
	f := b.hostsFile(t)
	h := f.FindAddress(fakevideohub.Address)
	if h == nil || h.Device != "videohub" || h.Name != "Studio Router" || f.Default != "Studio Router" {
		t.Errorf("host %+v, default %q", h, f.Default)
	}
	if out := b.must("status"); !strings.Contains(out, "\n5        Monitor A       5   Replay       unlocked\n") {
		t.Errorf("status:\n%s", out)
	}
	code, _, errOut := b.run("program", "2")
	if code != exitUsage || errOut != "bmdctl: Studio Router (192.0.2.31) is a Videohub; bmdctl videohub --help\n" {
		t.Errorf("%d %q", code, errOut)
	}
	if out := b.must("videohub", "--help"); !strings.Contains(out, "route <output> <input>") || !strings.Contains(out, "  inputs  ") {
		t.Errorf("help:\n%s", out)
	}
}

// Each write's line down a pipe: the router, what was done, before →
// after, confirmed; the blocks the router was sent.
func TestHubWrites(t *testing.T) {
	b, h := hubBench(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"route", "5", "7"}, "route Monitor A  Replay → Program  confirmed"},
		{[]string{"route", "monitor a", "prog"}, "route Monitor A  Program  confirmed"}, // already: not sent
		{[]string{"route", "proj", "camera 2"}, "route Projector  Playback 1 → Camera 2  confirmed"},
		{[]string{"input", "rename", "3", "Wide  Shot\n"}, "input rename 3  Camera 3 → Wide Shot  confirmed"},
		{[]string{"output", "rename", "Monitor A", "Director"}, "output rename 5  Monitor A → Director  confirmed"},
		{[]string{"output", "rename", "5", "Director"}, "output rename 5  Director  confirmed"},
		{[]string{"route", "director", "wide"}, "route Director  Program → Wide Shot  confirmed"},
	} {
		out := strings.Join(strings.Fields(b.must(c.args...)), " ")
		want := strings.Join(strings.Fields("Studio Router (192.0.2.31)  "+c.want), " ")
		if !strings.HasPrefix(out, want) {
			t.Errorf("%v: %q, want %q", c.args, out, c.want)
		}
	}
	want := []string{"VIDEO OUTPUT ROUTING:\n4 6", "VIDEO OUTPUT ROUTING:\n9 1", "INPUT LABELS:\n2 Wide Shot",
		"OUTPUT LABELS:\n4 Director", "VIDEO OUTPUT ROUTING:\n4 2"}
	if got := h.Commands(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sent %q", got)
	}
	if r := h.Routing(); r[4] != 2 || r[9] != 1 {
		t.Errorf("routing %v", r)
	}
}

// What bmdctl refuses, and what the router does.
func TestHubRefusals(t *testing.T) {
	b, h := hubBench(t)
	h.LockOutput(2, true)
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"route", "20", "1"}, exitUsage, "output 20: the router has 16 outputs, numbered from 1"},
		{[]string{"route", "0", "1"}, exitUsage, "output 0: the router has 16 outputs, numbered from 1"},
		{[]string{"route", "1", "17"}, exitUsage, "input 17: the router has 16 inputs, numbered from 1"},
		{[]string{"route", "nothing", "1"}, exitUsage, `output "nothing": no output is labelled so (bmdctl videohub outputs lists them)`},
		{[]string{"route", "spare", "1"}, exitUsage, `output "spare" could be 14 "Spare 1", 15 "Spare 2": give more of the label, or its number`},
		{[]string{"route", "3", "1"}, exitRejected, `the router refused: output 3 "Switcher 3" is locked by another client`},
		{[]string{"input", "rename", "1", strings.Repeat("x", 65)}, exitUsage, "label too long (max 64 characters)"},
		{[]string{"route", "1"}, exitUsage, "missing <input>"},
	} {
		code, _, errOut := b.run(c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d %q", c.args, code, errOut, c.code, c.msg)
		}
	}
	// A router that stops answering: the session ends, and the next
	// command opens another.
	h.Silence(true)
	code, _, errOut := b.run("route", "1", "2")
	if code != exitNoEcho || !strings.Contains(errOut, `sent "VIDEO OUTPUT ROUTING: 0 1", but the router did not answer within 5s`) {
		t.Errorf("silent: %d %q", code, errOut)
	}
	h.Silence(false)
	if out := b.must("route", "1", "2"); !strings.Contains(out, "Camera 1 → Camera 2") {
		t.Errorf("after: %q", out)
	}
}

// Reads: outputs, inputs, status, state, down a pipe and as JSON; watch.
func TestHubReads(t *testing.T) {
	b, h := hubBench(t)
	h.LockOutput(15, true)
	b.must("route", "2", "7")
	out := b.must("outputs")
	if !strings.HasPrefix(out, "Studio Router (192.0.2.31)  Blackmagic Smart Videohub 12G 16x16\n"+
		"1   Switcher 1      1   Camera 1     unlocked\n"+
		"2   Switcher 2      7   Program      unlocked\n") ||
		!strings.Contains(out, "\n16  Test            16  Black        locked\n") {
		t.Errorf("outputs:\n%s", out)
	}
	if out := b.must("inputs"); !strings.Contains(out, "\n7   Program      2, 7\n") || !strings.Contains(out, "\n2   Camera 2\n") {
		t.Errorf("inputs:\n%s", out)
	}
	var st engine.HubStatusResult
	var resp struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal([]byte(b.must("--json", "status")), &resp)
	json.Unmarshal(resp.Result, &st)
	if st.Name != "Studio Router" || st.Inputs != 16 || st.Outputs != 16 || st.Locked != 1 || st.Protocol != "2.8" ||
		len(st.Routes) != 16 || st.Routes[1].Input == nil || int(*st.Routes[1].Input) != 6 || st.Routes[1].InputLabel != "Program" ||
		st.Routes[15].Lock != "locked" || !strings.Contains(string(resp.Result), `"output":2,"label":"Switcher 2","input":7`) {
		t.Errorf("%+v\n%s", st, resp.Result)
	}
	if out := b.must("state", "--path", "routing.1"); out != "6\n" {
		t.Errorf("state: %q", out)
	}
	// watch: changes from another client, as they come. Nothing tells the
	// router a watch has begun, so a panel routes until it has seen two.
	done := make(chan string)
	go func() {
		_, out, _ := b.run("watch", "--count", "2")
		done <- out
	}()
	var got string
	for i := 0; got == ""; i++ {
		h.Route(4, i%2)
		select {
		case got = <-done:
		case <-time.After(50 * time.Millisecond):
		}
	}
	if strings.Count(got, "route  out 5 Monitor A  ") != 2 {
		t.Errorf("watch:\n%s", got)
	}
}

// At a terminal: the header names the router; writes are verb, before
// → after and the time; status, outputs and inputs lay out rows.
func TestTerminalHub(t *testing.T) {
	b := newBench(t)
	h := b.net.Hub(fakevideohub.Address, fakevideohub.New())
	tr := &transcript{b: b}
	// Locked before the session opens, so it comes in the preamble:
	// locked after, status could ask before the lock came in.
	h.LockOutput(15, true)
	tr.run("connect", fakevideohub.Address)
	tr.run("--host", "studio router", "status")
	tr.run("--host", "studio router", "inputs")
	tr.run("--host", "studio router", "route", "5", "7")
	tr.run("--host", "studio router", "route", "monitor a", "prog")
	tr.run("--host", "studio router", "route", "projector", "graphics")
	tr.run("--host", "studio router", "output", "rename", "5", "Director")
	tr.run("--host", "studio router", "input", "rename", "camera 3", "Wide Shot")
	tr.run("--host", "studio router", "route", "spare", "1")
	tr.run("--host", "studio router", "route", "16", "1")
	tr.run("--host", "studio router", "outputs")
	tr.run("--host", "studio router", "program", "2")
	tr.run("--host", "studio router", "disconnect")
	golden(t, "videohub.golden", tr.out.String())
}

// Every Videohub command runs against the fake router, as every ATEM
// command does against the fake switcher (TestEveryCommandRuns).
func TestEveryHubCommandRuns(t *testing.T) {
	b, h := hubBench(t)
	special := map[string][]string{
		"route":         {"route", "1", "2"},
		"input rename":  {"input", "rename", "1", "Wide"},
		"output rename": {"output", "rename", "1", "Main"},
		"watch":         {"watch", "--count", "1"},
	}
	c, _, _ := b.cli("")
	entries := c.reg.Entries("videohub")
	ran := 0
	for _, e := range entries {
		if e.Path == "disconnect" {
			continue // last
		}
		args := special[e.Path]
		if args == nil {
			args = strings.Fields(e.Path)
		}
		stop := make(chan struct{})
		if e.Path == "watch" {
			go func() {
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					case <-time.After(50 * time.Millisecond):
						h.Route(3, i%2)
					}
				}
			}()
		}
		if code, out, errOut := b.run(args...); code != 0 {
			t.Errorf("%v: exit %d\n%s%s", args, code, out, errOut)
		}
		close(stop)
		ran++
	}
	b.must("disconnect")
	if ran != len(entries)-1 || ran < 9 {
		t.Errorf("ran %d of %d", ran, len(entries))
	}
}
