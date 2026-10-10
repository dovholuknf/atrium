//go:build integration

package hubstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitStoreSettingsDefaultAndRoundTrip(t *testing.T) {
	s := open(t)
	hub := t.TempDir()

	// Nothing written: the default, and create_on_push off. No migration is involved.
	if got, err := s.GitStorePath(hub); err != nil || got != filepath.Join(hub, "git") {
		t.Fatalf("default = %q %v", got, err)
	}
	if on, err := s.GitCreateOnPush(); err != nil || on {
		t.Fatalf("create_on_push default = %v %v", on, err)
	}

	alt := filepath.ToSlash(filepath.Join(t.TempDir(), "forge"))
	if err := s.SetGitStore(alt); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GitStorePath(hub); got != alt {
		t.Fatalf("store = %q, want %q", got, alt)
	}
	// Stored under the names the design gives them.
	if v, _ := s.HubSetting("git.store"); v != alt {
		t.Fatalf("git.store row = %q", v)
	}
	if err := s.SetGitCreateOnPush(true); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.HubSetting("git.create_on_push"); v != "on" {
		t.Fatalf("git.create_on_push row = %q", v)
	}
	if on, _ := s.GitCreateOnPush(); !on {
		t.Fatal("not on")
	}
	if err := s.SetGitCreateOnPush(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.GitCreateOnPush(); on {
		t.Fatal("not off")
	}
	// Empty puts the store back to the default.
	if err := s.SetGitStore(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GitStorePath(hub); got != filepath.Join(hub, "git") {
		t.Fatalf("reset = %q", got)
	}
}

func TestGitStoreRefusesWhatIsNotADirectoryPath(t *testing.T) {
	s := open(t)
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"relative/path", "git", "./x", "../x", file, "/abs\nnewline", "/", "/abs\x00nul"} {
		if err := s.SetGitStore(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	if v, _ := s.HubSetting("git.store"); v != "" {
		t.Fatalf("a refused write changed the row: %q", v)
	}
}

// Anything but `on` reads as off, so a stray value never lets a push create a repository.
func TestGitCreateOnPushReadsStrayValuesAsOff(t *testing.T) {
	s := open(t)
	for _, v := range []string{"yes", "true", "1", "", "ON please", "off"} {
		if err := s.SetHubSetting("git.create_on_push", v); err != nil {
			t.Fatal(err)
		}
		if on, _ := s.GitCreateOnPush(); on && !strings.EqualFold(v, "on") {
			t.Errorf("%q read as on", v)
		}
	}
}
