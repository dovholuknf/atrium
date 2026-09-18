package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func serverFor(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	s := openStore(t)
	srv := New(s)
	return srv, s, srv.Handler()
}

func put(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(string(raw)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func post(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw := "{}"
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(raw))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// THE WHOLE REQUEST IS REFUSED, not half of it.
//
// A body that turns the toggle off AND moves the root has to leave the root
// alone. Applying half and reporting a failure is the worst of both: the caller
// reads an error and the machine has changed anyway.
func TestSaveProviderRefusedWholeOnBlockedToggle(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "git")
	wt := filepath.Join(base, "worktrees")
	mkDir(t, root)
	mkDir(t, filepath.Join(wt, "leftover"))

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(wt),
	}); err != nil {
		t.Fatal(err)
	}

	moved := filepath.ToSlash(filepath.Join(base, "elsewhere"))
	rec := put(t, h, "/v1/providers/github", map[string]any{
		"root": moved, "worktrees": false, "enabled": true,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "leftover") {
		t.Errorf("the refusal does not name what is in the way: %s", rec.Body.String())
	}

	back, err := st.Provider("github")
	if err != nil {
		t.Fatal(err)
	}
	if back.Root == moved {
		t.Error("the root moved on a refused request, which is the half-applied failure")
	}
	if !back.Worktrees {
		t.Error("the toggle went off on a refused request")
	}
}

// Turning it ON never reads the filesystem, because there is nothing to orphan.
func TestWorktreeToggleOnNeedsNoCheck(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "git")
	mkDir(t, root)
	// A worktree root that is full, and does not exist as far as the toggle is
	// concerned, because the toggle is going the other way.
	wt := filepath.Join(base, "worktrees")
	mkDir(t, filepath.Join(wt, "full-of-things"))

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := put(t, h, "/v1/providers/github", map[string]any{
		"root": filepath.ToSlash(root), "enabled": true,
		"worktrees": true, "worktree_root": filepath.ToSlash(wt),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("turning the toggle on was refused: %d %s", rec.Code, rec.Body.String())
	}
}

// Deleting a provider whose toggle is on is the same failure by a different
// door, so it gets the same refusal.
func TestDeleteProviderRefusedWhileWorktreesExist(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "git")
	wt := filepath.Join(base, "worktrees")
	mkDir(t, root)
	mkDir(t, filepath.Join(wt, "still-here"))

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(wt),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/v1/providers/github", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := st.Provider("github"); err != nil {
		t.Fatal("the provider was deleted by a refused request")
	}

	// Empty it, and the delete goes through.
	if err := os.RemoveAll(filepath.Join(wt, "still-here")); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/providers/github", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty worktree root should allow the delete, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// The check answers the same question and writes nothing, so somebody clearing
// the directory can ask whether they are done without attempting a save.
func TestCheckWorktreesSavesNothing(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "git")
	wt := filepath.Join(base, "worktrees")
	mkDir(t, root)
	mkDir(t, filepath.Join(wt, "one"))

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(wt),
	}); err != nil {
		t.Fatal(err)
	}

	rec := post(t, h, "/v1/providers/github/check-worktrees", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the check is a question, not a failure: %d", rec.Code)
	}
	var out struct {
		Clear    bool      `json:"clear"`
		Blockers []blocker `json:"blockers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Clear {
		t.Error("a directory holding something is not clear")
	}
	if len(out.Blockers) != 1 {
		t.Fatalf("expected one blocker, got %d", len(out.Blockers))
	}

	if err := os.RemoveAll(filepath.Join(wt, "one")); err != nil {
		t.Fatal(err)
	}
	rec = post(t, h, "/v1/providers/github/check-worktrees", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Clear {
		t.Error("an emptied directory should read as clear")
	}
	// Nothing about the provider changed by asking.
	p, err := st.Provider("github")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Worktrees {
		t.Error("asking the question turned the toggle off")
	}
}

// A root that cannot be read is a 200 carrying the problem, because nothing
// went wrong with the request and no row moved.
func TestDiscoverReportsAMissingRootWithoutFailing(t *testing.T) {
	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: "z:/not/here", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := post(t, h, "/v1/providers/github/discover", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "could not read that root") {
		t.Errorf("the body does not say what happened: %s", rec.Body.String())
	}
	// And it is recorded on the row, so the board can show it without asking
	// again.
	p, err := st.Provider("github")
	if err != nil {
		t.Fatal(err)
	}
	if p.LastError == "" {
		t.Error("the failure was not recorded on the provider")
	}
	if p.LastScanAt == nil {
		t.Error("the scan time was not recorded")
	}
}

// Making a worktree when the toggle is off names the toggle rather than failing
// somewhere inside git.
func TestMakeWorktreeRefusedWhenTheToggleIsOff(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "org", "repo"))

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := post(t, h, "/v1/providers/github/worktree", map[string]any{
		"org": "org", "repo": "repo", "branch": "wip",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "worktree support is off") {
		t.Errorf("the refusal does not name the toggle: %s", rec.Body.String())
	}
}

// ATRIUM DOES NOT CLONE. A repository that is not there is a sentence.
func TestMakeWorktreeRefusesToCloneAnything(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "git")
	mkDir(t, root)

	_, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(filepath.Join(base, "wt")),
	}); err != nil {
		t.Fatal(err)
	}
	rec := post(t, h, "/v1/providers/github/worktree", map[string]any{
		"org": "org", "repo": "missing", "branch": "wip",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not clone") {
		t.Errorf("the refusal should say so plainly: %s", rec.Body.String())
	}
}

// The destination is worked out from the declared layout, and a branch with a
// slash in it is flattened so it cannot leave an empty parent behind, which
// would block the feature's own toggle.
func TestWorktreeDestinationAndBranchFlattening(t *testing.T) {
	got := worktreeDest("d:/worktrees/github", "dovholuknf", "atrium", "rb1")
	if got != "d:/worktrees/github/dovholuknf/atrium/rb1" {
		t.Fatalf("got %q", got)
	}
	got = worktreeDest("d:/worktrees/github", "", "dotfiles", "feature/thing")
	if got != "d:/worktrees/github/dotfiles/feature-thing" {
		t.Fatalf("a slashed branch should flatten, got %q", got)
	}
}

// Presence travels on the read and is never written, so the row can say "not on
// disk" without having been rewritten.
func TestReposReportPresenceWithoutStoringIt(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "org", "here"))

	_, st, h := serverFor(t)
	p, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	discoverProvider(st, p)
	if _, err := st.SaveProviderRepo(store.ProviderRepo{
		Provider: "github", Org: "org", Repo: "not-cloned-yet",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/providers/github/repos", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var out struct {
		Repos []store.ProviderRepo `json:"repos"`
		Orgs  []string             `json:"orgs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Repos) != 2 {
		t.Fatalf("expected two rows, got %d", len(out.Repos))
	}
	seen := map[string]bool{}
	for _, r := range out.Repos {
		seen[r.Repo] = r.Present
	}
	if !seen["here"] {
		t.Error("a repository on disk reported as missing")
	}
	if seen["not-cloned-yet"] {
		t.Error("a repository that was only typed reported as present")
	}
	if len(out.Orgs) != 1 || out.Orgs[0] != "org" {
		t.Errorf("orgs came back as %v", out.Orgs)
	}
}
