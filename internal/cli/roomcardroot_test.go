package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
)

func TestNoCardRootNoteOnlyWhenWindowsFindsNowhere(t *testing.T) {
	t.Setenv("WORKTREE_ROOT", "")
	db := filepath.Join(t.TempDir(), "atrium.db")
	win := cardroot.Env{GOOS: "windows", HomeDir: func() (string, error) { return "", os.ErrNotExist }}
	note := noCardRootNote(win, db)
	for _, want := range []string{"atrium room set card_root", "D:/", "no card root"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q: %s", want, note)
		}
	}
	if got := noCardRootNote(cardroot.Env{GOOS: "linux", HomeDir: os.UserHomeDir}, db); got != "" {
		t.Errorf("linux has a default card root, got %q", got)
	}
	local := filepath.Join(t.TempDir(), "Local")
	win.LocalAppData = func() string { return local }
	if got := noCardRootNote(win, db); got != "" {
		t.Errorf("LOCALAPPDATA gives a card root, got %q", got)
	}
	win.LocalAppData = nil
	t.Setenv("WORKTREE_ROOT", t.TempDir())
	if got := noCardRootNote(win, db); got != "" {
		t.Errorf("WORKTREE_ROOT gives a card root, got %q", got)
	}
}

func TestRoomSetCardRootRefusesWhatCheckRefuses(t *testing.T) {
	db := filepath.Join(t.TempDir(), "atrium.db")
	if _, err := storeRoomSetting(db, "card_root", "relative/cards"); err == nil {
		t.Error("a relative card_root was accepted")
	}
	if _, err := storeRoomSetting(db, "card_root_required", "maybe"); err == nil {
		t.Error("card_root_required accepted a value that is not on or off")
	}
}
