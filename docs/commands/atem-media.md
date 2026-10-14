# atem: media

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The media pool and players: stills uploaded to and downloaded from slots, cleared, what each player shows.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem media capture`

Capture program into the next free still slot.

```
bmdctl media capture
```

No echo.

## `bmdctl atem media clear <slot>`

Empty a still slot, under the media pool's lock.

```
bmdctl media clear 5
```

**Destructive.** Echo: `MPfe:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-256, counted from 1 | required | still slot |

## `bmdctl atem media clear-unlocked <slot>`

Clear a still slot without taking the store lock (some models ignore this; media clear takes it).

```
bmdctl media clear-unlocked 5
```

**Destructive.** Echo: `MPfe:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-256, counted from 1 | required | still slot |

## `bmdctl atem media download <slot> [file]`

Download a still slot as a PNG.

```
bmdctl media download 3
bmdctl media download 3 logo.png
```

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-65536, counted from 1 | required | still slot |
| `[file]` | file, written | optional | where to write it; - is stdout (default: the slot's name .png) |

## `bmdctl atem media list`

List the media pool's stills and what each media player shows.

```
bmdctl media list
bmdctl media list --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | at a terminal, the empty slots too (a pipe and --json always have them) |

## `bmdctl atem media player <player>`

Point a media player at a still or a clip.

```
bmdctl media player 1 --still 3
bmdctl media player 2 --clip 1
```

Give at least one flag. Echo: `MPCE:<player>`.

| argument | value | | |
|---|---|---|---|
| `<player>` | uint, 1-256, counted from 1 | required | media player |
| `--still` | uint, 1-256, counted from 1 | optional | still slot (give this or --clip) |
| `--clip` | uint, 1-256, counted from 1 | optional | clip slot (give this or --still) |

## `bmdctl atem media upload <slot> <file>`

Upload a PNG, JPEG or GIF to a still slot, fitted to the video mode.

```
bmdctl media upload 3 logo.png
bmdctl media upload 4 sponsor.png --name "Main Sponsor"
bmdctl media upload 5 lower-third.png --premultiply off
```

Echo: `MPfe:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-65536, counted from 1 | required | still slot; replacing a used one needs --yes |
| `<file>` | file | required | the image; - reads stdin |
| `--name` | string | optional | the slot's name (default: the file's, without extension) |
| `--description` | string | optional | the slot's description |
| `--premultiply` | bool | optional | premultiply colour by alpha (default: for PNG, as WebATEM does) |
