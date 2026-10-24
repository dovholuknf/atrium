package claudeconf

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Which shell Claude Code runs a hook command in.
//
// On Windows Claude Code hands a hook command to Git Bash. Without one it
// blocks every tool call, and the way out is `"shell": "powershell"` on the
// hook entry. So the installer has to know whether Git Bash is there, and look
// where Claude Code looks, in this order:
//
//  1. CLAUDE_CODE_GIT_BASH_PATH, when it names a file.
//  2. git.exe on PATH: its folder's sibling, ..\bin\bash.exe.
//  3. C:\Program Files\Git\bin\bash.exe.
//
// A bash.exe that merely sits on PATH does not count. C:\Windows\System32 has
// WSL's launcher, which is not what Claude Code uses, and a PATH hit would
// have the installer leave the shell key off on a machine that needs it.
//
// Where bash is found the entries are written as they always were, so a room
// that works today is not touched.

// gitBashEnv is the override Claude Code reads for the bash.exe it should use.
const gitBashEnv = "CLAUDE_CODE_GIT_BASH_PATH"

// powershellShell is the value of the `shell` key.
const powershellShell = "powershell"

// GitBashFound reports whether Claude Code would find a bash.exe on this
// machine. It is the real probe, and Target.NeedsPowerShell is the seam tests
// use to stand in for it.
func GitBashFound() bool {
	return findGitBash(os.Getenv, exec.LookPath, fileExists) != ""
}

// findGitBash is GitBashFound with its three inputs passed in. It returns the
// bash.exe it found, or empty.
func findGitBash(getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) string {
	if p := strings.TrimSpace(getenv(gitBashEnv)); p != "" && exists(p) {
		return p
	}
	if git, err := lookPath("git.exe"); err == nil && git != "" {
		p := filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe")
		if exists(p) {
			return filepath.Clean(p)
		}
	}
	for _, root := range []string{getenv("ProgramFiles"), `C:\Program Files`} {
		if root == "" {
			continue
		}
		if p := filepath.Join(root, "Git", "bin", "bash.exe"); exists(p) {
			return p
		}
	}
	return ""
}

// GitBash is the bash.exe a room runs a script with: where Claude Code looks, then a Git atrium installed itself.
// Empty when there is none, and never a bash.exe that is only on PATH.
func GitBash() string {
	if p := findGitBash(os.Getenv, exec.LookPath, fileExists); p != "" {
		return p
	}
	var tools []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		tools = append(tools, filepath.Join(home, ".local", "share", "atrium-tools", "git", "bin", "bash.exe"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		tools = append(tools, filepath.Join(la, "atrium-tools", "git", "bin", "bash.exe"))
	}
	for _, p := range tools {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// needsPowerShell says whether hook entries for this target carry
// `"shell": "powershell"`. Only claude, only on Windows, only without bash.
func needsPowerShell(t Target) bool {
	if t.ID != Claude.ID {
		return false
	}
	if t.NeedsPowerShell != nil {
		return t.NeedsPowerShell()
	}
	return runtime.GOOS == "windows" && !GitBashFound()
}
