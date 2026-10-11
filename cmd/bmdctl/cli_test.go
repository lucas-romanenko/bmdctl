// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/fakeatem"
	"github.com/lucas-romanenko/bmdctl/internal/fixtures"
)

var update = flag.Bool("update", false, "rewrite docs/COMMANDS.md and testdata/terminal's golden files")

// docs/COMMANDS.md is generated from the command table.
func TestCommandsDoc(t *testing.T) {
	reg, err := engine.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := commandsDoc(reg)
	path := "../../docs/COMMANDS.md"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatal("docs/COMMANDS.md is stale: run go test ./cmd/bmdctl -run TestCommandsDoc -update")
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI("version")
	if code != 0 || !strings.HasPrefix(out, "bmdctl ") {
		t.Fatalf("%d %q", code, out)
	}
	version, commit, date = "v1.2.3", "abc1234", "2026-10-09"
	defer func() { version, commit, date = "", "", "" }()
	if _, out, _ := runCLI("version"); out != "bmdctl v1.2.3 (abc1234, 2026-10-09)\n" {
		t.Fatalf("%q", out)
	}
}

// The commit people see: GoReleaser's, else the build info's, short, and
// marked when the tree had changes.
func TestShortCommit(t *testing.T) {
	rev := func(modified string) []debug.BuildSetting {
		return []debug.BuildSetting{{Key: "vcs.revision", Value: "274d6a26df98f00d"}, {Key: "vcs.modified", Value: modified}}
	}
	for _, c := range []struct {
		ldflag   string
		settings []debug.BuildSetting
		want     string
	}{
		{"79a743fc4e41f30c", rev("true"), "79a743f"},
		{"", rev("false"), "274d6a2"},
		{"", rev("true"), "274d6a2+dirty"},
		{"", nil, ""},
	} {
		if got := shortCommit(c.ldflag, c.settings); got != c.want {
			t.Errorf("%q %v: %q, want %q", c.ldflag, c.settings, got, c.want)
		}
	}
}

// No parameter may share a name with a global flag, which the parser
// takes out of the line wherever it is.
func TestNoParameterShadowsAGlobalFlag(t *testing.T) {
	reg, _ := engine.NewRegistry()
	for _, d := range reg.Devices() {
		for _, e := range reg.Entries(d) {
			for _, p := range e.Params {
				if _, ok := globalFlags[p.Name]; ok {
					t.Errorf("%s %s --%s is a global flag", d, e.Path, p.Name)
				}
			}
		}
	}
}

// --help works for the device, every group and every command, with or
// without the device word.
func TestHelpAtEveryLevel(t *testing.T) {
	reg, _ := engine.NewRegistry()
	paths := map[string]bool{"": true}
	for _, e := range reg.Entries("atem") {
		w := e.Words()
		for i := 1; i <= len(w); i++ {
			paths[strings.Join(w[:i], " ")] = true
		}
	}
	for p := range paths {
		for _, dev := range [][]string{{"atem"}, nil} {
			args := append(append(dev, strings.Fields(p)...), "--help")
			if len(args) == 1 {
				continue // bare --help is the top level
			}
			code, out, errOut := runCLI(args...)
			if code != 0 || errOut != "" || !strings.HasPrefix(out, "usage: bmdctl [atem]") {
				t.Errorf("%q: %d %q %q", args, code, out, errOut)
			}
		}
	}
	if code, out, _ := runCLI("atem", "--help"); code != 0 || !strings.Contains(out, "--dry-run") || !strings.Contains(out, "--host H") || !strings.Contains(out, "transition ...") {
		t.Errorf("atem --help:\n%s", out)
	}
	if code, out, _ := runCLI("transition", "wipe", "-h"); code != 0 || !strings.Contains(out, "--width") ||
		!strings.Contains(out, "CircleIris=7") || !strings.Contains(out, "Echo: TWpP:<me>") ||
		!strings.Contains(out, "--me counts from 1, as ATEM Software Control does") || !strings.Contains(out, "uint, 1-256") {
		t.Errorf("wipe --help:\n%s", out)
	}
	if code, out, _ := runCLI("--help"); code != 0 || !strings.Contains(out, "atem") || !strings.Contains(out, "Exit status") {
		t.Errorf("--help: %d %q", code, out)
	}
}

// --dry-run prints the bytes and sends nothing. Expected bytes are the
// ones bmdwire v1.3.0's tests pin, with the 8-byte header in front. --me
// counts from 1 here.
func TestDryRun(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"program", "5"}, "000c000043506749" + "00000005"},
		{[]string{"program", "3010", "--me", "2"}, "000c000043506749" + "01000bc2"},
		// Aliases need no switcher: input 5, media player 1, aux 2.
		{[]string{"program", "cam5"}, "000c000043506749" + "00000005"},
		{[]string{"program", "mp1", "--me", "2"}, "000c000043506749" + "01000bc2"},
		{[]string{"aux", "1", "aux2"}, "000c000043417553" + "01001f42"},
		{[]string{"transition", "preview", "true", "--me", "2"}, "000c000043545072" + "01010000"},
		{[]string{"transition", "preview", "on", "--me", "2"}, "000c000043545072" + "01010000"},
		{[]string{"tbar", "20000"}, "000c000043545073" + "00002710"},
		{[]string{"multiviewer", "input", "7", "3010", "--multiviewer", "1"}, "000c0000434d7649" + "00060bc2"},
		{[]string{"auto-video-mode", "true"}, "000c00004169564d" + "01000000"},
		{[]string{"streaming", "audio-bitrate", "128000", "128000"}, "0010000053544142" + "0001f4000001f400"},
		{[]string{"time-of-day", "--time", "2026-09-11T12:00:00Z"}, "0010000053546f44" + "6aa3ed4000000000"},
		{[]string{"camera", "2", "8", "1", "--type", "fixed16", "--data", "0.5,-1.0"},
			"0020000043436d64" + "020801008000000000020000000000000400f80000000000"},
		{[]string{"transition", "mix", "1:00"}, "000c00004354 4d78 0019 0000"},
		{[]string{"--fps", "30", "transition", "mix", "1:00"}, "000c000043544d78001e0000"},
		{[]string{"classic-audio", "input", "1", "--balance", "-500"}, "0014000043414d49" + "0400000100000000fe0c0000"},
		// Indexes are typed from 1 and sent from 0.
		{[]string{"usk", "2", "on-air", "on"}, "000c0000434b4f6e" + "00010100"},
		{[]string{"dsk", "on-air", "on", "--dsk", "2"}, "000c000043447 34c" + "01010000"},
		{[]string{"aux", "2", "2001"}, "000c000043417553" + "010107d1"},
		{[]string{"media", "player", "2", "--still", "6"}, "001000004d505353" + "0301010500000000"},
		{[]string{"macro", "action", "run", "--slot", "4"}, "000c00004d416374" + "00030000"},
		{[]string{"macro", "action", "stop"}, "000c00004d416374" + "ffff0100"},
		{[]string{"audio", "eq", "1", "1", "--channel", "-1", "--q", "2", "-q"}, ""},
		{[]string{"supersource", "box", "1", "--y", "-200", "-y"}, "0020000043534250" + "00080000000000000000ff38000000000000000000000000"},
	} {
		for _, dev := range [][]string{{"atem"}, nil} {
			args := append(append(dev, "--dry-run"), c.args...)
			code, out, errOut := runCLI(args...)
			want := strings.ReplaceAll(c.want, " ", "") + "\n"
			if c.want == "" {
				want = "" // -q
			}
			if code != 0 || out != want {
				t.Errorf("%v: %d %q %q, want %q", args, code, out, errOut, want)
			}
		}
	}
	// --dry-run is accepted after the command too.
	if _, out, _ := runCLI("cut", "--me", "3", "--dry-run"); out != "000c00004443757402000000\n" {
		t.Errorf("trailing --dry-run: %q", out)
	}
}

func TestDryRunWarnsAndRefuses(t *testing.T) {
	code, _, errOut := runCLI("--dry-run", "video-mode", "1080p5994")
	if code != 0 || !strings.Contains(errOut, "destructive") {
		t.Errorf("video-mode: %d %q", code, errOut)
	}
	code, _, errOut = runCLI("--dry-run", "classic-audio", "levels", "true")
	if code != 0 || !strings.Contains(errOut, "unverified") {
		t.Errorf("classic-audio levels: %d %q", code, errOut)
	}
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"program", "5"}, 1, "--host"},
		{[]string{"program", "--host"}, 1, "--host needs a value"},
		{[]string{"--dry-run", "program"}, 1, "missing <source>: program <source>"},
		{[]string{"--dry-run", "program", "70000"}, 1, "out of range"},
		{[]string{"--dry-run", "program", "Stage Left"}, 1, "give the source id"},
		{[]string{"--dry-run", "program", "1", "--me", "0"}, 1, "counted from 1"},
		{[]string{"--dry-run", "program", "5", "--bogus", "1"}, 1, "has no --bogus"},
		{[]string{"--dry-run", "program", "5", "stray"}, 1, "unexpected \"stray\""},
		{[]string{"--dry-run", "transition", "wipe"}, 1, "nothing to set"},
		{[]string{"--dry-run", "transition", "mix", "1:30"}, 1, "not under the display rate, 25 fps"},
		{[]string{"--dry-run", "transition", "mix", "fast"}, 1, "not seconds:frames"},
		{[]string{"--dry-run", "transition", "settings", "--next", "0"}, 1, "selects no layer"},
		{[]string{"--dry-run", "media", "player", "1"}, 1, "nothing to set"},
		{[]string{"--dry-run", "media", "player", "1", "--still", "1", "--clip", "2"}, 1, "not both"},
		{[]string{"--dry-run", "usk", "0", "on-air", "on"}, 1, "keyer \"0\": counted from 1: 1-256"},
		{[]string{"--dry-run", "dsk", "on-air", "on", "--dsk", "0"}, 1, "counted from 1"},
		{[]string{"--dry-run", "transition", "stinger", "--clip", "7000"}, 1, "does not fit"},
		{[]string{"--dry-run", "nonsense"}, 1, "unknown command"},
		{[]string{"atem", "--dry-run", "nonsense"}, 1, "unknown command"},
		{[]string{"--dry-run", "transition"}, 1, "missing a subcommand"},
		{[]string{"--fps", "0", "cut"}, 1, "--fps"},
		{[]string{"--json=yes", "cut"}, 1, "takes no value"},
	} {
		code, _, errOut := runCLI(c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: %d %q, want %d and %q", c.args, code, errOut, c.code, c.msg)
		}
	}
}

// Commands go through the engine to the switcher. One with a state echo
// waits for it and is confirmed; one without is sent and left.
func TestSendThroughTheEngine(t *testing.T) {
	b := newBench(t)
	out := b.must(with("program", "5")...)
	if !strings.HasPrefix(out, "ATEM 1 M/E Constellation HD (192.0.2.10)  program  ME1  ") || !strings.Contains(out, "CAM2 → 5 ") ||
		!strings.Contains(out, " confirmed ") {
		t.Errorf("program: %q", out)
	}
	if c := b.sent("CPgI"); len(c) != 1 || fmt.Sprintf("%x", c[0]) != "000c000043506749"+"00000005" {
		t.Fatalf("switcher got %x", c)
	}
	out = b.must(with("atem", "cut")...)
	if !strings.Contains(out, "sent") || strings.Contains(out, "confirmed") {
		t.Errorf("cut: %q", out)
	}
	eventually(t, "the cut", func() bool { return len(b.sent("DCut")) == 1 })
	// One session served both: the engine kept it.
	if n := len(b.switchers()); n != 1 {
		t.Errorf("%d sessions", n)
	}
	// --dry-run wins over --host: nothing is sent.
	if code, out, _ := b.run(with("--dry-run", "program", "6")...); code != 0 || out != "000c00004350674900000006\n" {
		t.Fatalf("dry run with --host: %d %q", code, out)
	}
	if n := len(b.sent("CPgI")); n != 1 {
		t.Fatalf("%d CPgI after the dry run", n)
	}
}

// Sources are ids or the switcher's input names, long or short, in any
// case; the result shows the name.
func TestSourceNames(t *testing.T) {
	b := newBench(t)
	for _, c := range []struct{ name, id, short string }{
		{"camera 2", "0002", "CAM2"}, {"CAM1", "0001", "CAM1"}, {"color bars", "03e8", "Bars"}, {"mp1", "0bc2", "MP1"},
	} {
		out := b.must(with("preview", c.name)...)
		sent := b.sent("CPvI")
		if got := fmt.Sprintf("%x", sent[len(sent)-1][10:12]); got != c.id {
			t.Errorf("%q: sent source %s, want %s", c.name, got, c.id)
		}
		// Shown as watch shows it: by short name, the one it replaced
		// before it.
		if !strings.Contains(out, "→ "+c.short+" ") {
			t.Errorf("%q: %q", c.name, out)
		}
	}
	// The same source again: nothing changed, so no arrow.
	if out := b.must(with("preview", "mp1")...); !regexp.MustCompile(`  preview  ME1 +MP1 +confirmed `).MatchString(out) {
		t.Errorf("unchanged: %q", out)
	}
	code, _, errOut := b.run(with("program", "Camera 9")...)
	if code != exitUsage || !strings.Contains(errOut, `no input is named "Camera 9"`) {
		t.Errorf("unknown name: %d %q", code, errOut)
	}
}

// Rates in seconds:frames are read at the switcher's display rate unless
// --fps is given: one second is 30 frames on a 1080p59.94 switcher, not
// the fallback 25.
func TestRatesAtTheSwitchersRate(t *testing.T) {
	dump := bytes.Clone(fixtures.Bytes(t, "dump/initial-dump.bin"))
	i := bytes.Index(dump, []byte("VidM"))
	if i < 0 {
		t.Fatal("no VidM in the dump")
	}
	dump[i+4] = 13 // 1080p59.94
	b := newBenchDump(t, dump)
	b.must(with("transition", "mix", "1:00")...)
	b.must(with("--fps", "50", "transition", "mix", "1:00")...)
	b.must(with("transition", "mix", "1:29")...)
	c := b.sent("CTMx")
	if len(c) != 3 || c[0][9] != 30 || c[1][9] != 50 || c[2][9] != 59 {
		t.Fatalf("switcher got %x", c)
	}
	// 1:29 passes the check without a switcher, which can't know the rate.
	if code, _, errOut := b.run(with("transition", "mix", "1:30")...); code != exitUsage || !strings.Contains(errOut, "30 fps") {
		t.Errorf("1:30 at 30 fps: %d %q", code, errOut)
	}
}

// --json prints exactly what the engine sent; --quiet prints nothing.
func TestJSONAndQuiet(t *testing.T) {
	b := newBench(t)
	out := b.must(with("--json", "program", "CAM2")...)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("not one line: %q", out)
	}
	var r struct {
		OK        bool `json:"ok"`
		Confirmed bool `json:"confirmed"`
		Result    struct {
			Args    map[string]string `json:"args"`
			Command string            `json:"command"`
			Echo    string            `json:"echo"`
			State   struct {
				Index  int `json:"index"`
				Source int `json:"source"`
			} `json:"state"`
		} `json:"result"`
		TookMS *int64 `json:"took_ms"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if !r.OK || !r.Confirmed || r.Result.Command != "CPgI" || r.Result.Echo != "PrgI:0" || r.Result.State.Source != 2 ||
		r.Result.Args["source"] != "2 (Camera 2)" || r.TookMS == nil {
		t.Errorf("%s", out)
	}
	var keys map[string]json.RawMessage
	json.Unmarshal([]byte(out), &keys)
	if len(keys) != 4 {
		t.Errorf("keys %v", keys)
	}
	if code, out, errOut := b.run(with("--quiet", "program", "1")...); code != 0 || out != "" || errOut != "" {
		t.Errorf("quiet: %d %q %q", code, out, errOut)
	}
	// A failure is {ok:false, code, error} on stdout with --json.
	code, out, _ := b.run(with("--json", "program", "Nobody")...)
	if code != exitUsage || out != `{"ok":false,"code":"usage","error":"source \"Nobody\": no input is named \"Nobody\" (bmdctl inputs lists them)"}`+"\n" {
		t.Errorf("json failure: %d %q", code, out)
	}
	// --quiet still says what went wrong, on stderr.
	if code, out, errOut := b.run(with("-q", "program", "Nobody")...); code != exitUsage || out != "" || !strings.Contains(errOut, "Nobody") {
		t.Errorf("quiet failure: %d %q %q", code, out, errOut)
	}
}

// Each kind of failure has its exit status.
func TestExitCodes(t *testing.T) {
	b := newBench(t)
	b.onDial = func(sw *fakeatem.Switcher) { sw.Unanswered("CTWp") }
	// 3: sent, but the switcher doesn't echo it.
	code, _, errOut := b.run(with("--timeout", "200ms", "transition", "wipe", "--rate", "10")...)
	if code != exitNoEcho || !strings.Contains(errOut, "did not report TWpP:0 within 200ms (bmdctl status shows what the switcher holds now)") {
		t.Errorf("no echo: %d %q", code, errOut)
	}
	if len(b.sent("CTWp")) != 1 {
		t.Error("wipe not sent")
	}
	// 5: destructive without --yes, refused before the engine is asked.
	code, _, errOut = b.run(with("video-mode", "1080p50")...)
	if code != exitDestructive || !strings.HasSuffix(errOut, "add --yes to run it\n") || len(b.sent("CVdM")) != 0 {
		t.Errorf("destructive: %d %q", code, errOut)
	}
	// The engine refuses it too.
	c, o, _ := b.cli("")
	conn, err := c.connectEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := engine.Call(c.ctx, conn, engine.Request{Device: "atem", Host: fakeatem.Address, Path: "video-mode",
		Params: map[string]string{"mode": "1080p50"}}, nil)
	conn.Close()
	if err != nil || resp.OK || resp.Code != engine.CodeDestructive || o.Len() != 0 {
		t.Errorf("engine: %+v %v", resp, err)
	}
	// 1: usage.
	if code, _, _ := b.run(with("program")...); code != exitUsage {
		t.Errorf("usage: %d", code)
	}
	// 2: no switcher.
	b.mu.Lock()
	b.dialErr = errors.New("atem: dial 192.0.2.99:9910: context deadline exceeded")
	b.mu.Unlock()
	code, _, errOut = b.run("--host", "192.0.2.99", "cut")
	if code != exitUnreachable || !strings.Contains(errOut, "192.0.2.99") || !strings.HasSuffix(errOut, "(bmdctl doctor says why)\n") {
		t.Errorf("unreachable: %d %q", code, errOut)
	}
}

// Without an engine and unable to start one, a command is unreachable.
func TestNoEngine(t *testing.T) {
	code, _, errOut := runCLI("--host", fakeatem.Address, "cut")
	if code != exitUnreachable || !strings.Contains(errOut, "starting the engine") || !strings.Contains(errOut, "bmdctl doctor says why") {
		t.Errorf("%d %q", code, errOut)
	}
	if code, out, _ := runCLI("engine", "stop"); code != 0 || out != "no engine running\n" {
		t.Errorf("stop: %d %q", code, out)
	}
	if code, _, errOut := runCLI("engine", "status"); code != exitUnreachable || !strings.Contains(errOut, "no engine running") {
		t.Errorf("status: %d %q", code, errOut)
	}
}

// A command starts an engine when none answers and retries until it
// does.
func TestCommandStartsTheEngine(t *testing.T) {
	b := newBench(t)
	b.engine().Stop()
	eventually(t, "the old engine to go", func() bool {
		_, err := os.Stat(b.env["BMDCTL_SOCKET"])
		return os.IsNotExist(err)
	})
	c, o, e := b.cli("")
	spawned := 0
	c.spawn = func(exe string, args, env []string) error {
		spawned++
		if strings.Join(args, " ") != "engine --background" {
			t.Errorf("spawned %v", args)
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			if err := b.startEngine(engine.Config{}); err != nil {
				t.Error(err)
			}
		}()
		return nil
	}
	if code := c.run(with("program", "2")); code != 0 || spawned != 1 {
		t.Fatalf("%d, %d spawns\n%s%s", code, spawned, o, e)
	}
	// Starting the engine is connecting: said once, then the model and
	// video mode once the session is ready.
	if e.String() != "connecting to 192.0.2.10 …\nATEM 1 M/E Constellation HD, 1080p50\n" {
		t.Errorf("stderr %q", e)
	}
}

// An engine started from another build answers "stale" to a command and
// stops; the command starts an engine of its own build and goes to it,
// once.
func TestStaleEngineRestarts(t *testing.T) {
	b := newBench(t)
	restart := func(build string, bg bool) *engine.Engine {
		b.engine().Stop()
		eventually(t, "the old engine to go", func() bool {
			_, err := os.Stat(b.env["BMDCTL_SOCKET"])
			return os.IsNotExist(err)
		})
		if err := b.startEngine(engine.Config{BuildID: build, Background: bg}); err != nil {
			t.Fatal(err)
		}
		return b.engine()
	}
	// cli is a bmdctl of build b whose spawn starts an engine of build.
	cli := func(build string) (*cli, *int, *bytes.Buffer) {
		c, _, e := b.cli("")
		c.build = func() string { return "build-b" }
		spawned := new(int)
		c.spawn = func(string, []string, []string) error {
			*spawned++
			go func() {
				// As a background engine does, wait out the lock of
				// the one stopping.
				for range 100 {
					err := b.startEngine(engine.Config{BuildID: build, Background: true})
					if !errors.Is(err, engine.ErrRunning) {
						if err != nil {
							t.Error(err)
						}
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}()
			return nil
		}
		return c, spawned, e
	}
	old := restart("build-a", true)
	// Status and stop are the engine's own: answered whatever the build.
	c, spawned, _ := cli("build-b")
	var o bytes.Buffer
	c.stdout = &o
	if code := c.run([]string{"engine", "status"}); code != 0 || *spawned != 0 ||
		!strings.Contains(o.String(), "build     build-a (not this bmdctl's: the next command restarts the engine)") {
		t.Errorf("status: %d %d\n%s", code, *spawned, o.String())
	}
	// A command restarts it, once, and is sent to the new engine.
	if code := c.run(with("program", "2")); code != 0 || *spawned != 1 {
		t.Fatalf("program 2: %d, %d spawns", code, *spawned)
	}
	select {
	case <-old.Done():
	default:
		t.Error("the engine of another build still runs")
	}
	if got := b.lastSent(t, "CPgI"); got != "00000002" || b.engine() == old {
		t.Errorf("sent %s, new engine %v", got, b.engine() != old)
	}
	// The same build: nothing restarts.
	now := b.engine()
	if code := c.run(with("program", "1")); code != 0 || *spawned != 1 || b.engine() != now {
		t.Errorf("same build: %d, %d spawns", code, *spawned)
	}
	// An engine still of another build after the restart: the command
	// fails as unreachable, having started one engine.
	restart("build-a", true)
	c, spawned, errOut := cli("build-a")
	if code := c.run(with("program", "2")); code != exitUnreachable || *spawned != 1 || !strings.Contains(errOut.String(), "another build") {
		t.Errorf("still stale: %d, %d spawns, %q", code, *spawned, errOut)
	}
	// An engine run by hand (under systemd, in a container) answers
	// every build, and status says so.
	hand := restart("build-a", false)
	c, spawned, _ = cli("build-b")
	o.Reset()
	c.stdout = &o
	if code := c.run(with("program", "1")); code != 0 || *spawned != 0 || b.engine() != hand {
		t.Errorf("engine run by hand: %d, %d spawns", code, *spawned)
	}
	if c.run([]string{"engine", "status"}); !strings.Contains(o.String(), "build-a (not this bmdctl's; started by hand, it answers every build)") {
		t.Errorf("status:\n%s", o.String())
	}
}

// The engine runs in the foreground until stopped; engine status
// reports it and its sessions; it logs to its log file.
func TestEngineForeground(t *testing.T) {
	b := newBench(t)
	b.engine().Stop()
	c, _, errOut := b.cli("")
	done := make(chan int)
	go func() { done <- c.run([]string{"engine", "--log-level", "debug"}) }()
	eventually(t, "the engine", func() bool {
		code, _, _ := b.run("engine", "status")
		return code == 0
	})
	out := b.must("engine", "status")
	if !strings.Contains(out, "running, pid") || !strings.Contains(out, "sessions  none") || !strings.Contains(out, b.env["BMDCTL_SOCKET"]) {
		t.Errorf("status:\n%s", out)
	}
	var st struct {
		OK     bool          `json:"ok"`
		Result engine.Status `json:"result"`
	}
	if err := json.Unmarshal([]byte(b.must("engine", "status", "--json")), &st); err != nil || !st.OK || st.Result.Protocol != engine.Version {
		t.Errorf("status --json: %+v %v", st, err)
	}
	// A second foreground engine refuses to start.
	if code, _, e := b.run("engine"); code != exitUsage || !strings.Contains(e, "already running") {
		t.Errorf("second engine: %d %q", code, e)
	}
	if out := b.must("engine", "stop"); out != "engine stopped\n" {
		t.Errorf("stop: %q", out)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("engine exited %d: %s", code, errOut)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine still running")
	}
	log, err := os.ReadFile(filepath.Join(b.env["XDG_STATE_HOME"], "bmdctl", "engine.log"))
	if err != nil || !bytes.Contains(log, []byte("engine listening")) || !bytes.Contains(log, []byte("engine stopped")) {
		t.Errorf("log: %v\n%s", err, log)
	}
	if !strings.Contains(errOut.String(), "engine listening") {
		t.Errorf("foreground engine logs to stderr too: %q", errOut)
	}
}

// The real spawn: a command line starts this binary as a detached
// background engine, which answers, then stops on request.
func TestSpawnedEngine(t *testing.T) {
	b := newBench(t)
	b.engine().Stop()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "BMDCTL_TEST_MAIN=1")
	for k, v := range b.env {
		env = append(env, k+"="+v)
	}
	if err := engine.Spawn(exe, []string{"engine", "--background"}, env); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the spawned engine", func() bool {
		code, _, _ := b.run("engine", "status")
		return code == 0
	})
	out := b.must("engine", "status")
	if strings.Contains(out, fmt.Sprintf("pid %d,", os.Getpid())) {
		t.Errorf("the engine runs in the test process:\n%s", out)
	}
	b.must("engine", "stop")
	if code, _, _ := b.run("engine", "status"); code != exitUnreachable {
		t.Errorf("still running after stop: %d", code)
	}
}
