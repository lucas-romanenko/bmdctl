## What and why

<!-- What this changes, and the issue it answers. -->

## Checklist

- [ ] `go build ./... && go vet ./... && go test -race ./...` passes
- [ ] Commit messages are conventional (`feat(atem): ...`, `fix(cli): ...`, `docs: ...`)
- [ ] A new decision, or a difference from bmdwire v1.3.0, is in docs/PORTING.md
- [ ] After a command table change: `go test ./cmd/bmdctl -run TestCommandsDoc -update`
- [ ] After an output change: `go test ./cmd/bmdctl -run TestTerminal -update`, diff read, docs/GUIDE.md checked
- [ ] New files carry the licence header for their directory (`atem/`: LGPL-3.0-only, elsewhere MIT)
- [ ] No test contacts a real device unless built with `-tags hardware`

## Hardware

<!-- Run against a real device? Model, firmware, video mode, what you ran
and what happened. "No" is a fine answer. -->
