package cli

import (
	"strings"
	"testing"
)

// An unstamped build says `dev`, and saying so is the point.
//
// The tempting alternative is the nearest tag plus a commit count, which reads
// like a version and is not one. A package manager compares this string to
// decide whether you already have it, so a working-tree build claiming a number
// near a release is worse than one admitting it is not a release at all.
func TestAnUnstampedBuildAdmitsIt(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("the default version is %q, not dev. it is stamped by the linker, "+
			"and a value edited into the source is wrong on every build off a branch", Version)
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

// A commit is abbreviated in the one-line form. A forty character hash in a log
// header pushes everything else off the line, and the first twelve identify it.
func TestALongCommitIsShortenedInTheLine(t *testing.T) {
	before := Commit
	t.Cleanup(func() { Commit = before })

	Commit = "99e2d8e51ecdd19b4fe6f9048e06b40c7cae6937"
	line := VersionLine()
	if strings.Contains(line, Commit) {
		t.Fatalf("the whole hash is in the line: %q", line)
	}
	if !strings.Contains(line, "99e2d8e51ecd") {
		t.Fatalf("the line does not identify the commit: %q", line)
	}
}
