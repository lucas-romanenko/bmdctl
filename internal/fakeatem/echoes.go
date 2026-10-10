// SPDX-License-Identifier: MIT

package fakeatem

import (
	"encoding/binary"
	"fmt"
)

// The state packets the fake keeps and echoes, by opcode: how many leading
// payload bytes index them (the M/E, keyer, source ...) and their length.
var kept = map[string]struct{ index, size int }{
	"PrgI": {1, 4}, "PrvI": {1, 8}, "AuxS": {1, 4}, "TMxP": {1, 4}, "KeOn": {2, 4},
	"DskS": {1, 8}, "DskP": {1, 20}, "FtbP": {1, 4}, "ColV": {1, 8}, "InPr": {2, 36},
}

func stateKey(code string, index []byte) string { return fmt.Sprintf("%s:%x", code, index) }

// remember stores a state payload from the dump.
func (sw *Switcher) remember(code string, payload []byte) {
	k, ok := kept[code]
	if !ok || len(payload) < k.size {
		return
	}
	sw.state[stateKey(code, payload[:k.index])] = append([]byte(nil), payload[:k.size]...)
}

// update returns the stored payload for code at index (a zeroed one if
// there is none), lets f change it, stores it and returns it as a command.
func (sw *Switcher) update(code string, index []byte, f func(p []byte)) []byte {
	k := kept[code]
	key := stateKey(code, index)
	p := sw.state[key]
	if p == nil {
		p = make([]byte, k.size)
		copy(p, index)
	}
	f(p)
	sw.state[key] = p
	return Command(code, p)
}

// echo is the switcher's answer to one command: the state packets it
// changed. Commands not handled here change nothing.
func (sw *Switcher) echo(code string, c []byte) [][]byte {
	need := map[string]int{"CPgI": 4, "CPvI": 4, "DCut": 1, "DAut": 1, "CAuS": 4, "CTMx": 2, "CKOn": 3,
		"CDsL": 2, "CDsT": 2, "CDsR": 2, "FtbC": 3, "CClV": 8, "CInL": 32}
	if n, ok := need[code]; !ok || len(c) < n {
		return nil
	}
	switch code {
	case "CPgI":
		return [][]byte{sw.update("PrgI", c[:1], func(p []byte) { copy(p[2:4], c[2:4]) })}
	case "CPvI":
		return [][]byte{sw.update("PrvI", c[:1], func(p []byte) { copy(p[2:4], c[2:4]) })}
	case "DCut", "DAut":
		// Cut (an auto finishes at once): program and preview swap.
		var prog, prev [2]byte
		if p := sw.state[stateKey("PrgI", c[:1])]; p != nil {
			copy(prog[:], p[2:4])
		}
		if p := sw.state[stateKey("PrvI", c[:1])]; p != nil {
			copy(prev[:], p[2:4])
		}
		return [][]byte{
			sw.update("PrgI", c[:1], func(p []byte) { copy(p[2:4], prev[:]) }),
			sw.update("PrvI", c[:1], func(p []byte) { copy(p[2:4], prog[:]) }),
		}
	case "CAuS":
		return [][]byte{sw.update("AuxS", c[1:2], func(p []byte) { copy(p[2:4], c[2:4]) })}
	case "CTMx":
		return [][]byte{sw.update("TMxP", c[:1], func(p []byte) { p[1] = c[1] })}
	case "CKOn":
		return [][]byte{sw.update("KeOn", c[:2], func(p []byte) { p[2] = c[2] })}
	case "CDsL":
		return [][]byte{sw.update("DskS", c[:1], func(p []byte) { p[1] = c[1] })}
	case "CDsT":
		return [][]byte{sw.update("DskP", c[:1], func(p []byte) { p[1] = c[1] })}
	case "CDsR":
		return [][]byte{sw.update("DskP", c[:1], func(p []byte) { p[2] = c[1] })}
	case "FtbC":
		return [][]byte{sw.update("FtbP", c[1:2], func(p []byte) { p[1] = c[2] })}
	case "CClV":
		return [][]byte{sw.update("ColV", c[1:2], func(p []byte) {
			for bit, at := range []int{2, 4, 6} { // hue, saturation, luma
				if c[0]&(1<<bit) != 0 {
					copy(p[at:at+2], c[at:at+2])
				}
			}
		})}
	case "CInL":
		return [][]byte{sw.update("InPr", c[2:4], func(p []byte) {
			if c[0]&0x01 != 0 {
				copy(p[2:22], c[4:24])
			}
			if c[0]&0x02 != 0 {
				copy(p[22:26], c[24:28])
			}
			if c[0]&0x03 != 0 {
				p[26] = 0 // no longer the default name
			}
			if c[0]&0x04 != 0 {
				binary.BigEndian.PutUint16(p[30:], binary.BigEndian.Uint16(c[28:]))
			}
		})}
	}
	return nil
}
