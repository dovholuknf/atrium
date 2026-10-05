package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// SettingContextThresholdK is the context size, in thousands of tokens, past
// which a card's size mark turns the warn colour and an agent-launched card's
// launcher is told once. Every turn past it re-reads all of it.
const SettingContextThresholdK = "context_threshold_k"

// OverrideContextLimitK is the key of a card's own context limit in its overrides, in thousands of tokens.
// Empty means none: the runner's limit applies, then the board default.
const OverrideContextLimitK = "context_limit_k"

// The layers a context limit can come from, named on the row so the board can say which.
const (
	LimitFromCard   = "card"
	LimitFromRunner = "runner"
	LimitFromBoard  = "board"
)

// ContextLimitFor is the context limit in force for a card, in thousands of tokens, and which layer it came from:
// the card's own, else its runner's, else the board default. An unusable value in a layer is skipped.
func ContextLimitFor(st *store.Store, t *store.Task) (int, string) {
	if t != nil {
		if k, err := strconv.Atoi(strings.TrimSpace(t.Overrides[OverrideContextLimitK])); err == nil &&
			store.CheckContextLimitK(k) == nil && k > 0 {
			return k, LimitFromCard
		}
		if h, err := st.Harness(t.Runner); err == nil && h != nil && h.ContextLimitK > 0 &&
			store.CheckContextLimitK(h.ContextLimitK) == nil {
			return h.ContextLimitK, LimitFromRunner
		}
	}
	return contextThresholdK(st), LimitFromBoard
}

const (
	defaultContextThresholdK = 150
	minContextThresholdK     = 10
	maxContextThresholdK     = 2000
)

// ContextThreshold is the threshold in force, in tokens. Anything unusable
// reads as the default, the same rule TerminalMinCols follows.
func ContextThreshold(st *store.Store) int64 {
	return int64(contextThresholdK(st)) * 1000
}

func contextThresholdK(st *store.Store) int {
	v, err := st.Setting(SettingContextThresholdK)
	if err != nil {
		return defaultContextThresholdK
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < minContextThresholdK || n > maxContextThresholdK {
		return defaultContextThresholdK
	}
	return n
}

// checkContextThresholdK validates a typed threshold. Out of range is refused
// rather than clamped, and cleared means back to the default.
func checkContextThresholdK(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < minContextThresholdK || n > maxContextThresholdK {
		return "", fmt.Errorf("the context threshold takes a whole number of thousands of tokens from %d to %d, not %q",
			minContextThresholdK, maxContextThresholdK, value)
	}
	return strconv.Itoa(n), nil
}

// checkAutoNewContextK validates a typed automatic threshold. It may not be below the context
// threshold that tells a launcher, in force or typed in the same request, since the launcher must
// hear before the context is cleared. The daemon reads it with the same floor.
func checkAutoNewContextK(st *store.Store, value string, typedNotice *string) (string, error) {
	v, err := store.CheckAutoNewContextK(value)
	if err != nil || v == "" {
		return v, err
	}
	notice := noticeFloor(st, typedNotice)
	if n, _ := strconv.Atoi(v); n < notice {
		return "", fmt.Errorf("auto_new_context_k is %d, below context_threshold_k at %d: the launcher is told at the "+
			"lower number, so the automatic new context cannot come before it", n, notice)
	}
	return v, nil
}

// noticeFloor is the context threshold in force, or the one typed in the same request.
func noticeFloor(st *store.Store, typedNotice *string) int {
	notice := contextThresholdK(st)
	if typedNotice != nil {
		tv, terr := checkContextThresholdK(*typedNotice)
		switch {
		case terr != nil:
		case tv == "":
			notice = defaultContextThresholdK
		default:
			notice, _ = strconv.Atoi(tv)
		}
	}
	return notice
}

// checkContextCeilingK validates a typed ceiling, with the same floor as the automatic threshold.
func checkContextCeilingK(st *store.Store, value string, typedNotice *string) (string, error) {
	v, err := store.CheckContextCeilingK(value)
	if err != nil || v == "" {
		return v, err
	}
	if notice := noticeFloor(st, typedNotice); mustAtoi(v) < notice {
		return "", fmt.Errorf("context_ceiling_k is %s, below context_threshold_k at %d: the launcher is told at the "+
			"lower number, so the ceiling cannot come before it", v, notice)
	}
	return v, nil
}

func mustAtoi(s string) int { n, _ := strconv.Atoi(s); return n }

// EffectiveContextCeilingK is the ceiling in force, in thousands of tokens, never below the context
// threshold, for the same reason as EffectiveAutoNewContextK.
func EffectiveContextCeilingK(st *store.Store) int {
	k := st.ContextCeilingK()
	if floor := contextThresholdK(st); k < floor {
		k = floor
	}
	return k
}

// autoNewContextView is the automatic new context as stored, and what is in force.
func autoNewContextView(st *store.Store, out map[string]any) {
	for key, field := range map[string]string{
		store.SettingAutoNewContext:      "auto_new_context",
		store.SettingAutoNewContextK:     "auto_new_context_k",
		store.SettingAutoNewContextIdleS: "auto_new_context_idle_s",
		store.SettingContextCeilingK:     "context_ceiling_k",
	} {
		v, _ := st.Setting(key)
		out[field] = v
	}
	out["context_ceiling_k_now"] = EffectiveContextCeilingK(st)
	out["context_ceiling_k_default"] = store.DefaultContextCeilingK
	out["context_ceiling_k_min"] = store.MinContextCeilingK
	out["context_ceiling_k_max"] = store.MaxContextCeilingK
	out["auto_new_context_now"] = st.AutoNewContextMode()
	out["auto_new_context_modes"] = []string{store.AutoNewContextOff, store.AutoNewContextTagged, store.AutoNewContextAgents}
	out["auto_new_context_k_now"] = EffectiveAutoNewContextK(st)
	out["auto_new_context_k_default"] = store.DefaultAutoNewContextK
	out["auto_new_context_k_min"] = store.MinAutoNewContextK
	out["auto_new_context_k_max"] = store.MaxAutoNewContextK
	out["auto_new_context_idle_s_now"] = st.AutoNewContextIdleS()
	out["auto_new_context_idle_s_default"] = store.DefaultAutoNewContextIdleS
	out["auto_new_context_idle_s_min"] = store.MinAutoNewContextIdleS
	out["auto_new_context_idle_s_max"] = store.MaxAutoNewContextIdleS
}

// EffectiveAutoNewContextK is the setting in force, in thousands of tokens, never below the
// context threshold: a context_threshold_k raised after the setting was made would otherwise put
// the automatic cycle ahead of the notice it is meant to follow.
func EffectiveAutoNewContextK(st *store.Store) int {
	k := st.AutoNewContextK()
	if floor := contextThresholdK(st); k < floor {
		k = floor
	}
	return k
}

// ContextSizeOf returns a card's context size as the board draws it, or nil.
// Supplied by the daemon, which reads it from the card's transcript and holds
// it in memory. Never stored. See docs/runtime/activity-design.md.
var ContextSizeOf func(taskID string) any

// AutocompactOf returns a card's context limit and runner compaction window, or nil.
// Supplied by the daemon.
var AutocompactOf func(t *store.Task) any
