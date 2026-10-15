package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSplitArgsAllowsBranchBeforeOrAfterFlags(t *testing.T) {
	for _, in := range [][]string{{"b", "--item", "t-1"}, {"--item", "t-1", "b"}, {"--item=t-1", "b"}} {
		pos, flags := splitArgs(in, "item")
		if !reflect.DeepEqual(pos, []string{"b"}) || len(flags) == 0 {
			t.Fatalf("%v -> pos %v flags %v", in, pos, flags)
		}
	}
}

func TestLandPlanFastForwardOrCherryPick(t *testing.T) {
	if got := landPlan("x", true); !reflect.DeepEqual(got, [][]string{{"merge", "--ff-only", "hub/x"}}) {
		t.Fatal(got)
	}
	if got := landPlan("x", false); !reflect.DeepEqual(got, [][]string{{"cherry-pick", "claude/main..hub/x"}}) {
		t.Fatal(got)
	}
	rb := landRollback("abc", false)
	if len(rb) != 2 || rb[0][1] != "--abort" || rb[1][0] != "reset" {
		t.Fatal(rb)
	}
	if rb := landRollback("abc", true); len(rb) != 1 {
		t.Fatal(rb)
	}
}

type fakeGit struct {
	status, branch string
	failOn         string
	calls          []string
}

func (f *fakeGit) run(_ string, args ...string) (string, error) {
	c := strings.Join(args, " ")
	f.calls = append(f.calls, c)
	if f.failOn != "" && strings.HasPrefix(c, f.failOn) {
		return "conflict", errFake
	}
	switch {
	case strings.HasPrefix(c, "status"):
		return f.status, nil
	case strings.HasPrefix(c, "rev-parse --abbrev-ref"):
		return f.branch, nil
	case strings.HasPrefix(c, "rev-parse"):
		return "0123456789abcdef", nil
	}
	return "", nil
}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake failure" }

var errFake = fakeErr{}

func TestLandRefusesDirtyTreeWithoutTouchingBranch(t *testing.T) {
	g := &fakeGit{status: " M a.go", branch: "claude/main"}
	err := cmdLand([]string{"b", "--no-deploy"}, &bytes.Buffer{}, g)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatal(err)
	}
	if len(g.calls) != 1 {
		t.Fatalf("only the status check should run: %v", g.calls)
	}
}

func TestLandRollsBackOnConflict(t *testing.T) {
	g := &fakeGit{branch: "claude/main", failOn: "cherry-pick claude"}
	// merge-base failing means not an ancestor, so a cherry-pick is planned
	g2 := &mbFail{fakeGit: g}
	err := cmdLand([]string{"b", "--no-deploy"}, &bytes.Buffer{}, g2)
	if err == nil {
		t.Fatal("want an error")
	}
	last := g.calls[len(g.calls)-1]
	if !strings.HasPrefix(last, "reset --hard 0123456789abcdef") {
		t.Fatalf("claude/main was not restored: %v", g.calls)
	}
}

type mbFail struct{ *fakeGit }

func (m *mbFail) run(d string, args ...string) (string, error) {
	if args[0] == "merge-base" {
		m.calls = append(m.calls, "merge-base")
		return "", errFake
	}
	return m.fakeGit.run(d, args...)
}

func TestWorktreeFor(t *testing.T) {
	p := "worktree /r\nHEAD aaa\nbranch refs/heads/main\n\nworktree /r/wt\r\nHEAD bbb\r\nbranch refs/heads/claude/x\r\n"
	if got := worktreeFor(p, "claude/x"); got != "/r/wt" {
		t.Fatal(got)
	}
	if got := worktreeFor(p, "nope"); got != "" {
		t.Fatal(got)
	}
}

func TestSplitCard(t *testing.T) {
	if r, id := splitCard("sg3~abc", "x"); r != "sg3" || id != "abc" {
		t.Fatal(r, id)
	}
	if r, id := splitCard("abc", "x"); r != "x" || id != "abc" {
		t.Fatal(r, id)
	}
}

func TestPngNames(t *testing.T) {
	got := pngNames("a.png\r\nb.txt\r\nsub/c.png\r\nD.PNG\r\n")
	if !reflect.DeepEqual(got, []string{"a.png", "D.PNG"}) {
		t.Fatal(got)
	}
}

func TestMatchSaysUsesEachTurnOnce(t *testing.T) {
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string { return base.Add(d).Format(time.RFC3339) }
	says := []say{
		{ID: "s1", Room: "r", ToTask: "c", SentAt: ts(0), To: "w"},
		{ID: "s2", Room: "r", ToTask: "c", SentAt: ts(time.Minute), To: "w"},
		{ID: "s3", Room: "r", ToTask: "c", SentAt: ts(2 * time.Minute), To: "w@other"},
	}
	turns := []turn{{ID: "t1", Room: "r", TaskID: "c", Cause: "say", StartedAt: ts(2 * time.Second), Input: 1, CacheWrite5m: 2, CacheWrite1h: 3}}
	rows := matchSays(says, turns)
	if len(rows) != 2 {
		t.Fatalf("a say to another room is skipped: %v", rows)
	}
	if rows[0][9] != "true" || rows[0][11] != "5" || rows[1][9] != "false" {
		t.Fatalf("%v", rows)
	}
}

func TestTokenAndCardRows(t *testing.T) {
	cards := map[string]card{"r~1": {ID: "1", Title: "t", DisplayTitle: "Shown", Room: "r", CreatedAt: "2026-10-15T03:00:00Z", Status: "done"}}
	turns := []turn{
		{ID: "a", Room: "r", TaskID: "1", Cause: "say", Input: 1, CacheWrite5m: 2, CacheRead: 10, Output: 3, Model: "m", EndedAt: "2026-10-15T04:00:00Z"},
		{ID: "b", Room: "r", TaskID: "1", Input: 1, CacheRead: 5, Output: 1, Model: "m", EndedAt: "2026-10-15T05:00:00Z"},
		{ID: "c", Room: "r", TaskID: "9", CacheRead: 100},
	}
	rows, all := tokenRows(turns, cards)
	if len(rows) != 3 || rows[0][1] != "9" || rows[1][1] != "Shown" || rows[2][0] != "ALL" {
		t.Fatalf("%v", rows)
	}
	if all.turns != 3 || all.cacheRead != 115 || all.sayTurns != 1 {
		t.Fatalf("%+v", all)
	}
	since := time.Date(2026, 10, 15, 2, 30, 0, 0, time.UTC)
	cr := cardRows(cards, turns, since, map[string]string{"t": "abc"})
	if len(cr) != 1 || cr[0][4] != "m" || cr[0][6] != "2026-10-15T05:00:00Z" || cr[0][12] != "23" || cr[0][13] != "abc" {
		t.Fatalf("%v", cr)
	}
	if got := cardRows(cards, turns, since.Add(24*time.Hour), nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestWriteCSVQuotes(t *testing.T) {
	p := t.TempDir() + "/x.csv"
	if err := writeCSV(p, []string{"a", "b"}, [][]string{{"x,y", "z"}}); err != nil {
		t.Fatal(err)
	}
}
