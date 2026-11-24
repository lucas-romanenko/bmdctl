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
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"
)

// Default timeouts for a transfer whose context has no deadline: those of
// the atemwire and WebATEM callers. A macro transfer's is time without
// progress, with no cap on the whole (bmdwire#101, 85d2f58); the others
// cap the whole transfer.
const (
	StillDownloadTimeout = 30 * time.Second // ATEMConnection.download_still
	StillUploadTimeout   = 90 * time.Second // WebATEM uploader.py UPLOAD_TIMEOUT
	StillClearTimeout    = 5 * time.Second  // ATEMConnection.clear_still
	MacroTimeout         = 10 * time.Second // download_macro, upload_macro_bytecode
)

// macroTimeout is MacroTimeout, which tests shorten.
var macroTimeout = MacroTimeout

const (
	// lockRerequest is LOCK_REREQUEST_SECONDS: a lock request with no
	// LKOB back is not repeated sooner than this.
	lockRerequest = time.Second
	// FTDE 1 (try again) is asked again after a pause that doubles from
	// tryAgainFirst to tryAgainMax, and after tryAgainAttempts refusals in
	// a row the transfer fails like a fatal status, its lock given back
	// (TRY_AGAIN_*, bmdwire#73, 588a389, v1.11.0). It used to be asked
	// again at once, with no end: ~900 requests in 13 s on a 4K Plus whose
	// transfer engine was held by a download nobody drained (2026-11-04).
	tryAgainFirst    = 50 * time.Millisecond
	tryAgainMax      = time.Second
	tryAgainAttempts = 20
	// A download its caller stops waiting for is not abandoned mid-stream:
	// the switcher's transfer engine stays busy with a transfer neither
	// acknowledged nor finished, and answers every later request, from
	// every session, with FTDE 1 (a 4K Plus did it for good after a lock
	// release 15 s into a still, 2026-11-04). The abandoned transfer is
	// acknowledged to its end and its data dropped, and the store lock goes
	// back at its FTDC (or FTDE), or once nothing of it has come for
	// drainStall, or after drainCap in all (DRAIN_STALL, protocol.py's DRAIN_MAX,
	// bmdwire#73, 588a389, v1.11.0). Since bmdwire#80 (v1.13.0) that holds
	// for a request with nothing come of it yet, too: the switcher takes
	// up to 2.5 s to a still's first chunk on a LAN (2026-11-06).
	drainStall = 5 * time.Second
	drainCap   = 120 * time.Second
	// orphanIDs is ORPHAN_IDS: the ids of the session's last 64 transfers
	// are kept, and a chunk of one it no longer follows (its caller gave
	// up, a drain ended before the switcher started it) is acknowledged
	// all the same and dropped: a chunk left unacknowledged holds the
	// switcher's transfer engine, and every later request is refused with
	// FTDE 1 while it lasts (bmdwire#80, v1.13.0). Another session's
	// chunks are left alone.
	orphanIDs = 64
	// An upload is done only when the switcher says so (FTDC). Without
	// that, one that has made no progress for uploadStall in any phase
	// (its lock not granted, its transfer not started, no packet of ours
	// newly acknowledged once its data goes, no chunk cut, no budget
	// granted) goes again from the beginning in a new session, under a
	// new transfer id, uploadRetries times at most, then fails with an
	// *UploadStalledError naming the phase. While the switcher says
	// another client holds the store's lock the limit is lockWaitMax
	// instead (UPLOAD_STALL, UPLOAD_RETRIES, LOCK_WAIT_MAX; bmdwire#100,
	// d19c1cd, 174840f, 5587e15, e151696). The hardware bench, 2026-11-24
	// (Constellation HD, fw 2.30): after the cable was out for 3 s
	// mid-upload everything was acknowledged on both sides and the
	// switcher never granted the next budget, and the still sat at 47 %
	// until the caller gave up; started again in the same session, the
	// lock was asked for, acknowledged and never granted. Only a goodbye
	// frees what an abandoned transfer holds. uploadStall is above the
	// ~4 s a chunk too big for the path takes to be found (tooBigAfter),
	// during which nothing moves either.
	uploadStall   = 6 * time.Second
	uploadRetries = 2
	lockWaitMax   = 60 * time.Second
	// uploadLook is how often the lane looks at an upload in hand
	// (atemwire's _wake_at(now + 1.0)).
	uploadLook = time.Second
	// queuePortion is QUEUE_PORTION: queueChunks hands chunks to the
	// transport this many at a time; its bulk queue holds bulkQueueMax.
	// Chunks taken back from three budgets in flight (2,304) overflowed
	// it, dropping the oldest, and the still came out short.
	queuePortion = 512
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

// firstTransferID is _first_transfer_id: a session's transfer ids start
// at random in 1..0xFFFF (bmdwire#64, #68, 6876ba2, v1.9.0). The switcher
// broadcasts FTDC to every session with the owner's transfer id, and a
// session matches it against its own; every session used to start at 43
// (42, incremented before use), so two sessions on one switcher, this
// port's or bmdwire's, used the same ids. One took the other's FTDC for
// its own and stopped acknowledging the transfer the switcher was still
// sending, and the switcher refused every later request from both with
// FTDE 1, for good. It is a variable so that tests can pin it.
var firstTransferID = func() uint16 { return uint16(rand.IntN(0xFFFF) + 1) }

// nextTransferID is the id after id: a u16 on the wire, wrapping without
// 0 (6876ba2).
func nextTransferID(id uint16) uint16 { return id%0xFFFF + 1 }

// TransferError is a transfer the switcher refused: an FTDE whose status
// is not one of the three the engine recovers from (1 try again, 5 no
// lock, 6 lock taken by another session).
type TransferError struct {
	Store, Slot uint16
	Transfer    uint16 // the transfer id
	Status      uint8
}

// ErrUploadStalled is matched (errors.Is) by an *UploadStalledError.
var ErrUploadStalled = errors.New("atem: upload stalled")

// UploadStalledError is an upload given up on: no progress for After in
// the phase it was in, after Retries fresh starts, each in a new session,
// and the switcher never said it was done (bmdwire#100: atemwire's
// file-transfer-error with status 'stalled').
type UploadStalledError struct {
	Store, Slot uint16
	// Phase is where it stalled: "waiting for a new session", "waiting
	// for its lock", "waiting for the switcher to start it", or how much
	// of its data had been cut into chunks ("4000 of 10000 bytes cut").
	Phase   string
	After   time.Duration
	Retries int
	what    string
}

func (e *UploadStalledError) Error() string {
	return fmt.Sprintf("atem: %s stalled: no progress for %v (%s) after %d fresh starts, and the switcher never said it was done",
		e.what, e.After, e.Phase, e.Retries)
}

func (e *UploadStalledError) Is(target error) bool { return target == ErrUploadStalled }

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
	data        []byte // what is sent, compressed if it is
	dataLength  int    // the uncompressed length, FTSD's length
	hash        MD5    // MD5 of the uncompressed data, sent in FTFD
	name        string
	description string
	// offset is how much of data this attempt has cut into chunks, and
	// metadataSent whether its FTFD has gone, with the first budget; both
	// start again with each upload request (bmdwire#36: chunks used to be
	// cut off the front of data, so a retried upload lost its start).
	offset       int
	metadataSent bool

	done     chan transferResult // buffered 1; written once
	finished bool
	// queued: the task reached the lane (set before runTransfer waits;
	// read by the caller's goroutine only).
	queued bool

	// tryAgain counts the switcher's FTDE 1 refusals of this transfer
	// (TransferTask.try_again).
	tryAgain int
	// stallRetries counts an upload's fresh starts after uploadStall with
	// no progress (stall_retries), across sessions.
	stallRetries int
	// heard is when this transfer last made progress (UnixNano): a chunk
	// of a download arrived, or a budget, a chunk queued or an ACK of
	// ours moved an upload on (bmdwire#101). The stall clock counts from
	// it; the owner writes it, the caller reads it.
	heard atomic.Int64

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
//
// An upload that stalls goes again from the beginning (bmdwire#100):
// Retry is then its fresh start of Retries, on every report from then
// on, and Done starts again from 0.
type Progress struct {
	Done, Total    int
	Retry, Retries int
}

type progressKey struct{}

type stallKey struct{}

// WithStallTimeout returns a context under which a transfer gives up once
// d passes with no progress (waiting for the lock counts as none): for a
// download, no data arriving; for an upload, no budget, no chunk queued
// and none of our packets acknowledged. The context's deadline caps the
// whole transfer; without one, a still download's default timeout does:
// download_still's stall_timeout (bmdwire#73, 588a389, v1.11.0). A 4K
// still is 12 MB or more on the wire and an 8K one four times that, so
// one fixed timeout is either too short for them or far too long to
// notice a transfer that stopped. A macro transfer's default timeout is
// a stall timeout already (bmdwire#101); WithStallTimeout replaces it.
// The error of a stalled transfer says "stalled" and wraps
// context.DeadlineExceeded.
func WithStallTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, stallKey{}, d)
}

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
	if t.stallRetries > 0 {
		p.Retry, p.Retries = t.stallRetries, uploadRetries
	}
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

	// locks is true from the LKOB until we send the release, whatever
	// LKST says meanwhile: the switcher announces lock changes once a
	// frame, so the release of the frame just given back is announced
	// after the LKOB for the request sent with it (bmdwire#36).
	locks         map[uint16]bool
	lockRequested map[uint16]time.Time // _lock_requested
	// releasing marks a store we released whose release hasn't been
	// announced (no LKST since), so LockHolder still says this session
	// rather than another client. It defers nothing.
	releasing map[uint16]bool
	// yielding holds the stores whose next request waits another client's
	// turn (_yielding), foreignHeld those another client is seen holding,
	// from its LKST until a release's (_foreign_held); see lockCourtesy.
	// yieldTimer wakes the lane at yieldDue, the earliest yield's end.
	yielding    map[uint16]*yieldState
	foreignHeld map[uint16]bool
	yieldTimer  *time.Timer
	yieldDue    time.Time
	// ownRelease holds, for a store whose yield a release of ours
	// started, when we released it: the turn after it goes to a waiter
	// first (_own_release, lockHandover). othersSeen is when another
	// client was last seen at the stores (its LKST, its FTDC, an FTDE 6
	// to us), zero if never; lastGrant, when our last LKOB for each
	// store came (_last_grant).
	ownRelease map[uint16]time.Time
	othersSeen time.Time
	lastGrant  map[uint16]time.Time

	budget *FileTransferContinueDataField // transfer_budget: nil when exhausted

	// tryAgainDue is when the transfer refused with FTDE 1 is asked again
	// (_try_again_due), zero when none waits.
	tryAgainDue time.Time
	// draining is the abandoned download being acknowledged to its end
	// (_draining), nil if none; lastData, when the transfer in flight last
	// had a chunk (_transfer_last_data), zero before its first.
	draining *drainState
	lastData time.Time
	// asked holds the ids of the last orphanIDs transfers asked for,
	// newest last (_asked_tids); orphans, those whose chunks came after we
	// stopped following them.
	asked   []uint16
	orphans map[uint16]bool

	// The paced bulk queue's triggers (see bulkTrigger).
	bulkPending int
	bulkNext    time.Time
	bulkTimer   *time.Timer

	// uploadStall: what progress of the upload in hand looked like
	// (uploadMark, set while marked), since when; grants counts the
	// budgets granted (_upload_mark, _upload_mark_at, _grants).
	uploadMark   progressMark
	marked       bool
	uploadMarkAt time.Time
	grants       int
	// carried holds the uploads of a session lost or replaced, for the
	// next one (_carried_uploads); resetLane keeps them.
	carried []*transferTask
}

// progressMark is what counts as progress for an upload in the phase it
// is in (_upload_progress_mark): whether it is carried over, the session
// ready, its lock held, its transfer in flight and started, its transfer
// id, and once its data goes, our packets acknowledged, its data cut and
// the budgets granted.
type progressMark struct {
	task                                    *transferTask
	carried, ready, held, inFlight, started bool
	tid                                     uint16
	acked, offset, grants                   int
}

// Another client's turn (bmdwire#38 and #40, v1.5.2). Another client's
// request waiting for the store is handed it at our release, and nothing
// is announced; a request of ours soon after is granted beside it (a PLCK
// beside ATEM Software Control's LOCK) and aborts its transfer with FTDE
// 6, and ASC, which tries each still once when it connects, keeps no
// thumbnail. So after every per-frame release, and any client's announced
// release, a request waits for the store to be announced free (LKST) and
// lockCourtesy more; never while another client is seen holding it; and
// with no announcement, while anyone is seen at work (another session's
// FTDC, any LKST), asking after lockQuiet of quiet.
const (
	lockCourtesy = 60 * time.Millisecond // LOCK_COURTESY
	lockQuiet    = 10 * time.Second      // LOCK_QUIET
)

// The turn after our own release goes to whoever was waiting for it
// (bmdwire#63, #68, 6876ba2, v1.9.0). They ask lockCourtesy after the
// release is announced; so did we, and two requests that close together
// are both granted, or one is aborted (FTDE 6 to a download; to an upload,
// a refusal of every chunk of its budget). Measured on a Constellation HD,
// 2026-10-30: an uploader and a downloader in one process asked 0.3 ms
// apart after each upload's release. So after a release of ours, our next
// request waits lockHandover more: time for a waiter's request, its grant,
// and the LKST that announces it to reach us, after which the store is
// another client's and we ask once they let go. Only while another client
// has been seen within lockQuiet: alone, nothing is waiting, and a restore
// of 20 stills would pay it 20 times for nobody.
//
// Our release's own LKST comes at the switcher's next frame tick, about
// 40 ms later. A release announced later than lockEchoWindow is another
// client's: the store was handed to a request waiting at the switcher,
// unannounced, and that client has now let go; we ask as its waiter. And
// an LKST "taken" within lockEchoWindow of our own LKOB is the late
// announcement of our own grant (a transfer or a clear shorter than a
// frame, released before it was announced; c7a8788), not another client.
const (
	lockHandover   = 100 * time.Millisecond // LOCK_HANDOVER
	lockEchoWindow = 250 * time.Millisecond // LOCK_ECHO_WINDOW
)

// drainState is an abandoned download being drained: its transfer id and
// store, when it was abandoned and when it last had a chunk.
type drainState struct {
	tid, store  uint16
	since, last time.Time
}

// yieldState is a store's turn for other clients: since, when we released
// it or last saw anyone at work; announced, when it was announced free
// (zero until then).
type yieldState struct{ since, announced time.Time }

// yieldDue is _yield_due: when a store we are leaving free may be asked
// for again.
func (s *Session) yieldDue(store uint16, y *yieldState) time.Time {
	if !y.announced.IsZero() {
		if _, own := s.lane.ownRelease[store]; own && s.othersRecently() {
			return y.announced.Add(lockCourtesy + lockHandover)
		}
		return y.announced.Add(lockCourtesy)
	}
	return y.since.Add(lockQuiet)
}

// othersRecently is _others_recently: another client was seen within
// lockQuiet.
func (s *Session) othersRecently() bool {
	o := s.lane.othersSeen
	return !o.IsZero() && s.t.now().Sub(o) < lockQuiet
}

// endYield is _end_yield: the store's turn for others is over.
func (s *Session) endYield(store uint16) {
	delete(s.lane.yielding, store)
	delete(s.lane.ownRelease, store)
}

func newLane() lane {
	return lane{
		queues:        make(map[uint16][]*transferTask),
		nextID:        firstTransferID(),
		locks:         make(map[uint16]bool),
		lockRequested: make(map[uint16]time.Time),
		releasing:     make(map[uint16]bool),
		yielding:      make(map[uint16]*yieldState),
		foreignHeld:   make(map[uint16]bool),
		ownRelease:    make(map[uint16]time.Time),
		lastGrant:     make(map[uint16]time.Time),
		orphans:       make(map[uint16]bool),
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
	if w == 0 {
		// A mode the table doesn't have has no frame size (bmdwire#71,
		// 9e52191), so there is nothing to fit the image to.
		return fmt.Errorf("atem: no frame size for video mode %s, so no still can be sized for it", mode.Label())
	}
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
	e, err := EncodeStill(frame)
	if err != nil {
		return err
	}
	return s.UploadEncodedStill(ctx, slot, e, name, description)
}

// EncodedStill is a frame ready to upload: RLE-encoded, with the frame's
// length and MD5 (FTSD's length, FTFD's hash).
type EncodedStill struct {
	rle    []byte
	length int
	hash   MD5
}

// EncodeStill encodes a frame for UploadEncodedStill: the work
// UploadStillFrame does before it queues the upload (an 8 MB 1080p frame
// takes a fifth of a second), which a caller sending several stills can do
// for the next while one uploads. atemwire's upload takes RLE data the same
// way (compressed=True).
func EncodeStill(frame []byte) (EncodedStill, error) {
	rle, err := RLEEncode(frame)
	if err != nil {
		return EncodedStill{}, err
	}
	return EncodedStill{rle: rle, length: len(frame), hash: md5.Sum(frame)}, nil
}

// UploadEncodedStill uploads a frame EncodeStill encoded to still slot
// slot, as UploadStillFrame does.
func (s *Session) UploadEncodedStill(ctx context.Context, slot int, e EncodedStill, name, description string) error {
	if err := checkSlot("still", slot, 0xFFFF); err != nil {
		return err
	}
	t := &transferTask{
		kind: taskUpload, store: StoreStills, slot: uint16(slot),
		data: e.rle, dataLength: e.length, hash: e.hash,
		name: name, description: description,
	}
	_, err := s.runTransfer(ctx, t, StillUploadTimeout)
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
// once MacroTimeout passes with nothing arriving (bmdwire#101: it was the
// whole download's).
func (s *Session) DownloadMacro(ctx context.Context, slot int) ([]byte, error) {
	if err := checkSlot("macro", slot, 0xFFFF); err != nil {
		return nil, err
	}
	t := &transferTask{kind: taskDownload, store: StoreMacros, slot: uint16(slot)}
	return s.runTransfer(ctx, t, macroTimeout)
}

// UploadMacro uploads bytecode to macro slot slot (0-based) with a name and
// description (cut to 64 and 128 bytes): FTSD (mode 0x0300), FTCD budgets,
// FTDa chunks, FTFD with the bytecode's MD5, FTDC. Empty bytecode (FTSD
// and FTFD together, no budget) doesn't empty the slot: the switcher keeps
// a macro with the name and description and no ops. It is atemwire's
// upload_macro_bytecode; without a context deadline it gives up once
// MacroTimeout passes without progress (no budget, no chunk queued, none
// of our packets acknowledged), however long a slow link takes in all
// (bmdwire#101: it was the whole upload's).
//
// The switcher makes the slot a nameless macro as the upload starts (it
// pre-erases, FTSD mode 0x0300). A failed upload is withdrawn, as any
// transfer its caller gives up on is, and a slot the state showed empty
// before is emptied again (MAct 5, MacroDelete), or "Macro N" stays
// behind (bmdwire#101, 85d2f58). A slot that held a macro has lost it
// either way, and the error says so.
func (s *Session) UploadMacro(ctx context.Context, slot int, name, description string, bytecode []byte) error {
	if err := checkSlot("macro", slot, 0xFFFF); err != nil {
		return err
	}
	wasUsed := s.macroSlotUsed(uint16(slot))
	t := &transferTask{
		kind: taskUpload, store: StoreMacros, slot: uint16(slot),
		data: bytecode, dataLength: len(bytecode), hash: md5.Sum(bytecode),
		name: name, description: description,
	}
	_, err := s.runTransfer(ctx, t, macroTimeout)
	if err == nil || !t.queued {
		return err
	}
	s.cleanUpFailedMacroUpload(uint16(slot), wasUsed)
	if wasUsed == slotUsed {
		err = fmt.Errorf("%w; slot %d held a macro, which the switcher erased as the upload started", err, slot)
	}
	return err
}

// slotState is what the state says of a slot before a transfer.
type slotState uint8

const (
	slotUnknown slotState = iota // no MPrp for it yet
	slotEmpty
	slotUsed
)

// macroSlotUsed is _macro_slot_used: whether macro slot slot held a macro,
// as the state shows it.
func (s *Session) macroSlotUsed(slot uint16) slotState {
	ch := make(chan slotState, 1)
	if s.do(func() {
		p := s.state.Macros.Properties[slot]
		switch {
		case p == nil:
			ch <- slotUnknown
		case p.IsUsed:
			ch <- slotUsed
		default:
			ch <- slotEmpty
		}
	}) != nil {
		return slotUnknown
	}
	return <-ch
}

// cleanUpFailedMacroUpload is _clean_up_failed_upload, after runTransfer
// has withdrawn the task (atemwire's cancel_transfer): a slot that was
// empty before is emptied again, on a session that is still up. A slot the
// state didn't know is left alone.
func (s *Session) cleanUpFailedMacroUpload(slot uint16, wasUsed slotState) {
	if wasUsed != slotEmpty {
		return
	}
	sent := make(chan struct{})
	if s.do(func() {
		defer close(sent)
		if !s.initialized || s.t.state != stateEstablished {
			return
		}
		s.sendCommands(MacroAction{Slot: slot, Action: MacroDelete})
		s.log.Info("atem: macro upload failed: emptied the slot again (it was empty before)", "slot", slot)
	}) == nil {
		<-sent
	}
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
	if s.lane.locks[store] || s.lane.releasing[store] {
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
	// The stall clock, when asked for: the transfer gives up once it has
	// made no progress for stall.
	stall, _ := ctx.Value(stallKey{}).(time.Duration)
	if _, ok := ctx.Deadline(); !ok {
		if t.timeoutIsStall() {
			// The default timeout is time without progress, with no cap
			// on the whole (bmdwire#101); WithStallTimeout replaces it.
			if stall <= 0 {
				stall = timeout
			}
		} else {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}
	t.done = make(chan transferResult, 1)
	progress, _ := ctx.Value(progressKey{}).(func(Progress))
	if progress != nil {
		t.progress = make(chan Progress, 1)
	}
	t.size = len(t.data)
	var stallC <-chan time.Time // nil, and never ready, without it
	if stall > 0 && t.kind != taskClear {
		t.moved()
		stallC = time.After(stall)
	}
	var cause error
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
	t.queued = true
wait:
	for {
		select {
		case r := <-t.done:
			// The last report was queued before the result: when both
			// are ready, select may take the result first, and the
			// report would be lost.
			select {
			case p := <-t.progress:
				progress(p)
			default:
			}
			return r.data, r.err
		case p := <-t.progress: // nil, and never ready, without WithProgress
			progress(p)
		case <-stallC:
			quiet := time.Since(time.Unix(0, t.heard.Load()))
			if quiet >= stall {
				what := "no data"
				if t.kind == taskUpload {
					what = "no progress"
				}
				cause = fmt.Errorf("stalled (%s for %v): %w", what, stall, context.DeadlineExceeded)
				break wait
			}
			stallC = time.After(stall - quiet)
		case <-ctx.Done():
			cause = ctx.Err()
			break wait
		}
	}
	ch := make(chan error, 1)
	if err := s.do(func() { ch <- s.cancelTask(t, cause) }); err != nil {
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

// timeoutIsStall reports whether t's default timeout is time without
// progress rather than the whole transfer's: a macro's (bmdwire#101).
func (t *transferTask) timeoutIsStall() bool {
	return t.store == StoreMacros
}

// moved stamps t as making progress now, for the stall clock.
func (t *transferTask) moved() {
	t.heard.Store(time.Now().UnixNano())
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
// abandons it, its lock released per frame, except a still download with
// its request out, which is drained first (drainStall, bmdwire#73, #80). atemwire's callers instead
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
		// A still download the switcher is sending, or may be about to
		// start, is drained to its end, its lock kept until then
		// (drainStall); one that has gone quiet for drainStall is given up
		// on with its lock, as before.
		now := s.t.now()
		drain := t.kind == taskDownload && t.store != StoreMacros && l.requested &&
			l.locks[t.store] && (l.lastData.IsZero() || now.Sub(l.lastData) < drainStall) &&
			l.draining == nil
		l.tryAgainDue = time.Time{}
		l.popHead(t)
		l.transfer = nil
		l.requested = false
		l.buffer, l.bufferBytes = nil, 0
		if drain {
			l.draining = &drainState{tid: t.tid, store: t.store, since: now, last: now}
			s.log.Info("atem: download abandoned by its caller: acknowledging it to its end before giving the lock back",
				"task", t.describe(), "transfer", t.tid)
			s.armYieldTimer(now.Add(drainStall))
		} else {
			s.releaseThenContinue(t.store, true)
		}
	} else {
		if i := slices.Index(l.carried, t); i >= 0 {
			notGranted = false // its session, not its lock, was missing
			l.carried = slices.Delete(l.carried, i, i+1)
		}
		l.remove(t)
		s.transferTrigger(t.store, false)
	}
	err := fmt.Errorf("atem: %s: %w", t.describe(), cause)
	switch {
	case notGranted:
		err = fmt.Errorf("%w (%w)", err, ErrLockNotGranted)
	case t.tryAgain > 0:
		// The switcher answering "try again" (FTDE 1) all along is not
		// silence: its transfer engine is busy, with another client's
		// transfer or one nobody is taking (bmdwire#80).
		err = fmt.Errorf("%w: the switcher answered busy (try again) %d times; its transfer engine is taken", err, t.tryAgain)
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
	if t.kind == taskUpload {
		s.armYieldTimer(s.t.now().Add(uploadLook)) // uploadStall
	}
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
	if l.draining != nil {
		// The store lock is ours until the abandoned download ends; a
		// request now would meet its busy engine (FTDE 1).
		s.log.Debug("atem: an abandoned download is still finishing; the next transfer waits for it")
		return
	}
	if !l.tryAgainDue.IsZero() {
		// A transfer the switcher answered busy is asked again only after
		// its pause; the yield timer asks then.
		if s.t.now().Before(l.tryAgainDue) {
			s.armYieldTimer(l.tryAgainDue)
			return
		}
		l.tryAgainDue = time.Time{}
		retry = true
	}
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
		if l.foreignHeld[next.store] {
			// Another client's turn: its release's LKST asks for us.
			s.log.Debug("atem: store held by another client; asking once released", "store", next.store)
			return
		}
		if y := l.yielding[next.store]; y != nil {
			if due := s.yieldDue(next.store, y); s.t.now().Before(due) {
				s.log.Debug("atem: leaving the store free a moment for other clients", "store", next.store)
				s.armYieldTimer(due)
				return
			}
			s.endYield(next.store)
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
		s.releaseThenContinue(next.store, true)
		return
	}
	if !retry {
		l.nextID = nextTransferID(l.nextID)
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
	l.lastData = time.Time{}
	if next.kind == taskUpload {
		l.marked = false
		s.armYieldTimer(s.t.now().Add(uploadLook))
	}
	if n := len(l.asked); n == 0 || l.asked[n-1] != next.tid {
		l.asked = append(l.asked, next.tid)
		if len(l.asked) > orphanIDs {
			l.asked = l.asked[len(l.asked)-orphanIDs:]
		}
	}
	delete(l.orphans, next.tid)
	if next.kind == taskUpload {
		// Each attempt sends its metadata and its data from the start.
		next.offset, next.metadataSent = 0, false
	}
	if next.kind == taskUpload && next.dataLength == 0 {
		// An empty upload gets no FTCD, so FTFD goes with the request,
		// as ASC's "Restore from State" does for an empty macro.
		out = append(out, s.fileData(next))
		next.metadataSent = true
	}
	s.sendCommands(out...)
}

func (s *Session) fileData(t *transferTask) TransferFileData {
	return TransferFileData{Transfer: t.tid, Hash: t.hash, Name: t.name, Description: t.description}
}

// releaseThenContinue is _release_then_continue: a transfer is over, so
// give its store's lock back now, and leave another client its turn: the
// next request, now or queued later (a caller that queues one transfer at
// a time has nothing queued here), waits until the store is announced
// free and lockCourtesy more (bmdwire#38, #40). bmdwire#36 sent the
// release and the next request in one packet, as ASC does; that cut
// ASC's turn short. requestNext false leaves the next request to the
// store's release broadcast (after an FTDE 6, another session is queued
// for the store). The lock-exempt macro store starts the next at once.
func (s *Session) releaseThenContinue(store uint16, requestNext bool) {
	l := &s.lane
	l.transfer = nil
	if store != StoreMacros && l.locks[store] {
		s.log.Info("atem: releasing store lock (per frame)", "store", store)
		l.locks[store] = false
		l.releasing[store] = true
		s.sendCommands(Lock{Store: store})
		l.yielding[store] = &yieldState{since: s.t.now()}
		l.ownRelease[store] = s.t.now()
		if !requestNext {
			return
		}
	}
	s.transferTrigger(store, false)
}

// armYieldTimer has the lane woken at at, unless it is woken sooner.
func (s *Session) armYieldTimer(at time.Time) {
	l := &s.lane
	if !l.yieldDue.IsZero() && !at.Before(l.yieldDue) {
		return
	}
	l.yieldDue = at
	d := max(0, at.Sub(s.t.now()))
	if l.yieldTimer == nil {
		l.yieldTimer = time.NewTimer(d)
	} else {
		l.yieldTimer.Reset(d)
	}
}

// yieldTimerC is the yield timer's channel, nil when unarmed.
func (s *Session) yieldTimerC() <-chan time.Time {
	if s.lane.yieldTimer == nil || s.lane.yieldDue.IsZero() {
		return nil
	}
	return s.lane.yieldTimer.C
}

// serviceYields is _service_yields: ask for each store whose turn for
// others is over, and forget a turn that is over with nothing waiting.
// A turn not yet over (someone at work moved it) is only timed again, and
// a store another client holds waits for its release (bmdwire#42,
// 8877133: in bmdwire a caller's every Wakeup ran the trigger; here only
// the timer runs this, so the wire is the same either way).
func (s *Session) serviceYields() {
	l := &s.lane
	l.yieldDue = time.Time{}
	now := s.t.now()
	s.serviceDrain()
	s.serviceUploadStall()
	if due := l.tryAgainDue; !due.IsZero() {
		switch {
		case l.transfer == nil:
			l.tryAgainDue = time.Time{}
		case now.Before(due):
			s.armYieldTimer(due)
		default:
			l.tryAgainDue = time.Time{}
			s.transferTrigger(l.transfer.store, true)
		}
	}
	for store, y := range l.yielding {
		due := s.yieldDue(store, y)
		switch {
		case len(l.queues[store]) == 0:
			if !now.Before(due) {
				s.endYield(store)
			}
		case l.requested || l.locks[store] || l.foreignHeld[store]:
		case now.Before(due):
			s.armYieldTimer(due)
		default:
			s.transferTrigger(store, false)
		}
	}
}

// serviceDrain is _service_drain: give the abandoned download's lock back
// once nothing of it has come for drainStall, or it has run drainCap.
func (s *Session) serviceDrain() {
	d := s.lane.draining
	if d == nil {
		return
	}
	now := s.t.now()
	switch {
	case now.Sub(d.last) >= drainStall:
		s.log.Warn("atem: abandoned download stopped (nothing for a while): giving its lock back",
			"transfer", d.tid, "quiet", now.Sub(d.last).Round(time.Second))
		s.endDrain()
	case now.Sub(d.since) >= drainCap:
		s.log.Warn("atem: abandoned download stopped (over the cap): giving its lock back",
			"transfer", d.tid, "cap", drainCap)
		s.endDrain()
	default:
		s.armYieldTimer(minTime(d.last.Add(drainStall), d.since.Add(drainCap)))
	}
}

// endDrain is _end_drain: the abandoned download is over; give its store
// lock back and carry on with the queue.
func (s *Session) endDrain() {
	d := s.lane.draining
	s.lane.draining = nil
	if d != nil {
		s.releaseThenContinue(d.store, true)
	}
}

// ackOrphan is _ack_orphan: acknowledge a chunk of a transfer we asked for
// and no longer follow; anything else is not ours to answer.
func (s *Session) ackOrphan(tid uint16) {
	l := &s.lane
	if !slices.Contains(l.asked, tid) {
		s.log.Debug("atem: FTDa for a transfer not ours; ignoring", "transfer", tid)
		return
	}
	if !l.orphans[tid] {
		l.orphans[tid] = true
		s.log.Warn("atem: chunks of a download we no longer follow: acknowledging it to its end so the switcher can take the next request",
			"transfer", tid)
	}
	s.sendCommands(TransferAck{Transfer: tid})
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// someoneAtWork is _someone_at_work: another session's transfer ended, so
// a store we wait for, not yet announced free, is busy: lockQuiet starts
// again, and another client has been seen.
func (s *Session) someoneAtWork() {
	s.lane.othersSeen = s.t.now()
	for _, y := range s.lane.yielding {
		if y.announced.IsZero() {
			y.since = s.t.now()
		}
	}
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
		s.lane.lastGrant[f.Store] = s.t.now()
		delete(s.lane.lockRequested, f.Store)
		delete(s.lane.releasing, f.Store)
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

// onLockReleased is the release half of the lock-state branch. A release
// announced while we hold the store is our own previous one, announced a
// frame late: our hold ends only when we release it (bmdwire#36).
// Otherwise the store is seen free: a request waits lockCourtesy more, in
// case the client that released takes it again for its next transfer
// (ATEM Software Control does, announced as a release and a lock together;
// bmdwire#38), then goes ("the pounce").
func (s *Session) onLockReleased(store uint16) {
	l := &s.lane
	delete(l.releasing, store)
	if l.locks[store] {
		s.log.Debug("atem: release announced while we hold the store: our own, late", "store", store)
		return
	}
	delete(l.foreignHeld, store)
	now := s.t.now()
	if y := l.yielding[store]; y == nil {
		l.yielding[store] = &yieldState{since: now, announced: now}
		delete(l.ownRelease, store)
	} else if y.announced.IsZero() {
		y.announced = now
		if released, ok := l.ownRelease[store]; ok && now.Sub(released) > lockEchoWindow {
			// Not our release's echo: another client's (lockEchoWindow).
			delete(l.ownRelease, store)
		}
	}
	delete(l.lockRequested, store)
	if len(l.queues[store]) > 0 && !l.requested {
		s.transferTrigger(store, false)
	}
}

// onLockTaken is the other half: a lock taken is announced, so a release
// of ours before it has been announced too; taken by another client, the
// store is theirs until announced free, and a wait for it starts again
// (bmdwire#38).
func (s *Session) onLockTaken(store uint16) {
	l := &s.lane
	delete(l.releasing, store)
	if l.locks[store] {
		return
	}
	l.foreignHeld[store] = true
	now := s.t.now()
	if granted, ok := l.lastGrant[store]; !ok || now.Sub(granted) > lockEchoWindow {
		// Not the late announcement of a grant of ours: another client.
		l.othersSeen = now
	}
	if y := l.yielding[store]; y != nil {
		*y = yieldState{since: now}
		// Theirs now: the next turn after their release is ours as a
		// waiter, not as the releaser.
		delete(l.ownRelease, store)
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
	s.lane.grants++
	if t := s.lane.transfer; t != nil && t.kind == taskUpload {
		t.moved()
	}
	if t := s.lane.transfer; t != nil && t.kind == taskUpload && !t.metadataSent {
		// FTFD with the first budget, before the data, as ATEM Software
		// Control sends it (bmdwire#36); it used to wait for the bulk
		// queue to drain after the last chunk.
		s.sendFileData(t)
	}
	s.queueChunks()
}

// onChunk is the file-transfer-data branch: one downloaded chunk, kept and
// acknowledged.
func (s *Session) onChunk(f *FileTransferDataField) {
	l := &s.lane
	if d := l.draining; d != nil && f.Transfer == d.tid {
		// The abandoned download: acknowledged, its data dropped.
		d.last = s.t.now()
		s.sendCommands(TransferAck{Transfer: d.tid})
		return
	}
	if l.transfer == nil || f.Transfer != l.transfer.tid {
		// A chunk of a transfer we no longer follow (orphanIDs), or not
		// ours at all.
		s.ackOrphan(f.Transfer)
		return
	}
	l.lastData = s.t.now()
	l.transfer.moved()
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
	if d := l.draining; d != nil && f.Transfer == d.tid {
		s.log.Info("atem: abandoned download ended", "transfer", d.tid, "status", transferStatus(f.Status))
		s.endDrain()
		return
	}
	if l.orphans[f.Transfer] {
		delete(l.orphans, f.Transfer)
		s.log.Info("atem: a download we no longer followed ended", "transfer", f.Transfer, "status", transferStatus(f.Status))
		return
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
	case 1:
		// Try again, with the same transfer id, after a pause that
		// doubles each time; give up after tryAgainAttempts.
		t.tryAgain++
		if t.tryAgain == 1 {
			// Once per transfer: a busy switcher was invisible in the
			// logs, and read as "no data" at the caller.
			s.log.Info("atem: the switcher is busy (FTDE 1, try again); asking again", "task", t.describe())
		}
		if t.tryAgain > tryAgainAttempts {
			s.log.Error("atem: transfer refused too often (FTDE 1, try again): giving up; the switcher's transfer engine is busy",
				"task", t.describe(), "refusals", tryAgainAttempts)
			s.failTransfer(t, f)
			return
		}
		pause := min(tryAgainMax, tryAgainFirst<<(t.tryAgain-1))
		s.log.Debug("atem: retrying transfer", "in", pause, "try_again", t.tryAgain)
		l.tryAgainDue = s.t.now().Add(pause)
		s.armYieldTimer(l.tryAgainDue)
	case 5: // no lock: ask for it again
		l.locks[t.store] = false
		s.transferTrigger(t.store, true)
	case 6:
		// Another session asked for the store while ours was being set up:
		// the switcher aborted our request but we still hold the lock. Give
		// it back, keep the task, and ask again on the next release.
		delete(l.lockRequested, t.store)
		l.othersSeen = s.t.now()
		s.releaseThenContinue(t.store, false)
	default:
		s.failTransfer(t, f)
	}
}

// failTransfer is _fail_transfer: fatal for this transfer (a status other
// than 1, 5 and 6, or tryAgainAttempts refusals): drop it, release the
// lock, carry on.
func (s *Session) failTransfer(t *transferTask, f *FileTransferErrorField) {
	l := &s.lane
	l.tryAgainDue = time.Time{}
	l.popHead(t)
	l.transfer = nil
	l.buffer, l.bufferBytes = nil, 0
	s.releaseThenContinue(t.store, true)
	s.finish(t, transferResult{err: &TransferError{Store: t.store, Slot: t.slot, Transfer: f.Transfer, Status: f.Status}})
}

// onTransferComplete is the file-transfer-data-complete branch: the lock is
// released before the (slow) RLE decode and the delivery.
func (s *Session) onTransferComplete(f *FileTransferDataCompleteField) {
	l := &s.lane
	if d := l.draining; d != nil && f.Transfer == d.tid {
		s.log.Info("atem: abandoned download finished; its data dropped", "transfer", d.tid)
		s.endDrain()
		return
	}
	if l.orphans[f.Transfer] {
		delete(l.orphans, f.Transfer)
		s.log.Info("atem: a download we no longer followed finished; its data dropped", "transfer", f.Transfer)
		return
	}
	if l.transfer == nil {
		// The switcher announces every session's completions.
		s.log.Debug("atem: FTDC for another session's transfer")
		s.someoneAtWork()
		return
	}
	if f.Transfer != l.transfer.tid {
		s.someoneAtWork()
		return
	}
	t := l.transfer
	l.popHead(t)
	raw := slices.Concat(l.buffer...)
	l.buffer, l.bufferBytes = nil, 0
	l.requested = false
	s.releaseThenContinue(t.store, true)
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
	if m := s.t.maxChunk; m > 0 && m < size {
		// Datagrams that big do not reach this switcher (chunkSteps).
		size = m
	}
	for range int(l.budget.Count) {
		rest := t.data[min(t.offset, len(t.data)):]
		if len(rest) == 0 {
			break
		}
		chunk := rest[:min(size, len(rest))]
		used := size
		switch {
		case len(chunk) >= 8 && isRLEHeader(chunk[len(chunk)-8:]):
			chunk, used = rest[:min(size-8, len(rest))], size-8
		case len(chunk) >= 16 && isRLEHeader(chunk[len(chunk)-16:len(chunk)-8]):
			chunk, used = rest[:min(size-16, len(rest))], size-16
		}
		t.offset += used
		t.moved()
		l.budget.Count--
		if l.budget.Count == 0 {
			l.budget = nil
		}
		s.t.queuePacket(TransferData{Transfer: t.tid, Data: chunk}.Encode())
		if len(s.t.bulk) >= queuePortion {
			// The bulk queue holds bulkQueueMax: more at once (chunks
			// taken back from three budgets in flight, 2,304) dropped
			// the oldest, and the still came out short (QUEUE_PORTION).
			if err := s.t.drainQueue(); err != nil {
				s.fatal = err
				return
			}
		}
		t.report(min(t.offset, len(t.data)), t.size) // upload-progress
		if l.budget == nil {
			break
		}
	}
	// The whole budget at once, back to back, as ATEM Software Control
	// sends it (bmdwire#36): the budget is the switcher's flow control,
	// 768 chunks of a 1080p still at a time on a Constellation. Five
	// chunks per packet from the switcher took 23 s a still.
	if err := s.t.drainQueue(); err != nil {
		s.fatal = err
	}
}

func isRLEHeader(b []byte) bool {
	for _, c := range b {
		if c != 0xFE {
			return false
		}
	}
	return true
}

// chunksTakenBack is _chunks_taken_back (bmdwire#95): data chunks of
// our upload that did not reach the switcher, taken back by the
// transport. The switcher has none of them and still owes us that many of
// its budget. They go again from where they started, in smaller chunks.
func (s *Session) chunksTakenBack(tb *takenBack) {
	l := &s.lane
	t := l.transfer
	if t == nil || t.kind != taskUpload || t.tid != tb.transfer {
		s.log.Warn("atem: data chunks taken back of a transfer no longer in flight, dropped", "transfer", tb.transfer, "chunks", len(tb.sizes))
		return
	}
	back := 0
	for _, n := range tb.sizes {
		back += n
	}
	// The offset runs past the end after a last, short chunk.
	t.offset = min(t.offset, len(t.data)) - back
	n := uint16(len(tb.sizes))
	if l.budget == nil {
		l.budget = &FileTransferContinueDataField{Transfer: t.tid, Size: uint16(tb.limit), Count: n}
	} else {
		l.budget.Count += n
	}
	s.log.Info("atem: upload chunks go again smaller", "transfer", t.tid, "chunks", n, "bytes", back, "size", tb.limit)
	s.queueChunks()
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
	if t.offset < len(t.data) {
		s.queueChunks()
		return
	}
	if !t.metadataSent {
		// Not sent with the first budget; the switcher needs it to finish.
		s.sendFileData(t)
	}
}

// sendFileData sends an upload's FTFD: its name, description and MD5.
func (s *Session) sendFileData(t *transferTask) {
	s.log.Info("atem: sending file metadata", "transfer", t.tid)
	s.sendCommands(s.fileData(t))
	t.metadataSent = true
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
			l.armBulkTimer(wait)
			return
		}
	}
	sent, flushed, err := s.t.queueTrigger()
	if err != nil {
		s.fatal = err
		return
	}
	if sent > 0 {
		l.bulkNext = s.t.now().Add(s.t.delay)
		// aggressive_drain empties the queue within the one trigger,
		// sleeping between batches; here the next batch waits for the
		// timer, not for the switcher's next packet.
		if s.t.drain && len(s.t.bulk) > 0 && l.bulkPending == 0 {
			l.bulkPending = 1
			l.armBulkTimer(s.t.delay)
		}
	}
	if flushed && flush {
		s.queueFlushed()
	}
}

// armBulkTimer sets the deferred-trigger timer to fire after wait.
func (l *lane) armBulkTimer(wait time.Duration) {
	if l.bulkTimer == nil {
		l.bulkTimer = time.NewTimer(wait)
	} else {
		l.bulkTimer.Reset(wait)
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
	id, others, carried := l.nextID, l.othersSeen, l.carried
	timer, ytimer := l.bulkTimer, l.yieldTimer
	*l = newLane()
	l.nextID, l.othersSeen, l.carried = id, others, carried
	if timer != nil {
		timer.Stop()
		l.bulkTimer = timer
	}
	if ytimer != nil {
		ytimer.Stop()
		l.yieldTimer = ytimer
	}
}

// --- an upload that stops moving (bmdwire#100) --------------------------------

// uploadInHand is _upload_in_hand: the upload being worked on, the
// transfer in flight, else the first upload waiting at the head of a
// queue (for its lock or its start), else one carried over from a session
// lost, waiting for the next. nil with a download in flight.
func (s *Session) uploadInHand() *transferTask {
	l := &s.lane
	if t := l.transfer; t != nil {
		if t.kind == taskUpload {
			return t
		}
		return nil
	}
	for _, st := range l.order {
		if q := l.queues[st]; len(q) > 0 && q[0].kind == taskUpload {
			return q[0]
		}
	}
	if len(l.carried) > 0 {
		return l.carried[0]
	}
	return nil
}

// uploadProgressMark is _upload_progress_mark: what counts as progress
// for t in the phase it is in. Our own packets acknowledged count only
// once its data is going: while it waits for a session or a lock, the
// requests we send for them are acknowledged and answer nothing (the
// hardware bench: a lock asked for again, acknowledged, never granted).
func (s *Session) uploadProgressMark(t *transferTask) progressMark {
	l := &s.lane
	m := progressMark{
		task:     t,
		carried:  slices.Contains(l.carried, t),
		ready:    s.initialized,
		held:     l.locks[t.store],
		inFlight: l.transfer == t,
		tid:      t.tid,
	}
	m.started = m.inFlight && l.requested
	if m.started {
		m.acked, m.offset, m.grants = s.t.ackedThrough, t.offset, l.grants
	}
	return m
}

// serviceUploadStall is _service_upload_stall: an upload with no progress
// for uploadStall in any phase, the switcher not having said it is done,
// goes again from the beginning in a new session, uploadRetries times at
// most, then fails. The yield timer runs it (atemwire's loop() looks every
// 0.5 s, whatever arrives), at least every uploadLook while an upload is
// in hand.
func (s *Session) serviceUploadStall() {
	l := &s.lane
	t := s.uploadInHand()
	if t == nil {
		l.marked = false
		return
	}
	mark := s.uploadProgressMark(t)
	now := s.t.now()
	if !l.marked || mark != l.uploadMark {
		l.uploadMark, l.marked, l.uploadMarkAt = mark, true, now
	}
	limit := s.uploadStall
	if !l.locks[t.store] && l.foreignHeld[t.store] {
		// Another client's turn, the switcher says: longer, not for ever.
		limit = lockWaitMax
	}
	if now.Sub(l.uploadMarkAt) < limit {
		s.armYieldTimer(minTime(l.uploadMarkAt.Add(limit), now.Add(uploadLook)))
		return
	}
	l.marked = false
	var phase string
	switch {
	case mark.carried:
		phase = "waiting for a new session"
	case !l.locks[t.store] && l.transfer != t:
		phase = "waiting for its lock"
	case !l.requested:
		phase = "waiting for the switcher to start it"
	default:
		phase = fmt.Sprintf("%d of %d bytes cut", min(t.offset, len(t.data)), len(t.data))
	}
	attempt := t.stallRetries + 1
	if attempt > uploadRetries {
		s.log.Error("atem: upload failed: no progress after its fresh starts, and the switcher never said it was done",
			"task", t.describe(), "for", limit, "phase", phase, "fresh_starts", uploadRetries)
		s.giveUpUpload(t, &UploadStalledError{Store: t.store, Slot: t.slot, Phase: phase, After: limit, Retries: uploadRetries, what: t.describe()})
		return
	}
	t.stallRetries = attempt
	s.log.Warn("atem: upload not moving and not confirmed: starting it again from the beginning in a new session",
		"task", t.describe(), "for", limit, "phase", phase, "retry", attempt, "of", uploadRetries)
	// upload-retrying: from the beginning, so its progress goes back to
	// nothing, the retry on it.
	t.report(0, t.size)
	// Only a goodbye frees what the abandoned transfer holds; the upload
	// is carried over to the new session (disconnected).
	s.newSession(fmt.Sprintf("%s stalled (%s)", t.describe(), phase))
	s.armYieldTimer(s.t.now().Add(uploadLook))
}

// carryUploadsOver is _carry_uploads_over, before the lane is reset for a
// new session: every upload, the one in flight and those queued, is kept
// for the next session, to go again from the beginning. They used to fail
// with ErrDisconnected, as atemwire's were dropped without a word.
func (s *Session) carryUploadsOver() {
	l := &s.lane
	var tasks []*transferTask
	if t := l.transfer; t != nil && t.kind == taskUpload {
		tasks = append(tasks, t)
		l.transfer = nil
	}
	for _, st := range l.order {
		for _, q := range l.queues[st] {
			if q.kind == taskUpload && !slices.Contains(tasks, q) {
				tasks = append(tasks, q)
			}
		}
	}
	for _, q := range tasks {
		l.remove(q)
		q.tid, q.offset, q.metadataSent = 0, 0, false
		q.report(0, q.size)
		q.moved() // its offset back at 0: bmdwire#101's progress mark moves
		if !slices.Contains(l.carried, q) {
			l.carried = append(l.carried, q)
		}
	}
	if len(tasks) > 0 {
		s.log.Warn("atem: session ended with uploads not confirmed: they go again from the beginning in the next session",
			"uploads", len(tasks))
	}
}

// requeueCarriedUploads is _requeue_carried_uploads: a new session is
// ready, so the uploads carried over go back in their queues, in order,
// and are asked for.
func (s *Session) requeueCarriedUploads() {
	l := &s.lane
	carried := l.carried
	l.carried = nil
	var stores []uint16
	for _, q := range carried {
		if _, ok := l.queues[q.store]; !ok {
			l.order = append(l.order, q.store)
		}
		l.queues[q.store] = append(l.queues[q.store], q)
		if !slices.Contains(stores, q.store) {
			stores = append(stores, q.store)
		}
	}
	for _, st := range stores {
		s.transferTrigger(st, false)
	}
	if len(carried) > 0 {
		l.marked = false
		s.armYieldTimer(s.t.now().Add(uploadLook))
	}
}

// giveUpUpload is _give_up_upload: t off its queue, its lock given back,
// and its caller told why.
func (s *Session) giveUpUpload(t *transferTask, err error) {
	l := &s.lane
	l.remove(t)
	if i := slices.Index(l.carried, t); i >= 0 {
		l.carried = slices.Delete(l.carried, i, i+1)
	}
	l.requested = false
	l.budget = nil
	l.buffer, l.bufferBytes = nil, 0
	l.tryAgainDue = time.Time{}
	delete(l.lockRequested, t.store)
	if l.locks[t.store] {
		s.releaseThenContinue(t.store, true)
	} else {
		l.transfer = nil
		if len(l.queues[t.store]) > 0 {
			s.transferTrigger(t.store, false)
		}
	}
	s.finish(t, transferResult{err: err})
}

// failCarried fails the uploads carried over, for a session that is
// closing: there is no next one.
func (s *Session) failCarried(err error) {
	for _, t := range s.lane.carried {
		s.finish(t, transferResult{err: err})
	}
	s.lane.carried = nil
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
		if err := s.t.sendReliableNow(Lock{Store: st}.Encode()); err != nil {
			s.log.Debug("atem: teardown lock release failed", "store", st, "err", err)
		}
		l.locks[st] = false
	}
	clear(l.releasing)
	clear(l.lockRequested)
	clear(l.yielding)
	clear(l.ownRelease)
}

func sortedStores(m map[uint16]bool) []uint16 {
	var out []uint16
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
