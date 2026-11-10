package gitsync

import (
	"bytes"
	"os/exec"
)

// rawGit is git as a test sets a repository up, outside the typed ops, with the runner's environment and hardening.
func rawGit(dir string, env []string, stdin []byte, args ...string) (string, error) {
	var full []string
	for _, kv := range hardening {
		full = append(full, "-c", kv)
	}
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = CleanEnv(env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return out.String(), nil
}
