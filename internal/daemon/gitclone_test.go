package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

func cloneCall(t *testing.T, d *Daemon, id, url string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": url})
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/git/clone", strings.NewReader(string(body)))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleGitClone(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestGitCloneWithNoSCMRootSaysHowToSetIt(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "w", Worktree: "/tmp/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	code, out := cloneCall(t, d, task.ID, "https://github.com/acme/widget")
	if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "git.scm_root") {
		t.Fatalf("%d %v", code, out)
	}
}

func TestGitCloneOfAnOperatorsCloneAsksOnTheBoardOnceThenAddsTheRemotes(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "w", Worktree: "/tmp/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := d.st.SetSetting(gitsync.SettingSCMRoot, root); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(root, "github", "acme", "widget")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/acme/widget.git"}} {
		c := exec.Command("git", args...)
		c.Dir = clone
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	remotes := func() string {
		c := exec.Command("git", "remote")
		c.Dir = clone
		out, _ := c.Output()
		return strings.TrimSpace(string(out))
	}

	code, out := cloneCall(t, d, task.ID, "https://github.com/acme/widget")
	if code != http.StatusOK || out["state"] != "asked" {
		t.Fatalf("first call: %d %v", code, out)
	}
	if remotes() != "origin" {
		t.Fatalf("remotes before the yes: %q", remotes())
	}
	pend, err := d.st.PendingForTask(task.ID)
	if err != nil || len(pend) != 1 || pend[0].Tool != "atrium_git_clone" {
		t.Fatalf("pending = %+v %v", pend, err)
	}
	// Asking again is the same question, not a second one.
	if _, out := cloneCall(t, d, task.ID, "https://github.com/acme/widget"); out["state"] != "asked" {
		t.Fatalf("second call: %v", out)
	}
	if pend, _ := d.st.PendingForTask(task.ID); len(pend) != 1 {
		t.Fatalf("%d pending, want one", len(pend))
	}

	if _, err := d.decide(pend[0].ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	code, out = cloneCall(t, d, task.ID, "https://github.com/acme/widget")
	if code != http.StatusOK || out["state"] != "existing" || out["hub"] != "hub" {
		t.Fatalf("after the yes: %d %v", code, out)
	}
	if got := remotes(); !strings.Contains(got, "hub") {
		t.Fatalf("remotes after the yes: %q", got)
	}
}

func TestGitCloneDeniedLeavesTheCloneAlone(t *testing.T) {
	d := testDaemon(t)
	task, _, _ := d.st.Register(store.Observed{WireName: "w", Worktree: "/tmp/w", Runner: "claude"})
	root := t.TempDir()
	_ = d.st.SetSetting(gitsync.SettingSCMRoot, root)
	clone := filepath.Join(root, "github", "acme", "widget")
	_ = os.MkdirAll(clone, 0o755)
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/acme/widget.git"}} {
		c := exec.Command("git", args...)
		c.Dir = clone
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	cloneCall(t, d, task.ID, "https://github.com/acme/widget")
	pend, _ := d.st.PendingForTask(task.ID)
	if len(pend) != 1 {
		t.Fatalf("pending = %d", len(pend))
	}
	if _, err := d.decide(pend[0].ID, "block", "no", ""); err != nil {
		t.Fatal(err)
	}
	code, out := cloneCall(t, d, task.ID, "https://github.com/acme/widget")
	if code != http.StatusForbidden {
		t.Fatalf("%d %v", code, out)
	}
	c := exec.Command("git", "remote")
	c.Dir = clone
	if got, _ := c.Output(); strings.TrimSpace(string(got)) != "origin" {
		t.Fatalf("remotes = %q", got)
	}
}
