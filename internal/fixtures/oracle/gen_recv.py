# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate recv.json: what bmdwire's atemwire does with each state packet.

    python3 -I gen_recv.py /path/to/bmdwire recv.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.4.0. The C
extension (atemwire.mediaconvert) is stubbed; nothing here touches it.

For every opcode atemwire registers a Recv class for, plus a few it stores
raw, this builds payloads (random bytes at, under and over the expected
length, and well-formed variable-length ones) from a fixed seed, and feeds
each to a fresh AtemProtocol.save_field_data, the function atemwire's loop()
calls for every command in a packet. It records what happened: an exception,
nothing stored (consumed), raw bytes stored under the code, or a parsed
object, with where it landed in mixerstate and its attributes. The Go test
in atem/ checks the port does the same for every case.
"""
import json
import random
import struct
import sys
import types

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

import atemwire.messages as M  # noqa: E402
from atemwire.protocol import AtemProtocol  # noqa: E402

rng = random.Random(1337)


def rand(n):
    return bytes(rng.randrange(256) for _ in range(n))


def parses(cls, b):
    try:
        cls(b)
        return True
    except Exception:
        return False


def min_len(cls):
    """Smallest zero payload the class accepts (None if none up to 1200)."""
    for n in range(0, 1200):
        if parses(cls, bytes(n)):
            return n
    return None


def payloads(code, cls):
    out = []
    n = min_len(cls)
    exact = n is not None and not parses(cls, bytes(n + 1))
    if code == 'VidM':
        out += [bytes([m, 0, 0, 0]) for m in list(range(32)) + [255]]
        out += [b'', bytes([13])]
    elif code == '_VMC':
        valid = list(range(30))
        for count in range(4):
            b = struct.pack('>H2x', count)
            for _ in range(count):
                bits = [rng.getrandbits(30), rng.getrandbits(30)]
                b += struct.pack('>B3x I I ?', rng.choice(valid), bits[0], bits[1], rng.random() < .5)
            out.append(b)
        out.append(struct.pack('>H2x', 1) + struct.pack('>B3x I I ?', 40, 0, 0, 0))
        out.append(struct.pack('>H2x', 1) + struct.pack('>B3x I I ?', 1, 1 << 30, 0, 0))
        out.append(struct.pack('>H2x', 1) + struct.pack('>B3x I I ?', 1, 0, 1 << 31, 1))
        out.append(struct.pack('>H2x', 2) + struct.pack('>B3x I I ?', 1, 0, 0, 0))
        out.append(b'\x00')
    elif code == 'MPfe':
        for ln in (22, 23, 24, 25, 30, 45, 60):
            b = bytearray(rand(ln))
            if ln > 23:
                b[23] = rng.choice([0, 1, ln - 24, ln - 23, 200])
            out.append(bytes(b))
    elif code == 'MPrp':
        for nl, dl in ((0, 0), (3, 0), (5, 9), (12, 40)):
            body = rand(nl) + rand(dl)
            out.append(struct.pack('>H ?? H H', rng.randrange(100), 1, 0, nl, dl) + body + rand(rng.randrange(3)))
        out.append(struct.pack('>H ?? H H', 1, 1, 0, 10, 0) + b'short')
        out.append(b'\x00\x01')
    elif code in ('TlIn', 'TlSr', 'AMTl', 'FMTl'):
        step = {'TlIn': 1, 'TlSr': 3, 'AMTl': 3, 'FMTl': 11}[code]
        start = 15 if code == 'FMTl' else 2
        for count in (0, 1, 3, 6):
            out.append(struct.pack('>H', count) + rand(start - 2 + step * count))
        out.append(struct.pack('>H', 4) + rand(start - 2 + step * 2))
        out.append(b'\x00')
    elif code == 'AMLv':
        for count in (0, 1, 2, 5):
            off = ((36 + 2 * count + 3) // 4) * 4
            levels = b''.join(struct.pack('>I', rng.choice([0, 1, 8388608, rng.getrandbits(32)])) for _ in range(4 * count))
            head = struct.pack('>H2x', count) + b''.join(struct.pack('>I', rng.choice([0, 12345, 8388608, rng.getrandbits(32)])) for _ in range(8))
            srcs = b''.join(struct.pack('>H', rng.choice([1, 2, 1301, 2001])) for _ in range(count))
            out.append(head + srcs + bytes(off - 36 - 2 * count) + levels)
        out.append(struct.pack('>H2x', 3) + bytes(32) + bytes(4))
        out.append(bytes(35))
    elif code == 'CCdP':
        for dt in (0, 1, 2, 3, 4, 5, 128, 7):
            for cat, par in ((0, 0), (0, 1), (1, 2), (4, 4), (9, 9)):
                weird = [rng.choice([0, 0, 1, 2]) for _ in range(8)]
                head = bytes([rng.randrange(8), cat, par, dt] + weird) + rand(4)
                num = sum(weird)
                num = {(0, 0): 1, (0, 1): 0, (0, 2): 1, (0, 3): 1, (0, 4): 1, (0, 6): 1, (1, 2): 2}.get((cat, par), num)
                size = {0: 1, 1: 1, 2: 2, 3: 4, 4: 8, 128: 2}.get(dt, 0)
                out.append(head + rand(num * size))
                if rng.random() < .3:
                    out.append(head[:12])
                if num * size > 1 and rng.random() < .3:
                    out.append(head + rand(num * size - 1))
    elif code == 'FTDa':
        for ln in (3, 4, 10, 30):
            b = bytearray(rand(ln))
            if ln >= 4:
                struct.pack_into('>H', b, 2, rng.choice([0, ln - 4, ln, 500]))
            out.append(bytes(b))
    elif code == 'LKST':
        out += [struct.pack('>H??', s, st, 0) for s in (0, 1) for st in (False, True)]
        out += [b'\x00\x00\x02\x07', b'\x00\x00\x00']
    elif n is None:
        raise SystemExit(f'{code}: no zero payload parses')
    else:
        lens = [n] * 6 + [n + 1, n + 7] + ([n - 1] if n else [])
        if code in ('FEna', 'KeFS', 'FASD'):
            lens = list(range(0, 10)) + [12]
        for ln in lens:
            out.append(rand(ln))
        out.append(b'\xff' * n)
        out.append(bytes(n))
    return out


def normalize(v):
    if isinstance(v, bool) or v is None or isinstance(v, (int, float, str)):
        return v
    if isinstance(v, (bytes, bytearray)):
        return v.hex()
    if isinstance(v, (list, tuple)):
        return [normalize(x) for x in v]
    if isinstance(v, dict):
        return {str(k): normalize(x) for k, x in v.items()}
    return attrs(v)


RENAME = {'Y': 'sample_y', 'Cb': 'sample_cb', 'Cr': 'sample_cr'}


def attrs(obj):
    out = {}
    for k, v in vars(obj).items():
        if k == 'raw' or k.startswith('_'):
            continue
        out[RENAME.get(k, k)] = normalize(v)
    code = getattr(obj, 'CODE', '')
    if code in ('TlIn', 'TlSr'):
        lamps = lambda t: {'program': t[0], 'preview': t[1]}
        out['tally'] = ([lamps(t) for t in obj.tally] if code == 'TlIn'
                        else {str(k): lamps(t) for k, t in obj.tally.items()})
    if code == 'FASD':
        out['raw'] = obj.raw.hex()
    return out


def locate(mx, obj):
    """The mixerstate path to obj: [pretty key, index, ...]."""
    for k, v in mx.items():
        if v is obj:
            return [k]
        if isinstance(v, dict):
            def walk(d, path):
                for kk, vv in d.items():
                    if vv is obj:
                        return path + [kk]
                    if isinstance(vv, dict):
                        r = walk(vv, path + [kk])
                        if r:
                            return r
                return None
            r = walk(v, [k])
            if r:
                return r
    return None


def run(code, payload):
    p = AtemProtocol(ip='192.0.2.1')
    p.transport.sock.close()
    seen = []
    p.on('change', lambda key, contents: seen.append((key, contents)))
    try:
        p.save_field_data(code.encode(), payload)
    except Exception as e:
        return {'result': 'error', 'error': type(e).__name__}
    finally:
        p.transport.thread_queue.close()
    if not seen:
        return {'result': 'consumed'}
    key, contents = seen[-1]
    if isinstance(contents, (bytes, bytearray)):
        return {'result': 'raw', 'key': code}
    path = locate(p.mixerstate, contents)
    k = ':'.join([code] + [str(i) for i in path[1:]])
    return {'result': 'stored', 'name': path[0], 'key': k, 'value': attrs(contents)}


cases = []
for code, cls in sorted(M.RECV_BY_CODE.items()):
    for b in payloads(code, cls):
        c = {'code': code, 'payload': b.hex()}
        c.update(run(code, b))
        cases.append(c)
for code in ('InCm', 'RCPS', 'MPCS', '_MAC', 'Zzzz'):
    for b in (b'', rand(4), rand(17)):
        c = {'code': code, 'payload': b.hex()}
        c.update(run(code, b))
        cases.append(c)

with open(OUT, 'w') as f:
    json.dump({'bmdwire': 'v1.4.0', 'generator': 'internal/fixtures/oracle/gen_recv.py',
               'cases': cases}, f, indent=0, sort_keys=True)
    f.write('\n')
counts = {}
for c in cases:
    counts[c['result']] = counts.get(c['result'], 0) + 1
print(len(cases), 'cases over', len({c['code'] for c in cases}), 'codes:', counts)
