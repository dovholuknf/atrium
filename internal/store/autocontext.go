package store

import (
	"fmt"
	"strconv"
	"strings"
)

// The settings behind the automatic new context. See
// docs/runtime/auto-new-context-design.md sections 1, 2 and 8.

const (
	// SettingAutoNewContext is which cards are cycled at the threshold: `off`, `tagged` or `agents`.
	// Empty means the default, which is off.
	SettingAutoNewContext = "auto_new_context"
	// SettingAutoNewContextK is the size, in thousands of tokens, at which a subject card is cycled.
	SettingAutoNewContextK = "auto_new_context_k"
	// SettingAutoNewContextIdleS is how long an agent card sits idle first, in seconds.
	SettingAutoNewContextIdleS = "auto_new_context_idle_s"
	// SettingContextCeilingK is the size, in thousands of tokens, at which a card tagged
	// atrium:context-ceiling is cycled, mid-turn included, whatever the mode above says.
	SettingContextCeilingK = "context_ceiling_k"
)

// The values of SettingAutoNewContext.
const (
	AutoNewContextOff    = "off"
	AutoNewContextTagged = "tagged"
	AutoNewContextAgents = "agents"
)

const (
	DefaultAutoNewContextK = 300
	MinAutoNewContextK     = 50
	MaxAutoNewContextK     = 2000

	DefaultContextCeilingK = 150
	MinContextCeilingK     = 50
	MaxContextCeilingK     = 2000

	DefaultAutoNewContextIdleS = 45
	MinAutoNewContextIdleS     = 10
	MaxAutoNewContextIdleS     = 3600
)

// AutoNewContextMode reads the setting. Anything unreadable is off, the safe side: this action
// discards a live conversation.
func (s *Store) AutoNewContextMode() string {
	v, err := s.Setting(SettingAutoNewContext)
	if err != nil {
		return AutoNewContextOff
	}
	switch v = strings.TrimSpace(strings.ToLower(v)); v {
	case AutoNewContextTagged, AutoNewContextAgents:
		return v
	}
	return AutoNewContextOff
}

// AutoNewContextK reads the threshold, in thousands of tokens. Unusable reads as the default.
func (s *Store) AutoNewContextK() int {
	return s.intSetting(SettingAutoNewContextK, DefaultAutoNewContextK, MinAutoNewContextK, MaxAutoNewContextK)
}

// ContextCeilingK reads the ceiling, in thousands of tokens. Unusable reads as the default.
func (s *Store) ContextCeilingK() int {
	return s.intSetting(SettingContextCeilingK, DefaultContextCeilingK, MinContextCeilingK, MaxContextCeilingK)
}

// AutoNewContextIdleS reads the idle quiet, in seconds. Unusable reads as the default.
func (s *Store) AutoNewContextIdleS() int {
	return s.intSetting(SettingAutoNewContextIdleS, DefaultAutoNewContextIdleS, MinAutoNewContextIdleS, MaxAutoNewContextIdleS)
}

func (s *Store) intSetting(key string, def, lo, hi int) int {
	v, err := s.Setting(key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

// CheckAutoNewContext validates a typed mode. Empty means the default.
func CheckAutoNewContext(v string) (string, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", AutoNewContextOff, AutoNewContextTagged, AutoNewContextAgents:
		return v, nil
	}
	return "", fmt.Errorf("auto_new_context is off, tagged or agents, not %q", v)
}

// CheckAutoNewContextK validates a typed threshold. Out of range is refused, and empty means the
// default. The check against context_threshold_k needs that setting and is made by the caller.
func CheckAutoNewContextK(v string) (string, error) {
	return checkInt("auto_new_context_k", "thousands of tokens", v, MinAutoNewContextK, MaxAutoNewContextK)
}

// CheckContextCeilingK validates a typed ceiling. The check against context_threshold_k is the caller's.
func CheckContextCeilingK(v string) (string, error) {
	return checkInt("context_ceiling_k", "thousands of tokens", v, MinContextCeilingK, MaxContextCeilingK)
}

// CheckAutoNewContextIdleS validates a typed idle quiet, in seconds.
func CheckAutoNewContextIdleS(v string) (string, error) {
	return checkInt("auto_new_context_idle_s", "seconds", v, MinAutoNewContextIdleS, MaxAutoNewContextIdleS)
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

// The range a context limit takes, the board default's own: context_threshold_k.
const (
	MinContextLimitK = 10
	MaxContextLimitK = 2000
)

// CheckContextLimitK validates a runner's context limit. Zero means none and is the
// "use the board default" answer. Out of range is refused rather than clamped.
func CheckContextLimitK(k int) error {
	if k == 0 || (k >= MinContextLimitK && k <= MaxContextLimitK) {
		return nil
	}
	return fmt.Errorf("context_limit_k takes a whole number of thousands of tokens from %d to %d, or none, not %d",
		MinContextLimitK, MaxContextLimitK, k)
}

// CheckContextLimitText validates a typed card limit, the text form of CheckContextLimitK. Empty
// means none and is returned as empty, which removes the override.
func CheckContextLimitText(v string) (string, error) {
	return checkInt("context_limit_k", "thousands of tokens", v, MinContextLimitK, MaxContextLimitK)
}
