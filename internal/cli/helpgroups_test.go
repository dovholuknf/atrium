package cli

import "testing"

func TestRootHelpGroupsEveryCommandAndHidesTheHarnessOnes(t *testing.T) {
	for _, c := range newRoot().Commands() {
		if c.Hidden {
			continue
		}
		if c.GroupID == "" || c.GroupID == "more" {
			t.Errorf("atrium %s is in no named help group. add it to helpGroup", c.Name())
		}
	}
	for _, name := range []string{"session", "turn", "statusline", "control", "mcp-relay", "merged", "ssh-proxy"} {
		if c, _, err := newRoot().Find([]string{name}); err != nil || !c.Hidden {
			t.Errorf("atrium %s, which only a harness runs, shows in help", name)
		}
	}
	if c, _, _ := newRoot().Find([]string{"hook"}); c.GroupID != "runners" {
		t.Errorf("atrium hook is in %q, want runners", c.GroupID)
	}
}
