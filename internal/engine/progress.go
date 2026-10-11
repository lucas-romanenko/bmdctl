// SPDX-License-Identifier: MIT

package engine

// Progress and steps: how a transfer, or an operation of several steps
// (profile save and restore), tells a person how far it has come, in
// notes (protocol.go). The CLI draws a bar at a terminal; nothing else
// sees them.

import (
	"context"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
)

// Progress is how far a transfer has come: Done of Total, in bytes moved
// where Bytes says so (else in units of its own), and which of several
// it is (Item: "still 2 of 6").
type Progress struct {
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Bytes bool   `json:"bytes,omitempty"`
	Item  string `json:"item,omitempty"`
}

// Step is one step of an operation the Steps note announced: begun (no
// Done, no Error), done, with what it did and how long it took, or
// failed.
type Step struct {
	Index  int    `json:"index"`
	Done   bool   `json:"done,omitempty"`
	TookMS int64  `json:"took_ms,omitempty"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// progressEvery is the most often progress is noted: ten a second.
const progressEvery = 100 * time.Millisecond

// withProgress returns ctx with its transfer's progress noted, as f
// reads it, at most ten times a second and once at the end.
func (c *call) withProgress(ctx context.Context, f func(atem.Progress) Progress) context.Context {
	if !c.req.Notes {
		return ctx
	}
	var last time.Time
	return atem.WithProgress(ctx, func(p atem.Progress) {
		if time.Since(last) < progressEvery && p.Done < p.Total {
			return
		}
		last = time.Now()
		pr := f(p)
		c.note(Note{Progress: &pr})
	})
}

// bytesMoved is a single transfer's progress, in bytes.
func bytesMoved(p atem.Progress) Progress {
	return Progress{Done: int64(p.Done), Total: int64(p.Total), Bytes: true}
}

// steps announces an operation's steps, by the names a person sees.
func (c *call) steps(labels ...string) { c.note(Note{Steps: labels}) }

// step runs step i, noting its start and its end: what it did, or why it
// failed.
func (c *call) step(i int, run func() (detail string, err error)) error {
	start := time.Now()
	c.note(Note{Step: &Step{Index: i}})
	detail, err := run()
	s := Step{Index: i, Done: err == nil, TookMS: time.Since(start).Milliseconds(), Detail: detail}
	if err != nil {
		s.Error = err.Error()
	}
	c.note(Note{Step: &s})
	return err
}
