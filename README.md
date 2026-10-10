# bmdctl

A command-line tool, and the Go libraries under it, for controlling
Blackmagic Design production equipment over the network: ATEM switchers
first, then HyperDeck recorders, Videohub routers and Ultimatte keyers.

The libraries are a Go port of [bmdwire](https://github.com/lucas-romanenko/bmdwire)
v1.3.0, the Python package behind WebATEM. They speak the devices' own
protocols directly: no Blackmagic software, SDK or driver is involved.

## Status

Early. The ATEM package has its wire layer: the UDP transport, a session
that tracks the switcher's state, every state packet atemwire parses and
every command it can send. The CLI sends any of those commands to the
switcher you name, or prints the bytes it would send. Stills and macro
transfers, profiles and the other devices come later.

```
$ bmdctl atem --host 10.0.0.20 program --source 5
$ bmdctl atem --dry-run transition wipe --me 0 --width 50 --pattern CircleIris
001c0000435457700006000007001388000000000000000000000000
```

| package     | ports                           | licence        |
|-------------|---------------------------------|----------------|
| `atem`      | `atemwire` (UDP 9910)           | LGPL-3.0-only  |
| `hyperdeck` | `hyperdeckwire` (TCP 9993)      | MIT            |
| `videohub`  | `videohubwire` (TCP 9990)       | MIT            |
| `ultimatte` | `ultimattewire`                 | MIT            |
| `cmd/bmdctl`| the CLI                         | MIT            |

`atem` is LGPL because atemwire is: it is a fork of
[pyatem](https://git.sr.ht/~martijnbraam/pyatem) by Martijn Braam and the
OpenAtem contributors. See `atem/NOTICE`.

## What it is not

- No GUI and no TUI. bmdctl is a command you run, or a library you import.
- No scheduling. It does not run things at a time or on a cue; use cron, a
  show-control system or your own code for that.
- No telemetry. It sends nothing anywhere except to the devices you point
  it at.

## Building

```
go build ./...
go vet ./...
go test ./...
go run ./cmd/bmdctl version
```

Go 1.27. The device packages use only the standard library.

## Documentation

- `docs/SURFACE-atem.md`: everything atemwire can do, copied from bmdwire
  v1.3.0. It is the specification for the `atem` package.
- `docs/PORTING.md`: every decision the port makes, and every place it
  differs from bmdwire.
- `docs/COMMANDS.md`: every command the CLI knows, generated from the
  command table.

## Not affiliated with Blackmagic Design

bmdctl is an independent project. It is not affiliated with, endorsed by or
supported by Blackmagic Design Pty Ltd. ATEM, HyperDeck, Ultimatte and
Videohub are trademarks of Blackmagic Design.
