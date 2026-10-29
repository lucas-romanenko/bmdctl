# SPDX-License-Identifier: MIT
"""Write and render the image for bmdctl's launch post (docs/BRAND.md,
"Launch image"): the mark and the title, the tagline, two chips, then a
terminal running status and a cut on a switcher, a route on a router and
a play on a deck. 1200x1500, 4:5, the tallest LinkedIn shows a single
image at, with everything at least 80px inside the frame.

    python3 launch.py

run from docs/brand/launch/, writes launch.png beside it (needs
google-chrome and Pillow; IBM Plex Mono comes from Google Fonts, as in
social-preview.html). The terminal holds the CLI's own output, from
cmd/bmdctl/testdata/terminal, with the goldens' "#" timings filled in
and each status ending at column 72, as bmdctl ends it. Every name in it
is a stand-in.
"""

import html
import os
import re
import subprocess

from PIL import Image, ImageChops

HERE = os.path.dirname(os.path.abspath(__file__))
MARK = '../../../.github/mark/bmdctl-mark-512-dark.png'
FONT_LINK = ('<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600'
             '&display=block" rel="stylesheet">')
W, H = 1200, 1500
INSET = 80
COL = 72


def visible(s):
    return len(re.sub(r'</?[a-z]*>', '', s))


def result(left, status):
    """A result line: left as marked up, status dim, ending at column 72."""
    pad = COL - visible(left) - len(status)
    assert pad >= 2, (left, status)
    return left + ' ' * pad + f'<dim>{status}</>'


def term(lines):
    out = []
    for line in lines:
        assert visible(line) <= COL, line
        if line.startswith('$ '):
            out.append('<span class="ps">$</span> <span class="cmd">' + html.escape(line[2:]) + '</span>')
            continue
        s = html.escape(line, quote=False)
        s = s.replace('&lt;/&gt;', '</span>')
        for tag, cls in (('b', 'b'), ('dim', 'd'), ('red', 'pgm'), ('green', 'pvw')):
            s = s.replace(f'&lt;{tag}&gt;', f'<span class="{cls}">')
        out.append(s)
    return '\n'.join(out)


LINES = [
    '$ bmdctl studio status',
    '<b>STUDIO A</>  <dim>1080p25 · session 4m, idle 12s, pinned</>',
    '',
    '  ME 1        <red>PGM  CAM2</>           <green>PVW  CAM1</>',
    '              dip 1:05 · next background+key1 · FTB off',
    '  keys        1  luma  CAM4 / CAM5                                <red>on air</>',
    '              2  luma  BLK / BLK                                     <dim>off</>',
    '              3  luma  BLK / BLK                                     <dim>off</>',
    '              4  luma  BLK / BLK                                     <dim>off</>',
    '  dsk 1       CAM6 / CAM7                                         <red>on air</>',
    '  dsk 2       BLK / BLK                                              <dim>off</>',
    '  aux 1       MP1',
    '  aux 2       PGM',
    '  macro       <dim>idle</>',
    '',
    '$ bmdctl studio cut',
    '<b>STUDIO A</>  <dim>PGM CAM2 · PVW CAM1</>',
    result('  cut         <red>CAM1</> on program · <green>CAM2</> on preview', '30 ms'),
    '',
    '$ bmdctl router route 5 7',
    '<b>Studio Router</>',
    result('  route Monitor A  Replay → Program', '18 ms'),
    '',
    '$ bmdctl deck play --speed 50',
    '<b>Deck 1</>  <dim>stopped · clip 1</>',
    result('  play        stopped → play 50%', '16 ms'),
]

CSS = f'''
html, body {{ margin: 0; width: {W}px; height: {H}px; overflow: hidden; background: #0A0B0C; }}
body {{ font-family: "IBM Plex Mono", monospace; color: #EDEDEA; position: relative; }}
.col {{ position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%);
        display: flex; flex-direction: column; align-items: flex-start; }}
.lockup {{ display: flex; align-items: center; gap: 30px; }}
.lockup img {{ width: 84px; height: 84px; display: block; }}
.title {{ font-size: 64px; line-height: 1; white-space: nowrap; display: flex; align-items: baseline; }}
.title .p {{ color: #2FD07A; font-weight: 600; }}
.title .bmd {{ color: #9A9E9F; font-weight: 400; letter-spacing: -0.04em; }}
.title .ctl {{ color: #EDEDEA; font-weight: 600; letter-spacing: -0.04em; }}
.title .cur {{ display: inline-block; width: 0.53em; height: 0.83em; background: #EDEDEA; margin-left: 0.15em; transform: translateY(0.02em); }}
.tag {{ font-size: 22px; color: #7E8386; margin-top: 14px; white-space: nowrap; }}
.chips {{ display: flex; flex-direction: column; align-items: flex-start; gap: 10px; margin: 26px 0 0 114px; }}
.chip {{ font-size: 19px; color: #9A9E9F; background: #141618; border: 2px solid #34383C; border-radius: 9px; padding: 7px 16px; white-space: nowrap; }}
.term {{ margin-top: 40px; background: #141618; border: 2px solid #34383C; border-radius: 14px; overflow: hidden; }}
.bar {{ height: 34px; display: flex; align-items: center; gap: 9px; padding: 0 16px; border-bottom: 2px solid #26292C; background: #181A1D; }}
.bar i {{ width: 11px; height: 11px; border-radius: 50%; background: #34383C; display: block; }}
pre {{ margin: 0; padding: 22px 36px 26px; font: 400 22px/1.5 "IBM Plex Mono", monospace; white-space: pre; }}
.ps {{ color: #2FD07A; font-weight: 600; }} .cmd {{ color: #EDEDEA; font-weight: 500; }}
.b {{ font-weight: 600; color: #EDEDEA; }} .d {{ color: #7E8386; }}
.pgm {{ color: #D62718; font-weight: 600; }} .pvw {{ color: #2FD07A; font-weight: 600; }}
'''

TITLE = ('<div class="title"><span class="p">$&nbsp;</span><span class="bmd">bmd</span>'
         '<span class="ctl">ctl</span><span class="cur"></span></div>')

PAGE = f'''<!doctype html><html><head><meta charset="utf-8">{FONT_LINK}<style>{CSS}</style></head><body>
<div class="col">
  <div class="lockup"><img src="{MARK}" alt=""><div>{TITLE}
    <div class="tag"># control Blackmagic devices from the command line</div></div></div>
  <div class="chips"><span class="chip">ATEM · HyperDeck · Videohub · Ultimatte</span>
    <span class="chip">Mac · Windows · Linux</span></div>
  <div class="term"><div class="bar"><i></i><i></i><i></i></div><pre>{term(LINES)}</pre></div>
</div>
</body></html>'''

# The new headless Chrome's window is taller than the page it shows, so
# the page is shot 200px taller and cropped back to its size.
page_path = os.path.join(HERE, 'launch.html')
png = os.path.join(HERE, 'launch.png')
with open(page_path, 'w') as f:
    f.write(PAGE)
try:
    subprocess.run(['google-chrome', '--headless=new', '--hide-scrollbars', '--disable-gpu',
                    '--allow-file-access-from-files', f'--window-size={W},{H + 200}',
                    '--virtual-time-budget=8000', f'--screenshot={png}', 'file://' + page_path],
                   check=True, capture_output=True)
finally:
    os.remove(page_path)
im = Image.open(png).convert('RGB').crop((0, 0, W, H))
im.save(png, optimize=True)

# Everything but the ground must sit at least INSET px inside the frame.
ground = Image.new('RGB', im.size, (0x0A, 0x0B, 0x0C))
left, top, right, bottom = ImageChops.difference(im, ground).getbbox()
assert left >= INSET and top >= INSET and W - right >= INSET and H - bottom >= INSET, (left, top, right, bottom)
print(f'launch.png  {W}x{H}  content {left},{top} to {right},{bottom}: '
      f'{left}px left, {top}px top, {W - right}px right, {H - bottom}px bottom')
