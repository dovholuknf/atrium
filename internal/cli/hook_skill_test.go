package cli

import (
	"encoding/json"
	"testing"
)

func TestSkillNameReadsASkillCallAndASlashCommand(t *testing.T) {
	t.Parallel()
	call := hookInput{ToolName: "Skill", ToolInput: json.RawMessage(`{"skill":"review-panel","args":"396"}`)}
	if got := skillName("tool-start", call); got != "review-panel" {
		t.Fatalf("a Skill call read as %q", got)
	}
	if got := skillName("tool-end", call); got != "" {
		t.Fatalf("a finished Skill call read as %q", got)
	}
	if got := skillName("prompt", hookInput{Prompt: "  /review-panel 396"}); got != "review-panel" {
		t.Fatalf("a typed slash command read as %q", got)
	}
	if got := skillName("prompt", hookInput{Prompt: "walk the review"}); got != "" {
		t.Fatalf("a plain prompt read as %q", got)
	}
	if got := skillName("tool-start", hookInput{ToolName: "Bash", ToolInput: json.RawMessage(`{"skill":"x"}`)}); got != "" {
		t.Fatalf("a shell call read as %q", got)
	}
}
