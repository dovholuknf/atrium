//go:build integration

package store

import "testing"

func TestAnArtifactRecordedTwiceIsOneRowThatKeepsWhenItFirstAppeared(t *testing.T) {
	t.Parallel()
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "a1", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(Artifact{TaskID: task.ID, Kind: ArtifactFile, Ref: "a/b.go", Label: "new", Source: "hook", Bytes: 5}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(Artifact{TaskID: task.ID, Kind: ArtifactFile, Ref: "a/b.go", Source: "hook", Bytes: 9}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(Artifact{TaskID: task.ID, Kind: ArtifactReport, Ref: "a/b.go", Source: "tool"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Artifacts(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("the same file in two kinds is two entries and in one kind is one, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.Kind == ArtifactFile && (a.Label != "new" || a.Bytes != 9) {
			t.Fatalf("an update keeps the label it had and takes the new size: %+v", a)
		}
	}
}

func TestPruningACardTakesItsArtifacts(t *testing.T) {
	t.Parallel()
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "a2", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(Artifact{TaskID: task.ID, Kind: ArtifactLink, Ref: "http://x/"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE task SET status = 'done' WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(0); err != nil {
		t.Fatal(err)
	}
	got, err := s.Artifacts(task.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("a pruned card kept %d artifacts (err %v)", len(got), err)
	}
}
