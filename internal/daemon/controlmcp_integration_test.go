//go:build integration

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
	if got.Type != "stdio" || got.Command != "/opt/atrium/atrium" || !slices.Equal(got.Args, []string{"control"}) {
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

func TestRelayControlHTTP(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".atrium", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	other := `{"mcpServers":{"atrium-control":{"type":"http","url":"http://x"}}}`
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := relayControlHTTP(home, "/opt/atrium/atrium"); err != nil || read() != other {
		t.Fatalf("a non-hub entry changed: %v %s", err, read())
	}
	hub := `{"mcpServers":{"atrium-control":{"type":"http","url":"http://127.0.0.1:7778/_hub/mcp",` +
		`"headers":{"X-Atrium-Agent":"${ATRIUM_AGENT_NAME}"}},"other":{"command":"o"}}}`
	if err := os.WriteFile(path, []byte(hub), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := relayControlHTTP(home, "/opt/atrium/atrium"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(read()), &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.MCPServers["atrium-control"]
	if got.Type != "stdio" || got.Command != "/opt/atrium/atrium" ||
		!slices.Equal(got.Args, []string{"mcp-relay", "--url", "http://127.0.0.1:7778/_hub/mcp"}) {
		t.Fatalf("atrium-control = %+v", got)
	}
	if doc.MCPServers["other"].Command != "o" {
		t.Fatalf("other server lost: %s", read())
	}
}

func TestAReopenedRoomRepairsAMissingControlMCP(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		AgentAddr: freePort(t), HumanAddr: freePort(t), DBPath: filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll: 2 * time.Second, LocationFile: filepath.Join(dir, "daemon.json"),
	}
	first, err := testNew(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".atrium", "mcp.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	h, err := first.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.Args = []string{"--strict-mcp-config"}
	if _, err := first.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	first.Close()

	opts.AgentAddr, opts.HumanAddr = freePort(t), freePort(t)
	second, err := testNew(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mcp.json was not repaired: %v", err)
	}
	if h, _ := second.st.Harness("claude"); !slices.Contains(h.Args, "--mcp-config") {
		t.Fatalf("args = %q", h.Args)
	}
}
