package link

import "testing"

func TestLeanAgentsDroppedNamesARoomThatIgnoredThem(t *testing.T) {
	if got := LeanAgentsDropped(nil, nil, nil); got != nil {
		t.Fatalf("nothing asked, nothing dropped, got %v", got)
	}
	if got := LeanAgentsDropped([]string{"a"}, nil, []string{"atrium:lean", "atrium:agent:a"}); got != nil {
		t.Fatalf("a room that tagged the card applied them, got %v", got)
	}
	if got := LeanAgentsDropped([]string{"a"}, nil, []string{"origin:agent"}); len(got) != 1 || got[0] != "lean_agents" {
		t.Fatalf("got %v", got)
	}
}

func TestLeanLaunchIsOnForLeanAgentsOnAnyRunner(t *testing.T) {
	if !leanLaunch(launchInput{LeanAgents: []string{"a"}}, "codex") {
		t.Fatal("lean_agents implies lean, so the room can refuse the runner by name")
	}
	if leanLaunch(launchInput{}, "codex") || !leanLaunch(launchInput{}, "claude") {
		t.Fatal("the default is unchanged")
	}
}

func TestLeanSkillsDroppedIsNamedSeparately(t *testing.T) {
	got := LeanAgentsDropped([]string{"a"}, []string{"s"}, []string{"atrium:agent:a"})
	if len(got) != 1 || got[0] != "lean_skills" {
		t.Fatalf("got %v", got)
	}
}
