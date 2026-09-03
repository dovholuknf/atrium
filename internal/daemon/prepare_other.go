//go:build !windows

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// See prepare_windows.go for what this is for. The mechanism is the same and
// only the shell differs.

const prepareTimeout = 20 * time.Second

// captureEnv runs prepare in a shell and returns the environment afterwards.
//
// An interactive login shell, because the function being run is usually
// defined in a profile that a non-interactive shell never reads.
func captureEnv(prepare, cwd string) (map[string]string, error) {
	if strings.TrimSpace(prepare) == "" {
		return nil, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
	defer cancel()

	// `set -e` so a prepare command that fails stops here rather than letting
	// the env dump succeed and the runner start without what it needed.
	//
	// `env -0` so a value containing a newline does not read as two variables.
	cmd := exec.CommandContext(ctx, shell, "-lc", "set -e\n"+prepare+"\nenv -0")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("the prepare command did not finish in %s", prepareTimeout)
		}
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("the prepare command failed: %s",
				firstLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("the prepare command failed: %w", err)
	}

	env := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			env[k] = v
		}
	}
	return env, nil
}
