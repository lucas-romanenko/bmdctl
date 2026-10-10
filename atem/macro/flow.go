// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// Macro flow ops: bmdwire v1.3.0 atemwire/macrotransfer/flow.py.

// dMacroSleep: u32 frames.
func dMacroSleep(p params) []Param { return []Param{{"frames", utoa(p.u32(0))}} }

// eMacroSleep: a missing frames attribute is 0 (SURFACE §5.4 item 9), and
// the value is masked to 32 bits.
func eMacroSleep(a attrs) []byte { return u32le(a.intOr0("frames")) }
