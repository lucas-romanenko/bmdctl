# SPDX-License-Identifier: MIT
"""Generate client.json: what bmdwire's hyperdeckwire client does.

    python3 -I gen_client.py /path/to/bmdwire ../fixtures client.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.3.0. Nothing here
opens a socket: every session runs hyperdeckwire's real ``Hyperdeck`` class
(hyperdeckwire/client.py) over a scripted stand-in socket, the way its own
tests do, and records what it did.

A session is a byte stream the deck sends (the greeting and every response,
in order) and a list of calls. For connect and each call it records:

* ``sent``: the bytes the client wrote, as hex;
* ``consumed``: how far into the stream the client had read when the call
  returned (bytes received less what it still holds in its buffer), so the
  Go test can hand a session that reads eagerly exactly what hyperdeckwire
  had read, call by call;
* ``result``: the return value as JSON (a dict, a list of clips as dicts, an
  int, null), or ``error``: the exception's class and message, and for a
  HyperdeckError its code and text.

The sessions are hyperdeckwire's own test scripts (the fixtures beside this
directory), every method against every kind of answer (ok, each error code,
213, multi-line, asynchronous messages before and after, blank lines, LF
line ends, one byte per recv, malformed heads, the stream ending), greetings
of every shape, and desynchronising streams (a stray response, a malformed
line, a short block). ``parsers`` records ``_parse_disk_line``,
``_parse_clips_v1_line`` and ``_looks_like_clip_line`` on edge lines, and
``params`` what ``Response.params`` makes of bodies. The seed is fixed, so
the output changes only when bmdwire or this script does.
"""
import dataclasses
import json
import logging
import os
import random
import sys

BMDWIRE, FIXTURES, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
sys.path.insert(0, BMDWIRE)

from hyperdeckwire import client as C  # noqa: E402

# The clip lines it skips are recorded in the results; its warnings aren't.
logging.getLogger('hyperdeckwire').setLevel(logging.ERROR)

random.seed(20261011)


def fixture(name):
    with open(os.path.join(FIXTURES, name), 'rb') as f:
        return f.read()


GREETING = fixture('greeting.bin')


class Sock:
    """hyperdeckwire's tests' _FakeSocket, counting what was received."""

    def __init__(self, chunks):
        self._script = list(chunks)
        self._inbox = b''
        self.sent = b''
        self.received = 0
        self.closed = False

    def settimeout(self, t):
        pass

    def sendall(self, data):
        if self.closed:
            raise OSError('closed')
        self.sent += data

    def recv(self, n):
        if self.closed:
            return b''
        if not self._inbox:
            if not self._script:
                return b''
            self._inbox = self._script.pop(0)
        chunk, self._inbox = self._inbox[:n], self._inbox[n:]
        self.received += len(chunk)
        return chunk

    def shutdown(self, how):
        pass

    def close(self):
        self.closed = True


def jsonable(v):
    if v is None or isinstance(v, (bool, int, str)):
        return v
    if isinstance(v, dict):
        return {k: jsonable(x) for k, x in v.items()}
    if isinstance(v, list):
        return [jsonable(x) for x in v]
    if dataclasses.is_dataclass(v):
        return jsonable(dataclasses.asdict(v))
    raise TypeError(type(v))


def error(e):
    out = {'class': type(e).__name__, 'message': str(e)}
    if isinstance(e, C.HyperdeckError):
        out['code'], out['text'] = e.code, e.text
    return out


def invoke(hd, method, args):
    if method == 'set_configuration':
        return hd.set_configuration(**dict(args['params']))
    return getattr(hd, method)(**args)


def session(name, stream, calls, chunking='whole'):
    """Run one session over stream, cut into chunks as chunking says."""
    if chunking == 'whole':
        chunks = [stream] if stream else []
    elif chunking == 'bytes':
        chunks = [stream[i:i + 1] for i in range(len(stream))]
    else:  # random cuts
        chunks, i = [], 0
        while i < len(stream):
            n = random.randint(1, 9)
            chunks.append(stream[i:i + n])
            i += n
    sock = Sock(chunks)
    hd = C.Hyperdeck('192.0.2.11', socket_factory=lambda *_a, **_k: sock)
    rec = {'name': name, 'stream': stream.hex(), 'chunking': chunking}

    def step(fn):
        before = len(sock.sent)
        out = {}
        try:
            out['result'] = jsonable(fn())
        except Exception as e:  # noqa: BLE001 - every exception is recorded
            out['error'] = error(e)
        out['sent'] = sock.sent[before:].hex()
        out['consumed'] = sock.received - len(hd._buf)
        return out

    rec['connect'] = step(hd.connect)
    rec['connect']['model'] = hd.model
    rec['connect']['protocol_version'] = hd.protocol_version
    g = hd._greeting
    rec['connect']['greeting'] = None if g is None else {'code': g.code, 'text': g.text, 'lines': g.lines}
    rec['calls'] = []
    if 'error' not in rec['connect']:
        for method, args in calls:
            c = step(lambda: invoke(hd, method, args))
            c['method'], c['args'] = method, args
            rec['calls'].append(c)
    SESSIONS.append(rec)


SESSIONS = []
OK = b'200 ok\r\n'

# ---------------------------------------------------------------------------
# hyperdeckwire's own tests, on the fixtures copied from them
# ---------------------------------------------------------------------------

session('test_connect_parses_greeting', GREETING, [])
session('test_simple_ack_response_is_ok_no_lines', GREETING + fixture('ok.bin'), [('stop', {})])
session('test_multi_line_response_terminates_on_blank_line', GREETING + fixture('device-info.bin'),
        [('device_info', {})])
session('test_error_response_raises_hyperdeck_error_with_code', GREETING + fixture('remote-disabled.bin'),
        [('stop', {})])
session('test_async_message_skipped_so_real_response_returns',
        GREETING + fixture('async-transport-stopped-then-ok.bin'), [('stop', {})])
session('test_disk_list_filters_header_lines_and_returns_clips', GREETING + fixture('disk-list.bin'),
        [('disk_list', {})])
session('test_play_with_loop_and_single_clip_builds_correct_command', GREETING + fixture('ok.bin'),
        [('play', {'loop': True, 'single_clip': True})])
session('test_play_with_no_args_sends_plain_play', GREETING + fixture('ok.bin'), [('play', {})])
session('test_pause_sends_play_speed_zero', GREETING + fixture('ok.bin'), [('pause', {})])
session('test_clips_add_preserves_spaces_in_name', GREETING + fixture('ok.bin'),
        [('clips_add', {'name': 'Intro Loop animation2.mp4'})])
session('test_clips_add_with_before_clip_id_inserts', GREETING + fixture('ok.bin'),
        [('clips_add', {'name': 'newer.mp4', 'before_clip_id': 3})])
session('test_goto_clip_sends_clip_id_param', GREETING + fixture('ok.bin'), [('goto_clip', {'clip_id': 5})])
session('test_remote_enable_true_sets_remote_to_true', GREETING + fixture('ok.bin'),
        [('remote_enable', {'enable': True})])
session('test_set_configuration_single_param_builds_command', GREETING + fixture('ok.bin'),
        [('set_configuration', {'params': [['file_format', 'H.264High']]})])
session('test_set_configuration_stacks_multiple_params_in_one_command', GREETING + fixture('ok.bin'),
        [('set_configuration', {'params': [['file_format', 'QuickTimeProResHQ'],
                                           ['default_standard', '2160p25'], ['video_input', 'SDI']]})])
session('test_set_configuration_translates_bool_to_protocol_strings', GREETING + fixture('ok.bin'),
        [('set_configuration', {'params': [['record_cache', True], ['append_timestamp', False]]})])
session('test_set_configuration_no_args_is_noop', GREETING, [('set_configuration', {'params': []})])
session('test_set_configuration_raises_on_protocol_error', GREETING + fixture('invalid-value.bin'),
        [('set_configuration', {'params': [['file_format', 'NotARealFormat']]})])
session('test_set_configuration_accepts_213_deck_rebooting_as_success', GREETING + fixture('deck-rebooting.bin'),
        [('set_configuration', {'params': [['file_format', 'QuickTimeProResHQ']]})])
session('test_clips_count_returns_int', GREETING + fixture('clips-count.bin'), [('clips_count', {})])
session('test_context_manager_connects_and_closes', GREETING, [('close', {})])
session('test_close_is_idempotent', GREETING, [('close', {}), ('close', {})])

# ---------------------------------------------------------------------------
# Every method against every kind of answer
# ---------------------------------------------------------------------------

READS = {
    'device_info': b'204 device info:\r\nprotocol version: 1.13\r\nmodel: HyperDeck Studio HD Mini\r\n'
                   b'unique id: 7c2e0d18dc78\r\nslot count: 2\r\nsoftware version: 8.1.1\r\n'
                   b'name: Deck 4 wide\r\n\r\n',
    'remote_info': b'210 remote info:\r\nenabled: true\r\noverride: false\r\n\r\n',
    'slot_info': b'202 slot info:\r\nslot id: 1\r\nstatus: mounted\r\nvolume name: Untitled\r\n'
                 b'recording time: 12345\r\nvideo format: 1080p25\r\nblocked: false\r\n\r\n',
    'transport_info': b'208 transport info:\r\nstatus: stopped\r\nspeed: 0\r\nslot id: 1\r\nclip id: 2\r\n'
                      b'single clip: false\r\ndisplay timecode: 00:00:10:00\r\ntimecode: 00:00:10:00\r\n'
                      b'video format: 1080p25\r\nloop: false\r\n\r\n',
    'configuration': b'211 configuration:\r\naudio input: embedded\r\nvideo input: SDI\r\n'
                     b'file format: QuickTimeProResHQ\r\nrecord prefix: My Prefix: two\r\n\r\n',
    'disk_list': b'206 disk list:\r\nslot id: 1\r\n1: Intro Loop animation.mp4 H.264 1080p25 00:01:03:17\r\n'
                 b'2: Other Clip.mov ProRes 1080p25 00:00:30:00\r\n3: bad\r\n\r\n',
    'clips_get': b'205 clips info:\r\nclip count: 2\r\n1: Intro Loop animation.mp4 00:00:00:00 00:01:03:17\r\n'
                 b'2: Other.mov 00:01:03:17 00:00:30:00\r\n\r\n',
    'clips_count': b'214 clips count:\r\nclip count: 2\r\n\r\n',
}

METHODS = [
    ('device_info', {}), ('remote_info', {}), ('slot_info', {}), ('slot_info', {'slot_id': 2}),
    ('transport_info', {}), ('configuration', {}), ('disk_list', {}), ('disk_list', {'slot_id': 2}),
    ('clips_count', {}), ('clips_get', {}), ('stop', {}), ('play', {}),
    ('play', {'loop': True}), ('play', {'single_clip': True}), ('play', {'speed': 200}),
    ('play', {'speed': -5000}), ('play', {'speed': 0}), ('play', {'clip_id': 3}),
    ('play', {'clip_id': 3, 'loop': True, 'single_clip': True, 'speed': 50}),
    ('play', {'loop': False, 'single_clip': False}), ('pause', {}), ('clips_clear', {}),
    ('clips_add', {'name': 'a.mov'}), ('clips_add', {'name': 'folder/b c.mp4', 'before_clip_id': 1}),
    ('clips_add', {'name': ''}), ('clips_remove', {'clip_id': 2}), ('goto_clip', {'clip_id': 1}),
    ('goto_clip', {'clip_id': -1}), ('remote_enable', {'enable': True}), ('remote_enable', {'enable': False}),
    ('slot_select', {'slot_id': 2}), ('ping', {}),
    ('set_configuration', {'params': [['file_format', 'H.264High']]}),
    ('set_configuration', {'params': [['record prefix', 'Show 1'], ['record_cache', True], ['timecode_input', 'SDI']]}),
    ('set_configuration', {'params': [['default_standard', 25]]}),
]

ERRORS = [
    b'100 syntax error', b'101 unsupported parameter', b'102 invalid value', b'103 unsupported', b'104 disk full',
    b'105 no disk', b'106 disk error', b'107 timeline empty', b'108 internal error', b'109 out of range',
    b'110 no input', b'111 remote control disabled', b'112 clip not found', b'120 connection rejected',
    b'150 invalid state', b'151 invalid codec', b'160 invalid format', b'161 invalid token',
    b'162 format not prepared', b'163 parameterized single line command not supported', b'199 x', b'100',
]

ASYNC = [
    b'508 transport info:\r\nstatus: play\r\nspeed: 100\r\n\r\n',
    b'502 slot info:\r\nslot id: 2\r\nstatus: empty\r\n\r\n',
    b'510 remote info:\r\nenabled: false\r\n\r\n',
    b'511 configuration:\r\nfile format: H.264High\r\n\r\n',
    b'599 something\r\n',
]


def answer(method):
    return READS.get(method, OK)


for method, args in METHODS:
    a = answer(method)
    label = f'{method} {json.dumps(args, sort_keys=True)}'
    session(f'{label}: answered', GREETING + a, [(method, args)])
    session(f'{label}: answered, one byte per recv', GREETING + a, [(method, args)], 'bytes')
    session(f'{label}: answered, LF line ends', GREETING + a.replace(b'\r\n', b'\n'), [(method, args)])
    for e in random.sample(ERRORS, 3):
        session(f'{label}: {e.decode()}', GREETING + e + b'\r\n', [(method, args)])
    session(f'{label}: 200 ok', GREETING + OK, [(method, args)])
    session(f'{label}: 213 deck rebooting', GREETING + b'213 deck rebooting\r\n', [(method, args)])
    session(f'{label}: asynchronous messages first', GREETING + b''.join(random.sample(ASYNC, 3)) + a,
            [(method, args)], 'random')
    session(f'{label}: an asynchronous message after', GREETING + a + ASYNC[0], [(method, args), (method, args)])
    session(f'{label}: blank lines first', GREETING + b'\r\n\r\n\n' + a, [(method, args)])
    session(f'{label}: the stream ends', GREETING, [(method, args)])
    session(f'{label}: the stream ends inside the answer', GREETING + a[:len(a) // 2], [(method, args)])
    session(f'{label}: only an asynchronous message', GREETING + ASYNC[1], [(method, args)])
    session(f'{label}: a malformed head', GREETING + b'garbage line\r\n' + a, [(method, args), (method, args)])
    session(f'{label}: twice', GREETING + a + a, [(method, args), (method, args)])

# Every error code, with and without text, on one command.
for e in ERRORS:
    session(f'stop: {e.decode()}', GREETING + e + b'\r\n', [('stop', {})])

# Response codes at and outside each range, and odd heads.
for head in [b'0 zero', b'99 low', b'199 top of errors', b'200', b'201 x:', b'299 top', b'300 three', b'400 four',
             b'499 x', b'500 hello', b'599 top', b'600 six', b'999 nine', b'-1 neg', b'+200 ok', b'0200 ok',
             b'2_00 ok', b'\xd9\xa2\xd9\xa0\xd9\xa0 arabic', b'200  two spaces', b'200 ok:', b'200 ok::',
             b'200\tok', b' 200 ok', b'200 \xff\xfe bytes', b'2 00 ok', b'200_ ok', b'_200 ok',
             b'200 trailing\r\r', b'\x00200 ok']:
    body = b'k: v\r\n\r\n' if head.rstrip(b'\r').endswith(b':') else b''
    session(f'head {head!r}', GREETING + head + b'\r\n' + body, [('stop', {}), ('device_info', {})])

# ---------------------------------------------------------------------------
# Greetings
# ---------------------------------------------------------------------------

GREETINGS = [
    GREETING,
    GREETING.replace(b'\r\n', b'\n'),
    b'\r\n\r\n' + GREETING,
    b'500 connection info:\r\nprotocol version: 1.11\r\nmodel: HyperDeck Studio Mini\r\n\r\n',
    b'500 connection info:\r\nprotocol version:1.13\r\n  model :  HyperDeck Extreme 8K HDR  \r\n\r\n',
    b'500 connection info:\r\nmodel: A: B\r\nmodel: second wins\r\nno colon here\r\n\r\n',
    b'500 connection info\r\n',
    b'500 connection info:\r\n\r\n',
    b'500 connection info:\r\nmodel: caf\xc3\xa9 \xe2\x80\x94 \xff\xfe\xe2\x82 deck\r\n\r\n',
    b'500 connection info:\r\nmodel: \xe0\x80\x80 \xed\xa0\x80 \xf0\x9f\x98\r\nprotocol version: 1.13\r\n\r\n',
    b'500 connection info:\r\nmodel:\xc2\xa0 nbsp \xe3\x80\x80\r\nprotocol version: \x1c1.13\x1f\r\n\r\n',
    b'120 connection rejected\r\n',
    b'508 transport info:\r\nstatus: play\r\n\r\n',
    b'200 ok\r\n',
    b'garbage\r\n',
    b'',
    b'500 connection info:\r\nmodel: cut short',
    b'500 connection info:\r\nmodel: X\r\r\n',
    b'500 connection info:\r\nmodel: X\r\n   \r\n',
]
for i, g in enumerate(GREETINGS):
    for chunking in ('whole', 'bytes'):
        session(f'greeting {i}', g, [('device_info', {})] if g else [], chunking)
        session(f'greeting {i} then a command', g + OK, [('stop', {})], chunking)

# ---------------------------------------------------------------------------
# Listings with every kind of line
# ---------------------------------------------------------------------------

DISK_LINES = [
    b'slot id: 1', b'1: a.mov H.264 1080p25 00:00:01:00', b'2: name with  two spaces.mov ProRes 1080p25 00:00:02:00',
    b'3: only three tokens', b'4:a.mov H.264 1080p25 00:00:01:00', b' 5 : spaced.mov H.264 1080p25 00:00:01:00',
    b'6: a b c d e f', b'x: not a clip', b'07: zero padded.mov H.264 1080p50 01:00:00:00', b'8:', b'9: ',
    b'10: trailing.mov H.264 1080p25 00:00:01:00 ', b'\xd9\xa1\xd9\xa1: arabic id.mov H.264 1080p25 00:00:01:00',
    b'\xc2\xb2: superscript.mov H.264 1080p25 00:00:01:00', b'12: tab\there.mov H.264 1080p25 00:00:01:00',
    b'13: caf\xc3\xa9 \xff.mov H.264 1080p25 00:00:01:00', b'14:  .mov H.264 1080p25 00:00:01:00',
    b'15: a:b.mov H.264 1080p25 00:00:01:00', b'-1: negative.mov H.264 1080p25 00:00:01:00',
    b'clip count: 3', b'16: x\xc2\xa0y.mov H.264 1080p25 00:00:01:00',
]
for n in range(12):
    lines = random.sample(DISK_LINES, random.randint(1, 8))
    body = b'206 disk list:\r\n' + b''.join(l + b'\r\n' for l in lines) + b'\r\n'
    session(f'disk list mix {n}', GREETING + body, [('disk_list', {})])
    body = b'205 clips info:\r\n' + b''.join(l + b'\r\n' for l in lines) + b'\r\n'
    session(f'clips get mix {n}', GREETING + body, [('clips_get', {})])
for v in [b'0', b'12', b' 3 ', b'x', b'', b'-2', b'1_0', b'\xd9\xa3']:
    session(f'clips count {v!r}', GREETING + b'214 clips count:\r\nclip count: ' + v + b'\r\n\r\n',
            [('clips_count', {})])
session('clips count without its line', GREETING + b'214 clips count:\r\nother: 1\r\n\r\n', [('clips_count', {})])

# ---------------------------------------------------------------------------
# Streams that desynchronise, as they do in hyperdeckwire
# ---------------------------------------------------------------------------

session('a stray response is the next command\'s', GREETING + OK + b'102 invalid value\r\n',
        [('stop', {}), ('stop', {})])
session('a short block takes the next response as its body',
        GREETING + b'208 transport info:\r\nstatus: play\r\n' + OK + b'\r\n' + OK,
        [('transport_info', {}), ('stop', {})])
session('a malformed line, then the real answer', GREETING + b'??\r\n' + OK, [('stop', {}), ('stop', {})])
session('after close nothing is sent', GREETING + OK, [('close', {}), ('stop', {})])
session('a cue and loop, as hyperdeckwire does on hardware',
        GREETING + fixture('disk-list.bin') + OK + OK + OK + OK + READS['transport_info'],
        [('disk_list', {}), ('stop', {}), ('clips_clear', {}), ('clips_add', {'name': 'Intro Loop animation.mp4'}),
         ('play', {'loop': True, 'single_clip': True}), ('transport_info', {})])

# ---------------------------------------------------------------------------
# The line parsers and Response.params
# ---------------------------------------------------------------------------

PARSERS = []
LINES = [l.decode('utf-8', 'replace') for l in DISK_LINES] + [
    '', ' ', ':', '1', '1:', '1: a', '1: a b', '1: a b c', '1: a b c d', '1: a  b c d', ' 1: a b c d ',
    '1 : a b c d', '1:\ta b c d', '1: a b c d\t', '٣: a b c d', '²: a b c d', '1: 名前 H.264 1080p25 00:00:01:00',
    '1: a\u3000b c d', '1:\u00a0a b c d', '\u20281: a b c d', '1_0: a b c d', '+1: a b c d', '1: a b c\x1c',
]
for line in LINES:
    for fn in ('_parse_disk_line', '_parse_clips_v1_line', '_looks_like_clip_line'):
        rec = {'fn': fn, 'line': line}
        try:
            rec['result'] = jsonable(getattr(C, fn)(line))
        except Exception as e:  # noqa: BLE001
            rec['error'] = error(e)
        PARSERS.append(rec)

PARAMS = []
for lines in [
    [], ['no-colon-line'], ['a: 1', 'b: 2'], ['  unique id: abcdef  '], ['a: 1', 'a: 2'], ['a:b:c'], [':x'],
    ['k:'], ['k :  v  '], ['', '   ', '\t'], ['\u00a0key\u00a0: \u3000value\u2003'], ['\x1ck: v\x1f'],
    ['key with spaces: value with spaces'], ['k:\tv'], ['K: V', 'k: v'], ['ünïcode: välue'],
]:
    PARAMS.append({'lines': lines, 'result': C.Response(code=200, text='x', lines=lines).params()})

doc = {'bmdwire': 'v1.3.0', 'sessions': SESSIONS, 'parsers': PARSERS, 'params': PARAMS}
with open(OUT, 'w') as f:
    json.dump(doc, f, indent=1, ensure_ascii=False, sort_keys=True)
    f.write('\n')
print(f'{len(SESSIONS)} sessions, {sum(len(s["calls"]) for s in SESSIONS)} calls, '
      f'{len(PARSERS)} parser cases, {len(PARAMS)} params cases', file=sys.stderr)
