# Contributing

Thanks for looking. bmdctl is a port of [bmdwire](https://github.com/lucas-romanenko/bmdwire)
with a CLI on top, so most changes start from one rule: mirror bmdwire,
don't redesign it. [docs/PORTING.md](docs/PORTING.md) records every
decision the port has made, and its `Tracks:` line the bmdwire version it
follows; read the section for the code you're changing before you change
it, and add to it when you make a new one.
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) is the map.

Bug reports and device-support requests are welcome as issues. For a
change of any size, open an issue first so we can agree on the shape.

## Setup

- **Go 1.27** (the version in `go.mod`). There are no dependencies: the
  module has no `go.sum`, and the device packages use the standard
  library only.
- **Optional:** bash, zsh, fish and groff, which the tests use to
  syntax-check the completion scripts and the man page when they are
  installed; Python 3.12 and a bmdwire checkout, to regenerate the
  oracles (below); GoReleaser 2, for the release dry run.

```sh
git clone https://github.com/lucas-romanenko/bmdctl
cd bmdctl
go build ./... && go vet ./... && go test -race ./...
go run ./cmd/bmdctl version
```

## Commands

There is no Makefile; these are the commands, run from the repository's
root:

| Task | Command |
|---|---|
| Build, vet, test (what CI runs) | `gofmt -l .` (prints nothing), then `go build ./... && go vet ./... && go test -race ./...` |
| Run the CLI from source | `go run ./cmd/bmdctl <command>` |
| Regenerate docs/COMMANDS.md and docs/commands/ after a table change | `go generate ./cmd/bmdctl` |
| Rewrite the terminal goldens after an output change | `go test ./cmd/bmdctl -run TestTerminal -update`, then read the diff |
| Read the documentation site while writing | `pip install -r docsite/requirements.txt`, then `mkdocs serve` (http://127.0.0.1:8000/bmdctl/) |
| Check the site as CI does | `mkdocs build --strict` and `python3 docsite/test_terminal.py` |
| Regenerate the oracles | the scripts in `internal/fixtures/oracle/` and in `testdata/oracle/` under `hyperdeck/`, `videohub/` and `ultimatte/` (their READMEs) |
| Release dry run | `goreleaser release --snapshot --clean` (builds into `dist/`, publishes nothing) |

## Tests

- **Always with `-race`.** CI runs `go test -race ./...` on every push
  and pull request. The race detector changes timing as well as finding
  races: tests have passed without it and failed with it. A test that
  fails now and then shows itself run many times over:
  `go test ./atem -race -count=50 -run TestName`.
- **Wait for a condition, never for a sleep.** A test against the fake
  switcher waits for something to happen (`Dial` returning with the
  dump in, `Ready`, a transfer queued, a `connecting to` line), not for
  a sleep to be long enough. CI's runners are slower than a desk, and a
  sleep that is long enough locally isn't there. A sleep may only pace
  something (a burst window, a slow dial).
- **Read the exit status, not a pipe's.** `go test ./... | tail` reports
  `tail`'s status, so a failing run reads as passing. Write the output to
  a file and check `$?`, or check `${PIPESTATUS[0]}`.
- **Nothing leaves the machine.** Tests use loopback only; devices are
  the fakes in `internal/fakeatem`, `internal/fakehyperdeck`,
  `internal/fakevideohub`, `internal/fakeultimatte` and
  `internal/fakenet`.
- **What a terminal shows is snapshot-tested** in
  `cmd/bmdctl/testdata/terminal/`. A new view gets a golden. The rules
  for terminal output (72 columns, the header, the four colours) are in
  [docs/PORTING.md](docs/PORTING.md), "Terminal", with
  [docs/terminal-mock.html](docs/terminal-mock.html) as the picture.

## Hardware

No test, example, benchmark, generator or script may contact a real
switcher unless built with `-tags hardware`, and CI never builds with it.
A hardware test:

- reads its target from `BMDCTL_TEST_ATEM` and refuses anything other
  than `192.168.81.85` or `192.168.81.86`, the project's two bench
  switchers;
- only reads, unless `BMDCTL_TEST_WRITE=1` is also set;
- even then, never changes the video mode, clears stills, restores a
  profile or saves startup state.

The shipped binary isn't covered by the rule: it sends to whatever host
its user names. If you have run bmdctl against hardware (a model or a
command no one has tried), please say what happened in an issue; every
session is recorded in [docs/HARDWARE.md](docs/HARDWARE.md).

## Oracles

The oracle files (`internal/fixtures/oracle/*.json` and the
`testdata/oracle/` of `hyperdeck/`, `videohub/` and `ultimatte/`) record
what bmdwire itself does with thousands of generated inputs, and the Go
tests replay them. They are committed, so running the tests needs no
Python. Regenerating one needs bmdwire checked out at the tag its README
names (`v1.3.0`, `v1.4.0` or `v1.6.0`) beside this repository, as
`../bmdwire`; the scripts take its path. A change to a script, or a reason to think bmdwire's behaviour was
recorded wrong, is the only reason to regenerate.

## Adding a command

Commands come from the table in `internal/table`, and everything else is
generated from it: the parser, `--help`, completion,
docs/COMMANDS.md, the man page, the HTTP route and the MCP tool.

**An ATEM wire command** (one command packet). bmdwire must have it
first: a command or packet bmdwire doesn't have starts as a bmdwire pull
request, with its SURFACE.md entry, and is ported from there
(`CLAUDE.md`, "bmdwire first").

1. The command type, in the `atem/cmd_*.go` file for its SURFACE-atem.md
   table, mirroring atemwire's Send class: field names, masks, clamps and
   units as atemwire has them. Cite `bmdwire v1.3.0` where it matters.
2. Its spec in `commandSpecs` (`atem/table.go`): the path, a one-line
   summary, the command, the echo key it waits for, and each parameter
   mapped onto a field. Required arguments are positional, optional ones
   flags; indexes are `oneBased`.
3. One to three examples in `atem/table_examples.go` (`TestExamples`
   checks them).
4. A reader in `internal/engine/notes.go` (`readers`), if the state holds
   the value, so the result shows before → after and a write the switcher
   already holds sends nothing.
5. Cases in `internal/fixtures/oracle/gen_send.py`, then regenerate
   `send.json`.
6. `go generate ./cmd/bmdctl` for docs/COMMANDS.md and the group's page
   in docs/commands/.

A command whose first word is new needs a group in `atem/groups.go`
(`Groups`, `GroupOf`): that is its MCP tool. A new group also gets a
page in docs/commands/; list it in `mkdocs.yml`'s nav (`TestCommandsDoc`
says so if you don't).

**An operation** (reads state first, sends several commands, or moves a
file) is an entry with a function in the `internal/engine/ops_*.go` file
for its group, listed in `operations()` (`internal/engine/call.go`),
with examples in `internal/engine/examples.go`. Its terminal view goes
in `cmd/bmdctl/screen*.go`, with a golden.

**A HyperDeck command** is the same, hyperdeckwire first, in
`hyperdeck/table.go` (one line) or `internal/engine/hyperdeck.go` (reads
the deck first). **A Videohub or Ultimatte command**, videohubwire or
ultimattewire first, is always an operation, in
`internal/engine/videohub.go` or `internal/engine/ultimatte.go`, with its
views in `cmd/bmdctl/hub.go` or `keyer.go`.

`TestEveryCommandRuns` sends every entry to the fake switcher (and
`TestEveryDeckCommandRuns`, `TestEveryHubCommandRuns` and
`TestEveryUltCommandRuns` to the other fakes), so a new one has to work
there too; teach the fake (`internal/fakeatem/echoes.go`
or `mirror.go`) to answer it if needed. A command nobody has confirmed
on a switcher is marked `Unverified`, and says so when sent.

## Licences

The repository has two licences, and a file's directory decides which:

- **`atem/` is LGPL-3.0-only.** It is a port of atemwire, which is a
  fork of [pyatem](https://git.sr.ht/~martijnbraam/pyatem) by Martijn
  Braam and the OpenAtem contributors, and LGPL-3.0-only. A port is a
  derived work, so it keeps the licence: `atem/LICENSE`,
  `atem/LICENSE-gpl3.txt` and `atem/NOTICE` say so. Every file in `atem/`
  starts with the two copyright lines and
  `// SPDX-License-Identifier: LGPL-3.0-only`; copy them from
  `atem/doc.go`. `internal/fixtures/` holds data copied from atemwire's
  tests and is LGPL-3.0-only too.
- **Everything else is MIT** (`// SPDX-License-Identifier: MIT`): the CLI,
  `hyperdeck/`, `videohub/` and `ultimatte/` (ports of hyperdeckwire,
  videohubwire and ultimattewire, which are MIT, each with its test data
  in its `testdata/`) and the rest of `internal/`.

The split keeps the LGPL to the code that has to carry it. A program can
use the MIT packages under MIT terms, and the `atem` package under the
LGPL's. A contribution is under the licence of the directory it lands
in.

## Commits and pull requests

- One logical change per commit, with a
  [conventional commit](https://www.conventionalcommits.org/) message:
  `feat(atem): ...`, `fix(cli): ...`, `docs: ...`, `test(fixtures): ...`,
  `build(release): ...`, `ci: ...`. The release notes are grouped by
  these types, and `chore` is left out of them.
- `go build ./... && go vet ./... && go test -race ./...` is green at
  every commit, and CI is green on the pull request.
- A decision the code makes that bmdwire doesn't goes in
  docs/PORTING.md in the same commit, with the reason.
- The guide's samples (docs/guide/) are written by hand. If you change
  what a command prints, check the guide's sample of it; the quick
  start's (docs/quickstart.md) is checked against its terminal golden,
  `quickstart.golden`, and fails until you copy the new lines in.
- Releases are made by the maintainer, by pushing a `v*` tag.
