//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func reviewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "base")
	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nDEBUG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("commit", "-am", "feature")
	return filepath.ToSlash(dir)
}

func reviewCard(t *testing.T, st *store.Store, dir string, tags ...string) string {
	t.Helper()
	task, _, err := st.Register(store.Observed{WireName: "rv-" + filepath.Base(dir), Worktree: dir, Runner: "claude", PID: 4242})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTags(task.ID, tags); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func getReviewRoute(h http.Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+id+"/atrium-review", nil))
	return rec
}

func TestReviewRouteIs404UntilTheCardHoldsAReview(t *testing.T) {
	_, st, h := serverFor(t)
	id := reviewCard(t, st, reviewRepo(t), ReviewTag)
	if rec := getReviewRoute(h, id); rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := post(t, h, "/v1/tasks/"+id+"/atrium-review", map[string]any{"action": "start", "base": "main"}); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec := getReviewRoute(h, id)
	var v struct {
		Base     string           `json:"base"`
		Files    []map[string]any `json:"files"`
		Findings []map[string]any `json:"findings"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil || v.Base != "main" || len(v.Files) != 1 || v.Findings == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := getReviewRoute(h, "nosuchcard"); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown card: %d", rec.Code)
	}
}

func TestReviewActionsRunThroughTheRoute(t *testing.T) {
	_, st, h := serverFor(t)
	dir := reviewRepo(t)
	id := reviewCard(t, st, dir, ReviewTag)
	path := "/v1/tasks/" + id + "/atrium-review"
	post(t, h, path, map[string]any{"action": "start", "base": "main"})
	rec := post(t, h, path, map[string]any{
		"action": "finding", "cat": "HYGIENE", "kind": "code", "site": "a.txt:3", "title": "debug",
		"edits": []map[string]string{{"path": "a.txt", "old": "DEBUG\n", "new": ""}}, "note": "n", "footer": "f",
	})
	var out struct {
		Num   int `json:"num"`
		Lines int `json:"lines_vs_base"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Num != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := post(t, h, path, map[string]any{"action": "apply", "id": 1}); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "a.txt")); strings.Contains(string(raw), "DEBUG") {
		t.Fatalf("not applied: %q", raw)
	}
	if rec := post(t, h, path, map[string]any{"action": "dance"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
}

func TestOnlyAReviewCardCanPostToTheReviewRoute(t *testing.T) {
	_, st, h := serverFor(t)
	id := reviewCard(t, st, reviewRepo(t), "atrium:subagent")
	if rec := post(t, h, "/v1/tasks/"+id+"/atrium-review", map[string]any{"action": "start", "base": "main"}); rec.Code != http.StatusForbidden {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestAnEditOutsideTheCardIsRefusedByTheRoute(t *testing.T) {
	_, st, h := serverFor(t)
	dir := reviewRepo(t)
	id := reviewCard(t, st, dir, ReviewTag)
	path := "/v1/tasks/" + id + "/atrium-review"
	post(t, h, path, map[string]any{"action": "start", "base": "main"})
	outside := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := post(t, h, path, map[string]any{
		"action": "finding", "cat": "DEAD", "title": "t",
		"edits": []map[string]string{{"path": filepath.ToSlash(outside), "old": "keep", "new": "gone"}},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "outside") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if raw, _ := os.ReadFile(outside); string(raw) != "keep" {
		t.Fatalf("%q", raw)
	}
}
