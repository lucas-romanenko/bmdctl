// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The transfer engine (SURFACE.md §3.0), from bmdwire v1.3.0: the transfer
// lane of atemwire/protocol.py (AtemProtocol's queues, lock table and
// file-transfer handlers), _transfer.py (TransferTask), and the callers
// that wait on it: connection.py (download_still, clear_still,
// download_macro), macrotransfer/_io.py (upload_macro_bytecode) and
// WebATEM's atem_control/uploader.py (still upload). The lane is a state
// machine owned by the session's run goroutine; the exported methods queue
// a task there and wait for it. See docs/PORTING.md, "Transfers".

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"image"
	"slices"
	"time"
)

// Default timeouts for a transfer whose context has no deadline: those of
// the atemwire and WebATEM callers.
const (
	StillDownloadTimeout = 30 * time.Second // ATEMConnection.download_still
	StillUploadTimeout   = 90 * time.Second // WebATEM uploader.py UPLOAD_TIMEOUT
	StillClearTimeout    = 5 * time.Second  // ATEMConnection.clear_still
	MacroTimeout         = 10 * time.Second // download_macro, upload_macro_bytecode
)

const (
	// lockRerequest is LOCK_REREQUEST_SECONDS: a lock request with no
	// LKOB back is not repeated sooner than this.
	lockRerequest = time.Second
	// firstTransferID is atemwire's initial transfer_id; it is incremented
	// before each new transfer, so the first is 43.
	firstTransferID = 42
	// macroUploadMode is FTSD's mode for the macro store: write
	// uncompressed and pre-erase (0x0300). The still store takes
	// UploadWriteRLE.
	macroUploadMode = UploadWrite | UploadErase
)

var (
	// ErrLockNotGranted is wrapped in the error of a transfer that timed
	// out without the session ever being granted the store's lock: another
	// client (ATEM Software Control's media page, another application) is
	// holding it. atemwire's download_still says the same.
	ErrLockNotGranted = errors.New("atem: the store lock was never granted; another client is holding it")
)

// TransferError is a transfer the switcher refused: an FTDE whose status
// is not one of the three the engine recovers from (1 try again, 5 no
// lock, 6 lock taken by another session).
type TransferError struct {
	Store, Slot uint16
	Transfer    uint16 // the transfer id
	Status      uint8
}

func (e *TransferError) Error() string {
	return fmt.Sprintf("atem: the switcher rejected the transfer of store %d slot %d (transfer %d, status %s)",
		e.Store, e.Slot, e.Transfer, transferStatus(e.Status))
}

// transferStatus names an FTDE status as FileTransferErrorField.__repr__.
func transferStatus(s uint8) string {
	switch s {
	case 1:
		return "try-again"
	case 2:
		return "not-found"
	case 5:
		return "no-lock"
	case 6:
		return "lock-lost"
	}
	return fmt.Sprintf("unknown (%d)", s)
}

// LockHolder says who holds a store's lock, as far as the session can tell.
type LockHolder uint8

const (
	// LockFree: no lock reported taken, or the last report was a release.
	LockFree LockHolder = iota
	// LockThisSession: the switcher granted it to this session, which
	// hasn't seen its release confirmed yet.
	LockThisSession
	// LockOtherClient: the switcher reported it taken (LKST) and this
	// session doesn't hold it.
	LockOtherClient
)

func (h LockHolder) String() string {
	switch h {
	case LockFree:
		return "free"
	case LockThisSession:
		return "this session"
	case LockOtherClient:
		return "another client"
	}
	return fmt.Sprintf("LockHolder(%d)", uint8(h))
}

// taskKind is what a transferTask does.
type taskKind uint8

const (
	taskDownload taskKind = iota
	taskUpload
	taskClear // atemwire's TransferTask(clear=True): CSTL under the store lock
)

// transferTask is atemwire's TransferTask, plus the channel its caller
// waits on.
type transferTask struct {
	kind  taskKind
	store uint16
	slot  uint16
	tid   uint16

	// Uploads.
	data        []byte // what is still to be queued, compressed if it is
	dataLength  int    // the uncompressed length, FTSD's length
	hash        MD5    // MD5 of the uncompressed data, sent in FTFD
	name        string
	description string

	done     chan transferResult // buffered 1; written once
	finished bool

	// size is the upload's length as sent (compressed, if it is), for
	// its progress; progress, if not nil, takes the latest Progress
	// (buffered 1): the owner never waits on it.
	size     int
	progress chan Progress
}

// Progress is how far a transfer has come, in bytes, as bmdwire v1.3.0's
// protocol.py events count them: an upload's upload-progress, the bytes
// queued of all it sends (compressed, for a still); a still download's
// transfer-progress, the bytes received of the frame's uncompressed size
// (so it may end short of Total). A macro download reports none, as
// atemwire's doesn't.
type Progress struct {
	Done, Total int
}

type progressKey struct{}

// WithProgress returns a context under which a transfer reports its
// progress to f, from the goroutine that called the transfer (not the
// session's: atemwire raises its events on its worker thread, which here
// runs no callbacks), as often as it comes, latest first: a report not
// yet taken is replaced by the next.
func WithProgress(ctx context.Context, f func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, f)
}

// report hands the task's caller its progress, never waiting: a report
// not taken yet is replaced.
func (t *transferTask) report(done, total int) {
	if t.progress == nil || total <= 0 {
		return
	}
	p := Progress{Done: min(done, total), Total: total}
	select {
	case <-t.progress:
	default:
	}
	select {
	case t.progress <- p:
	default:
	}
}

type transferResult struct {
	data []byte
	err  error
}

// lane is the transfer state atemwire keeps on AtemProtocol. Only the
// session's run goroutine touches it.
type lane struct {
	// order holds the stores in the order their queues were created: the
	// order Python iterates transfer_queue in.
	order  []uint16
	queues map[uint16][]*transferTask // transfer_queue

	transfer    *transferTask // the transfer in flight
	requested   bool          // transfer_requested: FTSU/FTSD sent, not finished
	nextID      uint16        // transfer_id
	buffer      [][]byte      // transfer_buffer: download chunks
	bufferBytes int

	locks          map[uint16]bool      // locks: granted (true) or released by us (false)
	releasePending map[uint16]bool      // _lock_release_pending
	lockRequested  map[uint16]time.Time // _lock_requested

	budget *FileTransferContinueDataField // transfer_budget: nil when exhausted

	// The paced bulk queue's triggers (see bulkTrigger).
	bulkPending int
	bulkNext    time.Time
	bulkTimer   *time.Timer
}

func newLane() lane {
	return lane{
		queues:         make(map[uint16][]*transferTask),
		nextID:         firstTransferID,
		locks:          make(map[uint16]bool),
		releasePending: make(map[uint16]bool),
		lockRequested:  make(map[uint16]time.Time),
	}
}

// active is ATEMConnection._transfer_active: a transfer is queued or in
// flight. The outbox skips its 10 ms spacing meanwhile, as atemwire's
// worker does.
func (l *lane) active() bool {
	if l.transfer != nil || l.requested {
		return true
	}
	for _, q := range l.queues {
		if len(q) > 0 {
			return true
		}
	}
	return false
}

// remove takes t out of its store's queue, reporting whether it was there.
func (l *lane) remove(t *transferTask) bool {
	q := l.queues[t.store]
	i := slices.Index(q, t)
	if i < 0 {
		return false
	}
	l.queues[t.store] = slices.Delete(slices.Clone(q), i, i+1)
	return true
}

// popHead drops the head of a store's queue if it is t (the guard
// atemwire's clear and fatal-error paths use).
func (l *lane) popHead(t *transferTask) {
	if q := l.queues[t.store]; len(q) > 0 && q[0] == t {
		l.queues[t.store] = q[1:]
	}
}

// --- the public entry points ------------------------------------------------

// DownloadStill downloads still slot slot (0-based) and returns the frame:
// the switcher's 10-bit YCbCrA 4:2:2, RLE-decoded, width×height×4 bytes
// for the current video mode (YCbCrAToRGBA converts it). It is atemwire's
// ATEMConnection.download_still: PLCK, LKOB, FTSU, FTDa chunks each
// acknowledged with FTUA, FTDC, then the lock released. Without a context
// deadline it gives up after StillDownloadTimeout. A transfer the switcher
// refuses fails with a *TransferError; one that times out without the lock
// ever being granted wraps ErrLockNotGranted.
func (s *Session) DownloadStill(ctx context.Context, slot int) ([]byte, error) {
	if err := checkSlot("still", slot, 0xFFFF); err != nil {
		return nil, err
	}
	t := &transferTask{kind: taskDownload, store: StoreStills, slot: uint16(slot)}
	return s.runTransfer(ctx, t, StillDownloadTimeout)
}

// StillOptions are UploadStill's options.
type StillOptions struct {
	// Name and Description are the slot's metadata (FTFD), cut to 64 and
	// 128 bytes. WebATEM names a slot after the file, without extension.
	Name, Description string
	// Premultiply multiplies colour by alpha before the conversion, as
	// WebATEM does for PNG files (straight alpha) and nothing else.
	Premultiply bool
}

// UploadStill fits img into the current video mode's frame and uploads it
// to still slot slot (0-based). The fitting is WebATEM's _prepare_frame
// (FitFrame: a LANCZOS fit inside the frame, centred on transparent black);
// the frame is converted to the switcher's YCbCrA (RGBAToYCbCrA) and sent
// as UploadStillFrame sends it. Without a context deadline it gives up
// after StillUploadTimeout. Decoding an image file is the caller's job.
func (s *Session) UploadStill(ctx context.Context, slot int, img image.Image, opts StillOptions) error {
	if err := checkSlot("still", slot, 0xFFFF); err != nil {
		return err
	}
	mode, err := s.videoMode()
	if err != nil {
		return err
	}
	w, h := mode.FrameSize()
	frame, err := RGBAToYCbCrA(FitFrame(img, w, h), opts.Premultiply)
	if err != nil {
		return err
	}
	return s.UploadStillFrame(ctx, slot, frame, opts.Name, opts.Description)
}

// UploadStillFrame uploads a frame already in the switcher's format
// (YCbCrA 4:2:2, width×height×4 bytes for the current video mode, as
// DownloadStill returns it) to still slot slot. It is atemwire's
// AtemProtocol.upload for store 0 with compress=True: the MD5 is taken over
// the frame, the frame is RLE-encoded, then PLCK, LKOB, FTSD (mode 1, write
// RLE), FTCD budgets, FTDa chunks, FTFD, FTDC, and the lock released.
// Without a context deadline it gives up after StillUploadTimeout.
func (s *Session) UploadStillFrame(ctx context.Context, slot int, frame []byte, name, description string) error {
	if err := checkSlot("still", slot, 0xFFFF); err != nil {
		return err
	}
	rle, err := RLEEncode(frame)
	if err != nil {
		return err
	}
	t := &transferTask{
		kind: taskUpload, store: StoreStills, slot: uint16(slot),
		data: rle, dataLength: len(frame), hash: md5.Sum(frame),
		name: name, description: description,
	}
	_, err = s.runTransfer(ctx, t, StillUploadTimeout)
	return err
}

// ClearStill empties still slot slot (0-based) under the store lock: LOCK,
// LKOB, CSTL, then the lock released. It is atemwire's clear_still, the
// reliable way to clear a slot (some models ignore a CSTL from a session
// that doesn't hold the lock). It returns once the CSTL has been sent, not
// once the switcher confirms it: watch State.MediaPool.Files[slot].IsUsed
// for that. On timeout (StillClearTimeout without a deadline) a clear that
// hasn't been sent is withdrawn, so it can't empty the slot later.
func (s *Session) ClearStill(ctx context.Context, slot int) error {
	if err := checkSlot("still", slot, 0xFF); err != nil { // CSTL's slot is a u8
		return err
	}
	t := &transferTask{kind: taskClear, store: StoreStills, slot: uint16(slot)}
	_, err := s.runTransfer(ctx, t, StillClearTimeout)
	return err
}

// DownloadMacro downloads macro slot slot (0-based) and returns its
// bytecode, empty for an empty slot. The macro store takes no lock: FTSU,
// FTDa chunks each acknowledged with FTUA, FTDC. It is atemwire's
// ATEMConnection.download_macro; without a context deadline it gives up
// after MacroTimeout.
func (s *Session) DownloadMacro(ctx context.Context, slot int) ([]byte, error) {
	if err := checkSlot("macro", slot, 0xFFFF); err != nil {
		return nil, err
	}
	t := &transferTask{kind: taskDownload, store: StoreMacros, slot: uint16(slot)}
	return s.runTransfer(ctx, t, MacroTimeout)
}

// UploadMacro uploads bytecode to macro slot slot (0-based) with a name and
// description (cut to 64 and 128 bytes): FTSD (mode 0x0300), FTCD budgets,
// FTDa chunks, FTFD with the bytecode's MD5, FTDC. Empty bytecode empties
// the slot (FTSD and FTFD together, no budget). It is atemwire's
// upload_macro_bytecode; without a context deadline it gives up after
// MacroTimeout.
func (s *Session) UploadMacro(ctx context.Context, slot int, name, description string, bytecode []byte) error {
	if err := checkSlot("macro", slot, 0xFFFF); err != nil {
		return err
	}
	t := &transferTask{
		kind: taskUpload, store: StoreMacros, slot: uint16(slot),
		data: bytecode, dataLength: len(bytecode), hash: md5.Sum(bytecode),
		name: name, description: description,
	}
	_, err := s.runTransfer(ctx, t, MacroTimeout)
	return err
}

// LockHolder reports who holds store's lock as far as the session can
// tell: the switcher's LKST broadcasts don't name the holder, so a lock
// reported taken that this session wasn't granted is another client's.
func (s *Session) LockHolder(store uint16) LockHolder {
	ch := make(chan LockHolder, 1)
	if s.do(func() { ch <- s.lockHolder(store) }) != nil {
		return LockFree
	}
	return <-ch
}

func (s *Session) lockHolder(store uint16) LockHolder {
	if s.lane.locks[store] || s.lane.releasePending[store] {
		return LockThisSession
	}
	if l := s.state.Locks[store]; l != nil && l.State {
		return LockOtherClient
	}
	return LockFree
}

func checkSlot(what string, slot, max int) error {
	if slot < 0 || slot > max {
		return fmt.Errorf("atem: %s slot %d out of range 0-%d", what, slot, max)
	}
	return nil
}

// videoMode returns the switcher's video mode, which sets a still's size.
func (s *Session) videoMode() (*VideoModeField, error) {
	ch := make(chan *VideoModeField, 1)
	if err := s.do(func() {
		var m *VideoModeField
		if s.state.VideoMode.Mode != nil {
			c := *s.state.VideoMode.Mode
			m = &c
		}
		ch <- m
	}); err != nil {
		return nil, err
	}
	m := <-ch
	if m == nil {
		return nil, fmt.Errorf("%w: the switcher hasn't reported its video mode", ErrNotReady)
	}
	return m, nil
}

// runTransfer queues t on the lane and waits for it. If ctx has no
// deadline, timeout applies. On cancellation t is withdrawn: dequeued if it
// hasn't started, abandoned if it is in flight (see cancelTask).
func (s *Session) runTransfer(ctx context.Context, t *transferTask, timeout time.Duration) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	t.done = make(chan transferResult, 1)
	progress, _ := ctx.Value(progressKey{}).(func(Progress))
	if progress != nil {
		t.progress = make(chan Progress, 1)
	}
	t.size = len(t.data)
	errc := make(chan error, 1)
	if err := s.do(func() {
		if !s.initialized {
			errc <- ErrNotReady
			return
		}
		s.enqueueTask(t)
		errc <- nil
	}); err != nil {
		return nil, err
	}
	if err := <-errc; err != nil {
		return nil, err
	}
wait:
	for {
		select {
		case r := <-t.done:
			return r.data, r.err
		case p := <-t.progress: // nil, and never ready, without WithProgress
			progress(p)
		case <-ctx.Done():
			break wait
		}
	}
	ch := make(chan error, 1)
	if err := s.do(func() { ch <- s.cancelTask(t, ctx.Err()) }); err != nil {
		// The session ended, and with it the task.
		select {
		case r := <-t.done:
			return r.data, r.err
		default:
			return nil, err
		}
	}
	if err := <-ch; err != nil {
		return nil, err
	}
	// The task finished before the cancellation reached the lane.
	r := <-t.done
	return r.data, r.err
}

// describe names a task in errors.
func (t *transferTask) describe() string {
	switch {
	case t.kind == taskClear:
		return fmt.Sprintf("clear of still slot %d", t.slot)
	case t.store == StoreMacros && t.kind == taskUpload:
		return fmt.Sprintf("upload of macro slot %d", t.slot)
	case t.store == StoreMacros:
		return fmt.Sprintf("download of macro slot %d", t.slot)
	case t.kind == taskUpload:
		return fmt.Sprintf("upload of still slot %d", t.slot)
	}
	return fmt.Sprintf("download of still slot %d", t.slot)
}

// cancelTask withdraws t after its caller gave up, and returns the error
// the caller reports; nil if t had already finished. A queued task is
// dequeued (atemwire's dequeue_clear, for every kind of task) and the lane
// carries on; the transfer in flight is abandoned the way a fatal FTDE
// abandons it, its lock released per frame. atemwire's callers instead
// call abort_transfers, which empties the whole lane: that is safe there
// only because its transfers are serialised per connection.
func (s *Session) cancelTask(t *transferTask, cause error) error {
	if t.finished {
		return nil
	}
	l := &s.lane
	// Read before the release below: a timeout without the lock ever being
	// granted means another client holds it (download_still's hint).
	notGranted := t.store != StoreMacros && !l.locks[t.store]
	t.finished = true
	if l.transfer == t {
		l.popHead(t)
		l.transfer = nil
		l.requested = false
		l.buffer, l.bufferBytes = nil, 0
		s.releaseThenContinue(t.store)
	} else {
		l.remove(t)
		s.transferTrigger(t.store, false)
	}
	err := fmt.Errorf("atem: %s: %w", t.describe(), cause)
	if notGranted {
		err = fmt.Errorf("%w (%w)", err, ErrLockNotGranted)
	}
	return err
}

// finish hands t's caller its result, once.
func (s *Session) finish(t *transferTask, r transferResult) {
	if t.finished {
		return
	}
	t.finished = true
	t.done <- r
}

// --- the lane -----------------------------------------------------------------

// enqueueTask is download, upload and queue_clear: queue t and trigger its
// store.
func (s *Session) enqueueTask(t *transferTask) {
	l := &s.lane
	if _, ok := l.queues[t.store]; !ok {
		l.order = append(l.order, t.store)
	}
	l.queues[t.store] = append(l.queues[t.store], t)
	s.log.Info("atem: transfer queued", "task", t.describe())
	s.transferTrigger(t.store, false)
}

// sendCommands is AtemProtocol.send_commands: the commands in one reliable
// packet, straight to the transport, not through the outbox.
func (s *Session) sendCommands(cmds ...Command) {
	var b []byte
	for _, c := range cmds {
		b = append(b, c.Encode()...)
	}
	if err := s.t.sendReliable(b); err != nil {
		s.fatal = err
	}
}

// transferTrigger is _transfer_trigger: start the next transfer if it can,
// asking for its store's lock first; with nothing queued, release every
// lock held.
func (s *Session) transferTrigger(store uint16, retry bool) {
	l := &s.lane
	var next *transferTask
	if q := l.queues[store]; len(q) > 0 {
		next = q[0]
	}
	if next == nil {
		for _, st := range l.order {
			if q := l.queues[st]; len(q) > 0 {
				next = q[0]
				break
			}
		}
	}
	if next == nil {
		// Everything is done: give back the locks we hold. As in atemwire,
		// they stay marked held until the LKST echo.
		for _, st := range sortedStores(l.locks) {
			if l.locks[st] {
				s.log.Info("atem: releasing store lock", "store", st)
				s.sendCommands(Lock{Store: st})
			}
		}
		return
	}
	if next.store != StoreMacros && !l.locks[next.store] {
		if l.releasePending[next.store] {
			// A request sent while our own unlock is in flight is eaten by
			// the switcher; the LKST echo triggers again.
			s.log.Debug("atem: lock request deferred until the release echo", "store", next.store)
			return
		}
		if at, ok := l.lockRequested[next.store]; ok && s.t.now().Sub(at) < lockRerequest {
			s.log.Debug("atem: lock request already on the wire", "store", next.store)
			return
		}
		// A clear takes the whole store's lock: CSTL was verified honoured
		// under LOCK, not under PLCK.
		var req Command = PartialLock{Store: next.store, Slot: next.slot}
		if next.kind == taskClear {
			req = Lock{Store: next.store, State: true}
		}
		s.log.Info("atem: requesting store lock", "store", next.store)
		l.lockRequested[next.store] = s.t.now()
		s.sendCommands(req)
		return
	}
	if l.requested {
		return
	}
	if next.kind == taskClear {
		// No FTSU/FTDC arc: CSTL while holding the lock, then the
		// per-frame release, which in-order delivery puts after it.
		l.popHead(next)
		s.log.Info("atem: clearing still under the store lock", "slot", next.slot)
		s.sendCommands(ClearStill{Slot: uint8(next.slot)})
		s.finish(next, transferResult{}) // clear-dispatched
		s.releaseThenContinue(next.store)
		return
	}
	if !retry {
		l.nextID++
	}
	l.transfer = next
	next.tid = l.nextID
	var out []Command
	if next.kind == taskUpload {
		mode := UploadWriteRLE
		if next.store == StoreMacros {
			mode = macroUploadMode
		}
		out = append(out, TransferUploadRequest{
			Transfer: next.tid, Store: next.store, Slot: next.slot,
			Length: uint32(next.dataLength), Mode: mode,
		})
		s.log.Info("atem: requesting upload", "store", next.store, "slot", next.slot, "transfer", next.tid)
	} else {
		// A new or retried download starts its chunks afresh.
		l.buffer, l.bufferBytes = nil, 0
		out = append(out, TransferDownloadRequest{Transfer: next.tid, Store: next.store, Slot: uint32(next.slot)})
		s.log.Info("atem: requesting download", "store", next.store, "slot", next.slot, "transfer", next.tid)
	}
	l.requested = true
	if next.kind == taskUpload && next.dataLength == 0 {
		// An empty upload gets no FTCD, so FTFD goes with the request,
		// as ASC's "Restore from State" does for an empty macro.
		out = append(out, s.fileData(next))
	}
	s.sendCommands(out...)
}

func (s *Session) fileData(t *transferTask) TransferFileData {
	return TransferFileData{Transfer: t.tid, Hash: t.hash, Name: t.name, Description: t.description}
}

// releaseThenContinue is _release_then_continue: a transfer is over, so
// give its store's lock back now (the LKST echo starts the next one), or,
// for the lock-exempt macro store, start the next at once.
func (s *Session) releaseThenContinue(store uint16) {
	l := &s.lane
	l.transfer = nil
	if store != StoreMacros && l.locks[store] {
		s.log.Info("atem: releasing store lock (per frame)", "store", store)
		s.sendCommands(Lock{Store: store})
		l.releasePending[store] = true
		l.locks[store] = false
		return
	}
	s.transferTrigger(store, false)
}

// onTransferPacket handles the packets the engine consumes: LKOB, FTCD,
// FTDa, FTDE and FTDC, the branches of save_field_data that return before
// anything is stored. A parse error loses the rest of the packet, as in
// atemwire.
func (s *Session) onTransferPacket(code string, payload []byte) error {
	v, err := parsers[code].parse(payload)
	if err != nil {
		return err
	}
	switch f := v.(type) {
	case *LockObtainedField:
		s.log.Info("atem: got store lock", "store", f.Store)
		s.lane.locks[f.Store] = true
		delete(s.lane.lockRequested, f.Store)
		s.transferTrigger(f.Store, false)
	case *FileTransferContinueDataField:
		s.onBudget(*f)
	case *FileTransferDataField:
		s.onChunk(f)
	case *FileTransferErrorField:
		s.onTransferError(f)
	case *FileTransferDataCompleteField:
		s.onTransferComplete(f)
	}
	return nil
}

// onLockReleased is the release half of the lock-state branch: forget our
// lock, and if work is queued for the store, ask for it again at once
// ("the pounce").
func (s *Session) onLockReleased(store uint16) {
	l := &s.lane
	delete(l.locks, store)
	delete(l.releasePending, store)
	delete(l.lockRequested, store)
	if len(l.queues[store]) > 0 && !l.requested && !l.locks[store] {
		s.transferTrigger(store, false)
	}
}

// onBudget is the file-transfer-continue-data branch: the switcher grants
// count chunks of size bytes (rounded down to whole 8-byte words).
func (s *Session) onBudget(f FileTransferContinueDataField) {
	if size := f.Size / 8 * 8; size != f.Size {
		s.log.Debug("atem: adjusted transfer chunk size", "from", f.Size, "to", size)
		f.Size = size
	}
	s.lane.budget = &f
	s.queueChunks()
}

// onChunk is the file-transfer-data branch: one downloaded chunk, kept and
// acknowledged.
func (s *Session) onChunk(f *FileTransferDataField) {
	l := &s.lane
	if l.transfer == nil {
		// A straggler after the transfer ended or was abandoned.
		s.log.Debug("atem: FTDa with no transfer in flight")
		return
	}
	if f.Transfer != l.transfer.tid {
		s.log.Error("atem: file transfer data for the wrong transfer id", "got", f.Transfer, "want", l.transfer.tid)
		return
	}
	l.buffer = append(l.buffer, f.Data)
	l.bufferBytes += len(f.Data)
	// transfer-progress: a still's, of its frame's uncompressed size.
	if m := s.state.VideoMode.Mode; m != nil && l.transfer.store != StoreMacros {
		l.transfer.report(l.bufferBytes, m.Pixels()*4)
	}
	// atemwire acknowledges with slot 0: "it seems it's always 0 in practice".
	s.sendCommands(TransferAck{Transfer: l.transfer.tid})
}

// onTransferError is the file-transfer-error branch.
func (s *Session) onTransferError(f *FileTransferErrorField) {
	l := &s.lane
	if f.Status == 1 || f.Status == 5 || f.Status == 6 {
		s.log.Debug("atem: file transfer error", "transfer", f.Transfer, "status", transferStatus(f.Status))
	} else {
		s.log.Error("atem: file transfer error", "transfer", f.Transfer, "status", transferStatus(f.Status))
	}
	if l.transfer == nil {
		s.log.Debug("atem: FTDE with no transfer in flight")
		return
	}
	if f.Transfer != l.transfer.tid {
		// A stale or foreign transfer id must not fail ours.
		return
	}
	t := l.transfer
	l.requested = false
	switch f.Status {
	case 1: // try again, same transfer id
		s.transferTrigger(t.store, true)
	case 5: // no lock: ask for it again
		l.locks[t.store] = false
		s.transferTrigger(t.store, true)
	case 6:
		// Another session asked for the store while ours was being set up:
		// the switcher aborted our request but we still hold the lock. Give
		// it back, keep the task, and ask again on the next release.
		delete(l.lockRequested, t.store)
		s.releaseThenContinue(t.store)
	default:
		// Fatal for this transfer: drop it, release the lock, carry on.
		l.popHead(t)
		l.transfer = nil
		l.buffer, l.bufferBytes = nil, 0
		s.releaseThenContinue(t.store)
		s.finish(t, transferResult{err: &TransferError{Store: t.store, Slot: t.slot, Transfer: f.Transfer, Status: f.Status}})
	}
}

// onTransferComplete is the file-transfer-data-complete branch: the lock is
// released before the (slow) RLE decode and the delivery.
func (s *Session) onTransferComplete(f *FileTransferDataCompleteField) {
	l := &s.lane
	if l.transfer == nil {
		// The switcher announces every session's completions.
		s.log.Debug("atem: FTDC for another session's transfer")
		return
	}
	if f.Transfer != l.transfer.tid {
		return
	}
	t := l.transfer
	l.popHead(t)
	raw := slices.Concat(l.buffer...)
	l.buffer, l.bufferBytes = nil, 0
	l.requested = false
	s.releaseThenContinue(t.store)
	if t.kind == taskUpload {
		s.finish(t, transferResult{})
		return
	}
	if raw == nil {
		raw = []byte{}
	}
	if t.store != StoreStills {
		s.finish(t, transferResult{data: raw})
		return
	}
	frame, err := RLEDecode(raw)
	if err != nil {
		// atemwire logs the exception and its caller times out.
		err = fmt.Errorf("atem: %s: %w", t.describe(), err)
	}
	s.finish(t, transferResult{data: frame, err: err})
}

// queueChunks is _queue_chunks: cut the upload into the budget's chunks,
// never splitting an RLE run header from its count, and queue them on the
// transport's bulk queue.
func (s *Session) queueChunks() {
	l := &s.lane
	if l.budget == nil {
		s.log.Debug("atem: chunk budget exhausted, waiting for the next FTCD")
		return
	}
	if l.transfer == nil {
		s.log.Error("atem: FTCD with no transfer scheduled")
		return
	}
	t := l.transfer
	if t.kind != taskUpload {
		s.log.Error("atem: FTCD for a download")
		return
	}
	size := int(l.budget.Size)
	for range int(l.budget.Count) {
		if len(t.data) == 0 {
			break
		}
		chunk := t.data[:min(size, len(t.data))]
		used := size
		switch {
		case len(chunk) >= 8 && isRLEHeader(chunk[len(chunk)-8:]):
			chunk, used = t.data[:min(size-8, len(t.data))], size-8
		case len(chunk) >= 16 && isRLEHeader(chunk[len(chunk)-16:len(chunk)-8]):
			chunk, used = t.data[:min(size-16, len(t.data))], size-16
		}
		t.data = t.data[min(used, len(t.data)):]
		l.budget.Count--
		if l.budget.Count == 0 {
			l.budget = nil
		}
		s.t.queuePacket(TransferData{Transfer: t.tid, Data: chunk}.Encode())
		t.report(t.size-len(t.data), t.size) // upload-progress
		if l.budget == nil {
			break
		}
	}
	// atemwire calls queue_trigger here and drops what it returns.
	s.bulkTrigger(false)
}

func isRLEHeader(b []byte) bool {
	for _, c := range b {
		if c != 0xFE {
			return false
		}
	}
	return true
}

// queueFlushed is _queue_flushed: the bulk queue has drained. Queue more of
// the upload if the budget allows, or, when all of it is out, send FTFD.
func (s *Session) queueFlushed() {
	t := s.lane.transfer
	if t == nil {
		s.log.Debug("atem: bulk queue flushed with no transfer in flight")
		return
	}
	if t.kind != taskUpload {
		return
	}
	if len(t.data) > 0 {
		s.queueChunks()
		return
	}
	s.log.Info("atem: sending file metadata", "transfer", t.tid)
	s.sendCommands(s.fileData(t))
}

// bulkTrigger is queue_trigger, called where atemwire calls it: for every
// packet from the switcher once ACKs flow (twice for a control frame), and
// after chunks are queued. It sends up to five queued packets, at most one
// batch every 3 ms: atemwire sleeps 3 ms after each batch, and the session
// instead defers a trigger that comes sooner. When the queue has drained it
// reports the flush (TransferQueueFlushed) to queueFlushed, if flush.
func (s *Session) bulkTrigger(flush bool) {
	l := &s.lane
	if len(s.t.bulk) > 0 {
		if wait := l.bulkNext.Sub(s.t.now()); wait > 0 {
			l.bulkPending++
			if l.bulkTimer == nil {
				l.bulkTimer = time.NewTimer(wait)
			} else {
				l.bulkTimer.Reset(wait)
			}
			return
		}
	}
	sent, flushed, err := s.t.queueTrigger()
	if err != nil {
		s.fatal = err
		return
	}
	if sent > 0 {
		l.bulkNext = s.t.now().Add(bulkDelay)
	}
	if flushed && flush {
		s.queueFlushed()
	}
}

// onBulkTimer runs the triggers bulkTrigger deferred, one per batch slot.
func (s *Session) onBulkTimer() {
	l := &s.lane
	if l.bulkPending == 0 {
		return
	}
	l.bulkPending--
	s.bulkTrigger(true)
	if l.bulkPending > 0 && len(s.t.bulk) == 0 {
		// Nothing left to pace: the rest would only report the flush,
		// which one trigger already did.
		l.bulkPending = 0
	}
	if l.bulkPending > 0 {
		l.bulkTimer.Reset(max(0, l.bulkNext.Sub(s.t.now())))
	}
}

// bulkTimerC is the deferred-trigger timer's channel, nil when unarmed.
func (s *Session) bulkTimerC() <-chan time.Time {
	if s.lane.bulkTimer == nil || s.lane.bulkPending == 0 {
		return nil
	}
	return s.lane.bulkTimer.C
}

// resetLane is _reset_transfer_lane, for a session that died: the old
// session's locks died with it on the switcher, so nothing is sent. Every
// waiting caller fails with err. The transfer id carries on, as in
// atemwire.
func (s *Session) resetLane(err error) {
	l := &s.lane
	for _, st := range l.order {
		for _, t := range l.queues[st] {
			s.finish(t, transferResult{err: err})
		}
	}
	if l.transfer != nil {
		s.finish(l.transfer, transferResult{err: err})
	}
	id := l.nextID
	timer := l.bulkTimer
	*l = newLane()
	l.nextID = id
	if timer != nil {
		timer.Stop()
		l.bulkTimer = timer
	}
}

// releaseLocksNow is release_locks_now, for Close: give back every store
// lock held, straight to the socket, before the goodbye. Best effort.
func (s *Session) releaseLocksNow() {
	l := &s.lane
	for _, st := range sortedStores(l.locks) {
		if !l.locks[st] || st == StoreMacros {
			continue
		}
		s.log.Info("atem: releasing store lock (teardown)", "store", st)
		if err := s.t.sendReliable(Lock{Store: st}.Encode()); err != nil {
			s.log.Debug("atem: teardown lock release failed", "store", st, "err", err)
		}
		l.locks[st] = false
	}
	clear(l.releasePending)
	clear(l.lockRequested)
}

func sortedStores(m map[uint16]bool) []uint16 {
	var out []uint16
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
