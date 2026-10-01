package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pulls index, the run folder and the recipe.

func openPRStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/atrium.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.DefaultReviewsRoot = filepath.ToSlash(t.TempDir())
	return st
}

func newTestPR(t *testing.T, st *Store, head string) *PRReview {
	t.Helper()
	dir, err := st.RunFolder("openziti", "tlsuv", 378, head)
	if err != nil {
		t.Fatal(err)
	}
	p, created, err := st.CreatePR(NewPR{URL: "https://github.com/openziti/tlsuv/pull/378", Why: "why",
		Host: "github.com", Org: "openziti", Repo: "tlsuv", Number: 378, Head: head, RunDir: dir})
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	return p
}

func TestTheDefaultRecipeIsSeededOnceAndStaysDeleted(t *testing.T) {
	path := t.TempDir() + "/atrium.db"
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := first.PRRecipeByName("default")
	if err != nil {
		t.Fatal(err)
	}
	if r.Harness != "claude" || r.Match != "" || r.BudgetUSD <= 0 {
		t.Fatalf("seed is wrong: %+v", r)
	}
	if panel, err := r.Reviewers(); err != nil || len(panel) != 4 {
		t.Fatalf("seed panel: %v %d", err, len(panel))
	}
	if err := first.DeletePRRecipe("default"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.PRRecipeByName("default"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the deleted default came back: %v", err)
	}
	if _, err := second.RecipeFor("a/b"); !errors.Is(err, ErrNoPRRecipe) {
		t.Fatalf("no recipe should be reported, got %v", err)
	}
}

func TestCreatePRIsIdempotentPerRunFolder(t *testing.T) {
	st := openPRStore(t)
	a := newTestPR(t, st, "ad5ddf4aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	b, created, err := st.CreatePR(NewPR{Org: "openziti", Repo: "tlsuv", Number: 378, Host: "github.com",
		Head: "ad5ddf4aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RunDir: a.RunDir})
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("second ask: %v created=%v same=%v", err, created, b != nil && b.ID == a.ID)
	}
	if a.State != PRQueued || a.Head7 != "ad5ddf4" || a.OrgRepo != "openziti/tlsuv" {
		t.Fatalf("new row: %+v", a)
	}
	other := newTestPR2(t, st)
	if other.ID == a.ID {
		t.Fatal("a different head is a different review")
	}
}

func newTestPR2(t *testing.T, st *Store) *PRReview {
	t.Helper()
	dir, _ := st.RunFolder("openziti", "tlsuv", 378, "")
	p, _, err := st.CreatePR(NewPR{Org: "openziti", Repo: "tlsuv", Number: 378, Host: "github.com", RunDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreatePRRefusals(t *testing.T) {
	st := openPRStore(t)
	cases := map[string]NewPR{
		"no org":    {Repo: "r", Number: 1, RunDir: "x"},
		"no number": {Org: "o", Repo: "r", RunDir: "x"},
		"no dir":    {Org: "o", Repo: "r", Number: 1},
		"bad head":  {Org: "o", Repo: "r", Number: 1, RunDir: "x", Head: "zzz"},
		"long why":  {Org: "o", Repo: "r", Number: 1, RunDir: "x", Why: strings.Repeat("a", MaxPRWhy+1)},
	}
	for name, in := range cases {
		if _, _, err := st.CreatePR(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPRStatesStampAndClear(t *testing.T) {
	st := openPRStore(t)
	p := newTestPR(t, st, "")
	if p.StartedAt != "" {
		t.Fatal("queued rows have not started")
	}
	p, err := st.SetPRState(p.ID, PRFetching, "fetch", "")
	if err != nil || p.StartedAt == "" {
		t.Fatalf("fetching stamps started_at: %v %+v", err, p)
	}
	started := p.StartedAt
	p, _ = st.SetPRState(p.ID, PRRunning, "step 3", "")
	if p.StartedAt != started {
		t.Fatal("started_at is stamped once")
	}
	p, _ = st.SetPRState(p.ID, PRFailed, "step 3", "boom")
	if p.RunError != "boom" {
		t.Fatalf("failed keeps its error: %+v", p)
	}
	p, _ = st.SetPRState(p.ID, PRRunning, "step 3", "stale")
	if p.RunError != "" {
		t.Fatal("run_error is cleared outside failed")
	}
	p, _ = st.SetPRState(p.ID, PRReady, "", "")
	if p.ReadyAt == "" {
		t.Fatal("ready stamps ready_at")
	}
	if _, err := st.SetPRState(p.ID, "bogus", "", ""); err == nil {
		t.Fatal("a bogus state was accepted")
	}
	if _, err := st.SetPRState("nope", PRRunning, "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row: %v", err)
	}
	p, err = st.AddPRCost(p.ID, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = st.AddPRCost(p.ID, 0.25)
	if p.CostUSD != 0.75 {
		t.Fatalf("cost %v", p.CostUSD)
	}
	p, err = st.ResetPR(p.ID, "")
	if err != nil || p.State != PRQueued || p.CostUSD != 0 || p.StartedAt != "" || p.ReadyAt != "" {
		t.Fatalf("reset: %v %+v", err, p)
	}
}

func TestPRCountsHasEveryKey(t *testing.T) {
	st := openPRStore(t)
	counts, err := st.PRCounts()
	if err != nil || len(counts) != 6 {
		t.Fatalf("%v %v", err, counts)
	}
	p := newTestPR(t, st, "")
	st.SetPRState(p.ID, PRReady, "", "")
	counts, _ = st.PRCounts()
	if counts[PRReady] != 1 || counts[PRQueued] != 0 || len(counts) != 6 {
		t.Fatalf("%v", counts)
	}
	rows, _ := st.PRs(PRFilter{States: []string{PRQueued}})
	if len(rows) != 0 {
		t.Fatal("filter by state")
	}
	rows, _ = st.PRs(PRFilter{OrgRepo: "openziti/tlsuv"})
	if len(rows) != 1 {
		t.Fatal("filter by repo")
	}
}

func TestSetPRFetchedMovesTheRun(t *testing.T) {
	st := openPRStore(t)
	p := newTestPR(t, st, "")
	if !strings.HasSuffix(p.RunDir, "/pr-378-pending") {
		t.Fatalf("pending folder: %s", p.RunDir)
	}
	dir, _ := st.RunFolder("openziti", "tlsuv", 378, "ad5ddf4")
	p, err := st.SetPRFetched(p.ID, "AD5DDF4FFFF", "A title", "alice", dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Head != "ad5ddf4ffff" || p.Title != "A title" || p.Author != "alice" || p.RunDir != dir {
		t.Fatalf("%+v", p)
	}
	if _, err := st.SetPRFetched(p.ID, "xyz", "", "", ""); err == nil {
		t.Fatal("bad head accepted")
	}
	p, _ = st.SetPRWalker(p.ID, "card1")
	if p.WalkerTask != "card1" {
		t.Fatal("walker")
	}
	p, _ = st.SetPRSecond(p.ID, "done", "fine", "")
	if p.Second.State != "done" {
		t.Fatal("second")
	}
	if _, err := st.SetPRSecond(p.ID, "weird", "", ""); err == nil {
		t.Fatal("bad second state accepted")
	}
}

func TestRunFolderPathNames(t *testing.T) {
	cases := []struct {
		host, org, repo string
		n               int
		head, want      string
		bad             bool
	}{
		{"github.com", "openziti", "tlsuv", 378, "ad5ddf4aaaa", "/r/github-openziti-tlsuv/pr-378-ad5ddf4", false},
		{"github.com", "openziti", "tlsuv", 378, "", "/r/github-openziti-tlsuv/pr-378-pending", false},
		{"github.com", "..", "a/b", 1, "", "/r/github-_-a-b/pr-1-pending", false},
		{"github.com", "o", "r", 1, "nothex", "", true},
		{"github.com", "o", "r", 0, "", "", true},
	}
	for _, c := range cases {
		got, err := RunFolderPath("/r", c.host, c.org, c.repo, c.n, c.head)
		if c.bad {
			if err == nil {
				t.Errorf("%+v accepted", c)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%+v: got %q %v want %q", c, got, err, c.want)
		}
	}
	if _, err := RunFolderPath("", "h", "o", "r", 1, ""); err == nil {
		t.Error("empty root accepted")
	}
}

func TestRunFolderMakesItsSubfolders(t *testing.T) {
	st := openPRStore(t)
	dir, err := st.RunFolder("openziti", "tlsuv", 378, "ad5ddf4")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"steps", "findings"} {
		if fi, err := os.Stat(filepath.Join(filepath.FromSlash(dir), sub)); err != nil || !fi.IsDir() {
			t.Fatalf("%s not made: %v", sub, err)
		}
	}
	if again, err := st.RunFolder("openziti", "tlsuv", 378, "ad5ddf4"); err != nil || again != dir {
		t.Fatalf("not idempotent: %v", err)
	}
	if err := st.SetSetting(SettingReviewsRoot, "D:/elsewhere"); err != nil {
		t.Fatal(err)
	}
	if got := st.ReviewsRoot(); got != "D:/elsewhere" {
		t.Fatalf("setting ignored: %s", got)
	}
}

func goodRecipe(name, match string) PRRecipe {
	return PRRecipe{Name: name, Match: match, Harness: "claude", Panel: `[{"agent":"a","when":["*"]}]`,
		WalkerBrief: "walk", BudgetUSD: 1, TurnsCap: 5}
}

func TestRecipeForMostSpecificWins(t *testing.T) {
	st := openPRStore(t)
	for _, r := range []PRRecipe{goodRecipe("org", "openziti/*"), goodRecipe("exact", "openziti/tlsuv")} {
		if _, err := st.SavePRRecipe(r); err != nil {
			t.Fatal(err)
		}
	}
	for repo, want := range map[string]string{"openziti/tlsuv": "exact", "openziti/ziti": "org", "other/x": "default"} {
		got, err := st.RecipeFor(repo)
		if err != nil || got.Name != want {
			t.Errorf("%s: got %v %v want %s", repo, got, err, want)
		}
	}
}

func TestSavePRRecipeRefusals(t *testing.T) {
	st := openPRStore(t)
	mut := map[string]func(r *PRRecipe){
		"no name":     func(r *PRRecipe) { r.Name = " " },
		"no harness":  func(r *PRRecipe) { r.Harness = "" },
		"no brief":    func(r *PRRecipe) { r.WalkerBrief = "" },
		"bad match":   func(r *PRRecipe) { r.Match = "[" },
		"bad panel":   func(r *PRRecipe) { r.Panel = "nope" },
		"empty panel": func(r *PRRecipe) { r.Panel = "[]" },
		"no agent":    func(r *PRRecipe) { r.Panel = `[{"agent":"","when":["*"]}]` },
		"bad glob":    func(r *PRRecipe) { r.Panel = `[{"agent":"a","when":["["]}]` },
		"bad critics": func(r *PRRecipe) { r.Critics = "{" },
		"bad verify":  func(r *PRRecipe) { r.VerifyAt = "sometimes" },
		"no budget":   func(r *PRRecipe) { r.BudgetUSD = 0 },
		"no turns":    func(r *PRRecipe) { r.TurnsCap = 0 },
	}
	for name, f := range mut {
		r := goodRecipe("x", "")
		f(&r)
		if _, err := st.SavePRRecipe(r); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	saved, err := st.SavePRRecipe(goodRecipe("x", ""))
	if err != nil || saved.VerifyAt != "med" || saved.Critics != "[]" {
		t.Fatalf("defaults: %v %+v", err, saved)
	}
}

func TestReviewersForMatchesBaseNames(t *testing.T) {
	st := openPRStore(t)
	r, _ := st.PRRecipeByName("default")
	got, err := r.ReviewersFor([]string{"lib/src/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range got {
		names[e.Agent] = true
	}
	if !names["go-security-reviewer"] || names["c-systems-reviewer"] || !names["functional-tester"] {
		t.Fatalf("go file: %v", names)
	}
	got, _ = r.ReviewersFor([]string{"a/b.h"})
	if len(got) != 3 {
		t.Fatalf("header file: %v", got)
	}
}

func TestMovePRRefusesAWrongStateWithoutHaltingTheStore(t *testing.T) {
	st := openPRStore(t)
	p := newTestPR(t, st, "")
	if _, err := st.MovePR(p.ID, []string{PRFailed}, PRRunning, "", ""); !errors.Is(err, ErrPRState) {
		t.Fatalf("a queued row moved from failed: %v", err)
	}
	if halted, _ := st.Halted(); halted {
		t.Fatal("a wrong state halted the store")
	}
	if _, err := st.MovePR("nope", []string{PRQueued}, PRRunning, "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row: %v", err)
	}
	got, err := st.MovePR(p.ID, []string{PRQueued, PRFailed}, PRRunning, "panel", "")
	if err != nil || got.State != PRRunning {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := st.ResetPR(p.ID, "", PRFailed, PRAborted); !errors.Is(err, ErrPRState) {
		t.Fatalf("reset of a running row: %v", err)
	}
	if got, _ := st.PRByID(p.ID); got.State != PRRunning {
		t.Fatalf("a refused reset changed the row: %+v", got)
	}
	st.SetPRState(p.ID, PRFailed, "", "boom")
	if got, err := st.ResetPR(p.ID, "", PRFailed, PRAborted); err != nil || got.State != PRQueued {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestLivePRFindsTheRunningReviewOfAPullRequest(t *testing.T) {
	st := openPRStore(t)
	if got, err := st.LivePR("github.com", "openziti", "tlsuv", 378); err != nil || got != nil {
		t.Fatalf("empty index: %v %v", err, got)
	}
	p := newTestPR(t, st, "")
	// The fetch step moves the run off its pending folder.
	dir, _ := st.RunFolder("openziti", "tlsuv", 378, "ad5ddf4")
	st.SetPRFetched(p.ID, "ad5ddf4", "t", "a", dir)
	got, err := st.LivePR("github.com", "openziti", "tlsuv", 378)
	if err != nil || got == nil || got.ID != p.ID {
		t.Fatalf("%v %v", err, got)
	}
	st.SetPRState(p.ID, PRReady, "", "")
	if got, _ := st.LivePR("github.com", "openziti", "tlsuv", 378); got != nil {
		t.Fatal("a ready review is not live")
	}
	if got, _ := st.LivePR("github.com", "openziti", "tlsuv", 379); got != nil {
		t.Fatal("another pull request")
	}
}
