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
- `profile restore show.xml` loads a saved profile back, with its stills
  if they are beside it. It rewrites the switcher's setup, so it asks
  first. Program, preview and the video mode are left alone unless asked
  (`--program --preview`); `--macros-only` restores just the macros.
  Restore hasn't run on hardware yet
  ([HARDWARE.md](../HARDWARE.md#unverified)): try it on a spare switcher
  before a show.
