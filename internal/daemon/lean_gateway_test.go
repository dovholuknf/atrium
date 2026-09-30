package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

const leanGatewayMCP = `{"mcpServers":{
	"mercurius":{"type":"http","url":"http://127.0.0.1:8088/mcp"},
	"mercurius-worker":{"type":"http","url":"http://127.0.0.1:8089/mcp"},
	"atrium-control":{"type":"http","url":"http://127.0.0.1:7778/_hub/mcp"}}}`

// leanGatewayServers is the mcpServers a lean launch gets, from a temp mcp.json.
func leanGatewayServers(t *testing.T, extra []string, gateway string) map[string]map[string]string {
	t.Helper()
	got, err := leanArgs([]string{"--mcp-config", "C:/t/mcp.json"}, nil, "", extra, gateway,
		leanTestRead(map[string]string{"C:/t/mcp.json": leanGatewayMCP}))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]map[string]string `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(flagValue(t, got, "--mcp-config")), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.MCPServers
}

func TestLeanGatewayEmptySettingLeavesTheConfigUnchanged(t *testing.T) {
	read := leanTestRead(map[string]string{"C:/t/mcp.json": leanGatewayMCP})
	a, err := leanServers([]string{"C:/t/mcp.json"}, nil, "", read)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"mcpServers":{"atrium-control":{"type":"http","url":"http://127.0.0.1:7778/_hub/mcp"},` +
		`"mercurius":{"type":"http","url":"http://127.0.0.1:8088/mcp"}}}`
	if a != want {
		t.Fatalf("got %s\nwant %s", a, want)
	}
	// Naming mercurius with the setting off is dropped, as it always was.
	if _, mcp := leanOptions(LaunchRequest{MCP: []string{"mercurius"}}, nil, ""); len(mcp) != 0 {
		t.Fatalf("mcp = %v", mcp)
	}
}

func TestLeanGatewayDefaultLaunchGetsTheNarrowEntry(t *testing.T) {
	s := leanGatewayServers(t, nil, "mercurius-worker")
	if s["mercurius"]["url"] != "http://127.0.0.1:8089/mcp" || s["mercurius-worker"] != nil || len(s) != 2 {
		t.Fatalf("got %v", s)
	}
}

func TestLeanGatewayExplicitNameOrTagGetsTheWideEntry(t *testing.T) {
	on := true
	_, mcp := leanOptions(LaunchRequest{Lean: &on, MCP: []string{"mercurius"}}, nil, "mercurius-worker")
	if len(mcp) != 1 || mcp[0] != "mercurius" {
		t.Fatalf("mcp = %v", mcp)
	}
	if s := leanGatewayServers(t, mcp, "mercurius-worker"); s["mercurius"]["url"] != "http://127.0.0.1:8088/mcp" {
		t.Fatalf("explicit name got %v", s)
	}
	card := &store.Task{Tags: []string{LeanTag, leanMCPTagPrefix + "mercurius"}}
	_, mcp = leanOptions(LaunchRequest{}, card, "mercurius-worker")
	if s := leanGatewayServers(t, mcp, "mercurius-worker"); s["mercurius"]["url"] != "http://127.0.0.1:8088/mcp" {
		t.Fatalf("tag got %v", s)
	}
}

func TestLeanGatewayUnknownNameIsRefusedWithTheList(t *testing.T) {
	_, err := leanArgs([]string{"--mcp-config", "C:/t/mcp.json"}, nil, "", nil, "nosuch",
		leanTestRead(map[string]string{"C:/t/mcp.json": leanGatewayMCP}))
	if err == nil || !strings.Contains(err.Error(), "nosuch") || !strings.Contains(err.Error(), "mercurius-worker") {
		t.Fatalf("got %v", err)
	}
}

// A restart reads the card's tags again and keeps what it had.
func TestLeanGatewayRestartKeepsWhatTheCardHad(t *testing.T) {
	launched := leanTags([]string{"mercurius"})
	if !hasTag(launched, leanMCPTagPrefix+"mercurius") {
		t.Fatalf("a wide launch did not tag the card: %v", launched)
	}
	for _, c := range []struct {
		tags []string
		url  string
		says string
	}{
		{launched, "8088", "mercurius: mercurius (wide)"},
		{leanTags(nil), "8089", "mercurius: mercurius-worker"},
	} {
		card := &store.Task{Tags: c.tags}
		_, mcp := leanOptions(LaunchRequest{}, card, "mercurius-worker")
		if s := leanGatewayServers(t, mcp, "mercurius-worker"); !strings.Contains(s["mercurius"]["url"], c.url) {
			t.Errorf("tags %v got %v", c.tags, s)
		}
		if got := store.MercuriusFor(c.tags, "mercurius-worker"); got != c.says {
			t.Errorf("details say %q, want %q", got, c.says)
		}
	}
}
