//go:build integration

package link

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

type fakeLandGit struct {
	facts  gitsync.LandFacts
	asked  []string
	forged []bool
}

func (f *fakeLandGit) LandFacts(_ context.Context, branch, sha, workID string, forge func() gitsync.ForgeMain) (gitsync.LandFacts, error) {
	f.asked = append(f.asked, branch+" "+workID)
	if f.facts.Landed && forge != nil {
		forge()
	}
	return f.facts, nil
}

func (f *fakeLandGit) ForgeMain(_ context.Context, force bool) gitsync.ForgeMain {
	f.forged = append(f.forged, force)
	return gitsync.ForgeMain{Tip: landedSHA}
}

func (f *fakeLandGit) Contains(context.Context, string, string) (bool, bool, error) {
	return false, false, nil
}

// A BRANCH FAST-FORWARDED ONTO claude/main SHOWS `landed` AT ITS SHA THE NEXT TIME ANYTHING READS, and the steps are
// stored, so a reader that cannot reach git still has them.
func TestAnItemShowsLandedOnTheNextRefresh(t *testing.T) {
	x := newItemRig(t)
	if code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-1", "room": "sg4", "card": "c1",
		"generation": 1, "state": "running", "revision": 1, "owner_kind": "session", "owner": "orch", "created_by": "orch",
		"note": "n", "work_id": "f-thing"}), loopAddr); code != http.StatusOK {
		t.Fatalf("attach: %d %s", code, body)
	}
	g := &fakeLandGit{}
	x.p.SetLandGit(g)
	// With no branch named and nothing found by id there is nothing to say.
	if rows, err := x.p.RefreshGitSteps(context.Background(), "f-thing", nil); err != nil || rows != nil {
		t.Fatalf("no branch: %v %v", rows, err)
	}
	g.facts = gitsync.LandFacts{SHA: landedSHA}
	if code, body := x.send("POST", "/_hub/work/f-thing", `{"branch":"claude/thing","kind":"code"}`, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("a write from elsewhere: %d %s", code, body)
	}
	if code, body := x.send("POST", "/_hub/work/f-thing", `{"branch":"claude/thing","kind":"code"}`, loopAddr); code != http.StatusOK {
		t.Fatalf("name the branch: %d %s", code, body)
	}
	_, body := x.send("GET", "/_hub/work/f-thing", "", "203.0.113.9:4000")
	if !strings.Contains(body, `"step":"landed","state":"active"`) {
		t.Fatalf("on its branch only: %s", body)
	}
	g.facts = gitsync.LandFacts{SHA: landedSHA, Landed: true, LandedSHA: landedSHA, Paths: []string{"docs/a.md"},
		Pushed: true, PushedSHA: "9999999999999999999999999999999999999999", PushedVia: "origin"}
	x.p.RefreshAllGitSteps(context.Background(), true)
	steps, _ := x.st.WorkSteps("f-thing")
	got := map[string]string{}
	for _, s := range steps {
		got[s.Step] = s.State + " " + s.SHA
	}
	if got["landed"] != "done "+landedSHA || got["pushed"] != "done 9999999999999999999999999999999999999999" || !strings.HasPrefix(got["deployed"], "skipped") {
		t.Fatalf("steps = %v", got)
	}
	// Settled: the next pass has nothing left to learn about it.
	if ids, _ := x.st.WorkIdsOpen(); len(ids) != 0 {
		t.Fatalf("a settled item is still open: %v", ids)
	}
	if code, _ := x.send("POST", "/_hub/work/f-thing", `{"kind":"weird"}`, loopAddr); code != http.StatusBadRequest {
		t.Fatalf("a kind that is not one: %d", code)
	}
}

// AN ITEM NOBODY NAMED A BRANCH FOR IS READ BY ITS BUILDER'S SHA, and shows landed and pushed with rows that carry its id.
func TestAnItemWithNoBranchLandsByItsBuilderSha(t *testing.T) {
	x := newItemRig(t)
	x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-1", "room": "sg4", "card": "c1",
		"generation": 1, "state": "finished", "revision": 2, "owner_kind": "session", "owner": "orch", "created_by": "orch",
		"note": landedSHA, "work_id": "f-nb"}), loopAddr)
	g := &fakeLandGit{facts: gitsync.LandFacts{SHA: landedSHA, Landed: true, LandedSHA: "8888888888888888888888888888888888888888",
		Paths: []string{"docs/a.md"}, Pushed: true, PushedSHA: "9999999999999999999999999999999999999999", PushedVia: "origin"}}
	x.p.SetLandGit(g)
	// The board's own list is what first writes the item's building step, which is what says it has been built.
	x.send("GET", "/_hub/work", "", "203.0.113.9:4000")
	x.p.RefreshAllGitSteps(context.Background(), true)
	steps, _ := x.st.WorkSteps("f-nb")
	got := map[string]string{}
	for _, s := range steps {
		if s.BacklogID != "f-nb" {
			t.Fatalf("a step with no item: %+v", s)
		}
		got[s.Step] = s.State + " " + s.SHA
	}
	if got["landed"] != "done 8888888888888888888888888888888888888888" || got["pushed"] != "done 9999999999999999999999999999999999999999" {
		t.Fatalf("steps = %v", got)
	}
}

// ONE ASK OF THE FORGE PER PASS, however many items have landed, and none when nothing has.
func TestAPassAsksTheForgeOnceForEveryItem(t *testing.T) {
	x := newItemRig(t)
	for _, id := range []string{"f-a", "f-b", "f-c"} {
		x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-" + id, "room": "sg4", "card": "c-" + id,
			"generation": 1, "state": "running", "revision": 1, "owner_kind": "session", "owner": "orch", "created_by": "orch",
			"note": "n", "work_id": id}), loopAddr)
		x.send("POST", "/_hub/work/"+id, `{"branch":"claude/`+id+`"}`, loopAddr)
	}
	g := &fakeLandGit{facts: gitsync.LandFacts{SHA: landedSHA, Landed: true, LandedSHA: landedSHA, Paths: []string{"docs/a.md"}}}
	x.p.SetLandGit(g)
	g.forged = nil
	x.p.RefreshAllGitSteps(context.Background(), true)
	if len(g.forged) != 1 || !g.forged[0] {
		t.Fatalf("a forced pass asked the forge %v", g.forged)
	}
	g.forged = nil
	g.facts = gitsync.LandFacts{SHA: landedSHA}
	x.p.RefreshAllGitSteps(context.Background(), false)
	if len(g.forged) != 0 {
		t.Fatalf("nothing had landed and the forge was asked %v", g.forged)
	}
}

// AN ITEM WHOSE BUILD NEVER READ AS DONE AND WHOSE ROW HAS NO BRANCH STILL SHOWS LANDED once its changelog entry is on
// claude/main: the live hub left such items pending because nothing asked git about them.
func TestAnUnbuiltItemWithNoBranchLandsByItsChangelogEntry(t *testing.T) {
	x := newItemRig(t)
	x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-1", "room": "sg4", "card": "c1",
		"generation": 1, "state": "running", "revision": 1, "owner_kind": "session", "owner": "orch", "created_by": "orch",
		"note": "n", "work_id": "f-ub"}), loopAddr)
	g := &fakeLandGit{facts: gitsync.LandFacts{Landed: true, LandedSHA: "8888888888888888888888888888888888888888",
		Paths: []string{"docs/a.md"}, Pushed: true, PushedSHA: "9999999999999999999999999999999999999999", PushedVia: "origin"}}
	x.p.SetLandGit(g)
	x.p.RefreshAllGitSteps(context.Background(), true)
	steps, _ := x.st.WorkSteps("f-ub")
	got := map[string]string{}
	for _, s := range steps {
		got[s.Step] = s.State + " " + s.SHA
	}
	if got["landed"] != "done 8888888888888888888888888888888888888888" || got["pushed"] != "done 9999999999999999999999999999999999999999" {
		t.Fatalf("steps = %v", got)
	}
}
