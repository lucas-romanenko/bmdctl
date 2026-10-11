// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"
)

// spinner says what a request waits on while it waits, on one line of
// stderr that whatever comes next replaces: drawn once a wait passes
// spinAfter, redrawn every spinEvery with the time so far, and cleared
// before anything else is written to stdout or stderr (spinWriter), so
// it leaves nothing behind. Only at a terminal (cli.spins).
type spinner struct {
	mu    sync.Mutex
	out   io.Writer // stderr, the terminal
	paint func(code, s string) string
	after time.Duration    // how long a wait goes unshown
	now   func() time.Time // tests replace it
	label string
	since time.Time // when label was set
	shown bool      // a frame is on the line
	frame int
	stop  chan struct{}
	done  chan struct{}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinAfter = 200 * time.Millisecond
	spinEvery = 100 * time.Millisecond
	// clearLine erases the line the cursor is on, then returns to its
	// start: in that order, so exec's prefix (prefixWriter) lands after
	// the return, where the next text does.
	clearLine = "\x1b[2K\r"
	// spinLabel is the longest label shown, in characters, so the line
	// doesn't wrap on an 80-column terminal: a wrapped line can't be
	// cleared.
	spinLabel = 60
)

func newSpinner(out io.Writer, paint func(code, s string) string, after time.Duration, label string) *spinner {
	s := &spinner{out: out, paint: paint, after: after, now: time.Now, stop: make(chan struct{}), done: make(chan struct{})}
	s.set(label)
	return s
}

// run draws the spinner until close.
func (s *spinner) run() {
	defer close(s.done)
	t := time.NewTicker(spinEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.tick()
		}
	}
}

// tick draws the next frame, once the wait has gone on long enough.
func (s *spinner) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.now().Sub(s.since)
	if d < s.after {
		return
	}
	glyph := spinFrames[s.frame%len(spinFrames)]
	s.frame++
	fmt.Fprintf(s.out, "%s%s %s  %s", clearLine, s.paint(bold, glyph), s.label, s.paint(dim, seconds(d)))
	s.shown = true
}

// set says what the wait is for from now on; its time starts again.
func (s *spinner) set(label string) {
	if utf8.RuneCountInString(label) > spinLabel {
		label = string([]rune(label)[:spinLabel-1]) + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.label, s.since = label, s.now()
}

// elapsed is how long the wait for the current label has gone on.
func (s *spinner) elapsed() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.since)
}

// clear takes the spinner's line away, if a frame is on it. Under mu.
func (s *spinner) clear() {
	if s.shown {
		io.WriteString(s.out, clearLine)
		s.shown = false
	}
}

// close stops the spinner and clears its line.
func (s *spinner) close() {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
}

// spinWriter writes past the spinner: it clears the spinner's line
// first, so what is written takes its place, and the next frame is
// drawn below it.
type spinWriter struct {
	s *spinner
	w io.Writer
}

func (w spinWriter) Write(b []byte) (int, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	w.s.clear()
	return w.w.Write(b)
}

// seconds is a wait as the spinner shows it: 0.4s, 12.0s, 1m05s.
func seconds(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return since(d)
}

// spin shows the spinner, saying label, while a request waits, when
// spins allows and none shows already; stdout and stderr write past it
// until the returned stop, which clears its line and may be called
// again.
func (c *cli) spin(label string) (stop func()) {
	if !c.spins() || c.spinner != nil {
		return func() {}
	}
	s := newSpinner(c.stderr, c.colourErr, c.spinAfter, label)
	stdout, stderr := c.stdout, c.stderr
	c.stdout, c.stderr, c.spinner = spinWriter{s, stdout}, spinWriter{s, stderr}, s
	go s.run()
	return sync.OnceFunc(func() {
		s.close()
		c.stdout, c.stderr, c.spinner = stdout, stderr, nil
	})
}
