package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The per-card handoff file. See HandoffName and item 91.

// The name is the alias, else 13 characters of the id, and two ids sharing eight
// characters do not collide.
func TestHandoffName(t *testing.T) {
	withAlias := &store.Task{ID: "01a0ede4-945c-aaaa", Alias: "Merge"}
	if got := HandoffName(withAlias); got != "HANDOFF.merge.md" {
		t.Fatalf("alias name: %q", got)
	}
	a := &store.Task{ID: "01a0ede4-945c-aaaa"}
	b := &store.Task{ID: "01a0ede4-9999-bbbb"}
	if got := HandoffName(a); got != "HANDOFF.01a0ede4-945c.md" {
		t.Fatalf("id name: %q", got)
	}
	if HandoffName(a) == HandoffName(b) {
		t.Fatalf("two cards sharing an 8 character prefix share a file: %q", HandoffName(a))
	}
	if got := HandoffName(&store.Task{ID: "x", Alias: "a/b"}); strings.ContainsAny(got, `/\`) {
		t.Fatalf("unsafe name: %q", got)
	}
}

// ncSibling is a second card in the same directory, with its own terminal.
func ncSibling(t *testing.T, d *Daemon, dir, alias string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: "sibling", Worktree: filepath.ToSlash(dir), Runner: "claude", PID: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetAlias(task.ID, alias); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)
	typedRunner(t, d, task.ID)
	return task
}

// The handoff path is fixed when the cycle is claimed, and the prompt and the wake
// both use it, even if the room's handoff directory changes mid-cycle.
func TestNewContextUsesOnePathThroughTheCycle(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	first := filepath.Join(dir, task.ID+".md")
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := d.st.SetSetting(store.SettingContextHandoffDir, other); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), first) })
	if err := os.WriteFile(first, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ready(task); err != nil {
		t.Fatalf("ready refused the path the prompt named: %v", err)
	}
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.wake.sawSession(task.ID, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), newContextWake(first)) })
	if strings.Contains(f.written(), other) {
		t.Fatalf("the path changed mid-cycle: %q", f.written())
	}
}

// Two cards in one directory: neither another card's file nor a plain HANDOFF.md
// satisfies the check.
func TestHandoffWrittenIsPerCard(t *testing.T) {
	d := testDaemon(t)
	a, _, dir := ncCard(t, d)
	b := ncSibling(t, d, dir, "other")
	since := time.Now()

	if err := os.WriteFile(filepath.Join(dir, HandoffName(b)), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.handoffWritten(a.ID, HandoffName(a), since, "tok"); err == nil {
		t.Fatal("card B's handoff satisfied card A")
	}
	if err := os.WriteFile(filepath.Join(dir, "HANDOFF.md"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.handoffWritten(a.ID, HandoffName(a), since, "tok"); err == nil {
		t.Fatal("a plain HANDOFF.md satisfied card A")
	}
	if err := os.WriteFile(filepath.Join(dir, HandoffName(a)), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.handoffWritten(a.ID, HandoffName(a), since, "tok"); err != nil {
		t.Fatalf("A's own file was refused: %v", err)
	}
}

// A cycle is refused while a card in the same directory is mid-cycle, allowed
// once it finishes, and allowed while it merely exists.
func TestNewContextRefusedWhileASiblingInTheDirectoryIsMidCycle(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	a, _, dir := ncCard(t, d)
	b := ncSibling(t, d, dir, "other")

	if err := d.StartNewContext(a.ID); err != nil {
		t.Fatalf("refused merely because a sibling exists: %v", err)
	}
	err := d.StartNewContext(b.ID)
	if !isSharedErr(err) || !strings.Contains(err.Error(), a.DisplayTitle()) {
		t.Fatalf("not refused naming the other card: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+b.ID+"/new-context", nil)
	req.SetPathValue("id", b.ID)
	rec := httptest.NewRecorder()
	d.handleNewContext(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("answered %d, not a conflict", rec.Code)
	}

	d.nctx.clear(a.ID)
	if err := d.StartNewContext(b.ID); err != nil {
		t.Fatalf("refused after the sibling finished: %v", err)
	}
}
