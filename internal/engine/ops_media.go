// SPDX-License-Identifier: MIT

package engine

// The media pool: listing it, and moving stills through the session's
// transfer engine (atem/transfer.go). Files travel in the request and
// response frames; the engine decodes and encodes the images.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the formats a still may be uploaded from
	_ "image/jpeg"
	"image/png"
	"path"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

func slotParam(max float64, help string) table.Param {
	lo, hi := table.Bounds(0, max)
	return table.Param{Name: "slot", Kind: table.Uint, Min: lo, Max: hi, Required: true, OneBased: true, Help: help}
}

func mediaOperations() []operation {
	return []operation{
		{entry: table.Entry{Path: "media list", Summary: "List the media pool's stills and what each media player shows.",
			Params: []table.Param{{Name: "all", Kind: table.Bool, Default: "false",
				Help: "at a terminal, the empty slots too (a pipe and --json always have them)"}}},
			run: opMediaList},
		{entry: table.Entry{Path: "media upload", Summary: "Upload a PNG, JPEG or GIF to a still slot, fitted to the video mode.",
			Echo: "MPfe:<slot>",
			Params: []table.Param{slotParam(0xFFFF, "still slot; replacing a used one needs --yes"),
				{Name: "file", Kind: table.File, Required: true, Help: "the image; - reads stdin"},
				{Name: "name", Kind: table.String, Help: "the slot's name (default: the file's, without extension)"},
				{Name: "description", Kind: table.String, Help: "the slot's description"},
				{Name: "premultiply", Kind: table.Bool, Help: "premultiply colour by alpha (default: for PNG, as WebATEM does)"}}},
			run: opMediaUpload},
		{entry: table.Entry{Path: "media download", Summary: "Download a still slot as a PNG.",
			Syntax: "media download <slot> [file]",
			Params: []table.Param{slotParam(0xFFFF, "still slot"),
				{Name: "file", Kind: table.OutFile, Help: "where to write it; - is stdout (default: the slot's name .png)"}}},
			run: opMediaDownload},
		{entry: table.Entry{Path: "media clear", Summary: "Empty a still slot, under the media pool's lock.", Destructive: true,
			Echo: "MPfe:<slot>", Params: []table.Param{slotParam(0xFF, "still slot")}},
			run: opMediaClear},
	}
}

// StillEntry is one still slot.
type StillEntry struct {
	Slot table.Index `json:"slot"`
	Used bool        `json:"used"`
	Name string      `json:"name,omitempty"`
	Hash string      `json:"hash,omitempty"`
}

// PlayerEntry is what one media player shows.
type PlayerEntry struct {
	Player table.Index `json:"player"`
	Type   string      `json:"type"` // still or clip
	Slot   table.Index `json:"slot"`
	Name   string      `json:"name,omitempty"`
}

// MediaList is the media pool.
type MediaList struct {
	Stills  []StillEntry  `json:"stills"`
	Clips   int           `json:"clips"`
	Players []PlayerEntry `json:"players"`
}

func opMediaList(c *call) (any, bool, error) {
	st := c.state
	r := MediaList{Stills: []StillEntry{}, Players: []PlayerEntry{}}
	for slot := range st.MediaplayerSlots() {
		v := st.MediaplayerSlotInfo(slot)
		r.Stills = append(r.Stills, StillEntry{Slot: table.Index(slot), Used: v.IsUsed, Name: v.Name, Hash: v.Hash})
	}
	if s := st.MediaPool.Slots; s != nil {
		r.Clips = int(s.Clips)
	}
	for i, p := range st.MediaPlayers {
		if p == nil {
			continue
		}
		v := st.MediaplayerSelected(i)
		e := PlayerEntry{Player: table.Index(i), Type: "still", Slot: table.Index(v.Slot)}
		if v.SourceType == 2 {
			e.Type = "clip"
		} else {
			e.Name = st.MediaplayerSlotInfo(v.Slot).Name
		}
		r.Players = append(r.Players, e)
	}
	return r, false, nil
}

// TransferResult is what a transfer moved.
type TransferResult struct {
	Slot        table.Index `json:"slot"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	Bytes       int         `json:"bytes"`           // the file's size
	Width       int         `json:"width,omitempty"` // a still's frame
	Height      int         `json:"height,omitempty"`
	Source      string      `json:"source,omitempty"` // the image's size before fitting, for an upload
	Replaced    string      `json:"replaced,omitempty"`
}

// transferErr gives a transfer's error its code: refused by the switcher
// or held by another client is rejected, a timeout with the transfer under
// way is no echo, the session gone is unreachable.
func transferErr(err error) error {
	var te *atem.TransferError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &te), errors.Is(err, atem.ErrLockNotGranted):
		return &Error{CodeRejected, err}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{CodeNoEcho, fmt.Errorf("the switcher did not finish the transfer in time: %w", err)}
	}
	return sessionErr(err)
}

// transferCtx is the context a transfer runs under: the request's, with
// --timeout as its deadline when given (else the library's default).
func (c *call) transferCtx() (context.Context, context.CancelFunc) {
	if c.req.TimeoutMS > 0 {
		return context.WithTimeout(c.ctx, c.req.timeout())
	}
	return context.WithCancel(c.ctx)
}

func opMediaUpload(c *call) (any, bool, error) {
	slot := int(c.args.Value("slot").(uint64))
	data := c.req.Files["file"]
	if len(data) == 0 {
		return nil, false, codeErr(CodeUsage, "%s: no image (empty file)", c.entry.Label("file"))
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, codeErr(CodeUsage, "%s %s: not a PNG, JPEG or GIF image: %v", c.entry.Label("file"), c.args.Value("file"), err)
	}
	if n := c.state.MediaplayerSlots(); n > 0 && slot >= n {
		return nil, false, codeErr(CodeUsage, "%s %v: the media pool has %d stills, 1-%d", c.entry.Label("slot"), table.Index(slot), n, n)
	}
	r := TransferResult{Slot: table.Index(slot), Bytes: len(data), Source: fmt.Sprintf("%dx%d %s", img.Bounds().Dx(), img.Bounds().Dy(), format)}
	if old := c.state.MediaplayerSlotInfo(slot); old.IsUsed {
		if !c.req.Yes {
			return nil, false, c.refuse(c.stillImpact(slot), "replace it?", "still slot %v holds %q: add --yes to replace it", table.Index(slot), old.Name)
		}
		r.Replaced = old.Name
	}
	opts := atem.StillOptions{Premultiply: format == "png"} // WebATEM premultiplies PNG files only
	if c.args.Has("premultiply") {
		opts.Premultiply = c.args.Value("premultiply").(bool)
	}
	file := c.args.Value("file").(string)
	opts.Name = strings.TrimSuffix(path.Base(strings.ReplaceAll(file, `\`, "/")), path.Ext(file))
	if file == "-" {
		opts.Name = "still"
	}
	if c.args.Has("name") {
		opts.Name = c.args.Value("name").(string)
	}
	if c.args.Has("description") {
		opts.Description = c.args.Value("description").(string)
	}
	r.Name, r.Description = opts.Name, opts.Description
	if m := c.state.VideoMode.Mode; m != nil {
		r.Width, r.Height = m.FrameSize()
	}
	ctx, cancel := c.transferCtx()
	defer cancel()
	if err := c.sess.UploadStill(c.withProgress(ctx, bytesMoved), slot, img, opts); err != nil {
		return nil, false, transferErr(err)
	}
	return r, true, nil
}

func opMediaDownload(c *call) (any, bool, error) {
	slot := int(c.args.Value("slot").(uint64))
	info := c.state.MediaplayerSlotInfo(slot)
	if !info.IsUsed {
		return nil, false, codeErr(CodeUsage, "still slot %v is empty", table.Index(slot))
	}
	m := c.state.VideoMode.Mode
	if m == nil {
		return nil, false, codeErr(CodeUnreachable, "the switcher hasn't reported its video mode")
	}
	w, h := m.FrameSize()
	ctx, cancel := c.transferCtx()
	defer cancel()
	frame, err := c.sess.DownloadStill(c.withProgress(ctx, bytesMoved), slot)
	if err != nil {
		return nil, false, transferErr(err)
	}
	img, err := atem.FrameImage(frame, w, h)
	if err != nil {
		return nil, false, codeErr(CodeRejected, "still slot %v: %v (was it stored at another video mode?)", table.Index(slot), err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false, err
	}
	f := FileFrame{Param: "file", Data: buf.Bytes()}
	if !c.args.Has("file") {
		f.Name = fileName(info.Name, "still-"+table.Index(slot).String()) + ".png"
	}
	if err := c.out.file(f); err != nil {
		return nil, false, err
	}
	return TransferResult{Slot: table.Index(slot), Name: info.Name, Bytes: buf.Len(), Width: w, Height: h}, true, nil
}

// fileName makes a name a file name: no path separators or control
// characters, def when nothing is left.
func fileName(name, def string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if name == "" {
		return def
	}
	return name
}

// opMediaClear is atemwire's clear_still: the clear under the store lock.
// The session returns once the CSTL is sent; the engine then waits for the
// slot's MPfe to report it empty, the confirmation.
func opMediaClear(c *call) (any, bool, error) {
	slot := int(c.args.Value("slot").(uint64))
	name := c.state.MediaplayerSlotInfo(slot).Name
	changes, cancel := c.sess.Subscribe()
	defer cancel()
	ctx, cancelT := c.transferCtx()
	defer cancelT()
	if err := c.sess.ClearStill(ctx, slot); err != nil {
		return nil, false, transferErr(err)
	}
	r := TransferResult{Slot: table.Index(slot), Replaced: name}
	key := fmt.Sprintf("MPfe:%d", slot)
	timer := time.NewTimer(c.req.timeout())
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return nil, false, codeErr(CodeUnreachable, "%v", c.ctx.Err())
		case <-timer.C:
			return nil, false, codeErr(CodeNoEcho, "sent, but the switcher did not report still slot %v empty (%s) within %v", table.Index(slot), key, c.req.timeout())
		case ch, ok := <-changes:
			if !ok {
				return nil, false, codeErr(CodeUnreachable, "the session closed")
			}
			if f, isFile := ch.Value.(*atem.MediaplayerFileInfoField); isFile && ch.Key == key && !f.IsUsed {
				return r, true, nil
			}
		}
	}
}
