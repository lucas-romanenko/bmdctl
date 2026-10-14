# Switching

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

Tab completes input names while the engine runs ([INSTALL.md](../INSTALL.md#completion)).

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
