//go:build integration

package cli

import (
	"encoding/json"
	"testing"
)

func noPollCall(t *testing.T, tool, cmd string, bg bool) []byte {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"tool_name": tool,
		"tool_input": map[string]any{"command": cmd, "run_in_background": bg}})
	return b
}

func TestNoPollRefusesWaiting(t *testing.T) {
	for _, c := range []struct {
		tool, cmd string
		bg        bool
		deny      bool
	}{
		{"Bash", "sleep 30", false, true},
		{"Bash", "go test ./... && sleep 5", false, true},
		{"Bash", "until grep -q done out.txt; do sleep 2; done", false, true},
		{"Bash", "while ! test -f x; do sleep 1; done", false, true},
		{"PowerShell", "Start-Sleep -Seconds 20", false, true},
		{"PowerShell", "Start-Sleep 5", false, true},
		{"Bash", "tail -n 20 /tmp/tasks/abc123.output", false, true},
		{"PowerShell", "Get-Content C:/t/tasks/abc.output", false, true},
		{"Bash", "go test ./internal/cli", true, true},
		{"Bash", "node scripts/test-board-headless.js termWear", true, true},
		{"Bash", "go test ./internal/cli", false, false},
		{"Bash", "node scripts/test-board-headless.js termWear", false, false},
		{"Bash", "npm run dev", true, false},
		{"Bash", "grep sleep notes.md", false, false},
		{"Read", "sleep 30", false, false},
	} {
		got := noPollHook(noPollCall(t, c.tool, c.cmd, c.bg))
		if (got != nil) != c.deny {
			t.Errorf("%s %q bg=%v: deny=%v, want %v", c.tool, c.cmd, c.bg, got != nil, c.deny)
		}
	}
}

func TestNoPollFailsOpen(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(""), []byte("not json"), []byte("{}"), []byte(`{"tool_name":"Bash","tool_input":"sleep 9"}`)} {
		if got := noPollHook(in); got != nil {
			t.Errorf("%q: printed %s", in, got)
		}
	}
}
