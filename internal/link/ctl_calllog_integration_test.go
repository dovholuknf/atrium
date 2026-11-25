//go:build integration

package link

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/calllog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestControlToolCallIsLoggedBeforeItRuns(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub-calls.log")
	lg := calllog.New(dir, "hub-calls.log", 0)
	t.Cleanup(lg.Close)
	c := &controlMCP{calls: lg}
	var inside string
	h := c.logCalls(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		b, _ := os.ReadFile(p)
		inside = string(b)
		return &mcp.CallToolResult{IsError: true}, nil
	})
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "atrium_wake"},
		Extra:  &mcp.RequestExtra{Header: http.Header{"X-Atrium-Agent": {"card-9"}, "X-Atrium-Room": {"sg4"}}},
	}
	if _, err := h(context.Background(), "tools/call", req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inside, `"name":"atrium_wake"`) || !strings.Contains(inside, `"caller":"card-9"`) ||
		strings.Contains(inside, `"event":"end"`) {
		t.Fatalf("log while the tool ran = %q", inside)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"event":"end"`) || !strings.Contains(string(b), `"outcome":"error"`) {
		t.Fatalf("log after = %q", b)
	}
}
