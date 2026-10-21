# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: MIT
"""Generate client.json: what bmdwire's videohubwire client does.

    python3 -I gen_client.py /path/to/bmdwire client.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.6.0.

Each session is a Videohub on a scripted socket, as the library's own
tests drive it: the preamble's chunks, then steps. A call's reply chunks
are given to the client only once it has sent (on the wire a hub answers
what it was sent), a push between calls is the hub sending of its own,
and state() reads it. Each step records the bytes sent, the result
(state()'s dict, or None) or the exception (class and message), and the
client's six attributes after it, and how much of what the hub sent it
hadn't read yet (where it had, a session's state must be the same). The
Go test in videohub/ replays every
session through a Session.

Two things are left out because a Go session differs there on purpose
(docs/PORTING.md, "Videohub"): a call that times out is a session's last
(videohubwire carries on, a session ends), and no push holds an ACK or
NAK (videohubwire reads a pushed ACK as the next call's answer, a session
drops it as an answer to no command). Numbers stay within int64.
"""
import json
import random
import socket
import sys

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)

from videohubwire.client import Videohub  # noqa: E402

rng = random.Random(0x9990)


class Sock:
    """The hub's end: hands out the script, then the reply armed for each
    send once it is sent; a dry script is a timeout, b'' the hub closing."""

    def __init__(self, chunks):
        self.chunks = list(chunks)
        self.armed = []
        self.sent = b''
        self.closed = False

    def settimeout(self, t):
        pass

    def sendall(self, data):
        if self.closed:
            raise OSError('closed')
        self.sent += data
        self.chunks += self.armed
        self.armed = []

    def recv(self, n):
        if not self.chunks:
            raise socket.timeout()
        c = self.chunks[0]
        if c == b'':
            return b''
        take, rest = c[:n], c[n:]
        if rest:
            self.chunks[0] = rest
        else:
            self.chunks.pop(0)
        return take

    def shutdown(self, how):
        pass

    def close(self):
        self.closed = True


def attrs(vh):
    return {'protocol_version': vh.protocol_version, 'device': dict(vh.device),
            'input_labels': {str(k): v for k, v in vh.input_labels.items()},
            'output_labels': {str(k): v for k, v in vh.output_labels.items()},
            'routing': {str(k): v for k, v in vh.routing.items()},
            'locks': {str(k): v for k, v in vh.locks.items()}}


def unread(sock, vh):
    """How many bytes the hub has sent that the client hasn't taken in:
    a session reads them all the same."""
    return len(b''.join(sock.chunks)) + len(vh._buf)


def exc(e):
    return {'class': type(e).__name__, 'message': str(e)}


def run(name, preamble, steps):
    sock = Sock(preamble)
    vh = Videohub('192.0.2.31', read_timeout=0.05, socket_factory=lambda addr, timeout: sock)
    rec = {'name': name, 'preamble': [c.hex() for c in preamble], 'steps': []}
    try:
        vh.connect()
        rec['connect'] = {'attrs': attrs(vh), 'unread': unread(sock, vh)}
    except Exception as e:  # noqa: BLE001
        rec['connect'] = {'error': exc(e)}
        return rec
    for st in steps:
        out = dict(st)
        for k in ('reply', 'push'):
            if k in st:
                out[k] = [c.hex() for c in st[k]]
        before = len(sock.sent)
        if 'push' in st:
            sock.chunks += st['push']
            out['attrs'] = attrs(vh)
            rec['steps'].append(out)
            continue
        sock.armed = list(st.get('reply', []))
        call = st['call']
        try:
            if call == 'state':
                out['result'] = vh.state()
            elif call == 'close':
                vh.close()
                out['result'] = None
            else:
                getattr(vh, call)(*st.get('args', []))
                out['result'] = None
        except Exception as e:  # noqa: BLE001
            out['error'] = exc(e)
        sock.armed = []
        out['sent'] = sock.sent[before:].hex()
        out['attrs'] = attrs(vh)
        out['unread'] = unread(sock, vh)
        rec['steps'].append(out)
        if out.get('error', {}).get('message', '').startswith('timed out'):
            break  # a session ends there (the module docstring)
    return rec


with open(BMDWIRE + '/videohubwire/tests/test_videohubwire_client.py') as f:
    src = f.read()
ns = {}
exec(src[src.index('PREAMBLE = ('):src.index('class _FakeSocket')], ns)
PREAMBLE, END = ns['PREAMBLE'], ns['END_PRELUDE']


def block(header, lines=()):
    return (header + '\n' + ''.join(l + '\n' for l in lines) + '\n').encode()


def hub(n_in, n_out, *, labels=True, locks=True, marker=True, version='2.7', model='Blackmagic Smart Videohub 40 x 40',
        friendly='Test Hub', extra=b'', routes=None):
    b = block('PROTOCOL PREAMBLE:', [f'Version: {version}'])
    b += block('VIDEOHUB DEVICE:', ['Device present: true', f'Model name: {model}', f'Friendly name: {friendly}',
                                    f'Video inputs: {n_in}', f'Video outputs: {n_out}'])
    if labels:
        b += block('INPUT LABELS:', [f'{i} Input {i + 1}' for i in range(n_in)])
        b += block('OUTPUT LABELS:', [f'{i} Out {i + 1}' for i in range(n_out)])
    if locks:
        b += block('VIDEO OUTPUT LOCKS:', [f'{i} {rng.choice("UUUOL")}' for i in range(n_out)])
    routes = routes or {i: rng.randrange(max(n_in, 1)) for i in range(n_out)}
    b += block('VIDEO OUTPUT ROUTING:', [f'{d} {s}' for d, s in routes.items()])
    b += extra
    if marker:
        b += END
    return b


def chunked(b):
    """The stream in random pieces, as TCP may deliver it."""
    out, i = [], 0
    while i < len(b):
        n = rng.choice([1, 2, 3, 7, 50, 4096, 9999])
        out.append(b[i:i + n])
        i += n
    return out


ACK, NAK = b'ACK\n\n', b'NAK\n\n'
LABELS = ['Replay B', '', ' ', 'CAM\n1\r\nEVIL', 'a b  c\x1cd', 'x' * 64, 'x' * 65, 'é' * 64, 'é' * 65,
          '  padded  ', 'Wall Monitor 1', '中文 ラベル', 'tab\tsep', 'x' * 63 + ' y', 'quote "q" \'s\'', ' line']
PUSHES = [block('VIDEO OUTPUT ROUTING:', ['1 3']), block('INPUT LABELS:', ['0 Renamed', '2 ']),
          block('VIDEO OUTPUT LOCKS:', ['1 U', '3 L']), block('OUTPUT LABELS:', ['3 Wall']),
          block('VIDEOHUB DEVICE:', ['Friendly name: Renamed Hub']), block('CONFIGURATION:', ['Take Mode: true']),
          block('VIDEO OUTPUT ROUTING:', ['x 1', '2 y', '3 2']), b'\n' + block('INPUT LABELS:', ['1 Lost']),
          block('PROTOCOL PREAMBLE:', ['Version: 2.8'])]

sessions = []

# The library's tests' preamble, with and without the marker.
sessions.append(run('tests-preamble', [PREAMBLE + END], [{'call': 'state'}]))
sessions.append(run('tests-no-marker', [PREAMBLE], [{'call': 'state'}]))
sessions.append(run('tests-chunked', chunked(PREAMBLE + END), [{'call': 'state'}]))

# Preambles that fail or end oddly.
for name, pre in [
    ('garbage', [b'220 Welcome to some FTP server\r\n']),
    ('closed', [b'']),
    ('closed-mid-block', [PREAMBLE[:100], b'']),
    ('closed-after-essentials', [PREAMBLE, b'']),
    ('no-routing-no-marker', [PREAMBLE.split(b'VIDEO OUTPUT ROUTING:')[0]]),
    ('no-routing-marker', [PREAMBLE.split(b'VIDEO OUTPUT ROUTING:')[0] + END]),
    ('marker-only', [END]),
    ('marker-first', [END + PREAMBLE]),
    ('crlf', [PREAMBLE.replace(b'\n', b'\r\n') + END.replace(b'\n', b'\r\n')]),
    ('crlf-lines', [PREAMBLE.replace(b':\n', b':\r\n') + END]),
    ('stray-blank-lines', [PREAMBLE.replace(b'\n\nINPUT', b'\n\n\nINPUT') + END]),
    ('double-blank-start', [b'\n\n' + PREAMBLE + END]),
    ('invalid-utf8', [PREAMBLE.replace(b'Camera 1', b'Cam\xff\xfe 1').replace(b'Test Hub', b'Test \xc3Hub') + END]),
    ('unknown-blocks', [PREAMBLE + b'CONFIGURATION:\nTake Mode: true\n\nSERIAL PORT LABELS:\n0 Deck\n\n'
                        b'VIDEO INPUT STATUS:\n0 BNC\n\nACK\n\nNAK\n\n' + END]),
    ('device-odd-keys', [b'VIDEOHUB DEVICE:\nMODEL NAME: X\nno colon line\n Video Inputs : 3 \nVideo outputs:2\n' +
                         'Friendly Name:  A: B \n\u03a3\u0391\u03a3: sigma\n\u0130D: dotted\n\n'.encode() +
                         b'VIDEO OUTPUT ROUTING:\n0 0\n\n' + END]),
    ('int-forms', [PREAMBLE + b'VIDEO OUTPUT ROUTING:\n+1 0\n1_0 1\n\xd9\xa1 2\n 3 0\n01 1\n-1 2\n2 +3\n2 3_\n'
                   b'_2 1\n9223372036854775807 1\n2\t3\n\n' + END]),
    ('labels-forms', [PREAMBLE + b'INPUT LABELS:\n0   spaced   out  \n1 \n2\n-1 negative\n5 beyond\nx bad\n'
                      b'3 \xe2\x80\x83em\xe2\x80\x83space\n\nVIDEO OUTPUT LOCKS:\n0 X\n1 \n2 L extra\n\n' + END]),
    ('no-counts', [PREAMBLE.replace(b'Video inputs: 4\n', b'').replace(b'Video outputs: 4\n', b'') + END]),
    ('empty-counts', [PREAMBLE.replace(b'Video inputs: 4', b'Video inputs:').replace(b'Video outputs: 4', b'Video outputs: ') + END]),
    ('bad-counts', [PREAMBLE.replace(b'Video inputs: 4', b'Video inputs: four') + END]),
    ('bad-output-count', [PREAMBLE.replace(b'Video outputs: 4', b'Video outputs: 4.0') + END]),
    ('negative-counts', [PREAMBLE.replace(b'Video inputs: 4', b'Video inputs: -2') + END]),
    ('unicode-counts', [PREAMBLE.replace(b'Video inputs: 4', 'Video inputs: ٤'.encode()) + END]),
    ('version-twice', [b'PROTOCOL PREAMBLE:\nVersion: 2.7\nversion: 2.8\nVERSION :\n\n' + PREAMBLE + END]),
]:
    sessions.append(run(name, pre, [{'call': 'state'}]))

# Calls on the tests' hub: every reply shape for each command.
replies = [('ack', [ACK]), ('nak', [NAK]), ('push-then-ack', [PUSHES[0] + ACK]),
           ('pushes-then-ack', [PUSHES[1] + PUSHES[2] + PUSHES[6] + ACK]), ('ack-split', [b'AC', b'K\n', b'\n']),
           ('blank-then-ack', [b'\n\nACK\n\n']), ('ack-spaces', [b'  ACK  \n\n']), ('nak-then-ack', [NAK + ACK]),
           ('lowercase-ack', [b'ack\n\n' + ACK]), ('ack-with-body', [b'ACK\nextra\n\n'])]
calls = [('route', [1, 3]), ('route', [0, 0]), ('route', [3, 3]), ('set_input_label', [2, 'Replay B']),
         ('set_output_label', [0, 'Wall Monitor 1']), ('ping', [])]
for rname, reply in replies:
    for call, args in calls:
        sessions.append(run(f'{call}-{rname}', [PREAMBLE + END],
                            [{'call': call, 'args': args, 'reply': reply}, {'call': 'state'}]))
# Refused before the wire, on the tests' hub and on hubs without counts.
for pre_name, pre in [('tests', PREAMBLE + END),
                      ('no-counts', PREAMBLE.replace(b'Video inputs: 4\n', b'').replace(b'Video outputs: 4\n', b'') + END),
                      ('bad-counts', PREAMBLE.replace(b'Video inputs: 4', b'Video inputs: four') + END),
                      ('bad-output-count', PREAMBLE.replace(b'Video outputs: 4', b'Video outputs: x') + END),
                      ('negative-counts', PREAMBLE.replace(b'Video outputs: 4', b'Video outputs: -1') + END)]:
    steps = []
    for call, args in [('route', [99, 0]), ('route', [0, 99]), ('route', [-1, 0]), ('route', [0, -1]), ('route', [4, 0]),
                       ('route', [0, 4]), ('set_input_label', [99, 'x']), ('set_input_label', [4, 'x']),
                       ('set_input_label', [-1, 'x']), ('set_output_label', [-1, 'x']),
                       ('set_output_label', [4, 'x']), ('set_input_label', [0, 'x' * 65])]:
        steps.append({'call': call, 'args': args, 'reply': [ACK]})
    steps.append({'call': 'state'})
    sessions.append(run(f'refused-{pre_name}', [pre], steps))
# Labels: whitespace, length in characters, unicode.
for i, label in enumerate(LABELS):
    for setter in ('set_input_label', 'set_output_label'):
        sessions.append(run(f'{setter}-{i}', [PREAMBLE + END],
                            [{'call': setter, 'args': [i % 4, label], 'reply': [ACK]}, {'call': 'state'}]))
# Pushes between calls.
for i, push in enumerate(PUSHES):
    sessions.append(run(f'push-{i}', [PREAMBLE + END],
                        [{'push': [push]}, {'call': 'state'}, {'call': 'route', 'args': [2, 2], 'reply': [ACK]},
                         {'call': 'state'}]))
# Close, then calls: not connected.
sessions.append(run('close', [PREAMBLE + END],
                    [{'call': 'close'}, {'call': 'route', 'args': [1, 3], 'reply': [ACK]}, {'call': 'ping', 'reply': [ACK]},
                     {'call': 'close'}]))
# Timeouts, each a session's last.
for call, args in calls:
    for rname, reply in [('none', []), ('nak-only', [NAK]), ('crlf-ack', [b'ACK\r\n\r\n']), ('push-only', [PUSHES[0]])]:
        if call != 'ping' and rname == 'nak-only':
            continue
        sessions.append(run(f'{call}-timeout-{rname}', [PREAMBLE + END],
                            [{'call': call, 'args': args, 'reply': reply}]))
# Random hubs and random sequences of calls.
for n in range(60):
    n_in, n_out = rng.choice([(4, 4), (12, 12), (16, 16), (40, 40), (10, 6), (6, 10), (1, 1), (72, 72), (0, 0)])
    pre = hub(n_in, n_out, labels=rng.random() < .8, locks=rng.random() < .7, marker=rng.random() < .8)
    steps = []
    for _ in range(rng.randrange(1, 8)):
        r = rng.random()
        if r < .15:
            steps.append({'push': [rng.choice(PUSHES)]})
            steps.append({'call': 'state'})
            continue
        call = rng.choice(['route', 'route', 'route', 'set_input_label', 'set_output_label', 'ping', 'state'])
        if call == 'state':
            steps.append({'call': 'state'})
            continue
        reply = rng.choice([[ACK], [ACK], [NAK], [PUSHES[0] + ACK], [rng.choice(PUSHES) + ACK]])
        if call == 'route':
            args = [rng.randrange(-1, n_out + 2), rng.randrange(-1, n_in + 2)]
        elif call == 'ping':
            args = []
        else:
            args = [rng.randrange(-1, max(n_in, n_out) + 2), rng.choice(LABELS)]
        steps.append({'call': call, 'args': args, 'reply': reply})
    steps.append({'call': 'state'})
    sessions.append(run(f'random-{n}', chunked(pre) if rng.random() < .3 else [pre], steps))

with open(OUT, 'w') as f:
    f.write('{"bmdwire":"v1.6.0","generator":"videohub/testdata/oracle/gen_client.py","sessions":[\n')
    f.write(',\n'.join(json.dumps(s, sort_keys=True, ensure_ascii=True, separators=(',', ':')) for s in sessions))
    f.write('\n]}\n')
n_steps = sum(len(s['steps']) for s in sessions)
print(len(sessions), 'sessions,', n_steps, 'steps')
