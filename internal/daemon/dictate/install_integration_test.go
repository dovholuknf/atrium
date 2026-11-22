//go:build integration

package dictate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func fakeRelease(t *testing.T, model []byte) (*httptest.Server, []Asset) {
	t.Helper()
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, body := range map[string]string{
		"Release/whisper-server.exe": "exe", "Release/whisper.dll": "dll", "Release/../escape.txt": "x", "other/skip.txt": "y",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			_, _ = w.Write(zb.Bytes())
			return
		}
		_, _ = w.Write(model)
	}))
	return srv, []Asset{
		{Name: "bin.zip", URL: srv.URL + "/bin.zip", SHA256: sum(zb.Bytes()), Size: int64(zb.Len()), Unzip: "Release"},
		{Name: "m.bin", URL: srv.URL + "/m.bin", SHA256: sum(model), Size: int64(len(model))},
	}
}

func waitState(t *testing.T, i *Installer, want string) Status {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) { // wait: poll the installer status until it reaches the state
		if s := i.Status(); s.State == want {
			return s
		}
	}
	t.Fatalf("never reached %q, at %+v", want, i.Status())
	return Status{}
}

// A verified download lands in the state folder, the zip is flattened without escaping it, and Done gets the paths.
func TestInstallDownloadsVerifiesAndReportsPaths(t *testing.T) {
	srv, assets := fakeRelease(t, []byte("ggml-model"))
	defer srv.Close()
	dir := t.TempDir()
	got := make(chan [2]string, 1)
	i := &Installer{Dir: dir, Assets: assets, Done: func(b, m string) { got <- [2]string{b, m} }}
	if err := i.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, i, "ready")
	paths := <-got
	if !strings.HasSuffix(paths[0], "bin/whisper-server.exe") || !strings.HasSuffix(paths[1], "models/m.bin") {
		t.Fatalf("paths %v", paths)
	}
	for _, p := range []string{paths[0], filepath.Join(dir, "bin", "whisper.dll"), paths[1]} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("a zip entry escaped the folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "skip.txt")); err == nil {
		t.Fatal("a file outside the release folder was kept")
	}
}

// A file that does not match its pinned checksum is thrown away and nothing is configured.
func TestInstallRefusesAChecksumMismatch(t *testing.T) {
	srv, assets := fakeRelease(t, []byte("ggml-model"))
	defer srv.Close()
	assets[1].SHA256 = sum([]byte("something else"))
	dir := t.TempDir()
	i := &Installer{Dir: dir, Assets: assets, Done: func(string, string) { t.Error("Done after a bad checksum") }}
	if err := i.Start(); err != nil {
		t.Fatal(err)
	}
	if s := waitState(t, i, "failed"); !strings.Contains(s.Note, "pinned checksum") {
		t.Fatalf("note %q", s.Note)
	}
	if _, err := os.Stat(filepath.Join(dir, "download", "m.bin")); err == nil {
		t.Fatal("the unverified model was kept")
	}
}
