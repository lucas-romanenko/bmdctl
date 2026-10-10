# Hardware sessions

Every session of bmdctl against a real switcher, oldest first. Append one
section per session, never rewrite an old one: the date, the commit that
ran, the switcher (model, the protocol version its `_ver` packet reports,
the video mode), what was run, what happened, and what changed because
of it. The rules for hardware tests (which switchers, read-only unless
`BMDCTL_TEST_WRITE=1`, what is never done) are in `CLAUDE.md`.

## 2026-10-10: first run

- **Commit:** `576c4fd`, the head of main when the run was reported (the
  report didn't name the commit).
- **Switcher:** ATEM 1 M/E Constellation HD, protocol 2.30, 1080p25.
- **Run:** reads only, through the engine: the connect (`Dial`: the
  handshake, the whole state dump and the video mode; it sends `TiRq`
  itself), `info`, `status`, `state` and `watch`. No other command was
  sent to the switcher.
- **Result:** all of them worked.
- **Changed because of it** (`b7b9aff`):
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

- **Commit:** `8de68ca`.
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

- **Commit:** `1bf974a`.
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
