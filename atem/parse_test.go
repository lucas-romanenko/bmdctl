// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The assertions bmdwire v1.3.0's tests make about parsed state packets,
// ported. Each test names its Python counterpart. TestParsersMatchAtemwire
// (parse_oracle_test.go) covers every parser more broadly.

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

// storeOne stores one packet in a fresh state and returns the change.
func storeOne(t *testing.T, code string, payload []byte) (State, *Change) {
	t.Helper()
	s := newState()
	c, err := s.store(code, payload)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	return s, c
}

// test_fairlight_migration.py::test_mocp_decodes_master_compressor, on the
// live Constellation HD capture.
func TestFixtureMOCP(t *testing.T) {
	s, c := storeOne(t, "MOCP", fixtures.Bytes(t, "audio/MOCP-master-compressor.bin"))
	f := s.Fairlight.MasterCompressor
	if c.Key != "MOCP" || f == nil {
		t.Fatalf("%+v", c)
	}
	if !f.Enabled || f.Threshold != -1500 || f.Ratio != 397 || f.Attack != 140 || f.Hold != 0 || f.Release != 9300 {
		t.Fatalf("%+v", f)
	}
}

// test_fairlight_migration.py::test_amlp_decodes_master_limiter.
func TestFixtureAMLP(t *testing.T) {
	s, _ := storeOne(t, "AMLP", fixtures.Bytes(t, "audio/AMLP-master-limiter.bin"))
	f := s.Fairlight.MasterLimiter
	if !f.Enabled || f.Threshold != -501 || f.Attack != 71 || f.Hold != 0 || f.Release != 9300 {
		t.Fatalf("%+v", f)
	}
}

// test_master_eq_ambp.py::test_ambp_decodes_band5_reference.
func TestFixtureAMBP(t *testing.T) {
	s, c := storeOne(t, "AMBP", fixtures.Bytes(t, "audio/AMBP-band5.bin"))
	f := s.Fairlight.MasterEQBands[5]
	if c.Key != "AMBP:5" || f == nil {
		t.Fatalf("%+v", c)
	}
	if f.BandIndex != 5 || f.BandFrequency != 12900 || f.BandGain != 0 || f.BandQ != 71 {
		t.Fatalf("%+v", f)
	}
}

// test_master_eq_ambp.py::test_six_ambp_packets_persist_as_per_band_dict:
// six bands land as six entries, not one overwritten raw blob.
func TestAMBPKeepsEveryBand(t *testing.T) {
	s := newState()
	qs := []uint16{65, 110, 230, 250, 130, 95}
	for bi, q := range qs {
		b := make([]byte, 20)
		b[0], b[1], b[2], b[3], b[5] = byte(bi), 1, 0x3f, 0x01, 2
		binary.BigEndian.PutUint16(b[10:], uint16(100+bi))
		binary.BigEndian.PutUint16(b[16:], q)
		if _, err := s.store("AMBP", b); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.Fairlight.MasterEQBands) != 6 {
		t.Fatalf("%d bands", len(s.Fairlight.MasterEQBands))
	}
	for bi, q := range qs {
		if f := s.Fairlight.MasterEQBands[uint8(bi)]; f.BandIndex != uint8(bi) || f.BandQ != q {
			t.Fatalf("band %d: %+v", bi, f)
		}
	}
	if _, ok := s.Raw["AMBP"]; ok {
		t.Fatal("AMBP stored raw")
	}
}

// test_fairlight_migration.py::test_fasp_decodes_delay_at_offset_18: a
// decoy at byte 17 must not be read as the delay.
func TestFASPDelayAtOffset18(t *testing.T) {
	b := make([]byte, 52)
	binary.BigEndian.PutUint16(b, 1301)
	b[14], b[15] = 0x01, 0
	b[17] = 0xff
	b[18] = 2
	s, c := storeOne(t, "FASP", b)
	f := s.Fairlight.Strips["1301.0"].Properties
	if c.Key != "FASP:1301.0" || f.Index != 1301 || f.Delay != 2 || f.StripID != "1301.0" {
		t.Fatalf("%+v %+v", c, f)
	}
}

// test_dsl_scale.py: ColV hue, saturation and luma unpack scaled.
func TestColVScale(t *testing.T) {
	colv := func(hue, sat, luma uint16) *ColorGeneratorField {
		b := []byte{0, 0, byte(hue >> 8), byte(hue), byte(sat >> 8), byte(sat), byte(luma >> 8), byte(luma)}
		f, err := parseColV(b)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if f := colv(1805, 0, 0); f.Hue != 180.5 { // test_color_generator_field_hue_unpack
		t.Error(f.Hue)
	}
	if f := colv(0, 500, 0); f.Saturation != 0.5 { // test_color_generator_field_saturation_unpack
		t.Error(f.Saturation)
	}
	if f := colv(0, 0, 750); f.Luma != 0.75 { // test_color_generator_field_luma_unpack
		t.Error(f.Luma)
	}
}

// test_transition_migration.py::test_tdvp_rate_parses_byte2.
func TestTDvPRateAtByte2(t *testing.T) {
	f, err := parseTDvP(append([]byte{0, 40, 75, 5}, make([]byte, 16)...))
	if err != nil {
		t.Fatal(err)
	}
	if f.Rate != 75 || f.LogoRate != 40 || f.Style != 5 {
		t.Fatalf("%+v", f)
	}
}

// test_hyperdeck.py::test_rxms_reader_configured and
// test_rxms_reader_unconfigured_and_missing, at the parsed-packet level.
func TestRXMS(t *testing.T) {
	rxms := func(slot uint16, ip string, input uint16) []byte {
		b := binary.BigEndian.AppendUint16(nil, slot)
		b = append(b, 0, 0)
		b = append(b, net.ParseIP(ip).To4()...)
		b = binary.BigEndian.AppendUint16(b, input)
		return append(b, make([]byte, 10)...)
	}
	s, c := storeOne(t, "RXMS", rxms(0, "192.0.2.10", 4))
	if f := s.HyperDecks[0]; c.Key != "RXMS:0" || f.NetworkAddress != "192.0.2.10" || f.Input != 4 || !f.Configured {
		t.Fatalf("%+v", f)
	}
	s, _ = storeOne(t, "RXMS", rxms(1, "0.0.0.0", 0))
	if f := s.HyperDecks[1]; f.Configured || f.NetworkAddress != "0.0.0.0" {
		t.Fatalf("%+v", f)
	}
}

// test_profile_video_mode.py: every mode parses, and the SD labels without
// their aspect are what atemwire <= 1.2.0 saved for both aspects.
func TestVidMLabels(t *testing.T) {
	label := func(mode byte) string {
		f, err := parseVidM([]byte{mode, 0, 0, 0})
		if err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		l := f.Label()
		for i := range len(l) {
			if l[i] == ' ' {
				return l[:i]
			}
		}
		return l
	}
	for m := range byte(30) {
		label(m)
	}
	if label(0) != "525i59.94" || label(2) != "525i59.94" || label(1) != "625i50" || label(3) != "625i50" {
		t.Fatal(label(0), label(1), label(2), label(3))
	}
	// SURFACE.md §5.4 item 3: an unknown mode is an error.
	if _, err := parseVidM([]byte{30, 0, 0, 0}); err == nil {
		t.Fatal("mode 30 parsed")
	}
}

// test_session_hygiene_atemwire.py (test_ftda_with_no_transfer_is_ignored,
// test_ftde_with_no_transfer_is_ignored, ...): transfer packets with no
// transfer in flight are consumed, never stored.
func TestTransferPacketsAreConsumed(t *testing.T) {
	for _, c := range []struct {
		code    string
		payload []byte
	}{
		{"FTDa", []byte{0, 44, 0, 4, 'D', 'A', 'T', 'A'}},
		{"FTDE", []byte{0, 9, 1, 0}},
		{"FTDC", []byte{0, 44, 1, 2}},
		{"FTCD", make([]byte, 12)},
		{"LKOB", []byte{0, 0, 0, 0}},
	} {
		s := newState()
		ch, err := s.store(c.code, c.payload)
		if err != nil || ch != nil {
			t.Errorf("%s: %+v %v", c.code, ch, err)
		}
		if _, ok := s.Raw[c.code]; ok {
			t.Errorf("%s stored raw", c.code)
		}
	}
}

// test_lock_delivery.py stores a lock release (LKST state 0) and drops a
// lock taken. Here both are stored, per store (bmdwire#15): a release
// as atemwire does, a lock taken where atemwire stores nothing.
func TestLockState(t *testing.T) {
	s, c := storeOne(t, "LKST", []byte{0, 0, 0, 0})
	if c == nil || c.Key != "LKST:0" || s.Locks[0] == nil || s.Locks[0].State {
		t.Fatalf("release not stored: %+v", c)
	}
	s, c = storeOne(t, "LKST", []byte{0, 0, 1, 0})
	if c == nil || c.Key != "LKST:0" || s.Locks[0] == nil || !s.Locks[0].State {
		t.Fatalf("lock taken not stored: %+v", c)
	}
	if _, err := s.store("LKST", []byte{0xff, 0xff, 1, 0}); err != nil || len(s.Locks) != 2 || !s.Locks[0].State || !s.Locks[0xffff].State {
		t.Fatalf("stores overwrite each other: %v %+v", err, s.Locks)
	}
}

// *XFC is dropped unparsed whatever it holds, where atemwire raises
// KeyError and loses the rest of the packet (bmdwire#14).
func TestXFCIsDropped(t *testing.T) {
	for _, payload := range [][]byte{nil, {0, 0, 0, 3, 1, 0, 0, 0}} {
		s := newState()
		if c, err := s.store("*XFC", payload); err != nil || c != nil {
			t.Fatalf("%x: %+v %v", payload, c, err)
		}
	}
}

func TestDecodeReplaceMatchesPython(t *testing.T) {
	// Expected strings from Python 3.12 bytes.decode('utf-8', 'replace').
	for _, c := range []struct{ in, want string }{
		{"\xff\xfeA", "��A"},
		{"\xe2\x82A", "�A"},
		{"\xe2\x82", "�"},
		{"\xf0\x9f\x98", "�"},
		{"\xed\xa0\x80", "���"},
		{"\xc0\xafB", "��B"},
		{"\xf4\x90\x80\x80", "����"},
		{"caf\xc3\xa9", "café"},
		{"\xe0\x80\x80", "���"},
		{"\xf0\x80\x80\x80", "����"},
		{"\xe0\xa0", "�"},
		{"\x80\x80", "��"},
	} {
		if got := decodeReplace([]byte(c.in)); got != c.want {
			t.Errorf("%x: %q, want %q", c.in, got, c.want)
		}
	}
}

// amip is an AMIP payload in atemwire's layout, '>HB 2x B x BB x Hh 2x'
// (16 bytes, ATEM 8.0 firmware on), with pad bytes after it: 4 makes the
// 20 bytes of older firmware. test_audio_legacy.py::_amip.
func amip(index uint16, typ, plug, state uint8, volume uint16, balance int16, pad int) []byte {
	b := binary.BigEndian.AppendUint16(nil, index)
	b = append(b, typ, 0, 0, 0, 0, plug, state, 0)
	b = binary.BigEndian.AppendUint16(b, volume)
	b = binary.BigEndian.AppendUint16(b, uint16(balance))
	return append(b, make([]byte, 2+pad)...)
}

// test_audio_legacy.py::test_amip_parses_either_length: before 8.0
// firmware AMIP is 20 bytes, which ended the connection (bmdwire#49,
// 8fd3ece). Under 16 is still an error, as struct.unpack_from's is.
func TestAMIPEitherLength(t *testing.T) {
	for _, pad := range []int{0, 4} {
		s, c := storeOne(t, "AMIP", amip(3, 0, 2, 2, 65381, -10000, pad))
		f := s.Audio.Inputs["3.0"]
		if c.Key != "AMIP:3.0" || f == nil || f.Index != 3 || f.Type != 0 || f.Plug != 2 || f.State != 2 ||
			f.Volume != 65381 || f.Balance != -10000 || f.PlugName() != "HDMI" {
			t.Errorf("%d bytes: %s %+v", 16+pad, c.Key, f)
		}
	}
	st := newState()
	if _, err := st.store("AMIP", amip(3, 0, 2, 2, 65381, -10000, 0)[:15]); err == nil {
		t.Error("15 bytes parsed")
	}
}

// classicUnity is 2^23, 0 dB on the classic mixer's meter scale.
const classicUnity = 128 * 65536

// amlv is an AMLv payload: the count, master and monitor, the sources'
// u16s padded to 4 bytes, then their four u32s each.
// test_audio_legacy.py::_amlv.
func amlv(master, monitor [4]uint32, sources []uint16, levels [][4]uint32) []byte {
	b := binary.BigEndian.AppendUint16(nil, uint16(len(sources)))
	b = append(b, 0, 0)
	for _, v := range append(master[:], monitor[:]...) {
		b = binary.BigEndian.AppendUint32(b, v)
	}
	for _, s := range sources {
		b = binary.BigEndian.AppendUint16(b, s)
	}
	b = append(b, make([]byte, -len(b)&3)...)
	for _, l := range levels {
		for _, v := range l {
			b = binary.BigEndian.AppendUint32(b, v)
		}
	}
	return b
}

// test_audio_legacy.py::test_amlv_decodes_master_monitor_and_inputs: one
// bare packet carries every input, keyed by source, with an odd count
// padded.
func TestAMLv(t *testing.T) {
	const half = classicUnity / 2 // -6.02 dB
	round2 := func(l [4]float64) [4]float64 {
		for i := range l {
			l[i] = pyRound(l[i], 2)
		}
		return l
	}
	for _, sources := range [][]uint16{{1, 2}, {1, 2, 1001}} {
		levels := make([][4]uint32, len(sources))
		for i := range levels {
			levels[i] = [4]uint32{classicUnity, half, 0, classicUnity * 2}
		}
		s, c := storeOne(t, "AMLv", amlv([4]uint32{classicUnity, classicUnity, classicUnity, classicUnity}, [4]uint32{half, half, 0, 0}, sources, levels))
		lv := s.Audio.Levels
		if c.Key != "AMLv" || lv == nil || int(lv.Count) != len(sources) || lv.Master != [4]float64{} ||
			round2(lv.Monitor) != [4]float64{-6.02, -6.02, -60, -60} || len(lv.Input) != len(sources) {
			t.Fatalf("%v: %s %+v", sources, c.Key, lv)
		}
		for _, src := range sources {
			if got := round2(lv.Input[src]); got != [4]float64{0, -6.02, -60, 6.02} {
				t.Errorf("%v: input %d %v", sources, src, got)
			}
		}
	}
}
