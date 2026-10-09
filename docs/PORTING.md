# Porting bmdwire to Go: decisions

This file is the decision log for the port. Each section says what was
decided and why, so a later session can follow it instead of re-deriving
it. When you make a new decision, add it here in the same commit.

## Source of truth

- The port follows **bmdwire v1.3.0** (`github.com/lucas-romanenko/bmdwire`,
  tag `v1.3.0`, commit `cea58c2`). Cite that tag wherever the port refers
  to bmdwire, in comments and docs alike, so a reader knows which version a
  claim was checked against.
- `docs/SURFACE-atem.md` is atemwire's own inventory, copied verbatim with a
  header naming the tag. It is the specification for `atem/`. Its body says
  it describes 1.2.0; every fix it marks as made after 1.2.0 (§5.4) is in
  v1.3.0, so it describes v1.3.0 apart from those marks.
- Where SURFACE.md and the Python code disagree, the code at v1.3.0 wins,
  as SURFACE.md itself says. Each disagreement found during the port is
  listed under "SURFACE.md and the code" at the end of this file.
- **Mirror, don't redesign.** Behaviour, masks, clamps, units and quirks
  follow atemwire, including its known traps (SURFACE.md §5.4), unless a
  section below says otherwise. A deliberate difference is recorded here
  with its reason; an undocumented difference is a bug.

## Repository rules

- Module `github.com/lucas-romanenko/bmdctl`, Go 1.27.
- Device packages (`atem`, `hyperdeck`, `videohub`, `ultimatte`) use the
  standard library only. `golang.org/x/image` is allowed later for image
  fitting (still upload) and nowhere else.
- One commit per step of the plan, conventional commit messages, pushed
  after every commit. `go build ./... && go vet ./... && go test ./...` is
  green at every commit.
- The hardware rule (see `CLAUDE.md`): nothing contacts a real switcher
  unless built with `-tags hardware`; hardware tests take the target from
  `BMDCTL_TEST_ATEM`, accept only `192.168.81.85` and `192.168.81.86`, are
  read-only unless `BMDCTL_TEST_WRITE=1`, and never change the video mode,
  clear stills, restore a profile or save startup state. This phase has no
  hardware tests: everything runs against `internal/fixtures` or
  `internal/fakeatem`.
- Out of scope until asked: transfers (stills, macros), the macro bytecode
  codec, profiles, a background engine, hosts, HTTP, MCP, and the HyperDeck,
  Videohub and Ultimatte packages beyond their `doc.go`.

## Licensing

- `atem/` is a port of atemwire, which is LGPL-3.0-only (a fork of pyatem),
  so it is LGPL-3.0-only too. `atem/LICENSE` and `atem/LICENSE-gpl3.txt`
  are copied from bmdwire; `atem/NOTICE` gives the lineage. Every file in
  `atem/` starts with both copyright lines and the SPDX identifier.
- `internal/fixtures/` holds bytes and files copied from atemwire's test
  suite, so it carries atemwire's licence as well.
- Everything else (the CLI, the other device packages, `internal/table`,
  `internal/fakeatem`) is MIT, as bmdwire's non-ATEM packages are.

## Fixtures (`internal/fixtures`)

- The fixtures are bmdwire v1.3.0's own test data, copied raw: every packet
  and bytecode literal in `atemwire/tests/*.py`, the profile XML golden and
  the two `build_full_state` snapshots. Where a test builds a byte string
  from an expression, the file holds what the expression evaluates to. The
  README there names the test file and test for each, and whether the test
  calls it a live capture.
- bmdwire's tests contain only three recorded state packets (`MOCP`,
  `AMLP`, `AMBP`); the other tests build payloads with `struct.pack` inside
  the test. Those are not fixtures; the Go tests port them as code.
- The package embeds the directory and exposes `fixtures.Bytes(t, path)`.
  A test fails if any file is empty or missing from the README.
- Fixtures are LGPL-3.0-only, like the tests they came from.

## Transport (`atem/transport.go`)

Mirrors bmdwire v1.3.0 `atemwire/transport.py` (`Packet`, `UdpProtocol`):
UDP 9910; SYN with hello `0x01` under session `0x1337`, the switcher's SYN
answer (`0x02` accepted) ACKed, the session id then taken from the
switcher's next packet; the contiguous ACK high-water mark; dedup of
flagged retransmissions; in-order delivery with up to 512 packets parked
ahead of a gap; go-back-N resends only when the switcher asks (from header
bytes 6-7, at most 128, window 4096); the goodbye (SYN, hello `0x04`, only
when established); 15-bit sequence arithmetic throughout. No client
keepalive and no client retransmit timer, as in atemwire.

- **No thread.** atemwire splits the transport between a UDP thread
  (`_receive_packet_low`, `_send_packet_low`) and the protocol worker
  (`receive_packet`). Here both are methods on one struct called only from
  the session's owner goroutine (step 3), so it has no locks and no queues.
  It never reads the socket: the session hands it each datagram
  (`receive`), then each packet that comes out (`handle`).
- **ACKs go out immediately.** atemwire queues an ACK for its UDP thread to
  send on its next `select()` wake-up; here it is written while the
  datagram is processed. Same ACK number, same order relative to the data.
- **"5 s of silence" means what atemwire's `select()` means:** no datagram
  in from anyone (a foreign or malformed one counts) and no packet out
  through the normal send path, for 5 s. Resends and the goodbye bypass
  that path in atemwire, so they don't count here either. A client that
  keeps sending commands to a dead switcher therefore never times out, as
  in atemwire. On timeout the transport re-SYNs at once and the session
  reports a disconnect.
- **ConnectionReady without a stash.** atemwire marks the dump complete
  when `InCm` is stored, then returns a `ConnectionReady` sentinel for the
  next packet and keeps that packet for its next call (the 2026-09-16
  stash fix). `handle` reports `ready` and the packet's data together; the
  session acts on `ready` first, so the order is the same. As in atemwire,
  `connect` does not clear the mark.
- **Send errors.** `TRANSIENT_SEND_ERRNOS` (ENETUNREACH, EHOSTUNREACH,
  ECONNREFUSED, ENOBUFS, EAGAIN, EPERM, EINTR) drop the packet and the
  session carries on; as in atemwire the dropped packet uses up no sequence
  number. Any other write error is returned and ends the session: it is
  what kills atemwire's UDP thread. On Windows Go's `syscall` errnos are
  invented values, so a Winsock error may be treated as fatal there; the
  caller dials again. A packet sent while the session id is unknown is
  dropped, which is what atemwire's `to_bytes` failure does.
- **Foreign-peer guard** compares IP addresses. atemwire compares the
  sender's address string with the `ip` it was given; the session resolves
  a host name once when dialling, so a name works here.
- **A SYN with no hello byte** is ignored with a warning; atemwire raises
  `IndexError` out of `loop()`, which loses the packet the same way.
- **The SYN** carries sequence number 1 (`local_sequence_number` goes from
  -1 to 0 first) and zero in bytes 8-9, exactly as atemwire sends it.
- **Not ported yet:** the paced bulk send queue (`queue_packet`,
  `queue_trigger`, `batch_size` 5 / 3 ms, `aggressive_drain`, the
  `TransferQueueFlushed` sentinel). Only uploads use it; it arrives with
  transfers. `Wakeup` has no counterpart: the session's owner goroutine
  wakes on channels.
- **Logging.** `ATEMWIRE_PACKET_TRACE` is not read. The transport logs to a
  `*slog.Logger` (discarded by default): per-packet detail at debug,
  recoveries at warn, timeouts and dropped packets at error.
- **Tests** port `test_transport_ack_gap.py`, `test_transport_inorder.py`,
  `test_transport_connect_reset.py`, `test_transport_datagram_guard.py`,
  `test_transport_retransmit.py`, `test_transport_close_session.py` and the
  transport half of `test_lock_delivery.py` one for one against a fake
  `net.PacketConn`, using the `transport/` fixtures where the Python tests
  use those literals. Python tests with no counterpart say why in a comment
  where they would be.

## Session (`atem/session.go`, `atem/protocol.go`)

The session is what bmdwire v1.3.0 spreads over `ATEMConnection` (worker
thread, send queue, readiness flags), `AtemProtocol` (`loop`, `mixerstate`,
events) and `ready.wait_ready`. The pool (`acquire_connection`, ref counts,
the 2 s grace timer) is not part of it; it belongs with hosts, later.

API: `Dial(ctx, addr) (*Session, error)`; `Send(cmd) error`;
`SendAndWait(ctx, cmd) (Change, error)`; `State() State`;
`Subscribe() (<-chan Change, func())`; `Ready() <-chan struct{}`;
`Connected() bool`; `Close() error`. `Dialer{Conn, Logger}` carries the
two settings `Dial` needs a struct for.

- **One owner goroutine, one reader goroutine.** The owner holds the
  transport, the state, the outbox, the waiters and the subscribers, and
  does every socket write; nothing else touches them, so there is no mutex.
  The reader only calls `ReadFrom` and passes each datagram on: a blocking
  read can't share a `select` with the request channel, and polling with
  short read deadlines (atemwire's worker pumps every 10 ms) adds latency
  and wake-ups for nothing. atemwire has two threads per session as well,
  but the split differs: its UDP thread also ACKs and resends; here the
  owner does, so ACKs follow the state the owner has actually processed.
- **Requests are closures on an unbuffered channel.** Once a send to it
  succeeds the owner is running the closure, so a request can't be lost
  between acceptance and execution, and every request sees the state
  between two whole datagrams. `State` returns a deep copy, so callers can't
  race the owner.
- **Dial** is `ATEMConnection.connect` plus `wait_ready`: it returns when
  the dump is complete (`InCm`, then the next packet: atemwire's
  `ConnectionReady`) and the video mode has arrived. Without a context
  deadline it gives up after `CONNECT_TIMEOUT`, 15 s. On failure it says
  goodbye if the handshake got that far and closes the socket. It resolves
  `host` or `host:port` (default 9910) to IPv4 once and binds an
  unconnected ephemeral UDP socket with a 16 MiB receive buffer, as
  atemwire does.
- **Connected and Ready mirror `is_connected` and `is_ready`** (bmdwire
  v1.3.0, `edb5fa0`). `Connected` is true from Dial's return until `Close`
  or a fatal error, and stays true while the transport re-handshakes after
  a silence. `Ready` returns a channel closed at the end of each state dump;
  a disconnect swaps in a new open channel until the next dump completes.
  After `Close` it returns a channel that never closes, matching
  `is_ready == False`; callers wait on it with a context.
- **Events are `Change` values.** atemwire raises `connected`,
  `disconnected`, `change` and `change:<key>[:<index>]` to callbacks on its
  worker thread. Here one channel carries `ChangeState` (a packet stored),
  `ChangeReady` and `ChangeDisconnected`. `Change.Key` is the opcode followed
  by every index of the packet (`PrgI:0`, `KeOn:0:1`), not atemwire's
  pretty key and first index only, so an echo for keyer 1 can't satisfy a
  wait on keyer 0. Packets stored bare or raw have the opcode alone as key.
- **Subscribers never block the owner.** A full subscriber channel (1024)
  is closed and dropped rather than skipped silently or waited on: waiting
  would stall the ACKs and make the switcher resend. Every receiver gets its
  own copy of `Change.Value`.
- **Send** validates and encodes, rejects a command over 1200 bytes (where
  atemwire logs and drops it), queues it and returns; like atemwire's
  `send()` it doesn't wait for the wire. As in atemwire, `Send` works while
  the transport re-handshakes (`is_connected` stays true); such a command
  is most likely lost, as it is in atemwire.
- **Packing and pacing.** atemwire's worker drains its whole queue, packs
  the commands into packets of at most 1200 bytes and sleeps 10 ms between
  packets of one drain. The owner applies the same two rules without
  sleeping: a packet leaves at once if none left in the last 10 ms;
  otherwise it waits for the 10 ms mark and takes everything queued
  meanwhile, up to 1200 bytes. A lone command still goes out immediately.
  The difference: atemwire puts no gap between packets of *different*
  drains, so a caller sending in a tight loop can microburst; here no two
  command packets are ever closer than 10 ms. atemwire skips the spacing
  while a file transfer is running; transfers are a later phase.
- **SendAndWait** has no atemwire counterpart; atemwire's caller subscribes
  to `change:<key>` and waits (§4.4). The waiter is registered and the
  command queued in one owner step, so the echo can't be missed. It fails
  with `ErrNoEcho` for a command without an echo (§2.7) without sending,
  with `ErrNotReady` between a disconnect and the next dump without
  sending (a waiter there would be satisfied by the new dump, which says
  nothing about the command), and with `ErrDisconnected` if the session
  drops while it waits. On context cancellation the waiter is removed. An
  echo means a state packet with that key arrived after the command was
  queued; like atemwire, nothing ties it to this command in particular.
- **TiRq** is sent once per completed dump, straight to the transport and
  not through the outbox, as atemwire's `send_commands` call in `loop()`
  is.
- **Disconnects.** After 5 s of silence the transport re-SYNs and, if any
  data had arrived (`AtemProtocol.connected`), the session empties the
  state, reopens `Ready`, fails waiters and publishes `ChangeDisconnected`.
  A zero-length command header (atemwire's `ConnectionError`) does the
  same, then says goodbye and handshakes again.
- **Bad packets.** A packet whose commands can't be decoded (a truncated
  header, a non-UTF-8 code, and from step 5 a parser error) loses its
  remaining commands and the session carries on: an exception escaping
  atemwire's `loop()` does exactly that. This includes SURFACE.md §5.4
  item 3: an unknown `VidM` mode loses the rest of its packet and Dial
  waits for a video mode that never comes. Fixing it is a deliberate
  change for later, not part of the port.
- **Fatal errors** (a non-transient write error, any read error) end the
  session: `Connected` goes false, subscriber channels close and every
  method returns an error matching `ErrClosed` that also wraps the cause.
  atemwire's `on_died` callback and pool eviction have no counterpart; the
  caller dials again. On Windows an ICMP port-unreachable can surface as a
  read error (WSAECONNRESET) and end the session, as it ends atemwire's UDP
  thread.
- **Close** says goodbye, closes the socket, fails pending waiters with
  `ErrClosed` and closes subscriber channels. `release_locks_now` comes with
  transfers.
- **Not yet:** typed state (step 4) and parsers (step 5), so `State` holds
  every packet raw at this step; `last_run_macro_index` arrives with the
  `MRPr` parser; transfers, locks and the pool later.
