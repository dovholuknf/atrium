package daemon

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// keepaliveForkEnv is the fork's environment: the one a launch builds, minus the card's
// identity, with atrium's hooks off and the card's 1h cache pin.
//
// THE CARD'S OWN LAUNCH ENV AND EFFORT GO IN TOO, layered as a launch layers
// them (see launchOptionEnv). A fork without the card's env can reach a
// different endpoint or account than the card it is warming, and then it warms
// nothing and bills somebody else. The model is not passed here: the fork names
// it as `--model`, from the card's last reply.
func keepaliveForkEnv(h *store.Harness, t *store.Task, baseEnv []string) ([]string, error) {
	added, err := launchOptionEnv(h, t.LaunchEnv, "", t.Effort)
	if err != nil {
		return nil, err
	}
	env := overEnv(h.Env, added)
	base := make([]string, 0, 64)
	for _, kv := range baseEnv {
		if !strings.HasPrefix(strings.ToUpper(kv), "ATRIUM_PERM_GATE=") {
			base = append(base, kv)
		}
	}
	extra := map[string]string{"ATRIUM_PERM_GATE": "off"}
	// The memory section of the system prompt is in the cache key.
	if lean, _, _, _ := leanOptions(shared.LaunchRequest{}, t, ""); lean {
		leanEnv(extra)
	}
	if _, set := env[cycle.KeepaliveTTLVar]; !set {
		extra[cycle.KeepaliveTTLVar] = "1h"
	}
	return childEnvFrom(base, env, extra), nil
}
