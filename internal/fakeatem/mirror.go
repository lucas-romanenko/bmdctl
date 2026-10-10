// SPDX-License-Identifier: MIT

package fakeatem

import "fmt"

// Mirrored commands: ones the fake confirms without modelling them. It
// answers with the state packet the dump (or a later echo) gave for the
// same key, unchanged, so a session waiting for the command's echo sees
// one arrive. The value proves nothing; tests check what was sent.

// mirror is one mirrored command: the state packet that answers it and
// where the command carries that packet's index bytes.
type mirror struct {
	state string
	at    int // offset of the index in the command
	n     int // index bytes, the same at the start of the state packet
}

var mirrors = map[string]mirror{
	// transitions
	"CTTp": {"TrSS", 1, 1}, "CTPr": {"TrPr", 0, 1}, "CTPs": {"TrPs", 0, 1},
	"CTDp": {"TDpP", 1, 1}, "CTWp": {"TWpP", 2, 1}, "CTDv": {"TDvP", 2, 1}, "CTSt": {"TStP", 2, 1},
	"FEna": {"FEna", 0, 0},
}

// mirrored lists, by state opcode, how many leading bytes index it.
var mirrored = func() map[string]int {
	m := map[string]int{}
	for _, x := range mirrors {
		m[x.state] = x.n
	}
	return m
}()

func mirrorKey(code string, index []byte) string { return fmt.Sprintf("%s:%x", code, index) }

// rememberMirrored stores a state payload the mirrors may send back.
func (sw *Switcher) rememberMirrored(code string, payload []byte) {
	n, ok := mirrored[code]
	if !ok || len(payload) < n {
		return
	}
	sw.mirror[mirrorKey(code, payload[:n])] = append([]byte(nil), payload...)
}

// mirrorEcho answers a mirrored command with its stored state packet.
func (sw *Switcher) mirrorEcho(code string, c []byte) [][]byte {
	m, ok := mirrors[code]
	if !ok || len(c) < m.at+m.n {
		return nil
	}
	p := sw.mirror[mirrorKey(m.state, c[m.at:m.at+m.n])]
	if p == nil {
		return nil
	}
	return [][]byte{Command(m.state, p)}
}
