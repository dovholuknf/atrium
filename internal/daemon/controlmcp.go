package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// wireControlMCP puts `atrium control` as atrium-control in <home>/.atrium/mcp.json and names that file in the
// claude runner row, on a room's first start, as scripts/provision-room.ps1 step 11 does. The row passes
// --strict-mcp-config, so a session reads only the servers an --mcp-config file lists.
func wireControlMCP(st *store.Store, home, exe string) error {
	path := filepath.ToSlash(filepath.Join(home, ".atrium", "mcp.json"))
	doc := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s is not JSON, left as it is: %w", path, err)
		}
	}
	servers := map[string]json.RawMessage{}
	if s, ok := doc["mcpServers"]; ok {
		if err := json.Unmarshal(s, &servers); err != nil {
			return fmt.Errorf("%s has an mcpServers that is not an object, left as it is: %w", path, err)
		}
	}
	// One the operator made is theirs, whatever it runs.
	if _, ok := servers["atrium-control"]; !ok {
		servers["atrium-control"], _ = json.Marshal(map[string]any{
			"type": "stdio", "command": filepath.ToSlash(exe), "args": []string{"control"},
		})
		doc["mcpServers"], _ = json.Marshal(servers)
		out, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
			return err
		}
	}
	h, err := st.Harness("claude")
	if err != nil {
		return err
	}
	if slices.Contains(h.Args, "--mcp-config") {
		return nil
	}
	h.Args = append([]string{"--mcp-config", path}, h.Args...)
	if !slices.Contains(h.ResumeArgs, "--mcp-config") {
		h.ResumeArgs = append(h.ResumeArgs, "--mcp-config", path)
	}
	if len(h.ForkArgs) > 0 && !slices.Contains(h.ForkArgs, "--mcp-config") {
		h.ForkArgs = append(h.ForkArgs, "--mcp-config", path)
	}
	_, err = st.SaveHarness(*h)
	return err
}

// relayControlHTTP swaps an HTTP atrium-control entry for the hub's /_hub/mcp for the stdio relay. Claude Code never
// redials an HTTP server, so a hub restart would leave that session without atrium tools. Any other entry is left.
func relayControlHTTP(home, exe string) error {
	path := filepath.ToSlash(filepath.Join(home, ".atrium", "mcp.json"))
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	doc := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	servers := map[string]json.RawMessage{}
	if json.Unmarshal(doc["mcpServers"], &servers) != nil {
		return nil
	}
	var cur struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(servers["atrium-control"], &cur) != nil || cur.Type != "http" || !strings.Contains(cur.URL, "/_hub/mcp") {
		return nil
	}
	args := []string{"mcp-relay"}
	if !strings.Contains(cur.URL, "${") {
		args = append(args, "--url", cur.URL)
	}
	servers["atrium-control"], _ = json.Marshal(map[string]any{"type": "stdio", "command": filepath.ToSlash(exe), "args": args})
	doc["mcpServers"], _ = json.Marshal(servers)
	out, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}
