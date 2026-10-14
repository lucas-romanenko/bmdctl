# watch

`watch` prints what changes on the switcher, from any panel or program,
until Ctrl-C:

```
$ bmdctl watch
STUDIO A  PGM CAM2 · PVW CAM3 · watching · ctrl-c to stop
  14:02:13  preview     CAM3 → CAM4
  14:02:13  tally       CAM4 pvw, CAM3 off
  14:02:15  program     CAM2 → CAM1
  14:02:15  tally       CAM1 pgm, CAM2 off
  14:02:21  transition  mix  running 0.0s
  14:02:22  transition  mix  done 1.0s
  14:02:30  key 1       on air
  14:02:41  dsk 1       on air
  14:03:05  session dropped
  14:03:11  reconnected
```

`--filter` keeps changes whose key starts with one of its prefixes
(`--filter PrgI,PrvI`), `--count 5` stops after five, and `--all` adds
the clock ticks and duplicate tallies it leaves out.

`watch --json` prints one JSON line per change, for scripts. The first
line says watching has begun (from then on nothing is missed); each
change after it names the state packet, its key and its value. Keys are
the packet's code and indexes counted from 0, as the switcher sends
them: `PrgI:0` is M/E 1's program, `KeOn:0:1` M/E 1's keyer 2.

```
$ bmdctl watch --json --filter PrgI,PrvI
{"ok":true,"confirmed":false,"result":{"kind":"watching","time":"14:02:11.384"},"took_ms":2}
{"ok":true,"confirmed":false,"result":{"kind":"state","opcode":"PrvI","key":"PrvI:0","value":{"index":0,"source":4,"in_program":false},"time":"14:02:13.020"},"took_ms":1638}
{"ok":true,"confirmed":false,"result":{"kind":"state","opcode":"PrgI","key":"PrgI:0","value":{"index":0,"source":1},"time":"14:02:15.020"},"took_ms":3638}
```

A `disconnected` frame and then a `ready` one mean the session dropped
and came back. Tally lights can follow program and preview from `TlSr`,
the switcher's tally packet. For example, aux 2 following program:

```sh
bmdctl watch --json --filter PrgI:0 |
  jq --unbuffered -r 'select(.result.kind == "state") | .result.value.source' |
  while read -r src; do bmdctl aux 2 "$src"; done
```
