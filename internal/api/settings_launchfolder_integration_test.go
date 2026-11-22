//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func TestTheLaunchFolderAndModeSaveAndReadBack(t *testing.T) {
	t.Parallel()
	srv, _, _ := fileServer(t)

	// The host's own separators: backslashes on a Windows host, slashes elsewhere, where a backslash is no separator.
	body, _ := json.Marshal(map[string]string{"launch_folder": filepath.FromSlash("C:/git/projects/"), "launch_mode": "Place"})
	if rec := settingsPost(t, srv, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body.String())
	}
	out := settingsGet(t, srv)
	if out["launch_folder"] != "C:/git/projects" || out["launch_mode"] != "place" {
		t.Fatalf("read back as %v and %v", out["launch_folder"], out["launch_mode"])
	}
	if rec := settingsPost(t, srv, `{"launch_mode":"worktree"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown mode answered %d", rec.Code)
	}
	if rec := settingsPost(t, srv, `{"launch_folder":"","launch_mode":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d: %s", rec.Code, rec.Body.String())
	}
	if out := settingsGet(t, srv); out["launch_folder"] != "" || out["launch_mode"] != "" {
		t.Fatalf("cleared, read back as %v and %v", out["launch_folder"], out["launch_mode"])
	}
}

func TestLaunchFolderOfKeepsADriveRoot(t *testing.T) {
	for in, want := range map[string]string{"d:/": "d:/", "/": "/", " /home/me/git/ ": "/home/me/git", "": ""} {
		if got := launchFolderOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
