//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/store"
)

// aThrowaway is a card in a temporary directory atrium made, with a file in it
// so that a directory which was not deleted can be told from one that never
// had anything in it.
func aThrowaway(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	dir, err := makeThrowawayDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.FromSlash(dir)) })
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(dir), "notes.md"),
		[]byte("an hour of work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{
		WireName: "tryout", Worktree: dir, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Cleanup removes both the temporary directory and the card.
func TestTheEndOfAThrowawayTakesItsDirectoryAndItsCard(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := aThrowaway(t, d)

	d.endThrowaway(task.ID)

	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s is still there", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived its directory")
	}
}

// Ordinary cards and their directories must survive the same exit handler.
func TestAnOrdinaryCardIsNotThrownAway(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "real-work", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("an ordinary directory was deleted: %v", err)
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Fatal("an ordinary card was deleted")
	}
}

// Refuse cleanup outside atrium's temporary directory, even if the card is
// marked throwaway. Keep the card so the error remains visible.
func TestAThrowawayPointingSomewhereRealIsRefused(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "mislabelled", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a directory atrium never made was deleted: %v", err)
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Fatal("the card went even though the directory did not")
	}
}

// Promotion moves the directory after the session exits.
func TestAPromotedThrowawayMovesInsteadOfGoing(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := aThrowaway(t, d)
	to := filepath.Join(t.TempDir(), "kept")
	if err := d.st.SetPromoteTo(task.ID, to); err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(filepath.Join(to, "notes.md")); err != nil {
		t.Fatalf("the work did not arrive at %s: %v", to, err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal("the card was deleted despite being promoted")
	}
	if got.Throwaway || got.PromoteTo != "" {
		t.Fatalf("the card is still temporary: throwaway=%v promote_to=%q",
			got.Throwaway, got.PromoteTo)
	}
	if got.Worktree != filepath.ToSlash(to) {
		t.Fatalf("the card still points at %s", got.Worktree)
	}
}

// Do not reopen throwaways on restart, since their directories are deleted (B2-06).
func TestAThrowawayIsNotReopened(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := aThrowaway(t, d)
	other, _, err := d.st.Register(store.Observed{
		WireName: "real-work", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.saveReopen([]*runner{
		{taskID: task.ID, buf: newRing(64, 80)},
		{taskID: other.ID, buf: newRing(64, 80)},
	})

	var ids []string
	for _, w := range d.reopenWanted() {
		ids = append(ids, w.ID)
	}
	if !slices.Equal(ids, []string{other.ID}) {
		t.Fatalf("reopening %v, wanted only the ordinary card", ids)
	}
}

// Startup cleanup handles leftovers from a daemon killed before awaitExit.
func TestAThrowawayLeftBySomethingThatDiedIsSweptAtStartup(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := aThrowaway(t, d)

	d.sweepThrowaways()

	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s survived the sweep", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card survived the sweep")
	}
}

func TestAStoppedThrowawayLeavesNoDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := aThrowaway(t, d)
	shell, args := someShell(t)
	sleep := "Start-Sleep -Seconds 60"
	if shell == "sh" {
		sleep = "sleep 60"
	}
	if _, err := d.spawnPTY(task.ID, shell, append(args, sleep), filepath.FromSlash(task.Worktree), os.Environ()); err != nil {
		t.Skipf("could not start a runner here: %v", err)
	}
	r := d.sup.get(task.ID)
	t.Cleanup(func() {
		r.closePTY()
		select {
		case <-r.done:
		case <-time.After(20 * time.Second):
		}
	})

	_ = r.cmd.Process.Kill()

	gone := func() bool {
		_, err := os.Stat(filepath.FromSlash(task.Worktree))
		return os.IsNotExist(err)
	}
	testwait.PollEvery(testwait.HTTP, 30*time.Second, gone)
	if !gone() {
		t.Fatalf("%s is still there after the stop", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived the stop")
	}
}

func TestDeletingAThrowawayCardDeletesItsDirectory(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := aThrowaway(t, d)

	rec := httptest.NewRecorder()
	d.ap.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+task.ID, nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("the delete answered %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s outlived its card", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card is still there")
	}
}
