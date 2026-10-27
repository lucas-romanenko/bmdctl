# Profiles

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
- **A still the engine has seen isn't downloaded again.** Downloading a
  still takes about 1.8 s, at the switcher's own pace, so the engine keeps
  each one it downloads in `~/.cache/bmdctl/stills`, named by the MD5 the
  switcher reports for it, and downloads each picture once per save. A
  second save of an unchanged pool downloads nothing, and the stills step
  says how many it fetched (`✓ stills  20 stills, 8 downloaded`).
  `BMDCTL_STILL_CACHE_MB` sizes it (1024 unless set, 0 keeps none); past
  that the least recently used go, as does a still no save has used for
  30 days.
- **Each still's file is the one the XML names:** the slot's name,
  `Slot<n>.png` for a slot with none, and `<name>_2.png` for a second
  picture under a name already taken, so a restore puts every picture
  back in its own slot under its own name.
- `profile restore show.xml` loads a saved profile back, with its stills
  if they are beside it. It rewrites the switcher's setup, so it asks
  first. Program, preview and the video mode are left alone unless asked
  (`--program --preview`); `--macros-only` restores just the macros.
  On hardware, the macros, the Fairlight mixer and the media pool's stills
  have been restored so far ([HARDWARE.md](../HARDWARE.md#unverified)): try the rest on a
  spare switcher before a show.
