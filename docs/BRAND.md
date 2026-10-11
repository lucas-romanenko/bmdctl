# Brand

bmdctl's identity: the mark, the wordmarks, the colours and the README
banner. It is the third project in a family with webATEM (browser control
for ATEM switchers) and bmdwire (the Python protocol libraries). The
values below are final: match them exactly when you regenerate an asset
or use the brand somewhere new.

The assets are in `.github/`:

- `banner.png`: the README banner, 2560×520. It is at the top of
  `README.md`. GitHub strips CSS from rendered markdown, so the banner
  stays an image.
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

| Form | Spec | Use |
|---|---|---|
| `bmdctl` (primary) | `bmd` 400 `#9A9E9F`, `ctl` 600 `#EDEDEA`, `letter-spacing: -0.04em`, `line-height: 1` | Lockups, the banner |
| `bmd/ctl` | Plex Mono 600, the slash in `#2FD07A` | Docs, headings |
| `$ bmdctl` | Plex Mono 500, the `$` in `#2FD07A` | README prose, terminal screenshots |

On a light ground: `bmd` is `#6E706C`, `ctl` is `#111312` and accents are
`#1E9E57`.

**Lockup:** the mark on the left, the wordmark on the right, vertically
centred. A 56px mark has a 20px gap and a 34px wordmark; scale it in
proportion (the banner: a 168px mark, a 60px gap, a 102px wordmark).

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
- The lockup on the left.
- On the right, right-aligned:
  - "Control Blackmagic devices / from the command line." in Plex Mono
    400, 44px, `#C8CCCD`, on two lines.
  - `$ bmdctl  ·  single Go binary  ·  scripts and AI agents` in Plex Mono
    500, 34px: the `$` green, `bmdctl` `#EDEDEA`, the rest `#9A9E9F`.

## Legal

The README says "Not affiliated with or endorsed by Blackmagic Design Pty
Ltd." Never use Blackmagic logos or product imagery.
