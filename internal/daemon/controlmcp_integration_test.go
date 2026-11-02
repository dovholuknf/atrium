//go:build integration

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestWireControlMCP(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.ToSlash(filepath.Join(dir, "atrium.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	home := filepath.Join(dir, "home")
	path := filepath.ToSlash(filepath.Join(home, ".atrium", "mcp.json"))

	if err := wireControlMCP(st, home, "/opt/atrium/atrium"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.MCPServers["atrium-control"]
	if got.Type != "stdio" || got.Command != "/opt/atrium/atrium" || !slices.Equal(got.Args, []string{"mcp-relay"}) {
		t.Fatalf("atrium-control = %+v", got)
	}
	h, err := st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--mcp-config", path, "--strict-mcp-config"}; !slices.Equal(h.Args, want) {
		t.Fatalf("args = %q, want %q", h.Args, want)
	}
	if want := []string{"--resume", "{resume}", "--strict-mcp-config", "--mcp-config", path}; !slices.Equal(h.ResumeArgs, want) {
		t.Fatalf("resume_args = %q, want %q", h.ResumeArgs, want)
	}

	// An operator's own entry and other servers are kept, and a second run adds nothing to the row.
	mine := `{"mcpServers":{"atrium-control":{"type":"http","url":"http://x"},"other":{"command":"o"}}}`
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := wireControlMCP(st, home, "/opt/atrium/atrium"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != mine {
		t.Fatalf("operator's mcp.json rewritten: %s", raw)
	}
	if h, _ := st.Harness("claude"); len(h.Args) != 3 {
		t.Fatalf("args doubled: %q", h.Args)
	}
}
