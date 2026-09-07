package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Which runner a session hook says it is.
//
// It used to say `claude` as a literal, whoever ran it, so every codex card
// came up on the board wearing claude's colour and offering `claude --resume`
// for an id claude has never issued.

// postedRunner runs the session hook against a server that records the body,
// and answers with the runner it claimed to be.
func postedRunner(t *testing.T, flag string) string {
	t.Helper()
	var got struct {
		Runner string `json:"runner"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	defer withStdin(t, `{"cwd":"/tmp/some-worktree","session_id":"abc"}`)()
	reportSession(srv.URL, "start", "a-card", flag)
	return got.Runner
}

// The flag the hooks file carries, for a session atrium did not start.
func TestSessionReportsTheRunnerItsHookNames(t *testing.T) {
	t.Setenv("ATRIUM_RUNNER", "")
	if got := postedRunner(t, "codex"); got != "codex" {
		t.Fatalf("a codex hook reported %q", got)
	}
}

// Nothing said means claude, which is what every already-installed
// settings.json says and what every caller meant before there was a second
// runner.
func TestSessionWithNothingSaidIsStillClaude(t *testing.T) {
	t.Setenv("ATRIUM_RUNNER", "")
	if got := postedRunner(t, ""); got != "claude" {
		t.Fatalf("a hook that named no runner reported %q", got)
	}
}

// The environment wins, because it names the harness ROW rather than the
// runner. Two rows can both run codex against different models and both read
// the same hooks.json, so the file's answer is the right default and never the
// better one.
func TestLaunchedSessionPrefersTheRowItWasStartedAs(t *testing.T) {
	t.Setenv("ATRIUM_RUNNER", "codex-fast")
	if got := postedRunner(t, "codex"); got != "codex-fast" {
		t.Fatalf("the launched row was reported as %q, losing to the hooks file", got)
	}
}
