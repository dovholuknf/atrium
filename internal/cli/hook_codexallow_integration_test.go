//go:build integration

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexPreToolUseNeverAnswersABareAllow(t *testing.T) {
	t.Setenv("ATRIUM_GUARD", "on")
	for runner, want := range map[string]bool{"codex": false, "claude": true} {
		defer withStdin(t, string(guardCall("Write", map[string]any{"file_path": filepath.Join(t.TempDir(), "default.env")})))()
		root := newRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs([]string{"hook", "--event", "pre-tool-use", "--runner", runner})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), `"allow"`); got != want {
			t.Fatalf("%s: allow=%v, output %q", runner, got, out.String())
		}
	}
	for in, kept := range map[string]bool{
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`:                                 false,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask"}}`:                                   false,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"x"}}`:   true,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"ls"}}}`: true,
		`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`:                       true,
	} {
		if got := codexPreToolUse([]byte(in), "codex") != nil; got != kept {
			t.Fatalf("%s: kept=%v", in, got)
		}
	}
}
