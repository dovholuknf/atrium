package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"os"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// forkCardArgs is the card's restart command line, cut down to what the cache
// depends on. It is built by the code a restart uses: the harness's resume args,
// the card's stored extra args, and for a lean card the lean set. Model and effort
// are left to the fork, which names its own.
func forkCardArgs(st *store.Store, h *store.Harness, t *store.Task, hk *hooks) ([]string, error) {
	args, _, err := runnerArgsWith(h, t.ResumeID, "", launchOptions{Args: t.LaunchArgs})
	if err != nil {
		return nil, err
	}
	gateway := st.LeanWorkerGateway()
	if lean, mcp, kit, _ := leanOptions(shared.LaunchRequest{}, t, gateway); lean {
		if args, err = leanArgs(args, hk.readUserSettings(), "", mcp, kit, gateway, os.ReadFile, hk); err != nil {
			return nil, err
		}
	}
	return cycle.KeepFlags(args), nil
}

// forkEffortArgs is the card's launch effort in the shape its harness declares,
// appended to the fork's command line. Empty when the card was launched on the
// runner's default. The card's extra args are carried by forkCardArgs, filtered.
func forkEffortArgs(h *store.Harness, t *store.Task) ([]string, error) {
	return withMapped(h, nil, "effort", "level", h.EffortArgs, h.EffortEnv, t.Effort)
}
