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
	unixOnly(t)
	for _, tool := range []string{"sh", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	return runInstallWith(t, "sh", os.Getenv("PATH"), env...)
}

// runInstallWith runs install.sh under sh with PATH set to path.
func runInstallWith(t *testing.T, sh, path string, env ...string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(sh, "../../install.sh")
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + path}, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh is for macOS and Linux")
	}
}

// installTools are the commands install.sh runs besides curl or wget.
var installTools = []string{"sh", "uname", "tar", "gzip", "mktemp", "sha256sum", "shasum",
	"cut", "sed", "tr", "head", "awk", "rm", "mkdir", "id", "cp", "chmod", "mv", "ln"}

// toolDir is a directory to be the whole PATH: each of tools linked to
// to(tool), or left out where to gives "".
func toolDir(t *testing.T, tools []string, to func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		if target := to(tool); target != "" {
			if err := os.Symlink(target, filepath.Join(dir, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// checkInstalled checks bmdctl and its aliases are in dir.
func checkInstalled(t *testing.T, dir string) {
	t.Helper()
	if fi, err := os.Stat(filepath.Join(dir, "bmdctl")); err != nil || fi.Mode()&0o111 == 0 {
		t.Errorf("bmdctl: %v %v", fi, err)
	}
	for _, a := range []string{"atemctl", "hdctl", "vhctl", "ultctl"} {
		if target, err := os.Readlink(filepath.Join(dir, a)); err != nil || target != "bmdctl" {
			t.Errorf("%s: %q %v", a, target, err)
		}
	}
}

func TestInstallScript(t *testing.T) {
	srv := fakeRelease(t, "v9.9.9", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstall(t, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("%v\n%s%s", err, out, errOut)
	}
	checkInstalled(t, dir)
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

// TestInstallScriptWithWget installs with GNU wget and no curl.
func TestInstallScriptWithWget(t *testing.T) {
	unixOnly(t)
	wget, err := exec.LookPath("wget")
	if err != nil {
		t.Skip("no wget")
	}
	if v, _ := exec.Command(wget, "--version").Output(); !bytes.Contains(v, []byte("GNU Wget")) {
		t.Skip("wget isn't GNU's")
	}
	path := toolDir(t, append(installTools, "wget"), func(tool string) string {
		p, _ := exec.LookPath(tool)
		return p
	})
	srv := fakeRelease(t, "v9.9.9", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstallWith(t, filepath.Join(path, "sh"), path, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir)
	if err != nil || !strings.Contains(out, "downloading bmdctl v9.9.9") {
		t.Fatalf("%v\n%s%s", err, out, errOut)
	}
	checkInstalled(t, dir)
}

// TestInstallScriptOnBusyBox installs with nothing but BusyBox, as on
// Alpine: its sh, its wget (which has no --max-redirect) and its tools.
func TestInstallScriptOnBusyBox(t *testing.T) {
	unixOnly(t)
	busybox, err := exec.LookPath("busybox")
	if err != nil {
		t.Skip("no busybox")
	}
	path := toolDir(t, append(installTools, "wget"), func(tool string) string {
		if tool == "shasum" || tool == "gzip" {
			return "" // sha256sum, and tar's own gzip
		}
		return busybox
	})
	srv := fakeRelease(t, "v9.9.9", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstallWith(t, filepath.Join(path, "sh"), path, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir)
	if err != nil || !strings.Contains(out, "downloading bmdctl v9.9.9") {
		t.Fatalf("%v\n%s%s", err, out, errOut)
	}
	checkInstalled(t, dir)
}

func TestInstallScriptTakesAVersion(t *testing.T) {
	srv := fakeRelease(t, "v1.2.3", false)
	dir := filepath.Join(t.TempDir(), "bin")
	out, errOut, err := runInstall(t, "BMDCTL_BASE_URL="+srv.URL, "BMDCTL_INSTALL_DIR="+dir, "BMDCTL_VERSION=1.2.3")
	if err != nil || !strings.Contains(out, "downloading bmdctl v1.2.3") {
		t.Errorf("%v\n%s%s", err, out, errOut)
	}
}
