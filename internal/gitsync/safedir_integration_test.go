//go:build integration

package gitsync

import (
	"path/filepath"
	"testing"
)

// Newer git for Windows refuses "NUL" as a config path; "/dev/null" works everywhere.
func TestGitConfigGlobalIsSlashDevNull(t *testing.T) {
	for _, env := range [][]string{cgiEnv(), (&Store{}).env()} {
		found := false
		for _, kv := range env {
			if kv == "GIT_CONFIG_GLOBAL=/dev/null" {
				found = true
			}
		}
		if !found {
			t.Fatalf("GIT_CONFIG_GLOBAL is not /dev/null in %v", env)
		}
	}
}

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
