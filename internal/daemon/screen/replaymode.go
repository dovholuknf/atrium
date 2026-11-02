package screen

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// ReplayMode is how this daemon has been asked to replay history.
//
// Read on every attach rather than cached, so flipping it takes effect on the
// next attach instead of on the next restart. An attach is a websocket upgrade
// and one settings read is nothing beside it.
//
// A STORE THAT CANNOT ANSWER GETS THE DEFAULT. A halted store means the daemon
// is already reporting a failure, and picking some other rendering on top of
// that would be a second surprise.
func ReplayMode(st *store.Store) string {
	v, err := st.Setting(store.SettingReplayMode)
	if err != nil {
		return "screen"
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "raw":
		return "raw"
	case "flat", "flatten", "on", "1", "true", "yes":
		return "flat"
	}
	return "screen"
}
