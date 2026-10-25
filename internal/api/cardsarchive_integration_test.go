//go:build integration

package api

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

func cardNamed(t *testing.T, st *store.Store, name, dir string) *store.Task {
	t.Helper()
	task, _, err := st.Register(store.Observed{WireName: name, Worktree: filepath.ToSlash(dir), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func archiveCall(t *testing.T, oh *openHarness, body map[string]any) (int, archiveResult) {
	t.Helper()
	rec := post(t, oh.h, "/v1/cards/archive", body)
	var out archiveResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestArchiveByCwdClosesEveryCardInThatWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server and a database")
	}
	t.Parallel()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{}})
	dir := filepath.ToSlash(t.TempDir())
	a := cardNamed(t, oh.srv.st, "arch-a", dir)
	b := cardNamed(t, oh.srv.st, "arch-b", dir)
	other := cardNamed(t, oh.srv.st, "arch-other", filepath.ToSlash(t.TempDir()))

	code, out := archiveCall(t, oh, map[string]any{"cwd": dir, "wait": true})
	if code != 200 || len(out.Cards) != 2 || len(out.Closed) != 2 || len(out.Failed) != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	for _, id := range []string{a.ID, b.ID} {
		got, err := oh.srv.st.Get(id)
		if err != nil || got.Status != store.StatusDone || !slices.Contains(got.Tags, ClosedTag) {
			t.Fatalf("%s: %+v %v", id, got, err)
		}
		if !slices.Contains(oh.killed, id) {
			t.Fatalf("%s was not stopped: %v", id, oh.killed)
		}
	}
	if got, _ := oh.srv.st.Get(other.ID); slices.Contains(got.Tags, ClosedTag) {
		t.Fatal("a card in another folder was closed")
	}
	// a second call finds nothing left to close
	if code, out := archiveCall(t, oh, map[string]any{"cwd": dir, "wait": true}); code != 200 || len(out.Cards) != 0 {
		t.Fatalf("again: %d %+v", code, out)
	}
}

func TestArchiveByCwdMatchesSlashesCaseAndTrailingSeparator(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server and a database")
	}
	t.Parallel()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{}})
	dir := filepath.ToSlash(t.TempDir())
	cardNamed(t, oh.srv.st, "arch-slash", dir)
	ask := dir + "/"
	if runtime.GOOS == "windows" {
		ask = strings.ToUpper(strings.ReplaceAll(dir, "/", `\`)) + `\`
	}
	if code, out := archiveCall(t, oh, map[string]any{"cwd": ask, "wait": true}); code != 200 || len(out.Closed) != 1 {
		t.Fatalf("%q: %d %+v", ask, code, out)
	}
}

func TestArchiveByCwdWithNoCardIsEmptyNotAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server and a database")
	}
	t.Parallel()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{}})
	code, out := archiveCall(t, oh, map[string]any{"cwd": filepath.ToSlash(t.TempDir()), "wait": true})
	if code != 200 || len(out.Cards) != 0 || len(out.Closed) != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if rec := post(t, oh.h, "/v1/cards/archive", map[string]any{"wait": true}); rec.Code != 400 {
		t.Fatalf("no cwd: %d", rec.Code)
	}
	if rec := post(t, oh.h, "/v1/cards/archive", map[string]any{"cwd": "/x", "work": "burn"}); rec.Code != 400 {
		t.Fatalf("bad work: %d", rec.Code)
	}
}

func TestArchiveByCwdWithoutWaitAnswersAtOnceAndClosesBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server and a database")
	}
	t.Parallel()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{}})
	dir := filepath.ToSlash(t.TempDir())
	c := cardNamed(t, oh.srv.st, "arch-nowait", dir)
	code, out := archiveCall(t, oh, map[string]any{"cwd": dir})
	if code != 202 || len(out.Cards) != 1 || out.Cards[0] != c.ID {
		t.Fatalf("%d %+v", code, out)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := oh.srv.st.Get(c.ID); got != nil && slices.Contains(got.Tags, ClosedTag) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the card was never closed")
}
