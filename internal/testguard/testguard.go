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

// Agents are the runners a test must never start for real: each one bills a
// model and runs the machine's own hooks.
var Agents = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "ollama": true, "cursor-agent": true, "opencode": true,
	"aider": true, "node": true,
}

// IsAgent reports whether a command is one of Agents, by its base name.
func IsAgent(exe string) bool {
	base := strings.ToLower(filepath.Base(exe))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	return Agents[base]
}
