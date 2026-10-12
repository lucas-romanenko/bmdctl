# Brand

bmdctl's identity: the mark, the wordmarks, the colours and the README
banner. It is the third project in a family with webATEM (browser control
for ATEM switchers) and bmdwire (the Python protocol libraries). The
values below are final: match them exactly when you regenerate an asset
or use the brand somewhere new.

The assets are in `.github/`:

- `banner.gif`: the README banner, 2560×520, at the top of `README.md`.
  Its cursor blinks: GitHub strips CSS from rendered markdown but plays
  a GIF inline.
- `banner.png`: the same banner as a still, the cursor on, for wherever
  animation is unwanted, such as a social preview or print.
- `mark/bmdctl-mark-{512,256,112,64,56,32,16}-{dark,light}.png`: the mark,
  transparent outside the tile. Use `512-dark` as the GitHub avatar and
  `32` or `16` as a favicon.

Two other marks were studied and rejected: a typeset `>_`, where one glyph
can't carry two tally colours and the strokes are too thin at 16px, and a
bus with a cursor, which reads as a variant of bmdwire's mark. Only the
drawn chevron ships.

## Mark

A terminal prompt drawn in the family's tile. The chevron's upper stroke
is program red and its lower stroke is preview green, so the prompt also
reads as tally. The cursor is a plain white block.

Coordinates are in a 100×100 box, scaled to the tile size `S`.

**Tile**

- `S × S`, `border-radius: round(S × 0.235)`.
- Dark: field `#0A0B0C`, border `#34383C`.
- Light: field `#D3D2CD`, border `#CFCEC9`.
- Border width `max(1, round(S / 84))`, so 1px up to about 126px.

**Chevron**: two strokes, round caps, meeting at the apex. Width 11.

- Upper stroke from (26, 30) to (48, 50). Dark `#D62718`, light `#B81E10`.
- Lower stroke from (48, 50) to (26, 70). Dark `#2FD07A`, light `#1E9E57`.

**Cursor**: a rect at x 58, y 63, 22 × 10, corner radius about 2.8. Dark
`#EDEDEA`, light `#111312`.

**At 16px** (required there, and only there):

- No cursor.
- The chevron moves right to re-centre: (39, 30) → (61, 50) → (39, 70).
- Strokes are width 15, so each stays above 2px.

The cursor is drawn at 32px and above.

## Wordmarks

All set in IBM Plex Mono, always lowercase because bmdctl is a binary
name.

**The title** is the name typed at a prompt: `$ bmdctl` and a block
cursor. It heads the banner, a docs site's hero and slide titles; the
plain wordmarks below go in lockups and anywhere small.

- `$` and a space: Plex Mono 600, `#2FD07A`.
- `bmd` 400 `#9A9E9F`, `ctl` 600 `#EDEDEA`, `letter-spacing: -0.04em`.
- The cursor: a solid `#EDEDEA` block about 0.53em wide and 0.83em tall,
  on the baseline, 0.15em after the `l`. Where CSS runs it blinks at
  1.1s with `steps(1)`; in the README banner the GIF blinks it. Only the
  title's cursor blinks: the mark's never animates, anywhere.
- The tagline is a shell comment, `# control Blackmagic devices from the
  command line`, in Plex Mono 400, `#7E8386`.

Two other titles were studied and not chosen: a bitmap one (a pixel-grid
chevron, the VT323 font, scanlines) and a TUI panel (the name set into a
box-drawn frame over a reverse-video status bar). Only the shell prompt
ships.

| Form | Spec | Use |
|---|---|---|
| `bmdctl` (primary) | `bmd` 400 `#9A9E9F`, `ctl` 600 `#EDEDEA`, `letter-spacing: -0.04em`, `line-height: 1` | Lockups, and the name in the title |
| `bmd/ctl` | Plex Mono 600, the slash in `#2FD07A` | Docs, headings |
| `$ bmdctl` | Plex Mono 500, the `$` in `#2FD07A` | README prose, terminal screenshots |

On a light ground: `bmd` is `#6E706C`, `ctl` is `#111312` and accents are
`#1E9E57`.

**Lockup:** the mark on the left, the wordmark on the right, vertically
centred. A 56px mark has a 20px gap and a 34px wordmark; scale it in
proportion.

## Colour

| Token | Dark | Light | Role |
|---|---|---|---|
| `--pgm` | `#D62718` | `#B81E10` | Upper chevron stroke. Program, shared with webATEM. Not a general accent. |
| `--pvw` | `#2FD07A` | `#1E9E57` | Lower chevron stroke. Also the `$`, the slash and links. Dark ink on it, never white. |
| `--cursor` | `#EDEDEA` | `#111312` | The cursor block |
| `--tile` | `#0A0B0C` | `#D3D2CD` | The mark's field |
| `--tile-border` | `#34383C` | `#CFCEC9` | The tile's edge |
| `--ink` / paper | `#0E0F10` | `#F2F1EE` | Page ground |
| `--surface` | `#141618` | none | Cards, chips |
| `--text` | `#EDEDEA` | `#111312` | Primary text, `ctl` |
| `--muted` | `#9A9E9F` | `#6E706C` | Secondary text, `bmd` |
| `--faint` | `#7E8386` | none | Captions |

- bmdwire's copper is not used in bmdctl. The mark has the two tally
  colours and white, nothing else.
- Never add a fourth hue.

## README banner

Used as is. To regenerate it:

- 2560×520 on `#0A0B0C`, 160px padding at each side.
- On the left, the 168px mark, then a 72px gap, then the title at 120px
  with a 64×100 cursor. The tagline is 28px below it, at 40px, `#7E8386`.
- At the far right, right-aligned, in Plex Mono 400, 36px, `#9A9E9F`, on
  two lines: `single Go binary` and `terminal · scripts · AI agents`.
- The GIF is two frames, the cursor on and then off, 0.55s each,
  looping forever. The still is the first frame.

## Social preview

`docs/brand/social-preview.png`, 1280×640, is the image GitHub shows when
the repository is linked. GitHub has no API for it: upload it in the
repository's Settings, under Social preview.

- `#0A0B0C` ground, the 168px dark mark at 96px from the left, then the
  title at 112px with its tagline at 28px, as in the banner.
- Below, one line of terminal output in a `#141618` panel with a
  `#34383C` border: a cut, the new program source in `--pgm` and the new
  preview source in `--pvw`, its time dim (`#7E8386`).
- Everything stays 96px inside the left and right edges.
- `docs/brand/social-preview.html` is its source:
  `google-chrome --headless=new --hide-scrollbars --window-size=1280,640
  --virtual-time-budget=8000 --screenshot=social-preview.png
  social-preview.html`, from `docs/brand/`, renders it (it loads IBM Plex
  Mono from Google Fonts).

## Legal

The README says "Not affiliated with or endorsed by Blackmagic Design Pty
Ltd." Never use Blackmagic logos or product imagery.
