package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/store"
)

// What auto_new_context `agents` reaches, for a card the launch path tagged. A WORKER (origin:agent and
// atrium:subagent, which atrium_launch stamps by default) is NOT reached by `agents` by design: workers are
// short-lived and a cleared brief loses more than it saves (auto-new-context-design.md, decision 1). It is
// reached by `atrium:auto-new-context` or the ceiling tag. A director launch (origin:agent only) is reached.
func TestAutoContextAgentsModeAndTheLaunchPathsTags(t *testing.T) {
	worker := link.WithLauncherDept(link.AgentLaunchTags(nil), []string{"dept:runtime"})
	if !hasTag(worker, OriginAgentTag) || !hasTag(worker, SubagentTag) {
		t.Fatalf("a launch's tags = %v, want origin:agent and atrium:subagent", worker)
	}
	if autoModeSubject(&store.Task{Tags: worker}, store.AutoNewContextAgents) {
		t.Fatal("a plain worker is reached by `agents`, which the design excludes")
	}
	if !autoModeSubject(&store.Task{Tags: append(append([]string{}, worker...), AutoContextTag)}, store.AutoNewContextAgents) {
		t.Fatal("a worker launched with atrium:auto-new-context is not reached by `agents`")
	}
	director := link.AgentLaunchTags([]string{link.DirectorTag})
	if !autoModeSubject(&store.Task{Tags: director}, store.AutoNewContextAgents) {
		t.Fatalf("a director launch (%v) is not reached by `agents`", director)
	}
	// untagged (the bug): never reached, whatever the mode
	if autoModeSubject(&store.Task{}, store.AutoNewContextAgents) {
		t.Fatal("an untagged card is reached by `agents`")
	}
}
