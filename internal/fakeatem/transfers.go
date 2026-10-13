// SPDX-License-Identifier: MIT

package fakeatem

// The media and macro stores, their locks and file transfers: enough of a
// switcher for still and macro downloads, uploads and locked clears,
// including the FTDE statuses a session must recover from and a lock held
// by another client. Wire layouts are atemwire's (bmdwire v1.3.0
// messages/lock.py and file_transfer.py); the timing and windowing are the
// fake's own.

import (
	"bytes"
	"encoding/binary"
)

// The stores.
const (
	storeStills uint16 = 0
	storeMacros uint16 = 0xFFFF
)

// Transfer sizes.
var (
	ChunkSize  = 1296 // bytes per FTDa, both ways (a multiple of 8)
	ChunkCount = 16   // chunks per FTCD budget, and downloads in flight
)

// item is what a store slot holds.
type item struct {
	data        []byte // as transferred: RLE for a still, bytecode for a macro
	name        string
	description string
	hash        [16]byte
}

// xfer is a transfer in progress.
type xfer struct {
	store, slot uint16
	upload      bool

	// Downloads.
	data     []byte // what is left to send
	inFlight int    // chunks sent and not yet acknowledged

	// Uploads.
	length     int // the uncompressed length FTSD announced
	got        []byte
	decoded    int // uncompressed bytes in got
	budgetLeft int
}

// PutStill fills still slot slot with data, RLE-encoded as the switcher
// sends it, and broadcasts its MPfe.
func (sw *Switcher) PutStill(slot uint16, data []byte, name string, hash [16]byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.stills[slot] = &item{data: bytes.Clone(data), name: name, hash: hash}
	sw.broadcast(fileInfo(slot, sw.stills[slot]))
}

// Still returns what still slot slot holds: the RLE-encoded frame as
// uploaded, its name and the MD5 the uploader sent.
func (sw *Switcher) Still(slot uint16) (data []byte, name string, hash [16]byte, ok bool) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	it := sw.stills[slot]
	if it == nil {
		return nil, "", hash, false
	}
	return bytes.Clone(it.data), it.name, it.hash, true
}

// PutMacro fills macro slot slot and broadcasts its MPrp.
func (sw *Switcher) PutMacro(slot uint16, bytecode []byte, name, description string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.macros[slot] = &item{data: bytes.Clone(bytecode), name: name, description: description}
	sw.broadcast(macroProperties(slot, sw.macros[slot]))
}

// Macro returns what macro slot slot holds.
func (sw *Switcher) Macro(slot uint16) (bytecode []byte, name, description string, ok bool) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	it := sw.macros[slot]
	if it == nil {
		return nil, "", "", false
	}
	return bytes.Clone(it.data), it.name, it.description, true
}

// FailTransfers answers the next transfer requests (FTSU or FTSD), one
// each, with FTDE carrying these statuses instead of serving them. Status 5
// (no lock) also takes the session's lock away, silently; status 6 (another
// session collided) leaves it held, as a switcher does.
func (sw *Switcher) FailTransfers(statuses ...uint8) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.failNext = append(sw.failNext, statuses...)
}

// HoldUpload holds the next upload part way: once its first budget of
// chunks has arrived, the next budget (FTCD) waits until release is
// closed. The upload has begun and reported progress, and can't end
// until then.
func (sw *Switcher) HoldUpload(release <-chan struct{}) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.uploadHeld = release
}

// LockedBySession reports whether the session holds store's lock.
func (sw *Switcher) LockedBySession(store uint16) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.locks[store] == heldBySession
}

// grant gives store's lock to the session: LKOB to it, LKST to everyone.
func (sw *Switcher) grant(store uint16) [][]byte {
	sw.locks[store] = heldBySession
	delete(sw.lockQueued, store)
	return [][]byte{Command("LKOB", []byte{byte(store >> 8), byte(store), 0, 0}), lockState(store, true)}
}

// transferEcho answers the session's lock, transfer and clear commands.
// sw.mu is held.
func (sw *Switcher) transferEcho(code string, c []byte) [][]byte {
	switch code {
	case "PLCK", "LOCK":
		if len(c) < 4 {
			return nil
		}
		store := binary.BigEndian.Uint16(c)
		if code == "LOCK" && c[2] == 0 {
			if sw.locks[store] != heldBySession {
				return nil
			}
			delete(sw.locks, store)
			return [][]byte{lockState(store, false)}
		}
		switch sw.locks[store] {
		case 0, heldBySession:
			return sw.grant(store)
		}
		// Held by another client: a PLCK waits for its release; a LOCK is
		// dropped, as atemwire's notes say a switcher drops it.
		if code == "PLCK" {
			sw.lockQueued[store] = true
		}
		return nil
	case "CSTL":
		if len(c) < 1 || sw.locks[storeStills] != heldBySession {
			return nil // some models ignore a CSTL from a session without the lock
		}
		slot := uint16(c[0])
		delete(sw.stills, slot)
		return [][]byte{fileInfo(slot, nil)}
	case "FTSU":
		if len(c) < 8 {
			return nil
		}
		tid, store, slot := binary.BigEndian.Uint16(c), binary.BigEndian.Uint16(c[2:]), uint16(binary.BigEndian.Uint32(c[4:]))
		if r := sw.refuse(tid, store); r != nil {
			return r
		}
		var data []byte
		if it := sw.store(store)[slot]; it != nil {
			data = bytes.Clone(it.data)
		}
		x := &xfer{store: store, slot: slot, data: data}
		sw.xfers[tid] = x
		return sw.sendChunks(tid, x)
	case "FTUA":
		if len(c) < 2 {
			return nil
		}
		tid := binary.BigEndian.Uint16(c)
		x := sw.xfers[tid]
		if x == nil || x.upload {
			return nil
		}
		x.inFlight--
		return sw.sendChunks(tid, x)
	case "FTSD":
		if len(c) < 14 {
			return nil
		}
		tid, store, slot := binary.BigEndian.Uint16(c), binary.BigEndian.Uint16(c[2:]), binary.BigEndian.Uint16(c[6:])
		length, mode := int(binary.BigEndian.Uint32(c[8:])), binary.BigEndian.Uint16(c[12:])
		if r := sw.refuse(tid, store); r != nil {
			return r
		}
		if want := map[uint16]uint16{storeStills: 1, storeMacros: 0x0300}[store]; mode != want {
			return [][]byte{transferError(tid, 2)}
		}
		x := &xfer{store: store, slot: slot, upload: true, length: length}
		sw.xfers[tid] = x
		if length == 0 {
			return nil // no budget: FTFD follows at once
		}
		return [][]byte{sw.budget(tid, x)}
	case "FTDa":
		if len(c) < 4 {
			return nil
		}
		tid, size := binary.BigEndian.Uint16(c), int(binary.BigEndian.Uint16(c[2:]))
		x := sw.xfers[tid]
		if x == nil || !x.upload {
			return nil
		}
		chunk := c[4:min(len(c), 4+size)]
		x.got = append(x.got, chunk...)
		x.decoded += decodedLen(x.store, chunk)
		if x.budgetLeft--; x.budgetLeft == 0 && x.decoded < x.length {
			if release := sw.uploadHeld; release != nil {
				sw.uploadHeld = nil
				go func() {
					<-release
					sw.mu.Lock()
					b := sw.budget(tid, x)
					sw.mu.Unlock()
					sw.Send(b)
				}()
				return nil
			}
			return [][]byte{sw.budget(tid, x)}
		}
		return nil
	case "FTFD":
		if len(c) < 210 {
			return nil
		}
		tid := binary.BigEndian.Uint16(c)
		x := sw.xfers[tid]
		if x == nil || !x.upload {
			return nil
		}
		delete(sw.xfers, tid)
		it := &item{data: x.got, name: cstring(c[2:66]), description: cstring(c[66:194])}
		copy(it.hash[:], c[194:210])
		done := Command("FTDC", []byte{byte(tid >> 8), byte(tid), 1, 2})
		// Empty bytecode is stored too, a macro with its name and no ops:
		// a Constellation keeps one (docs/HARDWARE.md, the tenth run).
		sw.store(x.store)[x.slot] = it
		if x.store == storeMacros {
			return [][]byte{done, macroProperties(x.slot, it)}
		}
		return [][]byte{done, fileInfo(x.slot, it)}
	}
	return nil
}

// refuse checks a transfer request: a still transfer needs the session to
// hold the store's lock, and FailTransfers may have a status waiting.
func (sw *Switcher) refuse(tid, store uint16) [][]byte {
	if store == storeStills && sw.locks[store] != heldBySession {
		return [][]byte{transferError(tid, 5)}
	}
	if len(sw.failNext) == 0 {
		return nil
	}
	status := sw.failNext[0]
	sw.failNext = sw.failNext[1:]
	if status == 5 && sw.locks[store] == heldBySession {
		delete(sw.locks, store)
	}
	return [][]byte{transferError(tid, status)}
}

func (sw *Switcher) store(store uint16) map[uint16]*item {
	if store == storeMacros {
		return sw.macros
	}
	return sw.stills
}

// sendChunks sends a download's next chunks, up to ChunkCount in flight,
// and FTDC once every chunk has been acknowledged.
func (sw *Switcher) sendChunks(tid uint16, x *xfer) [][]byte {
	var out [][]byte
	for x.inFlight < ChunkCount && len(x.data) > 0 {
		n := min(ChunkSize, len(x.data))
		p := make([]byte, 4+n)
		binary.BigEndian.PutUint16(p, tid)
		binary.BigEndian.PutUint16(p[2:], uint16(n))
		copy(p[4:], x.data[:n])
		x.data = x.data[n:]
		x.inFlight++
		out = append(out, Command("FTDa", p))
	}
	if len(x.data) == 0 && x.inFlight <= 0 {
		delete(sw.xfers, tid)
		out = append(out, Command("FTDC", []byte{byte(tid >> 8), byte(tid), 1, 2}))
	}
	return out
}

// budget grants an upload ChunkCount chunks of ChunkSize bytes.
func (sw *Switcher) budget(tid uint16, x *xfer) []byte {
	x.budgetLeft = ChunkCount
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p, tid)
	binary.BigEndian.PutUint16(p[6:], uint16(ChunkSize))
	binary.BigEndian.PutUint16(p[8:], uint16(ChunkCount))
	return Command("FTCD", p)
}

// decodedLen is how many bytes a chunk of an upload stands for: a still's
// chunks are RLE (a run is never split across chunks), a macro's are not.
func decodedLen(store uint16, chunk []byte) int {
	if store != storeStills {
		return len(chunk)
	}
	n := 0
	for len(chunk) >= 8 {
		if bytes.Equal(chunk[:8], bytes.Repeat([]byte{0xFE}, 8)) && len(chunk) >= 24 {
			n += 8 * int(binary.BigEndian.Uint64(chunk[8:]))
			chunk = chunk[24:]
			continue
		}
		n += 8
		chunk = chunk[8:]
	}
	return n
}

func transferError(tid uint16, status uint8) []byte {
	return Command("FTDE", []byte{byte(tid >> 8), byte(tid), status, 0})
}

// fileInfo is a still slot's MPfe; nil means empty.
func fileInfo(slot uint16, it *item) []byte {
	p := make([]byte, 24)
	binary.BigEndian.PutUint16(p[2:], slot)
	if it != nil {
		p[4] = 1
		copy(p[5:21], it.hash[:])
		name := []byte(it.name)[:min(len(it.name), 255)]
		p[23] = byte(len(name))
		p = append(p, name...)
	}
	return Command("MPfe", p)
}

// macroProperties is a macro slot's MPrp; nil means empty.
func macroProperties(slot uint16, it *item) []byte {
	p := make([]byte, 8)
	binary.BigEndian.PutUint16(p, slot)
	if it != nil {
		p[2] = 1
		binary.BigEndian.PutUint16(p[4:], uint16(len(it.name)))
		binary.BigEndian.PutUint16(p[6:], uint16(len(it.description)))
		p = append(append(p, it.name...), it.description...)
	}
	return Command("MPrp", p)
}

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
