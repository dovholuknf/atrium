package daemon

import (
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// What the context cycle shares with the rest of the room, hoisted to shared so the cycle package needs no Daemon.
const (
	cycleBy          = shared.CycleBy
	ActivityThinking = shared.ActivityThinking
)

func viaShellIfScript(resolved string, args []string) (string, []string) {
	return shared.ViaShellIfScript(resolved, args)
}
func atriumLabel(what string) string                               { return shared.AtriumLabel(what) }
func orWord(s, fallback string) string                             { return shared.OrWord(s, fallback) }
func usageOfRefresh(r *store.KeepaliveRefresh) *store.SessionUsage { return shared.UsageOfRefresh(r) }
func envDuration(name string, def time.Duration) time.Duration     { return shared.EnvDuration(name, def) }
