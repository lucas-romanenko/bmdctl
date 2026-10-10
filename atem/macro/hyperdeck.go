// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package macro

// HyperDeck binding ops: bmdwire v1.3.0
// atemwire/macrotransfer/hyperdeck.py.
//
//	0x0110 HyperDeckNetworkAddress [u8 slot][u8 0][u16 marker 0x024a][u32 IPv4, byte-reversed]
//	0x011A HyperDeckInput          [u8 slot][u8 0][u16 switcher input]

// dHDNetworkAddress reads the address from bytes 4-7, reversed. A short
// block gives inet_ntoa's OSError.
func dHDNetworkAddress(p params) []Param {
	slot := p.str(0)
	var ip []byte
	if len(p) > 4 {
		ip = append(ip, p[4:min(len(p), 8)]...)
	}
	for i, j := 0, len(ip)-1; i < j; i, j = i+1, j-1 {
		ip[i], ip[j] = ip[j], ip[i]
	}
	return []Param{{"slot", slot}, {"networkAddress", inetNtoa(ip)}}
}

func dHDInput(p params) []Param {
	return []Param{{"slot", p.str(0)}, {"input", utoa(p.u16(2))}}
}

// eHDNetworkAddress parses the address with inet_aton, so it takes what
// glibc's inet_aton takes ("10.1", "0x7f.1", trailing text after a space)
// and raises OSError for the rest, as atemwire does (SURFACE §5.4 item 8).
func eHDNetworkAddress(a attrs) []byte {
	ip := inetAton(a.req("networkAddress"))
	return cat(pyBytes(a.int("slot"), num(0)), []byte{0x4a, 0x02, ip[3], ip[2], ip[1], ip[0]})
}

// eHDInput packs the input with struct.pack('<H'), unmasked: out of range
// raises struct.error.
func eHDInput(a attrs) []byte {
	return cat(pyBytes(a.int("slot"), num(0)), packH(a.int("input")))
}
