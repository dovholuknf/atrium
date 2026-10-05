package gitsync

import (
	"path/filepath"
	"testing"
)

func TestGitCGITrustsTheDirectoryItServes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	h := gitCGI("git", dir, [][2]string{{"http.receivepack", "false"}}, cgiEnv())
	want := filepath.ToSlash(dir)
	found := false
	for i, kv := range h.Env {
		if kv == "GIT_CONFIG_KEY_1=safe.directory" && h.Env[i+1] == "GIT_CONFIG_VALUE_1="+want {
			found = true
		}
	}
	if !found {
		t.Fatalf("safe.directory=%s not in the CGI environment: %v", want, h.Env)
	}
	count := false
	for _, kv := range h.Env {
		if kv == "GIT_CONFIG_COUNT=2" {
			count = true
		}
	}
	if !count {
		t.Fatalf("config count is wrong: %v", h.Env)
	}
}
