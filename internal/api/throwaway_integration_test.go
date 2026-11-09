//go:build integration

package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

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

// Move immediately when no session is running.
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

// Record the destination while the runner is using the directory; awaitExit moves it.
func TestPromotingALiveThrowawayWaitsForTheSession(t *testing.T) {
	s, st, dir := fileServer(t)
	task := throwawayCard(t, st, dir)
	s.hk.isSupervised = func(string) bool { return true }
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

// Reject an existing destination before moving anything.
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

// Reject relative destinations to avoid resolving against the daemon's directory.
func TestPromotingSomewhereRelativeIsRefused(t *testing.T) {
	if _, err := promoteTarget("kept"); err == nil {
		t.Fatal("a relative destination was accepted")
	}
}

func TestPromotingToANetworkPathIsRefused(t *testing.T) {
	if _, err := promoteTarget(`\\evil\s\kept`); err == nil {
		t.Fatal("a network destination was accepted")
	}
}

// Exercise the copy fallback used when rename cannot cross volumes.
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

func TestKeepingAThrowawayWithAHomeLeavesItWhereItIs(t *testing.T) {
	for _, live := range []bool{false, true} {
		s, st, dir := fileServer(t)
		task := throwawayCard(t, st, dir)
		s.hk.isSupervised = func(string) bool { return live }
		home := filepath.ToSlash(filepath.Dir(dir))
		if _, err := st.CardHomeNew(store.CardHome{Path: home, Kind: store.CardHomeThrowaway}); err != nil {
			t.Fatal(err)
		}
		if err := st.CardHomeCard(home, task.ID); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/promote", strings.NewReader(`{}`))
		r.SetPathValue("id", task.ID)
		w := httptest.NewRecorder()
		s.promoteCard(w, r)
		if w.Code != 200 {
			t.Fatalf("live=%v: promote answered %d: %s", live, w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
			t.Fatalf("live=%v: the work moved: %v", live, err)
		}
		got, _ := st.Get(task.ID)
		row, _, _ := st.CardHomeByPath(home)
		if live && (!got.Throwaway || got.PromoteTo == "") {
			t.Fatalf("a live throwaway was not marked: %+v", got)
		}
		if !live && (got.Throwaway || row.State != store.CardHomeKept) {
			t.Fatalf("an idle throwaway was not kept: throwaway=%v home %+v", got.Throwaway, row)
		}
	}
}
