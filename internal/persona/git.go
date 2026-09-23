package persona

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// git runs one READ-ONLY git command in dir and returns its trimmed output.
//
// Every caller in this package names a command that reads: log, diff, show,
// rev-parse, ls-files, merge-base, symbolic-ref, remote get-url. Nothing here
// adds, commits, checks out or pushes, and the design says atrium never will.
//
// GIT_OPTIONAL_LOCKS=0 stops even the reads from refreshing the index behind
// the operator's back, which is the one write `git status` and `git diff` make
// on their own.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

// lines splits git output into non-empty lines.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
