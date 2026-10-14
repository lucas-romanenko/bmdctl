# exec: cue files

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

## -t: timestamps

`-t` (`--timestamps`) starts each result line with the time of day,
for a log of what was done when. It works at a terminal and on each line
of `exec`:

```
$ bmdctl -t cut
STUDIO A  PGM CAM1 · PVW CAM3
  13:43:28  cut         CAM3 on program · CAM1 on preview          30 ms
```
