// Package testguard keeps a test from reaching a live atrium. Called from a
// package's TestMain, before any test runs.
//
// A test process inherits the shell it was started from, and on a machine
// running atrium that shell is usually inside a room: ATRIUM_LOCATION,
// ATRIUM_TASK_ID, ATRIUM_AGENT_NAME and the rest point at the live daemon. A
// test that spawned anything, or asked where atrium is, found the live room and
// wrote to it. On 2026-09-30 that put a test's cards on the live board.
// Per-test care failed twice. This does it once, for the whole process.
package testguard

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Scrub removes every ATRIUM_* variable from this process's environment and
// points ATRIUM_LOCATION at a file that does not exist, so nothing asks the
// machine's shared address file where atrium is. A test that needs a variable
// sets it itself with t.Setenv. It returns the directory it made, for the
// caller to remove when the tests are done.
func Scrub() string {
	// A CHILD OF A GUARDED TEST inherits an environment already scrubbed, plus the
	// mode variables its parent set on purpose (a helper process reading
	// ATRIUM_TEST_ENV_DUMP, say). Scrubbing again would take those.
	if os.Getenv(marker) == "1" {
		return ""
	}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && strings.HasPrefix(strings.ToUpper(kv[:i]), "ATRIUM_") {
			_ = os.Unsetenv(kv[:i])
		}
	}
	dir, err := os.MkdirTemp("", "atrium-testguard-")
	if err != nil {
		return ""
	}
	_ = os.Setenv("ATRIUM_LOCATION", filepath.Join(dir, "no-daemon.json"))
	Git(dir)
	_ = os.Setenv(marker, "1")
	return dir
}

// marker says this process's environment was scrubbed already.
const marker = "ATRIUM_TESTGUARD"

// Home gives this process a home directory with nothing in it, under dir, so a
// runner a test starts finds no agent settings and no hooks there, and nothing
// a test writes to "the user's" files lands in the real ones.
func Home(dir string) {
	if dir == "" {
		return
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return
	}
	// The git a test runs keeps the isolated config in the new home too.
	if os.Getenv(gitMarker) == "1" {
		_ = os.Setenv("GIT_CONFIG_GLOBAL", writeGitConfig(home))
		_ = os.Setenv("XDG_CONFIG_HOME", home)
	}
	for _, k := range []string{"HOME", "USERPROFILE"} {
		_ = os.Setenv(k, home)
	}
	// On Windows atrium's own files, daemon.json among them, live under these and
	// not under the home directory.
	for k, sub := range map[string]string{"APPDATA": "AppData/Roaming", "LOCALAPPDATA": "AppData/Local"} {
		p := filepath.Join(home, filepath.FromSlash(sub))
		if err := os.MkdirAll(p, 0o755); err == nil {
			_ = os.Setenv(k, p)
		}
	}
}

// Git makes every git a test runs ignore the operator's git setup: their global and system config, and with it a
// core.hooksPath whose hooks read /dev/tty or refuse unsigned commits, a gpgsign they have turned on, and an
// identity they have or have not set. Called from a package's TestMain, before any test runs. Scrub calls it, so a
// package that scrubs has it. It works under under, or in a directory of its own when that is empty, and returns
// the directory it made, for the caller to remove.
//
// The config is reached two ways. GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM cover a git started with os.Environ().
// gitsync.CleanEnv drops every GIT_* variable, so a git started through it finds the same config as the HOME,
// USERPROFILE and XDG_CONFIG_HOME this points at a home holding it as .gitconfig.
func Git(under string) string {
	// A CHILD OF A GUARDED TEST inherits all of this already.
	if os.Getenv(gitMarker) == "1" {
		return ""
	}
	dir := under
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "atrium-testgit-"); err != nil {
			return ""
		}
	}
	home := filepath.Join(dir, "gitconfig-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return ""
	}
	cfg := writeGitConfig(home)
	_ = os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
		_ = os.Setenv(k, home)
	}
	_ = os.Setenv(gitMarker, "1")
	return dir
}

// gitMarker says this process's git setup was isolated already.
const gitMarker = "TESTGUARD_GIT"

// writeGitConfig writes the config a test's git reads, as .gitconfig in home, and answers its path. The hooks
// directory beside it is empty on purpose. Auto maintenance is off because receive-pack runs it after the client has
// its answer and git unsets GIT_CONFIG_PARAMETERS for it, so only a config file reaches it.
func writeGitConfig(home string) string {
	hooks := filepath.Join(home, "no-hooks")
	_ = os.MkdirAll(hooks, 0o755)
	cfg := filepath.Join(home, ".gitconfig")
	body := "[user]\n\tname = Atrium Test\n\temail = test@atrium.invalid\n" +
		"[core]\n\thooksPath = " + filepath.ToSlash(hooks) + "\n" +
		"[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n" +
		"[gc]\n\tauto = 0\n[maintenance]\n\tauto = false\n"
	_ = os.WriteFile(cfg, []byte(body), 0o644)
	return cfg
}

// Agents are the runners a test must never start for real: each one bills a
// model and runs the machine's own hooks.
var Agents = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "ollama": true, "cursor-agent": true, "opencode": true,
	"aider": true, "node": true,
}

// IsAgent reports whether a command is one of Agents, by its base name. A
// backslash is a separator on every OS, so a Windows path is caught on Linux too.
func IsAgent(exe string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	return Agents[base]
}
