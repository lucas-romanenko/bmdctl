# Quick start

Five commands, from an address to a cut. What each prints below is
bmdctl's own output: a test runs these five commands against a fake
switcher named STUDIO A and checks this page against what they print.
Your switcher's name, sources and times will differ.

bmdctl needs to be [installed](INSTALL.md) on a machine that reaches the
switcher's UDP port 9910. Nothing else is: no Blackmagic software, and no
ATEM Software Control running.

## 1. Connect

`connect` asks what answers at the address, saves it in hosts.yml by the
name the switcher gives itself (set in ATEM Setup, else its model), makes
it the default and keeps a session open to it:

```
$ bmdctl connect 192.168.1.20
STUDIO A  PGM CAM2 · PVW CAM1 · 1080p25 · connected 328 ms
  saved as "STUDIO A", now the default
  tab completes input names
```

The first line is the header every command starts with: the switcher's
name, what was on program and preview when the command ran, and, because
this command opened the session, the video mode and how long connecting
took. Every command from now on goes to STUDIO A unless it names another
host ([Hosts](guide/hosts.md)).

## 2. Status

`status` is the switcher at a glance: M/E 1's program and preview, the
next transition, each keyer's fill and key and whether it is on air, the
aux outputs and the macro player.

```
$ bmdctl status
STUDIO A  1080p25 · session 6s, idle 6s, pinned

  ME 1        PGM  CAM2           PVW  CAM1
              dip 1:05 · next background+key1 · FTB off
  keys        1  luma  CAM4 / CAM5                                on air
              2  luma  BLK / BLK                                     off
              3  luma  BLK / BLK                                     off
              4  luma  BLK / BLK                                     off
  dsk 1       CAM6 / CAM7                                         on air
  dsk 2       BLK / BLK                                              off
  aux 1       MP1
  aux 2       PGM
  aux 3       BLK
  macro       idle
```

At a terminal, what is on program is red and what is on preview green,
as on the switcher's panel.

## 3. Inputs

`inputs` lists the sources and what each is called. A command takes a
source by its number (`3`), as `cam3` (input 3 whatever its name), or by
its short or long name (`CAM3`, `"Camera 3"`).

```
$ bmdctl inputs
STUDIO A  PGM CAM2 · PVW CAM1

       BLK   Black
    1  CAM1  Camera 1                                              ○ PVW
    2  CAM2  Camera 2                                              ● PGM
    3  CAM3  Camera 3
    4  CAM4  Camera 4
    5  CAM5  Camera 5
    6  CAM6  Camera 6
    7  CAM7  Camera 7
       Bars  Color Bars
       COL1  Color 1
       MP1   Media Player 1

  bmdctl inputs --all adds outputs, masks and keys
```

## 4. Preview

A command that changes something prints one line under the header: what
it did, the value before and after, and how long the switcher took to
report the change back.

```
$ bmdctl preview cam3
STUDIO A  PGM CAM2 · PVW CAM1
  preview     CAM1 → CAM3                                          24 ms
```

## 5. Cut

```
$ bmdctl cut
STUDIO A  PGM CAM2 · PVW CAM3
  cut         CAM3 on program · CAM2 on preview                    30 ms
```

The cut is confirmed when the switcher reports both buses. `bmdctl undo`
would put CAM2 back on program ([undo](guide/undo.md)).

## Next

- [Switching](guide/switching.md): auto and its transitions, fade to
  black, the aux outputs, and every way to name a source.
- [Hosts](guide/hosts.md): more than one switcher, short names, and which
  host a command goes to.
- [Scripting](guide/scripting.md): what a command prints into a pipe,
  `--json` and the exit status.
- [Commands](COMMANDS.md): every command and flag. At the terminal,
  `bmdctl <command> --help` prints one with examples.
