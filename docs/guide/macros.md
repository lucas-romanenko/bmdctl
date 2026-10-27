# Macros

Macros run on the switcher by number or by name, and move between the
switcher and files as ATEM Software Control's XML.

```
$ bmdctl macro list
STUDIO A  PGM CAM2 · PVW CAM3 · 100 slots

    1  Open Show
    2  Lower third in
    3  Half Time
  97 empty slots hidden · --all shows them
$ bmdctl macro run "Open Show"
STUDIO A  PGM CAM2 · PVW CAM3
  macro run   1 (Open Show)                                        12 ms
$ bmdctl macro download 1 open.xml
STUDIO A  PGM CAM2 · PVW CAM3
  download    macro 1  Open Show → open.xml                       109 ms
$ bmdctl macro upload 5 open.xml --name "Open Show B"
STUDIO A  PGM CAM2 · PVW CAM3
  upload      macro 5  Open Show B                                 96 ms
```

- A slot can be given by number or by the macro's name, as in `macro run
  "Open Show"`. `macro stop` stops the one running.
- Downloaded macros are ATEM Software Control's XML by default (the
  form a profile holds them in); `.json` or `--format json` gives a list
  of ops, anything else the raw bytecode. `macro upload` reads all
  three.
- `macro decode open.bin` and `macro encode open.xml open.bin` convert
  between bytecode and XML locally, with no switcher.
- `macro record 6 "Lower third in"` records what you do next into slot
  6, until `macro stop`.
- `macro delete`, and uploading or recording over a used slot, ask
  first, as media does. On hardware, only an empty macro has been
  uploaded so far ([HARDWARE.md](../HARDWARE.md#unverified)).
