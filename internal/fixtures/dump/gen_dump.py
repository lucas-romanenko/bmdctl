# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate initial-dump.bin: a synthetic initial state dump for the fake
switcher (internal/fakeatem).

    python3 -I gen_dump.py /path/to/bmdwire initial-dump.bin

bmdwire's tests contain no recorded state dump, so this one is built: the
commands a 1 M/E switcher sends after the handshake, concatenated (each an
8-byte header and its payload), ending with InCm. The values are the ones
test_build_full_state_characterization.py::_make_mixerstate uses, plus the
identity, topology and timecode packets that fixture leaves out. Every
payload is packed to atemwire's Recv layout, and the script then feeds each
command to bmdwire v1.3.0's AtemProtocol.save_field_data and checks that it
parses and lands under the expected mixerstate key, so the dump is one
atemwire itself accepts.
"""
import socket
import struct
import sys
import types

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

from atemwire.protocol import AtemProtocol  # noqa: E402

cmds = []


def cmd(code, payload):
    cmds.append((code, struct.pack('>H2x4s', 8 + len(payload), code.encode()) + payload))


def name(s, n):
    return s.encode().ljust(n, b'\0')[:n]


# identity, topology, video
cmd('_ver', struct.pack('>HH', 2, 30))
cmd('_pin', name('ATEM 1 M/E Constellation HD', 40) + bytes([0x1b]) + bytes(3))
cmd('_top', bytes([1, 24, 2, 6, 0, 2, 1, 0, 10, 1, 1, 0, 1]) + bytes(3))
cmd('_MeC', bytes([0, 4]) + bytes(2))
cmd('_mpl', bytes([20, 0]) + bytes(2))
cmd('_VMC', struct.pack('>H2x', 2)
    + struct.pack('>B3x I I ?', 12, (1 << 12) | (1 << 10), 0, False) + bytes(3)
    + struct.pack('>B3x I I ?', 27, 1 << 27, 1 << 12, True) + bytes(3))
cmd('VidM', bytes([12, 0, 0, 0]))
cmd('V3sl', bytes([1, 0, 0, 0]))
cmd('AiVM', bytes([0, 0, 0, 0]))
cmd('TCCc', bytes([0, 0, 0, 0]))

# inputs: index, name, short name, default name, source ports, external
# port type, port type, availability bits, M/E bits
INPUTS = [
    (1, 'Camera 1', 'CAM1', 1, 1, 0, 0x1f, 0x01),
    (2, 'Camera 2', 'CAM2', 1, 1, 0, 0x1f, 0x01),
    (1000, 'Color Bars', 'Bars', 0, 0, 2, 0x13, 0x01),
    (2001, 'Color 1', 'COL1', 0, 0, 3, 0x03, 0x01),
    (3010, 'Media Player 1', 'MP1', 0, 0, 4, 0x13, 0x01),
    (3011, 'Media Player 1 Key', 'MP1K', 0, 0, 5, 0x10, 0x01),
    (8001, 'Auxiliary 1', 'AUX1', 0, 0, 129, 0x02, 0x00),
    (10010, 'ME 1 Program', 'PGM', 0, 0, 128, 0x03, 0x00),
]
for idx, long_, short, ports, ext, port, avail, me in INPUTS:
    cmd('InPr', struct.pack('>H 20s 4s ?x HH Bx BB', idx, name(long_, 20), name(short, 4), True, ports, ext, port, avail, me))

# buses and transition (M/E 0)
cmd('PrgI', struct.pack('>BxH', 0, 2))
cmd('PrvI', struct.pack('>BxH?3x', 0, 1, False))
for aux, src in ((0, 3010), (1, 10010), (2, 0)):
    cmd('AuxS', struct.pack('>BxH', aux, src))
cmd('TrSS', struct.pack('>B 2B 2B 3x', 0, 1, 0x03, 1, 0x03))
cmd('TrPr', struct.pack('>B?2x', 0, False))
cmd('TrPs', struct.pack('>B?Bx H 2x', 0, False, 0, 0))
cmd('TMxP', struct.pack('>BB2x', 0, 25))
cmd('TDpP', struct.pack('>BBH', 0, 30, 2001))
cmd('TWpP', struct.pack('>BBBx HHHHHH ?? 2x', 0, 20, 5, 500, 1000, 5000, 1000, 5000, 5000, False, True))
cmd('TDvP', struct.pack('>BBBB HH ?? HH ??? 3x', 0, 0, 15, 2, 3010, 3011, True, False, 500, 700, False, False, True))
cmd('TStP', struct.pack('>BB?x HH ?x HHHH 2x', 0, 1, True, 500, 700, False, 48, 150, 34, 25))

# upstream keyers: keyer 0 a luma key on air, 1-3 off
for k in range(4):
    cmd('KeOn', struct.pack('>BB?x', 0, k, k == 0))
    cmd('KeBP', struct.pack('>BBB?x?HH?x hhhh', 0, k, 0, True, False, 4 if k == 0 else 0, 5 if k == 0 else 0,
                            False, 9000, -9000, -16000, 16000))
cmd('KeLm', struct.pack('>BB?x HH ?3x', 0, 0, True, 220, 300, False))
cmd('KeFS', bytes([0, 0, 1, 1, 0, 0, 0x01, 0]))

# downstream keyers
cmd('DskB', struct.pack('>BxHH2x', 0, 6, 7))
cmd('DskP', struct.pack('>B?B? HH ?? hhhh 2x', 0, False, 30, True, 660, 825, False, True, 9000, -9000, -16000, 16000))
cmd('DskS', struct.pack('>B???B3x', 0, True, False, False, 0))
cmd('DskB', struct.pack('>BxHH2x', 1, 0, 0))
cmd('DskP', struct.pack('>B?B? HH ?? hhhh 2x', 1, False, 25, False, 0, 0, False, False, 0, 0, 0, 0))
cmd('DskS', struct.pack('>B???B3x', 1, False, False, False, 0))

# fade to black, colour generators
cmd('FtbP', struct.pack('>BB2x', 0, 50))
cmd('FtbS', struct.pack('>B??B', 0, False, False, 0))
cmd('FEna', bytes([0, 1, 0, 0]))
cmd('ColV', struct.pack('>BxHHH', 0, 1200, 750, 500))
cmd('ColV', struct.pack('>BxHHH', 1, 3000, 1000, 250))

# media and macros
cmd('MPCE', bytes([0, 1, 0, 0]))
cmd('MPCE', bytes([1, 1, 1, 0]))
cmd('MPfe', struct.pack('>Bx H ? 16s 2x', 0, 0, True, bytes(range(16))) + bytes([5]) + b'still' + bytes(2))
cmd('MPfe', struct.pack('>Bx H ? 16s 2x', 0, 1, False, bytes(16)) + bytes(4))
for slot, used, name_ in ((0, True, b'Open Show'), (1, False, b''), (2, True, b'Half Time')):
    payload = struct.pack('>H ?? H H', slot, used, False, len(name_), 0) + name_
    cmd('MPrp', payload + bytes(-len(payload) % 4))
cmd('MRPr', struct.pack('>BBH', 0, 0, 0xFFFF))
cmd('MRcS', struct.pack('>?xH', False, 0xFFFF))

# HyperDeck binding, multiviewer
cmd('RXMS', struct.pack('>H2x', 0) + socket.inet_aton('10.0.0.50') + struct.pack('>H', 2) + bytes(10))
cmd('MvPr', bytes([0, 0, 0, 0]))
for w, src in enumerate((10010, 10011, 1, 2, 3, 4, 5, 6, 7, 8)):
    cmd('MvIn', struct.pack('>BBH??2x', 0, w, src, w >= 2, w < 2))

# Fairlight (the values build_full_state's fixture uses)
cmd('FAMP', struct.pack('>x ? 4x h 2x H i ? 3x', True, 150, 0, -300, False))
cmd('FAIP', struct.pack('>HB 2x B xxxx B x B 3x', 1, 0, 0, 0x01, 0))
cmd('FAIP', struct.pack('>HB 2x B xxxx B x B 3x', 2001, 1, 0, 0x01, 0))
for idx, delay, gain, eqe, eqg, dyn, pan, vol, state in ((1, 0, 0, False, 0, 0, 0, -600, 2),
                                                          (2001, 2, 0, True, 100, 50, -2000, -1200, 4)):
    cmd('FASP', struct.pack('>H 12xBBxxB 3x h 5x ? 4x h 2x Hh 4x h x B 2x',
                            idx, 0x01, 0, delay, gain, eqe, eqg, dyn, pan, vol, state))
cmd('AEBP', struct.pack('>H 2x 4x 6x BB B ? B B x B 4x H i H 2x', 1, 0x01, 0, 0, True, 0x3f, 0x01, 1, 100, 300, 100))
cmd('AICP', struct.pack('>H 12xBB ?3x i H 2x i i i', 1, 0x01, 0, True, -1000, 200, 140, 0, 7000))
cmd('AILP', struct.pack('>H 12xBB ?3x i i i i', 1, 0x01, 0, False, -800, 110, 0, 5000))
cmd('AMBP', struct.pack('>B ? B B x B 4x H i H 2x', 0, True, 0x3f, 0x01, 1, 40, 0, 71))
cmd('MOCP', struct.pack('>B 5x h H 2x i i i', 1, -1500, 397, 140, 0, 9300))
cmd('AMLP', struct.pack('>B 5x h i i i', 1, -501, 71, 0, 9300))
cmd('FMHP', struct.pack('> i 4x ? 23x', 500, True))
cmd('FAMS', struct.pack('> ? 8x B 12x BB', True, 1, 0x01, 0))

# timecode, then the end of the dump
cmd('Time', bytes([10, 20, 30, 5, 0, 0, 0, 0]))
cmd('InCm', bytes([1, 0, 0, 0]))

# Check every command against atemwire itself.
p = AtemProtocol(ip='192.0.2.10')
p.transport.sock.close()
for code, raw in cmds:
    before = set(p.mixerstate)
    p.save_field_data(code.encode(), raw[8:])   # raises if atemwire can't parse it
    if code != 'InCm':
        assert code not in p.mixerstate, f'{code} stored raw: atemwire did not parse it'
p.transport.thread_queue.close()
mx = p.mixerstate
assert mx['program-bus-input'][0].source == 2
assert mx['video-mode'].get_label() == '1080p50'
assert mx['input-properties'][3011].name == 'Media Player 1 Key'
assert mx['fairlight-strip-properties']['2001.0'].pan == -2000
assert mx['color-generator'][1].hue == 300.0
assert mx['macro-properties'][2].name == b'Half Time'

with open(OUT, 'wb') as f:
    f.write(b''.join(raw for _, raw in cmds))
print(len(cmds), 'commands,', sum(len(r) for _, r in cmds), 'bytes;', len(mx), 'mixerstate keys')
