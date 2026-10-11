// SPDX-License-Identifier: MIT

package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// The OpenAPI document has a path for every command that isn't a
// stream, each with a unique operation id and a schema whose every
// property has a type, and survives JSON.
func TestOpenAPI(t *testing.T) {
	reg, err := engine.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	doc := OpenAPI(reg, "test")
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string `json:"operationId"`
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]map[string]any `json:"properties"`
						Required   []string                  `json:"required"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Destructive bool `json:"x-destructive"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(b, &back); err != nil || back.OpenAPI != "3.1.0" {
		t.Fatalf("%v %q", err, back.OpenAPI)
	}
	ids := map[string]bool{}
	for p, ops := range back.Paths {
		for _, op := range ops {
			if ids[op.OperationID] {
				t.Errorf("%s: operationId %s twice", p, op.OperationID)
			}
			ids[op.OperationID] = true
			for ct, c := range op.RequestBody.Content {
				for name, prop := range c.Schema.Properties {
					if prop["type"] == nil || prop["description"] == nil {
						t.Errorf("%s %s %s: %v", p, ct, name, prop)
					}
				}
			}
		}
	}
	n := 0
	for _, d := range reg.Devices() {
		for _, e := range reg.Entries(d) {
			if e.Stream {
				continue
			}
			n++
			p := "/hosts/{host}/" + strings.ReplaceAll(e.Path, " ", "/")
			if d != "atem" {
				p = "/hosts/{host}/" + d + "/" + strings.ReplaceAll(e.Path, " ", "/")
			}
			op, ok := back.Paths[p]["post"]
			if !ok {
				t.Errorf("no POST %s", p)
				continue
			}
			if op.Destructive != e.Destructive {
				t.Errorf("%s: x-destructive %v", p, op.Destructive)
			}
		}
	}
	ops := 0
	for _, m := range back.Paths {
		ops += len(m)
	}
	if ops != n+3 {
		t.Errorf("%d operations, want %d commands and 3 more", ops, n)
	}
	if st := back.Paths["/hosts/{host}/state"]; len(st) != 2 {
		t.Errorf("/hosts/{host}/state: %v", st)
	}
	upload := back.Paths["/hosts/{host}/media/upload"]["post"].RequestBody.Content["multipart/form-data"].Schema
	if upload.Properties["file"]["format"] != "binary" || len(upload.Required) != 2 {
		t.Errorf("media upload: %+v", upload)
	}
	program := back.Paths["/hosts/{host}/program"]["post"].RequestBody.Content["application/json"].Schema
	if me := program.Properties["me"]; me["minimum"] != 1.0 || !strings.Contains(me["description"].(string), "counted from 1") {
		t.Errorf("program --me: %v", me)
	}
}

func TestStringValue(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{`"cam2"`, "cam2", true}, {`12`, "12", true}, {`-1.5`, "-1.5", true}, {`true`, "true", true},
		{`false`, "false", true}, {`["background","key1"]`, "background,key1", true}, {`[1, 2]`, "1,2", true},
		{`{"a":1}`, "", false}, {`[{}]`, "", false},
	} {
		var v any
		d := json.NewDecoder(strings.NewReader(c.in))
		d.UseNumber()
		d.Decode(&v)
		if got, ok := table.StringValue(v); got != c.want || ok != c.ok {
			t.Errorf("%s: %q %v", c.in, got, ok)
		}
	}
}

func TestStatus(t *testing.T) {
	for code, want := range map[string]int{"": 200, engine.CodeUsage: 400, engine.CodeDestructive: 409,
		engine.CodeRejected: 422, engine.CodeNoEcho: 504, engine.CodeUnreachable: 502, engine.CodeStale: 502} {
		if got := Status(code); got != want {
			t.Errorf("%q: %d, want %d", code, got, want)
		}
	}
}
