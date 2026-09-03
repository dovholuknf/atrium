//go:build windows

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Running a shell command and keeping the environment it produced.
//
// The pattern this serves: a shell function that puts a toolchain on PATH,
// run before starting an agent so the agent can see it. Doing that by hand
// means opening a terminal, and an agent started from a board has no terminal
// to run it in.
//
// The environment is captured and handed to the runner, rather than starting
// the runner underneath a shell. Under a shell the runner is no longer the
// process atrium owns, and the exit keys, the liveness check and terminate all
// start acting on the wrong process.

// prepareTimeout bounds a prepare command. It is somebody's shell function, so
// it should be instant, and a launch that hangs here would look like the runner
// failing to start.
const prepareTimeout = 20 * time.Second

// captureEnv runs prepare in a shell and returns the environment afterwards.
//
// The profile is loaded on purpose. A function like `add-clion_tools` is
// defined in the operator's profile, so running with -NoProfile would report
// that their own command does not exist.
func captureEnv(prepare, cwd string) (map[string]string, error) {
	if strings.TrimSpace(prepare) == "" {
		return nil, nil
	}
	shell := powershell()

	// Stop BEFORE the command, not after. Left until afterwards, a prepare
	// command that does not exist writes to stderr, carries on, and the env
	// dump succeeds, so the launch reports nothing and the agent starts
	// without the tools it was prepared for.
	//
	// -AsArray so a prepare command that somehow leaves one variable still
	// produces a list rather than a bare object.
	script := `$ErrorActionPreference = 'Stop'
` + prepare + `
Get-ChildItem env: | Select-Object Name,Value | ConvertTo-Json -Compress -Depth 2 -AsArray`

	ctx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-NonInteractive", "-Command", script)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("the prepare command did not finish in %s", prepareTimeout)
		}
		// Stderr carries what the shell actually complained about, which is
		// the only useful half of this error.
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("the prepare command failed: %s",
				firstLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("the prepare command failed: %w", err)
	}

	var pairs []struct{ Name, Value string }
	if err := json.Unmarshal(out, &pairs); err != nil {
		return nil, fmt.Errorf("could not read the environment back: %w", err)
	}
	env := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if p.Name != "" {
			env[p.Name] = p.Value
		}
	}
	return env, nil
}
