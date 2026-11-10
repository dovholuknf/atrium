package daemon

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestNeedTagsAddsMCPAgentAndSkillAndRefusesTheRest(t *testing.T) {
	tags, refused := needTags([]string{"mcp:mercurius", " skill:x ", "agent:y", "go", "tool:Bogus", "mcp:"})
	if want := []string{"atrium:mcp:mercurius", "atrium:skill:x", "atrium:agent:y"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags %v, want %v", tags, want)
	}
	if want := []string{"go", "tool:Bogus", "mcp:"}; !reflect.DeepEqual(refused, want) {
		t.Fatalf("refused %v, want %v", refused, want)
	}
}

func TestNeedTagsGivesTheSkillAndAgentToolsAndLeavesTheLaunchGrantToTheCaller(t *testing.T) {
	tags, refused := needTags([]string{"tool:Skill", "tool:Agent", "tool:atrium_launch", "tool:"})
	if want := []string{"atrium:tool:Skill", "atrium:tool:Agent"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags %v, want %v", tags, want)
	}
	if want := []string{"tool:"}; !reflect.DeepEqual(refused, want) {
		t.Fatalf("refused %v, want %v", refused, want)
	}
}

func TestAGrantedSkillToolComesBackOnASuperleanRestart(t *testing.T) {
	task := &store.Task{Tags: []string{LeanTag, SuperleanTag, "atrium:tool:Skill"}}
	_, _, kit, superlean := leanOptions(shared.LaunchRequest{}, task, "")
	if !superlean {
		t.Fatal("the card did not come back superlean")
	}
	if got := superleanTools(kit); !slices.Contains(got, "Skill") || slices.Contains(got, "Agent") {
		t.Fatalf("tools %v, want Skill and not Agent", got)
	}
	if got := leanDisallowedFor(kit); slices.Contains(got, "Skill") || !slices.Contains(got, "Agent") {
		t.Fatalf("disallowed %v, want Agent and not Skill", got)
	}
	if kept, _ := withoutLeanTags(task.Tags); len(kept) != 0 {
		t.Fatalf("lean: false kept %v", kept)
	}
}
