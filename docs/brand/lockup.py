# SPDX-License-Identifier: MIT
"""Write the mark and the lockup as SVG, from BRAND.md's values.

    python3 lockup.py IBMPlexMono-Regular.ttf IBMPlexMono-SemiBold.ttf

run from docs/brand/, writes bmdctl-mark-{dark,light}.svg and
bmdctl-lockup-{dark,light}.svg beside it. The wordmark's letters are
outlined from IBM Plex Mono (fontTools), so the files need no font
wherever they are shown. The fonts are Google Fonts' (ofl/ibmplexmono).
"""

import sys

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont

SCHEMES = {
    "dark": {"tile": "#0A0B0C", "border": "#34383C", "pgm": "#D62718", "pvw": "#2FD07A",
             "cursor": "#EDEDEA", "bmd": "#9A9E9F", "ctl": "#EDEDEA"},
    "light": {"tile": "#D3D2CD", "border": "#CFCEC9", "pgm": "#B81E10", "pvw": "#1E9E57",
              "cursor": "#111312", "bmd": "#6E706C", "ctl": "#111312"},
}

# The lockup: a 56px mark, a 20px gap and a 34px wordmark, vertically
# centred (BRAND.md, "Lockup"). Everything below is in those pixels.
MARK, GAP, SIZE = 56, 20, 34
TRACKING = -0.04  # em


def mark(c, size):
    """The mark at size px, as SVG elements: the tile, the chevron, the
    cursor, in BRAND.md's 100-unit box scaled to size."""
    k = size / 100
    bw = max(1, round(size / 84)) / k  # the border, in box units
    r = round(size * 0.235) / k
    return "\n".join([
        f'<rect x="{bw / 2:.3f}" y="{bw / 2:.3f}" width="{100 - bw:.3f}" height="{100 - bw:.3f}" '
        f'rx="{r - bw / 2:.3f}" fill="{c["tile"]}" stroke="{c["border"]}" stroke-width="{bw:.3f}"/>',
        f'<g fill="none" stroke-width="11" stroke-linecap="round">'
        f'<path d="M26 30L48 50" stroke="{c["pgm"]}"/><path d="M48 50L26 70" stroke="{c["pvw"]}"/></g>',
        f'<rect x="58" y="63" width="22" height="10" rx="2.8" fill="{c["cursor"]}"/>',
    ])


def word(font, text, x, baseline):
    """Outlines of text set at SIZE from x on the baseline, and where it
    ends."""
    glyphs, cmap = font.getGlyphSet(), font.getBestCmap()
    s = SIZE / font["head"].unitsPerEm
    paths = []
    for ch in text:
        g = glyphs[cmap[ord(ch)]]
        pen = SVGPathPen(glyphs, ntos=lambda v: f"{v:.2f}".rstrip("0").rstrip("."))
        g.draw(TransformPen(pen, (s, 0, 0, -s, x, baseline)))
        paths.append(pen.getCommands())
        x += g.width * s + TRACKING * SIZE
    return " ".join(paths), x


def main(regular, semibold):
    bmd, ctl = TTFont(regular), TTFont(semibold)
    # Centred as CSS centres a line of line-height 1: the half-leading
    # over the font's ascender.
    hhea, upm = bmd["hhea"], bmd["head"].unitsPerEm
    top = (MARK - SIZE) / 2
    baseline = top + (upm - (hhea.ascent - hhea.descent)) / 2 / upm * SIZE + hhea.ascent / upm * SIZE
    x = MARK + GAP
    bmd_d, x = word(bmd, "bmd", x, baseline)
    ctl_d, x = word(ctl, "ctl", x, baseline)
    width = round(x - TRACKING * SIZE + 0.5)  # the last letter's tracking isn't ink
    for scheme, c in SCHEMES.items():
        with open(f"bmdctl-mark-{scheme}.svg", "w") as f:
            f.write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">\n{mark(c, 100)}\n</svg>\n')
        with open(f"bmdctl-lockup-{scheme}.svg", "w") as f:
            f.write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {MARK}">\n'
                    f'<g transform="scale({MARK / 100})">\n{mark(c, MARK)}\n</g>\n'
                    f'<path fill="{c["bmd"]}" d="{bmd_d}"/>\n<path fill="{c["ctl"]}" d="{ctl_d}"/>\n</svg>\n')


if __name__ == "__main__":
    main(*sys.argv[1:3])
