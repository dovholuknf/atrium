//go:build integration

package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

func TestEveryLaunchArgTheRoomReadsIsAdvertisedByStdioControl(t *testing.T) {
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
	seen := 0
	for _, tl := range res.Tools {
		if tl.Name != "atrium_launch" && tl.Name != "atrium_launch_worker" {
			continue
		}
		seen++
		for _, arg := range missingLaunchArgs(tl.InputSchema, daemonLaunchArgs()) {
			t.Errorf("%s does not advertise %q, which the room reads", tl.Name, arg)
		}
	}
	if seen != 2 {
		t.Fatalf("found %d launch tools, want 2", seen)
	}
}

func daemonLaunchArgs() []string {
	notAgentFacing := map[string]bool{
		"harness": true, "resume": true, "task_id": true, "moved_from": true, "pinned": true, "pin_order": true,
		"launch_key": true, "window": true, "throwaway": true, "if_running": true, "spawned_by": true,
		"spawned_by_id": true, "spawned_by_dir": true, "report_to": true, "outside_code": true, "work_item": true,
		"item": true, "lifetime": true, "source": true, "external_id": true, "url": true, "source_kind": true,
		"source_url": true, "org": true, "host": true,
	}
	var out []string
	rt := reflect.TypeOf(shared.LaunchRequest{})
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" || notAgentFacing[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}
