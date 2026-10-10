//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A worker's row carries its dept and its director, a director rolls up under
// itself, and an operator's card rolls up under "".
func TestUsageGroupsOfACard(t *testing.T) {
	f := newUsageFix(t)
	dir, _, err := f.st.Register(store.Observed{WireName: "boss", Worktree: "/boss", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetTags(dir.ID, []string{DirectorTag, "dept:runtime"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetAlias(dir.ID, "rt"); err != nil {
		t.Fatal(err)
	}
	dir, _ = f.st.Get(dir.ID)
	w, _, err := f.st.Register(store.Observed{WireName: "worker", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetTags(w.ID, []string{"dept:ui"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetLineage(w.ID, dir.WireName, dir.ID); err != nil {
		t.Fatal(err)
	}
	row := &store.SessionUsage{TaskID: w.ID}
	f.u.stamp(row)
	if row.Dept != "ui" || row.Launcher != "rt" || row.LauncherID != dir.ID {
		t.Fatalf("worker row %+v", row)
	}
	row = &store.SessionUsage{TaskID: dir.ID}
	f.u.stamp(row)
	if row.Dept != "runtime" || row.Launcher != "rt" || row.LauncherID != dir.ID {
		t.Fatalf("director row %+v", row)
	}
	row = &store.SessionUsage{TaskID: f.task.ID}
	f.u.stamp(row)
	if row.Dept != "" || row.Launcher != "" || row.LauncherID != "" {
		t.Fatalf("operator row %+v", row)
	}
}
