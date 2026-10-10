# bmdctl: notes for future sessions

bmdctl is a Go port of bmdwire v1.3.0 (`../bmdwire`, checked out at tag
`v1.3.0`, commit `cea58c2`) plus one CLI binary. Read `docs/PORTING.md`
before changing anything in `atem/`: it records every decision already
made, so you follow it instead of re-deriving it. Add to it whenever you
make a new one.

## Build and test

```
go build ./... && go vet ./... && go test ./...
```

All three must be green after every commit. Nothing in the default build
or test run touches the network beyond loopback.

`docs/COMMANDS.md` is generated from the command table; after changing a
table entry, regenerate it with
`go test ./cmd/bmdctl -run TestCommandsDoc -update`. The oracles in
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
  codec, profiles and the display-unit readers. A background engine,
  hosts, HTTP, MCP, CLI commands beyond sending one command, and the other
  devices are later phases. Don't start them unless asked.

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
- `cmd/bmdctl/`: the CLI.
