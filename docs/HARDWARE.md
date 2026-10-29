# Hardware sessions

Every session of bmdctl against a real switcher, oldest first. Append one
section per session, never rewrite an old one: the date, the commit that
ran, the switcher (model, the protocol version its `_ver` packet reports,
the video mode), what was run, what happened, and what changed because
of it. The rules for hardware tests (which switchers, read-only unless
`BMDCTL_TEST_WRITE=1`, what is never done) are in `CLAUDE.md`.

The two summaries below are kept up to date with the log: after a
session, add what it verified to "Tested on" and take it off
"Unverified".

## Tested on

One ATEM model so far, in nineteen sessions, then one HyperDeck in four,
one Ultimatte in one and one Videohub in one. The ATEM's firmware was
first recorded in the tenth, 9.2.1; before that the rows give the
protocol version the switcher's `_ver` packet reported, 2.30. The
Videohub is one of the owner's two live routers, run by the owner by
hand; no script or test touches them.

| Device | Firmware | Video mode | Verified | Date |
|---|---|---|---|---|
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | The connect (handshake, state dump, video mode), `info`, `status`, `state`, `watch` | 2026-10-10 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | `preview` and `cut`, with `watch` showing them live | 2026-10-10 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | Still upload (a premultiplied PNG, scaled up with bars above and below), download, clear; the switcher returned the frame unchanged | 2026-10-10 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | `macro list`, `macro download` and `decode` (re-encoded byte for byte), `profile save` with its stills, `audio meters` (Fairlight) | 2026-10-10 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | A host renamed to its ATEM Setup name (hosts.yml version 1 to 2), `--host` by prefix, an unknown source refused before sending | 2026-10-10 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | `program`; a write the switcher already holds sends nothing (`already`) | 2026-10-11 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | Terminal output: the header, `connect`, `disconnect`, the `[y/N]` prompt, a profile's `✓` steps, `-t` | 2026-10-11 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | `cut` and `auto` confirmed by the switcher's reports; `undo` of a cut; `undo` refused mid-transition | 2026-10-11 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | The v0.1.1 release binary (darwin_arm64) on macOS, from a second machine over VPN; `bmdctl mcp` from Claude Code: its 15 tools, a state read, `program` confirmed; `watch --filter PrgI -t` | 2026-10-12 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | A `profile save` opened in ATEM Software Control 9.3.2's Restore dialog, every section listed; `profile restore --macros-only` of a name-only macro, which the switcher keeps | 2026-10-13 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | `profile restore` of the Fairlight mixer alone (`--macros-only --macros off --audio`): a dual-mono input's halves each to their own strip, the XLR to ASC's address for it; `profile save` of a dual-mono input as `DualMono` | 2026-10-13 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | `profile save` of a full media pool (20 stills, 8 different): 46 s, each still 1.8 s on the wire, as long as ATEM Software Control's own downloads take (a capture) | 2026-10-14 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | `profile save` with the still cache: the full pool's first save 16.2 s (each of 8 pictures downloaded once), the second 0.9 s (none downloaded) | 2026-10-14 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | `profile save` of a different picture under a name taken (`<name>_2.png`, names compared ignoring case) and of an unnamed still (`Slot<n>.png`), the XML's paths to match; `media upload` of a photographic 1080p still with aggressive drain in 1.8 s, 23.3 s without | 2026-10-14 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | `profile restore` of a whole media pool (20 photographic stills, `--macros-only --macros false --media-pool-images true`) in 73 s: every slot's name and frame as the profile's files, the renamed (`<name>_2.png`) and unnamed (`Slot<n>.png`) ones among them | 2026-10-14 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | Uploads as ATEM Software Control sends them (bmdwire#36): `profile restore` of 20 photographic stills in 11.2 s (ASC 12 s), every frame exact | 2026-10-15 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | Another client's turn (bmdwire#38, #40): ATEM Software Control connecting during back-to-back downloads loads all 20 thumbnails (none with bmdwire#36's handover) | 2026-10-15 |
| ATEM 1 M/E Constellation HD | 9.2.1 | 1080p25 | Uploads beside another session in the same process, as WebATEM runs them (bmdwire#42): 20 of 20, every frame exact; bmdwire 1.5.2 deadlocked after 2 | 2026-10-15 |
| ATEM 1 M/E Constellation HD | protocol 2.30 | 1080p25 | bmdctl after bmdwire 1.5.1 to 1.8.0's ports: the connect, `status`, `state`, every `show` area, `media list`, `macro list`, `audio meters` (Fairlight), `watch`; `preview`, `cut`, `auto`, `undo` of an auto once its transition is done; a still uploaded byte for byte (the switcher's hash the frame's MD5) and downloaded; `profile save` from the still cache, the one changed picture downloaded; the terminal views | 2026-10-29 |
| HyperDeck Studio HD Mini | 9.0.2, protocol 1.19 | 1080p25 | `connect`, `status`, `clips` (timeline and disk), `state`, `watch`; `stop`, `play` (speed, loop, single clip), `goto` by clip id, `slot`; `next`, `prev` and `record` refused as the deck refuses them | 2026-10-27 |
| HyperDeck Studio HD Mini | 9.0.2, protocol 1.19 | 1080p25 | `pause`; `configuration show` and `configuration set` (changed and back); `remote off` and `remote on`, which the deck takes over 9993 with remote off; `timeline add` (`--before`), `remove` and `clear` on a timeline with clips | 2026-10-27 |
| HyperDeck Studio HD Mini | 9.0.2, protocol 1.19 | 1080p25 | `loop off` and `loop on` during a playback (the clip played out, then looped past its end); `timeline clear` empty when read at once | 2026-10-28 |
| Ultimatte 12 4K | 2.3 | 1080p25 | `connect`, `status`, `prelude`, `controls`, `presets`, `network show`, `archive`; `restore` (presets and settings back byte for byte, the saved settings applied live) | 2026-10-27 |
| Videohub 40x40 12G | protocol 2.8 | | `connect ADDRESS` (the probe found a Videohub), `status`, `route` and a route back, from the v0.2.0 release | 2026-10-29 |

## Unverified

Not yet run against real hardware. Most of it is tested against the
fakes and, for the protocol, against bmdwire's own code.

- **Other devices and modes:** any ATEM but the Constellation HD, and
  any video mode but 1080p25.
- **HyperDeck:** `record` (the deck had no input), `goto` by timecode or
  name, a second disk, a setting whose change reboots the deck, and any
  deck but the Studio HD Mini. The rest is checked against hyperdeckwire,
  not a deck.
- **Videohub:** `inputs`, `outputs`, `watch`, `input rename`, `output
  rename`, a route to a locked output, and any router but the Videohub
  40x40 12G. The rest is checked against videohubwire (which routes
  Smart Videohub 40 x 40 and 80 x 80 routers in production), not a
  router.
- **Ultimatte:** `network set` (not run: a wrong value takes the unit off
  the network), a restore of another unit's archive or of Smart Remote
  4's Archive All, and any keyer but the 12 4K.
- **Hosts:** `hosts discover`; `hosts add --as`; a rename made in ATEM
  Setup while bmdctl is in use; the device probe (`connect ADDRESS`
  asking what answers) on anything but a Videohub.
- **Sessions:** a session that drops and reconnects (`watch`'s `session
  dropped` and `reconnected`, the connecting line on a reconnect).
- **undo:** undo after a change from a panel, which sets each bus back
  instead of cutting.
- **Writes:** every command that changes the switcher other than
  `program`, `preview`, `cut`, `auto`, `undo` and the still transfers:
  keyers (among them `usk keyframe-run runToInfinite --direction` by
  ATEM Software Control's location names, bmdwire#22), DSKs, aux,
  transition settings, audio (the Fairlight commands have gone out only
  inside the eleventh session's profile restore; no `audio` command has
  run), colour, multiviewer, macro run and record. Commands the table
  marks unverified say so when sent.
- **Transfers and profiles:** macro upload of a macro with ops (the
  tenth session uploaded only an empty one), and any macro upload since
  bmdwire#36 (FTFD with the first budget, the handover in one packet);
  an upload over a slower link (a VPN), where the whole-budget bursts and
  the resend timer would matter; ATEM Software Control connecting while
  bmdctl itself transfers (the seventeenth run verified another client's
  turn with bmdwire's code, bmdwire#38 and #40); `profile restore` beyond `--macros-only`, the
  Fairlight mixer and the media pool's stills; macro floats in a `profile
  save` since bmdwire#24 (six significant digits); a still that isn't premultiplied (JPEG, GIF,
  `--premultiply off`); a fit that shrinks the image or adds bars at the
  sides; the progress bar on a slow transfer.
- **Classic audio:** every `classic-audio` command, `classic-audio
  meters`, and the dB scale `audio master` and `audio levels` put the
  classic mixer's volume on. bmdwire v1.6.0's legacy mixer (bmdwire#49)
  hasn't met a legacy switcher either: its readers behind `show audio`
  (names, pan, the monitor as headphones), an `AMIP` from before ATEM 8.0
  firmware (20 bytes), and the `legacy-audio:` lines in the engine's log,
  which are there for a session with one to check the layouts against.
- **Platforms:** the Windows engine and its named pipe (the executables
  ran only under Wine); the macOS amd64 binary; zsh and fish completion
  in a real shell; the Docker image against a switcher.
- **Integrations:** `bmdctl serve` against a switcher; `bmdctl mcp`
  from Claude Desktop; MCP's state subscription
  (`bmdctl://hosts/{host}/state`) and `wait_for_change`.

## 2026-10-10: first run

- **Commit:** `21219e3`, the head of main when the run was reported (the
  report didn't name the commit).
- **Switcher:** ATEM 1 M/E Constellation HD, protocol 2.30, 1080p25.
- **Run:** reads only, through the engine: the connect (`Dial`: the
  handshake, the whole state dump and the video mode; it sends `TiRq`
  itself), `info`, `status`, `state` and `watch`. No other command was
  sent to the switcher.
- **Result:** all of them worked.
- **Changed because of it** (`9f3e85e`):
  - `watch` was swamped by the clock's `Time` packets: they are left out
    unless `--all`. Its human form named changes by opcode and printed
    raw JSON: it now uses atemwire's state names and gives program and
    preview, tally and running transitions a line each, with source
    names.
  - `state --path` to a leaf printed `value  1`: it now prints the bare
    value, for scripts.
- **Not yet verified on hardware:** every command that changes the
  switcher, transfers (stills, macros), profiles, and the meter stream.

## 2026-10-10: second run, writes

- **Commit:** `9144f87`.
- **Switcher:** the report didn't restate it; the first run's was an
  ATEM 1 M/E Constellation HD, protocol 2.30, 1080p25.
- **Run:** the first commands that change the switcher, through the
  engine: `preview` and `cut`, with `watch` running beside them. A still
  upload (`media upload`) was not attempted.
- **Result:** `preview` and `cut` worked (`preview` is confirmed by its
  echo; `cut` has none). `watch`'s human form was verified live.
- **Changed because of it** (the commit that added this section):
  - A background engine kept serving after the bmdctl binary was
    replaced. Every request now carries the client's build (the Go
    build ID); a background engine of another build answers `stale` and
    stops, and the command starts an engine of its own build and
    retries once.
  - `watch` printed `tally-index` and `fairlight-tally` changes that the
    tally line already covers: they are left out unless `--all`.
  - `inputs` lists each source (id, short name, long name, port type);
    it was `input list`, which still works. A source name no input has
    points at it.
- **Not yet verified on hardware:** every other command that changes
  the switcher, transfers (stills, macros), profiles, and the meter
  stream.

## 2026-10-10: third run, stills

- **Commit:** `7cbcb44`.
- **Switcher:** the same ATEM 1 M/E Constellation HD as the first run
  (protocol 2.30, 1080p25; the report didn't restate them, and the
  frames were 1920×1080).
- **Run:** the still transfers, through the engine, on slot 10 (as
  typed, counting from 1): `media upload 10` of a 1280×640 RGBA PNG
  (three flat colours, so PNG premultiply applied), `media download 10`,
  then `media clear 10 --yes`, with `media list` after it.
- **Result:** all three worked.
  - The upload was confirmed in 1085 ms. The fit scaled the picture
    1.5× to 1920×960 with 60-row bars above and below. The switcher
    reported the slot as "test" (the file's name), with an MD5 hash of
    `ccd26a6e6c0b11f5f68d25758953607a`, the same hash bmdctl computes
    over the frame it sent.
  - The download was confirmed in 573 ms, a 1920×1080 RGBA PNG. Red
    (214,39,24) came back as (214,39,23), green (47,208,122) as
    (46,208,121), and the transparent bar's corner pixel with alpha 12.
    Afterwards, the downloaded PNG (`back.png`) was compared with
    `FitFrame`, `RGBAToYCbCrA` (premultiplied) and `YCbCrAToRGBA` run
    locally on the source (`test.png`). All 8,294,400 bytes are equal,
    so the switcher stored and returned the frame unchanged. The colour
    shifts and the alpha of 12 come from the conversion, which matches
    atemwire's C byte for byte, not from the switcher.
  - The clear was confirmed in 14 ms (exit 0: `media clear confirmed
    14 ms slot 10, was "test"`), and `media list` showed slot 10 empty
    afterwards.
- **Changed because of it** (the commit that added this section): no
  code. `docs/PORTING.md` now records still upload, download and clear,
  the image fit, and the Y'CbCr and RLE conversion as verified on
  hardware.
- **Not yet verified on hardware:** a still that isn't premultiplied
  (JPEG, GIF or `--premultiply=false`), a fit that shrinks the image
  or adds bars at the sides, macro transfers, profiles, the meter
  stream, and every other command that changes the switcher.

## 2026-10-10: fourth run, macros, profile save, meters

- **Commit:** `45d4d40`.
- **Switcher:** the same Constellation as the first run: the saved
  profile names the ATEM 1 M/E Constellation HD at 1080p25. The report
  didn't restate the protocol version.
- **Run:** reads only, through the engine: `macro list`, `macro download
  1` (as typed: "TWO SHOT", index 0), `profile save` with its media pool
  images, and `audio meters`. `macro decode` ran locally on the
  downloaded bytecode.
- **Result:** all of them worked.
  - `macro list` showed 6 used slots.
  - `macro download 1` was confirmed in 109 ms: 712 bytes of bytecode,
    which `macro decode` turned into 52 ops. Afterwards, encoding those
    ops again gave the same 712 bytes.
  - `profile save` wrote 69,802 bytes of XML holding the six macros,
    and `ATEM Media Pool/slot1_Backdrop_A.png` (1920×1080)
    for the one used still.
  - `audio meters` streamed levels for 14 strips and the master.
- **Codec findings** in the decoded macro, both shared with bmdwire
  v1.3.0:
  - All 24 Fairlight strip ops carry sourceId bytes `00 01 ff ff ff ff
    ff ff`: 0xFFFFFFFFFFFF0100, i.e. -65280 (stereo), decoded unsigned
    as `18446744073709486336`. The report read that number as
    0xFFFFFFFFFFFF0000 (-65536), but it is 0xFFFFFFFFFFFF0100. The
    unsigned decode is deliberate in bmdwire (the form it says ATEM
    Software Control writes) and is kept. The bug is in the encoder: an
    op with no sourceId gets 0xFFFFFFFFFFFF0000, a value no op in the
    macro or in bmdwire's live captures carries.
  - The faders of 12 strips (Camera2 to Camera10, both media players
    and ExternalTRS) are silenced: bytes `ec 51 b8 fe`, Q16.16
    -21474836, -327.68 dB. bmdwire, and so bmdctl, decoded that as
    `-327.679993`, where bmdwire's own docs say ATEM Software Control
    writes `-327.68`. The profile saved on this run has `-327.679993`
    in its macros.
- **Changed because of it** (`377fff6`): a Fairlight strip op with no
  `sourceId` now encodes the stereo one, and a silenced Fairlight gain
  decodes as `-327.68`. Both are deliberate differences in
  `docs/PORTING.md`, to be filed against bmdwire. In the commit that
  added this section, `docs/PORTING.md` records macro download, profile
  save and the Fairlight meters as verified on hardware.
- **Not yet verified on hardware:** macro upload, profile restore,
  `classic-audio meters`, a still that isn't premultiplied, a fit that
  shrinks the image or adds bars at the sides, and every other command
  that changes the switcher.

## 2026-10-10: fifth run, the UX pass

- **Commit:** `7218b1e`. The engine's log gives its version as
  `v0.0.0-20261011010118-7218b1e4e386+dirty`: Go marks a build dirty for
  untracked files too, and the checkout had untracked scratch files;
  `git status` after the commit showed no changed tracked file.
- **Switcher:** the Constellation at 192.168.81.85, named "Studio ATEM"
  in ATEM Setup: the engine's log reports an ATEM 1 M/E Constellation HD
  at 1080p25. The report didn't restate the protocol version (2.30 on the
  first run).
- **Run:** the UX pass, end to end, in one second by the engine's log.
  The engine of the previous build was stopped and one of this build
  started. Then `preview`, to the host a version 1 hosts.yml named
  `bench` (198 ms, the connect included); `bmdctl` alone; `preview`
  again (21 ms); `status`; and `program cam1`, which the engine refused
  before sending anything: the switcher has no input of that name. The
  report says `--host` was given a prefix of the new name; the log
  doesn't say for which command.
- **Result:** all of it worked as designed.
  - The first command printed the connecting line, then the model and
    video mode.
  - The switcher's own name replaced the hand-chosen one, with the
    notice: hosts.yml is now version 2, `name: Studio ATEM`, `was:
    bench`, and the last host `Studio ATEM`.
  - The `preview` lines named the switcher and gave before → after by
    the inputs' short names, and `--host` reached the switcher by the
    prefix.
  - `bmdctl` showed the engine and the host; `status` showed how long
    the session had been idle and when it drops.
  - `program cam1` ended with its next step: `(bmdctl inputs lists
    them)`.
- **Changed because of it** (`12be2b6`): a write's before → after column
  is as wide as a change between the switcher's two longest source names,
  not a fixed 24; `bmdctl` and `status` show the git commit the engine
  was built from, the build ID kept for the stale-engine check; `bmdctl`
  prints `bmdctl help lists the commands` only when no hosts are
  configured. In the commit that added this section, `docs/PORTING.md`
  records the output conventions and the switcher naming its host as
  verified on hardware.
- **Not yet verified on hardware:** `hosts discover`; `hosts add` and
  `--as` (the run renamed a host, it added none); a session that drops
  and comes back (watch's `dropped` and `reconnected`, the connecting
  line on a reconnect); a rename made in ATEM Setup while bmdctl is in
  use; colour on a terminal; macro upload, profile restore,
  `classic-audio meters`, a still that isn't premultiplied, a fit that
  shrinks the image or adds bars at the sides, and every other command
  that changes the switcher.

## 2026-10-11: seventh run, the output rework

- **Commit:** `817ee63`. The engine's log gives its version as
  `v0.0.0-20261011052018-817ee630519b+dirty`: the checkout had untracked
  files (the brand handoff, a screenshot), no changed tracked file. The
  sixth run (2026-10-10 21:45, `70a96b8` by the engine's log) has no
  section here.
- **Switcher:** the Constellation at 192.168.81.85, "Studio ATEM": the
  engine's log reports an ATEM 1 M/E Constellation HD at 1080p25. The
  report didn't restate the protocol version (2.30 on the first run).
- **Run:** the terminal pass's output, at a terminal, in 18 seconds by
  the engine's log. The engine of the previous build was stopped and
  one of this build started. Then `connect` (75 ms), `preview` twice,
  `program`, `cut`, `undo`, `program wde` (refused before sending: no
  input of that name), `status`, `inputs`, `show`, `media upload` to
  still slot 10 (514 ms), `media clear` of it without `--yes`, refused
  and asked at the prompt, then sent once answered (34 ms), `profile
  save` (2.8 s: six macros and one still), a `cut` and `disconnect`.
  The report says `-t` was used; the log doesn't say for which command.
- **Result:** the output rework worked as designed: the connect line,
  `already`, the `[y/N]` prompt, the profile's `✓` step lines, `-t`'s
  time of day, and `disconnect`. The 514 ms upload left no bar in the
  scrollback, as designed, since the bar is erased once the result is
  printed; whether one was drawn couldn't be seen afterwards. Four
  things were wrong:
  - `undo` straight after the `cut` (10 ms later by the log) said
    `nothing to undo`: a cut pushed nothing on the undo stack.
  - The `cut`'s header showed program and preview after the cut, not
    what was live when it ran.
  - The status column ended at column 64 on results and wherever the
    row ended in tables, so `status` and `show keys` put one keyer's
    on-air state in different columns.
  - Nothing tested that a bar is drawn for a transfer over 200 ms.
- **Changed because of it** (`2c02b28`): a cut or an auto keeps both
  buses for undo, and undo cuts again (or, where the buses changed
  since, sets each back); the header shows program and preview as the
  command found them (one `on_air` note, before the command); every
  status ends at column 72, results and tables alike, and `status` and
  `show keys` share a keyer's columns; `TestUploadBar` checks, at a
  forced terminal with the real 200 ms, that an upload draws its bar at
  least once and leaves none. Before that (`c611203`, to make CI's
  goldens deterministic), a cut waits for the switcher's `PrgI` and
  `PrvI` before it answers, and an auto for its first `TrPs` or `PrgI`:
  an undo 10 ms after a cut, as in this run, could otherwise read the
  buses from before it. A cut's line now shows its time, not `sent`.
- **Not yet verified on hardware:** undo of a cut or an auto; that a
  cut's `PrgI` and `PrvI` arrive within the timeout on a real switcher
  (if they don't, the cut answers `sent` after `--timeout`); the
  header and column-72 layout; a bar drawn for a slow transfer;
  `hosts discover`; `hosts add` and `--as`; a session that drops and
  comes back; a rename made in ATEM Setup while bmdctl is in use; macro
  upload, profile restore, `classic-audio meters`, a still that isn't
  premultiplied, a fit that shrinks the image or adds bars at the
  sides, and every other command that changes the switcher.

## 2026-10-10: sixth run, the readability pass (recorded late)

- **Recorded on 2026-10-11, after the seventh run's section**, so it is
  out of order: no report was written at the time. It is from the
  engine's log alone, which says what was sent and how long it took,
  not what the terminal showed.
- **Commit:** `70a96b8`. The engine's log gives its version as
  `v0.0.0-20261011014228-70a96b8c5504`, not marked dirty.
- **Switcher:** the Constellation at 192.168.81.85, "Studio ATEM": the
  engine's log reports an ATEM 1 M/E Constellation HD at 1080p25. The
  log doesn't give the protocol version (2.30 on the first run).
- **Run:** in six seconds by the engine's log, from 21:44:57. The engine
  of the previous build (`7218b1e`) was stopped and one of this build
  started. Then `preview`, whose request opened the session (connected
  in about 320 ms); `program` (32 ms); `status`; `bmdctl engine
  status`; `inputs`; `state`; `program cam1`, refused before sending (no
  input of that name, as in the fifth run); and `media clear` of still
  slot 10 (19 ms; a destructive command, so `--yes` was given: the log
  shows no refusal). The session closed after ten idle minutes.
- **Result:** the `preview` stalled: 5,319 ms, then `no_echo`, `sent,
  but the switcher did not report PrvI:0 within 5s` (exit 3). The
  switcher wasn't the problem: it answered the `program` 42 ms later,
  in 32 ms. By the owner's diagnosis it was a no-op: the preview asked
  for the source already on preview, a switcher doesn't report a value
  that didn't change, and the engine waited its whole timeout for an
  echo that never came. Everything else answered at once.
- **Changed because of it** (`d1ca2cf`): a write the switcher already
  holds sends nothing. The engine reads the value through the
  command's reader first and answers confirmed with `"already": true`,
  and a terminal says `already` where the time goes. The seventh run
  verified `already` on hardware.
- **Not yet verified on hardware:** as after the fifth run.

## 2026-10-11: eighth run, cut and auto confirmation, undo

- **Commit:** `72419ea`. The engine's log gives its version as
  `v0.0.0-20261011153407-72419eaf4a05+dirty`: the checkout's untracked
  files, as in the seventh run.
- **Switcher:** the Constellation at 192.168.81.85, "Studio ATEM": the
  engine's log reports an ATEM 1 M/E Constellation HD at 1080p25. The
  report didn't restate the protocol version (2.30 on the first run).
- **Run:** in under a second by the engine's log, at 11:45:51. The
  engine of the previous build was stopped and one of this build
  started. Then `connect` (528 ms), `preview` (27 ms), `cut` (30 ms),
  `undo` (31 ms), `auto` (29 ms), `undo` again 8 ms after the auto
  answered, and `disconnect`.
- **Result:** all of it worked as designed, and no command waited out
  its timeout.
  - The cut answered confirmed in 30 ms: the switcher reported `PrgI`
    and `PrvI` well inside the 5 s the engine waits. That was the risk
    the seventh run's section left open.
  - The undo put the cut back in 31 ms. The buses were as the cut left
    them, so by the undo's rule it cut again; the log doesn't name the
    command sent.
  - The auto answered confirmed in 29 ms, when the transition started
    (`TrPs`), not when it ended. The undo sent 8 ms later found M/E 1
    mid-transition and was refused with `M/E 1 is mid-transition: undo
    once it is done`.
- **Changed because of it:** nothing in the code. In the commit that
  added this section, `docs/PORTING.md` records the confirmation of a
  cut and an auto, the undo of a cut, and undo's refusal mid-transition
  as verified on hardware.
- **Not yet verified on hardware:** undo of an auto once its transition
  is done (the refused undo's entry was kept, but the run disconnected
  before trying again); undo's other path, which sets each bus back
  after a change from a panel; the header and column-72 layout, and a
  bar drawn for a slow transfer (not in the report); `hosts discover`;
  `hosts add` and `--as`; a session that drops and comes back; a rename
  made in ATEM Setup while bmdctl is in use; macro upload, profile
  restore, `classic-audio meters`, a still that isn't premultiplied, a
  fit that shrinks the image or adds bars at the sides, and every other
  command that changes the switcher.

## 2026-10-12: ninth run, the macOS release and MCP

- **Commit:** `98cf772`, tagged `v0.1.1`: the release's darwin_arm64
  binary, not a build from a checkout.
- **Switcher:** "Studio ATEM", the Constellation of the earlier runs,
  from a second machine: a Mac reaching it over a VPN. The report didn't
  restate the model, protocol version or video mode (an ATEM 1 M/E
  Constellation HD, protocol 2.30, 1080p25 on earlier runs).
- **Run:** at a terminal, `version`, `connect`, `status` and `inputs`.
  Then `bmdctl mcp` from Claude Code, added with `claude mcp add bmdctl
  -- bmdctl mcp`: a query of the state, and a program change through
  MCP. Alongside, `watch --filter PrgI -t` ran from a shell.
- **Result:** all of it worked.
  - The release binary ran on macOS, and the four commands answered
    over the VPN.
  - Claude Code listed 15 tools (the twelve ATEM groups, the two
    HyperDeck ones and `list_hosts`). Its query of the state matched
    what the terminal showed.
  - The program change through MCP, MP1 → Cam7, was confirmed.
  - `watch --filter PrgI -t` logged 14 program changes, each with the
    right time.
  - To confirm "Cam7", the agent dumped the whole state with `--json`
    and parsed it with Python, where `inputs` answers that.
  - No MCP tool waits for a change, so the agent watched from a shell
    (by issue #2, a background shell it lost control of).
- **Changed because of it:** nothing in the code. In the commit that
  added this section, `skills/bmdctl/SKILL.md` says to look an input up
  by name or number with `inputs` (`atem_sources` inputs), never by
  dumping `state`. Issue #2 asks for an MCP tool that waits for the next
  change.
- **Not yet verified on hardware:** the macOS amd64 binary; `bmdctl mcp`
  from Claude Desktop; MCP's state subscription; `bmdctl serve` against
  a switcher; otherwise as after the eighth run.

## 2026-10-13: tenth run, profiles and ATEM Software Control

- **Commit:** the report didn't name the build or the machine.
- **Switcher:** an ATEM 1 M/E Constellation HD on firmware 9.2.1, the
  first session to record the firmware. The report didn't name the unit
  (the earlier runs' was "Studio ATEM") or restate the protocol version
  or video mode (2.30 and 1080p25 on earlier runs).
- **Run:** two checks against ATEM Software Control 9.3.2.
  - `profile save` to `4-bench.xml` on the Desktop, then that file
    opened in ASC's Restore dialog.
  - `profile restore --macros-only` of a profile holding `<Macro
    index="22" name="empty-test"/>`, a macro with a name and no ops,
    then `macro list`, and ASC's Macros window.
- **Result:** both worked.
  - ASC listed every section of the save in its Restore dialog and gave
    no warning. The items it greyed are hardware the unit lacks, not
    something missing from the file.
  - Slot 23 (index 22, counting from 0) then held a macro named
    empty-test with no ops, in `bmdctl macro list` and in ASC's Macros
    window. Apply uploads a macro without ops as empty bytecode, as
    atemwire does, and the switcher keeps it as a macro: restore must
    not skip name-only macros.
  - The `profile save` result line cut the long path from the right,
    and the size column after it was lost.
- **Changed because of it:** nothing in the code. In the commit that
  added this section, `docs/PORTING.md` records the save opening in ASC
  and the restore of a name-only macro as verified on hardware, and
  that the fake switcher, which empties the slot on an empty macro
  upload, differs from a switcher there. Issue #6 asks for a long path
  to be shortened from the left (`…/Desktop/4-bench.xml`), keeping the
  size.
- **Not yet verified on hardware:** `profile restore` beyond
  `--macros-only`; macro upload of a macro with ops; otherwise as after
  the ninth run.

## 2026-10-13: eleventh run, a dual-mono profile restore

- **Commit:** `dc06dbf`, the head of main.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD on firmware
  9.2.1, 1080p25.
- **Run:** with input 1 in Dual Mono in ATEM Software Control:
  - `profile save before.xml`, a backup.
  - `profile restore
    internal/fixtures/profile/profile_fairlight_dual_mono_asc.xml
    --macros-only --macros off --audio`: bmdwire#23's fixture, ASC
    9.3.2's own export of a Constellation HD with input 1 in Dual Mono
    (left -10 dB, pan -50, first EQ band a low shelf at 35 Hz, +3 dB;
    right -20 dB, pan +50, a low shelf at 46 Hz, -3 dB; the XLR and
    input 2 at -inf; master -0.08 dB), with every section but the
    Fairlight mixer off.
  - ASC's Fairlight mixer checked, then `profile save after.xml`.
- **Result:** it worked.
  - The restore reported 2 applied and 7 skipped, the counts the fake
    switcher gives for the same command (`TestProfileRestoreAudioOnly`):
    the master dynamics and the 15 strips applied; video mode, inputs,
    multiview, macros, camera control, talkback and program/preview
    gated off.
  - In ASC, input 1's left half read -10 dB, pan -50, its EQ shelf up,
    and the right -20 dB, pan +50, its shelf down: each half got its
    own values. The XLR (Mic), which apply now sends to channel 0
    (ASC's -256) instead of -1, went from -10 dB to -inf. Input 2 read
    -inf, the master -0.08 dB.
  - `after.xml` had `configuration="DualMono"` once, for input 1, with
    ids -256 and -255 for its halves and -256 for the XLR: save writes
    a dual-mono input as ASC does.
  - `before.xml` was overwritten by an accidental second run of the
    save, after the restore, so the mixer was reset by hand.
- **Changed because of it:** nothing in the code. In the commit that
  added this section, `docs/PORTING.md` records the dual-mono restore
  and save as verified on hardware, and the Unverified list above drops
  them.
- **Not yet verified on hardware:** `profile restore` beyond
  `--macros-only` and the Fairlight mixer; `usk keyframe-run
  --direction` by location name; macro floats in a `profile save` since
  bmdwire#24; otherwise as after the tenth run.

## 2026-10-14: twelfth run, a full media pool, and ASC's save captured

- **Commit:** `10e1944`, the installed binary (`v0.1.3-0.20261014141245-10e19441589c`);
  the profile and transfer code is the same at `683d5a5`, the head of
  main.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD (model 18),
  protocol 2.30, firmware 9.2.1 as recorded in the tenth run, 1080p25.
- **Why:** the owner's ATEM Software Control saves a profile with a
  full media pool in about a second; bmdctl and WebATEM take tens of
  seconds.
- **Run:** the owner filled all 20 still slots with 8 different 1080p
  images (one in six slots, one in four, one in three, two in two,
  three once). From the development server, through the engine:
  - `media list --all`: 20 used slots, 8 different MD5 hashes.
  - `profile save t2.xml`, timed.
  - `watch --json --all --filter LKST` beside `media download 1`, to see
    a lock taken and released in the watch.

  Then the owner quit ATEM Software Control, opened it, let it connect,
  saved a profile, and captured its traffic with Wireshark from the
  connect to the save (62 s, on the ASC machine, 192.168.81.54).
- **Result:**
  - bmdctl's save took 46.2 s: the six macros 0.5 s, then each still
    1.80 to 1.83 s from FTSU to the lock's release, and 0.38 to 0.59 s
    between stills (RLE decode, the PNG, the lock's release and the
    next request). It downloaded all 20 stills and wrote 8 PNGs (27 MB):
    the repeats share names, and each overwrote the last.
  - The watch showed the download's lock taken and released 1.8 s apart.
  - ASC saved the XML and 8 images in about a second, by the owner's
    stopwatch. The capture shows why. From 0.75 s after its hello to
    37.3 s, ASC downloaded every used still in full, one at a time: 20
    transfers of 8.32 MB, about 5,940 chunks of about 1,400 bytes each,
    1.78 to 1.82 s apiece, every chunk acknowledged with FTUA, up to 153
    chunks in flight, 17 to 39 ms (median 24) between stills. It
    downloaded the repeats too. At the save, 61.3 s in, it downloaded
    only the six used macro slots (three of them name-only, with no
    ops), in 0.5 s, and no still: the images it saved are the ones it downloaded on
    connecting.
  - On the wire ASC differs from bmdctl in ways that cost nothing here:
    a whole-store LOCK taken and released around each still, where
    atemwire asks for a PLCK; FTSU's last four bytes `00 ef 67 70` for a
    still (atemwire sends `00 d0 9b 8c`); FTUA's second field nonzero,
    where atemwire sends 0.
- **Changed because of it:** nothing in the code yet. A still takes as
  long on the wire for bmdctl as for ASC, about 1.8 s on this switcher.
  ASC's save is quick because it downloads the stills when it connects
  and saves those; bmdctl downloads them at the save, every slot,
  repeats included (atemwire's `download_media_pool_images` takes a
  cache by hash, `cache_get` and `cache_put`, which the port left out),
  and spends about 0.47 s of its own between stills. The capture's
  protocol details go to bmdwire first (CLAUDE.md, "bmdwire first").
  The capture, `saveall.pcapng` (956 MB), stays out of git.
- **Not yet verified on hardware:** as after the eleventh run.

## 2026-10-14: thirteenth run, profile saves from the still cache

- **Commit:** `797a3dc`, built from the working tree before it was
  committed, run as an engine of its own (its own socket and config,
  state and cache directories) beside the installed one.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the twelfth run left
  it, 20 stills of 8 different pictures.
- **Run:** `profile save` twice, `--json`, timed, into an empty still
  cache.
- **Result:** both saves worked, every still written.
  - The first took 16.2 s from the command to the answer, the engine's
    start and the connect included: each of the 8 pictures downloaded
    once, the other 12 slots marked `from_cache` (a picture already
    fetched for another slot). The twelfth run's save, every slot
    downloaded, took 46 s.
  - The second took 0.9 s: all 20 stills `from_cache`, nothing
    downloaded.
  - The cache held 8 PNGs, 27 MB; the save's folder 8 files, as the
    repeats share their names.
- **Changed because of it:** nothing; it confirms the still cache
  (`internal/stillcache`) on hardware.
- **Not yet verified on hardware:** a restore of a profile with
  renamed (`<name>_2`) or unnamed stills; otherwise as after the
  eleventh run.

## 2026-10-14: fourteenth run, renamed and unnamed stills, upload speed

- **Commit:** `1dc23d1`, built and run as an engine of its own (its own
  socket and config, state and cache directories), for everything up to
  the restore; then the working tree that became `ece244b`, the same
  code, for the last upload.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the twelfth run left
  it, 20 stills of 8 different pictures. Nothing from the media players
  was on air throughout.
- **Why:** the thirteenth run left a restore of a profile with renamed
  (`<name>_2`) or unnamed stills unverified.
- **Run:**
  - `profile save` of everything, as a backup: 15.8 s into an empty
    still cache, 20 stills in 8 PNGs.
  - Three `media upload --yes`, from the backup's PNGs, to make the
    cases: slot 18 a different picture as `backdrop_a`
    (slot 14 is `Backdrop_A`), slot 19 a different picture
    under the taken name `Backdrop_B`, slot 20 with
    `--name ""`, which the switcher kept unnamed.
  - `profile save` again, then a scratch picture uploaded to slots
    18–20, so a restore that left a slot alone would show.
  - `profile restore --macros-only --macros false --media-pool-images
    true` of that save was not run: the session's permission check
    refused it.
  - Offline, each saved PNG converted back to a frame as an upload
    converts it, its MD5 against the hash the switcher reported for the
    slot it came from.
  - The owner, in ATEM Software Control: a save with all 20 slots used,
    then every slot cleared, then a restore of a profile with 20 stills,
    timed by stopwatch. That left the pool with that profile's stills,
    under new hashes.
  - One `media upload` with aggressive drain: slot 19's own picture back
    into it, under its own name.
- **Result:**
  - The save named the files as designed: `backdrop_a_2.png`
    for slot 18, `Backdrop_B_2.png` for slot 19 and, for the
    unnamed slot 20, `Slot19.png`. bmdwire's
    `download_media_pool_images` names an unnamed still `Slot{slot}` by
    its index from 0, and bmdctl and WebATEM follow it; the XML's
    `index="19"` matches it, the slot number bmdctl and ASC show
    doesn't. Every `<Still path>` named its slot's file. 6.75 s, 3
    stills downloaded, 17 from the cache.
  - Each upload took 23.2 to 23.3 s, 22.73 s (±10 ms) from the request
    to the end of the data, whatever the picture: a photograph barely
    compresses, so about 8.3 MB went as about 5,940 chunks, five for
    each packet from the switcher. A 20-still restore would have taken
    about 8 minutes. The third run's 1.1 s upload was three flat
    colours, which RLE shrinks to almost nothing.
  - A PNG converted back doesn't give the frame the switcher hashed: 0
    of 3, for stills bmdctl itself had uploaded from known PNGs (those
    PNGs gave the switcher's hashes exactly, 3 of 3). ASC's own save and
    restore changed every hash too. A restore can't tell a slot already
    holds a picture by recomputing its hash.
  - ASC's save took about a second (its stills downloaded at connect,
    the twelfth run); its restore into an empty pool about 2 s per
    still. So its usual restores, under 10 s by the owner's account,
    must upload few stills or none.
  - With aggressive drain the upload took 1826 ms, the data 0.96 s, and
    the switcher reported the MD5 of the frame sent.
- **Changed because of it:** `ece244b`: the engine dials with aggressive
  drain (bmdwire's `aggressive_drain`, which WebATEM's uploader runs
  on), a 20-still restore about 40 s instead of 8 minutes.
  docs/PORTING.md, "Aggressive drain". Not yet: a restore that skips a
  slot already holding its picture, which needs another way to know it
  than the hash (the still cache's PNG for the slot's hash, compared
  with the profile's file).
- **Not yet verified on hardware:** the restore of renamed and unnamed
  stills, and a whole-pool restore at the new pace; otherwise as after
  the eleventh run.

## 2026-10-14: fifteenth run, a whole media pool restored

- **Commit:** `9bcd58d` (the code of `ece244b`, aggressive drain),
  built and run as an engine of its own, as in the fourteenth run.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the owner's ATEM
  Software Control restore left it (fourteenth run): 20 stills of 8
  pictures, slot 19 re-uploaded by bmdctl with its own picture. Nothing
  from the media players on air.
- **Why:** the fourteenth run couldn't run the restore; and a whole
  pool's restore at aggressive drain's pace hadn't been timed.
- **Run:**
  - `profile save` of everything, as a backup: 18.2 s, 9 files (slot 19,
    the same picture as slots 13, 16 and 17 under bmdctl's hash, got
    `Backdrop_C (BLUE)_2.png`).
  - The fourteenth run's cases again, three `media upload --yes`: slot
    18 a different picture as `backdrop_a`, slot 19 a
    different picture as `Backdrop_B`, slot 20 unnamed.
    Then `profile save`, a scratch picture into slots 18–20, and
    `profile restore --macros-only --macros false --media-pool-images
    true --yes` of the save, timed.
  - Each slot's name and hash after it, against the profile: the name
    its `<Still>` gives, and the MD5 of the frame its file converts to.
  - Slots 18–20 put back from the backup's files, under their names.
- **Result:**
  - Uploads took 1.46 to 1.69 s each.
  - The restore took 73.4 s for 20 stills and reported no error. All 20
    slots had the profile's name and exactly its frame (hash equal to
    the file's frame's MD5), `backdrop_a_2.png`,
    `Backdrop_B_2.png` and the unnamed `Slot19.png` in
    their slots.
  - Of the 73 s, the data took 0.95 to 1.00 s a still, about 20 s in
    all. Between stills: about 0.5 s, the next PNG decoded and converted
    after the last one's lock was released; but before slots 1, 4, 10,
    13, 16 and 19 the next lock request waited 5 to 6.6 s, and before 7,
    8 and 9 about 2 s. The wait is between the transfer being queued
    and its lock being asked for: the lane holds a request back until
    the switcher echoes the previous per-frame release (`LKST`), as a
    request sent while a release is in flight is eaten. The echo came
    within 0.3 s for most stills and 2 to 6 s for those. Whether the
    switcher is still storing the stills it took (they now come faster
    than at 23 s each) can't be told from the log.
  - Afterwards every slot's name was as before the run.
- **Changed because of it:** nothing yet. A restore is about 73 s for
  a whole pool of photographs, 8 minutes before `ece244b`, against
  about 2 s a still (40 s) for ATEM Software Control by the owner's
  stopwatch. What would close the gap: decoding the next still while
  one uploads (about 10 s), and whatever ASC does between stills,
  which a capture of its restore would show (whether it keeps the
  store's lock across stills, as bmdwire releases it per frame).
- **Not yet verified on hardware:** as after the fourteenth run, less
  the restore of renamed and unnamed stills and a whole pool's restore.

## 2026-10-15: sixteenth run, uploads as ATEM Software Control sends them

- **Commits:** `5ec2519` (bmdwire#36 ported) and `5d469ad` (the restore
  prepares each picture ahead), built from the working tree before each
  was committed and run as an engine of its own, as in the fourteenth
  run. And bmdwire's branch for #36 (`e2b0ced`), run from a script.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the owner's ATEM
  Software Control restore left it, 20 stills of 8 pictures. Nothing from
  the media players on air.
- **Why:** the fifteenth run's restore took 73 s, and the owner's ATEM
  Software Control restores much faster. The owner captured an ASC
  restore of 20 stills with Wireshark (2026-10-15). ASC uploaded all 20,
  changed or not, in about 12 s: 0.31 s of data a still, the release and
  the next lock request in one packet or the next and granted in under a
  millisecond, FTFD with the first budget, each 768-chunk budget sent
  whole in about 35 ms. bmdwire's MEDIA_LOCK.md has the capture.
- **Run:**
  - `profile save` of everything (read only), 1.2 s from the still cache,
    as the backup and the profile to restore.
  - bmdwire#36's `AtemProtocol` re-uploading those 20 stills to their
    slots, queued at once, from a script.
  - `profile restore --macros-only --macros false --media-pool-images
    true --yes` of that save, at `5ec2519`, then at `5d469ad`.
  - After each, every slot's name against the profile's and its hash
    against the MD5 of the frame its file converts to.
  - `profile save` with the still cache off (`BMDCTL_STILL_CACHE_MB=0`,
    read only), so its 8 pictures download back to back under the new
    handover.
- **Result:**
  - bmdwire: all 20 in 12.72 s, 0.60 to 0.64 s a still; every hash the
    MD5 of the frame sent; no resends.
  - bmdctl at `5ec2519`: 14.7 s from the first upload request to the
    answer (16.7 s for the command, with the engine's start and the
    connect). Each still's transfer 0.53 to 0.56 s, as ASC's; between
    one still's FTDC and the next FTSD, 0.03 to 0.30 s (median 0.22),
    the engine decoding, converting and RLE-encoding the next PNG.
  - bmdctl at `5d469ad`: 11.2 s from the first upload request to the
    answer (12.75 s for the command): transfers 0.56 s (median), 1 ms
    between one FTDC and the next FTSD.
  - The save: 16.0 s, 8 downloads at 1.84 to 1.85 s from one request to
    the next, the switcher's own pace with nothing between (the twelfth
    run had 0.38 to 0.59 s of bmdctl's between stills); 12 slots from the
    save's own copies of a picture; no warnings.
  - Every slot had its name and exactly its frame after each run; no
    warnings, no resends. The pool's pictures were unchanged; only the
    hashes moved to bmdctl's frames.
- **Changed because of it:** bmdwire#36, bd6ae77 (the lock handover in
  one packet, FTFD with the first budget, whole budgets, the resend timer,
  refused sends kept for retransmission) and its port, `5ec2519`; the
  restore preparing pictures ahead, `5d469ad`. A 20-still restore went
  from 8 minutes (the fourteenth run) to 73 s (the fifteenth) to 11.2 s.
- **Not yet verified on hardware:** a macro upload since bmdwire#36, and
  an upload over a slower link; otherwise as after the fifteenth run.


## 2026-10-15: seventeenth run, ATEM Software Control's turn

- **Commits:** bmdwire's code from its main, run from scripts: #36
  (1.5.0) with a first yield, then #38 (1.5.1) and #40 (1.5.2); bmdctl's
  port of them, built from the working tree before it was committed, for
  one restore.
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the sixteenth run left
  it, 20 stills of 8 pictures.
- **Why:** the owner opened ATEM Software Control while a WebATEM
  deployment (WebATEM 2.1.2, bmdwire 1.5.0) downloaded nine stills whose
  hashes had changed: ASC loaded no thumbnail for the first five, and
  loaded them only after a still was cleared from WebATEM.
- **Run:** a script downloading stills back to back for two to three
  minutes while the owner quit and reopened ASC; the owner reported the
  thumbnails, the script counted the times another client took the store.
  Two of the runs captured with Wireshark on the ASC machine.
- **Result:**
  - The captures: ASC asks for each still with a whole-store `LOCK`,
    queued while the other client holds the store and handed to ASC at
    its release, with `LKOB` and no `LKST` at all. The other client's next
    request, at once (1.5.0) or within half a second, was granted beside
    it, a `PLCK` beside ASC's `LOCK`, and its `FTSU` aborted ASC's
    download in progress with `FTDE 6`. ASC tries each still once: after
    20 stills and 20 `FTDE 6` it asked for nothing more.
  - A yield of a frame after the release announcement: 0 of 20, then 15
    of 20 with a frame after any client's release (a release and a lock
    announced together, ASC's next still, had let a request through), 0
    of 20 with a 0.5 s cap (no announcement comes when the store is handed
    over). Waiting for the store to be announced free, never asking while
    another client holds it, and waiting while anyone is seen at work
    (bmdwire#38): **20 of 20**, the script pausing 38 s for ASC's turn.
  - The same with the script queueing one still at a time, 250 ms after
    the last, as WebATEM's watcher does: 1.5.1 yields only with work
    queued at the release; with the yield after every release (bmdwire#40),
    **20 of 20**, a 38 s pause.
  - 20 uploads alone: 12.7 s with 1.5.0, 14.2 s with 1.5.2.
  - bmdctl's port, `profile restore` of the 20 stills with ASC open:
    31.5 s, every still waiting about 2 s after the last: ASC fetching
    each changed still again for its thumbnail. No warnings.
- **Changed because of it:** bmdwire#38 and #40 (released as 1.5.1 and
  1.5.2) and their port; WebATEM 2.1.3; bmdwire's MEDIA_LOCK.md,
  "Sharing the store with ATEM Software Control". bmdwire#36's handover
  was the cause; 1.4.0's echo wait had avoided it by accident, and was
  behind the fifteenth run's pauses (WebATEM refreshing thumbnails).
- **Not yet verified on hardware:** ASC connecting while bmdctl itself
  transfers (its port matches bmdwire's code, tested against the fake
  switcher's model of the same); otherwise as after the sixteenth run.
- *Reworded 2026-10-27 to leave out a private deployment's details.*

## 2026-10-15: eighteenth run, the UDP thread waiting on itself

- **Commits:** none of bmdctl's. bmdwire run from scripts: v1.5.2
  (`65b9ee0`) and bmdwire#42's branch (`81955c1`).
- **Switcher:** Studio ATEM, an ATEM 1 M/E Constellation HD, protocol
  2.30, firmware 9.2.1, 1080p25; the media pool as the sixteenth run left
  it.
- **Why:** the owner's check of WebATEM 2.1.3 on a WebATEM deployment:
  a profile restore of 20 stills, with ATEM Software Control connecting
  partway. ASC loaded all 20 thumbnails and the page waited 37 s for its
  turn, but the restore hung at its fourth still and never timed out. A
  stack dump of the server process showed the protocol thread in
  `drain_queue` and the UDP thread putting an ACK on its own queue, both
  waiting in `SocketQueue.put`: the socketpair under the queue holds 278
  entries, an upload budget is up to 768, and the UDP thread is its only
  reader.
- **Run:** scripts uploading the pool's own 20 stills back. In separate
  processes, beside one or two sessions downloading macros and one
  downloading stills: 20 of 20 on 1.5.2 both times. In one process with
  a session downloading stills as WebATEM's watcher does, both pumped as
  WebATEM pumps them: 1.5.2 hung after 2 of 20, faulthandler showing the
  same two stacks; bmdwire#42's branch, five runs, no hang; the two whose
  results were kept uploaded 20 of 20 with every slot's hash the frame
  sent (39.4 s and 26.1 s).
- **Result:** bmdwire#42 fixes it (the UDP thread sends its own packets
  at once). bmdctl has no such queue: its transport is one goroutine
  writing to the socket. Seen along the way, in 1.5.2 as well: two
  sessions waiting on the same release become due together, and the
  later one's grant aborts the earlier one's upload, which goes again
  (three runs of five); with a third session downloading macros, the
  still and macro downloads retried in lockstep without end.
- **Changed because of it:** bmdwire#42, and the WebATEM release that
  takes it.
- **Not yet verified on hardware:** as after the seventeenth run.
- *Reworded 2026-10-27 to leave out a private deployment's details.*

## 2026-10-27: nineteenth run, the first HyperDeck

- **Commit:** `c0d5593`, built as its own binary and run with its own
  config, state and socket, so the owner's hosts.yml was never touched.
  Every connect named its device (`--type`), so the probe knocked on no
  other port.
- **Deck:** the owner's test HyperDeck, a HyperDeck Studio HD Mini at
  192.168.82.192, software 9.0.2, protocol 1.19 (the fake speaks 1.13),
  1080p25. Slot 1 an SD card (64 GB, 44 min left), slots 2 and 3 empty,
  no video input. One 10 s DNxHD220 clip playing on loop, single clip.
- **Run:** read-only first: `connect --type hyperdeck`, `status`,
  `clips`, `clips --disk`, `state`, `state --path transport.status`, each
  down a pipe and as JSON; `watch` for 8 s; the terminal views. Then, with
  the owner's go-ahead, writes with a `watch` running through them: `stop`,
  `play`, `loop off`, `loop on`, `goto 1`, `slot 1`, `next`, `prev`,
  `play --speed 50` and `100`, `record bmdctl-test`, `stop`; then back to
  clip 1 playing on loop, single clip, as it was.
- **Result:** every read and write answered in 1 to 3 ms (172 ms for the
  `goto` out of preview), each write confirmed by the deck's own
  notification; `watch` showed each change as it came, before → after.
  The state parsed every block (none left over), and the terminal views
  kept to 72 columns. What the deck does that the fake doesn't:
  - `next` and `prev` on a one-clip timeline: the deck refuses `goto:
    clip id: +1` (and `-1`) with 109, out of range. The fake holds at
    the ends; docs/PORTING.md had this as unknown.
  - `record` with no input: refused, 110 no input, so nothing was
    written to the card. The attempt put the deck in preview, which
    `stop` didn't leave (it answers, and nothing changes); `goto` did.
  - `stop` clears loop and single clip (the deck's notification:
    `loop true → false, single clip true → false`), and `play` without
    `--loop` plays once: so `loop on` a second later found the clip had
    run out ("the deck is stopped").
  - Two of bmdctl's own: `watch` said `forward` where the result line
    said `forward 50%`, and `status` lists an empty slot as `empty, 0s
    left`.
- **Changed because of it:** nothing yet. The deck's answers at the ends
  of the timeline and its preview after a refused record go to bmdwire
  first (hyperdeckwire's notes), then the fake and bmdctl's messages.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-27: twentieth run, the first Ultimatte

- **Commit:** `c0d5593`, run as in the nineteenth.
- **Keyer:** the owner's test Ultimatte, an Ultimatte 12 4K at
  192.168.81.42, software release 2.3, protocol 2.3, 1080p25, no input
  locked. Label "Studio Keyer", presets "1" and "2", its current file
  "3", modified and not saved.
- **Run:** read-only first: `connect --type ultimatte`, `status`,
  `prelude`, `controls`, `presets`, `network show`, each down a pipe and as
  JSON, `archive`, the terminal views. Then, with the owner's go-ahead:
  `restore` of that archive without `--yes` (refused, exit 5), then with
  it; a prelude just before and after, and an archive after.
- **Result:** the reads matched the prelude field for field (666 lines,
  CONTROL DEFAULT among them), the controls' percentages are
  ultimattewire's ranges, and the terminal views kept to 72 columns.
  `archive` took 0.7 s: both presets, GPISettings, SavedSettings and the
  readable config, Smart Remote 4's layout. `restore` took 2.9 s, and the
  archive after it was byte for byte the same for the presets,
  GPISettings and SavedSettings. But writing SavedSettings applied it
  live: this unit's saved settings were still its defaults, and its
  1080p25 working state had never been saved. The current file became
  "Ultimatte Defaults" (clean), the video format 525i59.94 NTSC, and 38
  values changed (the densities, shadow, cleanup, the FG colour, the
  window skew, Monitor Out `fg` → `program`, Active Unit `off` → `on`).
  The restore's confirmation says only "its live state changes", and
  `--yes` skips it. The values before it were handed to the owner to set
  back on the unit; no command in bmdctl or ultimattewire sets them.
  Also: bmdctl calls the unit "Ultimatte", though its prelude's
  `IDENTITY:` says `Model: Ultimatte 12 4K`; neither bmdctl nor
  ultimattewire reads that block.
- **Changed because of it:** nothing yet. To bmdwire first: that a
  SavedSettings write applies it live, video format included, and the
  model in `IDENTITY:`. Then bmdctl's restore confirmation says what is
  lost (the unsaved current file, the video format), and its header names
  the model.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-27: a correction to the twentieth run

- **No hardware run:** this is read from the twentieth run's own
  preludes, kept from just before and just after its restore.
- **What the entry got wrong:** it says the restore left the keyer in
  525i59.94 NTSC, and that a SavedSettings write applies the video format
  too. The preludes say otherwise. The CONTROL section's `Video Format`,
  the setting, was `auto detect` before the restore and after it (its
  reference source `fg input` both times). What changed is the DEVICE
  section's `Video Format`, the format the unit was running: 1080p25
  before, 525i59.94 NTSC after, with no input locked either time. With
  nothing to detect, the unit fell back from the format it had last
  detected to its default, and a signal brings it back; the owner found
  it on auto detect.
- **What the restore did change**, as the entry says: the control values
  (38 of them) and the unsaved current file, now "Ultimatte Defaults".
- **Changed because of it:** bmdwire#56 corrects 1.7.0's restore docs.
  bmdctl's restore confirmation says "the saved settings become its live
  state", without "video format included", and the guide, PORTING.md and
  SURFACE-ultimatte.md say the same.

## 2026-10-27: twenty-first run, the new HyperDeck commands

- **Commit:** `f42e52f`, built and run as in the nineteenth, its own
  config, state and socket; connected with `--type hyperdeck`.
- **Deck:** the test HyperDeck Studio HD Mini at 192.168.82.192, software
  9.0.2, protocol 1.19, 1080p25, as the nineteenth run left it: one 10 s
  clip playing on loop, single clip; remote on; record prefix
  `HyperDeck`.
- **Run:** with the owner's go-ahead, and a `watch` through it: `pause`
  twice, `play --loop --single-clip`; `stop`, `timeline add` the clip
  `--before 1`, `clips`, `timeline remove 1`, `timeline clear` without
  `--yes` (refused, exit 5) and with it, `timeline add` the clip, `goto
  1`, `play --loop --single-clip`; `configuration show`, `configuration
  set record-prefix bmdctl-test` twice, then back to `HyperDeck`;
  `remote off`, `stop`, `remote on`.
- **Result:** `pause` stopped the deck (`play → stopped`), and again was
  already. `configuration show` listed all 20 settings; the record
  prefix changed and came back, the second set was already, and the
  deck's 511 notification carried each. `remote off` made the deck
  refuse writes; `remote on` was taken over 9993 with remote off, so
  bmdctl can't lock itself out. `timeline add --before 1` and `timeline
  remove 1` worked (two clips, then one). Then `timeline clear` emptied
  the timeline, and the deck answers `clips get` on an empty timeline
  with `107 timeline empty`, not an empty list: `timeline add` (which
  reads the timeline first), `clips`, `goto` and `play` all failed, and
  `status` went on showing the session's stale `1 clip`. The deck was
  put back (the clip on the timeline, playing on loop) with a one-off
  build that took that 107 as an empty timeline, never committed.
- **Changed because of it:** to bmdwire first: that `clips get` answers
  107 on an empty timeline, which hyperdeckwire's `clips_get` raises.
  Then bmdctl reads it as an empty timeline (`clips`, the `timeline`
  commands, `status`). `remote` loses its unverified mark.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-27: twenty-second run, an empty timeline after bmdwire 1.7.1

- **Commit:** `9bd3f55` (bmdwire 1.7.1 ported: `clips get`'s 107 on an
  empty timeline is no clips), built and run as in the nineteenth.
- **Deck:** the test Studio HD Mini, as the twenty-first run left it:
  the clip playing on loop.
- **Run:** `stop`, `timeline clear --yes`, `clips`, `status`, `timeline
  add` the clip, `goto 1`, `play --loop --single-clip`. Then again with
  `clips --json` run every 0.1 s after the clear, and the clip put back.
- **Result:** on the empty timeline `timeline add` worked (`empty → 1
  clip`), where the twenty-first run's failed, and the deck was back
  playing on loop both times. But the clear is applied after its `200
  ok`: 0.01 s after it, `clips get` still listed the one clip; at 0.16 s
  it answered 107, an empty timeline. So `timeline clear`'s read-back,
  at once, said `1 clip`, and a `clips` and a `status` run straight
  after it still listed the clip. The twenty-first run's `clips` after
  its clear, put down then to the session's stale state, was this too.
  `timeline add` and `remove` read back their changes at once.
- **Changed because of it:** to bmdwire first: that a `clips clear` is
  applied after its answer. Then `timeline clear` reads the timeline
  back until it is empty, for a while, instead of once.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-28: twenty-third run, loop and timeline clear

- **Commit:** `92bc3c3` (bmdwire 1.8.0 ported, and `2c4a9b1`'s
  `timeline clear` reading the timeline back until it is empty), built
  and run as in the nineteenth.
- **Deck:** the test Studio HD Mini, as the twenty-second run left it:
  the clip playing on loop, single clip; remote on.
- **Run:** read-only first (`connect`, `status`, `clips`, `state`). Then,
  with the owner's go-ahead and a `watch` through it: `loop off` while
  the clip looped, and 12 s to let it play out; `goto 1`, `play
  --single-clip`, `loop on`, and 12 s past the clip's end. Then twice:
  `stop`, `timeline clear --yes`, `clips` and `status` at once and
  `clips` a second later, `timeline add` the clip, `goto 1`, `play
  --loop --single-clip`.
- **Result:** `loop off` sent `play: loop: false single clip: true
  speed: 100`, a line no deck had been sent before (the nineteenth
  run's `loop off` was already): the deck's notification said `loop
  true → false`, it played on, and at the clip's end stopped, its
  notification clearing single clip as a `stop` does. `loop on` on the
  playback started without it: `loop false → true`, and the deck was
  still playing 12 s later. `timeline clear` took 0.07 s each time and
  said the timeline was empty; the `clips` and `status` right after it
  listed none (`0 clips`), where the twenty-second run's still listed
  the clip. The deck was left as it began, the clip playing on loop,
  single clip.
- **Changed because of it:** `loop` loses its unverified mark.
  `timeline clear`'s read-back comes off the Unverified list.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-29: twenty-fourth run, the ATEM before v0.2.0

- **Commit:** `dd65e52`, built and run as in the nineteenth. bmdctl
  hadn't driven the ATEM since the sixteenth run (`5d469ad`); since then
  it ported bmdwire 1.5.1 to 1.8.0, among them another client's turn at
  every release (bmdwire#38, #40), the UDP thread (#42) and the legacy
  mixer's readers (#49); a transfer's last progress report reaches its
  caller (`486515f`), and the audio mixer is picked as atemwire picks it
  (`bd6d9e1`).
- **Switcher:** Studio ATEM, the Constellation at 192.168.81.85,
  protocol 2.30, 1080p25; Media Player 1 (slot 1) on program, input 3 on
  preview; the media pool full, 20 stills of 7 pictures, slots 1 and 20
  in the media players.
- **Run:** read-only first: `connect`, `status`, `state`, `show` of
  every area, `media list`, `macro list`, `audio meters`, `watch` for
  8 s, each down a pipe and at a terminal; `profile save` (the backup)
  and `media download 2`. Then, with the owner's go-ahead and a `watch`
  through it: slot 2's own picture, as downloaded, uploaded back to slot
  2 under its name; `media download 2` again; `profile save` again;
  `preview 1`, `cut`, `auto`, `undo`; then `program` and `preview` put
  back, and `auto`, 2 s, `undo`.
- **Result:**
  - Every read answered in 10 to 20 ms. `show audio` and `audio meters`
    read the Fairlight mixer, so the mixer is picked right on a
    Fairlight switcher. The engine logged no warnings beyond the refused
    `undo` below.
  - The terminal views: the switcher's name bold with program and
    preview dim beside it, the value on program red and on preview
    green, every status dim and ending at column 72, nothing wider.
  - `profile save` took 14.2 s with an empty still cache (7 pictures
    downloaded at 1.8 s each). The second, after the upload, took 2.9 s
    and downloaded only slot 2's new picture; slot 2 got a file of its
    own (`<name>_2.png`), and the picture that had `_2` moved to `_3`.
  - The upload took 0.85 s. The switcher's hash for slot 2 was the MD5
    of the frame bmdctl built from the file, and that frame, converted
    back here, was the switcher's download pixel for pixel. A still
    downloaded and uploaded back is not the same frame, though: against
    the first download, 98.3% of pixels moved by 2 levels at most (8-bit
    RGB against the switcher's 10-bit Y'CbCr), and 0.13% by more than
    32, at colour edges, where a pixel pair's shared Cb and Cr fall
    outside RGB and are clipped. bmdwire's `rgb_to_atem` does the same.
  - Right after the upload the switcher reported the pool's lock taken
    by another client for 1.88 s, a still download's length; bmdctl's
    `media download` waited for it and then took the lock (4.2 s, against
    2.2 s for the one before).
  - `preview 1`, `cut` and `auto` were confirmed in 12 to 34 ms, both
    buses shown. `undo` straight after the `auto` was refused while the
    mix ran, as it should be. `program` and `preview`, put back while
    the mix still ran, were undone by its end, which took preview to
    program; set again after. Then `undo` 2 s after an `auto` set both
    buses back in 28 ms.
  - The switcher was left as it began: Media Player 1 on program, input 3
    on preview, no transition running, no lock held; slot 2 holds its
    picture as bmdctl converted it.
- **Changed because of it:** undo of a finished auto, the terminal
  layout and bmdctl's handling of another client's turn after its own
  upload have now run on hardware; the first two come off the
  Unverified list.
- **Not yet verified on hardware:** as the list at the top says.

## 2026-10-29: twenty-fifth run, the first Videohub

- **Commit:** `bf47547`, the v0.2.0 release binary, installed on the
  development machine with install.sh from main.
- **Router:** a Blackmagic Videohub 40x40 12G, protocol 2.8, one of the
  owner's two live routers; its name and address are left out, as a
  private deployment's.
- **Run:** by the owner, by hand: `connect ADDRESS`, `status`, a
  `route`, and a `route` putting the output back.
- **Result:**
  - `connect` asked the address what it is: the probe found a Videohub
    in 6 ms, the session opened and was pinned at 7 ms, and the router
    was saved to hosts.yml as the default host.
  - `status` answered from the session; each `route` was answered in
    under a millisecond by the engine's log (`took_ms` 0). The owner
    reported both routes working; their result lines weren't kept, so
    whether either was `already` isn't recorded.
  - The engine logged no warnings.
- **Changed because of it:** `connect`, `status` and `route` have run on
  a Videohub, and the probe on one; the Unverified list, the Videohub
  guide, the README and the Claude skill say so.
- **Not yet verified on hardware:** as the list at the top says.
