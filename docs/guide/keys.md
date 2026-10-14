# Keys and DSKs

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
