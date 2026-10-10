// SPDX-License-Identifier: MIT

package fakeatem

import "encoding/binary"

// The macro player and recorder, as far as their state shows: MAct runs,
// stops or deletes, MSRc starts a recording and MAct stop-record ends it,
// storing an empty macro under the recording's name. Nothing runs; a
// running macro stays "running" until stopped.

// recording is a macro being recorded.
type recording struct {
	slot              uint16
	name, description string
}

func playStatus(running bool, slot uint16) []byte {
	p := make([]byte, 4)
	if running {
		p[0] = 1
	}
	binary.BigEndian.PutUint16(p[2:], slot)
	return Command("MRPr", p)
}

func recordStatus(rec *recording) []byte {
	p := []byte{0, 0, 0xFF, 0xFF}
	if rec != nil {
		p[0] = 1
		binary.BigEndian.PutUint16(p[2:], rec.slot)
	}
	return Command("MRcS", p)
}

// macroEcho answers MAct and MSRc. sw.mu is held.
func (sw *Switcher) macroEcho(code string, c []byte) [][]byte {
	switch {
	case code == "MAct" && len(c) >= 3:
		slot := binary.BigEndian.Uint16(c)
		switch c[2] {
		case 0, 4: // run, continue
			return [][]byte{playStatus(true, slot)}
		case 1: // stop
			return [][]byte{playStatus(false, 0xFFFF)}
		case 2: // stop recording
			rec := sw.recording
			sw.recording = nil
			if rec == nil {
				return [][]byte{recordStatus(nil)}
			}
			sw.macros[rec.slot] = &item{name: rec.name, description: rec.description}
			return [][]byte{recordStatus(nil), macroProperties(rec.slot, sw.macros[rec.slot])}
		case 5: // delete
			delete(sw.macros, slot)
			return [][]byte{macroProperties(slot, nil)}
		}
	case code == "MSRc" && len(c) >= 6:
		nl, dl := int(binary.BigEndian.Uint16(c[2:])), int(binary.BigEndian.Uint16(c[4:]))
		if len(c) < 6+nl+dl {
			return nil
		}
		sw.recording = &recording{binary.BigEndian.Uint16(c), string(c[6 : 6+nl]), string(c[6+nl : 6+nl+dl])}
		return [][]byte{recordStatus(sw.recording)}
	}
	return nil
}
