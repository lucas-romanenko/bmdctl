// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Every command line in the skill's code blocks (skills/bmdctl/SKILL.md)
// reads as a command and passes the checks a line gets before it is
// sent, as the table's examples do: the skill can't teach a line bmdctl
// would refuse.
func TestSkillCommands(t *testing.T) {
	b, err := os.ReadFile("../../skills/bmdctl/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n >= 150 {
		t.Errorf("SKILL.md is %d lines; keep it under 150", n)
	}
	c := newCLI(io.Discard, io.Discard)
	inBlock, checked := false, 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "```") {
			inBlock = !inBlock
			continue
		}
		line = strings.TrimSpace(line)
		if !inBlock || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "jq") || strings.HasPrefix(line, "while") {
			continue
		}
		line = strings.TrimSpace(strings.TrimSuffix(line, "|"))
		args, err := splitWords(line)
		if err == nil {
			err = checkWords(c, args)
		}
		if err != nil {
			t.Errorf("%q: %v", line, err)
		}
		checked++
	}
	if checked < 15 {
		t.Errorf("checked %d lines", checked)
	}
}
