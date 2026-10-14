# atem: session

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The session and reads: connect, disconnect, status (what is on air), show one area, the raw state, watch changes.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem connect [address]`

Connect to a switcher and keep the session open; an address is asked what it is and saved as the default host.

```
bmdctl connect 192.168.10.240
bmdctl connect
bmdctl connect --pin off
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (asked what answers there, then saved to hosts.yml by the name it gives itself, as the default) or a host's name; else the default |
| `--pin` | bool | default true | keep the session open until disconnect or an hour without a command ($BMDCTL_PIN_IDLE); false lets it close after 10 idle minutes |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

## `bmdctl atem disconnect`

Close the engine's session to the switcher, pinned or not.

```
bmdctl disconnect
```

## `bmdctl atem show <area>`

Show one area of the switcher: keys, dsk, transition, audio, media, macros, aux or hyperdecks.

```
bmdctl show keys
bmdctl show transition
bmdctl show audio
```

| argument | value | | |
|---|---|---|---|
| `<area>` | enum: keys=0 dsk=1 transition=2 audio=3 media=4 macros=5 aux=6 hyperdecks=7 | required | keys, dsk, transition, audio, media, macros, aux or hyperdecks |
| `--all` | bool | default false | media and macros: the empty slots too, at a terminal |

## `bmdctl atem state`

Print the switcher's state as the session holds it, or one part of it.

```
bmdctl state
bmdctl state --path mes.0.program
bmdctl state --path fairlight.master
```

| argument | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: mes.0.program, inputs.1.name, fairlight.master |

## `bmdctl atem status`

Show what is on air: program, preview, transitions, keyers, macros.

```
bmdctl status
```

## `bmdctl atem watch`

Print each change the switcher reports, as it comes.

```
bmdctl watch
bmdctl watch --filter PrgI,PrvI
bmdctl watch --filter PrgI --count 1
```

Runs until interrupted.

| argument | value | | |
|---|---|---|---|
| `--filter` | list | optional | only keys starting with one of these: PrgI,KeOn:0 |
| `--count` | uint | optional | stop after this many changes |
| `--all` | bool | default false | include the clock's Time packets and the index and Fairlight tallies, left out unless --filter names them |
