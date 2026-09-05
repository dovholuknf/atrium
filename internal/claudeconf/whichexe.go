package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WHICH BINARY GOES INTO settings.json.
//
// DO NOT ANSWER THIS WITH `os.Executable()`. That is right in exactly one
// place, inside the daemon, where it is the daemon. Everywhere else it is the
// binary somebody TYPED, so `atrium hook install` run from a fresh build writes
// the build directory's path, the daemon is running from somewhere else, and
// every hook reads as pointing elsewhere. Rebuilding puts the drift back.
//
// The identity is the running daemon's binary, which the daemon records in the
// location file every hook already reads to find the port.
//
// Resolution, in order:
//
//  1. `ATRIUM_HOOK_EXE`.
//  2. The running daemon's own path, from the location file.
//  3. The caller's own path, which is correct when there is no daemon to ask.
//
// Never an error. No daemon running is the ordinary case for somebody setting
// this up, and `whereami.go` is explicit that staleness is not guarded here.

// HookExeEnv names the override.
const HookExeEnv = "ATRIUM_HOOK_EXE"

// locationReader is how this finds the running daemon, injected so the package
// does not import `internal/daemon` (which imports this one) and so a test can
// point it somewhere harmless.
//
// A function variable rather than an interface: there is one implementation and
// one caller, and an interface would be three names for one line.
var locationReader = defaultLocationPath

// HookExe is the path to write into a settings file, and to compare a
// registered command against.
//
// `fallback` is what to use when nothing better is known. Callers inside the
// daemon pass their own `os.Executable()`; callers outside pass theirs. Empty
// means work it out.
func HookExe(fallback string) string {
	if v := strings.TrimSpace(os.Getenv(HookExeEnv)); v != "" {
		return normalizeExe(v)
	}
	if exe := runningDaemonExe(); exe != "" {
		return exe
	}
	if strings.TrimSpace(fallback) != "" {
		return normalizeExe(fallback)
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return normalizeExe(self)
}

// runningDaemonExe reads the location file and returns the daemon's binary.
//
// Empty for every failure, which covers the two ordinary cases: no daemon has
// run on this machine, and a daemon older than this field wrote the file.
func runningDaemonExe() string {
	path, err := locationReader()
	if err != nil || path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var loc struct {
		Exe string `json:"exe"`
	}
	if err := json.Unmarshal(raw, &loc); err != nil {
		return ""
	}
	if strings.TrimSpace(loc.Exe) == "" {
		return ""
	}
	// A recorded path that is no longer there is worse than useless: it would
	// write a hook naming a binary that cannot run, which fails silently by
	// design. Falling through to the caller's own path is the better wrong
	// answer.
	if _, err := os.Stat(filepath.FromSlash(loc.Exe)); err != nil {
		return ""
	}
	return normalizeExe(loc.Exe)
}

// normalizeExe resolves symlinks and uses forward slashes.
//
// Symlinks because a shimmed atrium should write the real target and keep
// working when the shim moves. Forward slashes because settings.json is JSON
// and a Windows path in it is a string full of escapes otherwise.
func normalizeExe(p string) string {
	p = filepath.FromSlash(strings.TrimSpace(p))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.ToSlash(p)
}

// defaultLocationPath is `daemon.LocationPath` without the import cycle.
//
// DUPLICATED ON PURPOSE, and the duplication is one expression rather than a
// rule. `internal/daemon` imports this package for the hook definitions, so
// importing it back is a cycle. The alternative is a third package holding one
// path, which is more machinery than the thing it holds.
//
// If this ever disagrees with `whereami.go`, the symptom is a hook naming a
// binary that is not the daemon's, which is the exact defect this file exists
// to fix. `whichexe_test.go` asserts they agree.
// LocationPathForTest exposes the duplicated path so the daemon's own test can
// assert the two agree. Exported for that one purpose and named so.
func LocationPathForTest() (string, error) { return defaultLocationPath() }

func defaultLocationPath() (string, error) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
			return filepath.Join(d, "atrium", "daemon.json"), nil
		}
		if d := os.Getenv("XDG_STATE_HOME"); d != "" {
			return filepath.Join(d, "atrium", "daemon.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "atrium", "daemon.json"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "atrium", "daemon.json"), nil
}
