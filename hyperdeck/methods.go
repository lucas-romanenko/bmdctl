// SPDX-License-Identifier: MIT

package hyperdeck

// bmdwire v1.8.0's Hyperdeck methods (hyperdeckwire/client.py), each a
// command sent with SendAndWait, so a refusal is an *Error as
// HyperdeckError is raised, and its answer read as the method reads it.

import (
	"context"
	"errors"
	"fmt"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

func (s *Session) params(ctx context.Context, c Command) (map[string]string, error) {
	r, err := s.SendAndWait(ctx, c)
	if err != nil {
		return nil, err
	}
	return r.Params(), nil
}

func (s *Session) ok(ctx context.Context, c Command) error {
	_, err := s.SendAndWait(ctx, c)
	return err
}

// DeviceInfo returns device info's fields: model, protocol version,
// unique id, slot count, software version.
func (s *Session) DeviceInfo(ctx context.Context) (map[string]string, error) {
	return s.params(ctx, DeviceInfo{})
}

// RemoteInfo returns whether remote control is enabled and overridden.
func (s *Session) RemoteInfo(ctx context.Context) (map[string]string, error) {
	return s.params(ctx, RemoteInfo{})
}

// SlotInfo returns the active slot's info, or slotID's (nil for the
// active one).
func (s *Session) SlotInfo(ctx context.Context, slotID *int) (map[string]string, error) {
	return s.params(ctx, SlotInfo{SlotID: slotID})
}

// TransportInfo returns the transport: status, speed, slot id, clip id,
// single clip, display timecode, timecode, video format, loop.
func (s *Session) TransportInfo(ctx context.Context) (map[string]string, error) {
	return s.params(ctx, TransportInfo{})
}

// Configuration returns the configuration.
func (s *Session) Configuration(ctx context.Context) (map[string]string, error) {
	return s.params(ctx, Configuration{})
}

// SetConfiguration sets configuration parameters in one line; with none
// it sends nothing, as set_configuration does. A "213 deck rebooting" is
// a success, and the deck may then close the connection.
func (s *Session) SetConfiguration(ctx context.Context, settings ...Setting) error {
	if len(settings) == 0 {
		return nil
	}
	return s.ok(ctx, SetConfiguration{Settings: settings})
}

// DiskList returns the clips on the active disk, or slotID's.
func (s *Session) DiskList(ctx context.Context, slotID *int) ([]Clip, error) {
	r, err := s.SendAndWait(ctx, DiskList{SlotID: slotID})
	if err != nil {
		return nil, err
	}
	return parseClipLines(r.Lines, parseDiskLine), nil
}

// ClipsCount returns how many clips the timeline holds.
func (s *Session) ClipsCount(ctx context.Context) (int, error) {
	p, err := s.params(ctx, ClipsCount{})
	if err != nil {
		return 0, err
	}
	v, ok := p["clip count"]
	if !ok {
		v = "0"
	}
	n, err := pytext.Int(v)
	if err != nil {
		return 0, fmt.Errorf("hyperdeck: clips count: %w", err)
	}
	return int(n), nil
}

// ClipsGet returns the timeline's clips. An empty timeline is none, as
// hyperdeckwire 1.7.1's clips_get has it (bmdwire#57): a deck answers
// clips get on one with 107 timeline empty, not an empty 205, which
// empties the session's timeline too.
func (s *Session) ClipsGet(ctx context.Context) ([]Clip, error) {
	r, err := s.SendAndWait(ctx, ClipsGet{})
	var de *Error
	if errors.As(err, &de) && de.Code == 107 {
		s.do(func() { s.state.Timeline = []Clip{} })
		return []Clip{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseClipLines(r.Lines, parseClipsLine), nil
}

// Stop stops playback or recording.
func (s *Session) Stop(ctx context.Context) error { return s.ok(ctx, Stop{}) }

// PlayOptions are play's arguments: all optional, plain play without
// them. Speed is a percentage, -5000 to 5000. Loop and SingleClip go
// true or false as given and nil leaves them out, as play's do since
// bmdwire 1.8.0 (bmdwire#60).
type PlayOptions struct {
	Loop, SingleClip *bool
	Speed, ClipID    *int
}

// Play starts playback.
func (s *Session) Play(ctx context.Context, o PlayOptions) error {
	return s.ok(ctx, Play{Loop: o.Loop, SingleClip: o.SingleClip, Speed: o.Speed, ClipID: o.ClipID})
}

// Record starts recording on the active slot, into a clip named name,
// or with "" one the deck names from its record prefix (bmdwire#60).
func (s *Session) Record(ctx context.Context, name string) error {
	return s.ok(ctx, Record{Name: name})
}

// Pause holds the current frame (play at speed 0).
func (s *Session) Pause(ctx context.Context) error { return s.ok(ctx, Pause{}) }

// ClipsClear empties the timeline; nothing on disk is deleted.
func (s *Session) ClipsClear(ctx context.Context) error { return s.ok(ctx, ClipsClear{}) }

// ClipsAdd adds a clip to the timeline, at the end or before
// beforeClipID.
func (s *Session) ClipsAdd(ctx context.Context, name string, beforeClipID *int) error {
	return s.ok(ctx, ClipsAdd{Name: name, BeforeClipID: beforeClipID})
}

// ClipsRemove removes a timeline clip.
func (s *Session) ClipsRemove(ctx context.Context, clipID int) error {
	return s.ok(ctx, ClipsRemove{ClipID: clipID})
}

// GotoClip moves to the start of a timeline clip, without playing.
func (s *Session) GotoClip(ctx context.Context, clipID int) error {
	return s.ok(ctx, GotoClip{ClipID: clipID})
}

// GotoClipBy moves offset clips along the timeline, back when negative
// (bmdwire#60); 0 is refused unsent.
func (s *Session) GotoClipBy(ctx context.Context, offset int) error {
	return s.ok(ctx, GotoClipBy{Offset: offset})
}

// GotoTimecode moves to a timecode, HH:MM:SS:FF (;FF drop frame),
// without playing (bmdwire#60); anything else is refused unsent.
func (s *Session) GotoTimecode(ctx context.Context, timecode string) error {
	return s.ok(ctx, GotoTimecode{Timecode: timecode})
}

// Notify returns which notifications are on for this connection, each
// "true" or "false" (209 notify; bmdwire#60).
func (s *Session) Notify(ctx context.Context) (map[string]string, error) {
	return s.params(ctx, Notify{})
}

// SetNotify turns notifications on or off for this connection, in one
// notify: line (bmdwire#60). With nothing to set it sends nothing, as
// set_notify does. What the deck then sends comes through Subscribe,
// where hyperdeckwire's read_notifications returns it.
func (s *Session) SetNotify(ctx context.Context, kinds ...Setting) error {
	if len(kinds) == 0 {
		return nil
	}
	return s.ok(ctx, Notify{Settings: kinds})
}

// RemoteEnable turns remote control on or off.
func (s *Session) RemoteEnable(ctx context.Context, enable bool) error {
	return s.ok(ctx, RemoteEnable{Enable: enable})
}

// SlotSelect makes a slot active.
func (s *Session) SlotSelect(ctx context.Context, slotID int) error {
	return s.ok(ctx, SlotSelect{SlotID: slotID})
}

// Ping checks the deck answers.
func (s *Session) Ping(ctx context.Context) error { return s.ok(ctx, Ping{}) }
