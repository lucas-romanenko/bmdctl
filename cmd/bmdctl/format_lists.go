// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
)

func init() {
	humanFormats["input list"] = formatInputList
}

func formatInputList(p *printer, _ engine.Response, raw json.RawMessage) []string {
	var ins []engine.InputEntry
	json.Unmarshal(raw, &ins)
	rows := [][]string{}
	for _, in := range ins {
		rows = append(rows, []string{fmt.Sprint(in.Source), in.Long, in.Short, in.Type})
	}
	return p.table(rows)
}
