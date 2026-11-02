package daemon

import (
	"reflect"
	"testing"
)

func TestNeedTagsAddsMCPAgentAndSkillAndRefusesTheRest(t *testing.T) {
	tags, refused := needTags([]string{"mcp:mercurius", " skill:x ", "agent:y", "go", "tool:WebFetch", "mcp:"})
	if want := []string{"atrium:mcp:mercurius", "atrium:skill:x", "atrium:agent:y"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags %v, want %v", tags, want)
	}
	if want := []string{"go", "tool:WebFetch", "mcp:"}; !reflect.DeepEqual(refused, want) {
		t.Fatalf("refused %v, want %v", refused, want)
	}
}
