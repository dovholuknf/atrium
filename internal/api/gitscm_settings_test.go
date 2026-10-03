package api

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestGitSCMRootSavesReadsBackAndRefusesARelativePath(t *testing.T) {
	srv, _, _ := fileServer(t)
	if out := settingsGet(t, srv); out["git_scm_root"] != "" {
		t.Fatalf("unset reads %q", out["git_scm_root"])
	}
	root := filepath.ToSlash(t.TempDir())
	if rec := settingsPost(t, srv, `{"git_scm_root":"`+root+`","git_credential_helper":"store"}`); rec.Code != http.StatusOK {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body.String())
	}
	out := settingsGet(t, srv)
	if out["git_scm_root"] != root || out["git_credential_helper"] != "store" {
		t.Fatalf("read back %v %v", out["git_scm_root"], out["git_credential_helper"])
	}
	if rec := settingsPost(t, srv, `{"git_scm_root":"~/git"}`); rec.Code != http.StatusOK {
		t.Fatalf("~/git answered %d", rec.Code)
	}
	for _, body := range []string{`{"git_scm_root":"git"}`, `{"git_scm_root":"../x"}`, `{"git_scm_root":"/a\nb"}`, `{"git_credential_helper":"a\nb"}`} {
		if rec := settingsPost(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, rec.Code)
		}
	}
	if rec := settingsPost(t, srv, `{"git_scm_root":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d", rec.Code)
	}
	if rec := settingsPost(t, srv, `{"git_credential_hosts":"github.com, git.example.org"}`); rec.Code != http.StatusOK {
		t.Fatalf("hosts answered %d", rec.Code)
	}
	if out := settingsGet(t, srv); out["git_credential_hosts"] != "github.com, git.example.org" {
		t.Fatalf("hosts read back %v", out["git_credential_hosts"])
	}
	for _, body := range []string{`{"git_credential_hosts":"https://evil/x"}`, `{"git_credential_hosts":"a@b"}`} {
		if rec := settingsPost(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, rec.Code)
		}
	}
}
