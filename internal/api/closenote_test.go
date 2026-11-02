package api

import (
	"errors"
	"strings"
	"testing"
)

func TestRemoveNoteSaysNothingForAFileInUse(t *testing.T) {
	err := errors.New("git worktree: error: failed to delete 'C:/cards/x/work': unlinkat C:/cards/x/work: " +
		"The process cannot access the file because it is being used by another process.")
	if got := removeNote("C:/cards/x/work", err); got != "" {
		t.Fatalf("a lock the retry handles raised %q", got)
	}
}

func TestRemoveNoteIsOneShortLineOtherwise(t *testing.T) {
	got := removeNote("C:/cards/x/work", errors.New("C:/cards/x/work is still a worktree of its repository"))
	if got == "" || strings.Count(got, "C:/cards") != 0 || strings.Contains(got, "\n") {
		t.Fatalf("not one short line without the path: %q", got)
	}
}
