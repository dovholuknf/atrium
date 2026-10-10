//go:build integration

package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASayThenAtriumBlockedReachesTheLauncherOnce(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if _, code := tell(t, d, "worker", "orchestrator", "blocked: need a token"); code != http.StatusOK {
		t.Fatalf("the say answered %d", code)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token", Ended: true})
	if rec.Code != 200 || out["recorded"] != true {
		t.Fatalf("code %d, out %v", rec.Code, out)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher heard %d, want exactly the say", n)
	}
	if w, err := d.st.WorkItem(worker.ID); err == nil && w.State != "reported" {
		t.Fatalf("the work item is %q, want reported", w.State)
	}
}

func TestAtriumDoneThenASayReachesTheLauncherOnce(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	stubCommits(t, true, true)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true})
	out, code := tell(t, d, "worker", "orchestrator", "done abc1234")
	if code != http.StatusOK || out["delivered"] != "dropped" || !strings.Contains(out["note"].(string), "atrium_done") {
		t.Fatalf("the say was not dropped: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher heard %d, want exactly the report", n)
	}
}

func TestASayThatIsNotTheSameEndIsStillDelivered(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	stubCommits(t, true, true)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if out, code := tell(t, d, "worker", "orchestrator", "blocked: something else"); code != http.StatusOK || out["delivered"] == "dropped" {
		t.Fatalf("%d %v", code, out)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("the launcher heard %d, want 2", n)
	}
}

func TestAnEndingDoneTakesAnArtifactThatExistsOnTheRoom(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	dir := t.TempDir()
	if err := d.st.SetWorktree(worker.ID, filepath.ToSlash(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Artifact: "report.md",
		Recap: "done report.md", Ended: true})
	if rec.Code != 200 || out["recorded"] != true {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "report.md") {
		t.Fatalf("the launcher has %+v", msgs)
	}
}

func TestAnEndingDoneRefusesAMissingArtifactAndBothAtOnce(t *testing.T) {
	d := testDaemon(t)
	launchedPair(t, d)
	stubCommits(t, true, true)
	missing := filepath.Join(t.TempDir(), "nope.md")
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Artifact: missing, Ended: true}); rec.Code != 400 {
		t.Fatalf("a missing artifact: code %d", rec.Code)
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Artifact: missing, SHA: "abc1234",
		Ended: true}); rec.Code != 400 {
		t.Fatalf("both: code %d", rec.Code)
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone,
		Artifact: "https://example.com/r.md", Ended: true}); rec.Code != 200 {
		t.Fatalf("a URL: code %d", rec.Code)
	}
}
