package link

import (
	"context"
	"strings"
	"testing"
)

// ancestry is a LandGit that knows one fact: `newer` holds `older`.
type ancestry struct {
	LandGit
	newer, older string
}

func (a ancestry) Contains(_ context.Context, build, sha string) (bool, bool, error) {
	return build == a.newer && sha == a.older, true, nil
}

func buildIssueOf(p *Proxy, f *RoomSetup) string {
	f.Answering = true
	for _, i := range p.setupIssues(context.Background(), f) {
		if i.Kind == "build" {
			return i.Text
		}
	}
	return ""
}

func TestAnEqualCodeFingerprintRaisesNoBuildIssueWhateverTheBoard(t *testing.T) {
	p := &Proxy{boardID: "bbbbbbbbbbbb", hubCode: "c0de"}
	if got := buildIssueOf(p, &RoomSetup{Build: "aaaaaaaaaaaa", Code: "c0de"}); got != "" {
		t.Fatalf("a board-only difference raised %q", got)
	}
}

func TestADifferentCodeFingerprintIsNamedWithBothCommitsAndTheNewer(t *testing.T) {
	room, hub := strings.Repeat("1", 40), strings.Repeat("2", 40)
	p := &Proxy{boardID: "bbbbbbbbbbbb", hubCode: "c0de", hubBuild: hub, landGit: ancestry{newer: hub, older: room}}
	got := buildIssueOf(p, &RoomSetup{Build: "bbbbbbbbbbbb", Code: "beef", Commit: room})
	for _, want := range []string{"different atrium code", "11111111", "22222222", "the hub is newer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("issue %q lacks %q", got, want)
		}
	}
}

func TestARoomWithoutAFingerprintKeepsTheBoardCompare(t *testing.T) {
	p := &Proxy{boardID: "bbbbbbbbbbbb", hubCode: "c0de"}
	if got := buildIssueOf(p, &RoomSetup{Build: "aaaaaaaaaaaa"}); !strings.Contains(got, "different atrium build") {
		t.Fatalf("an old room's board difference said %q", got)
	}
	if got := buildIssueOf(p, &RoomSetup{Build: "bbbbbbbbbbbb"}); got != "" {
		t.Fatalf("an old room with the same board said %q", got)
	}
}
