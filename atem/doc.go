// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

// Package atem controls Blackmagic Design ATEM switchers over their UDP
// protocol (port 9910).
//
// It is a Go port of atemwire from bmdwire v1.3.0 and mirrors its
// behaviour. docs/SURFACE-atem.md (copied from bmdwire v1.3.0) is the
// specification; docs/PORTING.md records every place this package
// deliberately differs and why.
package atem
