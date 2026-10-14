# atem: outputs

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Multiviewers, SuperSource, recording and streaming.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem multiviewer input <window> <source>`

Route a source to a multiviewer window.

```
bmdctl multiviewer input 3 cam5
bmdctl multiviewer input 10 aux1 --multiviewer 2
```

**Unverified.** Echo: `MvIn:<multiviewer>:<window>`.

| argument | value | | |
|---|---|---|---|
| `<window>` | uint, 1-256, counted from 1 | required | window |
| `<source>` | source, 0-65535 | required | source id or input name |
| `--multiviewer` | uint, 1-256, counted from 1 | default 1 | multiviewer |

## `bmdctl atem multiviewer properties`

Set a multiviewer's layout and program/preview swap.

```
bmdctl multiviewer properties --swap
bmdctl multiviewer properties --layout 12 --multiviewer 2
```

**Unverified.** Give at least one flag. Echo: `MvPr:<multiviewer>`.

| argument | value | | |
|---|---|---|---|
| `--multiviewer` | uint, 1-256, counted from 1 | default 1 | multiviewer |
| `--layout` | uint, 0-255, 4-bit field: bit N splits quadrant N | optional | layout |
| `--swap` | bool | optional | swap program and preview |

## `bmdctl atem recording settings`

Set the recording file name and disks.

```
bmdctl recording settings --filename service-2026-10-11
bmdctl recording settings --in-camera
```

**Unverified.** Give at least one flag. Echo: `RMSu`.

| argument | value | | |
|---|---|---|---|
| `--filename` | string, up to 128 bytes | optional | file name |
| `--disk1` | uint, 0-4294967295 | optional | first disk |
| `--disk2` | uint, 0-4294967295 | optional | second disk |
| `--in-camera` | bool | optional | record in the cameras too |

## `bmdctl atem recording state <recording>`

Start or stop recording.

```
bmdctl recording state on
```

**Unverified.** Echo: `RTMS`.

| argument | value | | |
|---|---|---|---|
| `<recording>` | bool | required | recording |

## `bmdctl atem streaming audio-bitrate <min> <max>`

Set the streaming audio bitrate.

```
bmdctl streaming audio-bitrate 128000 192000
```

**Unverified.** Echo: `STAB`.

| argument | value | | |
|---|---|---|---|
| `<min>` | uint, 0-4294967295, bit/s | required | lowest |
| `<max>` | uint, 0-4294967295, bit/s | required | highest |

## `bmdctl atem streaming service`

Set the streaming service.

```
bmdctl streaming service --name YouTube --key abcd-1234-efgh-5678
bmdctl streaming service --url rtmp://a.rtmp.youtube.com/live2
bmdctl streaming service --bitrate-min 4500000 --bitrate-max 6000000
```

**Unverified.** Give at least one flag. Echo: `SRSU`.

| argument | value | | |
|---|---|---|---|
| `--name` | string, up to 64 bytes | optional | service name |
| `--url` | string, up to 512 bytes | optional | server URL |
| `--key` | string, up to 512 bytes | optional | stream key |
| `--bitrate-min` | uint, 0-4294967295, bit/s | optional | lowest bitrate (with --bitrate-max) |
| `--bitrate-max` | uint, 0-4294967295, bit/s | optional | highest bitrate (with --bitrate-min) |

## `bmdctl atem streaming state <streaming>`

Start or stop streaming.

```
bmdctl streaming state on
```

**Unverified.** Echo: `StRS`.

| argument | value | | |
|---|---|---|---|
| `<streaming>` | bool | required | streaming |

## `bmdctl atem supersource box <box>`

Set a SuperSource box.

```
bmdctl supersource box 1 --source cam1 --enabled
bmdctl supersource box 2 --source cam2 --x 800 --size 500
bmdctl supersource box 2 --masked --left 4000 --right 4000
```

**Unverified.** Give at least one flag. Echo: `SSBP:<supersource>:<box>`.

| argument | value | | |
|---|---|---|---|
| `<box>` | uint, 1-4, counted from 1 | required | box |
| `--supersource` | uint, 1-256, counted from 1 | default 1 | SuperSource |
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

## `bmdctl atem supersource properties`

Set SuperSource's art fill, key and layer.

```
bmdctl supersource properties --fill-source mp1 --key-source mp1k
bmdctl supersource properties --layer foreground --premultiplied
```

**Unverified.** Give at least one flag. Echo: `SSrc:<supersource>`.

| argument | value | | |
|---|---|---|---|
| `--supersource` | uint, 1-256, counted from 1 | default 1 | SuperSource |
| `--fill-source` | source, 0-65535 | optional | art fill source |
| `--key-source` | source, 0-65535 | optional | art key source |
| `--layer` | enum, 0-255: background=0 foreground=1 | optional | art layer |
| `--premultiplied` | bool | optional | pre-multiplied key |
| `--clip` | uint, 0-65535, 0-1000 | optional | clip |
| `--gain` | uint, 0-65535, 0-1000 | optional | gain |
| `--invert` | bool | optional | invert the key |
