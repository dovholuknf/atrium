package launch

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// A claude runner on Windows hands every hook command to Git Bash. Without one
// each tool call blocks, the runner cannot even read its brief, and the card
// sits idle looking like work. A room started over ssh has none of the PATH an
// interactive one has, which is how that happened. So the launch looks first,
// by the same order Claude Code does (see internal/base/claudeconf/shell.go), and
// either leaves things alone, points the runner at a Git atrium installed, or
// refuses with the reason before anything is spawned.

const gitBashEnvName = "CLAUDE_CODE_GIT_BASH_PATH"

// CheckGitBash is gitBashFor with this machine's own lookups. It returns the
// KEY=VALUE to add to the runner's env, or empty.
func CheckGitBash(runnerID string, env []string) (string, error) {
	home, _ := os.UserHomeDir()
	return gitBashFor(runtime.GOOS, runnerID, env, home, func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	})
}

// gitBashFor is the decision, with every lookup passed in so it runs the same
// on any OS. git.exe is looked for on the PATH of the env the runner will get,
// which is not necessarily this process's.
func gitBashFor(goos, runnerID string, env []string, home string, exists func(string) bool) (string, error) {
	if goos != "windows" || runnerID != "claude" {
		return "", nil
	}
	get := func(k string) string {
		v := ""
		for _, kv := range env {
			if i := strings.Index(kv, "="); i > 0 && strings.EqualFold(kv[:i], k) {
				v = kv[i+1:]
			}
		}
		return strings.TrimSpace(v)
	}
	if p := get(gitBashEnvName); p != "" && exists(p) {
		return "", nil
	}
	for _, dir := range strings.Split(get("PATH"), ";") {
		dir = strings.TrimRight(strings.TrimSpace(dir), `\/`)
		if dir == "" || !exists(dir+`\git.exe`) {
			continue
		}
		if exists(winDir(dir) + `\bin\bash.exe`) {
			return "", nil
		}
		break
	}
	roots := []string{get("ProgramFiles"), `C:\Program Files`}
	for _, r := range roots {
		if r != "" && exists(r+`\Git\bin\bash.exe`) {
			return "", nil
		}
	}
	// A Git atrium put there itself: not on PATH, but known.
	var tools []string
	if home != "" {
		tools = append(tools, home+`\.local\share\atrium-tools\git\bin\bash.exe`)
	}
	if la := get("LOCALAPPDATA"); la != "" {
		tools = append(tools, la+`\atrium-tools\git\bin\bash.exe`)
	}
	for _, p := range tools {
		if exists(p) {
			return gitBashEnvName + "=" + p, nil
		}
	}
	return "", fmt.Errorf("claude needs Git Bash on Windows and this room cannot find one, so every tool call " +
		"would fail. Set " + gitBashEnvName + " to a bash.exe, or put a full Git (not MinGit) on the room's PATH, " +
		"then start the room again")
}

// winDir is the folder of a Windows path, whichever slash it uses.
func winDir(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return ""
}
