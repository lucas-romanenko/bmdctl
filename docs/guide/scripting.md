# Scripting

When stdout isn't a terminal, each command prints one plain line, the
same from one release to the next. `--json` prints the engine's
response, `--quiet` nothing, and the exit status says what happened:

```
$ bmdctl program cam1 | cat
STUDIO A (192.168.1.20)  program  ME1            CAM2 → CAM1  confirmed    21 ms
$ bmdctl --json program cam2
{"ok":true,"confirmed":true,"result":{"args":{"me":"1","source":"2 (Camera 2)"},"command":"CPgI","echo":"PrgI:0","state":{"index":0,"source":2}},"took_ms":21}
$ bmdctl state --path mes.0.program.source
2
$ bmdctl --quiet media clear 3; echo $?
5
```

| Exit | Meaning | `--json` code |
|---|---|---|
| 0 | done | |
| 1 | usage error, or any other failure | `usage` |
| 2 | the switcher or the engine didn't answer | `unreachable`, `engine`, `stale` |
| 3 | sent, but not confirmed in time | `no_echo` |
| 4 | refused by the switcher (a transfer, a lock), or a profile restore with errors | `rejected` |
| 5 | destructive, and `--yes` wasn't given | `destructive` |

`state` prints the switcher's whole state; `--path` narrows it
(`mes.0.program`), and a path to one value prints the value alone.
State paths count from 0, as `watch`'s keys do; everything you type
elsewhere counts from 1. A loop over every host:

```sh
bmdctl hosts --json | jq -r '.result.hosts[] | select(.device == "atem") | .name' |
  while read -r h; do bmdctl --host "$h" profile save "$h.xml"; done
```

From Bitfocus Companion (its shell action), Node-RED (an exec node) or
Home Assistant (`shell_command`), call `bmdctl` the same way, or POST to
`bmdctl serve`.

## --dry-run

`--dry-run` prints the bytes a command would send, header included, and
sends nothing. It needs no switcher, so source names are limited to
numbers and the aliases (`cam2`, `mp1`, `bars`):

```
$ bmdctl --dry-run program cam2
000c00004350674900000002
$ bmdctl --dry-run transition wipe --width 50 --pattern CircleIris
001c0000435457700006000007001388000000000000000000000000
```

`000c` is the command's length (12 bytes), `0000` reserved, `43506749`
the code `CPgI`, `00` M/E 1 (the wire counts from 0), `00` padding and
`0002` source 2. Rates are counted at 25 fps unless `--fps` says
otherwise. Commands that read the switcher first (`status`, `undo`,
transfers) have no bytes to print.
