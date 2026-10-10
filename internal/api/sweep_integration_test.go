//go:build integration

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

func sweepNow(t *testing.T, oh *openHarness) sweepReport {
	t.Helper()
	rec := post(t, oh.h, "/v1/sweep", nil)
	var rep sweepReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil || rec.Code != 200 {
		t.Fatalf("sweep %d %s", rec.Code, rec.Body.String())
	}
	return rep
}

func TestSweepLeavesALiveCardAloneAndOffersADoneOne(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if rep := sweepNow(t, oh); len(rep.Leftovers) != 0 || len(rep.Freed) != 0 {
		t.Fatalf("a live card's rows were swept: %+v", rep)
	}
	if err := oh.srv.st.SetStatusBecause(card, store.StatusDone, "reported"); err != nil {
		t.Fatal(err)
	}
	rep := sweepNow(t, oh)
	if len(rep.Leftovers) != 1 || rep.Leftovers[0].Owner != card || rep.Leftovers[0].NoCard ||
		rep.Leftovers[0].Closed || len(rep.Leftovers[0].Rows) != 4 {
		t.Fatalf("leftovers %+v", rep.Leftovers)
	}
	if !exists(wt) {
		t.Fatal("a sweep removed something")
	}
	// GET answers the last sweep.
	rec := httpDo(t, oh.h, "GET", "/v1/sweep")
	var last sweepReport
	_ = json.Unmarshal(rec.Body.Bytes(), &last)
	if last.SweptAt != rep.SweptAt || len(last.Leftovers) != 1 {
		t.Errorf("GET %s", rec.Body.String())
	}

	// The board's press: the card's own close.
	rec = post(t, oh.h, "/v1/sweep/close", map[string]any{"owner": card, "confirm": true})
	var out closeOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || !out.Closed || out.Left != 0 || exists(wt) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rep := sweepNow(t, oh); len(rep.Leftovers) != 0 {
		t.Errorf("closed and still offered: %+v", rep.Leftovers)
	}
}

func TestSweepMarksWhatIsGoneFreed(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if err := oh.srv.st.SetStatusBecause(card, store.StatusDead, "killed"); err != nil {
		t.Fatal(err)
	}
	repo := ""
	rows, _ := oh.srv.st.Resources(card)
	for _, r := range rows {
		if r.Kind == store.ResWorktree {
			repo = r.Detail
		}
	}
	git(t, repo, "worktree", "remove", "--force", filepath.FromSlash(wt))
	git(t, repo, "branch", "-D", "feat/x")

	rep := sweepNow(t, oh)
	gone := map[string]bool{}
	for _, f := range rep.Freed {
		gone[f.Kind] = true
	}
	if len(rep.Freed) != 2 || !gone[store.ResWorktree] || !gone[store.ResBranch] {
		t.Fatalf("freed %+v", rep.Freed)
	}
	if len(rep.Leftovers) != 1 || len(rep.Leftovers[0].Rows) != 2 {
		t.Fatalf("the ref and the review are still held: %+v", rep.Leftovers)
	}
	after, _ := oh.srv.st.Resources(card)
	for _, r := range after {
		if (r.Kind == store.ResWorktree || r.Kind == store.ResBranch) && r.Live() {
			t.Errorf("%s is still live", r.Kind)
		}
	}
}

// A card forgotten with its inventory, or an open that died before its card started, is closed by owner.
func TestSweepClosesAnOwnerWithNoCard(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if err := oh.srv.st.MoveResources(card, "forgotten"); err != nil {
		t.Fatal(err)
	}
	rep := sweepNow(t, oh)
	if len(rep.Leftovers) != 1 || rep.Leftovers[0].Owner != "forgotten" || !rep.Leftovers[0].NoCard {
		t.Fatalf("leftovers %+v", rep.Leftovers)
	}
	rec := post(t, oh.h, "/v1/sweep/close", map[string]any{"owner": "forgotten"})
	var pv closePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != 200 || len(pv.Items) != 4 || !exists(wt) {
		t.Fatalf("preview %d %s", rec.Code, rec.Body.String())
	}
	rec = post(t, oh.h, "/v1/sweep/close", map[string]any{"owner": "forgotten", "confirm": true})
	var out closeOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || !out.Closed || out.Left != 0 || exists(wt) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(oh.killed) != 0 {
		t.Errorf("an owner with no card stopped a session: %v", oh.killed)
	}
	if rec := post(t, oh.h, "/v1/sweep/close", map[string]any{"owner": "nobody"}); rec.Code != 404 {
		t.Errorf("an owner holding nothing is %d", rec.Code)
	}
}

// An open in flight holds its rows under a pending owner, and the sweep does not offer them.
func TestSweepLeavesAnOpenInFlightAlone(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	if _, err := oh.srv.st.AddResource("pending:github.com/o/r/7:x", store.ResDir, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if rep := sweepNow(t, oh); len(rep.Leftovers) != 0 {
		t.Fatalf("an open in flight was offered: %+v", rep.Leftovers)
	}
}

// A card closed with a worktree kept is listed as kept on purpose, so the board does not word it as forgotten.
func TestSweepSaysWhatAClosedCardKept(t *testing.T) {
	oh, card, wt, _ := openForClose(t)
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(wt), "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	seq := strconv.Itoa(worktreeSeq(t, oh.srv.st, card))
	if code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "answers": map[string]string{seq: "keep"}}); code != 200 {
		t.Fatalf("%d %+v", code, out)
	}
	rep := sweepNow(t, oh)
	if len(rep.Leftovers) != 1 || !rep.Leftovers[0].Closed || len(rep.Leftovers[0].Rows) != 2 {
		t.Fatalf("leftovers %+v", rep.Leftovers)
	}
}

func TestSweepListsAWorktreeNobodyOwns(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	scm := t.TempDir()
	if err := oh.srv.st.SetSetting(gitsync.SettingSCMRoot, scm); err != nil {
		t.Fatal(err)
	}
	repo := upstream(t)
	root := filepath.Join(scm, "worktrees", "github.com", "o", "r")
	stray, held := filepath.Join(root, "stray"), filepath.Join(root, "held")
	git(t, repo, "worktree", "add", "-b", "stray", stray)
	git(t, repo, "worktree", "add", "-b", "held", held)
	cardIn(t, oh.srv.st, held)

	rep := sweepNow(t, oh)
	if len(rep.NotOwned) != 1 || sweepKey(rep.NotOwned[0].Path) != sweepKey(stray) {
		t.Fatalf("not owned %+v", rep.NotOwned)
	}
	if !exists(filepath.ToSlash(stray)) {
		t.Error("a sweep removed a worktree nobody owns")
	}
}
