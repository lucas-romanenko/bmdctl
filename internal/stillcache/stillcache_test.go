// SPDX-License-Identifier: MIT

package stillcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	a = strings.Repeat("ab", 16)
	b = strings.Repeat("cd", 16)
	c = strings.Repeat("ef", 16)
)

func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	at := time.Now().Add(-d)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// A still is kept by its hash, in any case the switcher writes it.
func TestKeptByHash(t *testing.T) {
	sc := New(filepath.Join(t.TempDir(), "stills"), 1<<20, 0)
	if err := sc.Put(a, []byte("png-a")); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{a, strings.ToUpper(a)} {
		if got, ok := sc.Get(h); !ok || string(got) != "png-a" {
			t.Errorf("Get(%s) = %q %v", h, got, ok)
		}
	}
	if !sc.Has(a) || sc.Has(b) {
		t.Error("Has")
	}
	if _, err := os.Stat(filepath.Join(sc.Dir(), a+".png")); err != nil {
		t.Error(err)
	}
}

// Only a real hash is kept: never the empty or all-zero one, never a
// path.
func TestOnlyRealHashes(t *testing.T) {
	sc := New(filepath.Join(t.TempDir(), "stills"), 1<<20, 0)
	for _, h := range []string{"", strings.Repeat("0", 32), "xyz", "../../etc/passwd", a + "/x", a[:30]} {
		sc.Put(h, []byte("x"))
		if _, ok := sc.Get(h); ok || sc.Has(h) {
			t.Errorf("%q kept", h)
		}
	}
	if entries, _ := os.ReadDir(sc.Dir()); len(entries) != 0 {
		t.Errorf("files: %v", entries)
	}
}

// A nil cache keeps nothing.
func TestNilKeepsNothing(t *testing.T) {
	var sc *Cache
	if err := sc.Put(a, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, ok := sc.Get(a); ok || sc.Has(a) || sc.Dir() != "" {
		t.Error("nil cache kept a still")
	}
	sc.Sweep()
}

// Past the cap, the least recently used go first; a Get is a use.
func TestLeastRecentlyUsedFirst(t *testing.T) {
	sc := New(filepath.Join(t.TempDir(), "stills"), 25, 0)
	for i, h := range []string{a, b, c} {
		sc.Put(h, []byte("0123456789"))
		age(t, filepath.Join(sc.Dir(), h+".png"), time.Duration(100-i*10)*time.Second)
	}
	// 30 bytes over 25: a, the least recently used, went when c came.
	if sc.Has(a) || !sc.Has(b) || !sc.Has(c) {
		t.Errorf("after c: a %v b %v c %v", sc.Has(a), sc.Has(b), sc.Has(c))
	}
	sc.Get(b) // b is now the newest
	sc.Put(a, []byte("0123456789"))
	if !sc.Has(a) || !sc.Has(b) || sc.Has(c) {
		t.Errorf("after a: a %v b %v c %v", sc.Has(a), sc.Has(b), sc.Has(c))
	}
}

// A still unused for the age limit goes; with no limit, only the cap
// applies.
func TestAgeLimit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stills")
	sc := New(dir, 1<<20, 30*24*time.Hour)
	sc.Put(a, []byte("old"))
	sc.Put(b, []byte("new"))
	age(t, filepath.Join(dir, a+".png"), 31*24*time.Hour)
	sc.Sweep()
	if sc.Has(a) || !sc.Has(b) {
		t.Errorf("a %v b %v", sc.Has(a), sc.Has(b))
	}
	unlimited := New(dir, 1<<20, 0)
	age(t, filepath.Join(dir, b+".png"), 365*24*time.Hour)
	unlimited.Sweep()
	if !unlimited.Has(b) {
		t.Error("no age limit removed b")
	}
}

// A sweep removes a dead writer's temporary file, keeps a fresh one,
// and never touches a file that isn't the cache's.
func TestSweepTouchesOnlyItsOwn(t *testing.T) {
	sc := New(filepath.Join(t.TempDir(), "stills"), 1, time.Hour)
	sc.Put(a, []byte("x"))
	stale := filepath.Join(sc.Dir(), "."+b+".1-2.tmp")
	fresh := filepath.Join(sc.Dir(), "."+c+".1-2.tmp")
	other := filepath.Join(sc.Dir(), "notes.txt")
	for _, p := range []string{stale, fresh, other} {
		os.WriteFile(p, []byte("xxxx"), 0o644)
	}
	age(t, stale, 2*time.Hour)
	age(t, other, 1000*time.Hour)
	sc.Sweep()
	for p, want := range map[string]bool{stale: false, fresh: true, other: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s exists %v, want %v", filepath.Base(p), err == nil, want)
		}
	}
}
