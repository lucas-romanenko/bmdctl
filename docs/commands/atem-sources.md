# atem: sources

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Inputs and sources: the list, renames, colour generators, camera control, HyperDeck slots.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem camera <destination> <category> <parameter>`

Send one Blackmagic camera control parameter.

```
bmdctl camera 1 0 3 --type fixed16 --data 0.5
bmdctl camera 2 1 2 --type int16 --data 5600,0
bmdctl camera 1 0 0 --relative --type fixed16 --data 0.05
```

**Unverified.** Echo: `CCdP:<destination>:<category>:<parameter>`.

| argument | value | | |
|---|---|---|---|
| `<destination>` | uint, 0-255 | required | camera; 255 for all |
| `<category>` | uint, 0-255 | required | category |
| `<parameter>` | uint, 0-255 | required | parameter |
| `--relative` | bool | default false | add to the current value |
| `--type` | enum, 0-255: bool=0 int8=1 int16=2 int32=3 int64=4 string=5 fixed16=128 | default bool | data type |
| `--data` | list | optional | comma-separated values of the data type |

## `bmdctl atem color <generator>`

Set a colour generator.

```
bmdctl color 1 --hue 210 --saturation 80 --luma 40
bmdctl color 2 --sat 0 --luma 100
```

Give at least one flag. Echo: `ColV:<generator>`.

| argument | value | | |
|---|---|---|---|
| `<generator>` | uint, 1-2, counted from 1 | required | colour generator |
| `--hue` | float, degrees, not clamped | optional | hue |
| `--saturation` | float, %, clamped to 0-100 | optional | saturation |
| `--luma` | float, %, clamped to 0-100 | optional | luma |

## `bmdctl atem hyperdeck-slot <slot>`

Bind a HyperDeck slot to an address and an input.

```
bmdctl hyperdeck-slot 1 --address 192.168.10.50 --input cam4
bmdctl hyperdeck-slot 2 --address 0.0.0.0
```

Give at least one flag. Echo: `RXMS:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-10, counted from 1 | required | HyperDeck slot |
| `--address` | string | optional | deck IPv4 address; 0.0.0.0 clears the slot |
| `--input` | source, 0-65535 | optional | switcher input the deck feeds |

## `bmdctl atem input rename <source>`

Rename an input.

```
bmdctl input rename cam1 --long-name "Stage Left" --short-name STGL
bmdctl input rename 2 --short-name WIDE
```

Give at least one flag. Echo: `InPr:<source>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--long-name` | string, up to 20 bytes | optional | long name |
| `--short-name` | string, up to 4 bytes | optional | short name |
| `--port-type` | uint, 0-65535 | optional | external port type (no atemwire operation sets it) |

## `bmdctl atem inputs`

List the switcher's sources, one per line: id, short name, long name and port type.

```
bmdctl inputs
bmdctl inputs --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | at a terminal, the outputs, key masks and media player keys too (a pipe and --json always have them) |
