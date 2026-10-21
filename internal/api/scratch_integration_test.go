//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func scratchReq(id, root, path string) *http.Request {
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/files?root="+root+"&path="+url.QueryEscape(path), nil)
	r.SetPathValue("id", id)
	return r
}

func TestTheBoardDownloadsFromTheCardsScratchFolder(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	scratchDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratchDir, "oidc.patch"), []byte("diff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "inside.txt"), []byte("home"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.CardScratchAdd(filepath.ToSlash(scratchDir), task.ID); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.downloadFile(w, scratchReq(task.ID, "scratch", filepath.Join(scratchDir, "oidc.patch")))
	if w.Code != http.StatusOK || w.Body.String() != "diff" {
		t.Fatalf("scratch download: %d %q", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	s.downloadFile(w, downloadReq(task.ID, url.QueryEscape(filepath.Join(scratchDir, "oidc.patch"))))
	if w.Code != http.StatusForbidden {
		t.Fatalf("the scratch folder is outside the card without root=scratch: %d", w.Code)
	}

	w = httptest.NewRecorder()
	s.downloadFile(w, scratchReq(task.ID, "scratch", filepath.Join(work, "inside.txt")))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a file outside the scratch folder answered %d", w.Code)
	}

	w = httptest.NewRecorder()
	s.downloadFile(w, scratchReq(task.ID, "scratch", filepath.Join(scratchDir, "..", "x")))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a path out of the scratch folder answered %d", w.Code)
	}
}

func TestACardWithNoScratchFolderHasNothingToDownload(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	w := httptest.NewRecorder()
	s.downloadFile(w, scratchReq(task.ID, "scratch", filepath.Join(work, "x")))
	if w.Code != http.StatusNotFound {
		t.Fatalf("answered %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	s.listFiles(w, scratchReq(task.ID, "scratch", ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("list answered %d, want 404", w.Code)
	}
}
