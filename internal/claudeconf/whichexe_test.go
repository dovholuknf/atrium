package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

// Which binary goes into settings.json.
//
// Each test is named for the way the answer gets to be wrong, because every one
// of these produced a board reporting six working hooks as `points elsewhere`.

// withLocation points the resolver at a location file written for the test, so
// nothing here reads or writes the machine's real one.
func withLocation(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prev := locationReader
	locationReader = func() (string, error) { return path, nil }
	t.Cleanup(func() { locationReader = prev })
}

// A binary that exists, for a location file to name.
func anExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "atrium-installed")
	if err := os.WriteFile(p, []byte("not really a binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(real)
}

// THE DEFECT. Installing hooks from a freshly built binary must write the path
// of the daemon that is RUNNING, not the path of the binary you typed.
func TestHookExePrefersTheRunningDaemon(t *testing.T) {
	installed := anExe(t)
	withLocation(t, `{"pid":1,"exe":"`+installed+`"}`)

	got := HookExe("D:/git/atrium/build.claude/atrium.exe")
	if got != installed {
		t.Fatalf("resolved %q, want the running daemon at %q", got, installed)
	}
}

// No daemon has ever run here, which is the ordinary case for somebody setting
// this up before starting anything.
func TestHookExeFallsBackWhenNoDaemonHasRun(t *testing.T) {
	withLocation(t, "")
	fallback := anExe(t)
	if got := HookExe(fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// A daemon older than the `exe` field wrote the file. It still has to answer.
func TestHookExeFallsBackWhenTheFileHasNoExe(t *testing.T) {
	withLocation(t, `{"pid":1,"agent":"http://localhost:7777"}`)
	fallback := anExe(t)
	if got := HookExe(fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// A recorded binary that has been deleted since. Writing a hook naming it would
// fail silently in every session, which is worse than the fallback being wrong.
func TestHookExeIgnoresARecordedBinaryThatIsGone(t *testing.T) {
	withLocation(t, `{"pid":1,"exe":"D:/gone/atrium.exe"}`)
	fallback := anExe(t)
	if got := HookExe(fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q when the recorded one is gone", got, fallback)
	}
}

// Unparseable is the same as absent. A truncated write must not stop hooks
// being installable.
func TestHookExeIgnoresAnUnreadableLocationFile(t *testing.T) {
	withLocation(t, `{"pid":1,`)
	fallback := anExe(t)
	if got := HookExe(fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// The override wins over everything, including a daemon that is running.
func TestHookExeHonoursTheOverride(t *testing.T) {
	withLocation(t, `{"pid":1,"exe":"`+anExe(t)+`"}`)
	mine := anExe(t)
	t.Setenv(HookExeEnv, mine)
	if got := HookExe("whatever"); got != mine {
		t.Fatalf("resolved %q, want the override %q", got, mine)
	}
}

// Forward slashes, always. settings.json is JSON, and a Windows path written
// with backslashes is a string full of escapes that the next reader gets wrong.
func TestHookExeUsesForwardSlashes(t *testing.T) {
	withLocation(t, "")
	got := HookExe(filepath.FromSlash(anExe(t)))
	if filepath.ToSlash(got) != got {
		t.Fatalf("resolved %q, want forward slashes", got)
	}
}

// The location path is duplicated from `internal/daemon/whereami.go` to avoid
// an import cycle. If the two ever disagree, hooks name a binary that is not
// the daemon's, which is the exact defect this file exists to prevent. The
// daemon side asserts the other half in `whereami_test.go`.
func TestDefaultLocationPathIsNotEmpty(t *testing.T) {
	p, err := defaultLocationPath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" || filepath.Base(p) != "daemon.json" {
		t.Fatalf("location path is %q, want one ending in daemon.json", p)
	}
}
