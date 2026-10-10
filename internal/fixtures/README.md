# Fixtures copied from bmdwire

Every recorded packet, bytecode sample and profile XML in bmdwire v1.3.0's
`atemwire/tests/` (tag `v1.3.0`, commit `cea58c2`), plus the two state
snapshot JSON files beside them. `oracle/` and `dump/` are the exceptions:
they are generated, not copied from bmdwire's tests (see below). Byte strings were copied raw from the Python
literals; where a test builds one from an expression (`bytes([...]) +
struct.pack(...)`), the file holds the bytes that expression evaluates to.
Files were copied byte for byte. Nothing here was captured by bmdctl.

These are data from atemwire's test suite and carry its licence,
LGPL-3.0-only (see `atem/LICENSE`).

`fixtures.go` embeds the directory so tests in other packages can load a
fixture by its path (`fixtures.Bytes(t, "audio/AMBP-band5.bin")`).
`fixtures_test.go` checks that every file is non-empty and listed below.

"origin" is the Python test file and test name; paths are relative to
`atemwire/tests/`. "live" means the test's own docstring or comment says the
bytes came from a switcher.

## audio/

State packets captured from a live switcher, as the payload after the 8-byte command header. The tests decode them with the matching `Recv` class.

| file | bytes | origin | notes |
|---|---|---|---|
| `AMBP-band5.bin` | 20 | `test_master_eq_ambp.py::test_ambp_decodes_band5_reference (_BAND5_REF)` | live capture, master EQ band 5; AMBP payload (trailing pad carries ASCII "LP") |
| `AMLP-master-limiter.bin` | 20 | `test_fairlight_migration.py::test_amlp_decodes_master_limiter` | live capture; AMLP payload |
| `MOCP-master-compressor.bin` | 24 | `test_fairlight_migration.py::test_mocp_decodes_master_compressor` | live capture, 1 M/E Constellation HD; MOCP payload (no 8-byte header) |

## transport/

Command bytes the transport tests put in a datagram's data. They are synthetic: the Python tests build them by hand. Go tests wrap them in a 12-byte packet header the same way the Python helpers do.

| file | bytes | origin | notes |
|---|---|---|---|
| `CTst-command.bin` | 10 | `test_transport_inorder.py::_raw (every test in the file)` | synthetic; packet data the in-order tests stamp onto each sequence. Note the Python literal has no 2 pad bytes, so the tag sits at 2..5 |
| `FTCD-command.bin` | 20 | `test_lock_delivery.py::test_flush_sentinel_keeps_the_packet` | synthetic; one zeroed FTCD command as packet data |
| `LKOB-command.bin` | 12 | `test_lock_delivery.py::_data_packet (test_connection_ready_sentinel_keeps_the_packet)` | synthetic; one LKOB command (8-byte header + store 0) as packet data |

## macro/

Macro bytecode samples (little-endian ops: `u16 length, u16 op code, params`), named `op<code>-<hex>.bin`. `atem/macro`'s tests decode them where bmdwire's tests use the same literals, and `oracle/macro.json` records what bmdwire v1.3.0's codec makes of every one. "capture" notes come from the comments in `test_fairlight_byte_identical_roundtrip_for_live_captures`, which also say which of them are synthetic.

| file | bytes | origin | notes |
|---|---|---|---|
| `op000c-08000c0000000000.bin` | 8 | `test_macro_encoder.py::test_decode_video_mode_canonical_name`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | video-mode 525i5994 (synthetic) |
| `op000c-08000c001b000000.bin` | 8 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | video-mode 1080p60 (synthetic) |
| `op000c-08000c001c000000.bin` | 8 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | video-mode 720p60 (live) |
| `op000c-08000c00ff000000.bin` | 8 | `test_macro_encoder.py::test_decode_video_mode_unknown_int_falls_back_to_string` | decode input (synthetic) |
| `op0035-0800350000000000.bin` | 8 | `test_macro_decoder.py::test_decoder_dve_mask_enable_false` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0035-0800350000020100.bin` | 8 | `test_macro_decoder.py::test_decoder_dve_mask_enable_true` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0036-0c00360000000c000080fbff.bin` | 12 | `test_macro_decoder.py::test_decoder_dve_mask_top_signed` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0037-0c00370000000c0033330700.bin` | 12 | `test_macro_decoder.py::test_decoder_dve_mask_bottom_signed` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0038-0c00380000000c000000f0ff.bin` | 12 | `test_macro_decoder.py::test_decoder_dve_mask_left_signed` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0039-0c00390000000c0000001000.bin` | 12 | `test_macro_decoder.py::test_decoder_dve_mask_right_signed` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op0047-0c00470000000c0000800000.bin` | 12 | `test_macro_decoder.py::test_decoder_xsize_unsigned` | decode input (synthetic; an existing-decoder regression case) |
| `op0048-0c00480000010c0000800100.bin` | 12 | `test_macro_decoder.py::test_decoder_ysize_unsigned` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op004a-0c004a0000000c000000f0ff.bin` | 12 | `test_macro_decoder.py::test_decoder_xpos_signed_negative` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op004a-0c004a0000000c00ffffefff.bin` | 12 | `test_macro_decoder.py::test_decoder_xpos_off_by_one_keeps_precision` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op004b-0c004b0000000c0000000900.bin` | 12 | `test_macro_decoder.py::test_decoder_ypos_signed_positive` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op009d-0c009d0000886c0200800000.bin` | 12 | `test_macro_decoder.py::test_decoder_dsk_gain_unsigned` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op009e-08009e0000010c00.bin` | 8 | `test_macro_decoder.py::test_decoder_dsk_mask_enable_true` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op00a4-0800a40000006c02.bin` | 8 | `test_macro_decoder.py::test_decoder_dsk_pre_multiply_false` | decode input, built by the test from bytes its module docstring says came from the 2026-04-29 op-code discovery run (live) |
| `op014a-14004a01010046430001ffffffffffff01000c00.bin` | 20 | `test_macro_encoder.py::test_decode_fairlight_strip_mix_type`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | CAM1 mix Off (live) |
| `op014a-14004a01010046430001ffffffffffff02000c00.bin` | 20 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | CAM1 mix On (synthetic) |
| `op014a-14004a01010046430001ffffffffffff04000c00.bin` | 20 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | CAM1 mix AFV (synthetic) |
| `op014b-14004b01010046430001ffffffffffff33f3ffff.bin` | 20 | `test_macro_encoder.py::test_decode_fairlight_strip_fader_gain`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | CAM1 vol -0.05 (live) |
| `op014b-14004b01020046430001ffffffffffff33f3ffff.bin` | 20 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | CAM2 vol -0.05 (live) |
| `op016b-08006b0185ebfeff.bin` | 8 | `test_macro_encoder.py::test_decode_fairlight_master_fader_gain`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | master -1.08 (live) |
| `op016b-08006b01f5e8ffff.bin` | 8 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | master -0.09 (live) |
| `op0202-0800020203010000.bin` | 8 | `test_macro_encoder.py::test_decode_ftb_enabled_true`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | FTB enabled mEBI=3 (live) |
| `op0202-0800020206000000.bin` | 8 | `test_macro_encoder.py::test_decode_ftb_enabled_false`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | FTB disabled mEBI=6 (live) |
| `op0202-0800020207010000.bin` | 8 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | FTB enabled mEBI=7 (live) |
| `op0202-08000202b7000000.bin` | 8 | `test_macro_encoder.py::test_decode_ftb_enabled_keeps_garbage_mEBI_byte`<br>`test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | FTB disabled mEBI=183 (garbage) (live) |
| `op0202-08000202b8010000.bin` | 8 | `test_macro_encoder.py::test_fairlight_byte_identical_roundtrip_for_live_captures` | FTB enabled mEBI=184 (garbage) (live) |
| `op9999-0800999901020304.bin` | 8 | `test_macro_decoder.py::test_unknown_opcode_round_trips_as_unknown` | unknown op code, decode input (synthetic) |
| `opabcd-0800cdabdeadbeef.bin` | 8 | `test_macro_encoder.py::test_encode_unknown_op_via_raw_params` | unknown op code, decode input (synthetic) |

## profile/

The ASC-style profile XML that `Profile.to_xml` must reproduce byte for byte for the single-M/E fixture state.

| file | bytes | origin | notes |
|---|---|---|---|
| `profile_single_me_save_golden.xml` | 29583 | `test_profile_granular.py::test_single_me_save_byte_identical_to_pre_4c1_golden` | copied verbatim |

## snapshot/

`build_full_state` characterization snapshots. They are not packets: the Python test builds `mixerstate` from hand-made objects and pins the JSON the snapshot builder makes from it. They are here for the later port of `build_full_state`.

| file | bytes | origin | notes |
|---|---|---|---|
| `build_full_state_multi_me_snapshot.json` | 30056 | `test_build_full_state_characterization.py::test_build_full_state_multi_me_characterization_snapshot` | copied verbatim |
| `build_full_state_snapshot.json` | 21718 | `test_build_full_state_characterization.py::test_build_full_state_characterization_snapshot` | copied verbatim |

## oracle/

Generated, not copied: what bmdwire v1.3.0 itself does with a given input,
recorded by running it (and, in `imaging.json`, what WebATEM's frame
preparation does). Each file has the script that made it beside it; rerun
it against a bmdwire checkout at tag `v1.3.0` to regenerate:

```
cd internal/fixtures/oracle
python3 -I gen_recv.py ../../../../bmdwire recv.json
python3 -I gen_send.py ../../../../bmdwire send.json
python3 -I gen_imaging.py ../../../../bmdwire imaging.json
python3 -I gen_macro.py ../../../../bmdwire macro.json
```

The scripts use a fixed random seed, so the output only changes if bmdwire
or the script does. `gen_recv.py` and `gen_send.py` stub the C extension;
`gen_imaging.py` compiles it, unmodified, and needs gcc and Pillow 10.2.0
(its docstring says how).

| file | bytes | origin | notes |
|---|---|---|---|
| `recv.json` | 319236 | `gen_recv.py`, driving bmdwire v1.3.0's `AtemProtocol.save_field_data` | 1,062 payloads over all 92 state packets and four raw-stored codes: random bytes at, under and over each parser's length, and well-formed variable-length packets. Each case records whether atemwire raised, consumed the packet, stored it raw or parsed it, and then its mixerstate key, index path and attributes. `TestParsersMatchAtemwire` replays them all |
| `send.json` | 1508486 | `gen_send.py`, calling bmdwire v1.3.0's operations (and, for the fields no operation sets, its Send classes) on a fake connection | 5,966 calls over all 82 commands, with value pools that probe rounding ties, clamps, NaN and infinities, and every enum name the operations accept. Each case records the bytes atemwire sent, or that it raised or chose to send nothing, and the Go command (type and field values in the operation's units) that must do the same. `TestCommandsMatchAtemwire` replays them all |
| `imaging.json` | 327121 | `gen_imaging.py`, calling bmdwire v1.3.0's `atemwire/imaging.py`: `rgb_to_atem`, `atem_to_rgb` and `rle_encode` run its C extension (`mediaconvertmodule.c`, compiled unmodified against a stand-in `Python.h` with this Python's CFLAGS), `rle_decode` is its pure Python; and WebATEM's `_prepare_frame` (atem-web-control v2.0.2, `atem_control/uploader.py` lines 292-310, reproduced in the script with an in-memory image in place of the file, on Pillow 10.2.0) | 84 `rgb_to_atem` cases (both premultiply settings; edge, saturated and random pixels; every 24-bit colour in both pixel slots; ragged lengths), 43 `atem_to_rgb` (alpha and chroma edges, bytes at and above 0x80, every (Y, Cb) and (Y, Cr) pair, 1 MiB of random words), 129 `rle_encode` (runs of 1 to 7 and long runs at the start, middle and end, the reserved 0xFE word, ragged lengths, random runs), 225 `rle_decode` (encoder outputs, hand-built, truncated and malformed streams; 40 raise), 219 fits of six generated patterns from 80 source shapes (wider, taller, equal aspect, 1 pixel wide or tall, up- and downscaled, already the target size) to nine targets from 5x3 to 1920x1080. Large inputs come from generators the Go test reimplements; large outputs are SHA-256 digests. Each case records the bytes or the exception (name, and message for the C functions). `TestImagingMatchesAtemwire` and `TestFitFrameMatchesWebATEM` replay them all |
| `macro.json` | 1194609 | `gen_macro.py`, calling bmdwire v1.3.0's `decode_macro_bytecode`, `encode_single_op` and `encode_macro_bytecode` | 1,987 bytecode cases (the 32 `macro/` samples; random, short and enum-walking parameter blocks for all 146 known op codes and for unknown ones; multi-op streams, empty, truncated and with bogus lengths), each with the ops atemwire decodes (ids, attribute names in dict order, values), the warning that stopped it if any, and what re-encoding those ops gives; and 4,272 op dicts built from attribute pools (enum names, numbers, numeric strings, out-of-range values, missing attributes, unknown ids, garbage) with the bytes `encode_single_op` returned or the class and message of what it raised. Recorded with CPython 3.12.3, whose messages it quotes. `TestDecodeMatchesAtemwire` and `TestEncodeMatchesAtemwire` replay them all |

## dump/

Generated, not copied: bmdwire's tests contain no recorded initial state
dump, so the fake switcher (`internal/fakeatem`) replays a synthetic one.
`gen_dump.py` builds it and checks it against bmdwire v1.3.0:

```
cd internal/fixtures/dump
python3 -I gen_dump.py ../../../../bmdwire initial-dump.bin
```

| file | bytes | origin | notes |
|---|---|---|---|
| `initial-dump.bin` | 2070 | `gen_dump.py`; values from `test_build_full_state_characterization.py::_make_mixerstate`, plus identity, topology, multiviewer and timecode packets that fixture leaves out | synthetic: 88 commands a 1 M/E switcher sends after the handshake, concatenated (8-byte header and payload each), ending with `InCm`. Each payload is packed to atemwire's Recv layout, and the script feeds every command to bmdwire's `AtemProtocol.save_field_data`, failing unless it parses and lands under its mixerstate key |
