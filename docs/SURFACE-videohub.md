<!--
Written for bmdctl from bmdwire v1.6.0 (tag v1.6.0, commit 4ad0799),
package videohubwire/, which has no SURFACE.md of its own. It is the
specification for bmdctl's videohub package, as docs/SURFACE-atem.md is
for atem. Corrections and port decisions go in docs/PORTING.md
("Videohub"); this file says what videohubwire does.
-->

# videohubwire: complete library surface

An inventory of everything bmdwire v1.6.0's `videohubwire` can do, built
for mapping to CLI commands. Every entry was read from the code
(`client.py`, `__init__.py`); where its README or a docstring says
otherwise, the code wins and §5.3 lists the disagreement. What the code
does with every kind of input was also recorded by running it
(`videohub/testdata/oracle/client.json`).

- **Source read:** `bmdwire` v1.6.0 (`github.com/lucas-romanenko/bmdwire`,
  commit `4ad0799`), package `videohubwire/`. Its code has not changed
  since 1.0.0 (`7b21d0a`); every release since has changed only the
  README's version.
- **One channel, one module:** `client.py`, the Videohub Ethernet Protocol
  on TCP 9990: a text protocol of blocks.
- **Protocol reference:** BMD's "Videohub Ethernet Protocol" document,
  which ships with the Videohub SDK. videohubwire wraps a subset of it
  (§5.1).
- **Verified on hardware** (its README): a broadcast control application
  routing Smart Videohub 40 x 40 and 80 x 80 routers in production; the
  tests' preamble has a Smart Videohub 40 x 40's shape on protocol 2.7.
  Other models were not exercised.
- **Public surface** (`__init__.py`): `Videohub`, `VideohubError`.

| § | contents |
|---|---|
| 1 | Commands (writes): the blocks `Videohub` sends, with their arguments |
| 2 | Reads: the blocks it parses, the state it keeps, `state()` |
| 3 | — (videohubwire has one channel) |
| 4 | Connection lifecycle: connect and the preamble, framing, pushed blocks, errors, close |
| 5 | Gaps: protocol not wrapped, docs and code, traps |
| — | Counts |

## 1. Commands (writes)

Every command is one block: a header line ending in a colon, body lines,
and the blank line that ends it, encoded UTF-8 and sent whole. The hub
answers a block with a bare `ACK` or `NAK` block; after sending, a method
reads blocks until one whose stripped header is `ACK` or `NAK`, applying
every other block it meets on the way (§4), within one `read_timeout` from
the send. Indices are the wire's, 0-based. All errors are `VideohubError`
except where noted.

| method | block sent | on ACK | notes |
|---|---|---|---|
| `route(dest, src)` | `VIDEO OUTPUT ROUTING:\n{dest} {src}\n\n` | `routing[dest] = src` at once, before the hub's own broadcast of the change | `int()` of both first. Refused before sending: either negative (`invalid route {dest} <- {src}`), or beyond the hub's counts when the device block gave them (`route {dest} <- {src} outside this hub ({inputs}x{outputs})`). NAK: `hub refused route {dest} <- {src} (destination locked?)`. No answer: `timed out waiting for ACK routing {dest} <- {src}` |
| `set_input_label(index, label)` | `INPUT LABELS:\n{index} {label}\n\n` | `input_labels[index] = label`, as sent | the label is `' '.join(str(label).split())`: every run of whitespace, Python's (Unicode's, and U+001C-U+001F), is one space and none is left at the ends, so a newline can't break the framing; an empty label is allowed (it clears the name). Refused before sending: more than 64 characters (code points) after that (`label too long (max 64 characters)`); `index` negative, or not below the device block's count (`input labels index {index} outside this hub`). NAK: `hub refused label change on input labels {index}`. No answer: `timed out waiting for ACK renaming input labels {index}` |
| `set_output_label(index, label)` | `OUTPUT LABELS:\n{index} {label}\n\n` | `output_labels[index] = label` | as `set_input_label`, with `output labels` in the messages |
| `ping()` | `PING:\n\n` | — | a liveness check. No answer: `timed out waiting for PING ack`; a NAK is applied as a block and waited past, so it ends in the timeout too |

- The label setters read the count (`video_inputs`, `video_outputs`)
  first, so a non-numeric count in the device block raises `ValueError`
  before the label is looked at; `route()` reads both counts after its
  negative check. `int(index)`, `int(dest)` and `int(src)` raise
  `ValueError` or `TypeError` for what they refuse.
- Nothing takes or releases a lock: videohubwire reports locks (§2) and
  never sends `VIDEO OUTPUT LOCKS`.
- A send after `close()` is `VideohubError('not connected')`.

## 2. Reads

videohubwire has no read commands: the hub sends its whole state on
connect (the preamble, §4) and pushes each change after it. The client
keeps what six block types say; `_apply_block` updates it and returns the
block's stripped header.

| block (stripped header) | updates | how |
|---|---|---|
| `PROTOCOL PREAMBLE:` | `protocol_version` | the `Version` line's value (§2.1, keys) |
| `VIDEOHUB DEVICE:` | `device` | every `key: value` line, merged into the dict |
| `INPUT LABELS:` | `input_labels` | indexed lines, values as text |
| `OUTPUT LABELS:` | `output_labels` | indexed lines, values as text |
| `VIDEO OUTPUT ROUTING:` | `routing` (dest → src) | indexed lines, values `int()`-ed |
| `VIDEO OUTPUT LOCKS:` | `locks` (dest → `U`, `O` or `L`) | indexed lines, values as text |

Every other block (`CONFIGURATION:`, `SERIAL PORT ...`, `MONITORING OUTPUT
...`, `VIDEO INPUT STATUS:`, `END PRELUDE:`, an `ACK` or `NAK` with no
command waiting) is read past and changes nothing.

### 2.1 Lines

- A block's body is its lines after the header that aren't empty once
  stripped (`str.strip()`, Unicode whitespace and U+001C-U+001F).
- **Keys** (`_kv`): each body line is split at its first colon; a line
  without one is skipped. The key is stripped and lower-cased
  (`str.lower()`), the value stripped. A later line with the same key
  wins.
- **Indexed lines** (`_apply_indexed`): the line is stripped, then split
  at its first space into the index and the rest. The index is `int()`
  of the first part: a sign, leading zeros, single underscores between
  digits and other scripts' decimal digits are all taken (`+1`, `01`,
  `1_0`, `١`), and a negative index is stored like any other. The value
  is the rest, stripped: text for labels and locks (spaces kept, so `0
  CAM 1` is `CAM 1`; `3 ` is an empty label), `int()` again for routing.
  A line either `int()` refuses is skipped with a warning (`videohubwire:
  skipping malformed line %r`, logger `videohubwire.client`); the rest of
  the block still applies.

### 2.2 `state()`

Drains pushed blocks (§4) and returns:

```
{'device':  {'model_name', 'friendly_name', 'video_inputs', 'video_outputs',
             'protocol_version'},
 'inputs':  [{'index', 'label'}, ...],
 'outputs': [{'index', 'label', 'source', 'lock'}, ...]}
```

- `model_name` and `friendly_name` are the device block's `model name`
  and `friendly name`, `''` when absent.
- **Counts:** `video_inputs` / `video_outputs` are `int()` of the device
  block's `video inputs` / `video outputs`, 0 when absent or empty. A
  count of 0 falls back to one past the highest label index seen (0 with
  none); a non-numeric count raises `ValueError`.
- `inputs` and `outputs` run from 0 to the count. A label that is absent
  or empty is `Input {i + 1}` / `Output {i + 1}` (1-based for display).
  `source` is the routing's value, `None` where there is none; `lock` the
  lock letter, `U` where there is none.
- The properties `video_inputs` and `video_outputs` are the counts without
  the fallback.

## 4. Connection lifecycle

- **`Videohub(host, port=9990, *, connect_timeout=3.0, read_timeout=3.0,
  socket_factory=None)`**, a context manager. One blocking socket, no
  thread, no locking. `socket_factory` (`socket.create_connection`'s
  signature) is the tests' hook.
- **connect** closes any previous socket and opens one with
  `connect_timeout`; on `OSError` it waits 0.3 s
  (`CONNECT_RETRY_DELAY_S`, for a first packet lost to ARP) and tries
  once more, the second failure raising as it is. Then it reads the
  preamble; any exception there closes the socket and propagates.
- **The preamble** (`_read_preamble`): blocks are read and applied until
  one whose header is `END PRELUDE:` (newer firmware ends the dump with
  it), or, once the essentials have arrived (a non-empty `device` and a
  non-empty `routing`), until the wire has been quiet for 0.3 s
  (`PREAMBLE_QUIET_S`, for older firmware that sends no marker). Before
  the essentials, the wait is what is left of `read_timeout` from the
  start of the read; running out raises `no state preamble from {host} —
  is this a Videohub?`. So does a hub that never sends a routing block
  and no marker.
- **Framing** (`_recv_block`): a block is the bytes up to the first
  `\n\n` in the stream, the `\n\n` dropped, decoded UTF-8 with
  `errors='replace'` and split at `\n`; the first line is the header.
  Reads are `recv(4096)`, appended to a buffer; a complete block already
  buffered is returned before the socket is read again. `recv` returning
  nothing is `VideohubError('connection closed by hub')`; a read that
  times out returns no block.
- **Pushed blocks:** after the preamble the hub sends the same block
  shapes whenever anything changes, from any client (a panel routing,
  another client relabelling). videohubwire reads them only when it reads
  anything: `state()` first drains, reading blocks 50 ms at a time until
  one doesn't come; a command applies every block that isn't its `ACK` or
  `NAK` while it waits.
- **Errors:** `VideohubError` for the protocol's failures (NAK, timeout,
  closed, no preamble, refused arguments); `ValueError` for a count
  `int()` refuses; the socket's own `OSError` from connect.
- **close** shuts the socket down (`SHUT_RDWR`, ignoring `OSError`) and
  closes it; a second close does nothing. Nothing is sent.
- **No reconnection, no events.** State is kept between calls; nothing
  calls back. The intended use is short-lived: connect, read or act,
  close (the preamble of a 120 x 120 hub is a few KB).

## 5. Gaps

### 5.1 In the protocol, not in videohubwire

From BMD's document, not sent or parsed: output locks (`VIDEO OUTPUT
LOCKS:` with `O` to take one, `U` to release it, `F` to force), take mode
and `CONFIGURATION:`, monitoring outputs (`MONITORING OUTPUT LABELS:`,
`VIDEO MONITORING OUTPUT ROUTING:`, `...LOCKS:`), serial ports
(`SERIAL PORT LABELS:`, `SERIAL PORT ROUTING:`, `SERIAL PORT LOCKS:`,
`SERIAL PORT DIRECTIONS:`), processing units, frame buffers, alarms,
`VIDEO INPUT STATUS:` and `VIDEO OUTPUT STATUS:`, and the request form of
any block (a header with an empty body, which asks the hub to send that
block again).

### 5.2 Implemented, but not exposed

The six dicts (`device`, `input_labels`, `output_labels`, `routing`,
`locks`) and `protocol_version` are public attributes, holding more than
`state()` returns: every device key, and labels, routes and locks at
indices outside the counts.

### 5.3 Docs and comments that say something the code doesn't do

None found: the README's protocol notes and the docstrings agree with the
code, the non-numeric count's `ValueError` included.

### 5.4 Behaviour a CLI will trip over (verified in code)

1. **A timeout leaves its answer on the wire.** A command that times out
   raises, but the connection stays open; if the hub answers later, the
   next command reads that `ACK` or `NAK` as its own.
2. **A stray blank line before a block loses the block.** The block is
   then the bytes after the first `\n\n`, which start with `\n`: its
   first line, the header, is empty, and the block is ignored.
3. **CR LF line ends aren't framing.** A hub sending `\r\n\r\n` between
   blocks never ends a block; the preamble times out (`no state
   preamble`).
4. **A non-numeric count raises `ValueError`**, not `VideohubError`, from
   `state()`, `route()` and the label setters (the README says so).
5. **The counts' fallback counts labels, not routes**: a hub whose device
   block gives no counts and whose labels are missing sizes `state()` to
   0, whatever its routing holds.
6. **ACK is optimistic:** `routing[dest] = src` on the ACK; if the hub
   then broadcasts something else (another client routing at once), the
   broadcast, read later, wins.
7. **A NAK to `ping()` is not a failure:** it is applied as a block (and
   ignored) and the wait goes on, to the timeout.
8. **Negative indices are kept** in the dicts (`-1 X` is a label at -1);
   `state()` never shows them.

## Counts

| | |
|---|---|
| Public names | 2 (`Videohub`, `VideohubError`) |
| Commands (blocks sent) | 4: routing, input labels, output labels, ping |
| Blocks parsed | 6 (§2), plus `END PRELUDE:`, `ACK` and `NAK` |
| `VideohubError` messages | 12 |
| Defaults | port 9990, connect and read timeouts 3 s, preamble quiet 0.3 s, connect retry after 0.3 s, labels at most 64 characters |
