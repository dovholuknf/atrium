package shared

import (
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// CycleBy is who a context cycle records itself as.
const CycleBy = "context-cycle"

// ActivityThinking is the activity a card shows while a turn runs.
const ActivityThinking = "thinking"

// AtriumLabel is the grey `[atrium] ...` label atrium types ahead of text that
// is not the operator's. peerBanner is one. The after-restart wake is the other.
// The rule peerBanner gives holds for every label: no carriage return in `what`.
func AtriumLabel(what string) string {
	return "\x1b[38;5;244m[atrium] " + what + " \x1b[0m"
}

// OrWord is a fallback for a word a runner did not supply.
func OrWord(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// UsageOfRefresh is a keep-alive refresh as a usage row. The fork writes at
// the 1h rate, because only cards on the 1h cache are refreshed.
func UsageOfRefresh(r *store.KeepaliveRefresh) *store.SessionUsage {
	return &store.SessionUsage{
		TaskID: r.TaskID, ResumeID: r.ResumeID, Started: r.At, Ended: r.At, Cause: store.UsageKeepalive,
		Model: r.Model, Replies: 1, Input: r.Input, Output: r.Output, CacheWrite1h: r.CacheWrite,
		CacheRead: r.CacheRead, Context: r.Context,
	}
}

// EnvDuration reads a duration from the environment, or def when it is unset or not positive.
func EnvDuration(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// LooksIdleAfter is how long a pty must be silent before the frame is looked at.
var LooksIdleAfter = EnvDuration("ATRIUM_LOOKS_IDLE", 25*time.Second)
