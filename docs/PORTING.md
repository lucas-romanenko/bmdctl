# Porting bmdwire to Go: decisions

This file is the decision log for the port. Each section says what was
decided and why, so a later session can follow it instead of re-deriving
it. When you make a new decision, add it here in the same commit.

## Source of truth

Tracks: bmdwire v1.6.0

- The port follows **bmdwire v1.6.0** (`github.com/lucas-romanenko/bmdwire`,
  tag `v1.6.0`, commit `0eb73e5`): v1.4.0 (tag `v1.4.0`, commit
  `e04920b`), bmdwire#36, #38, #40, #42, #46 and #49. It was written against **v1.3.0** (tag
  `v1.3.0`, commit `cea58c2`), and the code and these notes cite that tag
  wherever they were checked against it; v1.4.0 changed only what
  "bmdwire 1.4.0" below lists, so those citations hold for it. Cite the
  tag a claim was checked against, in comments and docs alike.
- **Fixes merged after v1.3.0** that the port takes too, each a bmdwire
  PR settled by a capture from ATEM Software Control: bmdwire#22
  (`b89fcb7`, merged as `6ef831f`), the fly-key locations ("Commands");
  bmdwire#23 (`d86ed31`, merged as `5404931`), dual-mono Fairlight
  inputs in profiles ("Profiles"); bmdwire#24 (`4011dcb`, merged as
  `785aac7`), macro floats to six significant digits ("Macro codec").
  Each is ported in its own commit with bmdwire's new tests and
  fixtures, and the oracles it changes are regenerated at its merge
  (`internal/fixtures/README.md` says which file is at which commit; the
  rest come out byte for byte the same there). Code that follows one of
  them cites it by PR and commit; v1.3.0 everywhere else means v1.3.0
  with these merged. All three are in v1.4.0.
- **bmdwire 1.4.0** (bmdwire#25 to #32) fixed the bugs this port had
  filed while porting v1.3.0, most of which the port had fixed already
  ("Deliberate differences" says, for each, what bmdwire now does and what
  difference remains). What the port took from it: a dump without a video
  mode re-handshakes for a fresh one (bmdwire#27, "Deliberate differences",
  bmdwire#13), and a save's image capture stops when the session goes,
  returning the slots done so far (bmdwire#31, "Operations", group g). The
  oracles it changes are regenerated at v1.4.0; the HyperDeck client's
  stays at v1.3.0 ("HyperDeck", Verification).
- **bmdwire 1.5.0** is bmdwire#36 (`bd6ae77`, merged as `32833b7`),
  uploads and the lock handover as ATEM Software Control does them, from
  a capture of its restore, and the transport's resend timer and kept
  sequence numbers ("Transport", "Transfers"), ported in `5ec2519` before
  the release. It changes no oracle; the oracles stay at v1.4.0.
- **bmdwire 1.5.1 and 1.5.2** are bmdwire#38 (`5827626`, merged as
  `f73356a`) and #40 (`1b30449`, merged as `4cac9ce`): another client's
  turn. 1.5.0's handover, the release and the next request in one packet,
  cut ATEM Software Control's turn short and left it without thumbnails
  (two captures on the ASC machine); now every release leaves the store
  until it is announced free and a frame more ("Transfers"). Ported
  together after both releases. No oracle changes.
- **bmdwire 1.5.3** is bmdwire#42 (`81955c1`, merged as `bfce193`). Its
  main fix has nothing to port: atemwire's UDP thread queued its own ACKs
  on a send queue only it drains, and an upload budget filling that queue
  stopped both threads for good (WebATEM's restore with ATEM Software
  Control open, the eighteenth run). bmdctl's transport is one goroutine
  writing to the socket, with no queue between threads. Its other fix,
  `_service_yields` running the trigger only once a turn is due and
  otherwise waking for it again, is mirrored in `serviceYields`; the wire
  was already the same, since only the yield timer runs it here, not a
  caller's every Wakeup. No oracle changes.
- **bmdwire 1.5.4** is bmdwire#46 (`c08f830`, merged as `1bf98f0`):
  macOS 12 refuses an `SO_RCVBUF` above `kern.ipc.maxsockbuf` with
  errno 55 instead of capping it, and atemwire's bare 16 MB request
  failed every connect there. Here the error was already discarded, so
  Dial went on with the system's default buffer; it now steps down 16,
  8, 4, 1 MiB as atemwire does ("Session", Dial). No oracle changes.
- **bmdwire 1.5.5** is bmdwire#47 (`1feedbd`, merged as `6bb1c84`):
  `abort_transfers()` cleared the wait for another client's turn, so a
  caller that timed out and retried (WebATEM's media pool, after 15 s)
  could ask in the middle of ATEM Software Control's turn and abort its
  download. Nothing to port: a caller giving up here goes through
  `cancelTask`, which withdraws that one task and leaves the turn alone,
  and a lock it gives back goes through `releaseThenContinue`, which
  starts one. Two tests mirror bmdwire's new ones. No oracle changes.
- **bmdwire 1.6.0** is bmdwire#49 (`02fba68`, merged as `55670cb`): the
  legacy (pre-Fairlight) audio mixer. Ported: an `AMIP` from before ATEM
  8.0 firmware, 20 bytes, parses as the 16 of later firmware do
  (`626c785`, "Parsers"), where it was an error that lost the rest of its
  packet; the five readers it adds (`audio_mixer_type` and the four
  `legacy_audio_*`) and its three unit helpers (`legacy_volume_to_db`,
  `legacy_db_to_volume`, `legacy_pan_to_balance`) ("Readers"); and the
  `legacy-audio:` log lines (`03fa389`, "Session"). Its other half,
  `build_full_state`'s audio section on a legacy mixer (`3e2f400`), waits
  with `build_full_state` ("Readers", Not ported); the readers it is built
  from are here. The CLI takes two things from it: `audio master
  --volume` on the classic mixer goes through `legacy_db_to_volume`
  ("Command table and CLI"), and `show audio` reads the classic mixer
  through the readers ("Operations"). `recv.json` and `readers.json` are
  regenerated at v1.6.0: the first changes only in its two `AMIP` cases
  over 16 bytes, the second gains the new readers, helpers and 13
  scenarios and keeps every earlier case as it was. None of it has met a
  legacy switcher, here or in bmdwire.
- The `Tracks:` line above says the same on one line, for workflows to
  read: `git describe` of the bmdwire commit the port follows, here the
  tag `v1.6.0`. Keep it a line of its own, exactly
  `Tracks: bmdwire <describe>`, and move it in the commit that finishes
  porting a later bmdwire.
- `docs/SURFACE-atem.md` is atemwire's own inventory, copied verbatim with a
  header naming the version: bmdwire's at v1.6.0, whose §5.4 marks each
  fix. It is the specification for `atem/`. Its body says it describes
  1.2.0; every fix it marks as made after 1.2.0 (§5.4) is in what the port
  follows, so it describes that apart from those marks.
- `docs/SURFACE-hyperdeck.md` is the same for `hyperdeck/`, written for
  bmdctl from hyperdeckwire's code at v1.3.0, which has no SURFACE.md.
  `docs/SURFACE-videohub.md` and `docs/SURFACE-ultimatte.md` are the same
  for `videohub/` and `ultimatte/` (phase 5), written from videohubwire's
  and ultimattewire's code at v1.6.0, which have none either; neither
  package's code has changed since 1.0.0 (videohubwire) and 0.2.0.dev0
  (ultimattewire, `5342705`).
- **Drift is reported, not merged.** `.github/workflows/surface-drift.yml`
  compares both files every night, less their header comment, with the
  SURFACE.md bmdwire main has beside each package, and keeps one issue,
  "SURFACE drift from bmdwire", open while they differ: rewritten each
  night, closed once they match. It never edits a SURFACE file; catching
  up is a port. Until hyperdeckwire, videohubwire and ultimattewire have
  `docs/SURFACE.md`, their copies have nothing to compare and the issue
  says so; once one has, make its comparison required in
  `.github/scripts/surface-drift.sh`, as the atemwire one is. Separately, bmdwire's publish workflow opens "Port
  bmdwire v<version>" here, labelled `port`, once a release is on PyPI.
- Where SURFACE.md and the Python code disagree, the code at v1.3.0 wins,
  as SURFACE.md itself says. Each disagreement found during the port is
  listed under "SURFACE.md and the code" at the end of this file.
- **Mirror, don't redesign.** Behaviour, masks, clamps, units and quirks
  follow atemwire, including its known traps (SURFACE.md §5.4), unless a
  section below says otherwise. A deliberate difference is recorded here
  with its reason; an undocumented difference is a bug. Bugs fixed on
  purpose rather than copied are listed under "Deliberate differences",
  each with its bmdwire issue.

## Repository rules

- Module `github.com/lucas-romanenko/bmdctl`, Go 1.27.
- Device packages (`atem`, `hyperdeck`, `videohub`, `ultimatte`) use the
  standard library only. `golang.org/x/image` was allowed for image fitting
  (still upload) and turned out not to be needed: the fit ports Pillow's own
  kernel (see "Still images").
- One commit per step of the plan, conventional commit messages, pushed
  after every commit. `go build ./... && go vet ./... && go test ./...` is
  green at every commit.
- The hardware rule (see `CLAUDE.md`) covers tests and development tooling:
  no test, example, generator or script contacts a real switcher unless
  built with `-tags hardware`; hardware tests take the target from
  `BMDCTL_TEST_ATEM`, accept only `192.168.81.85` and `192.168.81.86`, are
  read-only unless `BMDCTL_TEST_WRITE=1`, and never change the video mode,
  clear stills, restore a profile or save startup state. The shipped
  binary is not covered: it sends to whatever host the user names. There
  are no hardware tests yet: everything runs against `internal/fixtures` or
  `internal/fakeatem`.
- Phase 2 adds transfers (stills, macros), the macro bytecode codec,
  profiles and the display-unit readers. Phase 3 adds the binary: the
  background engine, hosts, every command through the engine, `exec`,
  completion, man pages and `doctor`. Phase 4 adds the HyperDeck package
  and its commands, the device probe, the alias binaries, `serve`, `mcp`,
  the Claude skill and the release. Out of scope until asked (phase 5):
  the Videohub and Ultimatte packages beyond their `doc.go`, and
  hyperdeckwire's FTP upload and configuration API.

## Licensing

- `atem/` is a port of atemwire, which is LGPL-3.0-only (a fork of pyatem),
  so it is LGPL-3.0-only too. `atem/LICENSE` and `atem/LICENSE-gpl3.txt`
  are copied from bmdwire; `atem/NOTICE` gives the lineage. Every file in
  `atem/` starts with both copyright lines and the SPDX identifier.
- `internal/fixtures/` holds bytes and files copied from atemwire's test
  suite, so it carries atemwire's licence as well.
- Everything else (the CLI, the other device packages, `internal/table`,
  `internal/fakeatem`, `internal/fakehyperdeck`, `internal/engine`,
  `internal/hosts`, `internal/mdns`, `internal/paths`) is MIT, as
  bmdwire's non-ATEM packages are. `hyperdeck/` is a port of
  hyperdeckwire, which is MIT; its fixtures and oracle are in
  `hyperdeck/testdata/`, not `internal/fixtures/`, so the LGPL directory
  holds only atemwire's data.

## Fixtures (`internal/fixtures`)

- The fixtures are bmdwire v1.3.0's own test data, copied raw: every packet
  and bytecode literal in `atemwire/tests/*.py`, the profile XML golden and
  the two `build_full_state` snapshots (these as at v1.6.0, which added
  `audio.mixer` to them, bmdwire#49). Where a test builds a byte string
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
keepalive, as in atemwire. Since bmdwire#36 (`bd6ae77`) there is a client
retransmit timer: what the switcher hasn't acknowledged in 250 ms is
resent (below).

- **No thread.** atemwire splits the transport between a UDP thread
  (`_receive_packet_low`, `_send_packet_low`) and the protocol worker
  (`receive_packet`). Here both are methods on one struct called only from
  the session's owner goroutine (step 3), so it has no locks and no queues.
  It never reads the socket: the session hands it each datagram
  (`receive`), then each packet that comes out (`handle`).
- **ACKs go out immediately.** atemwire queues an ACK for its UDP thread to
  send on its next `select()` wake-up; here it is written while the
  datagram is processed. Same ACK number, same order relative to the data.
- **"5 s of silence" is 5 s with no datagram accepted from the switcher**
  (right peer, well formed, not stale), counted from the last one or from
  the last SYN. This is a deliberate difference (bmdwire#11, see
  "Deliberate differences"). On timeout the transport re-SYNs at once and
  the session reports a disconnect.
- **ConnectionReady without a stash.** atemwire marks the dump complete
  when `InCm` is stored, then returns a `ConnectionReady` sentinel for the
  next packet and keeps that packet for its next call (the 2026-09-16
  stash fix). `handle` reports `ready` and the packet's data together; the
  session acts on `ready` first, so the order is the same. As in atemwire,
  `connect` does not clear the mark.
- **Send errors.** ENOBUFS and EAGAIN are tried again, 1 ms apart, up to
  20 times (`write`, atemwire's `_sendto`; the owner goroutine sleeps
  only while the kernel refuses). Then `TRANSIENT_SEND_ERRNOS`
  (ENETUNREACH, EHOSTUNREACH, ECONNREFUSED, ENOBUFS, EAGAIN, EPERM, EINTR)
  don't end the session: an ACK or a SYN is dropped, and a reliable packet
  keeps its sequence number and its copy for retransmission (bmdwire#36).
  Until then it was dropped with its number unused, as in atemwire: the
  next packet took the number, the switcher saw no gap, and an upload lost
  a chunk without a word. Any other write error is returned and ends the
  session: it is what kills atemwire's UDP thread. On Windows Go's `syscall` errnos are
  invented values, so a Winsock error may be treated as fatal there; the
  caller dials again. A packet sent while the session id is unknown is
  dropped, which is what atemwire's `to_bytes` failure does.
- **The resend timer** (bmdwire#36, `RESEND_AFTER`). The switcher's ACK
  number, which is cumulative (the last of our packets it has: ASC's
  restore capture never shows it ahead of the latest sent), is read from
  every packet carrying it (`onACK`). A reliable packet sent with none
  outstanding arms `resendDue`; an ACK that leaves some outstanding moves
  it on, one that leaves none clears it. When it passes, `resendUnacked`
  goes back N from the first unacknowledged packet (at most 128) and
  waits 250 ms again. atemwire checks after each `select()` of its UDP
  thread; here the session's run loop has a timer for it beside the
  silence timer. The switcher asks for a packet it finds missing only when
  a later one arrives, and nothing follows the last chunk of an upload
  budget: its loss stalled the upload until the caller's timeout.
- **Foreign-peer guard** compares IP addresses. atemwire compares the
  sender's address string with the `ip` it was given; the session resolves
  a host name once when dialling, so a name works here.
- **A SYN with no hello byte** is ignored with a warning; atemwire raises
  `IndexError` out of `loop()`, which loses the packet the same way.
- **The SYN** carries sequence number 1 (`local_sequence_number` goes from
  -1 to 0 first) and zero in bytes 8-9, exactly as atemwire sends it.
- **Session ids.** Packets carrying the previous session's id, and while
  the handshake is in flight anything but the SYN reply, are dropped
  before any receive state sees them; a deliberate difference (bmdwire#12).
- **The paced bulk send queue** (`queue_packet`, `queue_trigger`, the
  `TransferQueueFlushed` sentinel) is the transport's `bulk`; uploads use
  it, so it is described under "Transfers". `Wakeup` has no counterpart:
  the session's owner goroutine
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
  unconnected ephemeral UDP socket with a 16 MiB receive buffer, or the
  first of 8, 4 and 1 MiB the system takes (bmdwire#46), as atemwire
  does.
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
  command packets are ever closer than 10 ms. While a transfer is queued or
  running there is no spacing, as atemwire's worker skips it then
  (`_transfer_active`).
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
- **Legacy audio in the log** (bmdwire#49, `03fa389`). The first `AMIP`
  per source and the first `AMMO`, `AMmO` and `AMLv` a session stores are
  logged at Info as `atem: legacy-audio: ` and atemwire's own line
  (`legacyAudioDiagnostic`, `TestLegacyAudioDiagnostic` pins bmdwire's
  text): the code, the payload in hex and every decoded attribute, as
  Python prints them, with `volume_db` and `plug_name`. "First" is per
  `Change.Key` for the session's life, re-handshakes included, as
  atemwire's set lives as long as its `AtemProtocol`. `AMLv`'s inputs are
  printed in the packet's order, read again from the payload, since the
  state's map doesn't keep it. At each ready the mixer the dump showed
  (`AudioMixerType`) is logged, Info for `legacy` or `unknown`, Debug for
  Fairlight, which sends none of the four. atemwire guards the
  description with a `try`; formatting it here can't fail. The engine
  logs at Info, so the lines land in its log file, which is what a user
  with a legacy switcher can send.
- **TiRq** is sent once per completed dump, straight to the transport and
  not through the outbox, as atemwire's `send_commands` call in `loop()`
  is.
- **Disconnects.** After 5 s of silence the transport re-SYNs and, if any
  data had arrived (`AtemProtocol.connected`), the session empties the
  state, reopens `Ready`, fails waiters and publishes `ChangeDisconnected`.
  A zero-length command header (atemwire's `ConnectionError`) does the
  same, then says goodbye and handshakes again. `Ready` is reopened first,
  before any transfer or waiter is failed: atemwire's `'disconnected'`
  handler clears `_session_ready` before `_reset_transfer_lane` runs. The
  port first reopened it after the lane, so a caller told
  `ErrDisconnected` could wait on `Ready`, find the old closed channel
  and get `ErrNotReady` from its next call; CI hit it in
  `TestDisconnectFailsWaitingTransfers` (2026-10-12).
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
- **Close** gives back any store lock held (`release_locks_now`, see
  "Transfers"), says goodbye, closes the socket, fails pending waiters and
  transfers with `ErrClosed` and closes subscriber channels.
- **Not yet:** typed state (step 4) and parsers (step 5), so `State` holds
  every packet raw at this step; `last_run_macro_index` arrives with the
  `MRPr` parser; transfers, locks and the pool later.

## State (`atem/state.go`, `atem/clone.go`)

- **One Go type per state packet**, holding the attributes atemwire's Recv
  class sets, with the same names (Go-cased; the JSON name is atemwire's
  attribute name) and the same values and units. Scaled attributes stay
  scaled as atemwire scales them (`ColV` hue in degrees, `KeDV` border hue
  ÷ 10 and saturation ÷ 1000, meters in dB); everything else is the wire
  integer. Each type's comment names its opcode and its mixerstate key.
- **Names.** A packet type keeps its Recv class name, "Field" suffix
  included (`ProgramBusInputField`), with DVE, EQ, SDI and VU as Go
  initialisms. Commands take the Send class names without "Command"
  (`ProgramInput` for `CPgI`), which is what makes room for both: dropping
  "Field" would collide with about twenty commands (`InputProperties`,
  `VideoMode`, `KeyOnAir`, ...). Grouping types have plain names (`ME`,
  `USK`, `DSK`, `Transition`, `FTB`, `Fairlight`, `Video`, `Clock`, ...).
- **The tree.** `State` groups the packets the way their indices do:
  `MEs[me]` holds program, preview, `Transition`, `FTB`, `_MeC` and
  `USKs[keyer]` (with `Keyframes[keyframe]`); `DSKs[dsk]`, `Aux[aux]`,
  `ColorGenerators`, `MediaPlayers`, `Multiviewers[mv].Windows[window]`
  and `SuperSources[ss].Boxes[box]` are slices that grow to the highest
  index seen. Packets indexed by a u16 or a string are maps: `Inputs`,
  `HyperDecks`, `MediaPool.Files`, `Macros.Properties`, `Audio.Inputs`,
  `Fairlight.Inputs`, `Fairlight.Strips` (keyed by atemwire's strip id,
  `"<source>.<subchannel>"`, each strip holding `FASP`, its EQ bands and
  dynamics), and `CameraControl[dest][cat][param]`. Bare packets are
  single pointers; the last one wins, including `FMLv` and `RTMD`, as in
  atemwire. A nil pointer is a packet that hasn't arrived.
- **Tags.** Every field that holds packets is tagged `atem:"OPCODE"`; a
  test checks that the 85 stored opcodes (92 parsed, less the six the
  transfer engine consumes and `InCm`) each appear exactly once and that
  every JSON name is snake_case.
- **Raw.** `State.Raw` holds what atemwire stores as bytes under the
  4-character code: the 27 known-but-unparsed codes (§5.1), `InCm`, and any
  unknown code. Last packet wins.
- **Consumed packets** (`LKOB`, `FTCD`, `FTDC`, `FTDa`, `FTDE`, `*XFC`)
  have types but no place in the tree, as atemwire never stores them.
- **Locks.** `State.Locks` holds every `LKST`, lock taken or released,
  keyed by store, where atemwire stores only releases, bare: a deliberate
  difference (bmdwire#15).
- **Where Python's type doesn't carry over:** names and descriptions that
  atemwire keeps as `bytes` (`MPfe`, `MPrp`) are Go strings holding the
  same bytes, undecoded (JSON shows invalid UTF-8 as U+FFFD); `None` is a
  nil pointer (`RMSu` disks, `RTMS` time available); tuples are arrays
  (`[4]float64` meters) or `TallyLamps{Program, Preview}`; `CCdP` data is
  `[]any` of bool, int64 or float64; the MD5 in `MPfe` is an `MD5` that
  encodes as hex. `KACC`'s `Y`, `Cb`, `Cr` are `SampleY`, `SampleCb`,
  `SampleCr` (JSON `sample_y`...), because `Y` and the cursor's `y` would
  collide.
- **Methods** ported with the types: `VideoModeField.Label`, `FrameSize`,
  `Pixels` (atemwire's `get_label`, `get_resolution`, `get_pixels`),
  `TimeField.TotalSeconds`, `AudioInputField.PlugName`, and the three
  `get_rgb` methods through a port of `colorsys.hls_to_rgb`, checked
  bit for bit against Python 3.12.
- **Copies.** `deepCopy` (reflection, `clone.go`) copies the tree with
  nothing shared; `State()` and every `Change.Value` go through it. A test
  fills every field of a `State` and checks that the copy shares no
  pointer, slice or map with the original.
- `Macros.LastRun` is `ATEMConnection.last_run_macro_index`: -1 until a
  macro is seen running (see Parsers).

## Parsers (`atem/parse_*.go`, `atem/protocol.go`)

One file per SURFACE.md §2 table: `parse_mixeffect.go` (§2.2),
`parse_keyers.go` (§2.3), `parse_audio.go` (§2.4), `parse_system.go`
(§2.5). Each entry pairs a parse function (atemwire's Recv constructor)
with a store function (where `save_field_data` puts the result);
`State.store` dispatches on the opcode.

- **Lengths behave like Python's `struct`.** A class atemwire unpacks with
  `struct.unpack` must be exactly its size; one it reads with
  `unpack_from` or the declarative fields must be at least as long as its
  furthest field. Anything else is an error, which loses the rest of the
  packet (see the session's "Bad packets"). The few hand-written parsers
  that tolerate short payloads (`FEna`, `KeFS`) do here too, except that
  `KeFS` under two bytes errors, because atemwire's `KEY_FORMAT '>BB'`
  raises there before storing.
- **Strings** are cut at the first NUL and decoded as Python's
  `decode('utf-8', 'replace')` does, one U+FFFD per maximal invalid
  subpart (not per byte, as Go's own helpers would). Names atemwire keeps
  as bytes (`MPfe`, `MPrp`) stay undecoded.
- **Quirks copied on purpose:** an `MPfe` of exactly 23 bytes is an error
  (Python's `struct` raises on a zero-length Pascal string); an unknown
  `VidM` mode, or a `_VMC` bitfield with bit 30 or 31 set, is an error
  (atemwire's `ValueError`, §5.4 item 3); `CCdP` with data type 5 or an
  unknown type yields empty data. All of these are what bmdwire v1.3.0
  does. Two of its quirks are fixed instead (see "Deliberate
  differences"): `*XFC` is dropped, not an error (bmdwire#14), and `LKST`
  is stored taken or released, per store (bmdwire#15).
- **Keys.** `Change.Key` is the opcode and the indices atemwire's
  `KEY_FORMAT` reads, with the strip id in place of the first index for
  every packet that carries one (`FASP`, `AEBP`, `AICP`, `AILP`, `AIXP`
  and also `AMIP`, so `State.Audio.Inputs` is keyed `"<source>.0"`, as
  atemwire's `mixerstate['audio-input']` is).
- **`AMIP` reads its first 16 bytes** (bmdwire#49, `626c785`): the
  switcher sends 20 before ATEM 8.0 firmware, the first 14 the same
  (LibAtem's `AudioMixerInputGetCommand` and its `V8` twin). Until
  v1.6.0 atemwire wanted exactly 16, so an older legacy switcher's every
  `AMIP` lost the rest of its packet, here as there. Under 16 is still an
  error.
- **Consumed packets.** `LKOB`, `FTCD`, `FTDC`, `FTDa` and `FTDE` are
  parsed and handed to the transfer engine, never stored; `State.store`
  alone (the oracle replay) parses and drops them. A lock release (`LKST`)
  goes to the engine first, then is stored.
- **The last-run latch.** Storing an `MRPr` with the running bit set and an
  index other than 0xFFFF sets `State.Macros.LastRun` (bmdwire v1.3.0
  `a9f3646`: slot 0 counts). A disconnect empties the state but keeps the
  latch, as `ATEMConnection.last_run_macro_index` survives one.
- **Floats.** Values atemwire computes in floating point are computed the
  same way (same operations, same order), so they match bit for bit,
  except the classic mixer's meters (`AMLv`), where Go's `math.Log10` and
  CPython's libm `log10` can differ in the last place.
- **Verification.** `internal/fixtures/oracle/recv.json` holds 1,062
  payloads fed to bmdwire v1.6.0's real `save_field_data` and what it did
  with each. `TestParsersMatchAtemwire` replays every case: same error or
  not, same consumed/raw/stored outcome, same key, same attribute values
  (floats to 1e-12), and the value sits in the `State` tree under the
  field tagged with its opcode at the indices the key names. Every parser
  has cases. Where bmdctl differs on purpose (`*XFC`, `LKST`), the test
  rewrites the recorded case to what the port must do (`deliberate`). `parse_test.go` also ports, one for one, the assertions
  bmdwire's tests make about parsed packets, including the three live
  captures in `internal/fixtures/audio/`.


## Commands (`atem/cmd_*.go`, `atem/command.go`, `atem/rate.go`)

One file per SURFACE.md §1 table, 82 command types in all: one per
atemwire Send class. Each implements `Command` (`Opcode`, `Encode`,
`EchoKey`, `Unverified`, `Destructive`) and, except the four that take no
arguments, `Validator`.

- **Names** are the Send class names without "Command" (`ProgramInput`,
  `DkeyGain`, `KeyPropertiesDVE`), so atemwire's code and docs stay a grep
  away. Index fields are named for what they index (`ME`, `Keyer`, `DSK`,
  `Aux`, `Generator`, `Player`, `Multiviewer`, `SuperSource`); everything
  else keeps the Send field's name, Go-cased.
- **Display units in, wire units out.** A field takes the unit of the
  atemwire operation that sets it, and `Encode` applies that operation's
  conversion, clamp and rounding: percent ×10 or ×100 clamped, 0-1 ×10000,
  dB ×100 through `_db_to_wire`, DVE positions clamped to i32 only,
  stinger clip and gain unclamped, and so on, each documented on the
  field. Fields no operation sets (`DVESettings.LogoRate`,
  `KeyPropertiesDVE.BorderBevelEnabled`, `InputProperties.PortType`, the
  legacy audio, SuperSource, lock and transfer commands) take wire units, as
  the Send class does. Rates are frames: `ParseRate` and `DisplayFPS` port
  `parse_rate` and `display_fps` for callers holding "seconds:frames"
  strings, including the silent fallback to one second for a malformed
  string.
- **Rounding is Python's.** `round()` is half to even (`math.RoundToEven`),
  `int()` truncates, NaN and the infinities raise wherever Python's
  `round()` or `int()` would, and the one helper that runs `min`/`max` on
  a float first (`_percent_to_unit`, colour generator saturation and luma)
  keeps Python's argument-order behaviour with NaN (it gives 100%).
- **Optional fields are pointers.** A nil field is a field atemwire's
  operation wasn't given: not written, its mask bit not set. `Set(v)`
  makes the pointer. Required fields are plain values; their Go types
  (uint8, uint16, ...) are the wire types, so the out-of-range values
  struct.pack would reject can't be expressed.
- **One build per command.** Each type's `build` writes the payload
  through a small writer that records the first value that won't fit or
  can't be converted. `Encode` returns the bytes; `Validate` returns that
  error. `Session.Send` calls `Validate` first, so a command atemwire would
  raise on (struct.error, OverflowError, a ValueError for a bad argument
  combination) is refused instead of encoded wrongly. Where atemwire's
  operation silently sends nothing (an empty next-transition selection),
  `Validate` returns an error too: a command value can't mean "send
  nothing".
- **Operations folded in.** Where an operation picks values for the
  caller, the command does what the operation does: `FadeToBlackEnable`
  has no M/E because `set_ftb_disabled` always targets M/E 1 (§5.4 item
  5); `SetTimeOfDay` with a zero time means now; `MediaplayerSelect` and
  `StreamingServiceSet` refuse the argument combinations their
  constructors raise on. Read-modify-write operations (`toggle_usk`,
  `toggle_dsk`, `toggle_transition_*`, `set_transition_rate`) need state
  and are not commands; the CLI can build them from `State` later.
- **Unverified** are the 20 classes atemwire restored from upstream pyatem
  without re-verifying them (`CTPr`, `CTPs`, the four legacy audio, `CCmd`,
  `CMvI`, `CMvP`, `CSBP`, `CSSc`, `CRMS`, `RcTM`, `CRSS`, `STAB`, `StrR`,
  `AiVM`, `SRsv`, `SRcl`, `SToD`) and `CKMs` ("not Wireshark-validated").
  `CXMS` is not: atemwire live-validated it. **Destructive** are
  `VideoMode`, `SaveStartupState`, `ClearStartupState` and `ClearStill`.
- **Echo keys** come from SURFACE.md §2's "written by" column, in the
  parsers' key format (strip ids for Fairlight, `"<source>.0"` for the
  legacy audio input). Commands §2.7 lists as having no state of their own
  (`DCut`, `DAut`, `FtbA`, `DDsA`, `RFlK`, `Capt`, `MSlp`, `SFLN`, `SALN`,
  `SRsv`, `SRcl`, `SToD`, `TiRq`, and the lock and transfer commands)
  return "". `SFKF`'s echo is the stored keyframe (`KKFP`) for A and B and
  the fly state (`KeFS`) for the two keyframes the wire format doesn't
  document; `MAct`'s depends on the action. Echo keys are a Go addition and
  unverified on hardware like everything §4.4 says about confirmation.
- **Fly-key locations are one table** (bmdwire#22, `b89fcb7`).
  `FlyKeyLocationNames` is `helpers.FLYKEY_LOCATION_NAMES`: 0 CentreOfKey
  and 1-9 the 3x3 grid row by row, so 5 is MiddleCentre. It is `RFlK`'s
  direction byte, `KeFS` byte 5 and macro op 0x0056's location, as ASC's
  RFlK, KeFS and recorded macro for each of its ten locations showed.
  `atem/macro` keeps a copy, checked equal by
  `TestFlyKeyLocationNamesMatchAtem` as with the video modes. `usk
  keyframe-run --direction` takes ASC's names, and a number still goes
  through unchecked (WebATEM sends numbers). In v1.3.0 the macro table had
  no 5, so MiddleCentre was refused and a profile apply dropped the op,
  and the op's docstring called 5 the centre and had no 0. ASC sends 0x6E
  in RFlK's padding byte 3; atemwire, and so the port, 0. Pinned by
  `flykey_test.go` in `atem` and `atem/macro`: the ten RFlK payloads by
  name and by byte, the KeFS echoes, and `macro/flykey-10.bin`, the
  80-byte recorded macro, decoding to ASC's names in click order and
  encoding back byte for byte.
- **Narrower than atemwire on purpose:** `HyperdeckSettings` takes only
  dotted-quad IPv4 (atemwire's `inet_aton` also takes `"10.1"`-style
  shorthand); `CameraControl` string data must be a string or `[]byte`
  (atemwire's `bytes(v)` of an integer would send that many zero bytes).
- **Verification.** `internal/fixtures/oracle/send.json` records 5,966 calls
  of bmdwire v1.3.0's operations and Send classes and what each sent,
  raised or declined; `TestCommandsMatchAtemwire` builds the matching Go
  command for each and checks `Encode` byte for byte or that `Validate`
  fails. Every command type has cases. `cmd_test.go` ports the byte
  assertions of bmdwire's own tests one for one.

## Readers (`atem/read_*.go`)

bmdwire v1.3.0's 88 readers, and the five v1.6.0 adds for the legacy
audio mixer (bmdwire#49), 93 in all (the `atemwire.messages.*` functions
whose first parameter is `mx`, SURFACE.md §2.1), are methods on `State`,
one file per atemwire module (`read_switching.go`, `read_transition.go`,
..., `read_audio_legacy.go`), so callers never see wire units. `read_state.go` holds what they share from
`state.py`.

- **Names** are the reader names Go-cased with the package's initialisms
  (`USKDVE`, `DSKState`, `FTBRate`, `FairlightEQBands`, `SDI3GLevel`).
  `video_mode` is `CurrentVideoMode`: `State.VideoMode` is a field. Methods
  sharing a command type's name (`AuxSource`, `ColorGenerator`,
  `HyperdeckSettings`, `TransitionPosition`) are legal Go and stay.
- **Dicts are `View` structs** named for the reader (`USKDVEView`; the
  dynamics blocks are `CompressorView`, `LimiterView`, `ExpanderView`),
  since the bare names are taken by commands and `FairlightStrip`. Fields
  are the dict keys Go-cased (`ThresholdDB`, `AttackMS`, `AIType` for
  `_ai_type`), tagged with the key as JSON name, in atemwire's order.
  Lists are slices, tuples arrays (`VideoResolution` is `[2]int`), dicts
  keyed by id maps (`map[int]` for sources, `map[string]` for strip ids).
- **None is a nil pointer**, in results (`DVEStyle`, `USKFlyKeyframe`, the
  master dynamics, `CurrentVideoMode`, `PlanBindingPush`'s slot, a silenced
  `volume_db`, `FairlightSolo.StripID`) and arguments
  (`FairlightEQBands(nil)` is the master). Where atemwire returns `[]` or
  `{}` the Go value is empty, never nil.
- **Defaults.** A missing packet, or an index the state has no entry for
  (negative or out of range included), gives atemwire's default. Readers
  with a `default=` argument take it as a parameter (`ProductName(def)`,
  `DeviceName`, `SDI3GLevel`, `VideoResolution`); `PlanBindingPush` takes
  `exclude_slots` as variadic ints.
- **Floats** use atemwire's operations in atemwire's order, so they match
  bit for bit. `round(x, n)` is `pyRound`: the exact binary value formatted
  by `strconv` with half-even ties and read back, which is CPython's
  `double_round`; scaling by 10^n first is not. Products feeding a sum are
  wrapped in `float64()` so the compiler can't fuse them (`y_raw`).
  `int()` truncates.
- **Strings.** `decode_name` cuts at the first NUL, decodes like Python's
  `'replace'`, and strips with Python's `str.isspace()`: Go's
  `unicode.IsSpace` plus U+001C-U+001F. `product_name` falls back to its
  default for an empty name, `device_name` doesn't, as in atemwire.
- **Helpers.** `MECount`, `MEKeyerCount` and `DSKCount` (atemwire's private
  `_dsk_count`, exported because iterating DSKs needs it) are methods;
  `display_fps` stays `DisplayFPS(State)` (rate.go); `decode_name` and
  `md5_hex` are unexported (`decodeName`, `md5Hex`).
- **Ordering, where the state can't keep atemwire's.** `fairlight_strips`
  sorts by FAIP type, number and subchannel, stably over first-arrival
  order; the state keeps no arrival order, so `FairlightStrips` breaks ties
  by source, then strip id. The EQ bands are taken in map-key order before
  the stable sort by band index, and the dynamics maps are filled in
  ascending strip-id order; both match atemwire for anything a switcher
  sends (keys equal the packets' own band index and strip id).
- **The legacy mixer** (`read_audio_legacy.go`, bmdwire v1.6.0).
  `AudioMixerType` is `audio_mixer_type`: `fairlight` once `FAMP` has
  arrived, `legacy` once `AMMO` has, `unknown` before. The four readers'
  dicts are `LegacyAudioMasterView`, `LegacyAudioStripView`,
  `LegacyAudioHeadphonesView` and `LegacyAudioSoloView`, in the Fairlight
  views' shapes less what the legacy mixer lacks. `legacy_audio_strips`
  sorts by input type and source, stably over arrival order; `AMIP` is
  keyed by its own source, so no two strips a switcher sends tie, and
  the strip id breaks a tie a hand-built state has. The unit helpers are
  functions: `LegacyVolumeToDB(int) *float64` (nil for silence),
  `LegacyDBToVolume(*float64) uint16` (nil is atemwire's `None`,
  silence) and `LegacyPanToBalance(float64) (int16, error)`, whose error
  is the `ValueError` or `OverflowError` Python's `round()` raises on NaN
  and the infinities. `legacy_volume_to_db` goes through `log10`, the one
  libm function a reader result does: Go's `math.Log10` can differ from
  CPython's in the last place, but rounded to two places it gives the
  same bits for every u16 (`readers.json` carries the SHA-256 of all
  65,536 results, which `TestLegacyVolumeToDBEveryU16` recomputes).
  `legacy_db_to_volume`'s `pow` is floored to an integer, and matches
  over the oracle's 1,990 cases.
- **Not ported:** the read-modify-write operations built on readers
  (`toggle_usk`, `toggle_dsk`, `toggle_transition_*`, `set_transition_rate`)
  and `build_full_state`, with v1.6.0's legacy audio section (`audio.mixer`
  and the legacy shape, which the readers above make); both come later.
- **Verification.** `internal/fixtures/oracle/readers.json`
  (`gen_readers.py`) holds 36 scenarios: 2,336 packets fed to bmdwire
  v1.6.0's `save_field_data`, interleaved with 17,025 reader and helper
  calls and their results. The scenarios cover the empty state, all-0x00,
  all-0xFF and all-0x80 payloads, random 1-4 M/E switchers with partial
  packet sets, split and stereo Fairlight strips, names, HyperDeck
  planning, every video mode, chroma sample values on decimal rounding
  ties, and the legacy mixer (bmdwire's `_legacy_mx` from packets,
  random and filled ones, `AMIP` at both lengths, a `FAMP` beside them,
  and the unit helpers on their anchors, ties, clamps and NaN). The
  legacy scenarios come after the others, so the generator's fixed seed
  gives those the packets it gave them at v1.4.0. An exception is matched
  only by a helper that returns an error (`raised`). Each call uses every index the state has plus out-of-range ones.
  `TestReadersMatchAtemwire` replays them through `State.store` and checks
  values bit for bit, int vs float, None, dict keys and list order. Every
  reader and helper must have cases, and the empty scenario is also run on
  a zero `State{}`. For `fairlight_strips` the test re-sorts atemwire's
  list by the Go tie-break, which only reorders atemwire's ties.
  `read_test.go` ports bmdwire's reader assertions one for one; tests
  whose stand-ins Go's types can't express say why in a comment.

## Still images (`atem/imaging.go`, `atem/fit.go`)

`RGBAToYCbCrA`, `YCbCrAToRGBA` and `RLEEncode` port bmdwire v1.3.0's C
extension (`atemwire/mediaconvertmodule.c`: `rgb_to_atem`, `atem_to_rgb`,
`rle_encode`); `RLEDecode` ports `atemwire/imaging.py`'s pure-Python
`rle_decode`. `FitFrame` reproduces WebATEM's `_prepare_frame`
(atem-web-control v2.0.2, `atem_control/uploader.py`), which atemwire
leaves to its caller. `FrameImage` wraps `YCbCrAToRGBA` in an
`*image.NRGBA`, as profile save's `atem_to_rgb` plus `Image.frombytes`
does.

- **Bit for bit with the C.** Each float32 and float64 step is rounded where
  the C rounds it: float luma and chroma in `rgb_to_atem`; double
  temporaries, double `fmin`/`fmax`, float stores and truncating casts in
  `atem_to_rgb`. Every product that feeds a sum is converted explicitly, so
  Go can't fuse it into an FMA (it may on arm64 and GOAMD64=v3; the
  extension is built without FMA). The `#define`d constants are hex
  literals of the doubles gcc folds operation by operation; Go's exact
  constant folding differs in the last bit for three of them. No 10-bit
  input turns that into a different byte, but the port keeps gcc's values.
- **C quirks kept.** `atem_to_rgb` reads through a signed `char`, so bytes
  0 and 4 sign-extend before the shift (unobservable: such alphas saturate
  to 255 either way). `clamp` holds its value in a signed short, so one
  above 32767 comes back unclamped (unreachable). Each pair's Cb and Cr
  come from its second pixel alone. `rle_encode` compares a word index with
  a byte length, so a repeated word always continues the run and a run at
  the end is written by the flush after the loop. Width and height, unused
  by the C, are not parameters.
- **Errors where Python raises.** A length that isn't a multiple of 8, and
  the reserved word `FEFEFEFEFEFEFEFE` in `RLEEncode`, error with the C's
  messages after `atem: `. In `RLEDecode`: a ragged tail made only of 0xFE
  bytes (`IndexError`), a header without a whole count (`struct.error`),
  and a run too long to hold (`OverflowError`). A run whose word is cut
  short repeats the short word, and a ragged tail that isn't all 0xFE is
  copied, as in atemwire.
- **Deliberate differences.** `RLEDecode` refuses a run that would take its
  output past 256 MiB (twice an 8K frame) rather than try to allocate it;
  atemwire raises `MemoryError` only if the allocation fails, or is killed.
  `FrameImage` requires exactly width×height×4 bytes, where `frombytes`
  ignores surplus.
- **FitFrame** follows `_prepare_frame` from the pixels on. The scale and
  the `max(1, int(...))` size are Python's float64. Pillow 10.2.0's
  `resize(LANCZOS)` for RGBA is a copy when the size is unchanged;
  otherwise it converts RGBA to RGBa, runs `ImagingResampleInner`
  (`precompute_coeffs`, 22-bit `normalize_coeffs_8bpc`, a horizontal pass
  over the rows used and then a vertical pass, `clip8`), and converts RGBa
  back to RGBA. Then `paste(scaled, offset, scaled)` blends onto (0,0,0,0)
  with `paste_mask_RGBA`'s `BLEND`/`DIV255`, alpha channel included, so
  colour lands premultiplied and alpha as a²/255. That is what WebATEM
  uploads; it is reproduced, not corrected. Pillow is HPND (MIT-CMU), and
  the code says so. `golang.org/x/image` is not used: an exact port of
  Pillow's kernel is the point.
- **Not mirrored.** File decoding (Pillow's `open`/`convert('RGBA')`) is the
  caller's job. An `*image.NRGBA` is read as is; anything else goes through
  `color.NRGBAModel`, which may round differently from Pillow. The `.png`
  premultiply choice is the caller's (`StillOptions.Premultiply`). An empty
  source gives a transparent frame (Python raises `ZeroDivisionError`); a
  frame under 1×1 panics. The filter uses Go's `math.Sin`: for 7,214 size
  pairs (1-6,000 px into 1-1,920) every 22-bit weight equals the glibc-`sin`
  one, but a last-bit difference could in principle move a weight by one.
- **Verification.** `internal/fixtures/oracle/imaging.json` comes from
  `gen_imaging.py`. It compiles `mediaconvertmodule.c` unmodified against a
  stand-in `Python.h` with Python's own CFLAGS (this machine has no Python
  headers) and calls it through bmdwire's `imaging.py`; `rle_decode` is
  bmdwire's; the fits run `_prepare_frame`'s lines on Pillow 10.2.0. Its 700
  cases cover every 24-bit colour in both pixel slots with every alpha
  (non-premultiplied `rgb_to_atem` exhaustively), every (Y, Cb) and (Y, Cr)
  pair, 48 pixels a fused multiply-add would change (found by a C search),
  RLE runs, errors and malformed streams, and 219 fits up to 1920×1080.
  `TestImagingMatchesAtemwire` and `TestFitFrameMatchesWebATEM` replay them
  all; `-short` skips the colour sweeps (every 24-bit colour, the
  65,536-pair channel sweeps and the Y'CbCr pairs), which take most of
  the time. `imaging_test.go` ports `test_imaging_alpha.py` and
  `test_mediaconvert_hardening.py` one for one; `test_input_buffers_are_released`
  has no counterpart, because Go has no `Py_buffer`.
- **Verified on hardware** (the third run, docs/HARDWARE.md): `FitFrame`,
  premultiplied `RGBAToYCbCrA`, `RLEEncode`, `RLEDecode` and
  `YCbCrAToRGBA`. A 1280×640 PNG was uploaded to a Constellation at
  1080p25 and downloaded again. The switcher's MD5 equalled the frame's,
  and the downloaded RGBA equals the same steps run locally, byte for
  byte. Not yet seen on a switcher: conversion without premultiply, and
  a fit that shrinks the image or adds bars at the sides.

## Transfers (`atem/transfer.go`)

The transfer engine (SURFACE.md §3.0), from bmdwire v1.3.0: the lane of
`protocol.py` (`transfer_queue`, `transfer`, `transfer_requested`,
`locks`, `_lock_requested`, `transfer_budget`,
`_transfer_trigger`, `_release_then_continue`, `_queue_chunks`,
`_queue_flushed`, and the LKOB, lock-state, FTCD, FTDa, FTDE and FTDC
branches of `save_field_data`), `_transfer.py` (`TransferTask`), the bulk
send queue of `transport.py`, and the callers that wait on it:
`connection.py` (`download_still`, `clear_still`, `download_macro`),
`macrotransfer/_io.py` (`upload_macro_bytecode`) and WebATEM's
`atem_control/uploader.py` (still upload).

API: `DownloadStill(ctx, slot) ([]byte, error)`, `UploadStill(ctx, slot,
img, StillOptions)`, `UploadStillFrame(ctx, slot, frame, name,
description)`, `EncodeStill(frame)` and `UploadEncodedStill(ctx, slot,
EncodedStill, name, description)` (UploadStillFrame in two halves, so a
caller can RLE-encode and hash the next still while one uploads, as
atemwire's `upload` takes RLE data with `compressed=True`),
`ClearStill(ctx, slot)`, `DownloadMacro(ctx, slot)`,
`UploadMacro(ctx, slot, name, description, bytecode)`, `LockHolder(store)`;
`*TransferError`, `ErrLockNotGranted`.

- **On the owner goroutine.** The lane is session state like the rest,
  touched only by the run goroutine; a method queues a task there and waits
  on the task's channel. atemwire's lane is driven by the worker (packets)
  and by the caller's thread (`download`, `upload`, `queue_clear`) at once,
  which SURFACE §4.5 notes isn't thread safe; here nothing can race it.
- **The state machine is atemwire's, branch for branch**:
  - per-store queues, tried in the order they were created (Python's dict
    order); one transfer in flight; transfer ids from 43 (42, incremented
    before use), carried across reconnects;
  - PLCK for a transfer, a whole-store LOCK for a clear; a lock request not
    repeated within a second (`LOCK_REREQUEST_SECONDS`); on a release with
    work queued and the store not ours, an immediate re-request (the
    pounce);
  - the lock released after every frame, alone, and another client's turn
    after it (bmdwire#38, #40; `yielding`, `foreignHeld`, the lane's
    `yieldTimer`): the next request, queued then or later, waits until the
    store is announced free (LKST) and `lockCourtesy` (60 ms) more; a
    store another client is seen holding isn't asked for until announced
    free; with no announcement (the store handed to a client that was
    waiting, which the switcher doesn't announce) it waits while anyone is
    seen at work, another session's FTDC or any LKST, and asks after
    `lockQuiet` (10 s) of quiet. Any client's announced release starts the
    same frame's wait, so a release and a lock announced together (ASC
    taking the store again for its next still) cancel the request.
    bmdwire#36 sent the release and the next request in one packet, as
    ASC does: the switcher granted that request beside a waiting client's
    and aborted its transfer with FTDE 6. Our hold runs from the LKOB to
    our release: a release announced while we hold the store is our own,
    late, and changes nothing (`onLockReleased`). Until bmdwire#36 the
    next request waited for its release's echo (`_lock_release_pending`),
    which also waited out a client handed the store: the fifteenth
    hardware run's pauses of 2 to 6.6 s were WebATEM on the dev stack
    refreshing each new still's thumbnail. `releasing` keeps what that
    set told `LockHolder`, a release of ours not yet announced. The
    lock-exempt macro store goes on at once; with nothing queued, every
    held lock is released;
  - FTDE 1 retries with the same transfer id, 5 asks for the lock again, 6
    gives the lock back and keeps the task; any other status drops the task
    and releases the lock; FTDa, FTDE and FTDC with no transfer in flight or
    another transfer id are ignored;
  - FTDC pops the queue head and releases the lock before the RLE decode;
    each downloaded chunk is acknowledged with FTUA(tid, 0);
  - FTCD's size is rounded down to whole words and its transfer id isn't
    checked; a chunk never ends on an RLE header or a header and its count;
    an empty upload sends FTFD with FTSD (no budget comes); FTSD's mode is 1
    (write RLE) for stills and 0x0300 (write, pre-erase) for macros;
  - since bmdwire#36, FTFD goes with the first budget of each attempt,
    before the data (`metadataSent`), each budget is sent whole as it is
    granted (`drainQueue`), and chunks are cut at an offset reset on each
    attempt, so a retried upload starts from the beginning. ASC's restore
    of 20 1080p stills took 12 s; bmdwire's code after the change, 12.7 s
    on the same switcher.
- **Results go to the task, not to events.** atemwire raises
  `download-done`, `upload-done`, `clear-dispatched` and
  `file-transfer-error`; its callers filter by store and slot, attribute any
  fatal FTDE to whatever they wait on (§5.4 item 13) and are serialised per
  connection (`_transfer_serial_lock`) to make that safe. Here the engine
  finishes the task it is acting on, so a result can't reach the wrong
  caller and callers can overlap. A fatal status is a `*TransferError`
  naming the status as `FileTransferErrorField.__repr__` does. The progress
  events (`transfer-progress`, `upload-progress`) are ported (the
  terminal pass, for a progress bar) without a callback on the owner
  goroutine: a transfer under `WithProgress(ctx, f)` gets a channel
  holding the latest `Progress{Done, Total}`, which the owner replaces
  and never waits on, and `runTransfer` calls `f` on the caller's
  goroutine. As atemwire counts it: an upload, the bytes queued of all
  it sends (compressed, for a still; atemwire counts each as its UDP
  thread sends it); a still download, the bytes received of the frame's
  uncompressed size, so it may end short (atemwire every 20 chunks,
  here every chunk); a macro download none.
- **Cancellation withdraws one task.** When their timeout expires,
  atemwire's callers call `abort_transfers`, which empties the whole lane,
  safe there only because transfers are serialised. Here the end of a
  task's context withdraws that task alone: a queued one is dequeued
  (`dequeue_clear`, for every kind of task) and the lane triggered, so a
  lock nobody needs is released; the one in flight is abandoned as a fatal
  FTDE abandons it. A clear already sent isn't withdrawn and its caller
  succeeds, as `clear_still`'s does. A timeout with the store lock never
  granted wraps `ErrLockNotGranted`: `download_still`'s "another client is
  holding it". `abort_transfers` itself has no counterpart.
- **Default timeouts** when the context has no deadline: 30 s still
  download, 5 s clear, 10 s macro download and upload (atemwire's callers),
  90 s still upload (WebATEM's `UPLOAD_TIMEOUT`).
- **Not ready means refused.** A transfer asked for between a disconnect and
  the end of the next state dump fails with `ErrNotReady` before anything
  is sent; atemwire v1.3.0 checks only `is_connected` and queues into a
  session that is re-handshaking. bmdwire 1.4.0 checks `is_ready` too
  (bmdwire#31).
- **The bulk queue** is the transport's `bulk` (`send_queue`). Upload chunks
  wait there, but since bmdwire#36 only until `queueChunks` drains the
  budget (`drainQueue`), so the triggers below only report the drain.
  Each packet from the switcher once ACKs flow, and a control
  frame a second time, sends up to five (`batch_size`), as `receive_packet`
  calls `queue_trigger`; a drained queue is reported once and handled
  before that packet's data, as the `TransferQueueFlushed` sentinel is;
  `_queue_chunks` triggers too and drops the report, as atemwire does.
  atemwire sleeps 3 ms after each batch on its protocol thread; the owner
  goroutine can't sleep (it sends the ACKs), so a trigger within 3 ms of the
  last batch is deferred to the 3 ms mark instead. The queue's 1024 cap
  drops the oldest packet with an error, as the deque does, and the
  queue outlives a disconnect, as atemwire's does.
- **Aggressive drain** (`aggressive_drain`, `AtemProtocol`'s constructor
  flag) is `Dialer.AggressiveDrain`, off by default as in bmdwire: ten
  packets a batch, 1 ms apart, and a trigger drains the whole queue, so
  upload chunks go out as fast as the switcher's budgets (`FTCD`) allow
  rather than five per packet it sends. atemwire drains in one
  `queue_trigger`, sleeping between batches; here the batches after the
  first wait on the deferred-trigger timer (`armBulkTimer`), as the owner
  goroutine can't sleep. It was left out at first (only the
  constructor sets it, and bmdwire's pool, its shared connections,
  doesn't). The engine dials with it, as WebATEM's uploader opens its
  connections: on TEST ATEM 2 (docs/HARDWARE.md, fourteenth run) a
  photographic 1080p still took 23.3 s to upload ACK-paced, ASC about
  2 s, and with aggressive drain 1.8 s (the data 0.96 s). The switcher
  answers about fifty times a second during an upload, so five chunks
  per answer is about 260 chunks a second; WebATEM's notes say the same,
  ~25 s against ~3 s. bmdwire's pool keeps it off because its drain
  sleeps the thread that reads the switcher's packets; nothing here
  sleeps, and engine commands bypass the queue, so a command sent during
  an upload doesn't wait behind it. Since bmdwire#36 every budget is
  drained whole as it is queued, faster still (a still's data in about a
  third of a second, as ASC sends it), so the flag changes nothing: the
  engine and the CLI's bench no longer set it, and it stays as bmdwire
  keeps its own.
- **Engine commands bypass the outbox**, as `send_commands` bypasses the
  worker's queue: one reliable packet per call, straight to the transport.
- **Teardown.** `Close` writes the release of every still-store lock held
  straight to the socket before the goodbye (`release_locks_now`); a
  disconnect resets the lane without sending anything
  (`_reset_transfer_lane`) and fails waiting transfers with
  `ErrDisconnected`.
- **A frame that doesn't decode is an error.** atemwire logs the exception
  `rle_decode` raises in the FTDC handler and its caller times out; here the
  download fails with it. The lock is released first either way.
- **Locks.** `LockHolder(store)` combines the lane's lock table with
  `State.Locks` (bmdwire#15): the session holds a lock from its LKOB until
  its release's echo, and a lock reported taken otherwise is another
  client's. LKST doesn't name the holder, so if the broadcast for our own
  grant came before the LKOB it would read as another client's until then.
- **Stills.** `UploadStill` is WebATEM's `_prepare_frame` and upload:
  `FitFrame`, `RGBAToYCbCrA` (premultiplied if asked; WebATEM premultiplies
  `.png` files only), then `UploadStillFrame`, which is `AtemProtocol.upload`
  with `compress=True`: MD5 over the frame, `RLEEncode`, FTSD with the
  uncompressed length, FTFD with the name. The frame size is the session's
  video mode's; decoding an image file is the caller's job. WebATEM's wait
  for a slot to go off air and its MPfe hash check after the upload are not
  ported. `ClearStill` returns when the CSTL is sent, not when MPfe
  confirms it, as `clear_still` does. Slots are ints checked against the
  wire: u16, or u8 for `ClearStill` (CSTL). Still upload and download are
  verified on hardware (the third run, docs/HARDWARE.md). The upload's
  name and MD5 came back in the slot's MPfe. The clear is verified too:
  `media clear`'s wait for that MPfe was confirmed in 14 ms. Macro
  download is verified on hardware too (the fourth run: 712 bytes in
  109 ms). Macro upload is not yet.
- **Tests.** `transfer_test.go` drives the lane directly, as bmdwire's tests
  drive `AtemProtocol`, porting the engine tests of `test_lock_delivery.py`,
  `test_download_still.py`, `test_session_hygiene_atemwire.py` and
  `test_teardown_releases_locks.py` one for one, plus the bulk queue's
  pacing, chunking and flush. Tests of event-handler bookkeeping
  (`test_returns_frame_and_unregisters_handlers`,
  `test_other_slots_event_does_not_satisfy` and its clear twin,
  `test_raise_*`) and of `abort_transfers` have no counterpart: there are no
  handlers, and cancellation is per task. `upload_like_asc_test.go` ports
  `test_upload_like_asc.py` (bmdwire#36): the handover packet for uploads,
  downloads and a clear, a late release announcement, FTFD with the first
  budget, a retried upload, refused sends and the resend timer.
  `transfer_fake_test.go` runs transfers end to end against the fake
  switcher (among them uploads with lock changes announced a frame late,
  which stall with the old lock-state handler): still and macro round
  trips over several budgets, `UploadStill`'s fit and conversion, a locked
  clear, FTDE 1, 5 and 6 recovered and 2 fatal, a lock held by another
  client (a timeout that names it, a withdrawn clear that never runs, a
  waiting transfer that runs on its release), `*XFC` with a transfer queued,
  a disconnect failing waiting transfers, and a command echoed while a
  download is in flight.

## Macro codec (`atem/macro`)

Ports bmdwire v1.3.0 `atemwire/macrotransfer` (`__init__.py`,
`_helpers.py` and the per-feature codecs; `_io.py` belongs to transfers):
the 146 `_KNOWN_OPS` codes, one Go file per Python module. API: `Op{ID,
Params []Param}`, `Decode`, `EncodeOp`, `Encode`, `*Error{Class, Message}`.

- **Its own package**, importing nothing from `atem`, so `atem`'s transfer
  code and the profile package can both import it. It keeps a copy of
  `VIDEO_MODE_NAMES` and `VIDEO_MODE_XML_NAMES`; a test checks the first
  equals `atem.VideoModeNames`.
- **Ops are ordered.** `Params` holds atemwire's dict order, `id` excluded,
  because profile XML writes attributes in that order. `Get` returns the
  last match, as `dict(pairs)` would.
- **Python semantics, ported not approximated.** Attribute strings go
  through ports of CPython 3.12's `int()` (whitespace, sign, underscores,
  Unicode digits, the 4300-digit limit, `0x` prefix in base 16),
  `float()`, `round()` (half to even, exact big ints), `str.strip()`
  (`\x1c`-`\x1f` count as whitespace), `bytes([...])`, `bytes.fromhex`,
  `struct.pack`/`unpack_from` and glibc `inet_aton`, with exact
  arbitrary-precision masking. Encoders raise (panic, recovered in
  `EncodeOp`/`Decode`) in atemwire's evaluation order, so the first error is
  atemwire's.
- **Errors carry the Python class.** `EncodeOp` returns `*Error` wherever
  `encode_single_op` raises: `ValueError`, `struct.error`, `OSError` or
  `OverflowError`, with CPython's message. Profile apply drops `ValueError`
  ops and aborts on the rest (SURFACE §5.4 item 8); the class lets the
  profile port do the same. A decoder failure becomes `Unknown_0xNNNN` with
  `rawParams` and `decodeError` (CPython's message), as in atemwire.
- **Decode never fails outright.** Where atemwire logs a warning and returns
  the ops so far (truncated header, op_len < 4, overrun), `Decode` returns
  the same ops and an error with the warning text.
- **Quirks kept** (SURFACE §5.4 item 9 and more): unknown style/key-type
  strings encode as Mix/Luma and unknown mix type as On; mask 0 decodes as
  Background; u16/u32 values and several index bytes are masked silently
  while others raise; a missing `MacroSleep.frames` is
  0; marker bytes are rewritten; strip/master fader gain and HyperDeckInput
  overflow raise `struct.error`; HyperDeck addresses accept whatever glibc
  `inet_aton` accepts. `sourceId` decodes unsigned (`18446744073709486336`
  for stereo), the form bmdwire says ATEM Software Control writes
  (`_resolve_source_id`'s docstring, `docs/PROFILE_FORMAT.md`), and the
  encoder takes the signed form too.
- **Floats are written as ATEM Software Control writes them**
  (bmdwire#24, `4011dcb`): `fmtScalar` is `_fmt_scalar`'s six
  significant digits (`%.6g`), so Q16.16 -21474836 is `-327.68` (the
  silenced fader), -5243 `-0.0800018`, 471859 `7.2`, and 1
  `1.52588e-05`. Every float in the macros of twelve ASC exports from
  one Constellation HD has this form; v1.3.0's six decimals missed 50 of
  their 399 distinct strings. It is one rule for every value and
  attribute, so the port's special case for the silenced gain
  (bmdwire#17, a deliberate difference until now) is gone. Above 10 it
  keeps fewer decimals than Q16.16 resolves, so a decode and re-encode
  can move a value by a few 1/65536 steps, as saving and restoring
  through ASC does (the live -1048577 reads `-16` and encodes as
  -1048576). Go's `FormatFloat(v, 'g', 6, 64)` rounds the exact binary
  value half to even as Python does: checked against Python on every
  Q16.16 value in ±2^20, a million random ones and 181,076 exact ties at
  the seventh digit, and the oracle pins ties, the switch to an exponent
  and a carry. `floatformat_test.go` ports `test_macro_float_format.py`:
  the 98 ops of `profile/macro_floats_asc.xml` decode back to ASC's
  strings (v1.3.0's formatting gets 62 of their 98 values wrong), and
  the recorded silenced and -0.08 faders round-trip with their bytes.
- **One fix** (see "Deliberate differences"): a Fairlight strip op with
  no `sourceId` gets the stereo one.
- **Not matched:** Unicode classification uses Go's tables (17.0) where
  CPython 3.12 uses 15.0, so characters assigned since differ as digits and
  in `repr()` escapes; Go strings that aren't valid UTF-8 act as U+FFFD;
  messages follow CPython 3.12's wording. The per-op "decode failure"
  warning is not logged (the op's decodeError holds it).
- **Verification.** `internal/fixtures/oracle/macro.json` (`gen_macro.py`,
  CPython 3.12.3, bmdwire `v1.3.0-9-g785aac7`) holds 2,128 bytecode cases
  (the 33 `macro/` samples, random/short/edge parameter blocks for every op
  code and unknown ones, multi-op streams) and 4,273 op dicts from
  attribute pools.
  `TestDecodeMatchesAtemwire` and `TestEncodeMatchesAtemwire` require the
  same ops (ids, names, order, values), stop warnings, bytes, and error
  class and message (bmdwire's bytes for a strip op with no `sourceId`
  are first rewritten to the stereo default), and assert every op code has decode,
  decode-failure, encode and encode-failure cases. `decoder_test.go` and
  `encoder_test.go` port `test_macro_decoder.py`, `test_macro_encoder.py`
  and the macro halves of `test_wipe_pattern_names.py` and
  `test_input_video_migration.py` one for one, on the `macro/` fixtures.
- **On hardware** (the fourth run, docs/HARDWARE.md): the 712-byte macro
  downloaded from a Constellation decodes to 52 ops, Fairlight ones
  included, and they encode back to the same bytes.

## Profiles (`atem/profile`)

Ports bmdwire v1.3.0 `atemwire/profile` (SURFACE.md §3.3).
`Save(ctx, Source, *SaveOptions)`, `FromXML`/`FromFile`,
`(*Profile).ToXML`/`Apply(ctx, Target, *ApplyOptions)`; `Source` is
`State`+`DownloadMacro`, `Target` is `State`+`Send`+`UploadMacro`, and
`*atem.Session` is both (compile-time check). Options and `ApplyResult`
mirror options.py field for field; constructors give atemwire's defaults
and a zero value selects nothing.

- **Save reads the display readers** where save.py calls one, and the raw
  state where it reads mixerstate directly. There is no
  `wait_state_settled`: a Session's state is complete once Dial returns.
  Macros download one at a time within `MacroTimeout` (10 s). A failed
  download leaves the macro with its name and description only. A decode
  warning is logged and the ops before it are kept. Save fails only when
  ctx ends.
- **Fairlight strips are saved and applied per half** (bmdwire#23,
  `d86ed31`). An `<AudioSource id>` is the strip's wire address read as a
  signed i64, as ASC's export of an input in Dual Mono showed: -65280 a
  stereo strip (channel -1), -256 split channel 0 (a dual-mono input's
  left half, and the XLR's one strip), -255 split channel 1. Save writes
  `DualMono` when an input reports two split strips (FASP `is_split`
  0xFF), with each half's own id, left first: a source's strips go in the
  subchannel order of their ids, as atemwire now sorts them, so the
  port's stand-in for FASP arrival order is gone. A mono input's strip
  (the XLR's) keeps -256 however its FASP reports it; FAIP byte 10 for a
  dual-mono input wasn't captured, so save doesn't read it for that. Apply
  sends each strip to the channel its id names (`audioSourceChannel`:
  `int()` of the id, Python's spellings and the unsigned form a macro's
  `sourceId` holds included, masked to 64 bits; anything else is stereo,
  as before), and an error names the half (`fairlight.strip[1.1]`). Apply
  can't change an input's configuration (atemwire has no command for it),
  so a dual-mono profile restores onto an input that is already in Dual
  Mono. In v1.3.0 save wrote `Stereo` and -65280 for both halves, and
  apply sent every strip to channel -1, so the right half landed on the
  left at an address a dual-mono input doesn't have, and the XLR went to
  -1 too (SURFACE §5.4 item 8, a trap the port had inherited).
  `dualmono_test.go` ports bmdwire's tests on its fixture,
  `profile/profile_fairlight_dual_mono_asc.xml`. On hardware (the
  eleventh run, docs/HARDWARE.md), `profile restore` of that fixture with
  only `--audio` gave each half of input 1 its own fader, pan and EQ in
  ASC and moved the XLR, and a save wrote input 1 as `DualMono` with
  -256 and -255.
- **Apply runs atemwire's 17 sections in order.** Each operation is a
  function that makes the checks the operation makes when called, and
  records what fails as `_safe` does. Those checks: round() or int() of NaN
  raises ValueError, of an infinity OverflowError; `_db_to_wire` raises
  only for +inf; unknown mix option, EQ shape or frequency range raise
  ValueError with atemwire's message. A command that only fails to encode
  (Validate, or an int too big for its Go field) is dropped and logged, as
  ATEMConnection's worker drops what get_command refuses. So the results
  are atemwire's with an ATEMConnection, not with a bare AtemProtocol.
- **Attribute values are read with Python's rules.** `_float`, `_int` and
  `_bool` are rebuilt: float()'s grammar, Unicode digits, underscores,
  isspace rules. `_int` is held as an integral float64 so Python's big ints
  stay exact. An `_int` of infinity outside `_safe`, and a macro op whose
  encoding fails with anything but ValueError, end the apply: Apply returns
  that error (`*Error`, `*macro.Error`) with the partial result, where
  atemwire raises.
- **Rates** are `str(_int(rate))`, which parse_rate reads as frames at any
  frame rate, so Apply doesn't read DisplayFPS.
- **HyperDeck addresses** go through a port of glibc's inet_aton and are
  sent as the dotted quad they mean; what inet_aton refuses is dropped, as
  at encode time in atemwire.
- **Target errors** are recorded as ConnectionDeadError (ErrClosed,
  ErrDisconnected, ErrNotReady) or RuntimeError. A Target whose
  `Connected()` is false gets "macros: no connected protocol".
- **Go additions:** contexts (checked before each section), `ResyncWait`
  (0 means 3 s, negative means no pause), `MacroTimeout`, `Logger`.
- **XML layer:** a parser mirroring expat with namespace processing feeding
  a TreeBuilder, and a serializer mirroring ET.indent and ET.tostring plus
  `re.sub(r'\s+/>', '/>')`, byte for byte. FromXML's UTF-8, NUL and DTD
  guards (Python's IGNORECASE and Unicode `\b`) are atemwire's. ToXML
  re-indents the tree in place, as to_xml does. ParseError text and
  positions only approximate expat's. Non-ASCII name characters use Go's
  Unicode tables, not expat's Appendix B.
- **Not ported:** `download_media_pool_images` (needs still download and PNG
  export), `_source_symbol` (unused), `_ProtocolAdapter`/`_resolve_connection`.
- **Verification:** `oracle/profile.json` from `gen_profile.py` (bmdwire
  `v1.3.0-9-g785aac7`) holds the golden's state, 46 saves, 274 applies
  (18,628 events, 30 raising) and 130 parses, all replayed byte for
  byte. The golden's state holds a TStP media player of 3010, more than
  a u8, so the golden test injects that one value through the saver's
  `stingerSource` hook. `profile_test.go` ports bmdwire's profile tests
  one for one; `e2e_test.go` applies and saves through a Session against
  the fake switcher.
- **Save is verified on hardware** (the fourth run, docs/HARDWARE.md):
  `profile save` from a Constellation wrote the XML with its six macros,
  and the one used still as a PNG beside it. ATEM Software Control 9.3.2
  opens a save (the tenth run, a Constellation on firmware 9.2.1) in its
  Restore dialog with every section listed and no warning; the items it
  greys are hardware the unit lacks.
- **Name-only macros are restored, never skipped.** A `<Macro>` with a
  name and no ops (or none that encode) is uploaded as empty bytecode,
  as atemwire's `_apply_macros` does, and a switcher keeps it as a macro
  with that name and no ops: in the tenth run, `profile restore
  --macros-only` of `<Macro index="22" name="empty-test"/>` left slot 23
  holding empty-test, in `macro list` and in ASC's Macros window. A
  name-only macro is a macro, so restore must not skip it. Of apply's
  other sections only the Fairlight mixer is verified on hardware (the
  eleventh run; see "Fairlight strips are saved and applied per half").

## Command table and CLI (`internal/table`, `atem/table.go`, `cmd/bmdctl`)

- **`internal/table` is generic** (and MIT). An `Entry` is
  `{Device, Path, Summary, Params, Build, Operation, Local, Stream, Echo,
  Unverified, Destructive, AtLeastOne, Examples}`; a `Param` has a name, a kind
  (uint, int, float, bool, string, enum, flags, rate, time, list, source,
  file, out-file, dir), an optional range, a unit, help,
  required/default, and `OneBased` (an `Index`, below). A `Registry`
  checks entries as they
  are added (no duplicate paths, no path that is both a command and a
  group, `Build` set exactly when the entry isn't an operation, a syntax
  that fits, below), reads command lines (`ParseLine`) and renders
  `docs/COMMANDS.md`.
- **Required arguments are positional, in the order they're said;
  optional ones are flags** (phase 3, after the first hardware run).
  An entry's `Syntax` is its words with its positional arguments where a
  person says them: `program <source>`, `aux <aux> <source>`,
  `usk <keyer> fill <source>` (the keyer before what is done to it),
  `media upload <slot> <file>`. Left empty it is the path followed by
  each required parameter in the table's order, which is the order for
  every command but the upstream keyer's and the two downloads. `Add`
  refuses an entry with a required parameter that isn't positional, or
  a syntax whose words aren't its path. An optional argument is a flag
  (`--me`, `--dsk`, every setting of `usk <keyer> luma` or `transition
  wipe`), except one that reads as the object of the command: the file a
  download writes (`media download <slot> [file]`, `macro download <slot>
  [file]`), which may be left out.
  - **Reading a line.** `ParseLine` matches the words against every
    entry's syntax (literal words exactly, an argument taking any word),
    preferring a match that leaves no required argument empty, then the
    most literal words; values after the flags fill the arguments still
    empty, in order (`program --me 2 5`). A flag may be shortened to any
    unambiguous prefix (`--sat` for `--saturation`); an ambiguous one
    lists what it could be. Words that name no command give the longest
    group they begin, for its help; a command with words left over says
    which (`unexpected "6": program <source>`).
  - **The `--name` form**, how every argument was given before, is still
    read for every positional argument, for one release: `program
    --source 5` is `program 5`, `usk on-air --keyer 2 --on-air` is
    `usk 2 on-air on`. Help, `docs/COMMANDS.md`, the man page and
    completion show only the positional form; an argument given both ways
    is "given twice". A line that uses it prints one line on stderr,
    `bmdctl: --source is deprecated, use: program 5`, with the line
    rewritten (`Entry.Deprecated`: the syntax with the values, a bool as
    on or off, then the line's other flags as typed, quoted for a shell
    where they need it). It is printed once per invocation: `exec` prints
    it while checking the script, for the first line that uses the form,
    and not again. Drop the form, and the notice, in the release after
    this one.
  - **Messages** name a positional argument as it appears (`source
    "Nobody": no input is named "Nobody"`, `missing <source>: program
    <source>`, `keyer 5: M/E 1 has 4 upstream keyers`) and a flag as
    `--name`.
  - **Fairlight `--channel`** defaults to -1, a stereo strip, where it
    was required: `audio strip cam1 --volume -10` reads as said, and
    a split-mono half is `--channel 0` or `1`.
  - **`audio` is the Fairlight group, `classic-audio` the classic
    mixer's** (after the first hardware run; atemwire's tables call
    them fairlight and audio-legacy). `audio strip`, `audio eq`, `audio
    master` and the rest are Fairlight's; `classic-audio input`,
    `master`, `monitor`, `levels` and `meters` the classic mixer's.
    `fairlight` is a hidden second name for `audio` (`Registry.Alias`:
    it stands for `audio` wherever a line names no command of its own,
    which is every line; help, completion's lists and
    `docs/COMMANDS.md` leave it out, while `fairlight strip <Tab>`
    completes and `fairlight --help` is the audio group's). A line that
    names no command either way gets the deeper of the two groups for
    its help.
  - **Where both mixers have a command** (master, levels, meters),
    `audio` sends the one the switcher has and `classic-audio` forces
    the classic one. The switcher's mixer (`engine.Mixer`, also `info`'s
    `audio`) is the one whose capabilities it sent: `_FAC` for Fairlight,
    `_AMC` for the classic mixer, which atemwire doesn't parse and keeps
    raw (`State.Raw`). Failing both, it is the mixer whose state it sent
    (`FAMP`/`FAIP` or `AMMO`/`AMIP`); the topology (`_top`) says nothing
    about audio. bmdwire v1.6.0's `audio_mixer_type`
    (`State.AudioMixerType`) decides by `FAMP`, then `AMMO`, alone; `Mixer`
    keeps the capabilities first, and agrees with it except on a state
    with an input's packet but no master's, which it names and
    `audio_mixer_type` calls unknown. On a classic switcher the engine (`forMixer`, after the
    arguments parse) sends `classic-audio master` for `audio master` and
    `classic-audio levels` for `audio levels`, with the same arguments:
    a Fairlight-only one (`--eq`, `--eq-gain`, `--makeup`) is refused,
    naming the classic command, and `--volume`, in dB as Fairlight's is,
    goes on the classic mixer's scale through `atem.LegacyDBToVolume`,
    bmdwire v1.6.0's `legacy_db_to_volume`: floor(10^(dB/20) × 32768),
    clamped to 0-65381, so 0 dB is 32768, +6 dB 65380 and only louder
    the top; -inf is 0. (Before v1.6.0 atemwire left the scale raw and
    the port rounded, so -6 dB sent 16423 where it now sends 16422. The
    scale is LibAtem's and atem-connection's, unconfirmed on hardware,
    as the classic commands are.)
    `classic-audio master --volume` stays atemwire's raw value. The
    result says what was sent (`CommandResult.Sent`, `"sent":
    "classic-audio master"` in `--json`, and the result line names it,
    with its arguments as sent), and the CLI prints the unverified notice
    for it then, since it could not know before sending. `audio meters`
    reads whichever mixer the switcher has, `classic-audio meters` the
    classic one's. `--dry-run`, with no switcher, encodes the Fairlight
    command for `audio master` and `audio levels`; `classic-audio` gives
    the classic bytes.
- **Two kinds of entry.** A wire command has a `Build` that makes one
  `atem.Command`. An *operation* (phase 3) has none: the engine runs it by
  path (reading state, a transfer, a read-modify-write); a `Local` one
  needs no switcher and runs in the CLI; a `Stream` one answers until
  stopped.
- **Device packages fill it explicitly.** `atem.RegisterCommands(r)` adds
  the ATEM wire commands, `engine.RegisterOperations(r)` the operations;
  `engine.NewRegistry` does both and the CLI and the engine share it. No
  `init()` side effects, so a program that imports `atem` without the CLI
  gets no table.
- **One entry per wire command**, 75 of them: every command except the
  seven lock and file-transfer ones (§1.19), which a CLI reaches only
  through transfers. Paths group by SURFACE §1 table: `program`,
  `transition wipe`, `usk dve`, `audio strip`, ... Each parameter maps
  onto one command field and is set by reflection; integer ranges come from
  the field's Go type, so the table can't promise more than the wire holds.
  Camera control is the one hand-written builder (its `--data` values are
  read as `--type`).
- **Enum names are atemwire's**: the wipe-pattern and DVE-effect names of
  its macro tables, `VIDEO_MODE_NAMES`, `KEYFRAME_BY_NAME`, the Fairlight
  mix-option, EQ-shape and EQ-range names. Numbers are accepted too.
- **Masked commands need at least one field** (`AtLeastOne`): with none,
  atemwire would send a zero mask, which changes nothing. Entries derive
  this from having optional pointer fields.
- **Indexes count from 1, as ATEM Software Control and the panels count
  them:** M/Es (`--me`), upstream keyers (`<keyer>`), DSKs (`--dsk`), aux
  outputs (`<aux>`), still and clip slots (`<slot>`, `--still`,
  `--clip`), macro slots (`<slot>`), media players (`<player>`), colour
  generators (`color <generator>`), multiviewers and their windows
  (`--multiviewer`, `<window>`), SuperSources and their boxes
  (`--supersource`, `<box>`), HyperDeck slots (`hyperdeck-slot <slot>`)
  and EQ bands (`<band>`), on the command line, in `--help` and in
  results, `--json` included. Where every ATEM has the same number, the
  range says so (generators 1-2, boxes 1-4, bands 1-6; HyperDeck slots
  1-10, atemwire's 0-9) and completion offers them without a switcher;
  otherwise the range is the field's. The
  atem package counts from 0 and is unchanged; the CLI/engine boundary
  converts, and only `table.Index` (`internal/table/index.go`) adds or
  takes away the one: a one-based parameter is parsed by `ParseIndex`
  (so `Args` hold the package's value), `Range` shows the field's range
  through it (`--keyer`: 1-256), and a result that names an index holds
  an `Index`, whose JSON, `String` and `Format` (any verb, so even `%d`
  can't print the raw value) count from 1. Defaults moved with it
  (`--dsk 1`, `--me 1`, `--multiviewer 1`, `--supersource 1`). `macro action --slot`, which atemwire's 65535
  (whatever is running) used to default, is now optional: left out it
  sends 0xFFFF, and the highest slot it names is 65535 (0xFFFE).
  - **What still counts from 0**, because it is the library's or a file
    format's own: the switcher state `state` prints (`--path mes.0`),
    `watch`'s values and keys (`KeOn:0:1`), a wire command's echo key and
    echoed packet in its result, and the `index` attributes of ATEM
    Software Control's XML (`macro download`, `profile save`). No typed
    argument counts from 0 but `state --path`, which follows the state
    it reads. Numbers that aren't positions keep theirs: the stinger's
    `--player` is 1-4 on the wire already, camera control's
    `<destination>` is the camera's own number (255 for all), its `<category>` and
    `<parameter>` are Blackmagic's camera control ids (lens is 0, focus
    0.0), and Fairlight's `--channel` is -1 for a stereo strip and 0 or 1
    for the halves of a split-mono one. A still without a name is saved
    as atemwire's `Slot<n>.png`, n from 0.
- **Sources** (`source` kind: program, preview, aux, keyer and DSK fill
  and key, dip, wipe border, DVE fill and key, multiviewer windows,
  SuperSource, Fairlight and classic audio sources, monitor solo, HyperDeck
  input) are found in this order, case-insensitive, surrounding spaces
  ignored (`engine.Sources`, wrapping `atem.Resolver`, which is
  unchanged): a number is that id, not checked against the inputs (audio
  sources such as 1301 have no `InPr`); an alias (`engine.SourceAlias`)
  names a source whatever its label: `camN`, `camera N`, `inN`,
  `input N` are input N, `mpN` 3000+10N, `bars` 1000, `black`/`blk` 0,
  `colN` 2000+N, `pgm` 10010 and `pvw` 10011 (M/E 1's outputs), `cfN`
  7000+N, `auxN` 8000+N, provided the switcher lists that source (black
  always); else a short or long label (`State.InputsNamed`; one two
  inputs share is refused with both listed); else the one input a label
  starts (two or more are refused, listed). An alias that is also
  another input's label wins, and the engine says so in a note
  (`alias`), which a terminal prints once ever. Nothing found is the
  error it always was, `no input is named "x" (bmdctl inputs lists
  them)`, with a `no_input` note carrying the switcher's first six inputs
  for a terminal to list. (After the terminal pass, which asked for it:
  operators type cam2 whatever a camera is called. atemwire takes ids.)
  Labels need the switcher's state, so they resolve in the engine;
  `--dry-run` takes numbers and aliases.
- **Rates** take frames or "seconds:frames", converted at `DisplayFPS` of
  the session's state unless `--fps` is given (with `--dry-run`, `--fps`
  or 25, atemwire's fallback). Deliberately stricter than atemwire's
  `parse_rate`, which reads anything malformed as one second: the CLI
  refuses a rate that isn't ASCII digits, optionally `:` and digits, and
  (with the switcher's rate known) frames after the colon that aren't
  under the display rate. A typo on the command line is an error, not a
  silent default. `atem.ParseRate` itself is unchanged.
- **The CLI** uses the standard library only. `bmdctl version` prints the
  GoReleaser `-ldflags` version, else the build info. The device word is
  optional (`bmdctl program ...` is `bmdctl atem program ...`); help is
  `--help` at any level. Global flags are accepted anywhere on the line:
  `--host`, `--json`, `--quiet`/`-q`, `--yes`/`-y`, `--dry-run`, `--fps`,
  `--timeout`, `--no-color`, `--help`/`-h`. One-letter forms are single-dash only,
  because parameters named `q` (EQ Q) and `y` (SuperSource, chroma
  cursor) exist; a test keeps parameter names off the global flags. A
  bool flag given alone (`--fly`) is true; a positional bool takes on,
  off, true, false, yes, no, 1 or 0. Arguments are checked
  first without a switcher (names and rates for their form only), then:
  - `--dry-run` prints the command's bytes (header included) as hex on
    stdout and sends nothing, even with `--host`. Operations have none.
  - Otherwise the command goes to the engine (below), which parses it
    again against the switcher's state, sends it and waits for the echo.
    A destructive command without `--yes` is refused before that, and
    again by the engine, unless a person can be asked (the terminal pass:
    stdout and stdin both terminals, not `--json` or `--quiet`, and not a
    line of `exec`, whose lines are checked for `--yes` before any
    runs). Then the request carries `ask`; the engine dials, parses and
    checks it as for sending, and, still without Yes, refuses it with a
    `confirm` note first: what it would affect, from the switcher's
    state (`slot 1 holds "logo", used by MP1 on program`; `video mode
    1080p50 → 525i5994: every output resyncs`), and the question
    (`clear it?`). The engine's own refusals (a used still or macro slot
    replaced, `macro action` delete) note the same. The CLI prints them
    under the header with `[y/N]`; `y` or `yes`, in any case, sends the
    request again with Yes; anything else, Enter or the end of input
    prints `cancelled` and exits 5. A video mode the switcher runs
    already isn't asked about: nothing is sent (`already`). An unverified one prints a line saying so on
    stderr, unless `--json`.
- **Output.** `--json` prints each engine response line exactly as
  received, and a failure the CLI finds itself (a usage error, a
  destructive command without `--yes`, no host) in the same
  `{ok:false, code, error}` shape on stdout, so a script reads one form.
  `--quiet` prints nothing on stdout, `--json` or not; errors still go to
  stderr. What people see otherwise is set out under "Output conventions"
  below; the UX pass after the fourth hardware run left `--json` and
  `--quiet` exactly as they were.
- **Every command runs.** `TestEveryCommandRuns` sends every entry in the
  table, with sample arguments, through an engine to the fake switcher
  and requires it to succeed (the one exception, `media
  clear-unlocked`, is ignored by the fake as some models ignore it).
- **Exit codes** (phase 3): 0 done, 1 usage (including a command
  `Validate` refuses) or any other failure, 2 the switcher or the engine
  unreachable, 3 sent but not confirmed in time, 4 refused by the
  switcher, 5 destructive without `--yes`. Phase 2's were 0, 1 refused or
  unreachable, 2 usage.

## Output conventions (`cmd/bmdctl/output.go`)

The UX pass after the fourth hardware run. This is what a pipe gets,
and what a terminal got before the terminal pass, whose layout is under
"Terminal", below; a new command follows both, and says where it
can't.
None of it reaches `--json` or `--quiet`: what the CLI shows that a
response doesn't carry comes in note frames (Engine, "Notes"), which the
CLI prints only for people, so every JSON line is byte for byte what it
was. Verified on hardware (the fifth run, docs/HARDWARE.md): the
connecting line, before → after with short names, the overview, the
status idle countdown and an error's next step.

- **The switcher is named first, as `<name> (<address>)`.** The name is
  the host's in hosts.yml, which is the switcher's own (Hosts, below)
  unless `--as` gave one; for an address no host has, the name the
  switcher gives itself, from the session's note; the address alone when
  neither is known (an error before any session, `disconnect` with none).
  The address drops ATEM's default port, `:9910`.
- **A write is one line** (`printer.writeLine`), in columns so the lines
  of `exec` to one switcher line up: the switcher; what was done, as said,
  with the positional arguments that say where (`usk 2 on-air`, `aux 2`,
  `audio strip CAM1`), and where given by a flag (`ME1`, `DSK2`, `MV1`,
  `SS1`; another flag that says where only when typed); what changed;
  `confirmed` or `sent`; the time. The what-changed column is as wide as
  a change between the switcher's two longest source names, as lines
  show them (`CAM2 → CAM5` where every source has a four-letter short
  name), from the switcher note's `name_width`; a wider change pushes
  the status along. (After the fifth hardware run: a fixed 24 left wide
  gaps on a switcher of short names.) An argument says where when the
  entry's echo key names it (`KeOn:<me>:<keyer>`), or, for an entry
  without an echo, when it is an index. What changed is in watch's form:
  `CAM2 → CAM5` for an entry's one required argument (or a value it has
  no argument for, as `cut`'s program), else `rate 1:00 → 0:12, width 25
  → 50`; a value the switcher already held, or whose before the state
  doesn't hold, alone. Values read as watch shows them: sources by short
  name (else long name, else id), indexes from 1, enums by name, flags
  joined with `+`, booleans `on`/`off`, rates as seconds:frames, strings
  quoted.
- **Before and after come from the engine** (`internal/engine/notes.go`).
  Before is read from the state the command found, through atemwire's
  readers (`readers`, one per path): bmdwire v1.3.0 pairs each reader with
  the operation that sets the same value, so the units are the
  argument's (percent, dB, frames). After is read the same way from the
  state the echo left, so a confirmed line shows what the switcher
  reported, clamps and refusals included: against the fake switcher,
  whose `mirror.go` answers with the state it holds, a wipe shows its old
  rate. A command with no echo shows what it sent; `cut`, `auto`, `ftb
  toggle` and `dsk auto` show what they ask for (`moves`), `ftb toggle`
  and `dsk auto` marked `sent`, a cut or an auto `confirmed` once the
  switcher reports it (Engine, "Wire commands").
  A command with no reader shows its arguments as sent. An operation that
  sends one command says what it changes itself (`sendBuilt`'s change:
  the toggles, `transition next`, `transition rate`, the macro slot).
  Readers now cover the switching, transition, keyer, DSK, fade to black,
  colour, media player, multiviewer input, input name, video mode and
  Fairlight master commands; the DVE and chroma keys, Fairlight strips
  and the rest show what was sent until someone adds theirs.
- **A read starts with a header**, `<name> (<address>)  <model>  <video
  mode>`, then its rows: `status`, `info`, `state` (but not a path to one
  value, which prints the value alone, for scripts), `inputs`, `media
  list`, `macro list`, `video-modes`, `time`, the meters, and `watch`
  once it watches (`reports`). Transfers and profile save, restore and
  images are one line starting with the switcher, as a write's.
- **Opening a session says so, on stderr**: `connecting to <name>
  (<address>) …` when a command starts the engine (`connectEngine`'s
  `spawned`), when the engine dials for the request or makes it wait for
  another request's dial, and when it waits for a session re-handshaking
  after a silence; then `<model>, <video mode>` once it is ready. Once
  per switcher per invocation, so `exec` says it at most once per host;
  nothing when the session is warm; nothing with `--json` or `--quiet`.
  At a terminal the spinner says it instead, and the header says the
  rest (Terminal, below).
- **Colour only on a terminal**, stdout and stderr each checked for
  themselves, in the palette under Terminal, below: program and on air
  red, preview green and nothing else green. `--no-color` (new, global,
  passed by `exec` to its lines), `NO_COLOR` and `TERM=dumb` turn it
  off; `--json` never has it. `confirmed` is uncoloured, so green means
  preview alone.
- **Errors end with the next step** (`nextStep`), on stderr only:
  unreachable (exit 2) ends `(bmdctl doctor says why)`, not confirmed (3)
  `(bmdctl status shows what the switcher holds now)`, destructive (5)
  with `--yes` (every such message names it already); an unknown source
  already ends `(bmdctl inputs lists them)` and an unknown host `(bmdctl
  hosts lists them)`. The `{ok:false, code, error}` of `--json` keeps the
  engine's message unchanged.
- **`bmdctl` alone** (`overview.go`) prints the engine (running or not,
  pid, uptime, sessions, the git commit it was built from, and whether it
  is this bmdctl's build) and a line per host: model, video mode, `ready,
  session 1h04m, idle 2m10s, pinned` (`in use` for idle while a request,
  a watch or a subscription, uses it; `stale` for pinned past its idle
  timeout; or connecting, failed, no session), `(default)` (#7 added
  the session's age; `idle` came straight after `ready` before);
  then the engines' sessions to addresses no host has, by the switcher's
  name. With no host configured it ends with how to add one and `bmdctl
  help lists the commands`; with hosts, the help line is noise and is
  left out (after the fifth run). It asks a running engine (the
  `overview` request) and never starts one. `--json` has the same as
  `{engine, hosts}`. `bmdctl help` is the help, which bare `bmdctl`
  printed (exit 1) before.
- **The commit, not the build, is what people see** (after the fifth
  run). `bmdctl` and `status` (an `engine` row) show the engine's git
  commit, short as git prints it (`commitString`: GoReleaser's, else the
  toolchain's `vcs.revision`, with `+dirty` when `vcs.modified` is true,
  which Go sets for untracked files too). A build ID means nothing to a
  person; it stays what tells an engine of another build (`(not this
  bmdctl's build)`, and the stale restart), since two builds of one dirty
  tree share a commit. A binary without VCS stamps (`go run`) shows no
  commit.
- **`docs/COMMANDS.md`** is generated by `Registry.Markdown`; the golden test
  in `cmd/bmdctl` fails when it is stale and `-update` rewrites it.

## Terminal (`cmd/bmdctl/screen*.go`, `term.go`, `spinner.go`)

What a person typing commands sees. Two passes made it: the readability
pass after the fifth hardware run (palette, spinner, aligned tables),
and the terminal pass that followed, whose spec is
`docs/terminal-mock.html` and which this section records whole. Every
command a person runs follows these rules, and a new command does too
(CLAUDE.md says so); one that can't says so here. After the seventh
hardware run this section departs from the mock: every status ends at
column 72, a keyer's on air comes last, after how it is set, and a cut
or an auto shows its time, not `sent`. Where they differ, this section
is right.

**Where it applies.** Only when stdout is a terminal and neither
`--json` nor `--quiet` is given (`cli.terminal`); what is drawn on
stderr (the spinner, the bar) needs stderr to be one too, and `TERM`
not `dumb` (`cli.spins`); a question needs stdin to be one as well
(`cli.asks`). A pipe gets what "Output conventions" describes, byte for
byte as before; `--json` and `--quiet` are unchanged throughout, except
that a write the switcher already held has `"already": true` in its
result (Engine, "Wire commands"). Behaviour that isn't presentation
(source names, `connect ADDRESS`, `already`, `undo`, `show`,
examples in help) applies everywhere. What a terminal shows that a
response doesn't carry comes in note frames (Engine, "Notes":
`switcher`, `on_air`, `values`, `alias`, `no_input`, `progress`,
`steps`/`step`, `confirm`), which `--json` never prints.

### Layout (`screen.go`)

- **The switcher is named once per invocation**, in a header: its name
  bold (the host's, else the one it gives itself, else its address),
  then dim, joined by ` · `: `PGM <x> · PVW <y>`, M/E 1's program and
  preview by short name, as they were when the command ran: the
  `on_air` note, read once, before the command (after the seventh
  hardware run, where a cut's header showed what it had just put on
  air, next to a line saying the same; the header is the state the
  line's before → after starts from); what the
  invocation is (exec's `open.cue · 5 lines`); and, only when this
  invocation opened the session, the model (left out where it is the
  name), the video mode and `connected N ms`; then the view's own parts
  (`20 stills`, `keys`, `watching · ctrl-c to stop`). `status` leaves
  out PGM · PVW (its first row says them) and always has the model,
  video mode and `session 4m, idle 12s`. exec's lines share the header
  (`screenState`); a failed command prints no header unless a line of
  exec. `disconnect` is the one line that names the switcher:
  `<name>  disconnected`.
- **Nothing is wider than 72 columns** (`lineWidth`): a longer line is
  cut, with `…`, never wrapped (`fit`, which keeps colour codes and
  closes one left open). The header drops the model, then the video
  mode, then the connect time before it cuts; a result cuts what it did,
  never its status; a hint list cuts its list, never its last words. A
  document (macro decode's XML, a state value) is printed whole.
- **Results** are indented two spaces under the header:
  `<verb padded to 10>  <before> → <after>` and the status, dim, right-
  aligned to end at column 72, the line's last (`trailing`; it was 64,
  `timingEnd`, until the seventh hardware run, while status's and show
  keys' on-air columns ended where their rows did). Every row with a
  status ends there, results and tables alike: a time, `sent`,
  `already`, `rejected`, on air, `● PGM`, a session. The status always
  has eight columns (`statusWidth`, as wide as `rejected`), whatever it
  says: where a long line is cut doesn't move with the width of the
  time taken (CI's 3-digit times moved one golden's status a column). The verb is the command's words with the arguments that say
  where (`aux 2`, `usk 2 on-air`, `color 2`), and a flag that says where
  only when typed (`program ME2`): the header named the switcher, and
  ME1 on a one-M/E switcher is noise. A verb of ten or more just takes
  its two spaces. The status is the time taken (`32 ms`, `1.1 s`);
  `confirmed` is never printed. `sent` (no echo confirms it), `already`
  (the switcher held it; nothing was sent) and `rejected` (the switcher
  confirmed but kept the value it held: a value's `kept`) take the
  time's place. A cut or an auto transition says both buses: `MP1 on
  program · Ca2 on preview`. Transfers say the slot, the name and what
  moved (`upload  slot 10  sponsor  1920×1080`, `clear  slot 10  was
  "sponsor"`); undo is a write with the verb `undo`, and an undone cut
  or auto says both buses as a cut does: `undo  cut  CAM2 on program ·
  CAM1 on preview`. A change that names a file (a download's, a profile
  save's) shortens the path from the left to what fits, keeping its last
  components (`…/profiles/before-the-show.xml`), not cut from the
  right, which lost the file's name, its size and the status (#6;
  `resultLinePath`, `fitPath`).
- **Sources** in a result show by short name, else long name, else id;
  where what was typed differs (and isn't a number), it follows dim in
  brackets: `lkkk (cam1)`. An alias that shadowed an input's name is
  said once ever, dim, on stderr (the `alias` note, remembered in
  `$XDG_STATE_HOME/bmdctl/told`).
- **Reads** are the header, a blank line, then rows: a label in the verb
  column (`ME 1`, `keys`, `dsk 1`, `aux 1–6`) or an id column (dim,
  right-aligned), then values. `status` is ME rows (`PGM  Ca2` red, `PVW
  MP1` green, then `mix 1:00 · next background · FTB off`), keyers and
  DSKs ending in their on-air state at column 72 (`on air` red, `off`
  dim), aux outputs, the macro player (`idle` dim), recording and
  streaming. `show <area>` lays out one area the same way: a keyer's
  row is status's (`keyerRow`: number, type, fill / key) with how it is
  set dim after it and on air still last, so the same keyer has the
  same columns in both. `inputs`' `● PGM` and `○ PVW`, `macro list`'s
  running or recording, and `bmdctl`'s sessions end at column 72 too.
  `inputs`' marks sit forty columns from a short name, so the gap
  between the row and its mark is a dim leader, `· · ·`, a dot on every
  even column with two clear at each end, so the leaders line up row to
  row (#13, `leadered`; #12 asked for the marks beside the names
  instead, which would break the column-72 rule; the media list already
  has its MP1 and MP2 beside each still).
  `info`, `video-modes`,
  `time`, the meters and `state` follow.
- **Repeats collapse**: consecutive aux outputs on one source are one
  row, `aux 1–6  PGM`; empty media and macro slots no player shows are
  counted, `17 empty slots hidden · --all shows them` (a terminal asks
  the engine for every macro slot to count them); `inputs` hides
  outputs, key masks and media player keys unless `--all`, and an
  internal source has no number (nobody types 3010); what is on program
  and preview is marked `● PGM` red, `○ PVW` green.
- **Errors** are `error` red, two spaces, the message, with no
  `bmdctl atem program:` in front (the person just typed it), then, on
  the next line under the message (seven spaces), what to do next, dim:
  the engine's own (a trailing ` (bmdctl ...)` or `: add --yes ...`),
  else by exit status (`bmdctl doctor checks the network`, `bmdctl
  status shows what the switcher holds now`, `add --yes to do it`). A
  switcher that didn't answer is named (`STUDIO B (192.168.81.86) did
  not answer`), the cause in the hint. A source that names nothing is
  `no input "x"`, the hint the switcher's first six inputs as `N short
  long` on one line, then `· bmdctl inputs for all`. A destructive
  command refused says what it does (`video mode resyncs every output`,
  `dangers`).
- **bmdctl alone** is one dim line, `bmdctl <version>`, then a row per
  switcher (its alias, in a column only when some host has one; name
  bold, address, model, video mode, then its session: `● 4m` for one
  open four minutes, then dim `· idle 12s` (`· in use` while a request,
  a watch or an MCP subscription, uses it) and `· pinned`, or, idle past
  its timeout and not yet closed, `· stale`, plain to stand out from
  the dim; else `not connected` dim), then `bmdctl help` dim. The
  session's age and idle time (#7) take the room the model had: where a
  row and its session don't fit 72 columns the model column goes, for
  every row, as a header drops the model first, and every row is then
  cut at one column, so the cuts line up. In the model's place goes a
  dim tag for the kind of device (`ATEM`, `HD`, `VH`, `ULT`;
  `deviceTag`), so a deck still reads as a deck (#11). The mock's row
  has the model;
  `bmdctl status` and `info` still say it. The
  default's row has `*` in its indent's first column, and the last line
  says so: `* default · bmdctl use HOST · bmdctl help` (the mock has no
  mark; the indent keeps the columns where they were, as kubectl's and
  git's `*` do);
  with none, how to add one (`bmdctl connect ADDRESS`). A HyperDeck's
  row is a switcher's: its model and video format, and its session or
  `not connected` (a pipe's, `no session`); only a device bmdctl has no
  commands for (Videohub, Ultimatte) says `no commands yet`, which every
  device but an ATEM said until after the presentation pass. The engine,
  the commit and the hosts file are `bmdctl version`'s, which at a
  terminal prints them under the version. There is no other banner, and
  no prompt before results.
- **`engine status`** at a terminal is its pipe lines cut to 72
  columns (the build id, which is longer, ends in `…`), with each
  session as a row of a table: `session`, its address (the port left
  out where it is ATEM's), then how it stands in the overview's words,
  `ready · 2h04m · idle 12s · pinned · closes in 59m`, and its model and
  video mode only where they fit (#11; `engineStatusScreen`). A pipe
  keeps the old line.
- **`connect ADDRESS`** prints the header, then `saved as "<name>", now
  the default` (bold: bmdctl's own) and, the first time ever, `tab
  completes input names` (dim); `connect` alone, `connect  pinned until
  disconnect or 1h idle` (until disconnect, with `BMDCTL_PIN_IDLE=0`).
- **`-t`** (`--timestamps`) starts each result line with the time of
  day, `HH:MM:SS`, dim, after the indent (and exec's line number). Only
  at a terminal; exec passes it to its lines.
- **watch** is the header (`watching · ctrl-c to stop`), then a line
  per change: the time of day dim, the verb column (program, preview,
  tally, transition, key N, dsk N, input N), the change; `session
  dropped` and `reconnected` bold, `program and preview unchanged` dim
  where nothing changed while it was away. A keyer or DSK going on or
  off air has a line (a fading DSK's repeats are left out); anything
  else is its pretty key and JSON, cut to the line. The mock's `cut`
  line, pairing a program and a preview change, is not drawn: they
  arrive as two changes.
- **exec** numbers each result with its line in the file, has a failed
  line's error and hint in its place, and ends `done · N ok` (`· M
  failed`) or `stopped at line N`, dim (exec, below).

### Colour (`term.go`)

- **The palette is ANSI red (31), green (32), bold (1) and dim (2), and
  nothing else**: the 16 colours every terminal has, and no 256-colour
  or RGB code. bmdwire's copper and any fourth hue stay out
  (docs/BRAND.md); the owner kept the brand when asked.
- **Each means one thing.** Red: the value now on program (a write's
  after, status's `PGM`, `● PGM`), a keyer or DSK on air, fade to black
  black, recording, and the word `error`. Green: the value now on
  preview, and nothing else. Bold: the switcher's name and bmdctl's own
  lines (`session dropped`, `reconnected`, `saved as`). Dim: ids,
  times, hints, empties (`(empty)`, `off`, `idle`, `not connected`),
  the spinner and the banner. Nothing else is coloured; labels and
  values are plain. Of before → after only the after is coloured: the
  before has left the bus.
- **`--no-color`, `NO_COLOR` and `TERM=dumb`** take the colour away,
  not the layout. `TERM=dumb` also has no spinner (it can't clear a
  line): opening a session prints `connecting to ... …` on stderr, and
  the header says the rest.

### Waiting (`spinner.go`)

- **A wait shows on stderr once it passes 200 ms**, redraws every 100 ms
  (ten a second at most), and is erased before anything is written to
  stdout or stderr (`spinWriter`), so it leaves nothing behind. It is
  dim and indented: `⠸ <what> · 0.8 s`. It says `connecting to <name
  (address)>` while a session opens, what a transfer moves (`uploading
  logo.png → slot 3`, `downloading macro 4`), a step's name, else
  `waiting for <name>`.
- **A transfer of known size draws a bar** from the engine's `progress`
  notes (at most ten a second; still upload and download, macro upload,
  a profile's stills): `⠸ uploading logo.png → slot 3 █████░░░ 68%  816
  KB  0.8 s`, the bytes moved, or which of several (`still 2 of 6`). One
  of unknown size (a lock wait, a macro download) keeps the spinner.
- **Steps.** An operation of several steps (profile save: setup, with
  its macros, then stills; restore: setup, then stills) announces them;
  each finishes in place under the header, `✓ setup  3 macros  1.9 s`;
  a failed one becomes the error in place, `✗ stills  <why>` red, its
  next step under it, and the steps it left undone `· <step>  skipped`
  dim; a summary line ends it: `profile  show.xml  70 KB · 6 macros · 3
  stills  4.0 s`.

### Confirmation (`device.go`)

- A destructive command, or one replacing a used still or macro slot,
  without `--yes`, where a person can be asked (stdout and stdin
  terminals, not exec's lines): the engine says what it would affect
  (`confirm`); the header, that, then the question: `slot 1 holds
  "logo", used by MP1 on program` / `clear it? [y/N] `. `y` or `yes`, any
  case, sends it again with `--yes`; anything else, Enter or the end of
  input prints `cancelled` dim and exits 5. stdin not a terminal: refused
  as ever.

### Help

Every command's `--help` opens with its summary and up to three
examples from the table (`Entry.Examples`, "Completion, man page,
COMMANDS.md"), then the usage line and the argument and flag tables.

### Tests

`cmd/bmdctl/testdata/terminal/*.golden`, written by `terminal_test.go`
running command lines against the fake switcher with stdout, stderr
(and, where asked, stdin) forced to be terminals (`cli.tty`,
`cli.ttyErr`, `cli.ttyIn`), the spinner's 200 ms made an hour
(`cli.spinAfter`) so a slow machine draws no frame into a golden, and
`-t`'s clock stopped (`cli.now`). Codes are written as `<red>`,
`<green>`, `<b>`, `<dim>`, `</>`, `<clear>`, `<cr>`; a time taken is `#
ms`, keeping the column it ends in; a typed answer is echoed after
`[y/N]` as a terminal would. `go test ./cmd/bmdctl -run TestTerminal
-update` rewrites them; read the diff. The spinner's and the bar's
frames are tested on a clock the test moves; `TestFit` checks cutting.
`TestUploadBar` runs an upload at a forced terminal with the real
200 ms: the fake holds the upload's second budget (`HoldUpload`) until
stderr has shown a bar, so the upload can't end without one being drawn,
and checks the bar is gone afterwards (after the seventh hardware run,
whose 514 ms upload left no bar in the scrollback, as it shouldn't).

## exec (`cmd/bmdctl/exec.go`)

- **A file of command lines**, one per line, as typed after `bmdctl` (a
  leading `bmdctl` is allowed): split as a POSIX shell splits a simple
  command (`'...'`, `"..."` with its four escapes, `\`, `#` at a word's
  start), nothing expanded. `-` reads stdin.
- **Checked before anything runs.** Every line goes through the checks a
  command gets before it is sent (the command exists, its arguments
  parse, a wire command builds, a destructive one has `--yes`); one bad
  line and nothing is sent, every bad line listed. A show file with a
  typo on line 20 doesn't leave lines 1-19 done. What only the switcher
  can check (input names, rates at its frame rate) is still found when
  the line runs.
- **One host.** The host is resolved once, as any command resolves it,
  and put on every line as `--host`; a line's own `--host` wins for that
  line only. exec doesn't change the last host used. exec's `--json`,
  `--quiet`, `--yes`, `--fps` and `--timeout` go on every line.
- **In order, stopping at the first failure** with its exit status, or
  with `--continue` running every line and exiting with the first
  failure's status. Human output is each command's line, in fixed
  columns so the lines to one switcher align (the switcher's name leads
  each line, so lines to two differ); errors, and the connecting lines,
  go to stderr prefixed `line N:` (each spinner frame too). The banner,
  when the first line starts the engine, is printed once. `--no-color`
  goes on every line.
  `--json` prints one response line per command. `exec`, `engine`,
  `completion`, `man` and `doctor` can't be lines. At a terminal (the
  terminal pass) the lines share one header, which names the file and
  how many lines it has; each result starts with its line's number in
  the file, right-aligned; a failed line's error and next step are in
  its place, numbered, not prefixed `line N:`; and it ends, dim, `done ·
  N ok` (`· M failed` with `--continue`) or `stopped at line N`. A line
  never stops to ask for confirmation: lines were checked for `--yes`.

## Completion, man page, COMMANDS.md (`cmd/bmdctl`)

- **Completion** is one hidden command, `bmdctl __complete WORDS...`,
  which prints the candidates for the last word, one per line (or
  `:files` to have the shell complete file names); the bash, zsh and
  fish scripts `bmdctl completion SHELL` prints only call it and add
  what it prints, so the three can't disagree. It walks the command's
  syntax (`Registry.Next`): after a command's words come its next word
  or the value of its next positional argument, so `usk <Tab>` offers
  keyers and `usk 1 <Tab>` what a keyer does. It completes built-ins,
  command words, global flags and the optional arguments' flags (less
  those already given; a positional argument's hidden `--name` form is
  not offered), enum and flag names (after a comma too), bools, files,
  host names from
  `hosts.yml`, for a source parameter the switcher's input names, and
  for an index the values the switcher has, counted from 1 (as many
  M/Es, keyers on the `--me` given, DSKs, aux outputs, players, stills
  and clips, SuperSources, multiviewers and HyperDeck slots as `info`
  reports; the macro slots `macro list` reports, and for a slot that
  takes a name, the used ones' names too). Those it asks a running
  engine for (one second at most) and never starts one, so pressing Tab
  doesn't open a session to a switcher. Colour generators, SuperSource
  boxes and EQ bands, the same on every ATEM, need no engine. Matching
  ignores case.
- **The man page** `bmdctl man` prints (or writes to `--dir`) is
  bmdctl(1), rendered like `docs/COMMANDS.md` from the command table
  plus the CLI's own reference (`reference.go`: flags, built-ins,
  conventions, exit status, environment, files), in plain ASCII roff
  (anything else as groff's `\[uXXXX]`), checked with `groff -ww` where
  groff is installed.
- **`docs/COMMANDS.md`** starts with that same CLI reference, then an
  index of the commands by group (the MCP tools' groups, `Groups` in
  `atem/groups.go` and `hyperdeck/groups.go`); each group's entries are a
  page of their own, `docs/commands/<device>-<group>.md`, for the
  documentation site. `go generate ./cmd/bmdctl` writes them all
  (`TestCommandsDoc -update`); the test fails on a stale page, a page no
  group has, or one `mkdocs.yml`'s nav leaves out.
- **Examples** (the terminal pass): every entry carries one to three
  real command lines (`Entry.Examples`, as typed after `bmdctl`
  without the device word, the plainest first): the wire commands' in
  `atem/table_examples.go`, the operations' in
  `internal/engine/examples.go`, set as the entries are registered.
  `--help` opens with the summary and them, before the usage line and
  the argument and flag tables; `docs/COMMANDS.md` shows them in a code
  block under the summary, the man page indented after it.
  `TestExamples` requires one to three per entry, each at most 72
  columns, each reading as its own command and passing the checks a
  line gets before it is sent.

## doctor (`cmd/bmdctl/doctor.go`)

- **What it checks**, one line each, `ok`, `warn`, `fail` or
  `unreachable`: the engine (running or not; never started by doctor;
  its version against this CLI's, since an engine left running across an
  upgrade serves the old code), the socket (where it comes from, its
  directory's owner, a non-socket file in its place, a socket left by an
  engine that died, a mode other users can reach), the state directory
  (writable; the log's size), `hosts.yml` (parses; `File.Check`: names,
  addresses, device types, a default that isn't a host), `BMDCTL_HOST`
  and the last host (each a host or an address), and each host's
  reachability.
- **Reachability**: an ATEM the engine has a ready session to is
  reported from that session, not dialled a second time; otherwise doctor
  dials it itself (`atem.Dial`, 5 s), reports the model and video mode,
  and closes the session. HyperDeck, Videohub and Ultimatte have no
  protocol in bmdctl yet, so doctor checks that their TCP port (9993,
  9990, 9998) accepts a connection. Hosts are checked at once.
- **Exit status**: 1 if anything is misconfigured (`fail`), else 2 if a
  host only didn't answer, else 0; warnings don't count. `--json` prints
  `{ok, result: {checks}}`. Tests replace both diallers, so nothing
  leaves the machine.

## Hosts (`internal/hosts`, `cmd/bmdctl/hosts.go`)

- **`hosts.yml`** lives in `$XDG_CONFIG_HOME/bmdctl` (`~/.config`;
  `%AppData%\bmdctl` on Windows): `version: 2`, a `default` and a
  `hosts` list of `name`, optional `as` and `was`, `address` (`host` or
  `host:port`) and `device` (`atem`, `hyperdeck`, `videohub`,
  `ultimatte`; `atem` when left out). The standard library has no YAML
  reader, so `internal/hosts` reads exactly this shape of YAML (block
  list, plain or quoted scalars, comments, items at the key's indent or
  deeper) and refuses anything else with a line number; it writes the
  same shape back atomically. `bmdctl hosts add|rm|list|default|discover`
  edit it.
- **Hosts are named by their switchers** (the UX pass). `name` is what
  the switcher calls itself: the name set in ATEM Setup (WhoI), else its
  product name (`_pin`), as bmdwire v1.3.0's `atem_name`. `hosts add
  ADDRESS` asks it through the engine's `info` (so the session stays warm
  for the next command, and the connecting lines show) and stores that.
  `--as NAME` gives a name of its own, which the host goes by instead
  (`as`): for two switchers that call themselves the same, as two of one
  model without ATEM Setup names do; the second `hosts add` without it
  fails saying so. Since phase 4 `hosts add ADDRESS` first asks what
  answers there (Device probe, below; `--device` says it instead): an
  ATEM is asked its name as above, a HyperDeck, Videohub or Ultimatte
  says its own to the probe (`--as` where it says none); an address where
  nothing answers is an error, unless `--as` names it, which adds it
  anyway as an ATEM.
  Adding an address that is a host already asks it again: it goes by its
  switcher's name, or by `--as`. Output names a host by the name it goes
  by: its switcher's, unless `--as`.
- **Renames follow the switcher.** Every request's switcher note carries
  the name the switcher gives itself; when it differs from the host's
  `name`, the CLI (`adopt`) renames the host in hosts.yml, keeps the old
  name as `was`, moves the default and the last host to the new one, and
  says so on stderr (not with `--json` or `--quiet`, which rename all the
  same). A host that goes by `--as` keeps it; the change is said. A new
  name another host goes by isn't taken, and that is said, with the
  `--as` to fix it, until it is fixed. `--host` still takes `was`, so a
  script or `BMDCTL_HOST` naming the old name keeps working; one `was` is
  kept, the latest.
- **Migration.** A hosts.yml without `version` is bmdctl's earlier form,
  names chosen by hand, and reads as it is: it is the new form with no
  `as` or `was`. Each hand-chosen name stands until its switcher's next
  command, which renames it as above (`bench (192.168.81.85) calls itself
  "..." now: hosts.yml updated, and --host "bench" still works`); the
  file is written as version 2 then. Hand-chosen names are treated as
  provisional, not as `--as` names, because output is to show the
  switcher's own name unless `--as` was used; `hosts add ADDRESS --as
  NAME` restores one on purpose. Verified on hardware (the fifth run): a
  version 1 hosts.yml's `bench` became `TEST ATEM 2`, the switcher's
  ATEM Setup name, with `was: bench`, and a prefix of the new name
  reached it. `hosts discover` is not yet verified on hardware. `hosts add NAME ADDRESS`, the old form,
  is read for one release as `hosts add ADDRESS --as NAME`, with a notice.
- **Names** are any text a switcher could give itself: not empty, no
  spaces around it, no control characters, at most 100 characters. They
  match without regard to case, and go by (`Label`) `as`, else `name`;
  two hosts can't go by one name.
- **Aliases** (#3). A device's own name is often awkward to type (`BC
  1.2 - TESTTt`), so a host can have one `alias`, a short name it goes
  by as well as its name: wherever a name is taken (`--host`, the first
  word, `use`, `hosts rm`, MCP's and serve's host), exactly or by any
  unambiguous start, an alias is too. An alias is one word a command
  line carries as it is: no spaces or control characters, no leading
  dash, not an IP address, at most 100 characters. Names and aliases
  share one namespace, without regard to case: an alias another host goes
  by (as its name or its alias) is refused, and so is a new or renamed
  host's name that is another host's alias. A rename keeps the alias.
  A file with an alias is written as `version: 3`; without one, still
  version 2, so bmdctl v0.1.x, which refuses a key it doesn't know, reads
  every file it could before and says "a newer one wrote it?" of one it
  can't. `hosts alias NAME SHORT` sets one (another replaces it, `--rm`
  takes it away) and refuses, besides what `CheckAlias` does, any
  command's word in any case (a built-in, a device's word, the first word
  of any device's command: `cut`, `hosts`, `play`), since a line's first
  word is a command before it is a host. `hosts` notes it (`alias bc`)
  and `bmdctl` lists it before the name, in a column of its own only
  when some host has one, so a pipe's lines are as they were without
  aliases. At a terminal `hosts alias` names the host as a header does
  (bold, its address dim: no session says more) and the change as a
  result, `alias  bt → bc`, `none` dim, `already` where nothing changed.
- **Which host.** `--host` (or the first word, below), else
  `$BMDCTL_HOST`, else the host the last `--host` named, else the
  default. Each is resolved (`File.Resolve`): a
  host's name in any case; else the name one went by (`was`); else an IP
  address (with any port), finding the host at it if there is one; else
  any unambiguous start of a host's name (an ambiguous one lists the
  names it could be); else a DNS name, an address. A bare address that is
  a host's shows the host's name. Only `--host` sets "last used"
  (`$XDG_STATE_HOME/bmdctl/last-host`), and only when the switcher was
  reached, as the host's name, not the start of it typed (a host added
  later could make that ambiguous); `hosts default NAME` sets it too, so
  commands go to the new default at once. The default is the
  configured, portable choice; last used is this machine's memory of the
  most recent explicit one.
- **The host as the first word** (#3). `bmdctl HOST COMMAND ...` is
  `bmdctl --host HOST COMMAND ...` for that command alone (`hostWord`).
  The rule: a first word that is a command is the command (a built-in,
  a device's word, the first word of any device's command, not only the
  target's: otherwise a host called `play` would shadow a HyperDeck's
  command); otherwise a word that names a host (`File.Match`: its name
  or alias in any case, the start of either, `was`, its address) or is
  an IP address is the host; otherwise the line is refused, `no command
  or host named "x"`, with the hosts listed (each by its alias, else its
  name, shell-quoted) as the hint. A DNS name no host has is not taken,
  unlike `--host`'s: any word is a valid DNS name, so a mistyped command
  would be dialled. An ambiguous start is the ambiguity error `--host`
  gives. After a device's word the next word is taken the same way, a
  command being that device's: so `bmdctl atem bc cut` and an alias
  binary's line (`hdctl deck play`, which reads as `bmdctl hyperdeck deck
  play`) work. `--host`, given too, wins, and the word is dropped. The
  word doesn't set "last used", as `--host` does: `bmdctl use HOST` is
  how the default moves, and a one-off `bmdctl bc cut` leaving the next
  `cut` on bc would cut the wrong switcher. A word after a host that
  isn't a command is `unknown command`, as before. exec passes its host
  to its lines as a default under both (`cli.execHost`) instead of as a
  `--host` flag, so a line's first word wins over exec's host and the
  line's own `--host` over both. Completion offers the hosts' names and
  aliases with the commands in the first place (after a device's word,
  that device's hosts), and completes the rest of the line for the host
  the first word names.
- **`use HOST`** (#3) makes a host the default without opening a
  session: `hosts default HOST` in two words (hosts.yml's `default` and
  the last host both), so every command that names no host goes there.
  It takes what `hosts default` does, a host (by name, alias, the start
  of either, its address), and for an address no host has points at
  `hosts add`. `$BMDCTL_HOST` still wins, which it says on stderr. At a
  terminal: the host as a header, then `default  studio → bc` (each by
  its alias, else its name), `already` where it was the default and the
  last host both. `bmdctl` marks the default: the host a command naming
  none goes to now (`$BMDCTL_HOST`, else the last `--host`, else
  hosts.yml's `default`), not hosts.yml's `default` alone, which a
  `--host` since may have left behind; the pipe's `(default)` and
  `--json`'s `default` mean the same. Before `use`, they marked
  hosts.yml's.
- **Which device.** A device word on the line (`bmdctl atem ...`) wins;
  else a named host's `device`; else ATEM, so a bare address is an ATEM
  (except `connect ADDRESS`, which asks: Device probe, below). A device
  word that disagrees with the named host's type is refused (`deck
  (192.0.2.20) is a HyperDeck, not an ATEM`). A command the host's device
  hasn't but another has says what the host is: `DECK 1 (192.0.2.20) is a
  HyperDeck; bmdctl hyperdeck --help`, the step dim under it at a
  terminal; a host of a device with no commands (a Videohub, an Ultimatte
  until phase 5) says `...; bmdctl has no Videohub commands yet`.
  Device words are reserved: the ATEM command that binds a HyperDeck slot
  is `hyperdeck-slot`, not `hyperdeck`, which is the HyperDeck device
  (phase 4); a test keeps every command's first word off the device
  names.
- **`hosts discover`** (`internal/mdns`, `cmd/bmdctl/discover.go`) adds
  every switcher that announces itself within `--for` (2 s), by its own
  name. ATEM switchers announce DNS-SD `_switcher_ctrl._udp` with the
  name set in ATEM Setup as the instance name, the service ATEM Software
  Control browses; WebATEM's `discovery.py` (atem-web-control) found it
  on a VLAN of 250 switchers, and found `_blackmagic._tcp` to be the
  other Blackmagic devices', which bmdctl leaves alone until it has
  commands for them (phase 4). The standard library has no mDNS, so
  `internal/mdns` (MIT) sends one PTR query to 224.0.0.251:5353 from an
  ephemeral port (RFC 6762 §6.7, a "legacy unicast" query: responders
  answer that port, so a system responder holding 5353 doesn't matter),
  collects answers, asks once more halfway for any instance's missing
  SRV, TXT or A, and parses compressed names with a bound on pointers.
  An instance's address is its SRV target's A record, else the
  responder's. One switcher seen twice is one (TXT `unique id`, else the
  address). Discovery opens no session: the next command to each
  switcher checks the name, as for any host. A switcher at a host's
  address with a new name renames it; one whose name another host goes
  by is skipped, saying so. Tests replace the browse (`cli.discover`) and
  give `mdns.Browse` an in-memory `PacketConn`; nothing leaves the
  machine.

## Device probe (`internal/engine/probe.go`, `atem/probe.go`, `cmd/bmdctl/connect.go`)

Phase 4: `connect ADDRESS` (and `hosts add ADDRESS`) find out what is
there instead of taking every address for an ATEM.

- **Four questions at once, 2 s at most** (`ProbeTimeout`): an ATEM's
  UDP handshake on 9910, a HyperDeck's greeting on TCP 9993 (`500
  connection info`), a Videohub's preamble on TCP 9990 (`PROTOCOL
  PREAMBLE:`, then its `VIDEOHUB DEVICE:` block, read to its blank line,
  as videohubwire's `_read_preamble` reads it) and an Ultimatte's prelude
  on TCP 9998 (to `END PRELUDE:`, as ultimattewire's `read_preamble`
  reads it). The first to answer wins; nothing answering in time is
  `unreachable` (exit 2), the message naming the four ports. An address
  with a port is asked each way on that port; there the Videohub's
  device block, or the Ultimatte's `Label:`, must be in what is sent,
  since both start `PROTOCOL PREAMBLE:` and only the port tells them
  apart otherwise.
- **The ATEM's question** (`atem.Probe`, `Dialer.Probe`) leaves nothing
  open on the switcher. It sends the SYN Dial sends; on the switcher's
  acceptance (hello 0x02) it completes the handshake with Dial's ACK,
  takes the session id from the switcher's first packet (the dump's
  start) and says goodbye under it (SYN, hello 0x04), as Close does; a
  refusal opens nothing. A handshake left half done (as the first form
  of the probe left it, never ACKing the reply) would hold one of a
  switcher's few client slots until it timed out. Handing the completed
  handshake to the session connect opens next was the alternative; the
  engine's dial and the probe are separate requests, possibly far apart,
  so the probe closes its own. atemwire's `probe()` connects fully
  through the pool, which takes the whole dump. The fake switcher's
  `Dangling` says whether a SYN it answered was never completed or a
  session never said goodbye to, and the probe's test requires neither.
  Not yet seen on hardware.
- **Names, where the probe can ask:** a HyperDeck's from `device info`
  (its `name`, else its model; the deck is dialled `Bare` and closed),
  a Videohub's `Friendly name` (model `Model name`), an Ultimatte's
  `Label` (videohubwire and ultimattewire read the same fields). An
  ATEM's needs its state: the connect that follows has it, as before.
- **In the engine,** as the engine request `probe` (`device: engine`),
  which answers whatever the client's build: the engine is what reaches
  devices, and may run where the CLI's network isn't the same.
  `Config.ProbeATEM` and `Config.ProbeDial` are its two ways of asking;
  tests point them at `internal/fakenet`, a network of the fakes
  (`fakeatem` for the handshake, `fakehyperdeck`, and the Videohub
  preamble and Ultimatte prelude of videohubwire's and ultimattewire's
  tests), so nothing leaves the process.
- **The CLI's connect** (`connectCommand`): the device is the line's
  device word, else `--type` (connect's parameter: atem, hyperdeck,
  videohub or ultimatte, which also retypes a host already at that
  address), else the type hosts.yml has for the address, else what the
  probe says. A device with commands runs its connect as before, and
  `remember` saves the host as that device. A Videohub or Ultimatte,
  which bmdctl has no commands for until phase 5, is saved by the name
  it gave (the address, where it gave none) and made the default; a
  terminal shows the header (its name bold, its model and what it is
  dim), `saved as "Test Hub", now the default` and, dim, `bmdctl has no
  Videohub commands yet`; a pipe one line; `--json` the probe's result
  with what was saved. At a terminal the spinner says `asking ADDRESS
  what it is`; nothing answering is `error  nothing answered at ADDRESS
  within 2s`, with `bmdctl connect ADDRESS --type DEVICE connects
  without asking` under it (the ports are the pipe's message).
- **help** lists the device groups hosts.yml has a host of (`atem`,
  `hyperdeck`, ...), each with what it does, then one dim line naming
  the others (`also videohub, ultimatte (none in hosts.yml)`; with no
  host yet, all four).
- **Not verified on hardware:** any of it. The HyperDeck's greeting is
  hyperdeckwire's hardware-verified form; the Videohub's and Ultimatte's
  are their libraries' test samples.

## Alias binaries (`cmd/bmdctl/alias.go`, `.goreleaser.yml`, `install.sh`)

Phase 4: one binary, five names. Run as `atemctl`, `hdctl`, `vhctl` or
`ultctl` (argv[0]'s base name, case and `.exe` ignored for Windows),
bmdctl is `bmdctl <device>`: atem, hyperdeck, videohub, ultimatte.

- **The device word goes first** (`aliasArgs`): global flags are read
  anywhere, so `hdctl --json play` is `bmdctl hyperdeck --json play`. The
  device's own word is allowed again (`hdctl hyperdeck stop`); another
  device's is refused (`atemctl runs ATEM commands; bmdctl hyperdeck ...
  runs a HyperDeck's`). Built-ins (version, engine, hosts, exec,
  completion, man, doctor) are bmdctl's, unchanged. The engine an alias
  starts is the same executable (a symlink resolves to bmdctl; a Windows
  copy is the same build), so it isn't a stale one.
- **Help is the device's alone:** the alias with nothing but flags, with
  `help` or `--help`, prints that device's command list, its usage lines
  and examples under the alias's name (`usage: hdctl <command>`, `hdctl
  play --speed 200`). vhctl and ultctl say there are no commands yet.
- **Completion** scripts register all five names and ask the name typed
  (`"${COMP_WORDS[0]}" __complete ...`), which puts its device word in
  front.
- **Shipping.** GoReleaser's before hook makes `build/aliases/<name> ->
  bmdctl` symlinks, and the macOS and Linux archives carry them as
  symlinks (GoReleaser 2.18 keeps a symlink a symlink in a tar.gz, checked
  with a snapshot). Windows can't, so four Windows-only builds of the
  same main under the alias names put copies in the zip. `install.sh`
  links them beside bmdctl (`ln -sf bmdctl`).
- **Tests.** `alias_test.go` runs alias lines against the bench and,
  through `main`, a symlink named hdctl to the test binary;
  `internal/release` runs `install.sh` against a release served on
  loopback (its archive, checksums.txt, /latest's redirect).

## serve (`internal/serve`, `cmd/bmdctl/serve.go`)

Phase 4: the command table over HTTP/JSON. `bmdctl serve [--listen
ADDRESS]` runs it in the foreground, its requests sent to the engine
over its socket (started if none runs); `bmdctl engine --serve [--listen
ADDRESS]` runs it in the engine's own process, which sends them over
the same socket, for the container. Off unless asked for.

- **Routes** (Go's `net/http` patterns): `GET /hosts` (hosts.yml, as
  `bmdctl hosts --json` prints it), `GET /hosts/{host}/state` (`?path=`
  as `state --path`), `GET /hosts/{host}/events`, `POST
  /hosts/{host}/{command...}`, `GET /openapi.json`, `GET /` (where the
  rest are). Anything else is a 404 in the error shape.
- **The host** is resolved as `--host` is (a name in any case, the start
  of one, `was`, an address, a DNS name); its device type says which
  device's command the path names. The device's word may lead the path
  (`/hosts/x/hyperdeck/play`), as on the command line; one that disagrees
  with the host's type is refused, and a command the host's device lacks
  but another has says whose it is.
- **A command's path is its words**, not its arguments: `POST
  /hosts/studio/usk/on-air` with `{"keyer": 1, "on-air": true}`. The body
  is a JSON object (or nothing): strings as typed, numbers in their
  shortest form (`json.Number`, so 12 is "12"), booleans true or false,
  arrays joined with commas (lists and flags), null left out; an unknown
  name is a usage error, as an unknown flag is. A command that sends a
  file takes `multipart/form-data`: the file's part named for its
  parameter (its file name standing for the path the CLI would send),
  the other arguments as fields. A stream (watch) is `GET .../events`.
  The CLI's global flags are the query: `?yes=true`, `?timeout=2s`,
  `?fps=25`; anything else there is refused, so an argument can't be put
  in the wrong place unnoticed.
- **The answer is the engine's response, as `--json` prints it**, byte
  for byte where nothing is added, with an HTTP status for its code:
  200, usage 400, destructive 409, rejected 422, no_echo 504,
  unreachable, engine and stale 502. Two keys are added after the
  engine's: `files`, what a command writes (`media download`, `profile
  save`: param, name and base64 data, the frames the CLI writes to
  disk), and `confirm`, what a destructive command refused for want of
  `?yes=true` would affect (the engine's confirm note: the request asks,
  as a terminal does).
- **Events** are server-sent: each of watch's frames, exactly as
  `--json` prints it, a `data:` line each, the first
  `{"kind":"watching"}`, a comment every 15 s to keep proxies from
  closing an idle stream. The query is watch's arguments (`count`, and
  an ATEM's `filter` and `all`). The client hanging up cancels the
  engine's watch.
- **OpenAPI 3.1, from the table:** a POST per command that isn't a
  stream, at `/hosts/{host}/<words>` for an ATEM's and
  `/hosts/{host}/hyperdeck/<words>` for a HyperDeck's (both forms work
  for a deck host; the document gives each command one path), its body
  the entry's `InputSchema` (`internal/table/schema.go`, which mcp uses
  too: a JSON Schema per parameter kind with its range, from 1 for an
  index, and a description saying help, range, unit and default),
  `multipart/form-data` where a file is sent, `x-destructive` and
  `x-unverified`, and the entry's summary, usage and examples. The
  ATEM's `state` command shares its path with the GET.
- **No authentication.** It listens on 127.0.0.1:8880 unless `--listen`
  says otherwise, as WebATEM does; a terminal says there is none, and
  an address other machines can reach is said on stderr each time.
  Requests are logged one line each on stdout (method, path, status,
  time; at a terminal in the result's columns), not with `--quiet` or
  `--json`, where only the address is printed.
- **Tests.** `cmd/bmdctl/serve_test.go` drives every route on loopback
  against the bench's engine (writes, refusals and their statuses, a
  destructive command's confirm, an upload as multipart, a download's
  file, events, a deck's commands, `bmdctl serve` itself);
  `internal/serve` checks the OpenAPI document (every command a path,
  unique operation ids, typed properties) and the value conversion.

## mcp (`internal/mcp`, `cmd/bmdctl/mcp.go`)

Phase 4: `bmdctl mcp`, a Model Context Protocol server on stdin and
stdout (newline-delimited JSON-RPC 2.0; logs on stderr), its requests
sent to the engine as any command's are (started if none runs). How to
add it to Claude Code and Claude Desktop is docs/AGENTS.md.

- **Both eras of the protocol** (checked against the specification at
  modelcontextprotocol.io in October 2026): the current revision,
  2026-07-28, has no handshake (each request's `_meta` carries
  `io.modelcontextprotocol/protocolVersion`, `server/discover` is
  mandatory, results carry `resultType: "complete"` and list results
  `ttlMs` and `cacheScope`, and subscriptions are a
  `subscriptions/listen` stream); the handshake revisions 2025-11-25 back
  to 2024-11-05 open with `initialize` and subscribe with
  `resources/subscribe`. The server is dual-era, as the specification
  allows: a request with the modern `_meta` is served by the current
  revision (another version is `-32022` with the versions supported), an
  `initialize` negotiates the client's handshake version (or 2025-11-25),
  and either may be used on one stdio stream. Which era Claude Code and
  Claude Desktop open with is theirs to choose; both work.
- **A tool per command group**, fifteen in all: each device declares
  its groups (`atem.Groups`, `hyperdeck.Groups`: a name, a summary, the
  order a reader meets them) and every entry names its own
  (`Entry.Group`, by its first word through `GroupOf`; `Registry.Groups`
  and `GroupEntries`), so the tools come from the table and a new
  command joins its group's tool. The ATEM's twelve are switching
  (program, preview, cut, auto, tbar, undo, aux, ftb), transition, usk,
  dsk, audio (with classic-audio), media, macro, profile, sources
  (inputs, input, color, camera, hyperdeck-slot), outputs (multiviewer,
  supersource, recording, streaming), system (info, video modes,
  startup, time) and session (connect, disconnect, status, state, show,
  watch); a HyperDeck's two are transport and session. A tool is named
  `<device>_<group>` (`atem_switching`, `hyperdeck_transport`; at most
  64 characters, unique, checked), titled `ATEM switching`; it takes
  `subcommand` (required, an enum of the group's commands less the
  group's own word: `wipe` in transition, `ftb toggle` in switching),
  `host` (a host's name, the start of one or an address; the default
  host left out), the union of its subcommands' parameters (each
  `InputSchema` property, shared with serve, its description prefixed
  `for wipe, dve:` where not every subcommand takes it, and saying which
  require it), `confirm` where a subcommand is destructive or can be,
  and `seconds` where one streams. Its description is the group's
  summary and a line per subcommand: summary, usage, and whether it is
  destructive, unverified or streams. A tool per entry, 122 of them,
  costs a model's context more than it gives and is harder to choose
  from than fifteen groups; before the first release this replaced
  that, and `get_state` and `list_inputs`, which are now `atem_session`
  `state` and `atem_sources` `inputs`; `list_hosts` stays, and
  `wait_for_change` came after (below).
- **The schema is flat, the subcommand its discriminator.** A
  discriminated union (`oneOf` of one object per subcommand) would say
  exactly which arguments each takes, but Claude's API accepts no
  `oneOf`, `anyOf` or `allOf` at the top of a tool's `input_schema`,
  and clients differ in what they pass through. So the top level is one
  object, `additionalProperties: false`; a parameter whose schema
  differs between subcommands (`slot`'s range in media, `source` in
  transition) is an `anyOf` of its schemas, each saying whose it is, at
  the property, which every client takes. The exact check is the
  table's: the call is validated against the chosen subcommand's
  entry, and an argument it doesn't take is a tool error naming what it
  does take (`atem_switching program takes no "nonsense"; it takes me,
  source`), as is an unknown subcommand.
- **Annotations are set explicitly**, since the protocol's defaults call
  every tool not marked otherwise destructive: `readOnlyHint` for the
  groups whose every subcommand reads (atem_session and
  hyperdeck_session: status, state, show, watch, connect and disconnect
  change nothing on a device), `destructiveHint` exactly for the groups
  holding one of the table's destructive entries (system: video mode,
  startup save and clear; media: both still clears; macro: delete;
  profile: restore), `idempotentHint` for the reading groups,
  `openWorldHint` false (the devices bmdctl knows, not the internet).
  The per-entry read-only list (`readOnly`) still decides a result's
  status: done for a read, confirmed, sent, already or rejected for a
  write.
- **A result says whether anything changed.** Its structured content
  (and, for clients that read only text, the same JSON in a text block)
  is `{ok, status, confirmed, result, took_ms, device, changed,
  written}`: `status` confirmed (the device reported it back), sent (no
  confirmation exists for it), already (the device held it; nothing was
  sent), rejected (it confirmed but kept its value) for a write, done
  for a read; `changed` the engine's values note, `{param, before,
  after}`; `result` the engine's, as `--json` has it. A failure is a
  tool error (`isError`, `{ok: false, status: "error", code, error}`),
  which a model can act on; an unknown tool or malformed call is a
  protocol error.
- **Destructive subcommands ask for a person.** Without `confirm: true`
  the request asks (`Ask`), the engine refuses it with its confirm note,
  and the result says what it would affect and that the operator must
  agree before it is called again with `confirm: true` (`Not done: this
  is destructive. video mode 1080p50 → 525i5994: every output resyncs.
  ...`). The same holds for what is destructive only by its arguments:
  `media upload`, `macro upload` and `macro record` over a used slot,
  `macro action` delete (each says so on its line). `confirm` is the
  CLI's `--yes` (`Yes`), named for the agent's side of it; on a
  subcommand that never asks it is ignored. The skill (skills/bmdctl)
  tells the agent never to pass it on its own.
- **Files are this machine's**, as the CLI's are: a file a tool sends is
  read from the path given (relative to the server's working directory),
  one it produces written where its `file` or `dir` says (`outPath`, the
  CLI's), the paths in `written`. `-` is refused: stdout carries the
  protocol.
- **Watching.** Each host's state is a resource,
  `bmdctl://hosts/{host}/state` (`resources/list` from hosts.yml, a
  template for any address; `resources/read` is `state`). Subscribing
  (`resources/subscribe`, or `subscriptions/listen` with
  `resourceSubscriptions`) runs the engine's watch for it and sends
  `notifications/resources/updated` on each change, at most one per
  250 ms (an ATEM's changes come in bursts); the client reads the
  resource for what changed. A modern subscription is acknowledged once
  its watches run, every notification carries its id, and one the client
  cancels gets nothing more. The `watch` subcommands of `atem_session`
  and `hyperdeck_session` collect a stream's changes for `seconds` (5 by default, at most 60) or
  `count` (20) and return them.
- **wait_for_change** (#2; `wait.go`): a long poll, for an agent that
  can't take notifications. The first agent in the ninth hardware run
  ran `bmdctl watch --filter PrgI` in a background shell and lost
  control of it, because every tool returned at once and the `watch`
  subcommand's `seconds` lose whatever changes between two calls. The
  first call for a host starts the engine's watch for it (by device and
  address) and waits for its `watching`, as a subscription does; from
  then each change goes into that host's log, numbered from 1, the last
  1000 kept. A `cursor` is the log's id and a number
  (`mg9x2k1q1-42`), opaque to the agent: a call returns the changes
  after it that pass `filter`, at most 100 (`more: true` past that),
  waiting up to `timeout_s` (0 to 60, default 30) for the first and
  then 250 ms more for those right behind it (a cut is several packets;
  a deadline still ends it), and the cursor of the last change it
  looked at, those `filter` left out included, so they aren't looked
  at again. No cursor starts from now, so `timeout_s: 0` is how to
  mark a place before acting. A cursor of another log (the server
  restarted, the watch idled out or ended) or older than the log keeps
  says `missed: true` and starts from what there is. `filter` is
  `watch --filter`'s prefix match, done here, since one watch serves
  every call; `disconnected` and `ready` always pass. The watch runs
  without `--all`, so Time and the index and Fairlight tallies never
  come, filter or not. It stops ten minutes after the last call ends
  (`waitIdle`), so the session it holds in use can idle out; one that
  ends (the session closed) fails the calls waiting on it with its
  reason, and the next call starts another. The changes are the
  engine's `ChangeEvent`s as `watch --json` has them, in `events`, not
  also in `result` as the `watch` subcommand doubles them: an agent
  polling in a loop pays for every byte. A separate tool, not a
  subcommand of `atem_session`: subcommands are table entries, and a
  cursor is the MCP server's state, not a command's; one tool serves
  both devices.
- **Concurrency.** Each request runs on its own goroutine, answers are
  written whole under a lock, and `notifications/cancelled` cancels the
  request it names (a stream, a transfer).
- **Tests.** `cmd/bmdctl/mcp_test.go` drives `bmdctl mcp` over pipes
  against the bench's engine: the handshake, the tool list (about
  fifteen; names, schemas, annotations), confirmed, already and sent
  writes, a destructive refusal and then `confirm`, an upload over a
  used slot both ways, reads, `list_hosts`, a deck's tools and a
  switcher's tool sent to a deck, files both ways, unknown arguments and
  subcommands, resources and both kinds of subscription, the modern
  revision's discover, `resultType`, version error and cancellation, and
  `wait_for_change` from a cursor over a write, on a switcher and a
  deck. `internal/mcp` drives `wait_for_change` against a fake watch
  (`wait_test.go`: cursors, waking, the burst, filters, missed and more,
  a watch that ends or can't start, the idle stop, arguments), and checks that every entry is a subcommand of exactly one
  tool and in a group its device declares, that no schema has a
  top-level `oneOf`, `anyOf` or `allOf`, and that `destructiveHint` and
  `confirm` go with the destructive groups.
- **Verified from Claude Code** (the ninth run, docs/HARDWARE.md): v0.1.1
  on a Mac, added with `claude mcp add bmdctl -- bmdctl mcp`, listed the
  fifteen tools; a read of the state matched the terminal, and `program`
  through `atem_switching` (MP1 → Cam7) answered confirmed. Claude
  Desktop hasn't been tried, nor the state subscription from either. No
  tool waited for a change, so an agent that had to watch ran `bmdctl
  watch` in a shell instead (issue #2, since answered by
  `wait_for_change`, not yet tried from Claude Code).

## The Claude skill (`skills/bmdctl/SKILL.md`)

Phase 4: what an agent needs to run a live show with bmdctl, in under
150 lines (the owner's limit; `TestSkillCommands` enforces it): when to
use bmdctl; read the state before writing, and say what is on air before
a cut or an auto; how results read (confirmed, sent, already, rejected;
`--json`'s shape; exit codes); the destructive commands, and that the
agent asks the operator and never passes `--yes` (`confirm: true`) itself, one
yes per command; sequences through `exec`, whose lines are all checked
before any is sent; five recipes in the owner's order (load and key a
graphic, a pre-show reset, a scheduled content change, tally-driven
routing, a HyperDeck playback cue); how to read `watch --json`; caution
on models nobody has tried; COMMANDS.md for everything else. It speaks
CLI lines; an MCP tool is the line's group with the rest as
`subcommand` (`bmdctl dsk on-air on` is `atem_dsk {"subcommand": "on-air",
"on-air": true}`). Its description is
under 200 characters, claude.ai's limit for an uploaded skill.
`TestSkillCommands` runs every line of its code blocks through the checks
a typed line gets, so a renamed flag fails the build rather than misleads
an agent. docs/AGENTS.md says how to install it.

## Documentation site (`mkdocs.yml`, `docsite/`, `docs/`)

- **MkDocs Material, built from `docs/` as it is.** The docs stay plain
  Markdown that reads on GitHub; no file moved or was renamed. The hooks
  in `docsite/hooks/site.py` make the files a site: a top-level page
  named in capitals is served in lower case (`INSTALL.md` at `/install/`,
  `GUIDE.md` at `/guide/` with the guide's pages under it); a
  `--8<-- "FILE:section"` line includes a root file's text (the README's
  introduction and sample on the home page, all of `CONTRIBUTING.md`)
  with its relative links rewritten for the page; a link to a file the
  site doesn't build (Go source, the licences, and the records it leaves
  out: PORTING.md, the SURFACE copies, BRAND.md) goes to GitHub.
- **The guide is a page per section** (`docs/guide/`), `GUIDE.md` its
  index, so each task has its own URL. Its MCP section joined
  `docs/AGENTS.md`, which had its own copy of the Claude Code and Desktop
  setup; the full path for Claude Desktop is the guide's advice.
- **Terminal samples are coloured when the site is built**
  (`docsite/hooks/terminal.py`): a code block that opens with `$ ` is a
  sample, plain in the Markdown (GitHub can't colour it), and the hook
  puts back the CLI's colours by the CLI's rules ("Terminal").
  `docsite/test_terminal.py` takes the tags out of every terminal golden
  and requires the hook to put them back as they were, so the site can't
  colour a line as bmdctl doesn't. Panels are drawn as
  `docs/terminal-mock.html` draws a terminal, 72 columns wide.
- **Colours are BRAND.md's.** Program is `--pgm` `#D62718`, not
  terminal-mock.html's `#FF4A3A`; on the `#141618` panel that is 3.6:1,
  under WCAG AA's 4.5:1 for text of this size (the mock's is 5.4:1).
  The brand was kept; changing it is the owner's call. Text is IBM Plex
  Sans (BRAND.md names no text face; it is Plex Mono's family), code
  Plex Mono. The header is the lockup, `docs/brand/bmdctl-lockup-dark.svg`,
  dark in both schemes; the light scheme is BRAND.md's light column,
  and where it has none (a surface, captions) its text colour faded.
- **The quick start's output is the CLI's.** `TestTerminalQuickstart`
  runs its five commands against a fake switcher named STUDIO A (the
  fixture with a `WhoI`, black and inputs 3 to 7 named, at 1080p25) and
  checks `docs/quickstart.md`'s samples against `quickstart.golden`,
  times aside. The guide's samples stay hand-written.
- **Every command line in the site's pages passes bmdctl's checks**
  (`TestDocsCommands`, sharing `TestSkillCommands`' checker): prompted
  lines in samples, shell blocks' commands, and `bmdctl ...` spans in
  the text that aren't syntax. HARDWARE.md is left out: a log, in the
  syntax of its day, never rewritten.
- **Lists nest two spaces deep**, as GitHub reads them
  (`mdx_truly_sane_lists`; Python-Markdown wants four), so HARDWARE.md
  keeps its shape without being touched. An item holding more than one
  block is set off from the next by a blank line: without one,
  Python-Markdown runs its last paragraph into the next item.
- **Pinned** (`docsite/requirements.txt`): mkdocs 1.6.1 (2.0 drops the
  plugins and theme overrides the site uses, as Material's own notice
  says), mkdocs-material 9.7.7, pymdown-extensions 12.2,
  mdx_truly_sane_lists 1.3. Dependabot updates them, mkdocs held below 2.
- **Publishing**: `.github/workflows/pages.yml` regenerates the reference,
  builds with `--strict` and deploys with `actions/deploy-pages` on a
  push to main. The deploy is skipped while the repository is private
  (no Pages site to deploy to); once it is public, Settings > Pages >
  Source: GitHub Actions, once. CI's `docs site` job builds with
  `--strict` on every push.

## Release (`.goreleaser.yml`, `install.sh`, `scoop/bmdctl.json`, `Dockerfile`)

Phase 4. Pushing a `v*` tag runs `.github/workflows/release.yml`
(GoReleaser v2); the owner tags. `goreleaser release --snapshot --clean`
builds everything into `dist/` without publishing.

- **Binaries:** macOS arm64 and amd64, Linux amd64 and arm64 (build
  `bmdctl`), Windows amd64 (`bmdctl-windows` and the four alias builds),
  static (`CGO_ENABLED=0`), `-trimpath`, stripped. `main.version` is the
  tag (`bmdctl version` says `bmdctl v0.1.0 (<commit>, <date>)`), a
  snapshot's own version for a snapshot; `main.commit` and `main.date`
  as before.
- **Archives:** `bmdctl_<os>_<arch>.tar.gz` (`.zip` on Windows), the
  same name in every release, so `releases/latest/download/<name>` is
  always the latest release's (the Scoop manifest's url); the tag is in
  the release's URL, and `bmdctl version` says it. Each holds bmdctl,
  the alias links (copies on Windows), README.md and the licences.
  **Checksums:** `checksums.txt`. (They were named with the version
  until the Scoop change below, before any release.)
- **deb and rpm** (nfpm): bmdctl in `/usr/bin` with the aliases linked to
  it, the man page (`bmdctl man`, made by a before hook) and the bash,
  zsh and fish completions (bash's linked for each alias), the licences
  under `/usr/share/doc/bmdctl`. The maintainer is
  `Lucas Romanenko <lucas@romanenko.com>`, the owner's choice (it was
  the GitHub no-reply address until they named one).
- **The image**, `ghcr.io/lucas-romanenko/bmdctl` (linux amd64 and arm64,
  `<version>` and `latest`), is scratch with the binary, `HOME`,
  `XDG_CONFIG_HOME` and `XDG_STATE_HOME` under the `/data` volume (scratch
  has no home), the socket in the `/run/bmdctl` volume, and `engine
  --serve` as its command: the engine in the foreground with the HTTP
  API on 127.0.0.1:8880, for `--network host`.
- **Scoop:** no bucket repository, and no step of the release writes
  one: GoReleaser's `scoops` publish committed the manifest to main at
  each release, which the owner dropped before v0.1.0. `scoop/bmdctl.json`
  is written once: its url is
  `releases/latest/download/bmdctl_windows_amd64.zip`, with no hash
  (only a release knows it, and the url's archive changes with each),
  and its version `nightly`, which is how Scoop is told the url isn't
  pinned (checked against Scoop's source, `lib/install.ps1` and
  `libexec/scoop-update.ps1`, October 2026): it skips the hash check
  rather than warn that none is given, installs as `nightly-<date>`, and
  `scoop update bmdctl` fetches the latest again once the date has
  changed. A fixed version (`0.1.0`) with that url would be wrong after
  the next release and never update: Scoop compares the manifest's
  version, and its cache would hand back the old archive. The download
  is HTTPS from GitHub's release, which is what the hash would have
  vouched for. `checkver` (the GitHub release) and `autoupdate`
  (`releases/download/v$version/bmdctl_windows_amd64.zip`, its hash
  from that release's `checksums.txt`) are there so a bucket can pin a
  release with Scoop's own `checkver.ps1 -u`. No Homebrew tap.
- **install.sh**, for macOS and Linux, is POSIX sh: it maps `uname -s`
  and `uname -m` to the archive's names, takes the latest release's tag
  from the `/releases/latest` redirect (no API call, no rate limit) or
  `BMDCTL_VERSION`, downloads the archive and `checksums.txt` from that
  tag's release (not `latest/download`, so a release published between
  the two downloads can't pair one's archive with the other's checksums) with curl
  or wget, refuses an archive whose SHA-256 (sha256sum or shasum)
  doesn't match, installs to `BMDCTL_INSTALL_DIR`, else
  `/usr/local/bin` as root and `~/.local/bin` otherwise, by rename so a
  running bmdctl keeps its file, links the aliases, says when the
  directory isn't on PATH, and ends `next: bmdctl connect <ip>`.
  `BMDCTL_BASE_URL` points it elsewhere (a mirror, the tests).
- **The dry run before v0.1.0** (`goreleaser release --snapshot --clean`,
  GoReleaser 2.18.3): every artifact built. `bmdctl version` ran from the
  Linux amd64 archive and its aliases, from the amd64 deb installed in
  Debian and the amd64 rpm in Fedora, from the arm64 archive, deb, rpm and
  image under QEMU's user mode, from the amd64 image (whose `engine
  --serve` answered `/`, `/hosts` and `/openapi.json` on loopback), and
  from the five Windows executables under wine. The macOS binaries were
  checked to be Mach-O for their architecture with the version stamped,
  but nothing here could run them (one has since run on a Mac: below).
  A tagged build in a throwaway clone printed `bmdctl v0.1.0`.
  `internal/release` tests install.sh against a release served on
  loopback, and the manifest against the config.
  After the Scoop change it ran again (`--skip=docker`): the archives
  are `bmdctl_<os>_<arch>`, no manifest is written, the deb's and the
  rpm's maintainer is lucas@romanenko.com, the Windows zip holds the
  five executables at its top (the manifest's `bin`), and install.sh
  installed bmdctl and its aliases from the Linux amd64 archive and
  `checksums.txt` served on loopback.
- **On a Mac** (the ninth run, docs/HARDWARE.md): v0.1.1's darwin_arm64
  binary ran `version`, `connect`, `status`, `inputs`, `watch` and `mcp`
  against the Constellation over a VPN. The darwin_amd64 binary hasn't
  been run.

## Engine (`internal/engine`)

A background process holding one `atem.Session` per switcher, so a
command costs a round trip instead of a connect and a whole state dump,
and the switcher's live state is at hand to read a command line. It
replaces atemwire's connection pool (`acquire_connection`, reference
counts, a 2 s grace timer) with the policy phase 3 asked for: a session
lives until 10 minutes pass without a request, or an hour if pinned
(#7; until then a pin lasted until disconnect).

- **Where.** `$BMDCTL_SOCKET`, else `$XDG_RUNTIME_DIR/bmdctl.sock`, else
  `bmdctl-<uid>/bmdctl.sock` in `$TMPDIR` or `/tmp` (macOS and most
  containers have no runtime dir). The directory is created 0700 and
  refused if another user owns it; the socket is chmod 0600. A lock file
  beside it (`bmdctl.sock.lock`, `flock` where the platform has it) keeps
  two engines off one path; a socket left by a dead engine is removed. On
  Windows it is the named pipe `\\.\pipe\bmdctl-<user>`, created with
  `FILE_FLAG_FIRST_PIPE_INSTANCE` (a second engine fails), a DACL granting
  only the user and SYSTEM, and `PIPE_REJECT_REMOTE_CLIENTS`, through
  kernel32 and advapi32 directly: the standard library has no pipe API.
  The Windows code is vetted with `GOOS=windows` but has never run.
- **Starting.** A command that finds no engine runs
  `bmdctl engine --background` detached (its own session, standard
  streams on `/dev/null`, working directory `/`; on Windows
  `DETACHED_PROCESS`, leaving the job object where allowed) and retries
  for 5 s. Two commands racing both spawn; the loser finds the lock taken
  and exits quietly. `bmdctl engine` runs one in the foreground for
  systemd or Docker, logging to stderr as well. `bmdctl engine stop` asks
  it to stop and waits for the socket to go; `bmdctl engine status`
  reports its version, build, pid, socket, log and sessions.
- **An engine of another build restarts** (after the second hardware
  run, when an engine kept serving the previous binary's table). Every
  request carries the client's build (`build`), the Go toolchain's build
  ID of the executable (what `go tool buildid` prints: from the ELF note,
  or the marked string near the start of a Mach-O or PE file), or a
  SHA-256 of the file when it has none; it changes whenever the code
  does, and costs a few header reads, not a hash of the binary, on every
  command. The engine runs with its own (`Config.BuildID`). A background
  engine, one a command started (`Config.Background`), answers a device
  request from another build `{ok:false, code: "stale"}` and stops; the
  CLI waits for its socket to go, starts an engine (its own executable,
  so its own build) and sends the request again, once: a second `stale`
  is printed and exits 2. An engine run by hand (`bmdctl engine`, under
  systemd or in a container, where the host's CLI may be another build
  on purpose) answers every build: stopping it would leave the CLI
  starting engines of its own beside it. Requests to the engine itself
  (`status`, `stop`) are answered whatever the build, so `engine status`
  shows an engine's build, marked when it isn't this bmdctl's, and
  `doctor` warns. Completion sends its build too, so a Tab can stop a
  stale engine, but never starts one. An engine that is stopping holds
  its lock a moment after its socket has gone, so a background start
  that finds the lock taken and nothing answering retries for up to 2 s.
  The protocol version stays 1: `build` is optional, and an engine from
  before it (which ignores the field) has to be stopped by hand once.
- **Protocol.** One request per connection, one JSON object per line. In:
  `{v, device, host, path, params, files, yes, fps, timeout_ms, build}`, `params`
  being the `--name value` pairs as typed. Out: `{ok, confirmed, result,
  took_ms}` or `{ok:false, code, error}`, exactly those keys, preceded by
  any `{"file": {param, name, data}}` frames. A streaming command sends a
  response per result and ends with a failure or when the caller hangs
  up. `v` is the protocol version (1); another is refused with code
  `engine`, so a CLI and an engine built from different code can't
  misread each other. The caller hanging up cancels its request.
- **Notes** (the UX pass). A request with `notes` also gets `{"note":
  {...}}` frames before its response, each with one of: `connecting`
  (the session's key, when the request opens the session, waits for
  another request's dial, or waits for one re-handshaking), `switcher`
  (`engine.Switcher`: the name the switcher gives itself, which is WhoI's
  name, else `_pin`'s product name, as bmdwire v1.3.0's state.py
  `atem_name` is; product, video mode, pinned, how long the session had
  been idle before the request, how long it may stay idle after; every
  request that uses a session gets one), or `values` (a command's
  `{param, before, after}`, "Output conventions"). The CLI always asks
  and prints them only for people; `--json` prints response lines alone,
  as it skips file frames, so its output is unchanged. `Call` hands each
  frame to its callback as an `engine.Frame`. The protocol version stays
  1: an engine from before the field ignores it, and the CLI falls back
  to what responses carry. Two more (the terminal pass, "Terminal"):
  `on_air`, M/E 1's program and preview by id and short name as the
  request found them, sent once, after the `switcher` note and before
  the command runs, so a terminal's header shows what was live when it
  ran (until the seventh hardware run it was sent again once the
  command was done, and the CLI showed the second); and a value's
  `kept`, set when
  the switcher confirmed a command but answered with the value it
  already held, not the one sent. The engine's status carries `commit` too
  (`Config.Commit`), beside `build`. An `overview` engine request answers `{engine,
  switchers}`, the engine's status and a `Switcher` per session, for
  `bmdctl` alone; `status` is unchanged.
- **Files travel in the frames.** The engine may run where the caller's
  files aren't (a container, a service), so the CLI reads every `file`
  parameter and sends its bytes, and writes what comes back in file
  frames: to the `out-file` parameter's path, beside it for a named
  frame, or inside a `dir` parameter's directory. A frame name can't climb
  out (`..`, absolute). The engine reads and writes no file but its log.
- **Parsing happens in the engine**, against the session's state: the
  display rate for rates, the input names for sources. The CLI's own
  check beforehand only catches what needs no switcher.
- **Sessions** are keyed by `host:port`, lower-cased, port 9910 by
  default. Concurrent requests share one dial, made on the engine's
  context (`ConnectTimeout`, 15 s), not a request's, so one caller giving
  up doesn't fail the others. A session whose `Connected` has gone false
  is replaced on the next request; one that is re-handshaking after a
  silence is waited for (up to 15 s) before the state is read. The idle
  clock starts when the last request using a session ends.
- **Pinned sessions close too** (#7: an engine kept a pinned session to
  a studio switcher for two days, 41 hours after its last command, and
  was a second client nobody knew of during a hardware capture). A
  pinned session closes after `PinIdleTimeout`, an hour, without a
  request; `BMDCTL_PIN_IDLE` (read by `bmdctl engine` when it starts,
  `Config.PinIdle`) sets it: a duration or seconds, `0`, `off` or `never`
  for the old way. A bad value stops an engine run by hand and is logged
  (and the hour kept) by one a command started, which would otherwise
  fail every command. Idle means what it does unpinned: no request
  using the session, so a `watch`, or an MCP or HTTP subscription (each
  a `watch` request), keeps it open for as long as it runs, and its idle
  clock starts when it ends. `connect` pins and `connect --pin false`
  unpins; each change is logged (`pinned session`, `unpinned session`,
  with `closes_after_idle`), and so is an expiry, with how long it was
  idle and how old it was. The engine keeps its sessions' times by a
  `Clock` (`Config.Clock`; `internal/fakeclock` in tests, whose
  `AfterFunc` returns a stop func so it needn't import the engine), so
  a test runs an hour in a moment. A status says, per session, its age
  (`age_s`, by the engine's clock, since a CLI's may differ from a
  container's), how long it has been idle (`idle_s`, 0 while in use),
  how long until it closes (`idle_close_s`, pinned or not) and `stale`:
  idle past its timeout and not yet closed, as when the machine slept
  through the timer; the engine's status has `pin_idle_s` (0, never, as
  an engine from before #7 reports it). A note's `drop_s` is a pinned
  session's too, so `status` says `pinned, drops at 15:04 if unused`,
  and `connect` at a terminal `pinned until disconnect or 1h idle`. No
  maximum lifetime for the engine itself (#7 asked to consider one): a
  session that idles out is what held the switcher, and an engine with
  no sessions holds nothing.
- **Wire commands** are built and validated in the engine, then sent:
  with an echo key through `SendAndWait`, waiting `timeout_ms` (default
  5 s, `--timeout`), `confirmed` once it arrives; without one through
  `Send`, `confirmed` false, as atemwire's `send()` leaves it. The result
  is `{args, command, echo, state}`: the arguments for display, the
  opcode, the key waited for and the echoed packet.
- **A cut or an auto transition waits for what the switcher reports of
  it** (`call.effect`, after CI failed on every push from `90f3cdd`).
  Neither has an echo, so `Send` returned as soon as the packet was
  queued, and the next request (the next line of exec, an `undo`)
  could read the state from before the switcher's answer arrived: on CI's
  `go test -race` it usually had, locally it usually hadn't, and the
  goldens disagreed. Now the engine subscribes, sends, and waits up to
  `timeout_ms` for a cut's `PrgI` and `PrvI` on its M/E (when the next
  transition takes the background and program and preview differ; a cut
  of the keys alone is sent and left), or for an auto's first `TrPs` or
  `PrgI` (the transition starting, or the fake's, done at once). Then
  it answers `confirmed`, and the session's state, read on the session's
  goroutine, holds the whole packet. Not reported in time, it answers
  as before: sent, `confirmed` false, no error. `echo` stays empty.
  `ftb toggle`, `dsk auto` and the macro commands are still sent and
  left. atemwire's `send()` waits for nothing; this is the engine's,
  not the port's. Verified on hardware (the eighth run,
  docs/HARDWARE.md): on the Constellation a cut answered confirmed in
  30 ms, its `PrgI` and `PrvI` well inside the timeout, and an auto in
  29 ms, when the transition started rather than when it ended.
- **A write the switcher holds already sends nothing** (the terminal
  pass). Where a command's reader (notes.go, `readers`) holds every
  value it sets equal to the argument (an argument the echo key names,
  the M/E or keyer, says where, not what; one the reader doesn't know
  means it is sent), or an operation's before and after agree
  (`sendBuilt`: transition style, rate, next), the engine sends nothing
  and answers `confirmed` with `"already": true` in the result: the
  state the switcher reported holds the value. A terminal says
  `already` where the time goes. atemwire sends whatever it is asked
  to; this saves a video mode resync and an operator's double press.
- **Codes.** `usage` (exit 1), `unreachable`, `engine` and `stale` (2),
  `no_echo` (3), `rejected` (4), `destructive` (5). Session errors (`ErrClosed`,
  `ErrDisconnected`, `ErrNotReady`) are `unreachable`; a command that
  won't encode is `usage`.
- **Log.** `$XDG_STATE_HOME/bmdctl/engine.log` (`~/.local/state`;
  `%LocalAppData%\bmdctl` on Windows), slog text, one line per request
  and per session event, rotated at 5 MiB keeping three old files. The
  sessions log through it too, so `--log-level debug` shows the
  transport.
- **Stopping** closes the listener, cancels running requests and closes
  every session, which says goodbye to each switcher; it waits up to 5 s
  for requests to end. A connection accepted as it stops is closed
  unanswered: its request is counted (the WaitGroup's Add) under the
  engine's lock and only while not stopping, which Stop sets under that
  lock before it waits (`-race` found the two racing in phase 4).

## Operations (`internal/engine/ops_*.go`)

The commands that aren't one wire command. Each is a table entry with
`Operation` set and a function in the engine; the CLI only formats what
comes back. Indexes in results count from 1, as the flags do
(`table.Index`, above).

- **Group a, the session** (`ops_session.go`).
  - `status` (its `session` row, from the switcher note: idle before
    this request, or connected just now, and the time it drops unless
    pinned) reads the state through atemwire's readers: per M/E program
    and preview with input names, the transition (style, rate as
    seconds:frames, next layers, position, preview), fade to black and
    each keyer; each DSK, aux output, the macro player and recorder, and
    recording and streaming when the switcher has them. Streaming's
    `StRS` is named as SURFACE.md §2.5 observed it (on air is 4, not a
    bit).
  - `connect` opens the session and pins it (`--pin false` only opens
    it); `disconnect` closes it, pinned or not, and never dials one.
    `connect ADDRESS` (the terminal pass) goes to that address, as
    `--host` would, and the CLI then adds the switcher to hosts.yml by
    the name it gives itself, if no host has the address, and makes it
    the default and the last host.
  - `undo` (the terminal pass; bmdwire has none) puts back the last
    change this engine sent to program, preview, an aux output, or a
    keyer's or DSK's on air (`program`, `preview`, `aux`, `usk on-air`,
    `usk toggle`, `dsk on-air`, `dsk toggle`, `dsk auto`), or the last
    cut or auto transition (`cut`, `auto`): before each is sent, the
    value it replaces is kept, per session key, twenty deep, and pushed
    once it is sent; a change the switcher held already sent nothing and
    isn't kept. `undo` pops one and sends it back (a direct on-air for a
    DSK, not an auto), waiting for the echo; one that fails goes back on
    the stack. The stacks outlive sessions but not the engine. Nothing
    to undo is `{"undone": false}`, exit 0.
  - **A cut or an auto keeps both buses** (after the seventh hardware
    run, where `undo` after a cut said `nothing to undo`), and whether
    the next transition took the background, so swapped them. Its undo
    is a cut (`"what": "cut"` or `"auto"`, `"command": "DCut"`), which
    also takes back the keys the cut took on or off air, where program
    and preview are as it left them and the next transition still takes
    the background as it did. Where they changed since (from a panel:
    a change from here would be on the stack above it), program and
    preview are each set back instead (`CPgI`, `CPvI`, only those that
    differ). An auto is undone by a cut too, not another auto: undo
    puts the buses back at once. While the M/E is mid-transition undo
    refuses (`usage`) and keeps the entry, to try once it is done.
    Verified on hardware (the eighth run): undo straight after a cut put
    both buses back in 31 ms, and undo during an auto's transition was
    refused. An auto undone once its transition is done, and the path
    that sets each bus back, are not yet verified on hardware.
  - `show <area>` (the terminal pass) reads one area through the
    readers and returns it for the CLI to lay out: `keys` (each keyer's
    type, sources, on air, in the next transition, fly, mask, luma
    clip and gain), `dsk`, `transition` (the next one, each style's
    rate and settings, fade to black), `audio` (the Fairlight master and
    strips, or the classic mixer's master and inputs through the legacy
    readers, bmdwire v1.6.0: a media player is `MP<n>` and an external
    input is named by its source, where `InPr`'s name for the same id is
    the video source's, so 2001 was COL1), `media` and `macros` (as `media list` and `macro list
    --all`), `aux` and `hyperdecks` (the bound slots). `status` stays
    the short overview of them all. A DVE style atemwire's tables don't
    name (0-15) is `style N`.
  - `info` is identity and size: product and model, the name set in ATEM
    Setup, the firmware's protocol version, the video mode with its
    resolution and display rate, counts from `_top`, `_MeC` and `_mpl`,
    and whether the audio mixer is Fairlight or classic.
  - `state` prints the `State` tree as JSON, under atemwire's attribute
    names; `--path` follows dotted keys (list indices and map keys are
    numbers; a key that isn't there exactly is matched ignoring case),
    and an unknown key lists the ones there are. Humans get one
    `dotted.key value` line per leaf, and a path to a leaf
    (`mes.0.program.source`) prints the bare value alone, for scripts.
  - `watch` subscribes and streams each `Change`: `{kind, opcode, key,
    value, time}`, kind `state`, `ready` or `disconnected`. Its first
    frame is `{kind: "watching"}`, sent once the subscription exists, so
    a script knows from then on nothing is missed. `--filter` keeps keys
    starting with any of its prefixes, `--count` stops after that many.
    The clock's `Time` packets are left out unless `--all` is given or a
    `--filter` names them: on a switcher they drown everything else
    (found on the first hardware run, docs/HARDWARE.md). So are
    `tally-index` (`TlIn`) and `fairlight-tally` (`FMTl`), whose news
    the tally line, from `TlSr`, already gives (the second run). The
    engine leaves them out, so `--json` doesn't have them either.
  - **watch's human form** (`cmd/bmdctl/watch.go`) prints each change as
    its time, the pretty key (atemwire's mixerstate name with the key's
    indices, `key-on-air:0:1`, through `atem.PrettyName`, a Go addition
    exporting each parser's PRETTY) and the value as JSON; and for the
    common changes one line of its own: `program  ME1  CAM3 → CAM5`
    (preview likewise), `tally  CAM5 pgm, CAM3 off` (the sources whose
    lamps changed, by `TlSr`: on program first, then preview, then off),
    `transition  ME1  mix  running 0.4s` (from `TrPs`, timed from the
    first packet in transition by the events' times, then `done` and
    `idle`). Sources show by short name, else long name, else id. What
    it shows a change against comes from a `state` snapshot the CLI
    takes once the engine says it watches, kept up to date by the
    changes themselves (`InPr` renames included); a change in the moment
    between the two shows as the new value alone. `--json` prints the
    engine's frames, unchanged. It starts with the read header, colours
    program red and preview green, and (the UX pass) says `session to
    <name> (<address>) dropped` and `reconnected` inline. The state dump
    a switcher sends when the session comes back isn't news: the human
    form takes it in quietly, then prints `reconnected` and a line for
    each program and preview that changed while it was away, where
    before it printed every packet of the dump.
    A stream that ends normally sends no final response; the CLI exits 0
    on `--count` or Ctrl-C, 2 if the session closes.
- **Group b, switching** (`ops_switching.go`). The wire commands keep
  their one entry each; `transition position` is now `tbar`. Three
  operations read the state first, as atemwire's do:
  - `transition rate` is `set_transition_rate`: the rate goes to the
    settings of the M/E's current style (mix when unknown), checked
    against that command's field (u8, u16 for the stinger).
  - `transition next` sets the layers outright
    (`set_next_transition_layers`), or `--toggle` flips one in the
    current selection (`toggle_transition_key`,
    `toggle_transition_background`). Where atemwire silently sends
    nothing because the toggle would leave no layer, the CLI says so (a
    usage error): a command line asked for something.
  - `transition style` sets the style alone.
  An operation that sends one command answers like a wire command, and
  prints like one.
- **Group c, keyers, aux, inputs, colour** (`ops_keyers.go`). `usk
  toggle` and `dsk toggle` are atemwire's `toggle_usk` and `toggle_dsk`:
  the on-air state read, inverted, sent. The rename is now `input
  rename`, beside `inputs` (every source, one per line: id, short name,
  long name and the port type named from `InPr`'s categories; `--json`
  has the availability flags too). `inputs` was `input list` until the
  second hardware run; that name stays as a hidden alias
  (`Registry.Alias` takes several words). A source name no input has
  says `(bmdctl inputs lists them)`. `usk keyframe-run` no longer
  insists on `--direction`: its mask covers only the direction, and
  atemwire sends a plain run with a zero mask, so the table marks the
  entry `complete` rather than deriving "at least one".
- **Group d, audio** (`ops_audio.go`). The Fairlight (`audio ...`)
  and classic (`classic-audio input|master|monitor|levels`) wire
  commands keep their entries (named as above); Fairlight and classic
  sources take input names too. `audio meters` is new: the switcher streams levels only to a client
  that asked (`SFLN`, or `SALN` on the classic mixer), and atemwire
  stores `FMLv` bare, so the last strip's levels overwrite the others
  (SURFACE.md §5.4 item 6). The readout subscribes, turns the stream on,
  keeps each strip's latest packet for `--for` seconds (0.5 by default),
  and turns it off again. Strips come in `FairlightStrips` order with its
  display names. Levels are atemwire's dB (Fairlight's hundredths divided
  by 100, the classic mixer's log scale); level and peak are the fader's
  on Fairlight. The fake switcher answers `SFLN` with one burst of
  fixed levels for each strip it described, and the master. The
  Fairlight readout is verified on hardware (the fourth run,
  docs/HARDWARE.md): a Constellation streamed 14 strips and the master.
  `classic-audio meters` (`SALN`) is not yet verified.
- **Group e, media** (`ops_media.go`). `media list` is the still slots
  (`mediaplayer_slot_info`) and what each player shows. Transfers run
  through the session's transfer engine with the library's timeouts,
  `--timeout` replacing them when given:
  - `media upload` decodes a PNG, JPEG or GIF (the standard library's
    decoders) in the engine and calls `UploadStill`, so the fit and the
    conversion are WebATEM's. As WebATEM does, a PNG is premultiplied and
    nothing else (`--premultiply` overrides it) and the slot is named
    after the file without its extension (`still` from stdin). Replacing
    a used slot is destructive: without `--yes` it is refused with the
    name it holds.
  - `media download` converts the frame at the current video mode
    (`FrameImage`) and sends it as a PNG; without `--file` it is named
    after the slot, made safe as a file name. A frame of another size (a
    still stored at another video mode) is reported, not guessed at.
  - `media clear` is atemwire's `clear_still`, under the store lock, and
    is confirmed by the slot's `MPfe` reporting it empty. The bare `CSTL`
    entry, which some models ignore, is now `media clear-unlocked`.
  - Errors: a `*TransferError` or `ErrLockNotGranted` is `rejected` (4), a
    transfer that times out under way `no_echo` (3).
- **Group f, macros** (`ops_macro.go`). `macro run`, `stop` and `delete`
  send `MAct`; `--slot` there and in `download` takes a slot or a
  macro's name (case-insensitive; a name two macros share is refused).
  `macro list` shows the used slots (`--all` the empty ones too) and
  which runs or records. Deleting, and recording or uploading over a
  used slot, need `--yes`; so does `macro action --action delete`, and
  `macro record` into a used slot (both wire commands, checked before
  sending).
  - **The file form** is ATEM Software Control's: a `<Macro index name
    description>` element of `<Op id ...>` elements, the form profiles
    hold macros in, written by the profile package's serializer
    (`ToXML`) and read by its parser (`FromXML`, guards included). A
    file holding a profile or `<MacroPool>` with exactly one macro is
    read too. JSON is a list of op objects, `"id"` first and the
    attributes in atemwire's order (or `{name, description, ops}`);
    anything else is bytecode. `download` picks XML, JSON or bytecode by
    `--format` or the file's extension, XML by default.
  - `macro decode` and `macro encode` are local: the CLI runs them
    without an engine or a switcher. Decoding never fails outright, as
    the codec doesn't; its warning goes to stderr. Encoding fails with
    the codec's Python class and message.
  - The fake switcher models `MAct` and `MSRc` far enough for their
    state to show: running, stopped, recording, a recording stored as an
    empty macro under its name, a deletion.
- **Group g, profiles** (`ops_profile.go`). `profile save` is
  `profile.Save` with every `SaveOptions` flag; `profile restore` is
  `Apply` with every `ApplyOptions` flag, atemwire's defaults included
  (no program or preview, no fly keyframes), `--macros-only` starting
  from `MacrosOnly()`, and is destructive. The per-M/E blocks are
  `--mes` and `--keyers` (lists from 1) with the five per-M/E flags
  applied to each M/E chosen. A restore that records errors exits 4 and
  lists them; one that aborts (atemwire raising) lists the abort too.
  - **Images.** atemwire leaves media pool pixels to its caller:
    `download_media_pool_images` beside save, and for apply "the caller
    is expected to supply those bytes and push them into the media pool
    through its own upload path after applying the rest"
    (`restore_media_pool_images`). The engine is that caller. Save writes
    each used still as a PNG in `ATEM Media Pool/` beside the XML, named
    as below, a failing slot skipped and reported, as atemwire logs and
    skips it; `profile images` does only that, into `--dir`. Restore's
    CLI reads each `<Still path>` it can find beside the XML and sends
    it; after the apply the engine uploads each to its `index` under its
    `name` (PNGs premultiplied, as `media upload` does), reporting the
    ones with no file. Each picture is decoded, converted to a frame and
    encoded (`EncodeStill`) once, on its own goroutine, ahead of its turn
    while the still before it uploads, and kept until its last slot has
    it (`uploadImages`, `UploadEncodedStill`): done between uploads it
    cost half a second a still (the fifteenth hardware run), and a profile
    often names one picture for several slots. With it, 20 1080p stills
    restored in 11.2 s, 1 ms between one still's FTDC and the next FTSD
    (the sixteenth run). Since bmdwire#38 and #40 each release leaves
    another client its turn: with ATEM Software Control open, which
    fetches each changed still again for its thumbnail, every still waits
    for that fetch, 31.5 s for 20 (the seventeenth run).
  - **The still cache** (`internal/stillcache`, `engine.Config.Stills`).
    atemwire's `download_media_pool_images` takes `cache_get` and
    `cache_put`, by the slot's MPfe hash; the port left them out until
    the twelfth hardware run showed what they are for. A still downloads
    at the switcher's pace, 1.8 s for 1080p, for ATEM Software Control
    too, whose quick save writes the stills it fetched on connecting; a
    20-still pool took bmdctl 46 s. The engine's `cache_get` is the still
    cache, then this save's own pictures (each picture downloads once);
    its `cache_put` keeps every PNG it fetches. On disk in
    `~/.cache/bmdctl/stills` (`paths.Cache`; `%LocalAppData%\bmdctl\cache`
    on Windows, `/data/cache` in the image), by hash alone, so a picture
    on several switchers is kept once. Bounded by `BMDCTL_STILL_CACHE_MB`
    (1024; 0 keeps none), least recently used first, and by 30 days
    without a save: WebATEM's week assumes its watcher touching what a
    switcher shows, and here only a save touches a still. A write is a
    temporary file renamed into place; only the cache's own files are
    ever removed. `ImageResult.FromCache` is atemwire's `from_cache`, and
    the step says what was fetched ("20 stills, 8 downloaded"). Each
    download's PNG is encoded while the next one transfers, so the
    transfers run back to back; a picture whose download failed is
    reported for its other slots rather than tried again. When the
    session goes, the first still that needs a download ends the capture,
    which returns the slots done so far, as `download_media_pool_images`
    does since bmdwire#31 (`TestProfileSaveStopsWhenTheSessionGoes`).
  - **File names** (`mediaPoolFiles`). atemwire's XML names
    `ATEM Media Pool/<raw name>.png` and gives an unnamed slot no path,
    while the file needs a safe name (`fileName`), so the two could
    disagree, and two pictures under one name overwrote each other. The
    engine names each still once and sets the XML's `<Still path>` to
    that: the slot's name made safe, `Slot<n>` for none, `<name>_2` (`_3`
    ...) for a second picture under a name already taken (case ignored,
    as a Mac or Windows folder compares names), and one file for the same
    picture under the same name. WebATEM 2.1.1 does the same. It is how
    the app packages a profile, not the profile format, so it isn't a
    bmdwire change.
- **Group h, video mode, startup, time, the unverified commands**
  (`ops_system.go`). `video-modes` lists what `_VMC` says the switcher
  supports, by the names `--mode` takes; `video-mode` refuses a mode not
  in that list (when there is one) before the switcher drops everything
  to try. `time` sends `TiRq` and waits for the `Time` packet that
  answers it. Every entry atemwire restored unverified prints "unverified
  on hardware" on stderr when sent (not with `--json`); `video-mode`,
  `time` and `time-request` don't, as atemwire verified them. `startup
  save`, `startup clear` and `video-mode` are destructive, as before.
- **Indices the switcher doesn't have** are refused before sending:
  `--me` beyond `_top`'s M/E count, `--keyer` beyond the M/E's `_MeC`,
  `--dsk` beyond `_top`'s. Only counts the switcher reported are used, not
  the readers' defaults. A switcher ignores a command for an index it
  lacks, so without this the user would see only "no echo".

## Fake switcher (`internal/fakeatem`, `internal/fixtures/dump`)

- **It is the session's socket.** A `fakeatem.Switcher` implements
  `net.PacketConn`; tests pass it as `atem.Dialer.Conn` and dial
  `fakeatem.Address` (192.0.2.10, TEST-NET-1). The session's whole stack
  runs for real against it: handshake, ACKs, in-order delivery, parsers,
  state. MIT, like the rest of `internal/` except the fixtures.
- **What it does.** It accepts a SYN (hello 0x02 under 0x1337), gives the
  session a fresh id when the handshake ACK arrives, replays the dump in
  reliable packets of up to 1400 bytes, then sends one empty reliable packet
  (the post-dump ping that makes the session ready). It ACKs each of the
  session's reliable packets, resends its own unACKed packets after 60 ms
  flagged as retransmissions, pings every 500 ms, asks for a retransmission
  (header bytes 6-7) when the session's sequence skips, and ACKs duplicates
  without redoing them. `echoes.go` answers a set of common commands
  (`CPgI`, `CPvI`, `DCut`, `DAut`, `CAuS`, `CTMx`, `CKOn`, `CDsL`, `CDsT`,
  `CDsR`, `FtbC`, `CClV`, `CInL`) with the state packet a switcher would
  send; phase 3 adds `CVdM`, meter bursts for `SFLN`, and the macro
  player and recorder (`macros.go`). `mirror.go` confirms more without
  modelling them: it
  answers each with the state packet it holds for the same key, unchanged,
  so the engine's wait for an echo can be tested; the tests check the
  bytes sent. Anything else is ACKed and changes nothing. It is not a
  switcher model and makes no claim about switcher timing.
- **Controls.** `Unanswered(codes...)` takes commands without echoing
  them; `DropToClient(n)` drops the next n reliable packets (their
  resends get through); `DropSequences(...)` drops chosen sequence numbers
  of the next session; `DropFromClient(n)` ignores the next n datagrams
  from the session; `DelayACKs(d)` ACKs late; `Silence(true)` cuts the
  cable: nothing in or out, and the switcher forgets the session, as a real
  one does when its packets go unacknowledged. `SendStale(n)` sends n
  packets under the previous session's id around the next handshake, before
  the SYN reply and again before the new dump; `Send(cmds...)` sends any
  commands, malformed ones included; `Inject(from, b)` delivers a datagram
  from any address, silence or not; `HoldLock(store)` and
  `ReleaseLock(store)` play another client taking and releasing a store
  lock, broadcast as `LKST`; `LateLockState(d)` announces lock changes d
  late, those of a moment together, as a Constellation announces them at
  its next frame, after the LKOB for a request sent with a release
  (bmdwire#36). `Commands()` and `Handshakes()` let tests look inside.
- **Transfers** (`transfers.go`). The fake keeps a media pool and a macro
  pool. The session's PLCK or LOCK is granted (LKOB, and LKST to all) unless
  another client holds the lock; then a PLCK waits for its release and a
  LOCK is dropped. One from the session already holding it gets no
  answer, as MEDIA_LOCK.md says a switcher gives none; until bmdwire#36
  the fake granted it again, which hid the lock-state bug that change
  fixed. `QueueOther(store, hold)` plays ATEM Software Control waiting
  with a whole-store LOCK: handed the store at the session's release
  without an announcement, holding it for `hold`, then releasing it
  (announced); a request from the session meanwhile is granted beside it
  and aborts its turn (`OthersAborted`), as the switcher did in the
  captures behind bmdwire#38; `TestAnotherClientWaitingGetsItsTurn`
  fails with bmdwire#36's handover. CSTL works only under the lock. FTSU is served in
  1296-byte chunks, 16 in flight, each freed by its FTUA, then FTDC; an
  empty slot is an FTDC alone. FTSD (lock held for stills, mode 1 for
  stills and 0x0300 for macros, else FTDE 2) gets 16-chunk budgets until
  the announced uncompressed length is in (it counts RLE runs); once the
  data and FTFD are both in, in either order (ASC sends FTFD first), it
  stores the slot and sends FTDC and the MPfe or MPrp. Until bmdwire#36
  it finished on FTFD, the order atemwire sent it in. `FailTransfers(statuses...)`
  answers the next requests with FTDE (5 also takes the lock away, 6 leaves
  it held); `PutStill`, `Still`, `PutMacro`, `Macro` and `LockedBySession`
  fill and inspect it. An empty macro upload (FTSD of length 0, then FTFD)
  stores a macro with the upload's name and description and no ops, as
  a switcher does (the tenth run, docs/HARDWARE.md), which is what
  profile apply sends one for, as atemwire's comment says ASC's "Restore
  from State" does. Until the tenth run the fake emptied the slot
  instead; `TestMacroUploadEmptyThenList` uploads one and lists it.
- **The dump** (`internal/fixtures/dump/initial-dump.bin`) is synthetic:
  bmdwire's tests contain no recorded one. `gen_dump.py` builds 88
  commands from the values of bmdwire's `build_full_state`
  characterization fixture (plus identity, topology, multiviewer and
  timecode), each packed to atemwire's Recv layout, and checks every one
  against bmdwire v1.3.0's `save_field_data` before writing the file.
- **Tests** (`fakeatem_test.go`): the end-to-end test (Dial,
  `SendAndWait(ProgramInput{ME: 0, Source: 5})`, `State()` shows source
  5), the dump replayed into typed state with a TiRq after it, a lost dump
  packet recovered through the stalled ACK, a lost command recovered
  through the switcher's retransmission request, delayed ACKs, and silence
  followed by a re-handshake and a fresh dump (about 10 s; skipped with
  `-short`). The tests for the deliberate differences are in
  `atem/fake_test.go` (package `atem_test`, which can shorten the silence
  timeout through `export_test.go`).
- **Waiting in tests.** A test against the fake waits for a condition,
  never for a sleep to be long enough: CI's runner is slower than a
  desk. `Dial` returns only once the dump is in, so a test can send or
  transfer straight after it. After a disconnect, it waits on `Ready`
  (or `ChangeReady`) before the next one. A test that must act while a
  transfer waits for a lock waits for it to be queued (`waitQueued`,
  through `export_test.go`'s `QueuedTransfers`); one that must act while
  a command waits for the switcher waits for its `connecting to` line.
  A sleep only paces (a burst window, a slow dial).

## HyperDeck (`hyperdeck/`)

Ports bmdwire v1.3.0's `hyperdeckwire/client.py`, the HyperDeck Ethernet
Protocol on TCP 9993. hyperdeckwire has no SURFACE.md, so
`docs/SURFACE-hyperdeck.md` inventories it, written from the code;
`upload.py` (FTP) and `setup.py` (the configuration API) are in it and
not ported yet.

API: `Dial(ctx, addr)`, `Dialer{DialContext, Logger, Bare}`; `Send(cmd)`,
`Request(ctx, cmd) (Response, error)`, `SendAndWait(ctx, cmd)`, `State()`,
`Subscribe() (<-chan Change, func())`, `Ready()`, `Connected()`, `Close()`,
`Greeting()`, `Model()`, `ProtocolVersion()`; a method per hyperdeckwire
method (`DeviceInfo`, `DiskList`, `Play(ctx, PlayOptions)`, ...); a command
type per line (`Command{Line}`, `Validator`); `*Error{Code, Text}`,
`ErrClosed`, `ErrTimeout`, `ErrMalformed`.

- **atem.Session's shape, on a stream.** One owner goroutine holds the
  connection's writes, the commands waiting for answers, the state and the
  subscribers; one reader frames what the deck sends and hands it over.
  Methods hand the owner closures on an unbuffered channel and wait for
  them to have run. hyperdeckwire is one blocking socket read only when a
  request is made; here the reader reads all the time, so notifications
  reach the state between commands.
- **Answers are matched in order.** The deck answers every command once,
  in order, so each command sent registers a waiter, in the owner step
  that writes it, and the next answer that isn't asynchronous is the
  oldest waiter's. `SendAndWait` is `_check_ok` (a 1xx is an `*Error`,
  hyperdeckwire's `HyperdeckError`, `[code] text`); `Request` is
  `request` (any code). `Send` writes and returns; its answer is read and
  dropped, a refusal logged. HyperDeck acks every command, so a
  command's answer is its confirmation: unlike an ATEM there is no
  command without one.
- **Framing is `_recv_line` and `_read_response`:** LF ends a line, every
  CR before it goes, bytes decode as Python's `'replace'` (one U+FFFD per
  maximal ill-formed subpart), blank heads are skipped, the code is read
  with a port of `int()` (signs, leading zeros, underscores, any script's
  decimal digits; beyond int32 it is malformed, where Python goes on with
  a big integer; since phase 5 it strips as CPython's does, ASCII and
  non-ASCII Unicode whitespace but not U+001C-U+001F, which
  `internal/pytext`'s oracle found the first port stripping as
  `str.strip()` does), a head ending in a colon starts a block read to the first
  empty line, and `params()` strips with Python's `str.strip()` (Unicode
  whitespace and U+001C-U+001F). The clip parsers split on single spaces
  from the right, as `_parse_disk_line` and `_parse_clips_v1_line` do; the
  clip-line filter counts decimal digits where Python's `isdigit()` also
  counts superscripts, and a head of those is skipped by the parser there
  and by the filter here, with the same list. Messages quote with a port
  of `repr()`. These ports live in `internal/pytext` (MIT), which the
  three MIT device packages share; `atem`'s are its own, under the LGPL.
- **Dial** opens TCP with a 5 s deadline (`ConnectTimeout`,
  `DEFAULT_CONNECT_TIMEOUT`) and reads the greeting as `connect` does: the
  first block, whatever it is (`skip_async=False`). Then, unless `Bare`,
  it turns on the notifications bmdctl needs, one `notify: <kind>: true`
  line each (transport, slot, remote, configuration; not display
  timecode, which a deck sends every frame) and reads the deck's state:
  `device info`, `transport info`, `remote`, `configuration`, `clips get`
  and `slot info` for each of `slot count`'s slots (16 at most). A refusal
  among these (an old firmware without a notification) is logged and
  leaves that part empty; the connection failing fails Dial. Ready is
  closed when Dial returns. hyperdeckwire turns nothing on and reads
  nothing more; the oracle tests dial `Bare`.
- **State and notifications are bmdctl's** (hyperdeckwire keeps none):
  every block read, answer or notification, goes into `State` by its
  head's text: `connection info`, `device info`, `transport info`, `slot
  info` (by its `slot id`), `remote info`, `configuration`, `notify`,
  `clips info` (the timeline, parsed), `disk list` (by slot, parsed),
  anything else under `Other` by its text. An answer replaces its block; a
  notification, which carries only what changed, is merged into it.
  Fields keep the deck's names (`display timecode`); `State` has
  accessors (`Status`, `ClipID`, `SlotID`, `Timecode`, `Name`, `Model`,
  `RecordingTime`). A 5xx after the greeting is published as a `Change`
  (`Key` `transport`, `slot:2`, `remote`, `configuration`; `Params`, what
  it carried; `Value`, the block now), each subscriber its own copy; a
  subscriber 1024 behind is closed and dropped, as atem's are. `Name` is
  device info's `name` (what HyperDeck Setup sets, which hyperdeckwire's
  `set_name` docstring says `device info` reports), else its model.
- **No re-handshake.** TCP either works or ends: the stream ending, a
  read or write error, or a timeout ends the session (`Connected` false,
  waiters failed with an error matching `ErrClosed` that wraps the cause,
  subscribers sent `ChangeDisconnected` and closed). The caller dials
  again, as hyperdeckwire's caller reconnects.
- **Timeouts.** Without a context deadline an answer is waited for 5 s
  (`ReadTimeout`, `DEFAULT_READ_TIMEOUT`); with one, until it. The context
  ending first abandons the wait: the waiter stays, so its answer, when it
  comes, is dropped, not read as the next command's.
- **Commands** are hyperdeckwire's lines, written as it writes them
  (`play: clip id: N loop: true single clip: true speed: N`, in that
  order; `clips add: clip id: N name: X`, the name verbatim; a
  `Setting`'s underscores as spaces, bools `true`/`false`). `Play`'s loop
  and single clip are pointers: nil leaves them out, false writes
  `loop: false`, which hyperdeckwire can't (§5.4 item 8) and bmdctl's
  `loop off` needs. `SetConfiguration` with nothing to set refuses to be a
  command (`Validate`), where `set_configuration()` sends nothing; the
  method sends nothing, as hyperdeckwire's does.
- **Added from BMD's document** (`HyperDeckEthernetProtocol.pdf`,
  December 2024), which hyperdeckwire doesn't send and nobody has yet
  sent from bmdctl to a deck: `Record` (`record`, `record: name: X`),
  `GotoClipBy` (`goto: clip id: +1`, `-1`), `GotoTimecode` (`goto:
  timecode: HH:MM:SS:FF`, checked for that form), `Notify` and `Play`'s
  false loop and single clip. The CLI marks these unverified.
- **Deliberate differences**, each a trap of SURFACE-hyperdeck.md §5.4
  fixed rather than copied, with a test; the first four were filed
  against bmdwire, and hyperdeckwire 1.4.0 fixed them the same way
  (bmdwire#32), so they are differences from v1.3.0 only:
  - **A timeout ends the session** (§5.4 item 1): an answer later than
    its deadline would be read as the next command's. The owner checks
    the oldest waiter's deadline every 100 ms. `TestATimeoutEndsTheSession`;
    an abandoned wait's late answer is dropped, `TestALateAnswerIsDropped`.
    bmdwire#18.
  - **An answer to no command is dropped** and logged (§5.4 item 2), not
    given to the next command. `TestAnAnswerToNothingIsDropped`. A
    malformed head is still the waiting command's error, as in
    hyperdeckwire, and one with no command waiting is dropped too.
    bmdwire#19.
  - **A line break in a value is refused** (§5.4 item 5): every command is
    checked, and `ClipsAdd`, `Record`, `Notify`, `SetConfiguration` and
    `Raw` say so in `Validate`. `TestALineBreakIsRefused`. bmdwire#20.
  - **A refusal for a greeting fails Dial** (§5.4 item 6): a deck with all
    the clients it takes answers `120 connection rejected` and closes;
    Dial returns that `*Error` rather than a dead session.
    `TestARejectedConnectionFailsDial`; the oracle's greeting of 120
    expects it. bmdwire#21.
  - **After the stream has ended nothing is sent**: hyperdeckwire writes
    into a socket it found closed and reads its end again; the session has
    ended and returns `ErrClosed`. The oracle test expects that for calls
    after a `ConnectionError`.
- **Verification.** `hyperdeck/testdata/oracle/client.json` stays at
  v1.3.0. hyperdeckwire 1.4.0 drops an answer already buffered when it
  sends a request (bmdwire#19, #32), and `gen_client.py`'s fake socket
  hands the client the whole session on its first read, so at 1.4.0 it
  records that drop in 724 of the 747 sessions instead of the client's
  work. Regenerating needs the socket to release each answer only after
  its request, as `TestClientMatchesHyperdeckwire`'s gated connection
  already does; until then the 1.4.0 behaviours are pinned by bmdctl's
  own tests (the differences above). The file (`gen_client.py`) records
  hyperdeckwire v1.3.0's real client in 747
  scripted sessions, 872 calls: the bytes sent, how far into the stream it
  had read, and the result or exception. `TestClientMatchesHyperdeckwire`
  replays each through a `Bare` Session on a connection that gives it, call
  by call, exactly what hyperdeckwire had read (the session reads eagerly),
  and checks the bytes, results (dicts, clips, ints) and errors by class
  (`HyperdeckError` code, text and message; `ConnectionError` as
  `ErrClosed`; the malformed head's and `ValueError`'s messages), the
  greeting, model and protocol version, and that every method is called.
  `TestParsersMatchHyperdeckwire` checks the line parsers on 132 lines and
  `params` on 16 bodies. `client_test.go` ports
  `test_hyperdeckwire_client.py` one for one on the fixtures copied from
  it (`hyperdeck/testdata/fixtures`); `session_test.go` tests what bmdctl
  adds against the fake deck, over a pipe and over loopback TCP. Nothing
  here has run against a deck.

## HyperDeck commands (`hyperdeck/table.go`, `internal/engine/hyperdeck.go`, `cmd/bmdctl/deck.go`)

Phase 4: `bmdctl hyperdeck ...`, and every command to a host of type
`hyperdeck`, goes to a HyperDeck through the engine.

- **The table.** `hyperdeck.RegisterCommands` adds the commands that are
  one line each, built from their arguments as the ATEM's are (a
  `TableCommand`, whose bytes are the line and its newline): `play
  [--speed N] [--loop] [--single-clip] [--clip N]`, `stop`, `record
  [name]`, `next` and `prev` (`goto: clip id: +1`/`-1`), `slot <slot>`.
  The engine adds the ones that read the deck first: `goto <clip>`,
  `loop <on>`, `clips [--disk] [--slot N]`, `status`, `watch [--count
  N]`, `state [--path P]`, `connect [address]` and `disconnect`. Slots
  and clips count from 1, as the deck numbers them on the wire: nothing
  is converted. `--dry-run` prints the line itself (a text protocol's
  bytes). `record`, `next`, `prev` and `loop` are marked unverified:
  hyperdeckwire never sends them (SURFACE-hyperdeck.md §5.1), and the
  notice says so; `goto` by timecode is too, though the entry isn't
  marked, since by clip it is hyperdeckwire's own line.
- **The engine's sessions** are keyed by device: an address's key takes
  the device's port (9910, 9993), and a host holds an `*atem.Session` or
  a `*hyperdeck.Session` (`devSession`: Connected, Ready, Close) with its
  device. Dialling, sharing a dial, idling out, pinning, `status` and
  the overview are the same for both; `Config.DialHyperDeck` dials a
  deck (`hyperdeck.Dialer`, so notifications on and the state read).
- **Each request reads the transport first** (`transport info`): the
  deck's notifications don't carry the timecode as it plays, and the
  before of a result and a terminal's header (the `deck` note, `stopped
  · clip 2`, as `on_air` is a switcher's) should be what the deck holds.
  The `switcher` note carries the deck's name (device info's), model and
  video format, so the header, hosts.yml's renames and the overview work
  as for a switcher.
- **A write says what it changed**, before and after, in a `values`
  note: the transport (`stopped → play 50%`; the speed where it isn't
  100), the clip by name (`next`, `prev`, `goto`), the timecode (`goto
  00:01:30:00`), the slot, loop on or off. After the deck's 200 the
  engine reads the transport again: that answer comes after the
  command's, so it holds what the command did. A HyperDeck acks every
  command, so a write is always `confirmed` (or refused): there is no
  `sent`. A deck that already holds it sends nothing, `already`: `stop`
  while stopped, `play` as it plays (no other flag), the active slot
  again, loop as it is.
- **What the deck refuses** (a 1xx) is `rejected`, exit 4: `the deck
  refused play: remote control disabled (111)`. No answer within the
  timeout is `no_echo`, exit 3, and ends the session (Session,
  "Timeouts"); the session gone is `unreachable`, exit 2.
- **`goto <clip>`** takes a timecode (HH:MM:SS:FF, `;` for drop frame;
  hours under 24, minutes and seconds under 60), a clip's number, or a
  clip's name: exactly, ignoring case, else the one clip whose name it
  starts (two are listed); the timeline is read fresh for a name.
- **`loop on|off`** changes the playback under way: it sends `play` with
  the speed and single clip as they are and loop set, a `loop: false`
  hyperdeckwire can't send. The protocol keeps no loop apart from a
  playback, so a stopped deck has nothing to loop and the command says
  so (`bmdctl play --loop starts one looping`); not sending play there,
  which would start one, is deliberate.
- **`status`** reads each slot's info and remote control, then shows the
  transport (status, speed, loop, single clip), the timecode, the clip
  (number, name, duration), how many clips the timeline holds, each slot
  (volume, status, recording time left, the active one marked at the
  line's end) and whether remote control is on. `clips` reads the
  timeline (`clips get`), marking the clip the transport is on with its
  status at the line's end, or with `--disk`/`--slot` a slot's disk
  (`disk list`), with its formats. `state` is the session's
  `hyperdeck.State` as JSON, `--path` as a switcher's.
- **`watch`** streams the deck's notifications as `ChangeEvent`s: kind
  `state`, the code as opcode (`508`), the block as key (`transport`,
  `slot:2`, `remote`, `configuration`) and what the notification carried
  as value; `disconnected` when the session ends. The CLI shows each
  against a `state` snapshot taken once the engine says it watches, as a
  switcher's watch does: `transport  stopped → play`, `clip  Intro... →
  Interview.mov`, `remote  enabled true → false`.
- **Routing.** The device comes from the host's type (Hosts, "Which
  device"): `play` to an ATEM host says `STUDIO (192.0.2.10) is an ATEM;
  bmdctl atem --help`, `program 2` to a deck `DECK 1 (192.0.2.20) is a
  HyperDeck; bmdctl hyperdeck --help`. Completion completes the host's
  device's commands. `doctor` reads a deck's greeting and device info
  (model, protocol, software) where the engine has no ready session to
  it.
- **Colour:** `record` red, as recording is on a switcher; nothing else
  of a deck's is coloured but what is for every device (the name bold;
  ids, durations, the header's transport dim).
- **Tests.** `cmd/bmdctl/deck_test.go` runs each command through an
  engine to the fake deck (a pipe through `internal/fakenet`): the
  lines sent, each result's before → after, refusals, reads as text and
  JSON, watch, and that every entry runs (`TestEveryDeckCommandRuns`);
  `testdata/terminal/deck.golden` is the terminal's. None of it has met
  a deck: the owner will run it against one when one is on the bench.

## Fake HyperDeck (`internal/fakehyperdeck`)

- **A deck on any `net.Conn`.** `Deck.Dial` is a `hyperdeck.Dialer`'s
  `DialContext`, one end of a `net.Pipe`; `Deck.Listen` serves loopback,
  for what dials a port itself. MIT.
- **What it holds:** a HyperDeck Studio HD Mini named `Deck 1`, protocol
  1.13, two slots (`SD1` with three clips, the hardware-verified name
  `Intro Loop animation.mp4` first; `SD2` with one), a timeline (the
  active slot's clips, as a Studio's is), a transport (status, speed,
  clip, position in frames at 25 fps, loop, single clip), remote control
  and a configuration.
- **What it answers**, with BMD's codes and the 2xx blocks of §4.1:
  device info (with `name`), transport info, slot info, disk list, clips
  get, clips count, remote, configuration, notify (per connection),
  ping; play (speed 0 reports stopped, as hyperdeckwire found on
  hardware), stop, record (stopping adds the clip, named or `Clip
  0001.mov`), goto by clip id (absolute, or `+N`/`-N` held at the ends),
  by timecode, slot select, clips clear/add/remove, remote enable, quit.
  Writes need remote control (111); a clip it hasn't is 112, an empty
  timeline 107, a value out of range 102 or 109, anything else 100. Each
  change goes to the connections that turned its notification on, with
  only what changed (508, 502, 510, 511).
- **Controls:** `SetRemote` and `Press(status)` (the front panel, heard
  by every connection), `Silence` (takes commands, answers none), `Send`
  (raw text to every connection), `Reject(n)` (120 to the next n), `Drop`
  (closes every connection); `Commands`, `Status`, `Slot` to look inside.
- **Not a deck model.** The timecode moves only on goto, nothing plays
  through to the next clip, and nothing about a deck's timing is claimed.
  What a real deck answers to `goto: clip id: +1` on the last clip, or a
  goto beyond the timeline, is not known; the fake holds or refuses.

## Videohub (`videohub/`)

Phase 5. Ports bmdwire v1.6.0's `videohubwire/client.py`, the Videohub
Ethernet Protocol on TCP 9990. videohubwire has no SURFACE.md, so
`docs/SURFACE-videohub.md` inventories it, written from the code.

API: `Dial(ctx, addr)`, `Dialer{DialContext, Logger}`; `Send(ctx, cmd)`,
`Route`, `SetInputLabel`, `SetOutputLabel`, `Ping`; `State()`,
`Snapshot()` (videohubwire's `state()`), `Subscribe()`, `Ready()`,
`Connected()`, `Close()`, `Addr()`; a command type per block (`Route`,
`InputLabel`, `OutputLabel`, `Ping`, each with `Block()`); `*Error`
(videohubwire's `VideohubError`, its message) with `ErrRefused`,
`ErrTimeout` and `ErrClosed` for `errors.Is`.

- **atem.Session's shape, on a stream,** as hyperdeck's is: one owner
  goroutine holds the writes, the commands waiting, the state and the
  subscribers; one reader frames what the hub sends. videohubwire reads
  only inside a call (`state()` drains for 50 ms); here the reader reads
  all the time, so a push reaches the state when it comes and `Snapshot`
  needs no drain.
- **Answers are matched in order:** the hub answers every block with ACK
  or NAK, other blocks between. Each command sent registers a waiter in
  the owner step that writes it, and the next ACK or NAK is the oldest
  waiter's; every other block is applied (and published). A NAK while a
  ping waits is applied as a block, changing nothing, and the wait goes
  on, as `ping()` does. On ACK the change is applied at once (the route,
  the label as sent), before the hub's own broadcast, as videohubwire
  does.
- **Framing and parsing are ports, quirks included:** a block is the bytes
  to the first `\n\n`, decoded as Python's `'replace'`; the header is the
  first line stripped (so a stray blank line before a block loses it, and
  CR LF never ends one: SURFACE-videohub.md §5.4 items 2 and 3); keys
  stripped and `str.lower()`-ed, values stripped; indices and routes read
  with Python's `int()` (signs, underscores, other scripts' digits), a
  line that doesn't parse logged and skipped. The text rules are
  `internal/pytext`'s: `Fields` is `str.split()` (the label cleaning),
  `Lower` is `str.lower()`, with U+0130 and Final_Sigma as CPython has
  them. `int()` beyond int64 skips the line, where Python goes on with a
  big integer.
- **Dial** connects with `ConnectTimeout` (3 s), and once more after 0.3 s
  if that fails (videohubwire's cold-ARP retry), then reads the preamble
  as `_read_preamble` does: to `END PRELUDE:`, or, once the device block
  and a routing block have come, to 0.3 s without a block; before those
  `ReadTimeout` (3 s) from the start, then `no state preamble from
  <host> — is this a Videohub?`. The hub closing is `connection closed by
  hub`. Ready is closed when Dial returns.
- **Checks before sending** are videohubwire's, in its order and with its
  messages, against the counts the state holds: a negative index, an
  index at or past a count the device block gave, a label over 64
  characters (code points) once whitespace is collapsed. A count `int()`
  refuses is its `ValueError` message, not an `*Error`, as in Python.
  Refusing a label that isn't UTF-8 is bmdctl's (Python's strings can't
  hold one).
- **State and pushes:** `State` is the six attributes, indices 0-based
  as the wire's; `Name` is the friendly name, else the model. A block the
  hub pushes after the preamble is published as a `Change` (`Key`
  `routing`, `locks`, `input labels`, `output labels`, `device`,
  `protocol`; `Values` what it carried, by index or key), each subscriber
  its own copy, one 1024 behind closed and dropped. Blocks videohubwire
  ignores are ignored and not published. Once the session has ended,
  `State` returns what it held then.
- **Deliberate differences**, each SURFACE-videohub.md §5.4 behaviour a
  long-lived session can't keep:
  - **A timeout ends the session** (§5.4 item 1): the hub's late answer
    would otherwise be read as the next command's. The call returns
    videohubwire's message (`timed out waiting for ACK routing 1 <- 3`),
    matching `ErrTimeout`; the waiters behind it fail, the subscribers get
    `ChangeDisconnected`. `TestATimeoutEndsTheSession`.
  - **An answer to no command is dropped** (an ACK or NAK nobody waits
    for), not kept for the next one; videohubwire's drain ignores one too,
    but a call would have taken it. A context that ends first abandons the
    wait and leaves the waiter, so its answer, when it comes, is dropped.
    `TestAnAnswerToNothingIsDropped`, `TestALateAnswerIsDropped`.
  - **After the session has ended** a call fails: `not connected` after
    Close, as after `close()`, else why it ended.
- **Verification.** `testdata/oracle/client.json` (`gen_client.py`)
  records videohubwire v1.6.0's real client in 213 scripted sessions, 646
  steps: the bytes sent, the result or exception, the six attributes after
  each step and what it hadn't read. `TestClientMatchesVideohubwire`
  replays each through a Session on a scripted hub that releases a call's
  reply once the call is written (the session reads eagerly, and an early
  ACK would be an answer to nothing), and compares bytes, results, errors
  (class and message) and, where videohubwire had read everything, the
  attributes. The generator leaves out the two things the differences
  above change: a timeout is a session's last call, and no push holds an
  ACK. `client_test.go` ports `test_videohubwire_client.py` one for one on
  the fixtures copied from it (`testdata/fixtures`); `session_test.go`
  tests what bmdctl adds. Nothing here has run against a hub.

## Videohub commands (`internal/engine/videohub.go`, `cmd/bmdctl/hub.go`)

Phase 5: `bmdctl videohub ...` (`vhctl`), and every command to a host of
type `videohub`, goes to a router through the engine.

- **Every command is an operation:** a port is named by its number or its
  label, which only the router's state resolves, and the table's resolver
  knows one namespace where a route has two. So there are no wire
  entries and no `--dry-run`; `videohub.RegisterCommands` adds the groups
  alone (`routing`: route and the renames; `session`: the reads).
  Commands: `route <output> <input>`, `input rename <input> <label>`,
  `output rename <output> <label>`, `status`, `outputs`, `inputs`,
  `watch [--count N]`, `state [--path P]`, `connect [address]`,
  `disconnect`.
- **Ports count from 1**, as Videohub Control and the router's panel
  number them, in arguments, lines and JSON (`table.Index`); the wire,
  `state` and `watch --json` count from 0, as videohubwire does. A port
  is its number (numbers win), else its label exactly, ignoring case,
  else the one label it starts; two are listed. A number out of range
  says how many there are.
- **The engine's sessions** are keyed `host:9990`; `Config.DialVideohub`
  dials one (`videohub.Dialer`), with 9 s for the connect, its retry and
  the preamble. The `switcher` note carries the router's name (its
  friendly name, else its model) and model; there is nothing live to put
  in the header, as `on_air` is a switcher's.
- **A write says what it changed** in a `values` note: a route the output
  by its label and the input before → after; a rename the port's number
  and the label before → after (as `CleanLabel` sends it). One the router
  holds already is not sent, `already`. The router acks every block, so a
  write is confirmed (the ACK) or refused: never `sent`. The after is the
  state on the ACK, which applies the change at once, as videohubwire
  does.
- **What the router refuses** (NAK) is `rejected`, exit 4, the ports named
  from 1: `the router refused: output 16 "Test" is locked by another
  client` where its lock reads L, else `the router refused input 7
  "Program" to output 5 "Monitor A" (locked?)` (videohubwire's message
  counts from 0). No answer within `--timeout` is `no_echo`, exit 3, and
  ends the session (Videohub, Deliberate differences); the next command
  dials again. What videohubwire refuses before sending (a label over 64
  characters) is a usage error with its message.
- **status** is the router's size and locked outputs in the header, then a
  row per output: its number dim, its label, the input on it (`none` dim),
  and a lock at the line's end (`locked`, `ours`) dim. **outputs** is the
  rows alone; **inputs** each input's number, label and, dim, the outputs
  it is on (`to 1, 5`, `routed nowhere`). Down a pipe each is a table:
  number, label, input number and label, lock. `state` is the session's
  `videohub.State` as JSON.
- **watch** streams the blocks the router pushes as `ChangeEvent`s: kind
  `state`, the block as key (`routing`, `input labels`, `output labels`,
  `locks`, `device`, `protocol`) and what it carried as value, indices
  from 0. The CLI shows a line per index against a `state` snapshot taken
  once watching: `route  out 5 Monitor A  Replay → Program`, `label  in 3
  Camera 3  Camera 3 → Wide Shot`, `lock  out 16 Test  unlocked → locked`.
- **Elsewhere:** the probe, `connect ADDRESS` and `hosts add` found
  routers since phase 4; now `connect` opens the session, and a host is
  renamed when the router's name changes, as a switcher's is. `doctor`
  reads a router's preamble (model, size, protocol) where the engine has
  no ready session to it. The aliases, completion, help, `serve`
  (`/hosts/{host}/videohub/...`) and `mcp` (`videohub_routing`,
  `videohub_session`; the reads are read-only tools) take the commands
  from the table. The site colours a router's rows by their shape (a
  number first), since `status` is also a switcher's and a deck's.
- **Tests.** `cmd/bmdctl/hub_test.go` runs each command through an engine
  to the fake router (a pipe through `internal/fakenet`): the blocks sent,
  each write's before → after, the refusals (a locked output, a silent
  router), reads as text and JSON, watch, and that every entry runs
  (`TestEveryHubCommandRuns`); `testdata/terminal/videohub.golden` is the
  terminal's. The fake refuses only a locked output, so the refusal of an
  unlocked one is untested. None of it has met a router.

## Fake Videohub (`internal/fakevideohub`)

- **A router on any `net.Conn`.** `Hub.Dial` is a `videohub.Dialer`'s
  `DialContext`, one end of a `net.Pipe`; `Hub.Listen` serves loopback.
  MIT. Each connection has its own writer goroutine, so a client that
  stops reading holds up no other.
- **What it holds:** a Smart Videohub 12G 16x16 named `Studio Router`,
  protocol 2.8, sixteen labelled inputs (`Camera 1` ...) and outputs
  (`Switcher 1` ...), output N routed from input N, nothing locked. Its
  preamble is the six blocks, `CONFIGURATION:` and `END PRELUDE:`
  (`NoMarker` leaves the marker off, as older firmware does).
- **What it answers:** `PING:` ACK; a routing or label block ACK, if every
  line's index (and a route's input) is in range and no route's output is
  locked, else NAK and nothing changes; the request form (a header and no
  body) ACK and the whole block; anything else NAK. Every change is pushed
  to every connection, the one that made it too.
- **Controls:** `Route` and `LockOutput` (another client's, pushed to
  all), `Silence` (takes blocks, answers none), `Drop` (closes every
  connection); `Commands`, `Routing`, `Labels` to look inside.
- **Not a router model:** locks taken by bmdctl (`O`) aren't modelled,
  since videohubwire never takes one; take mode is always off.

## Traps inherited from atemwire

Behaviour the port reproduces because bmdwire v1.3.0 has it, found while
porting and not listed in SURFACE.md §5.4. Each is a candidate for a
deliberate fix later, recorded here as such when it is made.

- **The first dump packet is not recoverable.** The transport takes the
  first reliable packet it sees as its sequence baseline (`_rx_established`,
  `_deliver_next`). If the switcher's first packet after the handshake is
  lost, the second becomes the baseline, the ACK covers both, and a late
  copy of the first is dropped as already delivered: its fields never
  arrive. The port keeps this, but when it costs the video mode Dial now
  fails at once instead of timing out (bmdwire#13, "Deliberate
  differences"); anything else in that packet is still silently missing.
- **A lost command was recovered only by a later one.** With no client
  retransmit timer (SURFACE.md §4.2), a reliable packet the switcher never
  saw was resent only when a later packet showed the switcher the gap.
  Since bmdwire#36 the resend timer sends it again after 250 ms.
- **Parse errors lose the rest of the packet** (session section), including
  a 23-byte `MPfe`, a `KeFS` under two bytes, and a `_VMC` bitfield naming
  mode 30 or 31.

## Deliberate differences

Bugs in bmdwire v1.3.0 the port fixes instead of copying. Each is filed
against bmdwire (`github.com/lucas-romanenko/bmdwire/issues/<n>`), and each
has a test that fails on bmdwire v1.3.0's behaviour: against the fake
switcher (`atem/fake_test.go`), or for the macro codec in
`atem/macro/differences_test.go`. bmdwire 1.4.0 fixed them all; each
says what it does now and what difference, if any, remains.

- **bmdwire#11: the silence timeout counts accepted inbound packets
  only.** atemwire's UDP thread times out when `select()` has slept 5 s,
  and `select()` wakes on any datagram (a foreign or malformed one too) and
  on every packet it sends, so a client sending to a switcher that has
  gone silent never times out, keeps `is_ready` over stale state and sends
  into the void. Here the clock (`transport.lastRX`) restarts only on a
  datagram from the switcher's address that parses and isn't stale
  (bmdwire#12), and on each SYN, so every handshake gets a full window.
  Our own sends, resends and goodbyes never count.
  `TestSilenceIgnoresOwnSendsAndForeignDatagrams`; at the transport level
  `TestSilenceTimeoutReconnects`. bmdwire 1.4.0 does the same
  (bmdwire#25): no longer a difference.
- **bmdwire#12: stale packets during a re-handshake are dropped.** After a
  silence the switcher goes on resending the old session for a while.
  atemwire checks the session id only in `_handshake`, so those packets
  restart the silence window, the first reliable one becomes the new
  session's ACK baseline and delivery cursor (the new dump, starting again
  at low sequence numbers, is then dropped as duplicates), and one arriving
  just after the handshake ACK is adopted as the session id. Here
  `connect` remembers the previous switcher-assigned id, and `receive`
  drops, before anything else sees it (sequence history, ACK baseline,
  cursor, session-id adoption, silence clock), any packet carrying it, and,
  while in SYN_SENT, anything but a SYN under the hello id. If a switcher
  ever reused the previous id for the new session (after a reboot,
  perhaps), the new session's packets would be dropped too, the silence
  timeout would fire and the next handshake would get another id: a 5 s
  stall, not a hang. `TestStalePacketsDuringARehandshake` (the fake sends
  old-session packets before its SYN reply and again before the new dump);
  `TestTransportDropsStaleSessionPackets` follows the issue's script.
  bmdwire 1.4.0 does the same (bmdwire#26): no longer a difference.
- **bmdwire#13: a dump without a video mode re-handshakes, then fails
  Dial.** A lost first dump packet is lost for good (the first reliable
  packet sets the receive baseline), and when it carried the video mode
  atemwire v1.3.0's `wait_ready` waited out its 15 s. The issue offered
  two fixes: (A) expect a new session's first reliable sequence number,
  which nothing in bmdwire proved fixed, or (B) fail fast. bmdwire 1.4.0
  took B its way (bmdwire#27): at an `InCm` with no video mode stored,
  `save_field_data` raises `IncompleteStateDump` and `loop()` says goodbye
  and re-handshakes for a fresh dump, at most `INCOMPLETE_DUMP_RETRIES`
  (3) times in a row, the count starting again after a complete dump; then
  it carries on without a video mode. The port does that in the session
  (`Session.onData`, `incompleteDumpRetries`; the state still stores InCm,
  which `TestParsersMatchAtemwire` allows for). What remains different is
  the end: where atemwire carries on and `wait_ready` waits out its
  timeout, Dial fails at once with `ErrIncompleteDump`, naming the
  sequence number that arrived first (`transport.firstRX`) and any parse
  error during the dump (an unknown `VidM` mode, §5.4 item 3, ends here
  too). After Dial, the last dump without a video mode is logged and the
  session carries on, as atemwire's does. The silent loss of whatever
  else was in a lost first packet remains (see "Traps").
  `TestLosingTheFirstDumpPacketRehandshakes`,
  `TestAFirstDumpPacketAlwaysLostFailsDial`.
- **bmdwire#14: `*XFC` is dropped.** It is the OpenSwitcher TCP proxy's
  transfer-complete event; no switcher sends it over UDP. atemwire's
  handler indexes the store's transfer queue: with none it raises
  `KeyError`, losing the rest of the packet; with one it pops a real
  transfer and may raise `upload-done`. Here the parser entry drops it
  unparsed whatever its length, so it can never raise and never touch the
  transfer lane. `TestXFCDoesNotLoseTheRestOfThePacket`, `TestXFCIsDropped`.
  bmdwire 1.4.0 drops it too on UDP (bmdwire#28): no longer a difference.
- **bmdwire#15: `LKST` is stored taken and released, per store.** atemwire
  drops a lock-taken `LKST` and stores releases bare, so stores 0 and
  0xFFFF overwrite each other and nobody can see that ASC or another
  client holds the media lock. Here `State.Locks[store]` holds the latest
  `LKST` for each store and every one is published (`Change.Key`
  `LKST:<store>`). The packet doesn't name the holder: a lock taken while
  this session holds none is another client's. atemwire's reactions to a
  release (forget our lock, re-request for queued work) stay release-only.
  `TestAnotherClientsLockIsVisible`, `TestLockState`. bmdwire 1.4.0 stores
  a lock taken too and raises it (bmdwire#29), still under one bare key,
  the latest whatever its store: the per-store keys remain the difference.
- **bmdwire#16: a Fairlight strip op with no `sourceId` encodes the
  stereo one.** For a missing or empty `sourceId`,
  `_resolve_source_id` (`atemwire/macrotransfer/fairlight.py:455-457`)
  returns 0xFFFFFFFFFFFF0000 (-65536), although its comment says the
  stereo value, -65280, which is 0xFFFFFFFFFFFF0100. Every live capture
  in bmdwire's tests carries 0xFFFFFFFFFFFF0100, and so do all 24 strip
  ops in the macro downloaded on the fourth hardware run. Here the
  default is 0xFFFFFFFFFFFF0100. The decode is unchanged (unsigned, see
  "Macro codec"). `TestMissingSourceIDIsStereo`,
  `TestStereoSourceIDRoundTrips`. bmdwire 1.4.0 does the same
  (bmdwire#30): the oracle, regenerated at v1.4.0, holds bmdctl's bytes
  for the 42 cases `TestEncodeMatchesAtemwire` used to rewrite.

## SURFACE.md and the code

Where `docs/SURFACE-atem.md` and bmdwire v1.3.0's code disagree, the port
follows the code.

- **Version.** SURFACE.md says it describes 1.2.0 (`028ac26`); the port
  follows v1.3.0 (`cea58c2`), which has every fix §5.4 marks as made after
  1.2.0, and the fixes merged after 1.3.0 that §5.4 also marks
  (`b89fcb7`, `d86ed31`, `4011dcb`; "Source of truth").
- **§1 conventions, source names.** The private table also maps 4030 to
  `CleanFeed1` (alongside 7001); SURFACE.md's list leaves 4030 out.
- **§2.4 `audio-input` (`AMIP`)** was listed as indexed by source; atemwire
  keys it by strip id, `"<source>.0"`, because the class carries a
  `strip_id` and `save_field_data` puts that in place of the first index.
  v1.6.0's SURFACE.md lists it by strip, so the two agree from then on.
- **§2.5 `lock-state` (`LKST`)** is listed as stored bare; atemwire stores
  only releases. A packet reporting a lock taken is dropped before storage,
  with no events. (The port stores both, per store: bmdwire#15.)
- **§2.5 `transfer-complete` (`*XFC`)** is listed as consumed (TCP proxy
  only); on UDP its handler looks up a transfer queue that doesn't exist and
  raises `KeyError`, losing the rest of the packet. (The port drops it:
  bmdwire#14.)
- **§5.4 item 8** names `OSError` and `struct.error` as the exceptions a
  macro encoder leaks; `OverflowError` (an infinite fixed-point value) and
  a `ValueError` for an address with an embedded NUL exist too.
- **§3.2 "Fixed-point values are Q16.16"** holds, but signed values are
  range-checked (`ValueError`) while unsigned ones are masked silently.
- **§4.2 "after 5 s with no packets"**: the timeout is `select()` on the
  socket and the outbound queue, so it is 5 s with no datagram in (from
  anyone) and no packet out. A client that keeps sending to a dead switcher
  never times out. (The port counts accepted inbound packets only:
  bmdwire#11.)
