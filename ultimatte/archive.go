// SPDX-License-Identifier: MIT

package ultimatte

// Archive and restore, bmdwire v1.6.0's ultimattewire/profile/archive.py
// and restore.py: the zip Smart Remote 4 writes with Archive All and
// reads with Restore.

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// The binary channel's opcodes: reads, and their mirrors, writes.
const (
	OpReadSlot      = 0x0000
	OpReadResource  = 0x0003
	OpWriteSlot     = 0x0100
	OpWriteResource = 0x0103
)

// Resources are the two named resources an archive holds.
var Resources = []string{"GPISettings", "SavedSettings"}

// WireNamePrefix is the path a write's name starts with: the shape of
// Ultimatte Software Control's temporary directory, which the unit is
// believed to ignore but for the last segment and, for a slot, the
// presets/ anchor.
const WireNamePrefix = "/private/var/folders/00/000000000000000000000000000000/T/Ultimatte Software Control-PYREST"

// ArchiveInfo is archive_unit_to_bytes's info: the label, the FILE
// LIST's slots (duplicates kept), each slot's blob size where it was
// read, the two resources' sizes (0 where not read) and a warning per
// read that failed, which leaves that member out.
type ArchiveInfo struct {
	Label     string         `json:"label"`
	Slots     []string       `json:"slots"`
	SlotSizes map[string]int `json:"slot_sizes"`
	GPISize   int            `json:"gpi_size"`
	SavedSize int            `json:"saved_size"`
	Warnings  []string       `json:"warnings"`
}

// Archive is archive_unit_to_bytes: every preset slot the prelude's
// FILE LIST names and the GPISettings and SavedSettings resources, in a
// zip of Smart Remote's layout, with the annotated prelude riding along
// as <label>_config_readable.txt. A read that fails is a warning and its
// member is left out, never written as a placeholder a restore would
// push into live hardware. An empty prelude is an *Error; so is one
// AnnotatePreamble can't annotate (a value of inf), after every read.
// now is the members' time.
func (c *Client) Archive(ctx context.Context, host string, now time.Time) ([]byte, ArchiveInfo, error) {
	pre, err := c.ReadPrelude(ctx, host)
	if err != nil {
		return nil, ArchiveInfo{}, err
	}
	if pre == "" {
		return nil, ArchiveInfo{}, ioError(ErrShort, "empty preamble (unit unreachable or not responding)")
	}
	info := ArchiveInfo{Label: ExtractLabel(pre), Slots: ParseFileList(pre), SlotSizes: map[string]int{}, Warnings: []string{}}
	var order []string // the slots read, in their first place
	blobs := map[string][]byte{}
	for _, slot := range info.Slots {
		blob, err := c.BinaryRequest(ctx, host, OpReadSlot, []byte(slot))
		if err != nil {
			if ctx.Err() != nil {
				return nil, info, ctx.Err()
			}
			info.Warnings = append(info.Warnings, fmt.Sprintf("preset %s: %v", pytext.Repr(slot), err))
			continue
		}
		if _, ok := blobs[slot]; !ok {
			order = append(order, slot)
		}
		blobs[slot] = blob
		info.SlotSizes[slot] = len(blob)
	}
	resources := map[string][]byte{}
	for _, name := range Resources {
		blob, err := c.BinaryRequest(ctx, host, OpReadResource, []byte(name))
		if err != nil {
			if ctx.Err() != nil {
				return nil, info, ctx.Err()
			}
			info.Warnings = append(info.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		resources[name] = blob
	}
	info.GPISize, info.SavedSize = len(resources["GPISettings"]), len(resources["SavedSettings"])
	readable, err := AnnotatePreamble(pre)
	if err != nil {
		return nil, info, err
	}
	w := newZipWriter(now)
	w.dir("images/")
	for _, name := range Resources {
		if blob, ok := resources[name]; ok {
			w.file(name, blob)
		}
	}
	w.dir("quickfiles/")
	w.dir("presets/")
	for _, slot := range order {
		w.file("presets/"+slot, blobs[slot])
	}
	w.file(info.Label+"_config_readable.txt", []byte(readable))
	z, err := w.close()
	return z, info, err
}

// zipWriter writes members as CPython 3.12's zipfile writes them into a
// ZIP_DEFLATED archive: no data descriptors and no extra fields, made by
// 2.0 on Unix, needing 2.0, the UTF-8 flag for a name that isn't ASCII,
// local times; a directory stored, empty, 0o40755; a file deflated at
// zlib's default level, 0o600. The deflated bytes are Go's, not zlib's:
// they inflate to the same.
type zipWriter struct {
	buf  bytes.Buffer
	zw   *zip.Writer
	date uint16
	time uint16
	err  error
}

func newZipWriter(now time.Time) *zipWriter {
	w := &zipWriter{}
	w.zw = zip.NewWriter(&w.buf)
	// MS-DOS date and time, two seconds a step, as time.localtime gives.
	w.date = uint16((now.Year()-1980)<<9 | int(now.Month())<<5 | now.Day())
	w.time = uint16(now.Hour()<<11 | now.Minute()<<5 | now.Second()/2)
	return w
}

func (w *zipWriter) header(name string) *zip.FileHeader {
	h := &zip.FileHeader{Name: name, CreatorVersion: 3<<8 | 20, ReaderVersion: 20, ModifiedDate: w.date, ModifiedTime: w.time}
	if !isASCII(name) {
		h.Flags |= 0x800
	}
	return h
}

func (w *zipWriter) dir(name string) {
	h := w.header(name)
	h.Method = zip.Store
	h.ExternalAttrs = 0o40755 << 16
	if w.err == nil {
		_, w.err = w.zw.CreateRaw(h)
	}
}

func (w *zipWriter) file(name string, data []byte) {
	if w.err != nil {
		return
	}
	var comp bytes.Buffer
	fw, _ := flate.NewWriter(&comp, flate.DefaultCompression)
	fw.Write(data)
	fw.Close()
	h := w.header(name)
	h.Method = zip.Deflate
	h.ExternalAttrs = 0o600 << 16
	h.CRC32 = crc32.ChecksumIEEE(data)
	h.CompressedSize64, h.UncompressedSize64 = uint64(comp.Len()), uint64(len(data))
	var out io.Writer
	if out, w.err = w.zw.CreateRaw(h); w.err == nil {
		_, w.err = out.Write(comp.Bytes())
	}
}

func (w *zipWriter) close() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	if err := w.zw.Close(); err != nil {
		return nil, err
	}
	return w.buf.Bytes(), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Pushed is one member a restore pushed, or tried to.
type Pushed struct {
	OK    bool    `json:"ok"`
	Error *string `json:"error"`
}

// PresetPushed is a preset slot a restore pushed.
type PresetPushed struct {
	Slot string `json:"slot"`
	Pushed
}

// RestoreInfo is restore_unit_from_bytes's info: each preset pushed,
// each resource (nil where the zip has none, or the run stopped before
// it), and where it stopped: "preset:<slot>", "GPISettings",
// "SavedSettings", or nil when it didn't.
type RestoreInfo struct {
	Presets   []PresetPushed `json:"presets"`
	GPI       *Pushed        `json:"gpi"`
	Saved     *Pushed        `json:"saved"`
	StoppedAt *string        `json:"stopped_at"`
}

// RestorePlan is what a restore of a zip would push: its presets, in the
// order they go, and whether it has each resource.
type RestorePlan struct {
	Presets []string
	GPI     bool
	Saved   bool
}

// zipMembers reads a zip as zipfile does: its names (UTF-8 under flag
// 0x800, else code page 437), in the central directory's order, and each
// name's contents (the last member of a name, as getinfo finds it).
func zipMembers(b []byte) ([]string, map[string]*zip.File, error) {
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, nil, &Error{Class: "BadZipFile", Msg: "File is not a zip file", kind: ErrValue}
	}
	var names []string
	files := map[string]*zip.File{}
	for _, f := range r.File {
		name := f.Name
		if f.Flags&0x800 == 0 && !isASCII(name) {
			name = cp437(name)
		}
		names = append(names, name)
		files[name] = f
	}
	return names, files, nil
}

// Plan is what Restore would push from zip, in its order: every member
// under presets/ that isn't a directory, sorted, then GPISettings and
// SavedSettings where the zip has them. Anything else is ignored.
func Plan(zipBytes []byte) (RestorePlan, error) {
	names, _, err := zipMembers(zipBytes)
	if err != nil {
		return RestorePlan{}, err
	}
	var p RestorePlan
	for _, n := range names {
		if strings.HasPrefix(n, "presets/") && !strings.HasSuffix(n, "/") {
			p.Presets = append(p.Presets, n)
		}
	}
	slices.Sort(p.Presets)
	p.GPI, p.Saved = slices.Contains(names, "GPISettings"), slices.Contains(names, "SavedSettings")
	return p, nil
}

// Restore is restore_unit_from_bytes: the zip's presets pushed with
// OpWriteSlot, then GPISettings, then SavedSettings (which overwrites the
// live state, so last) with OpWriteResource, each named under
// WireNamePrefix as Ultimatte Software Control names them. The first
// push that fails stops it: ok is false and info says where. step, when
// not nil, is told each member before it is pushed. A zip that isn't one
// is an *Error.
func (c *Client) Restore(ctx context.Context, host string, zipBytes []byte, step func(member string)) (bool, RestoreInfo, error) {
	info := RestoreInfo{Presets: []PresetPushed{}}
	plan, err := Plan(zipBytes)
	if err != nil {
		return false, info, err
	}
	_, files, _ := zipMembers(zipBytes)
	// A member that can't be read fails the restore, as zf.read raises
	// outside the upload's try; only a push that fails is a stop.
	push := func(member string, opcode uint16, wire string) (error, error) {
		blob, err := read(files[member])
		if err != nil {
			return nil, err
		}
		if step != nil {
			step(member)
		}
		return c.UploadOne(ctx, host, opcode, WireNamePrefix+"/"+wire, blob), nil
	}
	failed := func(err error) Pushed { msg := err.Error(); return Pushed{Error: &msg} }
	for _, member := range plan.Presets {
		_, slot, _ := strings.Cut(member, "/")
		err, rerr := push(member, OpWriteSlot, "presets/"+slot)
		if rerr != nil {
			return false, info, rerr
		}
		if err != nil {
			if ctx.Err() != nil {
				return false, info, ctx.Err()
			}
			info.Presets = append(info.Presets, PresetPushed{slot, failed(err)})
			at := "preset:" + slot
			info.StoppedAt = &at
			return false, info, nil
		}
		info.Presets = append(info.Presets, PresetPushed{slot, Pushed{OK: true}})
	}
	for _, r := range []struct {
		name string
		has  bool
		dst  **Pushed
	}{{"GPISettings", plan.GPI, &info.GPI}, {"SavedSettings", plan.Saved, &info.Saved}} {
		if !r.has {
			continue
		}
		err, rerr := push(r.name, OpWriteResource, r.name)
		if rerr != nil {
			return false, info, rerr
		}
		if err != nil {
			if ctx.Err() != nil {
				return false, info, ctx.Err()
			}
			p := failed(err)
			*r.dst = &p
			name := r.name
			info.StoppedAt = &name
			return false, info, nil
		}
		*r.dst = &Pushed{OK: true}
	}
	return true, info, nil
}

func read(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// cp437 decodes a name as code page 437, as zipfile does a name without
// the UTF-8 flag.
func cp437(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp437High[c-0x80])
		}
	}
	return b.String()
}

var cp437High = []rune("ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜ¢£¥₧ƒáíóúñÑªº¿⌐¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■ ")
