// SPDX-License-Identifier: MIT

package engine

// Profiles: ATEM Software Control's XML, saved and applied by the profile
// package (atem/profile), with the media pool's images beside the XML as
// "ATEM Media Pool/<name>.png", the path the XML's <Still> elements name.
// atemwire leaves the images to its caller: save has
// download_media_pool_images, and apply "expects the caller to supply
// those bytes and push them into the media pool through its own upload
// path after applying the rest" (options.py, restore_media_pool_images).
// The engine is that caller.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strconv"
	"strings"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/atem/profile"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// MediaPoolDir is where a profile's images go, beside the XML.
const MediaPoolDir = "ATEM Media Pool"

// ImageParam is the request file key for the still a profile's <Still
// index> names: "image:<index>".
func ImageParam(index int) string { return "image:" + strconv.Itoa(index) }

func boolParam(name string, def bool, help string) table.Param {
	return table.Param{Name: name, Kind: table.Bool, Default: strconv.FormatBool(def), Help: help}
}

// meParams are the per-M/E sections of the save and restore dialogs.
func meParams(program, preview bool) []table.Param {
	return []table.Param{
		{Name: "mes", Kind: table.String, Default: "all", Help: "the M/Es the M/E sections cover: all, or a list from 1 (1,2)"},
		boolParam("program", program, "each M/E's program source"),
		boolParam("preview", preview, "each M/E's preview source"),
		boolParam("next-transition", true, "each M/E's next-transition layers"),
		boolParam("transition-style", true, "each M/E's transition style and its settings"),
		boolParam("ftb", true, "each M/E's fade to black"),
		{Name: "keyers", Kind: table.String, Default: "all", Help: "the upstream keyers: all, none, or a list from 1 (1,2)"},
	}
}

func profileOperations() []operation {
	save := append([]table.Param{
		{Name: "file", Kind: table.OutFile, Required: true, Help: "the profile XML to write; images go beside it"}},
		meParams(true, true)...)
	save = append(save,
		boolParam("downstream-keys", true, "<DownstreamKeys>"),
		boolParam("color-generators", true, "<ColorGenerators>"),
		boolParam("fairlight", true, "<FairlightAudioMixer>"),
		boolParam("camera-control", true, "<CameraControl>"),
		boolParam("media-pool", true, "<MediaPool>: the stills' names and paths"),
		boolParam("media-pool-images", true, "each used still as a PNG in \""+MediaPoolDir+"\" beside the XML"),
		boolParam("media-players", true, "<MediaPlayers>"),
		boolParam("aux", true, "<Auxiliaries>"),
		boolParam("counters", true, "<Counters>"),
		boolParam("settings", true, "<Settings>, input names included"),
		boolParam("video-mode", true, "<VideoMode>"),
		boolParam("hyperdecks", true, "<HyperDecks>"),
		boolParam("macros", true, "<MacroPool> and <MacroControl>, downloading each macro"))
	restore := append([]table.Param{
		{Name: "file", Kind: table.File, Required: true, Help: "the profile XML; its images are read from beside it"}},
		meParams(false, false)...)
	restore = append(restore,
		boolParam("macros-only", false, "start from nothing but the macros (ApplyOptions.macros_only), then add flags given"),
		boolParam("video-mode", true, "change the video mode if it differs: outputs resync"),
		boolParam("inputs", true, "input names"),
		boolParam("settings-flags", true, "settings flags (fade to black enabled)"),
		boolParam("color-generators", true, "colour generators"),
		boolParam("aux", true, "aux outputs"),
		boolParam("hyperdecks", true, "HyperDeck slots"),
		boolParam("downstream-keys", true, "downstream keyers"),
		boolParam("fly-keyframes", false, "drive each keyer through its saved keyframes to store them (shows on air)"),
		boolParam("media-players", true, "media players"),
		boolParam("audio", true, "the Fairlight mixer: master, strips, EQ, dynamics"),
		boolParam("macros", true, "the macro pool, uploading each macro"),
		boolParam("media-pool-images", true, "upload the stills the XML names, from \""+MediaPoolDir+"\" beside it"),
		boolParam("multiview", false, "no setter exists: noted as skipped"),
		boolParam("camera-control", false, "no setter exists: noted as skipped"),
		boolParam("talkback", false, "no setter exists: noted as skipped"))
	return []operation{
		{entry: table.Entry{Path: "profile save", Summary: "Save the switcher's setup as ATEM Software Control's profile XML, and its media pool images.",
			Params: save}, run: opProfileSave},
		{entry: table.Entry{Path: "profile restore", Summary: "Apply a profile XML to the switcher, and upload the images it names.",
			Destructive: true, Params: restore}, run: opProfileRestore},
		{entry: table.Entry{Path: "profile images", Summary: "Download every used still as a PNG, named as profiles name them.",
			Params: []table.Param{{Name: "dir", Kind: table.Dir, Default: MediaPoolDir, Help: "where to write them"}}},
			run: opProfileImages},
	}
}

// meOptions reads the per-M/E flags into one block per M/E slot.
func (c *call) meOptions() ([]profile.MixEffectOptions, error) {
	mes, err := indexList(c.args.Value("mes").(string), profile.PlatformMaxMEs, "--mes")
	if err != nil {
		return nil, err
	}
	keyers, err := indexList(c.args.Value("keyers").(string), 4, "--keyers")
	if err != nil {
		return nil, err
	}
	var out []profile.MixEffectOptions
	for me := range profile.PlatformMaxMEs {
		if !mes[me] {
			out = append(out, profile.MixEffectOptionsNone())
			continue
		}
		m := profile.MixEffectOptions{Program: c.flag("program"), Preview: c.flag("preview"),
			NextTransition: c.flag("next-transition"), TransitionStyle: c.flag("transition-style"), FadeToBlack: c.flag("ftb")}
		for k := range 4 {
			m.USK = append(m.USK, keyers[k])
		}
		out = append(out, m)
	}
	return out, nil
}

// indexList reads "all", "none" or a comma-separated list of indexes
// counted from 1, up to n, into a set of indexes.
func indexList(s string, n int, flag string) (map[int]bool, error) {
	set := map[int]bool{}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "all":
		for i := range n {
			set[i] = true
		}
		return set, nil
	case "none", "":
		return set, nil
	}
	for _, f := range strings.Split(s, ",") {
		i, err := table.ParseIndex(strings.TrimSpace(f))
		if err != nil || int(i) >= n {
			return nil, codeErr(CodeUsage, "%s %q: give all, none, or numbers 1-%d", flag, s, n)
		}
		set[int(i)] = true
	}
	return set, nil
}

func (c *call) flag(name string) bool { return c.args.Value(name).(bool) }

// ProfileResult is what a save, restore or image download did.
type ProfileResult struct {
	Product   string        `json:"product,omitempty"`
	VideoMode string        `json:"video_mode,omitempty"`
	Bytes     int           `json:"bytes,omitempty"` // the XML's size
	Applied   []string      `json:"applied,omitempty"`
	Skipped   []string      `json:"skipped,omitempty"`
	Errors    []string      `json:"errors,omitempty"`
	Summary   string        `json:"summary,omitempty"`
	Images    []ImageResult `json:"images,omitempty"`
}

// ImageResult is one media pool image saved or restored.
type ImageResult struct {
	Slot  table.Index `json:"slot"`
	Name  string      `json:"name"`
	Path  string      `json:"path"`
	Error string      `json:"error,omitempty"` // why it was skipped
}

func opProfileSave(c *call) (any, bool, error) {
	opts := profile.DefaultSaveOptions()
	var err error
	if opts.MEs, err = c.meOptions(); err != nil {
		return nil, false, err
	}
	opts.DownstreamKeys, opts.ColorGenerators = c.flag("downstream-keys"), c.flag("color-generators")
	opts.Fairlight, opts.CameraControl = c.flag("fairlight"), c.flag("camera-control")
	opts.MediaPoolMetadata, opts.MediaPoolImages = c.flag("media-pool"), c.flag("media-pool-images")
	opts.MediaPlayers, opts.Auxiliaries, opts.Counters = c.flag("media-players"), c.flag("aux"), c.flag("counters")
	opts.Settings, opts.VideoMode = c.flag("settings"), c.flag("video-mode")
	opts.HyperDecks, opts.Macros = c.flag("hyperdecks"), c.flag("macros")
	opts.Logger = c.e.log
	p, err := profile.Save(c.ctx, c.sess, opts)
	if err != nil {
		return nil, false, sessionErr(err)
	}
	xml, err := p.ToXML()
	if err != nil {
		return nil, false, err
	}
	r := ProfileResult{Product: c.state.ProductName(""), VideoMode: videoModeLabel(c.state), Bytes: len(xml)}
	if opts.MediaPoolImages {
		if r.Images, err = c.downloadImages("file", MediaPoolDir+"/"); err != nil {
			return nil, false, err
		}
	}
	if err := c.out.file(FileFrame{Param: "file", Data: []byte(xml)}); err != nil {
		return nil, false, err
	}
	return r, false, nil
}

func opProfileImages(c *call) (any, bool, error) {
	images, err := c.downloadImages("dir", "")
	if err != nil {
		return nil, false, err
	}
	return ProfileResult{Images: images}, true, nil
}

// downloadImages is download_media_pool_images: every used still in slot
// order, as a PNG named after the slot ("Slot<n>" when it has no name),
// sent as a file frame of param under prefix. A slot that fails is
// skipped and reported, as atemwire logs and skips it.
func (c *call) downloadImages(param, prefix string) ([]ImageResult, error) {
	st := c.state
	out := []ImageResult{}
	m := st.VideoMode.Mode
	if m == nil {
		return out, nil
	}
	w, h := m.FrameSize()
	for slot := range st.MediaplayerSlots() {
		info := st.MediaplayerSlotInfo(slot)
		if !info.IsUsed {
			continue
		}
		name := info.Name
		if name == "" {
			name = fmt.Sprintf("Slot%d", slot)
		}
		r := ImageResult{Slot: table.Index(slot), Name: name, Path: prefix + fileName(name, fmt.Sprintf("Slot%d", slot)) + ".png"}
		ctx, cancel := c.transferCtx()
		frame, err := c.sess.DownloadStill(ctx, slot)
		cancel()
		if err == nil {
			var img *image.NRGBA
			if img, err = atem.FrameImage(frame, w, h); err == nil {
				var buf bytes.Buffer
				if err = png.Encode(&buf, img); err == nil {
					err = c.out.file(FileFrame{Param: param, Name: r.Path, Data: buf.Bytes()})
				}
			}
		}
		if err != nil {
			if c.ctx.Err() != nil {
				return nil, codeErr(CodeUnreachable, "%v", c.ctx.Err())
			}
			r.Error = err.Error()
			c.e.log.Warn("media pool image skipped", "slot", slot, "name", name, "err", err)
		}
		out = append(out, r)
	}
	return out, nil
}

func opProfileRestore(c *call) (any, bool, error) {
	p, err := profile.FromXML(c.req.Files["file"])
	if err != nil {
		return nil, false, codeErr(CodeUsage, "--file %s: %v", c.args.Value("file"), err)
	}
	opts := profile.DefaultApplyOptions()
	given := func(name string) bool { _, ok := c.req.Params[name]; return ok }
	if c.flag("macros-only") {
		opts = profile.MacrosOnly()
	}
	set := func(name string, dst *bool) {
		if !c.flag("macros-only") || given(name) {
			*dst = c.flag(name)
		}
	}
	if !c.flag("macros-only") || given("mes") || given("keyers") || given("program") || given("preview") ||
		given("next-transition") || given("transition-style") || given("ftb") {
		if opts.MEs, err = c.meOptions(); err != nil {
			return nil, false, err
		}
	}
	set("video-mode", &opts.RestoreVideoMode)
	set("inputs", &opts.RestoreInputs)
	set("settings-flags", &opts.RestoreSettingsFlags)
	set("color-generators", &opts.RestoreColorGenerators)
	set("aux", &opts.RestoreAux)
	set("hyperdecks", &opts.RestoreHyperDecks)
	set("downstream-keys", &opts.RestoreDownstreamKeys)
	set("fly-keyframes", &opts.RestoreFlyKeyframes)
	set("media-players", &opts.RestoreMediaPlayers)
	set("audio", &opts.RestoreAudio)
	set("macros", &opts.RestoreMacros)
	set("media-pool-images", &opts.RestoreMediaPoolImages)
	set("multiview", &opts.RestoreMultiview)
	set("camera-control", &opts.RestoreCameraControl)
	set("talkback", &opts.RestoreTalkback)
	opts.Logger = c.e.log
	res, applyErr := p.Apply(c.ctx, c.sess, opts)
	r := ProfileResult{Product: p.Product(), VideoMode: p.VideoMode()}
	if res != nil {
		r.Applied, r.Skipped, r.Errors, r.Summary = res.Applied, res.Skipped, res.Errors, res.Summary()
	}
	if applyErr != nil {
		if c.ctx.Err() != nil {
			return nil, false, codeErr(CodeUnreachable, "%v", c.ctx.Err())
		}
		r.Errors = append(r.Errors, "aborted: "+applyErr.Error())
		r.Summary = res.Summary() + ", aborted"
		return r, false, nil
	}
	if opts.RestoreMediaPoolImages {
		r.Images = c.uploadImages(p)
	}
	return r, false, nil
}

// uploadImages pushes each <Still index name path> whose image the CLI
// sent into its slot, after the rest of the profile, as atemwire's
// caller is expected to.
func (c *call) uploadImages(p *profile.Profile) []ImageResult {
	out := []ImageResult{}
	pool := p.Root().Find("MediaPool")
	if pool == nil {
		return out
	}
	stills := pool.FindAll("Stills/Still")
	slices.SortStableFunc(stills, func(a, b *profile.Element) int { return idx(a) - idx(b) })
	for _, s := range stills {
		slot := idx(s) // the XML's index counts from 0, as ASC writes it
		r := ImageResult{Slot: table.Index(slot), Name: s.GetDefault("name", ""), Path: s.GetDefault("path", "")}
		if r.Path == "" {
			continue
		}
		data, ok := c.req.Files[ImageParam(slot)]
		var err error
		switch {
		case slot < 0:
			err = fmt.Errorf("index %q is not a slot", s.GetDefault("index", ""))
		case !ok:
			err = fmt.Errorf("no file at %s", r.Path)
		default:
			var img image.Image
			var format string
			if img, format, err = image.Decode(bytes.NewReader(data)); err == nil {
				ctx, cancel := c.transferCtx()
				err = c.sess.UploadStill(ctx, slot, img, atem.StillOptions{Name: r.Name, Premultiply: format == "png"})
				cancel()
			}
		}
		if err != nil {
			r.Error = err.Error()
			c.e.log.Warn("media pool image not restored", "slot", slot, "path", r.Path, "err", err)
		}
		out = append(out, r)
	}
	return out
}

func idx(e *profile.Element) int {
	n, err := strconv.Atoi(e.GetDefault("index", ""))
	if err != nil {
		return -1
	}
	return n
}
