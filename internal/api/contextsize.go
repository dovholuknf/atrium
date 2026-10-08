package api

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	// LimitFromRunner is a limit the runner's own compaction point cut down. See RunnerCeilingK.
	LimitFromRunner = "runner"
)

// RunnerCompactAtK says where a card's runner compacts its own conversation, in thousands of tokens, or 0 when it
// does not or atrium cannot tell. Supplied by the daemon, which can read the machine's Claude Code settings.
var RunnerCompactAtK func(t *store.Task) int

// RunnerCeilingK is the most a card's limit may be, in thousands of tokens, or 0 for no ceiling. Atrium wants
// the runner's compaction to land 10 percent AFTER its own cycle point (autocompactK in the daemon), so the
// ceiling is the runner's compaction point less that 10 percent. A runner that compacts at 220k gives 200k.
func RunnerCeilingK(t *store.Task) int {
	if RunnerCompactAtK == nil || t == nil {
		return 0
	}
	at := RunnerCompactAtK(t)
	if at <= 0 {
		return 0
	}
	if c := at * 10 / 11; c >= store.MinContextLimitK {
		return c
	}
	return store.MinContextLimitK
}

// ContextLimit is the limit in force on a card and how it was reached.
type ContextLimit struct {
	// K is the limit atrium acts on, in thousands of tokens. 0 is none.
	K int
	// From is the layer K came from: card, hub, default, or runner when the runner's ceiling cut it.
	From string
	// WantedK and WantedFrom are the limit before the ceiling, when the ceiling cut it. Zero when it did not.
	WantedK    int
	WantedFrom string
	// OwnK is the card's own limit as stored, 0 when it has none. Kept even when it is cut or not in force.
	OwnK int
	// CeilingK is the runner's ceiling, 0 when there is none.
	CeilingK int
}

// ContextLimitOf resolves a card's limit: the smaller of the card's own (else the hub's for its harness, else
// the built-in one) and the runner's ceiling.
func ContextLimitOf(st *store.Store, t *store.Task) ContextLimit {
	var out ContextLimit
	if t == nil {
		return out
	}
	if k, err := strconv.Atoi(strings.TrimSpace(t.Overrides[OverrideContextLimitK])); err == nil &&
		store.CheckContextLimitK(k) == nil && k > 0 {
		out.OwnK = k
		out.K, out.From = k, LimitFromCard
	} else {
		limits, from := st.ContextLimits(), LimitFromHub
		if v, _ := st.Setting(store.SettingContextLimits); strings.TrimSpace(v) == "" {
			from = LimitFromDefault
		}
		if k := limits[t.Runner]; k > 0 {
			out.K, out.From = k, from
		} else if h, err := st.Harness(t.Runner); err == nil && claudeHarness(h) {
			if k := limits["claude"]; k > 0 {
				out.K, out.From = k, from
			}
		}
	}
	if out.K > 0 {
		if c := RunnerCeilingK(t); c > 0 {
			out.CeilingK = c
			if out.K > c {
				out.WantedK, out.WantedFrom = out.K, out.From
				out.K, out.From = c, LimitFromRunner
			}
		}
	}
	return out
}

// ContextLimitFor is the context limit in force for a card, in thousands of tokens, and which layer it came
// from: the card's own, else the hub's limit for its harness, else the built-in one, and never above the
// runner's ceiling (then the layer is "runner"). A harness with no entry whose command is claude takes the
// `claude` entry. Zero means the card has no limit and never cycles.
func ContextLimitFor(st *store.Store, t *store.Task) (int, string) {
	l := ContextLimitOf(st, t)
	return l.K, l.From
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
	out["restart_wrap_wait_s"] = int(st.RestartWrapWait() / time.Second)
}

// ContextSizeOf returns a card's context size as the board draws it, or nil.
// Supplied by the daemon, which reads it from the card's transcript and holds
// it in memory. Never stored. See docs/runtime/activity-design.md.
var ContextSizeOf func(taskID string) any

// AutocompactOf returns a card's context limit and runner compaction window, or nil.
// Supplied by the daemon.
var AutocompactOf func(t *store.Task) any

// IsClaudeHarness is whether a runner row runs claude, for the daemon's read of claude's own settings.
func IsClaudeHarness(h *store.Harness) bool { return claudeHarness(h) }
