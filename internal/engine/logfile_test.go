// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "engine.log")
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	l.max = 100
	line := bytes.Repeat([]byte("x"), 39)
	line = append(line, '\n')
	for range 13 { // 520 bytes: two lines per file, six rotations
		if _, err := l.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	for _, name := range []string{"engine.log", "engine.log.1", "engine.log.2", "engine.log.3"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		if err != nil || len(b) == 0 || len(b) > 100 || len(b)%40 != 0 {
			t.Errorf("%s: %d bytes, %v", name, len(b), err)
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Errorf("engine.log.4 kept: %v", err)
	}
	// Reopening appends.
	l, _ = OpenLog(path)
	l.Write(line)
	l.Close()
	if b, _ := os.ReadFile(path); len(b) != 40 && len(b) != 80 {
		t.Errorf("reopened: %d bytes", len(b))
	}
}
