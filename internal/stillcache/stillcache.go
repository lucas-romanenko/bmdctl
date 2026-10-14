// SPDX-License-Identifier: MIT

// Package stillcache keeps the media pool's stills at full size on disk,
// one PNG per picture, named by the MD5 the switcher reports for it (the
// slot's MPfe hash). The picture behind a hash never changes, so a file
// kept under it is the slot's picture for as long as the slot reports
// that hash: a profile save takes a still from here instead of
// downloading it again, which costs about 1.8 s for a 1080p still at the
// switcher's own pace (docs/HARDWARE.md, the twelfth run). ATEM Software
// Control saves its profiles the same way, from the stills it downloaded
// on connecting; WebATEM keeps the same cache (webatem 2.1.0).
//
// Keyed by the hash alone, so a picture on several switchers is kept, and
// downloaded, once. An empty or all-zero hash is never kept or matched.
// Bounded by size (the least recently used go first) and by age (a still
// no save has used for that long goes). A miss costs a download, as it
// did before the cache, so evicting is always safe. A write is a
// temporary file renamed into place, so readers never see half a file,
// and only the cache's own files are ever removed.
//
// A nil *Cache keeps nothing: every method is a no-op or a miss.
package stillcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	hashRE  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	entryRE = regexp.MustCompile(`^[0-9a-f]{32}\.png$`)
	tempRE  = regexp.MustCompile(`^\.[0-9a-f]{32}\.[0-9]+-[0-9]+\.tmp$`)
	zero    = strings.Repeat("0", 32)
)

// staleTemp is how old a temporary file must be before a sweep takes it
// for a dead writer's.
const staleTemp = time.Hour

// Cache is a directory of stills. Its methods are safe for concurrent use.
type Cache struct {
	dir      string
	maxBytes int64
	maxAge   time.Duration // 0: no age limit
	mu       sync.Mutex    // one sweep at a time
	seq      uint64        // temporary file names, under mu
	now      func() time.Time
}

// New is a cache in dir (created when first written), holding at most
// maxBytes and dropping a still unused for maxAge (0 for no age limit).
func New(dir string, maxBytes int64, maxAge time.Duration) *Cache {
	return &Cache{dir: dir, maxBytes: maxBytes, maxAge: maxAge, now: time.Now}
}

// Dir is where the cache keeps its files ("" for a nil cache).
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// path is the file a hash is kept in, or "" for no cache or no real hash.
func (c *Cache) path(hash string) string {
	h := strings.ToLower(hash)
	if c == nil || !hashRE.MatchString(h) || h == zero {
		return ""
	}
	return filepath.Join(c.dir, h+".png")
}

// Get is the PNG kept for hash. A hit counts as a use.
func (c *Cache) Get(hash string) ([]byte, bool) {
	p := c.path(hash)
	if p == "" {
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	c.touch(p)
	return data, true
}

// Has reports whether a PNG is kept for hash, without counting a use.
func (c *Cache) Has(hash string) bool {
	p := c.path(hash)
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func (c *Cache) touch(p string) {
	t := c.now()
	os.Chtimes(p, t, t)
}

// Put keeps png as the picture for hash, then sweeps. It reports why it
// couldn't; a failure only costs a later download.
func (c *Cache) Put(hash string, png []byte) error {
	p := c.path(hash)
	if p == "" || len(png) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("still cache: %w", err)
	}
	c.mu.Lock()
	c.seq++
	temp := filepath.Join(c.dir, fmt.Sprintf(".%s.%d-%d.tmp", strings.ToLower(hash), os.Getpid(), c.seq))
	c.mu.Unlock()
	if err := os.WriteFile(temp, png, 0o644); err != nil {
		os.Remove(temp)
		return fmt.Errorf("still cache: %w", err)
	}
	if err := os.Rename(temp, p); err != nil {
		os.Remove(temp)
		return fmt.Errorf("still cache: %w", err)
	}
	c.touch(p)
	c.Sweep()
	return nil
}

// Sweep removes the stills past the age limit, then the least recently
// used until the cache is within its size, and any temporary file a dead
// writer left. Files that aren't the cache's own are never touched.
func (c *Cache) Sweep() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	now := c.now()
	type kept struct {
		mod  time.Time
		size int64
		path string
	}
	var keep []kept
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		switch {
		case tempRE.MatchString(e.Name()):
			if now.Sub(info.ModTime()) > staleTemp {
				os.Remove(p)
			}
		case entryRE.MatchString(e.Name()):
			if c.maxAge > 0 && now.Sub(info.ModTime()) > c.maxAge {
				os.Remove(p)
				continue
			}
			keep = append(keep, kept{info.ModTime(), info.Size(), p})
			total += info.Size()
		}
	}
	slices.SortFunc(keep, func(a, b kept) int { return a.mod.Compare(b.mod) })
	for _, k := range keep {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(k.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			total -= k.size
		}
	}
}
