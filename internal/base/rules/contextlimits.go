package rules

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SettingContextLimits is the context limit per harness, a JSON object of harness id to thousands of
// tokens. The hub owns it and hands it to every room. Empty reads as DefaultContextLimits.
const SettingContextLimits = "context_limits"

// The range a context limit takes.
const (
	MinContextLimitK = 10
	MaxContextLimitK = 2000
)

// DefaultContextLimits is the limit when nobody set one: claude at 200k, and nothing else cycles.
func DefaultContextLimits() map[string]int { return map[string]int{"claude": 200} }

// ParseContextLimits reads stored per-harness limits. Unset or unreadable reads as the default. A value out of
// range is dropped, so one bad entry does not turn the others off.
func ParseContextLimits(v string) map[string]int {
	if strings.TrimSpace(v) == "" {
		return DefaultContextLimits()
	}
	var raw map[string]int
	if json.Unmarshal([]byte(v), &raw) != nil {
		return DefaultContextLimits()
	}
	out := map[string]int{}
	for k, n := range raw {
		if k = strings.TrimSpace(k); k != "" && n >= MinContextLimitK && n <= MaxContextLimitK {
			out[k] = n
		}
	}
	return out
}

// CheckContextLimits validates typed per-harness limits and returns them as they are stored. An empty map is
// "back to the default" and is returned as "".
func CheckContextLimits(in map[string]int) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	clean := map[string]int{}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := in[k]
		name := strings.TrimSpace(k)
		if name == "" {
			return "", fmt.Errorf("context_limits needs a harness name for every limit")
		}
		if n < MinContextLimitK || n > MaxContextLimitK {
			return "", fmt.Errorf("context_limits for %s takes a whole number of thousands of tokens from %d to %d, not %d",
				name, MinContextLimitK, MaxContextLimitK, n)
		}
		clean[name] = n
	}
	b, err := json.Marshal(clean)
	return string(b), err
}
