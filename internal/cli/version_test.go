package cli

import (
	"strings"
	"testing"
)

// An unstamped build is the base number plus the short commit, and never sorts as a release.
func TestAnUnstampedBuildIsBasePlusCommit(t *testing.T) {
	if Version != "" {
		t.Fatalf("the default version is %q. it is stamped by the linker", Version)
	}
	commit := "99e2d8e51ecdd19b4fe6f9048e06b40c7cae6937"
	want := strings.TrimSpace(baseVersion) + "+99e2d8e"
	if v, _, _ := versionInfoOf(commit, "clean"); v != want {
		t.Fatalf("version is %q, want %q", v, want)
	}
	if v, _, _ := versionInfoOf(commit, "modified"); v != want {
		t.Fatalf("a modified tree changed the version to %q", v)
	}
}

// The one-line form is what a log header wants, and it has to survive both
// halves being absent: a `go build` with no flags and no repository leaves both
// the version and the commit empty.
func TestTheVersionLineSurvivesHavingNothingToSay(t *testing.T) {
	line := VersionLine()
	if !strings.HasPrefix(line, "atrium ") {
		t.Fatalf("the version line does not name the program: %q", line)
	}
	if strings.Contains(line, "()") {
		t.Fatalf("an empty commit left an empty bracket: %q", line)
	}
}

// A stamped tree state wins over Go's, in both directions. Go's counts untracked
// files, so a build that is exactly its commit has to be able to say clean.
func TestAStampedTreeWinsOverGo(t *testing.T) {
	commit := "99e2d8e51ecdd19b4fe6f9048e06b40c7cae6937"

	if _, _, dirty := versionInfoOf(commit, "clean"); dirty != "" {
		t.Fatalf("a tree stamped clean says %q", dirty)
	}
	if line := versionLineOf(commit, "clean"); strings.Contains(line, "modified") {
		t.Fatalf("a tree stamped clean is modified in the line: %q", line)
	}
	if _, _, dirty := versionInfoOf(commit, "modified"); dirty != "modified" {
		t.Fatalf("a tree stamped modified says %q", dirty)
	}
}

// A commit is abbreviated in the one-line form. A forty character hash in a log
// header pushes everything else off the line, and the first twelve identify it.
func TestALongCommitIsShortenedInTheLine(t *testing.T) {
	commit := "99e2d8e51ecdd19b4fe6f9048e06b40c7cae6937"
	line := versionLineOf(commit, "")
	if strings.Contains(line, commit) {
		t.Fatalf("the whole hash is in the line: %q", line)
	}
	if !strings.Contains(line, "99e2d8e51ecd") {
		t.Fatalf("the line does not identify the commit: %q", line)
	}
}
