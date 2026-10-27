// SPDX-License-Identifier: MIT

// Package ultimatte reaches Blackmagic Design Ultimatte 12 keyers over
// their two TCP channels, ported from bmdwire v1.7.0's ultimattewire: the
// text prelude on 9998 and its parsers (label, the preset FILE LIST, the
// CONTROL values), the block protocol on 9998 that reads and sets the
// network interface, the binary reads and writes on 9996, and Smart
// Remote 4's Archive All and Restore zip on top of them, checked against
// what ultimattewire itself does (testdata/oracle). Like ultimattewire it
// keeps nothing between calls: a Client opens its own connections for
// each and closes them. docs/SURFACE-ultimatte.md is ultimattewire's
// inventory and docs/PORTING.md, "Ultimatte", the decisions.
//
//	var c ultimatte.Client
//	zip, info, err := c.Archive(ctx, "192.0.2.21", time.Now())
//	iface, err := c.ReadNetwork(ctx, "192.0.2.21", 0)
package ultimatte
