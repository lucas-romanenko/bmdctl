// SPDX-License-Identifier: MIT

package release

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// scoop/bmdctl.json is the manifest Windows installs from by its raw URL:
// it names the release's Windows archive as GoReleaser names it, and the
// five binaries in it. GoReleaser rewrites it, hash and all, at each
// release, into the directory .goreleaser.yml's scoops section names.
func TestScoopManifest(t *testing.T) {
	b, err := os.ReadFile("../../scoop/bmdctl.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version      string `json:"version"`
		Architecture map[string]struct {
			URL  string   `json:"url"`
			Bin  []string `json:"bin"`
			Hash string   `json:"hash"`
		} `json:"architecture"`
		License string `json:"license"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	a, ok := m.Architecture["64bit"]
	want := "https://github.com/lucas-romanenko/bmdctl/releases/download/v" + m.Version + "/bmdctl_" + m.Version + "_windows_amd64.zip"
	if !ok || a.URL != want || m.License == "" {
		t.Errorf("manifest: %+v, want url %s", m, want)
	}
	for _, exe := range []string{"bmdctl.exe", "atemctl.exe", "hdctl.exe", "vhctl.exe", "ultctl.exe"} {
		if !slices.Contains(a.Bin, exe) {
			t.Errorf("bin lacks %s: %v", exe, a.Bin)
		}
	}
	cfg, err := os.ReadFile("../../.goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scoops:", "    directory: scoop", "name_template: \"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}\"",
		"name_template: checksums.txt"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf(".goreleaser.yml lacks %q", want)
		}
	}
	docker, _ := os.ReadFile("../../Dockerfile")
	if !strings.Contains(string(docker), `CMD ["engine", "--serve"]`) {
		t.Error(`the image's command is not engine --serve`)
	}
}
