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
