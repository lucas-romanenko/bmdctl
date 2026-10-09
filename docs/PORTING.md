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
  `LKST` is stored only when it reports a release, as atemwire does.
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
  (atemwire's `ValueError`, §5.4 item 3); `*XFC` is an error once parsed
  (atemwire's `KeyError` looking up a transfer queue that doesn't exist);
  `CCdP` with data type 5 or an unknown type yields empty data; `LKST`
  reporting a lock taken is dropped unstored. All of these are what bmdwire
  v1.3.0 does.
- **Keys.** `Change.Key` is the opcode and the indices atemwire's
  `KEY_FORMAT` reads, with the strip id in place of the first index for
  every packet that carries one (`FASP`, `AEBP`, `AICP`, `AILP`, `AIXP`
  and also `AMIP`, so `State.Audio.Inputs` is keyed `"<source>.0"`, as
  atemwire's `mixerstate['audio-input']` is).
- **Consumed packets.** `LKOB`, `FTCD`, `FTDC`, `FTDa` and `FTDE` are parsed
  and dropped: their consumer, the transfer engine, is a later phase.
  `LKOB` is the one with a side effect: with nothing queued, atemwire's
  `_transfer_trigger` releases a granted lock at once, so the session
  sends `LOCK(store, false)` when one arrives.
- **The last-run latch.** Storing an `MRPr` with the running bit set and an
  index other than 0xFFFF sets `State.Macros.LastRun` (bmdwire v1.3.0
  `a9f3646`: slot 0 counts). A disconnect empties the state but keeps the
  latch, as `ATEMConnection.last_run_macro_index` survives one.
- **Floats.** Values atemwire computes in floating point are computed the
  same way (same operations, same order), so they match bit for bit,
  except the classic mixer's meters (`AMLv`), where Go's `math.Log10` and
  CPython's libm `log10` can differ in the last place.
- **Verification.** `internal/fixtures/oracle/recv.json` holds 1,062
  payloads fed to bmdwire v1.3.0's real `save_field_data` and what it did
  with each. `TestParsersMatchAtemwire` replays every case: same error or
  not, same consumed/raw/stored outcome, same key, same attribute values
  (floats to 1e-12), and the value sits in the `State` tree under the
  field tagged with its opcode at the indices the key names. Every parser
  has cases. `parse_test.go` also ports, one for one, the assertions
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

## Command table and CLI (`internal/table`, `atem/table.go`, `cmd/bmdctl`)

- **`internal/table` is generic** (and MIT). An `Entry` is
  `{Device, Path, Summary, Params, Build, Echo, Unverified, Destructive,
  AtLeastOne}`; a `Param` has a name, a kind (uint, int, float, bool,
  string, enum, flags, rate, time, list), an optional range, a unit, help,
  and required/default. A `Registry` checks entries as they are added (no
  duplicate paths, no path that is both a command and a group), resolves
  command words, parses `--name value` arguments and renders
  `docs/COMMANDS.md`.
- **Device packages fill it explicitly.** `atem.RegisterCommands(r)` adds
  the ATEM entries; the CLI calls it. No `init()` side effects, so a
  program that imports `atem` without the CLI gets no table.
- **One entry per command**, 75 of them: every command except the seven
  lock and file-transfer ones (§1.19), which a CLI reaches only through
  transfers (later phase). Paths group by SURFACE §1 table: `program`,
  `transition wipe`, `usk dve`, `fairlight strip`, ... Each parameter maps
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
- **Rates** take frames or "seconds:frames". With no switcher to ask, the
  CLI converts at `--fps` (default 25, atemwire's fallback); once it talks to
  a switcher it will use `DisplayFPS` of the session's state.
- **The CLI** uses the standard library only. `bmdctl version` prints the
  GoReleaser `-ldflags` version, else the build info. `bmdctl atem --help`
  and `--help` after any group or command print that level's help;
  `--dry-run`, `--fps` and `--help` are accepted anywhere on the line.
  `--dry-run` validates the command and prints its bytes (header included)
  as hex on stdout, warnings for destructive and unverified commands on
  stderr, and sends nothing. Without `--dry-run` it refuses (exit 2): no
  command talks to a switcher in this phase. Exit codes: 0 done, 1 the
  command was refused (`Validate`), 2 a usage error.
- **`docs/COMMANDS.md`** is generated by `Registry.Markdown`; the golden test
  in `cmd/bmdctl` fails when it is stale and `-update` rewrites it.
- **Not yet:** source names (atemwire has no public source table, only
  the macro codec's private one), read-modify-write operations (toggles,
  `set_transition_rate`), reading state from the CLI, and sending.

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
  send; anything else is ACKed and changes nothing. It is not a switcher
  model and makes no claim about switcher timing.
- **Controls.** `DropToClient(n)` drops the next n reliable packets (their
  resends get through); `DropSequences(...)` drops chosen sequence numbers
  of the next session; `DropFromClient(n)` ignores the next n datagrams
  from the session; `DelayACKs(d)` ACKs late; `Silence(true)` cuts the
  cable: nothing in or out, and the switcher forgets the session, as a real
  one does when its packets go unacknowledged. `Commands()` and
  `Handshakes()` let tests look inside.
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
  `-short`).

## Traps inherited from atemwire

Behaviour the port reproduces because bmdwire v1.3.0 has it, found while
porting and not listed in SURFACE.md §5.4. Each is a candidate for a
deliberate fix later, recorded here as such when it is made.

- **The first dump packet is not recoverable.** The transport takes the
  first reliable packet it sees as its sequence baseline (`_rx_established`,
  `_deliver_next`). If the switcher's first packet after the handshake is
  lost, the second becomes the baseline, the ACK covers both, and a late
  copy of the first is dropped as already delivered: its fields never
  arrive. If they include the video mode, connecting times out.
  `TestLosingTheFirstDumpPacketLosesItForGood` pins it.
- **Stale packets during a re-handshake.** Packets from the previous
  session that arrive after the re-SYN reset the 5 s silence timer, so the
  transport doesn't re-SYN again while they keep coming, and the first
  reliable one sets the delivery cursor, so the new session's packets
  (starting again at 1) look like duplicates. The first effect stalled
  the fake switcher's reconnect test until the fake learned to forget a
  silenced session; the second follows from the same code, read rather
  than observed. The Python logic is the same line for line.
- **A lost command is recovered only by a later one.** With no client
  retransmit timer (by design, SURFACE.md §4.2), a reliable packet the
  switcher never sees is resent only when a later packet shows the switcher
  the gap. `SendAndWait` on that command times out.
- **Parse errors lose the rest of the packet** (session section), including
  a 23-byte `MPfe`, `*XFC` on UDP, a `KeFS` under two bytes, and a `_VMC`
  bitfield naming mode 30 or 31.

## SURFACE.md and the code

Where `docs/SURFACE-atem.md` and bmdwire v1.3.0's code disagree, the port
follows the code.

- **Version.** SURFACE.md says it describes 1.2.0 (`028ac26`); the port
  follows v1.3.0 (`cea58c2`), which has every fix §5.4 marks as made after
  1.2.0.
- **§1 conventions, source names.** The private table also maps 4030 to
  `CleanFeed1` (alongside 7001); SURFACE.md's list leaves 4030 out.
- **§2.4 `audio-input` (`AMIP`)** is listed as indexed by source; atemwire
  keys it by strip id, `"<source>.0"`, because the class carries a
  `strip_id` and `save_field_data` puts that in place of the first index.
- **§2.5 `lock-state` (`LKST`)** is listed as stored bare; only releases
  are stored. A packet reporting a lock taken is dropped before storage,
  with no events.
- **§2.5 `transfer-complete` (`*XFC`)** is listed as consumed (TCP proxy
  only); on UDP its handler looks up a transfer queue that doesn't exist and
  raises `KeyError`, losing the rest of the packet.
- **§4.2 "after 5 s with no packets"**: the timeout is `select()` on the
  socket and the outbound queue, so it is 5 s with no datagram in (from
  anyone) and no packet out. A client that keeps sending to a dead switcher
  never times out.
