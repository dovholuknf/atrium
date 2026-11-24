package ctl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAListedOutputSchemaAcceptsAFieldANewerBuildAdds(t *testing.T) {
	type room struct {
		Name string `json:"name"`
	}
	type out struct {
		Rooms []room `json:"rooms"`
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	OpenOutputs(s)
	mcp.AddTool(s, &mcp.Tool{Name: "rooms"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, out, error) {
		return nil, out{}, nil
	})
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
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
	b, err := json.Marshal(res.Tools[0].OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var sch jsonschema.Schema
	if err := json.Unmarshal(b, &sch); err != nil {
		t.Fatal(err)
	}
	r, err := sch.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	newer := map[string]any{"rooms": []any{map[string]any{"name": "m1", "hardware": map[string]any{}}}, "extra": 1}
	if err := r.Validate(newer); err != nil {
		t.Errorf("the listed schema rejects a field a newer build adds: %v", err)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "rooms"}); err != nil {
		t.Errorf("the call fails its own schema: %v", err)
	}
}
