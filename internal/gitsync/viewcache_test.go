//go:build integration && git

package gitsync

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

// A request for the list runs no git and answers at once, however many repositories there are, and the
// background build then fills in the branches.
func TestSnapshotRunsNoGitOnTheRequest(t *testing.T) {
	x := newStore(t, "main")
	x.forge.push(t, "main", "a.txt", "one", 1000)
	x.forge.push(t, "feature", "b.txt", "two", 2000)
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	dir, _ := x.s.Path("o/r")
	git(t, dir, "branch", "feature", "main")
	x.s.RefreshView()
	run := x.h.Runner
	// Wait for the build Init asked for, then count from there.
	testwait.Eventually(t, 20*time.Second, "the snapshot to be built", func() bool {
		return !x.s.Snapshot().Computed.IsZero() && len(x.s.Snapshot().Repos[0].Branches) != 0
	})
	testwait.Eventually(t, 20*time.Second, "the build to end", func() bool { return !x.s.Snapshot().Building })
	before := run.Spawned.Load()
	start := time.Now()
	var snap Snapshot
	for i := 0; i < 100; i++ {
		snap = x.s.Snapshot()
	}
	if per := time.Since(start) / 100; per > 50*time.Millisecond {
		t.Fatalf("a request took %v, want under 50ms", per)
	}
	if n := run.Spawned.Load() - before; n != 0 {
		t.Fatalf("100 requests started %d git processes, want 0", n)
	}
	if len(snap.Repos) != 1 || len(snap.Repos[0].Branches) != 1 || snap.Repos[0].Main.SHA == "" {
		t.Fatalf("snapshot = %+v", snap.Repos)
	}
	if snap.Computed.IsZero() {
		t.Fatal("the snapshot does not say how old it is")
	}
}

// Before the first build the answer is the directory listing alone, and says it is still building.
func TestSnapshotBeforeTheFirstBuildStartsNoGit(t *testing.T) {
	x := newStore(t, "main")
	x.forge.push(t, "main", "a.txt", "one", 1000)
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	// A fresh store over the same disk has built nothing.
	h := &Hub{Dir: x.root, Runner: NewRunner(), Repos: x.h.Repos}
	s := h.Store()
	snap := s.Snapshot()
	if len(snap.Repos) != 1 || !snap.Building || !snap.Computed.IsZero() {
		t.Fatalf("snapshot = %+v", snap)
	}
}
