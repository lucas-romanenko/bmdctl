# HyperDeck

A HyperDeck is a host like a switcher; commands go to it with `--host`,
or through `hdctl`, which is `bmdctl hyperdeck`. Most of it has run on a
HyperDeck Studio HD Mini; [HARDWARE.md](../HARDWARE.md#unverified) lists
what hasn't.

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
`pause` (it holds the frame, and the deck says stopped), `next`, `prev`,
`record [name]` and `slot <slot>` do what they say.

`timeline add <name>` puts a file of the active disk on the timeline, at
the end or `--before` a timeline clip; `timeline remove <clip>` takes one
off, by its number or name; `timeline clear` empties it, and asks first.
None of them touches the disk: the files stay, and `clips --disk` lists
them.

`configuration show` lists the deck's settings (video and audio input,
file format, record prefix, timecode), and `configuration set <setting>
<value>` changes one, named as `show` names it (`video-input` will do
for `video input`). `remote off` makes the deck refuse every write,
bmdctl's included, until `remote on`.
`record`, `next`, `prev`, `loop` and `goto` by timecode print a notice
that they are unverified on hardware.
