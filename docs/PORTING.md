# Porting bmdwire to Go: decisions

This file is the decision log for the port. Each section says what was
decided and why, so a later session can follow it instead of re-deriving
it. When you make a new decision, add it here in the same commit.

## Source of truth

- The port follows **bmdwire v1.3.0** (`github.com/lucas-romanenko/bmdwire`,
  tag `v1.3.0`, commit `cea58c2`). Cite that tag wherever the port refers
  to bmdwire, in comments and docs alike, so a reader knows which version a
  claim was checked against.
- `docs/SURFACE-atem.md` is atemwire's own inventory, copied verbatim with a
  header naming the tag. It is the specification for `atem/`. Its body says
  it describes 1.2.0; every fix it marks as made after 1.2.0 (§5.4) is in
  v1.3.0, so it describes v1.3.0 apart from those marks.
- Where SURFACE.md and the Python code disagree, the code at v1.3.0 wins,
  as SURFACE.md itself says. Each disagreement found during the port is
  listed under "SURFACE.md and the code" at the end of this file.
- **Mirror, don't redesign.** Behaviour, masks, clamps, units and quirks
  follow atemwire, including its known traps (SURFACE.md §5.4), unless a
  section below says otherwise. A deliberate difference is recorded here
  with its reason; an undocumented difference is a bug.

## Repository rules

- Module `github.com/lucas-romanenko/bmdctl`, Go 1.27.
- Device packages (`atem`, `hyperdeck`, `videohub`, `ultimatte`) use the
  standard library only. `golang.org/x/image` is allowed later for image
  fitting (still upload) and nowhere else.
- One commit per step of the plan, conventional commit messages, pushed
  after every commit. `go build ./... && go vet ./... && go test ./...` is
  green at every commit.
- The hardware rule (see `CLAUDE.md`): nothing contacts a real switcher
  unless built with `-tags hardware`; hardware tests take the target from
  `BMDCTL_TEST_ATEM`, accept only `192.168.81.85` and `192.168.81.86`, are
  read-only unless `BMDCTL_TEST_WRITE=1`, and never change the video mode,
  clear stills, restore a profile or save startup state. This phase has no
  hardware tests: everything runs against `internal/fixtures` or
  `internal/fakeatem`.
- Out of scope until asked: transfers (stills, macros), the macro bytecode
  codec, profiles, a background engine, hosts, HTTP, MCP, and the HyperDeck,
  Videohub and Ultimatte packages beyond their `doc.go`.

## Licensing

- `atem/` is a port of atemwire, which is LGPL-3.0-only (a fork of pyatem),
  so it is LGPL-3.0-only too. `atem/LICENSE` and `atem/LICENSE-gpl3.txt`
  are copied from bmdwire; `atem/NOTICE` gives the lineage. Every file in
  `atem/` starts with both copyright lines and the SPDX identifier.
- `internal/fixtures/` holds bytes and files copied from atemwire's test
  suite, so it carries atemwire's licence as well.
- Everything else (the CLI, the other device packages, `internal/table`,
  `internal/fakeatem`) is MIT, as bmdwire's non-ATEM packages are.
