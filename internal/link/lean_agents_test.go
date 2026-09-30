package link

import "testing"

func TestLeanAgentsDroppedNamesARoomThatIgnoredThem(t *testing.T) {
	if got := LeanAgentsDropped(nil, nil); got != nil {
		t.Fatalf("nothing asked, nothing dropped, got %v", got)
	}
	if got := LeanAgentsDropped([]string{"a"}, []string{"atrium:lean", "atrium:agent:a"}); got != nil {
		t.Fatalf("a room that tagged the card applied them, got %v", got)
	}
	if got := LeanAgentsDropped([]string{"a"}, []string{"origin:agent"}); len(got) != 1 || got[0] != "lean_agents" {
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
