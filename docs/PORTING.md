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
  completion, man pages and `doctor`. Out of scope until asked (phase 4):
  HTTP, MCP, and the HyperDeck, Videohub and Ultimatte packages beyond
  their `doc.go`.

## Licensing

- `atem/` is a port of atemwire, which is LGPL-3.0-only (a fork of pyatem),
  so it is LGPL-3.0-only too. `atem/LICENSE` and `atem/LICENSE-gpl3.txt`
  are copied from bmdwire; `atem/NOTICE` gives the lineage. Every file in
  `atem/` starts with both copyright lines and the SPDX identifier.
- `internal/fixtures/` holds bytes and files copied from atemwire's test
  suite, so it carries atemwire's licence as well.
- Everything else (the CLI, the other device packages, `internal/table`,
  `internal/fakeatem`, `internal/engine`, `internal/paths`) is MIT, as
  bmdwire's non-ATEM packages are.

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
  payloads fed to bmdwire v1.3.0's real `save_field_data` and what it did
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

bmdwire v1.3.0's 88 readers (the `atemwire.messages.*` functions whose
first parameter is `mx`, SURFACE.md §2.1) are methods on `State`, one file
per atemwire module (`read_switching.go`, `read_transition.go`, ...), so
callers never see wire units. `read_state.go` holds what they share from
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
- **Not ported:** the read-modify-write operations built on readers
  (`toggle_usk`, `toggle_dsk`, `toggle_transition_*`, `set_transition_rate`)
  and `build_full_state`; both come later.
- **Verification.** `internal/fixtures/oracle/readers.json`
  (`gen_readers.py`) holds 23 scenarios: 2,129 packets fed to bmdwire
  v1.3.0's `save_field_data`, interleaved with 7,048 reader and helper
  calls and their results. The scenarios cover the empty state, all-0x00,
  all-0xFF and all-0x80 payloads, random 1-4 M/E switchers with partial
  packet sets, split and stereo Fairlight strips, names, HyperDeck
  planning, every video mode, and chroma sample values on decimal rounding
  ties. Each call uses every index the state has plus out-of-range ones.
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
`locks`, `_lock_release_pending`, `_lock_requested`, `transfer_budget`,
`_transfer_trigger`, `_release_then_continue`, `_queue_chunks`,
`_queue_flushed`, and the LKOB, lock-state, FTCD, FTDa, FTDE and FTDC
branches of `save_field_data`), `_transfer.py` (`TransferTask`), the bulk
send queue of `transport.py`, and the callers that wait on it:
`connection.py` (`download_still`, `clear_still`, `download_macro`),
`macrotransfer/_io.py` (`upload_macro_bytecode`) and WebATEM's
`atem_control/uploader.py` (still upload).

API: `DownloadStill(ctx, slot) ([]byte, error)`, `UploadStill(ctx, slot,
img, StillOptions)`, `UploadStillFrame(ctx, slot, frame, name,
description)`, `ClearStill(ctx, slot)`, `DownloadMacro(ctx, slot)`,
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
    repeated within a second (`LOCK_REREQUEST_SECONDS`) and deferred while
    our own unlock's echo is pending; on any release with work queued, an
    immediate re-request (the pounce);
  - the lock released after every frame, the LKST echo starting the next
    task; the lock-exempt macro store going on at once; with nothing
    queued, every held lock released (left marked held until the echo);
  - FTDE 1 retries with the same transfer id, 5 asks for the lock again, 6
    gives the lock back and keeps the task; any other status drops the task
    and releases the lock; FTDa, FTDE and FTDC with no transfer in flight or
    another transfer id are ignored;
  - FTDC pops the queue head and releases the lock before the RLE decode;
    each downloaded chunk is acknowledged with FTUA(tid, 0);
  - FTCD's size is rounded down to whole words and its transfer id isn't
    checked; a chunk never ends on an RLE header or a header and its count;
    an empty upload sends FTFD with FTSD (no budget comes); FTSD's mode is 1
    (write RLE) for stills and 0x0300 (write, pre-erase) for macros.
- **Results go to the task, not to events.** atemwire raises
  `download-done`, `upload-done`, `clear-dispatched` and
  `file-transfer-error`; its callers filter by store and slot, attribute any
  fatal FTDE to whatever they wait on (§5.4 item 13) and are serialised per
  connection (`_transfer_serial_lock`) to make that safe. Here the engine
  finishes the task it is acting on, so a result can't reach the wrong
  caller and callers can overlap. A fatal status is a `*TransferError`
  naming the status as `FileTransferErrorField.__repr__` does. The progress
  events (`transfer-progress`, `upload-progress`) are not ported: no
  callback runs on the owner goroutine.
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
  is sent; atemwire checks only `is_connected` and queues into a session
  that is re-handshaking.
- **The bulk queue** is the transport's `bulk` (`send_queue`). Upload chunks
  wait there. Each packet from the switcher once ACKs flow, and a control
  frame a second time, sends up to five (`batch_size`), as `receive_packet`
  calls `queue_trigger`; a drained queue is reported once and handled
  before that packet's data, as the `TransferQueueFlushed` sentinel is;
  `_queue_chunks` triggers too and drops the report, as atemwire does.
  atemwire sleeps 3 ms after each batch on its protocol thread; the owner
  goroutine can't sleep (it sends the ACKs), so a trigger within 3 ms of the
  last batch is deferred to the 3 ms mark instead. `aggressive_drain` is
  set only by `AtemProtocol`'s constructor and isn't ported. The queue's
  1024 cap drops the oldest packet with an error, as the deque does, and
  the queue outlives a disconnect, as atemwire's does.
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
  handlers, and cancellation is per task. `transfer_fake_test.go` runs
  transfers end to end against the fake switcher: still and macro round
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
  while others raise; MiddleCentre raises; a missing `MacroSleep.frames` is
  0; marker bytes are rewritten; strip/master fader gain and HyperDeckInput
  overflow raise `struct.error`; HyperDeck addresses accept whatever glibc
  `inet_aton` accepts. `sourceId` decodes unsigned (`18446744073709486336`
  for stereo), the form bmdwire says ATEM Software Control writes
  (`_resolve_source_id`'s docstring, `docs/PROFILE_FORMAT.md`), and the
  encoder takes the signed form too.
- **Two fixes** (see "Deliberate differences"): a Fairlight strip op with
  no `sourceId` gets the stereo one, and a silenced Fairlight gain decodes
  as `-327.68`.
- **Not matched:** Unicode classification uses Go's tables (17.0) where
  CPython 3.12 uses 15.0, so characters assigned since differ as digits and
  in `repr()` escapes; Go strings that aren't valid UTF-8 act as U+FFFD;
  messages follow CPython 3.12's wording. The per-op "decode failure"
  warning is not logged (the op's decodeError holds it).
- **Verification.** `internal/fixtures/oracle/macro.json` (`gen_macro.py`,
  CPython 3.12.3) holds 1,987 bytecode cases (the 32 `macro/` samples,
  random/short/edge parameter blocks for every op code and unknown ones,
  multi-op streams) and 4,272 op dicts from attribute pools.
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
- **Fairlight strip order:** atemwire writes a source's strips in FASP
  arrival order, which the state doesn't keep, so they go in subchannel
  order (the order a switcher sends them).
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
- **Verification:** `oracle/profile.json` from `gen_profile.py` holds the
  golden's state, 46 saves, 274 applies (18,628 events, 30 raising) and 130
  parses, all replayed byte for byte. The golden's state holds a TStP media
  player of 3010, more than a u8, so the golden test injects that one value
  through the saver's `stingerSource` hook. `profile_test.go` ports
  bmdwire's profile tests one for one; `e2e_test.go` applies and saves
  through a Session against the fake switcher.
- **Save is verified on hardware** (the fourth run, docs/HARDWARE.md):
  `profile save` from a Constellation wrote the XML with its six macros,
  and the one used still as a PNG beside it. Apply (`profile restore`)
  is not yet verified on hardware.

## Command table and CLI (`internal/table`, `atem/table.go`, `cmd/bmdctl`)

- **`internal/table` is generic** (and MIT). An `Entry` is
  `{Device, Path, Summary, Params, Build, Operation, Local, Stream, Echo,
  Unverified, Destructive, AtLeastOne}`; a `Param` has a name, a kind
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
    about audio. On a classic switcher the engine (`forMixer`, after the
    arguments parse) sends `classic-audio master` for `audio master` and
    `classic-audio levels` for `audio levels`, with the same arguments:
    a Fairlight-only one (`--eq`, `--eq-gain`, `--makeup`) is refused,
    naming the classic command, and `--volume`, in dB as Fairlight's is,
    goes on the classic mixer's scale, which atemwire leaves raw (0-65381):
    32768 × 10^(dB/20), rounded, 32768 being 0 dB and 65381 +6 dB, the
    classic fader's top; -inf and anything below the scale are 0. (The
    scale is the one the classic mixer's u16 is known to hold; it is
    unconfirmed on hardware, as the classic commands are.)
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
  input) take an id or one of the switcher's input names, long or short,
  case-insensitive, surrounding spaces ignored (`atem.Resolver`,
  `State.InputsNamed`). A name two inputs share is refused with both
  listed. A numeric id is not checked against the inputs: audio sources
  such as 1301 have no `InPr`. Names need the switcher's state, so they
  resolve in the engine; `--dry-run` takes ids only.
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
  `--timeout`, `--help`/`-h`. One-letter forms are single-dash only,
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
    again by the engine. An unverified one prints a line saying so on
    stderr, unless `--json`.
- **Output.** `--json` prints each engine response line exactly as
  received, and a failure the CLI finds itself (a usage error, a
  destructive command without `--yes`, no host) in the same
  `{ok:false, code, error}` shape on stdout, so a script reads one form.
  `--quiet` prints nothing on stdout, `--json` or not; errors still go to
  stderr. Otherwise a result is one line: a wire command (or an
  operation that sends one) is its path, `confirmed` or `sent`, the time
  taken and the arguments as sent (indexes from 1, sources with their names,
  enums by name), in fixed columns so the lines of `exec` line up.
  Operations print one aligned line per row of their result. Colour (the
  status, what is on air) only when stdout is a terminal, and never with
  `NO_COLOR` set or `TERM=dumb`, nor with `--json`.
- **Every command runs.** `TestEveryCommandRuns` sends every entry in the
  table, with sample arguments, through an engine to the fake switcher
  and requires it to succeed (the one exception, `media
  clear-unlocked`, is ignored by the fake as some models ignore it).
- **Exit codes** (phase 3): 0 done, 1 usage (including a command
  `Validate` refuses) or any other failure, 2 the switcher or the engine
  unreachable, 3 sent but not confirmed in time, 4 refused by the
  switcher, 5 destructive without `--yes`. Phase 2's were 0, 1 refused or
  unreachable, 2 usage.
- **`docs/COMMANDS.md`** is generated by `Registry.Markdown`; the golden test
  in `cmd/bmdctl` fails when it is stale and `-update` rewrites it.

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
  columns so they align; errors go to stderr prefixed `line N:`.
  `--json` prints one response line per command. `exec`, `engine`,
  `completion`, `man` and `doctor` can't be lines.

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
- **`docs/COMMANDS.md`** starts with that same CLI reference, then the
  table.

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
  `%AppData%\bmdctl` on Windows): a `default` and a `hosts` list of
  `name`, `address` (`host` or `host:port`) and `device` (`atem`,
  `hyperdeck`, `videohub`, `ultimatte`; `atem` when left out). The
  standard library has no YAML reader, so `internal/hosts` reads exactly
  this shape of YAML (block list, plain or quoted scalars, comments,
  items at the key's indent or deeper) and refuses anything else with a
  line number; it writes the same shape back atomically. `bmdctl hosts
  add|rm|list|default` edit it.
- **Names** are a letter, then letters, digits, `-` and `_`, matched
  without regard to case, so no name reads as an address. Addresses are
  an IP literal or a DNS name, with an optional port.
- **Which host.** `--host` (a name, else an address), else
  `$BMDCTL_HOST`, else the host the last `--host` named, else the default.
  Only `--host` sets "last used" (`$XDG_STATE_HOME/bmdctl/last-host`), and
  only when the switcher was reached; `hosts default NAME` sets it too,
  so commands go to the new default at once. The default is the
  configured, portable choice; last used is this machine's memory of
  the most recent explicit one.
- **Which device.** A device word on the line (`bmdctl atem ...`) wins;
  else a named host's `device`; else ATEM, so a bare address is an ATEM.
  A device word that disagrees with the named host's type is refused.
  Device words are reserved: the ATEM command that binds a HyperDeck slot
  is `hyperdeck-slot`, not `hyperdeck`, which is the HyperDeck device
  (phase 4); a test keeps every command's first word off the device
  names.

## Engine (`internal/engine`)

A background process holding one `atem.Session` per switcher, so a
command costs a round trip instead of a connect and a whole state dump,
and the switcher's live state is at hand to read a command line. It
replaces atemwire's connection pool (`acquire_connection`, reference
counts, a 2 s grace timer) with the policy phase 3 asked for: a session
lives until 10 minutes pass without a request, unless pinned.

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
- **Wire commands** are built and validated in the engine, then sent:
  with an echo key through `SendAndWait`, waiting `timeout_ms` (default
  5 s, `--timeout`), `confirmed` once it arrives; without one through
  `Send`, `confirmed` false, as atemwire's `send()` leaves it. The result
  is `{args, command, echo, state}`: the arguments for display, the
  opcode, the key waited for and the echoed packet.
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
  for requests to end.

## Operations (`internal/engine/ops_*.go`)

The commands that aren't one wire command. Each is a table entry with
`Operation` set and a function in the engine; the CLI only formats what
comes back. Indexes in results count from 1, as the flags do
(`table.Index`, above).

- **Group a, the session** (`ops_session.go`).
  - `status` reads the state through atemwire's readers: per M/E program
    and preview with input names, the transition (style, rate as
    seconds:frames, next layers, position, preview), fade to black and
    each keyer; each DSK, aux output, the macro player and recorder, and
    recording and streaming when the switcher has them. Streaming's
    `StRS` is named as SURFACE.md §2.5 observed it (on air is 4, not a
    bit).
  - `connect` opens the session and pins it (`--pin false` only opens
    it); `disconnect` closes it, pinned or not, and never dials one.
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
    engine's frames, unchanged.
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
    each used still as `ATEM Media Pool/<name>.png` beside the XML (the
    path the XML's `<Still>` names; `Slot<n>` for a still without one,
    a failing slot skipped and reported, as atemwire logs and skips it);
    `profile images` does only that, into `--dir`. Restore's CLI reads
    each `<Still path>` it can find beside the XML and sends it; after
    the apply the engine uploads each to its `index` under its `name`
    (PNGs premultiplied, as `media upload` does), reporting the ones with
    no file.
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
  lock, broadcast as `LKST`. `Commands()` and `Handshakes()` let tests look
  inside.
- **Transfers** (`transfers.go`). The fake keeps a media pool and a macro
  pool. The session's PLCK or LOCK is granted (LKOB, and LKST to all) unless
  another client holds the lock; then a PLCK waits for its release and a
  LOCK is dropped. CSTL works only under the lock. FTSU is served in
  1296-byte chunks, 16 in flight, each freed by its FTUA, then FTDC; an
  empty slot is an FTDC alone. FTSD (lock held for stills, mode 1 for
  stills and 0x0300 for macros, else FTDE 2) gets 16-chunk budgets until
  the announced uncompressed length is in (it counts RLE runs); FTFD stores
  the slot and broadcasts its MPfe or MPrp. `FailTransfers(statuses...)`
  answers the next requests with FTDE (5 also takes the lock away, 6 leaves
  it held); `PutStill`, `Still`, `PutMacro`, `Macro` and `LockedBySession`
  fill and inspect it. An empty macro upload (FTSD of length 0, then FTFD)
  empties the slot, following atemwire's comment that ASC's "Restore from
  State" uses it for empty macros; profile apply also sends one to restore
  a macro's name without ops, and whether a switcher keeps that name is
  unverified on hardware.
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
- **A lost command is recovered only by a later one.** With no client
  retransmit timer (by design, SURFACE.md §4.2), a reliable packet the
  switcher never sees is resent only when a later packet shows the switcher
  the gap. `SendAndWait` on that command times out.
- **Parse errors lose the rest of the packet** (session section), including
  a 23-byte `MPfe`, a `KeFS` under two bytes, and a `_VMC` bitfield naming
  mode 30 or 31.

## Deliberate differences

Bugs in bmdwire v1.3.0 the port fixes instead of copying. Each is filed
against bmdwire (`github.com/lucas-romanenko/bmdwire/issues/<n>`), and each
has a test that fails on bmdwire's behaviour: against the fake switcher
(`atem/fake_test.go`), or for the macro codec in
`atem/macro/differences_test.go`.

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
  `TestSilenceTimeoutReconnects`.
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
- **bmdwire#13: a dump without a video mode fails Dial at once.** The
  issue offers two fixes. (A) expects a new session's first reliable
  sequence number, so a lost first packet stalls the ACK and is resent
  like any other. (B) fails fast. A needs that number to be fixed, and
  nothing in bmdwire v1.3.0 proves it: no test or fixture holds a captured
  handshake; the ACK-gap tests use 10 as the first reliable sequence, the
  handshake test 1 after an unreliable 5; the one "first dump packet"
  with sequence 1 (`test_lock_delivery.py`, `_data_packet(seq=1)`) is a
  hand-built literal; and the fake switcher's dump starts at 1 only
  because this port wrote it so. Other clients assume 1, but an assumption
  that is wrong would make every connect stall on a gap that doesn't
  exist. So the port takes B: when `InCm` is stored and the state has no
  video mode, Dial fails at once with `ErrIncompleteDump`, naming the
  sequence number that arrived first (`transport.firstRX`) and any parse
  error during the dump (an unknown `VidM` mode, §5.4 item 3, ends here
  too). atemwire waits out `wait_ready`'s 15 s. After Dial a dump without
  a video mode is logged as an error and the session carries on, as
  atemwire's does; a forced re-handshake could loop against a switcher
  whose `VidM` never parses. The silent loss of whatever else was in a
  lost first packet remains (see "Traps"). `TestLosingTheFirstDumpPacketFailsDialFast`.
- **bmdwire#14: `*XFC` is dropped.** It is the OpenSwitcher TCP proxy's
  transfer-complete event; no switcher sends it over UDP. atemwire's
  handler indexes the store's transfer queue: with none it raises
  `KeyError`, losing the rest of the packet; with one it pops a real
  transfer and may raise `upload-done`. Here the parser entry drops it
  unparsed whatever its length, so it can never raise and never touch the
  transfer lane. `TestXFCDoesNotLoseTheRestOfThePacket`, `TestXFCIsDropped`.
- **bmdwire#15: `LKST` is stored taken and released, per store.** atemwire
  drops a lock-taken `LKST` and stores releases bare, so stores 0 and
  0xFFFF overwrite each other and nobody can see that ASC or another
  client holds the media lock. Here `State.Locks[store]` holds the latest
  `LKST` for each store and every one is published (`Change.Key`
  `LKST:<store>`). The packet doesn't name the holder: a lock taken while
  this session holds none is another client's. atemwire's reactions to a
  release (forget our lock, re-request for queued work) stay release-only.
  `TestAnotherClientsLockIsVisible`, `TestLockState`.
- **bmdwire#16: a Fairlight strip op with no `sourceId` encodes the
  stereo one.** For a missing or empty `sourceId`,
  `_resolve_source_id` (`atemwire/macrotransfer/fairlight.py:455-457`)
  returns 0xFFFFFFFFFFFF0000 (-65536), although its comment says the
  stereo value, -65280, which is 0xFFFFFFFFFFFF0100. Every live capture
  in bmdwire's tests carries 0xFFFFFFFFFFFF0100, and so do all 24 strip
  ops in the macro downloaded on the fourth hardware run. Here the
  default is 0xFFFFFFFFFFFF0100. The decode is unchanged (unsigned, see
  "Macro codec"). `TestMissingSourceIDIsStereo`,
  `TestStereoSourceIDRoundTrips`. `TestEncodeMatchesAtemwire` rewrites
  bmdwire's bytes for the 42 oracle cases this changes.
- **bmdwire#17: a silenced Fairlight gain decodes as `-327.68`.** A
  silenced fader is recorded as -327.68 dB in Q16.16,
  -21474836 (bytes `ec 51 b8 fe`). `_fmt_scalar` (`_helpers.py:90-97`)
  prints six decimals, `-327.679993`; bmdwire's `docs/PROFILE_FORMAT.md`
  (lines 802-804) says ATEM Software Control writes `-327.68`. Here
  every Fairlight `gain` (strip and master faders, EQ, EQ band, input,
  dynamics, headphones) decodes that one value as `-327.68`, which
  encodes back to the same bytes. Other values keep `_fmt_scalar`'s
  form. bmdwire's comments suggest ASC writes six significant digits
  throughout (`-0.0800018` where `_fmt_scalar` gives `-0.080002`,
  `fairlight.py:45-49`), but two examples are too little to change every
  value on. `TestSilencedGainIsASCs`, `TestSilencedGainOnly`.

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
