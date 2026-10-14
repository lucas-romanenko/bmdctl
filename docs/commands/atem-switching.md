# atem: switching

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

What is on program, preview and the aux outputs: program, preview, cut, auto, the T-bar, fade to black, undo.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem auto`

Run the M/E's transition with its current style and rate.

```
bmdctl auto
bmdctl auto --me 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem aux <aux> <source>`

Route a source to an aux output.

```
bmdctl aux 1 pgm
bmdctl aux 2 cam4
bmdctl aux 3 cf1
```

Echo: `AuxS:<aux>`.

| argument | value | | |
|---|---|---|---|
| `<aux>` | uint, 1-256, counted from 1 | required | aux output |
| `<source>` | source, 0-65535 | required | source id or input name |

## `bmdctl atem cut`

Cut program and preview.

```
bmdctl cut
bmdctl cut --me 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem ftb enable <enabled>`

Make fade to black available or not (always M/E 1; see docs/PORTING.md).

```
bmdctl ftb enable on
```

Echo: `FEna`.

| argument | value | | |
|---|---|---|---|
| `<enabled>` | bool | required | available |

## `bmdctl atem ftb rate <rate>`

Set the fade-to-black rate.

```
bmdctl ftb rate 1:00
```

Echo: `FtbP:<me>`.

| argument | value | | |
|---|---|---|---|
| `<rate>` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem ftb toggle`

Fade to black, or back.

```
bmdctl ftb toggle
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem preview <source>`

Put a source on an M/E's preview bus.

```
bmdctl preview cam3
bmdctl preview col1 --me 2
```

Echo: `PrvI:<me>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem program <source>`

Put a source on an M/E's program bus.

```
bmdctl program cam2
bmdctl program "Stage Left"
bmdctl program mp1 --me 2
```

Echo: `PrgI:<me>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem tbar <position>`

Move the T-bar.

```
bmdctl tbar 5000
bmdctl tbar 10000 --me 2
```

**Unverified.** Echo: `TrPs:<me>`.

| argument | value | | |
|---|---|---|---|
| `<position>` | int, 0-10000, clamped | required | T-bar position |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem undo`

Undo the last program, preview, cut, auto, aux, keyer or DSK on-air change made from this machine.

```
bmdctl undo
```
