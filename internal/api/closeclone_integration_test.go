//go:build integration

package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// openWithAClone opens o/r#7 on a room with no checkout of o/r, so the open clones it (marked as atrium's when made
// is true) and the card owns the clone.
func openWithAClone(t *testing.T, made bool) (*openHarness, string, string, string) {
	t.Helper()
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	clone := filepath.Join(t.TempDir(), "scm", "github.com", "o", "r")
	oh.srv.SCMClone = func(_ context.Context, url string) (gitsync.SCMResult, error) {
		if _, err := os.Stat(filepath.Join(clone, ".git")); err == nil {
			return gitsync.SCMResult{Path: filepath.ToSlash(clone), State: "existing"}, nil
		}
		mkDir(t, filepath.Dir(clone))
		git(t, filepath.Dir(clone), "clone", "-q", oh.up, clone)
		if made {
			git(t, clone, "config", "atrium.clone", "made")
		}
		return gitsync.SCMResult{Path: filepath.ToSlash(clone), State: "cloned"}, nil
	}
	code, out := oh.open(t, openURL)
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	return oh, out["card"].(string), out["worktree"].(string), filepath.ToSlash(clone)
}

func cloneRow(t *testing.T, st *store.Store, card string) *store.CardResource {
	t.Helper()
	rows, _ := st.Resources(card)
	for _, r := range rows {
		if r.Kind == store.ResClone {
			return r
		}
	}
	t.Fatal("the open did not record the clone it made")
	return nil
}

// u-005 decision 4: every card cleans up when closed, the clone its open made included.
func TestCloseDeletesTheCloneTheOpenMade(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git clones and a database")
	}
	t.Parallel()
	oh, card, wt, clone := openWithAClone(t, true)
	if r := cloneRow(t, oh.srv.st, card); !r.Live() || r.Ref != clone {
		t.Fatalf("clone row %+v, want %s", r, clone)
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 200 || !out.Closed || out.Left != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if exists(wt) || exists(clone) {
		t.Fatalf("left on disk: worktree %v, clone %v", exists(wt), exists(clone))
	}
	if cloneRow(t, oh.srv.st, card).Live() {
		t.Error("the clone row was not freed")
	}
}

// A clone somebody else uses stays: another worktree hangs off it, or atrium did not mark it as made.
func TestCloseKeepsACloneSomethingElseUses(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git clones and a database")
	}
	t.Parallel()
	t.Run("another worktree", func(t *testing.T) {
		t.Parallel()
		oh, card, _, clone := openWithAClone(t, true)
		other := filepath.Join(t.TempDir(), "other")
		git(t, clone, "worktree", "add", "-q", "-b", "claude/other", other)
		code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
		if code != 200 || !out.Closed {
			t.Fatalf("%d %+v", code, out)
		}
		if !exists(clone) || !exists(other) {
			t.Fatal("the shared clone was deleted")
		}
		if !strings.Contains(strings.Join(out.Warn, "\n"), "1 other worktree hangs off it") {
			t.Errorf("the close did not say why: %v", out.Warn)
		}
		if !cloneRow(t, oh.srv.st, card).Live() {
			t.Error("a kept clone should stay live for the sweep")
		}
	})
	t.Run("not marked as made", func(t *testing.T) {
		t.Parallel()
		oh, card, _, clone := openWithAClone(t, false)
		code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
		if code != 200 || !out.Closed || !exists(clone) {
			t.Fatalf("%d %+v exists=%v", code, out, exists(clone))
		}
	})
	t.Run("a commit on no remote", func(t *testing.T) {
		t.Parallel()
		oh, card, _, clone := openWithAClone(t, true)
		git(t, clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "mine")
		code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
		if code != 200 || !out.Closed || !exists(clone) {
			t.Fatalf("%d %+v exists=%v", code, out, exists(clone))
		}
		if !strings.Contains(strings.Join(out.Warn, "\n"), "on no remote") {
			t.Errorf("the close did not say why: %v", out.Warn)
		}
	})
}
