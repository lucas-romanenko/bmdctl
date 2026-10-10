// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// SURFACE.md §1.19 lock and file transfer, from bmdwire v1.3.0
// atemwire/messages/lock.py and file_transfer.py: the building blocks of
// the transfer flows (transfer.go). None has an echo of its own; their
// replies drive the transfer engine.

// Stores (Lock.Store and the transfer requests).
const (
	StoreStills uint16 = 0
	StoreMacros uint16 = 0xFFFF
)

// Upload modes (TransferUploadRequest.Mode).
const (
	UploadWriteRLE uint16 = 1
	UploadWrite    uint16 = 256
	UploadErase    uint16 = 512
)

// Lock is LOCK (LockCommand): request (true) or release (false) a whole
// store's lock.
type Lock struct {
	Store uint16
	State bool
}

func (c Lock) Opcode() string    { return "LOCK" }
func (c Lock) Encode() []byte    { return c.build().encode() }
func (c Lock) Validate() error   { return c.build().err }
func (c Lock) EchoKey() string   { return "" }
func (c Lock) Unverified() bool  { return false }
func (c Lock) Destructive() bool { return false }

func (c Lock) build() *wire {
	w := newWire("LOCK", 4)
	w.u16(0, "store", int64(c.Store))
	w.flag(2, c.State)
	return w
}

// PartialLock is PLCK (PartialLockCommand): request one slot's lock. The
// trailer ff 01 is constant.
type PartialLock struct {
	Store, Slot uint16
}

func (c PartialLock) Opcode() string    { return "PLCK" }
func (c PartialLock) Encode() []byte    { return c.build().encode() }
func (c PartialLock) Validate() error   { return c.build().err }
func (c PartialLock) EchoKey() string   { return "" }
func (c PartialLock) Unverified() bool  { return false }
func (c PartialLock) Destructive() bool { return false }

func (c PartialLock) build() *wire {
	w := newWire("PLCK", 8)
	w.u16(0, "store", int64(c.Store))
	w.u16(2, "slot", int64(c.Slot))
	w.b[4], w.b[5] = 0xff, 0x01
	return w
}

// TransferDownloadRequest is FTSU (TransferDownloadRequestCommand): ask for
// a slot's contents. Byte 8 is 0x03 for the macro store; bytes 9-11 are
// the constants d0 9b 8c.
type TransferDownloadRequest struct {
	Transfer, Store uint16
	Slot            uint32
}

func (c TransferDownloadRequest) Opcode() string    { return "FTSU" }
func (c TransferDownloadRequest) Encode() []byte    { return c.build().encode() }
func (c TransferDownloadRequest) Validate() error   { return c.build().err }
func (c TransferDownloadRequest) EchoKey() string   { return "" }
func (c TransferDownloadRequest) Unverified() bool  { return false }
func (c TransferDownloadRequest) Destructive() bool { return false }

func (c TransferDownloadRequest) build() *wire {
	w := newWire("FTSU", 12)
	w.u16(0, "transfer", int64(c.Transfer))
	w.u16(2, "store", int64(c.Store))
	w.u32(4, "slot", int64(c.Slot))
	if c.Store == StoreMacros {
		w.b[8] = 0x03
	}
	w.b[9], w.b[10], w.b[11] = 0xd0, 0x9b, 0x8c
	return w
}

// TransferUploadRequest is FTSD (TransferUploadRequestCommand): announce an
// upload of Length bytes. Mode is an Upload* constant.
type TransferUploadRequest struct {
	Transfer, Store, Slot uint16
	Length                uint32
	Mode                  uint16
}

func (c TransferUploadRequest) Opcode() string    { return "FTSD" }
func (c TransferUploadRequest) Encode() []byte    { return c.build().encode() }
func (c TransferUploadRequest) Validate() error   { return c.build().err }
func (c TransferUploadRequest) EchoKey() string   { return "" }
func (c TransferUploadRequest) Unverified() bool  { return false }
func (c TransferUploadRequest) Destructive() bool { return false }

func (c TransferUploadRequest) build() *wire {
	w := newWire("FTSD", 16)
	w.u16(0, "transfer", int64(c.Transfer))
	w.u16(2, "store", int64(c.Store))
	w.u16(6, "slot", int64(c.Slot))
	w.u32(8, "length", int64(c.Length))
	w.u16(12, "mode", int64(c.Mode))
	return w
}

// TransferData is FTDa (TransferDataCommand): one upload chunk.
type TransferData struct {
	Transfer uint16
	Data     []byte
}

func (c TransferData) Opcode() string    { return "FTDa" }
func (c TransferData) Encode() []byte    { return c.build().encode() }
func (c TransferData) Validate() error   { return c.build().err }
func (c TransferData) EchoKey() string   { return "" }
func (c TransferData) Unverified() bool  { return false }
func (c TransferData) Destructive() bool { return false }

func (c TransferData) build() *wire {
	w := newWire("FTDa", 4+len(c.Data))
	w.u16(0, "transfer", int64(c.Transfer))
	w.u16(2, "size", int64(len(c.Data)))
	copy(w.b[4:], c.Data)
	return w
}

// TransferFileData is FTFD (TransferFileDataCommand): the slot's name,
// description and MD5, sent at the end of an upload. Name is cut to 64
// bytes, Description to 128.
type TransferFileData struct {
	Transfer    uint16
	Hash        MD5
	Name        string
	Description string
}

func (c TransferFileData) Opcode() string    { return "FTFD" }
func (c TransferFileData) Encode() []byte    { return c.build().encode() }
func (c TransferFileData) Validate() error   { return c.build().err }
func (c TransferFileData) EchoKey() string   { return "" }
func (c TransferFileData) Unverified() bool  { return false }
func (c TransferFileData) Destructive() bool { return false }

func (c TransferFileData) build() *wire {
	w := newWire("FTFD", 212)
	w.u16(0, "transfer", int64(c.Transfer))
	w.str(2, 64, c.Name)
	w.str(66, 128, c.Description)
	copy(w.b[194:210], c.Hash[:])
	return w
}

// TransferAck is FTUA (TransferAckCommand): acknowledge one downloaded
// chunk. atemwire always sends slot 0.
type TransferAck struct {
	Transfer, Slot uint16
}

func (c TransferAck) Opcode() string    { return "FTUA" }
func (c TransferAck) Encode() []byte    { return c.build().encode() }
func (c TransferAck) Validate() error   { return c.build().err }
func (c TransferAck) EchoKey() string   { return "" }
func (c TransferAck) Unverified() bool  { return false }
func (c TransferAck) Destructive() bool { return false }

func (c TransferAck) build() *wire {
	w := newWire("FTUA", 4)
	w.u16(0, "transfer", int64(c.Transfer))
	w.u16(2, "slot", int64(c.Slot))
	return w
}
