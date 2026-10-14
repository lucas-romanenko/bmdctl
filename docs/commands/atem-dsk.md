# atem: dsk

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Downstream keyers: fill and key, gain, mask, rate, tie, on air, auto.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem dsk auto`

Run a downstream keyer's auto transition.

```
bmdctl dsk auto
bmdctl dsk auto --dsk 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk fill <source>`

Set a downstream keyer's fill source.

```
bmdctl dsk fill mp1
bmdctl dsk fill mp2 --dsk 2
```

Echo: `DskB:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk gain`

Set a downstream keyer's clip, gain, pre-multiply and invert.

```
bmdctl dsk gain --premultiplied
bmdctl dsk gain --premultiplied off --clip 40 --gain 70
bmdctl dsk gain --invert --dsk 2
```

Give at least one flag. Echo: `DskP:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, clamped to 0-100 | optional | clip |
| `--gain` | float, %, clamped to 0-100 | optional | gain |
| `--invert` | bool | optional | invert the key |

## `bmdctl atem dsk key <source>`

Set a downstream keyer's key source.

```
bmdctl dsk key mp1k
bmdctl dsk key mp2k --dsk 2
```

Echo: `DskB:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk mask`

Set a downstream keyer's mask.

```
bmdctl dsk mask --enabled --top -4
bmdctl dsk mask --enabled off --dsk 2
```

Give at least one flag. Echo: `DskP:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |
| `--enabled` | bool | optional | mask on |
| `--top` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | top edge |
| `--bottom` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | bottom edge |
| `--left` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | left edge |
| `--right` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | right edge |

## `bmdctl atem dsk on-air <on-air>`

Put a downstream keyer on or off air.

```
bmdctl dsk on-air on
bmdctl dsk on-air off --dsk 2
```

Echo: `DskS:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `<on-air>` | bool | required | on air |
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk rate <rate>`

Set a downstream keyer's rate.

```
bmdctl dsk rate 1:00
bmdctl dsk rate 0:15 --dsk 2
```

Echo: `DskP:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `<rate>` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk tie <tie>`

Tie a downstream keyer to the next transition.

```
bmdctl dsk tie on
bmdctl dsk tie off --dsk 2
```

Echo: `DskP:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `<tie>` | bool | required | tie |
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

## `bmdctl atem dsk toggle`

Put a downstream keyer on air if it is off, or off if it is on.

```
bmdctl dsk toggle
bmdctl dsk toggle --dsk 2
```

Echo: `DskS:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |
