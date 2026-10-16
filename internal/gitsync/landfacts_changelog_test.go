package gitsync

import "testing"

// THE CHANGELOG NAME IS THE DATE AND THEN EXACTLY THE ID: `<date>-r-stuck.md` is another item's, not `stuck`'s.
func TestChangelogNamedMatchesTheItemsOwnEntryOnly(t *testing.T) {
	for path, want := range map[string]bool{
		"changelog/board/2026-10-15-stuck.md":     true,
		"changelog/board/2026-10-15-r-stuck.md":   false,
		"changelog/board/2026-10-15-stuck.md.bak": false,
		"changelog/2026-10-15-stuck.md":           false,
		"docs/board/2026-10-15-stuck.md":          false,
		"changelog/board/x-stuck.md":              false,
	} {
		if got := changelogNamed(path, "stuck"); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
}
