# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate send.json: the bytes bmdwire's atemwire sends for each command.

    python3 -I gen_send.py /path/to/bmdwire send.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.4.0; its C
extension is stubbed. Each case calls one atemwire operation (or, for the
fields no operation sets, the Send class itself) on a fake connection and
records what went out: the command's bytes, an error (atemwire raised, in
the operation or in get_command), or nothing (the operation chose not to
send). Alongside it is the Go command that should do the same: its type and
field values, in the operation's units. The Go test in atem/ builds that
command and checks its Encode (or Validate) against every case.

Rates: operations that take "seconds:frames" strings are called with a
connection whose mixerstate is empty, so atemwire uses 25 fps; the Go
command gets the frames parse_rate produced. Values the Go field type can't
hold are not generated: Go's types already refuse them.
"""
import datetime
import json
import math
import random
import sys
import types

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)
stub = types.ModuleType('atemwire.mediaconvert')
stub.rgb_to_atem = stub.atem_to_rgb = stub.rle_encode = None
sys.modules['atemwire.mediaconvert'] = stub

from atemwire.helpers import parse_rate  # noqa: E402
from atemwire.messages import (  # noqa: E402
    audio_legacy, camera_control, color_generator, downstream_keyer,
    fade_to_black, fairlight, file_transfer, hyperdeck, input_video, lock,
    macros, media, multiviewer, recording, streaming, supersource, switching,
    system_info, transition, upstream_keyer,
)

rng = random.Random(1337)
NAN, INF = float('nan'), float('inf')


class FakeConn:
    mixerstate = {}

    def __init__(self):
        self.sent = []

    def send(self, cmd):
        self.sent.append(cmd)


def jsonable(v):
    if isinstance(v, float):
        if math.isnan(v):
            return 'NaN'
        if math.isinf(v):
            return 'Infinity' if v > 0 else '-Infinity'
    if isinstance(v, list):
        return [jsonable(x) for x in v]
    return v


cases = []


def record(go_type, go_fields, label, call):
    conn = FakeConn()
    case = {'type': go_type, 'fields': {k: jsonable(v) for k, v in go_fields.items()}, 'call': label}
    try:
        call(conn)
        if not conn.sent:
            case['result'] = 'none'
        else:
            assert len(conn.sent) == 1, label
            case['result'] = 'bytes'
            case['bytes'] = conn.sent[0].get_command().hex()
    except Exception as e:
        case['result'] = 'error'
        case['error'] = type(e).__name__
    cases.append(case)


# --- value pools -----------------------------------------------------------

U8 = [0, 1, 2, 127, 255]
U16 = [0, 1, 1000, 3010, 32767, 65535]
U32 = [0, 1, 70000, 4294967295]
ME = [0, 1, 3]
BOOL = [False, True]


def rand_floats(lo, hi, n=4):
    return [round(rng.uniform(lo, hi), rng.choice([0, 1, 2, 3, 5, 9])) for _ in range(n)]


SPECIAL = [NAN, INF, -INF]
PERCENT = [-5, 0, 0.04, 0.05, 0.15, 0.25, 0.35, 12.5, 12.35, 33.333, 50, 87.65, 99.95, 100, 100.05, 250, 1e30] + SPECIAL + rand_floats(-10, 110)
UNIT = [-0.1, 0, 0.00005, 0.00015, 0.00025, 0.25, 0.5, 0.99995, 1, 1.5, 1e30] + SPECIAL + rand_floats(-0.2, 1.2)
SIGNED = [-40, -32.769, -32.768, -9, -1.0005, -0.0005, 0, 0.0005, 0.0015, 0.0025, 9, 16, 32.767, 32.768, 65.535] + SPECIAL + rand_floats(-20, 20)
DEG = [-1, 0, 0.05, 0.15, 0.25, 90, 180.45, 359.9, 6553.5, 6553.6] + SPECIAL + rand_floats(-400, 400)
DB = [-INF, -150, -100, -60.004, -20.005, -3.005, -0.005, 0, 0.005, 0.015, 6, 6.005, 10, 20, 25, NAN, INF] + rand_floats(-110, 30)
MS = [0, 0.5, 0.7, 0.005, 0.015, 100, 4000, 21474836.47, 21474836.48, -1] + SPECIAL + rand_floats(0, 4000)
RATIO = [1, 1.2, 1.005, 3, 20, 655.35, 655.36] + SPECIAL + rand_floats(1, 20)
RATES = ['0:00', '0:10', '1:00', '1:12', '2:15', '0:255', '10:05', '30', '255', 'abc', '1:2:3', ' 7 ', '1_0']
RATES16 = RATES + ['20:00', '100:00', '2621:15']


def frames(rate_str):
    return parse_rate(rate_str, 25)


def pick(pool, n=None):
    return pool if n is None else rng.sample(pool, min(n, len(pool)))


# --- operations, one field at a time ----------------------------------------

def single(go_type, op, index_args, go_index, param, go_field, pool, transform=None, check=None):
    """One case per pool value: op(conn, **index_args, param=v)."""
    for v in pool:
        gv = transform(v) if transform else v
        if check is not None and not check(gv):
            continue
        fields = dict(go_index)
        fields[go_field] = gv
        kwargs = dict(index_args)
        kwargs[param] = v
        record(go_type, fields, f'{op.__name__}({kwargs})', lambda c, op=op, kw=kwargs: op(c, **kw))


def fits_u8(v):
    return isinstance(v, int) and 0 <= v <= 255


def fits_u16(v):
    return isinstance(v, int) and 0 <= v <= 65535


# switching
for me in ME:
    single('ProgramInput', switching.set_program, {'me': me}, {'ME': me}, 'source', 'Source', U16)
    single('PreviewInput', switching.set_preview, {'me': me}, {'ME': me}, 'source', 'Source', U16)
    record('Cut', {'ME': me}, f'cut(me={me})', lambda c, me=me: switching.cut(c, me=me))
    record('Auto', {'ME': me}, f'auto(me={me})', lambda c, me=me: switching.auto(c, me=me))
for aux in (0, 5, 23):
    single('AuxSource', switching.set_aux_output, {'aux': aux}, {'Aux': aux}, 'source', 'Source', U16)

# transition
T = transition
for me in (0, 2):
    i, g = {'me': me}, {'ME': me}
    single('TransitionSettings', T.set_transition_style, i, g, 'style', 'Style', [0, 1, 2, 3, 4, 255])
    for bits in range(32):
        layers = dict(background=bool(bits & 1), key1=bool(bits & 2), key2=bool(bits & 4),
                      key3=bool(bits & 8), key4=bool(bits & 16))
        record('TransitionSettings', {'ME': me, 'NextTransition': bits},
               f'set_next_transition_layers({layers}, me={me})',
               lambda c, l=layers, me=me: T.set_next_transition_layers(c, me=me, **l))
    single('MixSettings', T.set_mix_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('DipSettings', T.set_dip_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('DipSettings', T.set_dip_source, i, g, 'source', 'Source', U16)
    single('WipeSettings', T.set_wipe_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('WipeSettings', T.set_wipe_pattern, i, g, 'pattern', 'Pattern', [0, 5, 17, 255])
    single('WipeSettings', T.set_wipe_fill_source, i, g, 'source', 'Source', U16)
    single('WipeSettings', T.set_wipe_flip_flop, i, g, 'flip_flop', 'FlipFlop', BOOL)
    single('WipeSettings', T.set_wipe_reverse, i, g, 'reverse', 'Reverse', BOOL)
    single('WipeSettings', T.set_wipe_position_x, i, g, 'position_x', 'PositionX', UNIT)
    single('WipeSettings', T.set_wipe_position_y, i, g, 'position_y', 'PositionY', UNIT)
    single('WipeSettings', T.set_wipe_softness, i, g, 'softness', 'Softness', PERCENT)
    single('WipeSettings', T.set_wipe_symmetry, i, g, 'symmetry', 'Symmetry', PERCENT)
    single('WipeSettings', T.set_wipe_width, i, g, 'width', 'Width', PERCENT)
    single('DVESettings', T.set_dve_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('DVESettings', T.set_dve_fill_source, i, g, 'source', 'FillSource', U16)
    single('DVESettings', T.set_dve_key_source, i, g, 'source', 'KeySource', U16)
    single('DVESettings', T.set_dve_enable_key, i, g, 'enable_key', 'KeyEnable', BOOL)
    single('DVESettings', T.set_dve_clip, i, g, 'clip', 'KeyClip', PERCENT)
    single('DVESettings', T.set_dve_gain, i, g, 'gain', 'KeyGain', PERCENT)
    single('DVESettings', T.set_dve_pre_multiplied, i, g, 'pre_multiplied', 'KeyPremultiplied', BOOL)
    single('DVESettings', T.set_dve_invert_key, i, g, 'invert_key', 'KeyInvert', BOOL)
    single('DVESettings', T.set_dve_style, i, g, 'style', 'Style', [0, 16, 31, 34, 255])
    single('DVESettings', T.set_dve_reverse, i, g, 'reverse', 'Reverse', BOOL)
    single('DVESettings', T.set_dve_flip_flop, i, g, 'flip_flop', 'FlipFlop', BOOL)
    single('StingerSettings', T.set_stinger_rate, i, g, 'rate_str', 'Rate', RATES16, frames, fits_u16)
    single('StingerSettings', T.set_stinger_mix_rate, i, g, 'rate_str', 'Rate', RATES16, frames, fits_u16)
    single('StingerSettings', T.set_stinger_source, i, g, 'source', 'MediaPlayer', [1, 2, 4, 255])
    single('StingerSettings', T.set_stinger_clip_duration, i, g, 'clip_duration', 'Duration', RATES16, frames, fits_u16)
    single('StingerSettings', T.set_stinger_trigger_point, i, g, 'trigger_point', 'TriggerPoint', RATES16, frames, fits_u16)
    single('StingerSettings', T.set_stinger_pre_roll, i, g, 'pre_roll', 'Preroll', RATES16, frames, fits_u16)
    single('StingerSettings', T.set_stinger_clip, i, g, 'clip', 'KeyClip', PERCENT + [6553.5, 6553.6, -0.04, -0.05, -0.06])
    single('StingerSettings', T.set_stinger_gain, i, g, 'gain', 'KeyGain', PERCENT + [6553.5, 6553.6])
    single('StingerSettings', T.set_stinger_pre_multiplied, i, g, 'pre_multiplied', 'KeyPremultiplied', BOOL)
    single('StingerSettings', T.set_stinger_invert_key, i, g, 'invert_key', 'KeyInvert', BOOL)
    single('TransitionPreview', T.set_transition_preview, i, g, 'enabled', 'Enabled', BOOL)
    single('TransitionPosition', T.set_transition_position, i, g, 'position', 'Position', [-5, 0, 1, 5000, 9999, 10000, 10001, 99999])
# DVE logo rate has no operation: the Send class takes it in wire units.
for v in U8:
    record('DVESettings', {'ME': 1, 'LogoRate': v}, f'DveSettingsCommand(index=1, logo_rate={v})',
           lambda c, v=v: c.send(T.DveSettingsCommand(index=1, logo_rate=v)))

# upstream keyer
U = upstream_keyer
for me, k in ((0, 0), (1, 3)):
    i, g = {'me': me, 'keyer': k}, {'ME': me, 'Keyer': k}
    single('KeyOnAir', U.set_usk_on_air, i, g, 'enabled', 'Enabled', BOOL)
    single('KeyType', U.set_usk_type, i, g, 'key_type', 'Type', [0, 1, 2, 3, 255])
    single('KeyType', U.set_usk_fly_enabled, i, g, 'enabled', 'FlyEnabled', BOOL)
    single('KeyFill', U.set_usk_fill_source, i, g, 'source', 'Source', U16)
    single('KeyCut', U.set_usk_key_source, i, g, 'source', 'Source', U16)
    single('KeyPropertiesLuma', U.set_usk_luma_clip, i, g, 'clip', 'Clip', PERCENT)
    single('KeyPropertiesLuma', U.set_usk_luma_gain, i, g, 'gain', 'Gain', PERCENT)
    single('KeyPropertiesLuma', U.set_usk_luma_invert, i, g, 'invert', 'InvertKey', BOOL)
    single('KeyPropertiesLuma', U.set_usk_luma_pre_multiplied, i, g, 'pre_multiplied', 'Premultiplied', BOOL)
    for _ in range(12):
        kw = {}
        f = dict(g)
        for p, gf, pool in (('clip', 'Clip', PERCENT), ('gain', 'Gain', PERCENT),
                            ('pre_multiplied', 'Premultiplied', BOOL), ('invert', 'InvertKey', BOOL)):
            if rng.random() < .6:
                kw[p] = f[gf] = rng.choice(pool)
        record('KeyPropertiesLuma', f, f'configure_usk_luma({i}, {kw})',
               lambda c, i=i, kw=kw: U.configure_usk_luma(c, **i, **kw))
    for op, gf, pool in ((U.set_usk_chroma_foreground, 'Foreground', UNIT), (U.set_usk_chroma_background, 'Background', UNIT),
                         (U.set_usk_chroma_key_edge, 'KeyEdge', UNIT), (U.set_usk_chroma_spill, 'Spill', UNIT),
                         (U.set_usk_chroma_flare_suppression, 'Flare', UNIT), (U.set_usk_chroma_brightness, 'Brightness', SIGNED),
                         (U.set_usk_chroma_contrast, 'Contrast', SIGNED), (U.set_usk_chroma_saturation, 'Saturation', UNIT + [2, 65.535, 65.536]),
                         (U.set_usk_chroma_red, 'Red', SIGNED), (U.set_usk_chroma_green, 'Green', SIGNED),
                         (U.set_usk_chroma_blue, 'Blue', SIGNED)):
        param = op.__code__.co_varnames[2]
        single('KeyPropertiesAdvancedChroma', op, i, g, param, gf, pool + [-0.0005, 0.0025])
    single('KeyPropertiesAdvancedChromaColorpicker', U.set_usk_chroma_sample, i, g, 'enabled', 'Cursor', BOOL)
    single('KeyPropertiesAdvancedChromaColorpicker', U.set_usk_chroma_preview, i, g, 'preview', 'Preview', BOOL)
    single('KeyPropertiesAdvancedChromaColorpicker', U.set_usk_chroma_sample_size, i, g, 'size', 'Size', UNIT + [0.062, 0.06, 0.9925, 0.993])
    for x, y in [(0, 0), (0.5, 0.5), (1, 1), (0.25, 0.75), (1.524, 0.5), (1.6, 0.5), (0.5, -1.5), (NAN, 0.5), (0.5, INF)] + [(rng.random(), rng.random()) for _ in range(4)]:
        record('KeyPropertiesAdvancedChromaColorpicker', dict(g, X=jsonable(x), Y=jsonable(y)),
               f'set_usk_chroma_sample_position({i}, x={x}, y={y})',
               lambda c, i=i, x=x, y=y: U.set_usk_chroma_sample_position(c, **i, x=x, y=y))
    for yy, cb, cr in [(0, 0, 0), (0.5, -0.5, 0.5), (1, 1, -1), (6.5535, 3.2767, -3.2768), (6.5536, 0, 0), (0, 3.2768, 0), (NAN, 0, 0)] + [(rng.random(), rng.uniform(-1, 1), rng.uniform(-1, 1)) for _ in range(4)]:
        record('KeyPropertiesAdvancedChromaColorpicker', dict(g, SampleY=jsonable(yy), SampleCb=jsonable(cb), SampleCr=jsonable(cr)),
               f'set_usk_chroma_sampled_color({i}, y={yy}, cb={cb}, cr={cr})',
               lambda c, i=i, yy=yy, cb=cb, cr=cr: U.set_usk_chroma_sampled_color(c, **i, y=yy, cb=cb, cr=cr))
    single('KeyPropertiesDVE', U.set_usk_dve_position_x, i, g, 'position_x', 'PosX', SIGNED + [1e7, 2147483.647, 2147483.648, -2147483.649, 1e30])
    single('KeyPropertiesDVE', U.set_usk_dve_position_y, i, g, 'position_y', 'PosY', SIGNED + [1e30, -1e30])
    single('KeyPropertiesDVE', U.set_usk_dve_size_x, i, g, 'size_x', 'SizeX', UNIT + [2, 2.0005, 1.0005, 3])
    single('KeyPropertiesDVE', U.set_usk_dve_size_y, i, g, 'size_y', 'SizeY', UNIT + [2, 3])
    single('KeyPropertiesDVE', U.set_usk_dve_rotation, i, g, 'rotation', 'Rotation', DEG + [214748364.75, 214748364.85])
    single('KeyPropertiesDVE', U.set_usk_dve_border_enabled, i, g, 'enabled', 'BorderEnabled', BOOL)
    single('KeyPropertiesDVE', U.set_usk_dve_border_hue, i, g, 'hue', 'BorderHue', DEG)
    single('KeyPropertiesDVE', U.set_usk_dve_border_saturation, i, g, 'saturation', 'BorderSaturation', PERCENT)
    single('KeyPropertiesDVE', U.set_usk_dve_border_luma, i, g, 'luma', 'BorderLuma', PERCENT)
    single('KeyPropertiesDVE', U.set_usk_dve_border_opacity, i, g, 'opacity', 'BorderOpacity', [0, 0.5, 1.5, 2.5, 100, 255, 255.5, 256, -0.5, -0.6] + SPECIAL)
    single('KeyPropertiesDVE', U.set_usk_dve_border_outer_width, i, g, 'outer_width', 'OuterWidth', [0, 0.005, 0.015, 16, 655.35, 655.36, -0.005] + SPECIAL)
    single('KeyPropertiesDVE', U.set_usk_dve_border_inner_width, i, g, 'inner_width', 'InnerWidth', [0, 0.025, 16, 655.36] + SPECIAL)
    for op, gf in ((U.set_usk_dve_border_outer_softness, 'OuterSoftness'), (U.set_usk_dve_border_inner_softness, 'InnerSoftness'),
                   (U.set_usk_dve_border_bevel_softness, 'BevelSoftness'), (U.set_usk_dve_border_bevel_position, 'BevelPosition')):
        single('KeyPropertiesDVE', op, i, g, op.__code__.co_varnames[2], gf, [0, 0.5, 1.5, 50, 100, 255, 256, -1] + SPECIAL)
    single('KeyPropertiesDVE', U.set_usk_dve_masked, i, g, 'masked', 'MaskEnabled', BOOL)
    for op, gf in ((U.set_usk_dve_top, 'MaskTop'), (U.set_usk_dve_bottom, 'MaskBottom'),
                   (U.set_usk_dve_left, 'MaskLeft'), (U.set_usk_dve_right, 'MaskRight')):
        single('KeyPropertiesDVE', op, i, g, op.__code__.co_varnames[2], gf, SIGNED)
    single('KeyPropertiesDVE', U.set_usk_dve_light_direction, i, g, 'direction', 'Angle', DEG)
    single('KeyPropertiesDVE', U.set_usk_dve_light_altitude, i, g, 'altitude', 'Altitude', [10, 10.5, 11.5, 100, 255.4, 255.5, 256, -1] + SPECIAL)
    single('KeyPropertiesDVE', U.set_usk_dve_shadow, i, g, 'shadow', 'ShadowEnabled', BOOL)
    single('KeyPropertiesDVE', U.set_usk_dve_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('KeyPropertiesPattern', U.set_usk_pattern_style, i, g, 'pattern', 'Pattern', [0, 7, 17, 255])
    single('KeyPropertiesPattern', U.set_usk_pattern_size, i, g, 'size', 'Size', PERCENT)
    single('KeyPropertiesPattern', U.set_usk_pattern_symmetry, i, g, 'symmetry', 'Symmetry', PERCENT)
    single('KeyPropertiesPattern', U.set_usk_pattern_softness, i, g, 'softness', 'Softness', PERCENT)
    single('KeyPropertiesPattern', U.set_usk_pattern_invert, i, g, 'invert', 'InvertPattern', BOOL)
    single('KeyPropertiesPattern', U.set_usk_pattern_position_x, i, g, 'position_x', 'PositionX', UNIT)
    single('KeyPropertiesPattern', U.set_usk_pattern_position_y, i, g, 'position_y', 'PositionY', UNIT)
    single('KeyPropertiesMask', U.set_usk_mask_enabled, i, g, 'enabled', 'Enabled', BOOL)
    for op, gf in ((U.set_usk_mask_top, 'Top'), (U.set_usk_mask_bottom, 'Bottom'),
                   (U.set_usk_mask_left, 'Left'), (U.set_usk_mask_right, 'Right')):
        single('KeyPropertiesMask', op, i, g, op.__code__.co_varnames[2], gf, SIGNED)
    for name, kf in (('a', 1), ('b', 2), ('full', 3), ('runToInfinite', 4)):
        record('KeyerKeyframeSet', dict(g, Keyframe=kf), f'set_keyer_fly_keyframe({i}, {name!r})',
               lambda c, i=i, n=name: U.set_keyer_fly_keyframe(c, **i, keyframe=n))
    for name, kf in (('a', 1), ('b', 2), ('full', 3)):
        record('KeyerKeyframeRun', dict(g, RunTo=kf), f'run_flying_key_keyframe({i}, {name!r})',
               lambda c, i=i, n=name: U.run_flying_key_keyframe(c, **i, keyframe=n))
    for d in (0, 1, 5, 9, 255):
        record('KeyerKeyframeRun', dict(g, RunTo=4, Direction=d), f'run_flying_key_infinite_direction({i}, {d})',
               lambda c, i=i, d=d: U.run_flying_key_infinite_direction(c, **i, direction=d))
# The DVE bevel type has no operation.
for v in (0, 1, 3, 255):
    record('KeyPropertiesDVE', {'ME': 0, 'Keyer': 1, 'BorderBevelEnabled': v},
           f'KeyPropertiesDveCommand(0, 1, border_bevel_enabled={v})',
           lambda c, v=v: c.send(U.KeyPropertiesDveCommand(0, 1, border_bevel_enabled=v)))

# downstream keyer
D = downstream_keyer
for dsk in (0, 1):
    i, g = {'dsk_idx': dsk}, {'DSK': dsk}
    single('DkeyOnair', D.set_dsk_on_air, i, g, 'enabled', 'OnAir', BOOL)
    single('DkeyTie', D.set_dsk_tie, i, g, 'tie', 'Tie', BOOL)
    record('DkeyAuto', g, f'dsk_auto({i})', lambda c, i=i: D.dsk_auto(c, **i))
    single('DkeyRate', D.set_dsk_rate, i, g, 'rate_str', 'Rate', RATES, frames, fits_u8)
    single('DkeySetFill', D.set_dsk_fill_source, i, g, 'source', 'Source', U16)
    single('DkeySetKey', D.set_dsk_key_source, i, g, 'source', 'Source', U16)
    single('DkeyMask', D.set_dsk_mask_enabled, i, g, 'enabled', 'Enabled', BOOL)
    for op, gf in ((D.set_dsk_mask_top, 'Top'), (D.set_dsk_mask_bottom, 'Bottom'),
                   (D.set_dsk_mask_left, 'Left'), (D.set_dsk_mask_right, 'Right')):
        single('DkeyMask', op, i, g, op.__code__.co_varnames[1], gf, SIGNED)
    single('DkeyGain', D.set_dsk_pre_multiplied, i, g, 'pre_multiplied', 'Premultiplied', BOOL)
    single('DkeyGain', D.set_dsk_clip, i, g, 'clip', 'Clip', PERCENT)
    single('DkeyGain', D.set_dsk_gain, i, g, 'gain', 'Gain', PERCENT)
    single('DkeyGain', D.set_dsk_invert_key, i, g, 'invert_key', 'Invert', BOOL)
    for _ in range(12):
        kw, f = {}, dict(g)
        for p, gf, pool in (('clip', 'Clip', PERCENT), ('gain', 'Gain', PERCENT),
                            ('pre_multiplied', 'Premultiplied', BOOL), ('invert', 'Invert', BOOL)):
            if rng.random() < .6:
                kw[p] = f[gf] = rng.choice(pool)
        record('DkeyGain', f, f'configure_dsk_gain({kw}, {i})', lambda c, i=i, kw=kw: D.configure_dsk_gain(c, **i, **kw))

# fade to black
for me in ME:
    record('FadeToBlack', {'ME': me}, f'fade_to_black(me={me})', lambda c, me=me: fade_to_black.fade_to_black(c, me=me))
    single('FadeToBlackConfig', fade_to_black.set_ftb_rate, {'me': me}, {'ME': me}, 'rate_str', 'Frames', RATES, frames, fits_u8)
    for disabled in BOOL:
        record('FadeToBlackEnable', {'Enabled': not disabled}, f'set_ftb_disabled({disabled}, me={me})',
               lambda c, d=disabled, me=me: fade_to_black.set_ftb_disabled(c, d, me=me))

# colour generators
for gen in (0, 1):
    i, g = {'generator': gen}, {'Generator': gen}
    single('ColorGenerator', color_generator.set_color_generator_hue, i, g, 'hue', 'Hue', DEG)
    single('ColorGenerator', color_generator.set_color_generator_saturation, i, g, 'saturation', 'Saturation', PERCENT)
    single('ColorGenerator', color_generator.set_color_generator_luma, i, g, 'luma', 'Luma', PERCENT)

# media
for slot in (0, 1, 19, 255):
    record('ClearStill', {'Slot': slot}, f'clear_still({slot})', lambda c, s=slot: media.clear_still(c, s))
record('CaptureStill', {}, 'capture_still()', lambda c: media.capture_still(c))
for player in (0, 1, 3):
    for slot in (0, 5, 255):
        record('MediaplayerSelect', {'Player': player, 'Still': slot}, f'set_media_player_still({player}, {slot})',
               lambda c, p=player, s=slot: media.set_media_player_still(c, p, s))
        record('MediaplayerSelect', {'Player': player, 'Clip': slot}, f'set_media_player_clip({player}, {slot})',
               lambda c, p=player, s=slot: media.set_media_player_clip(c, p, s))

# macros
for n in (1, 2, 100, 65535):
    record('MacroAction', {'Slot': n - 1, 'Action': 0}, f'execute_macro({n})', lambda c, n=n: macros.execute_macro(c, n))
for s in (0, 1, 99, 65534):
    record('MacroAction', {'Slot': s, 'Action': 0}, f'macro_run({s})', lambda c, s=s: macros.macro_run(c, s))
record('MacroAction', {'Slot': 65535, 'Action': 1}, 'macro_stop()', lambda c: macros.macro_stop(c))
record('MacroAction', {'Slot': 65535, 'Action': 2}, 'macro_stop_recording()', lambda c: macros.macro_stop_recording(c))
for a in (3, 4, 5):
    for s in (0, 7, 65535):
        record('MacroAction', {'Slot': s, 'Action': a}, f'MacroActionCommand(index={s}, action={a})',
               lambda c, s=s, a=a: c.send(macros.MacroActionCommand(index=s, action=a)))
for s, name, desc in ((0, 'Open', ''), (5, 'A', 'B'), (99, 'Show open', 'with a description'), (1, '', ''),
                      (2, 'Café ✓', 'ünïcode'), (3, 'abc', 'defg'), (4, 'x' * 300, 'y' * 7)):
    record('MacroRecord', {'Slot': s, 'Name': name, 'Description': desc}, f'macro_start_recording({s}, {name!r}, {desc!r})',
           lambda c, s=s, n=name, d=desc: macros.macro_start_recording(c, s, n, d))
for f in (0, 1, 25, 4294967295):
    record('MacroSleep', {'Frames': f}, f'macro_sleep({f})', lambda c, f=f: macros.macro_sleep(c, f))

# fairlight
F = fairlight
MIX = [('Off', 1), ('On', 2), ('AudioFollowVideo', 4), (1, 1), (2, 2), (4, 4), (0, 0), (255, 255)]
SHAPES = [(k, v) for k, v in F.FAIRLIGHT_EQ_SHAPES.items()] + [(0, 0), (255, 255)]
RANGES = [(k, v) for k, v in F.FAIRLIGHT_EQ_FREQ_RANGES.items()] + [(0, 0), (3, 3)]
for _ in range(40):
    kw, f = {}, {}
    for p, gf, pool in (('eq_enable', 'EQEnable', BOOL), ('eq_gain_db', 'EQGain', DB),
                        ('dynamics_makeup_db', 'DynamicsGain', DB), ('volume_db', 'Volume', DB), ('afv', 'AFV', BOOL)):
        if rng.random() < .5:
            kw[p] = f[gf] = rng.choice(pool)
    record('FairlightMasterProperties', f, f'set_fairlight_master({kw})', lambda c, kw=kw: F.set_fairlight_master(c, **kw))
for p, gf, pool in (('eq_gain_db', 'EQGain', DB), ('dynamics_makeup_db', 'DynamicsGain', DB), ('volume_db', 'Volume', DB)):
    for v in pool:
        record('FairlightMasterProperties', {gf: v}, f'set_fairlight_master({p}={v})', lambda c, p=p, v=v: F.set_fairlight_master(c, **{p: v}))
STRIPS = [(1, -1), (1301, -1), (1301, 0), (1301, 1), (2001, 5), (4, -2), (4, 127), (4, -128)]
for source, channel in STRIPS:
    base = {'Source': source, 'Channel': channel}
    for p, gf, pool in (('input_gain_db', 'Gain', DB), ('eq_gain_db', 'EQGain', DB), ('dynamics_makeup_db', 'DynamicsGain', DB),
                        ('pan', 'Balance', [-150, -100, -0.005, 0, 0.005, 50.5, 100, 101] + SPECIAL), ('fader_gain_db', 'Volume', DB)):
        for v in pick(pool, 6):
            record('FairlightStripProperties', dict(base, **{gf: jsonable(v)}), f'set_fairlight_strip({source}, {channel}, {p}={v})',
                   lambda c, s=source, ch=channel, p=p, v=v: F.set_fairlight_strip(c, s, ch, **{p: v}))
    for name, val in MIX:
        record('FairlightStripProperties', dict(base, State=val), f'set_fairlight_strip({source}, {channel}, mix_option={name!r})',
               lambda c, s=source, ch=channel, n=name: F.set_fairlight_strip(c, s, ch, mix_option=n))
    for d in (0, 1, 8, 255):
        record('FairlightStripProperties', dict(base, Delay=d), f'set_fairlight_strip({source}, {channel}, delay_frames={d})',
               lambda c, s=source, ch=channel, d=d: F.set_fairlight_strip(c, s, ch, delay_frames=d))
    for e in BOOL:
        record('FairlightStripProperties', dict(base, EQEnable=e), f'set_fairlight_strip({source}, {channel}, eq_enable={e})',
               lambda c, s=source, ch=channel, e=e: F.set_fairlight_strip(c, s, ch, eq_enable=e))
    for band in (0, 5):
        b = dict(base, Band=band)
        for e in BOOL:
            record('FairlightEQBandProperties', dict(b, Enabled=e), f'set_fairlight_eq_band({source}, {channel}, {band}, enabled={e})',
                   lambda c, s=source, ch=channel, bd=band, e=e: F.set_fairlight_eq_band(c, s, ch, bd, enabled=e))
        for name, val in SHAPES:
            record('FairlightEQBandProperties', dict(b, Shape=val), f'set_fairlight_eq_band(..., shape={name!r})',
                   lambda c, s=source, ch=channel, bd=band, n=name: F.set_fairlight_eq_band(c, s, ch, bd, shape=n))
        for name, val in RANGES:
            record('FairlightEQBandProperties', dict(b, FrequencyRange=val), f'set_fairlight_eq_band(..., frequency_range={name!r})',
                   lambda c, s=source, ch=channel, bd=band, n=name: F.set_fairlight_eq_band(c, s, ch, bd, frequency_range=n))
        for hz in (0, 29, 30, 1000, 21700, 21701, 100000, -5):
            record('FairlightEQBandProperties', dict(b, Frequency=hz), f'set_fairlight_eq_band(..., frequency_hz={hz})',
                   lambda c, s=source, ch=channel, bd=band, hz=hz: F.set_fairlight_eq_band(c, s, ch, bd, frequency_hz=hz))
        for v in pick(DB, 8):
            record('FairlightEQBandProperties', dict(b, Gain=jsonable(v)), f'set_fairlight_eq_band(..., gain_db={v})',
                   lambda c, s=source, ch=channel, bd=band, v=v: F.set_fairlight_eq_band(c, s, ch, bd, gain_db=v))
        for q in [0, 0.3, 0.295, 0.305, 0.71, 2.5, 10.3, 10.305, 20] + SPECIAL:
            record('FairlightEQBandProperties', dict(b, Q=jsonable(q)), f'set_fairlight_eq_band(..., q={q})',
                   lambda c, s=source, ch=channel, bd=band, q=q: F.set_fairlight_eq_band(c, s, ch, bd, q=q))
    for e in BOOL:
        for go_type, op in (('FairlightCompressorProperties', F.set_fairlight_compressor),
                            ('FairlightLimiterProperties', F.set_fairlight_limiter),
                            ('FairlightExpanderProperties', F.set_fairlight_expander)):
            record(go_type, dict(base, Enabled=e), f'{op.__name__}({source}, {channel}, enabled={e})',
                   lambda c, op=op, s=source, ch=channel, e=e: op(c, s, ch, enabled=e))
    for p, gf, pool in (('threshold_db', 'Threshold', DB), ('ratio', 'Ratio', RATIO), ('attack_ms', 'Attack', MS),
                        ('hold_ms', 'Hold', MS), ('release_ms', 'Release', MS)):
        for v in pick(pool, 5):
            record('FairlightCompressorProperties', dict(base, **{gf: jsonable(v)}), f'set_fairlight_compressor({source}, {channel}, {p}={v})',
                   lambda c, s=source, ch=channel, p=p, v=v: F.set_fairlight_compressor(c, s, ch, **{p: v}))
            if p != 'ratio':
                record('FairlightLimiterProperties', dict(base, **{gf: jsonable(v)}), f'set_fairlight_limiter({source}, {channel}, {p}={v})',
                       lambda c, s=source, ch=channel, p=p, v=v: F.set_fairlight_limiter(c, s, ch, **{p: v}))
    for p, gf, pool in (('threshold_db', 'Threshold', DB), ('range_db', 'Range', [0, 10.005, 60, 655.35, 655.36, -1] + SPECIAL),
                        ('ratio', 'Ratio', RATIO), ('attack_ms', 'Attack', MS), ('hold_ms', 'Hold', MS), ('release_ms', 'Release', MS)):
        for v in pick(pool, 5):
            record('FairlightExpanderProperties', dict(base, **{gf: jsonable(v)}), f'set_fairlight_expander({source}, {channel}, {p}={v})',
                   lambda c, s=source, ch=channel, p=p, v=v: F.set_fairlight_expander(c, s, ch, **{p: v}))
    for name, val in (('gate', 1), ('expander', 0), ('GATE', 1), (0, 0), (1, 1), (2, 1)):
        record('FairlightExpanderProperties', dict(base, Mode=val), f'set_fairlight_expander({source}, {channel}, mode={name!r})',
               lambda c, s=source, ch=channel, n=name: F.set_fairlight_expander(c, s, ch, mode=n))
for band in (0, 3, 5):
    b = {'Band': band}
    for e in BOOL:
        record('FairlightMasterEQBandProperties', dict(b, Enabled=e), f'set_fairlight_master_eq_band({band}, enabled={e})',
               lambda c, bd=band, e=e: F.set_fairlight_master_eq_band(c, bd, enabled=e))
    for name, val in SHAPES:
        record('FairlightMasterEQBandProperties', dict(b, Shape=val), f'set_fairlight_master_eq_band({band}, shape={name!r})',
               lambda c, bd=band, n=name: F.set_fairlight_master_eq_band(c, bd, shape=n))
    for name, val in RANGES:
        record('FairlightMasterEQBandProperties', dict(b, FrequencyRange=val), f'set_fairlight_master_eq_band({band}, frequency_range={name!r})',
               lambda c, bd=band, n=name: F.set_fairlight_master_eq_band(c, bd, frequency_range=n))
    for hz in (0, 30, 12900, 21700, 21701, -1):
        record('FairlightMasterEQBandProperties', dict(b, Frequency=hz), f'set_fairlight_master_eq_band({band}, frequency_hz={hz})',
               lambda c, bd=band, hz=hz: F.set_fairlight_master_eq_band(c, bd, frequency_hz=hz))
    for v in pick(DB, 8):
        record('FairlightMasterEQBandProperties', dict(b, Gain=jsonable(v)), f'set_fairlight_master_eq_band({band}, gain_db={v})',
               lambda c, bd=band, v=v: F.set_fairlight_master_eq_band(c, bd, gain_db=v))
    for q in [0.3, 0.71, 10.3, 11] + SPECIAL:
        record('FairlightMasterEQBandProperties', dict(b, Q=jsonable(q)), f'set_fairlight_master_eq_band({band}, q={q})',
               lambda c, bd=band, q=q: F.set_fairlight_master_eq_band(c, bd, q=q))
for e in BOOL:
    record('FairlightMasterCompressorProperties', {'Enabled': e}, f'set_fairlight_master_compressor(enabled={e})',
           lambda c, e=e: F.set_fairlight_master_compressor(c, enabled=e))
    record('FairlightMasterLimiterProperties', {'Enabled': e}, f'set_fairlight_master_limiter(enabled={e})',
           lambda c, e=e: F.set_fairlight_master_limiter(c, enabled=e))
for p, gf, pool in (('threshold_db', 'Threshold', DB), ('ratio', 'Ratio', RATIO), ('attack_ms', 'Attack', MS),
                    ('hold_ms', 'Hold', MS), ('release_ms', 'Release', MS)):
    for v in pool:
        record('FairlightMasterCompressorProperties', {gf: jsonable(v)}, f'set_fairlight_master_compressor({p}={v})',
               lambda c, p=p, v=v: F.set_fairlight_master_compressor(c, **{p: v}))
        if p != 'ratio':
            record('FairlightMasterLimiterProperties', {gf: jsonable(v)}, f'set_fairlight_master_limiter({p}={v})',
                   lambda c, p=p, v=v: F.set_fairlight_master_limiter(c, **{p: v}))
for e in BOOL:
    record('SendFairlightLevels', {'Enable': e}, f'enable_fairlight_levels({e})', lambda c, e=e: F.enable_fairlight_levels(c, e))

# classic audio
A = audio_legacy
for source in (1, 1301, 65535):
    for _ in range(10):
        kw, f = {}, {'Source': source}
        for p, gf, pool in (('balance', 'Balance', [-10000, -1, 0, 10000, 32767, -32768]), ('volume', 'Volume', [0, 32768, 65381, 65535]),
                            ('on', 'On', BOOL), ('afv', 'AFV', BOOL)):
            if rng.random() < .5:
                kw[p] = f[gf] = rng.choice(pool)
        record('AudioInput', f, f'set_audio_input({source}, {kw})', lambda c, s=source, kw=kw: A.set_audio_input(c, s, **kw))
for _ in range(10):
    kw, f = {}, {}
    for p, gf, pool in (('volume', 'Volume', [0, 65381, 65535]), ('afv', 'AFV', BOOL)):
        if rng.random() < .6:
            kw[p] = f[gf] = rng.choice(pool)
    record('AudioMasterProperties', f, f'set_audio_master({kw})', lambda c, kw=kw: A.set_audio_master(c, **kw))
for _ in range(16):
    kw, f = {}, {}
    for p, gf, pool in (('enabled', 'Enabled', BOOL), ('volume', 'Volume', U16), ('mute', 'Mute', BOOL), ('solo', 'Solo', BOOL),
                        ('solo_source', 'SoloSource', U16), ('dim', 'Dim', BOOL), ('dim_volume', 'DimVolume', U16)):
        if rng.random() < .5:
            kw[p] = f[gf] = rng.choice(pool)
    record('AudioMonitorProperties', f, f'set_audio_monitor({kw})', lambda c, kw=kw: A.set_audio_monitor(c, **kw))
for e in BOOL:
    record('SendAudioLevels', {'Enable': e}, f'enable_audio_levels({e})', lambda c, e=e: A.enable_audio_levels(c, e))

# camera control: Data elements are tagged with their Go type.
CC = [(0, [True, False, 1, 0]), (1, [-3, 7, -128, 127]), (1, [128]), (1, [0.5]), (2, [-1000, 1000, 32767]), (2, [40000]),
      (3, [100000, -1]), (4, [1, -(2 ** 63), 2 ** 63 - 1]), (5, ['ab', 'cdef']), (5, ['']), (128, [0.5, -1.0, 15.9995, -16]),
      (128, [16]), (128, [1, True]), (128, [NAN]), (6, [1]), (0, []), (3, []), (None, None), (0, None), (128, None)]


def tag(v):
    if isinstance(v, bool):
        return {'bool': v}
    if isinstance(v, int):
        return {'int': v}
    if isinstance(v, float):
        return {'float': jsonable(v)}
    return {'str': v}


for dest, cat, par, rel in ((2, 8, 1, False), (255, 0, 0, True), (1, 4, 2, False)):
    for dt, data in CC:
        f = {'Destination': dest, 'Category': cat, 'Parameter': par, 'Relative': rel,
             'Datatype': 0 if dt is None else dt, 'Data': None if data is None else [tag(v) for v in data]}
        record('CameraControl', f, f'camera_control({dest}, {cat}, {par}, {rel}, {dt}, {data})',
               lambda c, d=dest, ca=cat, p=par, r=rel, dt=dt, data=data: camera_control.camera_control(c, d, ca, p, r, dt, data))

# hyperdeck
for slot in (0, 4, 9, 65535):
    for kw, f in (({'network_address': '192.0.2.10'}, {'NetworkAddress': '192.0.2.10'}),
                  ({'network_address': '0.0.0.0'}, {'NetworkAddress': '0.0.0.0'}),
                  ({'switcher_input': 5}, {'SwitcherInput': 5}),
                  ({'switcher_input': 0}, {'SwitcherInput': 0}),
                  ({'network_address': '10.1.2.3', 'switcher_input': 65535}, {'NetworkAddress': '10.1.2.3', 'SwitcherInput': 65535}),
                  ({}, {}),
                  ({'network_address': 'not an ip'}, {'NetworkAddress': 'not an ip'})):
        f = dict(f, Slot=slot)
        record('HyperdeckSettings', f, f'set_hyperdeck_settings(slot={slot}, {kw})',
               lambda c, s=slot, kw=kw: hyperdeck.set_hyperdeck_settings(c, slot=s, **kw))

# inputs and video mode
for source in (1, 1000, 65535):
    for kw, f in (({'long_name': 'Camera 1'}, {'LongName': 'Camera 1'}), ({'short_name': 'CAM1'}, {'ShortName': 'CAM1'}),
                  ({'long_name': '', 'short_name': ''}, {'LongName': '', 'ShortName': ''}),
                  ({'long_name': 'An overly long name for an input', 'short_name': 'TOOLONG'},
                   {'LongName': 'An overly long name for an input', 'ShortName': 'TOOLONG'}),
                  ({'long_name': 'Caméra ünë ✓✓✓✓✓', 'short_name': 'Cé✓'}, {'LongName': 'Caméra ünë ✓✓✓✓✓', 'ShortName': 'Cé✓'}),
                  ({}, {})):
        f = dict(f, Source=source)
        record('InputProperties', f, f'set_input_label({source}, {kw})', lambda c, s=source, kw=kw: input_video.set_input_label(c, s, **kw))
for pt in (0, 1, 16, 65535):
    record('InputProperties', {'Source': 3, 'PortType': pt}, f'InputPropertiesCommand(3, port_type={pt})',
           lambda c, pt=pt: c.send(input_video.InputPropertiesCommand(3, port_type=pt)))
for name, mode in list(input_video.VIDEO_MODE_NAMES.items()) + [(m, m) for m in (0, 12, 29, 30, 255)]:
    record('VideoMode', {'Mode': mode}, f'set_video_mode({name!r})', lambda c, n=name: input_video.set_video_mode(c, n))

# multiviewer, supersource
for mv in (0, 1):
    for win, src in ((0, 1), (9, 3010), (15, 65535)):
        record('MultiviewInput', {'Multiviewer': mv, 'Window': win, 'Source': src}, f'set_multiviewer_input({win}, {src}, index={mv})',
               lambda c, w=win, s=src, mv=mv: multiviewer.set_multiviewer_input(c, w, s, index=mv))
    for layout in (0, 3, 15, 255):
        record('MultiviewProperties', {'Multiviewer': mv, 'Layout': layout}, f'set_multiviewer_layout({layout}, index={mv})',
               lambda c, l=layout, mv=mv: multiviewer.set_multiviewer_layout(c, l, index=mv))
    for swap in BOOL:
        record('MultiviewProperties', {'Multiviewer': mv, 'Swap': swap}, f'set_multiviewer_swap({swap}, index={mv})',
               lambda c, s=swap, mv=mv: multiviewer.set_multiviewer_swap(c, s, index=mv))
SS_BOX = [('enabled', 'Enabled', BOOL), ('source', 'Source', U16), ('x', 'X', [-4800, 0, 4800, -32768, 32767]),
          ('y', 'Y', [-3400, 3400]), ('size', 'Size', [70, 1000, 65535]), ('masked', 'Masked', BOOL),
          ('top', 'Top', [0, 18000]), ('bottom', 'Bottom', [0, 18000]), ('left', 'Left', [0, 32000]), ('right', 'Right', [0, 32000, 65535])]
for ss in (0, 1):
    for box in (0, 3):
        for _ in range(8):
            kw, f = {}, {'SuperSource': ss, 'Box': box}
            for p, gf, pool in SS_BOX:
                if rng.random() < .4:
                    kw[p] = f[gf] = rng.choice(pool)
            record('SupersourceBoxProperties', f, f'set_supersource_box({box}, index={ss}, {kw})',
                   lambda c, b=box, ss=ss, kw=kw: supersource.set_supersource_box(c, b, index=ss, **kw))
    for _ in range(10):
        kw, f = {}, {'SuperSource': ss}
        for p, gf, pool in (('fill_source', 'FillSource', U16), ('key_source', 'KeySource', U16), ('layer', 'Layer', [0, 1, 255]),
                            ('premultiplied', 'Premultiplied', BOOL), ('clip', 'Clip', [0, 500, 1000]), ('gain', 'Gain', [0, 1000, 65535]),
                            ('invert', 'Invert', BOOL)):
            if rng.random() < .4:
                kw[p] = f[gf] = rng.choice(pool)
        record('SupersourceProperties', f, f'set_supersource_properties(index={ss}, {kw})',
               lambda c, ss=ss, kw=kw: supersource.set_supersource_properties(c, index=ss, **kw))

# recording, streaming
for kw, f in (({'filename': 'Show 2026-09-11'}, {'Filename': 'Show 2026-09-11'}), ({'filename': 'x' * 200}, {'Filename': 'x' * 200}),
              ({'disk1': 0x12345678, 'disk2': 0}, {'Disk1': 0x12345678, 'Disk2': 0}), ({'record_in_camera': True}, {'RecordInCamera': True}),
              ({'record_in_camera': False}, {'RecordInCamera': False}), ({}, {}),
              ({'filename': 'é' * 70, 'disk1': 4294967295, 'disk2': 1, 'record_in_camera': True},
               {'Filename': 'é' * 70, 'Disk1': 4294967295, 'Disk2': 1, 'RecordInCamera': True})):
    record('RecordingSettingsSet', f, f'set_recording_settings({kw})', lambda c, kw=kw: recording.set_recording_settings(c, **kw))
record('RecorderStatus', {'Recording': True}, 'start_recording()', lambda c: recording.start_recording(c))
record('RecorderStatus', {'Recording': False}, 'stop_recording()', lambda c: recording.stop_recording(c))
for kw, f in (({'name': 'YouTube', 'url': 'rtmp://a.rtmp.youtube.com/live2', 'key': 'abcd-1234', 'bitrate_min': 6000000, 'bitrate_max': 9000000},
               {'Name': 'YouTube', 'URL': 'rtmp://a.rtmp.youtube.com/live2', 'Key': 'abcd-1234', 'BitrateMin': 6000000, 'BitrateMax': 9000000}),
              ({'key': 'k'}, {'Key': 'k'}), ({'name': 'n' * 100}, {'Name': 'n' * 100}), ({'url': 'u' * 600, 'key': 'k' * 513}, {'URL': 'u' * 600, 'Key': 'k' * 513}),
              ({'bitrate_min': 1}, {'BitrateMin': 1}), ({'bitrate_max': 1}, {'BitrateMax': 1}), ({}, {})):
    record('StreamingServiceSet', f, f'set_streaming_service({kw})', lambda c, kw=kw: streaming.set_streaming_service(c, **kw))
for lo, hi in ((128000, 128000), (0, 4294967295)):
    record('StreamingAudioBitrate', {'Min': lo, 'Max': hi}, f'set_streaming_audio_bitrate({lo}, {hi})',
           lambda c, lo=lo, hi=hi: streaming.set_streaming_audio_bitrate(c, lo, hi))
record('StreamingStatusSet', {'Streaming': True}, 'start_streaming()', lambda c: streaming.start_streaming(c))
record('StreamingStatusSet', {'Streaming': False}, 'stop_streaming()', lambda c: streaming.stop_streaming(c))

# system
S = system_info
for e in BOOL:
    record('AutoInputVideoMode', {'Enable': e}, f'set_auto_video_mode({e})', lambda c, e=e: S.set_auto_video_mode(c, e))
record('SaveStartupState', {}, 'save_startup_state()', lambda c: S.save_startup_state(c))
record('ClearStartupState', {}, 'clear_startup_state()', lambda c: S.clear_startup_state(c))
for ts in (0, 1, 1789128000, 4294967295, 4294967296):
    when = datetime.datetime.fromtimestamp(ts, datetime.timezone.utc)
    record('SetTimeOfDay', {'Time': ts}, f'set_time_of_day({when.isoformat()})', lambda c, w=when: S.set_time_of_day(c, w))
record('TimeRequest', {}, 'TimeRequestCommand()', lambda c: c.send(S.TimeRequestCommand()))

# lock and file transfer: Send classes, wire units.
for store, state in ((0, True), (0, False), (0xffff, True), (7, False)):
    record('Lock', {'Store': store, 'State': state}, f'LockCommand({store}, {state})', lambda c, s=store, st=state: c.send(lock.LockCommand(s, st)))
for store, slot in ((0, 0), (0, 19), (65535, 65535)):
    record('PartialLock', {'Store': store, 'Slot': slot}, f'PartialLockCommand({store}, {slot})',
           lambda c, s=store, sl=slot: c.send(lock.PartialLockCommand(s, sl)))
FT = file_transfer
for tid, store, slot in ((43, 0, 3), (44, 0xffff, 99), (65535, 1, 4294967295)):
    record('TransferDownloadRequest', {'Transfer': tid, 'Store': store, 'Slot': slot}, f'TransferDownloadRequestCommand({tid}, {store}, {slot})',
           lambda c, t=tid, s=store, sl=slot: c.send(FT.TransferDownloadRequestCommand(t, s, sl)))
for tid, store, slot, length, mode in ((43, 0, 3, 8294400, 1), (44, 0xffff, 99, 0, 0x0300), (1, 2, 65535, 4294967295, 512)):
    record('TransferUploadRequest', {'Transfer': tid, 'Store': store, 'Slot': slot, 'Length': length, 'Mode': mode},
           f'TransferUploadRequestCommand({tid}, {store}, {slot}, {length}, {mode})',
           lambda c, a=(tid, store, slot, length, mode): c.send(FT.TransferUploadRequestCommand(*a)))
for tid, data in ((43, ''), (44, '00112233'), (1, 'fe' * 1000)):
    record('TransferData', {'Transfer': tid, 'Data': data}, f'TransferDataCommand({tid}, {len(data) // 2} bytes)',
           lambda c, t=tid, d=data: c.send(FT.TransferDataCommand(t, bytes.fromhex(d))))
for tid, h, name, desc in ((43, '00' * 16, None, None), (44, '0123456789abcdef' * 2, 'Still 1', 'desc'), (45, 'ff' * 16, 'n' * 70, 'd' * 130)):
    f = {'Transfer': tid, 'Hash': h}
    if name is not None:
        f['Name'], f['Description'] = name, desc
    record('TransferFileData', f, f'TransferFileDataCommand({tid}, {h}, {name!r}, {desc!r})',
           lambda c, t=tid, h=h, n=name, d=desc: c.send(FT.TransferFileDataCommand(t, bytes.fromhex(h), name=n, description=d)))
for tid, slot in ((43, 0), (65535, 65535)):
    record('TransferAck', {'Transfer': tid, 'Slot': slot}, f'TransferAckCommand({tid}, {slot})', lambda c, t=tid, s=slot: c.send(FT.TransferAckCommand(t, s)))

with open(OUT, 'w') as f:
    json.dump({'bmdwire': 'v1.4.0', 'generator': 'internal/fixtures/oracle/gen_send.py', 'cases': cases},
              f, indent=0, sort_keys=True, ensure_ascii=False)
    f.write('\n')
counts = {}
for c in cases:
    counts[c['result']] = counts.get(c['result'], 0) + 1
print(len(cases), 'cases over', len({c['type'] for c in cases}), 'Go types:', counts)
