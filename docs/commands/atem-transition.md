# atem: transition

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The next transition: its style and rate, each style's settings (mix, dip, wipe, DVE, stinger), what it takes, preview.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem transition dip`

Set the dip transition.

```
bmdctl transition dip --source col1 --rate 0:15
bmdctl transition dip --source black --me 2
```

Give at least one flag. Echo: `TDpP:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | optional | rate |
| `--source` | source, 0-65535 | optional | dip source |

## `bmdctl atem transition dve`

Set the DVE transition.

```
bmdctl transition dve --style PushLeft --rate 0:20
bmdctl transition dve --style SqueezeTopLeft --reverse --flip-flop
bmdctl transition dve --fill-source mp1 --key-source mp1k --key-enable
```

Give at least one flag. Echo: `TDvP:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--logo-rate` | uint, 0-255, frames | optional | graphic logo-wipe rate (no atemwire operation sets it) |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | optional | DVE rate |
| `--style` | enum, 0-255: SqueezeTopLeft=16 SqueezeTop=17 SqueezeTopRight=18 SqueezeLeft=19 SqueezeRight=20 SqueezeBottomLeft=21 SqueezeBottom=22 SqueezeBottomRight=23 PushTopLeft=24 PushTop=25 PushTopRight=26 PushLeft=27 PushRight=28 PushBottomLeft=29 PushBottom=30 PushBottomRight=31 GraphicLogoWipe=34 | optional | DVE effect |
| `--fill-source` | source, 0-65535 | optional | fill source |
| `--key-source` | source, 0-65535 | optional | key source |
| `--key-enable` | bool | optional | enable the key |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, clamped to 0-100 | optional | key clip |
| `--gain` | float, %, clamped to 0-100 | optional | key gain |
| `--invert` | bool | optional | invert the key |
| `--reverse` | bool | optional | reverse direction |
| `--flip-flop` | bool | optional | flip flop |

## `bmdctl atem transition mix <rate>`

Set the mix transition's rate.

```
bmdctl transition mix 1:00
bmdctl transition mix 0:12 --me 2
```

Echo: `TMxP:<me>`.

| argument | value | | |
|---|---|---|---|
| `<rate>` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem transition next`

Set the layers the next transition takes, or toggle one.

```
bmdctl transition next --next background,key1
bmdctl transition next --toggle key1
```

Echo: `TrSS:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--next` | flags: background=1 key1=2 key2=4 key3=8 key4=16 | optional | the layers, replacing the selection (not none) |
| `--toggle` | enum: background=1 key1=2 key2=4 key3=8 key4=16 | optional | flip one layer in the current selection |

## `bmdctl atem transition preview <enabled>`

Turn transition preview on or off.

```
bmdctl transition preview on
bmdctl transition preview on --me 2
```

**Unverified.** Echo: `TrPr:<me>`.

| argument | value | | |
|---|---|---|---|
| `<enabled>` | bool | required | preview the transition |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem transition rate <rate>`

Set the rate of the M/E's current transition style.

```
bmdctl transition rate 0:20
bmdctl transition rate 30 --me 2
```

Echo: `TMxP, TDpP, TWpP, TDvP or TStP:<me>, by style`.

| argument | value | | |
|---|---|---|---|
| `<rate>` | rate, frames, or seconds:frames at the display rate | required | rate |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem transition settings`

Set the transition style and the layers the next transition takes.

```
bmdctl transition settings --style mix --next background
bmdctl transition settings --style wipe --me 2
```

Give at least one flag. Echo: `TrSS:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--style` | enum, 0-255: mix=0 dip=1 wipe=2 dve=3 stinger=4 | optional | transition style |
| `--next` | flags: background=1 key1=2 key2=4 key3=8 key4=16 | optional | layers the next transition takes (not none) |

## `bmdctl atem transition stinger`

Set the stinger transition.

```
bmdctl transition stinger --player 1 --duration 2:00 --trigger 0:12
bmdctl transition stinger --pre-roll 0:10 --rate 0:08
bmdctl transition stinger --premultiplied off --clip 50 --gain 70
```

Give at least one flag. Echo: `TStP:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--player` | uint, 0-255 | optional | media player slot, 1-4 (not a source id) |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, ×10 on the wire, not clamped | optional | key clip |
| `--gain` | float, %, ×10 on the wire, not clamped | optional | key gain |
| `--invert` | bool | optional | invert the key |
| `--pre-roll` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | pre-roll |
| `--duration` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | clip duration |
| `--trigger` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | trigger point |
| `--rate` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | mix rate |

## `bmdctl atem transition style <style>`

Set the transition style.

```
bmdctl transition style mix
bmdctl transition style stinger --me 2
```

Echo: `TrSS:<me>`.

| argument | value | | |
|---|---|---|---|
| `<style>` | enum: mix=0 dip=1 wipe=2 dve=3 stinger=4 | required | transition style |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

## `bmdctl atem transition wipe`

Set the wipe transition.

```
bmdctl transition wipe --pattern CircleIris --rate 1:00
bmdctl transition wipe --width 5 --source col1 --softness 20
bmdctl transition wipe --reverse --flip-flop
```

Give at least one flag. Echo: `TWpP:<me>`.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | optional | rate |
| `--pattern` | enum, 0-255: LeftToRightBar=0 TopToBottomBar=1 HorizontalBarnDoor=2 VerticalBarnDoor=3 CornersInFourBox=4 RectangleIris=5 DiamondIris=6 CircleIris=7 TopLeftBox=8 TopRightBox=9 BottomRightBox=10 BottomLeftBox=11 TopCentreBox=12 RightCentreBox=13 BottomCentreBox=14 LeftCentreBox=15 TopLeftDiagonal=16 TopRightDiagonal=17 | optional | wipe pattern |
| `--width` | float, %, clamped to 0-100 | optional | border width |
| `--source` | source, 0-65535 | optional | border fill source |
| `--symmetry` | float, %, clamped to 0-100 | optional | symmetry |
| `--softness` | float, %, clamped to 0-100 | optional | softness |
| `--position-x` | float, 0-1, clamped | optional | horizontal position |
| `--position-y` | float, 0-1, clamped | optional | vertical position |
| `--reverse` | bool | optional | reverse direction |
| `--flip-flop` | bool | optional | flip flop |
