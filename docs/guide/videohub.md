# Videohub

A Videohub router is a host like a switcher: commands go to it with
`--host`, or through `vhctl`, which is `bmdctl videohub`. The engine
keeps a session open to it, as it does to a switcher, so a route made at
a panel or in Videohub Control is in what bmdctl shows. `connect`,
`status` and `route` have run on a Videohub 40x40 12G; the rest hasn't
yet ([HARDWARE.md](../HARDWARE.md#unverified)).

```
$ bmdctl hosts add 192.168.1.60
added Studio Router (192.168.1.60)
$ bmdctl hosts alias "Studio Router" router
Studio Router  192.168.1.60
  alias       router
$ bmdctl --host router status
Studio Router  connected 41 ms · 16 × 16 · 1 output locked

   1  Switcher 1      Camera 1
   2  Switcher 2      Camera 2
   3  Switcher 3      Camera 3
   4  Switcher 4      Camera 4
   5  Monitor A       Replay
   6  Monitor B       Graphics
   7  Stream Encoder  Program
   8  Recorder 1      Preview
   9  Recorder 2      Multiview
  10  Projector       Playback 1
  11  Wall 1          Playback 2
  12  Wall 2          Studio Feed
  13  Truck           Remote 1
  14  Spare 1         Remote 2
  15  Spare 2         Bars
  16  Test            Black                                       locked
```

`status` is a row per output: its number, its label and the input on it,
and `locked` where another client holds it. `outputs` prints the rows
alone.

## Routing

`route <output> <input>` puts an input on an output. A port is its
number, counted from 1 as Videohub Control and the router's panel count
them, or its label, in any case, or the start of one label (`prog`). A
number always wins: an input labelled `7` is still input 7.

```
$ bmdctl --host router route 5 7
Studio Router
  route Monitor A  Replay → Program                                18 ms
$ bmdctl --host router route "monitor a" prog
Studio Router
  route Monitor A  Program                                       already
$ vhctl --host router route projector graphics
Studio Router
  route Projector  Playback 1 → Graphics                           17 ms
```

A route the router holds already sends nothing (`already`). The router
answers every change, so a result is the router's: there is no `sent`.
A start of a label that fits two is refused, with both, and so is a
route to a locked output, which the router refuses:

```
$ bmdctl --host router route spare 1
error  output "spare" could be 14 "Spare 1", 15 "Spare 2": give more of…
$ bmdctl --host router route 16 1
error  the router refused: output 16 "Test" is locked by another client
```

## Labels

`input rename` and `output rename` set a port's label, as Videohub
Control does. Each run of spaces or line ends becomes one space, with
none at the ends, and an empty label clears the name; a label over 64
characters is refused.

```
$ bmdctl --host router output rename 5 Director
Studio Router
  output rename 5  Monitor A → Director                            21 ms
$ bmdctl --host router input rename "camera 3" "Wide Shot"
Studio Router
  input rename 3  Camera 3 → Wide Shot                             19 ms
```

`inputs` lists the inputs and the outputs each one is on:

```
$ bmdctl --host router inputs
Studio Router  16 inputs

   1  Camera 1     to 1
   2  Camera 2     to 2
   3  Wide Shot    to 3
   4  Camera 4     to 4
   5  Replay       routed nowhere
   6  Graphics     to 6, 10
   7  Program      to 5, 7
   8  Preview      to 8
   9  Multiview    to 9
  10  Playback 1   routed nowhere
  11  Playback 2   to 11
  12  Studio Feed  to 12
  13  Remote 1     to 13
  14  Remote 2     to 14
  15  Bars         to 15
  16  Black        to 16
```

## Watching and scripts

`bmdctl --host router watch` prints a line for each change the router
reports, whoever made it: `route  out 5 Director  Program → Replay`, a
label or a lock the same way. `--count N` stops after N.

Down a pipe, `status`, `outputs` and `inputs` are tables, and `--json`
gives the engine's response (`bmdctl --host router outputs --json`):
ports counted from 1 there too. `state` is the session's whole state,
counted from 0 as the wire counts. [Scripting](scripting.md) has the
rest.
