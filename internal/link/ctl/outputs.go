package ctl

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// OpenOutputs makes every output schema s lists accept fields it does not name, so a client that cached the list
// keeps working when a newer build adds a field to an output.
func OpenOutputs(s *mcp.Server) {
	s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			lr, ok := res.(*mcp.ListToolsResult)
			if err != nil || !ok {
				return res, err
			}
			out := *lr
			out.Tools = make([]*mcp.Tool, len(lr.Tools))
			for i, t := range lr.Tools {
				tt := *t
				if tt.OutputSchema != nil {
					tt.OutputSchema = OpenSchema(tt.OutputSchema)
				}
				out.Tools[i] = &tt
			}
			return &out, nil
		}
	})
}

// OpenSchema is a copy of schema with every closed additionalProperties removed.
func OpenSchema(schema any) any {
	b, err := json.Marshal(schema)
	if err != nil {
		return schema
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return schema
	}
	openNode(v)
	return v
}

func openNode(v any) {
	switch n := v.(type) {
	case map[string]any:
		if ap, ok := n["additionalProperties"]; ok && closedSchema(ap) {
			delete(n, "additionalProperties")
		}
		for _, c := range n {
			openNode(c)
		}
	case []any:
		for _, c := range n {
			openNode(c)
		}
	}
}

func closedSchema(v any) bool {
	return v == false || reflect.DeepEqual(v, map[string]any{"not": map[string]any{}})
}
