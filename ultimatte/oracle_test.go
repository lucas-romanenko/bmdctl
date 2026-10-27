// SPDX-License-Identifier: MIT

package ultimatte

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

// testdata/oracle/oracle.json records what bmdwire v1.6.0's
// ultimattewire does (gen_oracle.py): its parsers on a pool of texts, and
// its calls over scripted connections, each with what it returned or
// raised and what every connection was sent. Every case is replayed
// here.

type ultOracle struct {
	Bmdwire string            `json:"bmdwire"`
	Ranges  map[string][6]any `json:"ranges"`
	Parsers []struct {
		Fn      string          `json:"fn"`
		Text    string          `json:"text"`
		Header  string          `json:"header"`
		Index   int             `json:"index"`
		Name    string          `json:"name"`
		Raw     string          `json:"raw"`
		Fields  [][2]string     `json:"fields"`
		Section string          `json:"section"`
		Result  json.RawMessage `json:"result"`
		Error   *ultError       `json:"error"`
	} `json:"parsers"`
	Calls []ultCall `json:"calls"`
}

type ultError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

type ultCall struct {
	Call    string         `json:"call"`
	Args    map[string]any `json:"args"`
	Scripts []struct {
		Refuse *string `json:"refuse"`
		Chunks []struct {
			Timeout bool   `json:"timeout"`
			Data    string `json:"data"`
		} `json:"chunks"`
	} `json:"scripts"`
	Result      json.RawMessage `json:"result"`
	Error       *ultError       `json:"error"`
	Connections []struct {
		Addr    string `json:"addr"`
		Refused bool   `json:"refused"`
		Sent    string `json:"sent"`
		Closed  bool   `json:"closed"`
	} `json:"connections"`
}

func loadUltOracle(t *testing.T) ultOracle {
	t.Helper()
	raw, err := os.ReadFile("testdata/oracle/oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o ultOracle
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&o); err != nil {
		t.Fatal(err)
	}
	if o.Bmdwire != "v1.7.0" || len(o.Calls) < 200 || len(o.Parsers) < 4000 {
		t.Fatalf("oracle from bmdwire %s: %d calls, %d parser cases", o.Bmdwire, len(o.Calls), len(o.Parsers))
	}
	return o
}

// sameError compares an error with what ultimattewire raised: an *Error
// of the same class, or (a connection refused, which the scripts make
// with the message) an error with the same message.
func sameErr(err error, want *ultError) string {
	switch {
	case want == nil && err == nil:
		return ""
	case want == nil:
		return fmt.Sprintf("error %v, ultimattewire none", err)
	case err == nil:
		return fmt.Sprintf("no error, ultimattewire %s(%q)", want.Class, want.Message)
	}
	var e *Error
	switch {
	case errors.As(err, &e) && (e.Class != want.Class || e.Msg != want.Message):
		return fmt.Sprintf("%s(%q), ultimattewire %s(%q)", e.Class, e.Msg, want.Class, want.Message)
	case e == nil && err.Error() != want.Message:
		return fmt.Sprintf("%q (%T), ultimattewire %s(%q)", err, err, want.Class, want.Message)
	}
	return ""
}

func asJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	json.Unmarshal(b, &out)
	return out
}

func wantJSON(raw json.RawMessage) any {
	var out any
	json.Unmarshal(raw, &out)
	return out
}

func TestRangesMatchUltimattewire(t *testing.T) {
	o := loadUltOracle(t)
	if len(o.Ranges) != len(Ranges) {
		t.Fatalf("%d ranges, ultimattewire %d", len(Ranges), len(o.Ranges))
	}
	num := func(v any) *float64 {
		n, ok := v.(json.Number)
		if !ok {
			return nil
		}
		f, _ := n.Float64()
		return &f
	}
	eq := func(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	for name, want := range o.Ranges {
		r, ok := Ranges[name]
		if !ok || r.RawMin != *num(want[0]) || r.RawMax != *num(want[1]) || !eq(r.DisplayMin, num(want[2])) ||
			!eq(r.DisplayDef, num(want[3])) || !eq(r.DisplayMax, num(want[4])) || r.Unit != want[5] {
			t.Errorf("%s: %+v, ultimattewire %v", name, r, want)
		}
	}
}

func TestParsersMatchUltimattewire(t *testing.T) {
	o := loadUltOracle(t)
	for i, c := range o.Parsers {
		where := fmt.Sprintf("case %d %s(%q)", i, c.Fn, c.Text)
		var got any
		var err error
		switch c.Fn {
		case "parse_section":
			got = ParseSection(c.Text, c.Header)
			where += " " + c.Header
		case "parse_file_list":
			got = ParseFileList(c.Text)
		case "extract_label":
			got = ExtractLabel(c.Text)
		case "extract_model":
			got = ExtractModel(c.Text)
		case "parse_current_file":
			name, status := ParseCurrentFile(c.Text)
			got = []string{name, status}
		case "annotate_preamble":
			got, err = AnnotatePreamble(c.Text)
		case "parse_interface":
			got, err = ParseInterface(c.Text, c.Index)
		case "to_display":
			where = fmt.Sprintf("case %d to_display(%q, %q)", i, c.Name, c.Raw)
			v, unit, ok := ToDisplay(c.Name, c.Raw)
			var want [2]*string
			json.Unmarshal(c.Result, &want)
			if ok != (want[0] != nil) {
				t.Errorf("%s: ok %v, ultimattewire %v", where, ok, want)
				continue
			}
			if ok {
				w, _ := strconv.ParseFloat(*want[0], 64)
				if math.Float64bits(v) != math.Float64bits(w) && !(math.IsNaN(v) && math.IsNaN(w)) || unit != *want[1] {
					t.Errorf("%s: %v %q, ultimattewire %s %q", where, v, unit, *want[0], *want[1])
				}
			}
			continue
		case "build_block":
			var fs []Field
			for _, f := range c.Fields {
				fs = append(fs, Field{f[0], f[1]})
			}
			var want string
			json.Unmarshal(c.Result, &want)
			if got := BuildBlock(c.Index, fs...); got != want || SectionName(c.Index) != c.Section {
				t.Errorf("%s: %q, ultimattewire %q", where, got, want)
			}
			continue
		default:
			t.Fatalf("no parser %q", c.Fn)
		}
		if d := sameErr(err, c.Error); d != "" {
			t.Errorf("%s: %s", where, d)
			continue
		}
		if err == nil && !reflect.DeepEqual(asJSON(got), wantJSON(c.Result)) {
			t.Errorf("%s:\n got %s\nwant %s", where, mustJSON(got), c.Result)
		}
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// replayNet hands out a call's scripts, one per connection, the last
// again once they run out, and keeps each connection.
type replayNet struct {
	mu      sync.Mutex
	scripts []any // *script factory: func() *script, or error
	i       int
	conns   []replayConn
}

type replayConn struct {
	addr    string
	refused bool
	s       *script
}

func (n *replayNet) dial(_ context.Context, _, addr string) (net.Conn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.scripts[min(n.i, len(n.scripts)-1)]
	n.i++
	if err, ok := s.(error); ok {
		n.conns = append(n.conns, replayConn{addr: addr, refused: true})
		return nil, err
	}
	sc := s.(func() *script)()
	n.conns = append(n.conns, replayConn{addr: addr, s: sc})
	return sc, nil
}

func TestCallsMatchUltimattewire(t *testing.T) {
	o := loadUltOracle(t)
	calls := map[string]int{}
	for i, call := range o.Calls {
		calls[call.Call]++
		t.Run(fmt.Sprintf("%d-%s", i, call.Call), func(t *testing.T) { replayCall(t, call) })
	}
	for _, c := range []string{"binary_request", "upload_one", "read_preamble", "exchange", "read_network",
		"set_network", "archive", "restore"} {
		if calls[c] == 0 {
			t.Errorf("no oracle calls to %s", c)
		}
	}
}

func replayCall(t *testing.T, call ultCall) {
	n := &replayNet{}
	for _, s := range call.Scripts {
		if s.Refuse != nil {
			n.scripts = append(n.scripts, errors.New(*s.Refuse))
			continue
		}
		var chunks []any
		for _, c := range s.Chunks {
			if c.Timeout {
				chunks = append(chunks, timeout{})
				continue
			}
			b, _ := hex.DecodeString(c.Data)
			chunks = append(chunks, b)
		}
		n.scripts = append(n.scripts, func() *script { return newScript(append([]any(nil), chunks...)...) })
	}
	c, _ := client()
	c.DialContext = n.dial
	arg := func(k string) any { return call.Args[k] }
	hexArg := func(k string) []byte { b, _ := hex.DecodeString(arg(k).(string)); return b }
	num := func(k string) int { v, _ := arg(k).(json.Number).Int64(); return int(v) }
	var got any
	var err error
	switch call.Call {
	case "binary_request":
		var b []byte
		if b, err = c.BinaryRequest(ctx, host, uint16(num("opcode")), hexArg("payload")); err == nil {
			got = hex.EncodeToString(b)
		}
	case "upload_one":
		err = c.UploadOne(ctx, host, uint16(num("opcode")), arg("name").(string), hexArg("payload"))
	case "read_preamble":
		got, err = c.ReadPrelude(ctx, host)
	case "exchange":
		got, err = c.Exchange(ctx, host, arg("block").(string))
	case "read_network":
		got, err = c.ReadNetwork(ctx, host, num("index"))
	case "set_network":
		var set NetworkSet
		str := func(k string) *string {
			if v, ok := call.Args[k]; ok {
				s := v.(string)
				return &s
			}
			return nil
		}
		set.Address, set.Netmask, set.Gateway = str("address"), str("netmask"), str("gateway")
		if v, ok := call.Args["dns"]; ok {
			dns := []string{}
			for _, d := range v.([]any) {
				dns = append(dns, d.(string))
			}
			set.DNS = &dns
		}
		if v, ok := call.Args["dynamic"]; ok {
			b := v.(bool)
			set.Dynamic = &b
		}
		if v, ok := call.Args["readback_timeout"]; ok {
			s, _ := v.(json.Number).Float64()
			c.ReadbackTimeout = time.Duration(s * float64(time.Second))
			if s == 0 {
				c.ReadbackTimeout = -1
			}
		}
		got, err = c.SetNetwork(ctx, host, num("index"), set)
	case "archive":
		var z []byte
		var info ArchiveInfo
		if z, info, err = c.Archive(ctx, host, time.Now(), nil); err == nil {
			got = map[string]any{"zip": zipRecord(t, z), "info": info}
		}
	case "restore":
		var ok bool
		var info RestoreInfo
		if ok, info, err = c.Restore(ctx, host, hexArg("zip"), nil); err == nil {
			got = map[string]any{"ok": ok, "info": restoreRecord(info)}
		}
	default:
		t.Fatalf("no call %q", call.Call)
	}
	if d := sameErr(err, call.Error); d != "" {
		t.Errorf("%s", d)
	} else if err == nil && !reflect.DeepEqual(asJSON(got), wantJSON(call.Result)) {
		t.Errorf("result:\n got %s\nwant %s", mustJSON(got), call.Result)
	}
	if len(n.conns) != len(call.Connections) {
		t.Fatalf("%d connections, ultimattewire %d", len(n.conns), len(call.Connections))
	}
	for i, want := range call.Connections {
		g := n.conns[i]
		if g.addr != want.Addr || g.refused != want.Refused {
			t.Errorf("connection %d: %s refused %v, ultimattewire %s %v", i, g.addr, g.refused, want.Addr, want.Refused)
			continue
		}
		if g.refused {
			continue
		}
		if sent := hex.EncodeToString(g.s.written()); sent != want.Sent || g.s.isClosed() != want.Closed {
			t.Errorf("connection %d: sent %s closed %v, ultimattewire %s %v", i, sent, g.s.isClosed(), want.Sent, want.Closed)
		}
	}
}

// zipRecord is the archive's zip as the generator records it.
func zipRecord(t *testing.T, z []byte) []map[string]any {
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]any{}
	for _, f := range r.File {
		data, err := read(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]any{"name": f.Name, "method": f.Method, "external": f.ExternalAttrs, "flags": f.Flags,
			"system": f.CreatorVersion >> 8, "create_version": f.CreatorVersion & 0xff, "extract_version": f.ReaderVersion,
			"crc": f.CRC32, "size": f.UncompressedSize64, "extra": hex.EncodeToString(f.Extra), "data": hex.EncodeToString(data)})
	}
	return out
}

// restoreRecord is RestoreInfo as the generator records the Python
// info: tuples as lists.
func restoreRecord(info RestoreInfo) map[string]any {
	presets := []any{}
	for _, p := range info.Presets {
		presets = append(presets, []any{p.Slot, p.OK, p.Error})
	}
	pair := func(p *Pushed) any {
		if p == nil {
			return nil
		}
		return []any{p.OK, p.Error}
	}
	return map[string]any{"presets": presets, "gpi": pair(info.GPI), "saved": pair(info.Saved), "stopped_at": info.StoppedAt}
}
