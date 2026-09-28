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

// ContextSizeOf returns a card's context size as the board draws it, or nil.
// Supplied by the daemon, which reads it from the card's transcript and holds
// it in memory. Never stored. See docs/activity-design.md.
var ContextSizeOf func(taskID string) any
