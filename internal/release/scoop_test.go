// SPDX-License-Identifier: MIT

package release

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// scoop/bmdctl.json is the manifest Windows installs from by its raw URL.
// Nothing rewrites it at a release: its url is the latest release's
// Windows archive, by the name GoReleaser gives it in every release, with
// no hash and the version Scoop takes for an unpinned url (nightly); its
// autoupdate names a given release's archive, for a bucket to pin one.
// .goreleaser.yml publishes no manifest and names archives without the
// version.
func TestScoopManifest(t *testing.T) {
	b, err := os.ReadFile("../../scoop/bmdctl.json")
	if err != nil {
		t.Fatal(err)
	}
	type arch struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	var m struct {
		Version      string          `json:"version"`
		Architecture map[string]arch `json:"architecture"`
		Bin          []string        `json:"bin"`
		Hash         string          `json:"hash"`
		License      string          `json:"license"`
		Checkver     struct {
			GitHub string `json:"github"`
		} `json:"checkver"`
		Autoupdate struct {
			Architecture map[string]arch `json:"architecture"`
			Hash         struct {
				URL string `json:"url"`
			} `json:"hash"`
		} `json:"autoupdate"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	const repo = "https://github.com/lucas-romanenko/bmdctl"
	a, ok := m.Architecture["64bit"]
	if !ok || m.Version != "nightly" || a.URL != repo+"/releases/latest/download/bmdctl_windows_amd64.zip" ||
		a.Hash != "" || m.Hash != "" || m.License == "" {
		t.Errorf("manifest: %+v", m)
	}
	for _, exe := range []string{"bmdctl.exe", "atemctl.exe", "hdctl.exe", "vhctl.exe", "ultctl.exe"} {
		if !slices.Contains(m.Bin, exe) {
			t.Errorf("bin lacks %s: %v", exe, m.Bin)
		}
	}
	if m.Checkver.GitHub != repo || m.Autoupdate.Architecture["64bit"].URL != repo+"/releases/download/v$version/bmdctl_windows_amd64.zip" ||
		m.Autoupdate.Hash.URL != "$baseurl/checksums.txt" {
		t.Errorf("checkver %+v, autoupdate %+v", m.Checkver, m.Autoupdate)
	}
	cfg, err := os.ReadFile("../../.goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "scoops:") || strings.Contains(string(cfg), "{{ .Version }}_{{ .Os }}") {
		t.Error(".goreleaser.yml publishes a Scoop manifest, or names an archive with its version")
	}
	for _, want := range []string{"name_template: \"{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}\"", "name_template: checksums.txt",
		"maintainer: Lucas Romanenko <lucas@romanenko.com>"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf(".goreleaser.yml lacks %q", want)
		}
	}
	docker, _ := os.ReadFile("../../Dockerfile")
	if !strings.Contains(string(docker), `CMD ["engine", "--serve"]`) {
		t.Error(`the image's command is not engine --serve`)
	}
}
