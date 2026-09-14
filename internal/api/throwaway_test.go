package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Keeping a throwaway, which is the half of the feature that makes the other
// half safe to use.

// throwawayCard is a card in a directory with work in it, marked temporary.
func throwawayCard(t *testing.T, st *store.Store, dir string) *store.Task {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("an hour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := cardIn(t, st, dir)
	if err := st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}
	return task
}

// quote is enough JSON quoting for a Windows path in a test.
func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

// WITH NOTHING RUNNING THE MOVE IS IMMEDIATE. Making somebody start a session
// in order to move a directory would be absurd.
func TestPromotingAnIdleThrowawayMovesItNow(t *testing.T) {
	s, st, dir := fileServer(t)
	task := throwawayCard(t, st, dir)
	to := filepath.Join(t.TempDir(), "kept")

	r := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/promote",
		strings.NewReader(`{"to":`+quote(to)+`}`))
	r.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	s.promoteCard(w, r)

	if w.Code != 200 {
		t.Fatalf("promote answered %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(to, "notes.md")); err != nil {
		t.Fatalf("the work did not arrive: %v", err)
	}
	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Throwaway || got.Worktree != filepath.ToSlash(to) {
		t.Fatalf("the card was not promoted: throwaway=%v worktree=%s", got.Throwaway, got.Worktree)
	}
}

// WITH A SESSION RUNNING IT IS A PROMISE. Windows will not let a live
// process have its working directory renamed, so the destination is written
// down and `awaitExit` carries it out.
func TestPromotingALiveThrowawayWaitsForTheSession(t *testing.T) {
	s, st, dir := fileServer(t)
	task := throwawayCard(t, st, dir)
	IsSupervised = func(string) bool { return true }
	t.Cleanup(func() { IsSupervised = nil })
	to := filepath.Join(t.TempDir(), "kept")

	r := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/promote",
		strings.NewReader(`{"to":`+quote(to)+`}`))
	r.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	s.promoteCard(w, r)

	if w.Code != 200 {
		t.Fatalf("promote answered %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatal("the directory moved while a session was still in it")
	}
	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PromoteTo != to || !got.Throwaway {
		t.Fatalf("promote_to=%q throwaway=%v, wanted the move recorded and the card still temporary",
			got.PromoteTo, got.Throwaway)
	}
}

// A DESTINATION THAT ALREADY EXISTS IS REFUSED, before anything is moved. The
// alternative is merging an hour of work into somebody else's directory.
func TestPromotingOntoSomethingThatExistsIsRefused(t *testing.T) {
	s, st, dir := fileServer(t)
	task := throwawayCard(t, st, dir)
	to := t.TempDir()

	r := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/promote",
		strings.NewReader(`{"to":`+quote(to)+`}`))
	r.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	s.promoteCard(w, r)

	if w.Code != 400 {
		t.Fatalf("answered %d, wanted a refusal", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Fatalf("the work moved anyway: %v", err)
	}
}

// A RELATIVE PATH IS REFUSED, because the daemon's own working directory is
// not anywhere the operator was thinking of.
func TestPromotingSomewhereRelativeIsRefused(t *testing.T) {
	if _, err := promoteTarget("kept"); err == nil {
		t.Fatal("a relative destination was accepted")
	}
}

// THE CROSS-VOLUME CASE, which is the ordinary one: temporary directories live
// on whichever volume the operating system keeps them on, and work is promoted
// somewhere else. Rename cannot do it, so the copy is what has to work.
func TestATreeIsCopiedWhenItCannotBeRenamed(t *testing.T) {
	from := t.TempDir()
	if err := os.MkdirAll(filepath.Join(from, "src", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "src", "inner", "a.txt"),
		[]byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "landed")

	if err := copyTree(from, to); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(to, "src", "inner", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "kept\n" {
		t.Fatalf("copied %q", got)
	}
}
