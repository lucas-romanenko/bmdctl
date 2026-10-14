# The engine

Every device command goes through one background process, the engine.
It holds a session to each device, so a command costs a round trip
rather than a connect and a full state dump, and it has the switcher's
state at hand to read names and rates against.

- **It starts itself.** The first command that needs it runs `bmdctl
  engine --background` and waits for it. It listens on a Unix socket,
  `$XDG_RUNTIME_DIR/bmdctl.sock` (else `bmdctl-<uid>/bmdctl.sock` in
  `$TMPDIR` or `/tmp`; `BMDCTL_SOCKET` overrides it), readable by your
  user alone. On Windows it is the named pipe `\\.\pipe\bmdctl-<user>`.
- **Sessions close after 10 minutes idle**, or an hour if pinned by
  `connect`. `BMDCTL_PIN_IDLE` sets the hour (`90m`, `2h`, or `0` for
  never), read when the engine starts: `bmdctl engine stop` after
  changing it. A `watch` or an MCP subscription counts as a command for
  as long as it runs.
- **It restarts after an upgrade.** Every request carries the build of
  the bmdctl that sent it. An engine a command started, asked by another
  build, stops, and the command starts one of its own build and sends
  again. An engine you started yourself answers every build.
- **Its log** is `~/.local/state/bmdctl/engine.log`, rotated at 5 MiB.
  `bmdctl engine --log-level debug` in the foreground shows the protocol.

```
$ bmdctl engine status
engine   running, pid 48213, up 2h14m3s
version  bmdctl v0.1.0 (e8eb8b9, 2026-10-11T19:11:25Z), protocol 1
build    MpUkle-BHtFbJgq0fFJH/soENu7Y-5cXGVrzdwhuM/KadqYJdIUmOfOSQQGaxu/1k1eVHazaCA2yhy1OyrP
socket   /run/user/1000/bmdctl.sock
log      /home/you/.local/state/bmdctl/engine.log
pins     close after 1h idle
session  192.168.1.20:9910  ready, session 2h04m, idle 12s, pinned, closes in 59m48s if idle  ATEM 1 M/E Constellation HD 1080p25
$ bmdctl engine stop
engine stopped
```

`engine stop` closes every session, saying goodbye to each switcher, and
waits for the socket to go. If the engine doesn't answer at all, `kill`
the pid `engine status` printed (or `pkill -f 'bmdctl engine'`); the
next command removes the socket it left and starts a new one. `bmdctl
doctor` checks the engine, its socket, hosts.yml and that each host
answers.

To run it as a service instead, run it in the foreground. A systemd user
unit, `~/.config/systemd/user/bmdctl.service`:

```ini
[Unit]
Description=bmdctl engine
After=network-online.target

[Service]
ExecStart=%h/.local/bin/bmdctl engine
Restart=on-failure

[Install]
WantedBy=default.target
```

`systemctl --user enable --now bmdctl` starts it; after upgrading bmdctl,
`systemctl --user restart bmdctl`. `bmdctl engine --serve` runs the HTTP
API in the same process (next section). [INSTALL.md](../INSTALL.md#docker)
covers the container.

## serve: the HTTP API

`bmdctl serve` serves every command as HTTP/JSON on `127.0.0.1:8880`,
through the engine. It has no authentication: whoever can reach it
controls the devices, so `--listen` to another address only on a network
you trust.

```
$ bmdctl serve
bmdctl serve  http://127.0.0.1:8880 · ctrl-c to stop
  no authentication: whoever reaches it controls the devices
  14:10:02  GET    /hosts                                     200 · 1 ms
  14:10:05  POST   /hosts/studio/program                     200 · 21 ms
```

```
$ curl -s localhost:8880/hosts
{"ok":true,"confirmed":false,"result":{"default":"STUDIO A","hosts":[{"name":"STUDIO A","address":"192.168.1.20","device":"atem"},{"name":"Deck 1","address":"192.168.1.50","device":"hyperdeck"}]},"took_ms":0}
$ curl -s -H 'Content-Type: application/json' -d '{"source": "cam3"}' localhost:8880/hosts/studio/program
{"ok":true,"confirmed":true,"result":{"args":{"me":"1","source":"3 (Camera 3)"},"command":"CPgI","echo":"PrgI:0","state":{"index":0,"source":3}},"took_ms":21}
```

- **A command is `POST /hosts/{host}/{its words}`**, its arguments a JSON
  object (`/hosts/studio/usk/on-air` with `{"keyer": 1, "on-air": true}`).
  The host is resolved as `--host` is: a name, the start of one, or an
  address. The body must be `application/json`.
- **Files** go as `multipart/form-data`: `curl -F slot=3 -F
  file=@sponsor.png localhost:8880/hosts/studio/media/upload`. A
  download's file comes back base64 in `files`.
- **Destructive commands** need `?yes=true`; without it the answer is
  409, with what it would affect in `confirm`.
- **The answer** is what `--json` prints, with an HTTP status for its
  code: 200, 400 usage, 409 destructive, 422 rejected, 504 not confirmed,
  502 unreachable.
- `GET /hosts/{host}/state` (`?path=mes.0`) reads the state;
  `GET /hosts/{host}/events` streams `watch --json`'s lines as
  server-sent events; `GET /openapi.json` describes every command.
