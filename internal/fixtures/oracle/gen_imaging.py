# Copyright 2026, Lucas Romanenko
# SPDX-License-Identifier: LGPL-3.0-only
"""Generate imaging.json: what bmdwire's still-image codecs and WebATEM's
frame fitting do with each input.

    python3 -I gen_imaging.py /path/to/bmdwire imaging.json

/path/to/bmdwire must be a checkout of bmdwire at tag v1.3.0. Pillow 10.2.0
and gcc must be installed; nothing else is needed (no Python headers).

The C extension. atemwire.mediaconvert (mediaconvertmodule.c) is compiled
unmodified into a shared library, against a stand-in Python.h this script
writes to a temporary directory. The stand-in defines just what the file
uses: PyObject, Py_buffer, Py_ssize_t, PyArg_ParseTuple for the "y*",
"y*II" and "y*IIp" formats, Py_BuildValue "y#", PyErr_Format,
PyErr_SetString, PyErr_NoMemory, PyBuffer_Release and the module-definition
types. Its compiler flags are the ones this Python was built with
(sysconfig CFLAGS and CCSHARED), which is how setuptools builds the real
extension, so the floating point is compiled the same way. The three
methods are called through ctypes and installed as the atemwire.mediaconvert
module, so the calls go through bmdwire's own atemwire/imaging.py wrappers;
rle_decode is imaging.py's pure-Python function.

The fit. fit() is WebATEM's _prepare_frame (atem-web-control v2.0.2,
atem_control/uploader.py, lines 292-310) with the file open replaced by an
in-memory RGBA image and the final rgb_to_atem call left off: what it
records is canvas.tobytes(), the straight RGBA frame _prepare_frame hands
to rgb_to_atem.

Inputs come from a fixed seed. Large inputs are not stored: they are made
by small generators (noise, sweep, colours, ycbcr, and the fit patterns)
that the Go test reimplements, and large outputs are stored as SHA-256
digests. Two sweeps are exhaustive: rgb_to_atem sees every 24-bit colour
in both pixel slots and every alpha, and atem_to_rgb every (Y, Cb) and
(Y, Cr) pair and every alpha, so rgb_to_atem without premultiply, and
atem_to_rgb's red, blue and alpha, are checked on every input they depend
on.

The search. Green depends on all of Y, Cb and Cr, and premultiplied
rgb_to_atem on all four channels, too many to sweep. A small C program,
compiled with the same flags, looks for inputs where the module's
arithmetic gives a different byte from two plausible mistakes in a port:
the #define'd constants folded exactly (Go's constant arithmetic) instead
of operation by operation, and multiply-adds fused into FMAs. The inputs
it finds become cases like any other; their expected bytes come from the
module. How many it found is recorded under "search".

The Go test in atem/ checks the port against every case.
"""
import ctypes
import hashlib
import json
import os
import random
import shlex
import struct
import subprocess
import sys
import sysconfig
import tempfile
import types
from fractions import Fraction

BMDWIRE, OUT = sys.argv[1], sys.argv[2]
sys.path.insert(0, BMDWIRE)

import PIL  # noqa: E402
from PIL import Image  # noqa: E402

assert PIL.__version__ == '10.2.0', PIL.__version__

rng = random.Random(4242)

# --- the C extension, compiled against a stand-in Python.h ---------------

PYTHON_H = r'''
/* Stand-in for <Python.h>: just enough for atemwire's mediaconvertmodule.c.
   Written by gen_imaging.py. */
#ifndef GEN_IMAGING_PYTHON_H
#define GEN_IMAGING_PYTHON_H
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef ptrdiff_t Py_ssize_t;
typedef struct _object PyObject;
typedef struct {
    void *buf;
    PyObject *obj;
    Py_ssize_t len;
} Py_buffer;
typedef PyObject *(*PyCFunction)(PyObject *, PyObject *);
typedef struct PyMethodDef {
    const char *ml_name;
    PyCFunction ml_meth;
    int ml_flags;
    const char *ml_doc;
} PyMethodDef;
#define METH_VARARGS 0x0001
typedef struct {
    int unused;
} PyModuleDef_Base;
#define PyModuleDef_HEAD_INIT {0}
typedef struct PyModuleDef {
    PyModuleDef_Base m_base;
    const char *m_name;
    const char *m_doc;
    Py_ssize_t m_size;
    PyMethodDef *m_methods;
} PyModuleDef;
#define PyMODINIT_FUNC __attribute__((visibility("default"))) PyObject *

/* Hidden, so the library binds to these and not to the running
   interpreter's exported functions of the same names. */
#pragma GCC visibility push(hidden)
extern PyObject *PyExc_ValueError;
int PyArg_ParseTuple(PyObject *args, const char *format, ...);
PyObject *Py_BuildValue(const char *format, ...);
PyObject *PyErr_Format(PyObject *exception, const char *format, ...);
void PyErr_SetString(PyObject *exception, const char *message);
PyObject *PyErr_NoMemory(void);
void PyBuffer_Release(Py_buffer *view);
PyObject *PyModule_Create(PyModuleDef *def);
#pragma GCC visibility pop
#endif
'''

SHIM_C = r'''
/* The functions the stand-in Python.h declares, plus shim_call, which
   gen_imaging.py calls through ctypes. Written by gen_imaging.py. */
#include "Python.h"
#include <stdarg.h>
#include <stdio.h>

struct _object {
    const char *name;          /* exception objects */
    const unsigned char *data; /* argument tuple: the y* buffer; bytes: the value */
    Py_ssize_t len;
    unsigned int width, height;
    int flag;
};

static struct _object value_error = {"ValueError"};
static struct _object memory_error = {"MemoryError"};
static struct _object type_error = {"TypeError"};
static struct _object module_object = {"module"};
PyObject *PyExc_ValueError = &value_error;

static PyModuleDef *module_def;
static const char *error_name;
static char error_message[512];
static int buffers_held;

static void set_error(PyObject *exception, const char *message) {
    error_name = exception->name;
    snprintf(error_message, sizeof error_message, "%s", message);
}

int PyArg_ParseTuple(PyObject *args, const char *format, ...) {
    va_list ap;
    if (strcmp(format, "y*") && strcmp(format, "y*II") && strcmp(format, "y*IIp")) {
        set_error(&type_error, format);
        return 0;
    }
    va_start(ap, format);
    Py_buffer *view = va_arg(ap, Py_buffer *);
    view->buf = (void *)args->data;
    view->obj = args;
    view->len = args->len;
    buffers_held++;
    if (format[2] == 'I') {
        *va_arg(ap, unsigned int *) = args->width;
        *va_arg(ap, unsigned int *) = args->height;
        if (format[4] == 'p') {
            *va_arg(ap, int *) = args->flag;
        }
    }
    va_end(ap);
    return 1;
}

PyObject *Py_BuildValue(const char *format, ...) {
    va_list ap;
    if (strcmp(format, "y#")) {
        set_error(&type_error, format);
        return NULL;
    }
    va_start(ap, format);
    const char *data = va_arg(ap, const char *);
    Py_ssize_t len = va_arg(ap, Py_ssize_t);
    va_end(ap);
    PyObject *o = calloc(1, sizeof *o);
    unsigned char *copy = malloc(len ? len : 1);
    if (!o || !copy) {
        free(o);
        free(copy);
        return PyErr_NoMemory();
    }
    memcpy(copy, data, len);
    o->name = "bytes";
    o->data = copy;
    o->len = len;
    return o;
}

PyObject *PyErr_Format(PyObject *exception, const char *format, ...) {
    va_list ap;
    va_start(ap, format);
    vsnprintf(error_message, sizeof error_message, format, ap);
    va_end(ap);
    error_name = exception->name;
    return NULL;
}

void PyErr_SetString(PyObject *exception, const char *message) {
    set_error(exception, message);
}

PyObject *PyErr_NoMemory(void) {
    set_error(&memory_error, "");
    return NULL;
}

void PyBuffer_Release(Py_buffer *view) {
    if (view->obj) {
        buffers_held--;
        view->obj = NULL;
    }
}

PyObject *PyModule_Create(PyModuleDef *def) {
    module_def = def;
    return &module_object;
}

/* Call the module method `name`. On success *out is a malloc'd copy of the
   bytes it returned (free it with free()); on failure the error is in
   shim_error_name and shim_error_message. */
__attribute__((visibility("default"))) int
shim_call(const char *name, const unsigned char *data, Py_ssize_t len,
          unsigned int width, unsigned int height, int flag,
          unsigned char **out, Py_ssize_t *out_len) {
    struct _object args = {"tuple", data, len, width, height, flag};
    error_name = NULL;
    error_message[0] = 0;
    if (!module_def) {
        set_error(&type_error, "module not initialised");
        return 0;
    }
    for (PyMethodDef *m = module_def->m_methods; m->ml_name; m++) {
        if (strcmp(m->ml_name, name) == 0) {
            PyObject *res = m->ml_meth(NULL, &args);
            if (!res) {
                return 0;
            }
            *out = (unsigned char *)res->data;
            *out_len = res->len;
            free(res);
            return 1;
        }
    }
    set_error(&type_error, name);
    return 0;
}

__attribute__((visibility("default"))) const char *shim_error_name(void) { return error_name; }
__attribute__((visibility("default"))) const char *shim_error_message(void) { return error_message; }
__attribute__((visibility("default"))) int shim_buffers_held(void) { return buffers_held; }
'''

SEARCH_C = r"""
/* Looks for inputs on which atemwire's mediaconvertmodule.c arithmetic
   gives a different byte from two plausible mistakes in a port: the
   #define'd constants folded exactly and rounded once (as Go folds constant
   expressions) instead of operation by operation, and multiply-adds
   contracted into fused multiply-adds (which Go may do on some CPUs unless
   each product is converted explicitly). The expressions are the module's,
   copied; the oracle's answers for the inputs found still come from the
   module itself. Written by gen_imaging.py. */
#include <math.h>
#include <stdint.h>
#include <stdio.h>

#define bt709_coeff_r 0.2126
#define bt709_coeff_g 0.7152
#define bt709_coeff_b 0.0722
#define bt709_coeff_ri (1.0 - bt709_coeff_r)
#define bt709_coeff_bi (1.0 - bt709_coeff_b)
#define bt709_coeff_bg (bt709_coeff_b / bt709_coeff_g)
#define bt709_coeff_rg (bt709_coeff_r / bt709_coeff_g)
#define y_offset (16 << 8)
#define y_range 219
#define cr_offset (128 << 8)
#define cr_middle (224 / 2)
#define bt709_ri_range (bt709_coeff_ri / cr_middle)
#define bt709_bi_range (bt709_coeff_bi / cr_middle)
#define LIMIT 8

static uint64_t state = 88172645463325252ULL;
static unsigned next(unsigned n) {
    state ^= state << 13;
    state ^= state >> 7;
    state ^= state << 17;
    return (unsigned)(state >> 11) % n;
}

/* fmax(0, fmin(255, v)) for a value that is not NaN, then the float the
   module stores it in, then the byte. */
static unsigned char byte_of(double v) {
    v = v < 255 ? v : 255;
    v = v > 0 ? v : 0;
    return (unsigned char)(float)v;
}

static unsigned short clamp(unsigned short v, unsigned short min, unsigned short max) {
    const short t = v < min ? min : v;
    return t > max ? max : t;
}

/* rgb_to_atem's y10, cb10 and cr10 for a pixel in the second slot. */
static void to_atem(int r, int g, int b, int a, int premultiply, int fused, int out[3]) {
    float rf = (float)r / 255, gf = (float)g / 255, bf = (float)b / 255;
    if (premultiply) {
        float af = (float)a / 255;
        rf = rf * af;
        gf = gf * af;
        bf = bf * af;
    }
    float y = fused ? fmaf(0.0722f, bf, fmaf(0.7152f, gf, 0.2126f * rf))
                    : (0.2126f * rf) + (0.7152f * gf) + (0.0722f * bf);
    float cb = (bf - y) / 1.8556f;
    float cr = (rf - y) / 1.5748f;
    out[0] = clamp((unsigned short)(fused ? fmaf(y, 876, 64) : (y * 876) + 64), 64, 940);
    out[1] = clamp((unsigned short)fmax(0.0f, fused ? fmaf(cb, 896, 512) : (cb * 896) + 512), 44, 960);
    out[2] = clamp((unsigned short)fmax(0.0f, fused ? fmaf(cr, 896, 512) : (cr * 896) + 512), 44, 960);
}

int main(void) {
    static float cbf[2][1024], crf[2][1024], yf[1024];
    const double bi[2] = {bt709_bi_range, EXACT_BI_RANGE}, ri[2] = {bt709_ri_range, EXACT_RI_RANGE};
    const double bg[2] = {bt709_coeff_bg, EXACT_BG}, rg[2] = {bt709_coeff_rg, EXACT_RG};
    for (int v = 0; v < 1024; v++) {
        for (int k = 0; k < 2; k++) {
            cbf[k][v] = bi[k] * ((v << 6) - cr_offset);
            crf[k][v] = ri[k] * ((v << 6) - cr_offset);
        }
        yf[v] = ((double)(v << 6) - y_offset) / y_range;
    }
    /* atem_to_rgb, exact constants: every (y, c) for red and blue, every
       (y, cb, cr) for green. */
    int found[3] = {0, 0, 0};
    for (int y = 0; y < 1024; y++) {
        for (int c = 0; c < 1024; c++) {
            if (found[0] < LIMIT && byte_of(yf[y] + crf[0][c]) != byte_of(yf[y] + crf[1][c])) {
                printf("constants %d %d %d\n", y, 512, c);
                found[0]++;
            }
            if (found[2] < LIMIT && byte_of(yf[y] + cbf[0][c]) != byte_of(yf[y] + cbf[1][c])) {
                printf("constants %d %d %d\n", y, c, 512);
                found[2]++;
            }
        }
    }
    for (int y = 0; y < 1024 && found[1] < LIMIT; y++) {
        for (int cb = 0; cb < 1024 && found[1] < LIMIT; cb++) {
            for (int cr = 0; cr < 1024; cr++) {
                double g0 = yf[y] - cbf[0][cb] * bg[0] - crf[0][cr] * rg[0];
                double g1 = yf[y] - cbf[1][cb] * bg[1] - crf[1][cr] * rg[1];
                if (byte_of(g0) != byte_of(g1) && found[1]++ < LIMIT) {
                    printf("constants %d %d %d\n", y, cb, cr);
                }
            }
        }
    }
    /* atem_to_rgb, the green difference fused: random (y, cb, cr). */
    int fused = 0;
    for (long i = 0; i < (1L << 24) && fused < LIMIT; i++) {
        int y = next(1024), cb = next(1024), cr = next(1024);
        double g0 = yf[y] - cbf[0][cb] * bg[0] - crf[0][cr] * rg[0];
        double g1 = fma(-(double)crf[0][cr], rg[0], fma(-(double)cbf[0][cb], bg[0], yf[y]));
        if (byte_of(g0) != byte_of(g1)) {
            printf("fused-green %d %d %d\n", y, cb, cr);
            fused++;
        }
    }
    /* rgb_to_atem, products fused: random pixels, each output field. */
    for (int premultiply = 0; premultiply < 2; premultiply++) {
        int field[3] = {0, 0, 0};
        for (long i = 0; i < (1L << 24); i++) {
            int r = next(256), g = next(256), b = next(256), a = next(256), o0[3], o1[3];
            to_atem(r, g, b, a, premultiply, 0, o0);
            to_atem(r, g, b, a, premultiply, 1, o1);
            for (int f = 0; f < 3; f++) {
                if (o0[f] != o1[f] && field[f] < LIMIT) {
                    printf("fused-rgb %d %d %d %d %d\n", premultiply, r, g, b, a);
                    field[f]++;
                }
            }
            if (field[0] == LIMIT && field[1] == LIMIT && field[2] == LIMIT) {
                break;
            }
        }
    }
    return 0;
}
"""

SOURCE = os.path.join(BMDWIRE, 'atemwire', 'mediaconvertmodule.c')
CFLAGS = shlex.split(sysconfig.get_config_var('CFLAGS')) + shlex.split(sysconfig.get_config_var('CCSHARED'))
GCC = '/usr/bin/gcc'

tmp = tempfile.TemporaryDirectory()
with open(os.path.join(tmp.name, 'Python.h'), 'w') as f:
    f.write(PYTHON_H)
with open(os.path.join(tmp.name, 'shim.c'), 'w') as f:
    f.write(SHIM_C)
LIB = os.path.join(tmp.name, 'mediaconvert.so')
subprocess.run([GCC, *CFLAGS, '-I', tmp.name, '-shared', '-o', LIB, SOURCE,
                os.path.join(tmp.name, 'shim.c'), '-lm'], check=True)
GCC_VERSION = subprocess.run([GCC, '--version'], check=True, capture_output=True, text=True).stdout.splitlines()[0]

lib = ctypes.CDLL(LIB)
lib.PyInit_mediaconvert.restype = ctypes.c_void_p
lib.shim_call.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_ssize_t, ctypes.c_uint, ctypes.c_uint,
                          ctypes.c_int, ctypes.POINTER(ctypes.POINTER(ctypes.c_ubyte)),
                          ctypes.POINTER(ctypes.c_ssize_t)]
lib.shim_error_name.restype = ctypes.c_char_p
lib.shim_error_message.restype = ctypes.c_char_p
libc = ctypes.CDLL(None)
libc.free.argtypes = [ctypes.c_void_p]
assert lib.PyInit_mediaconvert()


def call(name, data, width=0, height=0, flag=0):
    data = bytes(data)
    out = ctypes.POINTER(ctypes.c_ubyte)()
    n = ctypes.c_ssize_t()
    ok = lib.shim_call(name.encode(), data, len(data), width, height, flag, ctypes.byref(out), ctypes.byref(n))
    assert lib.shim_buffers_held() == 0, 'input buffer not released'
    if not ok:
        exc = {'ValueError': ValueError, 'MemoryError': MemoryError}[lib.shim_error_name().decode()]
        raise exc(lib.shim_error_message().decode())
    try:
        return ctypes.string_at(out, n.value)
    finally:
        libc.free(out)


mediaconvert = types.ModuleType('atemwire.mediaconvert')
mediaconvert.atem_to_rgb = lambda data, width, height: call('atem_to_rgb', data, width, height)
# PyArg_ParseTuple's "p" stores the argument's truth value.
mediaconvert.rgb_to_atem = lambda data, width, height, premultiply: call(
    'rgb_to_atem', data, width, height, bool(premultiply))
mediaconvert.rle_encode = lambda data: call('rle_encode', data)
sys.modules['atemwire.mediaconvert'] = mediaconvert

from atemwire import imaging  # noqa: E402

# The constants as Go would fold them: each exact, rounded once.
EXACT = {'EXACT_BI_RANGE': float((1 - Fraction('0.0722')) / 112).hex(),
         'EXACT_RI_RANGE': float((1 - Fraction('0.2126')) / 112).hex(),
         'EXACT_BG': float(Fraction('0.0722') / Fraction('0.7152')).hex(),
         'EXACT_RG': float(Fraction('0.2126') / Fraction('0.7152')).hex()}
with open(os.path.join(tmp.name, 'search.c'), 'w') as f:
    f.write(SEARCH_C)
SEARCH = os.path.join(tmp.name, 'search')
subprocess.run([GCC, *CFLAGS, *[f'-D{k}={v}' for k, v in EXACT.items()], '-o', SEARCH,
                os.path.join(tmp.name, 'search.c'), '-lm'], check=True)
found = {'constants': [], 'fused-green': [], 'fused-rgb': []}
for line in subprocess.run([SEARCH], check=True, capture_output=True, text=True).stdout.splitlines():
    kind, *values = line.split()
    found.setdefault(kind, []).append(tuple(map(int, values)))

# --- generators shared with the Go test ----------------------------------


def pack_block(a1, cb, y1, a2, cr, y2):
    """test_imaging_alpha.py's _pack_block: one 8-byte group, two pixels."""
    return bytes([
        (a1 >> 4) & 0xFF,
        ((a1 & 0x0F) << 4) | ((cb >> 6) & 0x0F),
        ((cb & 0x3F) << 2) | ((y1 >> 8) & 0x03),
        y1 & 0xFF,
        (a2 >> 4) & 0xFF,
        ((a2 & 0x0F) << 4) | ((cr >> 6) & 0x0F),
        ((cr & 0x3F) << 2) | ((y2 >> 8) & 0x03),
        y2 & 0xFF,
    ])


def noise(seed, n):
    """n bytes of xorshift32 output: each state, little-endian, in turn."""
    x = seed
    assert 0 < x < 1 << 32
    out = bytearray()
    for _ in range((n + 3) // 4):
        x ^= (x << 13) & 0xFFFFFFFF
        x ^= x >> 17
        x ^= (x << 5) & 0xFFFFFFFF
        out += x.to_bytes(4, 'little')
    return bytes(out[:n])


def sweep(mask):
    """131072 RGBA pixels in pairs: pair k has value k & 255 in the channels
    mask selects (1 red, 2 green, 4 blue) and alpha k >> 8."""
    out = bytearray()
    for k in range(65536):
        v, a = k & 255, k >> 8
        px = bytes([v if mask & 1 else 0, v if mask & 2 else 0, v if mask & 4 else 0, a])
        out += px + px
    return bytes(out)


def colours_chunks():
    """Every 24-bit colour in both slots, 2**24 pixel pairs, in chunks of
    2**20 pairs: pair k has colour k (red k >> 16, green (k >> 8) & 255,
    blue k & 255) and alpha (k * 167) & 255 in its second pixel, and colour
    k ^ 0xFFFFFF and alpha (k >> 4) & 255 in its first."""
    invert = bytes(255 - i for i in range(256))
    green = b''.join(bytes([v]) * 256 for v in range(256)) * 16
    blue = bytes(range(256)) * 4096
    a1 = bytes((i >> 4) & 255 for i in range(4096)) * 256
    a2 = bytes((i * 167) & 255 for i in range(256)) * 4096
    for c in range(16):
        red = b''.join(bytes([c * 16 + j]) * 65536 for j in range(16))
        buf = bytearray(8 << 20)
        buf[0::8], buf[1::8], buf[2::8], buf[3::8] = red.translate(invert), green.translate(invert), \
            blue.translate(invert), a1
        buf[4::8], buf[5::8], buf[6::8], buf[7::8] = red, green, blue, a2
        yield bytes(buf)


def ycbcr(mask):
    """2**20 Y'CbCrA words: word k has lumas k & 1023 and 1023 - (k & 1023),
    Cb k >> 10, Cr the same (mask 0) or 1023 - (k >> 10) (mask 1), and
    alphas k & 4095 and (k >> 8) & 4095."""
    out = bytearray()
    for k in range(1 << 20):
        y, c = k & 1023, k >> 10
        out += pack_block(k & 4095, c, y, (k >> 8) & 4095, c if mask == 0 else 1023 - c, 1023 - y)
    return bytes(out)


BINARY_ALPHA = bytes(0 if i < 128 else 255 for i in range(256))


def pattern(kind, w, h, seed):
    """A w x h straight-RGBA source image for the fit cases."""
    n = w * h
    if kind == 'noise':
        return noise(seed, 4 * n)
    if kind == 'noise-opaque':
        b = bytearray(noise(seed, 4 * n))
        b[3::4] = b'\xff' * n
        return bytes(b)
    if kind == 'noise-binary':
        b = bytearray(noise(seed, 4 * n))
        b[3::4] = bytes(b[3::4]).translate(BINARY_ALPHA)
        return bytes(b)
    if kind == 'gradient':
        out = bytearray()
        red = bytes(x * 255 // max(1, w - 1) for x in range(w))
        for y in range(h):
            row = bytearray(4 * w)
            row[0::4] = red
            row[1::4] = bytes([y * 255 // max(1, h - 1)]) * w
            row[2::4] = bytes((x ^ y) & 255 for x in range(w))
            row[3::4] = bytes(255 - (x + y) * 255 // max(1, w + h - 2) for x in range(w))
            out += row
        return bytes(out)
    if kind == 'checker':
        out = bytearray()
        for y in range(h):
            out += b''.join(b'\xff\xff\xff\xff' if (x // seed + y // seed) % 2 else b'\x00\x00\x00\xff'
                            for x in range(w))
        return bytes(out)
    if kind == 'solid':
        return seed.to_bytes(4, 'big') * n
    raise ValueError(kind)


def generate(spec):
    if spec['kind'] == 'noise':
        return noise(spec['seed'], spec['n'])
    if spec['kind'] == 'sweep':
        return sweep(spec['mask'])
    if spec['kind'] == 'ycbcr':
        return ycbcr(spec['mask'])
    return pattern(spec['kind'], spec['w'], spec['h'], spec['seed'])


def sha(b):
    return hashlib.sha256(b).hexdigest()


def output(case, b, limit=1024):
    if len(b) <= limit:
        case['out'] = b.hex()
    else:
        case['out_sha256'] = sha(b)
        case['out_len'] = len(b)


def error(case, e):
    case['error'] = type(e).__name__
    case['message'] = str(e)


# --- rgb_to_atem ----------------------------------------------------------

rgb_to_atem = []


def add_rgb_to_atem(premultiply, data=None, gen=None, limit=1024):
    case = {'premultiply': premultiply}
    if gen is not None:
        case['gen'] = gen
        data = generate(gen)
    else:
        case['in'] = data.hex()
    try:
        output(case, imaging.rgb_to_atem(data, 0, 0, premultiply), limit)
    except ValueError as e:
        error(case, e)
    rgb_to_atem.append(case)


EDGES = (0, 1, 2, 127, 128, 129, 253, 254, 255)
ALPHAS = (0, 1, 2, 16, 127, 128, 129, 254, 255)
edge_pixels = [bytes([r, g, b, a]) for r in (0, 128, 255) for g in (0, 128, 255) for b in (0, 128, 255)
               for a in (0, 255)]
saturated = [bytes(c) + bytes([a]) for c in ((255, 0, 0), (0, 255, 0), (0, 0, 255), (255, 255, 0), (0, 255, 255),
                                             (255, 0, 255), (255, 255, 255), (0, 0, 0)) for a in ALPHAS]
channel_edges = [bytes([v, v, v, a]) for v in EDGES for a in ALPHAS] + \
                [bytes([v, 0, 0, a]) for v in EDGES for a in ALPHAS] + \
                [bytes([0, v, 255 - v, a]) for v in EDGES for a in ALPHAS]
for premultiply in (False, True):
    for n in (1, 2, 4, 7, 9, 12, 15, 17):
        add_rgb_to_atem(premultiply, bytes(n))
    add_rgb_to_atem(premultiply, b'')
    add_rgb_to_atem(premultiply, bytes(8))
    add_rgb_to_atem(premultiply, b'\xff' * 8)
    add_rgb_to_atem(premultiply, bytes([200, 100, 50, 255] * 2))
    for pixels in (edge_pixels, saturated, channel_edges):
        frame = b''.join(pixels)
        if len(frame) % 8:
            frame += pixels[0]
        add_rgb_to_atem(premultiply, frame, limit=1 << 20)
        add_rgb_to_atem(premultiply, frame[4:] + frame[:4], limit=1 << 20)  # every pixel in the other slot
    for _ in range(16):
        add_rgb_to_atem(premultiply, noise(rng.randrange(1, 1 << 32), 8 * rng.randrange(1, 9)))
    for mask in (1, 2, 4, 7):
        add_rgb_to_atem(premultiply, gen={'kind': 'sweep', 'mask': mask})
    for _ in range(2):
        add_rgb_to_atem(premultiply, gen={'kind': 'noise', 'seed': rng.randrange(1, 1 << 32), 'n': 1 << 20})

for premultiply in (False, True):
    digest, n = hashlib.sha256(), 0
    for chunk in colours_chunks():
        out = imaging.rgb_to_atem(chunk, 0, 0, premultiply)
        digest.update(out)
        n += len(out)
    rgb_to_atem.append({'premultiply': premultiply, 'gen': {'kind': 'colours'},
                        'out_sha256': digest.hexdigest(), 'out_len': n})
    pixels = [bytes([r, g, b, a]) * 2 for p, r, g, b, a in found.get('fused-rgb', []) if p == premultiply]
    if pixels:
        add_rgb_to_atem(premultiply, b''.join(pixels))

# --- atem_to_rgb ----------------------------------------------------------

atem_to_rgb = []


def add_atem_to_rgb(data=None, gen=None, limit=1024):
    case = {}
    if gen is not None:
        case['gen'] = gen
        data = generate(gen)
    else:
        case['in'] = data.hex()
    try:
        output(case, imaging.atem_to_rgb(data, 0, 0), limit)
    except ValueError as e:
        error(case, e)
    atem_to_rgb.append(case)


for n in (1, 2, 3, 4, 5, 6, 7, 9, 12, 15, 17):
    add_atem_to_rgb(bytes(n))
add_atem_to_rgb(b'')
blocks = []
for a in (0, 1, 15, 16, 17, 20, 64, 500, 934, 935, 936, 937, 938, 940, 1023, 2048, 4095):
    blocks.append(pack_block(a, 512, 502, a, 512, 502))
for c in (0, 1, 44, 63, 64, 448, 511, 512, 513, 576, 940, 960, 961, 1023):
    for y in (0, 63, 64, 65, 502, 939, 940, 941, 1023):
        blocks.append(pack_block(937, c, y, 937, 1023 - c, 1023 - y))
        blocks.append(pack_block(937, c, y, 937, c, y))
add_atem_to_rgb(b''.join(blocks), limit=1 << 20)
# bytes 0 and 4 at and above 0x80, which the C reads through a signed char
signed = []
for b0 in (0x7f, 0x80, 0x81, 0xc0, 0xfe, 0xff):
    for b1 in (0x00, 0x0f, 0x80, 0xf0, 0xff):
        signed.append(bytes([b0, b1, 0x80, 0x00, b0, b1, 0xff, 0xff]))
        signed.append(bytes([b0, b1, 0xfc, 0xff, 0xff - b0, b1 ^ 0xff, 0x03, 0x80]))
add_atem_to_rgb(b''.join(signed), limit=1 << 20)
for _ in range(24):
    add_atem_to_rgb(noise(rng.randrange(1, 1 << 32), 8 * rng.randrange(1, 9)))
for _ in range(3):
    add_atem_to_rgb(gen={'kind': 'noise', 'seed': rng.randrange(1, 1 << 32), 'n': 1 << 20})
for mask in (0, 1):
    add_atem_to_rgb(gen={'kind': 'ycbcr', 'mask': mask})
for kind in ('constants', 'fused-green'):
    words = [pack_block(937, cb, y, 937, cr, y) for y, cb, cr in found.get(kind, [])]
    if words:
        add_atem_to_rgb(b''.join(words))

# --- rle_encode -----------------------------------------------------------

HEADER = b'\xfe' * 8
rle_encode = []


def expand(runs):
    return b''.join(bytes.fromhex(w) * n for w, n in runs)


def word():
    while True:
        w = noise(rng.randrange(1, 1 << 32), 8)
        if w != HEADER:
            return w.hex()


def add_rle_encode(data=None, runs=None):
    case = {}
    if runs is not None:
        case['runs'] = [[w, n] for w, n in runs]
        data = expand(runs)
    else:
        case['in'] = data.hex()
    try:
        out = imaging.rle_encode(data)
    except ValueError as e:
        error(case, e)
    else:
        output(case, out)
        case['decodes_back'] = bytes(imaging.rle_decode(out)) == data
    rle_encode.append(case)
    return case


W, X, Y, Z = (bytes([i] * 7 + [0x10 + i]).hex() for i in range(1, 5))
FE = HEADER.hex()
add_rle_encode(b'')
for n in (1, 2, 3, 4, 5, 6, 7, 9, 12, 15, 17, 23):
    add_rle_encode(bytes(n))
add_rle_encode(b'\xfe')
add_rle_encode(runs=[(X, 1)])
add_rle_encode(runs=[(X, 2)])
add_rle_encode(runs=[(X, 1), (Y, 1)])
for k in list(range(1, 8)) + [100, 1000, 65539]:
    add_rle_encode(runs=[(X, k)])                       # the whole input
    add_rle_encode(runs=[(X, k), (Y, 1), (Z, 1)])       # at the start
    add_rle_encode(runs=[(W, 1), (X, k), (Y, 1)])       # in the middle
    add_rle_encode(runs=[(W, 1), (X, k)])               # at the end
    add_rle_encode(runs=[(W, 2), (X, k), (Y, 3)])
# bmdwire's characterization case and adjacent runs
add_rle_encode(runs=[('0102030405060708', 1), ('11' * 8, 4)])
add_rle_encode(bytes(range(32)))
add_rle_encode(runs=[(X, 5), (Y, 5), (Z, 1), (X, 3), (W, 4), (X, 4)])
# the reserved header word anywhere is an error; near misses are not
add_rle_encode(runs=[(FE, 1)])
add_rle_encode(runs=[(FE, 3)])
add_rle_encode(runs=[('00' * 8, 1), (FE, 1), ('01' * 8, 1)])
add_rle_encode(runs=[(X, 5), (FE, 1)])
add_rle_encode(runs=[(X, 5), (FE, 1), (X, 5)])
add_rle_encode(runs=[(X, 1000), (Y, 2), (FE, 1)])
for near in ('fefefefefefefeff', 'fffefefefefefefe', 'fefefefe00fefefe', 'fe' * 7 + '00'):
    add_rle_encode(runs=[(near, 1)])
    add_rle_encode(runs=[(W, 1), (near, 5), (X, 1)])
# random runs over a small alphabet, so neighbouring runs often merge
for _ in range(40):
    alphabet = [word() for _ in range(rng.randrange(1, 4))]
    runs = [(rng.choice(alphabet), rng.randrange(1, 8)) for _ in range(rng.randrange(1, 20))]
    add_rle_encode(runs=runs)
for _ in range(4):
    alphabet = [word() for _ in range(3)]
    add_rle_encode(runs=[(rng.choice(alphabet), rng.randrange(1, 3000)) for _ in range(rng.randrange(50, 200))])
# a real frame: a fitted logo on a transparent canvas
logo = pattern('gradient', 40, 20, 0)
add_rle_encode(imaging.rgb_to_atem(logo, 40, 20, True))
assert all(c.get('decodes_back', True) for c in rle_encode)

# --- rle_decode -----------------------------------------------------------

rle_decode = []
SSIZE_MAX = (1 << 63) - 1


def decoded_size(data):
    """What rle_decode would allocate, walking the stream as it does. The
    machine overcommits memory, so a run Python would try to allocate must
    stay small: a mistake here would page the machine to death."""
    total, offset = 0, 0
    while offset < len(data):
        if data[offset:offset + 8] == HEADER:
            if offset + 16 > len(data):
                return total
            count, = struct.unpack_from('>Q', data, offset + 8)
            block = data[offset + 16:offset + 24]
            if count > SSIZE_MAX or len(block) * count > SSIZE_MAX:
                return total  # raises OverflowError before allocating
            total += len(block) * count
            offset += 24
        else:
            total += 8
            offset += 8
    return total


def add_rle_decode(data):
    assert decoded_size(data) < 1 << 24, data[:64].hex()
    case = {'in': data.hex()}
    try:
        out = bytes(imaging.rle_decode(data))
    except Exception as e:  # IndexError, struct.error, OverflowError
        case['error'] = type(e).__name__
    else:
        output(case, out)
    rle_decode.append(case)


def run(count, block):
    return HEADER + struct.pack('>Q', count) + block


x8, w8 = bytes.fromhex(X), bytes.fromhex(W)
add_rle_decode(b'')
add_rle_decode(w8)
add_rle_decode(w8 + x8)
for count in (0, 1, 2, 3, 4, 1000, 100000):
    add_rle_decode(run(count, x8))
    add_rle_decode(w8 + run(count, x8) + w8)
add_rle_decode(run(2, x8) + run(3, w8) + run(1, x8))
add_rle_decode(bytes.fromhex('0102030405060708') + b'\x11' * 8 + run(3, b'\x11' * 8))
add_rle_decode(run(5, b''))                        # the block is missing: nothing
add_rle_decode(run(5, b'\x01\x02\x03'))            # a short block repeats short
add_rle_decode(w8 + run(5, b'\x01\x02\x03'))
add_rle_decode(HEADER)                             # no count: struct.error
add_rle_decode(HEADER + bytes(7))
add_rle_decode(w8 + HEADER + bytes(4))
add_rle_decode(run(1 << 60, x8))                   # too long: OverflowError
add_rle_decode(run((1 << 63) - 1, x8))
add_rle_decode(run(1 << 63, x8))
add_rle_decode(run((1 << 64) - 1, b''))
add_rle_decode(run(1 << 62, b''))                  # long, but of nothing
add_rle_decode(run((1 << 63) - 1, b''))
add_rle_decode(HEADER + HEADER + x8)               # a header as the count
add_rle_decode(HEADER + HEADER)
add_rle_decode(w8 + b'\x01\x02\x03')               # ragged tails
add_rle_decode(w8 + b'\xfe\xfe\xfe')
add_rle_decode(w8 + b'\xfe\xfe\x00')
add_rle_decode(b'\xfe')
add_rle_decode(b'\xfe' * 7)
add_rle_decode(b'\xfe' * 7 + b'\x00')
add_rle_decode(b'\xfe' * 7 + b'\x00' + b'\xfe' * 3)
add_rle_decode(run(1, x8) + b'\xfe' * 4)
add_rle_decode(run(2, x8) + b'\x00')
for near in ('fefefefefefefeff', 'fffefefefefefefe', 'fefefefe00fefefe'):
    add_rle_decode(bytes.fromhex(near) + run(2, bytes.fromhex(near)))


def token():
    r = rng.random()
    if r < 0.45:
        return bytes.fromhex(word())
    if r < 0.55:  # a near miss: up to seven 0xFE bytes, then something else
        k = rng.randrange(0, 8)
        return b'\xfe' * k + bytes([rng.randrange(0, 254)]) + noise(rng.randrange(1, 1 << 32), 7 - k)
    return run(rng.randrange(0, 7), bytes.fromhex(word()))


for _ in range(60):
    stream = b''.join(token() for _ in range(rng.randrange(1, 12)))
    add_rle_decode(stream)
    add_rle_decode(stream[:rng.randrange(0, len(stream) + 1)])
    add_rle_decode(stream + bytes(rng.randrange(1, 8)))

# --- fit: WebATEM's _prepare_frame ----------------------------------------


def fit(src, sw, sh, width, height):
    """atem-web-control v2.0.2 atem_control/uploader.py:_prepare_frame
    (lines 292-310): an in-memory RGBA image in place of Image.open, and
    canvas.tobytes() returned instead of rgb_to_atem(canvas.tobytes(), ...).
    Also returns the scaled image, for diagnostics."""
    im = Image.frombytes('RGBA', (sw, sh), src)

    target_aspect = width / height
    src_aspect = im.width / im.height
    scale = (im.height / height) if src_aspect < target_aspect else (im.width / width)
    scaled = im.resize(
        (max(1, int(im.width / scale)), max(1, int(im.height / scale))),
        Image.Resampling.LANCZOS,
    )

    canvas = Image.new('RGBA', (width, height), (0, 0, 0, 0))
    canvas.paste(scaled, ((width - scaled.width) // 2, (height - scaled.height) // 2), scaled)
    return canvas.tobytes(), scaled


fits = []


def add_fit(kind, sw, sh, width, height, seed=None, limit=1024, ycbcra=False):
    if seed is None:
        seed = {'checker': rng.randrange(1, 9), 'gradient': 0}.get(kind, rng.randrange(1, 1 << 32))
    src = pattern(kind, sw, sh, seed)
    canvas, scaled = fit(src, sw, sh, width, height)
    case = {'src': {'kind': kind, 'w': sw, 'h': sh, 'seed': seed}, 'width': width, 'height': height,
            'scaled': [scaled.width, scaled.height], 'scaled_sha256': sha(scaled.tobytes())}
    output(case, canvas, limit)
    if ycbcra:
        # The whole upload path for a .png file: rgb_to_atem with premultiply.
        case['ycbcra_sha256'] = sha(imaging.rgb_to_atem(canvas, width, height, True))
    fits.append(case)


KINDS = ('noise', 'noise-opaque', 'noise-binary', 'gradient', 'checker', 'solid')
TINY = ((16, 9), (12, 12), (9, 16), (5, 3))
SMALL = ((64, 36), (72, 48), (90, 72))
shapes = [
    (64, 36), (72, 48), (90, 72),                      # exactly a target size
    (128, 72), (640, 360), (32, 18), (16, 9),          # 16:9
    (200, 50), (640, 100), (130, 36), (1000, 1), (7, 1), (300, 2),   # wider
    (50, 200), (36, 64), (1, 1000), (1, 7), (2, 300), (64, 37),      # taller
    (1, 1), (2, 2), (3, 2), (10, 30), (5, 5),          # tiny: upscale
    (64, 30), (50, 36), (72, 40), (60, 48),            # one side already fits
    (1920, 1080), (1000, 1000), (333, 777),            # downscale
]
for i, (sw, sh) in enumerate(shapes):
    for width, height in SMALL:
        add_fit(KINDS[i % len(KINDS)], sw, sh, width, height)
    for width, height in TINY[:2] if i % 2 else TINY[2:]:
        add_fit(KINDS[(i + 3) % len(KINDS)], sw, sh, width, height)
for _ in range(40):
    sw, sh = rng.randrange(1, 400), rng.randrange(1, 400)
    width, height = rng.choice(SMALL + TINY)
    add_fit(rng.choice(KINDS), sw, sh, width, height)
for kind in KINDS:
    add_fit(kind, 23, 17, 16, 9)
    add_fit(kind, 3, 5, 12, 12)
add_fit('solid', 100, 50, 64, 36, seed=0x123456FF)
add_fit('solid', 100, 50, 64, 36, seed=0x12345600)
add_fit('solid', 100, 50, 64, 36, seed=0xFFFFFF80)
# real mode sizes
for kind, (sw, sh), (width, height) in (
        ('noise', (1920, 1080), (1280, 720)),
        ('gradient', (1000, 1000), (1280, 720)),
        ('checker', (640, 200), (1280, 720)),
        ('noise-binary', (1280, 720), (1280, 720)),
        ('noise-binary', (1280, 720), (1920, 1080)),
        ('noise', (2000, 1500), (1920, 1080)),
        ('noise-opaque', (3000, 900), (1920, 1080)),
        ('gradient', (1, 1080), (1920, 1080)),
        ('solid', (4, 3), (1920, 1080))):
    add_fit(kind, sw, sh, width, height, ycbcra=True)

with open(OUT, 'w') as f:
    json.dump({'bmdwire': 'v1.3.0', 'pillow': PIL.__version__,
               'webatem': 'v2.0.2', 'generator': 'internal/fixtures/oracle/gen_imaging.py',
               'compiler': GCC_VERSION, 'cflags': ' '.join(CFLAGS),
               'search': {k: len(v) for k, v in sorted(found.items())},
               'rgb_to_atem': rgb_to_atem, 'atem_to_rgb': atem_to_rgb,
               'rle_encode': rle_encode, 'rle_decode': rle_decode, 'fit': fits},
              f, indent=0, sort_keys=True)
    f.write('\n')
print(f'{len(rgb_to_atem)} rgb_to_atem, {len(atem_to_rgb)} atem_to_rgb, {len(rle_encode)} rle_encode, '
      f'{len(rle_decode)} rle_decode, {len(fits)} fit cases; '
      f'{sum("error" in c for c in rle_decode)} decode errors; search found',
      {k: len(v) for k, v in sorted(found.items())})
