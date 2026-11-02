//go:build integration

package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// roomSession is a room's stdio control server, with its hub calls going over the room's real link, and a client on it.
func roomSession(t *testing.T, x *relayPair, r *Room, by, room string) *mcp.ClientSession {
	t.Helper()
	call := func(ctx context.Context, method, path string, body, out any) error {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		ans, err := r.Relay(ctx, wire.RelayRequest{Op: RelayHub, From: by, Method: method, Path: path, Body: raw})
		if err != nil {
			return err
		}
		if !ans.OK {
			return errors.New(ans.Error)
		}
		return json.Unmarshal(ans.Data, out)
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "room"}, nil)
	AddRoomBacklogTools(s, call, by, room)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(relayCtx(t), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return res, sb.String()
}

// THE FEATURE. A session on a room that is not the hub's machine calls atrium_backlog and reads the hub's row, and a
// row it files is the hub's, stamped with the room it came from rather than the one it says.
func TestARoomSessionsBacklogToolReachesTheHubOverTheLink(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	t.Cleanup(func() { x.proxy.WaitWork(10 * time.Second) })
	x.proxy.SetBacklog(st)

	// The hub's own board files the row, as the orchestrator on the hub machine would.
	code, body := (&backlogRig{p: x.proxy}).send("POST", "/_hub/backlog", js(map[string]any{"id": "f-hub-row",
		"dept": "fabric", "title": "filed on the hub", "body": "the whole spec", "by": "orch@sg4", "room": "sg4"}), loopAddr)
	if code != http.StatusCreated {
		t.Fatalf("file on the hub: %d %s", code, body)
	}

	cs := roomSession(t, x, x.miniR, "sa1", "sg4")
	res, text := callTool(t, cs, "atrium_backlog", map[string]any{"action": "get", "id": "f-hub-row"})
	if res.IsError || !strings.Contains(text, "the whole spec") || !strings.Contains(text, "filed on the hub") {
		t.Fatalf("get: %v %s", res.IsError, text)
	}
	if _, text = callTool(t, cs, "atrium_backlog", map[string]any{"action": "list", "open": true}); !strings.Contains(text, "f-hub-row") {
		t.Fatalf("list: %s", text)
	}

	// A write: the room says it is sg4, the hub knows it is m1mini.
	res, text = callTool(t, cs, "atrium_backlog", map[string]any{"action": "file", "id": "f-from-room", "dept": "fabric",
		"title": "filed by a room worker"})
	if res.IsError || !strings.Contains(text, `"filed_room":"m1mini"`) || !strings.Contains(text, "sa1@m1mini") {
		t.Fatalf("file: %v %s", res.IsError, text)
	}
	if res, text = callTool(t, cs, "atrium_backlog", map[string]any{"action": "status", "id": "f-from-room",
		"status": "held"}); res.IsError || !strings.Contains(text, `"held"`) {
		t.Fatalf("status: %v %s", res.IsError, text)
	}
	if res, _ = callTool(t, cs, "atrium_backlog", map[string]any{"action": "get", "id": "nope"}); !res.IsError {
		t.Fatal("a missing item was not an error")
	}

	// Reports, the same door.
	res, text = callTool(t, cs, "atrium_reports", map[string]any{"action": "add", "subject": "landed", "body": "sha abc"})
	if res.IsError || !strings.Contains(text, "rp_1") {
		t.Fatalf("report: %v %s", res.IsError, text)
	}
}

// Only the two endpoints, and nothing the hub keeps for itself.
func TestTheHubRelaysOnlyTheBacklogAndReportsEndpoints(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	for _, c := range []struct{ method, path string }{
		{"GET", "/_hub/inventory"},
		{"GET", "/_hub/backlogs"},
		{"GET", "/_hub/backlog/../inventory"},
		{"POST", "/_hub/backlog/follow"},
		{"GET", "/v1/tasks"},
		{"DELETE", "/_hub/backlog"},
	} {
		ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayHub, From: "sa1", Method: c.method, Path: c.path})
		if err != nil || ans.OK || ans.Code != http.StatusBadRequest {
			t.Errorf("%s %s: %+v %v", c.method, c.path, ans, err)
		}
	}
}
