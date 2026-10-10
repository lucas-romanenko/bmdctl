// SPDX-License-Identifier: MIT

package fakeatem

import (
	"maps"
	"slices"
)

// Mirrored commands: ones the fake confirms without modelling them. It
// answers with the state packet it holds for the same key, unchanged: the
// one a modelled echo (echoes.go) last sent, else the dump's, else a
// zeroed packet of the right size with the key filled in. A session
// waiting for the command's echo sees one arrive; the value proves
// nothing, and tests check what was sent.

// mirror is one mirrored command: the state packet that answers it, the
// byte offsets in the command that address it, the offsets in the state
// packet holding the same bytes, and the size of a zeroed packet.
type mirror struct {
	state   string
	cmdAt   []int
	stateAt []int
	size    int
}

func m(state string, cmdAt, stateAt []int, size int) mirror {
	return mirror{state, cmdAt, stateAt, size}
}

var (
	at0  = []int{0}
	at1  = []int{1}
	at2  = []int{2}
	at01 = []int{0, 1}
	at12 = []int{1, 2}
	at23 = []int{2, 3}
	at45 = []int{4, 5}
	none = []int{}
)

var mirrors = map[string]mirror{
	// transitions
	"CTTp": m("TrSS", at1, at0, 8), "CTPr": m("TrPr", at0, at0, 4), "CTPs": m("TrPs", at0, at0, 8),
	"CTDp": m("TDpP", at1, at0, 4), "CTWp": m("TWpP", at2, at0, 20), "CTDv": m("TDvP", at2, at0, 20),
	"CTSt": m("TStP", at2, at0, 20), "FEna": m("FEna", none, none, 4),
	// upstream keyers
	"CKeF": m("KeBP", at01, at01, 20), "CKeC": m("KeBP", at01, at01, 20), "CKTp": m("KeBP", at12, at01, 20),
	"CKMs": m("KeBP", at12, at01, 20), "CKLm": m("KeLm", at12, at01, 12), "CACK": m("KACk", at23, at01, 24),
	"CACC": m("KACC", at12, at01, 16), "CKPt": m("KePt", at12, at01, 16), "CKDV": m("KeDV", at45, at01, 60),
	// downstream keyers
	"CDsF": m("DskB", at0, at0, 8), "CDsC": m("DskB", at0, at0, 8), "CDsG": m("DskP", at1, at0, 20),
	"CDsM": m("DskP", at1, at0, 20),
	// Fairlight: a strip is its source and the split and subchannel bytes
	"CFSP": m("FASP", strip, stripState, 52), "CICP": m("AICP", strip, stripState, 40),
	"CILP": m("AILP", strip, stripState, 36), "CIXP": m("AIXP", strip, stripState, 40),
	"CEBP": m("AEBP", append(strip, 16), append(stripState, 16), 36), "CMBP": m("AMBP", at1, at0, 20),
	"CFMP": m("FAMP", none, none, 20), "CMCP": m("MOCP", none, none, 24), "CMLP": m("AMLP", none, none, 20),
	// the classic audio mixer
	"CAMI": m("AMIP", at23, at01, 16), "CAMM": m("AMMO", none, none, 8), "CAMm": m("AMmO", none, none, 12),
}

var (
	strip      = []int{2, 3, 14, 15}
	stripState = []int{0, 1, 14, 15}
)

// pick returns the bytes of b at the offsets.
func pick(b []byte, at []int) (string, bool) {
	k := make([]byte, len(at))
	for i, o := range at {
		if o >= len(b) {
			return "", false
		}
		k[i] = b[o]
	}
	return string(k), true
}

// rememberMirrored stores a dump payload the mirrors may send back.
func (sw *Switcher) rememberMirrored(code string, payload []byte) {
	for _, x := range append(slices.Collect(maps.Values(mirrors)), keyframeMirrors[:]...) {
		if x.state != code {
			continue
		}
		if k, ok := pick(payload, x.stateAt); ok {
			sw.mirror[code+":"+k] = append([]byte(nil), payload...)
		}
		return
	}
}

// keyframeMirrors answer SFKF: keyframes A and B are stored (KKFP), the
// other two show in the fly state (KeFS).
var keyframeMirrors = [2]mirror{m("KKFP", []int{0, 1, 2}, []int{0, 1, 2}, 52), m("KeFS", at01, at01, 4)}

// mirrorEcho answers a mirrored command with the state packet for its
// key.
func (sw *Switcher) mirrorEcho(code string, c []byte) [][]byte {
	x, ok := mirrors[code]
	if code == "SFKF" && len(c) > 2 {
		x, ok = keyframeMirrors[1], true
		if c[2] == 1 || c[2] == 2 {
			x = keyframeMirrors[0]
		}
	}
	if !ok {
		return nil
	}
	k, ok := pick(c, x.cmdAt)
	if !ok {
		return nil
	}
	if kp, modelled := kept[x.state]; modelled && kp.index == len(x.stateAt) {
		if p := sw.state[stateKey(x.state, []byte(k))]; p != nil {
			return [][]byte{Command(x.state, p)}
		}
	}
	p := sw.mirror[x.state+":"+k]
	if p == nil {
		p = make([]byte, x.size)
		for i, o := range x.stateAt {
			p[o] = k[i]
		}
		sw.mirror[x.state+":"+k] = p
	}
	return [][]byte{Command(x.state, p)}
}
