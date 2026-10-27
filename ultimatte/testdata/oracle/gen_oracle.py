# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: MIT
"""Generate oracle.json: what bmdwire's ultimattewire does.

    python3 -I gen_oracle.py /path/to/bmdwire oracle.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.7.0.

Three parts. The parsers (parse_section, parse_file_list, extract_label,
extract_model, parse_current_file, annotate_preamble, to_display,
parse_interface, build_block) on a pool
of preludes and echoes, with RANGES itself. The channels' calls
(binary_request, upload_one, read_preamble, exchange, read_network,
set_network, archive_unit_to_bytes, restore_unit_from_bytes), each over
scripted connections: socket.create_connection hands out the next script
(the last again once they run out), a script's chunks are what recv
returns (a timeout marker raises socket.timeout, a script run dry is the
unit closing), and set_network's clock moves only when it sleeps. Each
call records its result or exception (class and message) and, per
connection, the address dialled and the bytes sent. The archive's zip is
recorded member by member (name, method, attributes, flags, versions,
CRC, size and contents), all but its times and deflated bytes. The Go
test in ultimatte/ replays every call through a Client.
"""
import io
import json
import random
import socket
import struct
import sys
import types
import zipfile

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)

import ultimattewire._preamble as P  # noqa: E402
import ultimattewire._protocol as PR  # noqa: E402
import ultimattewire.network as N  # noqa: E402
from ultimattewire.profile import archive as A  # noqa: E402
from ultimattewire.profile import restore as R  # noqa: E402
from ultimattewire.profile._ranges import RANGES, annotate_preamble, to_display  # noqa: E402

rng = random.Random(0x9998)
TIMEOUT = 'TIMEOUT'


class Sock:
    def __init__(self, chunks):
        self.chunks = [c for c in chunks if c != b'']
        self.sent = b''
        self.closed = False

    def settimeout(self, t):
        pass

    def sendall(self, data):
        self.sent += data

    def recv(self, n):
        if not self.chunks:
            return b''
        head = self.chunks[0]
        if head == TIMEOUT:
            self.chunks.pop(0)
            raise socket.timeout('timed out')  # as a real socket's says
        take, rest = head[:n], head[n:]
        if rest:
            self.chunks[0] = rest
        else:
            self.chunks.pop(0)
        return take

    def close(self):
        self.closed = True


class Net:
    """Hands out scripted connections: each a list of chunks, or an
    exception's message to refuse with; the last again once they run out."""

    def __init__(self, scripts):
        self.scripts = list(scripts)
        self.i = 0
        self.socks = []

    def create_connection(self, addr, timeout=None):
        s = self.scripts[min(self.i, len(self.scripts) - 1)]
        self.i += 1
        if isinstance(s, str):
            self.socks.append({'addr': f'{addr[0]}:{addr[1]}', 'refused': True})
            raise ConnectionRefusedError(s)
        sock = Sock(s)
        self.socks.append({'addr': f'{addr[0]}:{addr[1]}', 'sock': sock})
        return sock


def enc(script):
    if isinstance(script, str):
        return {'refuse': script}
    return {'chunks': [{'timeout': True} if c == TIMEOUT else {'data': c.hex()} for c in script]}


def exc(e):
    return {'class': type(e).__name__ if type(e).__name__ != 'OSError' else 'IOError', 'message': str(e)}


def iface_dict(i):
    return {k: getattr(i, k) for k in ('index', 'name', 'mac', 'priority', 'dynamic', 'address', 'netmask', 'gateway',
                                       'dns', 'static_address', 'static_netmask', 'static_gateway', 'static_dns', 'raw')}


def zip_record(z):
    zf = zipfile.ZipFile(io.BytesIO(z))
    out = []
    for i in zf.infolist():
        out.append({'name': i.filename, 'method': i.compress_type, 'external': i.external_attr, 'flags': i.flag_bits,
                    'system': i.create_system, 'create_version': i.create_version,
                    'extract_version': i.extract_version, 'crc': i.CRC, 'size': i.file_size,
                    'extra': i.extra.hex(), 'data': zf.read(i).hex()})
    return out


def run(call, scripts, **kw):
    net = Net(scripts)
    PR.socket = types.SimpleNamespace(create_connection=net.create_connection, timeout=socket.timeout)
    PR._CONNECT_RETRY_DELAY = 0
    clock = [1_000_000.0]
    N.time = types.SimpleNamespace(time=lambda: clock[0], sleep=lambda s: clock.__setitem__(0, clock[0] + s))
    rec = {'call': call, 'args': {k: (v.hex() if isinstance(v, bytes) else v) for k, v in kw.items()},
           'scripts': [enc(s) for s in scripts]}
    host = '192.0.2.21'
    try:
        if call == 'binary_request':
            rec['result'] = PR.binary_request(host, kw['opcode'], kw['payload']).hex()
        elif call == 'upload_one':
            PR.upload_one(host, kw['opcode'], kw['name'], kw['payload'])
            rec['result'] = None
        elif call == 'read_preamble':
            rec['result'] = P.read_preamble(host)
        elif call == 'exchange':
            rec['result'] = N.exchange(host, kw['block'])
        elif call == 'read_network':
            rec['result'] = iface_dict(N.read_network(host, kw['index']))
        elif call == 'set_network':
            args = {k: v for k, v in kw.items() if k != 'index'}
            rec['result'] = iface_dict(N.set_network(host, kw['index'], **args))
        elif call == 'archive':
            z, info = A.archive_unit_to_bytes(host)
            rec['result'] = {'zip': zip_record(z), 'info': info}
        elif call == 'restore':
            ok, info = R.restore_unit_from_bytes(host, bytes.fromhex(rec['args']['zip']))
            rec['result'] = {'ok': ok, 'info': {'presets': [list(p) for p in info['presets']],
                                                'gpi': list(info['gpi']) if info['gpi'] else None,
                                                'saved': list(info['saved']) if info['saved'] else None,
                                                'stopped_at': info['stopped_at']}}
    except Exception as e:  # noqa: BLE001
        rec['error'] = exc(e)
    rec['connections'] = [{'addr': c['addr'], 'refused': True} if c.get('refused') else
                          {'addr': c['addr'], 'sent': c['sock'].sent.hex(), 'closed': c['sock'].closed}
                          for c in net.socks]
    return rec


# --------------------------------------------------------------------------
# Texts

with open(BMDWIRE + '/ultimattewire/tests/conftest.py') as f:
    src = f.read()
ns = {}
exec(src[src.index('SAMPLE_PRELUDE = ('):src.index('class FakeSocket')], ns)
SAMPLE = ns['SAMPLE_PRELUDE']
with open(BMDWIRE + '/ultimattewire/tests/test_network.py') as f:
    src = f.read()
ns = {}
exec(src[src.index('PRELUDE = '):src.index('@pytest.fixture')], ns)
NPRELUDE, ECHO = ns['PRELUDE'], ns['ECHO']

ctl = list(RANGES)


def control_lines(n):
    out = []
    for _ in range(n):
        name = rng.choice(ctl + ['Not A Param', 'Offset Black Balance', 'Offset Matte Density', 'Offset ', 'Offset'])
        v = rng.choice(['0', '5000', '10000', '2500', '1', '-50', '30', '30.9', '1e3', '1_000', '\u0661\u0662', '0x10',
                        'abc', '', ' 7 ', '99999999', '12.5', str(rng.randrange(-20000, 20000))])
        out.append(f'{name}: {v}')
    return out


PRELUDES = [SAMPLE, NPRELUDE.decode(), '', 'END PRELUDE:\n', 'Label: Keyer A\n', 'Label:\nNext Line\n',
            'Label:   \nX\n', 'Label:   ', 'Label:\n\n', 'Label: Café Ü 1\r\n', 'Label: a/b:c d\n', 'no label\n',
            'xLabel: no\nLabel: yes\n', 'Label: \u3000wide\u3000\n', 'Label:\u2028line\n',
            SAMPLE.replace('\n', '\r\n'), SAMPLE.replace('FILE LIST:', 'FILE LIST:  \t'),
            'FILE LIST:\npreset-a\npreset-b\nEND PRELUDE:\n', 'FILE LIST:\n\npreset a \n\n',
            'FILE LIST:\none\x1ctwo\u2028three\rfour\n', 'FILE LIST:\nA\nB C:\nC\n', 'FILE LIST:\nA\nBc:\nC\n',
            'FILE LIST:\nA\nB:  x\nC\n', 'FILE LIST:\nA\nB1 2:\t\n\nC\n', 'FILE LIST:\nA\nEND PRELUDE:\nB\n',
            'FILE LIST:\nA\nFILE LIST:\nB\n', 'FILE LISTS:\nA\nFILE LIST:\nB\n', 'X FILE LIST:\nA\n',
            'FILE LIST:\n\u0661\n\u00e9\u0301\n', 'CONTROL:\nMatte Density: inf\n', 'CONTROL:\nMatte Density: nan\n',
            'CONTROL:\nMatte Density: 1e300\n', 'CONTROL DEFAULT:\nBlack Balance: 0\n',
            'CONTROL:\n Indented: 1\nMatte Density 5000\nOffset Cursor X: 5000\nOffset : 1\n',
            'CONTROL:\nTransition Rate: 30\nOTHER:\nMatte Density: 5000\n']
for _ in range(25):
    body = '\n'.join(control_lines(rng.randrange(1, 12)))
    PRELUDES.append(f'Label: Keyer {rng.randrange(10)}\nSoftware Release: 2.1\n\nCONTROL:\n{body}\n'
                    f'FILE LIST:\n' + '\n'.join(f'slot-{i}' for i in range(rng.randrange(0, 4))) + '\n\nEND PRELUDE:\n')

# IDENTITY and CURRENT FILE (bmdwire#52), a pool of their own so
# PRELUDES, whose last two the calls below use, stays as it was: a 12 4K's
# sections as it sent them (software 2.3, 2026-10-27, its Unique ID
# zeroed), then the edges of _field.
UNIT_12_4K = ('PROTOCOL PREAMBLE:\nVersion: 2.3\n\nIDENTITY:\nModel: Ultimatte 12 4K\nLabel: Studio Keyer\n'
              'Unique ID: 00000000000000000000000000000000\n\nVERSION:\nProduct ID: BE8F\nSoftware Release: 2.3\n\n'
              'CURRENT FILE:\nName: 3\nStatus: Modified\n\nFILE LIST:\n1\n2\n\nEND PRELUDE:\n')
IDENTITIES = [
    UNIT_12_4K,
    UNIT_12_4K.replace('Name: 3\nStatus: Modified', 'Name: Ultimatte Defaults\nStatus: Clean'),
    UNIT_12_4K.replace('\n', '\r\n'),
    'IDENTITY:\nModel:   Ultimatte 12  \t\n\n', 'IDENTITY:\nModel:\nLabel: A\n', 'IDENTITY:\nmodel: X\nModel X\n',
    'IDENTITY:\nModel: a: b\n', 'IDENTITY:\n\nDEVICE:\nModel: Elsewhere\n', 'DEVICE:\nModel: Elsewhere\n',
    'IDENTITY:\nModel: First\nModel: Second\n', 'IDENTITY:\n  Model  :  Spaced  \n', 'Identity:\nModel: Lower\n',
    'IDENTITY:\nModel: one\u2028Model: two\n', 'IDENTITY:\nModel:\u00a0Nbsp\u3000\n',
    'CURRENT FILE:\nName: Studio B\n', 'CURRENT FILE:\nStatus: Clean\n', 'CURRENT FILE:\nName:x\nStatus:y\n',
    'CURRENT FILE:\nName: a\x1cStatus: b\n', 'CURRENT FILE:\n\nFILE LIST:\nName: no\n',
    'CURRENT FILE:\nName: 3\nStatus: Modified\nEND PRELUDE:\nName: after\n',
]

ECHOES = [ECHO, ECHO.replace('Dynamic IP: false', 'Dynamic IP: TRUE'), ECHO.replace('Dynamic IP: false\n', ''),
          ECHO.replace('192.0.2.98/255.255.252.0', '192.0.2.98'),
          ECHO.replace('Current Addresses: 192.0.2.98/255.255.252.0', 'Current Addresses: a/b, c/d'),
          ECHO.replace('Static DNS Servers:', 'Static DNS Servers: 8.8.8.8,8.8.4.4 1.1.1.1'),
          ECHO.replace('Priority: 0', 'Priority: 12'), ECHO.replace('Priority: 0', 'Priority: x'),
          ECHO.replace('Priority: 0', 'Priority: \u0661'), ECHO.replace('Priority: 0', 'Priority: \u00b2'),
          ECHO.replace('Name: ', 'Name:\t\t'), ECHO.replace('\n', '\r\n'), 'Name: a\nName: b\n', '', 'nothing\n',
          ECHO.replace('Current Addresses: 192.0.2.98/255.255.252.0', 'Current Addresses:  / '),
          'Current Addresses: 10.0.0.1/255.0.0.0\nStatic Addresses: 10.0.0.1/255.0.0.0\n']

parsers = []
for text in PRELUDES + IDENTITIES:
    for header in ('CONTROL', 'FILE LIST', 'GPI SETTINGS', 'NO SUCH', 'CONTROL DEFAULT', 'OTHER', 'B C'):
        parsers.append({'fn': 'parse_section', 'text': text, 'header': header, 'result': P.parse_section(text, header)})
    parsers.append({'fn': 'parse_file_list', 'text': text, 'result': P.parse_file_list(text)})
    parsers.append({'fn': 'extract_label', 'text': text, 'result': P.extract_label(text)})
    try:
        parsers.append({'fn': 'annotate_preamble', 'text': text, 'result': annotate_preamble(text)})
    except Exception as e:  # noqa: BLE001
        parsers.append({'fn': 'annotate_preamble', 'text': text, 'error': exc(e)})
for text in PRELUDES + IDENTITIES:
    parsers.append({'fn': 'extract_model', 'text': text, 'result': P.extract_model(text)})
    parsers.append({'fn': 'parse_current_file', 'text': text, 'result': list(P.parse_current_file(text))})
for text in ECHOES:
    for index in (0, 2):
        try:
            parsers.append({'fn': 'parse_interface', 'text': text, 'index': index,
                            'result': iface_dict(N.parse_interface(text, index))})
        except Exception as e:  # noqa: BLE001
            parsers.append({'fn': 'parse_interface', 'text': text, 'index': index, 'error': exc(e)})
for name in ctl + ['Not A Param', '', 'matte density']:
    for raw in ('0', '5000', '10000', '2500', '-50', '30.9', 'inf', '-inf', 'nan', 'abc', '', '1e3', ' 7 ', '1_000',
                '\u0661', str(rng.randrange(-30000, 30000)), repr(rng.uniform(-20000, 20000))):
        d, u = to_display(name, raw)
        parsers.append({'fn': 'to_display', 'name': name, 'raw': raw, 'result': [None if d is None else repr(float(d)), u]})
for index, fields in [(0, []), (2, []), (0, [('Static Gateway', '192.0.2.1')]),
                      (1, [('Dynamic IP', 'true'), ('Static Addresses', '1.2.3.4/255.0.0.0'), ('Static DNS Servers', '')])]:
    parsers.append({'fn': 'build_block', 'index': index, 'fields': fields,
                    'result': N.build_block(index, **dict(fields)), 'section': N.section_name(index)})

# --------------------------------------------------------------------------
# Calls

calls = []
be32 = lambda n: struct.pack('>I', n)  # noqa: E731

# binary_request
for chunks in ([b'\x00\x00'], [], [be32(10), b'abcd'], [be32(0)], [be32(3), b'abc'], [be32(3), b'a', b'b', b'c'],
               [TIMEOUT], [be32(5), TIMEOUT], [b'\x00', b'\x00', b'\x00', b'\x01', b'z'], [be32(2), b'xyz'],
               [be32(1 << 31), b'short'], [be32(4), b'\xff\xfe\x00\x01']):
    for opcode, payload in ((0x0003, b'GPISettings'), (0x0000, 'slot 1'), (0x0000, 'pr\u00e9set')):
        calls.append(run('binary_request', [chunks], opcode=opcode, payload=payload if isinstance(payload, bytes)
                         else payload.encode()))
calls.append(run('binary_request', ['[Errno 111] Connection refused', [be32(1), b'x']], opcode=0, payload=b'slot1'))
calls.append(run('binary_request', ['[Errno 111] Connection refused'], opcode=0, payload=b'slot1'))

# upload_one
for chunks in ([b'\x00' * 6], [b'\x00' * 3], [], [b'\x00\x00\x00\x00\x00\x01'], [TIMEOUT], [b'\x00', TIMEOUT],
               [b'\x00' * 7], [b'\x01'], [b'\x00', b'\x00', b'\x00', b'\x00', b'\x00', b'\x00'], [b'\x00\x00', b'\x02']):
    for opcode, name, payload in ((0x0100, 'presets/slot1', b'payload'), (0x0103, 'GPISettings', b''),
                                  (0x0103, '/x/presets/pr\u00e9set', b'\x00' * 9)):
        calls.append(run('upload_one', [chunks], opcode=opcode, name=name, payload=payload))
calls.append(run('upload_one', ['[Errno 113] No route to host'], opcode=0x0100, name='presets/a', payload=b'a'))

# read_preamble
pre = SAMPLE.encode()
for chunks in ([pre[:20], pre[20:], b'trailing\n', TIMEOUT], [b'Label: Keyer A\nCONT'], [], [TIMEOUT],
               [pre], [pre, b'more', b' and more'], [b'END PRE', b'LUDE:\nafter'], [b'\xff\xfe', b'END PRELUDE:\n'],
               [b'abc', TIMEOUT, b'never read'], [pre, TIMEOUT, b'never read'], ['Caf\u00e9 '.encode()[:4], TIMEOUT]):
    calls.append(run('read_preamble', [chunks]))

# exchange and read_network
P0 = NPRELUDE
replies = ['ACK\n\n' + ECHO, 'NAK\n\n', '', 'ACK\n\nNETWORK INTERFACE 0:\nStatic Addresses: x\n\n', '  \nACK\n\nbody\n\n',
           '\nNAK\n\n', 'ack\n\n', 'NAKED\n', 'ACK', 'ACK\n\nNETWORK INTERFACE 0:\nStatic DNS Servers:\n\n', 'x' * 100,
           'ACK\n\n' + ECHO.replace('Static DNS Servers:', 'Static DNS Servers: 1.1.1.1'), 'ACK\n\n\n\nbody']
for reply in replies:
    for split in (False, True):
        chunks = [P0] + ([reply[:5].encode(), reply[5:].encode()] if split else [reply.encode()]) + [TIMEOUT]
        calls.append(run('exchange', [chunks], block='NETWORK INTERFACE 0:\n\n'))
        calls.append(run('read_network', [chunks], index=0))
calls.append(run('read_network', [[b'no prelude marker', TIMEOUT, b'ACK\n\n' + ECHO.encode(), TIMEOUT]], index=1))
calls.append(run('read_network', [[P0, b'ACK\n\n' + ECHO.replace('Priority: 0', 'Priority: \u00b2').encode(), TIMEOUT]],
                 index=0))
calls.append(run('read_network', ['[Errno 111] Connection refused'], index=0))

# set_network
SET_ACK = 'ACK\n\nNETWORK INTERFACE 0:\nStatic Addresses: x\n\n'
REAPPLYING = ECHO.replace('Current Addresses: 192.0.2.98/255.255.252.0', 'Current Addresses: 0.0.0.0/255.255.0.0').replace(
    'Static Addresses: 192.0.2.98/255.255.252.0', 'Static Addresses: 192.0.2.98/255.255.248.0')
SETTLED = ECHO.replace('255.255.252.0', '255.255.248.0')


def sess(reply):
    return [P0, reply.encode(), TIMEOUT] if isinstance(reply, str) and not reply.startswith('[Errno') else reply


widen = {'address': '192.0.2.98', 'netmask': '255.255.248.0'}
for kw, script in [
    (widen, [SET_ACK, 'ACK\n\n' + SETTLED]),
    (widen, [SET_ACK, 'ACK\n\n' + ECHO]),
    (widen, [SET_ACK, 'ACK\n\n' + REAPPLYING, 'ACK\n\n' + SETTLED]),
    (widen, [SET_ACK, 'ACK\n\n' + REAPPLYING, 'ACK\n\n' + REAPPLYING, 'ACK\n\n' + SETTLED]),
    (dict(widen, readback_timeout=0), [SET_ACK, 'ACK\n\n' + REAPPLYING]),
    (dict(widen, readback_timeout=1.25), [SET_ACK, 'ACK\n\n' + REAPPLYING]),
    (widen, [SET_ACK, 'ACK\n\n' + REAPPLYING]),
    (dict(widen, readback_timeout=0), [SET_ACK, '']),
    (dict(widen, readback_timeout=0), [SET_ACK, 'NAK\n\n']),
    (dict(widen, readback_timeout=1), [SET_ACK, '[Errno 111] Connection refused']),
    (dict(widen, readback_timeout=1), [SET_ACK, '[Errno 111] Connection refused', 'ACK\n\n' + SETTLED]),
    (dict(widen, readback_timeout=0),
     [SET_ACK, 'ACK\n\n' + ECHO.replace('Static Addresses: 192.0.2.98/255.255.252.0', 'Static Addresses: 192.0.2.98/255.255.248.0')]),
    ({'gateway': '192.0.2.1'}, [SET_ACK, 'ACK\n\n' + ECHO]),
    ({'gateway': '192.0.2.1', 'dns': ['8.8.8.8', '1.1.1.1']}, [SET_ACK, 'ACK\n\n' + ECHO]),
    ({'dns': []}, [SET_ACK, 'ACK\n\n' + ECHO]),
    ({'dynamic': True}, [SET_ACK, 'ACK\n\n' + ECHO]),
    ({'dynamic': False, 'address': '10.0.0.5', 'netmask': '255.0.0.0', 'gateway': '10.0.0.1', 'dns': ['10.0.0.2']},
     [SET_ACK, 'ACK\n\n' + ECHO.replace('192.0.2.98/255.255.252.0', '10.0.0.5/255.0.0.0')]),
    ({'address': '192.0.2.98'}, [SET_ACK]),
    ({'netmask': '255.255.248.0'}, [SET_ACK]),
    ({}, [SET_ACK]),
    (widen, ['NAK\n\n']),
    (widen, ['']),
    (dict(widen, readback_timeout=0), [SET_ACK, 'ACK\n\n' + ECHO.replace('Priority: 0', 'Priority: \u00b2')]),
]:
    for index in (0, 1):
        calls.append(run('set_network', [sess(s) for s in script], index=index, **kw))

# archive


def archive_scripts(prelude, slots_ok, resources_ok):
    """The prelude, then a slot read per FILE LIST name, then the two
    resources: each a reply, a short one, a refusal or a timeout."""
    scripts = [[prelude.encode(), TIMEOUT] if prelude else []]
    for name in P.parse_file_list(prelude):
        scripts.append(slots_ok(name))
    for name in ('GPISettings', 'SavedSettings'):
        scripts.append(resources_ok(name))
    return scripts


def reply(blob):
    return [be32(len(blob)), blob]


for prelude in [SAMPLE, SAMPLE.replace('Keyer A', 'Ultimatte 12 4K'), SAMPLE.replace('preset-b', 'preset-a'),
                SAMPLE.replace('preset-a\npreset-b', 'pr\u00e9set\nslot/with/slashes\n spaced '),
                SAMPLE.replace('FILE LIST:\npreset-a\npreset-b\n', ''), SAMPLE.replace('Label: Keyer A\n', ''),
                SAMPLE.replace('Label: Keyer A', 'Label: '), SAMPLE.replace('Matte Density: 5000', 'Matte Density: inf'),
                SAMPLE.replace('\n', '\r\n'), '', PRELUDES[-1], PRELUDES[-2]]:
    for mode in ('ok', 'some fail', 'all fail'):
        def slots_ok(name, mode=mode):
            if mode == 'all fail' or mode == 'some fail' and name.endswith('b'):
                return rng.choice([[], [b'\x00'], [TIMEOUT], '[Errno 111] Connection refused'])
            return reply(f'blob:{name}'.encode() * rng.randrange(1, 40))

        def res_ok(name, mode=mode):
            if mode == 'all fail' or mode == 'some fail' and name == 'GPISettings':
                return rng.choice([[], [be32(9), b'gpi'], '[Errno 111] Connection refused'])
            return reply(f'{name} bytes'.encode() + bytes(rng.randrange(256) for _ in range(rng.randrange(0, 300))))

        calls.append(run('archive', archive_scripts(prelude, slots_ok, res_ok)))
calls.append(run('archive', ['[Errno 111] Connection refused']))

# restore


def zip_of(members, *, strip_utf8_flag=False):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, 'w', zipfile.ZIP_DEFLATED) as zf:
        for name, data in members:
            # As writestr(name, data) makes it, but at a fixed time: with
            # the time of the run, every regeneration changed every zip.
            zi = zipfile.ZipInfo(name, date_time=(2026, 10, 21, 0, 0, 0))
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.external_attr = (0o40775 << 16 | 0x10) if zi.is_dir() else 0o600 << 16
            zf.writestr(zi, data)
    b = bytearray(buf.getvalue())
    if strip_utf8_flag:  # as a zip written without the UTF-8 flag would be: cp437 names
        for sig, off in ((b'PK\x03\x04', 6), (b'PK\x01\x02', 8)):
            i = b.find(sig)
            while i >= 0:
                flags = struct.unpack_from('<H', b, i + off)[0]
                struct.pack_into('<H', b, i + off, flags & ~0x800)
                i = b.find(sig, i + 4)
    return bytes(b)


ZIPS = [
    zip_of([('SavedSettings', b'S'), ('presets/', b''), ('presets/zeta', b'Z'), ('presets/alpha', b'A'),
            ('GPISettings', b'G'), ('notes.txt', b'ignored')]),
    zip_of([('presets/alpha', b'A'), ('presets/beta', b'B'), ('GPISettings', b'G'), ('SavedSettings', b'S')]),
    zip_of([('presets/only', b'O')]),
    zip_of([('GPISettings', b'G')]),
    zip_of([]),
    zip_of([('presets/a/b', b'nested'), ('presets/B', b'upper'), ('presets/a', b'lower'), ('Presets/x', b'case'),
            ('x/GPISettings', b'deep'), ('presets/\u00e9', b'utf8')]),
    zip_of([('presets/\u00e9t\u00e9', b'cp437'), ('SavedSettings', b'S')], strip_utf8_flag=True),
    zip_of([('presets/dup', b'first'), ('presets/dup', b'second')]),
    zip_of([('presets/big', bytes(rng.randrange(256) for _ in range(5000)))]),
    b'not a zip',
]
for i, z in enumerate(ZIPS):
    for acks in ([[b'\x00' * 6]], [[b'\x00' * 6], [b'\x00\x00\x00\x00\x00\x01']], [[]], [[b'\x00' * 6], [TIMEOUT]],
                 [[b'\x00' * 6], [b'\x00' * 6], '[Errno 111] Connection refused']):
        scripts = list(acks)
        # Every upload after the scripted ones acks.
        scripts += [[b'\x00' * 6]] * 12
        if acks == [[]]:
            scripts = [[]]  # every upload closes without an ACK
        calls.append(run('restore', scripts, zip=z))

with open(OUT, 'w') as f:
    json.dump({'bmdwire': 'v1.7.0', 'generator': 'ultimatte/testdata/oracle/gen_oracle.py',
               'ranges': {k: list(v) for k, v in RANGES.items()}, 'parsers': parsers, 'calls': calls},
              f, ensure_ascii=True, separators=(',', ':'))
    f.write('\n')
print(len(parsers), 'parser cases,', len(calls), 'calls')
