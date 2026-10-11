<img src=".github/banner.gif" alt="bmdctl" width="100%">

# bmdctl

A command-line tool, and the Go libraries under it, for controlling
Blackmagic Design production equipment over the network: ATEM switchers
first, then HyperDeck recorders, Videohub routers and Ultimatte keyers.

The libraries are a Go port of [bmdwire](https://github.com/lucas-romanenko/bmdwire)
v1.3.0, the Python package behind WebATEM. They speak the devices' own
protocols directly: no Blackmagic software, SDK or driver is involved.

## Install

macOS and Linux, into `~/.local/bin` (`/usr/local/bin` as root), with the
alias commands beside it:

```sh
curl -fsSL https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/install.sh | sh
```

It downloads the release for this machine from GitHub, checks it
against the release's `checksums.txt` and prints the next step
(`bmdctl connect <ip>`). `BMDCTL_VERSION=v0.1.0` picks a release,
`BMDCTL_INSTALL_DIR` the directory.

- **Windows** ([Scoop](https://scoop.sh)):
  `scoop install https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/scoop/bmdctl.json`
  installs the latest release (Scoop lists it as `nightly`, since the
  manifest follows the latest release rather than pinning one);
  `scoop update bmdctl` fetches it again.
- **Debian, Ubuntu, Fedora:** the `.deb` and `.rpm` on the
  [releases page](https://github.com/lucas-romanenko/bmdctl/releases)
  (`sudo apt install ./bmdctl_*.deb`), with the man page and completions.
- **Docker:** `ghcr.io/lucas-romanenko/bmdctl`, which runs the engine
  with the HTTP API (below).
- **Go:** `go install github.com/lucas-romanenko/bmdctl/cmd/bmdctl@latest`.

`atemctl`, `hdctl`, `vhctl` and `ultctl` are bmdctl for one device
(`hdctl play` is `bmdctl hyperdeck play`); the archives and packages
ship them as links to bmdctl, copies on Windows.

## Status

Early. It has run against one switcher, an ATEM 1 M/E Constellation HD,
in eight sessions (docs/HARDWARE.md): reads, switching, stills, macros
and profile save. The ATEM
package has its wire layer: the UDP transport, a session that tracks the
switcher's state, every state packet atemwire parses and every command
it can send. It also moves stills and macros to and from the switcher,
encodes and decodes macros, and saves and applies ASC-style profiles.
The HyperDeck package speaks the HyperDeck Ethernet Protocol (TCP 9993):
transport, clips, slots and the deck's notifications, checked against
hyperdeckwire itself but not yet run against a deck. The CLI runs
everything through a background engine that keeps a session open to
each device; the first command starts it. Videohub and Ultimatte come
later.

```
$ bmdctl connect 192.168.1.20         # asks what it is; saved by its own name, the default
$ bmdctl status
$ bmdctl program cam2
$ bmdctl usk 1 on-air on --me 2
$ bmdctl media upload 1 logo.png --name Logo
$ bmdctl macro run "Open Show"
$ bmdctl exec opening-cue.txt
$ bmdctl --dry-run transition wipe --width 50 --pattern CircleIris
001c0000435457700006000007001388000000000000000000000000
$ hdctl --host "Deck 1" goto intro
$ hdctl --host "Deck 1" play --loop --single-clip
```

`docs/COMMANDS.md` lists every command and the CLI's conventions; `bmdctl
man` prints the same as a manual page, `bmdctl completion bash|zsh|fish`
a completion script, and `bmdctl doctor` checks the setup.

### The engine under systemd or Docker

Any command starts the engine in the background, and restarts it after
bmdctl is rebuilt or upgraded. To run it yourself, `bmdctl engine` runs
it in the foreground and logs to stderr; that one answers whichever
build of bmdctl sends to it, and is restarted by you. The image runs
`bmdctl engine --serve`: the engine and the HTTP API. A switcher is
reached over UDP on the local network, so give it the host's network;
the API is then on the host's 127.0.0.1:8880, and its socket on a volume
the host's bmdctl can use:

```
docker run -d --network host -v bmdctl:/data -v /run/bmdctl:/run/bmdctl \
    ghcr.io/lucas-romanenko/bmdctl
BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock bmdctl status
curl -s localhost:8880/hosts
```

hosts.yml and the log are in the `/data` volume.

### HTTP

`bmdctl serve` serves every command over HTTP/JSON, through the engine:
`POST /hosts/studio/program` with `{"source": "cam2"}`, `GET
/hosts/studio/state`, `GET /hosts/studio/events` (server-sent events), and
`GET /openapi.json`, generated from the command table. It listens on
127.0.0.1:8880 and has no authentication, as WebATEM: whoever can reach it
controls the devices. `bmdctl engine --serve` serves it from the engine's
own process.

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
go test -race ./...
go run ./cmd/bmdctl version
```

Go 1.27. The device packages use only the standard library. Releases are
GoReleaser's (`.goreleaser.yml`): pushing a `v*` tag runs
`.github/workflows/release.yml`, which builds the archives, packages,
image and checksums (it commits nothing: `scoop/bmdctl.json` always
names the latest release's archive);
`goreleaser release --snapshot --clean` builds them all locally into
`dist/` without publishing.

## Documentation

- `docs/SURFACE-atem.md`: everything atemwire can do, copied from bmdwire
  v1.3.0. It is the specification for the `atem` package.
- `docs/SURFACE-hyperdeck.md`: everything hyperdeckwire can do, written
  from bmdwire v1.3.0's code. It is the specification for the
  `hyperdeck` package.
- `docs/PORTING.md`: every decision the port makes, and every place it
  differs from bmdwire.
- `docs/COMMANDS.md`: every command the CLI knows, generated from the
  command table.
- `docs/AGENTS.md`: bmdctl for AI agents: the MCP server (`bmdctl mcp`)
  and how to add it to Claude Code and Claude Desktop.
- `docs/HARDWARE.md`: every session against a real switcher, and what
  it found.
- `docs/BRAND.md`: the mark, wordmarks, colours and banner.

## Not affiliated with Blackmagic Design

Not affiliated with or endorsed by Blackmagic Design Pty Ltd.

bmdctl is an independent project, and Blackmagic Design does not support
it. ATEM, HyperDeck, Ultimatte and Videohub are trademarks of Blackmagic
Design.
