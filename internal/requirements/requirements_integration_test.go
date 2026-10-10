//go:build integration

package requirements

import (
	"encoding/json"
	"os"
	"testing"
)

// Section 2.1 of the design, verbatim.
func TestTheDesignsOwnFileParsesToTheExpectedJSON(t *testing.T) {
	data, err := os.ReadFile("testdata/atrium.requirements.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(f)
	const want = `{"version":1,"atrium":{"min":"7eb1555"},` +
		`"git":{"base":"claude/main","mirror":"hub-main","clone":"{home}/git/github/{owner}/{repo}",` +
		`"worktrees":"{clone}-worktrees","fresh":true},` +
		`"toolchain":{"git":{"min":"2.39","windows":"git-for-windows"},"go":{"from":"go.mod"},` +
		`"node":{"min":"24"},"pwsh":{"min":"7","os":["windows"]}},` +
		`"runners":{"claude":{"hooks":"atrium","gate":"required","mcp":["atrium-control"],"smoke":true},` +
		`"codex":{"helpers":["codex-code-mode-host"],"smoke":true}},` +
		`"room":{"survives":"none","runner_auth":["claude","codex"]},"env":{},"services":[],"forges":{}}`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
