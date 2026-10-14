# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate macro.json: what bmdwire's macro bytecode codec does.

    python3 -I gen_macro.py /path/to/bmdwire macro.json [fixtures/macro]

/path/to/bmdwire must be a checkout of bmdwire at tag v1.4.0 (which has
bmdwire#22, the fly-key locations; #23, dual-mono Fairlight inputs; #24,
macro floats to six significant digits; and #30, a Fairlight strip op with
no sourceId as the stereo strip). The C
extension (atemwire.mediaconvert) is stubbed; nothing here touches it. The
third argument is the directory of macro bytecode samples; it defaults to
../macro beside this script (internal/fixtures/macro).

Everything goes through atemwire.macrotransfer's public functions,
decode_macro_bytecode and encode_single_op / encode_macro_bytecode, from a
fixed seed:

  decode  bytecode in, the op dicts out (ids, attribute names in dict order,
          values), the warning that stopped decoding if any, and what
          encode_macro_bytecode then makes of those ops (bytes, or the
          exception's class and message). Kinds: "file" (every sample in
          fixtures/macro), "op" (random and edge-case parameter bytes for
          every op code atemwire knows, plus unknown ones) and "stream"
          (multi-op bytecode: empty, truncated, bogus lengths, mixtures).
  encode  op dicts built from attribute pools (valid enum names, numbers,
          numeric strings, out-of-range values, missing attributes, unknown
          names, garbage) and what encode_single_op returns or raises.

Values are written one case per line. A string value too long to inline
is written as {"repeat": s, "times": n}, a message quoting one as {"head",
"repeat", "times", "tail"}; output bytes over 1 KiB as their length and
SHA-256. The Go test in atem/macro replays every case.

Exception messages (and decodeError attributes) are CPython's own wording,
so the file records the interpreter's version; it was generated with 3.12.
Another minor version may word some of them differently.
"""
import hashlib
import json
import logging
import os
import platform
import random
import re
import struct
import sys
import types

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
FIXTURES = (sys.argv[3] if len(sys.argv) > 3 else
            os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'macro'))
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

import atemwire.macrotransfer as MT  # noqa: E402
from atemwire.macrotransfer import _helpers as H  # noqa: E402
from atemwire.macrotransfer import fairlight as FL  # noqa: E402
from atemwire.macrotransfer import switching as SW  # noqa: E402
from atemwire.messages.input_video import VIDEO_MODE_NAMES  # noqa: E402

rng = random.Random(1337)


class Capture(logging.Handler):
    def __init__(self):
        super().__init__(logging.WARNING)
        self.msgs = []

    def emit(self, record):
        self.msgs.append(record.getMessage())


cap = Capture()
log = logging.getLogger('atemwire.macrotransfer')
log.addHandler(cap)
log.setLevel(logging.WARNING)
log.propagate = False


def rand(n):
    return bytes(rng.randrange(256) for _ in range(n))


class Rep:
    """A long string, written to the JSON in compact form."""

    def __init__(self, s, n):
        self.s, self.n = s, n

    def value(self):
        return self.s * self.n

    def json(self):
        return {'repeat': self.s, 'times': self.n}


def val(v):
    return v.value() if isinstance(v, Rep) else v


def ser(v):
    return v.json() if isinstance(v, Rep) else v


def cls(e):
    return 'struct.error' if isinstance(e, struct.error) else type(e).__name__


def result(fn):
    try:
        b = fn()
    except Exception as e:  # noqa: BLE001
        return {'error': cls(e), 'message': ser_long(str(e))}
    if len(b) > 1024:
        return {'len': len(b), 'sha256': hashlib.sha256(b).hexdigest()}
    return {'bytes': b.hex()}


def ser_long(s):
    # Messages quoting a Rep value are as long as the value; keep them short
    # in the JSON the same way.
    for unit in ('9', '1', 'ab', 'f', 'x'):
        if len(s) > 1024 and unit * 1000 in s:
            head, _, rest = s.partition(unit * 1000)
            n = 1000
            while rest.startswith(unit):
                rest = rest[len(unit):]
                n += 1
            return {'head': head, 'repeat': unit, 'times': n, 'tail': rest}
    return s


def ops_json(ops):
    out = []
    for d in ops:
        items = list(d.items())
        assert items[0][0] == 'id', d
        for k, v in items:
            assert isinstance(v, str), (k, v)
        out.append({'id': d['id'], 'params': [[k, v] for k, v in items[1:]]})
    return out


decode_cases = []


def decode_case(kind, name, raw):
    cap.msgs.clear()
    ops = MT.decode_macro_bytecode(raw)
    stops = [m for m in cap.msgs if m.startswith('macro bytecode')]
    assert len(stops) <= 1, stops
    c = {'kind': kind, 'name': name, 'raw': raw.hex(), 'ops': ops_json(ops)}
    if stops:
        c['stop'] = stops[0]
    c['encode'] = result(lambda: MT.encode_macro_bytecode(ops))
    decode_cases.append(c)
    return ops


def pack_op(code, params):
    return struct.pack('<HH', 4 + len(params), code) + params


KNOWN = list(MT._KNOWN_OPS.items())  # dict order


def natural_len(code, dec, enc):
    """Length of the parameter block atemwire's encoder writes for the op."""
    attrs = dec(bytes(32), None)
    return len(enc(dict(attrs, id='x')))


NATURAL = {code: natural_len(code, d, e) for code, (_, d, e) in KNOWN}

# --- (a) the bytecode samples ----------------------------------------------

for fn in sorted(os.listdir(FIXTURES)):
    with open(os.path.join(FIXTURES, fn), 'rb') as f:
        decode_case('file', fn, f.read())

# --- (b) every op code: random and edge-case parameter bytes ---------------

# Parameter blocks that walk an op's enum or value byte through its table.
TARGETED = {
    0x000C: [bytes([m, 0, 0, 0]) for m in list(range(32)) + [255]],
    0x0083: [bytes([0, s, 0, 0]) for s in range(7)] + [bytes([0, 255, 0, 0])],
    0x0084: [bytes([0, 0]) + struct.pack('<H', m) for m in
             (0, 1, 2, 3, 5, 0x1e, 0x1f, 0x20, 0x21, 0xffff, 0x8000)],
    0x007D: [bytes([1, 0x88]) + struct.pack('<H', p) for p in list(range(20)) + [255, 0xffff]],
    0x0034: [bytes([0, p, 0x6c, 0x02]) for p in list(range(14, 37)) + [255]],
    0x0028: [bytes([0, 1, t, 3]) for t in range(6)],
    0x0056: [bytes([0, 1, loc, 2]) for loc in range(11)] + [bytes([0, 1, 255, 2])],
    0x014A: [struct.pack('<HHQHH', 1, 0x4346, 0xFFFFFFFFFFFF0100, s, 12) for s in (0, 1, 2, 3, 4, 8, 0xffff)],
    0x014F: [struct.pack('<HHQBBH', 2, 0x03E1, 0xFFFFFFFFFFFF0000, 3, 0xAC, s)
             for s in (0, 1, 2, 3, 4, 8, 16, 32, 64, 0xffff)],
    0x0150: [struct.pack('<HHQBBH', 1301, 0x03E1, 0xFFFFFFFFFFFFFF00, 1, 0xAC, r)
             for r in (0, 1, 2, 3, 4, 8, 16, 0xffff)],
    0x0002: [bytes([0, 0]) + struct.pack('<H', s) for s in
             (0, 1, 20, 21, 1000, 2001, 2002, 3010, 3011, 3020, 3021, 1301, 1401, 4000, 4010, 4030,
              7001, 7002, 5010, 5011, 8001, 8006, 8007, 9000, 10010, 10011, 6000, 0xffff)],
    0x0110: [bytes([2, 0, 0x4a, 0x02]) + bytes(ip) for ip in
             ([40, 30, 20, 10], [0, 0, 0, 0], [255, 255, 255, 255], [10, 2, 0, 192])],
    0x0007: [struct.pack('<I', f) for f in (0, 1, 30, 0xffffffff)],
}
# Q16.16 values at the six-decimal rounding ties and other formatting edges,
# and since bmdwire#24's six significant digits: the silenced gain and its
# neighbours, exact ties at the seventh digit (12345.25, 12345.75, 1234.125,
# 123.0625, 12.03125, 1.015625, 0.1015625), the switch to an exponent below
# 1e-4 (6 and 7) and a carry to the next digit (655359).
FIXED = [0, 1, -1, 2, 3, 512, 1536, 2560, -512, -1536, 32768, 65536, -65536, -1048576, -1048577,
         471859, 589824, 98304, 0x7fffffff, -0x80000000, 33, 65535, 6553, 5243, -5243, -3277, -70779,
         -21474836, -21474837, -21474835, 809058304, 809091072, -80879616, 8065024, 788480, 66560,
         6656, 6, 7, -7, 655359]
for code in (0x0029, 0x004A, 0x0036, 0x0058, 0x007E, 0x001C, 0x009C):
    head = {0x007E: bytes([0, 0x88, 0x6c, 0x02]), 0x001C: bytes([1, 0, 0x6c, 0x02]),
            0x009C: bytes([1, 0, 0, 0])}.get(code, bytes([0, 1, 0x0c, 0]))
    TARGETED.setdefault(code, []).extend(head + struct.pack('<i', v) for v in FIXED)
for code in (0x014B, 0x0153, 0x016B):
    for v in FIXED:
        if code == 0x016B:
            p = struct.pack('<i', v)
        elif code == 0x0153:
            p = struct.pack('<HHQBBHi', 1, 0x03E1, 0xFFFFFFFFFFFF0000, 2, 0xAC, 12, v)
        else:
            p = struct.pack('<HHQi', 1, 0x4346, 0xFFFFFFFFFFFF0000, v)
        TARGETED.setdefault(code, []).append(p)
TARGETED[0x0151] = [struct.pack('<HHQBBHI', 1, 0x03E1, 0, 2, 0xAC, 12, f) for f in (0, 168, 0xffffffff)]

def short_blocks(dec, n):
    """Parameter blocks shorter than the op's: one per place the decoder
    fails (the exception and the offset it names), at its shortest and
    longest length, so every decoder error is recorded."""
    sites = {}
    for k in range(n):
        try:
            dec(bytes(k), None)
            continue
        except Exception as e:  # noqa: BLE001
            site = (type(e).__name__, re.sub(r'actual buffer size is \d+', '', str(e)))
        sites.setdefault(site, []).append(k)
    out = []
    for lens in sites.values():
        for k in sorted({lens[0], lens[-1]}):
            out.append(rand(k))
    return out


for code, (xml_id, dec, enc) in KNOWN:
    n = NATURAL[code]
    blocks = [rand(n) for _ in range(3)] + [bytes(n), b'\xff' * n, rand(n + 3)]
    blocks += short_blocks(dec, n)
    blocks += TARGETED.get(code, [])
    for p in blocks:
        decode_case('op', xml_id, pack_op(code, p))

for code in (0x0000, 0x0001, 0x0006, 0x0091, 0x0185 + 0x100, 0x9999, 0xABCD, 0xFFFF):
    for p in (b'', rand(1), rand(4), rand(9)):
        decode_case('op', f'0x{code:04x}', pack_op(code, p))

# --- (d) streams ------------------------------------------------------------

op_raws = [bytes.fromhex(c['raw']) for c in decode_cases if c['kind'] == 'op' and 'stop' not in c]
for raw in (b'', b'\x08', b'\x08\x00', b'\x08\x00\x02', b'\x00\x00\x04\x00', b'\x01\x00\x04\x00',
            b'\x02\x00\x04\x00\x00', b'\x03\x00\x04\x00\x00\x00', b'\x04\x00\x04\x00',
            b'\x04\x00\x02\x00', b'\x09\x00\x04\x00\x00\x00\x00\x00', b'\xff\xff\x04\x00' + bytes(20)):
    decode_case('stream', 'edge', raw)
for i in range(60):
    parts = [rng.choice(op_raws) for _ in range(rng.randrange(2, 7))]
    tail = rng.choice([b'', b'', rand(1), rand(3), b'\x04\x00\x02\x00', b'\x03\x00\x04\x00',
                       b'\x10\x00\x02\x00\x00\x00', pack_op(0x0002, b'')])
    if rng.random() < .3:
        parts.insert(rng.randrange(len(parts)), pack_op(rng.choice(KNOWN)[0], rand(rng.randrange(3))))
    decode_case('stream', 'mix', b''.join(parts) + tail)

# --- (c) encode: op dicts from attribute pools ------------------------------

IDX = ['0', '1', '3', '255', '256', '-1', '300', ' 2 ', '1_0', '0x1', '', 'abc', '2.0', '+7',
       '\u0663', '\uff17', '99999999999999999999', '-256', '\t4\n', '\x1c1', '1__0', '_1', '1_',
       '\u20031\u2003', '0 1', '00012', '-0', '1\x00']
U16 = ['0', '25', '65535', '65536', '70000', '-1', '-65536', 'abc', '1.5', '', ' 12 ', '1_000',
       '00012', '+5', '4294967296', '0x10', '\u0967\u0968', '-70000', '99999999999999999999999']
U32 = ['0', '30', '4294967295', '4294967296', '-1', '', 'abc', '12_000', '18446744073709551616',
       '1e3', ' 7 ', '-4294967297']
FP = ['0', '0.5', '-0.5', '1', '-16', '16.000015', '-16.000015', '1e3', '32767.99',
      '32767.99998474121', '32767.999992', '32768', '-32768', '-32768.00001', '65535.99', '65536',
      '65536.5', '1e10', '-1e10', '1e308', '-1e308', '1e309', 'inf', '-inf', 'Infinity', '+nan', 'nan',
      'NaN', 'abc', '', ' 1_0.5 ', '0.0078125', '1.5e-5', '7.2', '-0.05', '-1.08', '1_.5', '.5', '5.',
      '.', '1e', '0x10', '\u0663.5', '2.5', '0.00000762939453125', '0.0000228881835937500',
      '0.00003814697265625', '-0.00003814697265625', '\u22121', '1e-400', '-0', '-0.0', '1E+2',
      '+.5e1', '1_000.000_1', '1__0', 'infinity', 'infin', '-iNf', ' nan ', '0.1', '-37.5', '84.25',
      '4294967296', '1e19', '-9.5', '\u2003-2.5\u2003', '1\x00']
BOOL = ['True', 'False', 'true', 'TRUE', '1', '0', 'yes', 'Yes', 'on', 'On', ' true ', 'off', '',
        'garbage', '\x1ctrue', '\u3000yes', 'True\n', 'ON', '2', ' 1', 'YES ', 'tRuE', '\u212a']
SOURCE = ['Camera1', 'Camera20', 'Camera21', 'Camera0', 'Camera-1', 'Camera 5', 'Camera+1_0',
          'Camera\u0663', ' Black ', 'Black', 'Bars', 'Color1', 'Color2', 'MediaPlayer1',
          'MediaPlayer1Key', 'MediaPlayer2', 'MediaPlayer2Key', 'XLRMic', 'ExternalTRS', 'ProgramOut',
          'PreviewOut', 'CleanFeed1', 'CleanFeed2', 'ME1Program', 'ME1Preview', 'Auxiliary1',
          'Auxiliary2', 'Auxiliary3', 'Auxiliary4', 'Auxiliary5', 'Auxiliary6', 'MultiViewer1', '3010',
          '65535', '65536', '70000', '-1', 'Nonsense', 'camera1', '', '10010', '4030', 'Camera',
          'CameraX', 'Auxiliary7', 'black', '1_000', '\u2003Bars\u2003', 'Camera1.0', "Cam'era",
          'Ca"m\'e\\ra', 'Camera\x00', '\x1cBlack', 'Black\x7f', 'Camera\u00e9', 'Camera\U0001F600',
          'Camera\u200b', 'Camera\u0085', '99999999999999999999', 'Black\x1f', '\x1d3\x1e']
SOURCE_ID = ['18446744073709486336', '0', '-65280', '-256', '-1', '18446744073709551616',
             '18446744073709551615', 'abc', '', ' 12 ', '1.0', '1_0', '\u0661']
ENUMS = {
    'style': ['Mix', 'Dip', 'Wipe', 'DVE', 'Stinger', 'mix', 'Unknown', '', '3', '7', ' Wipe'],
    'source': ['Background', 'Key1', 'Background,Key2', 'Background,Key1,Key3', 'Key4', 'Key5',
               ' Background , Key1 ', '', 'Background,,Key1', 'background', 'Key1,Key1',
               'Key1,Key2,Key3,Key4,Background', ',', 'Foo', 'Key0', 'Key2\u2003,\x1cKey3'],
    'wipe': list(H._WIPE_PATTERN_NAMES.values()) + ['HorizontalBar', '', '5', '65536', '-1', '70000',
                                                    ' 3 ', 'abc', 'leftToRightBar', '0x3'],
    'dve': list(H._DVE_PATTERN_NAMES.values()) + ['SpinCWBottomLeft', '16', '300', '-1', '', '0',
                                                  'pushTop', '1_6'],
    'type': ['Luma', 'Chroma', 'Pattern', 'DVE', 'luma', '2', '', 'Foo'],
    'location': list(H._FLYKEY_LOCATION_NAMES.values()) + ['MiddleCentre', '5', '0', '256', '-1',
                                                         'foo', '', ' 7 '],
    'mixType': ['Off', 'On', 'AFV', 'AudioFollowVideo', 'off', '1', '4', '', ' On'],
    'shape': list(FL._FAIRLIGHT_EQ_SHAPE_NAMES.values()) + ['Peak', '3', '64', '65536', '-1', '',
                                                         'bandPass', '70000'],
    'eqrange': list(FL._FAIRLIGHT_EQ_RANGE_NAMES.values()) + ['Mid', '3', '65537', '', '-2', 'high'],
    'videoMode': list(VIDEO_MODE_NAMES) + ['1080p59.94', '255', '256', '-1', '27', ' 27 ', '',
                                           'abc', '1080P60', '\u0662\u0667', '0x1b'],
    'networkAddress': ['10.20.30.40', '192.0.2.10', '1.2.3.4 junk', '0x7f.1', '10.1', '4294967295',
                       '4294967296', '256.1.1.1', '1.2.3.4.5', '', 'abc', '1.2.3.\u00e9',
                       '1.2.3.4 \u00e9', '1.2\x00', '1.2.3.256', '1.2.65535', '1.2.65536',
                       '1.16777215', '1.16777216', '010.0x10.8.1', '08.1.1.1', '0x', '0xfg', '1..2',
                       '1.2.3.', ' 1.2.3.4', '1.2.3.4\t', '1.2.3.4\n5', '+1.2.3.4', '0x7fffffff',
                       '037777777777', '040000000000', '1.2.3.4\x1c', '99999999999999999999'],
    'hdinput': ['7', '0', '65535', '65536', '-1', 'abc', '', ' 9 ', '1_2'],
}
LONG_DIGITS = Rep('9', 4301)
LONG_JUNK = Rep('x', 300)  # int()'s message quotes at most 200 characters of it
RAW_PARAMS = ['deadbeef', '', 'de ad', 'abc', 'zz', 'DEADBEEF', ' de ad ', '\u00e9', '0', 'd e',
              'de\x00ad', 'de\tad\x0b', 'dead\x1c', 'ab\u00e9cd', '0x12', Rep('ab', 65531),
              Rep('ab', 65532)]

FP_ATTRS = {'clip', 'gain', 'xSize', 'ySize', 'xPosition', 'yPosition', 'top', 'bottom', 'left',
            'right', 'width', 'softness', 'hue', 'saturation', 'luminescence', 'outerWidth',
            'innerWidth', 'direction', 'foregroundLevel', 'backgroundLevel', 'keyEdge',
            'spillSuppress', 'flareSuppress', 'foregroundBrightness', 'foregroundContrast',
            'foregroundColour', 'foregroundRed', 'foregroundGreen', 'foregroundBlue', 'size',
            'threshold', 'ratio', 'attack', 'hold', 'release', 'qFactor'}
U16_ATTRS = {'rate', 'preRoll', 'clipDuration', 'triggerPoint', 'mixRate', 'innerSoftness',
             'outerSoftness', 'opacity', 'altitude', 'keyFrameIndex', 'index'}
IDX_ATTRS = {'mixEffectBlockIndex', 'keyIndex', 'auxiliaryIndex', 'colorGeneratorIndex', 'slot', 'band'}
BOOL_ATTRS = {'enabled', 'enable', 'onAir', 'tie', 'invert', 'preMultiply', 'reverse', 'flipFlop'}


def pool(xml_id, attr):
    if attr == 'input':
        return ENUMS['hdinput'] if xml_id == 'HyperDeckInput' else SOURCE
    if attr == 'range':
        return ENUMS['eqrange'] if 'Equaliser' in xml_id else FP
    if attr == 'pattern':
        return ENUMS['wipe'] if 'Wipe' in xml_id else ENUMS['dve']
    if attr == 'mediaPlayer':
        return U16 if 'Stinger' in xml_id else IDX
    if attr == 'sourceId':
        return SOURCE_ID
    if attr in ENUMS:
        return ENUMS[attr]
    if attr in FP_ATTRS:
        return FP
    if attr in U16_ATTRS:
        return U16
    if attr in IDX_ATTRS:
        return IDX
    if attr in BOOL_ATTRS:
        return BOOL
    if attr in ('frames', 'frequency'):
        return U32
    raise SystemExit(f'{xml_id}.{attr}: no pool')


encode_cases = []


def encode_case(op_id, params):
    d = {'id': op_id}
    for k, v in params:
        d[k] = val(v)
    c = {'id': op_id, 'params': [[k, ser(v)] for k, v in params]}
    c.update(result(lambda: MT.encode_single_op(d)))
    encode_cases.append(c)


WINDOW = 4
counters = {}
for code, (xml_id, dec, enc) in KNOWN:
    encode_case(xml_id, list(dec(rand(NATURAL[code]), None).items()))
    base = list(dec(bytes(NATURAL[code]), None).items())
    encode_case(xml_id, list(reversed(base)) + [('bogus', 'x')])
    encode_case(xml_id, [(k, 'x') for k, _ in base])  # garbage everywhere
    for i, (attr, _) in enumerate(base):
        encode_case(xml_id, base[:i] + base[i + 1:])  # attribute missing
        values = pool(xml_id, attr)
        if any(values is v for v in ENUMS.values()):
            chosen = values  # an op-specific table: all of it
        else:
            k = counters.get(id(values), 0)
            chosen = [values[(k + j) % len(values)] for j in range(WINDOW)]
            counters[id(values)] = k + WINDOW
        for v in chosen:
            encode_case(xml_id, base[:i] + [(attr, v)] + base[i + 1:])

# Every pool value at least once against one representative op per helper.
REPRESENTATIVE = {
    'ProgramInput': ('input', SOURCE), 'KeyFillInput': ('input', SOURCE),
    'FairlightAudioMixerInputSourceFaderGain': ('input', SOURCE),
    'DownstreamKeyFillInput': ('input', SOURCE), 'TransitionDipInput': ('input', SOURCE),
    'Cut': ('mixEffectBlockIndex', IDX), 'TransitionMixRate': ('mixEffectBlockIndex', IDX),
    'KeyMaskTop': ('keyIndex', IDX), 'KeyType': ('keyIndex', IDX), 'LumaKeyClip': ('keyIndex', IDX),
    'FadeToBlackEnabled': ('mixEffectBlockIndex', IDX), 'HyperDeckInput': ('slot', IDX),
    'MediaPlayerSourceStillIndex': ('mediaPlayer', IDX), 'ColorGeneratorHue': ('colorGeneratorIndex', IDX),
    'FairlightAudioMixerInputSourceEqualiserBandGain': ('band', IDX),
    'AuxiliaryInput': ('auxiliaryIndex', IDX), 'DownstreamKeyRate': ('keyIndex', IDX),
    'LumaKeyClip ': ('clip', FP), 'DVEAndFlyKeyXPosition': ('xPosition', FP),
    'KeyMaskTop ': ('top', FP), 'TransitionWipeBorderWidth': ('width', FP),
    'ColorGeneratorHue ': ('hue', FP), 'FairlightAudioMixerInputSourceFaderGain ': ('gain', FP),
    'FairlightAudioMixerMasterOutFaderGain': ('gain', FP), 'FairlightAudioMixerHeadphoneOutGain': ('gain', FP),
    'FairlightAudioMixerInputSourceCompressorThreshold': ('threshold', FP),
    'FairlightAudioMixerInputSourceEqualiserBandGain ': ('gain', FP),
    'TransitionMixRate ': ('rate', U16), 'DVEKeyBorderOpacity': ('opacity', U16),
    'DownstreamKeyRate ': ('rate', U16), 'MediaPlayerSourceStillIndex ': ('index', U16),
    'TransitionStingerSourceMediaPlayer': ('mediaPlayer', U16),
    'MacroSleep': ('frames', U32), 'FairlightAudioMixerInputSourceEqualiserBandFrequency': ('frequency', U32),
    'KeyOnAir': ('onAir', BOOL), 'DownstreamKeyOnAir': ('onAir', BOOL), 'DownstreamKeyTie': ('tie', BOOL),
    'KeyMaskEnable': ('enable', BOOL), 'TransitionWipeAndDVEReverse': ('reverse', BOOL),
    'DVEKeyBorderEnable': ('enable', BOOL), 'FadeToBlackEnabled ': ('enabled', BOOL),
    'FairlightAudioMixerInputSourceCompressorEnabled': ('enabled', BOOL),
    'FairlightAudioMixerInputSourceEqualiserBandEnabled': ('enabled', BOOL),
    'DownstreamKeyMaskEnable': ('enable', BOOL),
    'FairlightAudioMixerInputSourceMixType': ('sourceId', SOURCE_ID),
    'FairlightAudioMixerInputSourceLimiterRelease': ('sourceId', SOURCE_ID),
    'FairlightAudioMixerInputSourceEqualiserBandShape': ('sourceId', SOURCE_ID),
}
by_id = {xml_id: (code, dec) for code, (xml_id, dec, _) in KNOWN}
for key, (attr, values) in REPRESENTATIVE.items():
    xml_id = key.strip()
    code, dec = by_id[xml_id]
    base = list(dec(rand(NATURAL[code]), None).items())
    i = [k for k, _ in base].index(attr)
    for v in values:
        encode_case(xml_id, base[:i] + [(attr, v)] + base[i + 1:])
for xml_id, attr in (('ProgramInput', 'input'), ('Cut', 'mixEffectBlockIndex'),
                     ('TransitionMixRate', 'rate'), ('LumaKeyClip', 'clip')):
    code, dec = by_id[xml_id]
    base = list(dec(bytes(NATURAL[code]), None).items())
    i = [k for k, _ in base].index(attr)
    for v in (LONG_DIGITS, LONG_JUNK):
        encode_case(xml_id, base[:i] + [(attr, v)] + base[i + 1:])

# Op ids: unknown names, and Unknown_0x round trips via rawParams.
for op_id in ('NotAThing', '', 'programinput', 'Cut ', ' Cut', 'Unknown', 'unknown_0x12', 'Unknown_0X12'):
    encode_case(op_id, [('mixEffectBlockIndex', '0')])
for op_id in ('Unknown_0x', 'Unknown_0xzz', 'Unknown_0x0x12', 'Unknown_0x-1', 'Unknown_0x10000',
              'Unknown_0x 12 ', 'Unknown_0x1_2', 'Unknown_0xABCD', 'Unknown_0xabcd', 'Unknown_0x0002',
              'Unknown_0x+f', 'Unknown_0x__1', 'Unknown_0x_1', 'Unknown_0x0x_1', 'Unknown_0x\u0661',
              'Unknown_0x' + 'f' * 30, 'Unknown_0xffff', 'Unknown_0x-0', 'Unknown_0x\u20031',
              'Unknown_0x1\x00', "Unknown_0x'", 'Unknown_0x12\u00e9'):
    encode_case(op_id, [('rawParams', 'deadbeef')])
    encode_case(op_id, [])
for raw in RAW_PARAMS:
    encode_case('Unknown_0x9999', [('rawParams', raw)])
    encode_case('Unknown_0x9999', [('rawParams', raw), ('decodeError', 'x')])

# --- write ------------------------------------------------------------------


def dump(c):
    return json.dumps(c, separators=(',', ':'), ensure_ascii=True)


with open(OUT, 'w') as f:
    f.write('{"bmdwire":"v1.4.0","python":%s,"generator":"internal/fixtures/oracle/gen_macro.py",\n'
            % json.dumps(platform.python_version()))
    f.write('"decode":[\n' + ',\n'.join(dump(c) for c in decode_cases) + '\n],\n')
    f.write('"encode":[\n' + ',\n'.join(dump(c) for c in encode_cases) + '\n]}\n')

kinds = {}
for c in decode_cases:
    kinds[c['kind']] = kinds.get(c['kind'], 0) + 1
errs = sum(1 for c in encode_cases if 'error' in c)
print(len(decode_cases), 'decode cases', kinds, ';', len(encode_cases), 'encode cases,', errs, 'raising')
