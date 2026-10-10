// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Parsers for SURFACE.md §2.5 (outputs, devices, system), from bmdwire
// v1.3.0 atemwire/messages/multiviewer.py, supersource.py, hyperdeck.py,
// camera_control.py, recording.py, streaming.py, tally.py, input_video.py,
// system_info.py, lock.py and file_transfer.py.

import (
	"fmt"
	"net"
)

var systemParsers = []parser{
	entry("MvPr", "multiviewer-properties", parseMvPr, func(s *State, f *MultiviewerPropertiesField) string {
		at(&s.Multiviewers, int(f.Index)).Properties = f
		return key("MvPr", f.Index)
	}),
	entry("MvIn", "multiviewer-input", parseMvIn, func(s *State, f *MultiviewerInputField) string {
		mvWindow(s, f.Index, f.Window).Input = f
		return key("MvIn", f.Index, f.Window)
	}),
	entry("VuMC", "multiviewer-vu", parseVuMC, func(s *State, f *MultiviewerVUField) string {
		mvWindow(s, f.Index, f.Window).VU = f
		return key("VuMC", f.Index, f.Window)
	}),
	entry("SaMw", "multiviewer-safe-area", parseSaMw, func(s *State, f *MultiviewerSafeAreaField) string {
		mvWindow(s, f.Index, f.Window).SafeArea = f
		return key("SaMw", f.Index, f.Window)
	}),
	entry("SSrc", "supersource-properties", parseSSrc, func(s *State, f *SupersourcePropertiesField) string {
		at(&s.SuperSources, int(f.Index)).Properties = f
		return key("SSrc", f.Index)
	}),
	entry("SSBP", "supersource-box-properties", parseSSBP, func(s *State, f *SupersourceBoxPropertiesField) string {
		*at(&at(&s.SuperSources, int(f.Index)).Boxes, int(f.Box)) = f
		return key("SSBP", f.Index, f.Box)
	}),
	entry("RXMS", "hyperdeck-settings", parseRXMS, func(s *State, f *HyperdeckSettingsField) string {
		s.HyperDecks[f.Index] = f
		return key("RXMS", f.Index)
	}),
	entry("CCdP", "camera-control-data-packet", parseCCdP, func(s *State, f *CameraControlDataPacketField) string {
		cats := s.CameraControl[f.Destination]
		if cats == nil {
			cats = make(map[uint8]map[uint8]*CameraControlDataPacketField)
			s.CameraControl[f.Destination] = cats
		}
		params := cats[f.Category]
		if params == nil {
			params = make(map[uint8]*CameraControlDataPacketField)
			cats[f.Category] = params
		}
		params[f.Parameter] = f
		return key("CCdP", f.Destination, f.Category, f.Parameter)
	}),
	entry("RTMD", "recording-disk", parseRTMD, func(s *State, f *RecordingDiskField) string {
		s.Recording.Disk = f
		return "RTMD"
	}),
	entry("RMSu", "recording-settings", parseRMSu, func(s *State, f *RecordingSettingsField) string {
		s.Recording.Settings = f
		return "RMSu"
	}),
	entry("RTMS", "recording-status", parseRTMS, func(s *State, f *RecordingStatusField) string {
		s.Recording.Status = f
		return "RTMS"
	}),
	entry("RTMR", "recording-duration", parseRTMR, func(s *State, f *RecordingDurationField) string {
		s.Recording.Duration = f
		return "RTMR"
	}),
	entry("SRSU", "streaming-service", parseSRSU, func(s *State, f *StreamingServiceField) string {
		s.Streaming.Service = f
		return "SRSU"
	}),
	entry("STAB", "streaming-audio-bitrate", parseSTAB, func(s *State, f *StreamingAudioBitrateField) string {
		s.Streaming.AudioBitrate = f
		return "STAB"
	}),
	entry("StRS", "streaming-status", parseStRS, func(s *State, f *StreamingStatusField) string {
		s.Streaming.Status = f
		return "StRS"
	}),
	entry("SRSS", "streaming-stats", parseSRSS, func(s *State, f *StreamingStatsField) string {
		s.Streaming.Stats = f
		return "SRSS"
	}),
	entry("TlIn", "tally-index", parseTlIn, func(s *State, f *TallyIndexField) string {
		s.Tally.Index = f
		return "TlIn"
	}),
	entry("TlSr", "tally-source", parseTlSr, func(s *State, f *TallySourceField) string {
		s.Tally.Source = f
		return "TlSr"
	}),
	entry("VidM", "video-mode", parseVidM, func(s *State, f *VideoModeField) string {
		s.VideoMode.Mode = f
		return "VidM"
	}),
	entry("_VMC", "video-mode-capability", parseVMC, func(s *State, f *VideoModeCapabilityField) string {
		s.VideoMode.Capability = f
		return "_VMC"
	}),
	entry("AiVM", "auto-input-video-mode", parseAiVM, func(s *State, f *AutoInputVideoModeField) string {
		s.VideoMode.AutoInput = f
		return "AiVM"
	}),
	entry("V3sl", "sdi-3g-level", parseV3sl, func(s *State, f *SDI3GLevelField) string {
		s.VideoMode.SDI3GLevel = f
		return "V3sl"
	}),
	entry("_ver", "firmware-version", parseVer, func(s *State, f *FirmwareVersionField) string {
		s.Identity.Firmware = f
		return "_ver"
	}),
	entry("_pin", "product-name", parsePin, func(s *State, f *ProductNameField) string {
		s.Identity.Product = f
		return "_pin"
	}),
	entry("WhoI", "device-identity", parseWhoI, func(s *State, f *DeviceIdentityField) string {
		s.Identity.Device = f
		return "WhoI"
	}),
	entry("_top", "topology", parseTop, func(s *State, f *TopologyField) string {
		s.Topology = f
		return "_top"
	}),
	entry("_MeC", "mixer-effect-config", parseMeC, func(s *State, f *MixerEffectConfigField) string {
		at(&s.MEs, int(f.Index)).Config = f
		return key("_MeC", f.Index)
	}),
	entry("_mpl", "mediaplayer-slots", parseMpl, func(s *State, f *MediaplayerSlotsField) string {
		s.MediaPool.Slots = f
		return "_mpl"
	}),
	entry("Time", "time", parseTime, func(s *State, f *TimeField) string {
		s.Time.Timecode = f
		return "Time"
	}),
	entry("TCCc", "time-config", parseTCCc, func(s *State, f *TimeConfigField) string {
		s.Time.Config = f
		return "TCCc"
	}),
	entry("LKST", "lock-state", parseLKST, func(s *State, f *LockStateField) string {
		// atemwire drops a lock taken and stores a release bare. Both are
		// stored here, per store, so a caller can see another client
		// holding a lock (bmdwire#15).
		s.Locks[f.Store] = f
		return key("LKST", f.Store)
	}),
	// Consumed by atemwire's transfer engine, never stored. The session
	// handles LKOB itself (see Session.onData).
	entry[LockObtainedField]("LKOB", "lock-obtained", parseLKOB, nil),
	entry[FileTransferContinueDataField]("FTCD", "file-transfer-continue-data", parseFTCD, nil),
	entry[FileTransferDataCompleteField]("FTDC", "file-transfer-data-complete", parseFTDC, nil),
	entry[FileTransferDataField]("FTDa", "file-transfer-data", parseFTDa, nil),
	entry[FileTransferErrorField]("FTDE", "file-transfer-error", parseFTDE, nil),
	// *XFC is the TCP proxy's transfer-complete event; no switcher sends it
	// over UDP. atemwire's handler looks up the store's transfer queue: with
	// none it raises KeyError and loses the rest of the packet, with one it
	// pops a real transfer. Here it is dropped unparsed, whatever it holds,
	// so it can do neither (bmdwire#14).
	{code: "*XFC", name: "transfer-complete", parse: func([]byte) (any, error) { return nil, nil }},
}

// mvWindow returns window w of multiviewer mv, growing the tree to reach it.
func mvWindow(s *State, mv, w uint8) *MultiviewerWindow {
	return at(&at(&s.Multiviewers, int(mv)).Windows, int(w))
}

// parseMvPr: the four small-quadrant flags are bits 0-3 of the layout.
func parseMvPr(b []byte) (*MultiviewerPropertiesField, error) {
	if err := atLeast("MvPr", b, 4); err != nil {
		return nil, err
	}
	l := b[1]
	return &MultiviewerPropertiesField{
		Index: b[0], Layout: l, Flip: flag(b, 2), U1: b[3],
		TopLeftSmall: l&0x01 != 0, TopRightSmall: l&0x02 != 0, BottomLeftSmall: l&0x04 != 0, BottomRightSmall: l&0x08 != 0,
	}, nil
}

func parseMvIn(b []byte) (*MultiviewerInputField, error) {
	if err := atLeast("MvIn", b, 6); err != nil {
		return nil, err
	}
	return &MultiviewerInputField{Index: b[0], Window: b[1], Source: u16(b, 2), VU: flag(b, 4), SafeArea: flag(b, 5)}, nil
}

func parseVuMC(b []byte) (*MultiviewerVUField, error) {
	if err := atLeast("VuMC", b, 3); err != nil {
		return nil, err
	}
	return &MultiviewerVUField{Index: b[0], Window: b[1], Enabled: flag(b, 2)}, nil
}

func parseSaMw(b []byte) (*MultiviewerSafeAreaField, error) {
	if err := atLeast("SaMw", b, 3); err != nil {
		return nil, err
	}
	return &MultiviewerSafeAreaField{Index: b[0], Window: b[1], Enabled: flag(b, 2)}, nil
}

// parseSSrc: struct.unpack('>BxH HB? HH ?xxx'), 16 bytes.
func parseSSrc(b []byte) (*SupersourcePropertiesField, error) {
	if err := exactly("SSrc", b, 16); err != nil {
		return nil, err
	}
	return &SupersourcePropertiesField{
		Index: b[0], FillSource: u16(b, 2), KeySource: u16(b, 4), Layer: b[6], Premultiplied: flag(b, 7),
		Clip: u16(b, 8), Gain: u16(b, 10), Inverted: flag(b, 12),
	}, nil
}

// parseSSBP: struct.unpack('>BB?xH hhH ?x HHHH 2x'), 24 bytes.
func parseSSBP(b []byte) (*SupersourceBoxPropertiesField, error) {
	if err := exactly("SSBP", b, 24); err != nil {
		return nil, err
	}
	return &SupersourceBoxPropertiesField{
		Index: b[0], Box: b[1], Enabled: flag(b, 2), Source: u16(b, 4), X: i16(b, 6), Y: i16(b, 8), Size: u16(b, 10),
		Masked: flag(b, 12), MaskTop: u16(b, 14), MaskBottom: u16(b, 16), MaskLeft: u16(b, 18), MaskRight: u16(b, 20),
	}, nil
}

// parseRXMS: slot at 0, IPv4 at 4-7, switcher input at 8.
func parseRXMS(b []byte) (*HyperdeckSettingsField, error) {
	if err := atLeast("RXMS", b, 10); err != nil {
		return nil, err
	}
	ip := net.IP(b[4:8])
	return &HyperdeckSettingsField{
		Index: u16(b, 0), NetworkAddress: ip.String(), Input: u16(b, 8),
		Configured: b[4] != 0 || b[5] != 0 || b[6] != 0 || b[7] != 0,
	}, nil
}

// ccdpCountOverrides is CameraControlDataPacketField._NUM_OVERRIDES:
// (category, parameter) pairs whose element count atemwire fixes.
var ccdpCountOverrides = map[[2]uint8]int{
	{0, 0}: 1, {0, 1}: 0, {0, 2}: 1, {0, 3}: 1, {0, 4}: 1, {0, 6}: 1, {1, 2}: 2,
}

// parseCCdP: destination, category, parameter and data type in bytes 0-3;
// the element count is the sum of bytes 4-11 unless overridden; the data
// starts at byte 16 and is decoded only when the packet is longer than 16.
func parseCCdP(b []byte) (*CameraControlDataPacketField, error) {
	if err := atLeast("CCdP", b, 12); err != nil {
		return nil, err
	}
	f := &CameraControlDataPacketField{Destination: b[0], Category: b[1], Parameter: b[2], Datatype: b[3]}
	n := 0
	for _, c := range b[4:12] {
		n += int(c)
	}
	if o, ok := ccdpCountOverrides[[2]uint8{f.Category, f.Parameter}]; ok {
		n = o
	}
	f.Length = n
	if len(b) <= 16 {
		return f, nil
	}
	size := map[uint8]int{0: 1, 1: 1, 2: 2, 3: 4, 4: 8, 128: 2}[f.Datatype]
	if err := atLeast("CCdP", b, 16+n*size); err != nil {
		return nil, err
	}
	f.Data = []any{}
	if size == 0 {
		// UTF-8 (5) and unknown types: atemwire's format string is empty.
		return f, nil
	}
	for i := range n {
		off := 16 + i*size
		switch f.Datatype {
		case 0:
			f.Data = append(f.Data, b[off] != 0)
		case 1:
			f.Data = append(f.Data, int64(int8(b[off])))
		case 2:
			f.Data = append(f.Data, int64(i16(b, off)))
		case 3:
			f.Data = append(f.Data, int64(i32(b, off)))
		case 4:
			f.Data = append(f.Data, i64(b, off))
		case 128:
			f.Data = append(f.Data, float64(i16(b, off))/2048)
		}
	}
	return f, nil
}

// parseRTMD: the status bits are attached (1), ready (2), recording (3)
// and deleted (5); bit 0 is unmapped in atemwire.
func parseRTMD(b []byte) (*RecordingDiskField, error) {
	if err := atLeast("RTMD", b, 74); err != nil {
		return nil, err
	}
	st := u16(b, 8)
	return &RecordingDiskField{
		Index: u32(b, 0), TimeAvailable: u32(b, 4), Status: st, VolumeName: cstr(b[10:74]),
		IsAttached: st&(1<<1) != 0, IsReady: st&(1<<2) != 0, IsRecording: st&(1<<3) != 0, IsDeleted: st&(1<<5) != 0,
	}, nil
}

// optional is atemwire's "-1 means None".
func optional(v int32) *int32 {
	if v == -1 {
		return nil
	}
	return &v
}

func parseRMSu(b []byte) (*RecordingSettingsField, error) {
	if err := atLeast("RMSu", b, 137); err != nil {
		return nil, err
	}
	return &RecordingSettingsField{
		Filename: cstr(b[0:128]), Disk1: optional(i32(b, 128)), Disk2: optional(i32(b, 132)), RecordInCameras: flag(b, 136),
	}, nil
}

// parseRTMS: status bits recording (0), disk full (2), disk error (3),
// unformatted (4), dropped (5), stopping (7).
func parseRTMS(b []byte) (*RecordingStatusField, error) {
	if err := atLeast("RTMS", b, 8); err != nil {
		return nil, err
	}
	st := u16(b, 0)
	return &RecordingStatusField{
		Status: st, TimeAvailable: optional(i32(b, 4)),
		IsRecording: st&(1<<0) != 0, IsStopping: st&(1<<7) != 0, DiskFull: st&(1<<2) != 0,
		DiskError: st&(1<<3) != 0, DiskUnformatted: st&(1<<4) != 0, HasDropped: st&(1<<5) != 0,
	}, nil
}

func parseRTMR(b []byte) (*RecordingDurationField, error) {
	if err := atLeast("RTMR", b, 5); err != nil {
		return nil, err
	}
	return &RecordingDurationField{Hours: b[0], Minutes: b[1], Seconds: b[2], Frames: b[3], HasDroppedFrames: flag(b, 4)}, nil
}

func parseSRSU(b []byte) (*StreamingServiceField, error) {
	if err := atLeast("SRSU", b, 1096); err != nil {
		return nil, err
	}
	return &StreamingServiceField{
		Name: cstr(b[0:64]), URL: cstr(b[64:576]), Key: cstr(b[576:1088]), Min: u32(b, 1088), Max: u32(b, 1092),
	}, nil
}

func parseSTAB(b []byte) (*StreamingAudioBitrateField, error) {
	if err := atLeast("STAB", b, 8); err != nil {
		return nil, err
	}
	return &StreamingAudioBitrateField{Min: u32(b, 0), Max: u32(b, 4)}, nil
}

func parseStRS(b []byte) (*StreamingStatusField, error) {
	if err := atLeast("StRS", b, 2); err != nil {
		return nil, err
	}
	return &StreamingStatusField{Status: i16(b, 0)}, nil
}

func parseSRSS(b []byte) (*StreamingStatsField, error) {
	if err := atLeast("SRSS", b, 6); err != nil {
		return nil, err
	}
	return &StreamingStatsField{Bitrate: u32(b, 0), Cache: u16(b, 4)}, nil
}

// tallyLamps unpacks a tally byte: bit 0 program, bit 1 preview.
func tallyLamps(t byte) TallyLamps { return TallyLamps{Program: t&1 != 0, Preview: t&2 != 0} }

func parseTlIn(b []byte) (*TallyIndexField, error) {
	if err := atLeast("TlIn", b, 2); err != nil {
		return nil, err
	}
	f := &TallyIndexField{Num: u16(b, 0), Tally: []TallyLamps{}}
	for i := range int(f.Num) {
		if err := atLeast("TlIn", b, 3+i); err != nil {
			return nil, err
		}
		f.Tally = append(f.Tally, tallyLamps(b[2+i]))
	}
	return f, nil
}

func parseTlSr(b []byte) (*TallySourceField, error) {
	if err := atLeast("TlSr", b, 2); err != nil {
		return nil, err
	}
	f := &TallySourceField{Num: u16(b, 0), Tally: make(map[uint16]TallyLamps)}
	for i := range int(f.Num) {
		off := 2 + 3*i
		if err := atLeast("TlSr", b, off+3); err != nil {
			return nil, err
		}
		f.Tally[u16(b, off)] = tallyLamps(b[off+2])
	}
	return f, nil
}

// videoModes is VideoModeField._MODE_TABLE: mode → resolution, interlaced,
// field rate, widescreen.
var videoModes = map[uint8]VideoModeField{
	0: {0, 525, true, 59.94, false}, 1: {1, 625, true, 50, false},
	2: {2, 525, true, 59.94, true}, 3: {3, 625, true, 50, true},
	4: {4, 720, false, 50, true}, 5: {5, 720, false, 59.94, true},
	6: {6, 1080, true, 50, true}, 7: {7, 1080, true, 59.94, true},
	8: {8, 1080, false, 23.98, true}, 9: {9, 1080, false, 24, true},
	10: {10, 1080, false, 25, true}, 11: {11, 1080, false, 29.97, true},
	12: {12, 1080, false, 50, true}, 13: {13, 1080, false, 59.94, true},
	14: {14, 2160, false, 23.98, true}, 15: {15, 2160, false, 24, true},
	16: {16, 2160, false, 25, true}, 17: {17, 2160, false, 29.97, true},
	18: {18, 2160, false, 50, true}, 19: {19, 2160, false, 59.94, true},
	20: {20, 4320, false, 23.98, true}, 21: {21, 4320, false, 24, true},
	22: {22, 4320, false, 25, true}, 23: {23, 4320, false, 29.97, true},
	24: {24, 4320, false, 50, true}, 25: {25, 4320, false, 59.94, true},
	26: {26, 1080, false, 30, true}, 27: {27, 1080, false, 60, true},
	28: {28, 720, false, 60, true}, 29: {29, 1080, true, 60, true},
}

// videoMode is VideoModeField(mode): an unknown mode is an error, as it is
// a ValueError in atemwire (SURFACE.md §5.4 item 3).
func videoMode(mode uint8) (VideoModeField, error) {
	m, ok := videoModes[mode]
	if !ok {
		return VideoModeField{}, fmt.Errorf("atem: unknown resolution code %d, cannot continue", mode)
	}
	return m, nil
}

func parseVidM(b []byte) (*VideoModeField, error) {
	if err := atLeast("VidM", b, 1); err != nil {
		return nil, err
	}
	m, err := videoMode(b[0])
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// parseVMC: a count, then from byte 4 one 13-byte entry per mode (mode,
// 3 pad bytes, multiview and downscale bitfields, reconfigure).
func parseVMC(b []byte) (*VideoModeCapabilityField, error) {
	if err := atLeast("_VMC", b, 2); err != nil {
		return nil, err
	}
	f := &VideoModeCapabilityField{Modes: []VideoModeCapability{}}
	modes := func(bits uint32) ([]VideoModeField, error) {
		out := []VideoModeField{}
		for i := range 32 {
			if bits&(1<<i) != 0 {
				m, err := videoMode(uint8(i))
				if err != nil {
					return nil, err
				}
				out = append(out, m)
			}
		}
		return out, nil
	}
	for i := range int(u16(b, 0)) {
		off := 4 + 13*i
		if err := atLeast("_VMC", b, off+13); err != nil {
			return nil, err
		}
		var c VideoModeCapability
		var err error
		c.ModeNum = b[off]
		if c.Mode, err = videoMode(c.ModeNum); err != nil {
			return nil, err
		}
		if c.Multiview, err = modes(u32(b, off+4)); err != nil {
			return nil, err
		}
		if c.Downscale, err = modes(u32(b, off+8)); err != nil {
			return nil, err
		}
		c.Reconfigure = flag(b, off+12)
		f.Modes = append(f.Modes, c)
	}
	return f, nil
}

func parseAiVM(b []byte) (*AutoInputVideoModeField, error) {
	if err := atLeast("AiVM", b, 2); err != nil {
		return nil, err
	}
	return &AutoInputVideoModeField{Enabled: flag(b, 0), Detected: flag(b, 1)}, nil
}

func parseV3sl(b []byte) (*SDI3GLevelField, error) {
	if err := atLeast("V3sl", b, 1); err != nil {
		return nil, err
	}
	return &SDI3GLevelField{Level: b[0]}, nil
}

func parseVer(b []byte) (*FirmwareVersionField, error) {
	if err := atLeast("_ver", b, 4); err != nil {
		return nil, err
	}
	f := &FirmwareVersionField{Major: u16(b, 0), Minor: u16(b, 2)}
	f.Version = fmt.Sprintf("%d.%d", f.Major, f.Minor)
	return f, nil
}

func parsePin(b []byte) (*ProductNameField, error) {
	if err := atLeast("_pin", b, 41); err != nil {
		return nil, err
	}
	return &ProductNameField{Name: cstr(b[0:40]), Model: b[40]}, nil
}

func parseWhoI(b []byte) (*DeviceIdentityField, error) {
	if err := atLeast("WhoI", b, 176); err != nil {
		return nil, err
	}
	return &DeviceIdentityField{
		DeviceID: cstr(b[0:32]), IP: cstr(b[32:48]), Hostname: cstr(b[48:112]), Name: cstr(b[112:176]),
	}, nil
}

// parseTop: twelve counts, then byte 12, which is 1 when the multiviewer
// is routable.
func parseTop(b []byte) (*TopologyField, error) {
	if err := atLeast("_top", b, 13); err != nil {
		return nil, err
	}
	return &TopologyField{
		MEUnits: b[0], Sources: b[1], DownstreamKeyers: b[2], AuxOutputs: b[3], MixMinusOutputs: b[4],
		MediaPlayers: b[5], Multiviewers: b[6], RS485: b[7], HyperDecks: b[8], DVE: b[9], Stingers: b[10],
		SuperSources: b[11], MultiviewerRoutable: b[12] == 1,
	}, nil
}

func parseMeC(b []byte) (*MixerEffectConfigField, error) {
	if err := atLeast("_MeC", b, 2); err != nil {
		return nil, err
	}
	return &MixerEffectConfigField{Index: b[0], Keyers: b[1]}, nil
}

func parseMpl(b []byte) (*MediaplayerSlotsField, error) {
	if err := atLeast("_mpl", b, 2); err != nil {
		return nil, err
	}
	return &MediaplayerSlotsField{Stills: b[0], Clips: b[1]}, nil
}

func parseTime(b []byte) (*TimeField, error) {
	if err := atLeast("Time", b, 5); err != nil {
		return nil, err
	}
	return &TimeField{Hours: b[0], Minutes: b[1], Seconds: b[2], Frames: b[3], DropFrame: flag(b, 4)}, nil
}

func parseTCCc(b []byte) (*TimeConfigField, error) {
	if err := atLeast("TCCc", b, 1); err != nil {
		return nil, err
	}
	return &TimeConfigField{Mode: b[0]}, nil
}

func parseLKST(b []byte) (*LockStateField, error) {
	if err := atLeast("LKST", b, 4); err != nil {
		return nil, err
	}
	return &LockStateField{Store: u16(b, 0), State: flag(b, 2), U1: b[3]}, nil
}

func parseLKOB(b []byte) (*LockObtainedField, error) {
	if err := atLeast("LKOB", b, 2); err != nil {
		return nil, err
	}
	return &LockObtainedField{Store: u16(b, 0)}, nil
}

func parseFTCD(b []byte) (*FileTransferContinueDataField, error) {
	if err := atLeast("FTCD", b, 10); err != nil {
		return nil, err
	}
	return &FileTransferContinueDataField{Transfer: u16(b, 0), Size: u16(b, 6), Count: u16(b, 8)}, nil
}

func parseFTDC(b []byte) (*FileTransferDataCompleteField, error) {
	if err := atLeast("FTDC", b, 4); err != nil {
		return nil, err
	}
	return &FileTransferDataCompleteField{Transfer: u16(b, 0), U1: b[2], U2: b[3]}, nil
}

// parseFTDa: transfer id and size, then the chunk, cut at the end of the
// payload as Python's slice is.
func parseFTDa(b []byte) (*FileTransferDataField, error) {
	if err := atLeast("FTDa", b, 4); err != nil {
		return nil, err
	}
	f := &FileTransferDataField{Transfer: u16(b, 0), Size: u16(b, 2)}
	f.Data = append([]byte{}, b[4:min(len(b), 4+int(f.Size))]...)
	return f, nil
}

func parseFTDE(b []byte) (*FileTransferErrorField, error) {
	if err := atLeast("FTDE", b, 3); err != nil {
		return nil, err
	}
	return &FileTransferErrorField{Transfer: u16(b, 0), Status: b[2]}, nil
}
