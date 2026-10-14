// SPDX-License-Identifier: MIT

// Package fakeclock is a clock a test moves by hand, for the engine's
// sessions (engine.Config.Clock): when one was opened and last used, and
// when it closes.
package fakeclock

import (
	"slices"
	"sync"
	"time"
)

// Clock is a fake clock. Its zero value is not usable; New makes one.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*timer
}

type timer struct {
	at time.Time
	f  func()
}

// New returns a clock at t.
func New(t time.Time) *Clock { return &Clock{now: t} }

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc calls f once the clock has been advanced by d, unless stop
// is called first; stop reports whether it stopped the call.
func (c *Clock) AfterFunc(d time.Duration, f func()) (stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		n := len(c.timers)
		c.timers = slices.DeleteFunc(c.timers, func(x *timer) bool { return x == t })
		return len(c.timers) < n
	}
}

// Advance moves the clock on by d, calling each function it passes the
// time of, in order and before it returns, at that function's time.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	end := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		i := -1
		for j, t := range c.timers {
			if !t.at.After(end) && (i < 0 || t.at.Before(c.timers[i].at)) {
				i = j
			}
		}
		if i < 0 {
			c.now = end
			c.mu.Unlock()
			return
		}
		t := c.timers[i]
		c.timers = slices.Delete(c.timers, i, i+1)
		if t.at.After(c.now) {
			c.now = t.at
		}
		c.mu.Unlock()
		t.f()
	}
}

// Skip moves the clock on by d without calling what it passes, as a
// machine that slept through it would; the next Advance calls them.
func (c *Clock) Skip(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Pending is how many calls wait for the clock.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}
