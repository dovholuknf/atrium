package store

import (
	"testing"
	"time"
)

// Rows sum per department and per director, and a row from before the columns
// existed lands in @before and never in a department.
func TestUsageBucketsGroup(t *testing.T) {
	s := openTestStore(t)
	a, _, err := s.Register(Observed{WireName: "a", Worktree: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	since := until.Add(-time.Hour)
	rows := []*SessionUsage{
		{TaskID: a.ID, Ended: since.Add(time.Second), Input: 1, Dept: "core", Launcher: "d1", LauncherID: "x"},
		{TaskID: a.ID, Ended: since.Add(2 * time.Second), Input: 2, Dept: "core", Launcher: "d2", LauncherID: "y"},
		{TaskID: a.ID, Ended: since.Add(3 * time.Second), Input: 4, Dept: "", Launcher: ""},
		{TaskID: a.ID, Ended: since.Add(4 * time.Second), Input: 8},
	}
	for _, r := range rows {
		if err := s.AddSessionUsage(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE session_usage SET dept = NULL, launcher = NULL, launcher_id = NULL WHERE input = 8`); err != nil {
		t.Fatal(err)
	}
	for group, want := range map[string]map[string]int64{
		UsageByDept:     {"core": 3, "": 4, UsageBefore: 8},
		UsageByLauncher: {"d1": 1, "d2": 2, "": 4, UsageBefore: 8},
	} {
		got, err := s.UsageBucketsBy(since, until, 60, "", group)
		if err != nil || len(got.Buckets) != 1 {
			t.Fatalf("%s: %+v (%v)", group, got, err)
		}
		g := got.Buckets[0].Groups
		if len(g) != len(want) {
			t.Fatalf("%s: groups %v, want %v", group, g, want)
		}
		var sum int64
		for k, v := range want {
			if g[k] == nil || g[k].Input != v {
				t.Fatalf("%s: group %q is %v, want %d", group, k, g[k], v)
			}
			sum += g[k].Input
		}
		if sum != got.Buckets[0].Total.Input {
			t.Fatalf("%s: groups sum %d, total %d", group, sum, got.Buckets[0].Total.Input)
		}
	}
	plain, err := s.UsageBuckets(since, until, 60, "")
	if err != nil || plain.Buckets[0].Groups != nil {
		t.Fatalf("no group answers %+v (%v)", plain.Buckets[0].Groups, err)
	}
}
