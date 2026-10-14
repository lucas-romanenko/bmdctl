# Media pool

The media pool holds the stills the media players show. `media list`
says what each slot holds and which player shows it; the others move
stills between files and slots.

```
$ bmdctl media list
STUDIO A  PGM CAM2 · PVW CAM3 · 20 stills

   1  Logo     MP1
   2  (empty)  MP2
  18 empty slots hidden · --all shows them
$ bmdctl media upload 3 sponsor.png
STUDIO A  PGM CAM2 · PVW CAM3
  upload      slot 3  sponsor  1920×1080                           1.1 s
$ bmdctl media player 2 --still 3
STUDIO A  PGM CAM2 · PVW CAM3
  media player 2  still 2 → 3                                      18 ms
$ bmdctl media download 3 back.png
STUDIO A  PGM CAM2 · PVW CAM3
  download    slot 3  sponsor → back.png  1920×1080               573 ms
```

- **Upload** takes a PNG, JPEG or GIF and fits it to the video mode's
  frame, keeping its shape (bars where it doesn't fill). A PNG's colours
  are premultiplied by its alpha, as ATEM Software Control does
  (`--premultiply off` turns that off). The slot is named after the file
  unless `--name "Main Sponsor"`. While it runs, a progress bar on stderr
  shows `uploading sponsor.png → slot 3 █████░░░ 68%  816 KB  0.8 s`,
  and the result line replaces it.
- **Slots count from 1**, as ATEM Software Control shows them.
- **Download** writes a PNG; without a file name it is named after the
  slot.
- **Replacing a used slot, and clearing one, ask first.** bmdctl says
  what the slot holds and who shows it, and waits for `y`. Anything else
  cancels (exit 5). In a script, where nobody can be asked, add `--yes`.

```
$ bmdctl media clear 1
STUDIO A  PGM MP1 · PVW CAM3
  slot 1 holds "Logo", used by MP1 on program
  clear it? [y/N] y
  clear       slot 1  was "Logo"                                   19 ms
```
