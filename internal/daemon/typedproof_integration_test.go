//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func queuedLine(text string) string {
	return `{"type":"attachment","timestamp":"2026-10-16T01:02:03.000Z","attachment":{"type":"queued_command",` +
		`"commandMode":"prompt","prompt":"` + text + `"}}` + "\n"
}

// A typed say stays typed until the transcript shows the box drained it, and then it is delivered. A say the
// transcript never shows is noted once it is past typed_unread_after, and stays typed.
func TestATypedSayIsDeliveredOnlyWhenTheTranscriptShowsItRead(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target := cardFor(t, d, "listener")
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.ctx.transcript = func(string, string) string { return path }
	rec := func(text string) string {
		id, err := d.st.RecordSay(store.Say{FromWire: "sg4/boss", ToTask: target.ID, State: store.SayTyped}, text)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	read, unread := rec("READWORDS"), rec("LOSTWORDS")

	if n := d.proveTypedSays(target.ID, time.Now()); n != 0 {
		t.Fatalf("proved %d with nothing in the transcript", n)
	}
	if err := os.WriteFile(path, []byte(queuedLine("your launcher @boss instructs you: READWORDS")), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := d.proveTypedSays(target.ID, time.Now().Add(10*time.Minute)); n != 1 {
		t.Fatalf("proved %d, want 1", n)
	}
	r, _ := d.st.SayByID(read)
	u, _ := d.st.SayByID(unread)
	if r.State != store.SayDelivered || u.State != store.SayTyped {
		t.Fatalf("states after proof: %q %q", r.State, u.State)
	}
	if u.Note == "" {
		t.Fatalf("an unread say past typed_unread_after has no note")
	}
}
