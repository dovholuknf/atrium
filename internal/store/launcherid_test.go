package store

import "testing"

// LauncherID resolves report_to, then spawned_by_id, then spawned_by, never to the card itself, and
// LauncherIDs (the list's one SELECT) agrees with it on every card of one table.
func TestLauncherIDResolveAndTheBatchAgrees(t *testing.T) {
	s := openTestStore(t)
	mk := func(name string) *Task {
		c, _, err := s.Register(Observed{WireName: name, Worktree: "/tmp/" + name})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	boss, other := mk("boss"), mk("other")
	if err := s.SetAlias(other.ID, "oth"); err != nil {
		t.Fatal(err)
	}
	set := func(c *Task, by, byID, reportTo string) *Task {
		if err := s.SetLauncher(c.ID, by, byID); err != nil {
			t.Fatal(err)
		}
		if reportTo != "" {
			if err := s.SetReportTo(c.ID, reportTo); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	cases := []struct {
		name string
		card *Task
		want string
	}{
		{"report_to by alias beats spawned_by_id", set(mk("c-alias"), "boss", boss.ID, "oth"), other.ID},
		{"report_to by handle", set(mk("c-handle"), "boss", boss.ID, "other"), other.ID},
		{"report_to by id", set(mk("c-id"), "boss", boss.ID, other.ID), other.ID},
		{"stale report_to falls back to spawned_by_id", set(mk("c-stale"), "boss", boss.ID, "nobody"), boss.ID},
		{"spawned_by alone, by wire name", set(mk("c-wire"), "boss", "", ""), boss.ID},
		{"@human", set(mk("c-human"), HumanLauncher, "", ""), ""},
		{"another room", set(mk("c-away"), "boss@room", "room~x", ""), ""},
		{"unlaunched", mk("c-none"), ""},
	}
	self := mk("c-self")
	set(self, "c-self", "", "")
	cases = append(cases, struct {
		name string
		card *Task
		want string
	}{"spawned_by is its own handle", self, ""})
	selfID := mk("c-selfid")
	set(selfID, "boss", selfID.ID, selfID.WireName)
	cases = append(cases, struct {
		name string
		card *Task
		want string
	}{"report_to and spawned_by_id are itself, spawned_by is boss", selfID, boss.ID})

	batch := s.LauncherIDs()
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, _ := s.Get(c.card.ID)
		if g := s.LauncherID(got); g != c.want {
			t.Errorf("%s: LauncherID = %q, want %q", c.name, g, c.want)
		}
	}
	for _, c := range all {
		if a, b := s.LauncherID(c), batch[c.ID]; a != b {
			t.Errorf("%s: LauncherID %q but LauncherIDs %q", c.WireName, a, b)
		}
	}
}
