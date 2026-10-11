<img src=".github/banner.gif" alt="bmdctl" width="100%">

# bmdctl

A command-line tool, and the Go libraries under it, for controlling
Blackmagic Design production equipment over the network: ATEM switchers
first, then HyperDeck recorders, Videohub routers and Ultimatte keyers.

The libraries are a Go port of [bmdwire](https://github.com/lucas-romanenko/bmdwire)
v1.3.0, the Python package behind WebATEM. They speak the devices' own
protocols directly: no Blackmagic software, SDK or driver is involved.

## Status

Early. Its reads have run against one switcher (docs/HARDWARE.md); its
writes haven't yet. The ATEM package has its wire
layer: the UDP transport, a session that tracks the switcher's state,
every state packet atemwire parses and every command it can send. It
also moves stills and macros to and from the switcher, encodes and
decodes macros, and saves and applies ASC-style profiles. The CLI runs
everything through a background engine that keeps a session open to
each switcher; the first command starts it. The other devices come
later.

```
$ bmdctl hosts add studio 10.0.0.20
$ bmdctl hosts default studio
$ bmdctl program "Camera 2"
program                  confirmed    14 ms  me 1, source 2 (Camera 2)
$ bmdctl status
$ bmdctl usk 1 on-air on --me 2
$ bmdctl media upload 1 logo.png --name Logo
$ bmdctl macro run "Open Show"
$ bmdctl exec opening-cue.txt
$ bmdctl --dry-run transition wipe --width 50 --pattern CircleIris
001c0000435457700006000007001388000000000000000000000000
```

`docs/COMMANDS.md` lists every command and the CLI's conventions; `bmdctl
man` prints the same as a manual page, `bmdctl completion bash|zsh|fish`
a completion script, and `bmdctl doctor` checks the setup.

### The engine under systemd or Docker

Any command starts the engine in the background, and restarts it after
bmdctl is rebuilt or upgraded. To run it yourself, `bmdctl engine` runs
it in the foreground and logs to stderr; that one answers whichever
build of bmdctl sends to it, and is restarted by you. In a
container, put the socket on a volume the host shares (a switcher is
reached over UDP, so use the host's network):

```
docker run --network host -v /run/bmdctl:/run/bmdctl \
    -e BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock <image> engine
BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock bmdctl status
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
- `docs/HARDWARE.md`: every session against a real switcher, and what
  it found.
- `docs/BRAND.md`: the mark, wordmarks, colours and banner.

## Not affiliated with Blackmagic Design

Not affiliated with or endorsed by Blackmagic Design Pty Ltd.

bmdctl is an independent project, and Blackmagic Design does not support
it. ATEM, HyperDeck, Ultimatte and Videohub are trademarks of Blackmagic
Design.
