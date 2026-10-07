package api

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE CONTEXT LIMIT, the one line a card's context is held to. Past it atrium cycles the card's context. See
// docs/runtime/context-cycle-design.md.

// OverrideContextLimitK is the key of a card's own context limit in its overrides, in thousands of tokens.
// Empty means none: the hub's limit for its harness applies.
const OverrideContextLimitK = "context_limit_k"

// OverrideContextCycle is the card's own switch: "off" stops atrium cycling it. Empty means on.
const OverrideContextCycle = "context_cycle"

// The layers a context limit can come from, named on the row so the board can say which.
const (
	LimitFromCard    = "card"
	LimitFromHub     = "hub"
	LimitFromDefault = "default"
)

// ContextLimitFor is the context limit in force for a card, in thousands of tokens, and which layer it came
// from: the card's own, else the hub's limit for its harness, else the built-in one. A harness with no entry
// whose command is claude takes the `claude` entry. Zero means the card has no limit and never cycles.
func ContextLimitFor(st *store.Store, t *store.Task) (int, string) {
	if t == nil {
		return 0, ""
	}
	if k, err := strconv.Atoi(strings.TrimSpace(t.Overrides[OverrideContextLimitK])); err == nil &&
		store.CheckContextLimitK(k) == nil && k > 0 {
		return k, LimitFromCard
	}
	limits, from := st.ContextLimits(), LimitFromHub
	if v, _ := st.Setting(store.SettingContextLimits); strings.TrimSpace(v) == "" {
		from = LimitFromDefault
	}
	if k := limits[t.Runner]; k > 0 {
		return k, from
	}
	if h, err := st.Harness(t.Runner); err == nil && claudeHarness(h) {
		if k := limits["claude"]; k > 0 {
			return k, from
		}
	}
	return 0, ""
}

// ContextCycleOn is whether the card's own switch leaves cycling on.
func ContextCycleOn(t *store.Task) bool {
	return t != nil && !strings.EqualFold(strings.TrimSpace(t.Overrides[OverrideContextCycle]), "off")
}

// claudeHarness is a harness that runs claude, by id or by its command's name.
func claudeHarness(h *store.Harness) bool {
	if h == nil {
		return false
	}
	if strings.EqualFold(h.ID, "claude") {
		return true
	}
	leaf := strings.ToLower(filepath.Base(filepath.FromSlash(strings.TrimSpace(h.Cmd))))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		leaf = strings.TrimSuffix(leaf, ext)
	}
	return leaf == "claude"
}

// contextCycleView is the context cycle's settings as stored and in force.
func contextCycleView(st *store.Store, out map[string]any) {
	out["context_limits"] = st.ContextLimits()
	out["context_limits_default"] = store.DefaultContextLimits()
	out["context_limit_k_min"] = store.MinContextLimitK
	out["context_limit_k_max"] = store.MaxContextLimitK
	out["context_handoff_dir"] = st.ContextHandoffDir()
}

// ContextSizeOf returns a card's context size as the board draws it, or nil.
// Supplied by the daemon, which reads it from the card's transcript and holds
// it in memory. Never stored. See docs/runtime/activity-design.md.
var ContextSizeOf func(taskID string) any

// AutocompactOf returns a card's context limit and runner compaction window, or nil.
// Supplied by the daemon.
var AutocompactOf func(t *store.Task) any
