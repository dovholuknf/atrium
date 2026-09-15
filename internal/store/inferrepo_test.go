package store

import "testing"

// The path from the report. A worktree kept three directories BELOW its
// checkout, on a card nobody told the repo, drew as three nested headings
// called doc, troubleshooting and debug-skill.
func TestInferRepoReadsTheRepoOutOfAWorktreePath(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{
			"D:/worktrees/github/openziti/desktop-edge-win/more-debug-skill-updates/doc/troubleshooting/debug-skill",
			"desktop-edge-win",
		},
		// The ordinary shape, with the worktree at the repo itself.
		{"D:/git/github/dovholuknf/atrium", "atrium"},
		// Backslashes, because this is read off a Windows path.
		{`D:\git\github\openziti\zrok`, "zrok"},
		// Other forges, and a hostname rather than a bare name.
		{"/home/me/src/gitlab.com/acme/widget", "widget"},
		{"/home/me/src/bitbucket/team/thing/sub/dir", "thing"},
		{"/srv/codeberg/someone/project", "project"},
		// The org shares its name with the repo, which must not confuse it.
		{"D:/git/github/openziti/openziti/deep/path", "openziti"},
	}
	for _, c := range cases {
		if got := InferRepo(c.path); got != c.want {
			t.Errorf("InferRepo(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// AN EMPTY ANSWER IS THE SAFE ANSWER. Every caller falls back to what it did
// before, so a path this does not recognise cannot be made worse by it.
func TestInferRepoDeclinesWhatItCannotRead(t *testing.T) {
	for _, p := range []string{
		"",
		"   ",
		// No forge directory anywhere.
		"D:/work/some/random/place",
		// A forge directory with nothing useful under it. Two segments have to
		// follow or what was found is the tail of a path, not the start of a
		// tree.
		"D:/git/github",
		"D:/git/github/openziti",
		// `github` as the last thing on the path.
		"/home/me/notes/about/github",
	} {
		if got := InferRepo(p); got != "" {
			t.Errorf("InferRepo(%q) = %q, want empty", p, got)
		}
	}
}

// The three tiers, in order. An override beats what the launcher recorded,
// which beats the guess.
func TestDisplayRepoPrefersAnOverrideThenTheRecordThenTheGuess(t *testing.T) {
	const path = "D:/worktrees/github/openziti/desktop-edge-win/wt/doc/troubleshooting/debug-skill"

	guessed := &Task{Worktree: path}
	if got := guessed.DisplayRepo(); got != "desktop-edge-win" {
		t.Fatalf("with nothing recorded, got %q, want the guess", got)
	}

	recorded := &Task{Worktree: path, Repo: "told-you-so"}
	if got := recorded.DisplayRepo(); got != "told-you-so" {
		t.Fatalf("what the launcher recorded lost to the guess: %q", got)
	}

	overridden := &Task{
		Worktree:  path,
		Repo:      "told-you-so",
		Overrides: map[string]string{"repo": "what-i-typed"},
	}
	if got := overridden.DisplayRepo(); got != "what-i-typed" {
		t.Fatalf("the override lost: %q", got)
	}

	// Clearing the override falls back rather than answering empty, which is
	// what makes the menu entry's empty-to-undo work.
	cleared := &Task{
		Worktree:  path,
		Overrides: map[string]string{"repo": ""},
	}
	if got := cleared.DisplayRepo(); got != "desktop-edge-win" {
		t.Fatalf("clearing the override answered %q rather than falling back", got)
	}
}
