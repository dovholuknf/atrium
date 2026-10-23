//go:build integration

package cli

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// A lean worker on a room whose atrium-control is the stdio server must still have every tool its prompts name,
// atrium_publish included. See docs/changes/publish-lean.md.
func TestStdioControlServesEveryToolTheLeanPromptsName(t *testing.T) {
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := controlServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range res.Tools {
		have[tl.Name] = true
	}
	want := append(daemon.PromptToolNames(), "atrium_publish", "atrium_merge")
	for _, name := range want {
		if !have[name] {
			t.Errorf("stdio control lacks %s", name)
		}
	}
}
