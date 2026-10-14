# atem: macro

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Macros: run, stop, record, list, upload and download, decode and encode.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem macro action <action>`

Run, stop, continue or delete a macro, or stop recording.

```
bmdctl macro action stop-record
bmdctl macro action insert-user-wait
bmdctl macro action continue
```

Echo: `MRPr, MRcS or MPrp:<slot>, by action`.

| argument | value | | |
|---|---|---|---|
| `<action>` | enum, 0-255: run=0 stop=1 stop-record=2 insert-user-wait=3 continue=4 delete=5 | required | action |
| `--slot` | uint, 1-65535, counted from 1 | optional | macro slot (default: whatever is running or recording) |

## `bmdctl atem macro decode <file>`

Turn macro bytecode into XML ops (JSON with --json).

```
bmdctl macro decode open.bin
bmdctl macro decode open.bin --json
```

Runs in the CLI; needs no switcher.

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the bytecode; - reads stdin |

## `bmdctl atem macro delete <slot>`

Delete a macro.

```
bmdctl macro delete 4
bmdctl macro delete "Lower third in"
```

**Destructive.** Echo: `MPrp:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | string | required | macro slot, or the macro's name |

## `bmdctl atem macro download <slot> [file]`

Download a macro as XML ops, JSON ops or bytecode.

```
bmdctl macro download 1
bmdctl macro download 1 open.xml
bmdctl macro download 1 - --format json
```

| argument | value | | |
|---|---|---|---|
| `<slot>` | string | required | macro slot, or the macro's name |
| `[file]` | file, written | optional | where to write it; - is stdout (default: the macro's name, .xml) |
| `--format` | enum: xml=0 json=1 bin=2 | optional | default: from --file's extension, else xml |

## `bmdctl atem macro encode <file> <out>`

Turn XML or JSON ops into macro bytecode.

```
bmdctl macro encode open.xml open.bin
```

Runs in the CLI; needs no switcher.

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the ops; - reads stdin |
| `<out>` | file, written | required | where to write the bytecode; - is stdout |

## `bmdctl atem macro list`

List the macros: slot, name, description; and what runs or records.

```
bmdctl macro list
bmdctl macro list --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | include empty slots |

## `bmdctl atem macro record <slot> <name>`

Start recording a macro.

```
bmdctl macro record 5 "Lower third in"
bmdctl macro record 6 Open --description "titles and music"
```

Echo: `MRcS`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-65536, counted from 1 | required | macro slot |
| `<name>` | string | required | name |
| `--description` | string | optional | description |

## `bmdctl atem macro run <slot>`

Run a macro.

```
bmdctl macro run 1
bmdctl macro run "Lower third in"
```

Echo: `MRPr`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | string | required | macro slot, or the macro's name |

## `bmdctl atem macro sleep <frames>`

Record a wait into the macro being recorded.

```
bmdctl macro sleep 25
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<frames>` | uint, 0-4294967295 | required | frames to wait |

## `bmdctl atem macro stop`

Stop the macro that is running.

```
bmdctl macro stop
```

Echo: `MRPr`.

## `bmdctl atem macro upload <slot> <file>`

Upload a macro from XML ops, JSON ops or bytecode.

```
bmdctl macro upload 5 open.xml
bmdctl macro upload 6 open.bin --name Open
```

Echo: `MPrp:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-65535, counted from 1 | required | macro slot; replacing a used one needs --yes |
| `<file>` | file | required | the macro; - reads stdin |
| `--name` | string | optional | the macro's name (default: the file's <Macro name>, else its file name) |
| `--description` | string | optional | the macro's description (default: the file's) |
