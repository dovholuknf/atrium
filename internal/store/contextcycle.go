package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The settings behind the context cycle. See docs/runtime/context-cycle-design.md.

const (
	// SettingContextLimits is the context limit per harness, a JSON object of harness id to thousands of
	// tokens. The hub owns it and hands it to every room. Empty reads as DefaultContextLimits.
	SettingContextLimits = "context_limits"
	// SettingContextHandoffDir is where a cycling card writes its handoff, an absolute directory. Empty
	// means the default under the temp directory.
	SettingContextHandoffDir = "context_handoff_dir"
)

// DefaultContextLimits is the limit when nobody set one: claude at 200k, and nothing else cycles.
func DefaultContextLimits() map[string]int { return map[string]int{"claude": 200} }

// ContextLimits reads the per-harness limits. Unset or unreadable reads as the default. A value out of
// range is dropped, so one bad entry does not turn the others off.
func (s *Store) ContextLimits() map[string]int {
	v, err := s.Setting(SettingContextLimits)
	if err != nil || strings.TrimSpace(v) == "" {
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

// ContextHandoffDir reads the handoff directory, or "" for the default.
func (s *Store) ContextHandoffDir() string {
	v, err := s.Setting(SettingContextHandoffDir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// CheckContextHandoffDir validates a typed handoff directory: absolute, or empty for the default.
func CheckContextHandoffDir(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if !filepath.IsAbs(v) {
		return "", fmt.Errorf("context_handoff_dir must be an absolute directory, not %q", v)
	}
	return filepath.Clean(v), nil
}

func checkInt(name, unit, v string, lo, hi int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return "", fmt.Errorf("%s takes a whole number of %s from %d to %d, not %q", name, unit, lo, hi, v)
	}
	return strconv.Itoa(n), nil
}

// The range a context limit takes.
const (
	MinContextLimitK = 10
	MaxContextLimitK = 2000
)

// CheckContextLimitK validates a runner's context limit. Zero means none. Out of range is refused rather than
// clamped. The runner layer is no longer read (the hub's per-harness limit replaced it), but the column is
// still saved.
func CheckContextLimitK(k int) error {
	if k == 0 || (k >= MinContextLimitK && k <= MaxContextLimitK) {
		return nil
	}
	return fmt.Errorf("context_limit_k takes a whole number of thousands of tokens from %d to %d, or none, not %d",
		MinContextLimitK, MaxContextLimitK, k)
}

// CheckContextLimitText validates a typed card limit, the text form of CheckContextLimitK. Empty means none and
// is returned as empty, which removes the override.
func CheckContextLimitText(v string) (string, error) {
	return checkInt("context_limit_k", "thousands of tokens", v, MinContextLimitK, MaxContextLimitK)
}

// CheckContextCycleText validates a card's cycle switch: "off", or empty for on.
func CheckContextCycleText(v string) (string, error) {
	switch v = strings.TrimSpace(strings.ToLower(v)); v {
	case "", "on":
		return "", nil
	case "off":
		return v, nil
	}
	return "", fmt.Errorf("context_cycle is off, or empty for on, not %q", v)
}
