package cli

import "testing"

// r-021: a hook says when its name came from the directory, so the daemon can
// keep it off a finished card.
func TestHookAgentSaysWhenTheNameCameFromTheDirectory(t *testing.T) {
	t.Setenv("ATRIUM_AGENT_NAME", "")
	if a, src := hookAgent("", "D:/git/atrium"); a != "atrium" || src != "dir" {
		t.Fatalf("got %q %q, want atrium from dir", a, src)
	}
	if a, src := hookAgent("told", "D:/git/atrium"); a != "told" || src != "" {
		t.Fatalf("a flag name is told, got %q %q", a, src)
	}
	t.Setenv("ATRIUM_AGENT_NAME", "envname")
	if a, src := hookAgent("", "D:/git/atrium"); a != "envname" || src != "" {
		t.Fatalf("an environment name is told, got %q %q", a, src)
	}
}
