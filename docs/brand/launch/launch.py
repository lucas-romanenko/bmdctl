# SPDX-License-Identifier: MIT
"""Write and render the images for bmdctl's launch post (docs/BRAND.md,
"Launch images"): a hero, the title over a terminal, and two smaller
ones, four kinds of device and a cue file, for LinkedIn's three-image
layout (the first large, the other two below it).

    python3 launch.py

run from docs/brand/launch/, writes launch-{1,2,3}.png beside it (needs
google-chrome and Pillow; IBM Plex Mono comes from Google Fonts, as in
social-preview.html). The terminals hold the CLI's own output, from
cmd/bmdctl/testdata/terminal and docs/guide/exec.md, with the goldens'
"#" timings filled in and each status ending at column 72, as bmdctl
ends it. Every name in them is a stand-in.
"""

import html
import os
import re
import subprocess

from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
MARK = '../../../.github/mark/bmdctl-mark-512-dark.png'
FONT_LINK = ('<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600'
             '&display=block" rel="stylesheet">')
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
        if line.startswith('$ '):
            out.append('<span class="ps">$</span> <span class="cmd">' + html.escape(line[2:]) + '</span>')
            continue
        s = html.escape(line, quote=False)
        s = s.replace('&lt;/&gt;', '</span>')
        for tag, cls in (('b', 'b'), ('dim', 'd'), ('red', 'pgm'), ('green', 'pvw')):
            s = s.replace(f'&lt;{tag}&gt;', f'<span class="{cls}">')
        out.append(s)
    return '\n'.join(out)


HERO = [
    '$ bmdctl status',
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
    '$ bmdctl cut',
    '<b>STUDIO A</>  <dim>PGM CAM2 · PVW CAM1</>',
    result('  cut         <red>CAM1</> on program · <green>CAM2</> on preview', '30 ms'),
]

DEVICES = [
    '$ bmdctl --host router route 5 7',
    '<b>Studio Router</>',
    result('  route Monitor A  Replay → Program', '18 ms'),
    '',
    '$ bmdctl --host deck play --speed 50',
    '<b>Deck 1</>  <dim>stopped · clip 1</>',
    result('  play        stopped → play 50%', '16 ms'),
    '',
    '$ bmdctl --host keyer presets',
    '<b>Keyer A</>  <dim>3 presets</>',
    '',
    '  <dim>1</>  Green Screen',
    '  <dim>2</>  Blue Screen',
    '  <dim>3</>  Studio B',
    '',
    '$ bmdctl --host studio cut',
    '<b>STUDIO A</>  <dim>PGM CAM1 · PVW CAM2</>',
    result('  cut         <red>CAM2</> on program · <green>CAM1</> on preview', '31 ms'),
]

CUE = [
    '$ cat open.cue',
    '<dim># open the show</>',
    'preview cam1',
    'transition mix 1:00',
    'auto',
    'usk 1 fill cam3',
    'usk 1 on-air on',
    '',
    '$ bmdctl exec open.cue',
    '<b>STUDIO A</>  <dim>PGM CAM2 · PVW CAM3 · open.cue · 5 lines</>',
    result('  2  preview     CAM3 → <green>CAM1</>', '18 ms'),
    result('  3  transition mix  1:00', 'already'),
    result('  4  auto        <red>CAM1</> on program · <green>CAM2</> on preview', '29 ms'),
    result('  5  usk 1 fill  CAM4 → CAM3', '20 ms'),
    result('  6  usk 1 on-air  off → <red>on</>', '22 ms'),
    '  <dim>done · 5 ok</>',
]

CSS = '''
html, body { margin: 0; width: Wpx; height: Hpx; overflow: hidden; background: #0A0B0C; }
body { font-family: "IBM Plex Mono", monospace; color: #EDEDEA; position: relative; }
.lockup { display: flex; align-items: center; gap: 72px; }
.lockup img { width: 168px; height: 168px; display: block; }
.title { font-size: 120px; line-height: 1; white-space: nowrap; display: flex; align-items: baseline; }
.title .p { color: #2FD07A; font-weight: 600; }
.title .bmd { color: #9A9E9F; font-weight: 400; letter-spacing: -0.04em; }
.title .ctl { color: #EDEDEA; font-weight: 600; letter-spacing: -0.04em; }
.title .cur { display: inline-block; width: 0.53em; height: 0.83em; background: #EDEDEA; margin-left: 0.15em; transform: translateY(0.02em); }
.tag { font-size: 40px; color: #7E8386; margin-top: 28px; white-space: nowrap; }
.chips { display: flex; gap: 24px; margin-top: 48px; }
.chip { font-size: 34px; color: #9A9E9F; background: #141618; border: 2px solid #34383C; border-radius: 14px; padding: 14px 28px; white-space: nowrap; }
.cap { font-size: 46px; color: #7E8386; white-space: nowrap; }
.term { background: #141618; border: 2px solid #34383C; border-radius: 24px; overflow: hidden; }
.bar { height: 64px; display: flex; align-items: center; gap: 16px; padding: 0 28px; border-bottom: 2px solid #26292C; background: #181A1D; }
.bar i { width: 20px; height: 20px; border-radius: 50%; background: #34383C; display: block; }
pre { margin: 0; padding: 44px 56px 52px; font: 400 FSpx/1.5 "IBM Plex Mono", monospace; white-space: pre; }
.ps { color: #2FD07A; font-weight: 600; } .cmd { color: #EDEDEA; font-weight: 500; }
.b { font-weight: 600; color: #EDEDEA; } .d { color: #7E8386; }
.pgm { color: #D62718; font-weight: 600; } .pvw { color: #2FD07A; font-weight: 600; }
.tally { position: absolute; left: 0; right: 0; bottom: 0; height: 12px; display: flex; }
.tally span { flex: 1; } .tally span:first-child { background: #D62718; } .tally span:last-child { background: #2FD07A; }
'''


def page(w, h, fs, body):
    css = CSS.replace('W', str(w)).replace('H', str(h)).replace('FS', str(fs))
    return (f'<!doctype html><html><head><meta charset="utf-8">{FONT_LINK}<style>{css}</style></head>'
            f'<body>{body}<div class="tally"><span></span><span></span></div></body></html>')


def window(lines):
    return f'<div class="term"><div class="bar"><i></i><i></i><i></i></div><pre>{term(lines)}</pre></div>'


TITLE = ('<div class="title"><span class="p">$&nbsp;</span><span class="bmd">bmd</span>'
         '<span class="ctl">ctl</span><span class="cur"></span></div>')

pages = {
    'launch-1': page(2400, 1880, 40, f'''
<div style="position:absolute; left:50%; transform:translateX(-50%); top:130px; display:flex; flex-direction:column; align-items:flex-start;">
  <div class="lockup"><img src="{MARK}" alt=""><div>{TITLE}
    <div class="tag"># control Blackmagic devices from the command line</div></div></div>
  <div class="chips" style="margin-left:240px;"><span class="chip">ATEM · HyperDeck · Videohub · Ultimatte</span>
    <span class="chip">Mac · Windows · Linux · Docker</span></div>
  <div style="margin-top:84px;">{window(HERO)}</div>
</div>'''),
}
for name, lines, cap in (('launch-2', DEVICES, '# one command line for four kinds of device'),
                         ('launch-3', CUE, '# the show open, written once, run with one command')):
    pages[name] = page(2400, 1560, 40, f'''
<div style="position:absolute; left:50%; top:50%; transform:translate(-50%, -50%); display:flex; flex-direction:column; align-items:flex-start;">
  <div class="cap" style="margin-bottom:56px;">{cap}</div>{window(lines)}
</div>''')

# The new headless Chrome's window is taller than the page it shows, so
# each page is shot 200px taller and cropped back to its size.
for name, text in pages.items():
    w, h = map(int, re.search(r'width: (\d+)px; height: (\d+)px', text).groups())
    page_path = os.path.join(HERE, name + '.html')
    png = os.path.join(HERE, name + '.png')
    with open(page_path, 'w') as f:
        f.write(text)
    try:
        subprocess.run(['google-chrome', '--headless=new', '--hide-scrollbars', '--disable-gpu',
                        '--allow-file-access-from-files', f'--window-size={w},{h + 200}',
                        '--virtual-time-budget=8000', f'--screenshot={png}', 'file://' + page_path],
                       check=True, capture_output=True)
    finally:
        os.remove(page_path)
    Image.open(png).crop((0, 0, w, h)).save(png, optimize=True)
    print(f'{name}.png  {w}x{h}')
