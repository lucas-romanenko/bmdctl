# bmdctl guide

This walks through bmdctl one task at a time. Each section shows a
command and what it prints at a terminal. The examples use one switcher,
STUDIO A at 192.168.1.20 (an ATEM 1 M/E Constellation HD at 1080p25),
and one HyperDeck, Deck 1 at 192.168.1.50; your names, sources and
times will differ.

Colour doesn't show here. At a terminal the value now on program is red,
the value now on preview green, the switcher's name bold, and ids,
times and hints dim. Piped into another program, a command prints the
plain line format instead, and `--json` prints the engine's JSON
([Scripting](#scripting)). [COMMANDS.md](COMMANDS.md) lists every
command and flag; `bmdctl <command> --help` prints one with examples.

- [Hosts](#hosts)
- [Connecting](#connecting)
- [Sources](#sources)
- [Switching](#switching)
- [Keys and DSKs](#keys-and-dsks)
- [Aux outputs](#aux-outputs)
- [Media pool](#media-pool)
- [Macros](#macros)
- [Profiles](#profiles)
- [watch](#watch)
- [exec: cue files](#exec-cue-files)
- [undo](#undo)
- [--dry-run](#--dry-run)
- [-t: timestamps](#-t-timestamps)
- [HyperDeck](#hyperdeck)
- [Scripting](#scripting)
- [The engine](#the-engine)
- [serve: the HTTP API](#serve-the-http-api)
- [MCP: Claude Code and Claude Desktop](#mcp-claude-code-and-claude-desktop)

## Hosts

bmdctl keeps the devices it knows in `hosts.yml`
(`~/.config/bmdctl/hosts.yml`; `%AppData%\bmdctl\hosts.yml` on Windows).
You don't name them: a host goes by the name the device gives itself,
which for an ATEM is the name set in ATEM Setup, else its model, and for
a HyperDeck the name set in HyperDeck Setup.

`hosts add ADDRESS` asks what answers at the address (an ATEM on UDP
9910, a HyperDeck on TCP 9993, a Videohub on TCP 9990 or an Ultimatte on
TCP 9998, all at once, for 2 seconds) and saves it by its name:

```
$ bmdctl hosts add 192.168.1.20
added STUDIO A (192.168.1.20)
$ bmdctl hosts add 192.168.1.50
added Deck 1 (192.168.1.50)
$ bmdctl hosts default "STUDIO A"
default host: STUDIO A
$ bmdctl hosts
STUDIO A  192.168.1.20  atem       default
Deck 1    192.168.1.50  hyperdeck
```

- **Two devices with one name** (two switchers of one model with no
  ATEM Setup name both call themselves by the model): give the second
  one a name with `--as`. It goes by that name from then on.

  ```
  $ bmdctl hosts add 192.168.1.21 --as "STUDIO B"
  added STUDIO B (192.168.1.21), which calls itself "ATEM 1 M/E Constellation HD"
  ```

- **A short name:** a device's own name can be awkward to type. `hosts
  alias NAME SHORT` gives it a short one it goes by as well, wherever a
  name is taken; `bmdctl` and `hosts` show it before the name.

  ```
  $ bmdctl hosts alias "BC 1.2 - TESTTt" bc
  alias bc: BC 1.2 - TESTTt (192.168.1.22)
  $ bmdctl --host bc cut
  ```

  An alias is one word, and can't be a command's (`cut`, `hosts`) or
  another host's name or alias. Another alias replaces it; `hosts alias
  bc --rm` takes it away.
- **Renamed in ATEM Setup:** the next command to that switcher renames
  its host in hosts.yml and says so once. `--host` still takes the old
  name, so scripts keep working.
- **Which host a command goes to:** `--host`, else `$BMDCTL_HOST`, else
  the host the last `--host` named, else the default. `--host` takes a
  name or an alias in any case, any unambiguous start of one (`--host
  "studio b"`, `--host deck`), the name a host had before a rename, or an
  address.
- **The host as the first word:** `bmdctl bc cut` is `bmdctl --host bc
  cut`, for that command alone: the next command without a host goes
  where it went before. The first word takes what `--host` does, except
  a DNS name no host has (a mistyped command would be dialled): an IP
  address works. A word that is a command is always the command, so a
  host called `info` needs `--host info`; `--host`, given too, wins. A
  first word that is neither says so and lists the hosts:

  ```
  $ bmdctl progam 2
  error  no command or host named "progam"
         hosts: bc · studio · deck · bmdctl --help lists the commands
  ```

  In an `exec` file, a line's first word names its host the same way.
- **Changing the default:** `bmdctl use HOST` makes a host the one
  commands without a host go to, without connecting (`connect HOST` also
  pins a session). `bmdctl` marks it `*`. `$BMDCTL_HOST`, when set,
  still wins, and `use` says so.

  ```
  $ bmdctl use bc
  default host: BC 1.2 - TESTTt
  ```
- **`hosts discover`** adds every ATEM that announces itself over
  Bonjour within 2 seconds (`--for 5s` waits longer). Bonjour doesn't
  cross VLANs or VPNs; there, add switchers by address.
- **`hosts rm NAME`** forgets one.

The file is plain YAML and safe to edit or keep in git:

```yaml
version: 2
default: STUDIO A
hosts:
  - name: STUDIO A
    address: 192.168.1.20
    device: atem
  - name: Deck 1
    address: 192.168.1.50
    device: hyperdeck
```

## Connecting

You don't have to connect: the first command to a switcher opens a
session, through the background engine ([The engine](#the-engine)), and
later commands reuse it. A session closes after 10 minutes without a
command. `connect ADDRESS` does what `hosts add` does, makes the switcher
the default, and pins the session open until `disconnect`, or until an
hour passes without a command:

```
$ bmdctl connect 192.168.1.20
STUDIO A  PGM CAM2 · PVW CAM1 · 1080p25 · connected 328 ms
  saved as "STUDIO A", now the default
  tab completes input names
```

The first line is the header every command starts with: the switcher's
name, then what was on program and preview when the command ran, and,
when this command opened the session, the video mode and how long it
took. `connect` alone pins the default host's session; `connect --pin
off` opens it without pinning.

```
$ bmdctl connect
STUDIO A  PGM CAM2 · PVW CAM1
  connect     pinned until disconnect or 1h idle
$ bmdctl disconnect
STUDIO A  disconnected
```

`bmdctl` alone lists the hosts and their sessions, and never starts the
engine. `bmdctl status` is the switcher at a glance:

```
$ bmdctl
bmdctl v0.1.0

* STUDIO A  192.168.1.20  ATEM 1 M/E Constellation HD  1080p25      ● 4m

  * default · bmdctl use HOST · bmdctl help
$ bmdctl status
STUDIO A  1080p25 · session 4m, idle 12s, pinned

  ME 1        PGM  CAM2           PVW  CAM1
              mix 1:00 · next background · FTB off
  keys        1  luma  CAM4 / CAM4                                   off
              2  luma  MP1 / MP1K                                    off
              3  luma  MP1 / MP1K                                    off
              4  luma  MP1 / MP1K                                    off
  dsk 1       MP2 / MP2K                                          on air
  dsk 2       COL1 / COL1                                            off
  aux 1–6     PGM
  macro       idle
```

`bmdctl show keys`, `show dsk`, `show transition`, `show audio`, `show
media`, `show macros`, `show aux` and `show hyperdecks` each lay out one
area in more detail.

## Sources

Anything that takes a source (program, preview, aux, key fills, the
multiviewer) takes it several ways. bmdctl tries them in this order,
ignoring case:

| You type | It means |
|---|---|
| `2` | source id 2, input 2. Any id works: `1000` is bars, `3010` media player 1 |
| `cam2`, `camera 2`, `in2`, `input 2` | input 2, whatever it is called |
| `mp1`, `mp2` | media player 1 or 2 |
| `bars`, `black` (`blk`) | colour bars, black |
| `col1`, `col2` | colour generator 1 or 2 |
| `pgm`, `pvw` | M/E 1's program and preview outputs |
| `cf1`, `aux3` | clean feed 1, aux output 3 |
| `CAM2`, `"Camera 2"` | the input whose short or long name that is |
| `stage` | the one input whose name starts that way |

An alias wins over an input's label. If you have renamed input 1 to
"cam2", `cam2` still means input 2; bmdctl says so once, and `1` reaches
the input you named. A name two inputs share, or a start several names
share, is refused with the candidates listed. `bmdctl inputs` lists what
the switcher has; where what you typed isn't the short name, the result
shows it in brackets:

```
$ bmdctl inputs
STUDIO A  PGM CAM2 · PVW CAM1

    1  CAM1  Camera 1                                              ○ PVW
    2  CAM2  Camera 2                                              ● PGM
    3  CAM3  Camera 3
    4  CAM4  Camera 4
       Bars  Color Bars
       COL1  Color 1
       COL2  Color 2
       MP1   Media Player 1
       MP2   Media Player 2

  bmdctl inputs --all adds outputs, masks and keys
$ bmdctl preview "Camera 3"
STUDIO A  PGM CAM2 · PVW CAM1
  preview     CAM1 → CAM3 (Camera 3)                               24 ms
$ bmdctl program cam9
error  no input "cam9"
       1 CAM1 Camera 1 · 2 CAM2 Camera 2 · 3 CA… · bmdctl inputs for all
```

Tab completes input names while the engine runs ([INSTALL.md](INSTALL.md#completion)).

## Switching

```
$ bmdctl preview cam3
STUDIO A  PGM CAM2 · PVW CAM1
  preview     CAM1 → CAM3                                          24 ms
$ bmdctl cut
STUDIO A  PGM CAM2 · PVW CAM3
  cut         CAM3 on program · CAM2 on preview                    30 ms
$ bmdctl transition mix 0:12
STUDIO A  PGM CAM3 · PVW CAM2
  transition mix  1:00 → 0:12                                      22 ms
$ bmdctl auto
STUDIO A  PGM CAM3 · PVW CAM2
  auto        CAM2 on program · CAM3 on preview                    29 ms
$ bmdctl program cam2
STUDIO A  PGM CAM2 · PVW CAM3
  program     CAM2                                               already
$ bmdctl ftb toggle
STUDIO A  PGM CAM2 · PVW CAM3
  ftb toggle  off → black                                           sent
```

A result line is the command, what changed (before → after), and how
long the switcher took to report it back. Where the time would be, three
words can appear instead:

| Status | Meaning |
|---|---|
| `already` | the switcher held that value; nothing was sent |
| `sent` | the command has no report to wait for (`ftb toggle`, `dsk auto`) |
| `rejected` | the switcher reported back but kept the value it had |

- `cut` is confirmed when the switcher reports both buses; `auto` when
  the transition starts, not when it ends.
- `transition mix 0:12` sets the mix rate. Rates are frames or
  `seconds:frames` at the switcher's display rate. `transition wipe`,
  `dip`, `dve` and `stinger` set the other styles, `transition style`
  picks one, `transition next --next background,key1` sets what the
  next transition takes.
- `tbar 5000` moves the transition halfway (0 to 10000; unverified on
  hardware, and it says so).
- `ftb toggle` fades to black and back (`sent`: the switcher reports
  nothing for it); `ftb rate 1:00` sets its rate.
- On a switcher with more M/Es, `--me 2` (counted from 1, as ATEM
  Software Control counts) picks one; the result shows it,
  `program ME2`.

## Keys and DSKs

Upstream keyers take the keyer number first, then what to do with it;
downstream keyers take `--dsk N` (1 when left out).

```
$ bmdctl usk 1 fill cam3
STUDIO A  PGM CAM2 · PVW CAM3
  usk 1 fill  CAM4 → CAM3                                          21 ms
$ bmdctl usk 1 on-air on
STUDIO A  PGM CAM2 · PVW CAM3
  usk 1 on-air  off → on                                           22 ms
$ bmdctl dsk fill mp1
STUDIO A  PGM CAM2 · PVW CAM3
  dsk fill    MP2 → MP1                                            20 ms
$ bmdctl dsk key mp1k
STUDIO A  PGM CAM2 · PVW CAM3
  dsk key     MP2K → MP1K                                          19 ms
$ bmdctl dsk auto
STUDIO A  PGM CAM2 · PVW CAM3
  dsk auto    off → on                                              sent
$ bmdctl show keys
STUDIO A  PGM CAM2 · PVW CAM3 · keys

  ME 1        1  luma  CAM3 / CAM3  premultiplied                 on air
              2  luma  MP1 / MP1K  clip 0 gain 0                     off
              3  luma  MP1 / MP1K  clip 0 gain 0                     off
              4  luma  MP1 / MP1K  clip 0 gain 0                     off
```

- `usk 1 toggle` and `dsk toggle` flip on air; `usk 1 on-air off` and
  `dsk on-air off` take a key off.
- `usk 1 type --type chroma`, then `usk 1 chroma`, `usk 1 luma`, `usk 1
  pattern` and `usk 1 dve` set each key type's settings; `usk 1 mask`
  and `dsk mask` the masks.
- A key can ride the next transition instead of cutting on:
  `transition next --next background,key1`, then `auto`.
- `dsk auto` and `dsk toggle --dsk 2` work on the second DSK.

## Aux outputs

```
$ bmdctl aux 1 pgm
STUDIO A  PGM CAM2 · PVW CAM3
  aux 1       CAM1 → PGM                                           18 ms
$ bmdctl aux 2 cam4
STUDIO A  PGM CAM2 · PVW CAM3
  aux 2       PGM → CAM4                                           19 ms
$ bmdctl show aux
STUDIO A  PGM CAM2 · PVW CAM3 · aux

  aux 1       PGM
  aux 2       CAM4
  aux 3       PGM
```

`status` collapses neighbours on one source: `aux 3–6  PGM`. Any source
works, including `pvw`, `cf1` (clean feed) and other aux outputs.

## Media pool

```
$ bmdctl media list
STUDIO A  PGM CAM2 · PVW CAM3 · 20 stills

   1  Logo     MP1
   2  (empty)  MP2
  18 empty slots hidden · --all shows them
$ bmdctl media upload 3 sponsor.png
STUDIO A  PGM CAM2 · PVW CAM3
  upload      slot 3  sponsor  1920×1080                           1.1 s
$ bmdctl media player 2 --still 3
STUDIO A  PGM CAM2 · PVW CAM3
  media player 2  still 2 → 3                                      18 ms
$ bmdctl media download 3 back.png
STUDIO A  PGM CAM2 · PVW CAM3
  download    slot 3  sponsor → back.png  1920×1080               573 ms
```

- **Upload** takes a PNG, JPEG or GIF and fits it to the video mode's
  frame, keeping its shape (bars where it doesn't fill). A PNG's colours
  are premultiplied by its alpha, as ATEM Software Control does
  (`--premultiply off` turns that off). The slot is named after the file
  unless `--name "Main Sponsor"`. While it runs, a progress bar on stderr
  shows `uploading sponsor.png → slot 3 █████░░░ 68%  816 KB  0.8 s`,
  and the result line replaces it.
- **Slots count from 1**, as ATEM Software Control shows them.
- **Download** writes a PNG; without a file name it is named after the
  slot.
- **Replacing a used slot, and clearing one, ask first.** bmdctl says
  what the slot holds and who shows it, and waits for `y`. Anything else
  cancels (exit 5). In a script, where nobody can be asked, add `--yes`.

```
$ bmdctl media clear 1
STUDIO A  PGM MP1 · PVW CAM3
  slot 1 holds "Logo", used by MP1 on program
  clear it? [y/N] y
  clear       slot 1  was "Logo"                                   19 ms
```

## Macros

```
$ bmdctl macro list
STUDIO A  PGM CAM2 · PVW CAM3 · 100 slots

    1  Open Show
    2  Lower third in
    3  Half Time
  97 empty slots hidden · --all shows them
$ bmdctl macro run "Open Show"
STUDIO A  PGM CAM2 · PVW CAM3
  macro run   1 (Open Show)                                        12 ms
$ bmdctl macro download 1 open.xml
STUDIO A  PGM CAM2 · PVW CAM3
  download    macro 1  Open Show → open.xml                       109 ms
$ bmdctl macro upload 5 open.xml --name "Open Show B"
STUDIO A  PGM CAM2 · PVW CAM3
  upload      macro 5  Open Show B                                 96 ms
```

- A slot can be given by number or by the macro's name, as in `macro run
  "Open Show"`. `macro stop` stops the one running.
- Downloaded macros are ATEM Software Control's XML by default (the
  form a profile holds them in); `.json` or `--format json` gives a list
  of ops, anything else the raw bytecode. `macro upload` reads all
  three.
- `macro decode open.bin` and `macro encode open.xml open.bin` convert
  between bytecode and XML locally, with no switcher.
- `macro record 6 "Lower third in"` records what you do next into slot
  6, until `macro stop`.
- `macro delete`, and uploading or recording over a used slot, ask
  first, as media does. Macro upload hasn't run on hardware yet
  ([HARDWARE.md](HARDWARE.md#unverified)).

## Profiles

`profile save` writes the switcher's setup as ATEM Software Control's
XML, with every macro, and each used still as a PNG under `ATEM Media
Pool/` beside it:

```
$ bmdctl profile save show.xml
STUDIO A  PGM CAM2 · PVW CAM3
  ✓ setup     6 macros                                             1.9 s
  ✓ stills    1 still                                              0.9 s
  profile     show.xml  70 KB · 6 macros · 1 still                 2.8 s
$ git add show.xml "ATEM Media Pool" && git commit -m "STUDIO A before Saturday"
```

- `--media-pool-images off` leaves the stills out; `profile images
  --dir stills` writes only the stills.
- `profile restore show.xml` loads a saved profile back, with its stills
  if they are beside it. It rewrites the switcher's setup, so it asks
  first. Program, preview and the video mode are left alone unless asked
  (`--program --preview`); `--macros-only` restores just the macros.
  Restore hasn't run on hardware yet
  ([HARDWARE.md](HARDWARE.md#unverified)): try it on a spare switcher
  before a show.

## watch

`watch` prints what changes on the switcher, from any panel or program,
until Ctrl-C:

```
$ bmdctl watch
STUDIO A  PGM CAM2 · PVW CAM3 · watching · ctrl-c to stop
  14:02:13  preview     CAM3 → CAM4
  14:02:13  tally       CAM4 pvw, CAM3 off
  14:02:15  program     CAM2 → CAM1
  14:02:15  tally       CAM1 pgm, CAM2 off
  14:02:21  transition  mix  running 0.0s
  14:02:22  transition  mix  done 1.0s
  14:02:30  key 1       on air
  14:02:41  dsk 1       on air
  14:03:05  session dropped
  14:03:11  reconnected
```

`--filter` keeps changes whose key starts with one of its prefixes
(`--filter PrgI,PrvI`), `--count 5` stops after five, and `--all` adds
the clock ticks and duplicate tallies it leaves out.

`watch --json` prints one JSON line per change, for scripts. The first
line says watching has begun (from then on nothing is missed); each
change after it names the state packet, its key and its value. Keys are
the packet's code and indexes counted from 0, as the switcher sends
them: `PrgI:0` is M/E 1's program, `KeOn:0:1` M/E 1's keyer 2.

```
$ bmdctl watch --json --filter PrgI,PrvI
{"ok":true,"confirmed":false,"result":{"kind":"watching","time":"14:02:11.384"},"took_ms":2}
{"ok":true,"confirmed":false,"result":{"kind":"state","opcode":"PrvI","key":"PrvI:0","value":{"index":0,"source":4,"in_program":false},"time":"14:02:13.020"},"took_ms":1638}
{"ok":true,"confirmed":false,"result":{"kind":"state","opcode":"PrgI","key":"PrgI:0","value":{"index":0,"source":1},"time":"14:02:15.020"},"took_ms":3638}
```

A `disconnected` frame and then a `ready` one mean the session dropped
and came back. Tally lights can follow program and preview from `TlSr`,
the switcher's tally packet. For example, aux 2 following program:

```sh
bmdctl watch --json --filter PrgI:0 |
  jq --unbuffered -r 'select(.result.kind == "state") | .result.value.source' |
  while read -r src; do bmdctl aux 2 "$src"; done
```

## exec: cue files

`exec` runs a file of commands, one per line, as you would type them
after `bmdctl`:

```
$ cat open.cue
# open the show
preview cam1
transition mix 1:00
auto
usk 1 fill cam3
usk 1 on-air on
$ bmdctl exec open.cue
STUDIO A  PGM CAM2 · PVW CAM3 · open.cue · 5 lines
  2  preview     CAM3 → CAM1                                       18 ms
  3  transition mix  1:00                                        already
  4  auto        CAM1 on program · CAM2 on preview                 29 ms
  5  usk 1 fill  CAM4 → CAM3                                       20 ms
  6  usk 1 on-air  off → on                                        22 ms
  done · 5 ok
```

- **Every line is checked before the first is sent**: a typo on line 20
  sends nothing. Names and rates that only the switcher can check are
  checked as each line runs.
- **It stops at the first failure**, saying `stopped at line N`, with
  the line's error; `--continue` runs the rest and exits with the first
  failure's status.
- Lines are split as a shell splits them (quotes, `\`, `#` comments), but
  nothing is expanded. A line may start with `bmdctl`; its own `--host`
  wins for that line. `exec -` reads the lines from stdin.
- A destructive line needs `--yes` written on it: exec never stops to
  ask.
- From cron, a cue runs at a set time:
  `0 18 * * 5 $HOME/.local/bin/bmdctl --host "STUDIO A" exec /srv/cues/friday.cue`.
  cron's environment is bare: give bmdctl's full path, and set
  `XDG_RUNTIME_DIR` (or `BMDCTL_SOCKET`) as your shell has it, or the
  job starts an engine of its own beside yours.

## undo

`undo` puts back the last change this machine sent to program, preview,
an aux output or a keyer's or DSK's on air, or the last cut or auto:

```
$ bmdctl program cam1
STUDIO A  PGM CAM2 · PVW CAM3
  program     CAM2 → CAM1                                          21 ms
$ bmdctl undo
STUDIO A  PGM CAM1 · PVW CAM3
  undo        program  CAM1 → CAM2                                 20 ms
$ bmdctl cut
STUDIO A  PGM CAM2 · PVW CAM3
  cut         CAM3 on program · CAM2 on preview                    30 ms
$ bmdctl undo
STUDIO A  PGM CAM3 · PVW CAM2
  undo        cut  CAM2 on program · CAM3 on preview               31 ms
$ bmdctl undo
STUDIO A  PGM CAM2 · PVW CAM3
  nothing to undo
```

- Each switcher keeps the last 20 changes, in the engine: they survive
  a dropped session, not an engine restart. A change the switcher
  already held wasn't sent, so it isn't kept.
- An undone cut or auto is a cut, so the keys it took on or off air come
  back too. If program or preview have changed since (from a panel),
  undo sets each back instead.
- During a transition undo refuses, and keeps the entry for once it is
  done.

## --dry-run

`--dry-run` prints the bytes a command would send, header included, and
sends nothing. It needs no switcher, so source names are limited to
numbers and the aliases (`cam2`, `mp1`, `bars`):

```
$ bmdctl --dry-run program cam2
000c00004350674900000002
$ bmdctl --dry-run transition wipe --width 50 --pattern CircleIris
001c0000435457700006000007001388000000000000000000000000
```

`000c` is the command's length (12 bytes), `0000` reserved, `43506749`
the code `CPgI`, `00` M/E 1 (the wire counts from 0), `00` padding and
`0002` source 2. Rates are counted at 25 fps unless `--fps` says
otherwise. Commands that read the switcher first (`status`, `undo`,
transfers) have no bytes to print.

## -t: timestamps

`-t` (`--timestamps`) starts each result line with the time of day,
for a log of what was done when. It works at a terminal and on each line
of `exec`:

```
$ bmdctl -t cut
STUDIO A  PGM CAM1 · PVW CAM3
  13:43:28  cut         CAM3 on program · CAM1 on preview          30 ms
```

## HyperDeck

A HyperDeck is a host like a switcher; commands go to it with `--host`,
or through `hdctl`, which is `bmdctl hyperdeck`. Nothing here has run on
a real deck yet ([HARDWARE.md](HARDWARE.md#unverified)).

```
$ bmdctl --host deck status
Deck 1  HyperDeck Studio HD Mini · 1080p25 · session 3m, idle 40s

  transport   stopped · loop off · single clip off
  timecode    00:00:00:00
  clip        1  Intro Loop.mp4  00:01:03:17
  timeline    3 clips
  slot 1      SD1 · mounted · 2h00m left                        ● active
  slot 2      SD2 · mounted · 1h00m left
  remote      on
$ bmdctl --host deck goto interview
Deck 1  stopped · clip 1
  goto        Intro Loop.mp4 → Interview.mov                       35 ms
$ hdctl --host deck play --speed 50
Deck 1  stopped · clip 2
  play        stopped → play 50%                                   41 ms
```

`play --loop --single-clip` loops the clip it is on, as a background.

`clips` lists the timeline (`--disk` the active disk's files), `stop`,
`next`, `prev`, `record [name]` and `slot <slot>` do what they say.
`record`, `next`, `prev`, `loop` and `goto` by timecode print a notice
that they are unverified on hardware.

## Scripting

When stdout isn't a terminal, each command prints one plain line, the
same from one release to the next. `--json` prints the engine's
response, `--quiet` nothing, and the exit status says what happened:

```
$ bmdctl program cam1 | cat
STUDIO A (192.168.1.20)  program  ME1            CAM2 → CAM1  confirmed    21 ms
$ bmdctl --json program cam2
{"ok":true,"confirmed":true,"result":{"args":{"me":"1","source":"2 (Camera 2)"},"command":"CPgI","echo":"PrgI:0","state":{"index":0,"source":2}},"took_ms":21}
$ bmdctl state --path mes.0.program.source
2
$ bmdctl --quiet media clear 3; echo $?
5
```

| Exit | Meaning | `--json` code |
|---|---|---|
| 0 | done | |
| 1 | usage error, or any other failure | `usage` |
| 2 | the switcher or the engine didn't answer | `unreachable`, `engine`, `stale` |
| 3 | sent, but not confirmed in time | `no_echo` |
| 4 | refused by the switcher (a transfer, a lock), or a profile restore with errors | `rejected` |
| 5 | destructive, and `--yes` wasn't given | `destructive` |

`state` prints the switcher's whole state; `--path` narrows it
(`mes.0.program`), and a path to one value prints the value alone.
State paths count from 0, as `watch`'s keys do; everything you type
elsewhere counts from 1. A loop over every host:

```sh
bmdctl hosts --json | jq -r '.result.hosts[] | select(.device == "atem") | .name' |
  while read -r h; do bmdctl --host "$h" profile save "$h.xml"; done
```

From Bitfocus Companion (its shell action), Node-RED (an exec node) or
Home Assistant (`shell_command`), call `bmdctl` the same way, or POST to
`bmdctl serve`.

## The engine

Every device command goes through one background process, the engine.
It holds a session to each device, so a command costs a round trip
rather than a connect and a full state dump, and it has the switcher's
state at hand to read names and rates against.

- **It starts itself.** The first command that needs it runs `bmdctl
  engine --background` and waits for it. It listens on a Unix socket,
  `$XDG_RUNTIME_DIR/bmdctl.sock` (else `bmdctl-<uid>/bmdctl.sock` in
  `$TMPDIR` or `/tmp`; `BMDCTL_SOCKET` overrides it), readable by your
  user alone. On Windows it is the named pipe `\\.\pipe\bmdctl-<user>`.
- **Sessions close after 10 minutes idle**, or an hour if pinned by
  `connect`. `BMDCTL_PIN_IDLE` sets the hour (`90m`, `2h`, or `0` for
  never), read when the engine starts: `bmdctl engine stop` after
  changing it. A `watch` or an MCP subscription counts as a command for
  as long as it runs.
- **It restarts after an upgrade.** Every request carries the build of
  the bmdctl that sent it. An engine a command started, asked by another
  build, stops, and the command starts one of its own build and sends
  again. An engine you started yourself answers every build.
- **Its log** is `~/.local/state/bmdctl/engine.log`, rotated at 5 MiB.
  `bmdctl engine --log-level debug` in the foreground shows the protocol.

```
$ bmdctl engine status
engine    running, pid 48213, up 2h14m3s
version   bmdctl v0.1.0 (e8eb8b9, 2026-10-11T19:11:25Z), protocol 1
build     MpUkle-BHtFbJgq0fFJH/soENu7Y-5cXGVrzdwhuM/KadqYJdIUmOfOSQQGaxu/1k1eVHazaCA2yhy1OyrP
socket    /run/user/1000/bmdctl.sock
log       /home/you/.local/state/bmdctl/engine.log
session   192.168.1.20:9910  ready, pinned  ATEM 1 M/E Constellation HD 1080p25
$ bmdctl engine stop
engine stopped
```

`engine stop` closes every session, saying goodbye to each switcher, and
waits for the socket to go. If the engine doesn't answer at all, `kill`
the pid `engine status` printed (or `pkill -f 'bmdctl engine'`); the
next command removes the socket it left and starts a new one. `bmdctl
doctor` checks the engine, its socket, hosts.yml and that each host
answers.

To run it as a service instead, run it in the foreground. A systemd user
unit, `~/.config/systemd/user/bmdctl.service`:

```ini
[Unit]
Description=bmdctl engine
After=network-online.target

[Service]
ExecStart=%h/.local/bin/bmdctl engine
Restart=on-failure

[Install]
WantedBy=default.target
```

`systemctl --user enable --now bmdctl` starts it; after upgrading bmdctl,
`systemctl --user restart bmdctl`. `bmdctl engine --serve` runs the HTTP
API in the same process (next section). [INSTALL.md](INSTALL.md#docker)
covers the container.

## serve: the HTTP API

`bmdctl serve` serves every command as HTTP/JSON on `127.0.0.1:8880`,
through the engine. It has no authentication: whoever can reach it
controls the devices, so `--listen` to another address only on a network
you trust.

```
$ bmdctl serve
bmdctl serve  http://127.0.0.1:8880 · ctrl-c to stop
  no authentication: whoever reaches it controls the devices
  14:10:02  GET    /hosts                                     200 · 1 ms
  14:10:05  POST   /hosts/studio/program                     200 · 21 ms
```

```
$ curl -s localhost:8880/hosts
{"ok":true,"confirmed":false,"result":{"default":"STUDIO A","hosts":[{"name":"STUDIO A","address":"192.168.1.20","device":"atem"},{"name":"Deck 1","address":"192.168.1.50","device":"hyperdeck"}]},"took_ms":0}
$ curl -s -H 'Content-Type: application/json' -d '{"source": "cam3"}' localhost:8880/hosts/studio/program
{"ok":true,"confirmed":true,"result":{"args":{"me":"1","source":"3 (Camera 3)"},"command":"CPgI","echo":"PrgI:0","state":{"index":0,"source":3}},"took_ms":21}
```

- **A command is `POST /hosts/{host}/{its words}`**, its arguments a JSON
  object (`/hosts/studio/usk/on-air` with `{"keyer": 1, "on-air": true}`).
  The host is resolved as `--host` is: a name, the start of one, or an
  address. The body must be `application/json`.
- **Files** go as `multipart/form-data`: `curl -F slot=3 -F
  file=@sponsor.png localhost:8880/hosts/studio/media/upload`. A
  download's file comes back base64 in `files`.
- **Destructive commands** need `?yes=true`; without it the answer is
  409, with what it would affect in `confirm`.
- **The answer** is what `--json` prints, with an HTTP status for its
  code: 200, 400 usage, 409 destructive, 422 rejected, 504 not confirmed,
  502 unreachable.
- `GET /hosts/{host}/state` (`?path=mes.0`) reads the state;
  `GET /hosts/{host}/events` streams `watch --json`'s lines as
  server-sent events; `GET /openapi.json` describes every command.

## MCP: Claude Code and Claude Desktop

`bmdctl mcp` is a Model Context Protocol server on stdin and stdout: a
tool per command group (`atem_switching`, `atem_usk`, `atem_media`,
`hyperdeck_transport`, ...) plus `list_hosts`, with each host's state as
a resource an agent can subscribe to. Add hosts first (`bmdctl connect
ADDRESS`); the engine starts by itself.

**Claude Code**, for every project:

```sh
claude mcp add --transport stdio --scope user bmdctl -- bmdctl mcp
```

or for everyone who clones a project, in its `.mcp.json`:

```json
{
  "mcpServers": {
    "bmdctl": {
      "type": "stdio",
      "command": "bmdctl",
      "args": ["mcp"]
    }
  }
}
```

`claude mcp list` shows whether it connected; inside a session, `/mcp`
does.

**Claude Desktop**: Settings > Developer > Edit Config opens
`claude_desktop_config.json` (`~/Library/Application
Support/Claude/` on macOS, `%APPDATA%\Claude\` on Windows). Add the
server and restart Claude Desktop:

```json
{
  "mcpServers": {
    "bmdctl": {
      "command": "/Users/you/.local/bin/bmdctl",
      "args": ["mcp"]
    }
  }
}
```

A desktop app doesn't see your shell's PATH, so give the full path
(`command -v bmdctl` prints it; on Windows with Scoop,
`C:\\Users\\you\\scoop\\shims\\bmdctl.exe`, backslashes doubled in JSON).

What an agent then does: `bmdctl program cam2` is the tool call
`atem_switching {"subcommand": "program", "source": "cam2"}`, and its
result says `confirmed`, `sent`, `already` or `rejected` with each value
before and after. A destructive subcommand does nothing without
`confirm: true`, and says what it would affect for the agent to put to
you. The skill in `skills/bmdctl/` teaches an agent to work a live show
this way; [AGENTS.md](AGENTS.md) says how to install it and describes
the tools. It has been run from Claude Code against a switcher; Claude
Desktop hasn't been tried yet ([HARDWARE.md](HARDWARE.md#unverified)).
