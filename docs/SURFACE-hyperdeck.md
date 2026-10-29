<!--
Written for bmdctl from bmdwire v1.3.0 (tag v1.3.0, commit e5e336c),
package hyperdeckwire/, which has no SURFACE.md of its own, and brought
up to v1.8.0 (tag v1.8.0, commit 01024ad): 1.4.0's fixes (bmdwire#18 to #21,
§5.4), 1.7.0's notes about a deck (bmdwire#53), 1.7.1's empty timeline
(bmdwire#57) and 1.8.0's record, goto, loop off and notifications
(bmdwire#60) and the clear's lag (bmdwire#59). It is the
specification for bmdctl's hyperdeck package, as docs/SURFACE-atem.md is
for atem. Corrections and port decisions go in docs/PORTING.md
("HyperDeck"); this file says what hyperdeckwire does.
-->

# hyperdeckwire: complete library surface

An inventory of everything bmdwire v1.8.0's `hyperdeckwire` can do, built
for mapping to CLI commands. Every entry was read from the code
(`client.py`, `upload.py`, `setup.py`, `__init__.py`); where its README,
`docs/API.md` or a docstring says otherwise, the code wins and §5.3 lists
the disagreement. What the code does with every kind of answer was also
recorded by running it (`hyperdeck/testdata/oracle/client.json`).

- **Source read:** `bmdwire` v1.8.0 (`github.com/lucas-romanenko/bmdwire`),
  package `hyperdeckwire/`, first at v1.3.0 (`e5e336c`). `client.py`'s
  changes since: `5e0c2c9`, `c3f4071`, `ec1e029` and `d11eafd` (1.4.0,
  bmdwire#18 to #21), `54da0e9` (1.7.1, #57), `476850d` (#59) and
  `df00a20` (1.8.0, #60). `upload.py` and `setup.py` are as at 1.2.0
  (`f0a861f`).
- **Three channels, one module each:** `client.py`, the HyperDeck Ethernet
  Protocol on TCP 9993 (control, clips, timeline, configuration);
  `upload.py`, clip upload over the deck's FTP server; `setup.py`, the
  configuration API on the deck's HTTP port that HyperDeck Setup drives.
  bmdctl ports `client.py`; `upload.py` and `setup.py` are inventoried
  here (§3) and not ported yet.
- **Protocol reference:** BMD's `HyperDeckEthernetProtocol.pdf` (December
  2024). hyperdeckwire wraps a subset of it (§5.1 lists what it leaves out).
- **Verified on hardware** (its README and `docs/API.md`, "Validated
  against"): a HyperDeck Studio HD Mini on firmware 8.1.1 through the
  probe, clear, upload and cue-and-loop cycle; two HD Minis on 9.0.2 for
  the configuration API; and an HD Mini on 9.0.2, protocol 1.19, through
  bmdctl: the reads, notifications, `stop`, `play` (loop on and off),
  `goto_clip`, `slot_select`, and the refusals of a relative `goto` past
  the ends (109) and of `record` with no input (110). `goto_timecode`
  has run nowhere.
- **Public surface** (`__init__.py`): `Hyperdeck`, `Clip`, `Response`,
  `HyperdeckError`, `upload_clip`, `UploadResult`, `HyperdeckSetup`,
  `HyperdeckSetupError`, `SetupInfo`, `NetworkInterface`, `RemoteAdmin`,
  `User`.

| § | contents |
|---|---|
| 1 | Commands (writes): the 9993 lines `Hyperdeck` sends, with their arguments |
| 2 | Reads: the read commands, the blocks they return, `Response`, `Clip` and the line parsers |
| 3 | The other channels: FTP upload, the configuration API |
| 4 | Connection lifecycle: connect and the greeting, framing, asynchronous messages, errors, close |
| 5 | Gaps: protocol not wrapped, docs and code, traps |
| — | Counts |

## 1. Commands (writes)

Every write needs the deck's remote control enabled (`remote_info`,
`remote_enable`); without it the deck answers `111 remote control
disabled`. Each method sends one line, `command` or `command: key: value
key: value`, terminated by LF alone, encoded UTF-8, and raises
`HyperdeckError` for a 1xx answer (`_check_ok`). Values are written with
Python's f-strings, so ints are decimal and negative ones keep their sign.
A line holding a CR or LF raises `ValueError` before anything is sent
(1.4.0, bmdwire#20); other values go as given, speeds and ids unchecked.

| method | line sent | notes |
|---|---|---|
| `stop()` | `stop` | stops playback or recording; under the factory `stop mode: lastframe` the deck holds the frame. It also clears loop and single clip, so a `play()` after it plays once (a Studio HD Mini on 9.0.2, bmdwire#53) |
| `play(*, loop=None, single_clip=None, speed=None, clip_id=None)` | `play`, or `play: ` then `clip id: {n}`, `loop: true`/`false`, `single clip: true`/`false`, `speed: {int(speed)}` in that order, space-separated | `loop` and `single_clip` are written as given and left out when `None` (1.8.0, bmdwire#60; before, `False` was left out too, §5.4 item 8); `speed` is a percentage, -5000 to 5000, `int()`-truncated, unchecked |
| `pause()` | `play: speed: 0` | the protocol has no pause; the transport then reports `stopped` |
| `record(name=None)` | `record`, or `record: name: {name}` when `name` is truthy | 1.8.0 (bmdwire#60). With no input a Studio HD Mini on 9.0.2 refuses it, `110 no input`, and goes to preview all the same, which `stop` leaves and a `goto: clip id:` ends |
| `clips_clear()` | `clips clear` | empties the timeline; deletes nothing on disk. The deck answers before the timeline is empty: a `clips get` 0.01 s after the `200 ok` listed the old clips, at 0.16 s none (a Studio HD Mini on 9.0.2; bmdwire#59) |
| `clips_add(name, *, before_clip_id=None)` | `clips add: name: {name}` or `clips add: clip id: {n} name: {name}` | the name goes verbatim: the deck reads everything after `name:` to the end of the line, so spaces and folders (`folder/clip.mp4`) need no escaping |
| `clips_remove(clip_id)` | `clips remove: clip id: {n}` | renumbers the clips after it |
| `goto_clip(clip_id)` | `goto: clip id: {n}` | seeks to the clip's start without playing |
| `goto_clip_by(offset)` | `goto: clip id: {offset:+d}` (`+1`, `-1`) | 1.8.0 (bmdwire#60): `int(offset)`; 0 raises `ValueError('goto_clip_by: an offset of 0 clips moves nowhere')` unsent. Past either end a deck refuses it, `109 out of range` |
| `goto_timecode(timecode)` | `goto: timecode: {timecode}` | 1.8.0 (bmdwire#60): anything but a str that `_TIMECODE` matches whole (`HH:MM:SS:FF` or `HH:MM:SS;FF`, ASCII digits, hours under 24, minutes and seconds under 60) raises `ValueError('goto_timecode: {repr} is not a timecode, HH:MM:SS:FF')` unsent |
| `remote_enable(enable=True)` | `remote: enable: true` / `false` | |
| `slot_select(slot_id)` | `slot select: slot id: {n}` | |
| `set_configuration(**params)` | `configuration: {name}: {value} ...` | one line for all, applied as a batch; each keyword's underscores become spaces, a bool is `true`/`false`, anything else `str(value)`; no keywords sends nothing. A `213 deck rebooting` is a success, and the deck may then drop the connection, which nothing reconnects |
| `set_notify(**kinds)` | `notify: {name}: {value} ...` | 1.8.0 (bmdwire#60): stacked as `set_configuration` stacks (`_stack`); no keywords sends nothing. Kinds: `transport`, `slot`, `remote`, `configuration`, `display_timecode` (every frame) and the rest of BMD's list |
| `ping()` | `ping` | |

`set_configuration`'s docstring lists the common names: `file_format`
(H.264High, QuickTimeProResHQ, DNxHR_HQX, ...), `default_standard`
(1080p25, 2160p50, 720p5994, ...), `video_input` (SDI, 4xSDI, HDMI,
component, composite), `audio_input` (embedded, XLR, RCA), `audio_codec`
(PCM, AAC), `record_prefix`, `record_cache`, `append_timestamp`.

## 2. Reads

| method | line sent | returns |
|---|---|---|
| `device_info()` | `device info` | `params()`: model, protocol version, unique id, slot count, software version (and, by `setup.py`'s `set_name` docstring, the name HyperDeck Setup sets) |
| `remote_info()` | `remote` | `params()`: enabled, override |
| `slot_info(slot_id=None)` | `slot info` / `slot info: slot id: {n}` | `params()`: slot id, status, volume name, recording time, video format, blocked, ... |
| `transport_info()` | `transport info` | `params()`: status, speed, slot id, clip id, single clip, display timecode, timecode, video format, loop, timeline, ... |
| `configuration()` | `configuration` | `params()` |
| `notify()` | `notify` | `params()` of `209 notify`: each kind, `true` or `false` (1.8.0, bmdwire#60) |
| `disk_list(slot_id=None)` | `disk list` / `disk list: slot id: {n}` | `List[Clip]` from `_parse_disk_line` |
| `clips_count()` | `clips count` | `int(params().get('clip count', '0'))`; a value `int()` refuses raises `ValueError` |
| `clips_get()` | `clips get` | `List[Clip]` from `_parse_clips_v1_line` (version 1 rows); `[]` on an empty timeline, where the deck answers `107 timeline empty` (1.7.1, bmdwire#57) |
| `model`, `protocol_version` (properties) | | the greeting's `model` and `protocol version` params, `''` without |
| `read_notifications(timeout=0.0)` | | 1.8.0 (bmdwire#60): the asynchronous (5xx) `Response`s set aside since the last call, oldest first, forgotten once returned; see §4 |

- **`Response(code, text, lines)`**, frozen: `is_ok()` 200-299,
  `is_error()` 100-199, `is_async()` 500-599. `params()` flattens the
  lines: each is stripped (`str.strip()`, Unicode whitespace and
  U+001C-U+001F), skipped if empty or without a colon, split at its first
  colon, key and value stripped again; the last of two equal keys wins.
- **`Clip(clip_id, name, duration, start=None, file_format=None,
  video_format=None)`**, frozen. `disk_list` fills `file_format` and
  `video_format`, `clips_get` fills `start`; `duration` is HH:MM:SS:FF.
- **Which lines are clips** (`_looks_like_clip_line`): the stripped line's
  text before its first colon, unstripped, passes `str.isdigit()`. So
  `slot id: 1` and `clip count: 5` are headers, and ` 5 : x` (a space
  before the colon) is not a clip either.
- **`_parse_disk_line`**: `{id}: {name} {file format} {video format}
  {duration}`. The line is stripped and cut at its first colon; the id is
  `int()` of the stripped head; the rest, stripped, is split on single
  spaces (`split(' ')`, so two spaces make an empty token); fewer than four
  tokens raise `ValueError('malformed disk list line: ...')`; the last
  three are duration, video format and file format, and the others,
  joined by single spaces, the name. Names with spaces (`Intro Loop
  animation.mp4`, from hardware) survive; the formats can't hold one.
- **`_parse_clips_v1_line`**: `{id}: {name} {start} {duration}`, the same
  way with two trailing tokens and three at least (`malformed clips line`).
- **`_parse_clip_lines`** applies a parser to each clip line and skips (and
  logs) a line raising `ValueError` or `IndexError`: one bad row doesn't
  lose the rest. A head `isdigit()` accepts and `int()` doesn't (`²`) is
  skipped there.

## 3. The other channels

### 3.1 FTP upload — `upload.py`

`upload_clip(host, local_path, *, slot=None, progress_callback=None,
login_user='', login_pass='', timeout=120.0) -> UploadResult(name, size,
duration_seconds, slot_dir)` (`throughput_mb_s` property). `ftplib` on port
21; an empty login, retried as `anonymous` when refused. `STOR` at the root
is refused (550), so it lists the root and picks a storage volume
(`_resolve_slot_dir`): the given slot, else a numeric directory (the
Studio HD Mini names volumes `1`, `2`), else one starting `sd`, `ssd`,
`usb`, `nas` in that order; `System Volume Information` and `.Trashes` are
never picked. The clip is visible to `disk list` as soon as `STOR`
completes. Verified on a Studio HD Mini (47.6 MB/s for 50 MB).

### 3.2 The configuration API — `setup.py`

`HyperdeckSetup(host, *, timeout=4.0, opener=None)`: HTTP JSON at
`/admin/api/v1/`, the API HyperDeck Setup is a browser onto. Name
(`setup_basic`, `set_name`), network interface (`network`, `set_network`,
which reads the interface back until it is live on the new values, 20 s
at most), the FTP / Web Media Manager / Ethernet Protocol switches
(`network_access*`, `ethernet_protocol`, `set_ethernet_protocol`; states
`Disabled`, `Enabled`, `SecureOnly`), certificate and signing request,
users, date and time, NTP, time zone, language, identify, capabilities,
heartbeat and `reboot`. Every setter reads the setting back and raises
`HyperdeckSetupError` (`status is None`, message ending `CHECK THIS DECK`)
if the deck holds something else; a 401 means "Configure via USB and
Ethernet" is off, which only USB can turn on, so it has no setter.
Verified on two Studio HD Minis on 9.0.2.

## 4. Connection lifecycle

- **`Hyperdeck(host, port=9993, *, connect_timeout=5.0, read_timeout=5.0,
  socket_factory=None)`**, a context manager. One blocking socket, one
  thread; no locking. `socket_factory` is the tests' hook.
- **connect** closes any previous socket, opens one with `connect_timeout`,
  sets `read_timeout`, empties the notifications set aside, and reads one
  response with `skip_async=False`: the greeting, `500 connection info:`
  (`protocol version`, `model`). A 1xx greeting (`120 connection
  rejected`, too many clients) raises `HyperdeckError`, anything else but
  500 `OSError('unexpected greeting: ...')` with the response's repr
  (1.4.0, bmdwire#21; before, any first block was the greeting). A
  failure while reading it closes the socket and raises.
- **Framing** (`_recv_line`, `_read_response`): bytes up to each LF, every
  trailing CR stripped, decoded UTF-8 with `errors='replace'`. Empty head
  lines are skipped. The head is split at its first space into the code
  (`int()`: so `+200`, `0200`, `2_00` and other scripts' digits are 200)
  and the text; a code `int()` refuses raises `OSError('malformed response
  head: ...')` with the head's `repr`. A head whose text ends with a colon
  starts a block: one colon is taken off and lines are read to the first
  empty one. The block's form comes from the colon alone, not the code.
- **One deadline per response:** `read_timeout` from when reading starts,
  polled in 0.5 s slices; past it, `TimeoutError('no line within
  deadline')`. The stream ending (`recv` returning nothing), even in the
  middle of a line, is `ConnectionError('peer closed connection')`.
- **`request(command, timeout=None)`** first reads, without blocking,
  every complete response already arrived and drops it, logged at warning
  (`_drop_unrequested`, 1.4.0, bmdwire#19); a partial one stays. Then it
  sends the line and returns the next response that isn't asynchronous;
  it doesn't raise for 1xx. A timeout closes the connection before
  `TimeoutError` (1.4.0, bmdwire#18). Asynchronous (5xx) blocks met on the
  way, in the drain or the read, are set aside for `read_notifications`
  (1.8.0, bmdwire#60; before, logged at debug and dropped), at most
  `NOTIFICATION_BACKLOG` (1000), the oldest going first.
- **`read_notifications(timeout=0.0)`** drains as `request` does, then,
  with none set aside and `timeout` left, reads in 0.5 s slices until one
  arrives; a response that isn't one is dropped as unrequested. Waiting
  out `timeout` raises nothing and closes nothing. It returns them all
  and forgets them. Not connected, `OSError('not connected')`.
- **Errors:** `HyperdeckError(code, text)`, `str()` `[code] text`, for a
  1xx answer (codes in §4.1); the socket's own `OSError`,
  `ConnectionError`, `TimeoutError`; `OSError('not connected')` for a call
  after close; `ValueError` from `clips_count`, a line break,
  `goto_clip_by(0)` and `goto_timecode`.
- **close** sends `quit` (ignoring `OSError`), shuts the socket down and
  closes it; a second close does nothing.
- **No state, no events, no reconnection.** Nothing is kept between calls
  but the greeting and the notifications not yet read.

### 4.1 Response codes (BMD's document, as `docs/API.md` lists them)

100 syntax error, 101 unsupported parameter, 102 invalid value, 103
unsupported, 104 disk full, 105 no disk, 106 disk error, 107 timeline
empty, 108 internal error, 109 out of range, 110 no input, 111 remote
control disabled, 112 clip not found, 120 connection rejected, 150 invalid
state, 151 invalid codec, 160 invalid format, 161 invalid token, 162
format not prepared, 163 parameterized single line command not supported.
200 ok; 202 slot info, 204 device info, 205 clips info, 206 disk list, 208
transport info, 209 notify, 210 remote info, 211 configuration, 213 deck
rebooting, 214 clips count (the 2xx blocks hyperdeckwire's tests and
bmdctl's fake deck use). 500 connection info; asynchronous 502 slot info,
508 transport info, 510 remote info, 511 configuration, which carry only
the fields that changed.

## 5. Gaps

### 5.1 In the protocol, not in hyperdeckwire

`docs/API.md` lists them as not exposed: the `playrange` family, `goto`
beyond `goto_clip`, `goto_clip_by` and `goto_timecode` (a clip's or the
timeline's start or end, a relative timecode, a slot), `jog`, `shuttle`,
`record` beyond `record()` (`record: spill`, to a timecode), `format`,
`slate`, `nas`, and multi-line commands (`authenticate:`). Also absent:
`play option`, `play on startup`, `preview`, `uptime`, `identify`,
`watchdog`, `dynamic range`, `slot unblock`, `clips get` version 2 and its
`clip id`/`count` arguments.

Until 1.8.0 `record`, a relative `goto` and `goto` by timecode, and the
`notify` interface were on this list too; bmdwire#60 added them (§1,
§2), from lines bmdctl had sent first.

### 5.2 Implemented, but not exposed

- `request()` is public and doesn't raise for 1xx; `_check_ok` does.
  Until 1.8.0 `_read_response(skip_async=False)` was the only way to read
  a notification; `read_notifications` is it now.

### 5.3 Docs and comments that say something the code doesn't do

- `docs/API.md`, "About pause", says the protocol has no pause and `stop`
  is the pause equivalent; the code has `pause()`, `play: speed: 0`, which
  the README describes and API.md's write-command list leaves out.
- `client.py`'s docstring says 201-299 are "success with parameters
  (multi-line, blank-line terminated)": the framing goes by the head's
  trailing colon, whatever the code (`213 deck rebooting` is a 2xx single
  line).
- `docs/API.md` and the README call 120 "connection failed"; BMD's
  document, and the error table's own row, say rejected.

### 5.4 Behaviour a CLI will trip over (verified in code)

1. **A late answer is read as the next command's.** A response that comes
   after `read_timeout` stays in the stream; the next request reads it as
   its own, and every answer after is one behind. *Fixed in 1.4.0
   (bmdwire#18, `5e0c2c9`): a timeout closes the connection.*
2. **A stray answer does the same:** a response nobody asked for is the
   next request's. *Fixed in 1.4.0 (bmdwire#19, `c3f4071`) for one that
   has arrived before the request is sent, which is dropped; one that
   arrives after can't be told from the answer.*
3. **A malformed head is the current request's error**, and the block it
   heads, if any, is then read as the next request's answer.
4. **A short block swallows what follows:** a block missing its blank line
   takes the next response's head as a body line.
5. **No value is checked:** a newline in a clip name, a record prefix or
   any value sends a second command; speeds and ids outside the deck's
   ranges go as given. *Fixed in 1.4.0 for line breaks (bmdwire#20,
   `ec1e029`), which raise `ValueError` unsent; ranges are still
   unchecked.*
6. **The greeting isn't checked** (§4): a deck at its client limit
   "connects". *Fixed in 1.4.0 (bmdwire#21, `d11eafd`).*
7. **`213 deck rebooting` is success**, and the deck may then drop the
   connection; the next call fails.
8. **`play(loop=False)` sends a plain `play`:** loop and single clip can be
   turned on, never off, through `play`. *Fixed in 1.8.0 (bmdwire#60,
   `df00a20`): a `False` is sent, `None` left out.*
9. **`int()` reads codes and ids,** so `٢٠٠` and `2_00` are 200 and a clip
   row's id may be in any script's digits.

## Counts

- `Hyperdeck`: 28 public methods (`connect`, `close`, `request`,
  `read_notifications`, 15 writes in §1, 9 reads in §2) and 2 properties;
  `Response` (3 predicates and `params`), `Clip` (6 fields),
  `HyperdeckError`; 3 line parsers and their driver; `_stack`.
- `upload.py`: 1 function, 1 result type. `setup.py`: `HyperdeckSetup` with
  34 public methods, 4 result types, 1 error.
- Tests at v1.8.0: 62 in `test_hyperdeckwire_client.py`: v1.3.0's 28 and
  1.8.0's 23 ported one for one in `hyperdeck/client_test.go`, but for
  nine of the ten on `read_notifications`, whose queue a Session hasn't
  (the tenth is checked through Subscribe); the 11 of 1.4.0 to 1.7.1 are
  recorded in the oracle. 10 in `test_hyperdeckwire_upload.py` and 39 in
  `test_hyperdeckwire_setup.py` (not ported with their modules).
