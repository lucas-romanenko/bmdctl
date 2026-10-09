// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// The switcher's state: one Go type per state packet atemwire parses
// (bmdwire v1.3.0, atemwire/messages, the Recv classes; SURFACE.md §2), and
// the State tree that groups them.
//
// Naming: a packet type keeps its atemwire Recv class name, "Field" suffix
// included (ProgramBusInputField), with DVE, EQ, SDI and VU written as Go
// initialisms. Commands take the Send class names without "Command", so the
// two never collide. Each packet type's comment names its opcode and the
// mixerstate key atemwire stores it under; each field holds the same value
// and unit as atemwire's attribute of the same (snake_case) name, which is
// also its JSON name.
//
// In the tree, a nil pointer means the packet hasn't arrived, as a missing
// mixerstate key does. Every field that holds packets is tagged with their
// opcode (`atem:"PrgI"`). Indexed packets live where their index puts them:
// per M/E, per keyer, per DSK and so on; packets atemwire stores bare are
// single pointers, and the last one wins.

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
)

// State is everything the switcher has told the session: atemwire's
// AtemProtocol.mixerstate, typed.
type State struct {
	MEs             []ME                               `json:"mes"`
	Aux             []*AuxOutputSourceField            `atem:"AuxS" json:"aux"`
	DSKs            []DSK                              `json:"dsks"`
	FTBEnabled      *FadeToBlackEnabledField           `atem:"FEna" json:"ftb_enabled"`
	ColorGenerators []*ColorGeneratorField             `atem:"ColV" json:"color_generators"`
	Inputs          map[uint16]*InputPropertiesField   `atem:"InPr" json:"inputs"`
	MediaPool       MediaPool                          `json:"media_pool"`
	MediaPlayers    []*MediaplayerSelectedField        `atem:"MPCE" json:"media_players"`
	Macros          Macros                             `json:"macros"`
	Fairlight       Fairlight                          `json:"fairlight"`
	Audio           Audio                              `json:"audio"`
	Multiviewers    []Multiviewer                      `json:"multiviewers"`
	SuperSources    []SuperSource                      `json:"super_sources"`
	HyperDecks      map[uint16]*HyperdeckSettingsField `atem:"RXMS" json:"hyperdecks"`
	// CameraControl is keyed destination, category, parameter.
	CameraControl map[uint8]map[uint8]map[uint8]*CameraControlDataPacketField `atem:"CCdP" json:"camera_control"`
	Recording     Recording                                                   `json:"recording"`
	Streaming     Streaming                                                   `json:"streaming"`
	Tally         Tally                                                       `json:"tally"`
	VideoMode     Video                                                       `json:"video_mode"`
	Topology      *TopologyField                                              `atem:"_top" json:"topology"`
	Identity      Identity                                                    `json:"identity"`
	Time          Clock                                                       `json:"time"`
	LockState     *LockStateField                                             `atem:"LKST" json:"lock_state"`

	// Raw holds every packet atemwire stores as bytes under its code: the
	// codes it knows but doesn't parse (SURFACE.md §5.1), InCm, and any
	// code it doesn't know. The last packet with a code wins.
	Raw map[string][]byte `json:"raw"`
}

// ME is one mix/effect block. mixerstate keys program-bus-input,
// preview-bus-input, transition-*, fade-to-black(-state) and
// mixer-effect-config, each keyed by M/E index; USKs holds the key-*
// entries keyed by M/E and keyer.
type ME struct {
	Program    *ProgramBusInputField   `atem:"PrgI" json:"program"`
	Preview    *PreviewBusInputField   `atem:"PrvI" json:"preview"`
	Transition Transition              `json:"transition"`
	FTB        FTB                     `json:"ftb"`
	Config     *MixerEffectConfigField `atem:"_MeC" json:"config"`
	USKs       []USK                   `json:"usks"`
}

// Transition is one M/E's transition-* mixerstate entries.
type Transition struct {
	Settings *TransitionSettingsField `atem:"TrSS" json:"settings"`
	Preview  *TransitionPreviewField  `atem:"TrPr" json:"preview"`
	Position *TransitionPositionField `atem:"TrPs" json:"position"`
	Mix      *TransitionMixField      `atem:"TMxP" json:"mix"`
	Dip      *TransitionDipField      `atem:"TDpP" json:"dip"`
	Wipe     *TransitionWipeField     `atem:"TWpP" json:"wipe"`
	DVE      *TransitionDVEField      `atem:"TDvP" json:"dve"`
	Stinger  *TransitionStingerField  `atem:"TStP" json:"stinger"`
}

// FTB is one M/E's fade-to-black and fade-to-black-state entries. The
// fade-to-black-enabled packet is bare: see State.FTBEnabled.
type FTB struct {
	State *FadeToBlackStateField `atem:"FtbS" json:"state"`
	Rate  *FadeToBlackField      `atem:"FtbP" json:"rate"`
}

// USK is one upstream keyer: the key-* mixerstate entries keyed by M/E and
// keyer (key-properties-fly-keyframe also by keyframe).
type USK struct {
	OnAir          *KeyOnAirField                               `atem:"KeOn" json:"on_air"`
	Base           *KeyPropertiesBaseField                      `atem:"KeBP" json:"base"`
	Luma           *KeyPropertiesLumaField                      `atem:"KeLm" json:"luma"`
	AdvancedChroma *KeyPropertiesAdvancedChromaField            `atem:"KACk" json:"advanced_chroma"`
	ChromaPicker   *KeyPropertiesAdvancedChromaColorpickerField `atem:"KACC" json:"chroma_picker"`
	Pattern        *KeyPropertiesPatternField                   `atem:"KePt" json:"pattern"`
	DVE            *KeyPropertiesDVEField                       `atem:"KeDV" json:"dve"`
	Fly            *KeyPropertiesFlyField                       `atem:"KeFS" json:"fly"`
	// Keyframes is keyed by keyframe: 1 A, 2 B.
	Keyframes map[uint8]*KeyPropertiesFlyKeyframeField `atem:"KKFP" json:"keyframes"`
}

// DSK is one downstream keyer: dkey-properties-base, dkey-properties and
// dkey-state, keyed by DSK index.
type DSK struct {
	Base       *DkeyPropertiesBaseField `atem:"DskB" json:"base"`
	Properties *DkeyPropertiesField     `atem:"DskP" json:"properties"`
	State      *DkeyStateField          `atem:"DskS" json:"state"`
}

// MediaPool is mediaplayer-slots and mediaplayer-file-info.
type MediaPool struct {
	Slots *MediaplayerSlotsField `atem:"_mpl" json:"slots"`
	// Files is keyed by slot index only; the type byte is skipped, as in
	// atemwire (SURFACE.md §5.4 item 6), so a clip and a still with the
	// same index would share an entry.
	Files map[uint16]*MediaplayerFileInfoField `atem:"MPfe" json:"files"`
}

// Macros is macro-properties (per slot), macro-record-status and
// macro-play-status.
type Macros struct {
	Properties   map[uint16]*MacroPropertiesField `atem:"MPrp" json:"properties"`
	RecordStatus *MacroRecordStatusField          `atem:"MRcS" json:"record_status"`
	PlayStatus   *MacroPlayStatusField            `atem:"MRPr" json:"play_status"`
	// LastRun is the slot of the last macro seen running, or -1: the
	// latch ATEMConnection keeps as last_run_macro_index. It survives a
	// reconnect, as atemwire's does.
	LastRun int `json:"last_run"`
}

// Fairlight is the Fairlight audio mixer's mixerstate entries. Per-strip
// packets are grouped under Strips, keyed by atemwire's strip id,
// "<source>.<subchannel>".
type Fairlight struct {
	Master           *FairlightMasterPropertiesField            `atem:"FAMP" json:"master"`
	MasterEQBands    map[uint8]*AtemMasterEQBandPropertiesField `atem:"AMBP" json:"master_eq_bands"`
	MasterCompressor *FairlightMasterCompressorPropertiesField  `atem:"MOCP" json:"master_compressor"`
	MasterLimiter    *FairlightMasterLimiterPropertiesField     `atem:"AMLP" json:"master_limiter"`
	Strips           map[string]*FairlightStrip                 `json:"strips"`
	Inputs           map[uint16]*FairlightAudioInputField       `atem:"FAIP" json:"inputs"`
	Tally            *FairlightTallyField                       `atem:"FMTl" json:"tally"`
	Headphones       *FairlightHeadphonesField                  `atem:"FMHP" json:"headphones"`
	Solo             *FairlightSoloField                        `atem:"FAMS" json:"solo"`
	StripDelete      *FairlightStripDeleteField                 `atem:"FASD" json:"strip_delete"`
	// MeterLevels is stored bare, as in atemwire: the last strip's
	// levels win (SURFACE.md §5.4 item 6).
	MeterLevels  *FairlightMeterLevelsField  `atem:"FMLv" json:"meter_levels"`
	MasterLevels *FairlightMasterLevelsField `atem:"FDLv" json:"master_levels"`
}

// FairlightStrip is one strip's fairlight-strip-properties,
// atem-eq-band-properties (keyed by band), and compressor, limiter and
// expander entries.
type FairlightStrip struct {
	Properties *FairlightStripPropertiesField       `atem:"FASP" json:"properties"`
	EQBands    map[uint8]*AtemEQBandPropertiesField `atem:"AEBP" json:"eq_bands"`
	Compressor *FairlightCompressorPropertiesField  `atem:"AICP" json:"compressor"`
	Limiter    *FairlightLimiterPropertiesField     `atem:"AILP" json:"limiter"`
	Expander   *FairlightExpanderPropertiesField    `atem:"AIXP" json:"expander"`
}

// Audio is the classic (pre-Fairlight) audio mixer: audio-input (per
// source), audio-mixer-master-properties, audio-mixer-monitor-properties,
// audio-mixer-tally and audio-meter-levels.
type Audio struct {
	Inputs  map[uint16]*AudioInputField       `atem:"AMIP" json:"inputs"`
	Master  *AudioMixerMasterPropertiesField  `atem:"AMMO" json:"master"`
	Monitor *AudioMixerMonitorPropertiesField `atem:"AMmO" json:"monitor"`
	Tally   *AudioMixerTallyField             `atem:"AMTl" json:"tally"`
	Levels  *AudioMeterLevelsField            `atem:"AMLv" json:"levels"`
}

// Multiviewer is multiviewer-properties and, per window,
// multiviewer-input, multiviewer-vu and multiviewer-safe-area.
type Multiviewer struct {
	Properties *MultiviewerPropertiesField `atem:"MvPr" json:"properties"`
	Windows    []MultiviewerWindow         `json:"windows"`
}

// MultiviewerWindow is one multiviewer window.
type MultiviewerWindow struct {
	Input    *MultiviewerInputField    `atem:"MvIn" json:"input"`
	VU       *MultiviewerVUField       `atem:"VuMC" json:"vu"`
	SafeArea *MultiviewerSafeAreaField `atem:"SaMw" json:"safe_area"`
}

// SuperSource is supersource-properties and supersource-box-properties
// (per box).
type SuperSource struct {
	Properties *SupersourcePropertiesField      `atem:"SSrc" json:"properties"`
	Boxes      []*SupersourceBoxPropertiesField `atem:"SSBP" json:"boxes"`
}

// Recording is recording-disk, recording-settings, recording-status and
// recording-duration, all bare.
type Recording struct {
	// Disk is stored bare, as in atemwire: the last disk reported wins.
	Disk     *RecordingDiskField     `atem:"RTMD" json:"disk"`
	Settings *RecordingSettingsField `atem:"RMSu" json:"settings"`
	Status   *RecordingStatusField   `atem:"RTMS" json:"status"`
	Duration *RecordingDurationField `atem:"RTMR" json:"duration"`
}

// Streaming is streaming-service, streaming-audio-bitrate,
// streaming-status and streaming-stats.
type Streaming struct {
	Service      *StreamingServiceField      `atem:"SRSU" json:"service"`
	AudioBitrate *StreamingAudioBitrateField `atem:"STAB" json:"audio_bitrate"`
	Status       *StreamingStatusField       `atem:"StRS" json:"status"`
	Stats        *StreamingStatsField        `atem:"SRSS" json:"stats"`
}

// Tally is tally-index and tally-source.
type Tally struct {
	Index  *TallyIndexField  `atem:"TlIn" json:"index"`
	Source *TallySourceField `atem:"TlSr" json:"source"`
}

// Video is video-mode, video-mode-capability, auto-input-video-mode and
// sdi-3g-level.
type Video struct {
	Mode       *VideoModeField           `atem:"VidM" json:"mode"`
	Capability *VideoModeCapabilityField `atem:"_VMC" json:"capability"`
	AutoInput  *AutoInputVideoModeField  `atem:"AiVM" json:"auto_input"`
	SDI3GLevel *SDI3GLevelField          `atem:"V3sl" json:"sdi_3g_level"`
}

// Identity is firmware-version, product-name and device-identity.
type Identity struct {
	Firmware *FirmwareVersionField `atem:"_ver" json:"firmware"`
	Product  *ProductNameField     `atem:"_pin" json:"product"`
	Device   *DeviceIdentityField  `atem:"WhoI" json:"device"`
}

// Clock is time and time-config.
type Clock struct {
	Timecode *TimeField       `atem:"Time" json:"timecode"`
	Config   *TimeConfigField `atem:"TCCc" json:"config"`
}

// --- §2.2 mixeffect, aux, transition --------------------------------------

// ProgramBusInputField is PrgI, mixerstate['program-bus-input'][me].
type ProgramBusInputField struct {
	Index  uint8  `json:"index"`
	Source uint16 `json:"source"`
}

// PreviewBusInputField is PrvI, mixerstate['preview-bus-input'][me].
type PreviewBusInputField struct {
	Index     uint8  `json:"index"`
	Source    uint16 `json:"source"`
	InProgram bool   `json:"in_program"`
}

// AuxOutputSourceField is AuxS, mixerstate['aux-output-source'][aux]
// (0-based).
type AuxOutputSourceField struct {
	Index  uint8  `json:"index"`
	Source uint16 `json:"source"`
}

// TransitionSettingsField is TrSS, mixerstate['transition-settings'][me].
// Style is 0 Mix, 1 Dip, 2 Wipe, 3 DVE, 4 Stinger. The *_next fields are
// the pending selection.
type TransitionSettingsField struct {
	Index                  uint8 `json:"index"`
	Style                  uint8 `json:"style"`
	StyleNext              uint8 `json:"style_next"`
	NextTransitionBkgd     bool  `json:"next_transition_bkgd"`
	NextTransitionKey1     bool  `json:"next_transition_key1"`
	NextTransitionKey2     bool  `json:"next_transition_key2"`
	NextTransitionKey3     bool  `json:"next_transition_key3"`
	NextTransitionKey4     bool  `json:"next_transition_key4"`
	NextTransitionBkgdNext bool  `json:"next_transition_bkgd_next"`
	NextTransitionKey1Next bool  `json:"next_transition_key1_next"`
	NextTransitionKey2Next bool  `json:"next_transition_key2_next"`
	NextTransitionKey3Next bool  `json:"next_transition_key3_next"`
	NextTransitionKey4Next bool  `json:"next_transition_key4_next"`
}

// TransitionPreviewField is TrPr, mixerstate['transition-preview'][me].
type TransitionPreviewField struct {
	Index   uint8 `json:"index"`
	Enabled bool  `json:"enabled"`
}

// TransitionPositionField is TrPs, mixerstate['transition-position'][me].
// Position is 0-10000 (the T-bar).
type TransitionPositionField struct {
	Index           uint8  `json:"index"`
	InTransition    bool   `json:"in_transition"`
	FramesRemaining uint8  `json:"frames_remaining"`
	Position        uint16 `json:"position"`
}

// TransitionMixField is TMxP, mixerstate['transition-mix'][me]. Rate is in
// frames.
type TransitionMixField struct {
	Index uint8 `json:"index"`
	Rate  uint8 `json:"rate"`
}

// TransitionDipField is TDpP, mixerstate['transition-dip'][me].
type TransitionDipField struct {
	Index  uint8  `json:"index"`
	Rate   uint8  `json:"rate"`
	Source uint16 `json:"source"`
}

// TransitionWipeField is TWpP, mixerstate['transition-wipe'][me]. Width,
// symmetry, softness and the positions are 0-10000; Source is the border
// fill.
type TransitionWipeField struct {
	Index     uint8  `json:"index"`
	Rate      uint8  `json:"rate"`
	Pattern   uint8  `json:"pattern"`
	Width     uint16 `json:"width"`
	Source    uint16 `json:"source"`
	Symmetry  uint16 `json:"symmetry"`
	Softness  uint16 `json:"softness"`
	PositionX uint16 `json:"positionx"`
	PositionY uint16 `json:"positiony"`
	Reverse   bool   `json:"reverse"`
	FlipFlop  bool   `json:"flipflop"`
}

// TransitionDVEField is TDvP, mixerstate['transition-dve'][me]. Key clip
// and gain are 0-1000.
type TransitionDVEField struct {
	Index            uint8  `json:"index"`
	LogoRate         uint8  `json:"logo_rate"`
	Rate             uint8  `json:"rate"`
	Style            uint8  `json:"style"`
	FillSource       uint16 `json:"fill_source"`
	KeySource        uint16 `json:"key_source"`
	KeyEnable        bool   `json:"key_enable"`
	KeyPremultiplied bool   `json:"key_premultiplied"`
	KeyClip          uint16 `json:"key_clip"`
	KeyGain          uint16 `json:"key_gain"`
	KeyInvert        bool   `json:"key_invert"`
	Reverse          bool   `json:"reverse"`
	FlipFlop         bool   `json:"flipflop"`
}

// TransitionStingerField is TStP, mixerstate['transition-stinger'][me].
// MediaPlayer is the media player slot (1-4), not a source id; the times
// are frames.
type TransitionStingerField struct {
	Index            uint8  `json:"index"`
	MediaPlayer      uint8  `json:"mediaplayer"`
	KeyPremultiplied bool   `json:"key_premultiplied"`
	KeyClip          uint16 `json:"key_clip"`
	KeyGain          uint16 `json:"key_gain"`
	KeyInvert        bool   `json:"key_invert"`
	Preroll          uint16 `json:"preroll"`
	Duration         uint16 `json:"duration"`
	TriggerPoint     uint16 `json:"triggerpoint"`
	Rate             uint16 `json:"rate"`
}

// FadeToBlackStateField is FtbS, mixerstate['fade-to-black-state'][me].
type FadeToBlackStateField struct {
	Index           uint8 `json:"index"`
	Done            bool  `json:"done"`
	Transitioning   bool  `json:"transitioning"`
	FramesRemaining uint8 `json:"frames_remaining"`
}

// FadeToBlackField is FtbP, mixerstate['fade-to-black'][me]: the rate in
// frames.
type FadeToBlackField struct {
	Index uint8 `json:"index"`
	Rate  uint8 `json:"rate"`
}

// FadeToBlackEnabledField is FEna, mixerstate['fade-to-black-enabled'],
// bare. Enabled is atemwire's byte heuristic (byte 0 is 0 and byte 1 is 1),
// ambiguous on multi-M/E switchers (SURFACE.md §5.4 item 5).
type FadeToBlackEnabledField struct {
	Enabled  bool `json:"enabled"`
	Disabled bool `json:"disabled"`
}

// --- §2.3 keyers, color, inputs, media, macros ---------------------------

// KeyOnAirField is KeOn, mixerstate['key-on-air'][me][keyer].
type KeyOnAirField struct {
	Index   uint8 `json:"index"`
	Keyer   uint8 `json:"keyer"`
	Enabled bool  `json:"enabled"`
}

// KeyPropertiesBaseField is KeBP, mixerstate['key-properties-base'][me][keyer].
// Type is 0 Luma, 1 Chroma, 2 Pattern, 3 DVE; mask edges are ×1000.
type KeyPropertiesBaseField struct {
	Index       uint8  `json:"index"`
	Keyer       uint8  `json:"keyer"`
	Type        uint8  `json:"type"`
	Enabled     bool   `json:"enabled"`
	FlyEnabled  bool   `json:"fly_enabled"`
	FillSource  uint16 `json:"fill_source"`
	KeySource   uint16 `json:"key_source"`
	MaskEnabled bool   `json:"mask_enabled"`
	MaskTop     int16  `json:"mask_top"`
	MaskBottom  int16  `json:"mask_bottom"`
	MaskLeft    int16  `json:"mask_left"`
	MaskRight   int16  `json:"mask_right"`
}

// KeyPropertiesLumaField is KeLm, mixerstate['key-properties-luma'][me][keyer].
// Clip and gain are 0-1000.
type KeyPropertiesLumaField struct {
	Index         uint8  `json:"index"`
	Keyer         uint8  `json:"keyer"`
	Premultiplied bool   `json:"premultiplied"`
	Clip          uint16 `json:"clip"`
	Gain          uint16 `json:"gain"`
	KeyInverted   bool   `json:"key_inverted"`
}

// KeyPropertiesAdvancedChromaField is KACk,
// mixerstate['key-properties-advanced-chroma'][me][keyer]. Every value is
// ×1000 of what ATEM Software Control shows.
type KeyPropertiesAdvancedChromaField struct {
	Index         uint8  `json:"index"`
	Keyer         uint8  `json:"keyer"`
	Foreground    uint16 `json:"foreground"`
	Background    uint16 `json:"background"`
	KeyEdge       uint16 `json:"key_edge"`
	SpillSuppress uint16 `json:"spill_suppress"`
	FlareSuppress uint16 `json:"flare_suppress"`
	Brightness    int16  `json:"brightness"`
	Contrast      int16  `json:"contrast"`
	Saturation    uint16 `json:"saturation"`
	Red           int16  `json:"red"`
	Green         int16  `json:"green"`
	Blue          int16  `json:"blue"`
}

// KeyPropertiesAdvancedChromaColorpickerField is KACC,
// mixerstate['key-properties-advanced-chroma-colorpicker'][me][keyer].
// X and Y are the cursor position (±16000, ±9000). The sampled colour is
// atemwire's Y, Cb and Cr attributes, renamed SampleY/Cb/Cr because Go and
// JSON can't tell Y from y: Y = (wire − 625) / 8544, Cb and Cr =
// (wire − 5000) / 5000.
type KeyPropertiesAdvancedChromaColorpickerField struct {
	Index    uint8   `json:"index"`
	Keyer    uint8   `json:"keyer"`
	Cursor   bool    `json:"cursor"`
	Preview  bool    `json:"preview"`
	X        int16   `json:"x"`
	Y        int16   `json:"y"`
	Size     uint16  `json:"size"`
	SampleY  float64 `json:"sample_y"`
	SampleCb float64 `json:"sample_cb"`
	SampleCr float64 `json:"sample_cr"`
}

// RGB is atemwire's get_rgb: the sampled colour as BT.709 RGB, each
// clamped to 0-1.
func (f *KeyPropertiesAdvancedChromaColorpickerField) RGB() (r, g, b float64) {
	r = f.SampleY + f.SampleCr*1.5748
	g = f.SampleY + f.SampleCb*-0.1873 + f.SampleCr*-0.4681
	b = f.SampleY + f.SampleCb*1.8556
	clamp := func(v float64) float64 { return max(0, min(1, v)) }
	return clamp(r), clamp(g), clamp(b)
}

// KeyPropertiesPatternField is KePt,
// mixerstate['key-properties-pattern'][me][keyer]. Size, symmetry,
// softness and the positions are 0-10000.
type KeyPropertiesPatternField struct {
	Index     uint8  `json:"index"`
	Keyer     uint8  `json:"keyer"`
	Pattern   uint8  `json:"pattern"`
	Size      uint16 `json:"size"`
	Symmetry  uint16 `json:"symmetry"`
	Softness  uint16 `json:"softness"`
	PositionX uint16 `json:"position_x"`
	PositionY uint16 `json:"position_y"`
	Invert    bool   `json:"invert"`
}

// KeyPropertiesDVEField is KeDV, mixerstate['key-properties-dve'][me][keyer].
// Sizes, positions and mask edges are ×1000, rotation ×10. Border hue is
// degrees (wire ÷ 10), saturation and luma 0-1 (wire ÷ 1000); the write
// side is ×10 for all three (the "SCALE CONTRACT" in atemwire).
type KeyPropertiesDVEField struct {
	Index               uint8   `json:"index"`
	Keyer               uint8   `json:"keyer"`
	SizeX               int32   `json:"size_x"`
	SizeY               int32   `json:"size_y"`
	PosX                int32   `json:"pos_x"`
	PosY                int32   `json:"pos_y"`
	Rotation            int32   `json:"rotation"`
	BorderEnabled       bool    `json:"border_enabled"`
	ShadowEnabled       bool    `json:"shadow_enabled"`
	BorderBevel         uint8   `json:"border_bevel"`
	BorderOuterWidth    uint16  `json:"border_outer_width"`
	BorderInnerWidth    uint16  `json:"border_inner_width"`
	BorderOuterSoftness uint8   `json:"border_outer_softness"`
	BorderInnerSoftness uint8   `json:"border_inner_softness"`
	BorderBevelSoftness uint8   `json:"border_bevel_softness"`
	BorderBevelPosition uint8   `json:"border_bevel_position"`
	BorderOpacity       uint8   `json:"border_opacity"`
	BorderHue           float64 `json:"border_hue"`
	BorderSaturation    float64 `json:"border_saturation"`
	BorderLuma          float64 `json:"border_luma"`
	LightAngle          uint16  `json:"light_angle"`
	LightAltitude       uint8   `json:"light_altitude"`
	MaskEnabled         bool    `json:"mask_enabled"`
	MaskTop             int16   `json:"mask_top"`
	MaskBottom          int16   `json:"mask_bottom"`
	MaskLeft            int16   `json:"mask_left"`
	MaskRight           int16   `json:"mask_right"`
	Rate                uint8   `json:"rate"`
}

// BorderColorRGB is atemwire's get_border_color_rgb.
func (f *KeyPropertiesDVEField) BorderColorRGB() (r, g, b float64) {
	return hlsToRGB(f.BorderHue/360.0, f.BorderLuma, f.BorderSaturation)
}

// KeyPropertiesFlyField is KeFS, mixerstate['key-properties-fly'][me][keyer].
type KeyPropertiesFlyField struct {
	Index              uint8 `json:"index"`
	Keyer              uint8 `json:"keyer"`
	IsASet             bool  `json:"is_a_set"`
	IsBSet             bool  `json:"is_b_set"`
	RunToInfiniteIndex uint8 `json:"run_to_infinite_index"`
	AtKeyframeA        bool  `json:"at_keyframe_a"`
	AtKeyframeB        bool  `json:"at_keyframe_b"`
	AtKeyframeFull     bool  `json:"at_keyframe_full"`
	AtKeyframeInfinite bool  `json:"at_keyframe_infinite"`
}

// KeyPropertiesFlyKeyframeField is KKFP,
// mixerstate['key-properties-fly-keyframe'][me][keyer][keyframe].
// Keyframe is 1 A, 2 B; the units are KeyPropertiesDVEField's.
type KeyPropertiesFlyKeyframeField struct {
	Index               uint8   `json:"index"`
	Keyer               uint8   `json:"keyer"`
	Keyframe            uint8   `json:"keyframe"`
	SizeX               uint32  `json:"size_x"`
	SizeY               uint32  `json:"size_y"`
	PosX                int32   `json:"pos_x"`
	PosY                int32   `json:"pos_y"`
	Rotation            int32   `json:"rotation"`
	BorderOuterWidth    uint16  `json:"border_outer_width"`
	BorderInnerWidth    uint16  `json:"border_inner_width"`
	BorderOuterSoftness uint8   `json:"border_outer_softness"`
	BorderInnerSoftness uint8   `json:"border_inner_softness"`
	BorderBevelSoftness uint8   `json:"border_bevel_softness"`
	BorderBevelPosition uint8   `json:"border_bevel_position"`
	BorderOpacity       uint8   `json:"border_opacity"`
	BorderHue           float64 `json:"border_hue"`
	BorderSaturation    float64 `json:"border_saturation"`
	BorderLuma          float64 `json:"border_luma"`
	LightAngle          uint16  `json:"light_angle"`
	LightAltitude       uint8   `json:"light_altitude"`
	MaskTop             int16   `json:"mask_top"`
	MaskBottom          int16   `json:"mask_bottom"`
	MaskLeft            int16   `json:"mask_left"`
	MaskRight           int16   `json:"mask_right"`
}

// DkeyPropertiesBaseField is DskB, mixerstate['dkey-properties-base'][dsk].
type DkeyPropertiesBaseField struct {
	Index      uint8  `json:"index"`
	FillSource uint16 `json:"fill_source"`
	KeySource  uint16 `json:"key_source"`
}

// DkeyPropertiesField is DskP, mixerstate['dkey-properties'][dsk]. Clip
// and gain are 0-1000, the mask edges ×1000.
type DkeyPropertiesField struct {
	Index         uint8  `json:"index"`
	Tie           bool   `json:"tie"`
	Rate          uint8  `json:"rate"`
	Premultiplied bool   `json:"premultiplied"`
	Clip          uint16 `json:"clip"`
	Gain          uint16 `json:"gain"`
	InvertKey     bool   `json:"invert_key"`
	Masked        bool   `json:"masked"`
	Top           int16  `json:"top"`
	Bottom        int16  `json:"bottom"`
	Left          int16  `json:"left"`
	Right         int16  `json:"right"`
}

// DkeyStateField is DskS, mixerstate['dkey-state'][dsk].
type DkeyStateField struct {
	Index               uint8 `json:"index"`
	OnAir               bool  `json:"on_air"`
	IsTransitioning     bool  `json:"is_transitioning"`
	IsAutoTransitioning bool  `json:"is_autotransitioning"`
	FramesRemaining     uint8 `json:"frames_remaining"`
}

// ColorGeneratorField is ColV, mixerstate['color-generator'][generator].
// Hue is degrees (wire ÷ 10), saturation and luma 0-1 (wire ÷ 1000).
type ColorGeneratorField struct {
	Index      uint8   `json:"index"`
	Hue        float64 `json:"hue"`
	Saturation float64 `json:"saturation"`
	Luma       float64 `json:"luma"`
}

// RGB is atemwire's get_rgb.
func (f *ColorGeneratorField) RGB() (r, g, b float64) {
	return hlsToRGB(f.Hue/360.0, f.Luma, f.Saturation)
}

// InputPropertiesField is InPr, mixerstate['input-properties'][source].
// PortType is the category: 0 external, 1 black, 2 bars, 3 colour, 4 media
// player, 5 media player key, 6 SuperSource, 7 passthrough, 128 M/E output,
// 129 aux output, 130 key mask, 131 multiview output.
type InputPropertiesField struct {
	Index                   uint16 `json:"index"`
	Name                    string `json:"name"`
	ShortName               string `json:"short_name"`
	DefaultName             bool   `json:"default_name"`
	SourcePorts             uint16 `json:"source_ports"`
	ExternalPortType        uint16 `json:"external_port_type"`
	PortType                uint8  `json:"port_type"`
	AvailableAux            bool   `json:"available_aux"`
	AvailableMultiview      bool   `json:"available_multiview"`
	AvailableSupersourceArt bool   `json:"available_supersource_art"`
	AvailableSupersourceBox bool   `json:"available_supersource_box"`
	AvailableKeySource      bool   `json:"available_key_source"`
	AvailableAux1           bool   `json:"available_aux1"`
	AvailableAux2           bool   `json:"available_aux2"`
	AvailableUSB            bool   `json:"available_usb"`
	AvailableME1            bool   `json:"available_me1"`
	AvailableME2            bool   `json:"available_me2"`
	AvailableME3            bool   `json:"available_me3"`
	AvailableME4            bool   `json:"available_me4"`
}

// MD5 is a 16-byte hash. It encodes to JSON and text as hex.
type MD5 [16]byte

// MarshalText returns the hash as lower-case hex.
func (h MD5) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(h[:])), nil }

// UnmarshalText reads lower- or upper-case hex.
func (h *MD5) UnmarshalText(b []byte) error {
	if hex.DecodedLen(len(b)) != len(h) {
		return fmt.Errorf("atem: MD5 needs %d hex digits, got %d", 2*len(h), len(b))
	}
	_, err := hex.Decode(h[:], b)
	return err
}

// MediaplayerFileInfoField is MPfe, mixerstate['mediaplayer-file-info'][slot].
// Type 0 is a still. Name holds atemwire's bytes undecoded.
type MediaplayerFileInfoField struct {
	Type   uint8  `json:"type"`
	Index  uint16 `json:"index"`
	IsUsed bool   `json:"is_used"`
	Hash   MD5    `json:"hash"`
	Name   string `json:"name"`
}

// MediaplayerSelectedField is MPCE, mixerstate['mediaplayer-selected'][player].
// SourceType is 1 still, 2 clip.
type MediaplayerSelectedField struct {
	Index      uint8 `json:"index"`
	SourceType uint8 `json:"source_type"`
	Slot       uint8 `json:"slot"`
}

// MacroPropertiesField is MPrp, mixerstate['macro-properties'][slot]. Name
// and Description hold atemwire's bytes undecoded.
type MacroPropertiesField struct {
	Index       uint16 `json:"index"`
	IsUsed      bool   `json:"is_used"`
	IsInvalid   bool   `json:"is_invalid"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MacroRecordStatusField is MRcS, mixerstate['macro-record-status'], bare.
type MacroRecordStatusField struct {
	IsRecording bool   `json:"is_recording"`
	Index       uint16 `json:"index"`
}

// MacroPlayStatusField is MRPr, mixerstate['macro-play-status'], bare.
// Index is 0xFFFF when idle.
type MacroPlayStatusField struct {
	Running   bool   `json:"running"`
	Waiting   bool   `json:"waiting"`
	IsLooping bool   `json:"is_looping"`
	Index     uint16 `json:"index"`
}

// --- §2.4 audio -----------------------------------------------------------

// FairlightMasterPropertiesField is FAMP,
// mixerstate['fairlight-master-properties'], bare. Gains and volume are
// ×100 dB.
type FairlightMasterPropertiesField struct {
	EQEnable     bool   `json:"eq_enable"`
	EQGain       int16  `json:"eq_gain"`
	DynamicsGain uint16 `json:"dynamics_gain"`
	Volume       int32  `json:"volume"`
	AFV          bool   `json:"afv"`
}

// FairlightStripPropertiesField is FASP,
// mixerstate['fairlight-strip-properties'][strip id]. IsSplit is 0xFF for a
// split-mono strip; State is 1 Off, 2 On, 4 AFV; gains ×100 dB.
type FairlightStripPropertiesField struct {
	Index        uint16 `json:"index"`
	IsSplit      uint8  `json:"is_split"`
	Subchannel   uint8  `json:"subchannel"`
	Delay        uint8  `json:"delay"`
	Gain         int16  `json:"gain"`
	EQEnable     bool   `json:"eq_enable"`
	EQGain       int16  `json:"eq_gain"`
	DynamicsGain uint16 `json:"dynamics_gain"`
	Pan          int16  `json:"pan"`
	Volume       int16  `json:"volume"`
	State        uint8  `json:"state"`
	StripID      string `json:"strip_id"`
}

// AtemEQBandPropertiesField is AEBP,
// mixerstate['atem-eq-band-properties'][strip id][band].
type AtemEQBandPropertiesField struct {
	Index               uint16 `json:"index"`
	IsSplit             uint8  `json:"is_split"`
	Subchannel          uint8  `json:"subchannel"`
	BandIndex           uint8  `json:"band_index"`
	BandEnabled         bool   `json:"band_enabled"`
	BandPossibleFilters uint8  `json:"band_possible_filters"`
	BandFilter          uint8  `json:"band_filter"`
	BandFreqRange       uint8  `json:"band_freq_range"`
	BandFrequency       uint16 `json:"band_frequency"`
	BandGain            int32  `json:"band_gain"`
	BandQ               uint16 `json:"band_q"`
	StripID             string `json:"strip_id"`
}

// AtemMasterEQBandPropertiesField is AMBP,
// mixerstate['atem-master-eq-band-properties'][band].
type AtemMasterEQBandPropertiesField struct {
	BandIndex           uint8  `json:"band_index"`
	BandEnabled         bool   `json:"band_enabled"`
	BandPossibleFilters uint8  `json:"band_possible_filters"`
	BandFilter          uint8  `json:"band_filter"`
	BandFreqRange       uint8  `json:"band_freq_range"`
	BandFrequency       uint16 `json:"band_frequency"`
	BandGain            int32  `json:"band_gain"`
	BandQ               uint16 `json:"band_q"`
}

// FairlightCompressorPropertiesField is AICP,
// mixerstate['fairlight-compressor-properties'][strip id]. Values ×100.
type FairlightCompressorPropertiesField struct {
	Index      uint16 `json:"index"`
	IsSplit    uint8  `json:"is_split"`
	Subchannel uint8  `json:"subchannel"`
	Enabled    bool   `json:"enabled"`
	Threshold  int32  `json:"threshold"`
	Ratio      uint16 `json:"ratio"`
	Attack     int32  `json:"attack"`
	Hold       int32  `json:"hold"`
	Release    int32  `json:"release"`
	StripID    string `json:"strip_id"`
}

// FairlightLimiterPropertiesField is AILP,
// mixerstate['fairlight-limiter-properties'][strip id]. Values ×100.
type FairlightLimiterPropertiesField struct {
	Index      uint16 `json:"index"`
	IsSplit    uint8  `json:"is_split"`
	Subchannel uint8  `json:"subchannel"`
	Enabled    bool   `json:"enabled"`
	Threshold  int32  `json:"threshold"`
	Attack     int32  `json:"attack"`
	Hold       int32  `json:"hold"`
	Release    int32  `json:"release"`
	StripID    string `json:"strip_id"`
}

// FairlightExpanderPropertiesField is AIXP,
// mixerstate['fairlight-expander-properties'][strip id]. Mode is 0
// expander, 1 gate; values ×100.
type FairlightExpanderPropertiesField struct {
	Index      uint16 `json:"index"`
	IsSplit    uint8  `json:"is_split"`
	Subchannel uint8  `json:"subchannel"`
	Enabled    bool   `json:"enabled"`
	Mode       uint8  `json:"mode"`
	Threshold  int32  `json:"threshold"`
	Range      uint16 `json:"range"`
	Ratio      uint16 `json:"ratio"`
	Attack     int32  `json:"attack"`
	Hold       int32  `json:"hold"`
	Release    int32  `json:"release"`
	StripID    string `json:"strip_id"`
}

// FairlightMasterCompressorPropertiesField is MOCP,
// mixerstate['fairlight-master-compressor-properties'], bare. Values ×100.
type FairlightMasterCompressorPropertiesField struct {
	Enabled   bool   `json:"enabled"`
	Threshold int16  `json:"threshold"`
	Ratio     uint16 `json:"ratio"`
	Attack    int32  `json:"attack"`
	Hold      int32  `json:"hold"`
	Release   int32  `json:"release"`
}

// FairlightMasterLimiterPropertiesField is AMLP,
// mixerstate['fairlight-master-limiter-properties'], bare. Values ×100.
type FairlightMasterLimiterPropertiesField struct {
	Enabled   bool  `json:"enabled"`
	Threshold int16 `json:"threshold"`
	Attack    int32 `json:"attack"`
	Hold      int32 `json:"hold"`
	Release   int32 `json:"release"`
}

// FairlightAudioInputField is FAIP, mixerstate['fairlight-audio-input'][source].
// Level is the analog input level (mic or line).
type FairlightAudioInputField struct {
	Index  uint16 `json:"index"`
	Type   uint8  `json:"type"`
	Number uint8  `json:"number"`
	Split  uint8  `json:"split"`
	Level  uint8  `json:"level"`
}

// FairlightTallyField is FMTl, mixerstate['fairlight-tally'], bare. Tally
// is keyed by strip id.
type FairlightTallyField struct {
	Num   uint16          `json:"num"`
	Tally map[string]bool `json:"tally"`
}

// FairlightHeadphonesField is FMHP, mixerstate['fairlight-headphones'], bare.
type FairlightHeadphonesField struct {
	Volume  int32 `json:"volume"`
	Unmuted bool  `json:"unmuted"`
}

// FairlightSoloField is FAMS, mixerstate['fairlight-solo'], bare.
type FairlightSoloField struct {
	Solo       bool  `json:"solo"`
	Channel    uint8 `json:"channel"`
	IsSplitLR  uint8 `json:"is_split_lr"`
	Subchannel uint8 `json:"subchannel"`
}

// FairlightStripDeleteField is FASD, mixerstate['fairlight-strip-delete'],
// bare. atemwire keeps only the raw payload and never acts on it
// (SURFACE.md §5.4 item 7).
type FairlightStripDeleteField struct {
	Raw []byte `json:"raw"`
}

// FairlightMeterLevelsField is FMLv, mixerstate['fairlight-meter-levels'],
// bare: one strip's levels in dB (wire ÷ 100).
type FairlightMeterLevelsField struct {
	IsSplit      uint8      `json:"is_split"`
	Subchannel   uint8      `json:"subchannel"`
	Index        uint16     `json:"index"`
	StripID      string     `json:"strip_id"`
	Input        [4]float64 `json:"input"`
	ExpanderGR   float64    `json:"expander_gr"`
	CompressorGR float64    `json:"compressor_gr"`
	LimiterGR    float64    `json:"limiter_gr"`
	Output       [4]float64 `json:"output"`
	Level        [4]float64 `json:"level"`
}

// FairlightMasterLevelsField is FDLv, mixerstate['fairlight-master-levels'],
// bare, in dB.
type FairlightMasterLevelsField struct {
	Input        [4]float64 `json:"input"`
	CompressorGR float64    `json:"compressor_gr"`
	LimiterGR    float64    `json:"limiter_gr"`
	Output       [4]float64 `json:"output"`
	Level        [4]float64 `json:"level"`
}

// AudioInputField is AMIP, mixerstate['audio-input'][source], on the
// classic audio mixer.
type AudioInputField struct {
	Index   uint16 `json:"index"`
	Type    uint8  `json:"type"`
	Number  uint8  `json:"number"`
	Plug    uint8  `json:"plug"`
	State   uint8  `json:"state"`
	Volume  uint16 `json:"volume"`
	Balance int16  `json:"balance"`
	StripID string `json:"strip_id"`
}

// PlugName is atemwire's plug_name.
func (f *AudioInputField) PlugName() string {
	switch f.Plug {
	case 0:
		return "Internal"
	case 1:
		return "SDI"
	case 2:
		return "HDMI"
	case 3:
		return "Component"
	case 4:
		return "Composite"
	case 5:
		return "SVideo"
	case 32:
		return "XLR"
	case 64:
		return "AES"
	case 128:
		return "RCA"
	}
	return "Analog"
}

// AudioMixerMasterPropertiesField is AMMO,
// mixerstate['audio-mixer-master-properties'], bare.
type AudioMixerMasterPropertiesField struct {
	Volume uint16 `json:"volume"`
	AFV    bool   `json:"afv"`
}

// AudioMixerMonitorPropertiesField is AMmO,
// mixerstate['audio-mixer-monitor-properties'], bare.
type AudioMixerMonitorPropertiesField struct {
	Enabled    bool   `json:"enabled"`
	Volume     uint16 `json:"volume"`
	Mute       bool   `json:"mute"`
	Solo       bool   `json:"solo"`
	SoloSource uint16 `json:"solo_source"`
	Dim        bool   `json:"dim"`
	DimVolume  uint16 `json:"dim_volume"`
}

// AudioMixerTallyField is AMTl, mixerstate['audio-mixer-tally'], bare.
// Tally is keyed "<source>.0".
type AudioMixerTallyField struct {
	Num   uint16          `json:"num"`
	Tally map[string]bool `json:"tally"`
}

// AudioMeterLevelsField is AMLv, mixerstate['audio-meter-levels'], bare.
// Levels are dB; Input is keyed by source.
type AudioMeterLevelsField struct {
	Count   uint16                `json:"count"`
	Master  [4]float64            `json:"master"`
	Monitor [4]float64            `json:"monitor"`
	Input   map[uint16][4]float64 `json:"input"`
}

// --- §2.5 outputs, devices, system ----------------------------------------

// MultiviewerPropertiesField is MvPr, mixerstate['multiviewer-properties'][mv].
type MultiviewerPropertiesField struct {
	Index            uint8 `json:"index"`
	Layout           uint8 `json:"layout"`
	Flip             bool  `json:"flip"`
	U1               uint8 `json:"u1"`
	TopLeftSmall     bool  `json:"top_left_small"`
	TopRightSmall    bool  `json:"top_right_small"`
	BottomLeftSmall  bool  `json:"bottom_left_small"`
	BottomRightSmall bool  `json:"bottom_right_small"`
}

// MultiviewerInputField is MvIn, mixerstate['multiviewer-input'][mv][window].
// VU and SafeArea say whether the window supports them.
type MultiviewerInputField struct {
	Index    uint8  `json:"index"`
	Window   uint8  `json:"window"`
	Source   uint16 `json:"source"`
	VU       bool   `json:"vu"`
	SafeArea bool   `json:"safearea"`
}

// MultiviewerVUField is VuMC, mixerstate['multiviewer-vu'][mv][window].
type MultiviewerVUField struct {
	Index   uint8 `json:"index"`
	Window  uint8 `json:"window"`
	Enabled bool  `json:"enabled"`
}

// MultiviewerSafeAreaField is SaMw,
// mixerstate['multiviewer-safe-area'][mv][window].
type MultiviewerSafeAreaField struct {
	Index   uint8 `json:"index"`
	Window  uint8 `json:"window"`
	Enabled bool  `json:"enabled"`
}

// SupersourcePropertiesField is SSrc, mixerstate['supersource-properties'][ss].
// Layer is 0 background, 1 foreground; clip and gain 0-1000.
type SupersourcePropertiesField struct {
	Index         uint8  `json:"index"`
	FillSource    uint16 `json:"fill_source"`
	KeySource     uint16 `json:"key_source"`
	Layer         uint8  `json:"layer"`
	Premultiplied bool   `json:"premultiplied"`
	Clip          uint16 `json:"clip"`
	Gain          uint16 `json:"gain"`
	Inverted      bool   `json:"inverted"`
}

// SupersourceBoxPropertiesField is SSBP,
// mixerstate['supersource-box-properties'][ss][box].
type SupersourceBoxPropertiesField struct {
	Index      uint8  `json:"index"`
	Box        uint8  `json:"box"`
	Enabled    bool   `json:"enabled"`
	Source     uint16 `json:"source"`
	X          int16  `json:"x"`
	Y          int16  `json:"y"`
	Size       uint16 `json:"size"`
	Masked     bool   `json:"masked"`
	MaskTop    uint16 `json:"mask_top"`
	MaskBottom uint16 `json:"mask_bottom"`
	MaskLeft   uint16 `json:"mask_left"`
	MaskRight  uint16 `json:"mask_right"`
}

// HyperdeckSettingsField is RXMS, mixerstate['hyperdeck-settings'][slot]:
// a deck binding. NetworkAddress is dotted IPv4; Configured is false for
// 0.0.0.0.
type HyperdeckSettingsField struct {
	Index          uint16 `json:"index"`
	NetworkAddress string `json:"network_address"`
	Input          uint16 `json:"input"`
	Configured     bool   `json:"configured"`
}

// CameraControlDataPacketField is CCdP,
// mixerstate['camera-control-data-packet'][destination][category][parameter].
// Data is nil when the packet carries none; otherwise its elements are
// bool, int64 or (fixed16, datatype 128) float64. A UTF-8 packet
// (datatype 5) decodes to an empty Data, as in atemwire.
type CameraControlDataPacketField struct {
	Destination uint8 `json:"destination"`
	Category    uint8 `json:"category"`
	Parameter   uint8 `json:"parameter"`
	Datatype    uint8 `json:"datatype"`
	Length      int   `json:"length"`
	Data        []any `json:"data"`
}

// RecordingDiskField is RTMD, mixerstate['recording-disk'], bare.
type RecordingDiskField struct {
	Index         uint32 `json:"index"`
	TimeAvailable uint32 `json:"time_available"`
	Status        uint16 `json:"status"`
	VolumeName    string `json:"volumename"`
	IsAttached    bool   `json:"is_attached"`
	IsReady       bool   `json:"is_ready"`
	IsRecording   bool   `json:"is_recording"`
	IsDeleted     bool   `json:"is_deleted"`
}

// RecordingSettingsField is RMSu, mixerstate['recording-settings'], bare.
// A disk of -1 on the wire is nil, atemwire's None.
type RecordingSettingsField struct {
	Filename        string `json:"filename"`
	Disk1           *int32 `json:"disk1"`
	Disk2           *int32 `json:"disk2"`
	RecordInCameras bool   `json:"record_in_cameras"`
}

// RecordingStatusField is RTMS, mixerstate['recording-status'], bare.
// TimeAvailable is nil for -1 on the wire.
type RecordingStatusField struct {
	Status          uint16 `json:"status"`
	TimeAvailable   *int32 `json:"time_available"`
	IsRecording     bool   `json:"is_recording"`
	IsStopping      bool   `json:"is_stopping"`
	DiskFull        bool   `json:"disk_full"`
	DiskError       bool   `json:"disk_error"`
	DiskUnformatted bool   `json:"disk_unformatted"`
	HasDropped      bool   `json:"has_dropped"`
}

// RecordingDurationField is RTMR, mixerstate['recording-duration'], bare.
type RecordingDurationField struct {
	Hours            uint8 `json:"hours"`
	Minutes          uint8 `json:"minutes"`
	Seconds          uint8 `json:"seconds"`
	Frames           uint8 `json:"frames"`
	HasDroppedFrames bool  `json:"has_dropped_frames"`
}

// StreamingServiceField is SRSU, mixerstate['streaming-service'], bare.
// Min and Max are bitrates in bit/s.
type StreamingServiceField struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Key  string `json:"key"`
	Min  uint32 `json:"min"`
	Max  uint32 `json:"max"`
}

// StreamingAudioBitrateField is STAB, mixerstate['streaming-audio-bitrate'],
// bare.
type StreamingAudioBitrateField struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

// StreamingStatusField is StRS, mixerstate['streaming-status'], bare.
// Observed values: -1 unknown, 0 nothing, 1 idle, 2 connecting, 4 on air,
// 22 and 36 stopping.
type StreamingStatusField struct {
	Status int16 `json:"status"`
}

// StreamingStatsField is SRSS, mixerstate['streaming-stats'], bare.
type StreamingStatsField struct {
	Bitrate uint32 `json:"bitrate"`
	Cache   uint16 `json:"cache"`
}

// TallyLamps is one tally entry: atemwire's (program, preview) tuple.
type TallyLamps struct {
	Program bool `json:"program"`
	Preview bool `json:"preview"`
}

// TallyIndexField is TlIn, mixerstate['tally-index'], bare: one entry per
// input, in order.
type TallyIndexField struct {
	Num   uint16       `json:"num"`
	Tally []TallyLamps `json:"tally"`
}

// TallySourceField is TlSr, mixerstate['tally-source'], bare, keyed by
// source.
type TallySourceField struct {
	Num   uint16                `json:"num"`
	Tally map[uint16]TallyLamps `json:"tally"`
}

// VideoModeField is VidM, mixerstate['video-mode'], bare. Resolution,
// Interlaced, Rate (the field rate) and Widescreen come from atemwire's
// mode table.
type VideoModeField struct {
	Mode       uint8   `json:"mode"`
	Resolution int     `json:"resolution"`
	Interlaced bool    `json:"interlaced"`
	Rate       float64 `json:"rate"`
	Widescreen bool    `json:"widescreen"`
}

// Label is atemwire's get_label: "1080p59.94", "625i50 16:9".
func (f *VideoModeField) Label() string {
	pi := "p"
	if f.Interlaced {
		pi = "i"
	}
	aspect := ""
	if f.Resolution < 720 {
		aspect = " 4:3"
		if f.Widescreen {
			aspect = " 16:9"
		}
	}
	return strconv.Itoa(f.Resolution) + pi + strconv.FormatFloat(f.Rate, 'f', -1, 64) + aspect
}

// FrameSize is atemwire's get_resolution: the frame in pixels. It returns
// 0, 0 for a resolution atemwire's table lacks (atemwire raises KeyError).
func (f *VideoModeField) FrameSize() (width, height int) {
	switch f.Resolution {
	case 525:
		return 720, 480
	case 625:
		return 720, 576
	case 720:
		return 1280, 720
	case 1080:
		return 1920, 1080
	case 2160:
		return 3840, 2160
	case 4320:
		return 7680, 4320
	}
	return 0, 0
}

// Pixels is atemwire's get_pixels.
func (f *VideoModeField) Pixels() int {
	w, h := f.FrameSize()
	return w * h
}

// VideoModeCapabilityField is _VMC, mixerstate['video-mode-capability'],
// bare: the modes this switcher supports.
type VideoModeCapabilityField struct {
	Modes []VideoModeCapability `json:"modes"`
}

// VideoModeCapability is one entry of VideoModeCapabilityField: a mode,
// the multiviewer and down-converter modes it allows, and whether
// switching to it reconfigures the switcher.
type VideoModeCapability struct {
	ModeNum     uint8            `json:"modenum"`
	Mode        VideoModeField   `json:"mode"`
	Multiview   []VideoModeField `json:"multiview"`
	Downscale   []VideoModeField `json:"downscale"`
	Reconfigure bool             `json:"reconfigure"`
}

// AutoInputVideoModeField is AiVM, mixerstate['auto-input-video-mode'], bare.
type AutoInputVideoModeField struct {
	Enabled  bool `json:"enabled"`
	Detected bool `json:"detected"`
}

// SDI3GLevelField is V3sl, mixerstate['sdi-3g-level'], bare: 0 Level B,
// 1 Level A.
type SDI3GLevelField struct {
	Level uint8 `json:"level"`
}

// FirmwareVersionField is _ver, mixerstate['firmware-version'], bare.
type FirmwareVersionField struct {
	Major   uint16 `json:"major"`
	Minor   uint16 `json:"minor"`
	Version string `json:"version"`
}

// ProductNameField is _pin, mixerstate['product-name'], bare.
type ProductNameField struct {
	Name  string `json:"name"`
	Model uint8  `json:"model"`
}

// DeviceIdentityField is WhoI, mixerstate['device-identity'], bare. Not
// every firmware sends it.
type DeviceIdentityField struct {
	DeviceID string `json:"device_id"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Name     string `json:"name"`
}

// TopologyField is _top, mixerstate['topology'], bare: how many of each
// thing the switcher has.
type TopologyField struct {
	MEUnits             uint8 `json:"me_units"`
	Sources             uint8 `json:"sources"`
	DownstreamKeyers    uint8 `json:"downstream_keyers"`
	AuxOutputs          uint8 `json:"aux_outputs"`
	MixMinusOutputs     uint8 `json:"mixminus_outputs"`
	MediaPlayers        uint8 `json:"mediaplayers"`
	Multiviewers        uint8 `json:"multiviewers"`
	RS485               uint8 `json:"rs485"`
	HyperDecks          uint8 `json:"hyperdecks"`
	DVE                 uint8 `json:"dve"`
	Stingers            uint8 `json:"stingers"`
	SuperSources        uint8 `json:"supersources"`
	MultiviewerRoutable bool  `json:"multiviewer_routable"`
}

// MixerEffectConfigField is _MeC, mixerstate['mixer-effect-config'][me]:
// the number of upstream keyers on the M/E.
type MixerEffectConfigField struct {
	Index  uint8 `json:"index"`
	Keyers uint8 `json:"keyers"`
}

// MediaplayerSlotsField is _mpl, mixerstate['mediaplayer-slots'], bare.
type MediaplayerSlotsField struct {
	Stills uint8 `json:"stills"`
	Clips  uint8 `json:"clips"`
}

// TimeField is Time, mixerstate['time'], bare: the switcher's timecode.
type TimeField struct {
	Hours     uint8 `json:"hours"`
	Minutes   uint8 `json:"minutes"`
	Seconds   uint8 `json:"seconds"`
	Frames    uint8 `json:"frames"`
	DropFrame bool  `json:"dropframe"`
}

// TotalSeconds is atemwire's total_seconds (frames ignored).
func (f *TimeField) TotalSeconds() int {
	return int(f.Seconds) + 60*int(f.Minutes) + 3600*int(f.Hours)
}

// TimeConfigField is TCCc, mixerstate['time-config'], bare: 0 free run,
// 1 time of day.
type TimeConfigField struct {
	Mode uint8 `json:"mode"`
}

// LockStateField is LKST, mixerstate['lock-state'], bare. Only releases are
// stored: atemwire ignores a lock-state packet that reports a lock taken.
type LockStateField struct {
	Store uint16 `json:"store"`
	State bool   `json:"state"`
	U1    uint8  `json:"u1"`
}

// The packets below are consumed by atemwire's transfer engine and never
// stored (SURFACE.md §2.5). The types are here for the transfers phase.

// LockObtainedField is LKOB: the session was granted a store lock.
type LockObtainedField struct {
	Store uint16 `json:"store"`
}

// FileTransferContinueDataField is FTCD: an upload chunk budget.
type FileTransferContinueDataField struct {
	Transfer uint16 `json:"transfer"`
	Size     uint16 `json:"size"`
	Count    uint16 `json:"count"`
}

// FileTransferDataCompleteField is FTDC: a transfer finished.
type FileTransferDataCompleteField struct {
	Transfer uint16 `json:"transfer"`
	U1       uint8  `json:"u1"`
	U2       uint8  `json:"u2"`
}

// FileTransferDataField is FTDa: one downloaded chunk.
type FileTransferDataField struct {
	Transfer uint16 `json:"transfer"`
	Size     uint16 `json:"size"`
	Data     []byte `json:"data"`
}

// FileTransferErrorField is FTDE: 1 try again, 5 no lock, 6 another
// session collided; anything else is fatal for the transfer.
type FileTransferErrorField struct {
	Transfer uint16 `json:"transfer"`
	Status   uint8  `json:"status"`
}

// TransferCompleteField is *XFC, which only atemwire's (unshipped) TCP
// proxy transport sends.
type TransferCompleteField struct {
	Store  uint16 `json:"store"`
	Slot   uint16 `json:"slot"`
	Upload bool   `json:"upload"`
}

// --- helpers ---------------------------------------------------------------

func newState() State {
	return State{
		Inputs:        make(map[uint16]*InputPropertiesField),
		HyperDecks:    make(map[uint16]*HyperdeckSettingsField),
		CameraControl: make(map[uint8]map[uint8]map[uint8]*CameraControlDataPacketField),
		MediaPool:     MediaPool{Files: make(map[uint16]*MediaplayerFileInfoField)},
		Macros:        Macros{Properties: make(map[uint16]*MacroPropertiesField), LastRun: -1},
		Fairlight: Fairlight{
			MasterEQBands: make(map[uint8]*AtemMasterEQBandPropertiesField),
			Strips:        make(map[string]*FairlightStrip),
			Inputs:        make(map[uint16]*FairlightAudioInputField),
		},
		Audio: Audio{Inputs: make(map[uint16]*AudioInputField)},
		Raw:   make(map[string][]byte),
	}
}

// clone returns a deep copy of s: nothing in it is shared with s.
func (s State) clone() State { return deepCopy(s) }

// hasVideoMode reports whether the switcher has sent its video mode, the
// second half of atemwire's wait_ready condition.
func (s State) hasVideoMode() bool {
	if s.VideoMode.Mode != nil {
		return true
	}
	_, ok := s.Raw["VidM"]
	return ok
}

// hlsToRGB is Python's colorsys.hls_to_rgb, which atemwire's get_rgb
// methods use.
func hlsToRGB(h, l, s float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var m2 float64
	if l <= 0.5 {
		m2 = l * (1.0 + s)
	} else {
		m2 = l + s - (l * s)
	}
	m1 := 2.0*l - m2
	return hlsV(m1, m2, h+1.0/3.0), hlsV(m1, m2, h), hlsV(m1, m2, h-1.0/3.0)
}

func hlsV(m1, m2, hue float64) float64 {
	hue = pyMod(hue, 1.0)
	switch {
	case hue < 1.0/6.0:
		return m1 + (m2-m1)*hue*6.0
	case hue < 0.5:
		return m2
	case hue < 2.0/3.0:
		return m1 + (m2-m1)*(2.0/3.0-hue)*6.0
	}
	return m1
}

// pyMod is Python's float %, whose result takes the divisor's sign.
func pyMod(a, b float64) float64 {
	m := math.Mod(a, b)
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
