# bmdctl commands

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go test ./cmd/bmdctl -run TestCommandsDoc -update`.

Each command takes its parameters as `--name value` flags. **Unverified**
commands have a pinned wire format that nobody has confirmed on a
switcher; **destructive** ones are hard to undo or drop connections. The
echo is the state the device reports back once it has applied the
command; a command without one can't be confirmed.

## atem

### `bmdctl atem audio input`

Set a classic audio mixer input.

**Unverified.** Give at least one optional parameter. Echo: `AMIP:<source>.0`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | source id or input name |
| `--balance` | int, -32768 to 32767, ±10000 | optional | balance |
| `--volume` | uint, 0-65535, 0-65381 | optional | volume |
| `--on` | bool | optional | mix option on or off (wins over --afv) |
| `--afv` | bool | optional | audio follows video |

### `bmdctl atem audio levels`

Start or stop the classic mixer's meter stream.

**Unverified.** No echo.

| parameter | value | | |
|---|---|---|---|
| `--enable` | bool | required | stream meters |

### `bmdctl atem audio master`

Set the classic audio mixer's master.

**Unverified.** Give at least one optional parameter. Echo: `AMMO`.

| parameter | value | | |
|---|---|---|---|
| `--volume` | uint, 0-65535, 0-65381 | optional | volume |
| `--afv` | bool | optional | audio follows video |

### `bmdctl atem audio meters`

Read the audio meters: each strip's and the master's levels in dB, Fairlight or classic.

| parameter | value | | |
|---|---|---|---|
| `--for` | float, seconds | default 0.5 | how long to collect levels; the last of each strip's is shown |

### `bmdctl atem audio monitor`

Set the classic audio mixer's monitor.

**Unverified.** Give at least one optional parameter. Echo: `AMmO`.

| parameter | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | monitor on |
| `--volume` | uint, 0-65535 | optional | volume |
| `--mute` | bool | optional | mute |
| `--solo` | bool | optional | solo |
| `--solo-source` | source, 0-65535 | optional | solo source |
| `--dim` | bool | optional | dim |
| `--dim-volume` | uint, 0-65535 | optional | dim volume |

### `bmdctl atem auto`

Run the M/E's transition with its current style and rate.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |

### `bmdctl atem auto-video-mode`

Turn automatic input video mode detection on or off.

**Unverified.** Echo: `AiVM`.

| parameter | value | | |
|---|---|---|---|
| `--enable` | bool | required | detect the input video mode |

### `bmdctl atem aux`

Route a source to an aux output.

Echo: `AuxS:<aux>`.

| parameter | value | | |
|---|---|---|---|
| `--aux` | uint, 0-255 | required | aux output, 0-based |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem camera`

Send one Blackmagic camera control parameter.

**Unverified.** Echo: `CCdP:<destination>:<category>:<parameter>`.

| parameter | value | | |
|---|---|---|---|
| `--destination` | uint, 0-255 | required | camera; 255 for all |
| `--category` | uint, 0-255 | required | category |
| `--parameter` | uint, 0-255 | required | parameter |
| `--relative` | bool | default false | add to the current value |
| `--type` | enum, 0-255: bool=0 int8=1 int16=2 int32=3 int64=4 string=5 fixed16=128 | default bool | data type |
| `--data` | list | optional | comma-separated values of the data type |

### `bmdctl atem color`

Set a colour generator.

Give at least one optional parameter. Echo: `ColV:<generator>`.

| parameter | value | | |
|---|---|---|---|
| `--generator` | uint, 0-255 | required | colour generator, 0 or 1 |
| `--hue` | float, degrees, not clamped | optional | hue |
| `--saturation` | float, %, clamped to 0-100 | optional | saturation |
| `--luma` | float, %, clamped to 0-100 | optional | luma |

### `bmdctl atem connect`

Open a session to the switcher and keep it open until disconnect.

| parameter | value | | |
|---|---|---|---|
| `--pin` | bool | default true | keep the session open until disconnect; false lets it close after 10 idle minutes |

### `bmdctl atem cut`

Cut program and preview.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |

### `bmdctl atem disconnect`

Close the engine's session to the switcher, pinned or not.

### `bmdctl atem dsk auto`

Run a downstream keyer's auto transition.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |

### `bmdctl atem dsk fill`

Set a downstream keyer's fill source.

Echo: `DskB:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem dsk gain`

Set a downstream keyer's clip, gain, pre-multiply and invert.

Give at least one optional parameter. Echo: `DskP:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, clamped to 0-100 | optional | clip |
| `--gain` | float, %, clamped to 0-100 | optional | gain |
| `--invert` | bool | optional | invert the key |

### `bmdctl atem dsk key`

Set a downstream keyer's key source.

Echo: `DskB:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem dsk mask`

Set a downstream keyer's mask.

Give at least one optional parameter. Echo: `DskP:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--enabled` | bool | optional | mask on |
| `--top` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | top edge |
| `--bottom` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | bottom edge |
| `--left` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | left edge |
| `--right` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | right edge |

### `bmdctl atem dsk on-air`

Put a downstream keyer on or off air.

Echo: `DskS:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--on-air` | bool | required | on air |

### `bmdctl atem dsk rate`

Set a downstream keyer's rate.

Echo: `DskP:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |

### `bmdctl atem dsk tie`

Tie a downstream keyer to the next transition.

Echo: `DskP:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |
| `--tie` | bool | required | tie |

### `bmdctl atem dsk toggle`

Put a downstream keyer on air if it is off, or off if it is on.

Echo: `DskS:<dsk>`.

| parameter | value | | |
|---|---|---|---|
| `--dsk` | uint, 0-255 | default 0 | downstream keyer, 0-based |

### `bmdctl atem fairlight compressor`

Set a Fairlight strip's compressor.

Give at least one optional parameter. Echo: `AICP:<source>.<channel>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | required | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | compressor on |
| `--threshold` | float, dB, -60-0 | optional | threshold |
| `--ratio` | float, 1.2-20 | optional | ratio |
| `--attack` | float, ms, 0.7-100 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

### `bmdctl atem fairlight eq`

Set one EQ band of a Fairlight strip.

Give at least one optional parameter. Echo: `AEBP:<source>.<channel>:<band>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | required | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--band` | uint, 0-255 | required | band, 0-5 |
| `--enabled` | bool | optional | band on |
| `--shape` | enum, 0-255: LowShelf=1 LowPass=2 BandPass=4 Notch=8 HighPass=16 HighShelf=32 | optional | filter shape |
| `--range` | enum, 0-255: Low=1 MidLow=2 MidHigh=4 High=8 | optional | frequency range |
| `--frequency` | int, Hz, clamped to 30-21700 | optional | frequency |
| `--gain` | float, dB, clamped to ±20 | optional | gain |
| `--q` | float, clamped to 0.30-10.30 | optional | Q |

### `bmdctl atem fairlight expander`

Set a Fairlight strip's expander or gate.

Give at least one optional parameter. Echo: `AIXP:<source>.<channel>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | required | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | expander on |
| `--mode` | enum, 0-255: expander=0 gate=1 | optional | mode |
| `--threshold` | float, dB, -50-0 | optional | threshold |
| `--range` | float, dB, 0-60 | optional | range |
| `--ratio` | float, 1-3 | optional | ratio |
| `--attack` | float, ms, 0.5-100 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

### `bmdctl atem fairlight levels`

Start or stop the Fairlight meter streams (again after every reconnect).

No echo.

| parameter | value | | |
|---|---|---|---|
| `--enable` | bool | required | stream meters |

### `bmdctl atem fairlight limiter`

Set a Fairlight strip's limiter.

Give at least one optional parameter. Echo: `AILP:<source>.<channel>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | required | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--enabled` | bool | optional | limiter on |
| `--threshold` | float, dB, -60-0 | optional | threshold |
| `--attack` | float, ms, 0.7-30 | optional | attack |
| `--hold` | float, ms, 0-4000 | optional | hold |
| `--release` | float, ms, 50-4000 | optional | release |

### `bmdctl atem fairlight master`

Set the Fairlight master bus.

Give at least one optional parameter. Echo: `FAMP`.

| parameter | value | | |
|---|---|---|---|
| `--eq` | bool | optional | EQ on |
| `--eq-gain` | float, dB, clamped to ±20 | optional | EQ gain |
| `--makeup` | float, dB, clamped to 0-20 | optional | dynamics make-up gain |
| `--volume` | float, dB, clamped to -100-10; -inf is -100 | optional | fader level |
| `--afv` | bool | optional | audio follows video |

### `bmdctl atem fairlight master-compressor`

Set the Fairlight master bus compressor.

Give at least one optional parameter. Echo: `MOCP`.

| parameter | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | compressor on |
| `--threshold` | float, dB | optional | threshold |
| `--ratio` | float | optional | ratio |
| `--attack` | float, ms | optional | attack |
| `--hold` | float, ms | optional | hold |
| `--release` | float, ms | optional | release |

### `bmdctl atem fairlight master-eq`

Set one EQ band of the Fairlight master bus.

Give at least one optional parameter. Echo: `AMBP:<band>`.

| parameter | value | | |
|---|---|---|---|
| `--band` | uint, 0-255 | required | band, 0-5 |
| `--enabled` | bool | optional | band on |
| `--shape` | enum, 0-255: LowShelf=1 LowPass=2 BandPass=4 Notch=8 HighPass=16 HighShelf=32 | optional | filter shape |
| `--range` | enum, 0-255: Low=1 MidLow=2 MidHigh=4 High=8 | optional | frequency range |
| `--frequency` | int, Hz, clamped to 30-21700 | optional | frequency |
| `--gain` | float, dB, clamped to ±20 | optional | gain |
| `--q` | float, clamped to 0.30-10.30 | optional | Q |

### `bmdctl atem fairlight master-limiter`

Set the Fairlight master bus limiter.

Give at least one optional parameter. Echo: `AMLP`.

| parameter | value | | |
|---|---|---|---|
| `--enabled` | bool | optional | limiter on |
| `--threshold` | float, dB | optional | threshold |
| `--attack` | float, ms | optional | attack |
| `--hold` | float, ms | optional | hold |
| `--release` | float, ms | optional | release |

### `bmdctl atem fairlight strip`

Set a Fairlight strip.

Give at least one optional parameter. Echo: `FASP:<source>.<channel>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | audio source id (cameras 1...N, 1301 XLR mic, 1401 TRS jack) or input name |
| `--channel` | int, -128 to 127 | required | -1 for a stereo strip, 0 or 1 for a split-mono half |
| `--delay` | uint, 0-255, frames | optional | delay |
| `--gain` | float, dB, clamped to -100-6 | optional | input gain |
| `--eq` | bool | optional | EQ on |
| `--eq-gain` | float, dB, clamped to ±20 | optional | EQ gain |
| `--makeup` | float, dB, clamped to 0-20 | optional | dynamics make-up gain |
| `--pan` | float, -100 to 100, clamped | optional | pan |
| `--volume` | float, dB, clamped to -100-10; -inf is -100 | optional | fader level |
| `--mix` | enum, 0-255: Off=1 On=2 AudioFollowVideo=4 | optional | mix option |

### `bmdctl atem ftb enable`

Make fade to black available or not (always M/E 1; see docs/PORTING.md).

Echo: `FEna`.

| parameter | value | | |
|---|---|---|---|
| `--enabled` | bool | required | available |

### `bmdctl atem ftb rate`

Set the fade-to-black rate.

Echo: `FtbP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |

### `bmdctl atem ftb toggle`

Fade to black, or back.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |

### `bmdctl atem hyperdeck-slot`

Bind a HyperDeck slot to an address and an input.

Give at least one optional parameter. Echo: `RXMS:<slot>`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-65535 | required | HyperDeck slot, 0-9 |
| `--address` | string | optional | deck IPv4 address; 0.0.0.0 clears the slot |
| `--input` | source, 0-65535 | optional | switcher input the deck feeds |

### `bmdctl atem info`

Show the switcher's model, firmware, video mode and what it has.

### `bmdctl atem input list`

List the switcher's sources: ids, names and port types.

### `bmdctl atem input rename`

Rename an input.

Give at least one optional parameter. Echo: `InPr:<source>`.

| parameter | value | | |
|---|---|---|---|
| `--source` | source, 0-65535 | required | source id or input name |
| `--long-name` | string, up to 20 bytes | optional | long name |
| `--short-name` | string, up to 4 bytes | optional | short name |
| `--port-type` | uint, 0-65535 | optional | external port type (no atemwire operation sets it) |

### `bmdctl atem macro action`

Run, stop, continue or delete a macro, or stop recording.

Echo: `MRPr, MRcS or MPrp:<slot>, by action`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-65535 | default 65535 | macro slot, 0-based; 65535 for whatever is running or recording |
| `--action` | enum, 0-255: run=0 stop=1 stop-record=2 insert-user-wait=3 continue=4 delete=5 | required | action |

### `bmdctl atem macro record`

Start recording a macro.

Echo: `MRcS`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-65535 | required | macro slot, 0-based |
| `--name` | string | required | name |
| `--description` | string | optional | description |

### `bmdctl atem macro sleep`

Record a wait into the macro being recorded.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--frames` | uint, 0-4294967295 | required | frames to wait |

### `bmdctl atem media capture`

Capture program into the next free still slot.

No echo.

### `bmdctl atem media clear`

Empty a still slot, under the media pool's lock.

**Destructive.** Echo: `MPfe:<slot>`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-255 | required | still slot, 0-based |

### `bmdctl atem media clear-unlocked`

Clear a still slot without taking the store lock (some models ignore this; media clear takes it).

**Destructive.** Echo: `MPfe:<slot>`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-255 | required | still slot, 0-based |

### `bmdctl atem media download`

Download a still slot as a PNG.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-65535 | required | still slot, 0-based |
| `--file` | file, written | optional | where to write it; - is stdout (default: the slot's name .png) |

### `bmdctl atem media list`

List the media pool's stills and what each media player shows.

### `bmdctl atem media player`

Point a media player at a still or a clip.

Give at least one optional parameter. Echo: `MPCE:<player>`.

| parameter | value | | |
|---|---|---|---|
| `--player` | uint, 0-255 | required | media player, 0-based |
| `--still` | uint, 0-255 | optional | still slot (give this or --clip) |
| `--clip` | uint, 0-255 | optional | clip slot (give this or --still) |

### `bmdctl atem media upload`

Upload a PNG, JPEG or GIF to a still slot, fitted to the video mode.

Echo: `MPfe:<slot>`.

| parameter | value | | |
|---|---|---|---|
| `--slot` | uint, 0-65535 | required | still slot, 0-based; replacing a used one needs --yes |
| `--file` | file | required | the image; - reads stdin |
| `--name` | string | optional | the slot's name (default: the file's, without extension) |
| `--description` | string | optional | the slot's description |
| `--premultiply` | bool | optional | premultiply colour by alpha (default: for PNG, as WebATEM does) |

### `bmdctl atem multiviewer input`

Route a source to a multiviewer window.

**Unverified.** Echo: `MvIn:<multiviewer>:<window>`.

| parameter | value | | |
|---|---|---|---|
| `--multiviewer` | uint, 0-255 | default 0 | multiviewer, 0-based |
| `--window` | uint, 0-255 | required | window |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem multiviewer properties`

Set a multiviewer's layout and program/preview swap.

**Unverified.** Give at least one optional parameter. Echo: `MvPr:<multiviewer>`.

| parameter | value | | |
|---|---|---|---|
| `--multiviewer` | uint, 0-255 | default 0 | multiviewer, 0-based |
| `--layout` | uint, 0-255, 4-bit field: bit N splits quadrant N | optional | layout |
| `--swap` | bool | optional | swap program and preview |

### `bmdctl atem preview`

Put a source on an M/E's preview bus.

Echo: `PrvI:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem program`

Put a source on an M/E's program bus.

Echo: `PrgI:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem recording settings`

Set the recording file name and disks.

**Unverified.** Give at least one optional parameter. Echo: `RMSu`.

| parameter | value | | |
|---|---|---|---|
| `--filename` | string, up to 128 bytes | optional | file name |
| `--disk1` | uint, 0-4294967295 | optional | first disk |
| `--disk2` | uint, 0-4294967295 | optional | second disk |
| `--in-camera` | bool | optional | record in the cameras too |

### `bmdctl atem recording state`

Start or stop recording.

**Unverified.** Echo: `RTMS`.

| parameter | value | | |
|---|---|---|---|
| `--recording` | bool | required | recording |

### `bmdctl atem startup clear`

Clear the saved power-on state.

**Unverified.** **Destructive.** No echo.

### `bmdctl atem startup save`

Make the current state the power-on state.

**Unverified.** **Destructive.** No echo.

### `bmdctl atem state`

Print the switcher's state as the session holds it, or one part of it.

| parameter | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: mes.0.program, inputs.1.name, fairlight.master |

### `bmdctl atem status`

Show what is on air: program, preview, transitions, keyers, macros.

### `bmdctl atem streaming audio-bitrate`

Set the streaming audio bitrate.

**Unverified.** Echo: `STAB`.

| parameter | value | | |
|---|---|---|---|
| `--min` | uint, 0-4294967295, bit/s | required | lowest |
| `--max` | uint, 0-4294967295, bit/s | required | highest |

### `bmdctl atem streaming service`

Set the streaming service.

**Unverified.** Give at least one optional parameter. Echo: `SRSU`.

| parameter | value | | |
|---|---|---|---|
| `--name` | string, up to 64 bytes | optional | service name |
| `--url` | string, up to 512 bytes | optional | server URL |
| `--key` | string, up to 512 bytes | optional | stream key |
| `--bitrate-min` | uint, 0-4294967295, bit/s | optional | lowest bitrate (with --bitrate-max) |
| `--bitrate-max` | uint, 0-4294967295, bit/s | optional | highest bitrate (with --bitrate-min) |

### `bmdctl atem streaming state`

Start or stop streaming.

**Unverified.** Echo: `StRS`.

| parameter | value | | |
|---|---|---|---|
| `--streaming` | bool | required | streaming |

### `bmdctl atem supersource box`

Set a SuperSource box.

**Unverified.** Give at least one optional parameter. Echo: `SSBP:<supersource>:<box>`.

| parameter | value | | |
|---|---|---|---|
| `--supersource` | uint, 0-255 | default 0 | SuperSource, 0-based |
| `--box` | uint, 0-255 | required | box, 0-3 |
| `--enabled` | bool | optional | box on |
| `--source` | source, 0-65535 | optional | source |
| `--x` | int, -32768 to 32767, -4800-4800 | optional | x |
| `--y` | int, -32768 to 32767, -3400-3400 | optional | y |
| `--size` | uint, 0-65535, 70-1000 | optional | size |
| `--masked` | bool | optional | crop on |
| `--top` | uint, 0-65535, 0-18000 | optional | crop top |
| `--bottom` | uint, 0-65535, 0-18000 | optional | crop bottom |
| `--left` | uint, 0-65535, 0-32000 | optional | crop left |
| `--right` | uint, 0-65535, 0-32000 | optional | crop right |

### `bmdctl atem supersource properties`

Set SuperSource's art fill, key and layer.

**Unverified.** Give at least one optional parameter. Echo: `SSrc:<supersource>`.

| parameter | value | | |
|---|---|---|---|
| `--supersource` | uint, 0-255 | default 0 | SuperSource, 0-based |
| `--fill-source` | source, 0-65535 | optional | art fill source |
| `--key-source` | source, 0-65535 | optional | art key source |
| `--layer` | enum, 0-255: background=0 foreground=1 | optional | art layer |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | uint, 0-65535, 0-1000 | optional | clip |
| `--gain` | uint, 0-65535, 0-1000 | optional | gain |
| `--invert` | bool | optional | invert the key |

### `bmdctl atem tbar`

Move the T-bar.

**Unverified.** Echo: `TrPs:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--position` | int, 0-10000, clamped | required | T-bar position |

### `bmdctl atem time-of-day`

Set the switcher's clock.

**Unverified.** No echo.

| parameter | value | | |
|---|---|---|---|
| `--time` | time | default now | time |

### `bmdctl atem time-request`

Ask the switcher for its time (the reply is the Time packet).

No echo.

### `bmdctl atem transition dip`

Set the dip transition.

Give at least one optional parameter. Echo: `TDpP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | optional | rate |
| `--source` | source, 0-65535 | optional | dip source |

### `bmdctl atem transition dve`

Set the DVE transition.

Give at least one optional parameter. Echo: `TDvP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
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

### `bmdctl atem transition mix`

Set the mix transition's rate.

Echo: `TMxP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--rate` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |

### `bmdctl atem transition next`

Set the layers the next transition takes, or toggle one.

Echo: `TrSS:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--next` | flags: background=1 key1=2 key2=4 key3=8 key4=16 | optional | the layers, replacing the selection (not none) |
| `--toggle` | enum: background=1 key1=2 key2=4 key3=8 key4=16 | optional | flip one layer in the current selection |

### `bmdctl atem transition preview`

Turn transition preview on or off.

**Unverified.** Echo: `TrPr:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--enabled` | bool | required | preview the transition |

### `bmdctl atem transition rate`

Set the rate of the M/E's current transition style.

Echo: `TMxP, TDpP, TWpP, TDvP or TStP:<me>, by style`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--rate` | rate, frames, or seconds:frames at the display rate | required | rate |

### `bmdctl atem transition settings`

Set the transition style and the layers the next transition takes.

Give at least one optional parameter. Echo: `TrSS:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--style` | enum, 0-255: mix=0 dip=1 wipe=2 dve=3 stinger=4 | optional | transition style |
| `--next` | flags: background=1 key1=2 key2=4 key3=8 key4=16 | optional | layers the next transition takes (not none) |

### `bmdctl atem transition stinger`

Set the stinger transition.

Give at least one optional parameter. Echo: `TStP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--player` | uint, 0-255 | optional | media player slot, 1-4 (not a source id) |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, ×10 on the wire, not clamped | optional | key clip |
| `--gain` | float, %, ×10 on the wire, not clamped | optional | key gain |
| `--invert` | bool | optional | invert the key |
| `--pre-roll` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | pre-roll |
| `--duration` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | clip duration |
| `--trigger` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | trigger point |
| `--rate` | rate, 0-65535, frames, or seconds:frames at the display rate | optional | mix rate |

### `bmdctl atem transition style`

Set the transition style.

Echo: `TrSS:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--style` | enum: mix=0 dip=1 wipe=2 dve=3 stinger=4 | required | transition style |

### `bmdctl atem transition wipe`

Set the wipe transition.

Give at least one optional parameter. Echo: `TWpP:<me>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
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

### `bmdctl atem usk chroma`

Set an advanced chroma key.

Give at least one optional parameter. Echo: `KACk:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
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

### `bmdctl atem usk chroma-sample`

Set an advanced chroma key's sample cursor and colour.

Give at least one optional parameter. Echo: `KACC:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--cursor` | bool | optional | show the sample cursor |
| `--preview` | bool | optional | preview the key |
| `--x` | float, 0-1 | optional | cursor x |
| `--y` | float, 0-1 | optional | cursor y |
| `--size` | float, 0-1, clamped to 0.062-0.9925 | optional | cursor size |
| `--sample-y` | float, 0-1 | optional | sampled luma (for restoring a key) |
| `--sample-cb` | float, -1 to 1 | optional | sampled Cb |
| `--sample-cr` | float, -1 to 1 | optional | sampled Cr |

### `bmdctl atem usk dve`

Set a DVE (or fly) key.

Give at least one optional parameter. Echo: `KeDV:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
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

### `bmdctl atem usk fill`

Set an upstream keyer's fill source.

Echo: `KeBP:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem usk key`

Set an upstream keyer's key (cut) source.

Echo: `KeBP:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--source` | source, 0-65535 | required | source id or input name |

### `bmdctl atem usk keyframe-run`

Fly the key to a keyframe.

No echo.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--to` | enum, 0-255: a=1 b=2 full=3 runToInfinite=4 | required | keyframe to fly to |
| `--direction` | uint, 0-255 | optional | direction for runToInfinite; atemwire's two encodings agree on 1-4 and 6-9 (1 top left ... 9 bottom right) and disagree on the centre (5 or 0) |

### `bmdctl atem usk keyframe-set`

Store the keyer's DVE as a fly keyframe.

Echo: `KKFP:<me>:<keyer>:<keyframe> (A and B), KeFS:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--keyframe` | enum, 0-255: a=1 b=2 full=3 runToInfinite=4 | required | keyframe |

### `bmdctl atem usk luma`

Set a luma key.

Give at least one optional parameter. Echo: `KeLm:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | float, %, clamped to 0-100 | optional | clip |
| `--gain` | float, %, clamped to 0-100 | optional | gain |
| `--invert` | bool | optional | invert the key |

### `bmdctl atem usk mask`

Set an upstream keyer's mask.

**Unverified.** Give at least one optional parameter. Echo: `KeBP:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--enabled` | bool | optional | mask on |
| `--top` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | top edge |
| `--bottom` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | bottom edge |
| `--left` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | left edge |
| `--right` | float, display units: ±9 top and bottom, ±16 left and right (×1000) | optional | right edge |

### `bmdctl atem usk on-air`

Put an upstream keyer on or off air.

Echo: `KeOn:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--on-air` | bool | required | on air |

### `bmdctl atem usk pattern`

Set a pattern key.

Give at least one optional parameter. Echo: `KePt:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--pattern` | enum, 0-255: LeftToRightBar=0 TopToBottomBar=1 HorizontalBarnDoor=2 VerticalBarnDoor=3 CornersInFourBox=4 RectangleIris=5 DiamondIris=6 CircleIris=7 TopLeftBox=8 TopRightBox=9 BottomRightBox=10 BottomLeftBox=11 TopCentreBox=12 RightCentreBox=13 BottomCentreBox=14 LeftCentreBox=15 TopLeftDiagonal=16 TopRightDiagonal=17 | optional | pattern |
| `--size` | float, %, clamped to 0-100 | optional | size |
| `--symmetry` | float, %, clamped to 0-100 | optional | symmetry |
| `--softness` | float, %, clamped to 0-100 | optional | softness |
| `--position-x` | float, 0-1, clamped | optional | horizontal position |
| `--position-y` | float, 0-1, clamped | optional | vertical position |
| `--invert` | bool | optional | invert the pattern |

### `bmdctl atem usk toggle`

Put an upstream keyer on air if it is off, or off if it is on.

Echo: `KeOn:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |

### `bmdctl atem usk type`

Set an upstream keyer's type and fly mode.

Give at least one optional parameter. Echo: `KeBP:<me>:<keyer>`.

| parameter | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E (the library's ME field counts from 0) |
| `--keyer` | uint, 0-255 | required | upstream keyer, 0-based |
| `--type` | enum, 0-255: luma=0 chroma=1 pattern=2 dve=3 | optional | key type |
| `--fly` | bool | optional | fly key |

### `bmdctl atem video-mode`

Change the video mode: outputs resync and clients may be dropped.

**Destructive.** Echo: `VidM`.

| parameter | value | | |
|---|---|---|---|
| `--mode` | enum, 0-255: 525i5994=0 NTSC=0 625i50=1 PAL=1 NTSC_widescreen=2 PAL_widescreen=3 720p50=4 720p5994=5 1080i50=6 1080i5994=7 1080p2398=8 1080p24=9 1080p25=10 1080p2997=11 1080p50=12 1080p5994=13 2160p2398=14 2160p24=15 2160p25=16 2160p2997=17 2160p50=18 2160p5994=19 4320p2398=20 4320p24=21 4320p25=22 4320p2997=23 4320p50=24 4320p5994=25 1080p30=26 1080p60=27 720p60=28 1080i60=29 | required | video mode |

### `bmdctl atem watch`

Print each change the switcher reports, as it comes.

Runs until interrupted.

| parameter | value | | |
|---|---|---|---|
| `--filter` | list | optional | only keys starting with one of these: PrgI,KeOn:0 |
| `--count` | uint | optional | stop after this many changes |
