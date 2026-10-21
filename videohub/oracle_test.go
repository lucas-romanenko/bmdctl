// SPDX-License-Identifier: MIT

package videohub

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"
)

// testdata/oracle/client.json records what bmdwire v1.6.0's videohubwire
// client did in scripted sessions (gen_client.py): each step's bytes
// sent, result or exception, and its six attributes after. Each session
// is replayed here through a Session on a scripted hub that gives it the
// same stream, a call's reply once the call is written.

type vhOracle struct {
	Bmdwire  string      `json:"bmdwire"`
	Sessions []vhSession `json:"sessions"`
}

type vhSession struct {
	Name     string   `json:"name"`
	Preamble []string `json:"preamble"`
	Connect  struct {
		Attrs  *vhAttrs `json:"attrs"`
		Unread int      `json:"unread"`
		Error  *vhError `json:"error"`
	} `json:"connect"`
	Steps []vhStep `json:"steps"`
}

type vhStep struct {
	Call   string            `json:"call"`
	Args   []json.RawMessage `json:"args"`
	Reply  []string          `json:"reply"`
	Push   []string          `json:"push"`
	Result json.RawMessage   `json:"result"`
	Error  *vhError          `json:"error"`
	Sent   string            `json:"sent"`
	Attrs  vhAttrs           `json:"attrs"`
	Unread int               `json:"unread"`
}

type vhAttrs struct {
	ProtocolVersion string            `json:"protocol_version"`
	Device          map[string]string `json:"device"`
	InputLabels     map[string]string `json:"input_labels"`
	OutputLabels    map[string]string `json:"output_labels"`
	Routing         map[string]int    `json:"routing"`
	Locks           map[string]string `json:"locks"`
}

type vhError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

func chunks(t *testing.T, hx []string) [][]byte {
	var out [][]byte
	for _, h := range hx {
		b, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func attrsOf(st State) vhAttrs {
	keys := func(m map[int]string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[strconv.Itoa(k)] = v
		}
		return out
	}
	r := map[string]int{}
	for k, v := range st.Routing {
		r[strconv.Itoa(k)] = v
	}
	dev := map[string]string{}
	for k, v := range st.Device {
		dev[k] = v
	}
	return vhAttrs{ProtocolVersion: st.ProtocolVersion, Device: dev, InputLabels: keys(st.InputLabels),
		OutputLabels: keys(st.OutputLabels), Routing: r, Locks: keys(st.Locks)}
}

// sameError compares an error with the exception videohubwire raised:
// a VideohubError is an *Error, a ValueError any other error, each with
// the same message.
func sameError(err error, want *vhError) string {
	switch {
	case want == nil && err == nil:
		return ""
	case want == nil:
		return fmt.Sprintf("error %v, videohubwire none", err)
	case err == nil:
		return fmt.Sprintf("no error, videohubwire %s(%q)", want.Class, want.Message)
	}
	var ve *Error
	isVH := errors.As(err, &ve)
	if isVH != (want.Class == "VideohubError") || err.Error() != want.Message {
		return fmt.Sprintf("error %q (%T), videohubwire %s(%q)", err, err, want.Class, want.Message)
	}
	return ""
}

func TestClientMatchesVideohubwire(t *testing.T) {
	raw, err := os.ReadFile("testdata/oracle/client.json")
	if err != nil {
		t.Fatal(err)
	}
	var o vhOracle
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o.Bmdwire != "v1.6.0" || len(o.Sessions) < 200 {
		t.Fatalf("oracle from bmdwire %s, %d sessions", o.Bmdwire, len(o.Sessions))
	}
	calls := map[string]int{}
	for _, sess := range o.Sessions {
		for _, st := range sess.Steps {
			calls[st.Call]++
		}
		t.Run(sess.Name, func(t *testing.T) {
			t.Parallel()
			replay(t, sess)
		})
	}
	for _, m := range []string{"state", "route", "set_input_label", "set_output_label", "ping", "close"} {
		if calls[m] == 0 {
			t.Errorf("no oracle calls to %s", m)
		}
	}
}

func replay(t *testing.T, sess vhSession) {
	ctx := context.Background()
	c := newScripted(chunks(t, sess.Preamble)...)
	s, err := testDialer(c).Dial(ctx, "192.0.2.31")
	if d := sameError(err, sess.Connect.Error); d != "" {
		t.Fatalf("connect: %s", d)
	}
	if err != nil {
		return
	}
	defer s.Close()
	if sess.Connect.Unread == 0 {
		settled(t, s, c)
		if got := attrsOf(s.State()); !reflect.DeepEqual(got, *sess.Connect.Attrs) {
			t.Fatalf("after connect:\n got %+v\nwant %+v", got, *sess.Connect.Attrs)
		}
	}
	for i, st := range sess.Steps {
		where := fmt.Sprintf("step %d %s%s", i, st.Call, st.Args)
		if st.Push != nil {
			c.push(chunks(t, st.Push)...)
			continue
		}
		before := len(c.written())
		var err error
		switch st.Call {
		case "state":
			settled(t, s, c)
			var snap Snapshot
			if snap, err = s.Snapshot(); err == nil {
				got, _ := json.Marshal(snap)
				var g, w any
				json.Unmarshal(got, &g)
				json.Unmarshal(st.Result, &w)
				if !reflect.DeepEqual(g, w) {
					t.Errorf("%s:\n got %s\nwant %s", where, got, st.Result)
				}
			}
		case "close":
			err = s.Close()
		default:
			c.reply(chunks(t, st.Reply)...)
			err = s.Send(ctx, command(t, st))
			c.disarm()
		}
		if d := sameError(err, st.Error); d != "" {
			t.Errorf("%s: %s", where, d)
		}
		if sent := hex.EncodeToString([]byte(c.written()[before:])); sent != st.Sent {
			t.Errorf("%s: sent %x, videohubwire %s", where, c.written()[before:], st.Sent)
		}
		if st.Unread == 0 && st.Call != "close" && s.Connected() {
			settled(t, s, c)
		}
		if st.Unread == 0 {
			if got := attrsOf(s.State()); !reflect.DeepEqual(got, st.Attrs) {
				t.Errorf("%s: attributes\n got %+v\nwant %+v", where, got, st.Attrs)
			}
		}
	}
}

// command is the Command a step's call sends.
func command(t *testing.T, st vhStep) Command {
	num := func(i int) int {
		var n int
		if err := json.Unmarshal(st.Args[i], &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	text := func(i int) string {
		var s string
		if err := json.Unmarshal(st.Args[i], &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	switch st.Call {
	case "route":
		return Route{Dest: num(0), Src: num(1)}
	case "set_input_label":
		return InputLabel{Index: num(0), Label: text(1)}
	case "set_output_label":
		return OutputLabel{Index: num(0), Label: text(1)}
	case "ping":
		return Ping{}
	}
	t.Fatalf("no call %q", st.Call)
	return nil
}
