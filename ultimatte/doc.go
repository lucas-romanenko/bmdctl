// SPDX-License-Identifier: MIT

// Package ultimatte reaches Blackmagic Design Ultimatte 12 keyers over
// their two TCP channels, ported from ultimattewire (bmdwire v1.7.0, then
// v1.14.0): the text prelude on 9998 and its parsers (label, the preset
// FILE LIST, the CONTROL values), the block protocol on 9998 that reads
// and sets the network interface, the binary reads and writes on 9996,
// and Smart Remote 4's Archive All and Restore zip on top of them,
// checked against what ultimattewire itself does (testdata/oracle). Like
// ultimattewire's, those keep nothing between calls: a Client opens its
// own connections for each and closes them.
//
// Session (Client.Open) is ultimattewire 1.14.0's Ultimatte: one session
// on 9998 speaking Blackmagic's documented Ultimatte 12 Ethernet Protocol
// v2.0 in full, the unit's state kept current by its pushes, with the
// document's control table (Controls) checking every value before it is
// sent, and the media pool's image upload on 9996 (UploadImage). It has
// not met a unit, here or in ultimattewire.
// docs/SURFACE-ultimatte.md is ultimattewire's inventory and
// docs/PORTING.md, "Ultimatte", the decisions.
//
//	var c ultimatte.Client
//	zip, info, err := c.Archive(ctx, "192.0.2.21", time.Now(), nil)
//	iface, err := c.ReadNetwork(ctx, "192.0.2.21", 0)
//	s, err := c.Open(ctx, "192.0.2.21")
//	v, err := s.SetControl(ctx, "Matte Density", 4200, ultimatte.Options{})
package ultimatte
