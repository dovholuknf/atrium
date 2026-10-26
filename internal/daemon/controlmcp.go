package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dovholuknf/atrium/internal/store"
)

// wireControlMCP puts the stdio relay to atrium-control in <home>/.atrium/mcp.json and names that file in the
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
			"type": "stdio", "command": filepath.ToSlash(exe), "args": []string{"mcp-relay"},
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
	_, err = st.SaveHarness(*h)
	return err
}
