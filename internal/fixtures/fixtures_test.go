// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package fixtures

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Every fixture loads, is non-empty, and is listed in the README with its
// origin, so a file can't be added without saying where it came from.
func TestEveryFixtureLoadsAndIsDocumented(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = fs.WalkDir(FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		if b := Bytes(t, path); len(b) == 0 {
			t.Errorf("%s: empty", path)
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if !strings.Contains(string(readme), "| `"+name+"` |") {
			t.Errorf("%s: not listed in README.md", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no fixtures embedded")
	}
	t.Logf("%d fixtures", n)
}
