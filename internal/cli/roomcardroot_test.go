package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNoCardRootNoteOnlyOnAFreshWindowsRoom(t *testing.T) {
	t.Setenv("WORKTREE_ROOT", "")
	db := filepath.Join(t.TempDir(), "atrium.db")
	note := noCardRootNote("windows", db)
	for _, want := range []string{"atrium room set card_root", "D:/", "no card root"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q: %s", want, note)
		}
	}
	if got := noCardRootNote("linux", db); got != "" {
		t.Errorf("linux has a default card root, got %q", got)
	}
	t.Setenv("WORKTREE_ROOT", t.TempDir())
	if got := noCardRootNote("windows", db); got != "" {
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
