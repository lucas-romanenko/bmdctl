# bmdctl: notes for future sessions

bmdctl is a Go port of bmdwire (`../bmdwire`), written against v1.3.0
(tag `v1.3.0`, commit `e5e336c`) and following v1.8.0 (tag `v1.8.0`,
commit `01024ad`), as `docs/PORTING.md` ("Source of truth") says, plus
one CLI binary. Read `docs/PORTING.md`
before changing anything in `atem/`: it records every decision already
made, so you follow it instead of re-deriving it. Add to it whenever you
make a new one.

## Build and test

```
go build ./... && go vet ./... && go test -race ./...
```

All three must be green after every commit, and `gofmt -l .` must print
nothing (CI's Format step fails otherwise). `-race` is what CI runs
(`.github/workflows/ci.yml`), and the timing differs without it: a plain
`go test ./...` can pass where CI fails. Nothing in the default build or
test run touches the network beyond loopback.

Before every commit: go test -race ./<package> for every package you touched, not just the tests you were working on. Red CI from a test you didn't run is a wasted push.

After every push, wait for CI and check it (`gh run watch`, then
`gh run list --limit 1`) before reporting green. Local green is not
green.

Never pipe test output through `head`, `tail` or `grep` when the exit
code matters: the pipeline's status is the last command's, so a failing
`go test` reads as passing. Send it to a file and read the status, or
check `${PIPESTATUS[0]}`.

`docs/COMMANDS.md` and a page per command group in `docs/commands/` are
generated from the command table; after changing a table entry,
regenerate them with `go generate ./cmd/bmdctl` (it runs
`go test ./cmd/bmdctl -run TestCommandsDoc -update`). What a terminal
shows (colour, the prompt, the spinner) is snapshot-tested in
`cmd/bmdctl/testdata/terminal/`; after changing it, rewrite them with
`go test ./cmd/bmdctl -run TestTerminal -update` and read the diff. The oracles in
`internal/fixtures/oracle/` and the `testdata/oracle/` of `hyperdeck/`,
`videohub/` and `ultimatte/` are regenerated with the scripts beside
them (see the README there) against `../bmdwire` at the version each
file records (`recv.json`, `readers.json` and the Videohub oracle at
`v1.6.0`, the Ultimatte one at `v1.7.0`, the rest at `v1.4.0`;
`imaging.json` at `v1.3.0`, the README says why; the HyperDeck
`client.json` at `v1.8.0`).

The documentation site (`mkdocs.yml`) is built from `docs/` as it is:
`pip install -r docsite/requirements.txt`, then `mkdocs serve` to read
it and `mkdocs build --strict` (CI's `docs site` job) to check it;
`python3 docsite/test_terminal.py` checks its terminal colours against
the goldens. `.github/workflows/pages.yml` publishes it on a push to
main (skipped while the repository is private). docs/PORTING.md,
"Documentation site", records how it is put together.

Releases are GoReleaser's: the maintainer pushes the `v*` tag, so never
tag.
`goreleaser release --snapshot --clean` is the dry run (docs/PORTING.md,
"Release", says what was checked); `install.sh`, `scoop/bmdctl.json` and
the `Dockerfile` belong to it.

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
changed because of it. Never rewrite an earlier section. Then bring
the "Tested on" table and the "Unverified" list at its top up to date.

## bmdwire first

bmdctl follows bmdwire; it never leads it.

- Protocol knowledge lands in bmdwire first. A packet layout, a field, a
  quirk or a fix, wherever it was found (on hardware, in a capture,
  while porting), is a bmdwire PR with its tests before bmdctl has it.
- bmdwire's SURFACE.md is the source of truth. `docs/SURFACE-atem.md` is
  a copy of `atemwire/docs/SURFACE.md`, never edited ahead of it.
  hyperdeckwire, videohubwire and ultimattewire have none yet, so
  `docs/SURFACE-hyperdeck.md`, `docs/SURFACE-videohub.md` and
  `docs/SURFACE-ultimatte.md` were written here; anything new about those
  protocols still goes to bmdwire first, and into a `docs/SURFACE.md`
  beside the package there.
- bmdctl ports from it and cites the bmdwire PR (`bmdwire#N` and the
  commit) in the code and in `docs/PORTING.md`.
- Never add a command or packet to bmdctl that bmdwire doesn't have
  (anything sent to a device or parsed from one; a CLI verb built from
  what bmdwire has is fine).
- Two issues say when bmdctl is behind. bmdwire's publish workflow opens
  "Port bmdwire v<version>" (label `port`) for each release on PyPI, and
  `.github/workflows/surface-drift.yml` keeps "SURFACE drift from
  bmdwire" open while the SURFACE copies differ from bmdwire main. The
  `Tracks:` line in `docs/PORTING.md` names the bmdwire the port
  follows; move it in the commit that finishes a port.

## Rules

- **Mirror bmdwire, don't redesign it.** `docs/SURFACE-atem.md` is the spec
  for `atem/`, `docs/SURFACE-hyperdeck.md` for `hyperdeck/`,
  `docs/SURFACE-videohub.md` for `videohub/` and `docs/SURFACE-ultimatte.md`
  for `ultimatte/`; read the bmdwire package (`../bmdwire/atemwire/` and
  so on) for anything they leave out. Cite
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
  LGPL-3.0-only; hyperdeckwire's, videohubwire's and ultimattewire's (MIT)
  are in their packages' `testdata/`.
- **Commits.** One commit per step, conventional commit messages
  (`feat(atem): …`, `test(fixtures): …`, `docs: …`), and push after every
  commit. The build/vet/test line above is green at each one.
- **Scope.** Not ported yet: hyperdeckwire's FTP clip upload
  (`upload.py`) and its setup API (`setup.py`). Don't start either unless
  asked.

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
- `hyperdeck/`: the HyperDeck package (TCP 9993); `hyperdeck/testdata/`
  its fixtures and oracle. `internal/fakehyperdeck/`: a fake deck on any
  `net.Conn` (a pipe, or loopback).
- `videohub/`: the Videohub package (TCP 9990), `ultimatte/` the Ultimatte
  package (TCP 9998 and 9996), each with its fixtures and oracle in
  `testdata/`. `internal/fakevideohub/`, `internal/fakeultimatte/`: a fake
  router and a fake keyer. `internal/fakenet/`: the fakes at addresses,
  for the probe and the CLI's tests. `internal/pytext/`: CPython's text
  rules, which the ports' parsers follow.
- `internal/engine/`: the background engine (sessions, the socket
  protocol, the operations that aren't one wire command).
- `internal/hosts/`: hosts.yml. `internal/paths/`: where files live.
  `internal/mdns/`: the Bonjour browse behind `hosts discover`.
  `internal/stillcache/`: the engine's media pool stills by hash, which
  a profile save writes without downloading them again.
- `cmd/bmdctl/`: the CLI. `internal/serve/`: the HTTP API; `internal/mcp/`:
  the MCP server. `skills/bmdctl/SKILL.md`: the Claude skill (under 150
  lines; `TestSkillCommands` checks its command lines). `docs/AGENTS.md`
  says how agents use both.
- `docs/GUIDE.md` (the guide's index) and its pages in `docs/guide/`,
  `docs/quickstart.md`, `docs/INSTALL.md`, `docs/ARCHITECTURE.md` and
  `CONTRIBUTING.md` are written by hand: when a command's output, a
  flag or a path changes, check the guide's sample of it.
  `TestDocsCommands` checks every command line in them; the quick
  start's output is checked against `quickstart.golden`.
- `mkdocs.yml`, `docsite/`: the documentation site (its hooks, theme
  CSS, requirements and the colours' test). `docs/index.md` and
  `docs/contributing.md` are its shells for the README's and
  CONTRIBUTING's text; `docs/guide/` the guide's pages,
  `docs/commands/` the generated reference's.
- `.github/banner.gif` (`banner.png` is its still), `.github/mark/`: the
  README banner and the mark. `docs/brand/social-preview.png` is the
  repository's social preview (uploaded in Settings by hand), and
  `docs/brand/launch/` the launch post's images, with `launch.py`.
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
  (`printer.show`): bold name, then dim `PGM x · PVW y` as they were
  before the command ran (the one `on_air` note, read once), and model,
  mode and `connected N ms` only when this invocation opened the
  session. Never put the name or IP on a result line.
- Results are `c.resultLine(verb, change, status)`: two-space indent,
  verb padded to 10, `before → after`, status dim and right-aligned to
  end at column 72. Status is the time, or `sent`, `already`,
  `rejected`; never `confirmed`. A cut or auto shows both buses. A
  table row with a status (on air, `● PGM`, a session) ends it at
  column 72 too, through `trailing`.
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
  The site colours samples by the same rules, and
  `docsite/test_terminal.py` fails until `docsite/hooks/terminal.py`
  colours the new golden as the CLI does.
