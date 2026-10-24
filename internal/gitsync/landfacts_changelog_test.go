package gitsync

import "testing"

// THE CHANGELOG NAME IS THE DATE AND THEN EXACTLY THE ID: `<date>-r-stuck.md` is `r-stuck`'s, not `stuck`'s.
func TestChangelogIDIsTheItemsOwnEntryOnly(t *testing.T) {
	for path, want := range map[string]string{
		"changelog/board/2026-10-15-stuck.md":     "stuck",
		"changelog/board/2026-10-15-r-stuck.md":   "r-stuck",
		"changelog/board/2026-10-15-stuck.md.bak": "",
		"changelog/2026-10-15-stuck.md":           "",
		"docs/board/2026-10-15-stuck.md":          "",
		"changelog/board/x-stuck.md":              "",
		"changelog/board/2026-10-15-.md":          "",
		"changelog/board/2026x10-15-stuck.md":     "",
	} {
		if got, ok := changelogID(path); got != want || ok != (want != "") {
			t.Errorf("%s = %q %v, want %q", path, got, ok, want)
		}
	}
}
