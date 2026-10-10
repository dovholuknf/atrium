package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// TestMain doubles as the notify command: the test binary run with
// ATRIUM_NOTIFY_HELPER set is a program that records what it was given, which
// is portable where a shell script is not.
func TestMain(m *testing.M) {
	if mode := os.Getenv("ATRIUM_NOTIFY_HELPER"); mode != "" {
		os.Exit(notifyHelper(mode))
	}
	// No ATRIUM_* pointer from the shell this ran in reaches a test. After the
	// helper check, which is this binary run as a child with its own variables.
	dir := testguard.Scrub()
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

func notifyHelper(mode string) int {
	switch mode {
	case "record":
		stdin, _ := io.ReadAll(os.Stdin)
		rec := map[string]any{
			"name": os.Getenv("ATRIUM_NOTIFY_NAME"), "reason": os.Getenv("ATRIUM_NOTIFY_REASON"),
			"card": os.Getenv("ATRIUM_NOTIFY_CARD"), "room": os.Getenv("ATRIUM_NOTIFY_ROOM"),
			"stdin": string(stdin), "args": os.Args[1:],
		}
		line, _ := json.Marshal(rec)
		f, err := os.OpenFile(os.Getenv("ATRIUM_NOTIFY_OUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return 9
		}
		defer f.Close()
		fmt.Fprintln(f, string(line))
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "the phone is unreachable")
		fmt.Fprintln(os.Stderr, "second line")
		return 3
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 4096)
		for i := 0; i < 2048; i++ {
			if _, err := os.Stdout.Write(chunk); err != nil {
				return 0
			}
		}
		return 0
	}
	return 1
}

// ── identity ────────────────────────────────────────────

func TestNotifyNameIsTheAliasElseTheTitle(t *testing.T) {
	a, _ := NotifyIdentity("id1", json.RawMessage(`{"status":"needs-input","title":"T","alias":"sa89","waiting_since":"x"}`))
	b, _ := NotifyIdentity("id1", json.RawMessage(`{"status":"needs-input","title":"T","waiting_since":"x"}`))
	if a.Name != "sa89" || b.Name != "T" {
		t.Fatalf("names %q and %q", a.Name, b.Name)
	}
}

// ── the trigger ─────────────────────────────────────────

// ── presence ────────────────────────────────────────────

// ── the sink ────────────────────────────────────────────

// ── failure ─────────────────────────────────────────────

// ── never delaying anything ─────────────────────────────

// ── the endpoints ───────────────────────────────────────

// ── /v1/tasks/<id>/replies passes through to the owning room ─
