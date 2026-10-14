//go:build integration

package link

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// itemTools is the worker set without the four an item worker must not have.
func itemTools() []string {
	drop := map[string]bool{"atrium_say": true, "atrium_peers": true, "atrium_task": true, "atrium_report": true}
	var out []string
	for _, n := range workerSeven {
		if !drop[n] {
			out = append(out, n)
		}
	}
	return out
}

var itemClassTasks = []map[string]any{
	{"id": "i1", "wire_name": "itemw", "status": "working", "tags": []string{"atrium:subagent", "atrium:item", "origin:agent"}},
	{"id": "w1", "wire_name": "worker1", "status": "working", "tags": []string{"atrium:subagent", "origin:agent"}},
}

func TestAnItemWorkerLacksSayPeersTaskAndReport(t *testing.T) {
	h := newClassHarness(t, itemClassTasks)
	item := h.connect(t, "itemw", "alpha")
	sameNames(t, "item", toolNames(t, item), itemTools())
	for _, tool := range []string{"atrium_say", "atrium_peers", "atrium_task", "atrium_report"} {
		_, err := item.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unknown tool") {
			t.Errorf("%s by an item worker: err = %v, want unknown tool", tool, err)
		}
	}
}

func TestAnOrdinaryWorkerStillHasAllFour(t *testing.T) {
	h := newClassHarness(t, itemClassTasks)
	sameNames(t, "worker", toolNames(t, h.connect(t, "worker1", "alpha")), workerSeven)
}

// An item worker's finishing tools never speak of a launcher.
func TestAnItemWorkersEndToolsNeverNameTheLauncher(t *testing.T) {
	h := newClassHarness(t, itemClassTasks)
	res, err := h.connect(t, "itemw", "alpha").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tl := range res.Tools {
		if tl.Name == "atrium_done" || tl.Name == "atrium_blocked" {
			seen++
			if strings.Contains(strings.ToLower(tl.Description), "launcher") {
				t.Errorf("%s reads %q", tl.Name, tl.Description)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("saw %d end tools, want 2", seen)
	}
}

func itemLaunchBody(t *testing.T, h *classHarness, args map[string]any) map[string]any {
	t.Helper()
	before := len(*h.launches)
	s := h.connect(t, "boss", "alpha")
	if _, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_launch_worker", Arguments: args}); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(*h.launches) != before+1 {
		t.Fatalf("launches = %d, want %d", len(*h.launches), before+1)
	}
	return (*h.launches)[before]
}

func tagsOf(body map[string]any) []string {
	var out []string
	for _, v := range body["tags"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func TestAnItemLaunchIsTaggedAndEndsWithoutALauncher(t *testing.T) {
	h := newClassHarness(t, classTasks)
	body := itemLaunchBody(t, h, map[string]any{"cwd": "/work/dir", "prompt": "do the thing", "item": true})
	if !hasTag(tagsOf(body), ItemTag) {
		t.Errorf("tags = %v, want %s", tagsOf(body), ItemTag)
	}
	if p, _ := body["prompt"].(string); strings.Contains(strings.ToLower(p), "launcher") || !strings.Contains(p, "atrium_done") {
		t.Errorf("an item prompt is %q", p)
	}
}

// A launch without the flag is unchanged: no item tag, the same ending, no item key on the request.
func TestANonItemLaunchIsUnchanged(t *testing.T) {
	h := newClassHarness(t, classTasks)
	body := itemLaunchBody(t, h, map[string]any{"cwd": "/work/dir", "prompt": "do the thing"})
	if hasTag(tagsOf(body), ItemTag) {
		t.Errorf("tags = %v carry the item tag", tagsOf(body))
	}
	if p, want := body["prompt"], WithReportLine("do the thing", false); p != want {
		t.Errorf("prompt = %q, want %q", p, want)
	}
	if _, ok := body["item"]; ok {
		t.Errorf("an ordinary launch sends an item key: %v", body["item"])
	}
}
