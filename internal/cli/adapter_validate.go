package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/docs/runtime/contract"
)

// validateAgainst checks raw JSON against the contract schema named, and returns one line per fault. It knows only the
// keywords the contract's schemas use: type, required, properties, enum, items and anyOf of required lists.
func validateAgainst(name string, raw []byte) []string {
	b, err := contract.Schemas.ReadFile(name + ".schema.json")
	if err != nil {
		return []string{"no schema " + name}
	}
	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		return []string{name + ": schema unreadable: " + err.Error()}
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	return validateNode("", schema, doc)
}

func validateNode(at string, schema map[string]any, v any) []string {
	var faults []string
	if types := schemaTypes(schema["type"]); len(types) > 0 && !anyJSONType(types, v) {
		return []string{fmt.Sprintf("%s: want %s, got %T", pathOr(at), strings.Join(types, " or "), v)}
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if e == v {
				found = true
			}
		}
		if !found {
			faults = append(faults, fmt.Sprintf("%s: %v is not one of %v", pathOr(at), v, enum))
		}
	}
	switch x := v.(type) {
	case map[string]any:
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, there := x[r.(string)]; !there {
					faults = append(faults, fmt.Sprintf("%s: missing %q", pathOr(at), r))
				}
			}
		}
		if alts, ok := schema["anyOf"].([]any); ok && !anyAltHolds(alts, x) {
			faults = append(faults, pathOr(at)+": none of the alternatives is present")
		}
		props, _ := schema["properties"].(map[string]any)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if sub, ok := props[k].(map[string]any); ok {
				faults = append(faults, validateNode(at+"/"+k, sub, x[k])...)
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range x {
				faults = append(faults, validateNode(fmt.Sprintf("%s/%d", at, i), items, e)...)
			}
		}
	}
	return faults
}

func anyAltHolds(alts []any, obj map[string]any) bool {
	for _, a := range alts {
		req, _ := a.(map[string]any)["required"].([]any)
		ok := true
		for _, r := range req {
			if s, _ := obj[r.(string)].(string); s == "" {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// schemaTypes reads "type", which is one word or a list of them.
func schemaTypes(t any) []string {
	switch x := t.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, e.(string))
		}
		return out
	}
	return nil
}

func anyJSONType(types []string, v any) bool {
	for _, t := range types {
		if isJSONType(t, v) {
			return true
		}
	}
	return false
}

func isJSONType(t string, v any) bool {
	switch t {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "null":
		return v == nil
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	}
	return true
}

func pathOr(at string) string {
	if at == "" {
		return "/"
	}
	return at
}
