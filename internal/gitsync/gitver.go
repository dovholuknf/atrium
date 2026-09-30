package gitsync

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// MinGit is the oldest git that can take server config from the environment
// (GIT_CONFIG_COUNT), which is how neither side ever writes policy into a repository.
const MinGit = "2.31"

var gitVersionRe = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// CheckGit says whether the git on PATH can be used, and answers its version line. The
// refusal is a sentence a person can act on.
//
// On Windows a Cygwin or MSYS git is refused with the sentence scripts/room-git.ps1 -Check
// prints: it writes /cygdrive gitfiles that Git for Windows cannot resolve (f-005).
func (r *Runner) CheckGit(ctx context.Context) (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git is not installed on this machine, install it, then rerun")
	}
	out, err := r.Git(ctx, "", "--version")
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(out)
	if runtime.GOOS == "windows" {
		lp := strings.ToLower(path)
		if !strings.Contains(line, ".windows.") || strings.Contains(lp, "cygwin") || strings.Contains(lp, "msys") {
			return line, fmt.Errorf("%s at %s is not Git for Windows, which counts as missing. install it there, then rerun", line, path)
		}
	}
	m := gitVersionRe.FindStringSubmatch(line)
	if m == nil {
		return line, fmt.Errorf("could not read a version from %q", line)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	if maj < 2 || (maj == 2 && min < 31) {
		return line, fmt.Errorf("%s is older than %s, which git sync needs. update git, then rerun", line, MinGit)
	}
	return line, nil
}
