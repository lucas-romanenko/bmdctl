# HyperDeck

A HyperDeck is a host like a switcher; commands go to it with `--host`,
or through `hdctl`, which is `bmdctl hyperdeck`. Nothing here has run on
a real deck yet ([HARDWARE.md](../HARDWARE.md#unverified)).

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
