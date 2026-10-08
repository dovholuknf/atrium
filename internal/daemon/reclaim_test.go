package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-card-reclaim-on-done. A worker's done closes its card on every path, and the launcher's atrium_cull reclaims
// it. See reclaim.go.

// scratchDir is a directory a launcher made for a worker, holding the BRIEF.md atrium wrote.
func scratchDir(t *testing.T, extra ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "atrium-work", "w1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, briefFile), []byte("brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ── H3: done closes the card, by every door ─────────────────────────────────────────────────────────────────

// The launch text tells a worker to atrium_say `done <sha>`. That is a done report too: the card closes, the report
// lands on the work item, and the launcher is not sent a second notice for words it already has.
func TestSayingDoneToTheLauncherClosesTheCard(t *testing.T) {
	d := testDaemon(t)
	launcher := peerCard(t, d, "orchestrator")
	worker := cullCard(t, d, scratchDir(t), OriginAgentTag, SubagentTag)
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	worker, _ = d.st.Get(worker.ID)
	if _, err := d.st.CreateWorkItem(worker, store.NewWorkItem{Brief: "do the thing"}); err != nil {
		t.Fatal(err)
	}

	d.peerSaid(worker.WireName, launcher, "done 8918f846 all tests pass", KindNeeds)

	got, _ := d.st.Get(worker.ID)
	if got.Status != store.StatusDone {
		t.Fatalf("status = %s, want done", got.Status)
	}
	w := itemOf(t, d, worker.ID)
	if w.LastReport == nil || w.LastReport.Status != ReportDone {
		t.Fatalf("last report = %+v, want done", w.LastReport)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Errorf("the launcher has %d queued notices, want none: it has the worker's own words", n)
	}
}

// Free text is never read as a verdict, and only the worker's words to its own launcher count.
func TestOnlyDoneAndAShaClosesACardBySay(t *testing.T) {
	for _, text := range []string{
		"done with the refactor, one thing left", "done", "done abc", "blocked: no sha", "almost done 8918f846",
		"not done 8918f846",
	} {
		d := testDaemon(t)
		launcher := peerCard(t, d, "orchestrator")
		worker := cullCard(t, d, scratchDir(t), OriginAgentTag, SubagentTag)
		if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
			t.Fatal(err)
		}
		if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
			t.Fatal(err)
		}
		d.peerSaid(worker.WireName, launcher, text, KindNeeds)
		if got, _ := d.st.Get(worker.ID); got.Status == store.StatusDone {
			t.Errorf("%q closed the card", text)
		}
	}

	// The launcher saying it to the worker, or a card nobody launched saying it to anyone.
	d := testDaemon(t)
	launcher := peerCard(t, d, "orchestrator")
	worker := cullCard(t, d, scratchDir(t), OriginAgentTag, SubagentTag)
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	d.peerSaid(launcher.WireName, worker, "done 8918f846", KindNeeds)
	if got, _ := d.st.Get(worker.ID); got.Status == store.StatusDone {
		t.Error("the launcher saying done to the worker closed the worker's card")
	}
	own := peerCard(t, d, "mine")
	if err := d.st.SetStatus(own.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.peerSaid(own.WireName, launcher, "done 8918f846", KindNeeds)
	if got, _ := d.st.Get(own.ID); got.Status == store.StatusDone {
		t.Error("a card nobody launched was closed by a say")
	}
}

// A card tagged to stay open, or an investigation, is not ended by its own done report.
func TestAStayOpenCardIsNotExitedByDone(t *testing.T) {
	d := testDaemon(t)
	for _, tc := range []struct {
		tags []string
		exit bool
	}{
		{[]string{OriginAgentTag}, true},
		{[]string{OriginAgentTag, KeepOpenTag}, false},
		{[]string{OriginAgentTag, InvestigationTag}, false},
	} {
		task := &store.Task{SpawnedByID: "launcher", Tags: tc.tags}
		if got := d.exitsOnReport(task, FinishRequest{Status: ReportDone}); got != tc.exit {
			t.Errorf("tags %v: exits = %v, want %v", tc.tags, got, tc.exit)
		}
	}
}

// ── H11: the launcher's atrium_cull reclaims what the card made ─────────────────────────────────────────────

// A worker in a scratch directory: its session, its BRIEF.md and its directory go, and the card is archived with
// its history.
func TestCullReclaimsAScratchDirectoryAndItsBrief(t *testing.T) {
	d := testDaemon(t)
	dir := scratchDir(t)
	_, worker := mergedWorker(t, d, cullRepo{wt: dir})

	res, err := d.Cull(worker.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.BriefRemoved || !res.ScratchRemoved || res.Kept != "" {
		t.Fatalf("result = %+v, want the brief and the directory removed", res)
	}
	if exists(dir) {
		t.Error("the scratch directory is still there")
	}
	got, _ := d.st.Get(worker.ID)
	if got.ArchivedAt == nil {
		t.Error("the card is still on the board")
	}
	if got.Status != store.StatusDone {
		t.Errorf("status = %s: a reclaim must not file accepted work as dead", got.Status)
	}
	if w := itemOf(t, d, worker.ID); w.State != store.WorkAccepted {
		t.Errorf("work state = %s, want accepted", w.State)
	}
}

// What the worker left in its directory is its output, not atrium's to delete: BRIEF.md goes, the rest stays.
func TestCullKeepsAScratchDirectoryThatHoldsOtherFiles(t *testing.T) {
	d := testDaemon(t)
	dir := scratchDir(t, "notes.txt")
	_, worker := mergedWorker(t, d, cullRepo{wt: dir})

	res, err := d.Cull(worker.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.BriefRemoved || res.ScratchRemoved || !strings.Contains(res.Kept, "notes.txt") {
		t.Fatalf("result = %+v, want the brief gone and the directory kept with a reason naming the file", res)
	}
	if exists(filepath.Join(dir, briefFile)) || !exists(filepath.Join(dir, "notes.txt")) {
		t.Error("BRIEF.md should be gone and notes.txt kept")
	}
}

// H4 and the clint-started rule: none of these is ever reclaimed, and nothing on disk moves.
func TestCullNeverReclaimsWhatMustStay(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		set  func(t *testing.T, d *Daemon, w *store.Task)
		want string
	}{
		{"clints own card", []string{SubagentTag}, nil, "an agent did not launch it"},
		{"kept open", []string{OriginAgentTag, SubagentTag, KeepOpenTag}, nil, "stay open"},
		{"an investigation", []string{OriginAgentTag, SubagentTag, InvestigationTag}, nil, "stay open"},
		{"ended without finishing", []string{OriginAgentTag, SubagentTag}, func(t *testing.T, d *Daemon, w *store.Task) {
			if err := d.st.SetStatus(w.ID, store.StatusDead); err != nil {
				t.Fatal(err)
			}
		}, "its work is not done"},
		{"waiting on an answer", []string{OriginAgentTag, SubagentTag}, func(t *testing.T, d *Daemon, w *store.Task) {
			if err := d.st.SetStatus(w.ID, store.StatusNeedsInput); err != nil {
				t.Fatal(err)
			}
		}, "its work is not done"},
		{"last report was blocked", []string{OriginAgentTag, SubagentTag}, func(t *testing.T, d *Daemon, w *store.Task) {
			if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "which table?"}); rec.Code != 200 {
				t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
			}
			if err := d.st.SetStatus(w.ID, store.StatusDone); err != nil {
				t.Fatal(err)
			}
		}, "its last report was not done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := testDaemon(t)
			dir := scratchDir(t)
			_, worker := mergedWorker(t, d, cullRepo{wt: dir}, tc.tags...)
			if err := d.st.SetTags(worker.ID, tc.tags); err != nil {
				t.Fatal(err)
			}
			if tc.set != nil {
				tc.set(t, d, worker)
			}

			_, err := d.Cull(worker.ID, "")
			if err == nil {
				t.Fatal("the cull went ahead")
			}
			if !strings.Contains(err.Error(), tc.want) && !strings.Contains(err.Error(), "not tagged") {
				t.Errorf("err = %v, want it to say %q", err, tc.want)
			}
			if !exists(filepath.Join(dir, briefFile)) {
				t.Error("BRIEF.md was removed")
			}
			if got, _ := d.st.Get(worker.ID); got.ArchivedAt != nil {
				t.Error("the card was archived")
			}
		})
	}
}

// The sweep re-checks the same rules, so a card that became ineligible after it was marked is let go.
func TestTheSweepNeverCullsAStayOpenCard(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	markedMerged(t, d, DefaultCullInto)
	if err := d.st.SetTags(worker.ID, []string{OriginAgentTag, SubagentTag, KeepOpenTag}); err != nil {
		t.Fatal(err)
	}

	d.cullDue(itemOf(t, d, worker.ID))

	if !exists(r.wt) || !r.branchExists(t) {
		t.Error("a card tagged to stay open lost its worktree or branch")
	}
	if got, _ := d.st.Get(worker.ID); got.ArchivedAt != nil {
		t.Error("the card was archived")
	}
}

// A merge alone reclaims nothing: the setting is off until the operator sets a grace, and the launcher's
// atrium_cull is what says the work is merged and deployed.
func TestAMergeAloneReclaimsNothingByDefault(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	launcher := peerCard(t, d, "orchestrator")
	worker := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	if err := d.st.SetSetting(SettingMergedCullGrace, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}

	res, err := d.Merged(DefaultCullInto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Off || len(res.Marked) != 0 {
		t.Fatalf("result = %+v, want off and nothing marked", res)
	}
}

// ── what is removed, and what never is ──────────────────────────────────────────────────────────────────────

func TestReclaimScratchLeavesAGitCheckoutAlone(t *testing.T) {
	r := newCullRepo(t, true)
	// BRIEF.md tracked by the repository is somebody's file.
	if err := os.WriteFile(filepath.Join(r.main, briefFile), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cullGit(t, r.main, "add", briefFile)
	cullGit(t, r.main, "commit", "-q", "-m", "a tracked brief")
	sub := filepath.Join(r.main, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	briefGone, dirGone, kept := reclaimScratch(r.main)
	if briefGone || dirGone || kept == "" {
		t.Errorf("the main checkout: brief %v dir %v kept %q, want nothing removed and a reason", briefGone, dirGone, kept)
	}
	if !exists(filepath.Join(r.main, briefFile)) || !exists(r.main) {
		t.Error("the main checkout lost a file")
	}
	// An empty directory inside a checkout is not removed either.
	if _, dirGone, _ := reclaimScratch(sub); dirGone || !exists(sub) {
		t.Error("a directory inside a git checkout was removed")
	}
}

func TestReclaimScratchRefusesWhatIsNotItsToRemove(t *testing.T) {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	if _, dirGone, kept := reclaimScratch(root); dirGone || kept == "" {
		t.Errorf("a drive root: dir %v kept %q", dirGone, kept)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, dirGone, kept := reclaimScratch(home); dirGone || kept == "" {
			t.Errorf("the home folder: dir %v kept %q", dirGone, kept)
		}
	}
	if _, dirGone, kept := reclaimScratch("relative/dir"); dirGone || kept == "" {
		t.Errorf("a relative path: dir %v kept %q", dirGone, kept)
	}
	// Gone already is reclaimed.
	gone := filepath.Join(t.TempDir(), "nope")
	if briefGone, dirGone, kept := reclaimScratch(gone); !briefGone || !dirGone || kept != "" {
		t.Errorf("a directory already gone: %v %v %q", briefGone, dirGone, kept)
	}
}

// A BRIEF.md that is a link to somewhere else is not followed and not removed.
func TestReclaimScratchDoesNotFollowABriefLink(t *testing.T) {
	dir := scratchDir(t)
	outside := filepath.Join(t.TempDir(), "precious.txt")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, briefFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, briefFile)); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	briefGone, dirGone, _ := reclaimScratch(dir)
	if briefGone || dirGone {
		t.Errorf("brief %v dir %v, want a link left alone", briefGone, dirGone)
	}
	if !exists(outside) {
		t.Error("the file the link points at was removed")
	}
}
