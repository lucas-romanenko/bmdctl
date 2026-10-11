# bmdctl commands

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go test ./cmd/bmdctl -run TestCommandsDoc -update`.

A command's required arguments follow its words, in the order they're
said (`<name>`; `[name]` may be left out); everything optional is a
`--name value` flag, which may be shortened to any unambiguous prefix.
**Unverified** commands have a pinned wire format that nobody has confirmed on a
switcher; **destructive** ones are hard to undo or drop connections. The
echo is the state the device reports back once it has applied the
command; a command without one can't be confirmed. Its key names the
state as the device's library does, so an ATEM's indexes in it count
from 0.

## The command line

```
bmdctl [--host H] [--json|--quiet] [--yes] [atem] <command> [arguments] [--flag value ...]
```

### Flags, accepted anywhere on the line

- `--host H`: The device: a name from `bmdctl hosts` in any case, any unambiguous start of one, or host[:port] (UDP 9910 by default). Without it: $BMDCTL_HOST, else the host the last --host named, else the default host.
- `--json`: Print the engine's JSON response as it is: {ok, confirmed, result, took_ms} or {ok:false, code, error}, one per line.
- `--quiet, -q`: Print nothing on stdout; the exit status tells. Errors still go to stderr.
- `--yes, -y`: Allow a destructive command.
- `--dry-run`: Print a wire command's bytes, header included, and send nothing.
- `--fps N`: The frame rate for rates given as seconds:frames (default: the switcher's display rate; 25 with --dry-run).
- `--timeout D`: How long to wait for the switcher's echo (default 5s), or for a transfer to finish (default: the library's).
- `--no-color`: No colour, even on a terminal.
- `--timestamps, -t`: At a terminal, start each result line with the time of day (HH:MM:SS), dim.
- `--help, -h`: Help for the command line so far.

### Built-in commands

- `bmdctl`: Show the engine (running or not, its sessions, the git commit it runs) and a line per host: its model, video mode, how long its session has been idle, and which is the default. At a terminal: bmdctl's version, then a line per switcher. Never starts the engine.
- `bmdctl help`: Print the help.
- `bmdctl version`: Print bmdctl's version; at a terminal also the commit it was built from, the hosts file and the engine.
- `bmdctl engine [--log-level L]`: Run the background engine in the foreground, for systemd or Docker, logging to stderr and its log file. Any command starts one in the background when none is running, and restarts one it started from another build of bmdctl; one run with bmdctl engine answers every build.
- `bmdctl engine stop`: Stop the running engine; its sessions say goodbye to their switchers.
- `bmdctl engine status`: Show the engine's version, build, pid, socket, log and sessions.
- `bmdctl hosts [list]`: List the hosts.
- `bmdctl hosts add ADDRESS [--as NAME] [--device D]`: Add the device at ADDRESS by the name it gives itself, as the device that answers there (an ATEM, HyperDeck, Videohub or Ultimatte; --device says it instead): an ATEM's name as set in ATEM Setup, else its model; the others' as they report it. --as gives it another, for two devices with one name. An address added again is asked again.
- `bmdctl hosts discover [--for D]`: Add every switcher that announces itself on the network (Bonjour, _switcher_ctrl._udp) within D (2s), by its own name.
- `bmdctl hosts rm NAME`: Forget a host.
- `bmdctl hosts default [NAME]`: Show the default host, or make NAME the default and the host commands go to now.
- `bmdctl exec FILE|- [--continue]`: Run one command per line, in order, to one host. Every line is checked before the first is sent; it stops at the first failure unless --continue.
- `bmdctl completion bash|zsh|fish`: Print a shell completion script: commands, flags, host names and, while the engine runs, the switcher's input names and the indexes it has, counted from 1.
- `bmdctl man [--dir DIR]`: Print the bmdctl(1) manual page, or write it to DIR/bmdctl.1.
- `bmdctl doctor`: Check the engine, its socket and log, the hosts file and that each host answers, and say what is misconfigured. Exit 1 for a misconfiguration, 2 when a host only didn't answer.

### Conventions

- Arguments: A command's required arguments follow its words in the order they're said: `program 5`, `usk 1 fill cam3`, `media upload 10 logo.png`. Optional ones are --name flags, which may be shortened to any unambiguous prefix (--sat for --saturation); `[file]` in a syntax may be left out.
- Devices: A command without a device word goes to the device type of its host, an ATEM for an address no host has; a command that device doesn't have says what the host is (`DECK 1 is a HyperDeck; bmdctl hyperdeck --help`). `bmdctl atem ...` and `bmdctl hyperdeck ...` name the device. `connect ADDRESS` asks what answers there, an ATEM (UDP 9910), a HyperDeck (TCP 9993), a Videohub (TCP 9990) or an Ultimatte (TCP 9998), all at once for 2 s, and saves it by its own name; --type says it instead. `bmdctl help` lists the device groups hosts.yml has a host of.
- Aliases: Run as atemctl, hdctl, vhctl or ultctl, bmdctl is that device's command: `hdctl play` is `bmdctl hyperdeck play`, `atemctl cut` is `bmdctl atem cut`, and help lists that device's commands alone. Built-ins (version, hosts, exec, doctor, ...) are bmdctl's. Releases ship them as symlinks to bmdctl (copies on Windows), and install.sh links them.
- Hosts: A host goes by the name its switcher gives itself, unless --as gave it one. When a switcher is renamed in ATEM Setup, the next command to it renames its host in hosts.yml and says so; --host still takes the name it had.
- Output: A command that changes something prints one line: the switcher as `name (address)`, the command and where (ME1, `usk 2`), what changed as before → after, `confirmed` (the switcher reported it back) or `sent` (it has no such report), and the time. A command that reads prints a line naming the switcher, its model and video mode, then what it read. Opening a session prints `connecting to ...` and then the model and video mode on stderr. --json, --quiet and a pipe get exactly that. At a terminal the switcher is named once, in a bold header that also says what is on program and preview (and, when the command opened the session, the model, video mode and how long connecting took); each result is indented under it as the verb, before → after and the time, right-aligned; `confirmed` is never printed, and `sent`, `rejected` (the switcher kept what it held) take the time's place. Nothing is wider than 72 columns. Repeats collapse (`aux 1–6  PGM`, empty slots counted, not listed; --all lists them), an error is `error` then the message and, under it, what to do next, a wait of more than 200 ms shows a spinner on stderr that the result replaces (a transfer of known size, a progress bar; a profile, a line per step as it finishes), and a destructive command says what it would affect and asks [y/N] when stdin is a terminal too; -t starts each result with the time of day. Colour only on a terminal, in red, green, bold and dim alone: the value now on program (and a keyer on air) red, on preview green, the switcher's name and bmdctl's own lines bold, ids, times, hints and empty slots dim.
- Indexes: M/Es, upstream and downstream keyers, aux outputs, media and macro slots, media players, colour generators, multiviewers and their windows, SuperSources and their boxes, HyperDeck slots and EQ bands count from 1, as ATEM Software Control and the panels count them: on the command line, in --help and in results, --json included. The atem library counts them from 0, and so do the switcher state `state` and `watch` print (and `state --path` reads) and the index attributes of ATEM Software Control's XML files.
- Sources: A source is found in this order, case ignored: a number is that input (or source id); cam2, camera 2, in2 and input 2 are input 2, and mp1, bars, black (blk), col1, pgm, pvw, cf1 and aux3 the switcher's fixed sources, whatever their names; else an input's short or long name; else the one input whose name starts so. Where an alias is also an input's name the alias wins, and a terminal says so once. A source nothing names is an error listing the first inputs.
- Rates: A rate is a frame count or seconds:frames at the switcher's display rate (half the field rate from 48 fps up).

### Exit status

- 0: Done.
- 1: A usage error, or any other failure.
- 2: The switcher or the engine is unreachable.
- 3: Sent, but the switcher did not confirm it in time.
- 4: The switcher refused it (a transfer, a lock), or a profile restore recorded errors.
- 5: Destructive, and --yes was not given.

### Environment

- `BMDCTL_HOST`: The host a command goes to when --host doesn't name one.
- `BMDCTL_SOCKET`: The engine's socket (a pipe name on Windows), instead of $XDG_RUNTIME_DIR/bmdctl.sock.
- `XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_RUNTIME_DIR`: Where hosts.yml, the engine's log and last host, and the socket live.
- `NO_COLOR`: Set to anything: no colour, even on a terminal, as --no-color.
- `TERM`: `dumb`: no colour and no spinner.

### Files

- `~/.config/bmdctl/hosts.yml`: The named hosts and the default.
- `~/.local/state/bmdctl/engine.log`: The engine's log, rotated at 5 MiB, three old files kept.
- `~/.local/state/bmdctl/last-host`: The host the last --host named.
- `$XDG_RUNTIME_DIR/bmdctl.sock`: The engine's socket; a lock file beside it.

## atem

### `bmdctl atem audio compressor <source>`

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

### `bmdctl atem audio eq <source> <band>`

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

### `bmdctl atem audio expander <source>`

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

### `bmdctl atem audio levels <enable>`

Start or stop the meter streams, again after every reconnect; on a switcher with the classic mixer, sends classic-audio levels.

```
bmdctl audio levels on
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | stream meters |

### `bmdctl atem audio limiter <source>`

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

### `bmdctl atem audio master`

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

### `bmdctl atem audio master-compressor`

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

### `bmdctl atem audio master-eq <band>`

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

### `bmdctl atem audio master-limiter`

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

### `bmdctl atem audio meters`

Read the audio meters: each strip's and the master's levels in dB, from whichever mixer the switcher has.

```
bmdctl audio meters
bmdctl audio meters --for 3
```

| argument | value | | |
|---|---|---|---|
| `--for` | float, seconds | default 0.5 | how long to collect levels; the last of each strip's is shown |

### `bmdctl atem audio strip <source>`

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

### `bmdctl atem auto`

Run the M/E's transition with its current style and rate.

```
bmdctl auto
bmdctl auto --me 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem auto-video-mode <enable>`

Turn automatic input video mode detection on or off.

```
bmdctl auto-video-mode on
```

**Unverified.** Echo: `AiVM`.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | detect the input video mode |

### `bmdctl atem aux <aux> <source>`

Route a source to an aux output.

```
bmdctl aux 1 pgm
bmdctl aux 2 cam4
bmdctl aux 3 cf1
```

Echo: `AuxS:<aux>`.

| argument | value | | |
|---|---|---|---|
| `<aux>` | uint, 1-256, counted from 1 | required | aux output |
| `<source>` | source, 0-65535 | required | source id or input name |

### `bmdctl atem camera <destination> <category> <parameter>`

Send one Blackmagic camera control parameter.

```
bmdctl camera 1 0 3 --type fixed16 --data 0.5
bmdctl camera 2 1 2 --type int16 --data 5600,0
bmdctl camera 1 0 0 --relative --type fixed16 --data 0.05
```

**Unverified.** Echo: `CCdP:<destination>:<category>:<parameter>`.

| argument | value | | |
|---|---|---|---|
| `<destination>` | uint, 0-255 | required | camera; 255 for all |
| `<category>` | uint, 0-255 | required | category |
| `<parameter>` | uint, 0-255 | required | parameter |
| `--relative` | bool | default false | add to the current value |
| `--type` | enum, 0-255: bool=0 int8=1 int16=2 int32=3 int64=4 string=5 fixed16=128 | default bool | data type |
| `--data` | list | optional | comma-separated values of the data type |

### `bmdctl atem classic-audio input <source>`

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

### `bmdctl atem classic-audio levels <enable>`

Start or stop the classic mixer's meter stream.

```
bmdctl classic-audio levels on
```

**Unverified.** No echo.

| argument | value | | |
|---|---|---|---|
| `<enable>` | bool | required | stream meters |

### `bmdctl atem classic-audio master`

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

### `bmdctl atem classic-audio meters`

Read the classic audio mixer's meters: each input's and the master's levels in dB.

```
bmdctl classic-audio meters
bmdctl classic-audio meters --for 3
```

| argument | value | | |
|---|---|---|---|
| `--for` | float, seconds | default 0.5 | how long to collect levels; the last of each strip's is shown |

### `bmdctl atem classic-audio monitor`

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

### `bmdctl atem color <generator>`

Set a colour generator.

```
bmdctl color 1 --hue 210 --saturation 80 --luma 40
bmdctl color 2 --sat 0 --luma 100
```

Give at least one flag. Echo: `ColV:<generator>`.

| argument | value | | |
|---|---|---|---|
| `<generator>` | uint, 1-2, counted from 1 | required | colour generator |
| `--hue` | float, degrees, not clamped | optional | hue |
| `--saturation` | float, %, clamped to 0-100 | optional | saturation |
| `--luma` | float, %, clamped to 0-100 | optional | luma |

### `bmdctl atem connect [address]`

Connect to a switcher and keep the session open; an address is asked what it is and saved as the default host.

```
bmdctl connect 192.168.10.240
bmdctl connect
bmdctl connect --pin off
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (asked what answers there, then saved to hosts.yml by the name it gives itself, as the default) or a host's name; else the default |
| `--pin` | bool | default true | keep the session open until disconnect; false lets it close after 10 idle minutes |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

### `bmdctl atem cut`

Cut program and preview.

```
bmdctl cut
bmdctl cut --me 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem disconnect`

Close the engine's session to the switcher, pinned or not.

```
bmdctl disconnect
```

### `bmdctl atem dsk auto`

Run a downstream keyer's auto transition.

```
bmdctl dsk auto
bmdctl dsk auto --dsk 2
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

### `bmdctl atem dsk fill <source>`

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

### `bmdctl atem dsk gain`

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

### `bmdctl atem dsk key <source>`

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

### `bmdctl atem dsk mask`

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

### `bmdctl atem dsk on-air <on-air>`

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

### `bmdctl atem dsk rate <rate>`

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

### `bmdctl atem dsk tie <tie>`

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

### `bmdctl atem dsk toggle`

Put a downstream keyer on air if it is off, or off if it is on.

```
bmdctl dsk toggle
bmdctl dsk toggle --dsk 2
```

Echo: `DskS:<dsk>`.

| argument | value | | |
|---|---|---|---|
| `--dsk` | uint, 1-256, counted from 1 | default 1 | downstream keyer |

### `bmdctl atem ftb enable <enabled>`

Make fade to black available or not (always M/E 1; see docs/PORTING.md).

```
bmdctl ftb enable on
```

Echo: `FEna`.

| argument | value | | |
|---|---|---|---|
| `<enabled>` | bool | required | available |

### `bmdctl atem ftb rate <rate>`

Set the fade-to-black rate.

```
bmdctl ftb rate 1:00
```

Echo: `FtbP:<me>`.

| argument | value | | |
|---|---|---|---|
| `<rate>` | rate, 0-255, frames, or seconds:frames at the display rate | required | rate |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem ftb toggle`

Fade to black, or back.

```
bmdctl ftb toggle
```

No echo.

| argument | value | | |
|---|---|---|---|
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem hyperdeck-slot <slot>`

Bind a HyperDeck slot to an address and an input.

```
bmdctl hyperdeck-slot 1 --address 192.168.10.50 --input cam4
bmdctl hyperdeck-slot 2 --address 0.0.0.0
```

Give at least one flag. Echo: `RXMS:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-10, counted from 1 | required | HyperDeck slot |
| `--address` | string | optional | deck IPv4 address; 0.0.0.0 clears the slot |
| `--input` | source, 0-65535 | optional | switcher input the deck feeds |

### `bmdctl atem info`

Show the switcher's model, firmware, video mode and what it has.

```
bmdctl info
```

### `bmdctl atem input rename <source>`

Rename an input.

```
bmdctl input rename cam1 --long-name "Stage Left" --short-name STGL
bmdctl input rename 2 --short-name WIDE
```

Give at least one flag. Echo: `InPr:<source>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--long-name` | string, up to 20 bytes | optional | long name |
| `--short-name` | string, up to 4 bytes | optional | short name |
| `--port-type` | uint, 0-65535 | optional | external port type (no atemwire operation sets it) |

### `bmdctl atem inputs`

List the switcher's sources, one per line: id, short name, long name and port type.

```
bmdctl inputs
bmdctl inputs --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | at a terminal, the outputs, key masks and media player keys too (a pipe and --json always have them) |

### `bmdctl atem macro action <action>`

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

### `bmdctl atem macro decode <file>`

Turn macro bytecode into XML ops (JSON with --json).

```
bmdctl macro decode open.bin
bmdctl macro decode open.bin --json
```

Runs in the CLI; needs no switcher.

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the bytecode; - reads stdin |

### `bmdctl atem macro delete <slot>`

Delete a macro.

```
bmdctl macro delete 4
bmdctl macro delete "Lower third in"
```

**Destructive.** Echo: `MPrp:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | string | required | macro slot, or the macro's name |

### `bmdctl atem macro download <slot> [file]`

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

### `bmdctl atem macro encode <file> <out>`

Turn XML or JSON ops into macro bytecode.

```
bmdctl macro encode open.xml open.bin
```

Runs in the CLI; needs no switcher.

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the ops; - reads stdin |
| `<out>` | file, written | required | where to write the bytecode; - is stdout |

### `bmdctl atem macro list`

List the macros: slot, name, description; and what runs or records.

```
bmdctl macro list
bmdctl macro list --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | include empty slots |

### `bmdctl atem macro record <slot> <name>`

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

### `bmdctl atem macro run <slot>`

Run a macro.

```
bmdctl macro run 1
bmdctl macro run "Lower third in"
```

Echo: `MRPr`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | string | required | macro slot, or the macro's name |

### `bmdctl atem macro sleep <frames>`

Record a wait into the macro being recorded.

```
bmdctl macro sleep 25
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<frames>` | uint, 0-4294967295 | required | frames to wait |

### `bmdctl atem macro stop`

Stop the macro that is running.

```
bmdctl macro stop
```

Echo: `MRPr`.

### `bmdctl atem macro upload <slot> <file>`

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

### `bmdctl atem media capture`

Capture program into the next free still slot.

```
bmdctl media capture
```

No echo.

### `bmdctl atem media clear <slot>`

Empty a still slot, under the media pool's lock.

```
bmdctl media clear 5
```

**Destructive.** Echo: `MPfe:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-256, counted from 1 | required | still slot |

### `bmdctl atem media clear-unlocked <slot>`

Clear a still slot without taking the store lock (some models ignore this; media clear takes it).

```
bmdctl media clear-unlocked 5
```

**Destructive.** Echo: `MPfe:<slot>`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-256, counted from 1 | required | still slot |

### `bmdctl atem media download <slot> [file]`

Download a still slot as a PNG.

```
bmdctl media download 3
bmdctl media download 3 logo.png
```

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-65536, counted from 1 | required | still slot |
| `[file]` | file, written | optional | where to write it; - is stdout (default: the slot's name .png) |

### `bmdctl atem media list`

List the media pool's stills and what each media player shows.

```
bmdctl media list
bmdctl media list --all
```

| argument | value | | |
|---|---|---|---|
| `--all` | bool | default false | at a terminal, the empty slots too (a pipe and --json always have them) |

### `bmdctl atem media player <player>`

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

### `bmdctl atem media upload <slot> <file>`

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

### `bmdctl atem multiviewer input <window> <source>`

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

### `bmdctl atem multiviewer properties`

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

### `bmdctl atem preview <source>`

Put a source on an M/E's preview bus.

```
bmdctl preview cam3
bmdctl preview col1 --me 2
```

Echo: `PrvI:<me>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem profile images`

Download every used still as a PNG, named as profiles name them.

```
bmdctl profile images
bmdctl profile images --dir stills/
```

| argument | value | | |
|---|---|---|---|
| `--dir` | dir | default ATEM Media Pool | where to write them |

### `bmdctl atem profile restore <file>`

Apply a profile XML to the switcher, and upload the images it names.

```
bmdctl profile restore show.xml
bmdctl profile restore show.xml --program --preview
bmdctl profile restore show.xml --macros-only
```

**Destructive.**

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the profile XML; its images are read from beside it |
| `--mes` | string | default all | the M/Es the M/E sections cover: all, or a list from 1 (1,2) |
| `--program` | bool | default false | each M/E's program source |
| `--preview` | bool | default false | each M/E's preview source |
| `--next-transition` | bool | default true | each M/E's next-transition layers |
| `--transition-style` | bool | default true | each M/E's transition style and its settings |
| `--ftb` | bool | default true | each M/E's fade to black |
| `--keyers` | string | default all | the upstream keyers: all, none, or a list from 1 (1,2) |
| `--macros-only` | bool | default false | start from nothing but the macros (ApplyOptions.macros_only), then add flags given |
| `--video-mode` | bool | default true | change the video mode if it differs: outputs resync |
| `--inputs` | bool | default true | input names |
| `--settings-flags` | bool | default true | settings flags (fade to black enabled) |
| `--color-generators` | bool | default true | colour generators |
| `--aux` | bool | default true | aux outputs |
| `--hyperdecks` | bool | default true | HyperDeck slots |
| `--downstream-keys` | bool | default true | downstream keyers |
| `--fly-keyframes` | bool | default false | drive each keyer through its saved keyframes to store them (shows on air) |
| `--media-players` | bool | default true | media players |
| `--audio` | bool | default true | the Fairlight mixer: master, strips, EQ, dynamics |
| `--macros` | bool | default true | the macro pool, uploading each macro |
| `--media-pool-images` | bool | default true | upload the stills the XML names, from "ATEM Media Pool" beside it |
| `--multiview` | bool | default false | no setter exists: noted as skipped |
| `--camera-control` | bool | default false | no setter exists: noted as skipped |
| `--talkback` | bool | default false | no setter exists: noted as skipped |

### `bmdctl atem profile save <file>`

Save the switcher's setup as ATEM Software Control's profile XML, and its media pool images.

```
bmdctl profile save show.xml
bmdctl profile save show.xml --media-pool-images off
```

| argument | value | | |
|---|---|---|---|
| `<file>` | file, written | required | the profile XML to write; images go beside it |
| `--mes` | string | default all | the M/Es the M/E sections cover: all, or a list from 1 (1,2) |
| `--program` | bool | default true | each M/E's program source |
| `--preview` | bool | default true | each M/E's preview source |
| `--next-transition` | bool | default true | each M/E's next-transition layers |
| `--transition-style` | bool | default true | each M/E's transition style and its settings |
| `--ftb` | bool | default true | each M/E's fade to black |
| `--keyers` | string | default all | the upstream keyers: all, none, or a list from 1 (1,2) |
| `--downstream-keys` | bool | default true | <DownstreamKeys> |
| `--color-generators` | bool | default true | <ColorGenerators> |
| `--fairlight` | bool | default true | <FairlightAudioMixer> |
| `--camera-control` | bool | default true | <CameraControl> |
| `--media-pool` | bool | default true | <MediaPool>: the stills' names and paths |
| `--media-pool-images` | bool | default true | each used still as a PNG in "ATEM Media Pool" beside the XML |
| `--media-players` | bool | default true | <MediaPlayers> |
| `--aux` | bool | default true | <Auxiliaries> |
| `--counters` | bool | default true | <Counters> |
| `--settings` | bool | default true | <Settings>, input names included |
| `--video-mode` | bool | default true | <VideoMode> |
| `--hyperdecks` | bool | default true | <HyperDecks> |
| `--macros` | bool | default true | <MacroPool> and <MacroControl>, downloading each macro |

### `bmdctl atem program <source>`

Put a source on an M/E's program bus.

```
bmdctl program cam2
bmdctl program "Stage Left"
bmdctl program mp1 --me 2
```

Echo: `PrgI:<me>`.

| argument | value | | |
|---|---|---|---|
| `<source>` | source, 0-65535 | required | source id or input name |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem recording settings`

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

### `bmdctl atem recording state <recording>`

Start or stop recording.

```
bmdctl recording state on
```

**Unverified.** Echo: `RTMS`.

| argument | value | | |
|---|---|---|---|
| `<recording>` | bool | required | recording |

### `bmdctl atem show <area>`

Show one area of the switcher: keys, dsk, transition, audio, media, macros, aux or hyperdecks.

```
bmdctl show keys
bmdctl show transition
bmdctl show audio
```

| argument | value | | |
|---|---|---|---|
| `<area>` | enum: keys=0 dsk=1 transition=2 audio=3 media=4 macros=5 aux=6 hyperdecks=7 | required | keys, dsk, transition, audio, media, macros, aux or hyperdecks |
| `--all` | bool | default false | media and macros: the empty slots too, at a terminal |

### `bmdctl atem startup clear`

Clear the saved power-on state.

```
bmdctl startup clear
```

**Unverified.** **Destructive.** No echo.

### `bmdctl atem startup save`

Make the current state the power-on state.

```
bmdctl startup save
```

**Unverified.** **Destructive.** No echo.

### `bmdctl atem state`

Print the switcher's state as the session holds it, or one part of it.

```
bmdctl state
bmdctl state --path mes.0.program
bmdctl state --path fairlight.master
```

| argument | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: mes.0.program, inputs.1.name, fairlight.master |

### `bmdctl atem status`

Show what is on air: program, preview, transitions, keyers, macros.

```
bmdctl status
```

### `bmdctl atem streaming audio-bitrate <min> <max>`

Set the streaming audio bitrate.

```
bmdctl streaming audio-bitrate 128000 192000
```

**Unverified.** Echo: `STAB`.

| argument | value | | |
|---|---|---|---|
| `<min>` | uint, 0-4294967295, bit/s | required | lowest |
| `<max>` | uint, 0-4294967295, bit/s | required | highest |

### `bmdctl atem streaming service`

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

### `bmdctl atem streaming state <streaming>`

Start or stop streaming.

```
bmdctl streaming state on
```

**Unverified.** Echo: `StRS`.

| argument | value | | |
|---|---|---|---|
| `<streaming>` | bool | required | streaming |

### `bmdctl atem supersource box <box>`

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

### `bmdctl atem supersource properties`

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

### `bmdctl atem tbar <position>`

Move the T-bar.

```
bmdctl tbar 5000
bmdctl tbar 10000 --me 2
```

**Unverified.** Echo: `TrPs:<me>`.

| argument | value | | |
|---|---|---|---|
| `<position>` | int, 0-10000, clamped | required | T-bar position |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |

### `bmdctl atem time`

Read the switcher's clock: ask for it (TiRq) and wait for the Time packet.

```
bmdctl time
```

Echo: `Time`.

### `bmdctl atem time-of-day`

Set the switcher's clock.

```
bmdctl time-of-day
bmdctl time-of-day --time 2026-10-11T19:30:00Z
```

**Unverified.** No echo.

| argument | value | | |
|---|---|---|---|
| `--time` | time | default now | time |

### `bmdctl atem time-request`

Ask the switcher for its time (the reply is the Time packet).

```
bmdctl time-request
```

No echo.

### `bmdctl atem transition dip`

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

### `bmdctl atem transition dve`

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

### `bmdctl atem transition mix <rate>`

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

### `bmdctl atem transition next`

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

### `bmdctl atem transition preview <enabled>`

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

### `bmdctl atem transition rate <rate>`

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

### `bmdctl atem transition settings`

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

### `bmdctl atem transition stinger`

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

### `bmdctl atem transition style <style>`

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

### `bmdctl atem transition wipe`

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

### `bmdctl atem undo`

Undo the last program, preview, cut, auto, aux, keyer or DSK on-air change made from this machine.

```
bmdctl undo
```

### `bmdctl atem usk <keyer> chroma`

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

### `bmdctl atem usk <keyer> chroma-sample`

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

### `bmdctl atem usk <keyer> dve`

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

### `bmdctl atem usk <keyer> fill <source>`

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

### `bmdctl atem usk <keyer> key <source>`

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

### `bmdctl atem usk <keyer> keyframe-run <to>`

Fly the key to a keyframe.

```
bmdctl usk 1 keyframe-run a
bmdctl usk 1 keyframe-run full
bmdctl usk 1 keyframe-run runToInfinite --direction 3
```

No echo.

| argument | value | | |
|---|---|---|---|
| `<keyer>` | uint, 1-256, counted from 1 | required | upstream keyer |
| `<to>` | enum, 0-255: a=1 b=2 full=3 runToInfinite=4 | required | keyframe to fly to |
| `--me` | uint, 1-256, counted from 1 | default 1 | M/E |
| `--direction` | uint, 0-255 | optional | direction for runToInfinite; atemwire's two encodings agree on 1-4 and 6-9 (1 top left ... 9 bottom right) and disagree on the centre (5 or 0) |

### `bmdctl atem usk <keyer> keyframe-set <keyframe>`

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

### `bmdctl atem usk <keyer> luma`

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

### `bmdctl atem usk <keyer> mask`

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

### `bmdctl atem usk <keyer> on-air <on-air>`

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

### `bmdctl atem usk <keyer> pattern`

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

### `bmdctl atem usk <keyer> toggle`

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

### `bmdctl atem usk <keyer> type`

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

### `bmdctl atem video-mode <mode>`

Change the video mode: outputs resync and clients may be dropped.

```
bmdctl video-mode 1080p50
bmdctl video-mode 1080i5994
```

**Destructive.** Echo: `VidM`.

| argument | value | | |
|---|---|---|---|
| `<mode>` | enum, 0-255: 525i5994=0 NTSC=0 625i50=1 PAL=1 NTSC_widescreen=2 PAL_widescreen=3 720p50=4 720p5994=5 1080i50=6 1080i5994=7 1080p2398=8 1080p24=9 1080p25=10 1080p2997=11 1080p50=12 1080p5994=13 2160p2398=14 2160p24=15 2160p25=16 2160p2997=17 2160p50=18 2160p5994=19 4320p2398=20 4320p24=21 4320p25=22 4320p2997=23 4320p50=24 4320p5994=25 1080p30=26 1080p60=27 720p60=28 1080i60=29 | required | video mode |

### `bmdctl atem video-modes`

List the video modes the switcher supports, the current one marked.

```
bmdctl video-modes
```

### `bmdctl atem watch`

Print each change the switcher reports, as it comes.

```
bmdctl watch
bmdctl watch --filter PrgI,PrvI
bmdctl watch --filter PrgI --count 1
```

Runs until interrupted.

| argument | value | | |
|---|---|---|---|
| `--filter` | list | optional | only keys starting with one of these: PrgI,KeOn:0 |
| `--count` | uint | optional | stop after this many changes |
| `--all` | bool | default false | include the clock's Time packets and the index and Fairlight tallies, left out unless --filter names them |

## hyperdeck

### `bmdctl hyperdeck clips`

List the timeline's clips, or a disk's.

```
bmdctl clips
bmdctl clips --disk
bmdctl clips --slot 2
```

| argument | value | | |
|---|---|---|---|
| `--disk` | bool | default false | the active slot's disk, not the timeline |
| `--slot` | uint | optional | this slot's disk (implies --disk) |

### `bmdctl hyperdeck connect [address]`

Connect to a HyperDeck and keep the session open; an address is saved as the default host.

```
bmdctl connect 192.168.81.90
bmdctl connect
bmdctl connect --pin false
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (saved to hosts.yml by the name the deck gives itself, as the default) or a host's name; else the default |
| `--pin` | bool | default true | keep the session open until disconnect; false lets it close after 10 idle minutes |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

### `bmdctl hyperdeck disconnect`

Close the engine's session to the deck, pinned or not.

```
bmdctl disconnect
```

### `bmdctl hyperdeck goto <clip>`

Go to a timeline clip or a timecode, without playing.

```
bmdctl goto 2
bmdctl goto interview
bmdctl goto 00:01:30:00
```

| argument | value | | |
|---|---|---|---|
| `<clip>` | string | required | a timeline clip: its number, or its name or the start of it, in any case; or a timecode, HH:MM:SS:FF |

### `bmdctl hyperdeck loop <on>`

Turn looping on or off for the playback under way.

```
bmdctl loop on
bmdctl loop off
```

**Unverified.**

| argument | value | | |
|---|---|---|---|
| `<on>` | bool | required | on loops the timeline, or the clip with play --single-clip |

### `bmdctl hyperdeck next`

Go to the start of the next clip on the timeline.

```
bmdctl next
```

**Unverified.** Echo: `200 ok`.

### `bmdctl hyperdeck play`

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

### `bmdctl hyperdeck prev`

Go to the start of the previous clip on the timeline.

```
bmdctl prev
```

**Unverified.** Echo: `200 ok`.

### `bmdctl hyperdeck record [name]`

Start recording on the active slot, into a clip named as given.

```
bmdctl record
bmdctl record "Interview 2"
```

**Unverified.** Echo: `200 ok`.

| argument | value | | |
|---|---|---|---|
| `[name]` | string | optional | the clip's name; the deck names it from its record prefix otherwise |

### `bmdctl hyperdeck slot <slot>`

Make a slot the active one: its disk is what plays and records.

```
bmdctl slot 2
```

Echo: `200 ok`.

| argument | value | | |
|---|---|---|---|
| `<slot>` | uint, 1-255 | required | the slot, as the deck numbers them: 1 or 2 on a HyperDeck Studio |

### `bmdctl hyperdeck state`

Print what the session holds of the deck, or one part of it.

```
bmdctl state
bmdctl state --path transport.status
```

| argument | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: transport.status, slots.1 |

### `bmdctl hyperdeck status`

Show the transport, timecode, clip, slots and recording time left.

```
bmdctl status
```

### `bmdctl hyperdeck stop`

Stop playback or recording; the deck holds the last frame.

```
bmdctl stop
```

Echo: `200 ok`.

### `bmdctl hyperdeck watch`

Print each change the deck reports, as it comes.

```
bmdctl watch
bmdctl watch --count 5
bmdctl watch --json
```

Runs until interrupted.

| argument | value | | |
|---|---|---|---|
| `--count` | uint | optional | stop after this many changes |
