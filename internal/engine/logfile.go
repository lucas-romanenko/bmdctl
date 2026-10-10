// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Log rotation: when the log passes logMaxBytes it becomes engine.log.1,
// the older ones move up, and the oldest past logKeep is removed.
const (
	logMaxBytes = 5 << 20
	logKeep     = 3
)

// LogFile is an append-only log file that rotates itself by size.
type LogFile struct {
	path string
	max  int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenLog opens (creating it and its directory) the log at path.
func OpenLog(path string) (*LogFile, error) {
	l := &LogFile{path: path, max: logMaxBytes}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *LogFile) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, fi.Size()
	return nil
}

// Path is the log's path.
func (l *LogFile) Path() string { return l.path }

// Write appends p, rotating first if p would take the file past its
// limit. A record is never split across files.
func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *LogFile) rotate() error {
	l.f.Close()
	l.f = nil
	os.Remove(fmt.Sprintf("%s.%d", l.path, logKeep))
	for i := logKeep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	os.Rename(l.path, l.path+".1")
	return l.open()
}

// Close closes the file.
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
