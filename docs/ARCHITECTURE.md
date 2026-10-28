# Architecture

How bmdctl is put together, for someone about to change it. The decision
log behind every choice here is [PORTING.md](PORTING.md); the specs for
the device packages are [SURFACE-atem.md](SURFACE-atem.md),
[SURFACE-hyperdeck.md](SURFACE-hyperdeck.md),
[SURFACE-videohub.md](SURFACE-videohub.md) and
[SURFACE-ultimatte.md](SURFACE-ultimatte.md). bmdctl's device packages
are a Go port of bmdwire's Python libraries, following v1.8.0 (the
`Tracks:` line in PORTING.md); where this page says "atemwire",
"hyperdeckwire", "videohubwire" or "ultimattewire" it means bmdwire's
packages.

## The shape of it

```
  a person, a script,      an AI agent            an HTTP client
  cron, exec               (MCP over stdio)       (JSON, SSE)
        |                        |                      |
   bmdctl <command>         bmdctl mcp            bmdctl serve
   cmd/bmdctl               internal/mcp          internal/serve
        |                        |                      |
        +------------------------+----------------------+
                                 |
             one JSON request per connection, over a
             Unix socket (a named pipe on Windows)
                                 |
                   bmdctl engine  (internal/engine)
        sessions, parsing, operations, notes, undo, transfers
         /                 |                  |                 \
  atem.Session    hyperdeck.Session   videohub.Session   ultimatte.Client
  UDP 9910        TCP 9993            TCP 9990           TCP 9998, 9996
       |                 |                  |                  |
  ATEM switcher      HyperDeck          Videohub           Ultimatte
```

Every way in ends at the engine, and only the engine talks to devices.
The CLI, `mcp` and `serve` turn their input into the same request and
print, encode or serve the same response.

## Packages

| Package | Licence | What it is |
|---|---|---|
| `atem` | LGPL-3.0 | The ATEM protocol: UDP transport, session, typed state, the parsers for every state packet, every command, the readers, transfers, still conversion |
| `atem/macro` | LGPL-3.0 | The macro bytecode codec |
| `atem/profile` | LGPL-3.0 | Profile save and apply, ATEM Software Control's XML |
| `hyperdeck` | MIT | The HyperDeck Ethernet Protocol: session, state from notifications, commands |
| `videohub` | MIT | The Videohub Ethernet Protocol: session, the preamble and pushed blocks, routing and labels |
| `ultimatte` | MIT | The Ultimatte's two channels: the prelude and the network block protocol on 9998, slots and resources on 9996, Smart Remote's archive and restore |
| `internal/table` | MIT | The command table every device fills |
| `internal/engine` | MIT | The background engine, its socket protocol and the operations |
| `internal/hosts` | MIT | hosts.yml: reading, writing, resolving a name |
| `internal/paths` | MIT | Where the config, state, cache and socket live, per platform |
| `internal/mdns` | MIT | The Bonjour browse behind `hosts discover` |
| `internal/stillcache` | MIT | The media pool's stills on disk by hash, so a profile save writes a still it has seen without downloading it again |
| `internal/serve` | MIT | The HTTP API and its OpenAPI document |
| `internal/mcp` | MIT | The MCP server |
| `internal/fakeatem` | MIT | A fake switcher at the `net.PacketConn` level |
| `internal/fakehyperdeck` | MIT | A fake deck on any `net.Conn` |
| `internal/fakevideohub` | MIT | A fake router on any `net.Conn` |
| `internal/fakeultimatte` | MIT | A fake keyer, both its ports, on any `net.Conn` |
| `internal/fakenet` | MIT | A network of fakes at addresses, for the device probe and the CLI's tests |
| `internal/pytext` | MIT | CPython's text rules (decode, strip, split, `int()`, `float()`), for the ports' parsers |
| `internal/fixtures` | LGPL-3.0 | atemwire's test data and the oracles generated from atemwire |
| `internal/release` | MIT | Tests of install.sh and the Scoop manifest |
| `cmd/bmdctl` | MIT | The CLI: parsing, output, exec, completion, man page, doctor |

The device packages use the standard library only. They know nothing
of the table, the engine or the CLI: a program can import `atem` and
dial a switcher with no bmdctl around it.

## The command table

`internal/table` holds one `Entry` per command: its device, its words
(`usk on-air`), its syntax (`usk <keyer> on-air <on-air>`), a summary,
typed parameters (kind, range, unit, default, whether it counts from 1),
examples, and flags such as `Destructive`, `Unverified` and the echo it
waits for. There are two kinds:

- **A wire command** has a `Build` that turns parsed arguments into one
  `atem.Command`, by reflection onto the command's fields, so a range
  can't promise more than the field holds.
- **An operation** has no `Build`; the engine runs it by name. It reads
  state first (`usk toggle`, `transition rate`), runs a transfer
  (`media upload`), or is a read (`status`, `watch`). A `Local`
  operation (`macro decode`) runs in the CLI with no engine.

The devices fill the registry explicitly, with no `init()`:

```
  atem.RegisterCommands -------+
  hyperdeck.RegisterCommands --+
  videohub.RegisterCommands ---+--> table.Registry  (engine.NewRegistry)
  ultimatte.RegisterCommands --+          |
  engine.RegisterOperations ---+          +--> ParseLine: CLI, exec, serve, mcp
                                          +--> --help, bmdctl __complete
                                          +--> docs/COMMANDS.md, bmdctl(1)
                                          +--> GET /openapi.json
                                          +--> MCP tools, one per group
```

A Videohub's and an Ultimatte's `RegisterCommands` add their groups
alone: every command of theirs is an operation, since a router's port
is named by a label only its state knows and a keyer's calls each open
their own connections.

So a new command is a table entry, and the parser, help, completion,
the reference, the man page, the HTTP route and the MCP tool all come
from it. `TestCommandsDoc` fails when docs/COMMANDS.md or a group's page in
docs/commands/ is stale;
`TestEveryCommandRuns` sends every entry to the fake switcher;
`TestExamples` runs every example through the checks a typed line gets.

Indexes count from 1 on the command line, in results and in `--json`,
as ATEM Software Control counts them; the `atem` package counts from 0,
as the wire does. Only `table.Index` converts, at the boundary between
the CLI and the engine.

## The engine and its socket

The engine is one long-running process, started by the first command
that needs it (`bmdctl engine --background`), or run in the foreground
under systemd or in the container. It listens on
`$XDG_RUNTIME_DIR/bmdctl.sock` (0600, in a directory only its user
owns), or `\\.\pipe\bmdctl-<user>` on Windows, with a lock file so two
engines can't share a path.

One request per connection, one JSON object per line:

```
  CLI                               engine                       switcher
   |  {v:1, device:"atem",           |                              |
   |   host:"192.168.1.20",          |                              |
   |   path:"program",               |                              |
   |   params:{source:"cam2"},       |                              |
   |   build:"<build id>", notes}    |                              |
   | ------------------------------> | session for the host;        |
   |                                 | dial it if there is none     |
   |                                 | parse against its state:     |
   |                                 | cam2 -> source 2             |
   | <---- note {switcher: ...}      |                              |
   | <---- note {on_air: ...}        |                              |
   |                                 | CPgI ----------------------> |
   |                                 | <---------------------- PrgI |
   | <---- note {values: before,     |                              |
   |              after}             |                              |
   | <---- {ok, confirmed, result,   |                              |
   |        took_ms}                 |                              |
```

- **Parsing happens in the engine**, against the session's live state:
  input names, the display rate for rates, the counts of M/Es and
  keyers. The CLI's own check beforehand covers only what needs no
  switcher, which is also all `--dry-run` and `exec`'s pre-check use.
- **Notes** carry what a terminal shows and a response doesn't: the
  switcher's name and model, what was on air, before and after,
  progress, a destructive command's impact. `--json` never prints them,
  so its output stays exactly the response.
- **Files travel in frames.** The engine may run where the caller's files
  aren't (a container), so the CLI sends a file's bytes and writes what
  comes back. The engine writes no file but its log.
- **Builds.** Every request carries the client's Go build ID. An engine a
  command started answers another build with `stale` and stops; the CLI
  starts one of its own build and retries once. An engine started by
  hand answers every build.
- **Codes** map to exit statuses: `usage` 1; `unreachable`, `engine`,
  `stale` 2; `no_echo` 3; `rejected` 4; `destructive` 5.

## Sessions

`atem.Session` is atemwire's `ATEMConnection`, `AtemProtocol` and
`wait_ready` in one type, with no locks:

```
            requests (closures, unbuffered channel)
                             |
                             v
   reader goroutine ---> owner goroutine ---> socket writes
   (ReadFrom only)       transport: ACKs, ordering, resends
                         state, outbox, waiters, transfer lane,
                         subscribers
```

- **One owner goroutine** holds everything and does every write; a
  second only reads datagrams and hands them over. A request is a
  closure the owner runs between two whole datagrams, so nothing races
  and nothing is lost between being accepted and running. `State()`
  returns a deep copy.
- **`Dial`** returns once the state dump is complete and the video mode
  has arrived (15 s at most), so a caller can send at once.
- **`SendAndWait`** registers its waiter and queues the command in one
  owner step, so the echo can't be missed. `Send` queues and returns.
  Commands are packed into packets of up to 1200 bytes, no two packets
  closer than 10 ms, as atemwire paces them.
- **The engine keeps one session per `host:port`.** Concurrent requests
  share one dial. A session closes 10 minutes after its last request,
  or an hour if pinned (`connect`; `BMDCTL_PIN_IDLE`); one whose
  `Connected` went false is replaced on the next request.

`hyperdeck.Session` has the same shape over TCP: one owner, one reader,
answers matched to commands in order, and state kept from the deck's
notifications. `videohub.Session` is the same again, its state the
router's preamble and the blocks it pushes. An Ultimatte has no session:
`ultimatte.Client` opens the connections a call needs and closes them,
as ultimattewire does, so the engine keeps nothing for a keyer.

## State and subscriptions

`State` is a typed tree mirroring atemwire's mixerstate: one Go type per
state packet, grouped by its indexes (`MEs[me].USKs[keyer]`, `DSKs`,
`Aux`, `Inputs` by id, `Fairlight.Strips`, ...), field names and units
as atemwire has them. A nil pointer is a packet that hasn't arrived.

`Subscribe()` returns a channel of `Change` values:

| Kind | When |
|---|---|
| `ChangeState` | a state packet was stored; `Key` is its code and every index (`PrgI:0`, `KeOn:0:1`) |
| `ChangeReady` | a state dump completed |
| `ChangeDisconnected` | the session dropped |

A subscriber's channel holds 1024 changes; a subscriber that falls that
far behind is closed and dropped rather than allowed to stall the owner
(which would stall the ACKs). `watch`, the readers behind `status` and
`show`, the MCP resource subscriptions and `serve`'s event stream are all
built on `Subscribe` and `State`.

## Connected and Ready

Two different questions, mirroring atemwire's `is_connected` and
`is_ready`:

- **`Connected()`** is true from `Dial`'s return until `Close` or a fatal
  error. It stays true while the transport re-handshakes after a
  silence: the session is still trying.
- **`Ready()`** returns a channel that is closed at the end of each
  state dump. A disconnect swaps in a new, open channel until the next
  dump completes. Between the two, `SendAndWait` and transfers fail with
  `ErrNotReady` without sending: an echo then would come from the new
  dump and say nothing about the command.

```
  time -->
           Dial    dump .. InCm  ready      5 s silence  re-SYN  dump .. ready
  Connected  false -------> true ............................................
  Ready()    (open) -------------> closed ------> open (new) ------> closed
                                               ^
                                   reopened first, then waiters and
                                   transfers fail with ErrDisconnected
```

**The disconnect ordering rule** (3ccd09c): on a disconnect the session
reopens `Ready` before it tells anyone `ErrDisconnected`. Then it
empties the state, resets the transfer lane (failing its transfers),
fails the waiting commands and publishes `ChangeDisconnected`. The other
order lets a caller that has just been told `ErrDisconnected` wait on
`Ready`, find the old closed channel, and get `ErrNotReady` from its
next call. atemwire's `disconnected` handler clears `_session_ready`
before `_reset_transfer_lane` runs, and the port now does the same; CI
caught the difference in `TestDisconnectFailsWaitingTransfers`.

## Transfers

Stills and macros move through atemwire's transfer engine, ported branch
for branch, running on the session's owner goroutine:

- per-store queues, one transfer in flight, transfer ids carried across
  reconnects;
- the store lock (`PLCK` for a transfer, `LOCK` for a clear), released
  after every frame and asked for again at once when work is queued;
- uploads paced through the transport's bulk queue (`FTSD`, the
  switcher's `FTCD` budgets, `FTDa` chunks, `FTDC` done); downloads
  acknowledged chunk by chunk; `FTDE` statuses retried, re-locked or
  failed as atemwire does.

Each result goes to the task that asked, not to an event a caller must
filter, so callers can overlap. Cancelling a context withdraws that one
task. A transfer asked for while the session isn't `Ready` is refused
before anything is sent. Progress is a channel holding the latest
`{Done, Total}`, which the CLI turns into the bar.

Still upload is WebATEM's path: fit the image to the video mode's frame,
convert RGBA to Y'CbCrA (premultiplied for a PNG), RLE-encode, send. The
conversion matches atemwire's C extension byte for byte.

## The fakes

Nothing in the default test run touches the network beyond loopback.
The fakes are what the tests talk to instead:

- **`fakeatem.Switcher`** is a `net.PacketConn`. Tests pass it as the
  session's socket and dial `192.0.2.10`, so the whole stack runs for
  real: handshake, ACKs, ordering, retransmission, parsers, state. It
  replays a synthetic state dump (`internal/fixtures/dump`, generated
  from atemwire's own values and checked against it), echoes common
  commands with the state packet a switcher would send, models the
  media and macro pools and their locks, and has controls for the bad
  days: dropped and delayed packets, silence, stale session ids, another
  client holding a lock, failing transfers.
- **`fakehyperdeck`**, **`fakevideohub`** and **`fakeultimatte`** speak
  a deck's, a router's and a keyer's protocols over a pipe or loopback
  TCP. **`fakenet`** puts fakes at addresses, for the device probe and
  the CLI's tests.
- **The CLI's bench** (`cmd/bmdctl/harness_test.go`) runs a real engine
  on a socket in a temporary directory, its dialler handing each session
  a fresh fake. The terminal goldens in
  `cmd/bmdctl/testdata/terminal/` are this bench at a forced terminal.

A test against a fake waits for a condition (the dump, `Ready`, a queued
transfer, a `connecting to` line), never for a sleep to be long enough:
CI's runner is slower than a desk.

## Oracle tests against the Python

The port is checked against bmdwire itself, at the version each oracle
records, not only against tests written from reading it. Scripts beside the oracle files run the
Python on generated inputs and record what it did; the Go tests replay
every case and require the same result:

| Oracle | Records | Go test |
|---|---|---|
| `internal/fixtures/oracle/recv.json` | 1,062 payloads through every state parser | `TestParsersMatchAtemwire` |
| `internal/fixtures/oracle/send.json` | 5,966 calls over all 82 commands | `TestCommandsMatchAtemwire` |
| `internal/fixtures/oracle/readers.json` | 17,025 reader calls over 36 scenarios | `TestReadersMatchAtemwire` |
| `internal/fixtures/oracle/imaging.json` | colour conversion, RLE and the image fit | `TestImagingMatchesAtemwire`, `TestFitFrameMatchesWebATEM` |
| `internal/fixtures/oracle/macro.json` | 2,128 bytecode decodes, 4,273 op encodes | `TestDecodeMatchesAtemwire`, `TestEncodeMatchesAtemwire` |
| `internal/fixtures/oracle/profile.json` | profile saves, applies and parses | `TestSaveMatchesAtemwire` and three more |
| `hyperdeck/testdata/oracle/client.json` | 747 scripted sessions, 872 calls | `TestClientMatchesHyperdeckwire` |
| `videohub/testdata/oracle/client.json` | 213 scripted sessions, 646 steps | `TestClientMatchesVideohubwire` |
| `ultimatte/testdata/oracle/oracle.json` | the display ranges, 3,681 parser cases, 268 scripted calls | `TestRangesMatchUltimattewire` and two more |

The recorded files are committed, so the tests need no Python. Only
regenerating them needs a bmdwire checkout beside this repository, at
the tag each README names; the READMEs in `internal/fixtures/` and the
`testdata/` of `hyperdeck/`, `videohub/` and `ultimatte/` give the
commands. Where the port departs from bmdwire on purpose, the
oracle case says so and PORTING.md, "Deliberate differences", says why.

## Hardware tests

No test, example, generator or script may contact a real switcher
unless built with `-tags hardware`, and CI never sets that tag. A
hardware test reads its target from `BMDCTL_TEST_ATEM` and refuses
anything other than `192.168.81.85` or `192.168.81.86`; it only reads
unless `BMDCTL_TEST_WRITE=1` is set, and even then never changes the
video mode, clears stills, restores a profile or saves startup state.
Every session against real hardware, by test or by hand, is recorded in
[HARDWARE.md](HARDWARE.md). The shipped binary isn't covered: it sends
to whatever host its user names.
