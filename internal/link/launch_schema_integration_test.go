//go:build integration

package link

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

func TestEveryLaunchArgTheRoomReadsIsAdvertisedOnTheHub(t *testing.T) {
	ctx := context.Background()
	for _, class := range []ctl.Class{ctl.ClassFull, ctl.ClassWorker, ctl.ClassFullResident, ctl.ClassWorkerResident, ctl.ClassWorkerItem} {
		st, ct := mcp.NewInMemoryTransports()
		ss, err := newControl("", nil, nil).server(class).Connect(ctx, st, nil)
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
		for _, tl := range res.Tools {
			if tl.Name != "atrium_launch" && tl.Name != "atrium_launch_worker" {
				continue
			}
			props := schemaProps(t, tl.InputSchema)
			for _, arg := range daemonLaunchArgs() {
				if !props[arg] {
					t.Errorf("class %v: %s does not advertise %q, which the room reads", class, tl.Name, arg)
				}
			}
		}
	}
}

func schemaProps(t *testing.T, schema any) map[string]bool {
	t.Helper()
	m, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("input schema is %T", schema)
	}
	props, _ := m["properties"].(map[string]any)
	out := map[string]bool{}
	for k := range props {
		out[k] = true
	}
	return out
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
