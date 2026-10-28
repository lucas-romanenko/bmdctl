# hyperdeck: transport

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The transport: play, pause, stop, record, loop, next and previous clip, goto a clip or a timecode, the active slot.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl hyperdeck goto <clip>`

Go to a timeline clip or a timecode, without playing.

```
bmdctl goto 2
bmdctl goto interview
bmdctl goto 00:01:30:00
```

| argument | value | | |
|---|---|---|---|
| `<clip>` | string | required | a timeline clip: its number, or its name or the start of it, in any case; or a timecode, HH:MM:SS:FF |

## `bmdctl hyperdeck loop <on>`

Turn looping on or off for the playback under way.

```
bmdctl loop on
bmdctl loop off
```

| argument | value | | |
|---|---|---|---|
| `<on>` | bool | required | on loops the timeline, or the clip with play --single-clip |

## `bmdctl hyperdeck next`

Go to the start of the next clip on the timeline.

```
bmdctl next
```

**Unverified.** Echo: `200 ok`.

## `bmdctl hyperdeck pause`

Hold the current frame: play at speed 0, which the deck reports as stopped.

```
bmdctl pause
```

Echo: `200 ok`.

## `bmdctl hyperdeck play`

Start playback, or change the one under way.

```
bmdctl play
bmdctl play --speed 200
bmdctl play --clip 2 --loop --single-clip
```

Echo: `200 ok`.

| argument | value | | |
|---|---|---|---|
| `--speed` | int, -5000 to 5000, % | optional | 100 is normal speed, 200 double, -100 backwards, 0 holds the frame |
| `--loop` | bool | optional | loop: the timeline, or the clip with --single-clip; false plays to the end |
| `--single-clip` | bool | optional | play the current clip only |
| `--clip` | uint, 1-2147483647 | optional | start at this timeline clip (clips lists them) |

## `bmdctl hyperdeck prev`

Go to the start of the previous clip on the timeline.

```
bmdctl prev
```

**Unverified.** Echo: `200 ok`.

## `bmdctl hyperdeck record [name]`

Start recording on the active slot, into a clip named as given.

```
bmdctl record
bmdctl record "Interview 2"
```

**Unverified.** Echo: `200 ok`.

| argument | value | | |
|---|---|---|---|
| `[name]` | string | optional | the clip's name; the deck names it from its record prefix otherwise |

## `bmdctl hyperdeck slot <slot>`

Make a slot the active one: its disk is what plays and records.

```
bmdctl slot 2
```

Echo: `200 ok`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-255 | required | the slot, as the deck numbers them: 1 or 2 on a HyperDeck Studio |

## `bmdctl hyperdeck stop`

Stop playback or recording; the deck holds the last frame.

```
bmdctl stop
```

Echo: `200 ok`.
