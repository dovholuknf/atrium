package link

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/resources"
)

func callResources(t *testing.T, h *classHarness, agent string) resourcesOutput {
	t.Helper()
	s := h.connect(t, agent, "alpha")
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_resources"})
	if err != nil || res.IsError {
		t.Fatalf("atrium_resources: %v %+v", err, res)
	}
	var out resourcesOutput
	raw, _ := res.StructuredContent.(map[string]any)
	if raw == nil {
		t.Fatalf("no structured content: %+v", res)
	}
	out.File, _ = raw["file"].(string)
	out.Exists, _ = raw["exists"].(bool)
	out.Text, _ = raw["text"].(string)
	if rooms, ok := raw["rooms"].([]any); ok {
		for _, r := range rooms {
			m := r.(map[string]any)
			out.Rooms = append(out.Rooms, resourceRoom{Name: m["name"].(string), Online: m["online"] == true})
		}
	}
	return out
}

func TestWorkerReadsTheInventoryAndRooms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(resources.Path(dir), []byte("## m1mini\nssh m1mini. macOS arm64.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newClassHarness(t, classTasks)
	h.c.resourcesDir = func() string { return dir }
	h.c.rooms = func() []resourceRoom { return []resourceRoom{{Name: "sg3", Online: true}, {Name: "cdz"}} }

	out := callResources(t, h, "worker1")
	if !out.Exists || !strings.Contains(out.File, "ssh m1mini") || !strings.Contains(out.Text, "## m1mini") {
		t.Fatalf("file not returned: %+v", out)
	}
	if len(out.Rooms) != 2 || !strings.Contains(out.Text, "- cdz, offline") || !strings.Contains(out.Text, "- sg3, online") {
		t.Fatalf("rooms wrong: %+v", out)
	}
}

func TestNoResourcesFileSaysHowToMakeOne(t *testing.T) {
	h := newClassHarness(t, classTasks)
	h.c.resourcesDir = func() string { return t.TempDir() }
	out := callResources(t, h, "worker1")
	if out.Exists || out.File != "" || !strings.Contains(out.Text, "atrium resources init") {
		t.Fatalf("missing file not explained: %+v", out)
	}
}
