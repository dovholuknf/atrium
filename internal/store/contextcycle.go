package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/rules"
)

// The settings behind the context cycle. See docs/runtime/context-cycle-design.md.

const (
	// SettingContextHandoffDir is where a cycling card writes its handoff, an absolute directory. Empty
	// means the default under the temp directory.
	SettingContextHandoffDir = "context_handoff_dir"
)

// ContextLimits reads the per-harness limits. Unset or unreadable reads as the default.
func (s *Store) ContextLimits() map[string]int {
	v, err := s.Setting(rules.SettingContextLimits)
	if err != nil {
		return rules.DefaultContextLimits()
	}
	return rules.ParseContextLimits(v)
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

// CheckContextLimitK validates a runner's context limit. Zero means none. Out of range is refused rather than
// clamped. The runner layer is no longer read (the hub's per-harness limit replaced it), but the column is
// still saved.
func CheckContextLimitK(k int) error {
	if k == 0 || (k >= rules.MinContextLimitK && k <= rules.MaxContextLimitK) {
		return nil
	}
	return fmt.Errorf("context_limit_k takes a whole number of thousands of tokens from %d to %d, or none, not %d",
		rules.MinContextLimitK, rules.MaxContextLimitK, k)
}

// CheckContextLimitText validates a typed card limit, the text form of CheckContextLimitK. Empty means none and
// is returned as empty, which removes the override.
func CheckContextLimitText(v string) (string, error) {
	return checkInt("context_limit_k", "thousands of tokens", v, rules.MinContextLimitK, rules.MaxContextLimitK)
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
