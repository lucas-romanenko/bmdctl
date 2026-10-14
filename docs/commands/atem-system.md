# atem: system

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The switcher itself: what it is, its video mode, startup state, time.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem auto-video-mode <enable>`

Turn automatic input video mode detection on or off.

```
bmdctl auto-video-mode on
```

**Unverified.** Echo: `AiVM`.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | detect the input video mode |

## `bmdctl atem info`

Show the switcher's model, firmware, video mode and what it has.

```
bmdctl info
```

## `bmdctl atem startup clear`

Clear the saved power-on state.

```
bmdctl startup clear
```

**Unverified.** **Destructive.** No echo.

## `bmdctl atem startup save`

Make the current state the power-on state.

```
bmdctl startup save
```

**Unverified.** **Destructive.** No echo.

## `bmdctl atem time`

Read the switcher's clock: ask for it (TiRq) and wait for the Time packet.

```
bmdctl time
```

Echo: `Time`.

## `bmdctl atem time-of-day`

Set the switcher's clock.

```
bmdctl time-of-day
bmdctl time-of-day --time 2026-10-11T19:30:00Z
```

**Unverified.** No echo.

| argument | value | | |
|---|---|---|---|
| `--time` | time | default now | time |

## `bmdctl atem time-request`

Ask the switcher for its time (the reply is the Time packet).

```
bmdctl time-request
```

No echo.

## `bmdctl atem video-mode <mode>`

Change the video mode: outputs resync and clients may be dropped.

```
bmdctl video-mode 1080p50
bmdctl video-mode 1080i5994
```

**Destructive.** Echo: `VidM`.

| argument | value | | |
|---|---|---|---|
| `<mode>` | enum, 0-255: 525i5994=0 NTSC=0 625i50=1 PAL=1 NTSC_widescreen=2 PAL_widescreen=3 720p50=4 720p5994=5 1080i50=6 1080i5994=7 1080p2398=8 1080p24=9 1080p25=10 1080p2997=11 1080p50=12 1080p5994=13 2160p2398=14 2160p24=15 2160p25=16 2160p2997=17 2160p50=18 2160p5994=19 4320p2398=20 4320p24=21 4320p25=22 4320p2997=23 4320p50=24 4320p5994=25 1080p30=26 1080p60=27 720p60=28 1080i60=29 | required | video mode |

## `bmdctl atem video-modes`

List the video modes the switcher supports, the current one marked.

```
bmdctl video-modes
```
