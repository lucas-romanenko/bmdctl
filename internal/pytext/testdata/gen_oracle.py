# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: MIT
"""Generate oracle.json: what CPython does with the text rules
internal/pytext ports.

    python3 -I gen_oracle.py oracle.json

Needs no bmdwire: these are the interpreter's own rules (str.isspace,
unicodedata.decimal, str.isdigit, str.lower, and the string functions
bmdwire's MIT libraries call). Per-character properties are recorded for
every code point, with the ranges Unicode 15.0 leaves unassigned, which
the Go test skips: Go's unicode tables are newer than CPython 3.12's, so
a character assigned since may differ. String functions are recorded on
a pool of tricky strings and byte strings.
"""
import json
import random
import sys
import unicodedata

OUT = sys.argv[1]
rng = random.Random(0x7E47)

space, decimal, other_digit, lower, unassigned = [], [], [], [], []
start = None
for cp in range(0x110000):
    c = chr(cp)
    if unicodedata.category(c) == 'Cn':
        if start is None:
            start = cp
        continue
    if start is not None:
        unassigned.append([start, cp - 1])
        start = None
    if c.isspace():
        space.append(cp)
    d = unicodedata.decimal(c, None)
    if d is not None:
        decimal.append([cp, d])
    elif c.isdigit():
        other_digit.append(cp)
    if c.lower() != c:
        lower.append([cp, c.lower()])
if start is not None:
    unassigned.append([start, 0x10FFFF])

WS = ''.join(chr(c) for c in space)
DIGITS = ''.join(chr(c) for c, _ in decimal)
pool = [
    '', ' ', '\t\n', 'abc', '  abc  ', 'a b  c', ' a b c\x1cd\x1fe ', '　x　', '​x​',
    '᠎x', 'x\u0085y', 'a b c', 'a\r\nb\rc\nd\r\n', 'a\n', '\n', '\r\n\r\n', 'a\vb\fc', 'x\x1cy\x1dz\x1ew',
    '\x1fend', 'Label: Keyer A', 'Video Inputs', 'MODEL NAME', 'ΑΣ', 'ΑΣ.', 'Σ', 'ΑΣΑ', 'ΑΣ́', 'ὈΔΥΣΣΕΎΣ',
    'İstanbul', 'Ǆǅ', 'ẞ', 'ΣΑΣ ΑΣ', "Α'Σ", 'Ⅷ', 'Ｆｕｌｌ', 'ǈ', '\U0001D400',
    '0', '7', '-3', '+12', '007', '1_000', '1__0', '_1', '1_', '١٢٣', '۱۲', '१२३', '٣.٥', '١_٠', '²', '½', '①',
    ' 42 ', '9' * 25, '-0', '+-1', '0x10', '1e3', '1E-3', '-1.5e+2', '.5', '5.', '.', 'e5', '1e', '1e+',
    '1_0.5', '1._5', '1_.5', '1.5_0', '1e1_0', '1e_1', 'inf', '-inf', '+Infinity', 'INF', 'nan', '-NaN', 'nan(1)',
    'infinit', '  1.25\n', '1,5', '١e٣', '1e400', '-1e400', '1e-400', '0.1', '2.5', '30.9', '-50', 'abc1',
    "it's", 'say "hi"', 'both \'"', 'back\\slash', 'tab\there', 'nl\nhere', 'cr\rhere', '\x00\x07\x7f', '\x80\xa0\xad',
    '͸', '퟿', '', '﻿', '\U0001F600', '\U000E0001', 'é', 'é', '中文',
]
pool += [''.join(rng.choice(WS + 'aZ0_.-+eE ' + DIGITS[:200] + 'ΣΑİ\n\r') for _ in range(rng.randrange(1, 12)))
         for _ in range(300)]

bytes_pool = [b'', b'abc', b'\xff', b'\xc3', b'\xc3\xa9', b'\xc3\x28', b'\xe2\x82', b'\xe2\x82\xac', b'\xed\xa0\x80',
              b'\xf0\x9f\x98\x80', b'\xf0\x9f\x98', b'\xf4\x90\x80\x80', b'\xc0\xaf', b'\xe0\x80\xaf', b'\x80\x80',
              b'a\xffb\xfec', 'Café'.encode(), b'\xf8\x88\x80\x80\x80', b'\xe0\xa0', b'\xef\xbf\xbd']
bytes_pool += [bytes(rng.choice([0x41, 0x80, 0xbf, 0xc2, 0xdf, 0xe0, 0xed, 0xef, 0xf0, 0xf4, 0xff, 0xa0, 0x9f])
                     for _ in range(rng.randrange(1, 8))) for _ in range(200)]


def attempt(f, s):
    try:
        v = f(s)
    except ValueError as e:
        return {'error': str(e)}
    if isinstance(v, float):
        return {'float': repr(v)}
    return {'value': v}


cases = []
for s in pool:
    cases.append({
        's': s,
        'strip': s.strip(), 'split': s.split(), 'splitlines': s.splitlines(), 'lower': s.lower(),
        'repr': repr(s), 'isdecimal': s.isdecimal(), 'isdigit': s.isdigit(),
        'int': attempt(int, s), 'float': attempt(float, s),
    })
decodes = [{'b': b.hex(), 's': b.decode('utf-8', 'replace')} for b in bytes_pool]

with open(OUT, 'w') as f:
    json.dump({'python': sys.version.split()[0], 'unicode': unicodedata.unidata_version,
               'generator': 'internal/pytext/testdata/gen_oracle.py',
               'space': space, 'decimal': decimal, 'other_digit': other_digit, 'lower': lower,
               'unassigned': unassigned, 'cases': cases, 'decodes': decodes},
              f, ensure_ascii=True, separators=(',', ':'))
    f.write('\n')
print(len(cases), 'strings,', len(decodes), 'byte strings,', len(space), 'spaces,', len(decimal), 'decimals,',
      len(other_digit), 'other digits,', len(lower), 'lower mappings,', len(unassigned), 'unassigned ranges')
