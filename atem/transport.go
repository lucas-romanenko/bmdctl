// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The UDP transport, ported from bmdwire v1.3.0 atemwire/transport.py
// (Packet and UdpProtocol). Python splits it across a UDP thread
// (_receive_packet_low, _send_packet_low) and the protocol worker
// (receive_packet); here both halves are methods on one struct that only the
// session's owner goroutine calls, so nothing in it is locked. It never reads
// the socket itself: the session hands it each datagram.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"
)

// DefaultPort is the switcher's UDP port.
const DefaultPort = 9910

// Header flags, the top five bits of the first header word.
const (
	flagReliable              = 0x01
	flagSYN                   = 0x02
	flagRetransmission        = 0x04
	flagRequestRetransmission = 0x08
	flagACK                   = 0x10
)

const (
	headerLen = 12

	// helloSession is the session id a client uses until the switcher
	// assigns one. The first byte of a SYN's data is the hello opcode.
	helloSession  = 0x1337
	helloConnect  = 0x01
	helloAccepted = 0x02
	helloNoRoom   = 0x03 // every client slot is taken (bmdwire#75)
	helloClosing  = 0x04

	// ackMagic goes in header bytes 8-9 of every ACK atemwire sends.
	ackMagic = 0x61

	// The switcher's sequence numbers are 15 bits: they wrap 0x7FFF -> 0.
	seqMask = 0x7FFF
	seqHalf = 0x4000

	// receivedHistory is the length of atemwire's received_packets window,
	// which drives both the retransmission dedup and the contiguous ACK:
	// twice the park window, so parked packets are still known when the
	// ACK walk reaches them (bmdwire#80, 1.13.0; 1024 before).
	receivedHistory = 2 * defaultDeliveryParkWindow

	// silenceTimeout is the select() timeout of atemwire's UDP thread:
	// this long without a datagram accepted from the switcher, and the
	// transport re-handshakes (see lastRX).
	silenceTimeout = 5 * time.Second
)

// The paced bulk send queue (BaseProtocol.send_queue and queue_trigger),
// which carries upload chunks.
const (
	bulkBatch    = 5                    // batch_size: packets per trigger
	bulkDelay    = 3 * time.Millisecond // batch_delay: atemwire sleeps this long after a batch
	drainBatch   = 10                   // batch_size under aggressive_drain
	drainDelay   = time.Millisecond     // batch_delay under aggressive_drain
	bulkQueueMax = 1024                 // the deque's maxlen
)

// The defaults for the three tunables atemwire keeps as class attributes
// (and its tests monkeypatch).
const (
	defaultRetransmitWindow = 4096 // RETRANSMIT_WINDOW
	// defaultDeliveryParkWindow is DELIVERY_PARK_WINDOW, the
	// retransmission window's 4096 since bmdwire#80 (1.13.0). At 512, a
	// gap a lossy link took long to fill dropped every packet parked
	// behind it, packets the switcher had had ACKed and would not send
	// again; a download's FTDa among them never got its FTUA and the
	// download stalled. The switcher keeps a packet until it is
	// acknowledged, so a gap fills in the end.
	defaultDeliveryParkWindow = 4096
	defaultRetransmitMaxBurst = 128 // RETRANSMIT_MAX_BURST
)

// Reading what is waiting, and acknowledging it (bmdwire#73, 1.11.0).
const (
	// drainMax is DRAIN_MAX: datagrams the session takes from its read
	// goroutine in one go, acknowledged together after them.
	drainMax = 512
	// ackEvery is ACK_EVERY: within a long run, a cumulative ACK goes
	// every this many packets, so the switcher's window keeps moving. It
	// ACKs our uploads the same way, one per 16 packets.
	ackEvery = 32
)

// chunkSteps is CHUNK_STEPS (bmdwire#95): the data sizes an upload
// chunk goes down to when chunks of the switcher's own size (1,392 bytes,
// 1,416-byte datagrams) do not reach it. An ATEM Mini Pro over a VPN at
// 80 ms got the lock request, FTSD and FTFD and not one chunk in 90 s and
// some 70 resends (WebATEM #63); it takes our packets in order only, so
// nothing after the first chunk counted either. 1,224 bytes fit a
// 1,280-byte path.
var chunkSteps = []int{1224, 952, 504}

// chunkLimits is _CHUNK_LIMITS: the chunk size each switcher was last
// found to need, by address, for every session of the process.
var (
	chunkLimitsMu sync.Mutex
	chunkLimits   = map[string]int{}
)

func chunkLimit(addr *net.UDPAddr) int {
	if addr == nil {
		return 0
	}
	chunkLimitsMu.Lock()
	defer chunkLimitsMu.Unlock()
	return chunkLimits[addr.IP.String()]
}

func setChunkLimit(addr *net.UDPAddr, n int) {
	if addr == nil {
		return
	}
	chunkLimitsMu.Lock()
	defer chunkLimitsMu.Unlock()
	chunkLimits[addr.IP.String()] = n
}

// takenBack is ChunksTakenBack: data chunks of an upload that did not
// reach the switcher, taken back to go again in chunks of limit bytes.
// sizes are their data lengths, in the order they were sent.
type takenBack struct {
	transfer uint16
	sizes    []int
	limit    int
}

// ftdaChunk is _ftda: the transfer id and data length of a packet that
// carries exactly one upload data chunk, as queueChunks sends them.
func ftdaChunk(data []byte) (transfer uint16, size int, ok bool) {
	if len(data) < 12 || string(data[4:8]) != "FTDa" || int(binary.BigEndian.Uint16(data[0:2])) != len(data) {
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(data[8:10]), int(binary.BigEndian.Uint16(data[10:12])), true
}

// Resending what the switcher hasn't acknowledged, and sends the kernel
// refuses for a moment (bmdwire#36, d050c10).
const (
	// resendAfter is RESEND_AFTER: our reliable packets the switcher
	// hasn't acknowledged for this long are sent again, from the first
	// unacknowledged one. The switcher asks for a packet it finds missing
	// only when a later one arrives; nothing follows the last chunk of an
	// upload budget until the next budget, which it grants only once it
	// has that chunk. It acknowledges ours within milliseconds.
	//
	// Since bmdwire#80 (1.13.0) it is the timer's start only. How long
	// "not acknowledged" is follows the link, as TCP's timer does (RFC
	// 6298): the smoothed time from sending a packet to its ACK plus
	// four times its variation, measured on packets sent once (Karn),
	// never below resendMin nor above resendMax, doubled for each resend
	// nobody acknowledges. The switcher takes our packets in order only
	// and never asks for one it missed, so the FTUA after a lost one, and
	// the rest of a download with it, waited the whole timer: a fixed
	// 250 ms per lost packet, where the ACK comes back in 2 ms (p50) to
	// 28 ms (p99) on a LAN (2026-11-06).
	resendAfter = 250 * time.Millisecond
	resendMin   = 50 * time.Millisecond // RESEND_MIN
	resendMax   = time.Second           // RESEND_MAX
	// dupACKs is DUP_ACKS: the switcher's same ACK this many times more,
	// on packets carrying nothing else, and the first packet it lacks is
	// resent at once (TCP's fast retransmit): it has had later ones since
	// and dropped them.
	dupACKs = 2
	// resendLogEvery is how often the routine resends are summed up in a
	// warning; each is logged at debug.
	resendLogEvery = 5 * time.Second
	// tooBigAfter is TOO_BIG_AFTER (bmdwire#95): an upload data chunk
	// (FTDa) that is the first packet the switcher has not acknowledged,
	// sent this many times more while the switcher goes on sending, is
	// taken back with every chunk after it and goes again smaller
	// (chunkSteps).
	tooBigAfter = 3
	// gapAskAfter and gapAskEvery are GAP_ASK_AFTER and GAP_ASK_EVERY
	// (bmdwire#100, 9824cab): a gap in the switcher's packets (one lost,
	// later ones parked behind it) open gapAskAfter is asked for, a
	// retransmission request from the first missing one, and again every
	// gapAskEvery while it stays open, as the switchers ask us. Delivery
	// used to wait for the switcher to send the lost packet again on its
	// own; a Constellation HD did not, after a 3 s dropout with nothing
	// of ours outstanding: the budget (FTCD) lost in it never came, and
	// the upload sat at 47 % until the caller gave up (the hardware
	// bench, 2026-11-24).
	gapAskAfter = 100 * time.Millisecond
	gapAskEvery = 250 * time.Millisecond
	// sendRetries is SEND_RETRIES: a send the kernel refuses with ENOBUFS
	// (macOS, a full buffer) or EAGAIN is tried again, 1 ms apart, this
	// many times before the packet counts as lost on the wire.
	sendRetries = 20
)

type transportState uint8

const (
	stateClosed transportState = iota
	stateSYNSent
	stateSYNReceived // declared by atemwire, never entered
	stateEstablished
)

func (s transportState) String() string {
	switch s {
	case stateClosed:
		return "closed"
	case stateSYNSent:
		return "syn-sent"
	case stateSYNReceived:
		return "syn-received"
	case stateEstablished:
		return "established"
	}
	return fmt.Sprintf("state(%d)", uint8(s))
}

// packet is one datagram: a 12-byte header and the data after it.
//
//	bytes 0-1   flags (top 5 bits) | length (low 11 bits)
//	bytes 2-3   session id
//	bytes 4-5   acknowledgement number
//	bytes 6-7   retransmission request: the first sequence the sender is
//	            missing, on a FLAG_REQUEST_RETRANSMISSION packet
//	bytes 8-9   "remote sequence number": 0x61 on atemwire's ACKs
//	bytes 10-11 sequence number
type packet struct {
	flags          uint8
	length         uint16
	session        uint16
	ack            uint16
	retransmitFrom uint16
	remoteSeq      uint16
	seq            uint16
	data           []byte

	// handshake marks the ACK that completes the handshake (atemwire's
	// "_handshake" label): once it is sent, the session id is forgotten
	// and taken from the next packet the switcher sends.
	handshake bool
}

var (
	errMalformed   = errors.New("atem: malformed datagram")
	errAlreadyOpen = errors.New("atem: trying to open a connection that's already open")
)

// parsePacket mirrors Packet.from_bytes: the length in the header must
// equal the datagram's length.
func parsePacket(b []byte) (packet, error) {
	if len(b) < headerLen {
		return packet{}, fmt.Errorf("%w: %d bytes, shorter than the header", errMalformed, len(b))
	}
	word := binary.BigEndian.Uint16(b[0:])
	p := packet{
		flags:          uint8(word >> 11),
		length:         word & 0x07FF,
		session:        binary.BigEndian.Uint16(b[2:]),
		ack:            binary.BigEndian.Uint16(b[4:]),
		retransmitFrom: binary.BigEndian.Uint16(b[6:]),
		remoteSeq:      binary.BigEndian.Uint16(b[8:]),
		seq:            binary.BigEndian.Uint16(b[10:]),
	}
	if int(p.length) != len(b) {
		return packet{}, fmt.Errorf("%w: %d in header but data length is %d", errMalformed, p.length, len(b))
	}
	p.data = append([]byte(nil), b[headerLen:]...)
	return p, nil
}

// marshal mirrors Packet.to_bytes. Bytes 6-7 carry the sequence a
// retransmission request asks for from, zero on every other packet
// (bmdwire#100, 9824cab: until then atemwire never asked).
func (p *packet) marshal() []byte {
	b := make([]byte, headerLen+len(p.data))
	binary.BigEndian.PutUint16(b[0:], uint16(len(b))+uint16(p.flags)<<11)
	binary.BigEndian.PutUint16(b[2:], p.session)
	binary.BigEndian.PutUint16(b[4:], p.ack)
	binary.BigEndian.PutUint16(b[6:], p.retransmitFrom&seqMask)
	binary.BigEndian.PutUint16(b[8:], p.remoteSeq)
	binary.BigEndian.PutUint16(b[10:], p.seq)
	copy(b[headerLen:], p.data)
	return b
}

// seqHistory is atemwire's received_packets: the last receivedHistory
// sequence numbers received, with O(1) membership (atemwire's _SeenWindow
// since bmdwire#73).
type seqHistory struct {
	ring  [receivedHistory]uint16
	head  int // index of the oldest entry
	size  int
	count map[uint16]int
}

func (h *seqHistory) add(seq uint16) {
	if h.count == nil {
		h.count = make(map[uint16]int)
	}
	if h.size == receivedHistory {
		old := h.ring[h.head]
		if h.count[old]--; h.count[old] == 0 {
			delete(h.count, old)
		}
		h.ring[h.head] = seq
		h.head = (h.head + 1) % receivedHistory
	} else {
		h.ring[(h.head+h.size)%receivedHistory] = seq
		h.size++
	}
	h.count[seq]++
}

func (h *seqHistory) contains(seq uint16) bool { return h.count[seq] > 0 }

func (h *seqHistory) clear() { *h = seqHistory{} }

// transport is atemwire's UdpProtocol for one switcher.
type transport struct {
	conn net.PacketConn
	addr *net.UDPAddr
	log  *slog.Logger
	now  func() time.Time

	state        transportState
	sessionID    uint16
	sessionKnown bool // false where atemwire's session_id is None

	// prevSession is the id of the session before the last connect, while
	// prevKnown. Its packets are dropped unseen (bmdwire#12): after a
	// silence the switcher keeps resending the old session's packets for
	// a while, and they must not reach the new one.
	prevSession uint16
	prevKnown   bool

	localSeq      int    // last sequence sent; -1 right after connect
	remoteSeq     uint16 // sequence of the latest packet received
	ackNumber     uint16 // contiguous high-water mark we ACK
	rxEstablished bool   // the first reliable packet has set the baseline

	deliverNext  uint16 // next sequence owed to the session
	deliverKnown bool
	parked       map[uint16]packet
	// gapDue is when the open gap in the switcher's packets (something
	// parked) is next asked for (gapAskAfter), zero with no gap;
	// gapsAsked counts the requests, for the session's life
	// (_gap_due, gaps_asked; bmdwire#100).
	gapDue    time.Time
	gapsAsked int

	enableACK  bool
	hadTraffic bool
	// refused: the switcher answered the hello 0x03, "no room"
	// (bmdwire#75). The answer is not ACKed; connect clears it.
	refused bool

	// The ACKs received packets are owed (bmdwire#73): while the session
	// drains the datagrams waiting (batching), one cumulative ACK goes
	// after them, and one every ackEvery of a long run; otherwise at
	// once. lastACK is the number last sent this session, -1 for none;
	// an ACK that would say it again goes only for a resend of a packet
	// we have (ackRepeat), whose ACK the switcher evidently missed
	// (bmdwire#80).
	batching  bool
	acksOwed  int
	lastACK   int
	ackRepeat bool

	// markNextConnected is set when InCm (end of the state dump) is stored;
	// the next packet through handle reports ready before it is handled.
	markNextConnected bool

	received      seqHistory
	retransmitBuf map[uint16]*packet

	// bulk is atemwire's send_queue: the data of reliable packets (upload
	// chunks) waiting for queueTrigger. bulkEnabled is queue_enabled: the
	// queue has had packets since it last reported draining.
	bulk        [][]byte
	bulkEnabled bool
	// batch and delay are batch_size and batch_delay; drain is
	// aggressive_drain, which empties the queue on each trigger
	// (setAggressiveDrain).
	batch int
	delay time.Duration
	drain bool

	// lastRX is when a datagram was last accepted from the switcher (the
	// right peer, well formed, not stale), or connect last ran. The
	// silence timeout counts from it. atemwire's select() also wakes on
	// foreign datagrams and on its own sends, so a client sending to a
	// dead switcher never times out there (bmdwire#11); here neither
	// counts.
	lastRX  time.Time
	silence time.Duration // silenceTimeout; tests shorten it
	// lastHeard is when a datagram was last accepted from the switcher,
	// zero before the first; connect does not reset it (atemwire's
	// last_rx_monotonic). Dial's stall limits count from it.
	lastHeard time.Time

	// firstRX is the sequence of the first reliable packet of the session,
	// the one that set the receive baseline (rxEstablished). A packet the
	// switcher sent before it can't be recovered; see bmdwire#13 in
	// docs/PORTING.md.
	firstRX uint16

	retransmitWindow int
	parkWindow       int
	maxBurst         int

	// ackedThrough is the switcher's acknowledgement of our reliable
	// packets (the highest sequence it has, gap-free), -1 until its first
	// ACK; resendDue is when what it hasn't acknowledged is resent, zero
	// while nothing is outstanding (_acked_through, _resend_due).
	ackedThrough int
	resendDue    time.Time

	// The resend timer (bmdwire#80): when each of our reliable packets
	// sent once went (sentAt), the smoothed round trip and its variation
	// (rttKnown once measured), and the doubling for resends nobody
	// acknowledged. dupACKCount counts the same ACK again on packets that
	// carry nothing else. resends and fastResends count both kinds of
	// resend for the session's life, as atemwire's counters do.
	sentAt        map[uint16]time.Time
	srtt, rttvar  time.Duration
	rttKnown      bool
	backoff       int
	dupACKCount   int
	resends       int
	fastResends   int
	resendsLogged int
	resendLogged  time.Time

	// maxChunk is the data chunk size this switcher needs (chunkSteps;
	// 0: the switcher's own). stuckSeq is the first unacknowledged packet
	// the resend timer last fired for, -1 for none, and stuckCount how
	// many times in a row (bmdwire#95).
	maxChunk   int
	stuckSeq   int
	stuckCount int
	// stuckResentAt is when the packet at stuckSeq last went again: a
	// resend counts towards tooBigAfter only if the switcher was heard
	// since (bmdwire#100).
	stuckResentAt time.Time
}

func newTransport(conn net.PacketConn, addr *net.UDPAddr, log *slog.Logger) *transport {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &transport{
		conn:             conn,
		addr:             addr,
		log:              log,
		now:              time.Now,
		state:            stateClosed,
		sessionID:        helloSession,
		sessionKnown:     true,
		parked:           make(map[uint16]packet),
		retransmitBuf:    make(map[uint16]*packet),
		silence:          silenceTimeout,
		retransmitWindow: defaultRetransmitWindow,
		parkWindow:       defaultDeliveryParkWindow,
		maxBurst:         defaultRetransmitMaxBurst,
		batch:            bulkBatch,
		delay:            bulkDelay,
		ackedThrough:     -1,
		sentAt:           make(map[uint16]time.Time),
		backoff:          1,
		lastACK:          -1,
		maxChunk:         chunkLimit(addr),
		stuckSeq:         -1,
	}
}

// setAggressiveDrain is UdpProtocol's aggressive_drain: ten packets a
// batch, 1 ms apart, and every trigger drains the whole queue rather than
// sending one batch per packet from the switcher (bmdwire v1.4.0,
// transport.py; WebATEM's uploader runs on it).
func (t *transport) setAggressiveDrain() {
	t.batch, t.delay, t.drain = drainBatch, drainDelay, true
}

// connect resets the per-session state and sends the SYN. It is
// UdpProtocol.connect, which refuses to run unless the state is closed.
func (t *transport) connect() error {
	if t.state != stateClosed {
		return errAlreadyOpen
	}
	if t.sessionKnown && t.sessionID != helloSession {
		t.prevSession, t.prevKnown = t.sessionID, true
	}
	t.lastRX = t.now()
	t.localSeq = -1
	t.remoteSeq = 0
	t.ackNumber = 0
	t.rxEstablished = false
	t.deliverKnown = false
	clear(t.parked)
	t.gapDue = time.Time{}
	t.sessionID, t.sessionKnown = helloSession, true
	t.enableACK = false
	clear(t.retransmitBuf)
	t.ackedThrough, t.resendDue = -1, time.Time{}
	clear(t.sentAt)
	t.backoff, t.dupACKCount = 1, 0
	t.lastACK, t.ackRepeat, t.acksOwed = -1, false, 0
	t.received.clear()
	t.hadTraffic = false
	t.refused = false
	// markNextConnected is not reset, as in atemwire.

	syn := &packet{flags: flagSYN, data: []byte{helloConnect, 0, 0, 0, 0, 0, 0, 0}}
	if err := t.sendLow(syn); err != nil {
		return err
	}
	t.state = stateSYNSent
	return nil
}

// queuePacket is queue_packet: queue reliable packet data for queueTrigger.
// A full queue drops its oldest packet, loudly, as the bounded deque does.
func (t *transport) queuePacket(data []byte) {
	if len(t.bulk) == bulkQueueMax {
		t.log.Error("atem: bulk send queue overflow, dropping the oldest packet; the transfer is likely corrupt", "max", bulkQueueMax)
		t.bulk = t.bulk[1:]
	}
	t.bulk = append(t.bulk, data)
}

// drainQueue is drain_queue (bmdwire#36): send every queued packet now,
// back to back, as ATEM Software Control sends an upload budget. The
// switcher's FTCD budget is the flow control; it grants the next once it
// has these. queueTrigger reports the drained queue on its next call, as
// for a queue it emptied itself.
func (t *transport) drainQueue() error {
	if len(t.bulk) > 0 {
		t.bulkEnabled = true
	}
	for len(t.bulk) > 0 {
		d := t.bulk[0]
		t.bulk = t.bulk[1:]
		if err := t.sendReliable(d); err != nil {
			return err
		}
	}
	return nil
}

// queueTrigger is queue_trigger without its sleep: send up to a batch
// (bulkBatch, or drainBatch under aggressive drain) of queued packets. flushed is true once, on the first call that finds the
// queue empty after it had packets (atemwire's TransferQueueFlushed). The
// session paces the calls.
func (t *transport) queueTrigger() (sent int, flushed bool, err error) {
	if len(t.bulk) == 0 {
		if t.bulkEnabled {
			t.bulkEnabled = false
			return 0, true, nil
		}
		return 0, false, nil
	}
	t.bulkEnabled = true
	for sent < t.batch && len(t.bulk) > 0 {
		d := t.bulk[0]
		t.bulk = t.bulk[1:]
		if err := t.sendReliable(d); err != nil {
			return sent, false, err
		}
		sent++
	}
	return sent, false, nil
}

// silent reports whether nothing has been accepted from the switcher for
// the silence timeout.
func (t *transport) silent() bool {
	return t.now().Sub(t.lastRX) >= t.silence
}

// timeout is the UDP thread's select() timeout: drop the session and
// handshake again at once. The caller reports a disconnect.
//
// The session being replaced is said goodbye first (bmdwire#80, 1.13.0):
// if the link is back, the switcher ends it at once, its store lock and
// any transfer of it with it, instead of holding them until it reaps the
// session (about 5 s), the new session's lock request queued behind them.
func (t *transport) timeout() error {
	t.log.Error("atem: timeout reading from switcher", "addr", t.addr)
	t.closeSession()
	t.state = stateClosed
	return t.connect()
}

// sendReliable sends data in a reliable, sequenced packet (send_raw).
func (t *transport) sendReliable(data []byte) error {
	return t.sendLow(&packet{flags: flagReliable, data: data})
}

// sendLow is _send_packet_low. A non-nil error is fatal to the session: it
// is what kills atemwire's UDP thread.
func (t *transport) sendLow(p *packet) error {
	if !t.sessionKnown {
		// atemwire's to_bytes fails on a None session id and the packet
		// is dropped; it consumes no sequence number.
		t.log.Error("atem: dropping unserializable packet: session id not known yet", "flags", p.flags)
		return nil
	}
	p.session = t.sessionID
	if p.flags&flagACK == 0 {
		if t.localSeq == -1 {
			t.localSeq = 0
		}
		p.seq = uint16((t.localSeq + 1) % 0x8000)
	}
	raw := p.marshal()
	reliable := p.flags&(flagSYN|flagACK) == 0
	if err := t.write(raw); err != nil {
		if !transientSendError(err) {
			return fmt.Errorf("atem: send: %w", err)
		}
		if !reliable {
			// An ACK goes again with the next packet; a SYN is retried by
			// the silence timeout.
			t.log.Error("atem: transient send error (packet dropped)", "err", err)
			return nil
		}
		// A reliable packet the network refused keeps its sequence number
		// and its copy below, so the switcher finds the gap (or the resend
		// timer fires) and it goes again. Dropped with its number unused,
		// as it was until bmdwire#36, the next packet took the number: no
		// gap, and an upload short of a chunk without a word.
		t.log.Error("atem: send failed, kept for retransmission", "seq", p.seq, "err", err)
	}
	if reliable {
		t.localSeq = (t.localSeq + 1) % 0x8000
		seq := uint16(t.localSeq)
		t.retransmitBuf[seq] = p
		delete(t.retransmitBuf, uint16(mod(t.localSeq-t.retransmitWindow, 0x8000)))
		now := t.now()
		t.sentAt[seq] = now
		if t.resendDue.IsZero() {
			t.resendDue = now.Add(t.resendWait())
		}
	}
	if p.handshake {
		// Forget the temporary id: the switcher's next packet carries
		// the real one.
		t.sessionKnown = false
	}
	return nil
}

// write is _sendto: WriteTo, tried again for a moment while the kernel
// refuses it with ENOBUFS or EAGAIN; any other error, or the last
// refusal, is returned.
func (t *transport) write(raw []byte) error {
	var err error
	for range sendRetries {
		if _, err = t.conn.WriteTo(raw, t.addr); err == nil {
			return nil
		}
		if !errors.Is(err, syscall.ENOBUFS) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

// onACK is _on_ack: the switcher has our reliable packets through n, so
// the resend timer starts again while some remain unacknowledged.
// ackOnly: the packet carried nothing else (no reliable data), so the same
// number again is the switcher saying it has dropped what came after a
// lost packet (bmdwire#80).
func (t *transport) onACK(n uint16, ackOnly bool) {
	if t.ackedThrough == int(n) {
		if !ackOnly {
			return // a data packet repeating it says nothing
		}
		// The switcher, which takes our packets in order only, has had
		// later ones since and dropped them. Twice more and the first one
		// it lacks is resent now, instead of waiting out the timer: once
		// per ACK, and only while something is outstanding.
		t.dupACKCount++
		out := (t.localSeq - int(n)) & seqMask
		if t.dupACKCount == dupACKs && out > 0 && out < t.retransmitWindow && t.state == stateEstablished {
			t.fastResends++
			t.retransmitFrom((n+1)&seqMask, true)
			t.resendDue = t.now().Add(t.resendWait())
		}
		return
	}
	t.dupACKCount = 0
	prev := t.ackedThrough
	t.ackedThrough = int(n)
	out := (t.localSeq - int(n)) & seqMask
	if out >= t.retransmitWindow {
		// An ACK from before a wrap or a reconnect.
		t.resendDue = time.Time{}
		return
	}
	now := t.now()
	if sent, ok := t.sentAt[n]; ok {
		t.sampleRTT(now.Sub(sent))
	}
	if prev < 0 || (int(n)-prev)&seqMask >= t.retransmitWindow {
		for q := range t.sentAt {
			if d := int(q-n) & seqMask; d == 0 || d >= t.retransmitWindow {
				delete(t.sentAt, q)
			}
		}
	} else {
		for q := uint16(prev); q != n; {
			q = (q + 1) & seqMask
			delete(t.sentAt, q)
		}
	}
	t.backoff = 1
	if out == 0 {
		t.resendDue = time.Time{}
	} else {
		t.resendDue = now.Add(t.resendWait())
	}
}

// sampleRTT is _sample_rtt: one round trip measured, smoothed as RFC 6298
// smooths it (1/8, 1/4).
func (t *transport) sampleRTT(r time.Duration) {
	if !t.rttKnown {
		t.srtt, t.rttvar, t.rttKnown = r, r/2, true
		return
	}
	t.rttvar += (absDuration(t.srtt-r) - t.rttvar) / 4
	t.srtt += (r - t.srtt) / 8
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// resendWait is resend_after(): how long a reliable packet of ours waits
// for its ACK before it is sent again.
func (t *transport) resendWait() time.Duration {
	base := resendAfter
	if t.rttKnown {
		base = t.srtt + 4*t.rttvar
	}
	base = min(resendMax, max(resendMin, base))
	return min(resendMax, base*time.Duration(t.backoff))
}

// resendUnacked is _resend_unacked: once resendDue has passed, send again
// what the switcher hasn't acknowledged, from the first unacknowledged
// packet (go back N, maxBurst at most), double the wait, and wait again.
// On a lossy link this happens many times a second, so each is logged at
// debug and a warning sums them up every resendLogEvery. Upload chunks
// that do not reach the switcher come back instead, for the session to
// send again smaller (takeBackChunks).
func (t *transport) resendUnacked() *takenBack {
	if t.resendDue.IsZero() || t.now().Before(t.resendDue) {
		return nil
	}
	t.resendDue = time.Time{}
	if t.state != stateEstablished || t.ackedThrough < 0 {
		return nil
	}
	out := (t.localSeq - t.ackedThrough) & seqMask
	if out == 0 || out >= t.retransmitWindow {
		return nil
	}
	first := uint16((t.ackedThrough + 1) & seqMask)
	if int(first) != t.stuckSeq {
		t.stuckSeq, t.stuckCount = int(first), 1
	} else if t.lastHeard.After(t.stuckResentAt) {
		// Heard since it last went: the link is up (bmdwire#100).
		t.stuckCount++
	}
	t.stuckResentAt = t.now()
	if t.stuckCount > tooBigAfter {
		if tb := t.takeBackChunks(first); tb != nil {
			return tb
		}
	}
	waited := t.resendWait()
	t.log.Debug("atem: no ACK, resending", "from", first, "through", t.localSeq, "after", waited)
	t.resends++
	t.retransmitFrom(first, true)
	if now := t.now(); now.Sub(t.resendLogged) >= resendLogEvery {
		t.log.Warn("atem: resent packets the switcher had not acknowledged", "resends", t.resends-t.resendsLogged, "timer", waited)
		t.resendsLogged, t.resendLogged = t.resends, now
	}
	t.backoff = min(t.backoff*2, 64)
	t.resendDue = t.now().Add(t.resendWait())
	return nil
}

// takeBackChunks is _take_back_chunks (bmdwire#95): the data chunk at
// first was sent tooBigAfter times more and never acknowledged while the
// switcher goes on sending. It and everything after it are taken back:
// their sequence numbers are free, as the switcher takes nothing after a
// packet it lacks. Other packets among them go again now; the chunks
// come back for the session. nil if this is not the case.
func (t *transport) takeBackChunks(first uint16) *takenBack {
	p := t.retransmitBuf[first]
	if p == nil {
		return nil
	}
	transfer, size, ok := ftdaChunk(p.data)
	if !ok {
		return nil
	}
	limit := 0
	for _, s := range chunkSteps {
		if s < size {
			limit = s
			break
		}
	}
	if limit == 0 {
		return nil
	}
	if t.now().Sub(t.lastHeard) > resendMax {
		// The switcher has gone quiet too: the link, not the size.
		return nil
	}
	last := uint16(t.localSeq)
	var taken []*packet
	for seq := first; ; seq = (seq + 1) & seqMask {
		if q := t.retransmitBuf[seq]; q != nil {
			taken = append(taken, q)
		}
		delete(t.retransmitBuf, seq)
		delete(t.sentAt, seq)
		if seq == last {
			break
		}
	}
	t.localSeq = t.ackedThrough
	t.resendDue = time.Time{}
	t.backoff = 1
	t.stuckSeq, t.stuckCount = -1, 0
	t.maxChunk = limit
	setChunkLimit(t.addr, limit)
	tb := &takenBack{transfer: transfer, limit: limit}
	for _, q := range taken {
		if id, n, ok := ftdaChunk(q.data); ok && id == transfer {
			tb.sizes = append(tb.sizes, n)
			continue
		}
		q.flags &^= flagRetransmission
		if err := t.sendLow(q); err != nil {
			t.log.Error("atem: send failed", "err", err)
		}
	}
	t.log.Warn("atem: no data chunk got through, datagrams that big are not reaching the switcher; sending smaller chunks from now on",
		"addr", t.addr, "size", size, "sent", tooBigAfter+1, "chunks", len(tb.sizes), "limit", limit)
	return tb
}

// ackOwed is _ack_owed: a received packet wants acknowledging, now or at
// the end of the drain in progress. repeat: a resend of a packet we have,
// so the switcher missed our ACK and must hear it again even if it says
// nothing new.
func (t *transport) ackOwed(repeat bool) error {
	t.acksOwed++
	if repeat {
		t.ackRepeat = true
	}
	if t.batching {
		return nil
	}
	return t.flushACK()
}

// flushACK is _flush_ack: send the ACK owed, unless it would say what the
// last one said (packets parked behind a gap) and nothing asks for it
// again. A switcher that took the same ACK as a sign of life to hold off
// its resend would never send the packet the gap waits for (bmdwire#80).
func (t *transport) flushACK() error {
	if t.acksOwed == 0 {
		return nil
	}
	t.acksOwed = 0
	if int(t.ackNumber) == t.lastACK && !t.ackRepeat {
		return nil
	}
	t.ackRepeat = false
	t.lastACK = int(t.ackNumber)
	return t.sendLow(&packet{flags: flagACK, ack: t.ackNumber, remoteSeq: ackMagic})
}

// transientSendError is atemwire's TRANSIENT_SEND_ERRNOS: momentary network
// conditions that drop one packet without ending the session.
func transientSendError(err error) bool {
	for _, errno := range []syscall.Errno{
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ECONNREFUSED,
		syscall.ENOBUFS, syscall.EAGAIN, syscall.EPERM, syscall.EINTR,
	} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// receive is _receive_packet_low for one datagram. It returns the packets
// now owed to the session, in sequence order: none (dropped, duplicate or
// parked), the packet itself, or the packet plus the parked ones it
// released. A non-nil error is a fatal send error from the ACK.
func (t *transport) receive(b []byte, from net.Addr) ([]packet, error) {
	if !t.fromSwitcher(from) {
		// The socket is unconnected: anyone on the network can reach it.
		t.log.Debug("atem: ignoring datagram from foreign peer", "from", from)
		return nil, nil
	}
	p, err := parsePacket(b)
	if err != nil {
		t.log.Warn("atem: ignoring malformed datagram", "from", from, "err", err)
		return nil, nil
	}
	if t.stale(p) {
		t.log.Debug("atem: ignoring packet from another session", "session", p.session, "seq", p.seq, "state", t.state)
		return nil, nil
	}
	t.lastRX = t.now()
	t.lastHeard = t.lastRX
	if p.flags&flagACK != 0 {
		t.onACK(p.ack, p.flags&flagReliable == 0)
	}
	if p.flags&flagRequestRetransmission != 0 {
		// The switcher missed one of ours: go back N from the sequence in
		// header bytes 6-7.
		asked := p.retransmitFrom & seqMask
		if t.ackedThrough >= 0 {
			// It asks only for the first it lacks, so it has everything
			// before (bmdwire#100, 9824cab). Its ACKs for those may have
			// been lost (a dropout); taken as acknowledged, nothing it
			// has is sent again or taken back as too big.
			have := (asked - 1) & seqMask
			ahead := int(have-uint16(t.ackedThrough)) & seqMask
			if ahead > 0 && ahead < t.retransmitWindow && (t.localSeq-int(have))&seqMask < t.retransmitWindow {
				t.onACK(have, false)
			}
		}
		t.retransmitFrom(asked, false)
	}
	t.remoteSeq = p.seq
	if !t.sessionKnown {
		t.sessionID, t.sessionKnown = p.session, true
	}
	if p.flags&flagRetransmission != 0 && t.received.contains(t.remoteSeq) {
		// A resend of something we have: our ACK for it hasn't reached
		// the switcher, so say it again (bmdwire#73). Dropped silently,
		// the switcher resent its whole window every ~80 ms until the
		// next new packet happened to carry an ACK (2026-11-04).
		if p.flags&flagReliable != 0 && (t.enableACK || t.state == stateEstablished) {
			if err := t.ackOwed(true); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if p.flags&flagReliable != 0 {
		// Reliable packets only (bmdwire#80). The switcher's ACK-only
		// packets carry sequence number 0, and recorded here they made 0
		// look received for good: when its real packet 0, once per
		// 32,768, was lost, the ACK walked over it and its resend was
		// dropped as a duplicate (2026-11-06).
		t.received.add(t.remoteSeq)
	}
	t.updateACK(p)

	// ACK every reliable packet once established (or once ACKing has
	// started), and the first no-data control frame before that.
	if (p.flags&flagReliable != 0 && (t.enableACK || t.state == stateEstablished)) ||
		(!t.enableACK && len(p.data) == 0) {
		t.enableACK = true
		if err := t.ackOwed(false); err != nil {
			return nil, err
		}
	}

	// The in-order delivery gate: reliable packets after the baseline reach
	// the session strictly in sequence order. Handshake and other
	// unreliable frames pass straight through.
	if p.flags&flagReliable == 0 || !t.rxEstablished {
		return []packet{p}, nil
	}
	seq := p.seq
	if !t.deliverKnown {
		t.deliverNext, t.deliverKnown = (seq+1)&seqMask, true
		return []packet{p}, nil
	}
	dist := (seq - t.deliverNext) & seqMask
	switch {
	case dist == 0:
		t.deliverNext = (seq + 1) & seqMask
		out := []packet{p}
		for {
			q, ok := t.parked[t.deliverNext]
			if !ok {
				break
			}
			delete(t.parked, t.deliverNext)
			out = append(out, q)
			t.deliverNext = (t.deliverNext + 1) & seqMask
		}
		if len(t.parked) > 0 {
			t.gapDue = t.now().Add(gapAskAfter)
		} else {
			t.gapDue = time.Time{}
		}
		return out, nil
	case dist > seqHalf:
		// Behind the cursor: already delivered.
		return nil, nil
	case int(dist) <= t.parkWindow:
		// Ahead of a gap: hold it until the missing packet comes; asked
		// for if it does not soon (gapAskAfter).
		t.parked[seq] = p
		if t.gapDue.IsZero() {
			t.gapDue = t.now().Add(gapAskAfter)
		}
		return nil, nil
	}
	t.log.Error("atem: in-order delivery resync", "cursor", t.deliverNext, "seq", seq, "parked_dropped", len(t.parked))
	clear(t.parked)
	t.gapDue = time.Time{}
	t.deliverNext = (seq + 1) & seqMask
	return []packet{p}, nil
}

// checkGap is _check_gap (bmdwire#100, 9824cab): once the open gap in
// the switcher's packets is due (gapAskAfter), ask for its first missing
// packet, and again every gapAskEvery while it stays open. It returns
// when the gap is next due, zero with none. The request is not one of
// our reliable packets: no sequence number of its own, never buffered
// for a resend, as an ACK.
func (t *transport) checkGap() time.Time {
	if t.gapDue.IsZero() {
		return time.Time{}
	}
	if len(t.parked) == 0 || !t.deliverKnown || !t.sessionKnown {
		t.gapDue = time.Time{}
		return time.Time{}
	}
	now := t.now()
	if !now.Before(t.gapDue) {
		req := &packet{flags: flagRequestRetransmission, session: t.sessionID, retransmitFrom: t.deliverNext}
		if err := t.write(req.marshal()); err != nil {
			t.log.Debug("atem: asking the switcher for a lost packet failed", "seq", t.deliverNext, "err", err)
		}
		t.gapsAsked++
		t.log.Debug("atem: asked the switcher again for a lost packet", "seq", t.deliverNext, "parked", len(t.parked))
		t.gapDue = now.Add(gapAskEvery)
	}
	return t.gapDue
}

// stale reports a packet that belongs to no session of ours, which the
// transport drops before any of its state sees it (bmdwire#12; atemwire
// checks the session id only in _handshake): one carrying the previous
// session's id, or, while the handshake is in flight, anything but the
// switcher's SYN reply under the hello id. After the handshake ACK the
// session id is taken from the next packet that passes, so a stale one
// can't be adopted.
func (t *transport) stale(p packet) bool {
	if t.prevKnown && p.session == t.prevSession {
		return true
	}
	return t.state == stateSYNSent && (p.flags&flagSYN == 0 || p.session != helloSession)
}

// fromSwitcher compares hosts only, as atemwire does (address[0] != ip).
func (t *transport) fromSwitcher(from net.Addr) bool {
	if u, ok := from.(*net.UDPAddr); ok {
		return u.IP.Equal(t.addr.IP)
	}
	host, _, err := net.SplitHostPort(from.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.Equal(t.addr.IP)
}

// updateACK is _update_ack_number: before the first reliable packet the
// ACK mirrors the latest sequence; after it, it advances only through
// sequences received without a gap.
func (t *transport) updateACK(p packet) {
	if !t.rxEstablished {
		t.ackNumber = t.remoteSeq
		if p.flags&flagReliable != 0 {
			t.rxEstablished = true
			t.firstRX = p.seq
		}
		return
	}
	for t.received.contains((t.ackNumber + 1) & seqMask) {
		t.ackNumber = (t.ackNumber + 1) & seqMask
	}
}

// retransmitFrom is _retransmit_from: resend every buffered packet from
// requestSeq through the latest sent, in order, flagged as
// retransmissions, with their original sequence numbers. It writes to the
// socket directly, so resends don't take new sequence numbers. It returns
// the number resent. quiet logs it at debug: the resend timer's and the
// fast resend's, which are routine on a lossy link.
func (t *transport) retransmitFrom(requestSeq uint16, quiet bool) int {
	last := t.localSeq
	span := (last - int(requestSeq)) & seqMask
	if span >= t.retransmitWindow {
		t.log.Error("atem: retransmission request outside the window", "from", requestSeq, "latest", last, "window", t.retransmitWindow)
		return 0
	}
	resent := 0
	for offset := 0; offset < min(span+1, t.maxBurst); offset++ {
		seq := uint16((int(requestSeq) + offset) & seqMask)
		p, ok := t.retransmitBuf[seq]
		if !ok {
			continue
		}
		p.flags |= flagRetransmission
		delete(t.sentAt, seq) // Karn: its ACK may answer either copy
		if err := t.write(p.marshal()); err != nil {
			t.log.Error("atem: retransmit failed", "seq", seq, "err", err)
			break
		}
		resent++
	}
	level := slog.LevelWarn
	if quiet {
		level = slog.LevelDebug
	}
	t.log.Log(context.Background(), level, "atem: retransmission requested", "from", requestSeq, "resent", resent, "through", last)
	return resent
}

// handled is what handle makes of one delivered packet.
type handled struct {
	// ready: the state dump finished before this packet (atemwire's
	// ConnectionReady). The session reports it before using data.
	ready bool
	// data is the packet's command data, for a data packet on an
	// established session; nil otherwise.
	data []byte
	// control: a control frame (no data) on an established session, after
	// which atemwire triggers the bulk queue a second time.
	control bool
}

// handle is the per-packet part of receive_packet: the handshake while
// SYN_SENT, control frames and data once established.
func (t *transport) handle(p packet) (handled, error) {
	var h handled
	if t.markNextConnected {
		// atemwire returns the sentinel first and hands this same packet
		// over on the next call; doing both here keeps that order.
		t.markNextConnected = false
		h.ready = true
	}
	switch t.state {
	case stateSYNSent:
		t.hadTraffic = true
		return h, t.handshake(p)
	case stateEstablished:
		if p.length == headerLen {
			// A control frame. Start ACKing if nothing has yet.
			h.control = true
			if !t.enableACK {
				t.enableACK = true
				ack := &packet{flags: flagACK, ack: t.ackNumber, remoteSeq: ackMagic}
				if err := t.sendLow(ack); err != nil {
					return h, err
				}
			}
			return h, nil
		}
		h.data = p.data
	}
	return h, nil
}

// handshake is _handshake: answer the switcher's SYN with the ACK that
// completes the handshake. Only hello 0x02 (accepted) establishes the
// session. Hello 0x03, "no room", sets refused and is not ACKed
// (bmdwire#75): the switcher's session table is full, and asking again at
// once gets the same answer. atemwire ACKs any other answer and stays in
// SYN_SENT.
func (t *transport) handshake(p packet) error {
	if p.flags&flagSYN == 0 {
		return nil
	}
	if !t.sessionKnown || p.session != t.sessionID {
		return nil
	}
	if len(p.data) == 0 {
		// atemwire raises IndexError here; the packet is lost.
		t.log.Warn("atem: SYN without a hello opcode")
		return nil
	}
	if p.data[0] == helloNoRoom {
		t.refused = true
		t.log.Warn("atem: "+noFreeConnectionMessage, "addr", t.addr)
		return nil
	}
	if p.data[0] == helloAccepted {
		t.state = stateEstablished
	}
	return t.sendLow(&packet{flags: flagACK, handshake: true})
}

// closeSession is the goodbye: one SYN with hello 0x04, fire and forget,
// only on an established session. It releases every lock the session holds.
func (t *transport) closeSession() {
	if t.state != stateEstablished {
		return
	}
	bye := &packet{flags: flagSYN, data: []byte{helloClosing, 0, 0, 0, 0, 0, 0, 0}}
	if err := t.sendLow(bye); err != nil {
		t.log.Debug("atem: goodbye not sent", "err", err)
	}
	t.state = stateClosed
}

// mod is Python's %, which is never negative for a positive modulus.
func mod(a, m int) int {
	return ((a % m) + m) % m
}
