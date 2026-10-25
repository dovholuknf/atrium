//go:build integration

package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// scopeLog records the directories the worktree paths asked to have scoped.
type scopeLog struct {
	mu   sync.Mutex
	dirs []string
}

func (l *scopeLog) add(dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dirs = append(l.dirs, filepath.ToSlash(dir))
}

func (l *scopeLog) only(t *testing.T, want string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.dirs) != 1 || l.dirs[0] != filepath.ToSlash(want) {
		t.Errorf("scoped %v, want exactly [%s]", l.dirs, want)
	}
}

// EVERY PATH THAT RUNS `git worktree add` stamps this room's forwarder on the result, not only launch.
func TestAPlainWorktreeIsScopedToThisRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	base := t.TempDir()
	root := filepath.Join(base, "git")
	mkDir(t, root)
	mkDir(t, filepath.Join(root, "org"))
	git(t, root, "clone", "-q", upstream(t), filepath.Join(root, "org", "repo"))
	srv, st, h := serverFor(t)
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(filepath.Join(base, "wt")),
	}); err != nil {
		t.Fatal(err)
	}
	var log scopeLog
	srv.ScopeHub = log.add
	rec := post(t, h, "/v1/providers/github/worktree", map[string]any{"org": "org", "repo": "repo", "branch": "wip"})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	log.only(t, filepath.ToSlash(filepath.Join(base, "wt", "org", "repo", "wip")))
}

func TestAPRWorktreeIsScopedToThisRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	hn.checkout(t, "o", "r")
	var log scopeLog
	hn.srv.ScopeHub = log.add
	code, out := hn.ask(t)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	log.only(t, out["path"].(string))
}

func TestAnOpenedIssueWorktreeIsScopedToThisRoom(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	oh.checkout(t, "o", "r")
	var log scopeLog
	oh.srv.ScopeHub = log.add
	code, out := oh.open(t, "https://github.com/o/r/issues/3")
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	path, _ := out["worktree"].(string)
	if !strings.HasSuffix(path, "/o/r/issue-3") {
		t.Fatalf("worktree %q", path)
	}
	log.only(t, path)
}
