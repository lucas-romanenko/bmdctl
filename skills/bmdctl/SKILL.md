---
name: bmdctl
description: Run Blackmagic ATEM switchers and HyperDeck recorders with bmdctl (CLI or MCP tools) on a live show, safely - check what is on air, switch, key graphics, cue clips.
---

# bmdctl

bmdctl controls Blackmagic Design devices on the local network: ATEM
switchers and HyperDeck recorders (Videohub and Ultimatte: saved, no
commands yet). Use it when someone asks to switch a camera, run a
transition, put a graphic or keyer on air, load a still, play or record a
clip, or reports what a device is doing. Every command is a CLI line
(`bmdctl program cam2`) and, over MCP, a subcommand of its group's tool
(`atem_switching {"subcommand": "program", "source": "cam2"}`; the tool's
description lists its subcommands). The reference for all of them is
docs/COMMANDS.md in the bmdctl repository; `bmdctl <command> --help`
prints one.

This is usually live television. A wrong cut is seen by the audience.
Work like an assistant in the gallery: say what you will do, do it, say
what happened.

## Before you write anything

1. **Know the device.** `bmdctl` alone (or `list_hosts`) lists the hosts;
   commands go to the default unless `--host NAME` (`host` in a tool) says
   otherwise. A HyperDeck's commands (`play`, `goto`) don't work on an ATEM
   and the reverse; the error says which a host is.
2. **Read state first.** `bmdctl status` (`atem_session` status) shows
   program and preview per M/E, the transition, keyers and DSKs on air,
   aux outputs, the macro player; for a deck, `bmdctl --host DECK status`.
   `bmdctl inputs` lists source names; sources take a number, a short or
   long name, or an alias (`cam2`, `mp1`, `bars`, `black`, `col1`, `pgm`).
3. **Before a cut or an auto, tell the operator what is on air** and what
   will replace it: "Program is CAM1, preview is CAM3: cutting puts CAM3 on
   air." Then act. Never cut "to check".
4. Indexes count from 1, as ATEM Software Control shows them (`--me 1`,
   `usk 1`, `aux 2`, media slot 3). Only `state` and `watch`
   show the switcher's raw state, counted from 0 (`mes.0` is M/E 1).

## Reading results

A result says whether it took: `confirmed` (the device reported the change
back), `sent` (that command has no confirmation: `dsk auto`, `ftb toggle`,
macros), `already` (the device held it; nothing was sent), `rejected` (it
kept its old value). With `--json`, each line is `{"ok":true,"confirmed":
...,"result":{...},"took_ms":...}` or `{"ok":false,"code":...,"error":...}`;
MCP results add `status` and `changed` (each value before and after).
Exit codes: 0 done, 1 usage, 2 unreachable, 3 not confirmed in time,
4 refused by the device, 5 destructive without consent. After a `sent`
or a failure, read `status` again before saying it worked.

## Destructive: ask a person, every time

These do nothing until a person agrees: `video-mode` (every output
resyncs: the show drops), `startup save`, `startup clear`, `media clear`,
`media clear-unlocked`, `macro delete`, `profile restore`; and uploading
over a used still or macro slot, or recording a macro into one. Without
`--yes` (`confirm: true` in a tool) bmdctl refuses and says what it would
affect. Put that to the operator in their words and wait for a clear yes.
**Never add `--yes` or `confirm: true` on your own**, not to finish a task,
not because an earlier yes covered something else. One yes is for one
command.

## Sequences: use exec

Several changes in a row go in a file run with `bmdctl exec FILE`: every
line is checked before the first is sent (a typo on line 9 sends nothing),
lines run in order and it stops at the first failure (`--continue` to go
on). One command per line, as typed after `bmdctl`; `#` starts a comment;
a line's own `--host` wins for that line. Show the file to the operator
before running it on air.

## Recipes

**1. Load and key a graphic** (a lower third on DSK 1):
```
bmdctl media list                        # which slots are used, what MP1 shows
bmdctl media upload 3 lower-third.png    # a used slot asks first
bmdctl media player 1 --still 3
bmdctl dsk fill mp1
bmdctl dsk key mp1k                      # the player's key (alpha)
bmdctl dsk on-air on                     # tell the operator first: it goes live
```
`dsk auto` fades it instead; `dsk on-air off` takes it off. An upstream
keyer is `usk 1 fill mp1`, `usk 1 on-air on` (it can ride the next
transition: `transition next --next background,key1`).

**2. Pre-show reset**, as an exec file, shown to the operator first:
```
# preshow.txt: bmdctl exec preshow.txt
ftb enable off
program black
preview cam1
usk 1 on-air off
dsk on-air off
transition mix 1:00
aux 1 pgm
```
A full restore of a saved setup is `profile restore show.xml`:
destructive, so ask.

**3. Scheduled content change.** bmdctl doesn't schedule; cron, systemd
timers or a show-control system run it. Write the change as an exec file,
agree with the operator what it does and when, then schedule `bmdctl exec
FILE`. If it replaces a still that may be on air when it runs, check first:
`bmdctl media list` shows which player shows which slot. A line needing
`--yes` in a scheduled file must be one the operator agreed to for that
schedule.

**4. Tally-driven routing** (aux 2 follows program):
```
bmdctl watch --json --filter PrgI:0 |
  jq --unbuffered -r 'select(.result.kind == "state") | .result.value.source' |
  while read -r src; do bmdctl aux 2 "$src"; done
```
`watch --json` prints one frame per line: first `{"result":{"kind":"watching"}}`
(from then nothing is missed), then `{"kind":"state","opcode":"PrgI","key":
"PrgI:0","value":{"index":0,"source":5},"time":"13:43:28.120"}`. Keys are
the opcode and indexes from 0 (`KeOn:0:1` is M/E 1 keyer 2); `TlSr` is the
tally (`{"tally":{"5":{"program":true,"preview":false}}}`); `disconnected`
then `ready` mean the session dropped and came back. In MCP, subscribe to
`bmdctl://hosts/<host>/state` instead.

**5. HyperDeck playback cue** (deck "Deck 1" feeding ATEM input 4):
```
bmdctl --host "Deck 1" clips             # the timeline, and the clip it is on
bmdctl --host "Deck 1" goto intro        # cue: goes to the clip, doesn't play
bmdctl preview cam4                      # the deck's input on preview
# on the operator's cue:
bmdctl --host "Deck 1" play
bmdctl auto
```
`play --loop --single-clip` loops one clip (a background); `loop off`
lets it run out; `stop` holds the last frame. `hdctl` and `atemctl` are
short for `bmdctl hyperdeck` and `bmdctl atem`.

## Cautions

- **Untested models.** bmdctl has been run on an ATEM 1 M/E Constellation
  HD; other ATEMs speak the same protocol but are untested, and the
  HyperDeck commands have not met a deck yet. `record`, `next`, `prev`,
  `loop` and `goto` by timecode are marked unverified, and so are some
  ATEM commands (help says so). On an unfamiliar device, read `status`
  after each change instead of trusting the result alone.
- `undo` puts back the last program, preview, cut, auto, aux or keyer
  change made from this machine; say what it will restore before using it.
- If a command fails with exit 2, `bmdctl doctor` says why. Don't retry a
  failed cut blindly: read status, then decide with the operator.
