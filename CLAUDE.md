# bmdctl: notes for future sessions

bmdctl is a Go port of bmdwire v1.3.0 (`../bmdwire`, checked out at tag
`v1.3.0`, commit `cea58c2`) plus one CLI binary. Read `docs/PORTING.md`
before changing anything in `atem/`: it records every decision already
made, so you follow it instead of re-deriving it. Add to it whenever you
make a new one.

## Build and test

```
go build ./... && go vet ./... && go test -race ./...
```

All three must be green after every commit. `-race` is what CI runs
(`.github/workflows/ci.yml`); without it the timing differs, and CI
failed on every push from `68fadb8` to `4d3c452` while a plain
`go test ./...` passed. Nothing in the default build or test run touches
the network beyond loopback.

After every push, wait for CI and check it (`gh run watch`, then
`gh run list --limit 1`) before reporting green. Local green is not
green.

`docs/COMMANDS.md` is generated from the command table; after changing a
table entry, regenerate it with
`go test ./cmd/bmdctl -run TestCommandsDoc -update`. What a terminal
shows (colour, the prompt, the spinner) is snapshot-tested in
`cmd/bmdctl/testdata/terminal/`; after changing it, rewrite them with
`go test ./cmd/bmdctl -run TestTerminal -update` and read the diff. The oracles in
`internal/fixtures/oracle/` are regenerated with the scripts beside them
(see the README there) against `../bmdwire` at `v1.3.0`.

## The hardware rule

The rule covers tests and development tooling: tests, examples, benchmarks,
oracle and fixture generators, and any script in this repo. None of them
may contact a real switcher unless built with `-tags hardware`. It does not
cover the shipped `bmdctl` binary, which sends to whatever host the user
names with `--host`; its own tests point it at `internal/fakeatem`.

Hardware tests read the target from
`BMDCTL_TEST_ATEM` and refuse to run against anything other than
`192.168.81.85` or `192.168.81.86`. They are read-only unless
`BMDCTL_TEST_WRITE=1` is also set, and even then must never change the
video mode, clear stills, restore a profile or save startup state.

Everything else runs against `internal/fixtures` or the fake switcher in
`internal/fakeatem`.

`docs/HARDWARE.md` records every hardware session. After each one
(whoever ran it), append a section: date, commit, switcher model,
protocol version, video mode, what was run, what happened and what
changed because of it. Never rewrite an earlier section.

## Rules

- **Mirror bmdwire, don't redesign it.** `docs/SURFACE-atem.md` is the spec
  for `atem/`; read `../bmdwire/atemwire/` for anything it leaves out. Cite
  the tag (`bmdwire v1.3.0`) wherever the port references it. Where the port
  must differ, say so in `docs/PORTING.md` with the reason.
- **Standard library only** in the device packages (`atem`, `hyperdeck`,
  `videohub`, `ultimatte`). `golang.org/x/image` is allowed later, for
  image fitting only.
- **Licences.** `atem/` is LGPL-3.0-only: every file starts with the two
  copyright lines and `// SPDX-License-Identifier: LGPL-3.0-only` (copy them
  from `atem/doc.go`). `hyperdeck/`, `videohub/`, `ultimatte/`, `cmd/` and
  the other `internal/` packages are MIT (`// SPDX-License-Identifier: MIT`).
  `internal/fixtures/` holds data copied from atemwire's tests and is
  LGPL-3.0-only.
- **Commits.** One commit per step, conventional commit messages
  (`feat(atem): …`, `test(fixtures): …`, `docs: …`), and push after every
  commit. The build/vet/test line above is green at each one.
- **Scope.** Phase 2 added transfers (stills, macros), the macro bytecode
  codec, profiles and the display-unit readers. Phase 3 added the binary:
  the background engine, hosts, every command through the engine,
  `exec`, completion, man pages and `doctor`. HTTP serve, MCP and the
  other devices are phase 4. Don't start them unless asked.

## Where things are

- `atem/`: the ATEM package (transport, session, state, parsers, commands,
  readers, transfers, still image conversion).
- `atem/macro/`: the macro bytecode codec. `atem/profile/`: profile save
  and apply.
- `internal/fixtures/`: recorded packets, bytecode samples, the profile XML
  golden and the state snapshots, copied raw from bmdwire's tests, and the
  oracles generated from bmdwire; the README there lists where each came
  from.
- `internal/table/`: the command table every device package fills, which the
  CLI and `docs/COMMANDS.md` are generated from.
- `internal/fakeatem/`: a fake switcher at the `net.PacketConn` level.
- `internal/engine/`: the background engine (sessions, the socket
  protocol, the operations that aren't one wire command).
- `internal/hosts/`: hosts.yml. `internal/paths/`: where files live.
  `internal/mdns/`: the Bonjour browse behind `hosts discover`.
- `cmd/bmdctl/`: the CLI.
- `.github/banner.gif` (`banner.png` is its still), `.github/mark/`: the
  README banner and the mark.
  `docs/BRAND.md` is their spec; follow it for anything branded.

## Terminal output

Every command a person runs at a terminal follows these rules
(`docs/terminal-mock.html` is the picture, `docs/PORTING.md`
"Terminal" the record). A new command follows them too; if it can't,
say why there.

- Only at a terminal. A pipe gets the old line format (`docs/PORTING.md`,
  "Output conventions") byte for byte; `--json` and `--quiet` never
  change. Anything a terminal needs that a response doesn't carry comes
  in a note frame (`engine.Note`), which `--json` never prints; don't add
  fields to a result for it.
- The switcher is named once per invocation, in the header
  (`printer.show`): bold name, then dim `PGM x · PVW y`, and model, mode
  and `connected N ms` only when this invocation opened the session.
  Never put the name or IP on a result line.
- Results are `c.resultLine(verb, change, status)`: two-space indent,
  verb padded to 10, `before → after`, status right-aligned and dim.
  Status is the time, or `sent`, `already`, `rejected`; never
  `confirmed`. A cut or auto shows both buses.
- Nothing over 72 columns: `fit` cuts with `…`, never wrap.
- Colour: the value now on program red, on preview green, bold for the
  switcher's name and bmdctl's own lines, dim for ids, timings, hints,
  empties and the banner. Nothing else.
- Collapse repeats (`aux 1–6  PGM`), count empty slots instead of
  listing them (`--all` lists them), hide what people rarely want
  behind `--all`.
- Errors: `c.screenError(msg, hint)`: red `error`, the message without
  the command path, the next step dim under it.
- Waits over 200 ms show the spinner on stderr; a transfer of known
  size sends `progress` notes for a bar; several steps send `steps` and
  `step` notes and print `✓` lines. Destructive commands describe their
  impact in a `confirm` note so a terminal can ask `[y/N]`.
- Every table entry has one to three `Examples` (`TestExamples`).
- Every new view gets a golden in `cmd/bmdctl/testdata/terminal/`.
