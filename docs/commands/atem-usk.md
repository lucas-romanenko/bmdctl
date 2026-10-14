# atem: usk

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Upstream keyers: fill and key, type (luma, chroma, pattern, DVE) and its settings, mask, fly keyframes, on air.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem usk <keyer> chroma`

Set an advanced chroma key.

```
bmdctl usk 1 chroma --foreground 0.6 --background 0.4
bmdctl usk 1 chroma --spill 0.5 --flare 0.3
bmdctl usk 2 chroma --key-edge 0.5 --me 2
```

Give at least one flag. Echo: `KACk:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--foreground` | float, 0-1, ×1000 on the wire, not clamped | optional | foreground level |
| `--background` | float, 0-1, ×1000 on the wire, not clamped | optional | background level |
| `--key-edge` | float, 0-1, ×1000 on the wire, not clamped | optional | key edge |
| `--spill` | float, 0-1, ×1000 on the wire, not clamped | optional | spill suppression |
| `--flare` | float, 0-1, ×1000 on the wire, not clamped | optional | flare suppression |
| `--brightness` | float, about -1 to 1, ×1000 on the wire, not clamped | optional | brightness |
| `--contrast` | float, about -1 to 1, ×1000 on the wire, not clamped | optional | contrast |
| `--saturation` | float, 0-2, ×1000 on the wire, not clamped | optional | saturation |
| `--red` | float, about -1 to 1, ×1000 on the wire, not clamped | optional | red |
| `--green` | float, about -1 to 1, ×1000 on the wire, not clamped | optional | green |
| `--blue` | float, about -1 to 1, ×1000 on the wire, not clamped | optional | blue |

## `bmdctl atem usk <keyer> chroma-sample`

Set an advanced chroma key's sample cursor and colour.

```
bmdctl usk 1 chroma-sample --cursor --x 0.2 --y 0.5
bmdctl usk 1 chroma-sample --preview
bmdctl usk 1 chroma-sample --cursor off --preview off
```

Give at least one flag. Echo: `KACC:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--cursor` | bool | optional | show the sample cursor |
| `--preview` | bool | optional | preview the key |
| `--x` | float, 0-1 | optional | cursor x |
| `--y` | float, 0-1 | optional | cursor y |
| `--size` | float, 0-1, clamped to 0.062-0.9925 | optional | cursor size |
| `--sample-y` | float, 0-1 | optional | sampled luma (for restoring a key) |
| `--sample-cb` | float, -1 to 1 | optional | sampled Cb |
| `--sample-cr` | float, -1 to 1 | optional | sampled Cr |

## `bmdctl atem usk <keyer> dve`

Set a DVE (or fly) key.

```
bmdctl usk 1 dve --size-x 0.4 --size-y 0.4
bmdctl usk 1 dve --position-x 8 --position-y 4.5
bmdctl usk 1 dve --border --shadow --rate 0:20
```

Give at least one flag. Echo: `KeDV:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--size-x` | float, 0-2, clamped | optional | horizontal size |
| `--size-y` | float, 0-2, clamped | optional | vertical size |
| `--position-x` | float, display units (×1000) | optional | horizontal position |
| `--position-y` | float, display units (×1000) | optional | vertical position |
| `--rotation` | float, degrees | optional | rotation |
| `--border` | bool | optional | border on |
| `--shadow` | bool | optional | drop shadow on |
| `--bevel` | uint, 0-255 | optional | bevel type (no atemwire operation sets it) |
| `--outer-width` | float, ×100 on the wire | optional | outer border width |
| `--inner-width` | float, ×100 on the wire | optional | inner border width |
| `--outer-softness` | float, 0-100 | optional | outer border softness |
| `--inner-softness` | float, 0-100 | optional | inner border softness |
| `--bevel-softness` | float, 0-100 | optional | bevel softness |
| `--bevel-position` | float, 0-100 | optional | bevel position |
| `--opacity` | float, 0-100 | optional | border opacity |
| `--hue` | float, degrees, ×10 on the wire | optional | border hue |
| `--saturation` | float, ×10 on the wire | optional | border saturation |
| `--luma` | float, ×10 on the wire | optional | border luma |
| `--light-direction` | float, degrees | optional | light source direction |
| `--light-altitude` | float, 10-100 | optional | light source altitude |
| `--mask` | bool | optional | mask on |
| `--mask-top` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | mask top |
| `--mask-bottom` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | mask bottom |
| `--mask-left` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | mask left |
| `--mask-right` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | mask right |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | optional | fly rate |

## `bmdctl atem usk <keyer> fill <source>`

Set an upstream keyer's fill source.

```
bmdctl usk 1 fill cam3
bmdctl usk 2 fill mp1 --me 2
```

Echo: `KeBP:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem usk <keyer> key <source>`

Set an upstream keyer's key (cut) source.

```
bmdctl usk 1 key mp1k
bmdctl usk 2 key cam4 --me 2
```

Echo: `KeBP:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem usk <keyer> keyframe-run <to>`

Fly the key to a keyframe.

```
bmdctl usk 1 keyframe-run a
bmdctl usk 1 keyframe-run full
bmdctl usk 1 keyframe-run runToInfinite --direction MiddleCentre
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<to>` | enum, 0-255: a=1 b=2 full=3 runToInfinite=4 | required | keyframe to fly to |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--direction` | enum, 0-255: CentreOfKey=0 TopLeft=1 TopCentre=2 TopRight=3 MiddleLeft=4 MiddleCentre=5 MiddleRight=6 BottomLeft=7 BottomCentre=8 BottomRight=9 | optional | where runToInfinite flies the key: CentreOfKey, or the 3x3 grid row by row |

## `bmdctl atem usk <keyer> keyframe-set <keyframe>`

Store the keyer's DVE as a fly keyframe.

```
bmdctl usk 1 keyframe-set a
bmdctl usk 1 keyframe-set b --me 2
```

Echo: `KKFP:<me>:<keyer>:<keyframe> (A and B), KeFS:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<keyframe>` | enum, 0-255: a=1 b=2 full=3 runToInfinite=4 | required | keyframe |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem usk <keyer> luma`

Set a luma key.

```
bmdctl usk 1 luma --clip 40 --gain 60
bmdctl usk 1 luma --premultiplied
bmdctl usk 2 luma --invert --me 2
```

Give at least one flag. Echo: `KeLm:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, clamped to 0-100 | optional | clip |
| `--gain` | float, %, clamped to 0-100 | optional | gain |
| `--invert` | bool | optional | invert the key |

## `bmdctl atem usk <keyer> mask`

Set an upstream keyer's mask.

```
bmdctl usk 1 mask --enabled --left -8 --right 8
bmdctl usk 1 mask --enabled off
```

**Unverified.** Give at least one flag. Echo: `KeBP:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--enabled` | bool | optional | mask on |
| `--top` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | top edge |
| `--bottom` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | bottom edge |
| `--left` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | left edge |
| `--right` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | right edge |

## `bmdctl atem usk <keyer> on-air <on-air>`

Put an upstream keyer on or off air.

```
bmdctl usk 1 on-air on
bmdctl usk 2 on-air off --me 2
```

Echo: `KeOn:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<on-air>` | bool | required | on air |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem usk <keyer> pattern`

Set a pattern key.

```
bmdctl usk 1 pattern --pattern CircleIris --size 40
bmdctl usk 1 pattern --softness 20 --position-x 0.75
```

Give at least one flag. Echo: `KePt:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--pattern` | enum, 0-255: LeftToRightBar=0 TopToBottomBar=1 HorizontalBarnDoor=2 VerticalBarnDoor=3 CornersInFourBox=4 RectangleIris=5 DiamondIris=6 CircleIris=7 TopLeftBox=8 TopRightBox=9 BottomRightBox=10 BottomLeftBox=11 TopCentreBox=12 RightCentreBox=13 BottomCentreBox=14 LeftCentreBox=15 TopLeftDiagonal=16 TopRightDiagonal=17 | optional | pattern |
| `--size` | float, %, clamped to 0-100 | optional | size |
| `--symmetry` | float, %, clamped to 0-100 | optional | symmetry |
| `--softness` | float, %, clamped to 0-100 | optional | softness |
| `--position-x` | float, 0-1, clamped | optional | horizontal position |
| `--position-y` | float, 0-1, clamped | optional | vertical position |
| `--invert` | bool | optional | invert the pattern |

## `bmdctl atem usk <keyer> toggle`

Put an upstream keyer on air if it is off, or off if it is on.

```
bmdctl usk 1 toggle
bmdctl usk 2 toggle --me 2
```

Echo: `KeOn:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem usk <keyer> type`

Set an upstream keyer's type and fly mode.

```
bmdctl usk 1 type --type chroma
bmdctl usk 1 type --type dve --fly
```

Give at least one flag. Echo: `KeBP:<me>:<keyer>`.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--type` | enum, 0-255: luma=0 chroma=1 pattern=2 dve=3 | optional | key type |
| `--fly` | bool | optional | fly key |
