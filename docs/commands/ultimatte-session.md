# ultimatte: session

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Reads: status (label, presets, network), the presets, the control values, the prelude, connect.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl ultimatte connect [address]`

Check that an Ultimatte answers; an address is saved as the default host.

```
bmdctl connect 192.168.81.97
bmdctl connect
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (saved to hosts.yml by the keyer's label, as the default) or a host's name; else the default |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

## `bmdctl ultimatte controls`

List the keyer's control values: raw, and in the units Smart Remote shows them.

```
bmdctl controls
bmdctl controls --defaults
```

| argument | value | | |
|---|---|---|---|
| `--defaults` | bool | default false | the defaults (CONTROL DEFAULT), not the live values |

## `bmdctl ultimatte prelude`

Print what the keyer sends on connect, as it sends it: its label, controls and presets.

```
bmdctl prelude
```

## `bmdctl ultimatte presets`

List the keyer's saved presets, as its FILE LIST names them.

```
bmdctl presets
```

## `bmdctl ultimatte status`

Show the keyer: its label, its presets and its network interface.

```
bmdctl status
```
