# undo

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
