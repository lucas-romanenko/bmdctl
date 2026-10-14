# atem: audio

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The audio mixer: Fairlight strips, EQ, dynamics, the master and meters; the classic mixer's (classic-audio ...).

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem audio compressor <source>`

Set a Fairlight strip's compressor.

```
bmdctl audio compressor 1301 --enabled --threshold -20 --ratio 4
bmdctl audio compressor cam1 --attack 2 --release 200
```

Give at least one flag. Echo: `AICP:<source>.<channel>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | default -1 | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | compressor on |
| `--threshold` | float, dB, -60-0 | optional | threshold |
| `--ratio` | float, 1.2-20 | optional | ratio |
| `--attack` | float, ms, 0.7-100 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

## `bmdctl atem audio eq <source> <band>`

Set one EQ band of a Fairlight strip.

```
bmdctl audio eq 1301 1 --shape HighPass --frequency 80
bmdctl audio eq cam2 3 --frequency 2500 --gain -3 --q 2
bmdctl audio eq cam2 3 --enabled off
```

Give at least one flag. Echo: `AEBP:<source>.<channel>:<band>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `<band>` | uint, 1-6, counted from 1 | required | EQ band |
| `--channel` | int, -128 to 127 | default -1 | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | band on |
| `--shape` | enum, 0-255: LowShelf=1 LowPass=2 BandPass=4 Notch=8 HighPass=16 HighShelf=32 | optional | filter shape |
| `--range` | enum, 0-255: Low=1 MidLow=2 MidHigh=4 High=8 | optional | frequency range |
| `--frequency` | int, Hz, clamped to 30-21700 | optional | frequency |
| `--gain` | float, dB, clamped to ±20 | optional | gain |
| `--q` | float, clamped to 0.30-10.30 | optional | Q |

## `bmdctl atem audio expander <source>`

Set a Fairlight strip's expander or gate.

```
bmdctl audio expander 1301 --enabled --mode gate --threshold -45
bmdctl audio expander cam1 --range 20 --ratio 2
```

Give at least one flag. Echo: `AIXP:<source>.<channel>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | default -1 | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | expander on |
| `--mode` | enum, 0-255: expander=0 gate=1 | optional | mode |
| `--threshold` | float, dB, -50-0 | optional | threshold |
| `--range` | float, dB, 0-60 | optional | range |
| `--ratio` | float, 1-3 | optional | ratio |
| `--attack` | float, ms, 0.5-100 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

## `bmdctl atem audio levels <enable>`

Start or stop the meter streams, again after every reconnect; on a switcher with the classic mixer, sends classic-audio levels.

```
bmdctl audio levels on
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | stream meters |

## `bmdctl atem audio limiter <source>`

Set a Fairlight strip's limiter.

```
bmdctl audio limiter 1301 --enabled --threshold -6
bmdctl audio limiter cam1 --attack 1 --release 100
```

Give at least one flag. Echo: `AILP:<source>.<channel>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | default -1 | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | limiter on |
| `--threshold` | float, dB, -60-0 | optional | threshold |
| `--attack` | float, ms, 0.7-30 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

## `bmdctl atem audio master`

Set the master bus; on a switcher with the classic mixer, sends classic-audio master.

```
bmdctl audio master --volume -6
bmdctl audio master --volume -inf
bmdctl audio master --eq --eq-gain 2
```

Give at least one flag. Echo: `FAMP`.

| argument | value | | |
|---|---|---|---|
| `--eq` | bool | optional | EQ on (Fairlight only) |
| `--eq-gain` | float, dB, clamped to ±20 | optional | EQ gain (Fairlight only) |
| `--makeup` | float, dB, clamped to 0-20 | optional | dynamics make-up gain (Fairlight only) |
| `--volume` | float, dB, clamped to -100-10 (-inf to +6 on the classic mixer); -inf is -100 | optional | fader level |
| `--afv` | bool | optional | audio follows video |

## `bmdctl atem audio master-compressor`

Set the Fairlight master bus compressor.

```
bmdctl audio master-compressor --enabled --threshold -10 --ratio 3
```

Give at least one flag. Echo: `MOCP`.

| argument | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | compressor on |
| `--threshold` | float, dB | optional | threshold |
| `--ratio` | float | optional | ratio |
| `--attack` | float, ms | optional | attack |
| `--hold` | float, ms | optional | hold |
| `--release` | float, ms | optional | release |

## `bmdctl atem audio master-eq <band>`

Set one EQ band of the Fairlight master bus.

```
bmdctl audio master-eq 1 --shape LowShelf --gain -2
bmdctl audio master-eq 6 --shape HighShelf --gain 1.5
```

Give at least one flag. Echo: `AMBP:<band>`.

| argument | value | | |
|---|---|---|---|
| `<band>` | uint, 1-6, counted from 1 | required | EQ band |
| `--enabled` | bool | optional | band on |
| `--shape` | enum, 0-255: LowShelf=1 LowPass=2 BandPass=4 Notch=8 HighPass=16 HighShelf=32 | optional | filter shape |
| `--range` | enum, 0-255: Low=1 MidLow=2 MidHigh=4 High=8 | optional | frequency range |
| `--frequency` | int, Hz, clamped to 30-21700 | optional | frequency |
| `--gain` | float, dB, clamped to ±20 | optional | gain |
| `--q` | float, clamped to 0.30-10.30 | optional | Q |

## `bmdctl atem audio master-limiter`

Set the Fairlight master bus limiter.

```
bmdctl audio master-limiter --enabled --threshold -1
```

Give at least one flag. Echo: `AMLP`.

| argument | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | limiter on |
| `--threshold` | float, dB | optional | threshold |
| `--attack` | float, ms | optional | attack |
| `--hold` | float, ms | optional | hold |
| `--release` | float, ms | optional | release |

## `bmdctl atem audio meters`

Read the audio meters: each strip's and the master's levels in dB, from whichever mixer the switcher has.

```
bmdctl audio meters
bmdctl audio meters --for 3
```

| argument | value | | |
|---|---|---|---|
| `--for` | float, seconds | default 0.5 | how long to collect levels; the last of each strip's is shown |

## `bmdctl atem audio strip <source>`

Set a Fairlight strip.

```
bmdctl audio strip cam1 --volume -6
bmdctl audio strip 1301 --gain 12 --mix On
bmdctl audio strip cam2 --mix AudioFollowVideo
```

Give at least one flag. Echo: `FASP:<source>.<channel>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | default -1 | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--delay` | uint, 0-255, frames | optional | delay |
| `--gain` | float, dB, clamped to -100-6 | optional | input gain |
| `--eq` | bool | optional | EQ on |
| `--eq-gain` | float, dB, clamped to ±20 | optional | EQ gain |
| `--makeup` | float, dB, clamped to 0-20 | optional | dynamics make-up gain |
| `--pan` | float, -100 to 100, clamped | optional | pan |
| `--volume` | float, dB, clamped to -100-10; -inf is -100 | optional | fader level |
| `--mix` | enum, 0-255: Off=1 On=2 AudioFollowVideo=4 | optional | mix option |

## `bmdctl atem classic-audio input <source>`

Set a classic audio mixer input.

```
bmdctl classic-audio input cam1 --afv
bmdctl classic-audio input cam2 --on --volume 32768
bmdctl classic-audio input cam3 --balance -5000
```

**Unverified.** Give at least one flag. Echo: `AMIP:<source>.0`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--balance` | int, -32768 to 32767, ±10000 | optional | balance |
| `--volume` | uint, 0-65535, 0-65381 | optional | volume |
| `--on` | bool | optional | mix option on or off (wins over --afv) |
| `--afv` | bool | optional | audio follows video |

## `bmdctl atem classic-audio levels <enable>`

Start or stop the classic mixer's meter stream.

```
bmdctl classic-audio levels on
```

**Unverified.** No echo.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | stream meters |

## `bmdctl atem classic-audio master`

Set the classic audio mixer's master.

```
bmdctl classic-audio master --volume 32768
bmdctl classic-audio master --afv
```

**Unverified.** Give at least one flag. Echo: `AMMO`.

| argument | value | | |
|---|---|---|---|
| `--volume` | uint, 0-65535, 0-65381 | optional | volume |
| `--afv` | bool | optional | audio follows video |

## `bmdctl atem classic-audio meters`

Read the classic audio mixer's meters: each input's and the master's levels in dB.

```
bmdctl classic-audio meters
bmdctl classic-audio meters --for 3
```

| argument | value | | |
|---|---|---|---|
| `--for` | float, seconds | default 0.5 | how long to collect levels; the last of each strip's is shown |

## `bmdctl atem classic-audio monitor`

Set the classic audio mixer's monitor.

```
bmdctl classic-audio monitor --enabled --volume 32768
bmdctl classic-audio monitor --solo --solo-source cam2
bmdctl classic-audio monitor --dim
```

**Unverified.** Give at least one flag. Echo: `AMmO`.

| argument | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | monitor on |
| `--volume` | uint, 0-65535 | optional | volume |
| `--mute` | bool | optional | mute |
| `--solo` | bool | optional | solo |
| `--solo-source` | source, 0-65535 | optional | solo source |
| `--dim` | bool | optional | dim |
| `--dim-volume` | uint, 0-65535 | optional | dim volume |
