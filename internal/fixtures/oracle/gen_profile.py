# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate profile.json: what bmdwire's atemwire.profile saves and applies.

    python3 -I gen_profile.py /path/to/bmdwire profile.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.3.0. The C
extension (atemwire.mediaconvert) is stubbed; nothing here touches it, and
nothing touches the network: saves read a mixerstate, applies go to a
recording connection.

Four parts:

- golden: the mixerstate test_profile_granular.py builds for
  test_single_me_save_byte_identical_to_pre_4c1_golden, recorded as entries
  (mixerstate key, index path, attributes). The script checks that
  Profile.from_atem on it reproduces ../profile/profile_single_me_save_golden.xml
  byte for byte. The test builds that state from stand-in objects; two of
  them are not packets: the product name stand-in holds bytes (recorded
  decoded, as decode_name reads it) and the video mode stand-in has only a
  label (recorded as the VidM mode whose label it is, 1080p60).
- saves: Profile.from_atem on states built by feeding well-formed packets
  to AtemProtocol.save_field_data (1 to 4 M/Es, partial packet sets,
  Fairlight strips split and not, macros, HyperDecks, every video mode),
  plus the stand-in states of bmdwire's tests, under several SaveOptions,
  with each used macro's download answered with recorded bytecode, an empty
  slot or an error. Each records the state, the options, the downloads
  and the XML.
- applies: Profile.apply of the golden, of every save, of perturbed saves
  and of hand-written documents (legacy and SD video mode labels, missing
  sections, NaN, infinities, huge and malformed values, unknown enum
  names, macros whose ops raise ValueError and other errors, upload
  failures) under several ApplyOptions, onto live states of 1 and 4 M/Es
  at various video modes. A recording connection captures every command
  (its bytes, or that get_command raised: ATEMConnection's worker drops
  those), every macro upload (upload_macro_bytecode replaced; slot, name,
  description, bytecode, and an error for chosen slots), every
  time.sleep, and the ApplyResult lists or the exception apply raised.
- parses: Profile.from_xml(...).to_xml() of tricky documents, or the
  exception, for the XML layer.

A source's Fairlight strips are fed in subchannel order: atemwire writes
them in the order their packets first arrived, which bmdctl's state does
not keep. Fixed seed; output only changes if bmdwire or this script does.
"""
import dataclasses
import importlib.util
import json
import os
import random
import socket
import struct
import sys
import time
import types
import xml.etree.ElementTree as ET

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

import atemwire.macrotransfer as MT  # noqa: E402
import atemwire.messages as M  # noqa: E402
import atemwire.profile as P  # noqa: E402
from atemwire.messages.input_video import VIDEO_MODE_XML_NAMES, VideoModeField  # noqa: E402
from atemwire.profile import ApplyOptions, MixEffectOptions, Profile, SaveOptions  # noqa: E402
from atemwire.protocol import AtemProtocol  # noqa: E402

# from_atem's settle wait polls a live dump; these states are complete.
P.wait_state_settled = lambda atem, timeout=3.0: None

spec = importlib.util.spec_from_file_location(
    'granular', os.path.join(BMDWIRE, 'atemwire', 'tests', 'test_profile_granular.py'))
granular = importlib.util.module_from_spec(spec)
spec.loader.exec_module(granular)

rng = random.Random(0x9F0F11E)

# --- recording ---------------------------------------------------------------

RENAME = {'Y': 'sample_y', 'Cb': 'sample_cb', 'Cr': 'sample_cr'}


def norm(v, stand_in=False):
    if isinstance(v, bool) or v is None or isinstance(v, (int, str)):
        return v
    if isinstance(v, float):
        return v if v == v and abs(v) != float('inf') else repr(v)
    if isinstance(v, (bytes, bytearray)):
        # A stand-in's bytes stand for a str field (the product name).
        return bytes(v).decode('utf-8', 'replace') if stand_in else bytes(v).hex()
    if isinstance(v, (list, tuple)):
        return [norm(x, stand_in) for x in v]
    if isinstance(v, dict):
        return {str(k): norm(x, stand_in) for k, x in v.items()}
    return attrs(v)


def attrs(obj):
    if hasattr(obj, 'CODE'):  # a Recv instance
        return {RENAME.get(k, k): norm(v) for k, v in vars(obj).items()
                if k != 'raw' and not k.startswith('_')}
    out = {}
    for k in dir(obj):
        if k.startswith('_'):
            continue
        v = getattr(obj, k)
        if not callable(v):
            out[k] = norm(v, stand_in=True)
    return out


def entries(mx):
    out = []

    def walk(key, v, path):
        if isinstance(v, dict):
            for k, x in v.items():
                walk(key, x, path + [k])
        elif isinstance(v, (bytes, bytearray)):
            raise SystemExit(f'raw mixerstate entry {key}')
        else:
            if key == 'video-mode' and not hasattr(v, 'mode'):
                # A label-only stand-in: the mode whose label it is.
                mode = next(m for m in sorted(VideoModeField._MODE_TABLE)
                            if VideoModeField(struct.pack('>B3x', m)).get_label() == v.get_label())
                v = VideoModeField(struct.pack('>B3x', mode))
            out.append([key, path, attrs(v)])
    for key, v in mx.items():
        walk(key, v, [])
    return out


RECV_BY_PRETTY = {cls.PRETTY: cls for cls in M.RECV_BY_CODE.values() if getattr(cls, 'PRETTY', None)}


def realize(mx):
    """A copy of a stand-in mixerstate with every stand-in replaced by the
    real Recv class parsed from zeros and given the stand-in's attributes,
    so both sides read the same full set of attributes."""
    def real(key, v):
        if hasattr(v, 'CODE'):
            return v
        if key == 'video-mode':
            mode = next(m for m in sorted(VideoModeField._MODE_TABLE)
                        if VideoModeField(struct.pack('>B3x', m)).get_label() == v.get_label())
            return VideoModeField(struct.pack('>B3x', mode))
        cls = RECV_BY_PRETTY[key]
        obj = cls(bytes(min_len(cls)))
        for k, x in attrs(v).items():
            setattr(obj, k, x)
        return obj

    def walk(key, v):
        if isinstance(v, dict):
            return {k: walk(key, x) for k, x in v.items()}
        return real(key, v)
    return {k: walk(k, v) for k, v in mx.items()}


def options(o):
    return dataclasses.asdict(o)


def exc(e):
    cls = 'struct.error' if isinstance(e, struct.error) else type(e).__name__
    return {'class': cls, 'message': str(e)}


# --- packets (as gen_readers.py builds them) -----------------------------------

def min_len(cls):
    for n in range(0, 200):
        try:
            cls(bytes(n))
            return n
        except Exception:
            pass
    return None


LEN = {}
for code in ('PrgI PrvI AuxS TrSS TrPs TMxP TDpP TWpP TDvP TStP FtbS FtbP '
             'KeOn KeBP KeLm KACk KACC KePt KeDV KeFS KKFP DskB DskP DskS '
             'ColV InPr MPCE RXMS FAMP FASP AEBP AMBP AICP AILP '
             'AIXP MOCP AMLP FAIP FMHP _pin V3sl _top _MeC MvPr MvIn VuMC SaMw').split():
    LEN[code] = min_len(M.RECV_BY_CODE[code])
LEN['FEna'] = 4
LEN['KeFS'] = 8
LEN['VidM'] = 4

EDGE = [0x00, 0x00, 0x00, 0xff, 0xff, 0x01, 0x80, 0x7f]


def rb(n):
    return bytes(rng.choice(EDGE) if rng.random() < .45 else rng.randrange(256) for _ in range(n))


def pick(choices):
    return rng.choice(choices) if rng.random() < .8 else rng.randrange(256)


def u16_at(b, off, v):
    struct.pack_into('>H', b, off, v)


def i16_at(b, off, v):
    struct.pack_into('>h', b, off, v)


NAMES = ['Camera 1', 'CAM1', '  Padded  ', 'Tab\there', '', 'Ünïcödé', 'MP1',
         'a"b<c>&d', "it's", 'x' * 30, 'line\nbreak', '\u0085nel']


GARBAGE = [0.0]  # the share of names that are random bytes


def name_bytes(size):
    r = rng.random()
    if r >= GARBAGE[0]:
        raw = rng.choice(NAMES).encode()
    elif r < .7 * GARBAGE[0]:
        raw = rng.choice(NAMES).encode()
    elif r < .85 * GARBAGE[0]:
        raw = rb(rng.randrange(size + 1))
    else:
        raw = bytes([rng.choice([0xff, 0xc3, 0xe2])]) + rng.choice(NAMES).encode()
    raw = raw[:size]
    return raw + bytes(size - len(raw))


def build(code, idx=()):
    if code == 'VidM':
        return bytes([idx[0] if idx else rng.randrange(30), 0, 0, 0])
    if code == 'FEna':
        return rng.choice([b'\x00\x01\x00\x00', b'\x00\x00\x00\x00', b'\x01\x01\x00\x00', b'\x00\x02\x00\x00'])
    if code == 'MPfe':
        slot, = idx
        name = name_bytes(rng.choice([1, 4, 12, 30])).rstrip(b'\x00') or b'n'
        hsh = bytes(16) if rng.random() < .3 else rb(16)
        head = struct.pack('>Bx H ? 16s 2x', 0, slot, rng.random() < .75, hsh)
        return head + bytes([len(name)]) + name
    if code == 'MPrp':
        slot, used = idx
        nm = name_bytes(rng.choice([0, 5, 12])).rstrip(b'\x00')
        desc = name_bytes(rng.choice([0, 9, 20])).rstrip(b'\x00')
        return struct.pack('>H??HH', slot, used, False, len(nm), len(desc)) + nm + desc
    b = bytearray(rb(LEN[code]))
    if code in ('PrgI', 'PrvI', 'AuxS', 'TrPs', 'TMxP', 'TDpP', 'TWpP', 'TDvP',
                'TStP', 'FtbS', 'FtbP', 'DskB', 'DskP', 'DskS', 'ColV', 'MPCE', 'MvPr'):
        b[0] = idx[0]
        if code == 'TStP' and rng.random() < .6:
            b[1] = rng.choice([1, 2, 3, 4])
        if code == 'TDvP' and rng.random() < .6:
            b[3] = rng.randrange(14, 34)
        if code == 'TWpP':
            b[2] = pick(list(range(18)))
        if code == 'MPCE':
            b[1] = rng.choice([1, 2, 1, 0])
    elif code == 'TrSS':
        b[0] = idx[0]
        b[1] = pick([0, 1, 2, 3, 4, 5])
    elif code == '_MeC':
        b[0], b[1] = idx[0], idx[1]
    elif code in ('KeOn', 'KeBP', 'KeLm', 'KACk', 'KACC', 'KePt', 'KeDV', 'KeFS'):
        b[0], b[1] = idx
        if code == 'KeBP':
            b[2] = pick([0, 1, 2, 3])
        if code == 'KePt':
            b[2] = pick(list(range(18)))
    elif code == 'KKFP':
        b[0], b[1], b[2] = idx
    elif code in ('MvIn', 'VuMC', 'SaMw'):
        b[0], b[1] = idx
    elif code in ('InPr', 'RXMS', 'FAIP'):
        u16_at(b, 0, idx[0])
        if code == 'InPr':
            b[2:22] = name_bytes(20)
            b[22:26] = name_bytes(4)
            b[32] = pick([0, 0, 0, 1, 2, 3, 4, 128, 129])
            u16_at(b, 28, rng.choice([0, 1, 2, 4, 8, 16, 3, 24, 0xffff]))
        elif code == 'RXMS':
            ip, inp = idx[1], idx[2]
            b[4:8] = socket.inet_aton(ip)
            u16_at(b, 8, inp)
        else:
            b[2] = pick([0, 1, 2, 3])
            b[5] = pick([0, 1, 2, 3])
            b[12] = pick([0, 1, 2, 4])
    elif code in ('FASP', 'AEBP', 'AICP', 'AILP', 'AIXP'):
        source, split, sub = idx[:3]
        u16_at(b, 0, source)
        b[14], b[15] = split, sub
        if code == 'FASP':
            b[49] = pick([1, 2, 4, 3, 0])
            if rng.random() < .5:
                i16_at(b, 46, rng.choice([-10000, -10001, -9999, -32768, 1000, 0, -1]))
        elif code == 'AEBP':
            b[16] = idx[3]
            b[19] = pick([1, 2, 4, 8, 16, 32, 0, 3])
            b[21] = pick([1, 2, 4, 8, 0, 3])
        elif code == 'AIXP':
            b[17] = pick([0, 1, 2])
    elif code == 'AMBP':
        b[0] = idx[0]
        b[3] = pick([1, 2, 4, 8, 16, 32, 0, 3])
        b[5] = pick([1, 2, 4, 8, 0, 3])
    elif code == 'FAMP':
        if rng.random() < .5:
            struct.pack_into('>i', b, 12, rng.choice([-10000, -10001, -9999, 1000, 0, -1]))
    elif code == '_pin':
        b[0:40] = name_bytes(40)
    elif code == 'V3sl':
        b[0] = pick([0, 1, 2])
    return bytes(b)


MACRO_SAMPLES = []
for fn in sorted(os.listdir(os.path.join(HERE, '..', 'macro'))):
    with open(os.path.join(HERE, '..', 'macro', fn), 'rb') as f:
        MACRO_SAMPLES.append(f.read())


def macro_download():
    r = rng.random()
    if r < .55:
        return b''.join(rng.choice(MACRO_SAMPLES) for _ in range(rng.randrange(1, 6)))
    if r < .7:
        return b''
    if r < .85:
        return TimeoutError('macro download timed out')
    # Bytecode that stops making sense part way: decode keeps what came before.
    return rng.choice(MACRO_SAMPLES) + b'\x02\x00'


class Switcher:
    """A state built from packets, as save_field_data stores them."""

    def __init__(self):
        self.p = AtemProtocol(ip='192.0.2.1')
        self.p.transport.sock.close()
        self.p.transport.thread_queue.close()
        self.downloads = {}

    def feed(self, code, payload):
        try:
            self.p.save_field_data(code.encode(), payload)
        except Exception as e:
            raise SystemExit(f'{code} {payload.hex()} raised {e!r}')

    @property
    def mixerstate(self):
        return self.p.mixerstate


def order_strips(pk):
    """Keep each source's FASP packets in subchannel order (see above)."""
    idx = [i for i, (code, _) in enumerate(pk) if code == 'FASP']
    fasp = sorted((pk[i] for i in idx), key=lambda cb: (cb[1][0:2], cb[1][15]))
    for i, item in zip(idx, fasp):
        pk[i] = item
    return pk


def random_switcher(n_me, keyers, n_dsk, density, mode=None, macros=True, fairlight=True):
    sw = Switcher()
    pk = []

    def add(code, idx=(), always=False):
        if always or rng.random() < density:
            pk.append((code, build(code, idx)))
            return True
        return False

    pk.append(('_top', bytes([n_me, rng.randrange(4, 30), n_dsk]) + rb(LEN['_top'] - 3)))
    for me in range(n_me):
        add('_MeC', (me, keyers[me]), always=True)
        for code in ('PrgI', 'PrvI', 'TrSS', 'TrPs', 'TMxP', 'TDpP', 'TWpP',
                     'TDvP', 'TStP', 'FtbS', 'FtbP'):
            add(code, (me,))
        for k in range(keyers[me]):
            for code in ('KeOn', 'KeBP', 'KeLm', 'KACk', 'KACC', 'KePt', 'KeDV', 'KeFS'):
                add(code, (me, k))
            for kf in (1, 2):
                add('KKFP', (me, k, kf))
    for d in range(n_dsk):
        for code in ('DskB', 'DskP', 'DskS'):
            add(code, (d,))
    for a in rng.sample(range(8), rng.randrange(0, 6)):
        add('AuxS', (a,))
    for g in range(2):
        add('ColV', (g,))
    for slot in sorted(rng.sample(range(0, 40), 4)):
        add('MPfe', (slot,))
    for mp in range(rng.randrange(0, 4)):
        add('MPCE', (mp,))
    if macros:
        for slot in sorted(rng.sample(range(0, 100), rng.randrange(1, 6))):
            used = rng.random() < .8
            if add('MPrp', (slot, used), always=True) and used:
                sw.downloads[slot] = macro_download()
    for slot in range(rng.randrange(0, 11)):
        ip = rng.choice(['10.0.0.50', '192.0.2.10', '0.0.0.0', '0.0.0.0'])
        add('RXMS', (slot, ip, rng.choice([0, 3, 4, 65535])))
    for src in rng.sample([1, 2, 3, 4, 5, 6, 1000, 2001, 2002, 3010, 3011, 6000, 7001, 8001, 10010],
                          rng.randrange(3, 12)):
        add('InPr', (src,))
    for mv in range(rng.randrange(0, 3)):
        add('MvPr', (mv,))
        for w in rng.sample(range(10), rng.randrange(0, 6)):
            for code in ('MvIn', 'VuMC', 'SaMw'):
                add(code, (mv, w))
    if fairlight:
        for src in rng.sample([1, 2, 3, 4, 1301, 1302, 1401, 2001], rng.randrange(1, 7)):
            add('FAIP', (src,), always=True)
            split = rng.random() < .35
            for sub in ([0, 1] if split else [0]):
                if rng.random() < .85:
                    pk.append(('FASP', build('FASP', (src, 0xff if split else 0x01, sub))))
                for band in rng.sample(range(6), rng.randrange(0, 4)):
                    add('AEBP', (src, 0xff if split else 0x01, sub, band))
                for code in ('AICP', 'AILP', 'AIXP'):
                    add(code, (src, 0xff if split else 0x01, sub))
        for band in rng.sample(range(6), rng.randrange(0, 6)):
            add('AMBP', (band,))
        for code in ('FAMP', 'MOCP', 'AMLP', 'FMHP'):
            add(code)
    for code in ('FEna', '_pin', 'V3sl'):
        add(code)
    pk.append(('VidM', build('VidM', () if mode is None else (mode,))))
    rng.shuffle(pk)
    for code, b in order_strips(pk):
        sw.feed(code, b)
    return sw


# --- saves --------------------------------------------------------------------

class SaveConn:
    def __init__(self, mx, downloads):
        self.mixerstate = mx
        self.downloads = downloads
        self.asked = []

    def download_macro(self, slot, timeout=10.0):
        self.asked.append(slot)
        d = self.downloads.get(slot, TimeoutError('no such slot'))
        if isinstance(d, Exception):
            raise d
        return d


saves = []
SAVED = {}


def record_save(name, mx, opts, downloads=None, stand_in=False, state_from=None):
    downloads = downloads or {}
    conn = SaveConn(mx, downloads)
    xml = Profile.from_atem(conn, opts).to_xml()
    rec = {k: ({'error': str(v)} if isinstance(v, Exception) else {'hex': v.hex()})
           for k, v in downloads.items()}
    case = {'name': name, 'stand_in': stand_in, 'options': options(opts),
            'downloads': {str(k): v for k, v in rec.items()}, 'asked': conn.asked, 'xml': xml}
    if state_from:
        case['state_from'] = state_from  # the same state as that save's
    else:
        case['state'] = entries(mx)
    saves.append(case)
    SAVED[name] = xml
    return xml


# The golden: test_profile_granular's state, default options.
golden_mx = granular._synthetic_mixerstate()
golden_xml = Profile.from_atem(granular._S(mixerstate=golden_mx), SaveOptions()).to_xml()
with open(os.path.join(HERE, '..', 'profile', 'profile_single_me_save_golden.xml'), encoding='utf-8') as f:
    if f.read() != golden_xml:
        raise SystemExit('golden XML does not match bmdwire')
golden = {'state': entries(golden_mx)}
SAVED['golden'] = golden_xml

# The 4-M/E stand-in state, the stand-in KKFP keyframes and the DSK tests'.
record_save('granular-4me', realize(granular._synthetic_mixerstate(4)), SaveOptions())
kmx = granular._synthetic_mixerstate()
kmx['key-properties-fly-keyframe'] = {0: {0: {1: granular._KKFP(size_x=1500, size_y=500, pos_y=0),
                                              2: granular._KKFP(size_x=1500, size_y=500, pos_y=13000)}}}
mes = SaveOptions(mes=[MixEffectOptions()])
record_save('granular-keyframes', realize(kmx), mes)
dmx = granular._synthetic_mixerstate()
dmx['topology'] = granular._S(downstream_keyers=4)
dmx['dkey-state'] = {i: granular._S(on_air=False) for i in range(4)}
dmx['dkey-properties-base'] = {i: granular._S(fill_source=10 + i, key_source=20 + i) for i in range(4)}
o = SaveOptions()
o.mes[0].program = False
o.mes[0].usk = [True, False, True, True]
record_save('granular-dsk4', realize(dmx), o)

# Packet-built switchers.
SWITCHERS = {}
specs = [('sw-1me-full', 1, [4], 2, 1.0), ('sw-1me-sparse', 1, [4], 1, .35),
         ('sw-2me', 2, [4, 2], 2, .8), ('sw-4me', 4, [4, 2, 1, 4], 4, .7),
         ('sw-3me-sparse', 3, [1, 4, 2], 1, .4), ('sw-1me-dense', 1, [2], 2, .95)]
for name, n_me, keyers, n_dsk, density in specs:
    # One switcher has names of random bytes: atemwire writes them into
    # the XML as they decode, control characters included, so that save
    # doesn't parse back.
    GARBAGE[0] = 1.0 if name == 'sw-3me-sparse' else 0.0
    sw = random_switcher(n_me, keyers, n_dsk, density)
    SWITCHERS[name] = sw
    record_save(name, sw.mixerstate, SaveOptions(), sw.downloads)


def save_opts(**kw):
    o = SaveOptions()
    for k, v in kw.items():
        setattr(o, k, v)
    return o


variants = [
    ('mes-none', save_opts(mes=[MixEffectOptions.none() for _ in range(4)])),
    ('mes-short', save_opts(mes=[MixEffectOptions(program=False, usk=[False, True])])),
    ('no-fairlight-macros', save_opts(fairlight=False, macros=False, counters=False)),
    ('only-settings', save_opts(mes=[], downstream_keys=False, color_generators=False, fairlight=False,
                                camera_control=False, media_pool_metadata=False, media_players=False,
                                auxiliaries=False, counters=False, video_mode=False, hyperdecks=False,
                                macros=False)),
    ('apply-default-mes', save_opts(mes=[MixEffectOptions.apply_default() for _ in range(4)], hyperdecks=False)),
]
for (vname, opts), swname in zip(variants, ['sw-2me', 'sw-4me', 'sw-1me-full', 'sw-3me-sparse', 'sw-1me-dense']):
    sw = SWITCHERS[swname]
    record_save(f'{swname}-{vname}', sw.mixerstate, opts, sw.downloads, state_from=swname)

# The test_profile_roundtrip stand-in state.
rt = {
    'product-name': type('PN', (), {'name': b'ATEM 1 M/E Constellation HD'})(),
    'video-mode': granular._VideoMode(),
    'aux-output-source': {i: granular._S(source=10010) for i in range(6)},
    'color-generator': {i: granular._S(hue=0.0, saturation=0.0, luma=0.5) for i in range(2)},
    'mediaplayer-selected': {i: granular._S(source_type=1, slot=0) for i in range(2)},
    'input-properties': {i: granular._S(index=i, name=f'Camera {i}', short_name=f'Cam{i}', port_type=0,
                                        source_ports=1) for i in range(1, 11)},
}
for k in ('program-bus-input', 'preview-bus-input', 'transition-settings', 'transition-position',
          'transition-mix', 'transition-dip', 'transition-wipe', 'transition-dve',
          'fade-to-black', 'fade-to-black-state'):
    rt[k] = granular._synthetic_mixerstate()[k]
record_save('roundtrip-synthetic', realize(rt), SaveOptions())

# Every video mode, video mode only (test_profile_video_mode).
VM_ONLY = SaveOptions(
    mes=[MixEffectOptions.none() for _ in range(4)], downstream_keys=False, color_generators=False,
    fairlight=False, camera_control=False, media_pool_metadata=False, media_pool_images=False,
    media_players=False, auxiliaries=False, counters=False, settings=False, video_mode=True,
    hyperdecks=False, macros=False)
VM_STATES = {}
for mode in sorted(VideoModeField._MODE_TABLE):
    sw = Switcher()
    sw.feed('VidM', bytes([mode, 0, 0, 0]))
    VM_STATES[mode] = sw.mixerstate
    record_save(f'videomode-{mode}', sw.mixerstate, VM_ONLY)
record_save('videomode-none', {}, VM_ONLY)

# --- applies ------------------------------------------------------------------

EVENTS = []
FAIL_UPLOADS = {}


def fake_upload(protocol, slot, name, description, bytecode, *, timeout=10.0):
    EVENTS.append({'upload': {'slot': slot, 'name': name, 'description': description, 'bytecode': bytecode.hex()}})
    if slot in FAIL_UPLOADS:
        raise RuntimeError(FAIL_UPLOADS[slot])


MT.upload_macro_bytecode = fake_upload
time.sleep = lambda s: EVENTS.append({'sleep': s})


class ApplyConn:
    def __init__(self, mx, connected):
        self.mixerstate = mx
        self.protocol = types.SimpleNamespace(connected=connected)

    def send(self, cmd):
        try:
            b = cmd.get_command().hex()
        except Exception:
            EVENTS.append({'drop': cmd.CODE})
            return
        # Commands repeat across cases: events index a table of them.
        if b not in COMMAND_INDEX:
            COMMAND_INDEX[b] = len(COMMANDS)
            COMMANDS.append(b)
        EVENTS.append({'cmd': COMMAND_INDEX[b]})


COMMANDS, COMMAND_INDEX = [], {}


applies = []


def record_apply(name, xml=None, from_save=None, live=None, opts=None, factory=None, connected=True,
                 fail=None, macros=False):
    global EVENTS
    EVENTS = []
    FAIL_UPLOADS.clear()
    FAIL_UPLOADS.update(fail or {})
    live = live if live is not None else {}
    src = xml if xml is not None else SAVED[from_save]
    p = Profile.from_xml(src)
    if factory == 'macros_only':
        opts = ApplyOptions.macros_only()
    elif factory == 'default':
        opts = ApplyOptions()
    case = {'name': name, 'live': entries(live), 'connected': connected,
            'fail_uploads': {str(k): v for k, v in (fail or {}).items()}}
    if xml is not None:
        case['xml'] = xml
    else:
        case['from_save'] = from_save
    if factory:
        case['factory'] = factory
    else:
        case['options'] = options(opts)
    if macros:
        try:
            case['macros'] = [{'index': m['index'], 'name': m['name'], 'description': m['description'],
                               'ops': [[op['id'], [[k, v] for k, v in op.items() if k != 'id']]
                                       for op in m['ops']]}
                              for m in p.macros()]
        except Exception as e:
            case['macros_raised'] = exc(e)
    try:
        res = p.apply(ApplyConn(live, connected), opts)
        case['result'] = {'applied': res.applied, 'skipped': res.skipped, 'errors': res.errors,
                          'summary': res.summary()}
    except Exception as e:
        case['raised'] = exc(e)
    case['events'] = EVENTS
    applies.append(case)


def all_on():
    o = ApplyOptions(mes=[MixEffectOptions() for _ in range(4)])
    o.restore_fly_keyframes = True
    o.restore_multiview = o.restore_camera_control = o.restore_talkback = True
    return o


def live_state(n_me, mode=None):
    sw = Switcher()
    if n_me > 1:
        sw.feed('_top', bytes([n_me, 8, 2]) + bytes(LEN['_top'] - 3))
    if mode is not None:
        sw.feed('VidM', bytes([mode, 0, 0, 0]))
    return sw.mixerstate


LIVE1, LIVE4 = live_state(1, 27), live_state(4, 12)

record_apply('golden-default', from_save='golden', factory='default')
record_apply('golden-all-on', from_save='golden', opts=all_on(), live=LIVE1)
record_apply('golden-macros-only', from_save='golden', factory='macros_only')
def parseable(xml):
    try:
        Profile.from_xml(xml)
        return True
    except Exception:
        return False


for name in [s['name'] for s in saves if not s['name'].startswith('videomode-') and parseable(s['xml'])]:
    if name in ('granular-4me', 'sw-1me-full', 'sw-4me', 'sw-2me-mes-none', 'roundtrip-synthetic'):
        record_apply(f'{name}-default', from_save=name, factory='default', live=LIVE4 if 'me' in name else LIVE1)
    record_apply(f'{name}-all-on', from_save=name, opts=all_on(), live=LIVE4,
                 fail={s: 'FTDE code 5' for s in range(0, 100, 7)})
    record_apply(f'{name}-macros-only', from_save=name, factory='macros_only', macros=True)

# Video modes: each saved mode onto a switcher with none, at it, and at another.
for mode in sorted(VideoModeField._MODE_TABLE):
    other = 27 if mode != 27 else 12
    for live_name, live in (('none', {}), ('same', VM_STATES[mode]), ('other', VM_STATES[other])):
        record_apply(f'videomode-{mode}-{live_name}', from_save=f'videomode-{mode}', factory='default', live=live)


def doc(inner, attrs=' majorVersion="2" minorVersion="1"'):
    return f'<Profile{attrs}>{inner}</Profile>'


VM_APPLY = ApplyOptions.macros_only()
VM_APPLY.restore_macros = False
VM_APPLY.restore_video_mode = True
labels = sorted({VideoModeField(struct.pack('>B3x', m)).get_label().split(' ')[0] for m in VideoModeField._MODE_TABLE})
for label in labels + ['NTSC', 'PAL', '525i5994', 'NTSC_widescreen', 'PAL_widescreen', 'bogus', '', ' 1080p60',
                       '1080P60', '625i50', '525i59.94']:
    record_apply(f'videomode-label-{label!r}', xml=doc(f'<VideoMode videoMode="{label}"/>'), opts=VM_APPLY)
record_apply('videomode-missing-attr', xml=doc('<VideoMode/>'), opts=VM_APPLY)
record_apply('videomode-625i50-at-pal', xml=doc('<VideoMode videoMode="625i50"/>'), opts=VM_APPLY, live=VM_STATES[1])
record_apply('videomode-legacy-at-same', xml=doc('<VideoMode videoMode="1080p59.94"/>'), opts=VM_APPLY, live=VM_STATES[13])

# Hand-written documents.
POOL = ['nan', 'inf', '-inf', '1e30', '-1e30', '-5', '0', '', 'abc', ' 7 ', '1_0', '300', '70000', '-1',
        '0.5', '2.5', '12.345', 'True', 'false', 'yes', 'MediaPlayer2', 'Key1,Key3', 'Notch', 'Bogus',
        '-INF', '1e400', '\u0663', 'MidHigh', 'gate', 'DVE', 'PushLeft', '255', '65536', '4294967296',
        '-0.0', '1e-7', '-1e-7', '100.0000004', ' NaN ', '0x10', '+5', '5.']

ME_BLOCK = granular._full_me_profile().root.find('MixEffectBlocks')
ME_XML = ET.tostring(ME_BLOCK, encoding='unicode')

hand = {
    'empty-root': doc(''),
    'bare-root': '<Profile/>',
    'other-root': '<Other><VideoMode videoMode="1080p50"/></Other>',
    'full-me': ET.tostring(granular._full_me_profile().root, encoding='unicode'),
    'me-indices': doc('<MixEffectBlocks>' + ''.join(
        f'<MixEffectBlock index="{i}"><Program input="{n}"/><Preview input="{n + 1}"/>'
        f'<NextTransition selection="Key2, Background"/><FadeToBlack rate="{n}"/></MixEffectBlock>'
        for n, i in enumerate(['0', '1', '3', '-1', '', 'x', '0.9', '1e0', '2']))
        + '</MixEffectBlocks>'),
    'me-out-of-range': doc('<MixEffectBlocks><MixEffectBlock index="0"><Program input="70000"/>'
                           '<Preview input="-1"/><FadeToBlack rate="300"/></MixEffectBlock>'
                           '<MixEffectBlock index="1"><Program input="65535"/><Preview input="1e30"/>'
                           '<FadeToBlack rate="-2"/></MixEffectBlock></MixEffectBlocks>'),
    'me-index-inf': doc('<MixEffectBlocks><MixEffectBlock index="0"><Program input="5"/></MixEffectBlock>'
                        '<MixEffectBlock index="inf"><Program input="6"/></MixEffectBlock></MixEffectBlocks>'),
    'next-transition': doc('<MixEffectBlocks>' + ''.join(
        f'<MixEffectBlock index="0"><NextTransition selection="{s}"/></MixEffectBlock>'
        for s in ['Background,Key1,Key2,Key3,Key4', 'Background, Key2', 'Key3', '', ',,', 'key1', ' Key4 ']) +
        '<MixEffectBlock index="0"><NextTransition/></MixEffectBlock></MixEffectBlocks>'),
    'transition-values': doc('<MixEffectBlocks>' + ''.join(
        f'<MixEffectBlock index="0"><TransitionStyle style="{st}">'
        f'<MixParameters rate="{rate}"/><DipParameters rate="{rate}" input="{src}"/>'
        f'<WipeParameters rate="{rate}" pattern="{pat}" symmetry="{f1}" xPosition="{f2}" yPosition="{f3}" '
        f'reverseDirection="{b}" flipFlip="{b}" borderInput="{src}" borderWidth="{f2}" borderSoftness="{f1}"/>'
        f'<StingerParameters source="{ss}" preMultipliedKey="{b}" clip="{f1}" gain="{f3}" invert="{b}" '
        f'clipDuration="{rate}" triggerPoint="{src}" mixRate="{rate}" preroll="{rate}"/>'
        f'<DVEParameters rate="{rate}" reverseDirection="{b}" flipFlop="{b}" effect="{eff}" fillSource="{src}" '
        f'keySource="{src}" enableKey="{b}" preMultipliedKey="{b}" clip="{f2}" gain="{f1}" invertKey="{b}"/>'
        f'</TransitionStyle></MixEffectBlock>'
        for st, rate, src, pat, f1, f2, f3, b, ss, eff in [
            ('Wipe', '30', '2001', 'CircleIris', '33.3', '0.25', '0.75', 'True', 'MediaPlayer2', 'PushLeft'),
            ('Bogus', '300', '70000', 'Bogus', 'nan', 'inf', '-inf', 'yes', 'MediaPlayer', 'Squeeze'),
            ('DVE', '-5', '-1', 'TopRightDiagonal', '1e30', '-1e30', '1e-7', ' TRUE ', '3', 'SqueezeTopLeft'),
            ('Stinger', '1_0', ' 7 ', 'LeftToRightBar', '-0.0', '100.0000004', '12.345', 'on', ' MediaPlayer4', ''),
            ('Mix', 'abc', '', 'HorizontalBarnDoor', '', 'abc', '5.', '1', 'MediaPlayer\u0663', 'PushBottomRight'),
            ('Dip', '255', '65535', 'DiamondIris', '-inf', 'nan', 'inf', 'no', '300', 'PushTop'),
        ]) + '</MixEffectBlocks>'),
    'transition-rate-inf': doc('<MixEffectBlocks><MixEffectBlock index="0"><TransitionStyle style="Mix">'
                               '<MixParameters rate="20"/><DipParameters rate="12" input="inf"/>'
                               '<WipeParameters rate="5"/></TransitionStyle></MixEffectBlock></MixEffectBlocks>'),
    'keys': doc('<MixEffectBlocks><MixEffectBlock index="0"><Keys>' + ''.join(
        f'<Key index="{k}" type="{t}" inputCut="{src}" inputFill="{src}" onAir="{b}" masked="{b}" '
        f'maskTop="{f1}" maskBottom="{f2}" maskLeft="{f3}" maskRight="{f1}">'
        f'<LumaParameters preMultiplied="{b}" clip="{f1}" gain="{f2}" inverse="{b}"/>'
        f'<AdvancedChromaParameters foregroundLevel="{f1}" backgroundLevel="{f2}" keyEdge="{f3}" '
        f'spillSuppress="{f1}" flareSuppress="{f2}" brightness="{f3}" contrast="{f1}" saturation="{f2}" '
        f'red="{f3}" green="{f1}" blue="{f2}" cursorXPosition="{f3}" cursorYPosition="{f1}" cursorSize="{f2}" '
        f'sampledY="{f3}" sampledCb="{f1}" sampledCr="{f2}"/>'
        f'<PatternParameters style="{pat}" inverse="{b}" size="{f1}" symmetry="{f2}" softness="{f3}" '
        f'xPosition="{f1}" yPosition="{f2}"/>'
        f'<DVEParameters maskEnabled="{b}" maskTop="{f1}" maskBottom="{f2}" maskLeft="{f3}" maskRight="{f1}" '
        f'shadowEnabled="{b}" lightSourceDirection="{f2}" lightSourceAltitude="{f3}" borderEnabled="{b}" '
        f'borderBevelHue="{f1}" borderBevelSaturation="{f2}" borderBevelLuma="{f3}" borderWidthOut="{f1}" '
        f'borderWidthIn="{f2}" borderSoftnessOut="{f3}" borderSoftnessIn="{f1}" borderBevelOpacity="{f2}" '
        f'borderBevelPosition="{f3}" borderBevelSoftness="{f1}"/>'
        f'<FlyParameters enabled="{b}" xPosition="{f1}" yPosition="{f2}" xSize="{f3}" ySize="{f1}" '
        f'rotation="{f2}" rate="{rate}">'
        f'<KeyFrameA xSize="{f1}" ySize="{f2}" xPosition="{f3}" yPosition="{f1}" rotation="{f2}" '
        f'borderWidthOut="{f3}" borderOpacity="{f1}" borderHue="{f2}" borderLightSourceDirection="{f3}" '
        f'borderLightSourceAltitude="{f1}" maskTop="{f2}" maskRight="{f3}"/>'
        f'<KeyFrameB xPosition="{f2}" borderLuma="{f1}"/>'
        f'</FlyParameters></Key>'
        for k, t, src, b, f1, f2, f3, pat, rate in [
            ('0', 'Chroma', '1', 'True', '6.25', '8.0', '-4.5', 'CircleIris', '20'),
            ('1', 'DVE', '70000', 'False', 'nan', '0.5', '1e30', 'Bogus', '300'),
            ('2', 'Bogus', '-1', 'yes', 'inf', '-inf', '33.33', 'TopLeftBox', '-1'),
            ('3', 'Pattern', '3010', 'x', '1e30', '-1e30', '0.0625', 'LeftToRightBar', 'abc'),
            ('5', 'Luma', '1', 'True', '1', '1', '1', 'DiamondIris', '1'),
            ('-1', 'Luma', '1', 'True', '1', '1', '1', 'DiamondIris', '1'),
        ]) + '<Key index="0"/><Key/></Keys></MixEffectBlock></MixEffectBlocks>'),
    'key-index-inf': doc('<MixEffectBlocks><MixEffectBlock index="0"><Keys><Key index="1" type="Luma"/>'
                         '<Key index="inf"/></Keys></MixEffectBlock></MixEffectBlocks>'),
    'key-fill-inf': doc('<MixEffectBlocks><MixEffectBlock index="0"><Keys><Key index="1" type="DVE" '
                        'inputCut="2" inputFill="inf"/></Keys></MixEffectBlock></MixEffectBlocks>'),
    'chroma-partial': doc('<MixEffectBlocks><MixEffectBlock index="0"><Keys><Key index="0" type="Chroma">'
                          '<AdvancedChromaParameters cursorSize="6.25" cursorXPosition="8.0" sampledY="0.5"/></Key>'
                          '<Key index="1"><AdvancedChromaParameters cursorYPosition="1" sampledCb="0.1" '
                          'sampledCr="0.1" saturation="250"/></Key></Keys></MixEffectBlock></MixEffectBlocks>'),
    'fly-only-enable': doc('<MixEffectBlocks><MixEffectBlock index="0"><Keys><Key index="0" type="DVE">'
                           '<FlyParameters enabled="True"/></Key></Keys></MixEffectBlock></MixEffectBlocks>'),
    'dsk': doc('<DownstreamKeys>' + ''.join(
        f'<DownstreamKey index="{i}" fillSource="{src}" keySource="{src}" rate="{rate}" maskEnabled="{b}" '
        f'maskTop="{f1}" maskBottom="{f2}" maskLeft="{f1}" maskRight="{f2}" preMultipliedKey="{b}" '
        f'clip="{f1}" gain="{f2}" invert="{b}" onAir="{b}" tie="{b}"/>'
        for i, src, rate, b, f1, f2 in [
            ('0', '1', '25', 'True', '0.5', '-9'), ('1', '70000', '300', 'False', 'nan', 'inf'),
            ('3', '-1', '-1', 'yes', '1e30', '-inf'), ('300', '2', '2', 'True', '1', '2'),
            ('-1', '2', '2', 'True', '1', '2'), ('', '', '', '', '', '')]) + '<DownstreamKey/></DownstreamKeys>'),
    'dsk-index-inf': doc('<DownstreamKeys><DownstreamKey index="0" tie="True"/><DownstreamKey index="inf"/>'
                         '</DownstreamKeys>'),
    'colour-aux-media': doc(
        '<ColorGenerators>' + ''.join(f'<ColorGenerator index="{i}" hue="{h}" saturation="{s}" luma="{l}"/>'
                                      for i, h, s, l in [('0', '359.9', '50', '75'), ('1', 'nan', 'nan', 'inf'),
                                                         ('2', '1e30', '-5', '1e30'), ('300', '1', '1', '1'),
                                                         ('-1', '1', '1', '1')]) + '</ColorGenerators>'
        '<Auxiliaries>' + ''.join(f'<Auxiliary id="{i}" input="{s}"/>' for i, s in [
            ('8001', '1'), ('8006', '10010'), ('8000', '2'), ('9000', '3'), ('8257', '4'), ('', '5'),
            ('8002', '70000'), ('8003', '-1'), ('8004', 'abc')]) + '</Auxiliaries>'
        '<MediaPlayers>' + ''.join(f'<MediaPlayer index="{i}" sourceType="{t}" sourceIndex="{s}"/>' for i, t, s in [
            ('0', 'Still', '3'), ('1', 'Clip', '1'), ('2', 'clip', '300'), ('3', '', '-1'), ('300', 'Still', '1')])
        + '<MediaPlayer/></MediaPlayers>'),
    'aux-input-inf': doc('<Auxiliaries><Auxiliary id="8001" input="1"/><Auxiliary id="8002" input="inf"/>'
                         '</Auxiliaries>'),
    'settings': doc('<Settings ftbEnabled="False"><Inputs>' + ''.join(
        f'<Input id="{i}"{a}/>' for i, a in [
            ('1', ' shortName="CAM1" longName="Camera One"'), ('2', ' longName="Only long"'),
            ('3', ' shortName="SH"'), ('4', ''), ('0', ' longName="zero"'), ('-3', ' longName="neg"'),
            ('70000', ' longName="big"'), ('5', ' shortName="" longName=""'),
            ('6', ' longName="An overly long name for an input" shortName="TOOLONG"'),
            ('7', ' longName="Caméra ünë ✓✓✓✓✓" shortName="Cé✓"'), ('x', ' longName="bad id"')])
        + '</Inputs></Settings>'),
    'settings-ftb-default': doc('<Settings/>'),
    'settings-ftb-junk': doc('<Settings ftbEnabled="maybe"/>'),
    'inputs-id-inf': doc('<Settings><Inputs><Input id="1" longName="a"/><Input id="inf" longName="b"/>'
                         '</Inputs></Settings>'),
    'hyperdecks': doc('<HyperDecks>' + ''.join(
        f'<HyperDeck id="{i}"{a}/>' for i, a in [
            ('0', ' networkAddress="192.0.2.10" input="3"'), ('1', ' networkAddress="0.0.0.0" input="0"'),
            ('2', ' networkAddress="10.1" input="5"'), ('3', ' networkAddress="0x7f.1" input="5"'),
            ('4', ' networkAddress="1.2.3.4 junk" input="5"'), ('5', ' networkAddress="not an ip"'),
            ('6', ' networkAddress=""'), ('7', ' networkAddress="  10.0.0.9  " input="70000"'),
            ('8', ''), ('-1', ' networkAddress="10.0.0.1"'), ('70000', ' networkAddress="10.0.0.2"'),
            ('9', ' networkAddress="010.0.0.1" input="1"'), ('10', ' networkAddress="256.1.1.1"'),
            ('11', ' networkAddress="1.2.3.4.5"'), ('12', ' networkAddress="4294967295"'),
            ('13', ' networkAddress="1.16777215"'), ('14', ' networkAddress="08.1.1.1"'),
            ('15', ' networkAddress="1.2.3.0x"'), ('16', ' networkAddress="1..2"')]) + '</HyperDecks>'),
    'hyperdeck-input-inf': doc('<HyperDecks><HyperDeck id="0" networkAddress="10.0.0.1"/>'
                               '<HyperDeck id="1" input="inf"/></HyperDecks>'),
    'fairlight': doc(
        '<FairlightAudioMixer masterOutFaderGain="-inf" followFadeToBlack="True">'
        '<MasterOutEqualizer enabled="True" gain="-5.96">'
        '<EqualizerBand index="0" enabled="False" shape="HighPass" frequencyRange="Low" frequency="46" gain="0" qFactor="0.71"/>'
        '<EqualizerBand index="2" enabled="True" shape="BandPass" frequencyRange="MidLow" frequency="150" gain="20" qFactor="2.3"/>'
        '<EqualizerBand index="3" shape="Bogus" frequency="inf" gain="inf" qFactor="nan"/>'
        '<EqualizerBand index="4" frequencyRange="Huge" frequency="1e30" gain="nan" qFactor="-inf"/>'
        '<EqualizerBand index="300" frequency="-5" gain="-inf" qFactor="1e30"/>'
        '<EqualizerBand/>'
        '</MasterOutEqualizer>'
        '<MasterOutDynamicsProcessor makeupGain="inf">'
        '<Compressor enabled="True" threshold="-23.5" ratio="6" attack="12" hold="7" release="250"/>'
        '<Limiter enabled="True" threshold="nan" attack="2.2" hold="5" release="1e30"/>'
        '</MasterOutDynamicsProcessor>'
        '<AudioInputs>'
        '<AudioInput id="1"><AudioSource id="-65280" inputGain="3" pan="-50" faderGain="-inf" mixOption="On">'
        '<Equalizer enabled="True" gain="2"><EqualizerBand index="1" enabled="True" shape="LowShelf" '
        'frequencyRange="Low" frequency="49" gain="1" qFactor="1"/></Equalizer>'
        '<DynamicsProcessor makeupGain="3"><Expander enabled="True" gateMode="True" threshold="-40" range="20" '
        'ratio="2" attack="3" hold="0" release="90"/><Compressor enabled="True" threshold="-30" ratio="2.5" '
        'attack="2" hold="10" release="100"/><Limiter enabled="True" threshold="-6" attack="1" hold="5" '
        'release="80"/></DynamicsProcessor></AudioSource></AudioInput>'
        '<AudioInput id="1301"><AudioSource id="-256" inputGain="nan" pan="inf" faderGain="inf" mixOption="Bogus" '
        'delayFrames="3"><Equalizer gain="inf"><EqualizerBand index="0" shape="Notch" frequency="inf"/>'
        '</Equalizer><DynamicsProcessor makeupGain="-inf"><Expander gateMode="False" threshold="nan"/>'
        '</DynamicsProcessor></AudioSource>'
        '<AudioSource id="-256" inputGain="1" pan="nan" faderGain="-6" delayFrames="inf"/>'
        '<AudioSource id="-256" inputGain="inf" mixOption="AudioFollowVideo" delayFrames="300"/>'
        '<AudioSource id="-256" pan="1e30" faderGain="garbage" delayFrames="2"/></AudioInput>'
        '<AudioInput id="70000"><AudioSource faderGain=" -Infinity "/></AudioInput>'
        '<AudioInput id="0"><AudioSource/></AudioInput><AudioInput id="2"/>'
        '</AudioInputs></FairlightAudioMixer>'),
    'fairlight-no-master': doc('<FairlightAudioMixer><AudioInputs><AudioInput id="2"><AudioSource/>'
                               '</AudioInput></AudioInputs></FairlightAudioMixer>'),
    'fairlight-band-index-inf': doc('<FairlightAudioMixer><MasterOutEqualizer><EqualizerBand index="1"/>'
                                    '<EqualizerBand index="inf"/></MasterOutEqualizer></FairlightAudioMixer>'),
    'fairlight-input-inf': doc('<FairlightAudioMixer><AudioInputs><AudioInput id="1"><AudioSource/></AudioInput>'
                               '<AudioInput id="inf"/></AudioInputs></FairlightAudioMixer>'),
}
for name, x in hand.items():
    record_apply(f'hand-{name}-all-on', xml=x, opts=all_on(), live=LIVE4)
    record_apply(f'hand-{name}-default', xml=x, factory='default', live=LIVE1)

# Options: per-M/E gating, short and long lists, cross-model.
full = hand['full-me']


def gated(**kw):
    o = ApplyOptions(mes=[MixEffectOptions.none()], restore_downstream_keys=False,
                     restore_color_generators=False, restore_audio=False, restore_aux=False,
                     restore_video_mode=False, restore_inputs=False, restore_macros=False,
                     restore_media_players=False, restore_settings_flags=False,
                     restore_media_pool_images=False, restore_hyperdecks=False)
    for k, v in kw.items():
        setattr(o.mes[0], k, v)
    return o


for kw in [dict(program=True), dict(preview=True), dict(next_transition=True), dict(transition_style=True),
           dict(fade_to_black=True), dict(usk=[False, False, True, False]), dict(usk=[True, False, True, True]),
           dict(usk=[True] * 6), dict(usk=[True]), {}]:
    record_apply('gate-' + ','.join(f'{k}={v}' for k, v in kw.items()), xml=full, opts=gated(**kw))
o4 = ApplyOptions(mes=[MixEffectOptions() for _ in range(4)])
o4.mes[1].usk[0] = False
o4.mes[2] = MixEffectOptions.none()
for live_name, live in (('1me', {}), ('4me', LIVE4)):
    record_apply(f'granular-4me-gated-{live_name}', from_save='granular-4me', opts=o4, live=live)
o = ApplyOptions()
o.restore_multiview = o.restore_camera_control = o.restore_talkback = True
o.restore_audio = o.restore_video_mode = o.restore_inputs = False
record_apply('gaps-opted-in', from_save='sw-2me', opts=o, live=LIVE4)
o = ApplyOptions(mes=[])
record_apply('mes-empty', from_save='sw-2me', opts=o, live=LIVE4)

# Macros: clean, partial, all dropped, empty, upload failures, abort, no protocol.
MAC = {
    'clean': ('<Macro index="0" name="A" description=""><Op id="ProgramInput" mixEffectBlockIndex="0" input="Camera1"/>'
              '<Op id="Cut" mixEffectBlockIndex="0"/></Macro><Macro index="1" name="B" description="">'
              '<Op id="PreviewInput" mixEffectBlockIndex="0" input="Camera2"/></Macro>'),
    'partial': ('<Macro index="0" name="WIDE CAM" description="d"><Op id="ProgramInput" mixEffectBlockIndex="0" '
                'input="Camera1"/><Op id="_TestSyntheticUnencodableOp2" input="Camera1" sourceId="0" mixType="On"/>'
                '<Op id="Cut" mixEffectBlockIndex="0"/><Op id="_TestSyntheticUnencodableOp" threshold="-20"/>'
                '<Op id="_TestSyntheticUnencodableOp2"/><Op/><Op id="Cut"/></Macro>'),
    'all-dropped': ('<Macro index="0" name="all-bad" description="all dropped"><Op id="Nope" ratio="2"/>'
                    '<Op id="Nope2" threshold="-20"/></Macro>'),
    'empty': '<Macro index="7" name="meta-only" description="just a name"/>',
    'mixed': ('<Macro index="0" name="clean" description=""><Op id="Cut" mixEffectBlockIndex="0"/></Macro>'
              '<Macro index="1" name="dropped" description=""><Op id="Cut" mixEffectBlockIndex="0"/>'
              '<Op id="X" threshold="-20"/></Macro><Macro index="2" name="boom" description="">'
              '<Op id="Cut" mixEffectBlockIndex="0"/></Macro><Macro index="3" name="boom \'2\'" '
              'description=""><Op id="Y"/></Macro><Macro index="-1" name="neg"/><Macro name="noindex"/>'
              '<Macro index="x" name="badindex"/>'),
    'names': ('<Macro index="4" name="it\'s &quot;q&quot;" description="tab&#9;nl&#10;"><Op id="Cut" '
              'mixEffectBlockIndex="0"/></Macro><Macro index="5" name="Ünï ✓" description="é"/>'
              '<Macro index="2" name="&#x85;&#xa0;&#x2028;&#x200b;&#xe000;&#x378;&#xe0001;&#x1f600;\\x">'
              '<Op id="Nope"/><Op id="Cut" mixEffectBlockIndex="0"/></Macro>'
              '<Macro index="7" name="both \'&quot;\' &#x7f;&#9;" description="d"/>'),
    'abort-struct': ('<Macro index="0" name="ok"><Op id="Cut" mixEffectBlockIndex="0"/></Macro>'
                     '<Macro index="1" name="bad"><Op id="ProgramInput" mixEffectBlockIndex="300" input="Camera1"/>'
                     '<Op id="HyperDeckInput" slot="0" input="65536"/></Macro><Macro index="2" name="never"/>'),
    'abort-oserror': ('<Macro index="0" name="hd"><Op id="HyperDeckNetworkAddress" hyperDeckIndex="0" '
                      'networkAddress="bad ip"/></Macro>'),
    'abort-overflow': ('<Macro index="0" name="inf"><Op id="DVEAndFlyKeyXPosition" mixEffectBlockIndex="0" '
                       'keyIndex="0" xPosition="inf"/></Macro>'),
    'index-inf': '<Macro index="0" name="a"/><Macro index="inf" name="b"/>',
    'raw-unknown': ('<Macro index="9" name="raw"><Op id="Unknown_0x9999" rawParams="01020304"/>'
                    '<Op id="Unknown_0xzz" rawParams="00"/><Op id="Unknown_0x0002" rawParams="zz"/></Macro>'),
}
for name, inner in MAC.items():
    x = doc(f'<MacroPool>{inner}</MacroPool>')
    record_apply(f'macros-{name}', xml=x, factory='macros_only', macros=True)
    record_apply(f'macros-{name}-fail', xml=x, factory='macros_only', fail={0: 'ATEM rejected macro upload (status=not-found)',
                                                                            2: 'FTDE code 5', 7: 'timed out'})
record_apply('macros-no-protocol', xml=doc(f'<MacroPool>{MAC["clean"]}</MacroPool>'), factory='macros_only',
             connected=False)
record_apply('macros-empty-pool', xml=doc('<MacroPool/>'), factory='macros_only')
record_apply('macros-no-pool', xml=doc(''), factory='macros_only')

# Perturbed saves: random applied attributes set to awkward values.
APPLIED = ('MixEffectBlocks', 'DownstreamKeys', 'ColorGenerators', 'Auxiliaries', 'Settings', 'VideoMode',
           'HyperDecks', 'FairlightAudioMixer', 'MediaPlayers', 'MacroPool')
for n, base in enumerate(['sw-1me-full', 'sw-2me', 'sw-4me', 'sw-1me-dense', 'sw-1me-sparse', 'golden',
                          'sw-4me']):
    root = ET.fromstring(SAVED[base])
    for child in list(root):
        if child.tag not in APPLIED:
            root.remove(child)
    s = root.find('Settings')
    if s is not None:
        for child in list(s):
            if child.tag != 'Inputs':
                s.remove(child)
    targets = [(e, k) for e in root.iter() for k in e.attrib]
    for e, k in rng.sample(targets, min(len(targets), 25 + 5 * n)):
        v = rng.choice(POOL)
        if v == 'inf' and rng.random() < .7:
            v = 'nan'
        e.set(k, v)
    x = ET.tostring(root, encoding='unicode')
    record_apply(f'perturbed-{n}-{base}', xml=x, opts=all_on(), live=LIVE4,
                 fail={s: 'FTDE code 5' for s in range(1, 100, 3)})

# --- parses -------------------------------------------------------------------

PARSE = [
    '<a/>', '\ufeff<a/>', ' <a/>', '<a/> ', '<?xml version="1.0"?><a/>', ' <?xml version="1.0"?><a/>',
    '<?xml version="1.1"?><a/>', '<?xml version="1.0" encoding="utf-16"?><a/>',
    '<?xml version="1.0" encoding="latin-1"?><a>é</a>', '<?xml version="1.0" standalone="yes"?><a/>',
    '<?xml version="1.0" standalone="maybe"?><a/>', '<?xml encoding="utf-8"?><a/>', '<?XML version="1.0"?><a/>',
    '<?xml-stylesheet href="x"?><a/>', '<a x="1\t2\n3\r4\r\n5"/>', '<a x="1&#9;2&#10;3&#13;4"/>',
    '<a>1\r\n2\r3</a>', '<a><![CDATA[x\r\ny<&>]]>z</a>', '<a>x<!-- c -->y<?pi d?>z</a>',
    '<a x=\'q"q\' y="a\'b"/>', '<a x="&lt;&gt;&amp;&quot;&apos;"/>', '<a x="&#x41;&#65;"/>',
    '<a x="&#0;"/>', '<a x="&#1;"/>', '<a x="&#xD800;"/>', '<a x="&#xFFFE;"/>', '<a>&#x10FFFF;</a>',
    '<a>&#x110000;</a>', '<a x="&foo;"/>', '<a x="<"/>', '<a x=">"/>', '<a>]]></a>', '<a>></a>',
    '<a x="1" x="2"/>', '<a></b>', '<a>', '', '<a/><b/>', '<a/>junk', '<a/><!-- c -->', '<a/><?pi?>',
    '<a b/>', '<a b=c/>', '<a b = "c" />', '<a\tb="c"\n/>', '<1a/>', '<a-b.c_d/>', '<été/>',
    '<a:b xmlns:a="u"/>', '<a:b/>', '<b xmlns="u"><c/></b>', '<b xmlns="u"><c xmlns=""/></b>',
    '<b xmlns:p="u" p:x="1" x="2"/>', '<b xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>', '<b xmlns:p=""/>',
    '<b xml:lang="en"/>', '<b xmlns:xml="http://www.w3.org/XML/1998/namespace"/>', '<b xmlns:xml="u"/>',
    '<b xmlns:xmlns="u"/>', '<a:b:c xmlns:a="u"/>',
    '<b xmlns:p="http://www.w3.org/2001/XMLSchema-instance" p:t="1"><p:c/></b>',
    '<b xmlns:p="u1" xmlns:q="u2"><q:c p:a="1"/></b>', '<a>\x01</a>', '<a>\x7f</a>', '<a>\u0085\u2028</a>',
    '<a>\ufffe</a>', '<a x="\u00a0"/>', '<a>  <b/>  </a>', '<a>\u00a0<b/>\u3000</a>', '<a>text<b/>tail</a>',
    '<a><b>t</b></a>', '<a><b> </b></a>', '<a b="&amp;lt;"/>', '<!-- c --><a/>', '<!-- c -- d --><a/>',
    '<!---><a/>', '<!DOCTYPEx a><a/>', '<![CDATA[x]]><a/>', '<a><![CDATA[]]></a>', '<a>&amp</a>',
    '<a>& b</a>', '<a>&#;</a>', '<a>&#x;</a>', '<a>&#x41</a>', '<a>&#65abc;</a>', '<a x="1"y="2"/>',
    '<a >x</a >', '</a>', '<a/ >', '<?pi?><a/>', '<?xml version="1.0"?><?xml version="1.0"?><a/>',
    '<a><?xml version="1.0"?></a>', '<a><?xMl v?></a>', '<a>\t\n</a>', '<a x=""/>', '<a></a>',
    '<a>x</a>\n\n', '<a>\U0001F600</a>', '<a x="&#x1F600;"/>', '<_a/>', '<:a/>', '<xmlfoo/>',
    '<a x:y="1"/>', '<?xml version = "1.0"?><a/>', "<?xml version='1.0'?><a/>",
    '<?xml version="1.0"encoding="utf-8"?><a/>', '<?xml version="1.0" foo="bar"?><a/>', '<?xml?><a/>',
    '<!DOCTYPE x><a/>', '<!doctype x [<!entity e "z">]><a/>', '<!DOCTYPEé x><a/>', '<!ENT\u0130TY x><a/>',
    '<!ENT\u0131TY_ x><a/>', '<!DOCTYPE', '<a>\x00</a>', '<Profile\x00/>',
    '<!-- a note --><Profile majorVersion="2" minorVersion="1"/>',
    '<Profile majorVersion="2" minorVersion="1"><Settings><Input long="Caméra Café" short="CAMé"/></Settings></Profile>',
    '<a><b><c/></b><d>x</d>tail</a>', '<a>\n<b/>\n<c>\n</c></a>', '<a><b/>   </a>',
    '<Profile majorVersion="x" minorVersion="inf" product="P&amp;Q"><VideoMode/></Profile>',
    '<Profile majorVersion="1e3" minorVersion=" 7 "><VideoMode videoMode="PAL"/></Profile>',
]
PARSE_BYTES = [b'\xff<a/>', b'<a>\xc3</a>', b'<a>\xe2\x82</a>', b'<a x="\xed\xa0\x80"/>',
               '<a/>'.encode('utf-16-le'), '<a/>'.encode('utf-16'), b'\xef\xbb\xbf<a/>']
parses = []


def record_parse(data):
    case = {'hex': data.hex()} if isinstance(data, bytes) else {'xml': data}
    try:
        p = Profile.from_xml(data)
    except Exception as e:
        case['error'] = exc(e)
        parses.append(case)
        return
    case['out'] = p.to_xml()
    props = {}
    for prop in ('major_version', 'minor_version', 'product', 'video_mode'):
        try:
            props[prop] = getattr(p, prop)
        except Exception as e:
            props[prop] = {'raised': exc(e)}
    case['props'] = props
    parses.append(case)


for x in PARSE:
    record_parse(x)
for b in PARSE_BYTES:
    record_parse(b)
# Each save parsed back: the same XML again, or the parser's refusal.
for s in saves:
    try:
        s['reparse'] = Profile.from_xml(s['xml']).to_xml() == s['xml']
    except Exception as e:
        s['reparse'] = exc(e)

out = {'bmdwire': 'v1.3.0', 'generator': 'internal/fixtures/oracle/gen_profile.py',
       'golden': golden, 'saves': saves, 'applies': applies, 'commands': COMMANDS, 'parses': parses}
with open(OUT, 'w') as f:
    json.dump(out, f, separators=(',', ':'), sort_keys=True, ensure_ascii=False)
    f.write('\n')
n_events = sum(len(a['events']) for a in applies)
print(len(saves), 'saves,', len(applies), 'applies with', n_events, 'events,', len(parses), 'parses;',
      sum(1 for a in applies if 'raised' in a), 'applies raise')
