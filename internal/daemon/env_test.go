package daemon

import (
	"os"
	"strings"
	"testing"
)

// The daemon is usually started from inside a claude session, so its
// environment carries that session's markers. Passing them to a launched
// runner makes the new session believe it is a child of the old one, which
// silently turns transcript saving off.
func TestChildEnvDropsInheritedSessionMarkers(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("ATRIUM_AGENT_NAME", "stale-name")
	t.Setenv("PATH_LIKE_KEEPER", "keep me")

	env := childEnv(nil, map[string]string{"ATRIUM_AGENT_NAME": "fresh-name"})

	seen := map[string]string{}
	for _, kv := range env {
		if i := strings.Index(kv, "="); i > 0 {
			seen[kv[:i]] = kv[i+1:]
		}
	}
	for _, gone := range []string{"CLAUDE_CODE_CHILD_SESSION", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"} {
		if _, ok := seen[gone]; ok {
			t.Errorf("%s leaked into the launched runner", gone)
		}
	}
	if seen["PATH_LIKE_KEEPER"] != "keep me" {
		t.Error("an unrelated variable was dropped")
	}
	if seen["ATRIUM_AGENT_NAME"] != "fresh-name" {
		t.Errorf("agent name is %q, want the launch's own value", seen["ATRIUM_AGENT_NAME"])
	}
}

// A harness may set its own environment, and atrium's values are applied last
// so a launch always identifies itself correctly.
func TestChildEnvLayering(t *testing.T) {
	env := childEnv(
		map[string]string{"MODEL": "llama3", "ATRIUM_TASK_ID": "harness-wins-not"},
		map[string]string{"ATRIUM_TASK_ID": "the-real-one"},
	)
	var model, task string
	for _, kv := range env {
		if strings.HasPrefix(kv, "MODEL=") {
			model = strings.TrimPrefix(kv, "MODEL=")
		}
		// Last occurrence wins in a process environment, so scan to the end.
		if strings.HasPrefix(kv, "ATRIUM_TASK_ID=") {
			task = strings.TrimPrefix(kv, "ATRIUM_TASK_ID=")
		}
	}
	if model != "llama3" {
		t.Errorf("harness env lost: MODEL is %q", model)
	}
	if task != "the-real-one" {
		t.Errorf("atrium env did not win: ATRIUM_TASK_ID is %q", task)
	}
}

func TestInheritedTaint(t *testing.T) {
	for _, k := range []string{
		"CLAUDE_CODE_CHILD_SESSION", "claude_code_entrypoint", "CLAUDECODE",
		"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID",
	} {
		if !inheritedTaint(k) {
			t.Errorf("%s should be stripped", k)
		}
	}
	for _, k := range []string{"PATH", "HOME", "ATRIUM_HUB_URL", "ATRIUM_PERM_GATE", "USERPROFILE"} {
		if inheritedTaint(k) {
			t.Errorf("%s should be inherited", k)
		}
	}
	_ = os.Environ
}
