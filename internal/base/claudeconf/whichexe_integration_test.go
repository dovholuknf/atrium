//go:build integration

package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

// withLocation points the resolver at a location file written for the test, so
// nothing here reads or writes the machine's real one.
func withLocation(t *testing.T, body string) func() (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func() (string, error) { return path, nil }
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
	locate := withLocation(t, `{"pid":1,"exe":"`+installed+`"}`)

	got := hookExeWith(locate, "D:/git/atrium/build.claude/atrium.exe")
	if got != installed {
		t.Fatalf("resolved %q, want the running daemon at %q", got, installed)
	}
}

// No daemon has ever run here, which is the ordinary case for somebody setting
// this up before starting anything.
func TestHookExeFallsBackWhenNoDaemonHasRun(t *testing.T) {
	locate := withLocation(t, "")
	fallback := anExe(t)
	if got := hookExeWith(locate, fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// A daemon older than the `exe` field wrote the file. It still has to answer.
func TestHookExeFallsBackWhenTheFileHasNoExe(t *testing.T) {
	locate := withLocation(t, `{"pid":1,"agent":"http://localhost:7777"}`)
	fallback := anExe(t)
	if got := hookExeWith(locate, fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// A recorded binary that has been deleted since. Writing a hook naming it would
// fail silently in every session, which is worse than the fallback being wrong.
func TestHookExeIgnoresARecordedBinaryThatIsGone(t *testing.T) {
	locate := withLocation(t, `{"pid":1,"exe":"D:/gone/atrium.exe"}`)
	fallback := anExe(t)
	if got := hookExeWith(locate, fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q when the recorded one is gone", got, fallback)
	}
}

// Unparseable is the same as absent. A truncated write must not stop hooks
// being installable.
func TestHookExeIgnoresAnUnreadableLocationFile(t *testing.T) {
	locate := withLocation(t, `{"pid":1,`)
	fallback := anExe(t)
	if got := hookExeWith(locate, fallback); got != fallback {
		t.Fatalf("resolved %q, want the caller's own binary %q", got, fallback)
	}
}

// The override wins over everything, including a daemon that is running.
func TestHookExeHonoursTheOverride(t *testing.T) {
	locate := withLocation(t, `{"pid":1,"exe":"`+anExe(t)+`"}`)
	mine := anExe(t)
	t.Setenv(HookExeEnv, mine)
	if got := hookExeWith(locate, "whatever"); got != mine {
		t.Fatalf("resolved %q, want the override %q", got, mine)
	}
}

// Forward slashes, always. settings.json is JSON, and a Windows path written
// with backslashes is a string full of escapes that the next reader gets wrong.
func TestHookExeUsesForwardSlashes(t *testing.T) {
	locate := withLocation(t, "")
	got := hookExeWith(locate, filepath.FromSlash(anExe(t)))
	if filepath.ToSlash(got) != got {
		t.Fatalf("resolved %q, want forward slashes", got)
	}
}
