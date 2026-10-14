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
	"time"

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
	// FromCache: saved without downloading it for this slot, from the
	// still cache or from another slot holding the same picture (atemwire's
	// from_cache).
	FromCache bool `json:"from_cache,omitempty"`
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
	// Two steps for a person: the setup (the macros downloaded with it),
	// then the stills.
	c.steps(nonEmptySteps("setup", opts.MediaPoolImages && usedStills(c.state) > 0, "stills")...)
	var p *profile.Profile
	err = c.step(0, func() (string, error) {
		var err error
		if p, err = profile.Save(c.ctx, c.sess, opts); err != nil {
			return "", sessionErr(err)
		}
		if opts.Macros {
			return count(usedMacros(c.state), "macro"), nil
		}
		return "", nil
	})
	if err != nil {
		return nil, false, err
	}
	// Each still's <Still path> is the file its PNG is written to.
	pointStillsAt(p, mediaPoolFiles(c.state))
	xml, err := p.ToXML()
	if err != nil {
		return nil, false, err
	}
	r := ProfileResult{Product: c.state.ProductName(""), VideoMode: videoModeLabel(c.state), Bytes: len(xml)}
	if opts.MediaPoolImages && usedStills(c.state) > 0 {
		err := c.step(1, func() (string, error) {
			var err error
			r.Images, err = c.downloadImages("file", MediaPoolDir+"/")
			return imagesDetail(r.Images), err
		})
		if err != nil {
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

// mediaPoolFiles is each used still's file in the media pool folder: the
// one name its PNG and the profile XML's <Still path> both take, so a
// restore (bmdctl's, ATEM Software Control's) finds every slot's picture
// where the XML says. WebATEM 2.1.1 does the same; atemwire's XML names
// the raw slot name and leaves an unnamed slot's path empty, which a
// written file can't always match. The name is the slot's, made safe for
// a file system (fileName), Slot<n> for none. Slots with the same name and
// the same picture (hash) share a file; a different picture under a name
// already taken gets <name>_2 (_3 ...), compared ignoring case as a Mac or
// Windows folder compares names.
func mediaPoolFiles(st atem.State) map[int]string {
	taken := map[string]string{} // lower-cased file → picture
	files := map[int]string{}
	for slot := range st.MediaplayerSlots() {
		info := st.MediaplayerSlotInfo(slot)
		if !info.IsUsed {
			continue
		}
		key := pictureKey(slot, info.Hash)
		base := fileName(info.Name, fmt.Sprintf("Slot%d", slot))
		name := base + ".png"
		for n := 2; ; n++ {
			if k, ok := taken[strings.ToLower(name)]; !ok || k == key {
				break
			}
			name = fmt.Sprintf("%s_%d.png", base, n)
		}
		taken[strings.ToLower(name)] = key
		files[slot] = name
	}
	return files
}

// pictureKey names a slot's picture: its hash, or the slot itself while
// the hash is unknown (empty, or all zero before the switcher has hashed
// it), which no other slot shares.
func pictureKey(slot int, hash string) string {
	if strings.Trim(hash, "0") == "" {
		return fmt.Sprintf("slot %d", slot)
	}
	return strings.ToLower(hash)
}

// pointStillsAt sets each used slot's <Still path> to its file
// (mediaPoolFiles).
func pointStillsAt(p *profile.Profile, files map[int]string) {
	pool := p.Root().Find("MediaPool")
	if pool == nil {
		return
	}
	for _, s := range pool.FindAll("Stills/Still") {
		if f, ok := files[idx(s)]; ok {
			s.Set("path", MediaPoolDir+"/"+f)
		}
	}
}

// downloadImages is download_media_pool_images: every used still in slot
// order, as a PNG named by mediaPoolFiles, sent as a file frame of param
// under prefix. Its cache_get comes first: a picture the still cache
// keeps (engine.Config.Stills), or one already fetched for another slot,
// is no download; each fetched still is kept for next time (cache_put).
// The PNG of each download is encoded while the next one downloads, so
// the transfers run back to back. A slot that fails is skipped and
// reported, as atemwire logs and skips it; another slot with the same
// picture reports the same failure rather than waiting out the same
// transfer again.
func (c *call) downloadImages(param, prefix string) ([]ImageResult, error) {
	st := c.state
	m := st.VideoMode.Mode
	if m == nil {
		return []ImageResult{}, nil
	}
	w, h := m.FrameSize()
	files := mediaPoolFiles(st)
	cache := c.e.cfg.Stills
	type still struct {
		slot            int
		name, hash, key string
	}
	var stills []still
	toFetch := map[string]bool{}
	for slot := range st.MediaplayerSlots() {
		info := st.MediaplayerSlotInfo(slot)
		if !info.IsUsed {
			continue
		}
		name := info.Name
		if name == "" {
			name = fmt.Sprintf("Slot%d", slot)
		}
		s := still{slot, name, info.Hash, pictureKey(slot, info.Hash)}
		stills = append(stills, s)
		if !cache.Has(s.hash) {
			toFetch[s.key] = true
		}
	}
	out := make([]ImageResult, len(stills))
	pngs := map[string][]byte{}   // this save's pictures, by key
	failed := map[string]string{} // pictures whose download failed
	written := map[string]bool{}  // files sent, lower-cased
	send := func(i int, data []byte, fromCache bool) error {
		out[i].FromCache = fromCache
		f := files[stills[i].slot]
		if written[strings.ToLower(f)] {
			return nil
		}
		written[strings.ToLower(f)] = true
		return c.out.file(FileFrame{Param: param, Name: prefix + f, Data: data})
	}
	skip := func(i int, err string) {
		out[i].Error = err
		c.e.log.Warn("media pool image skipped", "slot", stills[i].slot, "name", stills[i].name, "err", err)
	}
	// The last download's PNG, encoding while the next transfer runs, and
	// the slots waiting for it (its own first).
	type encoded struct {
		png []byte
		err error
	}
	var pending struct {
		key, hash string
		waiting   []int
		done      chan encoded
	}
	collect := func() error {
		if pending.done == nil {
			return nil
		}
		r := <-pending.done
		key, hash, waiting := pending.key, pending.hash, pending.waiting
		pending.done, pending.waiting = nil, nil
		if r.err != nil {
			failed[key] = r.err.Error()
			for _, i := range waiting {
				skip(i, failed[key])
			}
			return nil
		}
		pngs[key] = r.png
		if err := cache.Put(hash, r.png); err != nil {
			c.e.log.Warn("media pool image not kept", "err", err)
		}
		for n, i := range waiting {
			if err := send(i, r.png, n > 0); err != nil {
				return err
			}
		}
		return nil
	}
	fetched := 0
	for i, s := range stills {
		out[i] = ImageResult{Slot: table.Index(s.slot), Name: s.name, Path: prefix + files[s.slot]}
		switch data, ok := pngs[s.key]; {
		case pending.done != nil && pending.key == s.key:
			pending.waiting = append(pending.waiting, i)
			continue
		case ok:
			if err := send(i, data, true); err != nil {
				return nil, err
			}
			continue
		case failed[s.key] != "":
			skip(i, failed[s.key])
			continue
		}
		if data, ok := cache.Get(s.hash); ok {
			pngs[s.key] = data
			if err := send(i, data, true); err != nil {
				return nil, err
			}
			continue
		}
		fetched++
		n := len(toFetch)
		ctx, cancel := c.transferCtx()
		base, item := int64(fetched-1)*int64(w*h*4), fmt.Sprintf("still %d of %d", fetched, n)
		ctx = c.withProgress(ctx, func(p atem.Progress) Progress {
			return Progress{Done: base + int64(p.Done), Total: int64(n) * int64(p.Total), Bytes: true, Item: item}
		})
		frame, err := c.sess.DownloadStill(ctx, s.slot)
		cancel()
		if err := collect(); err != nil {
			return nil, err
		}
		if err != nil {
			if c.ctx.Err() != nil {
				return nil, codeErr(CodeUnreachable, "%v", c.ctx.Err())
			}
			failed[s.key] = err.Error()
			skip(i, failed[s.key])
			continue
		}
		done := make(chan encoded, 1)
		pending.key, pending.hash, pending.waiting, pending.done = s.key, s.hash, []int{i}, done
		go func() {
			img, err := atem.FrameImage(frame, w, h)
			var buf bytes.Buffer
			if err == nil {
				err = png.Encode(&buf, img)
			}
			done <- encoded{buf.Bytes(), err}
		}()
	}
	if err := collect(); err != nil {
		return nil, err
	}
	return out, nil
}

func opProfileRestore(c *call) (any, bool, error) {
	p, err := profile.FromXML(c.req.Files["file"])
	if err != nil {
		return nil, false, codeErr(CodeUsage, "%s %s: %v", c.entry.Label("file"), c.args.Value("file"), err)
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
	images := opts.RestoreMediaPoolImages && len(stillsToUpload(p)) > 0
	c.steps(nonEmptySteps("setup", images, "stills")...)
	var res *profile.ApplyResult
	var applyErr error
	c.step(0, func() (string, error) {
		res, applyErr = p.Apply(c.ctx, c.sess, opts)
		if res == nil {
			return "", applyErr
		}
		return res.Summary(), applyErr
	})
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
		start := time.Now()
		if images {
			c.note(Note{Step: &Step{Index: 1}})
		}
		r.Images = c.uploadImages(p)
		if images {
			c.note(Note{Step: &Step{Index: 1, Done: true, Detail: imagesDetail(r.Images), TookMS: time.Since(start).Milliseconds()}})
		}
	}
	return r, false, nil
}

// nonEmptySteps is first, then each later step whose condition holds.
func nonEmptySteps(first string, more bool, then string) []string {
	if more {
		return []string{first, then}
	}
	return []string{first}
}

// usedStills is how many still slots hold a still.
func usedStills(st atem.State) int {
	n := 0
	for slot := range st.MediaplayerSlots() {
		if st.MediaplayerSlotInfo(slot).IsUsed {
			n++
		}
	}
	return n
}

// usedMacros is how many macro slots hold a macro.
func usedMacros(st atem.State) int {
	n := 0
	for _, p := range st.Macros.Properties {
		if p != nil && p.IsUsed {
			n++
		}
	}
	return n
}

// imagesDetail says what an images step moved: "3 stills", "2 stills, 1
// failed", and how many were downloaded when the cache or a shared
// picture saved some: "20 stills, 8 downloaded", "20 stills, none
// downloaded".
func imagesDetail(ims []ImageResult) string {
	ok, failed, kept := 0, 0, 0
	for _, im := range ims {
		switch {
		case im.Error != "":
			failed++
		case im.FromCache:
			ok++
			kept++
		default:
			ok++
		}
	}
	s := count(ok, "still")
	switch {
	case kept == 0:
	case kept == ok:
		s += ", none downloaded"
	default:
		s += fmt.Sprintf(", %d downloaded", ok-kept)
	}
	if failed > 0 {
		s += fmt.Sprintf(", %d failed", failed)
	}
	return s
}

func count(n int, thing string) string { return fmt.Sprintf("%d %s%s", n, thing, plural(n)) }

// stillsToUpload are a profile's <Still> elements that name a file.
func stillsToUpload(p *profile.Profile) []*profile.Element {
	pool := p.Root().Find("MediaPool")
	if pool == nil {
		return nil
	}
	var out []*profile.Element
	for _, s := range pool.FindAll("Stills/Still") {
		if s.GetDefault("path", "") != "" {
			out = append(out, s)
		}
	}
	return out
}

// uploadImages pushes each <Still index name path> whose image the CLI
// sent into its slot, after the rest of the profile, as atemwire's
// caller is expected to.
func (c *call) uploadImages(p *profile.Profile) []ImageResult {
	n, i := len(stillsToUpload(p)), 0
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
		i++
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
				base, item := int64(i-1)*1000, fmt.Sprintf("still %d of %d", i, n)
				ctx = c.withProgress(ctx, func(p atem.Progress) Progress {
					return Progress{Done: base + int64(p.Done)*1000/int64(p.Total), Total: int64(n) * 1000, Item: item}
				})
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
