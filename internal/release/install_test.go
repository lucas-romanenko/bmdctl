// SPDX-License-Identifier: MIT

package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease is a release's archive for this machine (a bmdctl that
// prints its version) and its checksums, served over loopback the way
// GitHub serves a release: /latest redirects to the tag's page.
func fakeRelease(t *testing.T, tag string, corrupt bool) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\necho \"bmdctl " + strings.TrimPrefix(tag, "v") + " ($0)\"\n"
	tw.WriteHeader(&tar.Header{Name: "bmdctl", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	for _, a := range []string{"atemctl", "hdctl"} {
		tw.WriteHeader(&tar.Header{Name: a, Linkname: "bmdctl", Typeflag: tar.TypeSymlink, Mode: 0o777})
	}
	tw.Close()
	gz.Close()
	archive := fmt.Sprintf("bmdctl_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	if corrupt {
		sum[0] ^= 1
	}
	checksums := fmt.Sprintf("%x  %s\n%x  bmdctl_other.zip\n", sum, archive, sha256.Sum256(nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/download/"+tag+"/"+archive, func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(checksums)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runInstall(t *testing.T, env ...string) (string, string, error) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh is for macOS and Linux")
	}
	for _, tool := range []string{"sh", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	home := t.TempDir()
	cmd := exec.Command("sh", "../../install.sh")
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func TestInstallScript(t *testing.T) {
	srv := fakeRelease(t, "v9.9.9", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstall(t, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("%v\n%s%s", err, out, errOut)
	}
	if fi, err := os.Stat(filepath.Join(dir, "bmdctl")); err != nil || fi.Mode()&0o111 == 0 {
		t.Errorf("bmdctl: %v %v", fi, err)
	}
	for _, a := range []string{"atemctl", "hdctl", "vhctl", "ultctl"} {
		if target, err := os.Readlink(filepath.Join(dir, a)); err != nil || target != "bmdctl" {
			t.Errorf("%s: %q %v", a, target, err)
		}
	}
	for _, want := range []string{"downloading bmdctl v9.9.9 for " + runtime.GOOS + "/" + runtime.GOARCH,
		"installed " + dir + "/bmdctl, with atemctl, hdctl, vhctl, ultctl linked to it", "bmdctl 9.9.9",
		"is not on your PATH", "next: bmdctl connect <ip>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// An alias runs bmdctl, as itself.
	if b, err := exec.Command(filepath.Join(dir, "hdctl")).Output(); err != nil || !strings.HasSuffix(strings.TrimSpace(string(b)), "/hdctl)") {
		t.Errorf("hdctl: %q %v", b, err)
	}
}

func TestInstallScriptRefusesABadDownload(t *testing.T) {
	srv := fakeRelease(t, "v9.9.9", true)
	dir := filepath.Join(t.TempDir(), "bin")
	_, errOut, err := runInstall(t, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir)
	if err == nil || !strings.Contains(errOut, "doesn't match checksums.txt") {
		t.Errorf("%v %q", err, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "bmdctl")); err == nil {
		t.Error("installed anyway")
	}
}

func TestInstallScriptTakesAVersion(t *testing.T) {
	srv := fakeRelease(t, "v1.2.3", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstall(t, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir, "BMDCTL_VERSION=1.2.3")
	if err != nil || !strings.Contains(out, "downloading bmdctl v1.2.3") {
		t.Errorf("%v\n%s%s", err, out, errOut)
	}
}
