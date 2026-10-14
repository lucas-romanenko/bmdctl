# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate readers.json: what bmdwire's atemwire readers return.

    python3 -I gen_readers.py /path/to/bmdwire readers.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.4.0. The C
extension (atemwire.mediaconvert) is stubbed; nothing here touches it.

The readers are the functions in atemwire.messages.* whose first parameter
is the mixerstate (``mx``): 88 of them at v1.4.0, found by introspection so
none is missed. Each scenario starts from a fresh AtemProtocol and feeds it
well-formed state packets through save_field_data, the function atemwire's
loop() calls, then calls readers with every relevant index argument
(including out-of-range ones). Feeds and calls can interleave, so a later
packet overwriting an earlier one is covered too. Scenarios cover the empty
state, all-zero and all-0xFF payloads, random states of 1 to 4 M/Es with
partial packet sets (so the defaults for missing packets meet present
ones), Fairlight strips split and not, string decoding, HyperDeck slot
planning, every video mode and rounding-sensitive chroma sample values.
The state.py helpers the readers rely on (me_count, me_keyer_count,
_dsk_count, display_fps, decode_name, md5_hex) are recorded as well.

Each step records the packet fed (code and payload hex) or the call (name,
args, kwargs) and its result as JSON (tuples become lists, dict keys
strings), or the exception's name. The Go test in atem/ replays every
scenario through State.store and the matching State method.
"""
import importlib
import inspect
import json
import pkgutil
import random
import socket
import struct
import sys
import types

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

import atemwire.messages as M  # noqa: E402
from atemwire import state as S  # noqa: E402
from atemwire.protocol import AtemProtocol  # noqa: E402

READERS = {}
for info in pkgutil.iter_modules(M.__path__):
    mod = importlib.import_module('atemwire.messages.' + info.name)
    for fname, fn in vars(mod).items():
        if (inspect.isfunction(fn) and fn.__module__ == mod.__name__
                and list(inspect.signature(fn).parameters)[:1] == ['mx']):
            READERS[fname] = fn
if len(READERS) != 88:
    raise SystemExit(f'expected 88 readers, found {len(READERS)}')
HELPERS = {
    'me_count': S.me_count, 'me_keyer_count': S.me_keyer_count,
    '_dsk_count': S._dsk_count, 'display_fps': S.display_fps,
    'decode_name': S.decode_name, 'md5_hex': S.md5_hex,
}

rng = random.Random(0x5EED2026)


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
             'ColV InPr MPCE _mpl MRPr RXMS FAMP FASP AEBP AMBP AICP AILP '
             'AIXP MOCP AMLP FAIP FMHP FAMS _pin WhoI V3sl _top _MeC').split():
    LEN[code] = min_len(M.RECV_BY_CODE[code])
LEN['FEna'] = 4
LEN['KeFS'] = 8  # the parser tolerates less, but the flags sit up to byte 6
LEN['VidM'] = 4

# Bytes biased towards the edges: zero, maxima, -1 and the sign bit, so
# fields often hold 0, 0xFFFF (-1 when signed), 0x8000 or 0x7FFF.
EDGE = [0x00, 0x00, 0x00, 0xff, 0xff, 0x01, 0x80, 0x7f]


def rb(n):
    return bytes(rng.choice(EDGE) if rng.random() < .45 else rng.randrange(256)
                 for _ in range(n))


def body(code, fill):
    n = LEN[code]
    return bytearray(rb(n) if fill is None else bytes([fill]) * n)


def pick(fill, choices):
    """A meaningful value most of the time; the fill byte when there is one."""
    if fill is not None:
        return fill
    return rng.choice(choices) if rng.random() < .75 else rng.randrange(256)


def u16_at(b, off, v):
    struct.pack_into('>H', b, off, v)


def i16_at(b, off, v):
    struct.pack_into('>h', b, off, v)


NAMES = [
    'Camera 1', 'CAM1', '  Padded  ', 'Tab\there', '\x1cfs\x1f', ' ', '',
    ' nbsp ', '　ideo　', 'zw​', '᠎mvs', 'Ünïcödé',
    'MP1', 'Color 1', '\u0085nel', 'x' * 30,
]


def name_bytes(size, fill=None):
    """A NUL-padded name field: text, invalid UTF-8, garbage after the NUL."""
    if fill is not None:
        return bytes([fill]) * size
    r = rng.random()
    if r < .6:
        raw = rng.choice(NAMES).encode()
    elif r < .75:
        raw = rb(rng.randrange(size + 1))
    else:
        raw = bytes([rng.choice([0xff, 0xc3, 0xe2, 0x80, 0xf0])]) + rng.choice(NAMES).encode()
    raw = raw[:size]
    if len(raw) < size:
        raw += b'\x00' + (rb(size - len(raw) - 1) if rng.random() < .3 else bytes(size - len(raw) - 1))
    return raw


def build(code, idx=(), fill=None):
    """One well-formed payload for code; idx are the KEY_FORMAT indices."""
    if code == 'VidM':
        mode = rng.randrange(30) if fill is None else 29 if fill == 0xff else fill % 30
        return bytes([mode, 0, 0, 0])
    if code == 'FEna':
        if fill is not None:
            return bytes([fill] * 4)
        return rng.choice([b'\x00\x01\x00\x00', b'\x00\x00\x00\x00', b'\x01\x01\x00\x00', b'\x00\x02\x00\x00', rb(4)])
    if code == '_VMC':
        modes = idx[0] if idx else [rng.randrange(30) for _ in range(rng.randrange(5))]
        b = struct.pack('>H2x', len(modes))
        for m in modes:
            b += struct.pack('>B3x I I ?', m, rng.getrandbits(30), rng.getrandbits(30), rng.random() < .5)
        return b
    if code == 'MPfe':
        slot, = idx
        if fill is not None:
            # 24 bytes: a one-byte Pascal string whose length byte is capped.
            return struct.pack('>Bx H ? 16s 2x', fill, slot, fill != 0, bytes([fill]) * 16) + bytes([fill])
        name = name_bytes(rng.choice([1, 4, 12, 30]))
        hsh = bytes(16) if rng.random() < .3 else rb(16)
        n = len(name) if rng.random() < .8 else rng.choice([0, 255, len(name) - 1])
        head = struct.pack('>Bx H ? 16s 2x', rng.choice([0, 0, 1]), slot, rng.random() < .7, hsh)
        return head + bytes([n]) + name
    if code == 'MPrp':
        slot, = idx
        nm = name_bytes(rng.choice([0, 5, 12]), fill) if fill is None else b''
        desc = name_bytes(rng.choice([0, 9]), None) if fill is None else b''
        used = rng.random() < .7 if fill is None else fill != 0
        return struct.pack('>H??HH', slot, used, used and rng.random() < .2, len(nm), len(desc)) + nm + desc

    b = body(code, fill)
    if code in ('PrgI', 'PrvI', 'AuxS', 'TrPs', 'TMxP', 'TDpP', 'TWpP', 'TDvP',
                'TStP', 'FtbS', 'FtbP', 'DskB', 'DskP', 'DskS', 'ColV', 'MPCE'):
        b[0] = idx[0]
    elif code == 'TrSS':
        b[0] = idx[0]
        b[1] = pick(fill, [0, 1, 2, 3, 4, 5])
    elif code == '_MeC':
        b[0] = idx[0]
        b[1] = pick(fill, [0, 1, 2, 3, 4])
    elif code in ('KeOn', 'KeBP', 'KeLm', 'KACk', 'KACC', 'KePt', 'KeDV', 'KeFS'):
        b[0], b[1] = idx
    elif code == 'KKFP':
        b[0], b[1], b[2] = idx
    elif code in ('InPr', 'RXMS', 'FAIP'):
        u16_at(b, 0, idx[0])
        if code == 'InPr':
            b[2:22] = name_bytes(20, fill)
            b[22:26] = name_bytes(4, fill)
            b[32] = pick(fill, [0, 1, 2, 3, 4, 5, 6, 7, 128, 129, 130, 131])
        elif code == 'RXMS':
            ip, inp = idx[1], idx[2]
            if ip is not None:
                b[4:8] = socket.inet_aton(ip)
                u16_at(b, 8, inp)
        else:
            b[2] = pick(fill, [0, 1, 2, 3])
            b[5] = pick(fill, [0, 1, 2, 3])
            b[12] = pick(fill, [0, 1, 2, 4])
    elif code in ('FASP', 'AEBP', 'AICP', 'AILP', 'AIXP'):
        source, split, sub = idx[:3]
        u16_at(b, 0, source)
        b[14], b[15] = split, sub
        if code == 'FASP':
            b[49] = pick(fill, [1, 2, 4, 3, 0])
            if fill is None and rng.random() < .5:
                i16_at(b, 46, rng.choice([-10000, -10001, -9999, -32768, 1000, 0, -1]))
        elif code == 'AEBP':
            b[16] = idx[3]
            b[19] = pick(fill, [1, 2, 4, 8, 16, 32, 0, 3])
            b[21] = pick(fill, [1, 2, 4, 8, 0, 3])
        elif code == 'AIXP':
            b[17] = pick(fill, [0, 1, 2])
    elif code == 'AMBP':
        b[0] = idx[0]
        b[3] = pick(fill, [1, 2, 4, 8, 16, 32, 0, 3])
        b[5] = pick(fill, [1, 2, 4, 8, 0, 3])
    elif code == 'FAMP':
        if fill is None and rng.random() < .5:
            struct.pack_into('>i', b, 12, rng.choice([-10000, -10001, -9999, -2147483648, 1000, 0, -1]))
    elif code == 'FAMS':
        if fill is None:
            b[0] = rng.choice([0, 1, 1, 2])
            b[9] = rng.choice([1, 2, 255, rng.randrange(256)])
            b[22] = rng.choice([0x01, 0xff, 0xff, 0])
            b[23] = rng.choice([0, 1, 2])
    elif code == 'FMHP':
        if fill is None and rng.random() < .3:
            b[4] = 0  # the bool byte, sometimes plainly false
    elif code == '_pin':
        b[0:40] = name_bytes(40, fill)
    elif code == 'WhoI':
        for off, size in ((0, 32), (32, 16), (48, 64), (112, LEN['WhoI'] - 112)):
            b[off:off + size] = name_bytes(size, fill)
    elif code == 'V3sl':
        b[0] = pick(fill, [0, 1, 2])
    elif code == '_top':
        if fill is None:
            for i in range(12):
                b[i] = rng.choice([0, 1, 2, 3, 4, 4, 6, 10, 255])
    elif code == 'MRPr':
        if fill is None and rng.random() < .4:
            u16_at(b, 2, 0xFFFF)
    return bytes(b)


def norm(v):
    if v is None or isinstance(v, (bool, int, float, str)):
        return v
    if isinstance(v, (bytes, bytearray)):
        return v.hex()
    if isinstance(v, (list, tuple)):
        return [norm(x) for x in v]
    if isinstance(v, (set, frozenset)):
        return sorted(v)
    if isinstance(v, dict):
        return {str(k): norm(x) for k, x in v.items()}
    raise TypeError(f'cannot record {type(v).__name__}')


class Scenario:
    def __init__(self, name):
        self.name = name
        self.steps = []
        self.p = AtemProtocol(ip='192.0.2.1')
        self.p.transport.sock.close()
        self.p.transport.thread_queue.close()

    def feed(self, code, payload):
        try:
            self.p.save_field_data(code.encode(), payload)
        except Exception as e:
            raise SystemExit(f'{self.name}: {code} {payload.hex()} raised {e!r}')
        self.steps.append({'feed': code, 'payload': payload.hex()})

    def call(self, name, *args, **kwargs):
        fn = READERS.get(name) or HELPERS[name]
        mx = self.p.mixerstate
        step = {'call': name, 'args': norm(list(args))}
        if kwargs:
            step['kwargs'] = norm(kwargs)
        try:
            if name in ('decode_name', 'md5_hex'):
                step['result'] = norm(fn(*args, **kwargs))
            else:
                step['result'] = norm(fn(mx, *args, **kwargs))
        except Exception as e:
            step['error'] = type(e).__name__
        self.steps.append(step)

ME_READERS = [
    'program_source', 'preview_source', 'transition_style',
    'transition_in_transition', 'transition_position',
    'transition_frames_remaining', 'transition_selection', 'mix_rate',
    'dip_rate', 'dip_source', 'active_transition_rate', 'wipe_rate',
    'wipe_pattern', 'wipe_fill_source', 'wipe_position_x', 'wipe_position_y',
    'wipe_softness', 'wipe_symmetry', 'wipe_width', 'wipe_reverse',
    'wipe_flip_flop', 'dve_rate', 'dve_fill_source', 'dve_key_source',
    'dve_enable_key', 'dve_clip', 'dve_gain', 'dve_pre_multiplied',
    'dve_invert_key', 'dve_style', 'dve_reverse', 'dve_flip_flop',
    'stinger_rate', 'stinger_source', 'stinger_clip_duration',
    'stinger_trigger_point', 'stinger_pre_roll', 'stinger_clip',
    'stinger_gain', 'stinger_pre_multiplied', 'stinger_invert_key',
    'ftb_active', 'ftb_in_transition', 'ftb_frames_remaining', 'ftb_rate',
    'me_keyer_count',
]
USK_READERS = ['usk_on_air', 'usk_type', 'usk_fill_source', 'usk_key_source',
               'usk_luma', 'usk_chroma', 'usk_dve', 'usk_pattern', 'usk_mask',
               'usk_fly']
BARE_READERS = [
    'ftb_disabled', 'mediaplayer_slots', 'all_input_sources',
    'renamable_inputs', 'fairlight_master', 'fairlight_strips',
    'fairlight_audio_inputs', 'fairlight_compressor', 'fairlight_limiter',
    'fairlight_expander', 'fairlight_master_compressor',
    'fairlight_master_limiter', 'fairlight_headphones', 'fairlight_solo',
    'video_mode', 'available_video_modes', 'video_resolution',
    'product_name', 'device_name', 'sdi_3g_level', 'macro_play_running',
    'macro_play_index', 'me_count', '_dsk_count', 'display_fps',
]


def call_all(s, mes=(), pairs=(), dsks=(), auxes=(), gens=(), decks=(),
             files=(), players=(), sources=(), macro_slots=(), strips=(),
             ips=(), keyframes=('A', 'B')):
    """Every reader, with the indices the state has plus out-of-range ones."""
    def around(xs, extra=(-1,)):
        xs = sorted(set(xs))
        return sorted(set(xs) | set(extra) | {(xs[-1] + 1) if xs else 0, 0})
    for me in around(mes):
        for r in ME_READERS:
            s.call(r, me)
    mes_l = sorted(set(mes)) or [0]
    out = [(-1, 0), (0, -1), (mes_l[-1] + 1, 0)]
    for me in mes_l:
        ks = [k for m, k in pairs if m == me]
        out.append((me, (max(ks) + 1) if ks else 0))
    for me, k in sorted(set(pairs)) + out:
        for r in USK_READERS:
            s.call(r, me, k)
        for which in keyframes:
            s.call('usk_fly_keyframe', me, k, which)
    for d in around(dsks):
        s.call('dsk_state', d)
    for a in around(auxes):
        s.call('aux_source', a)
    for g in around(gens):
        s.call('color_generator', g)
    for slot in around(decks, (-1, 10, 65535, 65536)):
        s.call('hyperdeck_settings', slot)
    for slot in around(files, (-1, 65536)):
        s.call('mediaplayer_slot_info', slot)
    for mp in around(players):
        s.call('mediaplayer_selected', mp)
    for src in around(sources, (-1, 65535, 65536)):
        s.call('input_label', src)
    for slot in around(macro_slots, (-1, 65536)):
        s.call('macro_entry', slot)
    s.call('fairlight_eq_bands', None)
    for sid in sorted(set(strips)) + ['', '99.9', 'nonsense']:
        s.call('fairlight_eq_bands', sid)
    for ip in sorted(set(ips)) + ['10.9.9.9', '0.0.0.0']:
        for inp in (0, 3):
            s.call('plan_binding_push', ip, inp)
        s.call('plan_binding_push', ip, 3, exclude_slots={0, 1})
    s.call('plan_binding_push', '10.9.9.9', 1, exclude_slots=set(range(10)))
    for r in BARE_READERS:
        s.call(r)
    s.call('video_resolution', default=(640, 480))
    s.call('product_name', default='Unknown')
    s.call('device_name', default='Unknown')
    s.call('sdi_3g_level', default='Missing')


def random_state(name, n_me, keyers, n_dsk, density, fill=None, shuffle=True):
    """A switcher with n_me M/Es (keyers[me] keyers each), n_dsk DSKs and
    the rest, each packet present with probability density."""
    s = Scenario(name)
    pk = []
    ix = {k: [] for k in ('mes', 'pairs', 'dsks', 'auxes', 'gens', 'decks', 'files',
                          'players', 'sources', 'macro_slots', 'strips', 'ips')}

    def add(code, idx=(), **kw):
        if fill is not None or rng.random() < density:
            pk.append((code, build(code, idx, fill)))
            return True
        return False

    for me in range(n_me):
        ix['mes'].append(me)
        add('_MeC', (me,))
        for code in ('PrgI', 'PrvI', 'TrSS', 'TrPs', 'TMxP', 'TDpP', 'TWpP',
                     'TDvP', 'TStP', 'FtbS', 'FtbP'):
            add(code, (me,))
        for k in range(keyers[me]):
            ix['pairs'].append((me, k))
            for code in ('KeOn', 'KeBP', 'KeLm', 'KACk', 'KACC', 'KePt', 'KeDV', 'KeFS'):
                add(code, (me, k))
            for kf in (1, 2, 3):
                if kf < 3 or rng.random() < .2:
                    add('KKFP', (me, k, kf))
    for d in range(n_dsk):
        ix['dsks'].append(d)
        for code in ('DskB', 'DskP', 'DskS'):
            add(code, (d,))
    for a in range(rng.randrange(1, 8)):
        ix['auxes'].append(a)
        add('AuxS', (a,))
    for g in range(rng.randrange(1, 3)):
        ix['gens'].append(g)
        add('ColV', (g,))
    for slot in sorted(rng.sample(range(0, 40), 4)) + [65535]:
        ix['files'].append(slot)
        add('MPfe', (slot,))
    for mp in range(rng.randrange(1, 4)):
        ix['players'].append(mp)
        add('MPCE', (mp,))
    for slot in sorted(rng.sample(range(0, 100), 4)) + [65535]:
        ix['macro_slots'].append(slot)
        add('MPrp', (slot,))
    deck_ips = ['10.0.0.50', '10.0.0.60', '192.0.2.10']
    for slot in range(10):
        ix['decks'].append(slot)
        ip = rng.choice(deck_ips) if rng.random() < .4 else '0.0.0.0'
        if add('RXMS', (slot, ip, rng.choice([0, 3, 4, 65535]))):
            ix['ips'].append(ip)
    srcs = [0, 1, 2, 3, 4, 1000, 1301, 1302, 2001, 2002, 3010, 3011, 6000, 7001,
            8001, 10010, 10011, 65535]
    for src in rng.sample(srcs, rng.randrange(4, len(srcs))):
        ix['sources'].append(src)
        add('InPr', (src,))
    # Fairlight: strips split and not, their inputs, bands and dynamics.
    for src in rng.sample([0, 1, 2, 3, 1301, 1302, 2001, 2002, 65535], rng.randrange(0, 7)):
        add('FAIP', (src,))
        split = rng.random() < .35
        subs = [0, 1] if split else [rng.choice([0, 0, 1, 7])]
        for sub in subs:
            flag = 0xff if split else rng.choice([0x01, 0x01, 0x00, 0x02])
            sid = f'{src}.{sub}' if split else f'{src}.0'
            ix['strips'].append(sid)
            add('FASP', (src, flag, sub))
            for band in rng.sample(range(6), rng.randrange(0, 4)):
                add('AEBP', (src, flag, sub, band))
            for code in ('AICP', 'AILP', 'AIXP'):
                add(code, (src, flag, sub))
    for band in rng.sample(range(6), rng.randrange(0, 6)):
        add('AMBP', (band,))
    for code in ('FAMP', 'MOCP', 'AMLP', 'FMHP', 'FAMS', 'FEna', '_mpl', 'MRPr',
                 'VidM', '_VMC', '_pin', 'WhoI', 'V3sl', '_top'):
        add(code)
    if shuffle:
        rng.shuffle(pk)
    for code, b in pk:
        s.feed(code, b)
    call_all(s, **ix)
    return s


scenarios = []

# The handshake window: nothing has arrived.
s = Scenario('empty')
call_all(s, keyframes=('A', 'B', 'a', 'b', 'x', ''))
scenarios.append(s)

# Every packet at once, all zero and all 0xFF (maxima, -1 when signed).
for fill in (0x00, 0xff, 0x80):
    scenarios.append(random_state(f'fill-{fill:02x}', 2, [2, 1], 2, 1.0, fill=fill, shuffle=False))

# Random switchers: 1 to 4 M/Es, partial packet sets.
for i, (n_me, density) in enumerate([(1, 1.0), (1, .7), (1, .4), (2, 1.0), (2, .6),
                                      (3, .5), (4, .8), (4, .3), (1, .9), (2, .85),
                                      (3, 1.0), (1, .2)]):
    keyers = [rng.choice([1, 2, 4]) for _ in range(n_me)]
    scenarios.append(random_state(f'random-{i}', n_me, keyers, rng.randrange(1, 5), density))

# Chroma sample position, size and colour, swept: x values whose exact
# decimal (x+16000)/32000 is a tie at four places, and Y/Cb/Cr wire values
# for which int(Y*8544+625) or int(Cb*5000+5000) loses one.
s = Scenario('chroma-sweep')
xs = [-15992 + 16 * j for j in range(0, 2000, 37)] + [-32768, -16001, -16000, 0, 15999, 16000, 16001, 32767]
ys = [-32768, -9001, -9000, 0, 8999, 9000, 9001, 32767]
lossy_y = [w for w in range(65536) if int(((w - 625) / 8544) * 8544 + 625) != w][:12]
lossy_c = [w for w in range(65536) if int(((w - 5000) / 5000) * 5000 + 5000) != w][:12]
ywires = [0, 624, 625, 626, 9169, 65535] + lossy_y
cwires = [0, 4999, 5000, 5001, 10000, 65535] + lossy_c
for k, x in enumerate(xs):
    y = ys[k % len(ys)] if k < len(ys) else rng.randrange(-32768, 32768)
    size = rng.choice([0, 620, 1000, 5000, 9925, 65535, rng.randrange(65536)])
    yw = ywires[k % len(ywires)] if k < 2 * len(ywires) else rng.randrange(65536)
    cb = cwires[k % len(cwires)] if k < 2 * len(cwires) else rng.randrange(65536)
    cr = cwires[(k + 3) % len(cwires)] if k < 2 * len(cwires) else rng.randrange(65536)
    payload = struct.pack('>BB?? hhH HHH', 0, k, k % 2 == 0, k % 3 == 0, x, y, size, yw, cb, cr)
    s.feed('KACC', payload)
    s.call('usk_chroma', 0, k)
scenarios.append(s)

# Every video mode, one after another, then a capability list of them all.
s = Scenario('video-modes')
for mode in range(30):
    s.feed('VidM', bytes([mode, 0, 0, 0]))
    for r in ('video_mode', 'video_resolution', 'display_fps'):
        s.call(r)
    s.call('video_resolution', default=(640, 480))
s.feed('_VMC', build('_VMC', (list(range(30)),)))
s.call('available_video_modes')
s.feed('_VMC', build('_VMC', ([],)))
s.call('available_video_modes')
scenarios.append(s)

# Names: whitespace Python strips and Go's unicode.IsSpace doesn't (and the
# reverse), invalid UTF-8, empty names, NULs inside byte names.
s = Scenario('names')
for i, text in enumerate(NAMES + ['\x1c', '\t\n\x0b\x0c\r', 'a\x00b']):
    raw = text.encode()[:20]
    b = bytearray(build('InPr', (i,)))
    b[2:22] = raw + bytes(20 - len(raw))
    b[22:26] = (raw[:4] + bytes(4))[:4]
    s.feed('InPr', bytes(b))
    s.call('input_label', i)
    pin = bytearray(build('_pin'))
    pin[0:40] = (text.encode() + bytes(40))[:40]
    s.feed('_pin', bytes(pin))
    s.call('product_name')
    s.call('product_name', default='Unknown')
    who = bytearray(build('WhoI'))
    who[112:] = (text.encode() + bytes(64))[:len(who) - 112]
    s.feed('WhoI', bytes(who))
    s.call('device_name')
    s.call('device_name', default='Unknown')
    nm = text.encode() + b'\x00tail'
    s.feed('MPrp', struct.pack('>H??HH', i, True, False, len(nm), 3) + nm + b'dsc')
    s.call('macro_entry', i)
    s.feed('MPfe', struct.pack('>Bx H ? 16s 2x', 0, i, True, bytes(15) + b'\x01') + bytes([len(nm)]) + nm)
    s.call('mediaplayer_slot_info', i)
s.call('all_input_sources')
s.call('renamable_inputs')
scenarios.append(s)

# HyperDeck slot planning: reuse, drift, first free, exclusions, full pool,
# the same IP in two slots, and a pool that isn't 0-9.
s = Scenario('hyperdeck-plan')
s.call('plan_binding_push', '10.0.0.50', 4)
for slot, ip, inp in [(0, '10.0.0.50', 3), (1, '0.0.0.0', 0), (2, '10.0.0.60', 5),
                      (3, '10.0.0.50', 7), (4, '0.0.0.0', 9)]:
    s.feed('RXMS', build('RXMS', (slot, ip, inp)))
for ip in ('10.0.0.50', '10.0.0.60', '10.0.0.70', '0.0.0.0'):
    for inp in (0, 3, 5, 7):
        s.call('plan_binding_push', ip, inp)
        s.call('plan_binding_push', ip, inp, exclude_slots=[1])
        s.call('plan_binding_push', ip, inp, exclude_slots=(1, 4))
for slot in range(5, 12):
    s.feed('RXMS', build('RXMS', (slot, f'10.0.1.{slot}', slot)))
s.call('plan_binding_push', '10.9.9.9', 1)
s.call('plan_binding_push', '10.9.9.9', 1, exclude_slots={1})
s.call('plan_binding_push', '10.9.9.9', 1, exclude_slots={1, 4})
for slot in (1, 4):
    s.feed('RXMS', build('RXMS', (slot, f'10.0.2.{slot}', 0)))
s.call('plan_binding_push', '10.9.9.9', 1)
s.call('plan_binding_push', '10.0.2.4', 0)
for slot in range(12):
    s.call('hyperdeck_settings', slot)
scenarios.append(s)

# Fairlight strips in detail: ordering by input type, number and
# subchannel (ties included), names from InPr, MP and Mic/TRS labels.
s = Scenario('fairlight-strips')
strip_specs = [  # source, split, subchannels, FAIP (type, number, level) or None
    (5, False, [0], (0, 1, 0)), (1, False, [0], (0, 1, 0)), (2, False, [0], (0, 0, 0)), (3, True, [0, 1], (0, 2, 0)),
    (4, False, [0], None), (1301, False, [0], (2, 0, 1)),
    (1302, False, [0], (2, 1, 4)), (1303, True, [1, 0], (2, 1, 2)), (2001, False, [0], (1, 0, 0)),
    (2002, True, [0, 1], (1, 1, 0)), (6, False, [0], (3, 0, 0)), (7, False, [0], (7, 1, 0)),
    (65535, False, [0], (0, 255, 0)), (0, False, [0], (1, 255, 0)),
]
for src, split, subs, faip in strip_specs:
    if faip is not None:
        b = bytearray(build('FAIP', (src,)))
        b[2], b[5], b[12] = faip
        s.feed('FAIP', bytes(b))
    for sub in subs:
        s.feed('FASP', build('FASP', (src, 0xff if split else 0x01, sub)))
for src, text in ((1, 'Camera 1'), (2, ''), (3, 'Cam 3'), (4, 'Four'), (6, ' '), (7, 'Seven')):
    b = bytearray(build('InPr', (src,)))
    b[2:22] = (text.encode() + bytes(20))[:20]
    b[22:26] = (text[:2].encode() + bytes(4))[:4]
    s.feed('InPr', bytes(b))
s.call('fairlight_strips')
s.call('fairlight_audio_inputs')
s.feed('FASP', build('FASP', (1, 0x01, 0)))
s.feed('FAIP', build('FAIP', (2,)))
s.call('fairlight_strips')
for src in (1, 3, 1303):
    for band in (5, 0, 3):
        s.feed('AEBP', build('AEBP', (src, 0xff if src in (3, 1303) else 0x01, 1 if src != 1 else 0, band)))
for sid in ('1.0', '3.0', '3.1', '1303.1', '1303.0', '4.0'):
    s.call('fairlight_eq_bands', sid)
for code in ('AICP', 'AILP', 'AIXP'):
    for src, split, sub in ((1, 0x01, 0), (3, 0xff, 0), (3, 0xff, 1), (1302, 0x02, 5)):
        s.feed(code, build(code, (src, split, sub)))
for r in ('fairlight_compressor', 'fairlight_limiter', 'fairlight_expander'):
    s.call(r)
for solo in (b'\x01' + bytes(8) + b'\x05' + bytes(12) + b'\xff\x01',
             b'\x01' + bytes(8) + b'\x05' + bytes(12) + b'\x01\x01',
             b'\x00' + bytes(8) + b'\x05' + bytes(12) + b'\xff\x01',
             b'\x02' + bytes(8) + b'\xff' + bytes(12) + b'\x00\x02'):
    s.feed('FAMS', solo)
    s.call('fairlight_solo')
for vol in (-10001, -10000, -9999, 0, 1000, -32768, 32767, 12345):
    b = bytearray(build('FASP', (9, 0x01, 0)))
    i16_at(b, 46, vol)
    s.feed('FASP', bytes(b))
    s.call('fairlight_strips')
    fm = bytearray(build('FAMP'))
    struct.pack_into('>i', fm, 12, vol)
    s.feed('FAMP', bytes(fm))
    s.call('fairlight_master')
    hp = bytearray(build('FMHP'))
    struct.pack_into('>i', hp, 0, vol)
    s.feed('FMHP', bytes(hp))
    s.call('fairlight_headphones')
for band in (4, 1, 2):
    s.feed('AMBP', build('AMBP', (band,)))
    s.call('fairlight_eq_bands', None)
scenarios.append(s)

# Overwrites and dispatch: the transition style moving through every value
# active_transition_rate knows (and two it doesn't), FEna variants, macro
# play status, counts from _top, _MeC and DSK state entries.
s = Scenario('sequences')
for code in ('TMxP', 'TDpP', 'TWpP', 'TDvP', 'TStP'):
    s.feed(code, build(code, (0,)))
for style in (0, 1, 2, 3, 4, 5, 255):
    b = bytearray(build('TrSS', (0,)))
    b[1] = style
    s.feed('TrSS', bytes(b))
    s.call('active_transition_rate', 0)
    s.call('transition_style', 0)
for fena in (b'\x00\x01\x00\x00', b'\x00\x00\x00\x00', b'\x01\x01', b'', b'\x00', b'\x00\x01'):
    s.feed('FEna', fena)
    s.call('ftb_disabled')
for mrpr in (b'\x01\x00\x00\x05', b'\x00\x00\xff\xff', b'\x03\x01\x00\x00', b'\x02\x00\x12\x34'):
    s.feed('MRPr', mrpr)
    s.call('macro_play_running')
    s.call('macro_play_index')
for me, keyers in ((0, 0), (2, 2), (1, 255), (0, 4)):
    s.feed('_MeC', bytes([me, keyers]) + bytes(LEN['_MeC'] - 2))
    s.call('me_count')
    for m in (-1, 0, 1, 2, 3):
        s.call('me_keyer_count', m)
for d in (0, 3):
    s.feed('DskS', build('DskS', (d,)))
    s.call('_dsk_count')
for top in (bytes(16), bytes([3, 8, 2]) + bytes(13), bytes([0, 8, 0]) + bytes(13), bytes([255, 0, 255]) + bytes(13)):
    s.feed('_top', top[:LEN['_top']])
    s.call('me_count')
    s.call('_dsk_count')
for level in (0, 1, 2, 255):
    s.feed('V3sl', bytes([level]) + bytes(LEN['V3sl'] - 1))
    s.call('sdi_3g_level')
    s.call('sdi_3g_level', default='Missing')
for stills in (0, 20, 255):
    s.feed('_mpl', bytes([stills, 3]) + bytes(LEN['_mpl'] - 2))
    s.call('mediaplayer_slots')
scenarios.append(s)

# The state.py helpers on raw values.
s = Scenario('helpers')
names = [b'', b'\x00', b'abc', b'  abc  ', b'abc\x00def', b'\x1c abc \x1f', b'\xc2\x85abc\xc2\xa0',
         '　x　'.encode(), '᠎x'.encode(), '​x​'.encode(), b'\xff\xfeab',
         b'\xe2\x80', b'\xf0\x9f\x98\x80 ', b'\t\n\x0b\x0c\r x', b'\xed\xa0\x80', b'\xf4\x90\x80\x80',
         b'\xe0\x80\xaf', b'\xc0\xaf', b' \x00 ', ' a '.encode(), '\u0085'.encode()]
names += [rb(rng.randrange(1, 24)) for _ in range(30)]
for raw in names:
    s.call('decode_name', raw)
for h in [bytes(16), bytes(15) + b'\x01', b'\x80' + bytes(15)] + [rb(16) for _ in range(5)]:
    s.call('md5_hex', h)
scenarios.append(s)


called = {st['call'] for sc in scenarios for st in sc.steps if 'call' in st}
missing = (set(READERS) | set(HELPERS)) - called
if missing:
    raise SystemExit(f'no cases for {sorted(missing)}')

with open(OUT, 'w') as f:
    f.write('{"bmdwire":"v1.4.0","generator":"internal/fixtures/oracle/gen_readers.py",\n')
    f.write('"readers":' + json.dumps(sorted(READERS)) + ',\n')
    f.write('"helpers":' + json.dumps(sorted(HELPERS)) + ',\n')
    f.write('"scenarios":[\n')
    for i, sc in enumerate(scenarios):
        f.write('{"name":' + json.dumps(sc.name) + ',"steps":[\n')
        f.write(',\n'.join(json.dumps(st, separators=(',', ':'), sort_keys=True)
                           for st in sc.steps))
        f.write('\n]}' + (',' if i < len(scenarios) - 1 else '') + '\n')
    f.write(']}\n')

n_calls = sum(1 for sc in scenarios for st in sc.steps if 'call' in st)
n_feeds = sum(1 for sc in scenarios for st in sc.steps if 'feed' in st)
print(len(scenarios), 'scenarios,', n_feeds, 'packets fed,', n_calls, 'calls over',
      len(called), 'functions')
